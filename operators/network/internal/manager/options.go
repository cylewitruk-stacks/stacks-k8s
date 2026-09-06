// Package manager owns controller-runtime process configuration.
package manager

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// Options contains manager runtime settings.
type Options struct {
	// Component selects topology or Bitcoin production controllers with separate credentials and RBAC.
	Component string
	// ProductionCredentialsFile names the mounted static producer credential document.
	ProductionCredentialsFile string
	// ProductionEnabled describes deployment configuration to the topology controller.
	ProductionEnabled bool
	// TransactionsEnabled describes the separately deployed transfer worker.
	TransactionsEnabled bool
	// TransactionAccountFile names its mounted administrator-selected account.
	TransactionAccountFile string
	MetricsAddress         string
	ProbeAddress           string
	Namespace              string
	Concurrency            int
	LeaderElection         bool
}

// Bind registers manager flags.
func (o *Options) Bind(flags *flag.FlagSet) {
	flags.StringVar(&o.Component, "component", "topology", "Controller component: topology or bitcoin-production.")
	flags.BoolVar(&o.TransactionsEnabled, "stacks-transactions-enabled", false, "Report that the separate STX transfer worker is enabled.")
	flags.StringVar(&o.TransactionAccountFile, "transaction-account-file", "/etc/stacks-transactions/account.json", "Mounted transfer account profile.")
	flags.BoolVar(&o.ProductionEnabled, "bitcoin-production-enabled", false, "Report that the separately deployed Bitcoin producer is enabled.")
	flags.StringVar(&o.ProductionCredentialsFile, "production-credentials-file", "/etc/bitcoin-production/credentials.json", "Mounted producer credential document.")
	flags.StringVar(&o.MetricsAddress, "metrics-bind-address", ":8080", "Prometheus metrics address.")
	flags.StringVar(&o.ProbeAddress, "health-probe-bind-address", ":8081", "Health probe address.")
	flags.StringVar(&o.Namespace, "watch-namespace", os.Getenv("WATCH_NAMESPACE"), "Namespace to watch; defaults to the ServiceAccount namespace.")
	flags.IntVar(&o.Concurrency, "max-concurrent-reconciles", 2, "Maximum reconciles per controller.")
	flags.BoolVar(&o.LeaderElection, "leader-elect", true, "Enable leader election for upgrade-safe single-writer operation.")
}

// New constructs the namespaced controller manager.
func (o Options) New(scheme *runtime.Scheme) (ctrl.Manager, error) {
	if o.Component != "" && o.Component != "topology" && o.Component != "bitcoin-production" && o.Component != "stacks-transactions" {
		return nil, fmt.Errorf("unsupported controller component %q", o.Component)
	}
	if o.Concurrency < 1 {
		return nil, fmt.Errorf("max-concurrent-reconciles must be positive")
	}
	namespace := o.Namespace
	if namespace == "" {
		value, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
		if err != nil {
			return nil, fmt.Errorf("determine watch namespace: %w", err)
		}
		namespace = strings.TrimSpace(string(value))
	}
	config, err := ctrl.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("load Kubernetes configuration: %w", err)
	}
	leaderID := "stacks-network-operator.network.stacks.org"
	if o.Component == "bitcoin-production" {
		leaderID = "bitcoin-production.bitcoin.stacks.org"
	}
	if o.Component == "stacks-transactions" {
		leaderID = "transaction-production.stacks.stacks.org"
	}
	shutdown := 30 * time.Second
	manager, err := ctrl.NewManager(config, ctrl.Options{Scheme: scheme, Metrics: metricsserver.Options{BindAddress: o.MetricsAddress},
		GracefulShutdownTimeout: &shutdown,
		HealthProbeBindAddress:  o.ProbeAddress, LeaderElection: o.LeaderElection, LeaderElectionID: leaderID,
		Cache: cache.Options{DefaultNamespaces: map[string]cache.Config{namespace: {}}}})
	if err != nil {
		return nil, fmt.Errorf("create manager: %w", err)
	}
	if err := manager.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return nil, err
	}
	if err := manager.AddReadyzCheck("readyz", func(request *http.Request) error {
		ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		defer cancel()
		return manager.GetAPIReader().List(ctx, &corev1.ConfigMapList{}, client.InNamespace(namespace), client.Limit(1))
	}); err != nil {
		return nil, err
	}
	return manager, nil
}
