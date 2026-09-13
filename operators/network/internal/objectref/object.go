package objectref

import (
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// Fresh allocates an empty registered object of the same Kubernetes type.
// Callers must validate their supported-kind set before using this generic factory.
func Fresh(object client.Object, scheme *runtime.Scheme) (client.Object, error) {
	if object == nil || (reflect.ValueOf(object).Kind() == reflect.Pointer && reflect.ValueOf(object).IsNil()) || scheme == nil {
		return nil, fmt.Errorf("object and scheme are required")
	}
	gvk, err := apiutil.GVKForObject(object, scheme)
	if err != nil {
		return nil, err
	}
	fresh, err := scheme.New(gvk)
	if err != nil {
		return nil, err
	}
	result, ok := fresh.(client.Object)
	if !ok {
		return nil, fmt.Errorf("registered type %s is not a client object", gvk)
	}
	result.GetObjectKind().SetGroupVersionKind(gvk)
	return result, nil
}
