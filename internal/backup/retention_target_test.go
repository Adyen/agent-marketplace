package backup

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// spec: backup-recovery/retention-refuses-replaced-target-directory-generations
func TestRotateRefusesReplacedTargetDirectoryGeneration(t *testing.T) {
	for _, lane := range []string{"ordinary", "codex-set"} {
		for _, replacement := range []string{"directory", "alias"} {
			t.Run(lane+"/"+replacement, func(t *testing.T) {
				home, root := t.TempDir(), t.TempDir()
				t.Setenv("HOME", home)
				native := filepath.Join(root, "native.txt")
				if err := os.WriteFile(native, []byte("inert original retained archive bytes"), 0o640); err != nil {
					t.Fatal(err)
				}
				create := func(root, home string) error {
					if lane == "ordinary" {
						return Create(map[hcc.Target]ManagedPaths{hcc.TargetFactory: {Root: root, RelPaths: []string{"native.txt"}}})
					}
					_, err := CreateCodexSet([]CodexPartition{{ID: "machine-agents", Root: root, RelPaths: []string{"native.txt"}}, {ID: "machine-codex", Root: filepath.Join(home, ".codex")}})
					return err
				}
				for range 2 {
					if err := create(root, home); err != nil {
						t.Fatal(err)
					}
				}
				targetName, targetKey, expectedMembers := "factory", hcc.TargetFactory, 2
				restore := func() (string, error) { return Restore(hcc.TargetFactory, root) }
				if lane == "codex-set" {
					targetName, targetKey, expectedMembers = "codex", hcc.TargetCodex, 6
					restore = func() (string, error) {
						return RestoreCodexSet([]CodexPartition{{ID: "machine-agents", Root: root, RelPaths: []string{"native.txt"}}, {ID: "machine-codex", Root: filepath.Join(home, ".codex")}})
					}
				}
				target := filepath.Join(home, ".agent-marketplace", "backups", targetName)
				originalParent, err := os.Stat(target)
				if err != nil {
					t.Fatal(err)
				}
				originalRaw := make(map[string][]byte)
				originalInfo := make(map[string]os.FileInfo)
				entries, err := os.ReadDir(target)
				if err != nil || len(entries) != expectedMembers {
					t.Fatalf("real original retention fixture missing: %v %v", entries, err)
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
				before, err := ListBackups()
				if err != nil || len(before[targetKey]) != 2 {
					t.Fatalf("genuine complete retention inputs missing: %v %v", before, err)
				}
				foreignHome, foreignRoot := t.TempDir(), t.TempDir()
				t.Setenv("HOME", foreignHome)
				if err := os.WriteFile(filepath.Join(foreignRoot, "native.txt"), []byte("foreign inert retention archive bytes"), 0o640); err != nil {
					t.Fatal(err)
				}
				for range 2 {
					if err := create(foreignRoot, foreignHome); err != nil {
						t.Fatal(err)
					}
				}
				foreign := filepath.Join(foreignHome, ".agent-marketplace", "backups", targetName)
				foreignParent, err := os.Stat(foreign)
				if err != nil {
					t.Fatal(err)
				}
				foreignRaw := make(map[string][]byte)
				foreignInfo := make(map[string]os.FileInfo)
				entries, err = os.ReadDir(foreign)
				if err != nil || len(entries) != expectedMembers {
					t.Fatalf("real foreign retention fixture missing: %v %v", entries, err)
				}
				for _, entry := range entries {
					name := entry.Name()
					foreignRaw[name], err = os.ReadFile(filepath.Join(foreign, name))
					if err != nil {
						t.Fatal(err)
					}
					foreignInfo[name], err = os.Stat(filepath.Join(foreign, name))
					if err != nil {
						t.Fatal(err)
					}
				}
				t.Setenv("HOME", home)
				currentNative, err := os.Stat(native)
				if err != nil {
					t.Fatal(err)
				}
				retained := filepath.Join(t.TempDir(), "retained-original-backup-target")
				changed := false
				logger := slog.Default()
				t.Cleanup(func() { slog.SetDefault(logger) })
				slog.SetDefault(slog.New(codexRestoreObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), onRetentionTarget: func(path string) {
					if changed {
						return
					}
					changed = true
					observed, err := os.Stat(path)
					if err != nil || !os.SameFile(originalParent, observed) {
						t.Fatalf("retention did not reach real original target: %q %v", path, err)
					}
					if err := os.Rename(target, retained); err != nil {
						t.Fatal(err)
					}
					if replacement == "alias" {
						err = os.Symlink(foreign, target)
					} else {
						err = os.Rename(foreign, target)
					}
					if err != nil {
						t.Fatal(err)
					}
				}}))
				err = Rotate(Config{MaxDays: 1, MaxPerDay: 1})
				foreignPath := target
				if replacement == "alias" {
					foreignPath = foreign
				}
				for name, expected := range foreignRaw {
					if raw, readErr := os.ReadFile(filepath.Join(foreignPath, name)); readErr != nil || !bytes.Equal(raw, expected) {
						t.Fatalf("retention deleted foreign archive after target selection: hit=%t err=%v name=%q raw=%q read=%v", changed, err, name, raw, readErr)
					}
				}
				if !changed || !errors.Is(err, fsboundary.ErrGenerationConflict) {
					t.Fatalf("retention target replacement did not generation-refuse: %v", err)
				}
				for name, expected := range originalRaw {
					if raw, err := os.ReadFile(filepath.Join(retained, name)); err != nil || !bytes.Equal(raw, expected) {
						t.Fatalf("original archive bytes changed: %q %v", name, err)
					}
					if info, err := os.Stat(filepath.Join(retained, name)); err != nil || !os.SameFile(originalInfo[name], info) || originalInfo[name].Mode() != info.Mode() || !originalInfo[name].ModTime().Equal(info.ModTime()) {
						t.Fatalf("original archive metadata changed: %q %v", name, err)
					}
				}
				for name, expected := range foreignInfo {
					if info, err := os.Stat(filepath.Join(foreignPath, name)); err != nil || !os.SameFile(expected, info) || expected.Mode() != info.Mode() || !expected.ModTime().Equal(info.ModTime()) {
						t.Fatalf("foreign archive metadata changed: %q %v", name, err)
					}
				}
				if info, err := os.Stat(retained); err != nil || !os.SameFile(originalParent, info) || originalParent.Mode() != info.Mode() || !originalParent.ModTime().Equal(info.ModTime()) {
					t.Fatalf("original target metadata changed: %v", err)
				}
				if info, err := os.Stat(foreignPath); err != nil || !os.SameFile(foreignParent, info) || foreignParent.Mode() != info.Mode() || !foreignParent.ModTime().Equal(info.ModTime()) {
					t.Fatalf("foreign target metadata changed: %v", err)
				}
				if entries, err := os.ReadDir(retained); err != nil || len(entries) != expectedMembers {
					t.Fatalf("original membership changed: %v %v", entries, err)
				}
				if entries, err := os.ReadDir(foreignPath); err != nil || len(entries) != expectedMembers {
					t.Fatalf("foreign membership changed: %v %v", entries, err)
				}
				if raw, err := os.ReadFile(native); err != nil || string(raw) != "inert original retained archive bytes" {
					t.Fatalf("retention mutated original native bytes: %q %v", raw, err)
				}
				if info, err := os.Stat(native); err != nil || !os.SameFile(currentNative, info) || currentNative.Mode() != info.Mode() || !currentNative.ModTime().Equal(info.ModTime()) {
					t.Fatalf("native metadata changed: %v", err)
				}
				if replacement == "alias" {
					if link, err := os.Readlink(target); err != nil || link != foreign {
						t.Fatalf("foreign retention alias changed: %q %v", link, err)
					}
					if err := os.Remove(target); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.Rename(target, foreign); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Rename(retained, target); err != nil {
					t.Fatal(err)
				}
				if err := Rotate(Config{MaxDays: 1, MaxPerDay: 1}); err != nil {
					t.Fatalf("original retention healthy retry refused: %v", err)
				}
				listed, err := ListBackups()
				if err != nil || len(listed[targetKey]) != 1 || listed[targetKey][0] != before[targetKey][1] {
					t.Fatalf("real retention did not leave newest actual archive: %v %v", listed, err)
				}
				if err := os.WriteFile(native, []byte("inert changed native bytes after rotation"), 0o640); err != nil {
					t.Fatal(err)
				}
				if restored, err := restore(); err != nil || restored != listed[targetKey][0] {
					t.Fatalf("retained original archive cannot really restore: %q %v", restored, err)
				}
				if raw, err := os.ReadFile(native); err != nil || string(raw) != "inert original retained archive bytes" {
					t.Fatalf("retained archive did not genuinely recover: %q %v", raw, err)
				}
			})
		}
	}
}

func TestRotateInitialBackupRootAliasRetainsHealthyRetention(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	if err := Rotate(Config{MaxDays: 1, MaxPerDay: 1}); err != nil {
		t.Fatalf("optional absent retention root refused: %v", err)
	}
	native := filepath.Join(root, "native.txt")
	if err := os.WriteFile(native, []byte("inert initial-alias retained archive"), 0o640); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := Create(map[hcc.Target]ManagedPaths{hcc.TargetFactory: {Root: root, RelPaths: []string{"native.txt"}}}); err != nil {
			t.Fatal(err)
		}
	}
	backups := filepath.Join(home, ".agent-marketplace", "backups")
	retained := filepath.Join(t.TempDir(), "retained-initial-backup-root")
	if err := os.Rename(backups, retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(retained, backups); err != nil {
		t.Fatal(err)
	}
	if err := Rotate(Config{MaxDays: 1, MaxPerDay: 1}); err != nil {
		t.Fatalf("supported initial retention root alias refused: %v", err)
	}
	listed, err := ListBackups()
	if err != nil || len(listed[hcc.TargetFactory]) != 1 {
		t.Fatalf("initial alias did not actually rotate: %v %v", listed, err)
	}
	if err := os.WriteFile(native, []byte("inert changed initial-alias native bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	if restored, err := Restore(hcc.TargetFactory, root); err != nil || restored != listed[hcc.TargetFactory][0] {
		t.Fatalf("initial-alias retained archive cannot restore: %q %v", restored, err)
	}
	if raw, err := os.ReadFile(native); err != nil || string(raw) != "inert initial-alias retained archive" {
		t.Fatalf("initial-alias actual recovery missed bytes: %q %v", raw, err)
	}
	if link, err := os.Readlink(backups); err != nil || link != retained {
		t.Fatalf("initial retention root alias changed: %q %v", link, err)
	}
}
