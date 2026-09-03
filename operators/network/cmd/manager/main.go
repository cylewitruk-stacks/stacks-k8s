// Command manager runs the standalone Stacks network topology controllers.
package main

import (
	"flag"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/operators/network/api/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/bitcoinnode"
	manageroptions "github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/manager"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/network"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksnode"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stackssigner"
)

func main() {
	logging := zap.Options{Development: false}
	logging.BindFlags(flag.CommandLine)
	options := manageroptions.Options{}
	options.Bind(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&logging)))

	scheme := runtime.NewScheme()
	must(clientgoscheme.AddToScheme(scheme))
	must(appsv1.AddToScheme(scheme))
	must(corev1.AddToScheme(scheme))
	must(networkv1alpha1.AddToScheme(scheme))
	manager, err := options.New(scheme)
	must(err)

	must((&network.Reconciler{Client: manager.GetClient(), APIReader: manager.GetAPIReader(), Scheme: manager.GetScheme()}).SetupWithManager(manager, options.Concurrency))
	must((&bitcoinnode.Reconciler{Client: manager.GetClient(), APIReader: manager.GetAPIReader(), Scheme: manager.GetScheme()}).SetupWithManager(manager, options.Concurrency))
	must((&stacksnode.Reconciler{Client: manager.GetClient(), APIReader: manager.GetAPIReader(), Scheme: manager.GetScheme()}).SetupWithManager(manager, options.Concurrency))
	must((&stackssigner.Reconciler{Client: manager.GetClient(), APIReader: manager.GetAPIReader(), Scheme: manager.GetScheme()}).SetupWithManager(manager, options.Concurrency))
	must(manager.Start(ctrl.SetupSignalHandler()))
}

func must(err error) {
	if err != nil {
		ctrl.Log.Error(err, "fatal error")
		os.Exit(1)
	}
}
