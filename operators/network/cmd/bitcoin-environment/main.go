// Command bitcoin-environment emits a fresh disposable Bitcoin-only environment with static RPC separation.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/environment"
	"k8s.io/apimachinery/pkg/util/validation"
	"os"
	"strconv"
	"strings"
)

func main() {
	namespace := flag.String("namespace", "", "Fresh namespace dedicated to this environment (required).")
	name := flag.String("name", "bitcoin", "StacksNetwork name.")
	image := flag.String("image", environment.DefaultBitcoinImage, "Bitcoin Core image; defaults to the pinned 31.1 image index.")
	address := flag.String("address", "mipcBbFg9gMiCh81Kj8tqqdgoZub1ZJRfn", "Regtest coinbase destination; the default is a test-only address with no provided spending key.")
	reorganization := flag.Bool("reorganization", false, "Provision the optional local reorganization RPC method profile.")
	interval := flag.Int("interval-seconds", 5, "Fixed total policy opportunity interval.")
	weightsFlag := flag.String("target-weights", "1", "Comma-separated weights for 1..8 connected Bitcoin targets (bitcoin, bitcoin-2, ...).")
	flag.Parse()
	weights, err := parseWeights(*weightsFlag)
	must(err)
	if len(validation.IsDNS1123Label(*namespace)) != 0 || len(validation.IsDNS1123Label(*name)) != 0 || *interval < 1 || *interval > 86400 {
		fmt.Fprintln(os.Stderr, "namespace/name must be DNS labels and interval must be 1..86400")
		os.Exit(1)
	}
	objects, err := environment.Bitcoin(environment.BitcoinOptions{Namespace: *namespace, Name: *name, Image: *image, Address: *address, Reorganization: *reorganization, Interval: *interval, Weights: weights})
	must(err)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	must(encoder.Encode(map[string]any{"apiVersion": "v1", "kind": "List", "items": objects.Resources}))
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// parseWeights bounds the helper's target count and relative shares.
func parseWeights(value string) ([]int32, error) {
	parts := strings.Split(value, ",")
	if len(parts) < 1 || len(parts) > 8 {
		return nil, fmt.Errorf("target-weights requires 1..8 weights")
	}
	result := make([]int32, len(parts))
	for i, part := range parts {
		weight, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || weight < 1 || weight > 1000 {
			return nil, fmt.Errorf("target weights must be integers in 1..1000")
		}
		result[i] = int32(weight)
	}
	return result, nil
}
