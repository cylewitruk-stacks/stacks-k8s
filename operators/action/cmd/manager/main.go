// Command manager runs namespaced bounded-action lifecycle controllers.
package main

import (
	"flag"
	"os"

	actionv2 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha2"
	bitcoinv2 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	networkv2 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/action/controllers/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/action/internal/manager"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
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
	for _, add := range []func(*runtime.Scheme) error{clientgoscheme.AddToScheme, actionv2.AddToScheme, bitcoinv2.AddToScheme, networkv2.AddToScheme} {
		must(add(scheme))
	}
	configuration, err := ctrl.GetConfig()
	must(err)
	m, err := options.New(configuration, scheme)
	must(err)
	for kind, enabled := range map[actionv2.Kind]bool{actionv2.KindBitcoinBlockGeneration: options.GenerationEnabled, actionv2.KindBitcoinReorganization: options.ReorganizationEnabled} {
		if enabled {
			must((&foundation.Reconciler{Client: m.GetClient(), Reader: m.GetAPIReader(), Kind: kind, Concurrency: options.Concurrency}).SetupWithManager(m))
		}
	}
	must(m.Start(ctrl.SetupSignalHandler()))
}

func must(err error) {
	if err != nil {
		ctrl.Log.Error(err, "fatal controller error")
		os.Exit(1)
	}
}
