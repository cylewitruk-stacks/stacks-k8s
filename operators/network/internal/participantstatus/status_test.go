package participantstatus

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// legacyEntry describes the preview's status Update ownership without emulating SSA.
func legacyEntry(fields string) metav1.ManagedFieldsEntry {
	return metav1.ManagedFieldsEntry{
		Manager:     "foundation",
		Operation:   metav1.ManagedFieldsOperationUpdate,
		APIVersion:  api.GroupVersion.String(),
		Subresource: "status",
		FieldsType:  "FieldsV1",
		FieldsV1:    metav1.NewFieldsV1(fields),
	}
}

func TestLegacyManagersRequireAllMatchingEntriesToBeSafe(t *testing.T) {
	safe := legacyEntry(
		`{"f:status":{"f:admission":{"f:policyDigest":{}},` +
			`"f:conditions":{"k:{\"type\":\"Resolved\"}":{"f:reason":{}}}}}`,
	)
	for name, change := range map[string]func(*metav1.ManagedFieldsEntry){
		"runtime subtree": func(e *metav1.ManagedFieldsEntry) {
			e.FieldsV1.SetRawString(`{"f:status":{"f:runtime":{}}}`)
		},
		"foreign condition": func(e *metav1.ManagedFieldsEntry) {
			e.FieldsV1.SetRawString(`{"f:status":{"f:conditions":{"k:{\"type\":\"ConfigVerified\"}":{}}}}`)
		},
		"mixed metadata": func(e *metav1.ManagedFieldsEntry) {
			e.FieldsV1.SetRawString(`{"f:metadata":{},"f:status":{"f:admission":{}}}`)
		},
		"malformed JSON": func(e *metav1.ManagedFieldsEntry) { e.FieldsV1.SetRawString(`{`) },
		"null status":    func(e *metav1.ManagedFieldsEntry) { e.FieldsV1.SetRawString(`{"f:status":null}`) },
		"scalar admission": func(e *metav1.ManagedFieldsEntry) {
			e.FieldsV1.SetRawString(`{"f:status":{"f:admission":3}}`)
		},
		"scalar conditions": func(e *metav1.ManagedFieldsEntry) {
			e.FieldsV1.SetRawString(`{"f:status":{"f:conditions":"bad"}}`)
		},
		"missing fields":      func(e *metav1.ManagedFieldsEntry) { e.FieldsV1 = nil },
		"unknown fields type": func(e *metav1.ManagedFieldsEntry) { e.FieldsType = "Other" },
		"other API version":   func(e *metav1.ManagedFieldsEntry) { e.APIVersion = "network.stacks.org/v1alpha1" },
	} {
		t.Run(name, func(t *testing.T) {
			unsafe := *safe.DeepCopy()
			change(&unsafe)
			for _, entries := range [][]metav1.ManagedFieldsEntry{{unsafe}, {safe, unsafe}, {unsafe, safe}} {
				p := &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{ManagedFields: entries}}
				if NeedsMigration(p) {
					t.Fatal("unsafe same-manager entry authorized migration")
				}
			}
		})
	}
	foreign := *safe.DeepCopy()
	foreign.Manager = "external-readiness"
	domain := *safe.DeepCopy()
	domain.Manager = "stacks-network-domain-bitcoinnode"
	spec := legacyEntry(`{"f:spec":{"f:kind":{}}}`)
	spec.Subresource = ""
	applied := *safe.DeepCopy()
	applied.Operation = metav1.ManagedFieldsOperationApply
	for _, e := range []metav1.ManagedFieldsEntry{foreign, domain, spec, applied} {
		p := &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{ManagedFields: []metav1.ManagedFieldsEntry{e}}}
		if NeedsMigration(p) {
			t.Fatalf("unrelated manager selected: %+v", e)
		}
		p.ManagedFields = append(p.ManagedFields, safe)
		if got := legacyManagers(p).UnsortedList(); !slices.Equal(got, []string{"foundation"}) {
			t.Fatalf("safe legacy migration lost beside unrelated entry: %v", got)
		}
	}
}

// statusFixture provides separately persisted old admission and unrelated domain/spec ownership.
func statusFixture() *api.StacksNetworkParticipant {
	legacy := legacyEntry(`{"f:status":{"f:admission":{"f:policyDigest":{}}}}`)
	spec := legacyEntry(`{"f:spec":{"f:kind":{}}}`)
	spec.Subresource = ""
	domain := legacyEntry(`{"f:status":{"f:conditions":{"k:{\"type\":\"WorkloadReady\"}":{"f:reason":{}}}}}`)
	domain.Manager = "external-readiness"
	return &api.StacksNetworkParticipant{
		TypeMeta: metav1.TypeMeta{APIVersion: api.GroupVersion.String(), Kind: "StacksNetworkParticipant"},
		ObjectMeta: metav1.ObjectMeta{
			Name:            "participant",
			Namespace:       "lab",
			UID:             "original",
			ResourceVersion: "1",
			ManagedFields:   []metav1.ManagedFieldsEntry{legacy, spec, domain},
		},
		Spec: api.StacksNetworkParticipantSpec{
			Kind:          "BitcoinNode",
			Configuration: api.Configuration{BitcoinNode: &bitcoin.BitcoinNodeSpec{}},
		},
		Status: api.ParticipantStatus{
			Admission: &api.Admission{
				PolicyDigest:  "old",
				Configuration: api.Configuration{BitcoinNode: &bitcoin.BitcoinNodeSpec{}},
			},
			Runtime: &api.ParticipantRuntimeStatus{PolicyDigest: "domain-policy"},
		},
	}
}

// statusClient intercepts the SSA transport only; API-server ownership behavior belongs in envtest.
func statusClient(t *testing.T, live *api.StacksNetworkParticipant, hooks interceptor.Funcs) client.WithWatch {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithReturnManagedFields().
		WithStatusSubresource(&api.StacksNetworkParticipant{}).
		WithObjects(live.DeepCopy()).
		WithInterceptorFuncs(hooks).
		Build()
}

// checkMigrationPatch verifies the exact metadata-only operation scope and stale-identity guards.
func checkMigrationPatch(t *testing.T, p client.Object, patch client.Patch) {
	t.Helper()
	if patch.Type() != types.JSONPatchType {
		t.Fatal("migration is not JSON patch")
	}
	data, err := patch.Data(p)
	if err != nil {
		t.Fatal(err)
	}
	var operations []map[string]json.RawMessage
	if err := json.Unmarshal(data, &operations); err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, op := range operations {
		var path string
		if err := json.Unmarshal(op["path"], &path); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	if !slices.Equal(paths, []string{"/metadata/uid", "/metadata/managedFields", "/metadata/resourceVersion"}) {
		t.Fatalf("unexpected migration scope: %s", data)
	}
	var operation, uid, revision string
	if err := json.Unmarshal(operations[0]["op"], &operation); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(operations[0]["value"], &uid); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(operations[2]["value"], &revision); err != nil {
		t.Fatal(err)
	}
	if operation != "test" || uid != string(p.GetUID()) || revision != p.GetResourceVersion() {
		t.Fatalf("missing migration preconditions: %s", data)
	}
}

func TestApplyPreservesAliasedCandidateAcrossMigrationResponse(t *testing.T) {
	live := statusFixture()
	p := live.DeepCopy()
	p.Status.Admission.PolicyDigest = "candidate"
	owned := api.ParticipantStatus{Admission: p.Status.Admission}
	migrations, applies := 0, 0
	c := statusClient(t, live, interceptor.Funcs{
		Patch: func(
			ctx context.Context,
			c client.WithWatch,
			obj client.Object,
			patch client.Patch,
			opts ...client.PatchOption,
		) error {
			migrations++
			checkMigrationPatch(t, obj, patch)
			if err := c.Patch(ctx, obj, patch, opts...); err != nil {
				return err
			}
			// Exercise the same pointer-reusing JSON response decode as the typed client.
			response := obj.DeepCopyObject().(*api.StacksNetworkParticipant)
			response.Status.Admission.PolicyDigest = "old"
			data, _ := json.Marshal(response)
			return json.Unmarshal(data, obj)
		},
		SubResourcePatch: func(
			_ context.Context,
			_ client.Client,
			subresource string,
			obj client.Object,
			patch client.Patch,
			opts ...client.SubResourcePatchOption,
		) error {
			applies++
			if subresource != "status" || patch.Type() != types.ApplyPatchType {
				t.Fatal("wrong status transport")
			}
			options := &client.SubResourcePatchOptions{}
			for _, opt := range opts {
				opt.ApplyToSubResourcePatch(options)
			}
			if options.FieldManager != AggregateManager || options.Force == nil || !*options.Force {
				t.Fatal("incorrect apply options")
			}
			body := obj.(*api.StacksNetworkParticipant)
			if body.Status.Admission.PolicyDigest != "candidate" || body.Status.Runtime != nil ||
				len(body.Status.Conditions) != 0 {
				t.Fatalf("migration changed or broadened candidate payload: %+v", body.Status)
			}
			if body.ResourceVersion == "1" || body.UID != live.UID {
				t.Fatal("apply did not use refreshed migration revision and pinned UID")
			}
			if len(body.ManagedFields) != 0 || len(body.OwnerReferences) != 0 || body.Spec.Kind != "" {
				t.Fatal("apply copied fetched metadata/spec")
			}
			body.ResourceVersion = "3"
			return nil
		},
	})
	if err := Apply(context.Background(), c, p, owned, AggregateManager); err != nil {
		t.Fatal(err)
	}
	if migrations != 1 || applies != 1 || p.Status.Admission.PolicyDigest != "candidate" || p.ResourceVersion != "3" {
		t.Fatal("migration/apply sequence or response refresh failed")
	}
	var persisted api.StacksNetworkParticipant
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(live), &persisted); err != nil {
		t.Fatal(err)
	}
	for _, original := range live.ManagedFields[1:] {
		if !slices.ContainsFunc(
			persisted.ManagedFields,
			func(entry metav1.ManagedFieldsEntry) bool { return reflect.DeepEqual(entry, original) },
		) {
			t.Fatalf("migration changed unrelated manager %q/%q", original.Manager, original.Subresource)
		}
	}
	if persisted.Status.Admission.PolicyDigest != "old" || persisted.Status.Runtime.PolicyDigest != "domain-policy" {
		t.Fatal("metadata migration changed persisted status")
	}
}

func TestMigrationFailuresPreventStatusApply(t *testing.T) {
	for _, failure := range []string{"conflict", "replacement"} {
		t.Run(failure, func(t *testing.T) {
			live := statusFixture()
			p := live.DeepCopy()
			applies := 0
			if failure == "replacement" {
				live.UID = "replacement"
			}
			conflict := apierrors.NewConflict(
				schema.GroupResource{Group: api.GroupVersion.Group, Resource: "stacksnetworkparticipants"},
				p.Name,
				errors.New("stale revision"),
			)
			c := statusClient(t, live, interceptor.Funcs{
				Patch: func(
					ctx context.Context,
					c client.WithWatch,
					obj client.Object,
					patch client.Patch,
					opts ...client.PatchOption,
				) error {
					checkMigrationPatch(t, obj, patch)
					if failure == "conflict" {
						return conflict
					}
					return c.Patch(ctx, obj, patch, opts...)
				},
				SubResourcePatch: func(
					context.Context,
					client.Client,
					string,
					client.Object,
					client.Patch,
					...client.SubResourcePatchOption,
				) error {
					applies++
					return nil
				},
			})
			err := Apply(
				context.Background(),
				c,
				p,
				api.ParticipantStatus{Admission: p.Status.Admission},
				AggregateManager,
			)
			if err == nil || applies != 0 {
				t.Fatalf("migration failure reached status apply: err=%v, applies=%d", err, applies)
			}
			if failure == "conflict" && !apierrors.IsConflict(err) {
				t.Fatalf("conflict was hidden: %v", err)
			}
			var current api.StacksNetworkParticipant
			if err := c.Get(context.Background(), client.ObjectKeyFromObject(live), &current); err != nil {
				t.Fatal(err)
			}
			if current.UID != live.UID || !reflect.DeepEqual(current.Status, live.Status) ||
				!reflect.DeepEqual(current.ManagedFields, live.ManagedFields) {
				t.Fatal("rejected migration changed current identity/status/ownership")
			}
		})
	}
}

func TestMigrationLostAcknowledgementRecoversFromFreshRead(t *testing.T) {
	live := statusFixture()
	p := live.DeepCopy()
	migrations, applies := 0, 0
	lost := errors.New("migration response lost")
	c := statusClient(t, live, interceptor.Funcs{
		Patch: func(
			ctx context.Context,
			c client.WithWatch,
			obj client.Object,
			patch client.Patch,
			opts ...client.PatchOption,
		) error {
			migrations++
			checkMigrationPatch(t, obj, patch)
			if err := c.Patch(ctx, obj, patch, opts...); err != nil {
				return err
			}
			return lost
		},
		SubResourcePatch: func(
			context.Context,
			client.Client,
			string,
			client.Object,
			client.Patch,
			...client.SubResourcePatchOption,
		) error {
			applies++
			return nil
		},
	})
	owned := api.ParticipantStatus{Admission: live.Status.Admission.DeepCopy()}
	if err := Apply(context.Background(), c, p, owned, AggregateManager); !errors.Is(err, lost) {
		t.Fatalf("lost acknowledgement not returned: %v", err)
	}
	if applies != 0 {
		t.Fatal("status applied after uncertain migration acknowledgement")
	}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(live), p); err != nil {
		t.Fatal(err)
	}
	if NeedsMigration(p) || !Managed(p, AggregateManager) {
		t.Fatal("fresh read did not recognize committed conversion")
	}
	if err := Apply(context.Background(), c, p, owned, AggregateManager); err != nil {
		t.Fatal(err)
	}
	if migrations != 1 || applies != 1 {
		t.Fatalf("retry repeated migration: migrations=%d, applies=%d", migrations, applies)
	}
}

func TestApplyRequiresIdentityRevisionAndManager(t *testing.T) {
	for _, missing := range []string{"uid", "revision", "manager"} {
		t.Run(missing, func(t *testing.T) {
			p := statusFixture()
			manager := AggregateManager
			switch missing {
			case "uid":
				p.UID = ""
			case "revision":
				p.ResourceVersion = ""
			case "manager":
				manager = ""
			}
			// A nil client makes any pre-validation I/O a test failure.
			if err := Apply(context.Background(), nil, p, api.ParticipantStatus{}, manager); err == nil {
				t.Fatal("accepted incomplete apply identity")
			}
		})
	}
}

func TestOwnsConditionUsesExactManagerSubresourceAndEntry(t *testing.T) {
	entry := legacyEntry(`{"f:status":{"f:conditions":{"k:{\"type\":\"WorkloadReady\"}":{"f:type":{}}}}}`)
	entry.Manager = AggregateManager
	entry.Operation = metav1.ManagedFieldsOperationApply
	p := &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{ManagedFields: []metav1.ManagedFieldsEntry{entry}}}
	if !OwnsCondition(p, AggregateManager, "WorkloadReady") {
		t.Fatal("partial fallback ownership was missed")
	}
	if OwnsCondition(p, "domain", "WorkloadReady") || OwnsCondition(p, AggregateManager, "Resolved") {
		t.Fatal("unrelated condition ownership matched")
	}
	p.ManagedFields[0].Subresource = ""
	if OwnsCondition(p, AggregateManager, "WorkloadReady") || Managed(p, AggregateManager) {
		t.Fatal("non-status ownership matched")
	}
}
