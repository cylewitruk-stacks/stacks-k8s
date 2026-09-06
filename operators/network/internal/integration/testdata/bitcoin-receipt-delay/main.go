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

// delayKey marks the first generation response selected for a delivery fault.
type delayKey struct{}

func main() {
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
		if delay, _ := response.Request.Context().Value(delayKey{}).(bool); delay && response.StatusCode == http.StatusOK {
			if os.Getenv("RECEIPT_FAULT") == "drop" {
				log.Print("acceptance fixture: Bitcoin returned generation receipt; dropping response connection")
				return dropReceipt
			}
			log.Print("acceptance fixture: Bitcoin returned generation receipt; delaying delivery for 15 seconds")
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
		if json.Unmarshal(body, &request) == nil && request.Method == "generatetoaddress" && delayed.CompareAndSwap(false, true) {
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
