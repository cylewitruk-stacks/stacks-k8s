package v1alpha2

import (
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GroupVersion identifies the reusable bitcoin API.
var GroupVersion = schema.GroupVersion{Group: "bitcoin.stacks.org", Version: "v1alpha2"}

// AddToScheme registers definitions and list kinds.
func AddToScheme(s *runtime.Scheme) error {
	s.AddKnownTypes(GroupVersion, &BitcoinNode{}, &BitcoinNodeList{}, &BitcoinWallet{}, &BitcoinWalletList{}, &BitcoinBlockSchedule{}, &BitcoinBlockScheduleList{}, &BitcoinBlockProduction{}, &BitcoinBlockProductionList{}, &BitcoinExecution{}, &BitcoinExecutionList{}, &BitcoinInitialization{}, &BitcoinInitializationList{}, &BitcoinBlockScheduleOverride{}, &BitcoinBlockScheduleOverrideList{})
	metav1.AddToGroupVersion(s, GroupVersion)
	return nil
}

// BitcoinNode is an independently owned reusable declaration.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
type BitcoinNode struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec contains reusable inputs.
	Spec BitcoinNodeSpec `json:"spec"`
	// Status reports direct-input resolution only.
	Status common.ResolutionStatus `json:"status,omitempty"`
}

// BitcoinNodeList contains reusable declarations.
// +kubebuilder:object:root=true
type BitcoinNodeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items contains declarations.
	Items []BitcoinNode `json:"items"`
}

// BitcoinWallet is an independently owned reusable declaration.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.status) || !has(oldSelf.status.digest) || self.spec == oldSelf.spec",message="resolved identity is immutable"
type BitcoinWallet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec contains reusable inputs.
	Spec BitcoinWalletSpec `json:"spec"`
	// Status reports direct-input resolution only.
	Status common.ResolutionStatus `json:"status,omitempty"`
}

// BitcoinWalletList contains reusable declarations.
// +kubebuilder:object:root=true
type BitcoinWalletList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items contains declarations.
	Items []BitcoinWallet `json:"items"`
}

// BitcoinBlockSchedule is an independently owned reusable declaration.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:validation:XValidation:rule="self.spec == oldSelf.spec",message="schedules are immutable"
type BitcoinBlockSchedule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec contains reusable inputs.
	Spec BitcoinBlockScheduleSpec `json:"spec"`
	// Status reports direct-input resolution only.
	Status common.ResolutionStatus `json:"status,omitempty"`
}

// BitcoinBlockScheduleList contains reusable declarations.
// +kubebuilder:object:root=true
type BitcoinBlockScheduleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items contains declarations.
	Items []BitcoinBlockSchedule `json:"items"`
}

// BitcoinBlockProduction is an independently owned reusable declaration.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
type BitcoinBlockProduction struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec contains reusable inputs.
	Spec BitcoinBlockProductionSpec `json:"spec"`
	// Status reports direct-input resolution only.
	Status common.ResolutionStatus `json:"status,omitempty"`
}

// BitcoinBlockProductionList contains reusable declarations.
// +kubebuilder:object:root=true
type BitcoinBlockProductionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items contains declarations.
	Items []BitcoinBlockProduction `json:"items"`
}

// GetResolutionStatus exposes reusable-input status to resource-focused controllers.
func (v *BitcoinNode) GetResolutionStatus() *common.ResolutionStatus { return &v.Status }

// GetResolutionStatus exposes reusable-input status to resource-focused controllers.
func (v *BitcoinWallet) GetResolutionStatus() *common.ResolutionStatus { return &v.Status }

// GetResolutionStatus exposes reusable-input status to resource-focused controllers.
func (v *BitcoinBlockSchedule) GetResolutionStatus() *common.ResolutionStatus { return &v.Status }

// GetResolutionStatus exposes reusable-input status to resource-focused controllers.
func (v *BitcoinBlockProduction) GetResolutionStatus() *common.ResolutionStatus { return &v.Status }
