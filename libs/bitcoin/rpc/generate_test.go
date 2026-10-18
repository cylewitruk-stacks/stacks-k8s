package rpc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func generationServer(t *testing.T, calls *atomic.Int32, count int, hashes []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body struct {
			ID     string `json:"id"`
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Method != "generatetoaddress" || len(body.Params) != 3 || body.Params[0] != float64(count) ||
			body.Params[1] != "address" || body.Params[2] != float64(1000000) {
			t.Errorf("generation wire contract changed: %v %v", body.Method, body.Params)
		}
		result, _ := json.Marshal(hashes)
		_, _ = fmt.Fprintf(w, `{"id":%q,"result":%s,"error":null}`, body.ID, result)
	}))
}

func TestGenerateBlocksSendsCountAndReturnsLastHash(t *testing.T) {
	hashes := []string{strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)}
	var calls atomic.Int32
	server := generationServer(t, &calls, 3, hashes)
	defer server.Close()
	last, err := New(Credentials{}).GenerateBlocks(t.Context(), server.URL, "address", "receipt", 3)
	if err != nil || last != hashes[2] || calls.Load() != 1 {
		t.Fatal(err, last, calls.Load())
	}
}

func TestGenerateBlocksMaximumReceiptFitsResponseBound(t *testing.T) {
	hashes := make([]string, MaxGeneratedBlocks)
	for i := range hashes {
		hashes[i] = fmt.Sprintf("%064x", i+1)
	}
	var calls atomic.Int32
	server := generationServer(t, &calls, MaxGeneratedBlocks, hashes)
	defer server.Close()
	last, err := New(Credentials{}).GenerateBlocks(t.Context(), server.URL, "address", "receipt", MaxGeneratedBlocks)
	if err != nil || last != hashes[MaxGeneratedBlocks-1] {
		t.Fatal(err, last)
	}
}

func TestGenerateBlocksRequiresEveryRequestedHash(t *testing.T) {
	for name, hashes := range map[string][]string{
		"short":   {strings.Repeat("a", 64), strings.Repeat("b", 64)},
		"long":    {strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64), strings.Repeat("d", 64)},
		"invalid": {strings.Repeat("a", 64), strings.Repeat("G", 64), strings.Repeat("c", 64)},
		"length":  {strings.Repeat("a", 64), strings.Repeat("b", 63), strings.Repeat("c", 64)},
	} {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			server := generationServer(t, &calls, 3, hashes)
			defer server.Close()
			if last, err := New(Credentials{}).GenerateBlocks(
				t.Context(), server.URL, "address", "receipt", 3,
			); err == nil || last != "" {
				t.Fatal("incomplete batch receipt accepted", last)
			}
		})
	}
}

func TestGenerateBlocksRejectsUnsupportedCountWithoutSending(t *testing.T) {
	for _, count := range []int{0, -1, MaxGeneratedBlocks + 1} {
		var calls atomic.Int32
		server := generationServer(t, &calls, count, nil)
		_, err := New(Credentials{}).GenerateBlocks(t.Context(), server.URL, "address", "receipt", count)
		server.Close()
		if err == nil || calls.Load() != 0 {
			t.Fatal("unsupported count was sent", count, err)
		}
	}
}
