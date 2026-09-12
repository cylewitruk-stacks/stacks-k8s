// Package manager configures the action controller-runtime manager.
package manager

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	actionv2 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha2"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// Options contains manager runtime settings.
type Options struct {
	// APIVersion selects the independent served lifecycle contract.
	APIVersion string
	// GenerationEnabled enables finite generation lifecycle projection.
	GenerationEnabled bool
	// ReorganizationEnabled enables the existing reorganization lifecycle.
	ReorganizationEnabled bool
	// MetricsAddress binds the Prometheus endpoint.
	MetricsAddress string
	// ProbeAddress binds health and readiness endpoints.
	ProbeAddress string
	// Namespace limits watched objects and leader election.
	Namespace string
	// Concurrency bounds parallel reconciles per kind.
	Concurrency int
	// LeaderElection selects one active lifecycle writer.
	LeaderElection bool
}

// Bind registers manager flags.
func (o *Options) Bind(flags *flag.FlagSet) {
	flags.StringVar(&o.APIVersion, "api-version", "v1alpha2", "Served action API version.")
	flags.BoolVar(&o.GenerationEnabled, "bitcoin-generation-enabled", true, "Enable finite generation lifecycle.")
	flags.BoolVar(&o.ReorganizationEnabled, "bitcoin-reorganization-enabled", false, "Enable reorganization lifecycle.")
	flags.StringVar(&o.MetricsAddress, "metrics-bind-address", ":8080", "Prometheus metrics address.")
	flags.StringVar(&o.ProbeAddress, "health-probe-bind-address", ":8081", "Health probe address.")
	flags.StringVar(&o.Namespace, "watch-namespace", os.Getenv("WATCH_NAMESPACE"), "Namespace to watch; defaults to the ServiceAccount namespace.")
	flags.IntVar(&o.Concurrency, "max-concurrent-reconciles", 2, "Maximum concurrent reconciles per action kind.")
	flags.BoolVar(&o.LeaderElection, "leader-elect", true, "Enable leader election for upgrade-safe single-writer operation.")
}

// New constructs a namespaced controller manager.
func (o Options) New(configuration *rest.Config, scheme *runtime.Scheme) (ctrl.Manager, error) {
	if o.APIVersion != "" && o.APIVersion != "v1alpha2" {
		return nil, fmt.Errorf("unsupported action API version")
	}
	if !o.GenerationEnabled && !o.ReorganizationEnabled {
		return nil, fmt.Errorf("at least one action controller must be enabled")
	}
	if o.Concurrency < 1 || o.Concurrency > 32 {
		return nil, fmt.Errorf("max-concurrent-reconciles must be between 1 and 32")
	}
	namespace := o.Namespace
	if namespace == "" {
		value, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
		if err != nil {
			return nil, fmt.Errorf("determine watch namespace: %w", err)
		}
		namespace = strings.TrimSpace(string(value))
	}
	if namespace == "" {
		return nil, fmt.Errorf("watch namespace must not be empty")
	}
	manager, err := ctrl.NewManager(configuration, ctrl.Options{
		Scheme: scheme, Metrics: metricsserver.Options{BindAddress: o.MetricsAddress}, HealthProbeBindAddress: o.ProbeAddress,
		LeaderElectionNamespace: namespace, LeaderElection: o.LeaderElection, LeaderElectionID: "stacks-action-operator.actions.stacks.org",
		Cache: cache.Options{DefaultNamespaces: map[string]cache.Config{namespace: {}}},
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
		if o.GenerationEnabled {
			return manager.GetAPIReader().List(ctx, &actionv2.BitcoinBlockGenerationList{}, client.InNamespace(namespace), client.Limit(1))
		}
		return manager.GetAPIReader().List(ctx, &actionv2.BitcoinReorganizationList{}, client.InNamespace(namespace), client.Limit(1))
	}); err != nil {
		return nil, err
	}
	return manager, nil
}
