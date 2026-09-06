package contract

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestRuntimeImportsStayIndependent excludes cross-operator runtime dependencies, including unused imports.
func TestRuntimeImportsStayIndependent(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..")
	for _, operator := range []string{"network", "action", "observability"} {
		base := filepath.Join(root, "operators", operator)
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imp := range file.Imports {
				value, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					return err
				}
				prefix := "github.com/cylewitruk-stacks/stacks-k8s/operators/"
				if strings.HasPrefix(value, prefix) && !strings.HasPrefix(value, prefix+operator+"/") {
					t.Errorf("%s imports another operator runtime: %s", path, value)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestActionCRDsHaveOneChartOwner prevents baseline installation from taking action ownership.
func TestActionCRDsHaveOneChartOwner(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "charts")
	for _, name := range []string{"actions.stacks.org_bitcoinblockgenerations.yaml", "actions.stacks.org_bitcoinreorganizations.yaml"} {
		if _, err := os.Stat(filepath.Join(root, "stacks-action-operator", "crds", name)); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(root, "stacks-network-operator", "crds", name)); !os.IsNotExist(err) {
			t.Fatalf("action CRD has network owner: %s (%v)", name, err)
		}
	}
}
