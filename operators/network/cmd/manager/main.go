// Command manager runs the standalone Stacks network topology controllers.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	bitcoinv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	stacksv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/bitcoinnode"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/generation"
	manageroptions "github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/manager"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/network"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/production"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/reorganization"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksnode"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stackssigner"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/transactions"
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
	must(bitcoinv1alpha1.AddToScheme(scheme))
	must(actionv1.AddToScheme(scheme))
	must(stacksv1alpha1.AddToScheme(scheme))
	manager, err := options.New(scheme)
	must(err)

	if options.Component == "stacks-transactions" {
		data, err := os.ReadFile(options.TransactionAccountFile)
		must(err)
		var profile transactions.AccountProfile
		must(json.Unmarshal(data, &profile))
		must((&transactions.Reconciler{Client: manager.GetClient(), APIReader: manager.GetAPIReader(), Profile: profile, Signer: transactions.LocalSigner{AccountFile: options.TransactionAccountFile, Script: "/opt/stacks-transactions/sign.mjs"}, RPC: transactions.NewNodeRPC()}).SetupWithManager(manager))
	} else if options.Component == "bitcoin-production" {
		data, err := os.ReadFile(options.ProductionCredentialsFile)
		must(err)
		var credentials production.Credentials
		must(json.Unmarshal(data, &credentials))
		if credentials.Username == "" || credentials.Password == "" {
			must(fmt.Errorf("producer username and password are required"))
		}
		must((&production.Reconciler{Client: manager.GetClient(), APIReader: manager.GetAPIReader(), RPC: production.NewBitcoinRPC(credentials), ConfigDigest: credentials.ConfigDigest, ActionsEnabled: options.GenerationEnabled, ReorganizationEnabled: options.ReorganizationEnabled}).SetupWithManager(manager, options.Concurrency))
		if options.ReorganizationEnabled {
			must((&reorganization.Reconciler{Client: manager.GetClient(), APIReader: manager.GetAPIReader()}).SetupWithManager(manager))
		}
		if options.GenerationEnabled {
			must((&generation.Reconciler{Client: manager.GetClient(), APIReader: manager.GetAPIReader()}).SetupWithManager(manager))
		}
	} else {
		must((&network.Reconciler{Client: manager.GetClient(), APIReader: manager.GetAPIReader(), Scheme: manager.GetScheme(), ProductionEnabled: options.ProductionEnabled, TransactionsEnabled: options.TransactionsEnabled}).SetupWithManager(manager, options.Concurrency))
		must((&bitcoinnode.Reconciler{Client: manager.GetClient(), APIReader: manager.GetAPIReader(), Scheme: manager.GetScheme()}).SetupWithManager(manager, options.Concurrency))
		must((&stacksnode.Reconciler{Client: manager.GetClient(), APIReader: manager.GetAPIReader(), Scheme: manager.GetScheme()}).SetupWithManager(manager, options.Concurrency))
		must((&stackssigner.Reconciler{Client: manager.GetClient(), APIReader: manager.GetAPIReader(), Scheme: manager.GetScheme()}).SetupWithManager(manager, options.Concurrency))
	}
	must(manager.Start(ctrl.SetupSignalHandler()))
}

func must(err error) {
	if err != nil {
		ctrl.Log.Error(err, "fatal error")
		os.Exit(1)
	}
}
