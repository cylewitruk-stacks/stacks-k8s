// Command bitcoin-observer exposes read-only local Core observations without a Kubernetes client.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/bitcoinobserver"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/bitcoinrpc"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	interval := flag.Duration("interval", bitcoinobserver.DefaultInterval, "delay between completed polls (5s to 300s)")
	endpoint := flag.String("rpc-endpoint", "http://127.0.0.1:18443", "Bitcoin RPC endpoint")
	listen := flag.String("listen", fmt.Sprintf(":%d", bitcoinobserver.MetricsPort), "Prometheus listener")
	credentials := flag.String("credentials-dir", "/rpc", "directory containing username and password files")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	if err := run(ctx, *endpoint, *listen, *credentials, *interval); err != nil {
		cancel()
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cancel()
}

// run keeps RPC unavailability inside observation results rather than process health.
func run(ctx context.Context, endpoint, listen, directory string, interval time.Duration) error {
	// #nosec G304 -- The administrator supplies the mounted credential directory, never RPC input.
	username, err := os.ReadFile(directory + "/username")
	if err != nil {
		return fmt.Errorf("read observer username file")
	}
	// #nosec G304 -- Fixed credential filename under the administrator-selected directory.
	password, err := os.ReadFile(directory + "/password")
	if err != nil {
		return fmt.Errorf("read observer password file")
	}
	if string(username) != bitcoinobserver.Username || len(password) != 64 {
		return fmt.Errorf("invalid observer credential files")
	}
	observer, err := bitcoinobserver.New(
		bitcoinrpc.New(bitcoinrpc.Credentials{Username: string(username), Password: string(password)}),
		endpoint,
		interval,
	)
	if err != nil {
		return err
	}
	registry := prometheus.NewRegistry()
	if err := registry.Register(observer); err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	server := &http.Server{
		Addr:              listen,
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		observer.Run(ctx, func(record bitcoinobserver.Record) { _ = json.NewEncoder(os.Stdout).Encode(record) })
	}()
	stopped := make(chan struct{})
	go func() { defer close(stopped); <-ctx.Done(); _ = server.Close() }()
	err = server.ListenAndServe()
	cancel()
	<-done
	<-stopped
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
