package foundation

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/protocolcontracts"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var epochNames = []string{"1.0", "2.0", "2.05", "2.1", "2.2", "2.3", "2.4", "2.5", "3.0", "3.1", "3.2", "3.3", "3.4", "4.0"}

// DefaultEpochs returns a fresh copy of the supported activation profile.
func DefaultEpochs() []api.Epoch {
	heights := []int64{0, 0, 203, 204, 206, 207, 208, 209, 252, 253, 254, 255, 262, 282}
	result := make([]api.Epoch, len(heights))
	for i, h := range heights {
		result[i] = api.Epoch{Name: epochNames[i], StartHeight: h}
	}
	return result
}
func validateEpochs(epochs []api.Epoch) error {
	if len(epochs) != len(epochNames) {
		return fmt.Errorf("complete fourteen-epoch schedule required")
	}
	for i, e := range epochs {
		if e.Name != epochNames[i] || e.StartHeight < 0 || e.StartHeight > math.MaxUint32 || i < 2 && e.StartHeight != 0 || i > 0 && e.StartHeight < epochs[i-1].StartHeight {
			return fmt.Errorf("invalid ordered epoch schedule")
		}
	}
	return nil
}
func gates(epochs []api.Epoch, pox api.PoX) ([]api.Gate, error) {
	if err := validateEpochs(epochs); err != nil {
		return nil, err
	}
	l, p := int64(pox.RewardCycleLength), int64(pox.PrepareLength)
	if l < 2 || p < 1 || p >= l {
		return nil, fmt.Errorf("invalid PoX cycle lengths")
	}
	nakamoto, waterfall := epochs[8].StartHeight, epochs[13].StartHeight
	firstCycle := nakamoto / l
	secondCycle := waterfall/l + 1
	enroll4 := firstCycle*l - p - 1
	enroll5 := waterfall + 2
	if firstCycle < 1 || enroll4 <= epochs[7].StartHeight+1 || nakamoto-1 <= enroll4 || waterfall-1 <= nakamoto || enroll5 >= secondCycle*l-p || secondCycle*l-1 <= enroll5 {
		return nil, fmt.Errorf("epoch schedule leaves insufficient enrollment and initialization windows")
	}
	return []api.Gate{{Name: "PrepareBitcoin", BitcoinCeiling: epochs[2].StartHeight}, {Name: "EnrollPoX4", BitcoinCeiling: enroll4, TargetCycle: ptr.To(firstCycle)}, {Name: "PrepareNakamoto", BitcoinCeiling: nakamoto - 1, TargetCycle: ptr.To(firstCycle)}, {Name: "PreparePoX5", BitcoinCeiling: waterfall - 1}, {Name: "EnrollPoX5", BitcoinCeiling: enroll5, TargetCycle: ptr.To(secondCycle)}, {Name: "PrepareWaterfall", BitcoinCeiling: secondCycle*l - 1, TargetCycle: ptr.To(secondCycle)}}, nil
}

func compileGenesis(ctx context.Context, r client.Reader, root *api.StacksNetwork, all map[string]*candidate) (api.StacksGenesisSpec, error) {
	spec := api.StacksGenesisSpec{Chain: api.Chain{Profile: root.Spec.Profile, Epochs: DefaultEpochs(), PoX: api.PoX{RewardCycleLength: 20, PrepareLength: 5}}, Source: api.GenesisSource{NetworkUID: root.UID}}
	if spec.Chain.Profile == "" {
		spec.Chain.Profile = "regtest-pox4-pox5-v1"
	}
	if root.Spec.EpochSchedule != nil {
		spec.Chain.Epochs = root.Spec.EpochSchedule.Epochs
	}
	if root.Spec.EpochScheduleRef != nil {
		var schedule api.StacksEpochSchedule
		if err := r.Get(ctx, types.NamespacedName{Namespace: root.Namespace, Name: root.Spec.EpochScheduleRef.Name}, &schedule); err != nil || schedule.DeletionTimestamp != nil {
			return spec, fmt.Errorf("epoch schedule unavailable")
		}
		spec.Chain.Epochs = schedule.Spec.Epochs
		spec.Source.Dependencies = append(spec.Source.Dependencies, binding("StacksEpochSchedule", &schedule, Digest(schedule.Spec)))
	}
	if root.Spec.Genesis != nil && root.Spec.Genesis.PoX != nil {
		spec.Chain.PoX = *root.Spec.Genesis.PoX
	}
	var err error
	spec.Bootstrap.Gates, err = gates(spec.Chain.Epochs, spec.Chain.PoX)
	if err != nil {
		return spec, err
	}
	accounts := map[string]*stacks.StacksAccount{}
	balances := map[string]uint64{}
	var total uint64
	allocate := func(ref common.NameRef, amount common.Amount) error {
		var a stacks.StacksAccount
		if err := r.Get(ctx, types.NamespacedName{Namespace: root.Namespace, Name: ref.Name}, &a); err != nil || !resolved(&a) {
			return fmt.Errorf("genesis account %s unavailable", ref.Name)
		}
		n, err := strconv.ParseUint(string(amount), 10, 64)
		if err != nil {
			return fmt.Errorf("genesis balance overflows uint64")
		}
		address := a.Status.Identity.Address
		if old, ok := balances[address]; ok {
			if old != n {
				return fmt.Errorf("aliases disagree on balance for %s", address)
			}
		} else {
			if math.MaxUint64-total < n {
				return fmt.Errorf("genesis supply overflows uint64")
			}
			total += n
			balances[address] = n
		}
		accounts[ref.Name] = &a
		return nil
	}
	if root.Spec.Genesis != nil {
		for _, a := range root.Spec.Genesis.Allocations {
			if err := allocate(a.AccountRef, a.AmountMicroSTX); err != nil {
				return spec, err
			}
		}
	}
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	sort.Strings(names)
	counts := map[api.ParticipantKind]int{}
	stackedSigners := map[string]bool{}
	minerWallets := map[string]bool{}
	// Required baseline signing roles need funding, but accounts may be shared.
	funded := map[string]bool{}
	requireFunding := func(c *candidate, ref *common.NameRef) error {
		if ref == nil || c.accounts[ref.Name] == nil {
			return fmt.Errorf("mutation account unavailable")
		}
		funded[c.accounts[ref.Name].Status.Identity.Address] = true
		return nil
	}
	var production *candidate
	for _, name := range names {
		c := all[name]
		v := c.configuration
		counts[c.instance.Spec.Kind]++
		req := api.BootstrapRequirement{Participant: binding("StacksNetworkParticipant", c.instance, ""), PolicyDigest: Digest(v), Dependencies: c.dependencies}
		accountNames := make([]string, 0, len(c.accounts))
		for name := range c.accounts {
			accountNames = append(accountNames, name)
		}
		sort.Strings(accountNames)
		for _, name := range accountNames {
			a := c.accounts[name]
			req.Accounts = append(req.Accounts, api.PublicAccount{Binding: binding("StacksAccount", a, a.Status.Digest), Identity: *a.Status.Identity})
		}
		if v.StacksFaucet != nil {
			f := v.StacksFaucet
			// Zero opts out of automatic funding; later requests may fund the faucet.
			if *f.GenesisBalanceMicroSTX != "0" {
				if err := allocate(*f.AccountRef, *f.GenesisBalanceMicroSTX); err != nil {
					return spec, err
				}
			}
		}
		if v.StacksNode != nil && v.StacksNode.Mining != nil && ptr.Deref(v.StacksNode.Mining.Enabled, false) {
			n := v.StacksNode
			wallet := c.wallets[n.Mining.BitcoinWalletRef.Name]
			a := c.accounts[n.IdentityAccountRef.Name]
			if wallet == nil || a == nil || wallet.Spec.KeySource == nil || wallet.Spec.KeySource.StacksMinerAccountRef == nil || wallet.Spec.KeySource.StacksMinerAccountRef.Name != a.Name {
				return spec, fmt.Errorf("miner wallet must derive from its identity account")
			}
			node := all[n.BitcoinNodeRef.Name]
			loaded := false
			for _, ref := range ptr.Deref(node.configuration.BitcoinNode.WalletRefs, nil) {
				loaded = loaded || ref.Name == wallet.Name
			}
			if !loaded {
				return spec, fmt.Errorf("miner wallet is not loaded on its Bitcoin participant")
			}
			minerWallets[wallet.Name] = true
		}
		if v.StacksStacker != nil {
			s := v.StacksStacker
			if stackedSigners[s.SignerRef.Name] {
				return spec, fmt.Errorf("multiple stackers select signer %s", s.SignerRef.Name)
			}
			stackedSigners[s.SignerRef.Name] = true
			for _, ref := range []*common.NameRef{s.HolderAccountRef, s.AdministratorAccountRef} {
				if err := requireFunding(c, ref); err != nil {
					return spec, err
				}
			}
			req.AmountMicroSTX = s.AmountMicroSTX
			req.LockCycles = s.LockCycles
			req.RenewWhenRemainingCycles = s.RenewWhenRemainingCycles
			// The initial lock must cover the last PoX-4 cycle preceding activation.
			first := *spec.Bootstrap.Gates[1].TargetCycle
			last := (spec.Chain.Epochs[13].StartHeight - 1) / int64(spec.Chain.PoX.RewardCycleLength)
			if first+int64(*s.LockCycles) <= last {
				return spec, fmt.Errorf("initial lock does not cover the PoX-4 transition")
			}
		}
		if v.StacksContractSet != nil {
			s := v.StacksContractSet
			if err := requireFunding(c, s.DeployerAccountRef); err != nil {
				return spec, err
			}
			req.RegistryInitialization = s.Initialization
			spec.Chain.Contracts = api.ContractBindings{Deployer: c.accounts[s.DeployerAccountRef.Name].Status.Identity.Address, Bundle: *s.Bundle, SourceHashes: map[string]string{}}
			var pins struct {
				Contracts []protocolcontracts.Source `json:"contracts"`
			}
			if err := json.Unmarshal(protocolcontracts.Pins(), &pins); err != nil {
				return spec, err
			}
			for _, pin := range pins.Contracts {
				spec.Chain.Contracts.SourceHashes[pin.Name] = pin.SHA256
			}
		}
		if v.StacksTransactionProduction != nil {
			if err := requireFunding(c, v.StacksTransactionProduction.AccountRef); err != nil {
				return spec, err
			}
		}
		if v.BitcoinBlockProduction != nil {
			production = c
			wallet := c.wallets[v.BitcoinBlockProduction.PayoutWalletRef.Name]
			req.BitcoinPayoutWallet = ptrBinding(binding("BitcoinWallet", wallet, wallet.Status.Digest))
			req.BitcoinInitialization = v.BitcoinBlockProduction.Initialization
		}
		spec.Bootstrap.Requirements = append(spec.Bootstrap.Requirements, req)
	}
	for _, kind := range []api.ParticipantKind{"BitcoinNode", "StacksNode", "StacksSigner", "StacksStacker", "StacksContractSet", "StacksTransactionProduction", "BitcoinBlockProduction"} {
		if counts[kind] == 0 {
			return spec, fmt.Errorf("initial network requires %s", kind)
		}
	}
	if len(minerWallets) == 0 {
		return spec, fmt.Errorf("initial network requires a Stacks miner")
	}
	for name, c := range all {
		if c.configuration.StacksSigner != nil && !stackedSigners[name] {
			return spec, fmt.Errorf("signer %s has no initial stacker", name)
		}
	}
	init := production.configuration.BitcoinBlockProduction.Initialization
	if init.MinimumHeight != spec.Bootstrap.Gates[0].BitcoinCeiling {
		return spec, fmt.Errorf("Bitcoin initialization height must match the first gate")
	}
	if 100+int64(len(minerWallets))*int64(init.MatureOutputsPerMiner) > init.MinimumHeight {
		return spec, fmt.Errorf("initial gate cannot mature the requested miner outputs")
	}
	for name := range minerWallets {
		found := false
		for _, ref := range ptr.Deref(init.MinerWalletRefs, nil) {
			found = found || ref.Name == name
		}
		if !found {
			return spec, fmt.Errorf("initialization omits miner wallet %s", name)
		}
	}
	// Required baseline roles need funding; optional faucet requests can wait for later funding.
	for _, c := range all {
		for _, a := range c.accounts {
			if funded[a.Status.Identity.Address] && balances[a.Status.Identity.Address] == 0 {
				return spec, fmt.Errorf("mutation account %s is not funded", a.Name)
			}
		}
		if s := c.configuration.StacksStacker; s != nil {
			amount, _ := strconv.ParseUint(string(*s.AmountMicroSTX), 10, 64)
			if balances[c.accounts[s.HolderAccountRef.Name].Status.Identity.Address] <= amount {
				return spec, fmt.Errorf("stacker balance must exceed stake")
			}
		}
	}
	for address, amount := range balances {
		spec.Chain.Allocations = append(spec.Chain.Allocations, api.Allocation{Address: address, AmountMicroSTX: common.Amount(strconv.FormatUint(amount, 10))})
	}
	sort.Slice(spec.Chain.Allocations, func(i, j int) bool { return spec.Chain.Allocations[i].Address < spec.Chain.Allocations[j].Address })
	accountNames := make([]string, 0, len(accounts))
	for name := range accounts {
		accountNames = append(accountNames, name)
	}
	sort.Strings(accountNames)
	for _, name := range accountNames {
		a := accounts[name]
		spec.Source.Dependencies = append(spec.Source.Dependencies, binding("StacksAccount", a, a.Status.Digest))
	}
	spec.Source.InputDigest = semanticInputDigest(spec, all)
	data, err := json.Marshal(spec)
	if err != nil {
		return spec, err
	}
	if len(data) > 900*1024 {
		return spec, fmt.Errorf("genesis artifact exceeds 900 KiB")
	}
	return spec, nil
}
