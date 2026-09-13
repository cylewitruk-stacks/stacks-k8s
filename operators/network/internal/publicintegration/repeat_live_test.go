//go:build live

package publicintegration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// networkEpoch records runtime identities independently of reusable input identities.
type networkEpoch struct {
	Root         types.UID            `json:"rootUID"`
	Genesis      types.UID            `json:"genesisUID"`
	Chain        string               `json:"chainDigest"`
	Participants map[string]types.UID `json:"participants"`
	Pods         map[types.UID]bool   `json:"pods"`
	Claims       map[types.UID]bool   `json:"claims"`
	Volumes      map[string]bool      `json:"volumes"`
}

// captureNetworkEpoch reads actual actor storage and worker bindings without reading credentials.
func (h *harness) captureNetworkEpoch(ctx context.Context, s snapshot) (networkEpoch, error) {
	e := networkEpoch{
		Root:         h.rootUID,
		Chain:        s.Status.GenesisDigest,
		Participants: map[string]types.UID{},
		Pods:         map[types.UID]bool{},
		Claims:       map[types.UID]bool{},
		Volumes:      map[string]bool{},
	}
	if s.Status.GenesisRef == nil || s.Status.GenesisRef.UID == "" {
		return e, fmt.Errorf("frozen genesis unavailable")
	}
	e.Genesis = s.Status.GenesisRef.UID
	for _, p := range s.Participants {
		e.Participants[p.Name] = p.Identity.UID
		if control := p.Status.BitcoinControl; control != nil {
			for _, binding := range control.Pods {
				if binding.Terminated {
					continue
				}
				var pod corev1.Pod
				if err := h.c.Get(
					ctx,
					client.ObjectKey{Namespace: h.config.namespace, Name: binding.Pod.Name},
					&pod,
				); err != nil {
					return e, err
				}
				if binding.Pod.UID == "" || pod.UID != binding.Pod.UID || pod.DeletionTimestamp != nil {
					return e, fmt.Errorf("Bitcoin control Pod changed during epoch capture")
				}
				e.Pods[pod.UID] = true
			}
		}
		if r := p.Status.Runtime; r != nil && r.PodRef != nil &&
			(p.Kind == "BitcoinNode" || p.Kind == "StacksNode" || p.Kind == "StacksSigner") {
			var pod corev1.Pod
			if err := h.c.Get(
				ctx,
				client.ObjectKey{Namespace: h.config.namespace, Name: r.PodRef.Name},
				&pod,
			); err != nil {
				return e, err
			}
			if pod.UID != r.PodRef.UID || pod.DeletionTimestamp != nil {
				return e, fmt.Errorf("actor Pod changed during epoch capture")
			}
			e.Pods[pod.UID] = true
			claim, err := h.actorClaim(ctx, r)
			if err != nil {
				return e, err
			}
			e.Claims[claim.Claim.UID] = true
			e.Volumes[claim.Volume] = true
		}
		if x := p.Status.Execution; x != nil && x.PodUID != "" {
			found := false
			for _, id := range s.Status.Identities {
				if id.UID != p.Identity.UID || id.Worker == nil || id.Worker.Pod.UID != x.PodUID {
					continue
				}
				var pod corev1.Pod
				if err := h.c.Get(
					ctx,
					client.ObjectKey{Namespace: h.config.namespace, Name: id.Worker.Pod.Name},
					&pod,
				); err != nil {
					return e, err
				}
				if pod.UID != x.PodUID || pod.DeletionTimestamp != nil {
					return e, fmt.Errorf("worker Pod changed during epoch capture")
				}
				e.Pods[pod.UID] = true
				found = true
				break
			}
			if !found {
				return e, fmt.Errorf("worker binding unavailable")
			}
		}
	}
	return e, nil
}

// independentNetworkEpoch rejects runtime or storage reuse while requiring the same chain inputs.
func independentNetworkEpoch(old, next networkEpoch) error {
	if old.Root == "" || next.Root == "" || old.Root == next.Root || old.Genesis == "" || next.Genesis == "" ||
		old.Genesis == next.Genesis ||
		old.Chain == "" ||
		old.Chain != next.Chain {
		return fmt.Errorf("repeated network genesis identity differs from contract")
	}
	if len(old.Participants) != len(next.Participants) || len(next.Pods) == 0 || len(old.Pods) != len(next.Pods) ||
		len(next.Claims) == 0 ||
		len(old.Claims) != len(next.Claims) ||
		len(next.Volumes) == 0 ||
		len(old.Volumes) != len(next.Volumes) {
		return fmt.Errorf("repeated network has incomplete runtime evidence")
	}
	for name, id := range old.Participants {
		if current := next.Participants[name]; id == "" || current == "" || id == current {
			return fmt.Errorf("participant %s was reused or lost", name)
		}
	}
	for id := range next.Pods {
		if id == "" || old.Pods[id] {
			return fmt.Errorf("repeated network reused a Pod")
		}
	}
	for id := range next.Claims {
		if id == "" || old.Claims[id] {
			return fmt.Errorf("repeated network reused a claim")
		}
	}
	for name := range next.Volumes {
		if name == "" || old.Volumes[name] {
			return fmt.Errorf("repeated network reused persistent data")
		}
	}
	return nil
}

// recreateNetwork creates one root in the already-owned namespace without reapplying inputs.
func (h *harness) recreateNetwork(ctx context.Context) error {
	var ns corev1.Namespace
	if err := h.c.Get(ctx, client.ObjectKey{Name: h.config.namespace}, &ns); err != nil {
		return err
	}
	if ns.UID != h.namespaceUID || ns.DeletionTimestamp != nil {
		return fmt.Errorf("repeated namespace identity changed")
	}
	root := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": api.GroupVersion.String(),
			"kind":       "StacksNetwork",
			"metadata":   map[string]any{"name": "network", "namespace": h.config.namespace},
			"spec":       h.declared.root.DeepCopy().Object["spec"],
		},
	}
	marker := string(uuid.NewUUID())
	root.SetAnnotations(map[string]string{"network.stacks.org/qualification-create": marker})
	createErr := h.c.Create(ctx, root)
	if createErr != nil {
		current := &api.StacksNetwork{}
		if err := h.c.Get(ctx, client.ObjectKey{Namespace: h.config.namespace, Name: "network"}, current); err != nil {
			return fmt.Errorf("new root create unconfirmed: %w", createErr)
		}
		if current.Annotations["network.stacks.org/qualification-create"] != marker ||
			current.DeletionTimestamp != nil {
			return fmt.Errorf("new root identity cannot be attributed")
		}
		root.SetUID(current.UID)
	}
	if root.GetUID() == "" || root.GetUID() == h.rootUID {
		return fmt.Errorf("new root UID unavailable")
	}
	h.rootUID = root.GetUID()
	h.createdRoot = true
	return h.event(
		"repeated-root-created",
		map[string]any{"namespaceUID": h.namespaceUID, "networkUID": h.rootUID, "createAcknowledged": createErr == nil},
	)
}

// qualifyRepeat proves two network incarnations from the same surviving declarations.
func (h *harness) qualifyRepeat(ctx context.Context, before snapshot) error {
	old, err := h.captureNetworkEpoch(ctx, before)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(old, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(
		filepath.Join(h.evidence, "first-network-epoch.json"),
		append(data, '\n'),
		0o600,
	); err != nil {
		return err
	}
	if err = h.disposeNetwork(ctx, false); err != nil {
		return err
	}
	if err = h.recreateNetwork(ctx); err != nil {
		return err
	}
	ready, err := h.wait(ctx, "repeated-initialized", h.config.timeout, true, func(s snapshot) (bool, error) {
		_, progressing := progress(s)
		return progressing && condition(s, "Initialized", metav1.ConditionTrue) &&
			condition(s, "Operational", metav1.ConditionTrue), nil
	})
	if err != nil {
		return err
	}
	ready, err = h.awaitProgress(ctx, "repeated-native-progress", ready)
	if err != nil {
		return err
	}
	next, err := h.captureNetworkEpoch(ctx, ready)
	if err != nil {
		return err
	}
	if err = independentNetworkEpoch(old, next); err != nil {
		return err
	}
	if err = h.event("independent-network-epochs", map[string]any{"first": old, "second": next}); err != nil {
		return err
	}
	return nil
}
