// Command foundation runs the composable API foundation or one scoped identity resolver.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	actions "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha2"
	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/bitcoincontrol"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/faucetrequest"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/networkruntime"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantworkload"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
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

// actionOptions opts individual Bitcoin mechanisms into their independent action APIs.
type actionOptions struct {
	generation, reorganization bool
}

func main() {
	var actions actionOptions
	flag.BoolVar(
		&actions.generation,
		"bitcoin-generation-enabled",
		false,
		"enable bounded Bitcoin block-generation actions",
	)
	flag.BoolVar(
		&actions.reorganization,
		"bitcoin-reorganization-enabled",
		false,
		"enable bounded Bitcoin reorganization actions",
	)
	mode := flag.String(
		"mode",
		foundation.ModeController,
		"controller, resolve-key, resolve-bitcoin-config, resolve-stacks-config, "+
			"validate-stacks-config or bitcoin-control",
	)
	input := flag.String("input", "", "public resolver binding JSON")
	inputFile := flag.String("input-file", "", "bounded public configuration resolver request file")
	image := flag.String("resolver-image", "", "image used for scoped identity resolver Jobs")
	health := flag.String("health-probe-bind-address", ":8081", "health/readiness listen address")
	leader := flag.Bool("leader-elect", true, "use leader election for controller replicas")
	flag.Parse()
	ctrl.SetLogger(zap.New())
	request, err := publicInput(*mode, *input, *inputFile)
	if err == nil {
		err = run(ctrl.SetupSignalHandler(), *mode, request, *image, *health, *leader, actions)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, mode, input, image, health string, leader bool, enabled actionOptions) error {
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		clientgoscheme.AddToScheme,
		api.AddToScheme,
		bitcoin.AddToScheme,
		stacks.AddToScheme,
		actions.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			return err
		}
	}
	config, err := ctrl.GetConfig()
	if err != nil {
		return err
	}
	if mode == foundation.ModeResolveKey {
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
	if mode == participantworkload.ModeResolveBitcoinConfig {
		var binding participantworkload.BitcoinConfigInput
		if err := json.Unmarshal([]byte(input), &binding); err != nil {
			return fmt.Errorf("invalid public configuration binding")
		}
		c, err := client.New(config, client.Options{Scheme: scheme})
		if err != nil {
			return err
		}
		return participantworkload.RunBitcoinConfigResolver(ctx, c, binding)
	}
	if mode == participantworkload.ModeResolveStacksConfig || mode == participantworkload.ModeValidateStacksConfig {
		var binding participantworkload.StacksConfigInput
		if err := json.Unmarshal([]byte(input), &binding); err != nil {
			return fmt.Errorf("invalid public Stacks configuration binding")
		}
		c, err := client.New(config, client.Options{Scheme: scheme})
		if err != nil {
			return err
		}
		if mode == participantworkload.ModeValidateStacksConfig {
			return participantworkload.RunStacksCandidateConfigResolver(ctx, c, binding)
		}
		return participantworkload.RunStacksConfigResolver(ctx, c, binding)
	}
	if mode == bitcoincontrol.ModeBitcoinControl {
		var binding bitcoincontrol.WorkerInput
		if err := json.Unmarshal([]byte(input), &binding); err != nil {
			return fmt.Errorf("invalid public control-worker binding")
		}
		c, err := client.New(config, client.Options{Scheme: scheme})
		if err != nil {
			return err
		}
		return bitcoincontrol.RunWorker(ctx, c, c, binding)
	}
	if mode != foundation.ModeController || image == "" {
		return fmt.Errorf("controller requires --resolver-image")
	}
	manager, err := ctrl.NewManager(
		config,
		ctrl.Options{
			Cache:                  foundation.CacheOptions(),
			Scheme:                 scheme,
			LeaderElection:         leader,
			LeaderElectionID:       "stacks-network-operator.network.stacks.org",
			HealthProbeBindAddress: health,
			Metrics:                metricsserver.Options{BindAddress: "0"},
			Client: client.Options{
				Cache: &client.CacheOptions{
					DisableFor: []client.Object{
						&corev1.Secret{},
						&corev1.ServiceAccount{},
						&rbacv1.Role{},
						&rbacv1.RoleBinding{},
					},
				},
			},
		},
	)
	if err != nil {
		return err
	}
	for name, object := range map[string]client.Object{
		"genesis":                &api.StacksGenesis{},
		"bitcoin-execution":      &bitcoin.BitcoinExecution{},
		"bitcoin-initialization": &bitcoin.BitcoinInitialization{},
	} {
		artifacts := &foundation.ArtifactReconciler{
			Client: manager.GetClient(),
			Reader: manager.GetAPIReader(),
			Object: object,
			Name:   "artifact-" + name,
		}
		if err := artifacts.SetupWithManager(manager); err != nil {
			return err
		}
	}
	actorKinds := []api.ParticipantKind{
		api.ParticipantBitcoinNode,
		api.ParticipantStacksNode,
		api.ParticipantStacksSigner,
	}
	configuration := &participantworkload.Reconciler{
		Client:        manager.GetClient(),
		Reader:        manager.GetAPIReader(),
		ResolverImage: image,
	}
	projection := &networkruntime.Reconciler{
		Client:     manager.GetClient(),
		Reader:     manager.GetAPIReader(),
		Scheme:     scheme,
		ActorKinds: actorKinds,
	}
	kinds := append(
		append([]api.ParticipantKind(nil), actorKinds...),
		api.ParticipantBitcoinBlockProduction,
		api.ParticipantStacksTransactionProduction,
		api.ParticipantStacksStacker,
		api.ParticipantStacksContractSet,
		api.ParticipantStacksFaucet,
	)
	//nolint:contextcheck // Manager setup registers lifetime indexes before the manager starts serving requests.
	if err := foundation.Register(
		manager,
		image,
		foundation.RuntimeOptions{Configurations: configuration, Kinds: kinds, Runtime: projection},
	); err != nil {
		return err
	}
	for _, kind := range actorKinds {
		actor := &participantworkload.Reconciler{
			Client:        manager.GetClient(),
			Reader:        manager.GetAPIReader(),
			ResolverImage: image,
			Kind:          kind,
		}
		if kind == api.ParticipantBitcoinNode {
			actor.BeforeStop = func(ctx context.Context, p *api.StacksNetworkParticipant) (bool, error) {
				return bitcoincontrol.CheckDrained(ctx, manager.GetAPIReader(), p)
			}
		} else {
			actor.BeforeStop = func(ctx context.Context, p *api.StacksNetworkParticipant) (bool, error) {
				return stacksworker.CheckActorStop(ctx, manager.GetAPIReader(), p)
			}
		}
		if err := actor.SetupWithManager(manager); err != nil {
			return err
		}
	}
	control := &bitcoincontrol.WorkloadReconciler{
		Client:                manager.GetClient(),
		Reader:                manager.GetAPIReader(),
		Image:                 image,
		ActionsEnabled:        enabled.generation,
		ReorganizationEnabled: enabled.reorganization,
	}
	if err := control.SetupWithManager(manager); err != nil {
		return err
	}
	overrides := &bitcoincontrol.OverrideReconciler{Client: manager.GetClient(), Reader: manager.GetAPIReader()}
	if err := overrides.SetupWithManager(manager); err != nil {
		return err
	}
	scheduler := &bitcoincontrol.Scheduler{Client: manager.GetClient(), Reader: manager.GetAPIReader()}
	if err := scheduler.SetupWithManager(manager); err != nil {
		return err
	}
	production := &bitcoincontrol.ProductionStatusReconciler{
		Client: manager.GetClient(),
		Reader: manager.GetAPIReader(),
	}
	if err := production.SetupWithManager(manager); err != nil {
		return err
	}
	requests := &faucetrequest.Reconciler{Client: manager.GetClient(), Reader: manager.GetAPIReader()}
	//nolint:contextcheck // Manager setup registers lifetime indexes before the manager starts serving requests.
	if err := requests.SetupWithManager(manager); err != nil {
		return err
	}
	profiles := stacksworker.Profiles{Client: manager.GetClient(), Reader: manager.GetAPIReader(), Image: image}
	for _, kind := range []api.ParticipantKind{
		api.ParticipantStacksTransactionProduction,
		api.ParticipantStacksStacker,
		api.ParticipantStacksContractSet,
		api.ParticipantStacksFaucet,
	} {
		worker := &stacksworker.Reconciler{
			Client:         manager.GetClient(),
			Reader:         manager.GetAPIReader(),
			Kind:           kind,
			ResolveProfile: profiles.Resolve,
			ResolveReads:   profiles.Reads,
		}
		if err := worker.SetupWithManager(manager); err != nil {
			return err
		}
	}
	if err := manager.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return err
	}
	if err := manager.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return err
	}
	return manager.Start(ctx)
}
