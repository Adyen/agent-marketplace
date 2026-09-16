package materialize

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestWindsurfLegacyHookDirectoryPreservedWhenDesiredEmpty(t *testing.T) {
	for _, machineScope := range []bool{false, true} {
		for _, symlink := range []bool{false, true} {
			t.Run("machine="+boolString(machineScope)+"/symlink="+boolString(symlink), func(t *testing.T) {
				home := t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
				t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
				repo := t.TempDir()
				root := filepath.Join(repo, ".windsurf")
				if machineScope {
					root = filepath.Join(home, ".codeium", "windsurf")
				}
				legacy := filepath.Join(root, "hooks", "pre_run_command.json")
				if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
					t.Fatal(err)
				}
				if symlink {
					target := filepath.Join(t.TempDir(), "legacy.json")
					if err := os.WriteFile(target, []byte(`{"command":"echo legacy"}`), 0o644); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(target, legacy); err != nil {
						t.Fatal(err)
					}
				} else if err := os.WriteFile(legacy, []byte(`{"command":"echo legacy"}`), 0o644); err != nil {
					t.Fatal(err)
				}
				if _, err := Run(context.Background(), repo, nil, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile})); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Lstat(legacy); err != nil {
					t.Fatalf("legacy hook path was removed: %v", err)
				}
			})
		}
	}
}
