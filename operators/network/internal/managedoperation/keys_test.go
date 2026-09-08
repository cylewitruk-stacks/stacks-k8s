package managedoperation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stackssdk"
)

func TestMountedKeyNeedsExactDigestAndAccountWithoutSecretAPI(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "assigned"), 0700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(stackssdk.Key{Address: "account", PrivateKey: "private"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assigned", "key.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	// A nil API reader ensures this process cannot fall back to namespace Secret reads.
	r := &Runtime{KeyDirectory: dir, SDK: &testEncoder{}}
	ref := stacks.ArtifactReference{Name: "assigned", Key: "key.json", Digest: digest(data)}
	key, err := r.key(context.Background(), "namespace", ref, "account")
	if err != nil || key.Address != "account" {
		t.Fatalf("mounted key not usable: %v", err)
	}
	if _, err := r.key(context.Background(), "namespace", ref, "other"); err == nil {
		t.Fatal("account mismatch accepted")
	}
	changed := ref
	changed.Digest = digest([]byte("other"))
	if _, err := r.key(context.Background(), "namespace", changed, "account"); err == nil {
		t.Fatal("changed key accepted")
	}
	for _, name := range []string{"../assigned", "..", "foreign"} {
		changed = ref
		changed.Name = name
		if _, err := r.key(context.Background(), "namespace", changed, "account"); err == nil {
			t.Fatal("unmounted key accepted")
		}
	}
}
