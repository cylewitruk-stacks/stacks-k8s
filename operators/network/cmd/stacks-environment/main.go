// Command stacks-environment provisions a fresh managed regtest Stacks network.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/environment"
	"sigs.k8s.io/yaml"
)

func main() {
	var options environment.StacksOptions
	flag.StringVar(&options.Namespace, "namespace", "", "Fresh namespace (required).")
	flag.StringVar(&options.Name, "name", "stacks", "Network name.")
	flag.StringVar(&options.Image, "stacks-image", "", "Explicit Stacks node/signer image (required).")
	flag.StringVar(&options.SDKDirectory, "sdk-directory", "transactions", "Directory containing the offline SDK adapters.")
	flag.StringVar(&options.SBTCContracts, "sbtc-contracts", "", "Pinned checkout contracts/contracts directory (required).")
	interval := flag.Int("interval-seconds", 10, "Baseline transfer interval.")
	profile := flag.String("genesis-profile", "", "Optional local StacksGenesisProfile YAML/JSON, or an exported CR.")
	keys := flag.String("account-keys", "", "Private JSON map of stacker/transfer/manager-admin/sbtc-deployer seeds for predeclared profile accounts.")
	flag.Parse()
	if *keys != "" {
		data, err := os.ReadFile(*keys)
		if err != nil {
			fatal(err)
		}
		if json.Unmarshal(data, &options.AccountKeys) != nil {
			fatal(fmt.Errorf("invalid account-keys document"))
		}
	}
	if *interval < 1 || *interval > 86400 {
		fatal(fmt.Errorf("interval must be 1..86400"))
	}
	options.Interval = int32(*interval)
	if *profile != "" {
		data, err := os.ReadFile(*profile)
		if err != nil {
			fatal(err)
		}
		var recipe network.StacksGenesisProfile
		if err = yaml.UnmarshalStrict(data, &recipe); err != nil {
			fatal(err)
		}
		if recipe.APIVersion != network.GroupVersion.String() || recipe.Kind != "StacksGenesisProfile" {
			fatal(fmt.Errorf("expected network.stacks.org/v1alpha1 StacksGenesisProfile"))
		}
		options.Genesis = &recipe.Spec
	}
	doc, err := environment.Stacks(options)
	if err != nil {
		fatal(err)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(doc); err != nil {
		fatal(err)
	}
}

// fatal reports configuration failures without printing private manifest contents.
func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
