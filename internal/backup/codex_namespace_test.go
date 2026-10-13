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

// spec: backup-recovery/codex-backup-namespace-refuses-retargeted-parent
func TestCreateCodexNamespaceRefusesRetargetedParent(t *testing.T) {
	for _, level := range []string{"home", "data", "backups", "target"} {
		for _, replacement := range []string{"alias", "directory"} {
			t.Run(level+"/"+replacement, func(t *testing.T) {
				home, agents, codex, foreign := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
				t.Setenv("HOME", home)
				agentsFile, codexFile := filepath.Join(agents, "agents.txt"), filepath.Join(codex, "config.txt")
				if err := os.WriteFile(agentsFile, []byte("inert original agents partition"), 0o640); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(codexFile, []byte("inert original codex partition"), 0o600); err != nil {
					t.Fatal(err)
				}
				agentsInfo, err := os.Stat(agentsFile)
				if err != nil {
					t.Fatal(err)
				}
				codexInfo, err := os.Stat(codexFile)
				if err != nil {
					t.Fatal(err)
				}
				partitions := []CodexPartition{{ID: "machine-agents", Root: agents, RelPaths: []string{"agents.txt"}}, {ID: "machine-codex", Root: codex, RelPaths: []string{"config.txt"}}}
				selected := home
				switch level {
				case "data":
					selected = filepath.Join(home, ".agent-marketplace")
				case "backups":
					selected = filepath.Join(home, ".agent-marketplace", "backups")
				case "target":
					selected = filepath.Join(home, ".agent-marketplace", "backups", "codex")
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
				foreignFile := filepath.Join(foreign, "foreign.txt")
				if err := os.WriteFile(foreignFile, []byte("foreign inert Codex namespace bytes"), 0o640); err != nil {
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
				retained := filepath.Join(t.TempDir(), "retained-original-codex-parent")
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
						t.Fatalf("Codex namespace did not reach real selected parent: %q %v", path, err)
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
				operation, err := CreateCodexSet(partitions)
				foreignPath := foreign
				if replacement == "directory" {
					foreignPath = selected
				}
				if entries, readErr := os.ReadDir(foreignPath); readErr != nil || len(entries) != 1 || entries[0].Name() != "foreign.txt" {
					t.Fatalf("Codex created foreign namespace/archive/set: hit=%t op=%q err=%v entries=%v read=%v", changed, operation, err, entries, readErr)
				}
				if !changed || operation != "" || !errors.Is(err, fsboundary.ErrGenerationConflict) {
					t.Fatalf("Codex namespace did not generation-refuse: %q %v", operation, err)
				}
				if entries, err := os.ReadDir(retained); err != nil || len(entries) != 0 {
					t.Fatalf("Codex refusal left directory/archive/manifest residue: %v %v", entries, err)
				}
				if raw, err := os.ReadFile(filepath.Join(foreignPath, "foreign.txt")); err != nil || string(raw) != "foreign inert Codex namespace bytes" {
					t.Fatalf("foreign namespace bytes changed: %q %v", raw, err)
				}
				if info, err := os.Stat(filepath.Join(foreignPath, "foreign.txt")); err != nil || !os.SameFile(foreignInfo, info) || foreignInfo.Mode() != info.Mode() || !foreignInfo.ModTime().Equal(info.ModTime()) {
					t.Fatalf("foreign file metadata changed: %v", err)
				}
				if info, err := os.Stat(foreignPath); err != nil || !os.SameFile(foreignDirectory, info) || foreignDirectory.Mode() != info.Mode() || !foreignDirectory.ModTime().Equal(info.ModTime()) {
					t.Fatalf("foreign namespace metadata changed: %v", err)
				}
				if info, err := os.Stat(retained); err != nil || !os.SameFile(original, info) || original.Mode() != info.Mode() || !original.ModTime().Equal(info.ModTime()) {
					t.Fatalf("original namespace metadata changed: %v", err)
				}
				for _, item := range []struct {
					path, bytes string
					info        os.FileInfo
				}{{agentsFile, "inert original agents partition", agentsInfo}, {codexFile, "inert original codex partition", codexInfo}} {
					if raw, err := os.ReadFile(item.path); err != nil || string(raw) != item.bytes {
						t.Fatalf("partition source bytes changed: %q %v", raw, err)
					}
					if info, err := os.Stat(item.path); err != nil || !os.SameFile(item.info, info) || item.info.Mode() != info.Mode() || !item.info.ModTime().Equal(info.ModTime()) {
						t.Fatalf("partition source metadata changed: %v", err)
					}
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
				operation, err = CreateCodexSet(partitions)
				if err != nil || operation == "" {
					t.Fatalf("genuine Codex namespace retry refused: %q %v", operation, err)
				}
				listed, err := ListBackups()
				if err != nil || len(listed[hcc.TargetCodex]) != 1 || listed[hcc.TargetCodex][0] != operation {
					t.Fatalf("genuine complete Codex set missing: %v %v", listed, err)
				}
				if err := os.WriteFile(agentsFile, []byte("inert changed agents bytes"), 0o640); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(codexFile, []byte("inert changed codex bytes"), 0o600); err != nil {
					t.Fatal(err)
				}
				if restored, err := RestoreCodexSet(partitions); err != nil || restored != operation {
					t.Fatalf("genuine namespace set cannot really restore: %q %v", restored, err)
				}
				if raw, err := os.ReadFile(agentsFile); err != nil || string(raw) != "inert original agents partition" {
					t.Fatalf("agents partition not genuinely recovered: %q %v", raw, err)
				}
				if raw, err := os.ReadFile(codexFile); err != nil || string(raw) != "inert original codex partition" {
					t.Fatalf("codex partition not genuinely recovered: %q %v", raw, err)
				}
			})
		}
	}
}

func TestCreateCodexNamespaceInitialAliasesAndEmptyPartitions(t *testing.T) {
	for _, level := range []string{"home", "data", "backups", "target"} {
		t.Run(level, func(t *testing.T) {
			home, agents, codex, selected := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			alias := home
			switch level {
			case "data":
				alias = filepath.Join(home, ".agent-marketplace")
			case "backups":
				alias = filepath.Join(home, ".agent-marketplace", "backups")
			case "target":
				alias = filepath.Join(home, ".agent-marketplace", "backups", "codex")
			}
			if level == "home" {
				retained := filepath.Join(t.TempDir(), "retained-initial-home")
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
			agentsFile, codexFile := filepath.Join(agents, "agents.txt"), filepath.Join(codex, "config.txt")
			if err := os.WriteFile(agentsFile, []byte("inert initial-alias agents bytes"), 0o640); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(codexFile, []byte("inert initial-alias codex bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			partitions := []CodexPartition{{ID: "machine-agents", Root: agents, RelPaths: []string{"agents.txt"}}, {ID: "machine-codex", Root: codex, RelPaths: []string{"config.txt"}}}
			operation, err := CreateCodexSet(partitions)
			if err != nil || operation == "" {
				t.Fatalf("initial Codex namespace alias refused: %q %v", operation, err)
			}
			target := filepath.Join(home, ".agent-marketplace", "backups", "codex")
			entries, err := os.ReadDir(target)
			if err != nil || len(entries) != 3 {
				t.Fatalf("initial alias did not really create complete two-partition set: %v %v", entries, err)
			}
			for _, entry := range entries {
				mode := os.FileMode(0o600)
				if entry.Name() == operation+".set.json" {
					mode = 0o644
				}
				if info, err := os.Stat(filepath.Join(target, entry.Name())); err != nil || info.Mode().Perm() != mode {
					t.Fatalf("initial-alias set member mode changed: %q %v", entry.Name(), err)
				}
			}
			if err := os.WriteFile(agentsFile, []byte("inert changed initial-alias agents"), 0o640); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(codexFile, []byte("inert changed initial-alias codex"), 0o600); err != nil {
				t.Fatal(err)
			}
			if restored, err := RestoreCodexSet(partitions); err != nil || restored != operation {
				t.Fatalf("initial Codex namespace alias cannot restore set: %q %v", restored, err)
			}
			if raw, err := os.ReadFile(agentsFile); err != nil || string(raw) != "inert initial-alias agents bytes" {
				t.Fatalf("initial-alias agents recovery missed bytes: %q %v", raw, err)
			}
			if raw, err := os.ReadFile(codexFile); err != nil || string(raw) != "inert initial-alias codex bytes" {
				t.Fatalf("initial-alias codex recovery missed bytes: %q %v", raw, err)
			}
			if link, err := os.Readlink(alias); err != nil || link != selected {
				t.Fatalf("initial Codex namespace alias changed: %q %v", link, err)
			}
		})
	}
	t.Run("empty-partitions", func(t *testing.T) {
		home, agents, codex := t.TempDir(), t.TempDir(), t.TempDir()
		t.Setenv("HOME", home)
		partitions := []CodexPartition{{ID: "machine-agents", Root: agents}, {ID: "machine-codex", Root: codex}}
		operation, err := CreateCodexSet(partitions)
		if err != nil || operation == "" {
			t.Fatalf("empty Codex set contract changed: %q %v", operation, err)
		}
		listed, err := ListBackups()
		if err != nil || len(listed[hcc.TargetCodex]) != 1 || listed[hcc.TargetCodex][0] != operation {
			t.Fatalf("genuine empty two-partition set not listed: %v %v", listed, err)
		}
		if restored, err := RestoreCodexSet(partitions); err != nil || restored != operation {
			t.Fatalf("genuine empty set cannot restore: %q %v", restored, err)
		}
	})
}
