package main

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// Wire names are independently checked against contracts/investigation-preflight-v1.json.
// Keep the tool free of operator runtime dependencies.
const (
	conditionWorkloadsReady = "WorkloadsReady"
	watchNamespaceOption    = "watch-namespace"
	managerContainer        = "manager"
	chaosProfileLabel       = "network.stacks.org/chaos-profile"
	chaosProfileValue       = "network-faults-v1"
	chaosInjectAnnotation   = "chaos-mesh.org/inject"
	chaosInjectValue        = "enabled"
	clockSkewTolerance      = 5 * time.Second
)

var (
	telemetryResource = schema.GroupVersionResource{
		Group:    "observation.stacks.org",
		Version:  "v1alpha2",
		Resource: "networktelemetries",
	}
	namespaceResource  = schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}
	deploymentResource = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
)

// telemetryWire reads only the telemetry contract fields needed by this tool.
type telemetryWire struct {
	Metadata metav1.ObjectMeta `json:"metadata"`
	Spec     struct {
		NetworkName string `json:"networkName"`
		NetworkUID  string `json:"networkUID"`
	} `json:"spec"`
	Status struct {
		Admitted   bool               `json:"admitted"`
		Conditions []metav1.Condition `json:"conditions"`
		Recording  *struct {
			ObservedGeneration int64       `json:"observedGeneration"`
			BackendReady       bool        `json:"backendReady"`
			HeartbeatAt        metav1.Time `json:"heartbeatAt"`
			Sources            []struct {
				Name       string      `json:"name"`
				Available  bool        `json:"available"`
				ObservedAt metav1.Time `json:"observedAt"`
			} `json:"sources"`
		} `json:"recording"`
	} `json:"status"`
}

// inspect performs independent uncached GETs; each check reports its own boundary.
func inspect(ctx context.Context, c dynamic.Interface, r request) report {
	out := report{NetworkUID: r.NetworkUID, Passed: true}
	checkNetwork(ctx, c, r, &out)
	checkTelemetry(ctx, c, r, &out)
	checkObserver(ctx, c, r, &out)
	if r.Chaos {
		checkChaos(ctx, c, r, &out)
	}
	return out
}

// checkNetwork establishes only current root identity, never protocol health.
func checkNetwork(ctx context.Context, c dynamic.Interface, r request, out *report) {
	root, err := c.Resource(api.GroupVersion.WithResource("stacksnetworks")).
		Namespace(r.Namespace).
		Get(ctx, r.Network, metav1.GetOptions{})
	switch {
	case err != nil:
		out.add("network", checkUnknown, "network read unavailable")
	case string(root.GetUID()) != r.NetworkUID || root.GetDeletionTimestamp() != nil:
		out.add("network", checkFail, "network UID mismatch or deletion in progress")
	default:
		out.add("network", checkPass, "selected network UID exists; no protocol-health assertion")
	}
}

// checkTelemetry verifies current recording identity, health and requested sources.
func checkTelemetry(ctx context.Context, c dynamic.Interface, r request, out *report) {
	tele, err := c.Resource(telemetryResource).Namespace(r.Namespace).Get(ctx, r.Telemetry, metav1.GetOptions{})
	var t telemetryWire
	if err == nil {
		err = decodeWire(tele, &t)
	}
	if err != nil {
		out.add("telemetry", checkUnknown, "telemetry read unavailable")
	} else {
		ready := t.Metadata.DeletionTimestamp == nil && t.Spec.NetworkName == r.Network &&
			t.Spec.NetworkUID == r.NetworkUID &&
			t.Status.Admitted
		conditionReady := false
		for _, condition := range t.Status.Conditions {
			if condition.Type == conditionWorkloadsReady && condition.Status == metav1.ConditionTrue &&
				condition.ObservedGeneration == t.Metadata.Generation {
				conditionReady = true
			}
		}
		if ready && conditionReady {
			out.add("telemetry", checkPass, "matching admitted recording reports current WorkloadsReady")
		} else {
			out.add("telemetry", checkFail, "recording identity, admission or current workload readiness missing")
		}
		recording := t.Status.Recording
		if !ready || recording == nil || recording.ObservedGeneration != t.Metadata.Generation ||
			!recording.BackendReady ||
			!fresh(recording.HeartbeatAt.Time, r.MaxAgeSeconds) {
			out.add("recorder", checkFail, "current fresh backend-ready recorder heartbeat missing")
		} else {
			out.add("recorder", checkPass, "fresh heartbeat reports backend ready; continuity not asserted")
		}
		for _, required := range r.RequiredSources {
			ok := false
			if ready && recording != nil && recording.ObservedGeneration == t.Metadata.Generation {
				for _, source := range recording.Sources {
					if source.Name == required && source.Available && fresh(source.ObservedAt.Time, r.MaxAgeSeconds) {
						ok = true
					}
				}
			}
			if ok {
				out.add("source/"+required, checkPass, "fresh available source status")
			} else {
				out.add("source/"+required, checkFail, "fresh available source status missing")
			}
		}
	}
}

// checkObserver separates desired scope from the controller's rollout observation.
func checkObserver(ctx context.Context, c dynamic.Interface, r request, out *report) {
	dep, err := c.Resource(deploymentResource).
		Namespace(r.OperatorNamespace).
		Get(ctx, r.OperatorDeployment, metav1.GetOptions{})
	var d appsv1.Deployment
	if err == nil {
		err = decodeWire(dep, &d)
	}
	if err != nil {
		out.add("observer-scope", checkUnknown, "operator deployment read unavailable")
		out.add("observer-rollout", checkUnknown, "operator deployment rollout unavailable")
		return
	}
	selected := false
	for _, container := range d.Spec.Template.Spec.Containers {
		if container.Name == managerContainer {
			selected = watchScope(container.Args, r.Namespace)
		}
	}
	if selected && d.DeletionTimestamp == nil {
		out.add("observer-scope", checkPass, "requested deployment template explicitly watches namespace")
	} else {
		out.add(
			"observer-scope",
			checkFail,
			"manager scope missing, unsupported argument layout, or deployment deleting",
		)
	}
	if rolloutCurrent(&d) {
		out.add(
			"observer-rollout",
			checkPass,
			"deployment controller reports current updated and available replicas; per-Pod identity not asserted",
		)
	} else {
		out.add("observer-rollout", checkFail, "deployment rollout is absent, stale, incomplete or unavailable")
	}
}

// rolloutCurrent excludes scaled-to-zero, stale, unavailable and overlapping rollouts.
func rolloutCurrent(d *appsv1.Deployment) bool {
	desired := int32(1)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	return d.DeletionTimestamp == nil && d.Generation > 0 && desired > 0 &&
		d.Status.ObservedGeneration == d.Generation && d.Status.Replicas == desired &&
		d.Status.UpdatedReplicas == desired && d.Status.ReadyReplicas == desired &&
		d.Status.AvailableReplicas == desired && d.Status.UnavailableReplicas == 0
}

// checkChaos checks namespace enrollment, not fault admission or daemon health.
func checkChaos(ctx context.Context, c dynamic.Interface, r request, out *report) {
	ns, e := c.Resource(namespaceResource).Get(ctx, r.Namespace, metav1.GetOptions{})
	switch {
	case e != nil:
		out.add("chaos-enrollment", checkUnknown, "namespace read unavailable")
	case ns.GetDeletionTimestamp() == nil && ns.GetLabels()[chaosProfileLabel] == chaosProfileValue &&
		ns.GetAnnotations()[chaosInjectAnnotation] == chaosInjectValue:
		out.add(
			"chaos-enrollment",
			checkPass,
			"namespace opted in; exact fault manifest still requires server-side dry-run",
		)
	default:
		out.add("chaos-enrollment", checkFail, "namespace fault label or injection annotation missing")
	}
}

// decodeWire uses each consumer's own typed decoder, without an operator dependency.
func decodeWire(object *unstructured.Unstructured, target any) error {
	data, err := json.Marshal(object.Object)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

// fresh allows bounded clock skew without extending the maximum observation age.
func fresh(t time.Time, seconds int) bool { return freshAt(t, time.Now(), seconds) }

// freshAt makes the exact freshness boundaries testable without wall-clock races.
func freshAt(t, now time.Time, seconds int) bool {
	age := now.Sub(t)
	return !t.IsZero() && age >= -clockSkewTolerance && age <= time.Duration(seconds)*time.Second
}

// watchScope follows last-value-wins flag semantics for the manager's explicit scope.
func watchScope(args []string, namespace string) bool {
	scope := ""
	needsScope := false
	for _, arg := range args {
		if needsScope {
			scope = arg
			needsScope = false
			continue
		}
		if arg == "--" || !strings.HasPrefix(arg, "-") {
			break
		}
		name, value, hasValue := strings.Cut(arg, "=")
		if name == "--"+watchNamespaceOption || name == "-"+watchNamespaceOption {
			if hasValue {
				scope = value
			} else {
				needsScope = true
			}
		} else if !hasValue {
			return false // Other bare flags may consume a value; support only chart-style key=value.
		}
	}
	if needsScope {
		return false
	}
	for _, name := range strings.Split(scope, ",") {
		if strings.TrimSpace(name) == namespace {
			return true
		}
	}
	return false
}
