// Command manager runs namespaced bounded-action lifecycle controllers.
package main

import (
	"flag"
	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	bitcoinv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	networkv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/action/controllers/generation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/action/controllers/reorganization"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/action/internal/manager"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"os"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func main() {
	options := manager.Options{}
	options.Bind(flag.CommandLine)
	logs := zap.Options{Development: false}
	logs.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&logs)))
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{clientgoscheme.AddToScheme, actionv1.AddToScheme, bitcoinv1.AddToScheme, networkv1.AddToScheme} {
		must(add(scheme))
	}
	configuration, err := ctrl.GetConfig()
	must(err)
	m, err := options.New(configuration, scheme)
	must(err)
	if options.GenerationEnabled {
		must((&generation.Reconciler{Client: m.GetClient(), APIReader: m.GetAPIReader(), Concurrency: options.Concurrency}).SetupWithManager(m))
	}
	if options.ReorganizationEnabled {
		must((&reorganization.Reconciler{Client: m.GetClient(), APIReader: m.GetAPIReader(), Concurrency: options.Concurrency}).SetupWithManager(m))
	}
	must(m.Start(ctrl.SetupSignalHandler()))
}

func must(err error) {
	if err != nil {
		ctrl.Log.Error(err, "fatal controller error")
		os.Exit(1)
	}
}
