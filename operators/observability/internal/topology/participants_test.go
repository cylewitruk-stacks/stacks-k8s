package topology

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestParticipantSnapshotVerifiesAllActorKindsWithoutGlobalHealth(t *testing.T) {
	for _, kind := range []api.ParticipantKind{"BitcoinNode", "StacksNode", "StacksSigner"} {
		t.Run(string(kind), func(t *testing.T) {
			fixture := participantFixture(kind)
			// Physical identity remains observable during pause or a root protocol failure.
			fixture.root.Spec.Operation = "Paused"
			fixture.root.Status.Phase = "Failed"
			fixture.root.Status.Conditions = []metav1.Condition{
				{Type: "Operational", Status: metav1.ConditionFalse, Reason: "ProtocolUnavailable"},
			}
			reader := fixtureReader(t, fixture)
			snapshot, err := reader.Observe(t.Context(), testNamespace, "network", "")
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.Binding.NetworkAPIVersion != api.GroupVersion.String() ||
				snapshot.Binding.InventoryDigest != "" ||
				len(snapshot.Binding.SnapshotDigest) != 71 ||
				len(snapshot.Actors) != 1 {
				t.Fatalf("snapshot: %+v", snapshot)
			}
			actor := snapshot.Actors[0]
			if actor.ResourceUID != fixture.participant.UID || actor.ContainerID != "containerd://process" ||
				actor.ConfigurationUID != "config-uid" ||
				actor.ConfigurationEvidence != "controller-reported" ||
				actor.ConfigurationReportUID != fixture.report.UID ||
				actor.RuntimeImageID != testImageID ||
				len(actor.Services) != len(fixture.services) {
				t.Fatalf("actor: %+v", actor)
			}
			// There is deliberately no Secret object in the API reader.
			if _, err := reader.Observe(
				t.Context(),
				testNamespace,
				"network",
				snapshot.Binding.SnapshotDigest,
			); err != nil {
				t.Fatal(err)
			}
			if _, err := reader.Observe(t.Context(), testNamespace, "network", testDigest); !IsInconclusive(err) {
				t.Fatalf("wrong snapshot digest: %v", err)
			}
		})
	}
}

func TestParticipantSnapshotRejectsIdentityDrift(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*participantObjects)
		pending bool
	}{
		{"allocation missing", func(f *participantObjects) { f.root.Status.Identities = nil }, true},
		{"removed identity", func(f *participantObjects) { f.root.Status.Identities[0].Removing = true }, false},
		{
			"same-name participant replacement",
			func(f *participantObjects) { f.participant.UID = "replacement" },
			true,
		},
		{"participant owner version", func(f *participantObjects) {
			f.participant.OwnerReferences[0].APIVersion = "network.stacks.org/v1alpha1"
		}, false},
		{
			"participant owner name",
			func(f *participantObjects) { f.participant.OwnerReferences[0].Name = "different" },
			false,
		},
		{"missing admission", func(f *participantObjects) { f.participant.Status.Admission = nil }, true},
		{"tampered complete policy", func(f *participantObjects) {
			f.participant.Status.Admission.Configuration.StacksNode.Image = ptr.To("changed")
		}, false},
		{
			"runtime old generation",
			func(f *participantObjects) { f.participant.Status.Runtime.ObservedGeneration-- },
			true,
		},
		{"terminated actor", func(f *participantObjects) { f.participant.Status.Runtime.Terminated = true }, true},
		{
			"runtime old policy",
			func(f *participantObjects) { f.participant.Status.Runtime.PolicyDigest = testConfig },
			false,
		},
		{"workload replacement", func(f *participantObjects) { f.workload.UID = "other" }, false},
		{"workload owner", func(f *participantObjects) { f.workload.OwnerReferences[0].UID = "other" }, false},
		{"workload pending", func(f *participantObjects) { f.workload.Status.ReadyReplicas = 0 }, true},
		{"pod replacement", func(f *participantObjects) { f.pod.UID = "other" }, false},
		{"pod owner", func(f *participantObjects) { f.pod.OwnerReferences[0].Name = "other" }, false},
		{"pod readiness", func(f *participantObjects) { f.pod.Status.Conditions = nil }, true},
		{
			"container restart",
			func(f *participantObjects) { f.pod.Status.ContainerStatuses[0].ContainerID = "containerd://new" },
			false,
		},
		{
			"terminated container",
			func(f *participantObjects) { f.pod.Status.ContainerStatuses[0].State.Running = nil },
			false,
		},
		{
			"runtime image",
			func(f *participantObjects) { f.pod.Status.ContainerStatuses[0].ImageID = testConfig },
			false,
		},
		{"requested image", func(f *participantObjects) { f.pod.Spec.Containers[0].Image = "changed" }, false},
		{
			"policy annotation",
			func(f *participantObjects) { f.pod.Annotations[policyAnnotation] = testConfig },
			false,
		},
		{
			"public config annotation",
			func(f *participantObjects) { f.pod.Annotations[configurationAnnotation] = testImageID },
			false,
		},
		{"mount name", func(f *participantObjects) { f.pod.Spec.Volumes[0].Secret.SecretName = "other" }, false},
		{"mount absent", func(f *participantObjects) { f.pod.Spec.Containers[0].VolumeMounts = nil }, false},
		{"service owner", func(f *participantObjects) { f.services[0].OwnerReferences[0].UID = "other" }, false},
		{
			"service selector",
			func(f *participantObjects) { delete(f.services[0].Spec.Selector, "network.stacks.org/participant-uid") },
			false,
		},
		{
			"service extra selector",
			func(f *participantObjects) { f.services[0].Spec.Selector["other"] = "missing" },
			false,
		},
		{
			"report config UID",
			func(f *participantObjects) { f.participant.Status.Runtime.ConfigRef.UID = "new-config" },
			false,
		},
		{"report changed content", func(f *participantObjects) { f.report.Data["input.json"] += " " }, false},
		{"report missing", func(f *participantObjects) { f.report.Data = nil }, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := participantFixture("StacksNode")
			test.mutate(f)
			_, err := fixtureReader(t, f).Observe(t.Context(), testNamespace, "network", "")
			if test.pending {
				if !IsNotReady(err) {
					t.Fatalf("want pending, got %v", err)
				}
			} else if !IsInconclusive(err) {
				t.Fatalf("want inconclusive, got %v", err)
			}
		})
	}
}

func TestParticipantSnapshotRejectsUnknownPolicyFields(t *testing.T) {
	f := participantFixture("StacksNode")
	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(f.participant)
	if err != nil {
		t.Fatal(err)
	}
	if err := unstructured.SetNestedField(
		raw,
		"unexpected",
		"status",
		"admission",
		"configuration",
		"stacksNode",
		"futurePolicy",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeParticipant(
		&unstructured.Unstructured{Object: raw},
	); err == nil ||
		!strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field accepted: %v", err)
	}
}

func TestParticipantSnapshotChangesOnAcceptedProcessAndServiceRoll(t *testing.T) {
	f := participantFixture("BitcoinNode")
	before, err := fixtureReader(t, f).Observe(t.Context(), testNamespace, "network", "")
	if err != nil {
		t.Fatal(err)
	}
	f.pod.Status.ContainerStatuses[0].ContainerID = "containerd://replacement"
	f.participant.Status.Runtime.ContainerID = "containerd://replacement"
	after, err := fixtureReader(t, f).Observe(t.Context(), testNamespace, "network", "")
	if err != nil {
		t.Fatal(err)
	}
	if before.Binding.SnapshotDigest == after.Binding.SnapshotDigest {
		t.Fatal("process replacement hidden")
	}
	f.services[0].UID = "replacement-service"
	serviceRoll, err := fixtureReader(t, f).Observe(t.Context(), testNamespace, "network", "")
	if err != nil {
		t.Fatal(err)
	}
	if after.Binding.SnapshotDigest == serviceRoll.Binding.SnapshotDigest {
		t.Fatal("Service replacement hidden")
	}
}

func TestParticipantSnapshotRejectsMidObservationDrift(t *testing.T) {
	for _, resource := range []string{"root", "participant", "pod", "report"} {
		t.Run(resource, func(t *testing.T) {
			reader := fixtureReader(t, participantFixture("StacksSigner"))
			reader.APIReader = &driftReader{Reader: reader.APIReader, resource: resource}
			if _, err := reader.Observe(t.Context(), testNamespace, "network", ""); !IsInconclusive(err) {
				t.Fatalf("mid-observation %s change: %v", resource, err)
			}
		})
	}
}

// participantObjects is one public actor chain with no private objects.
type participantObjects struct {
	root        *api.StacksNetwork
	participant *api.StacksNetworkParticipant
	workload    *appsv1.StatefulSet
	pod         *corev1.Pod
	services    []*corev1.Service
	report      *corev1.ConfigMap
}

func participantFixture(kind api.ParticipantKind) *participantObjects {
	f := &participantObjects{}
	meta := func(name string, uid types.UID) metav1.ObjectMeta {
		return metav1.ObjectMeta{Name: name, Namespace: testNamespace, UID: uid, Generation: 1}
	}
	f.root = &api.StacksNetwork{
		ObjectMeta: meta("network", "network-v2"),
		Spec: api.StacksNetworkSpec{
			Operation: "Running",
			Participants: []api.Participant{
				{Name: "actor", Kind: kind, Definition: api.Definition{Inline: &api.Configuration{}}},
			},
		},
		Status: api.StacksNetworkStatus{
			Identities: []api.InstanceIdentity{{Name: "actor", UID: "participant-uid"}},
		},
	}
	config := api.Configuration{}
	fields := common.ActorFields{Image: ptr.To("native:release")}
	//nolint:exhaustive // Fixture wire values remain independent of production enum constants.
	switch kind {
	case "BitcoinNode":
		config.BitcoinNode = &bitcoin.BitcoinNodeSpec{ActorFields: fields}
	case "StacksNode":
		config.StacksNode = &stacks.StacksNodeSpec{ActorFields: fields}
	case "StacksSigner":
		config.StacksSigner = &stacks.StacksSignerSpec{ActorFields: fields}
	}
	f.root.Spec.Participants[0].Definition.Inline = &config
	encoded, _ := json.Marshal(config)
	policy := bytesDigest(encoded)
	f.participant = &api.StacksNetworkParticipant{
		TypeMeta:   metav1.TypeMeta{APIVersion: api.GroupVersion.String(), Kind: "StacksNetworkParticipant"},
		ObjectMeta: meta("allocated-actor", "participant-uid"),
		Spec: api.StacksNetworkParticipantSpec{
			NetworkUID:      f.root.UID,
			ParticipantName: "actor",
			Kind:            kind,
			Configuration:   config,
			Source:          api.Source{Generation: 1, Digest: policy},
		},
		Status: api.ParticipantStatus{Admission: &api.Admission{PolicyDigest: policy, Configuration: config}},
	}
	f.participant.OwnerReferences = ownerReference(api.GroupVersion.String(), "StacksNetwork", f.root.Name, f.root.UID)
	labels := map[string]string{
		"network.stacks.org/network-uid":      string(f.root.UID),
		"network.stacks.org/participant":      "actor",
		"network.stacks.org/participant-uid":  string(f.participant.UID),
		"network.stacks.org/participant-kind": string(kind),
		"network.stacks.org/role":             "actor",
	}
	owned := func(name string, uid types.UID) metav1.ObjectMeta {
		result := meta(name, uid)
		result.Labels = maps.Clone(labels)
		result.OwnerReferences = ownerReference(
			api.GroupVersion.String(),
			"StacksNetworkParticipant",
			f.participant.Name,
			f.participant.UID,
		)
		return result
	}
	annotations := map[string]string{policyAnnotation: policy, configurationAnnotation: testConfig}
	spec := corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name:         actorContainerV2(kind),
				Image:        *fields.Image,
				VolumeMounts: []corev1.VolumeMount{{Name: "config", MountPath: "/config", ReadOnly: true}},
			},
		},
		Volumes: []corev1.Volume{
			{
				Name:         "config",
				VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "configuration"}},
			},
		},
	}
	f.workload = &appsv1.StatefulSet{
		ObjectMeta: owned("workload", "workload-uid"),
		Spec: appsv1.StatefulSetSpec{
			Replicas:    ptr.To[int32](1),
			ServiceName: "p2p",
			Selector:    &metav1.LabelSelector{MatchLabels: maps.Clone(labels)},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: maps.Clone(labels), Annotations: maps.Clone(annotations)},
				Spec:       *spec.DeepCopy(),
			},
		},
		Status: appsv1.StatefulSetStatus{
			ObservedGeneration: 1,
			ReadyReplicas:      1,
			CurrentRevision:    "revision",
			UpdateRevision:     "revision",
		},
	}
	f.pod = &corev1.Pod{
		ObjectMeta: meta("workload-0", "pod-uid"),
		Spec:       spec,
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:        actorContainerV2(kind),
					Image:       *fields.Image,
					ImageID:     testImageID,
					ContainerID: "containerd://process",
					Ready:       true,
					State:       corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
				},
			},
		},
	}
	f.pod.Labels = maps.Clone(labels)
	f.pod.Labels[appsv1.ControllerRevisionHashLabelKey] = "revision"
	f.pod.Annotations = maps.Clone(annotations)
	f.pod.OwnerReferences = ownerReference("apps/v1", "StatefulSet", f.workload.Name, f.workload.UID)
	state := &api.ParticipantRuntimeStatus{
		ObservedGeneration: 1,
		PolicyDigest:       policy,
		WorkloadRefs:       []common.Binding{{Kind: "StatefulSet", Name: f.workload.Name, UID: f.workload.UID}},
		PodRef:             &common.Binding{Kind: "Pod", Name: f.pod.Name, UID: f.pod.UID},
		ContainerID:        "containerd://process",
		ImageID:            testImageID,
		ConfigRef: &common.Binding{
			Kind:        "Secret",
			Name:        "configuration",
			UID:         "config-uid",
			Fingerprint: testImageID,
		},
		ConfigurationDigest: testConfig,
	}
	if kind == "BitcoinNode" {
		state.ConfigurationDigest = ""
		delete(f.pod.Annotations, configurationAnnotation)
		delete(f.workload.Spec.Template.Annotations, configurationAnnotation)
	}
	ports := map[string]int32{"p2p": 18444, "rpc": 18443}
	//nolint:exhaustive // Fixture wire values remain independent of production enum constants.
	switch kind {
	case "StacksNode":
		ports = map[string]int32{"p2p": 20444, "rpc": 20443}
	case "StacksSigner":
		ports = map[string]int32{"events": 30000}
	}
	for name, port := range ports {
		service := &corev1.Service{
			ObjectMeta: owned(name, types.UID(name+"-uid")),
			Spec: corev1.ServiceSpec{
				Selector: maps.Clone(labels),
				Ports:    []corev1.ServicePort{{Name: name, Port: port}},
			},
		}
		f.services = append(f.services, service)
		state.Endpoints = append(
			state.Endpoints,
			api.RuntimeEndpoint{Name: name, Host: name + "." + testNamespace + ".svc", Port: port},
		)
	}
	f.participant.Status.Runtime = state
	f.report = &corev1.ConfigMap{ObjectMeta: owned("report", "report-uid")}
	f.refreshReport()
	return f
}

func (f *participantObjects) refreshReport() {
	input := map[string]any{
		"namespace":      f.participant.Namespace,
		"participantUID": f.participant.UID,
		"policyDigest":   f.participant.Status.Admission.PolicyDigest,
		"config":         f.participant.Status.Runtime.ConfigRef,
		"report":         common.Binding{Kind: "ConfigMap", Name: f.report.Name, UID: f.report.UID},
	}
	raw, _ := json.Marshal(input)
	output, _ := json.Marshal(
		map[string]string{
			"inputDigest":  bytesDigest(raw),
			"configDigest": f.participant.Status.Runtime.ConfigRef.Fingerprint,
		},
	)
	f.report.Data = map[string]string{"input.json": string(raw), "report.json": string(output)}
}

func ownerReference(version, kind, name string, uid types.UID) []metav1.OwnerReference {
	return []metav1.OwnerReference{{
		APIVersion: version,
		Kind:       kind,
		Name:       name,
		UID:        uid,
		Controller: ptr.To(true),
	}}
}

func fixtureReader(t *testing.T, f *participantObjects) Reader {
	t.Helper()
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	AddNetworkTypes(scheme)
	objects := []client.Object{f.root, f.participant, f.workload, f.pod, f.report}
	for _, service := range f.services {
		objects = append(objects, service)
	}
	return Reader{
		APIReader: &noSecretReader{Reader: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()},
	}
}

// noSecretReader makes any accidental private-object read fail the fixture.
type noSecretReader struct{ client.Reader }

func (r *noSecretReader) Get(
	ctx context.Context,
	key client.ObjectKey,
	object client.Object,
	opts ...client.GetOption,
) error {
	if _, ok := object.(*corev1.Secret); ok || object.GetObjectKind().GroupVersionKind().Kind == "Secret" {
		return fmt.Errorf("private Secret read forbidden")
	}
	return r.Reader.Get(ctx, key, object, opts...)
}

func (r *noSecretReader) List(ctx context.Context, object client.ObjectList, opts ...client.ListOption) error {
	if _, ok := object.(*corev1.SecretList); ok {
		return fmt.Errorf("private Secret list forbidden")
	}
	return r.Reader.List(ctx, object, opts...)
}

// driftReader replaces one exact resource identity during the final direct read.
type driftReader struct {
	client.Reader
	resource            string
	rootReads, podReads int
}

func (r *driftReader) Get(
	ctx context.Context,
	key client.ObjectKey,
	object client.Object,
	opts ...client.GetOption,
) error {
	if err := r.Reader.Get(ctx, key, object, opts...); err != nil {
		return err
	}
	change := false
	switch object.(type) {
	case *api.StacksNetwork:
		r.rootReads++
		change = r.resource == "root" && r.rootReads == 2
	case *corev1.Pod:
		r.podReads++
		change = r.resource == "pod" && r.podReads == 2
	case *unstructured.Unstructured:
		change = r.resource == "participant"
	case *corev1.ConfigMap:
		change = r.resource == "report"
	}
	if change {
		object.SetResourceVersion("changed")
		object.SetUID("replacement")
	}
	return nil
}
