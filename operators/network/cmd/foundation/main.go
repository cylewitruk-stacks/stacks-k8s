// Command foundation runs the composable API foundation or one scoped identity resolver.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func main() {
	mode := flag.String("mode", "controller", "controller or resolve-key")
	input := flag.String("input", "", "public resolver binding JSON")
	image := flag.String("resolver-image", "", "image used for scoped identity resolver Jobs")
	health := flag.String("health-probe-bind-address", ":8081", "health/readiness listen address")
	leader := flag.Bool("leader-elect", true, "use leader election for controller replicas")
	flag.Parse()
	ctrl.SetLogger(zap.New())
	if err := run(ctrl.SetupSignalHandler(), *mode, *input, *image, *health, *leader); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(ctx context.Context, mode, input, image, health string, leader bool) error {
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{clientgoscheme.AddToScheme, api.AddToScheme, bitcoin.AddToScheme, stacks.AddToScheme} {
		if err := add(scheme); err != nil {
			return err
		}
	}
	config, err := ctrl.GetConfig()
	if err != nil {
		return err
	}
	if mode == "resolve-key" {
		var binding foundation.KeyJobInput
		if err := json.Unmarshal([]byte(input), &binding); err != nil {
			return fmt.Errorf("invalid public resolver binding")
		}
		c, err := client.New(config, client.Options{Scheme: scheme})
		if err != nil {
			return err
		}
		return foundation.RunKeyJob(ctx, c, binding)
	}
	if mode != "controller" || image == "" {
		return fmt.Errorf("controller requires --resolver-image")
	}
	manager, err := ctrl.NewManager(config, ctrl.Options{Cache: foundation.CacheOptions(), Scheme: scheme, LeaderElection: leader, LeaderElectionID: "stacks-network-foundation.network.stacks.org", HealthProbeBindAddress: health, Metrics: metricsserver.Options{BindAddress: "0"}, Client: client.Options{Cache: &client.CacheOptions{DisableFor: []client.Object{&corev1.Secret{}, &corev1.ServiceAccount{}, &rbacv1.Role{}, &rbacv1.RoleBinding{}}}}})
	if err != nil {
		return err
	}
	if err := foundation.Register(manager, image); err != nil {
		return err
	}
	if err := manager.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return err
	}
	if err := manager.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return err
	}
	return manager.Start(ctx)
}
