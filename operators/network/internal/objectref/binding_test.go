package objectref

import (
	"reflect"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestConcreteBindingsMatchWireAndScheme checks independent literals and registered types.
func TestConcreteBindingsMatchWireAndScheme(t *testing.T) {
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		corev1.AddToScheme,
		appsv1.AddToScheme,
		api.AddToScheme,
		stacks.AddToScheme,
		bitcoin.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	meta := metav1.ObjectMeta{
		Name:      "bound",
		Namespace: "test",
		UID:       "uid",
		Labels:    map[string]string{"private": "excluded"},
	}
	cases := []struct {
		kind   string
		object client.Object
		ref    common.Binding
	}{
		{
			"StacksNetworkParticipant",
			&api.StacksNetworkParticipant{},
			Participant(&api.StacksNetworkParticipant{ObjectMeta: meta}),
		},
		{"StacksNetwork", &api.StacksNetwork{}, Network(&api.StacksNetwork{ObjectMeta: meta})},
		{"StacksGenesis", &api.StacksGenesis{}, Genesis(&api.StacksGenesis{ObjectMeta: meta})},
		{
			"StacksEpochSchedule",
			&api.StacksEpochSchedule{},
			EpochSchedule(&api.StacksEpochSchedule{ObjectMeta: meta}),
		},
		{"StacksAccount", &stacks.StacksAccount{}, Account(&stacks.StacksAccount{ObjectMeta: meta})},
		{"BitcoinWallet", &bitcoin.BitcoinWallet{}, BitcoinWallet(&bitcoin.BitcoinWallet{ObjectMeta: meta})},
		{
			"BitcoinInitialization",
			&bitcoin.BitcoinInitialization{},
			BitcoinInitialization(&bitcoin.BitcoinInitialization{ObjectMeta: meta}),
		},
		{
			"BitcoinExecution",
			&bitcoin.BitcoinExecution{},
			BitcoinExecution(&bitcoin.BitcoinExecution{ObjectMeta: meta}),
		},
		{
			"BitcoinBlockSchedule",
			&bitcoin.BitcoinBlockSchedule{},
			BitcoinBlockSchedule(&bitcoin.BitcoinBlockSchedule{ObjectMeta: meta}),
		},
		{
			"BitcoinBlockScheduleOverride",
			&bitcoin.BitcoinBlockScheduleOverride{},
			BitcoinScheduleOverride(&bitcoin.BitcoinBlockScheduleOverride{ObjectMeta: meta}),
		},
		{"ConfigMap", &corev1.ConfigMap{}, ConfigMap(&corev1.ConfigMap{ObjectMeta: meta})},
		{"Pod", &corev1.Pod{}, Pod(&corev1.Pod{ObjectMeta: meta})},
		{"Service", &corev1.Service{}, Service(&corev1.Service{ObjectMeta: meta})},
		{"StatefulSet", &appsv1.StatefulSet{}, StatefulSet(&appsv1.StatefulSet{ObjectMeta: meta})},
		{"Deployment", &appsv1.Deployment{}, Deployment(&appsv1.Deployment{ObjectMeta: meta})},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			expected := common.Binding{Kind: c.kind, Name: "bound", UID: "uid"}
			if c.ref != expected {
				t.Fatalf("reference = %#v, want %#v", c.ref, expected)
			}
			gvks, _, err := scheme.ObjectKinds(c.object)
			if err != nil || len(gvks) != 1 || gvks[0].Kind != c.ref.Kind {
				t.Fatalf("scheme mismatch: %v, %v", gvks, err)
			}
			pinned := WithFingerprint(c.ref, "sha256:public")
			expected.Fingerprint = "sha256:public"
			if pinned != expected || c.ref.Fingerprint != "" {
				t.Fatal("fingerprint changed identity or source")
			}
		})
	}
	// A concrete object cannot be relabeled by stale or forged TypeMeta.
	pod := &corev1.Pod{
		ObjectMeta: meta,
		TypeMeta:   metav1.TypeMeta{APIVersion: "wrong/v1", Kind: "StacksAccount"},
	}
	if ref := Pod(pod); ref.Kind != "Pod" {
		t.Fatalf("trusted TypeMeta: %#v", ref)
	}
}

// TestSecretMetadataRejectsUnknownTypes preserves the metadata-only identity boundary.
func TestSecretMetadataRejectsUnknownTypes(t *testing.T) {
	for _, typ := range []metav1.TypeMeta{
		{},
		{APIVersion: "v1", Kind: "ConfigMap"},
		{APIVersion: "wrong/v1", Kind: "Secret"},
		{APIVersion: "v1", Kind: "Secret"},
	} {
		object := &metav1.PartialObjectMetadata{
			TypeMeta:   typ,
			ObjectMeta: metav1.ObjectMeta{Name: "secret", UID: "uid"},
		}
		ref, err := SecretMetadata(object)
		valid := typ.APIVersion == "v1" && typ.Kind == "Secret"
		if valid {
			if err != nil || ref != (common.Binding{Kind: "Secret", Name: "secret", UID: "uid"}) {
				t.Fatalf("valid metadata: %#v, %v", ref, err)
			}
		} else if err == nil || ref != (common.Binding{}) {
			t.Fatalf("invalid metadata accepted: %#v", object)
		}
	}
	if _, err := SecretMetadata(nil); err == nil {
		t.Fatal("nil accepted")
	}
}

// TestFreshObjectUsesRegisteredTypeWithoutCopyingFetchedState checks reread isolation.
func TestFreshObjectUsesRegisteredTypeWithoutCopyingFetchedState(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	before := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "old", UID: "uid"},
		Data:       map[string]string{"old": "must disappear"},
	}
	before.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
	fresh, err := Fresh(before, scheme)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fresh, &corev1.ConfigMap{TypeMeta: metav1.TypeMeta{
		APIVersion: "v1",
		Kind:       "ConfigMap",
	}}) {
		t.Fatalf("copied stale fields: %#v", fresh)
	}
	if before.Data["old"] != "must disappear" {
		t.Fatal("source mutated")
	}
	for _, object := range []client.Object{nil, (*corev1.Pod)(nil), &api.StacksNetwork{}} {
		if result, err := Fresh(object, scheme); err == nil || result != nil {
			t.Fatalf("invalid object %T produced %T, %v", object, result, err)
		}
	}
	spoofed, err := Fresh(&corev1.Pod{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"}}, scheme)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := spoofed.(*corev1.Pod); !ok ||
		spoofed.GetObjectKind().GroupVersionKind() != corev1.SchemeGroupVersion.WithKind("Pod") {
		t.Fatalf("used forged TypeMeta: %T", spoofed)
	}
	if _, err := Fresh(before, nil); err == nil {
		t.Fatal("nil scheme accepted")
	}
	// A concrete type registered at two versions must not silently choose an epoch.
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "other", Version: "v1", Kind: "ConfigMap"},
		&corev1.ConfigMap{},
	)
	if _, err := Fresh(&corev1.ConfigMap{}, scheme); err == nil {
		t.Fatal("ambiguous type accepted")
	}
}

// TestFreshRetainsExplicitRegisteredVersion prevents version loss across rereads.
func TestFreshRetainsExplicitRegisteredVersion(t *testing.T) {
	scheme := runtime.NewScheme()
	first := schema.GroupVersionKind{Group: "test", Version: "v1", Kind: "ConfigMap"}
	second := schema.GroupVersionKind{Group: "test", Version: "v2", Kind: "ConfigMap"}
	scheme.AddKnownTypeWithName(first, &corev1.ConfigMap{})
	scheme.AddKnownTypeWithName(second, &corev1.ConfigMap{})
	object := &corev1.ConfigMap{}
	object.SetGroupVersionKind(second)
	fresh, err := Fresh(object, scheme)
	if err != nil || fresh.GetObjectKind().GroupVersionKind() != second {
		t.Fatalf("version lost: %#v, %v", fresh, err)
	}
}
