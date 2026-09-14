package recorder

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	observation "github.com/cylewitruk-stacks/stacks-k8s/operators/observability/api/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/observability/internal/telemetry"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Recorder owns source subscriptions and its disjoint status subtree.
type Recorder struct {
	// Dynamic supplies namespace-scoped allowlisted list/watch access.
	Dynamic dynamic.Interface
	// Client gets the exact telemetry object and patches only its execution status.
	Client client.Client
	// Telemetry is the immutable process admission snapshot.
	Telemetry *observation.NetworkTelemetry
	// PodUID attributes this process to its Kubernetes workload.
	PodUID types.UID
	// CollectorHTTPClient optionally supplies the bounded collector-health transport.
	CollectorHTTPClient *http.Client
	// Sink writes bounded already-redacted records.
	Sink              Sink
	mu                sync.Mutex
	states            map[string]observation.SourceStatus
	pendingGaps       map[string]bool
	known             map[types.UID]bool
	backendReady      bool
	attributionFull   bool
	collectorSessions map[string]float64
}

// Run resumes observation after failure with explicit discontinuity; it never resumes a mutation.
func (r *Recorder) Run(ctx context.Context) error {
	if r.Telemetry == nil || !r.Telemetry.Status.Admitted || r.PodUID == "" || r.Sink == nil {
		return fmt.Errorf("recorder requires admitted identity and backend")
	}
	r.states = map[string]observation.SourceStatus{}
	r.collectorSessions = map[string]float64{}
	r.pendingGaps = map[string]bool{}
	r.known = map[types.UID]bool{r.Telemetry.Spec.NetworkUID: true}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var workers sync.WaitGroup
	if r.Telemetry.Spec.Sources.Objects {
		for _, source := range sources {
			workers.Go(func() { r.observe(ctx, source) })
		}
	}
	defer workers.Wait()
	// A process start is always a coverage boundary, including a graceful rollout.
	r.gap(ctx, SourceRecorder, "Recorder started; continuity before this process is unverified")
	configuration, _ := json.Marshal(map[string]any{
		"telemetryUID": r.Telemetry.UID, "generation": r.Telemetry.Generation,
		"spec": r.Telemetry.Spec, "tablePrefix": telemetry.TablePrefix(r.Telemetry),
	})
	r.emit(ctx, Record{
		Time: time.Now(), NetworkUID: string(r.Telemetry.Spec.NetworkUID), ObjectUID: string(r.Telemetry.UID),
		PodUID: string(r.PodUID), Source: SourceRecorder, EventType: EventConfiguration, Body: string(configuration),
	})
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		if err := r.publish(ctx); err != nil {
			cancel()
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if telemetry.NeedsCollector(r.Telemetry) {
				r.collectorHealth(ctx)
			}
			r.emit(ctx, Record{
				Time:       time.Now(),
				NetworkUID: string(r.Telemetry.Spec.NetworkUID),
				PodUID:     string(r.PodUID),
				Source:     SourceRecorder,
				EventType:  EventHeartbeat,
				Body:       `{"scope":"recorder heartbeat only"}`,
			})
		}
	}
}

// observe re-lists after interruption and marks every unaccounted boundary before exporting new snapshots.
func (r *Recorder) observe(ctx context.Context, source schema.GroupVersionResource) {
	name := sourceID(source)
	resource := r.Dynamic.Resource(source).Namespace(r.Telemetry.Namespace)
	for ctx.Err() == nil {
		r.gap(ctx, name, "Source subscription starts or reconnects; intermediate writes may be absent")
		rv, err := r.snapshot(ctx, resource, name)
		if err == nil {
			timeout := int64(120)
			stream, watchErr := resource.Watch(ctx, metav1.ListOptions{
				ResourceVersion: rv, TimeoutSeconds: &timeout,
				AllowWatchBookmarks: true,
			})
			err = watchErr
			if watchErr == nil {
				r.consume(ctx, stream, name)
				stream.Stop()
			}
		}
		r.sourceError(name, err)
		delay := 5 * time.Second
		if apierrors.IsNotFound(err) || apierrors.IsForbidden(err) {
			delay = time.Minute
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// snapshot paginates current state without claiming the list contains intermediate transitions.
func (r *Recorder) snapshot(ctx context.Context, resource dynamic.ResourceInterface, source string) (string, error) {
	continuation, rv := "", ""
	for {
		readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		list, err := resource.List(readCtx, metav1.ListOptions{Limit: 200, Continue: continuation})
		cancel()
		if err != nil {
			return "", err
		}
		for i := range list.Items {
			r.object(ctx, &list.Items[i], source, EventSnapshot)
		}
		rv = list.GetResourceVersion()
		continuation = list.GetContinue()
		if continuation == "" {
			r.available(source, true)
			return rv, nil
		}
	}
}

// consume accepts only typed public objects; watch errors force a marked re-list.
func (r *Recorder) consume(ctx context.Context, stream watch.Interface, source string) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-stream.ResultChan():
			if !ok || event.Type == watch.Error {
				return
			}
			if event.Type == watch.Bookmark {
				r.available(source, true)
				continue
			}
			object, ok := event.Object.(*unstructured.Unstructured)
			if !ok {
				return
			}
			r.object(ctx, object, source, string(event.Type))
			r.available(source, true)
		}
	}
}

// object filters exact network identity before redaction or persistence.
func (r *Recorder) object(ctx context.Context, o *unstructured.Unstructured, source, event string) {
	if !r.belongs(o) {
		return
	}
	r.mu.Lock()
	exhausted := false
	if len(r.known) < 4096 {
		r.known[o.GetUID()] = true
	} else if !r.known[o.GetUID()] && !r.attributionFull {
		r.attributionFull = true
		exhausted = true
	}
	r.mu.Unlock()
	if exhausted {
		r.gap(ctx, SourceRecorder, "Event attribution capacity exhausted; Events for new source UIDs may be absent")
	}
	participant := o.GetLabels()[api.LabelParticipantUID]
	if o.GetKind() == api.KindStacksNetworkParticipant {
		participant = string(o.GetUID())
	}
	pod := ""
	if o.GetKind() == "Pod" {
		pod = string(o.GetUID())
	}
	r.emit(ctx, Record{
		Time: time.Now(), NetworkUID: string(r.Telemetry.Spec.NetworkUID), ParticipantUID: participant,
		PodUID: pod, ObjectUID: string(o.GetUID()), Source: source, EventType: event, Body: publicBody(o),
	})
}

// belongs distinguishes authority-derived Kubernetes identity from untrusted source payload fields.
func (r *Recorder) belongs(o *unstructured.Unstructured) bool {
	uid := string(r.Telemetry.Spec.NetworkUID)
	if o.GetNamespace() != r.Telemetry.Namespace {
		return false
	}
	if o.GetKind() == api.KindStacksNetwork {
		return string(o.GetUID()) == uid
	}
	if o.GetKind() == "Event" {
		involved, _, _ := unstructured.NestedString(o.Object, "involvedObject", "uid")
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.known[types.UID(involved)]
	}
	if o.GetLabels()[api.LabelNetworkUID] == uid {
		return true
	}
	for _, path := range [][]string{{"spec", "networkUID"}, {"spec", "source", "networkUID"}} {
		value, _, _ := unstructured.NestedString(o.Object, path...)
		if value == uid {
			return true
		}
	}
	for _, owner := range o.GetOwnerReferences() {
		if string(owner.UID) == uid {
			return true
		}
	}
	return false
}

// gap retains a conservative discontinuity and retries its marker after backend failure.
func (r *Recorder) gap(ctx context.Context, source, message string) {
	r.mu.Lock()
	state := r.states[source]
	state.Name = source
	state.Reason = observation.SourceUnavailable
	state.Available = false
	state.ObservedAt = metav1.Now()
	state.Gaps++
	r.states[source] = state
	r.pendingGaps[source] = true
	r.mu.Unlock()
	r.emit(ctx, Record{
		Time: time.Now(), NetworkUID: string(r.Telemetry.Spec.NetworkUID), Source: source,
		EventType: EventGap, Body: message,
	})
}

// emit reports uncertainty on export failure and writes a gap before the next ordinary record.
func (r *Recorder) emit(ctx context.Context, record Record) {
	r.mu.Lock()
	pending := r.pendingGaps[record.Source]
	r.mu.Unlock()
	if pending && record.EventType != EventGap {
		gap := record
		gap.EventType = EventGap
		gap.Body = "Prior export or source interval is unaccounted; duplicates and loss are possible"
		if err := r.Sink.Write(ctx, gap); err != nil {
			r.exportResult(record.Source, false)
			return
		}
	}
	err := r.Sink.Write(ctx, record)
	r.exportResult(record.Source, err == nil)
}

// exportResult keeps source-specific outage state separate from the most recent backend acknowledgement.
func (r *Recorder) exportResult(source string, success bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.backendReady = success
	if source == SourceRecorder {
		state := r.states[source]
		state.Name = source
		state.Available = success
		state.ObservedAt = metav1.Now()
		state.Reason = observation.SourceUnavailable
		if success {
			state.Reason = observation.SourceAvailable
		}
		r.states[source] = state
	}
	if !success && !r.pendingGaps[source] {
		state := r.states[source]
		state.Name = source
		state.Reason = observation.SourceUnavailable
		state.ObservedAt = metav1.Now()
		state.Gaps++
		r.states[source] = state
	}
	r.pendingGaps[source] = !success
}

// available updates source freshness even when the watched resource has no new events.
func (r *Recorder) available(source string, available bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.states[source]
	state.Name = source
	state.Available = available
	state.Reason = observation.SourceUnavailable
	if available {
		state.Reason = observation.SourceAvailable
	}
	state.ObservedAt = metav1.Now()
	r.states[source] = state
}

// publish fresh-reads its declaration and applies only status.recording at a bounded cadence.
func (r *Recorder) publish(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current := &observation.NetworkTelemetry{}
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(r.Telemetry), current); err != nil {
		return err
	}
	if current.UID != r.Telemetry.UID || current.Generation != r.Telemetry.Generation ||
		!current.DeletionTimestamp.IsZero() {
		return fmt.Errorf("recording declaration changed; waiting for the replacement collector configuration")
	}
	r.mu.Lock()
	status := observation.RecordingStatus{
		ObservedGeneration: r.Telemetry.Generation,
		PodUID:             r.PodUID,
		HeartbeatAt:        metav1.Now(),
		BackendReady:       r.backendReady,
	}
	for _, source := range r.states {
		status.Sources = append(status.Sources, source)
	}
	r.mu.Unlock()
	sort.Slice(status.Sources, func(i, j int) bool { return status.Sources[i].Name < status.Sources[j].Name })
	value, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&status)
	if err != nil {
		return err
	}
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": observation.GroupVersion.String(),
		"kind":       observation.KindNetworkTelemetry, "metadata": map[string]any{
			"name": current.Name, "namespace": current.Namespace,
			"uid": string(current.UID),
		}, "status": map[string]any{"recording": value},
	}}
	return r.Client.Status().
		//nolint:staticcheck // Minimal SSA matches the public operator until generated apply configurations are introduced.
		Patch(ctx, u, client.Apply, client.FieldOwner(telemetry.RecorderFieldManager), client.ForceOwnership)
}

// sourceError keeps optional API absence distinct from permission and transport failures.
func (r *Recorder) sourceError(source string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.states[source]
	state.Name = source
	state.Available = false
	state.ObservedAt = metav1.Now()
	state.Reason = observation.SourceReadUnavailable
	switch {
	case err == nil:
		state.Reason = observation.SourceWatchInterrupted
	case apierrors.IsNotFound(err):
		state.Reason = observation.SourceAPINotInstalled
	case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
		state.Reason = observation.SourceAccessDenied
	}
	r.states[source] = state
}
