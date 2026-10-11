package backup

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// spec: backup-recovery/backup-listing-refuses-replaced-target-directory-generation
func TestListBackupsRefusesReplacedTargetDirectoryGeneration(t *testing.T) {
	for _, lane := range []string{"ordinary", "codex-set"} {
		for _, replacement := range []string{"alias", "directory"} {
			t.Run(lane+"/"+replacement, func(t *testing.T) {
				home, root := t.TempDir(), t.TempDir()
				t.Setenv("HOME", home)
				native := filepath.Join(root, "native.txt")
				if err := os.WriteFile(native, []byte("inert archived listing control"), 0o640); err != nil {
					t.Fatal(err)
				}
				targetName, targetKey := "factory", hcc.TargetFactory
				restore := func() (string, error) { return Restore(hcc.TargetFactory, root) }
				partitions := []CodexPartition{{ID: "machine-agents", Root: root, RelPaths: []string{"native.txt"}}, {ID: "machine-codex", Root: filepath.Join(home, ".codex")}}
				if lane == "ordinary" {
					if err := Create(map[hcc.Target]ManagedPaths{hcc.TargetFactory: {Root: root, RelPaths: []string{"native.txt"}}}); err != nil {
						t.Fatal(err)
					}
				} else {
					if _, err := CreateCodexSet(partitions); err != nil {
						t.Fatal(err)
					}
					targetName, targetKey = "codex", hcc.TargetCodex
					restore = func() (string, error) { return RestoreCodexSet(partitions) }
				}
				before, err := ListBackups()
				if err != nil || len(before[targetKey]) != 1 {
					t.Fatalf("real initial backup missing: %v %v", before, err)
				}
				target := filepath.Join(home, ".agent-marketplace", "backups", targetName)
				original, err := os.Stat(target)
				if err != nil {
					t.Fatal(err)
				}
				originalRaw := make(map[string][]byte)
				originalInfo := make(map[string]os.FileInfo)
				entries, err := os.ReadDir(target)
				if err != nil {
					t.Fatal(err)
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
				retained := filepath.Join(t.TempDir(), "retained-original-target")
				foreign := t.TempDir()
				if lane == "ordinary" {
					if err := os.WriteFile(filepath.Join(foreign, "2999-01-01-000000.tar.gz"), []byte("foreign inert filename-only listing member"), 0o600); err != nil {
						t.Fatal(err)
					}
				} else {
					foreignHome, foreignRoot := t.TempDir(), t.TempDir()
					t.Setenv("HOME", foreignHome)
					if err := os.WriteFile(filepath.Join(foreignRoot, "native.txt"), []byte("foreign inert complete Codex archive"), 0o640); err != nil {
						t.Fatal(err)
					}
					foreignPartitions := []CodexPartition{{ID: "machine-agents", Root: foreignRoot, RelPaths: []string{"native.txt"}}, {ID: "machine-codex", Root: filepath.Join(foreignHome, ".codex")}}
					for range 2 {
						if _, err := CreateCodexSet(foreignPartitions); err != nil {
							t.Fatal(err)
						}
					}
					foreign = filepath.Join(foreignHome, ".agent-marketplace", "backups", "codex")
					t.Setenv("HOME", home)
				}
				foreignRaw := make(map[string][]byte)
				foreignInfo := make(map[string]os.FileInfo)
				entries, err = os.ReadDir(foreign)
				if err != nil {
					t.Fatal(err)
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
				foreignDirectory, err := os.Stat(foreign)
				if err != nil {
					t.Fatal(err)
				}
				changed := false
				logger := slog.Default()
				t.Cleanup(func() { slog.SetDefault(logger) })
				slog.SetDefault(slog.New(codexRestoreObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), onListingTarget: func(path string) {
					if changed {
						return
					}
					changed = true
					observed, err := os.Stat(path)
					if err != nil || !os.SameFile(original, observed) {
						t.Fatalf("listing did not reach admitted actual target: %q %v", path, err)
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
				listed, err := ListBackups()
				if err == nil {
					t.Fatalf("backup listing admitted foreign archive names after target replacement: hit=%t listed=%v", changed, listed)
				}
				if !changed || listed != nil || !errors.Is(err, fsboundary.ErrGenerationConflict) {
					t.Fatalf("listing replacement did not generation-refuse: hit=%t %v %v", changed, listed, err)
				}
				foreignPath := foreign
				if replacement == "alias" {
					if link, err := os.Readlink(target); err != nil || link != foreign {
						t.Fatalf("listing changed foreign alias: %q %v", link, err)
					}
				} else {
					foreignPath = target
				}
				for name, expected := range foreignRaw {
					if raw, err := os.ReadFile(filepath.Join(foreignPath, name)); err != nil || !bytes.Equal(raw, expected) {
						t.Fatalf("foreign listing bytes changed: %q %v", name, err)
					}
					if info, err := os.Stat(filepath.Join(foreignPath, name)); err != nil || !os.SameFile(foreignInfo[name], info) || foreignInfo[name].Mode() != info.Mode() || !foreignInfo[name].ModTime().Equal(info.ModTime()) {
						t.Fatalf("foreign listing member metadata changed: %q %v", name, err)
					}
				}
				if info, err := os.Stat(foreignPath); err != nil || !os.SameFile(foreignDirectory, info) || foreignDirectory.Mode() != info.Mode() || !foreignDirectory.ModTime().Equal(info.ModTime()) {
					t.Fatalf("foreign listing directory metadata changed: %v", err)
				}
				for name, expected := range originalRaw {
					if raw, err := os.ReadFile(filepath.Join(retained, name)); err != nil || !bytes.Equal(raw, expected) {
						t.Fatalf("original listing member bytes changed: %q %v", name, err)
					}
					if info, err := os.Stat(filepath.Join(retained, name)); err != nil || !os.SameFile(originalInfo[name], info) || originalInfo[name].Mode() != info.Mode() || !originalInfo[name].ModTime().Equal(info.ModTime()) {
						t.Fatalf("original listing member metadata changed: %q %v", name, err)
					}
				}
				if info, err := os.Stat(retained); err != nil || !os.SameFile(original, info) || original.Mode() != info.Mode() || !original.ModTime().Equal(info.ModTime()) {
					t.Fatalf("original listing directory metadata changed: %v", err)
				}
				if entries, err := os.ReadDir(retained); err != nil || len(entries) != len(originalRaw) {
					t.Fatalf("original listing membership changed: %v %v", entries, err)
				}
				if entries, err := os.ReadDir(foreignPath); err != nil || len(entries) != len(foreignRaw) {
					t.Fatalf("foreign listing membership changed: %v %v", entries, err)
				}
				if replacement == "alias" {
					err = os.Remove(target)
				} else {
					err = os.Rename(target, foreign)
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(retained, target); err != nil {
					t.Fatal(err)
				}
				listed, err = ListBackups()
				if err != nil || len(listed[targetKey]) != 1 || listed[targetKey][0] != before[targetKey][0] {
					t.Fatalf("healthy real listing not recovered: %v %v", listed, err)
				}
				if err := os.WriteFile(native, []byte("inert changed listing control"), 0o640); err != nil {
					t.Fatal(err)
				}
				if restored, err := restore(); err != nil || restored != before[targetKey][0] {
					t.Fatalf("listed real backup cannot recover: %q %v", restored, err)
				}
				if raw, err := os.ReadFile(native); err != nil || string(raw) != "inert archived listing control" {
					t.Fatalf("listing healthy control did not really recover archive: %q %v", raw, err)
				}
			})
		}
	}
}

func TestListBackupsInitialRootAliasAndOptionalAbsenceRetainRecovery(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	if listed, err := ListBackups(); err != nil || len(listed) != 0 {
		t.Fatalf("optional missing root changed: %v %v", listed, err)
	}
	native := filepath.Join(root, "native.txt")
	if err := os.WriteFile(native, []byte("inert root-alias listing archive"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := Create(map[hcc.Target]ManagedPaths{hcc.TargetFactory: {Root: root, RelPaths: []string{"native.txt"}}}); err != nil {
		t.Fatal(err)
	}
	before, err := ListBackups()
	if err != nil || len(before[hcc.TargetFactory]) != 1 {
		t.Fatalf("healthy listing fixture missing: %v %v", before, err)
	}
	backups := filepath.Join(home, ".agent-marketplace", "backups")
	retained := filepath.Join(t.TempDir(), "retained-backup-root")
	if err := os.Rename(backups, retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(retained, backups); err != nil {
		t.Fatal(err)
	}
	listed, err := ListBackups()
	if err != nil || len(listed[hcc.TargetFactory]) != 1 || listed[hcc.TargetFactory][0] != before[hcc.TargetFactory][0] {
		t.Fatalf("supported initial root alias listing refused: %v %v", listed, err)
	}
	if err := os.WriteFile(native, []byte("inert changed alias native payload"), 0o640); err != nil {
		t.Fatal(err)
	}
	if restored, err := Restore(hcc.TargetFactory, root); err != nil || restored != before[hcc.TargetFactory][0] {
		t.Fatalf("initial-alias listed archive cannot really recover: %q %v", restored, err)
	}
	if raw, err := os.ReadFile(native); err != nil || string(raw) != "inert root-alias listing archive" {
		t.Fatalf("alias listing archive missed original bytes: %q %v", raw, err)
	}
	if target, err := os.Readlink(backups); err != nil || target != retained {
		t.Fatalf("initial backup-root alias changed: %q %v", target, err)
	}
}

func TestListBackupsKeepsTargetInspectionErrorCause(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission denial is not observable as root")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "factory")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(target, 0o755) })
	listed, err := ListBackups()
	if listed != nil || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("unreadable target listing lost its cause: %v %v", listed, err)
	}
	if errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("unreadable target misreported as a generation conflict: %v", err)
	}
	if !strings.Contains(err.Error(), "backup: list factory") {
		t.Fatalf("listing refusal does not name the target: %v", err)
	}
}
