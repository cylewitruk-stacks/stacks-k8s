// Command stacks-bootstrap initializes a fresh provisioned direct-staking regtest network.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/bootstrap"
)

func main() {
	flags := flag.NewFlagSet("stacks-bootstrap", flag.ExitOnError)
	var options bootstrap.Options
	flags.StringVar(&options.Manifest, "manifest", "", "Private stacks-environment output (required).")
	flags.StringVar(&options.Kubeconfig, "kubeconfig", "", "Explicit kubeconfig (required).")
	flags.StringVar(&options.Context, "context", "", "Explicit context (required).")
	flags.StringVar(&options.SDKDirectory, "sdk-directory", "transactions", "Directory containing offline SDK adapters.")
	flags.StringVar(&options.Evidence, "evidence", "", "New public evidence path; existing files are rejected.")
	flags.StringVar(&options.SBTCContracts, "sbtc-contracts", "", "Pinned external sBTC contracts/contracts directory (PoX-5 only).")
	flags.IntVar(&options.BitcoinPort, "bitcoin-port", 19443, "Loopback Bitcoin forwarding port.")
	flags.IntVar(&options.StacksPort, "stacks-port", 20443, "Loopback Stacks forwarding port.")
	flags.Parse(os.Args[1:])
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := bootstrap.Run(ctx, options); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
