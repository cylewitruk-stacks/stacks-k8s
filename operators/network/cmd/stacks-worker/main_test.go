package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMountedKeyIsBoundedAndErrorsDoNotDiscloseInput(t *testing.T) {
	for _, value := range []string{"private-secret-invalid", strings.Repeat("f", 257), strings.Repeat("0", 64)} {
		path := filepath.Join(t.TempDir(), "key")
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		key, err := mountedKey(path)
		if err == nil || key != "" || strings.Contains(err.Error(), value) {
			t.Fatal("invalid key accepted or exposed")
		}
	}
	path := filepath.Join(t.TempDir(), "key")
	value := strings.Repeat("0", 63) + "1"
	if err := os.WriteFile(path, []byte(value+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	key, err := mountedKey(path)
	if err != nil || key != value+"01" {
		t.Fatal("valid mounted key not normalized")
	}
}
