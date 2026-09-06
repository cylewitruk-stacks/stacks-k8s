package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ProductionPolicy offers one opportunity per interval across a bounded weighted target set.
type ProductionPolicy struct {
	// Targets names declared actors; weights distribute the policy's total offered rate.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	// +listType=map
	// +listMapKey=name
	Targets []ProductionTarget `json:"targets"`
	// IntervalSeconds bounds the fixed policy cadence, independently of receipt time.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=86400
	IntervalSeconds int32 `json:"intervalSeconds"`
	// Paused stops new opportunities; already authorized work may still complete.
	Paused bool `json:"paused,omitempty"`
}

// ProductionTarget configures one weighted actor and its coinbase destination.
type ProductionTarget struct {
	// Name identifies a Bitcoin actor declared in the owning network.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	Name string `json:"name"`
	// Weight is a relative share of generation opportunities, without availability redistribution.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000
	Weight int32 `json:"weight"`
	// Address receives this target's generated coinbase outputs.
	// +kubebuilder:validation:MinLength=14
	// +kubebuilder:validation:MaxLength=128
	Address string `json:"address"`
}

// BitcoinBlockProduction maintains one aggregate-owned weighted production policy.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Opportunities",type=integer,JSONPath=`.status.opportunities`
type BitcoinBlockProduction struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec is compiled from StacksNetwork.
	Spec BitcoinBlockProductionSpec `json:"spec"`
	// Status pins target ledgers and records bounded scheduling facts.
	Status BitcoinBlockProductionStatus `json:"status,omitempty"`
}

// BitcoinBlockProductionList contains aggregate production policies.
// +kubebuilder:object:root=true
type BitcoinBlockProductionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BitcoinBlockProduction `json:"items"`
}

// BitcoinBlockProductionSpec binds mutable policy to one network incarnation.
// +kubebuilder:validation:XValidation:rule="self.networkName == oldSelf.networkName && self.networkUID == oldSelf.networkUID",message="network identity is immutable"
type BitcoinBlockProductionSpec struct {
	// NetworkName identifies the owning StacksNetwork.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	NetworkName string `json:"networkName"`
	// NetworkUID prevents replacement networks from inheriting authority.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	NetworkUID string `json:"networkUID"`
	// Policy declares the total offered cadence and target weights.
	Policy ProductionPolicy `json:"policy"`
}

// BitcoinBlockProductionStatus records selection independently of target execution.
type BitcoinBlockProductionStatus struct {
	// ObservedGeneration identifies the scheduling configuration.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Phase summarizes scheduler availability, not network health or receipts.
	// +kubebuilder:validation:Enum=Running;Paused;Waiting;Blocked;Abandoned
	Phase string `json:"phase,omitempty"`
	// Message reports scheduling/configuration state without credentials.
	Message string `json:"message,omitempty"`
	// Opportunities counts durably published weighted selections.
	// +kubebuilder:validation:Minimum=0
	Opportunities int64 `json:"opportunities,omitempty"`
	// UnassignedOpportunities counts selections skipped before a target ledger could be pinned.
	// +kubebuilder:validation:Minimum=0
	UnassignedOpportunities int64 `json:"unassignedOpportunities,omitempty"`
	// LastUnassignedTarget names the most recent selection without a pinned ledger.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	LastUnassignedTarget string `json:"lastUnassignedTarget,omitempty"`
	// NextOpportunityAt enforces cadence across reconciles and restarts without catch-up.
	NextOpportunityAt *metav1.MicroTime `json:"nextOpportunityAt,omitempty"`
	// Targets retains up to sixteen identities for the policy lifetime, including removed targets.
	// +kubebuilder:validation:MaxItems=16
	// +listType=map
	// +listMapKey=name
	Targets []TargetLedger `json:"targets,omitempty"`
}

// TargetLedger permanently pins one target's execution resource and latest opportunity.
type TargetLedger struct {
	// Name is the logical actor name.
	Name string `json:"name"`
	// ResourceName identifies the internal BitcoinProductionTarget.
	ResourceName string `json:"resourceName"`
	// UID pins the ledger even after target removal or resource loss.
	UID string `json:"uid"`
	// Offered counts selections of this target, including unavailable or reserved opportunities.
	Offered int64 `json:"offered,omitempty"`
	// Opportunity retains the latest selection for this target; earlier unseen selections are skipped.
	Opportunity *ProductionOpportunity `json:"opportunity,omitempty"`
}

// ProductionOpportunity authorizes at most one baseline dispatch within its original interval.
type ProductionOpportunity struct {
	// Number is the target's monotonically increasing offered count.
	Number int64 `json:"number"`
	// PolicyGeneration binds the selected policy configuration.
	PolicyGeneration int64 `json:"policyGeneration"`
	// ExpiresAt bounds admission; it does not cancel an already armed server operation.
	ExpiresAt metav1.MicroTime `json:"expiresAt"`
}

// Ledger returns the retained target record, including inactive targets.
func (p *BitcoinBlockProduction) Ledger(name string) *TargetLedger {
	for i := range p.Status.Targets {
		if p.Status.Targets[i].Name == name {
			return &p.Status.Targets[i]
		}
	}
	return nil
}

// Target returns an active target declaration.
func (p *ProductionPolicy) Target(name string) *ProductionTarget {
	for i := range p.Targets {
		if p.Targets[i].Name == name {
			return &p.Targets[i]
		}
	}
	return nil
}

// ExecutionPolicy compiles a target's immutable name and current production settings.
func (p *ProductionPolicy) ExecutionPolicy(name string) *TargetPolicy {
	t := p.Target(name)
	if t == nil {
		return nil
	}
	return &TargetPolicy{Target: name, Address: t.Address, IntervalSeconds: p.IntervalSeconds, Paused: p.Paused}
}

// Binds verifies the root's retained UID pin and target ownership.
func (p *BitcoinBlockProduction) Binds(t *BitcoinProductionTarget) bool {
	record := p.Ledger(t.Spec.Policy.Target)
	return record != nil && record.ResourceName == t.Name && record.UID == string(t.UID) &&
		t.Namespace == p.Namespace && t.Spec.ProductionUID == string(p.UID) &&
		t.Spec.NetworkName == p.Spec.NetworkName && t.Spec.NetworkUID == p.Spec.NetworkUID && metav1.IsControlledBy(t, p)
}
