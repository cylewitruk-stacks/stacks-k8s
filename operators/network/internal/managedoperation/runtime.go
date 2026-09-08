// Package managedoperation reconciles declared contract and stacking capabilities.
package managedoperation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/accountledger"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/execution"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/naming"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksrpc"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stackssdk"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stackstx"
	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// errExecutionFailed keeps a definite unsuccessful operation visible without resubmission.
var errExecutionFailed = errors.New("recorded transaction executed unsuccessfully")

// Encoder is an offline dependency; it cannot observe clusters or submit transactions.
type Encoder interface {
	Sign(context.Context, string, any) (stackstx.Transaction, error)
	VerifyKey(context.Context, stackssdk.Key) error
	EncodeArguments(context.Context, []stackssdk.Argument) ([]string, error)
}

// Runtime composes native observations, account authority and offline signing.
type Runtime struct {
	// Binding selects the only capability this process may execute.
	Binding execution.Binding
	// KeyDirectory contains explicitly mounted signing artifacts; empty supports library callers.
	KeyDirectory string
	// Client writes only capability and account resources.
	Client client.Client
	// Reader bypasses the cache for authority and credential reads.
	Reader client.Reader
	// RPC supplies native chain observations.
	RPC *stacksrpc.Client
	// Ledger serializes every transaction on the declared managed accounts.
	Ledger *accountledger.Ledger
	// SDK supplies only offline signing, verification and value encoding.
	SDK Encoder
}

// parent verifies current environment and consumer incarnations before any observation.
func (r *Runtime) parent(ctx context.Context, object client.Object, name, uid, kind string) (*network.StacksNetwork, error) {
	parent := &network.StacksNetwork{}
	if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: object.GetNamespace(), Name: name}, parent); err != nil {
		return nil, err
	}
	if string(parent.UID) != uid || !parent.DeletionTimestamp.IsZero() || !metav1.IsControlledBy(object, parent) || !object.GetDeletionTimestamp().IsZero() {
		return nil, fmt.Errorf("capability is not owned by the current environment")
	}
	for _, identity := range parent.Status.Capabilities {
		if identity.Kind == kind && identity.Name == object.GetName() && identity.UID == string(object.GetUID()) {
			return parent, nil
		}
	}
	return nil, fmt.Errorf("capability incarnation has not been pinned")
}

// account reads the capability's exclusively assigned ledger, without touching its key.
func (r *Runtime) account(ctx context.Context, parent *network.StacksNetwork, name string) (*stacks.StacksAccount, error) {
	account := &stacks.StacksAccount{}
	if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: parent.Namespace, Name: naming.Child(parent.Name, "account-"+name)}, account); err != nil {
		return nil, err
	}
	if account.Spec.NetworkUID != string(parent.UID) || !metav1.IsControlledBy(account, parent) {
		return nil, fmt.Errorf("managed account belongs to another environment")
	}
	return account, nil
}

// key verifies exact Secret bytes and derives public encodings before offline signing.
func (r *Runtime) key(ctx context.Context, namespace string, ref stacks.ArtifactReference, address string) (stackssdk.Key, error) {
	var data []byte
	if r.KeyDirectory != "" {
		if ref.Name == "" || ref.Key == "" || strings.ContainsAny(ref.Name+ref.Key, "/\\") || ref.Name == ".." || ref.Key == ".." {
			return stackssdk.Key{}, fmt.Errorf("invalid signing artifact path")
		}
		var err error
		data, err = os.ReadFile(filepath.Join(r.KeyDirectory, ref.Name, ref.Key))
		if err != nil {
			return stackssdk.Key{}, fmt.Errorf("mounted signing artifact cannot be read")
		}
	} else {
		secret := &core.Secret{}
		if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: ref.Name}, secret); err != nil || !secret.DeletionTimestamp.IsZero() {
			return stackssdk.Key{}, fmt.Errorf("signing Secret cannot be read")
		}
		data = secret.Data[ref.Key]
	}
	if len(data) > 4096 || digest(data) != ref.Digest {
		return stackssdk.Key{}, fmt.Errorf("signing artifact does not match its declared digest")
	}
	var key stackssdk.Key
	if json.Unmarshal(data, &key) != nil || (address != "" && key.Address != address) {
		return stackssdk.Key{}, fmt.Errorf("signing Secret does not match its account")
	}
	if err := r.SDK.VerifyKey(ctx, key); err != nil {
		return stackssdk.Key{}, err
	}
	return key, nil
}

// source verifies one declared public artifact before encoding a deployment.
func (r *Runtime) source(ctx context.Context, namespace string, ref stacks.ArtifactReference) (string, error) {
	object := &core.ConfigMap{}
	if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: ref.Name}, object); err != nil {
		return "", fmt.Errorf("contract source ConfigMap cannot be read")
	}
	data := []byte(object.Data[ref.Key])
	if !object.DeletionTimestamp.IsZero() || len(data) == 0 || len(data) > 128*1024 || digest(data) != ref.Digest {
		return "", fmt.Errorf("contract source does not match its declared digest")
	}
	return string(data), nil
}

// status writes only changed observations using optimistic locking.
func (r *Runtime) status(ctx context.Context, object client.Object, desired stacks.ManagedOperationStatus) error {
	desired.ObservedGeneration = object.GetGeneration()
	base := object.DeepCopyObject().(client.Object)
	current := operationStatus(object)
	// Workload readiness belongs to the provisioning controller.
	desired.Conditions = append([]metav1.Condition(nil), current.Conditions...)
	if reflect.DeepEqual(*current, desired) {
		return nil
	}
	*current = desired
	return r.Client.Status().Patch(ctx, object, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}

// operationStatus selects the status owned by this capability controller.
func operationStatus(object client.Object) *stacks.ManagedOperationStatus {
	switch c := object.(type) {
	case *stacks.StacksContractSet:
		return &c.Status
	case *stacks.StacksStackingParticipant:
		return &c.Status
	default:
		panic("unsupported managed capability")
	}
}

// execute retains the receipt in the consumer before releasing its account reservation.
func (r *Runtime) execute(ctx context.Context, consumer client.Object, account *stacks.StacksAccount, request accountledger.Request) error {
	request.ConsumerUID = string(consumer.GetUID())
	tx, err := r.Ledger.Ensure(ctx, client.ObjectKeyFromObject(account), request)
	if err != nil {
		return err
	}
	if tx == nil || tx.Receipt == nil {
		return accountledger.ErrWaiting
	}
	status := operationStatus(consumer).DeepCopy()
	found := false
	for i := range status.Receipts {
		if status.Receipts[i].Account == account.Name {
			status.Receipts[i].Transaction = *tx.DeepCopy()
			found = true
		}
	}
	if !found {
		status.Receipts = append(status.Receipts, stacks.OperationAcknowledgement{Account: account.Name, Transaction: *tx.DeepCopy()})
	}
	if err := r.status(ctx, consumer, *status); err != nil {
		return err
	}
	if err := r.Ledger.Acknowledge(ctx, client.ObjectKeyFromObject(account), string(consumer.GetUID()), tx); err != nil {
		return err
	}
	if !tx.Receipt.Success {
		return errExecutionFailed
	}
	return nil
}

// drain observes the recorded TxID even when current chain state already reflects its effects.
func (r *Runtime) drain(ctx context.Context, consumer client.Object, account *stacks.StacksAccount) error {
	tx := account.Status.Transaction
	if tx == nil {
		return nil
	}
	if tx.Acknowledged {
		if tx.Receipt != nil && !tx.Receipt.Success {
			return errExecutionFailed
		}
		return nil
	}
	return r.execute(ctx, consumer, account, accountledger.Request{Ordinal: tx.Ordinal, Digest: tx.OperationDigest,
		Sign: func(context.Context, int64) (stackstx.Transaction, error) {
			return stackstx.Transaction{}, fmt.Errorf("recorded transaction cannot be resigned")
		}})
}

// digest binds public intent and artifacts without retaining private signing inputs.
func digest(data []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(data)) }
