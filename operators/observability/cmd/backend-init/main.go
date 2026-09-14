// Command backend-init configures the administrator-owned local Greptime retention profile.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func main() {
	endpoint := flag.String("endpoint", "http://127.0.0.1:14000", "Forwarded Greptime HTTP base URL.")
	authFile := flag.String("auth-file", "", "Protected passwd file used for the greptime-auth Secret.")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	err := configure(ctx, *endpoint, *authFile)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Greptime public database and existing physical metric table use the 24h retention profile.")
}

// configure is an explicit administrator operation, never called by a recording worker.
func configure(ctx context.Context, endpoint, authFile string) error {
	base, err := url.Parse(endpoint)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil ||
		base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return fmt.Errorf("invalid backend base URL")
	}
	// The administrator explicitly selects this local credential file; it is never a cluster-provided path.
	data, err := os.ReadFile(authFile) // #nosec G304
	if err != nil {
		return fmt.Errorf("read administrator credential file: %w", err)
	}
	password := ""
	for _, line := range strings.Split(string(data), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "admin:readwrite="); ok {
			password = value
		}
	}
	if password == "" {
		return fmt.Errorf("passwd file has no admin:readwrite entry")
	}
	c := &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	execute := func(sql string) ([][]any, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(endpoint, "/")+"/v1/sql",
			strings.NewReader(url.Values{"sql": []string{sql}}.Encode()))
		if err != nil {
			return nil, err
		}
		req.SetBasicAuth("admin", password)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		res, err := c.Do(req)
		if err != nil {
			return nil, fmt.Errorf("backend request failed")
		}
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("backend returned HTTP %d", res.StatusCode)
		}
		var response struct {
			Code   int    `json:"code"`
			Error  string `json:"error"`
			Output []struct {
				Records struct {
					Rows [][]any `json:"rows"`
				} `json:"records"`
			} `json:"output"`
		}
		if err := json.NewDecoder(io.LimitReader(res.Body, 65536)).
			Decode(&response); err != nil || response.Code != 0 ||
			response.Error != "" ||
			len(response.Output) != 1 {
			return nil, fmt.Errorf("backend did not acknowledge retention configuration")
		}
		return response.Output[0].Records.Rows, nil
	}
	if _, err := execute("ALTER DATABASE public SET 'ttl' = '24h'"); err != nil {
		return err
	}
	rows, err := execute("SHOW TABLES LIKE 'greptime_physical_table'")
	if err != nil {
		return err
	}
	if len(rows) > 0 {
		_, err = execute("ALTER TABLE greptime_physical_table SET 'ttl' = '24h'")
	}
	return err
}
