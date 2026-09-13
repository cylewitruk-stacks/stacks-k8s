package rpc

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestNakamotoTipRequiresNativeHeaderAndMatchingIdentity(t *testing.T) {
	view := ChainView{
		Info:          Info{StacksHeight: 3},
		ConsensusHash: strings.Repeat("a", 40),
		IndexBlockID:  strings.Repeat("b", 64),
	}
	valid := fmt.Sprintf(`{"anchored_header":{"Nakamoto":{"chain_length":3,"consensus_hash":%q}}}`, view.ConsensusHash)
	for _, tc := range []struct {
		name, body    string
		code          int
		want, wantErr bool
	}{
		{"Nakamoto", valid, 200, true, false},
		{"legacy", `{"anchored_header":{"Epoch2":{"version":0}}}`, 200, false, false},
		{"missing", `{}`, 200, false, true},
		{"null", `{"anchored_header":{"Nakamoto":null}}`, 200, false, true},
		{"height absent", strings.Replace(valid, `"chain_length":3,`, "", 1), 200, false, true},
		{"height differs", strings.Replace(valid, `"chain_length":3`, `"chain_length":4`, 1), 200, false, true},
		{
			"consensus differs",
			strings.ReplaceAll(valid, view.ConsensusHash, strings.Repeat("c", 40)),
			200,
			false,
			true,
		},
		{"ambiguous family", `{"anchored_header":{"Epoch2":{},"Nakamoto":{}}}`, 200, false, true},
		{"unknown family", `{"anchored_header":{"Future":{}}}`, 200, false, true},
		{"missing endpoint", valid, 404, false, true},
		{"malformed", `{`, 200, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodGet || r.URL.Path != "/v3/tenures/tip_metadata/"+view.ConsensusHash {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				w.WriteHeader(tc.code)
				if _, err := fmt.Fprint(w, tc.body); err != nil {
					t.Error(err)
				}
			})
			got, err := c.NakamotoTip(context.Background(), view)
			if got != tc.want || (err != nil) != tc.wantErr || calls != 1 {
				t.Fatalf("got %v, %v, calls=%d", got, err, calls)
			}
			bad := view
			bad.ConsensusHash = "../other"
			if ok, err := c.NakamotoTip(context.Background(), bad); ok || err == nil || calls != 1 {
				t.Fatal("invalid identity reached transport")
			}
		})
	}
}
