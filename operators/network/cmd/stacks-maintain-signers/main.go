// Command stacks-maintain-signers maintains one externally provisioned PoX-4 stacker.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/bootstrap"
)

func main() {
	var options bootstrap.Options
	flag.StringVar(&options.Manifest, "manifest", "", "Private provisioning document (required).")
	flag.StringVar(&options.SDKDirectory, "sdk-directory", "transactions", "Offline SDK adapter directory.")
	flag.StringVar(&options.Evidence, "evidence", "", "New public renewal evidence path; existing files are rejected.")
	flag.IntVar(&options.StacksPort, "stacks-port", 20443, "Existing loopback-only Stacks port-forward.")
	duration := flag.Int("duration-seconds", 0, "Maintenance duration, 1..86400 seconds (required).")
	flag.Parse()
	if *duration < 1 || *duration > 86400 {
		fmt.Fprintln(os.Stderr, "duration must be 1..86400 seconds")
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := bootstrap.Maintain(ctx, options, time.Duration(*duration)*time.Second); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
