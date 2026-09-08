package workers

import (
	"context"
	"strings"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func TestWorkersBindIdentityAndMountOnlyAssignedKeys(t *testing.T) {
	settings := Settings{Image: "operator:test", SDKImage: "sdk:test", PullPolicy: core.PullIfNotPresent, PullSecrets: []core.LocalObjectReference{{Name: "private-registry"}}}
	for _, component := range []string{"bitcoin-production", "stacks-transactions", "stacks-contracts", "stacks-stacking", "stacks-receipts"} {
		t.Run(component, func(t *testing.T) {
			owner := &core.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "capability", Namespace: "experiment", UID: "capability-uid"}}
			p := plan{owner: owner, component: component, network: "network", networkUID: "network-uid", accounts: []string{"assigned-account"}}
			if component == "bitcoin-production" || component == "stacks-transactions" {
				p.credentials = "exclusive-credentials"
			}
			if component == "stacks-contracts" || component == "stacks-stacking" {
				p.keys = []stacks.ArtifactReference{{Name: "own-key", Key: "key.json"}, {Name: "own-key", Key: "key.json"}}
			}
			d := deployment(p, settings)
			pod := d.Spec.Template.Spec
			c := pod.Containers[0]
			if len(pod.ImagePullSecrets) != 1 || pod.ImagePullSecrets[0].Name != "private-registry" {
				t.Fatal("worker lost namespace-local image pull credentials")
			}
			if *d.Spec.Replicas != 1 || d.Spec.Strategy.Type != apps.RecreateDeploymentStrategyType {
				t.Fatal("unexpected workload rollout")
			}
			if !strings.Contains(strings.Join(c.Args, " "), "--capability-uid=capability-uid") || !strings.Contains(strings.Join(c.Args, " "), "--watch-namespace=experiment") {
				t.Fatal("missing execution identity")
			}
			if pod.ServiceAccountName != Name(owner, component) {
				t.Fatal("shared execution identity")
			}
			if pod.HostNetwork || pod.HostPID || pod.HostIPC || !*c.SecurityContext.ReadOnlyRootFilesystem || *c.SecurityContext.AllowPrivilegeEscalation {
				t.Fatal("worker sandbox weakened")
			}
			if component == "stacks-receipts" && len(pod.Volumes) != 0 {
				t.Fatal("receipt worker holds credentials")
			}
			if len(p.keys) > 0 && (len(pod.Volumes) != 1 || len(pod.Volumes[0].Secret.Items) != 1 || pod.Volumes[0].Secret.SecretName != "own-key") {
				t.Fatal("signing mount was broadened or duplicated")
			}
			permissions, err := rules(p, settings)
			if err != nil {
				t.Fatal(err)
			}
			for _, rule := range permissions {
				for _, resource := range rule.Resources {
					if resource == "secrets" || resource == "pods/exec" || resource == "*" || resource == "deployments" {
						t.Fatalf("worker acquired forbidden %s authority", resource)
					}
					for _, verb := range rule.Verbs {
						if verb == "patch" && (rule.APIGroups[0] == "stacks.stacks.org" || rule.APIGroups[0] == "bitcoin.stacks.org") && len(rule.ResourceNames) == 0 {
							t.Fatal("unscoped ledger mutation")
						}
					}
				}
			}
		})
	}
}

func TestForeignExecutionResourcesAreNeverAdopted(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = core.AddToScheme(scheme)
	_ = apps.AddToScheme(scheme)
	owner := &core.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "capability", Namespace: "one", UID: "current"}}
	foreign := &apps.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "one"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, foreign).Build()
	r := Reconciler{Client: c, Reader: c, Scheme: scheme}
	desired := foreign.DeepCopy()
	if r.apply(context.Background(), owner, desired) == nil {
		t.Fatal("foreign Deployment adopted")
	}
	current := &apps.Deployment{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(foreign), current); err != nil {
		t.Fatal(err)
	}
	if len(current.OwnerReferences) != 0 {
		t.Fatal("foreign Deployment mutated")
	}
}

func TestManagedMountRequiresPinnedCurrentAccountAssignment(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = network.AddToScheme(scheme)
	_ = stacks.AddToScheme(scheme)
	_ = bitcoin.AddToScheme(scheme)
	parent := &network.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "one", UID: "parent"}, Spec: network.StacksNetworkSpec{Operation: &network.NetworkOperation{}}}
	account := &stacks.StacksAccount{ObjectMeta: metav1.ObjectMeta{Name: "network-account-holder", Namespace: "one", UID: "account"}, Spec: stacks.StacksAccountSpec{NetworkUID: "parent", Policy: stacks.ManagedAccountPolicy{Name: "holder", ConsumerKind: "StacksStackingParticipant", Consumer: "signer", KeySecretRef: stacks.ArtifactReference{Name: "holder", Key: "key.json"}}}}
	if err := controllerutil.SetControllerReference(parent, account, scheme); err != nil {
		t.Fatal(err)
	}
	parent.Status.Capabilities = []network.CapabilityIdentity{{Kind: "StacksAccount", Name: account.Name, UID: string(account.UID)}}
	parent.Spec.Operation.Accounts = []stacks.ManagedAccountPolicy{account.Spec.Policy}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent, account).Build()
	r := Reconciler{Client: c, Reader: c, Scheme: scheme}
	if _, err := r.account(context.Background(), parent, "holder", "StacksStackingParticipant", "signer", true); err != nil {
		t.Fatal(err)
	}
	if _, err := r.account(context.Background(), parent, "holder", "StacksStackingParticipant", "different", true); err == nil {
		t.Fatal("another participant acquired account mount")
	}
	parent.Status.Capabilities[0].UID = "different"
	if _, err := r.account(context.Background(), parent, "holder", "StacksStackingParticipant", "signer", true); err == nil {
		t.Fatal("replaced account mount accepted")
	}
}

func TestManagedPermissionsRejectEmptyAccountScope(t *testing.T) {
	for _, component := range []string{"stacks-contracts", "stacks-stacking", "stacks-receipts"} {
		for _, names := range [][]string{nil, {}, {""}, {"assigned", ""}} {
			permissions, err := rules(plan{component: component, accounts: names}, Settings{})
			if err == nil || len(permissions) != 0 {
				t.Fatalf("%s emitted permissions for empty scope %q", component, names)
			}
		}
	}
}

// staleAbsenceReader represents an object created after the controller's absence read.
type staleAbsenceReader struct{ client.Reader }

func (r staleAbsenceReader) Get(ctx context.Context, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
	if _, ok := o.(*apps.Deployment); ok {
		return apierrors.NewNotFound(schema.GroupResource{Group: "apps", Resource: "deployments"}, key.Name)
	}
	return r.Reader.Get(ctx, key, o, opts...)
}

func TestConcurrentForeignCreationIsNotAdopted(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = core.AddToScheme(scheme)
	_ = apps.AddToScheme(scheme)
	owner := &core.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "capability", Namespace: "one", UID: "owner"}}
	foreign := &apps.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "one", Labels: map[string]string{"external": "kept"}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, foreign).Build()
	r := Reconciler{Client: c, Reader: staleAbsenceReader{c}, Scheme: scheme}
	desired := &apps.Deployment{ObjectMeta: metav1.ObjectMeta{Name: foreign.Name, Namespace: foreign.Namespace}}
	if err := r.apply(context.Background(), owner, desired); !apierrors.IsAlreadyExists(err) {
		t.Fatalf("concurrent create should be refused: %v", err)
	}
	current := &apps.Deployment{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(foreign), current); err != nil {
		t.Fatal(err)
	}
	if len(current.OwnerReferences) != 0 || current.Labels["external"] != "kept" {
		t.Fatal("concurrent foreign object was adopted or mutated")
	}
}
