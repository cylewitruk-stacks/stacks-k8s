// Package receipts retains legacy transaction events that the native Nakamoto index does not serve.
package receipts

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/accountledger"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stackstx"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CanonicalReader verifies event block identity against the admitted node's canonical legacy headers.
type CanonicalReader interface {
	LegacyCanonical(context.Context, string, string) (bool, error)
}

// Server accepts bounded receipt events only from the account's currently admitted ingress Pod.
// It acknowledges a matching event only after its receipt is durable; node delivery retries never submit transactions.
type Server struct {
	// Ledger owns receipt accounting and mutation authority.
	Ledger *accountledger.Ledger
	// RPC checks canonical membership independently of callback contents.
	RPC CanonicalReader
	// Namespace bounds event candidates to this worker's environment scope.
	Namespace string
	// NetworkUID limits accounting to one network incarnation.
	NetworkUID string
	// Address is the HTTP listener; empty uses :8082.
	Address string
}

// NeedLeaderElection allows receipt retention on every replica; optimistic locking resolves duplicate delivery.
func (*Server) NeedLeaderElection() bool { return false }

// Start serves events until manager shutdown and drains bounded HTTP requests.
func (s *Server) Start(ctx context.Context) error {
	address := s.Address
	if address == "" {
		address = ":8082"
	}
	server := &http.Server{Addr: address, Handler: s, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 8192}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
	}
	drain, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err := server.Shutdown(drain)
	if err != nil {
		_ = server.Close()
		return err
	}
	err = <-done
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// ServeHTTP ignores unrelated events and retains exact execution facts for already authorized accounts.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != "/new_block" {
		w.WriteHeader(http.StatusOK)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	var event struct {
		SignerSignature json.RawMessage `json:"signer_signature"`
		BlockID         string          `json:"index_block_hash"`
		Transactions    []struct {
			ID     string `json:"txid"`
			Bytes  string `json:"raw_tx"`
			Result string `json:"raw_result"`
			Status string `json:"status"`
		} `json:"transactions"`
	}
	body := http.MaxBytesReader(w, r.Body, 2*1024*1024)
	if err := json.NewDecoder(body).Decode(&event); err != nil {
		http.Error(w, "invalid bounded event", http.StatusBadRequest)
		return
	}
	// Nakamoto receipts are recovered through the native transaction index.
	if len(event.SignerSignature) > 0 && string(event.SignerSignature) != "null" {
		w.WriteHeader(http.StatusOK)
		return
	}
	id := strings.TrimPrefix(event.BlockID, "0x")
	if decoded, err := hex.DecodeString(id); err != nil || len(decoded) != 32 {
		http.Error(w, "invalid block identity", http.StatusBadRequest)
		return
	}
	accounts := &stacks.StacksAccountList{}
	if err := s.Ledger.Reader.List(ctx, accounts, client.InNamespace(s.Namespace)); err != nil {
		http.Error(w, "receipt candidates unavailable", http.StatusServiceUnavailable)
		return
	}
	for i := range accounts.Items {
		account := &accounts.Items[i]
		if s.NetworkUID != "" && account.Spec.NetworkUID != s.NetworkUID {
			continue
		}
		pending := account.Status.Transaction
		if pending == nil || pending.Receipt != nil || account.Status.Phase == "Abandoned" {
			continue
		}
		for _, tx := range event.Transactions {
			if strings.TrimPrefix(tx.ID, "0x") != pending.TxID {
				continue
			}
			raw := stackstx.Transaction{TxID: pending.TxID, Bytes: strings.TrimPrefix(tx.Bytes, "0x")}
			result, err := hex.DecodeString(strings.TrimPrefix(tx.Result, "0x"))
			if !stackstx.Valid(raw) || err != nil || len(result) < 2 || len(result) > 128*1024 || !((tx.Status == "success" && result[0] == 7) || (tx.Status == "abort_by_response" && result[0] == 8)) {
				http.Error(w, "execution identity unavailable", http.StatusBadRequest)
				return
			}
			target, err := s.Ledger.Admission.Admit(ctx, account, pending.ConsumerUID, false)
			if err != nil {
				http.Error(w, "ingress identity unavailable", http.StatusServiceUnavailable)
				return
			}
			endpoint, err := url.Parse(target.Endpoint)
			peer, _, peerErr := net.SplitHostPort(r.RemoteAddr)
			if err != nil || peerErr != nil || net.ParseIP(peer) == nil || net.ParseIP(endpoint.Hostname()) == nil || !net.ParseIP(peer).Equal(net.ParseIP(endpoint.Hostname())) {
				http.Error(w, "event source is not the admitted ingress", http.StatusForbidden)
				return
			}
			canonical, err := s.RPC.LegacyCanonical(ctx, target.Endpoint, id)
			if err != nil || !canonical {
				http.Error(w, "canonical legacy receipt unavailable", http.StatusServiceUnavailable)
				return
			}
			if err := s.Ledger.RecordReceipt(ctx, account, stackstx.Inclusion{Source: "LegacyEvent", Found: true, Success: result[0] == 7, BlockID: id}); err != nil {
				http.Error(w, "receipt accounting unavailable", http.StatusServiceUnavailable)
				return
			}
		}
	}
	w.WriteHeader(http.StatusOK)
}

// String exposes only listener scope for diagnostics, never callback bodies.
func (s *Server) String() string { return fmt.Sprintf("legacy receipts in namespace %s", s.Namespace) }
