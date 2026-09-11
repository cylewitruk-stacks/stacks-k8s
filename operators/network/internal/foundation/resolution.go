package foundation

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type candidate struct {
	instance      *api.StacksNetworkParticipant
	configuration api.Configuration
	source        api.Source
	dependencies  []common.Binding
	accounts      map[string]*stacks.StacksAccount
	wallets       map[string]*bitcoin.BitcoinWallet
}

func binding(kind string, obj client.Object, digest string) common.Binding {
	return common.Binding{Kind: kind, Name: obj.GetName(), UID: obj.GetUID(), Fingerprint: digest}
}
func (c *candidate) account(ctx context.Context, r client.Reader, ref *common.NameRef, sign bool) error {
	if ref == nil {
		return fmt.Errorf("required account reference is missing")
	}
	var a stacks.StacksAccount
	if err := r.Get(ctx, types.NamespacedName{Namespace: c.instance.Namespace, Name: ref.Name}, &a); err != nil {
		return fmt.Errorf("account %s unavailable", ref.Name)
	}
	if !resolved(&a) {
		return fmt.Errorf("account %s unresolved", ref.Name)
	}
	if sign && a.Status.CredentialsRef == nil {
		return fmt.Errorf("account %s lacks signing credentials", ref.Name)
	}
	c.accounts[ref.Name] = &a
	c.dependencies = append(c.dependencies, binding("StacksAccount", &a, a.Status.Digest))
	return nil
}
func (c *candidate) wallet(ctx context.Context, r client.Reader, ref *common.NameRef) error {
	if ref == nil {
		return fmt.Errorf("required wallet reference is missing")
	}
	var w bitcoin.BitcoinWallet
	if err := r.Get(ctx, types.NamespacedName{Namespace: c.instance.Namespace, Name: ref.Name}, &w); err != nil {
		return fmt.Errorf("wallet %s unavailable", ref.Name)
	}
	if !resolved(&w) {
		return fmt.Errorf("wallet %s unresolved", ref.Name)
	}
	c.wallets[ref.Name] = &w
	c.dependencies = append(c.dependencies, binding("BitcoinWallet", &w, w.Status.Digest))
	return nil
}
func (c *candidate) participant(all map[string]*candidate, ref *common.NameRef, kind string) error {
	if ref == nil {
		return fmt.Errorf("required %s participant reference missing", kind)
	}
	other := all[ref.Name]
	if other == nil || string(other.instance.Spec.Kind) != kind {
		return fmt.Errorf("participant %s must select an admitted %s", ref.Name, kind)
	}
	c.dependencies = append(c.dependencies, binding("StacksNetworkParticipant", other.instance, ""))
	return nil
}
func positive(v *common.Amount) error {
	if v == nil {
		return fmt.Errorf("amount is required")
	}
	n, err := strconv.ParseUint(string(*v), 10, 64)
	if err != nil || n == 0 {
		return fmt.Errorf("amount must be a positive uint64")
	}
	return nil
}
func (c *candidate) validate(ctx context.Context, r client.Reader, all map[string]*candidate) error {
	c.accounts = map[string]*stacks.StacksAccount{}
	c.wallets = map[string]*bitcoin.BitcoinWallet{}
	var errs []error
	account := func(ref *common.NameRef, sign bool) { errs = append(errs, c.account(ctx, r, ref, sign)) }
	wallet := func(ref *common.NameRef) { errs = append(errs, c.wallet(ctx, r, ref)) }
	participant := func(ref *common.NameRef, kind string) { errs = append(errs, c.participant(all, ref, kind)) }
	var fields *common.ActorFields
	var peers *common.Peers
	switch {
	case c.configuration.BitcoinNode != nil:
		v := c.configuration.BitcoinNode
		fields = &v.ActorFields
		peers = v.Peers
		for _, ref := range ptr.Deref(v.WalletRefs, nil) {
			wallet(&ref)
		}
	case c.configuration.StacksNode != nil:
		v := c.configuration.StacksNode
		fields = &v.ActorFields
		peers = v.Peers
		participant(v.BitcoinNodeRef, "BitcoinNode")
		account(v.IdentityAccountRef, true)
		if v.Mining != nil && v.Mining.BitcoinWalletRef != nil {
			wallet(v.Mining.BitcoinWalletRef)
		}
		if v.Mining != nil && ptr.Deref(v.Mining.Enabled, false) && v.Mining.BitcoinWalletRef == nil {
			errs = append(errs, fmt.Errorf("miner wallet is required"))
		}
	case c.configuration.StacksSigner != nil:
		v := c.configuration.StacksSigner
		fields = &v.ActorFields
		participant(v.NodeRef, "StacksNode")
		account(v.AccountRef, true)
	case c.configuration.StacksStacker != nil:
		v := c.configuration.StacksStacker
		participant(v.SignerRef, "StacksSigner")
		participant(v.TargetNodeRef, "StacksNode")
		account(v.HolderAccountRef, true)
		account(v.AdministratorAccountRef, true)
		errs = append(errs, positive(v.AmountMicroSTX))
		if ptr.Deref(v.RenewWhenRemainingCycles, 0) >= ptr.Deref(v.LockCycles, 0) {
			errs = append(errs, fmt.Errorf("renewal threshold must be below lock cycles"))
		}
	case c.configuration.StacksFaucet != nil:
		v := c.configuration.StacksFaucet
		participant(v.TargetNodeRef, "StacksNode")
		account(v.AccountRef, true)
		errs = append(errs, positive(v.MaxRequestMicroSTX))
	case c.configuration.StacksTransactionProduction != nil:
		v := c.configuration.StacksTransactionProduction
		participant(v.TargetNodeRef, "StacksNode")
		account(v.AccountRef, true)
		errs = append(errs, positive(v.AmountMicroSTX), positive(v.FeeMicroSTX))
		if v.Interval == nil {
			errs = append(errs, fmt.Errorf("interval missing"))
		} else {
			_, err := duration(*v.Interval)
			errs = append(errs, err)
		}
		if v.Recipient == nil {
			errs = append(errs, fmt.Errorf("recipient missing"))
		} else if v.Recipient.AccountRef != nil {
			account(v.Recipient.AccountRef, false)
		} else if v.Recipient.Address == nil {
			errs = append(errs, fmt.Errorf("recipient missing"))
		}
	case c.configuration.StacksContractSet != nil:
		v := c.configuration.StacksContractSet
		participant(v.TargetNodeRef, "StacksNode")
		account(v.DeployerAccountRef, true)
		if v.Initialization == nil {
			errs = append(errs, fmt.Errorf("registry initialization missing"))
		} else {
			for i := range v.Initialization.SignerAccountRefs {
				account(&v.Initialization.SignerAccountRefs[i], false)
			}
			account(&v.Initialization.AggregateKeyAccountRef, false)
			if int(v.Initialization.Threshold) > len(v.Initialization.SignerAccountRefs) {
				errs = append(errs, fmt.Errorf("registry threshold exceeds signers"))
			}
		}
	case c.configuration.BitcoinBlockProduction != nil:
		v := c.configuration.BitcoinBlockProduction
		wallet(v.PayoutWalletRef)
		if len(ptr.Deref(v.Targets, nil)) == 0 {
			errs = append(errs, fmt.Errorf("production targets missing"))
		}
		seen := map[string]bool{}
		for _, target := range ptr.Deref(v.Targets, nil) {
			participant(&target.NodeRef, "BitcoinNode")
			if seen[target.NodeRef.Name] || target.Weight < 1 {
				errs = append(errs, fmt.Errorf("invalid or duplicate production target"))
			}
			seen[target.NodeRef.Name] = true
		}
		if v.ScheduleRef != nil && v.Schedule != nil {
			errs = append(errs, fmt.Errorf("schedule sources conflict"))
		} else if v.ScheduleRef != nil {
			var schedule bitcoin.BitcoinBlockSchedule
			if err := r.Get(ctx, types.NamespacedName{Namespace: c.instance.Namespace, Name: v.ScheduleRef.Name}, &schedule); err != nil || schedule.DeletionTimestamp != nil {
				errs = append(errs, fmt.Errorf("schedule unavailable"))
			} else {
				v.Schedule = &schedule.Spec
				v.ScheduleRef = nil
				c.dependencies = append(c.dependencies, binding("BitcoinBlockSchedule", &schedule, Digest(schedule.Spec)))
			}
		}
		if v.Schedule == nil {
			errs = append(errs, fmt.Errorf("schedule missing"))
		} else {
			errs = append(errs, validateSchedule(*v.Schedule))
		}
		if v.Initialization == nil {
			errs = append(errs, fmt.Errorf("Bitcoin initialization missing"))
		} else {
			participant(&v.Initialization.TargetNodeRef, "BitcoinNode")
			for _, ref := range ptr.Deref(v.Initialization.MinerWalletRefs, nil) {
				wallet(&ref)
			}
		}
	default:
		return fmt.Errorf("configuration branch missing")
	}
	if peers != nil {
		for _, ref := range ptr.Deref(peers.NodeRefs, nil) {
			other := all[ref.Name]
			if other == nil && retainedSeed(c.instance.Status.Admission, ref.Name) {
				continue
			}
			if other == nil || other.instance.Spec.Kind != c.instance.Spec.Kind {
				errs = append(errs, fmt.Errorf("seed %s has wrong participant kind or is absent", ref.Name))
			}
		}
	}
	if fields != nil {
		if fields.Storage != nil && fields.Storage.Size != nil {
			q, err := resource.ParseQuantity(*fields.Storage.Size)
			if err != nil || q.Sign() <= 0 {
				errs = append(errs, fmt.Errorf("storage size must be a positive Kubernetes quantity"))
			}
		}
		if fields.Image == nil || *fields.Image == "" {
			errs = append(errs, fmt.Errorf("actor image is required"))
		}
		if cfg := fields.Config; cfg != nil && (cfg.Overrides != nil || ptr.Deref(cfg.Compatibility, "Managed") == "Unverified" || len(cfg.ServiceRefs) > 0) {
			errs = append(errs, fmt.Errorf("configuration escape-hatch validation requires the actor activation slice"))
		}
		if cfg := fields.Config; cfg != nil && cfg.SecretRef != nil {
			metadata := &metav1.PartialObjectMetadata{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}}
			if err := r.Get(ctx, types.NamespacedName{Namespace: c.instance.Namespace, Name: cfg.SecretRef.Name}, metadata); err != nil || metadata.DeletionTimestamp != nil {
				errs = append(errs, fmt.Errorf("configuration Secret metadata unavailable"))
			} else {
				c.dependencies = append(c.dependencies, binding("Secret", metadata, ""))
			}
			// Full configuration compatibility is not yet implemented. Never approve bootstrap from uninspected configuration.
			errs = append(errs, fmt.Errorf("complete configuration Secret compatibility requires the actor activation slice"))
		}
	}
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	// The dependency set is stable independent of traversal order and repeated uses.
	byKey := map[string]common.Binding{}
	for _, b := range c.dependencies {
		byKey[b.Kind+"/"+b.Name] = b
	}
	c.dependencies = nil
	for _, b := range byKey {
		c.dependencies = append(c.dependencies, b)
	}
	sort.Slice(c.dependencies, func(i, j int) bool {
		a, b := c.dependencies[i], c.dependencies[j]
		return a.Kind+"/"+a.Name < b.Kind+"/"+b.Name
	})
	return nil
}

// retainedSeed treats removal of an already resolved startup hint as peer loss, not authority withdrawal.
func retainedSeed(admission *api.Admission, name string) bool {
	if admission == nil {
		return false
	}
	var peers *common.Peers
	if v := admission.Configuration.BitcoinNode; v != nil {
		peers = v.Peers
	}
	if v := admission.Configuration.StacksNode; v != nil {
		peers = v.Peers
	}
	if peers != nil {
		for _, ref := range ptr.Deref(peers.NodeRefs, nil) {
			if ref.Name == name {
				return true
			}
		}
	}
	return false
}
