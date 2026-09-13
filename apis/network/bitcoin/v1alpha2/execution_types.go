package v1alpha2

import (
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// BitcoinExecution retains one node's managed RPC authority across worker restarts.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:validation:XValidation:rule="self.spec.networkUID == oldSelf.spec.networkUID && self.spec.participant == oldSelf.spec.participant",message="execution identity is immutable"
type BitcoinExecution struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec binds a participant and one scheduler generation opportunity.
	Spec BitcoinExecutionSpec `json:"spec"`
	// Status is atomically updated by the scoped control worker.
	Status BitcoinExecutionStatus `json:"status,omitempty"`
}

// BitcoinExecutionList contains per-node authority records.
// +kubebuilder:object:root=true
type BitcoinExecutionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items contains records.
	Items []BitcoinExecution `json:"items"`
}

// BitcoinExecutionSpec separates immutable target binding from mutable desired work.
type BitcoinExecutionSpec struct {
	// NetworkUID is the owning network incarnation.
	NetworkUID types.UID `json:"networkUID"`
	// Participant is the exact BitcoinNode participant.
	Participant common.Binding `json:"participant"`
	// Offer is the scheduler's sole bounded generation opportunity.
	Offer *BitcoinBlockOffer `json:"offer,omitempty"`
}

// BitcoinBlockOffer specifies one block without a sequence of commands.
type BitcoinBlockOffer struct {
	// Override pins the active timing request, when this opportunity used one.
	Override *common.Binding `json:"override,omitempty"`
	// Mode distinguishes frozen bootstrap advancement from completed-initialization baseline authority.
	// Omission preserves existing bootstrap records.
	// +kubebuilder:validation:Enum=Bootstrap;Baseline
	Mode string `json:"mode,omitempty"`
	// Target binds the sole selected participant for a baseline opportunity.
	Target *common.Binding `json:"target,omitempty"`
	// Initialization binds the retained network scheduler record.
	Initialization common.Binding `json:"initialization"`
	// Production identifies the admitted production participant.
	Production common.Binding `json:"production"`
	// PolicyDigest identifies the admitted production policy.
	PolicyDigest string `json:"policyDigest"`
	// Number is monotonic within the initialization record.
	// +kubebuilder:validation:Minimum=1
	Number int64 `json:"number"`
	// Address is the exact public coinbase destination.
	// +kubebuilder:validation:MaxLength=128
	Address string `json:"address"`
	// Wallet pins the payout identity used for accounting.
	Wallet common.Binding `json:"wallet"`
	// ExpectedHeight is the converged pre-dispatch height.
	// +kubebuilder:validation:Minimum=0
	ExpectedHeight int64 `json:"expectedHeight"`
	// ExpectedTip is the converged pre-dispatch block hash.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{64}$`
	ExpectedTip string `json:"expectedTip"`
	// Ceiling is the frozen gate's maximum permitted resulting height.
	// +kubebuilder:validation:Minimum=1
	Ceiling int64 `json:"ceiling"`
	// ExpiresAt prevents a stale opportunity from initiating a new request.
	ExpiresAt metav1.Time `json:"expiresAt"`
}

// BitcoinTargetIdentity pins the observed actor, endpoint and credential profile.
type BitcoinTargetIdentity struct {
	// Participant binds the node participant.
	Participant common.Binding `json:"participant"`
	// Pod binds the admitted actor Pod.
	Pod common.Binding `json:"pod"`
	// ContainerID identifies the actor process instance.
	ContainerID string `json:"containerID"`
	// Endpoint binds the selected Pod address, never a replacement Service target.
	Endpoint string `json:"endpoint"`
	// Configuration pins immutable server configuration.
	Configuration common.Binding `json:"configuration"`
	// Credentials pins immutable worker RPC credentials without publishing their bytes.
	Credentials common.Binding `json:"credentials"`
	// PolicyDigest identifies the accepted actor policy.
	PolicyDigest string `json:"policyDigest"`
}

// BitcoinWalletOperation supplies public inputs for one wallet mutation.
type BitcoinWalletOperation struct {
	// Wallet pins the reusable wallet incarnation.
	Wallet common.Binding `json:"wallet"`
	// Name is the local Core wallet name.
	Name string `json:"name"`
	// Descriptor is a validated checksummed public descriptor for import.
	Descriptor string `json:"descriptor,omitempty"`
}

// BitcoinArmedRPC is a durable one-shot authority granted to one process nonce.
type BitcoinArmedRPC struct {
	// Action pins the finite action that exclusively owns this arm.
	Action *common.Binding `json:"action,omitempty"`
	// Address supplies the fixed public payout for finite generation.
	Address string `json:"address,omitempty"`
	// BlockHash is the captured invalidity marker for finite reorganization.
	BlockHash string `json:"blockHash,omitempty"`
	// ID identifies this unique process-bound request.
	ID string `json:"id"`
	// ProcessNonce identifies the acquiring worker process, not its Pod name.
	ProcessNonce string `json:"processNonce"`
	// Reservation identifies the current mutation owner; it survives receipt accounting.
	Reservation common.Binding `json:"reservation"`
	// Target is the exact actor/transport identity admitted before arming.
	Target BitcoinTargetIdentity `json:"target"`
	// Method selects one bounded supported RPC.
	// +kubebuilder:validation:Enum=CreateWallet;LoadWallet;ImportDescriptor;UnloadWallet;Generate;InvalidateBlock;ReconsiderBlock
	Method string `json:"method"`
	// Wallet supplies required wallet-specific inputs.
	Wallet *BitcoinWalletOperation `json:"wallet,omitempty"`
	// Offer preserves an immutable generation opportunity snapshot.
	Offer *BitcoinBlockOffer `json:"offer,omitempty"`
	// ArmedAt records authorization time, not server execution time.
	ArmedAt metav1.Time `json:"armedAt"`
}

// BitcoinRPCReceipt retains attributable success and its original observation time.
type BitcoinRPCReceipt struct {
	// Request preserves the exact accounted execution identity and inputs.
	Request BitcoinArmedRPC `json:"request"`
	// ReceivedAt is the original response time, preserved during accounting retries.
	ReceivedAt metav1.Time `json:"receivedAt"`
	// BlockHash identifies the single generated block, for Generate only.
	BlockHash string `json:"blockHash,omitempty"`
}

// BitcoinWalletObservation reports per-node wallet facts, never global balance.
type BitcoinWalletObservation struct {
	// Wallet pins the reusable identity.
	Wallet common.Binding `json:"wallet"`
	// Name is the local Core wallet name.
	Name string `json:"name"`
	// Address is the resolved public payout address.
	Address string `json:"address"`
	// Ready confirms a loaded watch-only descriptor wallet with the intended descriptor.
	Ready bool `json:"ready"`
	// MatureOutputs counts observed outputs with at least 101 confirmations.
	// +kubebuilder:validation:Minimum=0
	MatureOutputs int32 `json:"matureOutputs"`
}

// BitcoinObservation contains successful read facts bound to one actor process.
type BitcoinObservation struct {
	// Target binds the observed actor process and transport.
	Target BitcoinTargetIdentity `json:"target"`
	// Height is the processed regtest height.
	// +kubebuilder:validation:Minimum=0
	Height int64 `json:"height"`
	// Tip is the processed regtest tip hash.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{64}$`
	Tip string `json:"tip"`
	// ObservedAt records successful reads, never a status heartbeat alone.
	ObservedAt metav1.Time `json:"observedAt"`
	// Wallets records the currently attached public wallets on this node.
	// +kubebuilder:validation:MaxItems=100
	// +listType=map
	// +listMapKey=name
	Wallets []BitcoinWalletObservation `json:"wallets,omitempty"`
}

// BitcoinExecutionStatus is one CAS-protected authority and accounting unit.
type BitcoinExecutionStatus struct {
	// Action retains finite mutation ownership until lifecycle receipt acknowledgement.
	Action *BitcoinActionReservation `json:"action,omitempty"`
	// Reservation retains the current mutation owner's exclusion after accounting.
	Reservation *common.Binding `json:"reservation,omitempty"`
	// Armed blocks every replacement while delivery or receipt accounting is outstanding.
	Armed *BitcoinArmedRPC `json:"armed,omitempty"`
	// LastReceipt retains the last atomically accounted success.
	LastReceipt *BitcoinRPCReceipt `json:"lastReceipt,omitempty"`
	// CompletedOffer prevents repeated execution of an already accounted offer.
	// +kubebuilder:validation:Minimum=0
	CompletedOffer int64 `json:"completedOffer,omitempty"`
	// BlocksGenerated counts attributable successful generation receipts.
	// +kubebuilder:validation:Minimum=0
	BlocksGenerated int64 `json:"blocksGenerated,omitempty"`
	// Control acknowledges a current root pause only after local sending has stopped.
	Control *BitcoinControlAcknowledgement `json:"control,omitempty"`
	// Drain records bounded terminal-control acknowledgement without claiming server quiescence.
	Drain *BitcoinDrainStatus `json:"drain,omitempty"`
	// PendingWalletRemoval preserves a detached wallet until acknowledgement or observed absence.
	PendingWalletRemoval *BitcoinWalletOperation `json:"pendingWalletRemoval,omitempty"`
	// Observation retains successful native state reads.
	Observation *BitcoinObservation `json:"observation,omitempty"`
	// Phase distinguishes idle/active/uncertain execution without claiming quiescence.
	// +kubebuilder:validation:Enum=Idle;Armed;Blocked;Abandoned
	Phase string `json:"phase,omitempty"`
}

// FrozenBitcoinWallet preserves initial public funding identity.
type FrozenBitcoinWallet struct {
	// Wallet binds the reusable wallet incarnation and public digest.
	Wallet common.Binding `json:"wallet"`
	// Name is the local Core wallet name.
	Name string `json:"name"`
	// Address is the fixed coinbase destination.
	Address string `json:"address"`
	// Descriptor is the fixed public watch-only descriptor.
	Descriptor string `json:"descriptor"`
}

// BitcoinInitialization retains bootstrap requirements and sole scheduler state.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:validation:XValidation:rule="self.spec == oldSelf.spec",message="Bitcoin initialization is immutable"
type BitcoinInitialization struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec freezes the first Bitcoin gate and initial cohort.
	Spec BitcoinInitializationSpec `json:"spec"`
	// Status is written only by the scheduler.
	Status BitcoinInitializationStatus `json:"status,omitempty"`
}

// BitcoinInitializationList contains retained bootstrap records.
// +kubebuilder:object:root=true
type BitcoinInitializationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items contains records.
	Items []BitcoinInitialization `json:"items"`
}

// BitcoinInitializationSpec preserves the first gate independently of production removal.
type BitcoinInitializationSpec struct {
	// NetworkUID identifies the owning independent environment.
	NetworkUID types.UID `json:"networkUID"`
	// Genesis binds the frozen requirements artifact.
	Genesis common.Binding `json:"genesis"`
	// Production binds the captured initial production participant.
	Production common.Binding `json:"production"`
	// Target binds the single initialization generation participant.
	Target common.Binding `json:"target"`
	// Nodes pins every Bitcoin participant required to converge for the first gate.
	// +kubebuilder:validation:MaxItems=1000
	Nodes []common.Binding `json:"nodes"`
	// MinimumHeight is the first frozen ceiling and target height.
	// +kubebuilder:validation:Minimum=1
	MinimumHeight int64 `json:"minimumHeight"`
	// MatureOutputsPerMiner is the frozen per-miner funding requirement.
	// +kubebuilder:validation:Minimum=1
	MatureOutputsPerMiner int32 `json:"matureOutputsPerMiner"`
	// MinerWallets is the exact immutable initial funding cohort.
	// +kubebuilder:validation:MaxItems=100
	MinerWallets []FrozenBitcoinWallet `json:"minerWallets"`
	// PayoutWallet is the pinned destination for maturity/height advancement blocks.
	PayoutWallet FrozenBitcoinWallet `json:"payoutWallet"`
}

// BitcoinFundingCount records attributable coinbase-generation receipts per wallet.
type BitcoinFundingCount struct {
	// WalletUID pins the recipient's reusable identity.
	WalletUID types.UID `json:"walletUID"`
	// Outputs counts successful initialization generation receipts.
	// +kubebuilder:validation:Minimum=0
	Outputs int32 `json:"outputs"`
}

// BitcoinInitializationStatus persists cadence and one selected opportunity.
type BitcoinInitializationStatus struct {
	// Override retains the sole active timing override independently of bootstrap progress.
	Override *BitcoinActiveOverride `json:"override,omitempty"`
	// Baseline retains ongoing cadence and selection after every frozen gate completes.
	Baseline *BitcoinBaselineStatus `json:"baseline,omitempty"`
	// Production identifies the current compatible producer independently of frozen provenance.
	Production *common.Binding `json:"production,omitempty"`
	// NextOpportunityAt is the durable cadence anchor; pauses do not create catch-up bursts.
	NextOpportunityAt *metav1.Time `json:"nextOpportunityAt,omitempty"`
	// Offer is the sole selected, durable generation opportunity.
	Offer *BitcoinBlockOffer `json:"offer,omitempty"`
	// LastAccountedOffer prevents duplicate funding accounting after retries.
	// +kubebuilder:validation:Minimum=0
	LastAccountedOffer int64 `json:"lastAccountedOffer,omitempty"`
	// Funded counts exact successful funding receipts.
	// +kubebuilder:validation:MaxItems=100
	// +listType=map
	// +listMapKey=walletUID
	Funded []BitcoinFundingCount `json:"funded,omitempty"`
	// FirstCeilingObservedAt preserves the ceiling observation deadline across pause/restart.
	FirstCeilingObservedAt *metav1.Time `json:"firstCeilingObservedAt,omitempty"`
	// PreparedAt records the first complete wallet/maturity/convergence observation.
	PreparedAt *metav1.Time `json:"preparedAt,omitempty"`
	// Phase is a truthful first-gate hold/progress observation.
	// +kubebuilder:validation:Enum=Waiting;Preparing;Paused;Held;Blocked;Abandoned
	Phase string `json:"phase,omitempty"`
	// Reason is a bounded machine-readable hold classification.
	Reason string `json:"reason,omitempty"`
}

// BitcoinDrainStatus binds graceful drain evidence to current terminal control.
type BitcoinDrainStatus struct {
	// ProcessNonce identifies the acknowledging worker process.
	ProcessNonce string `json:"processNonce"`
	// NetworkGeneration identifies the evaluated root control revision.
	NetworkGeneration int64 `json:"networkGeneration"`
	// Reason identifies the terminal control being acknowledged.
	Reason string `json:"reason"`
	// RequestedAt preserves the start of the bounded drain.
	RequestedAt metav1.Time `json:"requestedAt"`
	// CompletedAt records successful acknowledgement after draining or retaining uncertainty.
	CompletedAt metav1.Time `json:"completedAt"`
	// Outcome distinguishes settled local execution from retained Armed uncertainty.
	// +kubebuilder:validation:Enum=Drained;Uncertain
	Outcome string `json:"outcome"`
}

// BitcoinControlAcknowledgement reports a bounded worker control observation.
type BitcoinControlAcknowledgement struct {
	// NetworkGeneration identifies the exact evaluated root operation revision.
	NetworkGeneration int64 `json:"networkGeneration"`
	// ProcessNonce identifies the worker that stopped sending.
	ProcessNonce string `json:"processNonce"`
	// Operation is the acknowledged desired operation.
	// +kubebuilder:validation:Enum=Paused
	Operation string `json:"operation"`
	// ObservedAt is the first successful acknowledgement time for this process and revision.
	ObservedAt metav1.Time `json:"observedAt"`
	// HeartbeatAt proves this process still observes pause without an outstanding send.
	HeartbeatAt metav1.Time `json:"heartbeatAt"`
}
