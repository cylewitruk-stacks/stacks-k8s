//go:build live

package publicintegration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// waitClient injects read failures without creating any live-cluster client.
type waitClient struct {
	client.Client
	getError, listError error
	once                bool
	gets, lists         int
}

func (c *waitClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	c.gets++
	if c.getError != nil && (!c.once || c.gets == 1) {
		return c.getError
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

func (c *waitClient) List(ctx context.Context, obj client.ObjectList, opts ...client.ListOption) error {
	c.lists++
	if c.listError != nil {
		return c.listError
	}
	return c.Client.List(ctx, obj, opts...)
}

// waitFixture supplies a known root identity and a local evidence directory.
func waitFixture(t *testing.T, root *api.StacksNetwork) (*harness, *waitClient) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := bitcoin.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	builder := fake.NewClientBuilder().WithScheme(scheme)
	if root != nil {
		builder = builder.WithObjects(root)
	}
	c := &waitClient{Client: builder.Build()}
	return &harness{
		c:        c,
		rootUID:  "root-uid",
		config:   liveConfig{fixtureOptions: fixtureOptions{namespace: "test"}},
		evidence: t.TempDir(),
	}, c
}

func waitRoot() *api.StacksNetwork {
	return &api.StacksNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "test", UID: "root-uid", Generation: 1},
		Spec:       api.StacksNetworkSpec{Operation: "Running"},
	}
}

func TestWaitFailsOnExactRootLossOrTerminalControl(t *testing.T) {
	for _, mode := range []string{"missing", "replacement", "stopped", "deleting", "failed"} {
		t.Run(mode, func(t *testing.T) {
			root := waitRoot()
			switch mode {
			case "missing":
				root = nil
			case "replacement":
				root.UID = "other-root"
			case "stopped":
				root.Spec.Operation = "Stopped"
			case "deleting":
				root.DeletionTimestamp = &metav1.Time{Time: time.Now()}
				root.Finalizers = []string{"test/retained"}
			case "failed":
				root.Status.Phase = "Failed"
			}
			h, c := waitFixture(t, root)
			// Terminal root control must be visible even if participant discovery is unavailable.
			c.listError = apierrors.NewServiceUnavailable("collection unavailable")
			called := false
			result, err := h.wait(
				context.Background(),
				"progress",
				time.Minute,
				true,
				func(snapshot) (bool, error) { called = true; return true, nil },
			)
			if err == nil || errors.Is(err, context.DeadlineExceeded) || called || c.gets != 1 || c.lists != 0 {
				t.Fatalf("terminal result=%v called=%v gets=%d lists=%d", err, called, c.gets, c.lists)
			}
			if (mode == "missing" || mode == "replacement") && (result.Root.UID != "" || !result.At.IsZero()) {
				t.Fatal("root loss fabricated a fresh observation")
			}
		})
	}
}

func TestWaitRetriesReadFailuresAndMissingStartupObservations(t *testing.T) {
	for _, mode := range []string{
		"transient-root",
		"transient-collection",
		"missing-collection",
		"startup-empty",
		"unbound-root",
	} {
		t.Run(mode, func(t *testing.T) {
			root := waitRoot()
			if mode == "unbound-root" {
				root = nil
			}
			h, c := waitFixture(t, root)
			switch mode {
			case "transient-root":
				c.getError = apierrors.NewServiceUnavailable("retry")
			case "transient-collection":
				c.listError = apierrors.NewServiceUnavailable("retry")
			case "missing-collection":
				c.listError = apierrors.NewNotFound(
					schema.GroupResource{Group: api.GroupVersion.Group, Resource: "stacksnetworkparticipants"},
					"startup",
				)
			case "unbound-root":
				h.rootUID = ""
			}
			accepts := 0
			_, err := h.wait(
				context.Background(),
				"startup",
				40*time.Millisecond,
				true,
				func(s snapshot) (bool, error) { accepts++; return len(s.Participants) > 0, nil },
			)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("retryable observation failed fast: %v", err)
			}
			if mode == "startup-empty" {
				if accepts != 1 {
					t.Fatal("successful empty startup read was not evaluated")
				}
			} else if accepts != 0 {
				t.Fatal("failed read reached success predicate")
			}
		})
	}
}

func TestWaitCanObserveRecoveryWithoutAcceptingFailedRead(t *testing.T) {
	h, c := waitFixture(t, waitRoot())
	c.getError = apierrors.NewTimeoutError("temporary", 0)
	c.once = true
	accepts := 0
	result, err := h.wait(
		context.Background(),
		"recovered",
		5*time.Second,
		true,
		func(s snapshot) (bool, error) { accepts++; return s.Root.UID == h.rootUID, nil },
	)
	if err != nil || accepts != 1 || c.gets != 2 || result.At.IsZero() {
		t.Fatalf("recovery err=%v accepts=%d gets=%d", err, accepts, c.gets)
	}
}

func TestWaitAllowsExpectedStopInCleanupAndHonorsCancellation(t *testing.T) {
	root := waitRoot()
	root.Spec.Operation = "Stopped"
	root.Status.Phase = "Stopped"
	h, _ := waitFixture(t, root)
	if _, err := h.wait(
		context.Background(),
		"cleanup",
		time.Second,
		false,
		func(s snapshot) (bool, error) { return s.Status.Phase == "Stopped", nil },
	); err != nil {
		t.Fatal(err)
	}
	h, _ = waitFixture(t, waitRoot())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.wait(
		ctx,
		"interrupted",
		time.Minute,
		true,
		func(snapshot) (bool, error) { t.Fatal("canceled failed read was accepted"); return true, nil },
	); !errors.Is(err, context.Canceled) ||
		!strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("cancellation: %v", err)
	}
}

// TestWaitRetainsLastActiveEvidenceOnFailure preserves prior actor facts without delaying terminal detection.
func TestWaitRetainsLastActiveEvidenceOnFailure(t *testing.T) {
	root := waitRoot()
	h, c := waitFixture(t, root)
	ctx := context.Background()
	p := &api.StacksNetworkParticipant{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "p",
			Namespace: "test",
			UID:       "p",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: api.GroupVersion.String(),
					Kind:       "StacksNetwork",
					Name:       root.Name,
					UID:        root.UID,
					Controller: ptr.To(true),
				},
			},
		},
		Spec: api.StacksNetworkParticipantSpec{NetworkUID: root.UID},
	}
	if err := c.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	first := true
	_, err := h.wait(ctx, "progress", time.Minute, true, func(_ snapshot) (bool, error) {
		if first {
			first = false
			root.Status.Phase = "Failed"
			if err := c.Update(ctx, root); err != nil {
				return false, err
			}
		}
		return false, nil
	})
	if err == nil {
		t.Fatal("failure not reported")
	}
	data, err := os.ReadFile(filepath.Join(h.evidence, "progress-last-active.json"))
	if err != nil {
		t.Fatal(err)
	}
	var captured snapshot
	if err = json.Unmarshal(data, &captured); err != nil {
		t.Fatal(err)
	}
	if captured.Status.Phase == "Failed" || len(captured.Participants) != 1 ||
		captured.Participants[0].Identity.UID != p.UID {
		t.Fatal("prior participant evidence was lost or relabeled")
	}
}
