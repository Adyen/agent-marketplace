package backup

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// spec: backup-recovery/restore-final-destination-replacement-preserves-original-authority
func TestRestoreFinalDestinationReplacementPreservesOriginalAuthority(t *testing.T) {
	for _, lane := range []string{"ordinary", "mapped", "codex-set"} {
		for _, state := range []string{"existing", "absent", "parent"} {
			t.Run(lane+"/"+state, func(t *testing.T) {
				home, root := t.TempDir(), t.TempDir()
				t.Setenv("HOME", home)
				destination := filepath.Join(root, "native.json")
				if err := os.WriteFile(destination, []byte("inert archived native payload"), 0o640); err != nil {
					t.Fatal(err)
				}
				restore := func() (string, error) { return Restore(hcc.TargetFactory, root) }
				switch lane {
				case "ordinary":
					if err := Create(map[hcc.Target]ManagedPaths{hcc.TargetFactory: {Root: root, RelPaths: []string{"native.json"}}}); err != nil {
						t.Fatal(err)
					}
				case "mapped":
					mappings := map[string]string{".agent-marketplace-native/machine-mcp/.claude.json": destination}
					if err := Create(map[hcc.Target]ManagedPaths{hcc.TargetClaude: {Root: filepath.Join(root, "absent-ordinary-root"), MappedFiles: mappings}}); err != nil {
						t.Fatal(err)
					}
					restore = func() (string, error) { return Restore(hcc.TargetClaude, root, mappings) }
				case "codex-set":
					partitions := []CodexPartition{{ID: "machine-agents", Root: root, RelPaths: []string{"native.json"}}, {ID: "machine-codex", Root: filepath.Join(home, ".codex")}}
					if _, err := CreateCodexSet(partitions); err != nil {
						t.Fatal(err)
					}
					restore = func() (string, error) { return RestoreCodexSet(partitions) }
				}
				if err := os.WriteFile(destination, []byte("inert current native generation"), 0o640); err != nil {
					t.Fatal(err)
				}
				original, err := os.Stat(destination)
				if err != nil {
					t.Fatal(err)
				}
				retained := filepath.Join(t.TempDir(), "original-native-generation")
				retainedParent := filepath.Join(t.TempDir(), "original-native-parent")
				var foreignParent os.FileInfo
				if state == "absent" {
					if err := os.Rename(destination, retained); err != nil {
						t.Fatal(err)
					}
				}
				changed := false
				var replacement os.FileInfo
				logger := slog.Default()
				t.Cleanup(func() { slog.SetDefault(logger) })
				slog.SetDefault(slog.New(codexRestoreObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), onPublication: func(string) {
					if changed {
						return
					}
					changed = true
					if state == "existing" {
						if err := os.Rename(destination, retained); err != nil {
							t.Fatal(err)
						}
					}
					if state == "parent" {
						if err := os.Rename(root, retainedParent); err != nil {
							t.Fatal(err)
						}
						if err := os.Mkdir(root, 0o700); err != nil {
							t.Fatal(err)
						}
						retained = filepath.Join(retainedParent, "native.json")
					}
					if err := os.WriteFile(destination, []byte("foreign inert final destination replacement"), 0o600); err != nil {
						t.Fatal(err)
					}
					replacement, err = os.Stat(destination)
					if err != nil {
						t.Fatal(err)
					}
					if state == "parent" {
						foreignParent, err = os.Stat(root)
						if err != nil {
							t.Fatal(err)
						}
					}
				}}))
				restored, err := restore()
				if raw, readErr := os.ReadFile(destination); readErr != nil || string(raw) != "foreign inert final destination replacement" {
					t.Fatalf("restore overwrote late destination generation: hit=%t timestamp=%q err=%v raw=%q read=%v", changed, restored, err, raw, readErr)
				}
				if !changed || restored != "" || !errors.Is(err, fsboundary.ErrGenerationConflict) {
					t.Fatalf("restore final destination did not generation refuse: %q %v", restored, err)
				}
				if info, err := os.Stat(destination); err != nil || !os.SameFile(replacement, info) || replacement.Mode() != info.Mode() || !replacement.ModTime().Equal(info.ModTime()) {
					t.Fatalf("replacement metadata changed: %v", err)
				}
				if raw, err := os.ReadFile(retained); err != nil || string(raw) != "inert current native generation" {
					t.Fatalf("retained original bytes changed: %q %v", raw, err)
				}
				if info, err := os.Stat(retained); err != nil || !os.SameFile(original, info) || original.Mode() != info.Mode() || !original.ModTime().Equal(info.ModTime()) {
					t.Fatalf("retained original metadata changed: %v", err)
				}
				if entries, err := os.ReadDir(root); err != nil || len(entries) != 1 || entries[0].Name() != "native.json" {
					t.Fatalf("refused restore left staging/unexpected members: %v %v", entries, err)
				}
				foreignPath := filepath.Join(t.TempDir(), "retained-foreign")
				if state == "parent" {
					if info, err := os.Stat(root); err != nil || !os.SameFile(foreignParent, info) || foreignParent.Mode() != info.Mode() || !foreignParent.ModTime().Equal(info.ModTime()) {
						t.Fatalf("foreign parent metadata changed: %v", err)
					}
					if entries, err := os.ReadDir(retainedParent); err != nil || len(entries) != 1 || entries[0].Name() != "native.json" {
						t.Fatalf("original parent stage cleanup changed members: %v %v", entries, err)
					}
					if err := os.Rename(root, foreignPath); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(retainedParent, root); err != nil {
						t.Fatal(err)
					}
					foreignPath = filepath.Join(foreignPath, "native.json")
				} else if err := os.Rename(destination, foreignPath); err != nil {
					t.Fatal(err)
				}
				if state == "existing" {
					if err := os.Rename(retained, destination); err != nil {
						t.Fatal(err)
					}
				}
				restored, err = restore()
				if err != nil || restored == "" {
					t.Fatalf("healthy restore failed: %q %v", restored, err)
				}
				if raw, err := os.ReadFile(destination); err != nil || string(raw) != "inert archived native payload" {
					t.Fatalf("healthy restore did not publish archive bytes: %q %v", raw, err)
				}
				if info, err := os.Stat(destination); err != nil || info.Mode().Perm() != 0o640 {
					t.Fatalf("healthy restore mode changed: %v", err)
				}
				if raw, err := os.ReadFile(foreignPath); err != nil || string(raw) != "foreign inert final destination replacement" {
					t.Fatalf("healthy restore changed foreign generation: %q %v", raw, err)
				}
			})
		}
	}
}

func TestRestoreInitialOutputAliasRetainsHealthyPublication(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	file := filepath.Join(root, "native.json")
	if err := os.WriteFile(file, []byte("inert archived alias payload"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := Create(map[hcc.Target]ManagedPaths{hcc.TargetFactory: {Root: root, RelPaths: []string{"native.json"}}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("inert current alias payload"), 0o640); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "selected-output-root")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	if restored, err := Restore(hcc.TargetFactory, alias); err != nil || restored == "" {
		t.Fatalf("supported initial restore root alias refused: %q %v", restored, err)
	}
	if raw, err := os.ReadFile(file); err != nil || string(raw) != "inert archived alias payload" {
		t.Fatalf("alias restore did not genuinely publish archive: %q %v", raw, err)
	}
	if info, err := os.Stat(file); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("alias restore changed archive mode: %v", err)
	}
	if link, err := os.Readlink(alias); err != nil || link != root {
		t.Fatalf("alias restore changed selected link: %q %v", link, err)
	}
}
