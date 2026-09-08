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
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/accountledger"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/bitcoinnode"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/ledgerlifecycle"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/managedoperation"
	manageroptions "github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/manager"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/network"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/production"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/productionscheduler"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/receipts"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksnode"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksrpc"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stackssdk"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stackssigner"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/transactions"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/workers"
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

	if options.Component == "stacks-contracts" || options.Component == "stacks-stacking" || options.Component == "stacks-receipts" {
		rpc := stacksrpc.NewClient()
		ledger := &accountledger.Ledger{Client: manager.GetClient(), Reader: manager.GetAPIReader(), RPC: rpc, Admission: accountledger.Admitter{Reader: manager.GetAPIReader()}}
		operation := &managedoperation.Runtime{Binding: options.Binding(), KeyDirectory: "/etc/stacks-keys", Client: manager.GetClient(), Reader: manager.GetAPIReader(), RPC: rpc, Ledger: ledger, SDK: stackssdk.Adapter{Directory: "/opt/stacks-transactions"}}
		switch options.Component {
		case "stacks-contracts":
			must((&managedoperation.ContractReconciler{Runtime: operation}).SetupWithManager(manager))
		case "stacks-stacking":
			must((&managedoperation.StackingReconciler{Runtime: operation}).SetupWithManager(manager))
		case "stacks-receipts":
			must(manager.Add(&receipts.Server{Ledger: ledger, RPC: rpc, Namespace: options.Namespace, NetworkUID: options.NetworkUID}))
		}
	} else if options.Component == "stacks-transactions" {
		data, err := os.ReadFile(options.TransactionAccountFile)
		must(err)
		var profile transactions.AccountProfile
		must(json.Unmarshal(data, &profile))
		must((&transactions.Reconciler{Binding: options.Binding(), Client: manager.GetClient(), APIReader: manager.GetAPIReader(), Profile: profile, Signer: transactions.LocalSigner{AccountFile: options.TransactionAccountFile, Script: "/opt/stacks-transactions/sign.mjs"}, RPC: transactions.NewNodeRPC()}).SetupWithManager(manager))
	} else if options.Component == "bitcoin-production" {
		data, err := os.ReadFile(options.ProductionCredentialsFile)
		must(err)
		var credentials production.Credentials
		must(json.Unmarshal(data, &credentials))
		if credentials.Username == "" || credentials.Password == "" {
			must(fmt.Errorf("producer username and password are required"))
		}
		must((&production.Reconciler{Binding: options.Binding(), Client: manager.GetClient(), APIReader: manager.GetAPIReader(), RPC: production.NewBitcoinRPC(credentials), ConfigDigest: credentials.ConfigDigest, ActionsEnabled: options.GenerationEnabled, ReorganizationEnabled: options.ReorganizationEnabled}).SetupWithManager(manager, options.Concurrency))
	} else {
		if options.WorkerImage == "" && options.ProductionEnabled || options.SDKWorkerImage == "" && (options.TransactionsEnabled || options.OperationEnabled) {
			must(fmt.Errorf("enabled capabilities require worker image defaults"))
		}
		if options.ProductionEnabled {
			must((&productionscheduler.Reconciler{Client: manager.GetClient(), APIReader: manager.GetAPIReader()}).SetupWithManager(manager))
		}
		// Account disposal stays available independently of execution Pod readiness.
		must((&accountledger.Reconciler{Client: manager.GetClient(), Reader: manager.GetAPIReader()}).SetupWithManager(manager))
		must(ledgerlifecycle.SetupWithManager(manager))
		components := []string{}
		if options.ProductionEnabled {
			components = append(components, "bitcoin-production")
		}
		if options.TransactionsEnabled {
			components = append(components, "stacks-transactions")
		}
		if options.OperationEnabled {
			components = append(components, "stacks-contracts", "stacks-stacking", "stacks-receipts")
		}
		for _, component := range components {
			must((&workers.Reconciler{Client: manager.GetClient(), Reader: manager.GetAPIReader(), Scheme: scheme, Component: component, Settings: options.Workers()}).SetupWithManager(manager))
		}
		must((&network.Reconciler{Client: manager.GetClient(), APIReader: manager.GetAPIReader(), Scheme: manager.GetScheme(), ProductionEnabled: options.ProductionEnabled, TransactionsEnabled: options.TransactionsEnabled, OperationEnabled: options.OperationEnabled}).SetupWithManager(manager, options.Concurrency))
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
