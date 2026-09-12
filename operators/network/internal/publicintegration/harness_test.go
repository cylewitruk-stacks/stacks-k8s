//go:build live

package publicintegration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	actions "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha2"
	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// liveConfig fixes explicit access, fixture inputs and all observation bounds.
type liveConfig struct {
	fixtureOptions
	kubeconfig, kubecontext, evidence                     string
	operatorNamespace, operatorName                       string
	operatorUID                                           types.UID
	timeout, progressTimeout, cleanupTimeout, pauseWindow time.Duration
}

// readConfig refuses implicit cluster or environment selection.
func readConfig() (liveConfig, error) {
	c := liveConfig{fixtureOptions: fixtureOptions{path: envDefault("STACKS_PUBLIC_FIXTURE", defaultFixture), namespace: os.Getenv("STACKS_PUBLIC_NAMESPACE"), variant: envDefault("STACKS_PUBLIC_VARIANT", "minimal14"), bitcoinImage: os.Getenv("STACKS_PUBLIC_BITCOIN_IMAGE"), stacksImage: os.Getenv("STACKS_PUBLIC_STACKS_IMAGE"), signerImage: os.Getenv("STACKS_PUBLIC_SIGNER_IMAGE")}, kubeconfig: os.Getenv("STACKS_PUBLIC_KUBECONFIG"), kubecontext: os.Getenv("STACKS_PUBLIC_CONTEXT"), evidence: os.Getenv("STACKS_PUBLIC_EVIDENCE_DIR"), operatorNamespace: os.Getenv("STACKS_PUBLIC_OPERATOR_NAMESPACE"), operatorName: os.Getenv("STACKS_PUBLIC_OPERATOR_NAME"), operatorUID: types.UID(os.Getenv("STACKS_PUBLIC_OPERATOR_UID"))}
	if c.kubeconfig == "" || c.kubecontext == "" || c.namespace == "" || c.bitcoinImage == "" || c.stacksImage == "" {
		return c, fmt.Errorf("explicit STACKS_PUBLIC_KUBECONFIG, CONTEXT, NAMESPACE, BITCOIN_IMAGE and STACKS_IMAGE required")
	}
	if c.signerImage == "" {
		c.signerImage = c.stacksImage
	}
	for _, field := range []struct {
		name, defaultValue string
		target             *time.Duration
		minimum, maximum   time.Duration
	}{
		{"CADENCE", "5s", &c.cadence, time.Second, time.Hour}, {"TIMEOUT", "45m", &c.timeout, time.Minute, 2 * time.Hour}, {"PROGRESS_TIMEOUT", "5m", &c.progressTimeout, 10 * time.Second, 20 * time.Minute}, {"CLEANUP_TIMEOUT", "5m", &c.cleanupTimeout, 30 * time.Second, 20 * time.Minute}, {"PAUSE_WINDOW", "20s", &c.pauseWindow, 10 * time.Second, 5 * time.Minute},
	} {
		value, err := time.ParseDuration(envDefault("STACKS_PUBLIC_"+field.name, field.defaultValue))
		if err != nil || value < field.minimum || value > field.maximum {
			return c, fmt.Errorf("invalid STACKS_PUBLIC_%s bound", field.name)
		}
		*field.target = value
	}
	if raw := os.Getenv("STACKS_PUBLIC_NODE_MAP"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &c.nodeMap); err != nil {
			return c, fmt.Errorf("STACKS_PUBLIC_NODE_MAP must be a JSON hostname map")
		}
	}
	if c.operatorNamespace != "" || c.operatorName != "" || c.operatorUID != "" {
		if c.operatorNamespace == "" || c.operatorName == "" || c.operatorUID == "" {
			return c, fmt.Errorf("operator restart requires explicit namespace, name and UID together")
		}
	}
	return c, nil
}

// envDefault reads a test option without changing process environment.
func envDefault(name, value string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return value
}

// identity is the public metadata retained for one observed resource.
type identity struct {
	Name            string    `json:"name"`
	UID             types.UID `json:"uid"`
	Generation      int64     `json:"generation"`
	ResourceVersion string    `json:"resourceVersion"`
}

func objectIdentity(o client.Object) identity {
	return identity{Name: o.GetName(), UID: o.GetUID(), Generation: o.GetGeneration(), ResourceVersion: o.GetResourceVersion()}
}

// participantEvidence includes public controller facts, never mounted key material.
type participantEvidence struct {
	Identity identity              `json:"identity"`
	Name     string                `json:"participant"`
	Kind     api.ParticipantKind   `json:"kind"`
	Status   api.ParticipantStatus `json:"status"`
}

// executionEvidence includes native public observations and retained receipt accounting.
type executionEvidence struct {
	Identity       identity                       `json:"identity"`
	ParticipantUID types.UID                      `json:"participantUID"`
	Status         bitcoin.BitcoinExecutionStatus `json:"status"`
}

// snapshot contains the complete public evidence used by a lifecycle assertion.
type snapshot struct {
	At           time.Time               `json:"at"`
	Root         identity                `json:"root"`
	Operation    string                  `json:"operation"`
	Deleting     bool                    `json:"deleting,omitempty"`
	Status       api.StacksNetworkStatus `json:"status"`
	Participants []participantEvidence   `json:"participants,omitempty"`
	Executions   []executionEvidence     `json:"executions,omitempty"`
}

// harness owns only its newly created namespace and exact network incarnation.
type harness struct {
	t                                      *testing.T
	c                                      client.Client
	config                                 liveConfig
	declared                               declarations
	namespaceUID, rootUID                  types.UID
	evidence                               string
	createdNamespace, createdRoot, cleaned bool
}

// newHarness prepares access/evidence before any create and never uses a default context.
func newHarness(t *testing.T, config liveConfig, declared declarations) (*harness, error) {
	restConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(&clientcmd.ClientConfigLoadingRules{ExplicitPath: config.kubeconfig}, &clientcmd.ConfigOverrides{CurrentContext: config.kubecontext}).ClientConfig()
	if err != nil {
		return nil, err
	}
	restConfig.Timeout = 10 * time.Second
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{clientgoscheme.AddToScheme, api.AddToScheme, bitcoin.AddToScheme, stacks.AddToScheme, actions.AddToScheme} {
		if err := add(scheme); err != nil {
			return nil, err
		}
	}
	c, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}
	directory := config.evidence
	if directory == "" {
		directory, err = os.MkdirTemp("", "stacks-publicintegration-")
		if err != nil {
			return nil, err
		}
	} else if err = os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	return &harness{t: t, c: c, config: config, declared: declared, evidence: directory}, nil
}

// create requires a fresh namespace, creates reusable declarations, then activates the root.
func (h *harness) create(ctx context.Context) error {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: h.config.namespace, Labels: map[string]string{"network.stacks.org/public-qualification": "true"}}}
	if err := h.c.Create(ctx, ns); err != nil {
		return fmt.Errorf("fresh namespace create failed; existing namespaces are never adopted: %w", err)
	}
	h.createdNamespace = true
	h.namespaceUID = ns.UID
	for _, o := range h.declared.reusable {
		if err := h.c.Create(ctx, o.DeepCopy(), client.DryRunAll); err != nil {
			return fmt.Errorf("declaration schema %s/%s: %w", o.GetKind(), o.GetName(), err)
		}
	}
	if err := h.c.Create(ctx, h.declared.root.DeepCopy(), client.DryRunAll); err != nil {
		return fmt.Errorf("network schema: %w", err)
	}
	for _, o := range h.declared.reusable {
		if err := h.c.Create(ctx, o); err != nil {
			return fmt.Errorf("create %s/%s: %w", o.GetKind(), o.GetName(), err)
		}
	}
	if err := h.c.Create(ctx, h.declared.root); err != nil {
		return err
	}
	h.createdRoot = true
	h.rootUID = h.declared.root.GetUID()
	return h.event("declarations-created", map[string]any{"namespace": h.config.namespace, "namespaceUID": h.namespaceUID, "networkUID": h.rootUID, "variant": h.config.variant, "cadence": h.config.cadence.String(), "bitcoinImage": h.config.bitcoinImage, "stacksImage": h.config.stacksImage, "signerImage": h.config.signerImage})
}

// rootUnavailable identifies definitive loss of the exact root, never a collection read error.
type rootUnavailable struct{ reason string }

func (e *rootUnavailable) Error() string { return e.reason }

// readSnapshot reads only public APIs and checks the root's exact identity first.
func (h *harness) readSnapshot(ctx context.Context) (snapshot, error) {
	var s snapshot
	root := &api.StacksNetwork{}
	if err := h.c.Get(ctx, client.ObjectKey{Namespace: h.config.namespace, Name: "network"}, root); err != nil {
		if apierrors.IsNotFound(err) && h.rootUID != "" {
			return s, &rootUnavailable{reason: "bound network root disappeared"}
		}
		return s, err
	}
	if root.UID != h.rootUID {
		return s, &rootUnavailable{reason: "bound network root was replaced"}
	}
	s = snapshot{At: time.Now().UTC(), Root: objectIdentity(root), Operation: root.Spec.Operation, Deleting: root.DeletionTimestamp != nil, Status: *root.Status.DeepCopy()}
	if failed(s) || stopped(s) {
		return s, nil
	}
	var participants api.StacksNetworkParticipantList
	if err := h.c.List(ctx, &participants, client.InNamespace(h.config.namespace)); err != nil {
		return s, err
	}
	for _, p := range participants.Items {
		if p.Spec.NetworkUID == root.UID && metav1.IsControlledBy(&p, root) {
			s.Participants = append(s.Participants, participantEvidence{Identity: objectIdentity(&p), Name: p.Spec.ParticipantName, Kind: p.Spec.Kind, Status: *p.Status.DeepCopy()})
		}
	}
	var records bitcoin.BitcoinExecutionList
	if err := h.c.List(ctx, &records, client.InNamespace(h.config.namespace)); err != nil {
		return s, err
	}
	for _, record := range records.Items {
		if record.Spec.NetworkUID == root.UID && metav1.IsControlledBy(&record, root) {
			s.Executions = append(s.Executions, executionEvidence{Identity: objectIdentity(&record), ParticipantUID: record.Spec.Participant.UID, Status: *record.Status.DeepCopy()})
		}
	}
	sort.Slice(s.Participants, func(i, j int) bool { return s.Participants[i].Name < s.Participants[j].Name })
	sort.Slice(s.Executions, func(i, j int) bool { return s.Executions[i].Identity.Name < s.Executions[j].Identity.Name })
	s.At = time.Now().UTC()
	return s, nil
}

// record writes a bounded latest snapshot and a small fixed set of stage evidence files.
func (h *harness) record(stage string, s snapshot) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(h.evidence, "latest.json"), data, 0600); err != nil {
		return err
	}
	if stage != "" {
		if err := os.WriteFile(filepath.Join(h.evidence, stage+".json"), data, 0600); err != nil {
			return err
		}
		return h.event(stage, map[string]any{"networkUID": s.Root.UID, "phase": s.Status.Phase, "generation": s.Root.Generation})
	}
	return nil
}

// wait polls fresh public evidence; failures never refresh native observation times.
func (h *harness) wait(ctx context.Context, stage string, bound time.Duration, failFast bool, accept func(snapshot) (bool, error)) (snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, bound)
	defer cancel()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var last snapshot
	var lastErr error
	ended := func() (snapshot, error) {
		_ = h.record(stage+"-timeout", last)
		return last, fmt.Errorf("%s ended: %w (last API error: %v)", stage, ctx.Err(), lastErr)
	}
	for {
		if ctx.Err() != nil {
			return ended()
		}
		s, err := h.readSnapshot(ctx)
		if ctx.Err() != nil {
			return ended()
		}
		var lost *rootUnavailable
		if failFast && errors.As(err, &lost) {
			_ = h.event(stage+"-root-unavailable", map[string]any{"networkUID": h.rootUID, "reason": lost.reason})
			return last, fmt.Errorf("%s: %w", stage, err)
		}
		if s.Root.UID != "" {
			if failFast && (failed(s) || stopped(s)) && last.Root.UID == s.Root.UID && len(last.Participants) > 0 {
				if err := h.record(stage+"-last-active", last); err != nil {
					return s, err
				}
			}
			last = s
			if e := h.record("", s); e != nil {
				return s, e
			}
			if failFast && stopped(s) {
				_ = h.record(stage+"-stopped", s)
				return s, fmt.Errorf("root stopped or deleting during %s", stage)
			}
			if failFast && failed(s) {
				_ = h.record(stage+"-failed", s)
				return s, fmt.Errorf("root Failed during %s: %s", stage, failureReason(s))
			}
		}
		if err == nil {
			done, e := accept(s)
			if e != nil {
				return s, e
			}
			if done {
				return s, h.record(stage, s)
			}
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ended()
		case <-ticker.C:
		}
	}
}

// stopped recognizes explicit terminal control without interpreting missing actor observations.
func stopped(s snapshot) bool { return s.Operation == "Stopped" || s.Deleting }

// failed recognizes either terminal root failure representation.
func failed(s snapshot) bool {
	return s.Status.Phase == "Failed" || meta.IsStatusConditionTrue(s.Status.Conditions, "Failed")
}
func failureReason(s snapshot) string {
	if c := meta.FindStatusCondition(s.Status.Conditions, "Failed"); c != nil {
		return c.Reason
	}
	return s.Status.Phase
}
func condition(s snapshot, kind string, status metav1.ConditionStatus) bool {
	c := meta.FindStatusCondition(s.Status.Conditions, kind)
	return c != nil && c.Status == status && c.ObservedGeneration == s.Root.Generation
}

// setOperation patches only the current root with optimistic concurrency.
func (h *harness) setOperation(ctx context.Context, operation string) error {
	for range 5 {
		var root api.StacksNetwork
		if err := h.c.Get(ctx, client.ObjectKey{Namespace: h.config.namespace, Name: "network"}, &root); err != nil {
			return err
		}
		if root.UID != h.rootUID {
			return fmt.Errorf("operation refused replacement root")
		}
		before := root.DeepCopy()
		root.Spec.Operation = operation
		err := h.c.Patch(ctx, &root, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
		if apierrors.IsConflict(err) {
			continue
		}
		return err
	}
	return errors.New("operation conflicted repeatedly")
}

// event retains the fixed sequence without serializing client configuration or Secrets.
func (h *harness) event(stage string, detail any) error {
	data, err := json.Marshal(struct {
		At      time.Time `json:"at"`
		Stage   string    `json:"stage"`
		Details any       `json:"details"`
	}{time.Now().UTC(), stage, detail})
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(h.evidence, "events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(data, '\n'))
	return err
}
