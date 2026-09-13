//go:build live

package publicintegration

import (
	"context"
	"fmt"
	"sort"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// operatorContainer records requested images separately from the runtime's reported identity.
type operatorContainer struct {
	Name        string `json:"name"`
	Image       string `json:"image"`
	ImageID     string `json:"imageID"`
	ContainerID string `json:"containerID"`
}

// operatorPod records only public process metadata, excluding environment and mounted data.
type operatorPod struct {
	Pod        identity            `json:"pod"`
	ReplicaSet identity            `json:"replicaSet"`
	Containers []operatorContainer `json:"containers"`
}

// recordOperator captures the selected installation before creating a fixture; it never requests a rollout.
func (h *harness) recordOperator(ctx context.Context) error {
	var deployment appsv1.Deployment
	if err := h.c.Get(
		ctx,
		client.ObjectKey{Namespace: h.config.operatorNamespace, Name: h.config.operatorName},
		&deployment,
	); err != nil {
		return err
	}
	if deployment.UID == "" || deployment.UID != h.config.operatorUID || deployment.DeletionTimestamp != nil {
		return fmt.Errorf("operator evidence: Deployment identity unavailable or changed")
	}
	var sets appsv1.ReplicaSetList
	if err := h.c.List(ctx, &sets, client.InNamespace(deployment.Namespace)); err != nil {
		return err
	}
	owned := map[types.UID]*appsv1.ReplicaSet{}
	for i := range sets.Items {
		rs := &sets.Items[i]
		if rs.UID != "" && metav1.IsControlledBy(rs, &deployment) {
			owned[rs.UID] = rs
		}
	}
	var pods corev1.PodList
	if err := h.c.List(ctx, &pods, client.InNamespace(deployment.Namespace)); err != nil {
		return err
	}
	observed := []operatorPod{}
	for _, pod := range pods.Items {
		owner := metav1.GetControllerOf(&pod)
		if owner == nil || owned[owner.UID] == nil || !metav1.IsControlledBy(&pod, owned[owner.UID]) ||
			pod.DeletionTimestamp != nil ||
			pod.Status.Phase != corev1.PodRunning {
			continue
		}
		if pod.UID == "" {
			return fmt.Errorf("operator evidence: Pod UID missing")
		}
		item := operatorPod{Pod: objectIdentity(&pod), ReplicaSet: objectIdentity(owned[owner.UID])}
		for _, container := range pod.Spec.Containers {
			found := false
			for _, status := range pod.Status.ContainerStatuses {
				if status.Name != container.Name {
					continue
				}
				if status.State.Running == nil || status.ImageID == "" || status.ContainerID == "" {
					return fmt.Errorf(
						"operator evidence: running image identity missing for %s/%s",
						pod.Name,
						container.Name,
					)
				}
				item.Containers = append(
					item.Containers,
					operatorContainer{
						Name:        container.Name,
						Image:       container.Image,
						ImageID:     status.ImageID,
						ContainerID: status.ContainerID,
					},
				)
				found = true
			}
			if !found {
				return fmt.Errorf("operator evidence: container status missing for %s/%s", pod.Name, container.Name)
			}
		}
		if len(item.Containers) == 0 {
			return fmt.Errorf("operator evidence: Pod containers missing")
		}
		observed = append(observed, item)
	}
	if len(observed) == 0 {
		return fmt.Errorf("operator evidence: no owned running Pods")
	}
	sort.Slice(observed, func(i, j int) bool { return observed[i].Pod.Name < observed[j].Pod.Name })
	requested := map[string]string{}
	for _, container := range deployment.Spec.Template.Spec.Containers {
		requested[container.Name] = container.Image
	}
	return h.event(
		"operator-artifact",
		map[string]any{
			"namespace":       deployment.Namespace,
			"deployment":      objectIdentity(&deployment),
			"requestedImages": requested,
			"pods":            observed,
		},
	)
}

// recordGenesis captures immutable gates as soon as the root publishes their exact artifact.
func (h *harness) recordGenesis(ctx context.Context, root *api.StacksNetwork) error {
	ref := root.Status.GenesisRef
	if ref == nil {
		return nil
	}
	if ref.UID == "" {
		return fmt.Errorf("genesis evidence: published UID missing")
	}
	if h.recordedGenesisUID == ref.UID {
		return nil
	}
	var genesis api.StacksGenesis
	if err := h.c.Get(ctx, client.ObjectKey{Namespace: root.Namespace, Name: ref.Name}, &genesis); err != nil {
		return err
	}
	if genesis.UID != ref.UID || genesis.DeletionTimestamp != nil || genesis.Spec.Source.NetworkUID != root.UID ||
		!metav1.IsControlledBy(&genesis, root) {
		return fmt.Errorf("genesis evidence: artifact identity differs from root")
	}
	if err := h.event(
		"genesis-artifact",
		map[string]any{
			"networkUID":    root.UID,
			"genesis":       objectIdentity(&genesis),
			"genesisDigest": root.Status.GenesisDigest,
			"gates":         genesis.Spec.Bootstrap.Gates,
		},
	); err != nil {
		return err
	}
	h.recordedGenesisUID = genesis.UID
	return nil
}
