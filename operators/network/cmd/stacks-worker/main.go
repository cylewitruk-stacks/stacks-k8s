// Command stacks-worker runs one scoped, process-local Stacks protocol role.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/protocolcontracts"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksoperation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// arguments are public immutable process bindings supplied by the workload controller.
type arguments struct{ role, namespace, participant, participantUID, networkUID, profile string }

func main() {
	var args arguments
	var verifyContracts string
	flag.StringVar(
		&verifyContracts,
		"verify-contracts",
		"",
		"verify a pinned contract bundle without accessing Kubernetes",
	)
	flag.StringVar(&args.role, "role", "", "declared participant role")
	flag.StringVar(&args.namespace, "namespace", "", "network namespace")
	flag.StringVar(&args.participant, "participant", "", "generated participant name")
	flag.StringVar(&args.participantUID, "participant-uid", "", "exact participant UID")
	flag.StringVar(&args.networkUID, "network-uid", "", "exact network UID")
	flag.StringVar(&args.profile, "profile", "", "public immutable worker profile JSON")
	flag.Parse()
	if verifyContracts != "" {
		if _, err := protocolcontracts.Load(verifyContracts); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	ctrl.SetLogger(zap.New())
	if err := run(ctrl.SetupSignalHandler(), args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run creates only scoped Kubernetes clients; signing keys remain in this worker process.
func run(ctx context.Context, args arguments) error {
	if args.role != string(api.ParticipantStacksTransactionProduction) &&
		args.role != string(api.ParticipantStacksStacker) &&
		args.role != string(api.ParticipantStacksContractSet) &&
		args.role != string(api.ParticipantStacksFaucet) {
		return fmt.Errorf("unsupported worker role")
	}
	if args.namespace == "" || args.participant == "" || args.participantUID == "" || args.networkUID == "" ||
		os.Getenv("POD_UID") == "" ||
		os.Getenv("POD_NAME") == "" {
		return fmt.Errorf("incomplete worker process identity")
	}
	var profile stacksworker.Profile
	if len(args.profile) > 64<<10 || json.Unmarshal([]byte(args.profile), &profile) != nil {
		return fmt.Errorf("invalid public worker profile")
	}
	normalized, err := profile.Normalize()
	if err != nil {
		return err
	}
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		clientgoscheme.AddToScheme,
		api.AddToScheme,
		stacks.AddToScheme,
		bitcoin.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			return err
		}
	}
	config, err := ctrl.GetConfig()
	if err != nil {
		return err
	}
	// Direct reads and status writes must not strand the sole execution loop.
	// Reflector watches use their own unbounded client below.
	directConfig := rest.CopyConfig(config)
	directConfig.Timeout = 10 * time.Second
	c, err := client.New(directConfig, client.Options{Scheme: scheme})
	if err != nil {
		return err
	}
	d, err := dynamic.NewForConfig(config)
	if err != nil {
		return err
	}
	role, prerequisites, err := protocolRole(api.ParticipantKind(args.role), normalized, c)
	if err != nil {
		return err
	}
	if faucet, ok := role.(*stacksoperation.FaucetRole); ok {
		faucet.Namespace = args.namespace
		faucet.ParticipantUID = types.UID(args.participantUID)
		faucet.PodUID = types.UID(os.Getenv("POD_UID"))
	}
	worker := stacksworker.Runtime{
		Client:          c,
		Dynamic:         d,
		Namespace:       args.namespace,
		ParticipantName: args.participant,
		NetworkUID:      types.UID(args.networkUID),
		ParticipantUID:  types.UID(args.participantUID),
		PodName:         os.Getenv("POD_NAME"),
		PodUID:          types.UID(os.Getenv("POD_UID")),
		Profile:         normalized,
		Role:            role,
		Prerequisites:   prerequisites,
	}
	return worker.Run(ctx)
}

// mountedKey bounds private input and never includes its bytes in errors.
func mountedKey(path string) (string, error) {
	// #nosec G304 -- Explicit CLI input path is intentionally caller-selected and read with a size limit.
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("mounted signing key unavailable")
	}
	defer func() { _ = file.Close() }() // Read/cleanup completion cannot change the operation's result.
	data, err := io.ReadAll(io.LimitReader(file, 257))
	if err != nil || len(data) > 256 {
		return "", fmt.Errorf("mounted signing key is invalid")
	}
	key, err := identity.CompressedPrivateKey(strings.TrimSpace(string(data)))
	if err != nil {
		return "", fmt.Errorf("mounted signing key is invalid")
	}
	return key, nil
}

// protocolRole binds mounted private inputs to the role's public identity resolver.
func protocolRole(
	kind api.ParticipantKind,
	profile stacksworker.Profile,
	reader client.Client,
) (stacksworker.Role, func(context.Context, stacksworker.Snapshot) error, error) {
	read := func(role string) (string, error) {
		for _, key := range profile.Keys {
			if key.Role == role {
				return mountedKey("/keys/" + role + "/key")
			}
		}
		return "", fmt.Errorf("required signing key mount is missing")
	}
	senderRole := stacksworker.KeyRoleSender
	//nolint:exhaustive // Only the supported Stacks worker roles receive these inputs; other kinds stay excluded.
	switch kind {
	case api.ParticipantStacksStacker:
		senderRole = stacksworker.KeyRoleHolder
	case api.ParticipantStacksContractSet:
		senderRole = stacksworker.KeyRoleDeployer
	}
	key, err := read(senderRole)
	if err != nil {
		return nil, nil, err
	}
	public, err := identity.FromPrivate(key)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid mounted identity")
	}
	inputs := stacksoperation.PublicInputs{Reader: reader, Sender: public.Address}
	//nolint:exhaustive // Only the supported Stacks worker roles receive these inputs; other kinds stay excluded.
	switch kind {
	case api.ParticipantStacksFaucet:
		role, err := stacksoperation.NewFaucetRole(key, public.Address)
		if err != nil {
			return nil, nil, err
		}
		role.Client, role.Resolve = reader, inputs.Faucet
		// Requests carry their own fresh prerequisites; an idle faucet may activate.
		return role, func(context.Context, stacksworker.Snapshot) error { return nil }, nil
	case api.ParticipantStacksTransactionProduction:
		role, err := stacksoperation.NewTransferRole(key, public.Address)
		if err != nil {
			return nil, nil, err
		}
		role.Resolve = inputs.Transfer
		return role, func(ctx context.Context, s stacksworker.Snapshot) error {
			_, err := inputs.Transfer(ctx, s)
			return err
		}, nil
	case api.ParticipantStacksStacker:
		consensus, err := read("consensus")
		if err != nil {
			return nil, nil, err
		}
		administratorKey, err := read(stacksworker.KeyRoleAdministrator)
		if err != nil {
			return nil, nil, err
		}
		signer, err := identity.FromPrivate(consensus)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid mounted consensus identity")
		}
		administrator, err := identity.FromPrivate(administratorKey)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid mounted administrator identity")
		}
		role, err := stacksoperation.NewStackerRole(
			key,
			public.Address,
			consensus,
			signer.PublicKey,
			administratorKey,
			administrator.Address,
		)
		if err != nil {
			return nil, nil, err
		}
		role.ResolvePoX4, role.ResolvePoX5 = inputs.PoX4, inputs.PoX5
		return role, func(ctx context.Context, s stacksworker.Snapshot) error {
			_, err := inputs.PoX5(
				ctx,
				s,
			)
			return err
		}, nil
	case api.ParticipantStacksContractSet:
		role, err := stacksoperation.NewContractRole(key, public.Address, "/protocol/sbtc")
		if err != nil {
			return nil, nil, err
		}
		role.Resolve = inputs.Contracts
		return role, func(ctx context.Context, s stacksworker.Snapshot) error {
			_, err := inputs.Contracts(ctx, s)
			return err
		}, nil
	default:
		return nil, nil, fmt.Errorf("unsupported worker role")
	}
}
