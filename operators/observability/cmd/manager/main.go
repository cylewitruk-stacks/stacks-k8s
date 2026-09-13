// Command manager runs the read-only observability operator.
package main

import (
	"flag"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	observationv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/operators/observability/api/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/observability/internal/manager"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/observability/internal/observation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/observability/internal/topology"
)

func main() {
	options := manager.Options{}
	options.Bind(flag.CommandLine)
	zapOptions := zap.Options{Development: false}
	zapOptions.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zapOptions)))

	scheme := runtime.NewScheme()
	must(clientgoscheme.AddToScheme(scheme))
	must(observationv1alpha1.AddToScheme(scheme))
	topology.AddNetworkTypes(scheme)
	controllerManager, err := options.New(scheme)
	must(err)
	must(
		(&observation.Reconciler{
			Client:    controllerManager.GetClient(),
			APIReader: controllerManager.GetAPIReader(),
		}).SetupWithManager(
			controllerManager,
			options.Concurrency,
		),
	)
	must(controllerManager.Start(ctrl.SetupSignalHandler()))
}

func must(err error) {
	if err != nil {
		ctrl.Log.Error(err, "fatal controller error")
		os.Exit(1)
	}
}
