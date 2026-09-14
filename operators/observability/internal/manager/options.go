// Package manager configures the observation controller-runtime manager.
package manager

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/observability/internal/telemetry"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// Options contains manager runtime settings.
type Options struct {
	// WorkerImage selects recorder workloads.
	WorkerImage string
	// CollectorImage selects upstream collection workloads.
	CollectorImage string
	// LeaderNamespace holds the installation lease independently of watched namespaces.
	LeaderNamespace string
	MetricsAddress  string
	ProbeAddress    string
	Namespace       string
	Concurrency     int
	LeaderElection  bool
}

// Bind registers manager flags.
func (o *Options) Bind(flags *flag.FlagSet) {
	flags.StringVar(&o.WorkerImage, "worker-image", "stacks-observability-operator:0.1.0", "Recorder image.")
	flags.StringVar(&o.CollectorImage, "collector-image", telemetry.DefaultCollectorImage, "Pinned collector image.")
	flags.StringVar(
		&o.LeaderNamespace,
		"leader-election-namespace",
		"stacks-observation-system",
		"Installation namespace.",
	)
	flags.StringVar(&o.MetricsAddress, "metrics-bind-address", ":8080", "Prometheus metrics address.")
	flags.StringVar(&o.ProbeAddress, "health-probe-bind-address", ":8081", "Health probe address.")
	flags.StringVar(
		&o.Namespace,
		"watch-namespace",
		"",
		"Comma-separated enrolled namespaces; defaults to the installation namespace.",
	)
	flags.IntVar(&o.Concurrency, "max-concurrent-reconciles", 2, "Maximum concurrent observations.")
	flags.BoolVar(
		&o.LeaderElection,
		"leader-elect",
		true,
		"Enable leader election for upgrade-safe single-writer operation.",
	)
}

// New constructs a namespaced controller manager.
func (o Options) New(scheme *runtime.Scheme) (ctrl.Manager, error) {
	if o.Concurrency < 1 {
		return nil, fmt.Errorf("max-concurrent-reconciles must be positive")
	}
	namespaces, err := o.enrolledNamespaces()
	if err != nil {
		return nil, err
	}
	cacheOptions := cache.Options{ReaderFailOnMissingInformer: true, DefaultNamespaces: namespaces}
	configuration, err := ctrl.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("load Kubernetes configuration: %w", err)
	}
	manager, err := ctrl.NewManager(configuration, ctrl.Options{
		Scheme:                  scheme,
		Metrics:                 metricsserver.Options{BindAddress: o.MetricsAddress},
		HealthProbeBindAddress:  o.ProbeAddress,
		LeaderElection:          o.LeaderElection,
		LeaderElectionID:        "stacks-observability-operator.observation.stacks.org",
		Cache:                   cacheOptions,
		LeaderElectionNamespace: o.LeaderNamespace,
	})
	if err != nil {
		return nil, fmt.Errorf("create manager: %w", err)
	}
	if err := manager.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return nil, err
	}
	if err := manager.AddReadyzCheck("readyz", func(request *http.Request) error {
		ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		defer cancel()
		return manager.GetAPIReader().
			List(ctx, &corev1.PodList{}, client.InNamespace(o.LeaderNamespace), client.Limit(1))
	}); err != nil {
		return nil, err
	}
	return manager, nil
}

// enrolledNamespaces validates the explicit cache boundary independently of cluster access.
func (o Options) enrolledNamespaces() (map[string]cache.Config, error) {
	namespace := o.Namespace
	if namespace == "" {
		namespace = o.LeaderNamespace
	}
	result := map[string]cache.Config{}
	for _, name := range strings.Split(namespace, ",") {
		name = strings.TrimSpace(name)
		if len(validation.IsDNS1123Label(name)) != 0 {
			return nil, fmt.Errorf("invalid enrolled namespace")
		}
		if _, exists := result[name]; exists {
			return nil, fmt.Errorf("duplicate enrolled namespace")
		}
		result[name] = cache.Config{}
	}
	if len(result) > 32 {
		return nil, fmt.Errorf("at most 32 namespaces may be enrolled")
	}
	return result, nil
}
