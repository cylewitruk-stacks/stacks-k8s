package foundation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/objectref"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type resolvable interface {
	client.Object
	GetResolutionStatus() *common.ResolutionStatus
}

// IdentityReconciler resolves an account or wallet without reading private key data.
type IdentityReconciler struct {
	// Client writes public status and owned resolver resources.
	Client client.Client
	// Reader performs current public/metadata identity checks.
	Reader client.Reader
	// Scheme provides ownership type information.
	Scheme *runtime.Scheme
	// Wallet selects BitcoinWallet rather than StacksAccount.
	Wallet bool
	// Image is the scoped Go resolver image.
	Image string
}

func (r *IdentityReconciler) object() resolvable {
	if r.Wallet {
		return &bitcoin.BitcoinWallet{}
	}
	return &stacks.StacksAccount{}
}

// Reconcile resolves public inputs or provisions a restricted key-inspection Job.
func (r *IdentityReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	obj := r.object()
	if err := r.Reader.Get(ctx, request.NamespacedName, obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if obj.GetDeletionTimestamp() != nil {
		return ctrl.Result{}, nil
	}
	base := obj.DeepCopyObject().(client.Object)
	status := obj.GetResolutionStatus()
	previous := *status.DeepCopy()
	input, err := sourceSpec(obj)
	if err != nil {
		return ctrl.Result{}, err
	}
	inputDigest := Digest(input)
	result, resolveErr := r.resolve(ctx, obj, inputDigest)
	reason := common.ConditionResolved
	condition := metav1.ConditionTrue
	message := "Public identity resolved"
	if resolveErr != nil {
		reason = api.ReasonIdentityUnavailable
		if errors.Is(resolveErr, errResolverFailed) {
			reason = reasonResolverFailed
		}
		if errors.Is(resolveErr, ErrUnsupportedWalletProfile) {
			reason = reasonUnsupportedWalletProfile
		}
		condition = metav1.ConditionFalse
		message = resolveErr.Error()
	} else {
		*status = result
	}
	status.ObservedGeneration = obj.GetGeneration()
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{Type: common.ConditionResolved, Status: condition, Reason: reason, Message: message, ObservedGeneration: obj.GetGeneration()})
	if !equal(previous, *status) {
		if err := r.Client.Status().Patch(ctx, obj, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			return ctrl.Result{}, err
		}
	}
	if resolveErr != nil && !errors.Is(resolveErr, errResolverFailed) && !errors.Is(resolveErr, ErrUnsupportedWalletProfile) {
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

func (r *IdentityReconciler) resolve(ctx context.Context, obj resolvable, inputDigest string) (common.ResolutionStatus, error) {
	status := *obj.GetResolutionStatus().DeepCopy()
	var imported *common.SecretKeyRef
	generated, descriptor := true, false
	switch v := obj.(type) {
	case *stacks.StacksAccount:
		if key := v.Spec.Key; key != nil {
			if key.PublicIdentity != nil {
				public, err := identity.FromPublic(key.PublicIdentity.PublicKey)
				if err != nil || public.Address != key.PublicIdentity.Address {
					return status, fmt.Errorf("public key and testnet address disagree")
				}
				return resolvedIdentity(status, public, "", "", nil, ""), nil
			}
			if key.SecretRef != nil {
				imported = key.SecretRef
				generated = false
			}
		}
	case *bitcoin.BitcoinWallet:
		if err := ValidateWalletProfile(v.Spec); err != nil {
			return status, err
		}
		if key := v.Spec.KeySource; key != nil {
			if key.StacksMinerAccountRef != nil {
				var account stacks.StacksAccount
				if err := r.Reader.Get(ctx, types.NamespacedName{Namespace: v.Namespace, Name: key.StacksMinerAccountRef.Name}, &account); err != nil {
					return status, fmt.Errorf("miner account unavailable")
				}
				if !resolved(&account) {
					return status, fmt.Errorf("miner account is unresolved")
				}
				if len(status.Dependencies) > 0 && status.Dependencies[0].UID != account.UID {
					return status, fmt.Errorf("miner account identity changed")
				}
				status.Dependencies = []common.Binding{objectref.WithFingerprint(objectref.Account(&account), account.Status.Digest)}
				public, err := identity.FromPublic(account.Status.Identity.PublicKey)
				if err != nil {
					return status, err
				}
				return resolvedIdentity(status, public, "pkh("+public.MiningPublicKey+")", public.MiningAddress, nil, ""), nil
			}
			if key.SecretRef != nil {
				imported = key.SecretRef
				generated = false
				descriptor = true
			}
		}
	}
	kind := stacks.KindStacksAccount
	if r.Wallet {
		kind = bitcoin.KindBitcoinWallet
	}
	credentials := common.SecretKeyRef{Name: RuntimeName("", string(obj.GetUID()), kind, obj.GetName(), "key"), Key: "privateKey"}
	if imported != nil {
		credentials = *imported
	}
	metadata := &metav1.PartialObjectMetadata{TypeMeta: metav1.TypeMeta{APIVersion: corev1.SchemeGroupVersion.String(), Kind: common.KindSecret}}
	err := r.Reader.Get(ctx, types.NamespacedName{Namespace: obj.GetNamespace(), Name: credentials.Name}, metadata)
	if apierrors.IsNotFound(err) && generated && status.Digest == "" {
		empty := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: credentials.Name, Namespace: obj.GetNamespace()}}
		if err := controllerutil.SetControllerReference(obj, empty, r.Scheme, controllerutil.WithBlockOwnerDeletion(false)); err != nil {
			return status, err
		}
		if err := r.Client.Create(ctx, empty); err != nil && !apierrors.IsAlreadyExists(err) {
			return status, err
		}
		return status, fmt.Errorf("waiting for generated credential identity")
	}
	if err != nil || metadata.DeletionTimestamp != nil {
		return status, fmt.Errorf("credential metadata unavailable; resolved keys are never regenerated")
	}
	if generated && !ownedUID(metadata, obj.GetUID()) {
		return status, fmt.Errorf("generated credential has foreign ownership")
	}
	if status.Digest != "" && (status.CredentialsUID != metadata.UID || status.CredentialsRef == nil || *status.CredentialsRef != credentials) {
		return status, fmt.Errorf("credential identity changed")
	}
	reportName := RuntimeName("", string(obj.GetUID()), kind, obj.GetName(), "report-"+strings.TrimPrefix(inputDigest, "sha256:"))
	report := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: reportName, Namespace: obj.GetNamespace(), Labels: map[string]string{managedByLabel: foundationManager}}}
	if err := createOwned(ctx, r.Client, r.Scheme, obj, report); err != nil {
		return status, err
	}
	if err := r.Reader.Get(ctx, client.ObjectKeyFromObject(report), report); err != nil {
		return status, err
	}
	if !ownedUID(report, obj.GetUID()) {
		return status, fmt.Errorf("identity report has foreign ownership")
	}
	if data := report.Data["report.json"]; data != "" {
		var result KeyReport
		if len(data) > 16384 || json.Unmarshal([]byte(data), &result) != nil {
			return status, fmt.Errorf("invalid public identity report")
		}
		if result.SourceUID != obj.GetUID() || result.InputDigest != inputDigest || result.CredentialsUID != metadata.UID {
			return status, fmt.Errorf("identity report binding changed")
		}
		verified, err := identity.FromPublic(result.Public.PublicKey)
		if err != nil || verified != result.Public {
			return status, fmt.Errorf("invalid public report encodings")
		}
		if r.Wallet {
			_, address, err := identity.FromDescriptor(result.Descriptor)
			if err != nil || address != result.BitcoinAddress {
				return status, fmt.Errorf("invalid public wallet report")
			}
		}
		return resolvedIdentity(status, result.Public, result.Descriptor, result.BitcoinAddress, &credentials, metadata.UID), nil
	}
	in := KeyJobInput{Namespace: obj.GetNamespace(), SourceUID: obj.GetUID(), InputDigest: inputDigest, CredentialsRef: credentials, CredentialsUID: metadata.UID, Generate: generated, Descriptor: descriptor, ReportName: report.Name, ReportUID: report.UID}
	if err := provisionKeyJob(ctx, r.Client, r.Reader, r.Scheme, obj, r.Image, in); err != nil {
		return status, err
	}
	return status, fmt.Errorf("waiting for scoped identity resolver report")
}

func resolvedIdentity(status common.ResolutionStatus, p identity.Public, descriptor, address string, credentials *common.SecretKeyRef, uid types.UID) common.ResolutionStatus {
	status.Identity = &common.PublicIdentity{Address: p.Address, PublicKey: p.PublicKey}
	status.CredentialsRef = credentials
	status.CredentialsUID = uid
	status.BitcoinAddress = address
	status.Descriptor = descriptor
	status.Digest = Digest(struct {
		Identity   common.PublicIdentity
		Descriptor string
	}{*status.Identity, descriptor})
	return status
}
func resolved(obj resolvable) bool {
	s := obj.GetResolutionStatus()
	return obj.GetDeletionTimestamp() == nil && s.ObservedGeneration == obj.GetGeneration() && s.Identity != nil && meta.IsStatusConditionTrue(s.Conditions, common.ConditionResolved)
}

// SetupWithManager registers targeted public-report and metadata-only credential notifications.
func (r *IdentityReconciler) SetupWithManager(m ctrl.Manager) error {
	return r.setupIdentityWatches(m)
}

// DefaultAccountName names reusable identities independently of network instances.
func DefaultAccountName(name, suffix string) string {
	candidate := name + "-" + suffix
	if len(candidate) <= 63 {
		return candidate
	}
	hash := sha256.Sum256([]byte(candidate))
	return strings.TrimRight(candidate[:50], "-") + "-" + hex.EncodeToString(hash[:])[:12]
}

// EnsureDefaultAccount creates an independently or instance-owned generated identity.
func EnsureDefaultAccount(ctx context.Context, c client.Client, s *runtime.Scheme, owner client.Object, name string) error {
	account := &stacks.StacksAccount{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: owner.GetNamespace()}, Spec: stacks.StacksAccountSpec{Key: &common.KeySource{Generate: ptr.To(true)}}}
	return createOwned(ctx, c, s, owner, account)
}
