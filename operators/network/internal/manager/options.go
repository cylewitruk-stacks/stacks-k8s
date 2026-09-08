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

	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/execution"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/workers"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// Options contains manager runtime settings.
type Options struct {
	// LeaderNamespace locates the installation or worker election Lease.
	LeaderNamespace string
	// CapabilityName and CapabilityUID bind execution to one resource incarnation.
	CapabilityName, CapabilityUID string
	// NetworkUID isolates a receipt sink to its owning network incarnation.
	NetworkUID string
	// WorkerImage and SDKWorkerImage are installation defaults, without environment credentials.
	WorkerImage, SDKWorkerImage, WorkerPullPolicy string
	// WorkerPullSecrets is a comma-separated list of namespace-local image pull Secret names.
	WorkerPullSecrets string
	// ReorganizationEnabled enables compensated local suffix replacement.
	ReorganizationEnabled bool
	// GenerationEnabled enables finite actions on the shared Bitcoin executor.
	GenerationEnabled bool
	// Component selects topology or Bitcoin production controllers with separate credentials and RBAC.
	Component string
	// ProductionCredentialsFile names the mounted static producer credential document.
	ProductionCredentialsFile string
	// ProductionEnabled describes deployment configuration to the topology controller.
	ProductionEnabled bool
	// TransactionsEnabled describes the separately deployed transfer worker.
	TransactionsEnabled bool
	// OperationEnabled reports deployment of the managed protocol worker.
	OperationEnabled bool
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
	flags.BoolVar(&o.ReorganizationEnabled, "bitcoin-reorganization-enabled", false, "Enable compensated regtest suffix replacement.")
	flags.BoolVar(&o.GenerationEnabled, "bitcoin-generation-enabled", false, "Enable finite generation on the shared Bitcoin executor.")
	flags.StringVar(&o.Component, "component", "topology", "Controller component: topology, bitcoin-production, stacks-transactions, stacks-contracts, stacks-stacking or stacks-receipts.")
	flags.BoolVar(&o.TransactionsEnabled, "stacks-transactions-enabled", false, "Enable transfer worker provisioning; existing execution follows network policy.")
	flags.BoolVar(&o.OperationEnabled, "stacks-operation-enabled", false, "Enable managed worker provisioning; existing execution follows network policy.")
	flags.StringVar(&o.TransactionAccountFile, "transaction-account-file", "/etc/stacks-transactions/account.json", "Mounted transfer account profile.")
	flags.BoolVar(&o.ProductionEnabled, "bitcoin-production-enabled", false, "Enable Bitcoin baseline scheduling and worker provisioning; does not revoke existing work.")
	flags.StringVar(&o.ProductionCredentialsFile, "production-credentials-file", "/etc/bitcoin-production/credentials.json", "Mounted producer credential document.")
	flags.StringVar(&o.MetricsAddress, "metrics-bind-address", ":8080", "Prometheus metrics address.")
	flags.StringVar(&o.ProbeAddress, "health-probe-bind-address", ":8081", "Health probe address.")
	flags.StringVar(&o.Namespace, "watch-namespace", os.Getenv("WATCH_NAMESPACE"), "Watch one namespace; empty watches all namespaces for the operator.")
	flags.StringVar(&o.LeaderNamespace, "leader-namespace", os.Getenv("OPERATOR_NAMESPACE"), "Namespace of the leader-election Lease.")
	flags.StringVar(&o.CapabilityName, "capability-name", "", "Assigned execution resource name.")
	flags.StringVar(&o.CapabilityUID, "capability-uid", "", "Assigned execution resource UID.")
	flags.StringVar(&o.NetworkUID, "network-uid", "", "Owning network UID for receipt accounting.")
	flags.StringVar(&o.WorkerImage, "worker-image", "", "Image used for controller-provisioned Go workers.")
	flags.StringVar(&o.SDKWorkerImage, "sdk-worker-image", "", "Image used for controller-provisioned offline SDK workers.")
	flags.StringVar(&o.WorkerPullPolicy, "worker-pull-policy", "IfNotPresent", "Worker image pull policy.")
	flags.StringVar(&o.WorkerPullSecrets, "worker-pull-secrets", "", "Comma-separated image pull Secret names; provision in every network namespace.")
	flags.IntVar(&o.Concurrency, "max-concurrent-reconciles", 2, "Maximum reconciles per controller.")
	flags.BoolVar(&o.LeaderElection, "leader-elect", true, "Enable leader election for upgrade-safe single-writer operation.")
}

// New constructs the namespaced controller manager.
func (o *Options) New(scheme *runtime.Scheme) (ctrl.Manager, error) {
	if o.Component != "" && o.Component != "topology" && o.Component != "bitcoin-production" && o.Component != "stacks-transactions" && o.Component != "stacks-contracts" && o.Component != "stacks-stacking" && o.Component != "stacks-receipts" {
		return nil, fmt.Errorf("unsupported controller component %q", o.Component)
	}
	if o.Concurrency < 1 {
		return nil, fmt.Errorf("max-concurrent-reconciles must be positive")
	}
	namespace := o.Namespace
	if o.LeaderNamespace == "" {
		value, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
		if err != nil {
			return nil, fmt.Errorf("determine leader namespace: %w", err)
		}
		o.LeaderNamespace = strings.TrimSpace(string(value))
	}
	if o.Component != "" && o.Component != "topology" && (namespace == "" || o.CapabilityName == "" || o.CapabilityUID == "" || o.NetworkUID == "") {
		return nil, fmt.Errorf("execution workers require namespace, capability name/UID and network UID")
	}
	if o.WorkerPullPolicy != "IfNotPresent" && o.WorkerPullPolicy != "Always" && o.WorkerPullPolicy != "Never" {
		return nil, fmt.Errorf("unsupported worker image pull policy")
	}
	for _, name := range strings.Split(o.WorkerPullSecrets, ",") {
		if name != "" && len(validation.IsDNS1123Subdomain(name)) != 0 {
			return nil, fmt.Errorf("invalid worker image pull Secret name")
		}
	}
	config, err := ctrl.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("load Kubernetes configuration: %w", err)
	}
	leaderID := "stacks-network-operator.network.stacks.org"
	if o.CapabilityUID != "" {
		leaderID = "execution-" + o.CapabilityUID
	}
	cacheOptions := cache.Options{}
	if namespace != "" {
		cacheOptions.DefaultNamespaces = map[string]cache.Config{namespace: {}}
	}
	shutdown := 30 * time.Second
	manager, err := ctrl.NewManager(config, ctrl.Options{Scheme: scheme, Metrics: metricsserver.Options{BindAddress: o.MetricsAddress},
		GracefulShutdownTimeout: &shutdown,
		HealthProbeBindAddress:  o.ProbeAddress, LeaderElection: o.LeaderElection, LeaderElectionID: leaderID, LeaderElectionNamespace: o.LeaderNamespace,
		Cache: cacheOptions})
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

// Binding returns the execution identity supplied by the capability controller.
func (o Options) Binding() execution.Binding {
	return execution.Binding{Namespace: o.Namespace, Name: o.CapabilityName, UID: types.UID(o.CapabilityUID)}
}

// Workers returns public workload installation defaults.
func (o Options) Workers() workers.Settings {
	refs := []corev1.LocalObjectReference{}
	for _, name := range strings.Split(o.WorkerPullSecrets, ",") {
		if name != "" {
			refs = append(refs, corev1.LocalObjectReference{Name: name})
		}
	}
	return workers.Settings{PullSecrets: refs, Image: o.WorkerImage, SDKImage: o.SDKWorkerImage, PullPolicy: corev1.PullPolicy(o.WorkerPullPolicy), Generation: o.GenerationEnabled, Reorganization: o.ReorganizationEnabled}
}
