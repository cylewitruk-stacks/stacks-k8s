package bootstrap

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestPinnedSourcesAndManagerBinding(t *testing.T) {
	var pins struct {
		Revision  string           `json:"revision"`
		Contracts []contractSource `json:"contracts"`
	}
	if err := json.Unmarshal(contractPins, &pins); err != nil {
		t.Fatal(err)
	}
	if len(pins.Revision) != 40 || len(pins.Contracts) != 5 {
		t.Fatal("incomplete source pin")
	}
	if _, err := loadContracts(""); err == nil {
		t.Fatal("missing source bundle accepted")
	}
	directory := t.TempDir()
	for _, item := range pins.Contracts {
		if len(item.SHA256) != 64 {
			t.Fatal("missing digest")
		}
		if err := os.WriteFile(filepath.Join(directory, item.Name+".clar"), []byte("(ok true)"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := loadContracts(directory); err == nil || !strings.Contains(err.Error(), directory) {
		t.Fatalf("tampered bundle accepted or missing path: %v", err)
	}
	holder := "ST000000000000000000002AMW42H"
	source, err := directManager(holder)
	if err != nil || !strings.Contains(source, "(define-constant holder '"+holder+")") {
		t.Fatalf("holder not bound: %v", err)
	}
	if _, err = directManager(holder + "') (ok true)"); err == nil {
		t.Fatal("principal injection accepted")
	}
}

func TestContractExecutionRequiresCanonicalExactSuccessfulBytes(t *testing.T) {
	raw := []byte(strings.Repeat("x", 120))
	sum := sha512.Sum512_256(raw)
	tx := signedTransaction{TxID: hex.EncodeToString(sum[:]), Bytes: hex.EncodeToString(raw)}
	for _, mode := range []string{"success", "failure"} {
		t.Run(mode, func(t *testing.T) {
			sends := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					sends++
					_ = json.NewEncoder(w).Encode(tx.TxID)
					return
				}
				result := "(ok true)"
				if mode == "failure" {
					result = "(err u101)"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"tx": "0x" + tx.Bytes, "result": result, "index_block_hash": strings.Repeat("a", 64), "is_canonical": true})
			}))
			defer server.Close()
			u, _ := url.Parse(server.URL)
			port, _ := strconv.Atoi(u.Port())
			s := &session{http: server.Client(), options: Options{StacksPort: port, Evidence: filepath.Join(t.TempDir(), "evidence.json")}}
			if err := s.claimEvidence("test"); err != nil {
				t.Fatal(err)
			}
			err := s.execute(context.Background(), tx, "test publication")
			if (err == nil) != (mode == "success") || sends != 1 {
				t.Fatalf("mode %s, sends %d, err %v", mode, sends, err)
			}
			data, _ := os.ReadFile(s.options.Evidence)
			if !strings.Contains(string(data), "TransactionAuthorized") || !strings.Contains(string(data), tx.TxID) {
				t.Fatal("missing pre-submission evidence")
			}
		})
	}
}

func TestSignedPublicationBound(t *testing.T) {
	for _, size := range []int{99, 100, 128 * 1024, 256*1024 + 1} {
		raw := []byte(strings.Repeat("x", size))
		sum := sha512.Sum512_256(raw)
		tx := signedTransaction{TxID: fmt.Sprintf("%x", sum), Bytes: hex.EncodeToString(raw)}
		if validTransaction(tx) != (size >= 100 && size <= 256*1024) {
			t.Fatalf("unexpected wire bound for %d", size)
		}
	}
}

func TestExecutionRejectsWrongBytesAndNoncanonicalClaims(t *testing.T) {
	raw := []byte(strings.Repeat("x", 120))
	sum := sha512.Sum512_256(raw)
	id := hex.EncodeToString(sum[:])
	for _, mode := range []string{"canonical", "fork", "wrong-bytes", "missing-block", "failed"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				block := strings.Repeat("a", 64)
				wire := hex.EncodeToString(raw)
				result := "(ok true)"
				if mode == "wrong-bytes" {
					wire = hex.EncodeToString([]byte(strings.Repeat("y", 120)))
				}
				if mode == "missing-block" {
					block = ""
				}
				if mode == "failed" {
					result = "(err u1)"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"tx": wire, "result": result, "is_canonical": mode != "fork", "index_block_hash": block})
			}))
			defer server.Close()
			u, _ := url.Parse(server.URL)
			port, _ := strconv.Atoi(u.Port())
			s := &session{http: server.Client(), options: Options{StacksPort: port}}
			receipt, err := s.execution(context.Background(), id)
			if mode == "wrong-bytes" || mode == "missing-block" {
				if err == nil {
					t.Fatal("unbound inclusion accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if receipt.Found != (mode != "fork") || receipt.Success != (mode == "canonical") {
				t.Fatalf("wrong evidence: %+v", receipt)
			}
		})
	}
}
