// Command bitcoin-receipt-delay delays or drops one real receipt in an isolated acceptance fixture.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

// delayKey marks the first selected method response for a delivery fault.
type delayKey struct{}

// methodKey identifies the request for credential-free receipt evidence.
type methodKey struct{}

func main() {
	method := os.Getenv("RECEIPT_METHOD")
	if method == "" {
		method = "generatetoaddress"
	}
	switch method {
	case "generatetoaddress", "invalidateblock", "reconsiderblock":
	default:
		log.Fatal("unsupported fixture method")
	}
	var invalidated atomic.Bool

	args := append(append([]string(nil), os.Args[1:]...), "-rpcbind=127.0.0.1:28443", "-rpcport=28443")
	bitcoin := exec.Command("/opt/bitcoin-31.1/bin/bitcoind-real", args...)
	bitcoin.Stdout, bitcoin.Stderr = os.Stdout, os.Stderr
	if err := bitcoin.Start(); err != nil {
		log.Fatal(err)
	}
	target, _ := url.Parse("http://127.0.0.1:28443")
	proxy := httputil.NewSingleHostReverseProxy(target)
	dropReceipt := errors.New("acceptance fixture discarded receipt")
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		if errors.Is(err, dropReceipt) {
			connection, _, hijackErr := w.(http.Hijacker).Hijack()
			if hijackErr == nil {
				_ = connection.Close()
			}
			return
		}
		http.Error(w, "Bitcoin RPC unavailable", http.StatusBadGateway)
	}
	proxy.ModifyResponse = func(response *http.Response) error {
		if method, _ := response.Request.Context().Value(methodKey{}).(string); method == "invalidateblock" && response.StatusCode == http.StatusOK {
			invalidated.Store(true)
		}
		if delay, _ := response.Request.Context().Value(delayKey{}).(bool); delay && response.StatusCode == http.StatusOK {
			label := method
			if label == "generatetoaddress" {
				label = "generation"
			}
			if os.Getenv("RECEIPT_FAULT") == "drop" {
				log.Printf("acceptance fixture: Bitcoin returned %s receipt; dropping response connection", label)
				return dropReceipt
			}
			log.Printf("acceptance fixture: Bitcoin returned %s receipt; delaying delivery for 15 seconds", label)
			time.Sleep(15 * time.Second)
		}
		return nil
	}
	var delayed atomic.Bool
	server := &http.Server{Addr: ":18443", ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 65537))
		r.Body.Close()
		if err != nil || len(body) > 65536 {
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var request struct {
			Method string `json:"method"`
		}
		if json.Unmarshal(body, &request) == nil {
			r = r.WithContext(context.WithValue(r.Context(), methodKey{}, request.Method))
		}
		eligible := os.Getenv("RECEIPT_AFTER_INVALIDATION") != "true" || invalidated.Load()
		if eligible && request.Method == method && delayed.CompareAndSwap(false, true) {
			r = r.WithContext(context.WithValue(r.Context(), delayKey{}, true))
		}
		proxy.ServeHTTP(w, r)
	})}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	go func() { <-ctx.Done(); _ = bitcoin.Process.Signal(syscall.SIGTERM); _ = server.Close() }()
	go func() {
		if err := bitcoin.Wait(); err != nil {
			log.Printf("Bitcoin process exited: %v", err)
		}
		stop()
		_ = server.Close()
	}()
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
