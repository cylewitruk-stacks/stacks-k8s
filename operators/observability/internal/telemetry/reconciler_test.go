package telemetry

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	observation "github.com/cylewitruk-stacks/stacks-k8s/operators/observability/api/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/yaml"
)

// fixture supplies a complete immutable recording request.
func fixture() *observation.NetworkTelemetry {
	return &observation.NetworkTelemetry{
		ObjectMeta: metav1.ObjectMeta{Name: "capture", Namespace: "lab"},
		Spec: observation.NetworkTelemetrySpec{
			NetworkName:      "network",
			NetworkUID:       "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
			StorageSecretRef: "storage",
			Retention:        observation.Retention{Window: "24h"},
			Sources:          observation.Sources{Objects: true, Logs: true, Metrics: true},
		},
	}
}

func TestCollectorConfigurationAndScopedRoles(t *testing.T) {
	object := fixture()
	object.UID = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	config, err := CollectorConfig(object)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := yaml.UnmarshalStrict([]byte(config), &parsed); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "/var/log/pods/lab_", "X-Greptime-Log-Extract-Keys",
		"honor_labels: false", "honor_timestamps: false", "file_storage", "max_elapsed_time: 120s", "[REDACTED]",
	} {
		if !strings.Contains(config, required) {
			t.Fatalf("missing collector boundary %q", required)
		}
	}
	for _, rule := range RecorderRules("capture") {
		for _, resource := range rule.Resources {
			if resource == "secrets" {
				t.Fatal("recorder can read Secrets")
			}
			if strings.HasSuffix(resource, "/status") {
				if len(rule.ResourceNames) != 1 || rule.ResourceNames[0] != "capture" {
					t.Fatal("unscoped status writer")
				}
			} else {
				for _, verb := range rule.Verbs {
					if verb != "get" && verb != "list" && verb != "watch" {
						t.Fatal("recorder has mutation authority")
					}
				}
			}
		}
	}
}

func TestTelemetryAdmissionOwnershipAndSSAWithAPIServer(t *testing.T) {
	environment := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "..", "charts", "stacks-network-operator", "crds"),
			filepath.Join("..", "..", "..", "..", "charts", "stacks-observability-operator", "crds"),
		}, ErrorIfCRDPathMissing: true, DownloadBinaryAssets: true, DownloadBinaryAssetsVersion: "1.37.0",
		BinaryAssetsDirectory: filepath.Join(os.TempDir(), "stacks-network-operator-envtest"),
	}
	cfg, err := environment.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := environment.Stop(); err != nil {
			t.Error(err)
		}
	})
	scheme := runtime.NewScheme()
	if err := RegisterWorkloadTypes(scheme); err != nil {
		t.Fatal(err)
	}
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(c.Create(t.Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "lab"}}))
	root := &api.StacksNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "lab"},
		Spec:       api.StacksNetworkSpec{Operation: "Paused"},
	}
	must(c.Create(t.Context(), root))
	object := fixture()
	// A wrong UID cannot provision a recorder, even when the name resolves.
	must(c.Create(t.Context(), object))
	r := &Reconciler{Client: c, APIReader: c, WorkerImage: "recorder:qualified", CollectorImage: DefaultCollectorImage}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(object)}
	reconcile := func() { t.Helper(); _, err := r.Reconcile(t.Context(), request); must(err) }
	reconcile()
	pods := &appsv1.DeploymentList{}
	must(c.List(t.Context(), pods, client.InNamespace("lab")))
	if len(pods.Items) != 0 {
		t.Fatal("wrong-UID workload created")
	}
	must(c.Get(t.Context(), request.NamespacedName, object))
	object.Spec.NetworkUID = root.UID
	if err := c.Update(t.Context(), object); !apierrors.IsInvalid(err) {
		t.Fatalf("identity change admitted: %v", err)
	}
	must(c.Delete(t.Context(), object))
	object = fixture()
	object.Spec.NetworkUID = root.UID
	must(c.Create(t.Context(), object))
	reconcile()
	reconcile()
	must(c.Get(t.Context(), request.NamespacedName, object))
	if !object.Status.Admitted || len(object.OwnerReferences) != 0 {
		t.Fatal("recording admission or independent ownership broken")
	}
	// Both status writers survive; applying one must not copy or clear the other.
	recorder := &observation.NetworkTelemetry{
		TypeMeta:   metav1.TypeMeta{APIVersion: observation.GroupVersion.String(), Kind: "NetworkTelemetry"},
		ObjectMeta: metav1.ObjectMeta{Name: object.Name, Namespace: object.Namespace},
		Status: observation.NetworkTelemetryStatus{
			Recording: &observation.RecordingStatus{
				PodUID:       "pod-session",
				HeartbeatAt:  metav1.Now(),
				BackendReady: true,
			},
		},
	}
	//nolint:staticcheck // Exercise the same minimal SSA transport as production.
	must(c.Status().Patch(t.Context(), recorder, client.Apply, client.FieldOwner(RecorderFieldManager)))
	reconcile()
	must(c.Get(t.Context(), request.NamespacedName, object))
	if object.Status.Recording == nil || !object.Status.Recording.BackendReady || !object.Status.Admitted {
		t.Fatal("status writer clobbered peer")
	}
	savedUID := object.UID
	// Retention cannot mutate an already selected table's TTL through a misleading hints-only update.
	object.Spec.Retention.Window = "6h"
	if err := c.Update(t.Context(), object); !apierrors.IsInvalid(err) {
		t.Fatalf("retention change admitted: %v", err)
	}
	must(c.Get(t.Context(), request.NamespacedName, object))
	// Root deletion does not delete evidence workloads or revoke access to retained facts.
	must(c.Delete(t.Context(), root))
	stale := object.DeepCopy()
	stale.Status = observation.NetworkTelemetryStatus{}
	r.Client = staleTelemetryClient{Client: c, object: stale}
	// Even a late failed admission report must retain the already-persisted binding.
	must(r.report(t.Context(), stale, false, observation.ConditionNetworkResolved, metav1.ConditionFalse,
		observation.ReasonNetworkUnavailable, "stale rejection"))
	reconcile()
	r.Client = c
	must(c.Get(t.Context(), request.NamespacedName, object))
	if object.UID != savedUID || !object.Status.Admitted {
		t.Fatal("recording rebound or lost after root deletion")
	}
	deployment := &appsv1.Deployment{}
	key := client.ObjectKey{Namespace: "lab", Name: Name(object) + "-recorder"}
	must(c.Get(t.Context(), key, deployment))
	if !metav1.IsControlledBy(deployment, object) {
		t.Fatal("recorder is not telemetry-owned")
	}
	// Source changes remain usable after root deletion. Disabling node sources removes only owned resources.
	object.Spec.Sources = observation.Sources{Objects: true}
	must(c.Update(t.Context(), object))
	reconcile()
	collectorKey := client.ObjectKey{Namespace: "lab", Name: Name(object) + "-collector"}
	if err := c.Get(t.Context(), collectorKey, &appsv1.DaemonSet{}); !apierrors.IsNotFound(err) {
		t.Fatalf("collector not withdrawn: %v", err)
	}
	must(c.Get(t.Context(), request.NamespacedName, object))
	object.Spec.Sources.Metrics = true
	must(c.Update(t.Context(), object))
	reconcile()
	ds := &appsv1.DaemonSet{}
	must(c.Get(t.Context(), collectorKey, ds))
	for _, volume := range ds.Spec.Template.Spec.Volumes {
		if volume.HostPath != nil {
			t.Fatal("metrics-only transition retained host mount")
		}
	}
	// Unrelated admission/defaulted fields must survive both storage transitions.
	ds.Spec.Template.Spec.DNSPolicy = corev1.DNSDefault
	ds.Spec.Template.Spec.SchedulerName = "custom-scheduler"
	ds.Spec.Template.Spec.Containers[0].TerminationMessagePath = "/custom-termination-log"
	ds.Spec.Template.Spec.SecurityContext.SupplementalGroups = []int64{1234}
	must(c.Update(t.Context(), ds))
	// Both storage unions must survive real API-server apply transitions.
	for _, logs := range []bool{true, false, true} {
		must(c.Get(t.Context(), request.NamespacedName, object))
		object.Spec.Sources.Logs = logs
		must(c.Update(t.Context(), object))
		reconcile()
		must(c.Get(t.Context(), collectorKey, ds))
		if ds.Spec.Template.Spec.DNSPolicy != corev1.DNSDefault ||
			ds.Spec.Template.Spec.SchedulerName != "custom-scheduler" ||
			ds.Spec.Template.Spec.RestartPolicy != corev1.RestartPolicyAlways ||
			ds.Spec.Template.Spec.Containers[0].TerminationMessagePath != "/custom-termination-log" ||
			len(ds.Spec.Template.Spec.SecurityContext.SupplementalGroups) != 1 ||
			ds.Spec.Template.Spec.SecurityContext.SupplementalGroups[0] != 1234 {
			t.Fatal("storage transition changed unrelated Pod fields or defaults")
		}
		security := ds.Spec.Template.Spec.SecurityContext
		if logs && (*security.RunAsUser != 0 || security.RunAsNonRoot != nil || security.FSGroup != nil) {
			t.Fatal("log transition retained metrics-only security settings")
		}
		if !logs && (*security.RunAsUser != 65532 || !*security.RunAsNonRoot || *security.FSGroup != 65532) {
			t.Fatal("metrics transition did not apply non-root security settings")
		}
		for _, volume := range ds.Spec.Template.Spec.Volumes {
			if !logs && volume.HostPath != nil {
				t.Fatal("host mount survived metrics-only transition")
			}
			if volume.Name == "checkpoints" && ((volume.HostPath != nil) != logs) {
				t.Fatal("checkpoint union not replaced")
			}
		}
	}
	// A foreign object at a deterministic name is never adopted.
	config := &corev1.ConfigMap{}
	key.Name = Name(object)
	must(c.Get(t.Context(), key, config))
	must(c.Delete(t.Context(), config))
	foreign := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
		Data:       map[string]string{"keep": "foreign"},
	}
	must(c.Create(t.Context(), foreign))
	_, err = r.Reconcile(t.Context(), request)
	if err == nil {
		t.Fatal("foreign config adopted")
	}
	must(c.Get(t.Context(), key, foreign))
	if len(foreign.OwnerReferences) != 0 || foreign.Data["keep"] != "foreign" {
		t.Fatal("foreign object modified")
	}
	// The recorder credential cannot patch its source network or read private material.
	user, err := environment.AddUser(envtest.User{Name: "recorder-check"}, cfg)
	must(err)
	must(c.Create(t.Context(), &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "test-recorder-user", Namespace: "lab"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: Name(object) + "-recorder"},
		Subjects:   []rbacv1.Subject{{Kind: "User", Name: "recorder-check"}},
	}))
	restricted, err := client.New(user.Config(), client.Options{Scheme: scheme})
	must(err)
	must(restricted.Get(t.Context(), request.NamespacedName, &observation.NetworkTelemetry{}))
	if err := restricted.Get(
		t.Context(),
		client.ObjectKey{Namespace: "lab", Name: "private"},
		&corev1.Secret{},
	); !apierrors.IsForbidden(
		err,
	) {
		t.Fatalf("recorder Secret access not denied: %v", err)
	}
	root.Spec.Operation = "Stopped"
	if err := restricted.Update(t.Context(), root); !apierrors.IsForbidden(err) {
		t.Fatalf("network mutation not denied: %v", err)
	}
	recorder.SetManagedFields(nil)
	recorder.SetResourceVersion("")
	recorder.Status = observation.NetworkTelemetryStatus{Recording: recorder.Status.Recording}
	//nolint:staticcheck // Exercise the same minimal SSA transport as production.
	must(restricted.Status().Patch(t.Context(), recorder, client.Apply, client.FieldOwner(RecorderFieldManager)))
}

// staleTelemetryClient models a lagging informer while all writes still use the real API server.
type staleTelemetryClient struct {
	client.Client
	object *observation.NetworkTelemetry
}

func (c staleTelemetryClient) Get(
	ctx context.Context,
	key client.ObjectKey,
	object client.Object,
	options ...client.GetOption,
) error {
	if telemetry, ok := object.(*observation.NetworkTelemetry); ok {
		*telemetry = *c.object.DeepCopy()
		return nil
	}
	return c.Client.Get(ctx, key, object, options...)
}
