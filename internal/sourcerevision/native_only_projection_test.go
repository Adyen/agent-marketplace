package sourcerevision

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

const nativeOnlyFactoryHooks = `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo pre"}]}],"SessionStart":[{"hooks":[{"type":"command","command":"echo start"}]}]}`

func writeNativeOnlyFactoryRoot(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, ".factory", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// spec: hook-resources/native-only-git-hook-sources-retain-frozen-projection
func TestNativeOnlyFactoryRootRetainsFrozenProjection(t *testing.T) {
	for _, harness := range []string{"factory", ""} {
		t.Run("harness_"+harness, func(t *testing.T) {
			root := writeNativeOnlyFactoryRoot(t, nativeOnlyFactoryHooks)
			result, err := Build(Input{SnapshotRoot: root, SourceHarness: harness, RevisionSchema: "v4"})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			projection := result.Projection()
			if projection.SourceHarness != "factory" {
				t.Fatalf("frozen projection source harness = %q, want factory", projection.SourceHarness)
			}
			if len(projection.Resources) != 0 {
				t.Fatalf("frozen projection = %#v, want no resources", projection.Resources)
			}
			if result.Catalog != nil {
				t.Fatal("native-only root returned a runtime catalog")
			}
			current, err := catalog.Load(root, "")
			if err != nil {
				t.Fatalf("current catalog: %v", err)
			}
			hooks := 0
			for _, entry := range current.Entries {
				if entry.Kind == ir.KindHook {
					hooks++
				}
			}
			if hooks != 2 {
				t.Fatalf("current catalog hooks = %d, want 2", hooks)
			}
		})
	}
}

// spec: hook-resources/native-only-git-hook-sources-retain-frozen-projection
func TestNativeOnlyFactoryRootRefusesMalformedNativeInput(t *testing.T) {
	root := writeNativeOnlyFactoryRoot(t, `{"PreToolUse":[`)
	if _, err := Build(Input{SnapshotRoot: root, SourceHarness: "factory", RevisionSchema: "v4"}); !errors.Is(err, ErrCatalog) {
		t.Fatalf("malformed native hook file error = %v, want ErrCatalog", err)
	}
}
