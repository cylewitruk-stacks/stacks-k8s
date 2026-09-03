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
	MetricsAddress string
	ProbeAddress   string
	Namespace      string
	Concurrency    int
	LeaderElection bool
}

// Bind registers manager flags.
func (o *Options) Bind(flags *flag.FlagSet) {
	flags.StringVar(&o.MetricsAddress, "metrics-bind-address", ":8080", "Prometheus metrics address.")
	flags.StringVar(&o.ProbeAddress, "health-probe-bind-address", ":8081", "Health probe address.")
	flags.StringVar(&o.Namespace, "watch-namespace", os.Getenv("WATCH_NAMESPACE"), "Namespace to watch; defaults to the ServiceAccount namespace.")
	flags.IntVar(&o.Concurrency, "max-concurrent-reconciles", 2, "Maximum reconciles per controller.")
	flags.BoolVar(&o.LeaderElection, "leader-elect", true, "Enable leader election for upgrade-safe single-writer operation.")
}

// New constructs the namespaced controller manager.
func (o Options) New(scheme *runtime.Scheme) (ctrl.Manager, error) {
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
	manager, err := ctrl.NewManager(config, ctrl.Options{Scheme: scheme, Metrics: metricsserver.Options{BindAddress: o.MetricsAddress},
		HealthProbeBindAddress: o.ProbeAddress, LeaderElection: o.LeaderElection, LeaderElectionID: "stacks-network-operator.network.stacks.org",
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
