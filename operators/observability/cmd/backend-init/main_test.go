package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestConfigureRetentionAndExistingPhysicalTable(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			var statements []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				user, password, ok := r.BasicAuth()
				if !ok || user != "admin" || password != "test-only" {
					t.Error("wrong administrator identity")
				}
				if err := r.ParseForm(); err != nil {
					t.Fatal(err)
				}
				sql := r.Form.Get("sql")
				statements = append(statements, sql)
				if strings.HasPrefix(sql, "SHOW") {
					if existing {
						_, _ = w.Write([]byte(`{"output":[{"records":{"rows":[["greptime_physical_table"]]}}]}`))
					} else {
						_, _ = w.Write([]byte(`{"output":[{"records":{"rows":[]}}]}`))
					}
				} else {
					_, _ = w.Write([]byte(`{"output":[{"affectedrows":0}]}`))
				}
			}))
			defer server.Close()
			file := filepath.Join(t.TempDir(), "passwd")
			if err := os.WriteFile(file, []byte("admin:readwrite=test-only\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := configure(t.Context(), server.URL, file); err != nil {
				t.Fatal(err)
			}
			want := []string{"ALTER DATABASE public SET 'ttl' = '24h'", "SHOW TABLES LIKE 'greptime_physical_table'"}
			if existing {
				want = append(want, "ALTER TABLE greptime_physical_table SET 'ttl' = '24h'")
			}
			if !reflect.DeepEqual(statements, want) {
				t.Fatalf("statements %v", statements)
			}
		})
	}
}

func TestConfigureRejectsUnacknowledgedAdministrativeWrite(t *testing.T) {
	for _, body := range []string{`{"error":"PRIVATE","code":1000}`, `{}`, `not-json`} {
		t.Run(body, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(
				http.HandlerFunc(
					func(w http.ResponseWriter, _ *http.Request) { calls++; _, _ = w.Write([]byte(body)) },
				),
			)
			defer server.Close()
			file := filepath.Join(t.TempDir(), "passwd")
			if err := os.WriteFile(file, []byte("admin:readwrite=test-only\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			err := configure(t.Context(), server.URL, file)
			if err == nil || strings.Contains(err.Error(), "PRIVATE") || calls != 1 {
				t.Fatalf("result=%v calls=%d", err, calls)
			}
		})
	}
}
