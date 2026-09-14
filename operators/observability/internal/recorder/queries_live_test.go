//go:build live

package recorder

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestDocumentedQueries requires a populated disposable recording with a known scrape failure and recovery.
func TestDocumentedQueries(t *testing.T) {
	if os.Getenv("STACKS_TELEMETRY_QUERY_LIVE") != "1" {
		t.Skip("requires populated telemetry qualification")
	}
	values := map[string]string{}
	uuid := regexp.MustCompile(`^[a-f0-9-]{36}$`)
	for _, name := range []string{"network-uid", "telemetry-uid", "participant-uid", "prefix", "from", "to"} {
		value := os.Getenv("STACKS_TELEMETRY_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")))
		if strings.HasSuffix(name, "uid") && !uuid.MatchString(value) {
			t.Fatalf("invalid %s", name)
		}
		values[name] = value
	}
	if !regexp.MustCompile(`^stacks_[a-f0-9]{32}$`).MatchString(values["prefix"]) {
		t.Fatal("invalid table prefix")
	}
	from, err := time.Parse(time.RFC3339, values["from"])
	if err != nil {
		t.Fatal(err)
	}
	to, err := time.Parse(time.RFC3339, values["to"])
	if err != nil || !to.After(from) || to.Sub(from) > 15*time.Minute {
		t.Fatal("query window must be at most fifteen minutes")
	}
	data, err := os.ReadFile(
		os.Getenv("STACKS_TELEMETRY_AUTH_FILE"),
	) // #nosec G304 G703 -- Explicit qualification credential file.
	if err != nil {
		t.Fatal(err)
	}
	password := ""
	for _, line := range strings.Split(string(data), "\n") {
		if value, ok := strings.CutPrefix(line, "reader:readonly="); ok {
			password = value
		}
	}
	if password == "" {
		t.Fatal("reader credential required")
	}
	text, err := os.ReadFile("../../../../docs/observability/queries.sql")
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(string(text), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			lines = append(lines, line)
		}
	}
	sql := strings.Join(lines, "\n")
	for key, value := range values {
		sql = strings.ReplaceAll(sql, "<"+key+">", value)
	}
	endpoint := os.Getenv("STACKS_TELEMETRY_ENDPOINT")
	if _, err := NewExporter(endpoint, "Basic dGVzdDp0ZXN0", "stacks_unused", "1h"); err != nil {
		t.Fatal(err)
	}
	c := &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	for i, query := range strings.Split(sql, ";") {
		if strings.TrimSpace(query) == "" {
			continue
		}
		// #nosec G704 -- Explicit validated administrator qualification endpoint; redirects disabled.
		req, err := http.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			endpoint+"/v1/sql",
			strings.NewReader(url.Values{"sql": {query}}.Encode()),
		)
		if err != nil {
			t.Fatal(err)
		}
		req.SetBasicAuth("reader", password)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		res, err := c.Do(req) // #nosec G704 -- Explicit validated administrator endpoint; redirects disabled.
		if err != nil {
			t.Fatal("query transport unavailable")
		}
		var result struct {
			Output []struct {
				Records struct {
					Rows [][]any `json:"rows"`
				} `json:"records"`
			} `json:"output"`
			Error string `json:"error"`
		}
		err = json.NewDecoder(io.LimitReader(res.Body, 32*1024*1024)).Decode(&result)
		_ = res.Body.Close()
		if err != nil || res.StatusCode != 200 || result.Error != "" || len(result.Output) != 1 {
			t.Fatalf("query %d failed: decode=%v status=%d backend=%s", i+1, err, res.StatusCode, result.Error)
		}
		rows := result.Output[0].Records.Rows
		t.Logf("query=%d rows=%d", i+1, len(rows))
		if len(rows) == 0 {
			t.Fatalf("query %d did not answer a populated fixture", i+1)
		}
		if i == 7 {
			for _, row := range rows {
				if version, ok := row[2].(string); !ok || version == "" {
					t.Fatal("dedup query omitted resourceVersion")
				}
			}
		}
		if i == 4 {
			found := false
			for _, row := range rows {
				if row[0] == values["participant-uid"] && row[2] == float64(0) && row[3] == float64(1) {
					found = true
				}
			}
			if !found {
				t.Fatal("scrape query did not retain both failure and success for the target identity")
			}
		}
	}
}
