package v1alpha2

import (
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GroupVersion identifies the reusable stacks API.
var GroupVersion = schema.GroupVersion{Group: "stacks.stacks.org", Version: "v1alpha2"}

// AddToScheme registers definitions and list kinds.
func AddToScheme(s *runtime.Scheme) error {
	s.AddKnownTypes(GroupVersion, &StacksAccount{}, &StacksAccountList{}, &StacksNode{}, &StacksNodeList{}, &StacksSigner{}, &StacksSignerList{}, &StacksStacker{}, &StacksStackerList{}, &StacksContractSet{}, &StacksContractSetList{}, &StacksFaucet{}, &StacksFaucetList{}, &StacksTransactionProduction{}, &StacksTransactionProductionList{}, &StacksFaucetRequest{}, &StacksFaucetRequestList{})
	metav1.AddToGroupVersion(s, GroupVersion)
	return nil
}

// StacksAccount is an independently owned reusable declaration.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.status) || !has(oldSelf.status.digest) || self.spec == oldSelf.spec",message="resolved identity is immutable"
type StacksAccount struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec contains reusable inputs.
	Spec StacksAccountSpec `json:"spec"`
	// Status reports direct-input resolution only.
	Status common.ResolutionStatus `json:"status,omitempty"`
}

// StacksAccountList contains reusable declarations.
// +kubebuilder:object:root=true
type StacksAccountList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items contains declarations.
	Items []StacksAccount `json:"items"`
}

// StacksNode is an independently owned reusable declaration.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
type StacksNode struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec contains reusable inputs.
	Spec StacksNodeSpec `json:"spec"`
	// Status reports direct-input resolution only.
	Status common.ResolutionStatus `json:"status,omitempty"`
}

// StacksNodeList contains reusable declarations.
// +kubebuilder:object:root=true
type StacksNodeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items contains declarations.
	Items []StacksNode `json:"items"`
}

// StacksSigner is an independently owned reusable declaration.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
type StacksSigner struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec contains reusable inputs.
	Spec StacksSignerSpec `json:"spec"`
	// Status reports direct-input resolution only.
	Status common.ResolutionStatus `json:"status,omitempty"`
}

// StacksSignerList contains reusable declarations.
// +kubebuilder:object:root=true
type StacksSignerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items contains declarations.
	Items []StacksSigner `json:"items"`
}

// StacksStacker is an independently owned reusable declaration.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
type StacksStacker struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec contains reusable inputs.
	Spec StacksStackerSpec `json:"spec"`
	// Status reports direct-input resolution only.
	Status common.ResolutionStatus `json:"status,omitempty"`
}

// StacksStackerList contains reusable declarations.
// +kubebuilder:object:root=true
type StacksStackerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items contains declarations.
	Items []StacksStacker `json:"items"`
}

// StacksContractSet is an independently owned reusable declaration.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
type StacksContractSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec contains reusable inputs.
	Spec StacksContractSetSpec `json:"spec"`
	// Status reports direct-input resolution only.
	Status common.ResolutionStatus `json:"status,omitempty"`
}

// StacksContractSetList contains reusable declarations.
// +kubebuilder:object:root=true
type StacksContractSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items contains declarations.
	Items []StacksContractSet `json:"items"`
}

// StacksFaucet is an independently owned reusable declaration.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
type StacksFaucet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec contains reusable inputs.
	Spec StacksFaucetSpec `json:"spec"`
	// Status reports direct-input resolution only.
	Status common.ResolutionStatus `json:"status,omitempty"`
}

// StacksFaucetList contains reusable declarations.
// +kubebuilder:object:root=true
type StacksFaucetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items contains declarations.
	Items []StacksFaucet `json:"items"`
}

// StacksTransactionProduction is an independently owned reusable declaration.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
type StacksTransactionProduction struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec contains reusable inputs.
	Spec StacksTransactionProductionSpec `json:"spec"`
	// Status reports direct-input resolution only.
	Status common.ResolutionStatus `json:"status,omitempty"`
}

// StacksTransactionProductionList contains reusable declarations.
// +kubebuilder:object:root=true
type StacksTransactionProductionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items contains declarations.
	Items []StacksTransactionProduction `json:"items"`
}

// GetResolutionStatus exposes reusable-input status to resource-focused controllers.
func (v *StacksAccount) GetResolutionStatus() *common.ResolutionStatus { return &v.Status }

// GetResolutionStatus exposes reusable-input status to resource-focused controllers.
func (v *StacksNode) GetResolutionStatus() *common.ResolutionStatus { return &v.Status }

// GetResolutionStatus exposes reusable-input status to resource-focused controllers.
func (v *StacksSigner) GetResolutionStatus() *common.ResolutionStatus { return &v.Status }

// GetResolutionStatus exposes reusable-input status to resource-focused controllers.
func (v *StacksStacker) GetResolutionStatus() *common.ResolutionStatus { return &v.Status }

// GetResolutionStatus exposes reusable-input status to resource-focused controllers.
func (v *StacksContractSet) GetResolutionStatus() *common.ResolutionStatus { return &v.Status }

// GetResolutionStatus exposes reusable-input status to resource-focused controllers.
func (v *StacksFaucet) GetResolutionStatus() *common.ResolutionStatus { return &v.Status }

// GetResolutionStatus exposes reusable-input status to resource-focused controllers.
func (v *StacksTransactionProduction) GetResolutionStatus() *common.ResolutionStatus {
	return &v.Status
}
