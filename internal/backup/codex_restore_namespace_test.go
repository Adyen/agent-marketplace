package backup

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

// spec: backup-recovery/codex-restore-namespace-retains-original-root-authority
func TestRestoreCodexNamespaceRetainsOriginalRootAuthority(t *testing.T) {
	for _, layout := range []string{"absent-root", "absent-nested-root", "existing-root"} {
		for _, replacement := range []string{"alias", "directory"} {
			t.Run(layout+"/"+replacement, func(t *testing.T) {
				home, parent, codex, foreign := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
				t.Setenv("HOME", home)
				agents := filepath.Join(parent, "native")
				if layout == "absent-nested-root" {
					agents = filepath.Join(parent, "nested", "native")
				}
				if err := os.MkdirAll(agents, 0o750); err != nil {
					t.Fatal(err)
				}
				agentsFile, codexFile := filepath.Join(agents, "agents.txt"), filepath.Join(codex, "config.txt")
				if err := os.WriteFile(agentsFile, []byte("inert archived restore namespace agents"), 0o640); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(codexFile, []byte("inert archived restore namespace codex"), 0o600); err != nil {
					t.Fatal(err)
				}
				partitions := []CodexPartition{{ID: "machine-agents", Root: agents, RelPaths: []string{"agents.txt"}}, {ID: "machine-codex", Root: codex, RelPaths: []string{"config.txt"}}}
				operation, err := CreateCodexSet(partitions)
				if err != nil {
					t.Fatal(err)
				}
				selected, expectedOriginalMembers := parent, 0
				var currentAgents os.FileInfo
				if layout == "existing-root" {
					selected, expectedOriginalMembers = agents, 1
					if err := os.WriteFile(agentsFile, []byte("inert current preserved agents"), 0o640); err != nil {
						t.Fatal(err)
					}
					currentAgents, err = os.Stat(agentsFile)
					if err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.Remove(agentsFile); err != nil {
						t.Fatal(err)
					}
					if err := os.Remove(agents); err != nil {
						t.Fatal(err)
					}
					if layout == "absent-nested-root" {
						if err := os.Remove(filepath.Dir(agents)); err != nil {
							t.Fatal(err)
						}
					}
				}
				originalParent, err := os.Stat(selected)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(codexFile, []byte("inert current unaffected codex"), 0o600); err != nil {
					t.Fatal(err)
				}
				currentCodex, err := os.Stat(codexFile)
				if err != nil {
					t.Fatal(err)
				}
				foreignFile := filepath.Join(foreign, "foreign.txt")
				if err := os.WriteFile(foreignFile, []byte("foreign inert restore namespace bytes"), 0o640); err != nil {
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
				target := filepath.Join(home, ".agent-marketplace", "backups", "codex")
				originalRaw := make(map[string][]byte)
				originalInfo := make(map[string]os.FileInfo)
				entries, err := os.ReadDir(target)
				if err != nil || len(entries) != 3 {
					t.Fatalf("real complete restore evidence missing: %v %v", entries, err)
				}
				for _, entry := range entries {
					name := entry.Name()
					originalRaw[name], err = os.ReadFile(filepath.Join(target, name))
					if err != nil {
						t.Fatal(err)
					}
					originalInfo[name], err = os.Stat(filepath.Join(target, name))
					if err != nil {
						t.Fatal(err)
					}
				}
				retained := filepath.Join(t.TempDir(), "retained-original-restore-parent")
				changed := false
				logger := slog.Default()
				t.Cleanup(func() { slog.SetDefault(logger) })
				slog.SetDefault(slog.New(codexRestoreObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), onRestoreNamespace: func(path string) {
					if changed {
						return
					}
					changed = true
					observed, err := os.Stat(path)
					if err != nil || !os.SameFile(originalParent, observed) {
						t.Fatalf("Codex restore namespace did not reach original root/parent: %q %v", path, err)
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
				restored, err := RestoreCodexSet(partitions)
				foreignPath := foreign
				if replacement == "directory" {
					foreignPath = selected
				}
				if entries, readErr := os.ReadDir(foreignPath); readErr != nil || len(entries) != 1 || entries[0].Name() != "foreign.txt" {
					t.Fatalf("Codex restore created foreign root/native bytes: hit=%t restored=%q err=%v entries=%v read=%v", changed, restored, err, entries, readErr)
				}
				if !changed || restored != "" || !errors.Is(err, fsboundary.ErrGenerationConflict) {
					t.Fatalf("Codex restore namespace replacement did not generation-refuse: %q %v", restored, err)
				}
				if entries, err := os.ReadDir(retained); err != nil || len(entries) != expectedOriginalMembers {
					t.Fatalf("refused restore changed original namespace/stage membership: %v %v", entries, err)
				}
				if raw, err := os.ReadFile(filepath.Join(foreignPath, "foreign.txt")); err != nil || string(raw) != "foreign inert restore namespace bytes" {
					t.Fatalf("foreign namespace bytes changed: %q %v", raw, err)
				}
				if info, err := os.Stat(filepath.Join(foreignPath, "foreign.txt")); err != nil || !os.SameFile(foreignInfo, info) || foreignInfo.Mode() != info.Mode() || !foreignInfo.ModTime().Equal(info.ModTime()) {
					t.Fatalf("foreign file metadata changed: %v", err)
				}
				if info, err := os.Stat(foreignPath); err != nil || !os.SameFile(foreignDirectory, info) || foreignDirectory.Mode() != info.Mode() || !foreignDirectory.ModTime().Equal(info.ModTime()) {
					t.Fatalf("foreign namespace metadata changed: %v", err)
				}
				if info, err := os.Stat(retained); err != nil || !os.SameFile(originalParent, info) || originalParent.Mode() != info.Mode() || !originalParent.ModTime().Equal(info.ModTime()) {
					t.Fatalf("original namespace metadata changed: %v", err)
				}
				if currentAgents != nil {
					if raw, err := os.ReadFile(filepath.Join(retained, "agents.txt")); err != nil || string(raw) != "inert current preserved agents" {
						t.Fatalf("original agents bytes changed: %q %v", raw, err)
					}
					if info, err := os.Stat(filepath.Join(retained, "agents.txt")); err != nil || !os.SameFile(currentAgents, info) || currentAgents.Mode() != info.Mode() || !currentAgents.ModTime().Equal(info.ModTime()) {
						t.Fatalf("original agents metadata changed: %v", err)
					}
				}
				if raw, err := os.ReadFile(codexFile); err != nil || string(raw) != "inert current unaffected codex" {
					t.Fatalf("later partition bytes changed on namespace refusal: %q %v", raw, err)
				}
				if info, err := os.Stat(codexFile); err != nil || !os.SameFile(currentCodex, info) || currentCodex.Mode() != info.Mode() || !currentCodex.ModTime().Equal(info.ModTime()) {
					t.Fatalf("later partition metadata changed: %v", err)
				}
				for name, raw := range originalRaw {
					if observed, err := os.ReadFile(filepath.Join(target, name)); err != nil || !bytes.Equal(observed, raw) {
						t.Fatalf("original archive/manifest evidence bytes changed: %q %v", name, err)
					}
					if info, err := os.Stat(filepath.Join(target, name)); err != nil || !os.SameFile(originalInfo[name], info) || originalInfo[name].Mode() != info.Mode() || !originalInfo[name].ModTime().Equal(info.ModTime()) {
						t.Fatalf("original evidence metadata changed: %q %v", name, err)
					}
				}
				if replacement == "alias" {
					if link, err := os.Readlink(selected); err != nil || link != foreign {
						t.Fatalf("foreign root/parent alias changed: %q %v", link, err)
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
				if restored, err := RestoreCodexSet(partitions); err != nil || restored != operation {
					t.Fatalf("restored original namespace cannot really recover: %q %v", restored, err)
				}
				if raw, err := os.ReadFile(agentsFile); err != nil || string(raw) != "inert archived restore namespace agents" {
					t.Fatalf("genuine root agents recovery missed bytes: %q %v", raw, err)
				}
				if raw, err := os.ReadFile(codexFile); err != nil || string(raw) != "inert archived restore namespace codex" {
					t.Fatalf("genuine codex recovery missed bytes: %q %v", raw, err)
				}
			})
		}
	}
}

func TestRestoreCodexNamespaceInitialParentAliasAndRootShape(t *testing.T) {
	for _, absent := range []bool{false, true} {
		t.Run(fmt.Sprint(absent), func(t *testing.T) {
			home, actual, codex, selected := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			alias := filepath.Join(selected, "parent")
			if err := os.Symlink(actual, alias); err != nil {
				t.Fatal(err)
			}
			agents := filepath.Join(alias, "native")
			if err := os.Mkdir(agents, 0o750); err != nil {
				t.Fatal(err)
			}
			agentsFile, codexFile := filepath.Join(agents, "agents.txt"), filepath.Join(codex, "config.txt")
			if err := os.WriteFile(agentsFile, []byte("inert initial-parent agents archive"), 0o640); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(codexFile, []byte("inert initial-parent codex archive"), 0o600); err != nil {
				t.Fatal(err)
			}
			partitions := []CodexPartition{{ID: "machine-agents", Root: agents, RelPaths: []string{"agents.txt"}}, {ID: "machine-codex", Root: codex, RelPaths: []string{"config.txt"}}}
			operation, err := CreateCodexSet(partitions)
			if err != nil {
				t.Fatal(err)
			}
			if absent {
				if err := os.Remove(agentsFile); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(agents); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(agentsFile, []byte("inert changed initial-parent agents"), 0o640); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(codexFile, []byte("inert changed initial-parent codex"), 0o600); err != nil {
				t.Fatal(err)
			}
			if restored, err := RestoreCodexSet(partitions); err != nil || restored != operation {
				t.Fatalf("supported initial parent alias/root creation refused: %q %v", restored, err)
			}
			if raw, err := os.ReadFile(agentsFile); err != nil || string(raw) != "inert initial-parent agents archive" {
				t.Fatalf("initial-parent agents recovery missed bytes: %q %v", raw, err)
			}
			if raw, err := os.ReadFile(codexFile); err != nil || string(raw) != "inert initial-parent codex archive" {
				t.Fatalf("initial-parent codex recovery missed bytes: %q %v", raw, err)
			}
			if link, err := os.Readlink(alias); err != nil || link != actual {
				t.Fatalf("initial restore parent alias changed: %q %v", link, err)
			}
			if err := os.Remove(agentsFile); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(agents); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(agents, []byte("inert non-directory root"), 0o600); err != nil {
				t.Fatal(err)
			}
			if restored, err := RestoreCodexSet(partitions); err == nil || restored != "" {
				t.Fatalf("non-directory restore root unexpectedly admitted: %q %v", restored, err)
			}
			if raw, err := os.ReadFile(agents); err != nil || string(raw) != "inert non-directory root" {
				t.Fatalf("unsafe root refusal changed bytes: %q %v", raw, err)
			}
		})
	}
}
