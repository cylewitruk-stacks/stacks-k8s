package stacksworker

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestNamedWatchRetainsFieldSelectorOnRelistAndReconnect(t *testing.T) {
	gvr := schema.GroupVersionResource{Group: "network.stacks.org", Version: "v1alpha2", Resource: "stacksnetworkparticipants"}
	c := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{gvr: "StacksNetworkParticipantList"})
	lists, watches := 0, 0
	c.PrependReactor("list", "stacksnetworkparticipants", func(action clienttesting.Action) (bool, runtime.Object, error) {
		lists++
		if action.(clienttesting.ListAction).GetListRestrictions().Fields.String() != "metadata.name=exact-participant" {
			t.Fatal("relist escaped named authorization")
		}
		return true, &unstructured.UnstructuredList{}, nil
	})
	c.PrependWatchReactor("stacksnetworkparticipants", func(action clienttesting.Action) (bool, watch.Interface, error) {
		watches++
		if action.(clienttesting.WatchAction).GetWatchRestrictions().Fields.String() != "metadata.name=exact-participant" {
			t.Fatal("watch escaped named authorization")
		}
		return true, watch.NewRaceFreeFake(), nil
	})
	stream := namedWatch(c, "test", ReadBinding{APIVersion: "network.stacks.org/v1alpha2", Resource: gvr.Resource, Name: "exact-participant"})
	for range 2 {
		if _, err := stream.ListWithContext(context.Background(), metav1.ListOptions{FieldSelector: "metadata.name=wrong"}); err != nil {
			t.Fatal(err)
		}
		w, err := stream.WatchWithContext(context.Background(), metav1.ListOptions{ResourceVersion: "12"})
		if err != nil {
			t.Fatal(err)
		}
		w.Stop()
	}
	if lists != 2 || watches != 2 {
		t.Fatal("watch reconnect fixture incomplete")
	}
}
