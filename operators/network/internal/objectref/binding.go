// Package objectref captures public references from concrete Kubernetes objects.
// Constructors preserve only name and UID; callers select fingerprints explicitly.
package objectref

import (
	"fmt"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// capture is private so callers cannot pair an object with an unrelated kind.
func capture(kind string, object metav1.Object) common.Binding {
	return common.Binding{Kind: kind, Name: object.GetName(), UID: object.GetUID()}
}

// WithFingerprint attaches the caller's public content identity to a reference.
func WithFingerprint(ref common.Binding, fingerprint string) common.Binding {
	ref.Fingerprint = fingerprint
	return ref
}

// SecretMetadata narrows a metadata-only read without accessing Secret contents.
func SecretMetadata(object *metav1.PartialObjectMetadata) (common.Binding, error) {
	if object == nil || object.GroupVersionKind() != corev1.SchemeGroupVersion.WithKind(common.KindSecret) {
		return common.Binding{}, fmt.Errorf("expected v1 Secret metadata")
	}
	return capture(common.KindSecret, object), nil
}

// Participant captures a StacksNetworkParticipant reference from its concrete type.
func Participant(object *api.StacksNetworkParticipant) common.Binding {
	return capture(api.KindStacksNetworkParticipant, object)
}

// Network captures a StacksNetwork reference from its concrete type.
func Network(object *api.StacksNetwork) common.Binding {
	return capture(api.KindStacksNetwork, object)
}

// Genesis captures a StacksGenesis reference from its concrete type.
func Genesis(object *api.StacksGenesis) common.Binding {
	return capture(api.KindStacksGenesis, object)
}

// EpochSchedule captures a StacksEpochSchedule reference from its concrete type.
func EpochSchedule(object *api.StacksEpochSchedule) common.Binding {
	return capture(api.KindStacksEpochSchedule, object)
}

// Account captures a StacksAccount reference from its concrete type.
func Account(object *stacks.StacksAccount) common.Binding {
	return capture(stacks.KindStacksAccount, object)
}

// BitcoinWallet captures a BitcoinWallet reference from its concrete type.
func BitcoinWallet(object *bitcoin.BitcoinWallet) common.Binding {
	return capture(bitcoin.KindBitcoinWallet, object)
}

// BitcoinInitialization captures a BitcoinInitialization reference from its concrete type.
func BitcoinInitialization(object *bitcoin.BitcoinInitialization) common.Binding {
	return capture(bitcoin.KindBitcoinInitialization, object)
}

// BitcoinExecution captures a BitcoinExecution reference from its concrete type.
func BitcoinExecution(object *bitcoin.BitcoinExecution) common.Binding {
	return capture(bitcoin.KindBitcoinExecution, object)
}

// BitcoinBlockSchedule captures a BitcoinBlockSchedule reference from its concrete type.
func BitcoinBlockSchedule(object *bitcoin.BitcoinBlockSchedule) common.Binding {
	return capture(bitcoin.KindBitcoinBlockSchedule, object)
}

// BitcoinScheduleOverride captures a BitcoinBlockScheduleOverride reference from its concrete type.
func BitcoinScheduleOverride(object *bitcoin.BitcoinBlockScheduleOverride) common.Binding {
	return capture(bitcoin.KindBitcoinBlockScheduleOverride, object)
}

// ConfigMap captures a ConfigMap reference from its concrete type.
func ConfigMap(object *corev1.ConfigMap) common.Binding {
	return capture(common.KindConfigMap, object)
}

// Pod captures a Pod reference from its concrete type.
func Pod(object *corev1.Pod) common.Binding {
	return capture(common.KindPod, object)
}

// Service captures a Service reference from its concrete type.
func Service(object *corev1.Service) common.Binding {
	return capture(common.KindService, object)
}

// StatefulSet captures a StatefulSet reference from its concrete type.
func StatefulSet(object *appsv1.StatefulSet) common.Binding {
	return capture(common.KindStatefulSet, object)
}

// Deployment captures a Deployment reference from its concrete type.
func Deployment(object *appsv1.Deployment) common.Binding {
	return capture(common.KindDeployment, object)
}
