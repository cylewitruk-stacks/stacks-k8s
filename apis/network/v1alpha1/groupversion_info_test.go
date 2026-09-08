package v1alpha1

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
)

func TestAddToSchemeRegistersEveryTopologyKind(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	objects := []runtime.Object{
		&BitcoinNode{},
		&BitcoinNodeList{},
		&StacksNetwork{},
		&StacksNetworkList{},
		&StacksNode{},
		&StacksNodeList{},
		&StacksSigner{},
		&StacksSignerList{},
	}
	for _, object := range objects {
		gvks, _, err := scheme.ObjectKinds(object)
		if err != nil {
			t.Fatal(err)
		}
		if len(gvks) != 1 || gvks[0].GroupVersion() != GroupVersion {
			t.Fatalf("%T registered as %v", object, gvks)
		}
	}
}
