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

// spec: backup-recovery/symbolic-restore-refuses-detached-named-parent-generation
func TestRestoreSymbolicRefusesDetachedNamedParentGeneration(t *testing.T) {
	for _, lane := range []string{"ordinary", "codex-set"} {
		t.Run(lane, func(t *testing.T) {
			home, root := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			entry := filepath.Join(root, "entry")
			if err := os.Symlink("inert-archived-parent-target", entry); err != nil {
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
			if err := os.WriteFile(entry, []byte("inert original detached-parent leaf"), 0o640); err != nil {
				t.Fatal(err)
			}
			original, err := os.Lstat(entry)
			if err != nil {
				t.Fatal(err)
			}
			originalParent, err := os.Stat(root)
			if err != nil {
				t.Fatal(err)
			}
			retained := filepath.Join(t.TempDir(), "original-parent")
			changed := false
			var foreign, foreignParent os.FileInfo
			logger := slog.Default()
			t.Cleanup(func() { slog.SetDefault(logger) })
			slog.SetDefault(slog.New(codexRestoreObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), onSymbolic: func(path string) {
				if changed {
					return
				}
				changed = true
				actual, err := os.Lstat(path)
				if err != nil || !os.SameFile(original, actual) {
					t.Fatalf("detached-parent consumer did not reach admitted leaf: %v", err)
				}
				if err := os.Rename(root, retained); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(root, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(entry, []byte("foreign inert named-parent leaf"), 0o600); err != nil {
					t.Fatal(err)
				}
				foreign, err = os.Stat(entry)
				if err != nil {
					t.Fatal(err)
				}
				foreignParent, err = os.Stat(root)
				if err != nil {
					t.Fatal(err)
				}
			}}))
			restored, err := restore()
			if raw, readErr := os.ReadFile(filepath.Join(retained, "entry")); readErr != nil || string(raw) != "inert original detached-parent leaf" {
				t.Fatalf("symbolic restore mutated detached original after parent replacement: hit=%t timestamp=%q err=%v raw=%q read=%v", changed, restored, err, raw, readErr)
			}
			if !changed || restored != "" || !errors.Is(err, fsboundary.ErrGenerationConflict) {
				t.Fatalf("symbolic detached parent did not refuse: %q %v", restored, err)
			}
			if raw, err := os.ReadFile(entry); err != nil || string(raw) != "foreign inert named-parent leaf" {
				t.Fatalf("symbolic refusal changed foreign bytes: %q %v", raw, err)
			}
			if info, err := os.Stat(entry); err != nil || !os.SameFile(foreign, info) || foreign.Mode() != info.Mode() || !foreign.ModTime().Equal(info.ModTime()) {
				t.Fatalf("foreign leaf metadata changed: %v", err)
			}
			if info, err := os.Stat(root); err != nil || !os.SameFile(foreignParent, info) || foreignParent.Mode() != info.Mode() || !foreignParent.ModTime().Equal(info.ModTime()) {
				t.Fatalf("foreign parent metadata changed: %v", err)
			}
			if info, err := os.Lstat(filepath.Join(retained, "entry")); err != nil || !os.SameFile(original, info) || original.Mode() != info.Mode() || !original.ModTime().Equal(info.ModTime()) {
				t.Fatalf("original leaf metadata changed: %v", err)
			}
			if info, err := os.Stat(retained); err != nil || !os.SameFile(originalParent, info) || originalParent.Mode() != info.Mode() || !originalParent.ModTime().Equal(info.ModTime()) {
				t.Fatalf("original parent metadata changed: %v", err)
			}
			for _, parent := range []string{root, retained} {
				if entries, err := os.ReadDir(parent); err != nil || len(entries) != 1 || entries[0].Name() != "entry" {
					t.Fatalf("symbolic refusal changed parent members: %v %v", entries, err)
				}
			}
			foreignRoot := filepath.Join(t.TempDir(), "retained-foreign-parent")
			if err := os.Rename(root, foreignRoot); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(retained, root); err != nil {
				t.Fatal(err)
			}
			restored, err = restore()
			if err != nil || restored == "" {
				t.Fatalf("healthy symbolic parent restore failed: %q %v", restored, err)
			}
			if target, err := os.Readlink(entry); err != nil || target != "inert-archived-parent-target" {
				t.Fatalf("healthy restore did not recover actual symbolic target: %q %v", target, err)
			}
			if raw, err := os.ReadFile(filepath.Join(foreignRoot, "entry")); err != nil || string(raw) != "foreign inert named-parent leaf" {
				t.Fatalf("healthy restore changed retained foreign parent: %q %v", raw, err)
			}
		})
	}
}

func TestRestoreSymbolicInitialParentAliasRetainsRecovery(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	entry := filepath.Join(root, "entry")
	if err := os.Symlink("inert-archived-alias-target", entry); err != nil {
		t.Fatal(err)
	}
	if err := Create(map[hcc.Target]ManagedPaths{hcc.TargetFactory: {Root: root, RelPaths: []string{"entry"}}}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(entry); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte("inert current alias native leaf"), 0o640); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "selected-parent")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	if restored, err := Restore(hcc.TargetFactory, alias); err != nil || restored == "" {
		t.Fatalf("supported symbolic parent alias refused: %q %v", restored, err)
	}
	if target, err := os.Readlink(entry); err != nil || target != "inert-archived-alias-target" {
		t.Fatalf("symbolic alias control did not really recover: %q %v", target, err)
	}
	if target, err := os.Readlink(alias); err != nil || target != root {
		t.Fatalf("symbolic recovery changed initial parent alias: %q %v", target, err)
	}
}
