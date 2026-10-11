package backup

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// spec: backup-recovery/ordinary-backup-namespace-refuses-retargeted-parent
func TestCreateOrdinaryNamespaceRefusesRetargetedParent(t *testing.T) {
	for _, lane := range []string{"ordinary", "mapped"} {
		for _, level := range []string{"home", "data", "backups", "target"} {
			for _, replacement := range []string{"alias", "directory"} {
				t.Run(lane+"/"+level+"/"+replacement, func(t *testing.T) {
					home, root, foreign := t.TempDir(), t.TempDir(), t.TempDir()
					t.Setenv("HOME", home)
					native := filepath.Join(root, "native.txt")
					if err := os.WriteFile(native, []byte("inert original namespace archive bytes"), 0o640); err != nil {
						t.Fatal(err)
					}
					managed := ManagedPaths{Root: root, RelPaths: []string{"native.txt"}}
					restore := func() (string, error) { return Restore(hcc.TargetFactory, root) }
					const mappedName = ".agent-marketplace-native/machine-mcp/native.json"
					if lane == "mapped" {
						managed = ManagedPaths{Root: filepath.Join(root, "absent-source-root"), MappedFiles: map[string]string{mappedName: native}}
						restore = func() (string, error) { return Restore(hcc.TargetFactory, root, map[string]string{mappedName: native}) }
					}
					selected := home
					switch level {
					case "data":
						selected = filepath.Join(home, ".agent-marketplace")
					case "backups":
						selected = filepath.Join(home, ".agent-marketplace", "backups")
					case "target":
						selected = filepath.Join(home, ".agent-marketplace", "backups", "factory")
					}
					if level != "home" {
						if err := os.MkdirAll(selected, 0o750); err != nil {
							t.Fatal(err)
						}
					}
					original, err := os.Stat(selected)
					if err != nil {
						t.Fatal(err)
					}
					currentNative, err := os.Stat(native)
					if err != nil {
						t.Fatal(err)
					}
					foreignFile := filepath.Join(foreign, "foreign.txt")
					if err := os.WriteFile(foreignFile, []byte("foreign inert namespace bytes"), 0o640); err != nil {
						t.Fatal(err)
					}
					foreignInfo, err := os.Stat(foreignFile)
					if err != nil {
						t.Fatal(err)
					}
					foreignDirectory, err := os.Stat(foreign)
					if err != nil {
						t.Fatal(err)
					}
					retained := filepath.Join(t.TempDir(), "retained-original-namespace")
					changed := false
					logger := slog.Default()
					t.Cleanup(func() { slog.SetDefault(logger) })
					slog.SetDefault(slog.New(codexRestoreObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), onNamespaceParent: func(path string) {
						if changed {
							return
						}
						changed = true
						observed, err := os.Stat(path)
						if err != nil || !os.SameFile(original, observed) {
							t.Fatalf("ordinary backup did not reach original namespace parent: %q %v", path, err)
						}
						if err := os.Rename(selected, retained); err != nil {
							t.Fatal(err)
						}
						if replacement == "alias" {
							err = os.Symlink(foreign, selected)
						} else {
							err = os.Rename(foreign, selected)
						}
						if err != nil {
							t.Fatal(err)
						}
					}}))
					err = Create(map[hcc.Target]ManagedPaths{hcc.TargetFactory: managed})
					foreignPath := foreign
					if replacement == "directory" {
						foreignPath = selected
					}
					if entries, readErr := os.ReadDir(foreignPath); readErr != nil || len(entries) != 1 || entries[0].Name() != "foreign.txt" {
						t.Fatalf("ordinary backup created namespace/archive in foreign root: hit=%t err=%v entries=%v read=%v", changed, err, entries, readErr)
					}
					if !changed || !errors.Is(err, fsboundary.ErrGenerationConflict) {
						t.Fatalf("ordinary namespace retarget did not generation-refuse: hit=%t %v", changed, err)
					}
					if entries, err := os.ReadDir(retained); err != nil || len(entries) != 0 {
						t.Fatalf("namespace refusal left original archive/directory residue: %v %v", entries, err)
					}
					if raw, err := os.ReadFile(filepath.Join(foreignPath, "foreign.txt")); err != nil || string(raw) != "foreign inert namespace bytes" {
						t.Fatalf("foreign namespace bytes changed: %q %v", raw, err)
					}
					if info, err := os.Stat(filepath.Join(foreignPath, "foreign.txt")); err != nil || !os.SameFile(foreignInfo, info) || foreignInfo.Mode() != info.Mode() || !foreignInfo.ModTime().Equal(info.ModTime()) {
						t.Fatalf("foreign namespace file metadata changed: %v", err)
					}
					if info, err := os.Stat(foreignPath); err != nil || !os.SameFile(foreignDirectory, info) || foreignDirectory.Mode() != info.Mode() || !foreignDirectory.ModTime().Equal(info.ModTime()) {
						t.Fatalf("foreign namespace directory metadata changed: %v", err)
					}
					if info, err := os.Stat(retained); err != nil || !os.SameFile(original, info) || original.Mode() != info.Mode() || !original.ModTime().Equal(info.ModTime()) {
						t.Fatalf("original namespace directory metadata changed: %v", err)
					}
					if raw, err := os.ReadFile(native); err != nil || string(raw) != "inert original namespace archive bytes" {
						t.Fatalf("native namespace source bytes changed: %q %v", raw, err)
					}
					if info, err := os.Stat(native); err != nil || !os.SameFile(currentNative, info) || currentNative.Mode() != info.Mode() || !currentNative.ModTime().Equal(info.ModTime()) {
						t.Fatalf("native namespace source metadata changed: %v", err)
					}
					if replacement == "alias" {
						if link, err := os.Readlink(selected); err != nil || link != foreign {
							t.Fatalf("foreign namespace alias changed: %q %v", link, err)
						}
						if err := os.Remove(selected); err != nil {
							t.Fatal(err)
						}
					} else {
						if err := os.Rename(selected, foreign); err != nil {
							t.Fatal(err)
						}
					}
					if err := os.Rename(retained, selected); err != nil {
						t.Fatal(err)
					}
					if err := Create(map[hcc.Target]ManagedPaths{hcc.TargetFactory: managed}); err != nil {
						t.Fatalf("genuine ordinary namespace creation retry refused: %v", err)
					}
					listed, err := ListBackups()
					if err != nil || len(listed[hcc.TargetFactory]) != 1 {
						t.Fatalf("healthy namespace did not publish a real archive: %v %v", listed, err)
					}
					if err := os.WriteFile(native, []byte("inert changed namespace native bytes"), 0o640); err != nil {
						t.Fatal(err)
					}
					if restored, err := restore(); err != nil || restored != listed[hcc.TargetFactory][0] {
						t.Fatalf("genuine namespace archive cannot restore: %q %v", restored, err)
					}
					if raw, err := os.ReadFile(native); err != nil || string(raw) != "inert original namespace archive bytes" {
						t.Fatalf("namespace archive did not genuinely recover bytes: %q %v", raw, err)
					}
				})
			}
		}
	}
}

func TestCreateOrdinaryNamespaceInitialAliasesAndEmptySource(t *testing.T) {
	for _, level := range []string{"home", "data", "backups", "target"} {
		t.Run(level, func(t *testing.T) {
			home, root, selected := t.TempDir(), t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			if err := Create(map[hcc.Target]ManagedPaths{hcc.TargetFactory: {Root: root}}); err != nil {
				t.Fatal(err)
			}
			if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
				t.Fatalf("empty source created namespace: %v %v", entries, err)
			}
			alias := home
			switch level {
			case "data":
				alias = filepath.Join(home, ".agent-marketplace")
			case "backups":
				alias = filepath.Join(home, ".agent-marketplace", "backups")
			case "target":
				alias = filepath.Join(home, ".agent-marketplace", "backups", "factory")
			}
			if level == "home" {
				retained := filepath.Join(t.TempDir(), "retained-empty-home")
				if err := os.Rename(home, retained); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.MkdirAll(filepath.Dir(alias), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(selected, alias); err != nil {
				t.Fatal(err)
			}
			native := filepath.Join(root, "native.txt")
			if err := os.WriteFile(native, []byte("inert initial-alias namespace archive"), 0o640); err != nil {
				t.Fatal(err)
			}
			if err := Create(map[hcc.Target]ManagedPaths{hcc.TargetFactory: {Root: root, RelPaths: []string{"native.txt"}}}); err != nil {
				t.Fatalf("supported initial namespace alias refused: %v", err)
			}
			target := filepath.Join(home, ".agent-marketplace", "backups", "factory")
			entries, err := os.ReadDir(target)
			if err != nil || len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".tar.gz") {
				t.Fatalf("initial alias did not publish actual archive: %v %v", entries, err)
			}
			timestamp := strings.TrimSuffix(entries[0].Name(), ".tar.gz")
			archive := filepath.Join(target, entries[0].Name())
			if info, err := os.Stat(archive); err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("initial-alias archive is not private: %v", err)
			}
			if err := os.WriteFile(native, []byte("inert changed initial-alias source"), 0o640); err != nil {
				t.Fatal(err)
			}
			if restored, err := Restore(hcc.TargetFactory, root); err != nil || restored != timestamp {
				t.Fatalf("initial namespace alias archive cannot really restore: %q %v", restored, err)
			}
			if raw, err := os.ReadFile(native); err != nil || string(raw) != "inert initial-alias namespace archive" {
				t.Fatalf("initial-alias namespace archive missed original bytes: %q %v", raw, err)
			}
			if link, err := os.Readlink(alias); err != nil || link != selected {
				t.Fatalf("initial namespace alias changed: %q %v", link, err)
			}
		})
	}
}
