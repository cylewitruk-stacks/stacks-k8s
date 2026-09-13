package v1alpha2

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// TestKindWireAndSchemeIdentity checks names independently against registered concrete types.
func TestKindWireAndSchemeIdentity(t *testing.T) {
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := appsv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := rbacv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		kind, wire string
		object     runtime.Object
	}{
		{KindSecret, "Secret", &corev1.Secret{}},
		{KindConfigMap, "ConfigMap", &corev1.ConfigMap{}},
		{KindPod, "Pod", &corev1.Pod{}},
		{KindService, "Service", &corev1.Service{}},
		{KindStatefulSet, "StatefulSet", &appsv1.StatefulSet{}},
		{KindDeployment, "Deployment", &appsv1.Deployment{}},
		{KindReplicaSet, "ReplicaSet", &appsv1.ReplicaSet{}},
		{KindRole, "Role", &rbacv1.Role{}},
		{KindServiceAccount, "ServiceAccount", &corev1.ServiceAccount{}},
	} {
		t.Run(tc.wire, func(t *testing.T) {
			if tc.kind != tc.wire {
				t.Fatalf("kind %q, want %q", tc.kind, tc.wire)
			}
			gvks, _, err := s.ObjectKinds(tc.object)
			if err != nil {
				t.Fatal(err)
			}
			if len(gvks) != 1 || gvks[0].Kind != tc.kind {
				t.Fatalf("%T registered as %v, want %s", tc.object, gvks, tc.kind)
			}
		})
	}
}
