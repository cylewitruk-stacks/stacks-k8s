//go:build live

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	operatorlabels "github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/labels"
)

const liveTimeout = 4 * time.Minute

type liveResult struct {
	SchemaVersion       string          `json:"schemaVersion"`
	Namespace           string          `json:"namespace"`
	Network             string          `json:"network"`
	InitialDigest       string          `json:"initialDigest"`
	ExpandedDigest      string          `json:"expandedDigest"`
	ConfigurationDigest string          `json:"configurationDigest"`
	ResumedDigest       string          `json:"resumedDigest"`
	ReplacementDigest   string          `json:"replacementDigest"`
	FinalDigest         string          `json:"finalDigest"`
	Assertions          map[string]bool `json:"assertions"`
	CompletedAt         metav1.Time     `json:"completedAt"`
}

func TestLiveMutableTopologyLifecycle(t *testing.T) {
	namespace := requiredEnvironment(t, "STACKS_NETWORK_LIVE_NAMESPACE")
	actorImage := requiredEnvironment(t, "STACKS_NETWORK_LIVE_ACTOR_IMAGE")
	resultPath := os.Getenv("STACKS_NETWORK_LIVE_RESULT")

	scheme := runtime.NewScheme()
	liveMust(t, clientgoscheme.AddToScheme(scheme))
	liveMust(t, networkv1alpha1.AddToScheme(scheme))
	configuration := liveConfiguration(t)
	kubeClient, err := client.New(configuration, client.Options{Scheme: scheme})
	liveMust(t, err)
	ctx := context.Background()
	name := fmt.Sprintf("topology-live-%d", time.Now().Unix())
	key := types.NamespacedName{Namespace: namespace, Name: name}
	result := liveResult{SchemaVersion: "network.stacks.org/live-qualification/v1", Namespace: namespace, Network: name, Assertions: map[string]bool{}}

	networkObject := liveNetwork(name, namespace, actorImage)
	liveMust(t, kubeClient.Create(ctx, networkObject))
	t.Cleanup(func() {
		current := &networkv1alpha1.StacksNetwork{}
		if err := kubeClient.Get(ctx, key, current); err == nil {
			_ = kubeClient.Delete(ctx, current)
		}
	})

	initial := waitNetworkReady(t, ctx, kubeClient, key, 2)
	result.InitialDigest = initial.Status.InventoryDigest
	result.Assertions["initial-topology-ready"] = true
	initialBitcoinPod := actorIdentity(t, initial, "bitcoin").PodUID
	initialFollowerPod := actorIdentity(t, initial, "follower").PodUID

	oldManagerUID := restartManager(t, ctx, kubeClient, namespace)
	result.Assertions["manager-restarted"] = oldManagerUID != ""

	updateNetwork(t, ctx, kubeClient, key, func(value *networkv1alpha1.StacksNetwork) {
		value.Spec.BitcoinNodes = append(value.Spec.BitcoinNodes, networkv1alpha1.BitcoinNodeTemplate{
			Name:     "bitcoin-peer",
			PeerRefs: []string{"bitcoin"}, Config: liveGeneratedBitcoin(), Container: listener(18443),
		})
	})
	expanded := waitNetworkReady(t, ctx, kubeClient, key, 3)
	assertDifferent(t, "expanded inventory", initial.Status.InventoryDigest, expanded.Status.InventoryDigest)
	assertSame(t, "unrelated actor addition Bitcoin Pod", string(initialBitcoinPod), string(actorIdentity(t, expanded, "bitcoin").PodUID))
	assertSame(t, "unrelated actor addition Stacks Pod", string(initialFollowerPod), string(actorIdentity(t, expanded, "follower").PodUID))
	result.ExpandedDigest = expanded.Status.InventoryDigest
	result.Assertions["actor-added"] = true
	result.Assertions["unrelated-actors-not-rolled"] = true

	beforeConfigPod := actorIdentity(t, expanded, "bitcoin").PodUID
	updateNetwork(t, ctx, kubeClient, key, func(value *networkv1alpha1.StacksNetwork) {
		value.Spec.BitcoinNodes[0].Config = networkv1alpha1.ConfigSource{Inline: &networkv1alpha1.InlineConfig{
			Key: "bitcoin.conf", Data: "regtest=1\nserver=1\n# live qualification rollout\n",
		}}
	})
	configured := waitNetworkReady(t, ctx, kubeClient, key, 3)
	assertDifferent(t, "configuration inventory", expanded.Status.InventoryDigest, configured.Status.InventoryDigest)
	assertDifferent(t, "configuration Pod", beforeConfigPod, actorIdentity(t, configured, "bitcoin").PodUID)
	result.ConfigurationDigest = configured.Status.InventoryDigest
	result.Assertions["configuration-rolled"] = true

	updateNetwork(t, ctx, kubeClient, key, func(value *networkv1alpha1.StacksNetwork) { value.Spec.Suspended = true })
	waitNetworkPhase(t, ctx, kubeClient, key, "Suspended", false)
	waitReplicas(t, ctx, kubeClient, namespace, name, 0)
	result.Assertions["suspension-withdrew-inventory"] = true

	updateNetwork(t, ctx, kubeClient, key, func(value *networkv1alpha1.StacksNetwork) { value.Spec.Suspended = false })
	resumed := waitNetworkReady(t, ctx, kubeClient, key, 3)
	result.ResumedDigest = resumed.Status.InventoryDigest
	result.Assertions["network-resumed"] = true

	beforeReplacement := actorIdentity(t, resumed, "follower").PodUID
	pod := &corev1.Pod{}
	liveMust(t, kubeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: actorIdentity(t, resumed, "follower").PodName}, pod))
	liveMust(t, kubeClient.Delete(ctx, pod, client.PropagationPolicy(metav1.DeletePropagationForeground)))
	replaced := waitInventoryActorUID(t, ctx, kubeClient, key, "follower", beforeReplacement)
	assertDifferent(t, "replacement inventory", resumed.Status.InventoryDigest, replaced.Status.InventoryDigest)
	result.ReplacementDigest = replaced.Status.InventoryDigest
	result.Assertions["pod-replacement-rebound-identity"] = true

	updateNetwork(t, ctx, kubeClient, key, func(value *networkv1alpha1.StacksNetwork) {
		value.Spec.BitcoinNodes = value.Spec.BitcoinNodes[:1]
	})
	finalNetwork := waitNetworkReady(t, ctx, kubeClient, key, 2)
	waitAbsent(t, ctx, kubeClient, types.NamespacedName{Namespace: namespace, Name: name + "-bitcoin-peer"}, &networkv1alpha1.BitcoinNode{})
	result.FinalDigest = finalNetwork.Status.InventoryDigest
	result.Assertions["actor-removed"] = true

	liveMust(t, kubeClient.Delete(ctx, finalNetwork, client.PropagationPolicy(metav1.DeletePropagationForeground)))
	waitNetworkResourcesAbsent(t, ctx, kubeClient, namespace, name)
	result.Assertions["clean-teardown"] = true
	result.CompletedAt = metav1.NewTime(time.Now().UTC())
	if resultPath != "" {
		writeLiveResult(t, resultPath, result)
	}
}

func liveConfiguration(t *testing.T) *rest.Config {
	t.Helper()
	kubeconfig := os.Getenv("STACKS_NETWORK_LIVE_KUBECONFIG")
	contextName := os.Getenv("STACKS_NETWORK_LIVE_CONTEXT")
	if kubeconfig == "" {
		t.Fatal("STACKS_NETWORK_LIVE_KUBECONFIG must name the qualification kubeconfig")
	}
	loading := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	configuration, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loading, overrides).ClientConfig()
	liveMust(t, err)
	return configuration
}

func liveNetwork(name, namespace, image string) *networkv1alpha1.StacksNetwork {
	storage := false
	return &networkv1alpha1.StacksNetwork{TypeMeta: metav1.TypeMeta{APIVersion: networkv1alpha1.GroupVersion.String(), Kind: "StacksNetwork"}, ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}, Spec: networkv1alpha1.StacksNetworkSpec{
		Defaults: networkv1alpha1.NetworkDefaults{
			BitcoinImage: image, StacksNodeImage: image, DependencyImage: image, ImagePullPolicy: corev1.PullIfNotPresent,
			Workload: networkv1alpha1.WorkloadSpec{Storage: &networkv1alpha1.StorageSpec{Enabled: &storage}},
		},
		BitcoinNodes: []networkv1alpha1.BitcoinNodeTemplate{{
			Name: "bitcoin", Config: liveGeneratedBitcoin(), Container: listener(18443),
		}},
		StacksNodes: []networkv1alpha1.StacksNodeTemplate{{
			Name: "follower", Role: networkv1alpha1.StacksNodeFollower, BitcoinNodeRef: "bitcoin",
			Config: liveGeneratedStacks(), Container: listener(20443),
		}},
	}}
}

func liveGeneratedBitcoin() networkv1alpha1.ConfigSource {
	return networkv1alpha1.ConfigSource{Generated: &networkv1alpha1.GeneratedConfig{Profile: "bitcoin-regtest/v1"}}
}

func liveGeneratedStacks() networkv1alpha1.ConfigSource {
	return networkv1alpha1.ConfigSource{Generated: &networkv1alpha1.GeneratedConfig{Profile: "nakamoto-regtest-node/v1"}}
}

func listener(port int) *networkv1alpha1.ContainerOverride {
	return &networkv1alpha1.ContainerOverride{Command: []string{"sh", "-c"}, Args: []string{fmt.Sprintf("while true; do nc -l -p %d >/dev/null; done", port)}}
}

func restartManager(t *testing.T, ctx context.Context, kubeClient client.Client, namespace string) types.UID {
	t.Helper()
	pods := &corev1.PodList{}
	liveMust(t, kubeClient.List(ctx, pods, client.InNamespace(namespace), client.MatchingLabels{"app.kubernetes.io/name": "stacks-network-operator"}))
	if len(pods.Items) != 1 || !podReady(&pods.Items[0]) {
		t.Fatalf("manager Pod count/readiness = %d/%t", len(pods.Items), len(pods.Items) == 1 && podReady(&pods.Items[0]))
	}
	oldUID := pods.Items[0].UID
	liveMust(t, kubeClient.Delete(ctx, &pods.Items[0]))
	eventuallyLive(t, "replacement manager Pod", func() bool {
		current := &corev1.PodList{}
		if err := kubeClient.List(ctx, current, client.InNamespace(namespace), client.MatchingLabels{"app.kubernetes.io/name": "stacks-network-operator"}); err != nil {
			return false
		}
		return len(current.Items) == 1 && current.Items[0].UID != oldUID && podReady(&current.Items[0])
	})
	return oldUID
}

func updateNetwork(t *testing.T, ctx context.Context, kubeClient client.Client, key types.NamespacedName, mutate func(*networkv1alpha1.StacksNetwork)) {
	t.Helper()
	liveMust(t, retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &networkv1alpha1.StacksNetwork{}
		if err := kubeClient.Get(ctx, key, current); err != nil {
			return err
		}
		mutate(current)
		return kubeClient.Update(ctx, current)
	}))
}

func waitNetworkReady(t *testing.T, ctx context.Context, kubeClient client.Client, key types.NamespacedName, actors int32) *networkv1alpha1.StacksNetwork {
	t.Helper()
	var result *networkv1alpha1.StacksNetwork
	eventuallyLive(t, fmt.Sprintf("network Ready with %d actors", actors), func() bool {
		current := &networkv1alpha1.StacksNetwork{}
		if err := kubeClient.Get(ctx, key, current); err != nil {
			return false
		}
		if current.Status.ObservedGeneration != current.Generation || current.Status.Phase != "Ready" || !current.Status.InventoryReady || current.Status.InventoryDigest == "" || current.Status.ReadyActors != actors || current.Status.DesiredActors != actors || int32(len(current.Status.Actors)) != actors {
			return false
		}
		result = current
		return true
	})
	return result
}

func waitNetworkPhase(t *testing.T, ctx context.Context, kubeClient client.Client, key types.NamespacedName, phase string, inventoryReady bool) {
	t.Helper()
	eventuallyLive(t, "network phase "+phase, func() bool {
		current := &networkv1alpha1.StacksNetwork{}
		return kubeClient.Get(ctx, key, current) == nil && current.Status.ObservedGeneration == current.Generation && current.Status.Phase == phase && current.Status.InventoryReady == inventoryReady && (!inventoryReady || current.Status.InventoryDigest != "") && (inventoryReady || current.Status.InventoryDigest == "")
	})
}

func waitReplicas(t *testing.T, ctx context.Context, kubeClient client.Client, namespace, networkName string, replicas int32) {
	t.Helper()
	eventuallyLive(t, fmt.Sprintf("all replicas = %d", replicas), func() bool {
		sets := &appsv1.StatefulSetList{}
		if err := kubeClient.List(ctx, sets, client.InNamespace(namespace), client.MatchingLabels{operatorlabels.NetworkKey: networkName}); err != nil || len(sets.Items) == 0 {
			return false
		}
		for index := range sets.Items {
			if sets.Items[index].Spec.Replicas == nil || *sets.Items[index].Spec.Replicas != replicas {
				return false
			}
		}
		return true
	})
}

func waitInventoryActorUID(t *testing.T, ctx context.Context, kubeClient client.Client, key types.NamespacedName, actor, previous string) *networkv1alpha1.StacksNetwork {
	t.Helper()
	var result *networkv1alpha1.StacksNetwork
	eventuallyLive(t, "replacement identity for "+actor, func() bool {
		current := &networkv1alpha1.StacksNetwork{}
		if err := kubeClient.Get(ctx, key, current); err != nil || !current.Status.InventoryReady {
			return false
		}
		for _, identity := range current.Status.Actors {
			if identity.Name == actor && identity.PodUID != previous {
				result = current
				return true
			}
		}
		return false
	})
	return result
}

func actorIdentity(t *testing.T, networkObject *networkv1alpha1.StacksNetwork, actor string) networkv1alpha1.ActorIdentity {
	t.Helper()
	for _, identity := range networkObject.Status.Actors {
		if identity.Name == actor {
			return identity
		}
	}
	t.Fatalf("actor %q has no admitted identity", actor)
	return networkv1alpha1.ActorIdentity{}
}

func waitAbsent(t *testing.T, ctx context.Context, kubeClient client.Client, key types.NamespacedName, object client.Object) {
	t.Helper()
	eventuallyLive(t, fmt.Sprintf("%T %s absent", object, key.Name), func() bool {
		return apierrors.IsNotFound(kubeClient.Get(ctx, key, object))
	})
}

func waitNetworkResourcesAbsent(t *testing.T, ctx context.Context, kubeClient client.Client, namespace, networkName string) {
	t.Helper()
	eventuallyLive(t, "network resources absent", func() bool {
		if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: networkName}, &networkv1alpha1.StacksNetwork{}); !apierrors.IsNotFound(err) {
			return false
		}
		selector := client.MatchingLabels{operatorlabels.NetworkKey: networkName}
		lists := []client.ObjectList{&networkv1alpha1.BitcoinNodeList{}, &networkv1alpha1.StacksNodeList{}, &networkv1alpha1.StacksSignerList{}, &appsv1.StatefulSetList{}, &corev1.PodList{}, &corev1.ServiceList{}, &corev1.ConfigMapList{}, &corev1.PersistentVolumeClaimList{}}
		for _, list := range lists {
			if err := kubeClient.List(ctx, list, client.InNamespace(namespace), selector); err != nil {
				return false
			}
			if listLength(list) != 0 {
				return false
			}
		}
		return true
	})
}

func listLength(list client.ObjectList) int {
	switch value := list.(type) {
	case *networkv1alpha1.BitcoinNodeList:
		return len(value.Items)
	case *networkv1alpha1.StacksNodeList:
		return len(value.Items)
	case *networkv1alpha1.StacksSignerList:
		return len(value.Items)
	case *appsv1.StatefulSetList:
		return len(value.Items)
	case *corev1.PodList:
		return len(value.Items)
	case *corev1.ServiceList:
		return len(value.Items)
	case *corev1.ConfigMapList:
		return len(value.Items)
	case *corev1.PersistentVolumeClaimList:
		return len(value.Items)
	default:
		panic(fmt.Sprintf("unsupported list %T", list))
	}
}

func podReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func eventuallyLive(t *testing.T, description string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(liveTimeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", description)
}

func assertDifferent(t *testing.T, description, before, after string) {
	t.Helper()
	if before == "" || after == "" || before == after {
		t.Fatalf("%s did not change: before=%q after=%q", description, before, after)
	}
}

func assertSame(t *testing.T, description, before, after string) {
	t.Helper()
	if before == "" || after == "" || before != after {
		t.Fatalf("%s changed: before=%q after=%q", description, before, after)
	}
}

func requiredEnvironment(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("%s is required", name)
	}
	return value
}

func writeLiveResult(t *testing.T, path string, result liveResult) {
	t.Helper()
	content, err := json.MarshalIndent(result, "", "  ")
	liveMust(t, err)
	content = append(content, '\n')
	liveMust(t, os.MkdirAll(filepath.Dir(path), 0o700))
	liveMust(t, os.WriteFile(path, content, 0o600))
}

func liveMust(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
