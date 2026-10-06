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

// spec: backup-recovery/symbolic-restore-preserves-late-foreign-destination-generation
func TestRestoreSymbolicPreservesLateForeignDestinationGeneration(t *testing.T) {
	for _, lane := range []string{"ordinary", "codex-set"} {
		shapes := []string{"regular", "directory", "absent"}
		if lane == "ordinary" {
			shapes = append(shapes, "symlink")
		}
		for _, shape := range shapes {
			t.Run(lane+"/"+shape, func(t *testing.T) {
				home, root := t.TempDir(), t.TempDir()
				t.Setenv("HOME", home)
				entry := filepath.Join(root, "entry")
				if err := os.Symlink("inert-archived-symbolic-target", entry); err != nil {
					t.Fatal(err)
				}
				restore := func() (string, error) { return Restore(hcc.TargetFactory, root) }
				if lane == "ordinary" {
					if err := Create(map[hcc.Target]ManagedPaths{hcc.TargetFactory: {Root: root, RelPaths: []string{"entry"}}}); err != nil {
						t.Fatal(err)
					}
				} else {
					partitions := []CodexPartition{{ID: "machine-agents", Root: root, RelPaths: []string{"entry"}}, {ID: "machine-codex", Root: filepath.Join(home, ".codex")}}
					if _, err := CreateCodexSet(partitions); err != nil {
						t.Fatal(err)
					}
					restore = func() (string, error) { return RestoreCodexSet(partitions) }
				}
				if err := os.Remove(entry); err != nil {
					t.Fatal(err)
				}
				switch shape {
				case "regular":
					if err := os.WriteFile(entry, []byte("inert original native leaf"), 0o640); err != nil {
						t.Fatal(err)
					}
				case "directory":
					if err := os.Mkdir(entry, 0o750); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(entry, "original.txt"), []byte("inert original native leaf"), 0o640); err != nil {
						t.Fatal(err)
					}
				case "symlink":
					if err := os.Symlink("inert-current-symbolic-target", entry); err != nil {
						t.Fatal(err)
					}
				}
				original, err := os.Lstat(entry)
				if shape != "absent" && err != nil {
					t.Fatal(err)
				}
				if shape == "absent" && !os.IsNotExist(err) {
					t.Fatalf("absence setup failed: %v", err)
				}
				retained := filepath.Join(t.TempDir(), "original-native-leaf")
				changed := false
				var directory, member os.FileInfo
				logger := slog.Default()
				t.Cleanup(func() { slog.SetDefault(logger) })
				slog.SetDefault(slog.New(codexRestoreObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), onSymbolic: func(path string) {
					if changed {
						return
					}
					changed = true
					actual, pathErr := os.Lstat(path)
					if shape == "absent" {
						if !os.IsNotExist(pathErr) {
							t.Fatalf("symbolic boundary did not reach admitted absence: %q %v", path, pathErr)
						}
					} else {
						if pathErr != nil || !os.SameFile(original, actual) {
							t.Fatalf("symbolic boundary did not reach admitted native leaf: %q %v", path, pathErr)
						}
						if err := os.Rename(entry, retained); err != nil {
							t.Fatal(err)
						}
					}
					if err := os.Mkdir(entry, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(entry, "foreign.txt"), []byte("foreign inert subtree member"), 0o600); err != nil {
						t.Fatal(err)
					}
					directory, err = os.Stat(entry)
					if err != nil {
						t.Fatal(err)
					}
					member, err = os.Stat(filepath.Join(entry, "foreign.txt"))
					if err != nil {
						t.Fatal(err)
					}
				}}))
				restored, err := restore()
				if raw, readErr := os.ReadFile(filepath.Join(entry, "foreign.txt")); readErr != nil || string(raw) != "foreign inert subtree member" {
					t.Fatalf("symbolic restore deleted foreign replacement tree: hit=%t timestamp=%q err=%v raw=%q read=%v", changed, restored, err, raw, readErr)
				}
				if !changed || restored != "" || !errors.Is(err, fsboundary.ErrGenerationConflict) {
					t.Fatalf("symbolic replacement did not generation refuse: %q %v", restored, err)
				}
				if info, err := os.Stat(entry); err != nil || !os.SameFile(directory, info) || directory.Mode() != info.Mode() || !directory.ModTime().Equal(info.ModTime()) {
					t.Fatalf("foreign directory metadata changed: %v", err)
				}
				if info, err := os.Stat(filepath.Join(entry, "foreign.txt")); err != nil || !os.SameFile(member, info) || member.Mode() != info.Mode() || !member.ModTime().Equal(info.ModTime()) {
					t.Fatalf("foreign member metadata changed: %v", err)
				}
				switch shape {
				case "regular", "directory":
					file := retained
					if shape == "directory" {
						file = filepath.Join(retained, "original.txt")
					}
					if raw, err := os.ReadFile(file); err != nil || string(raw) != "inert original native leaf" {
						t.Fatalf("original native bytes changed: %q %v", raw, err)
					}
				case "symlink":
					if target, err := os.Readlink(retained); err != nil || target != "inert-current-symbolic-target" {
						t.Fatalf("original symbolic metadata changed: %q %v", target, err)
					}
				}
				if shape != "absent" {
					if info, err := os.Lstat(retained); err != nil || !os.SameFile(original, info) || original.Mode() != info.Mode() || !original.ModTime().Equal(info.ModTime()) {
						t.Fatalf("original native metadata changed: %v", err)
					}
				}
				if entries, err := os.ReadDir(root); err != nil || len(entries) != 1 || entries[0].Name() != "entry" {
					t.Fatalf("symbolic refusal changed root membership: %v %v", entries, err)
				}
				foreign := filepath.Join(t.TempDir(), "retained-foreign-tree")
				if err := os.Rename(entry, foreign); err != nil {
					t.Fatal(err)
				}
				if shape != "absent" {
					if err := os.Rename(retained, entry); err != nil {
						t.Fatal(err)
					}
				}
				restored, err = restore()
				if err != nil || restored == "" {
					t.Fatalf("healthy symbolic restore failed: %q %v", restored, err)
				}
				if target, err := os.Readlink(entry); err != nil || target != "inert-archived-symbolic-target" {
					t.Fatalf("healthy restore did not publish archived symbolic target: %q %v", target, err)
				}
				if raw, err := os.ReadFile(filepath.Join(foreign, "foreign.txt")); err != nil || string(raw) != "foreign inert subtree member" {
					t.Fatalf("healthy restore changed retained foreign tree: %q %v", raw, err)
				}
			})
		}
	}
}

func TestRestoreCodexSymbolicPreflightPreservesExistingSymlink(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	entry := filepath.Join(root, "entry")
	if err := os.Symlink("inert-archived-symbolic-target", entry); err != nil {
		t.Fatal(err)
	}
	partitions := []CodexPartition{{ID: "machine-agents", Root: root, RelPaths: []string{"entry"}}, {ID: "machine-codex", Root: filepath.Join(home, ".codex")}}
	if _, err := CreateCodexSet(partitions); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(entry); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("inert-current-symbolic-target", entry); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(entry)
	if err != nil {
		t.Fatal(err)
	}
	reached := false
	logger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(logger) })
	slog.SetDefault(slog.New(codexRestoreObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), onSymbolic: func(string) { reached = true }}))
	if restored, err := RestoreCodexSet(partitions); err == nil || restored != "" || reached {
		t.Fatalf("Codex symbolic preflight contract changed: reached=%t %q %v", reached, restored, err)
	}
	if info, err := os.Lstat(entry); err != nil || !os.SameFile(before, info) || before.Mode() != info.Mode() || !before.ModTime().Equal(info.ModTime()) {
		t.Fatalf("Codex preflight changed symbolic generation: %v", err)
	}
	if target, err := os.Readlink(entry); err != nil || target != "inert-current-symbolic-target" {
		t.Fatalf("Codex preflight changed symbolic text: %q %v", target, err)
	}
	if err := os.Rename(entry, filepath.Join(t.TempDir(), "retained-current-link")); err != nil {
		t.Fatal(err)
	}
	if restored, err := RestoreCodexSet(partitions); err != nil || restored == "" || !reached {
		t.Fatalf("Codex symbolic healthy control not exercised: reached=%t %q %v", reached, restored, err)
	}
	if target, err := os.Readlink(entry); err != nil || target != "inert-archived-symbolic-target" {
		t.Fatalf("Codex symbolic control did not really restore: %q %v", target, err)
	}
}
