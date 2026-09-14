//go:build live

package recorder

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TestGreptimeAppendAndRedaction qualifies the external ingestion contract without any network mutation.
func TestGreptimeAppendAndRedaction(t *testing.T) {
	if os.Getenv("STACKS_TELEMETRY_LIVE") != "1" {
		t.Skip("set STACKS_TELEMETRY_LIVE=1 for the disposable Greptime profile")
	}
	// Explicit administrator-selected qualification credential file.
	data, err := os.ReadFile(
		os.Getenv("STACKS_TELEMETRY_AUTH_FILE"),
	) // #nosec G304 G703 -- Explicit local administrator input.
	if err != nil {
		t.Fatal(err)
	}
	passwords := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		name, value, ok := strings.Cut(line, "=")
		if ok {
			passwords[name] = value
		}
	}
	if passwords["ingest:writeonly"] == "" || passwords["reader:readonly"] == "" {
		t.Fatal("ingestion and query credentials required")
	}
	endpoint := os.Getenv("STACKS_TELEMETRY_ENDPOINT")
	auth := &http.Request{Header: make(http.Header)}
	auth.SetBasicAuth("ingest", passwords["ingest:writeonly"])
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		t.Fatal(err)
	}
	uid := hex.EncodeToString(token)
	table := "stacks_qualification_" + uid
	exporter, err := NewExporter(endpoint, auth.Header.Get("Authorization"), table, "1h")
	if err != nil {
		t.Fatal(err)
	}
	object := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Pod", "metadata": map[string]any{"name": "canary", "uid": uid},
		"status": map[string]any{"message": "authorization=TEST_ONLY_PRIVATE", "phase": "Running"},
	}}
	record := Record{
		Time:       time.Now(),
		NetworkUID: uid,
		ObjectUID:  uid,
		Source:     "qualification",
		EventType:  EventSnapshot,
		Body:       publicBody(object),
	}
	for range 2 {
		if err := exporter.Write(t.Context(), record); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	c := &http.Client{
		Timeout:       3 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	for {
		sql := "SELECT count(*) AS records, " +
			"sum(CASE WHEN body LIKE '%TEST_ONLY_PRIVATE%' THEN 1 ELSE 0 END) AS leaks FROM " +
			table + " WHERE network_uid='" + uid + "'"
		// #nosec G704 -- Explicit administrator-selected qualification backend, validated by NewExporter.
		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodPost,
			strings.TrimSuffix(endpoint, "/")+"/v1/sql",
			strings.NewReader(url.Values{"sql": {sql}}.Encode()),
		)
		if err != nil {
			t.Fatal(err)
		}
		req.SetBasicAuth("reader", passwords["reader:readonly"])
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		res, err := c.Do(req) // #nosec G704 -- Administrator-selected backend; redirects disabled.
		if err != nil {
			t.Fatal("query unavailable")
		}
		var response struct {
			Output []struct {
				Records struct {
					Rows [][]int `json:"rows"`
				} `json:"records"`
			} `json:"output"`
		}
		err = json.NewDecoder(io.LimitReader(res.Body, 65536)).Decode(&response)
		_ = res.Body.Close()
		if err != nil || res.StatusCode != 200 || len(response.Output) != 1 {
			t.Fatal("query not acknowledged")
		}
		rows := response.Output[0].Records.Rows
		if len(rows) == 1 && len(rows[0]) == 2 && rows[0][0] == 2 && rows[0][1] == 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("duplicate retention or redaction assertion failed")
		case <-time.After(200 * time.Millisecond):
		}
	}
	t.Logf("retained two identical timestamp/tag records with zero credential-canary leaks; table=%s ttl=1h", table)
}
