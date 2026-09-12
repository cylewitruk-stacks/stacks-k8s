package topology

import (
	"os"
	"path/filepath"
	"testing"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	observation "github.com/cylewitruk-stacks/stacks-k8s/operators/observability/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestParticipantObservationWithAPIServerAndReadOnlyRBAC(t *testing.T) {
	environment := &envtest.Environment{CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "..", "charts", "stacks-network-operator", "crds"), filepath.Join("..", "..", "..", "..", "charts", "stacks-observability-operator", "crds")}, ErrorIfCRDPathMissing: true, DownloadBinaryAssets: true, DownloadBinaryAssetsVersion: "1.37.0", BinaryAssetsDirectory: filepath.Join(os.TempDir(), "stacks-network-operator-envtest")}
	configuration, err := environment.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := environment.Stop(); err != nil {
			t.Error(err)
		}
	})
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = rbacv1.AddToScheme(scheme)
	_ = observation.AddToScheme(scheme)
	AddNetworkTypes(scheme)
	admin, err := client.New(configuration, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(admin.Create(t.Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}))
	f := participantFixture("BitcoinNode")
	create := func(object client.Object) {
		object.SetUID("")
		object.SetResourceVersion("")
		object.SetGeneration(0)
		must(admin.Create(t.Context(), object))
	}
	rootStatus := f.root.Status
	f.root.Status = api.StacksNetworkStatus{}
	create(f.root)
	f.participant.Spec.NetworkUID = f.root.UID
	f.participant.OwnerReferences[0].UID = f.root.UID
	pStatus := f.participant.Status
	f.participant.Status = api.ParticipantStatus{}
	create(f.participant)
	rootStatus.Identities[0].UID = f.participant.UID
	f.root.Status = rootStatus
	must(admin.Status().Update(t.Context(), f.root))
	owned := func(object client.Object) {
		object.GetLabels()["network.stacks.org/network-uid"] = string(f.root.UID)
		object.GetLabels()["network.stacks.org/participant-uid"] = string(f.participant.UID)
		owners := object.GetOwnerReferences()
		owners[0].UID = f.participant.UID
		object.SetOwnerReferences(owners)
	}
	owned(f.workload)
	f.workload.Spec.Selector.MatchLabels["network.stacks.org/network-uid"] = string(f.root.UID)
	f.workload.Spec.Selector.MatchLabels["network.stacks.org/participant-uid"] = string(f.participant.UID)
	f.workload.Spec.Template.Labels["network.stacks.org/network-uid"] = string(f.root.UID)
	f.workload.Spec.Template.Labels["network.stacks.org/participant-uid"] = string(f.participant.UID)
	workloadStatus := f.workload.Status
	f.workload.Status = appsv1.StatefulSetStatus{}
	create(f.workload)
	workloadStatus.Replicas = 1
	workloadStatus.ObservedGeneration = f.workload.Generation
	f.workload.Status = workloadStatus
	must(admin.Status().Update(t.Context(), f.workload))
	f.pod.Labels["network.stacks.org/network-uid"] = string(f.root.UID)
	f.pod.Labels["network.stacks.org/participant-uid"] = string(f.participant.UID)
	f.pod.OwnerReferences[0].UID = f.workload.UID
	podStatus := f.pod.Status
	f.pod.Status = corev1.PodStatus{}
	create(f.pod)
	f.pod.Status = podStatus
	must(admin.Status().Update(t.Context(), f.pod))
	for _, service := range f.services {
		owned(service)
		service.Spec.Selector["network.stacks.org/network-uid"] = string(f.root.UID)
		service.Spec.Selector["network.stacks.org/participant-uid"] = string(f.participant.UID)
		create(service)
	}
	owned(f.report)
	f.report.Data = nil
	create(f.report)
	pStatus.Runtime.PodRef.UID = f.pod.UID
	pStatus.Runtime.WorkloadRefs[0].UID = f.workload.UID
	pStatus.Runtime.ObservedGeneration = f.participant.Generation
	f.participant.Status = pStatus
	f.refreshReport()
	must(admin.Update(t.Context(), f.report))
	must(admin.Status().Update(t.Context(), f.participant))
	user, err := environment.AddUser(envtest.User{Name: "observation-reader"}, configuration)
	must(err)
	role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "observation-read", Namespace: testNamespace}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{"network.stacks.org"}, Resources: []string{"stacksnetworks", "stacksnetworkparticipants"}, Verbs: []string{"get", "list"}}, {APIGroups: []string{""}, Resources: []string{"pods", "services", "configmaps"}, Verbs: []string{"get", "list"}}, {APIGroups: []string{"apps"}, Resources: []string{"statefulsets"}, Verbs: []string{"get", "list"}}}}
	must(admin.Create(t.Context(), role))
	must(admin.Create(t.Context(), &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: role.Name, Namespace: testNamespace}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: role.Name}, Subjects: []rbacv1.Subject{{Kind: "User", APIGroup: rbacv1.GroupName, Name: "observation-reader"}}}))
	bounded, err := client.New(user.Config(), client.Options{Scheme: scheme})
	must(err)
	reader := Reader{APIReader: bounded}
	snapshot, err := reader.Observe(t.Context(), testNamespace, "network", "")
	must(err)
	if snapshot.Binding.InventoryDigest != "" || snapshot.Actors[0].ConfigurationEvidence != "controller-reported" {
		t.Fatalf("evidence boundary: %+v", snapshot)
	}
	// Protocol health updates are real status writes, but not a new actor identity.
	var currentRoot api.StacksNetwork
	must(admin.Get(t.Context(), client.ObjectKeyFromObject(f.root), &currentRoot))
	previousVersion := currentRoot.ResourceVersion
	currentRoot.Status.Phase = "Initializing"
	must(admin.Status().Update(t.Context(), &currentRoot))
	if currentRoot.ResourceVersion == previousVersion {
		t.Fatal("status update did not reach the API server")
	}
	afterHeartbeat, err := reader.Observe(t.Context(), testNamespace, "network", snapshot.Binding.SnapshotDigest)
	must(err)
	if afterHeartbeat.Binding != snapshot.Binding {
		t.Fatal("protocol status changed the actor snapshot identity")
	}
	if err := bounded.Get(t.Context(), client.ObjectKey{Namespace: testNamespace, Name: "configuration"}, &corev1.Secret{}); !apierrors.IsForbidden(err) {
		t.Fatalf("Secret access must be forbidden: %v", err)
	}
	if err := bounded.Update(t.Context(), f.participant); !apierrors.IsForbidden(err) {
		t.Fatalf("participant mutation must be forbidden: %v", err)
	}
	object := &observation.NetworkObservation{ObjectMeta: metav1.ObjectMeta{Name: "current", Namespace: testNamespace}, Spec: observation.NetworkObservationSpec{NetworkRef: observation.LocalObjectReference{Name: "network"}, ExpectedSnapshotDigest: snapshot.Binding.SnapshotDigest}}
	must(admin.Create(t.Context(), object))
	object.Status = observation.NetworkObservationStatus{ObservedGeneration: object.Generation, Phase: observation.ObservationReady, Binding: &snapshot.Binding, Actors: snapshot.Actors}
	must(admin.Status().Update(t.Context(), object))
	actual := &observation.NetworkObservation{}
	must(admin.Get(t.Context(), client.ObjectKeyFromObject(object), actual))
	if actual.Status.Binding.SnapshotDigest != snapshot.Binding.SnapshotDigest || actual.Status.Actors[0].ContainerID != snapshot.Actors[0].ContainerID || actual.Status.Actors[0].ConfigurationUID != "config-uid" {
		t.Fatalf("schema lost snapshot identity: %+v", actual.Status)
	}
	// Real REST decoding must not retain fields omitted after an API status update.
	t.Run("allocation removed during consistency pass", func(t *testing.T) {
		before := currentRoot.DeepCopy()
		currentRoot.Status.Identities = nil
		must(admin.Status().Update(t.Context(), &currentRoot))
		reads := directRead{reader: bounded, objects: []client.Object{before}}
		if err := reads.stable(t.Context()); !IsInconclusive(err) {
			t.Fatalf("removed allocation survived REST decoding: %v", err)
		}
		currentRoot.Status.Identities = before.Status.Identities
		must(admin.Status().Update(t.Context(), &currentRoot))
	})
	t.Run("configuration map entry removed during consistency pass", func(t *testing.T) {
		var report corev1.ConfigMap
		must(admin.Get(t.Context(), client.ObjectKeyFromObject(f.report), &report))
		before := report.DeepCopy()
		delete(report.Data, "report.json")
		must(admin.Update(t.Context(), &report))
		reads := directRead{reader: bounded, objects: []client.Object{before}}
		if err := reads.stable(t.Context()); !IsInconclusive(err) {
			t.Fatalf("removed report entry survived REST decoding: %v", err)
		}
		report.Data = before.Data
		must(admin.Update(t.Context(), &report))
	})
	// The API assigns a new Pod UID to the same name; stale participant status cannot verify it.
	must(admin.Delete(t.Context(), f.pod, client.GracePeriodSeconds(0)))
	f.pod.ResourceVersion = ""
	f.pod.UID = ""
	f.pod.Status = corev1.PodStatus{}
	must(admin.Create(t.Context(), f.pod))
	if _, err := reader.Observe(t.Context(), testNamespace, "network", ""); !IsInconclusive(err) {
		t.Fatalf("same-name Pod replacement accepted: %v", err)
	}
}
