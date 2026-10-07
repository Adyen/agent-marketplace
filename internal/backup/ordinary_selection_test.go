package backup

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// spec: backup-recovery/ordinary-restore-retains-selected-archive-and-directory-generations
func TestOrdinaryRestoreRetainsSelectedArchiveAndDirectoryGenerations(t *testing.T) {
	for _, lane := range []string{"ordinary", "mapped"} {
		for _, replacement := range []string{"parent-alias", "parent-directory", "archive"} {
			t.Run(lane+"/"+replacement, func(t *testing.T) {
				home, root := t.TempDir(), t.TempDir()
				t.Setenv("HOME", home)
				native := filepath.Join(root, "native.txt")
				if lane == "mapped" {
					native = filepath.Join(t.TempDir(), "native.txt")
				}
				if err := os.WriteFile(native, []byte("inert original selected archive payload"), 0o640); err != nil {
					t.Fatal(err)
				}
				managed := ManagedPaths{Root: root, RelPaths: []string{"native.txt"}}
				restore := func() (string, error) { return Restore(hcc.TargetFactory, root) }
				const mappedName = ".agent-marketplace-native/machine-mcp/native.json"
				if lane == "mapped" {
					managed = ManagedPaths{Root: filepath.Join(root, "absent-ordinary-root"), MappedFiles: map[string]string{mappedName: native}}
					restore = func() (string, error) { return Restore(hcc.TargetFactory, root, map[string]string{mappedName: native}) }
				}
				if err := Create(map[hcc.Target]ManagedPaths{hcc.TargetFactory: managed}); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(home, ".agent-marketplace", "backups", "factory")
				entries, err := os.ReadDir(target)
				if err != nil || len(entries) != 1 {
					t.Fatalf("real selected archive setup: %v %v", entries, err)
				}
				archiveName := entries[0].Name()
				archive := filepath.Join(target, archiveName)
				originalRaw, err := os.ReadFile(archive)
				if err != nil {
					t.Fatal(err)
				}
				originalArchive, err := os.Stat(archive)
				if err != nil {
					t.Fatal(err)
				}
				foreignHome, foreignRoot := t.TempDir(), t.TempDir()
				t.Setenv("HOME", foreignHome)
				foreignNative := filepath.Join(foreignRoot, "native.txt")
				if err := os.WriteFile(foreignNative, []byte("foreign inert selected archive payload"), 0o640); err != nil {
					t.Fatal(err)
				}
				foreignManaged := ManagedPaths{Root: foreignRoot, RelPaths: []string{"native.txt"}}
				if lane == "mapped" {
					foreignManaged = ManagedPaths{Root: filepath.Join(foreignRoot, "absent-ordinary-root"), MappedFiles: map[string]string{mappedName: foreignNative}}
				}
				if err := Create(map[hcc.Target]ManagedPaths{hcc.TargetFactory: foreignManaged}); err != nil {
					t.Fatal(err)
				}
				foreign := filepath.Join(foreignHome, ".agent-marketplace", "backups", "factory")
				entries, err = os.ReadDir(foreign)
				if err != nil || len(entries) != 1 {
					t.Fatalf("real foreign archive setup: %v %v", entries, err)
				}
				foreignArchive := filepath.Join(foreign, archiveName)
				if entries[0].Name() != archiveName {
					if err := os.Rename(filepath.Join(foreign, entries[0].Name()), foreignArchive); err != nil {
						t.Fatal(err)
					}
				}
				foreignRaw, err := os.ReadFile(foreignArchive)
				if err != nil {
					t.Fatal(err)
				}
				foreignInfo, err := os.Stat(foreignArchive)
				if err != nil {
					t.Fatal(err)
				}
				t.Setenv("HOME", home)
				if err := os.WriteFile(native, []byte("inert current native generation"), 0o640); err != nil {
					t.Fatal(err)
				}
				current, err := os.Stat(native)
				if err != nil {
					t.Fatal(err)
				}
				retained := filepath.Join(t.TempDir(), "retained-selected-generation")
				originalPath, originalDirectory := archive, target
				foreignPath, foreignDirectory := foreignArchive, foreign
				var originalParent, foreignParent os.FileInfo
				changed := false
				logger := slog.Default()
				t.Cleanup(func() { slog.SetDefault(logger) })
				slog.SetDefault(slog.New(codexRestoreObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), onRestoreSelected: func(path string) {
					if changed {
						return
					}
					changed = true
					observed, err := os.Stat(path)
					if err != nil || !os.SameFile(originalArchive, observed) {
						t.Fatalf("restore did not select actual original archive: %q %v", path, err)
					}
					switch replacement {
					case "parent-alias", "parent-directory":
						if err := os.Rename(target, retained); err != nil {
							t.Fatal(err)
						}
						originalPath, originalDirectory = filepath.Join(retained, archiveName), retained
						if replacement == "parent-alias" {
							err = os.Symlink(foreign, target)
						} else {
							err = os.Rename(foreign, target)
							foreignPath, foreignDirectory = archive, target
						}
					case "archive":
						if err := os.Rename(archive, retained); err != nil {
							t.Fatal(err)
						}
						originalPath = retained
						err = os.Rename(foreignArchive, archive)
						foreignPath = archive
					}
					if err != nil {
						t.Fatal(err)
					}
					originalParent, err = os.Stat(originalDirectory)
					if err != nil {
						t.Fatal(err)
					}
					foreignParent, err = os.Stat(foreignDirectory)
					if err != nil {
						t.Fatal(err)
					}
				}}))
				restored, err := restore()
				if raw, readErr := os.ReadFile(native); readErr != nil || string(raw) != "inert current native generation" {
					t.Fatalf("restore consumed foreign archive after original selection: hit=%t restored=%q err=%v raw=%q read=%v", changed, restored, err, raw, readErr)
				}
				if !changed || restored != "" || !errors.Is(err, fsboundary.ErrGenerationConflict) {
					t.Fatalf("ordinary selection substitution did not generation-refuse: %q %v", restored, err)
				}
				if !strings.Contains(err.Error(), "backup: restore factory: ") {
					t.Fatalf("selection refusal does not name the restored target: %v", err)
				}
				if info, err := os.Stat(native); err != nil || !os.SameFile(current, info) || current.Mode() != info.Mode() || !current.ModTime().Equal(info.ModTime()) {
					t.Fatalf("current native metadata changed: %v", err)
				}
				if raw, err := os.ReadFile(originalPath); err != nil || !bytes.Equal(raw, originalRaw) {
					t.Fatalf("original selected archive bytes changed: %v", err)
				}
				if info, err := os.Stat(originalPath); err != nil || !os.SameFile(originalArchive, info) || originalArchive.Mode() != info.Mode() || !originalArchive.ModTime().Equal(info.ModTime()) {
					t.Fatalf("original archive metadata changed: %v", err)
				}
				if info, err := os.Stat(originalDirectory); err != nil || !os.SameFile(originalParent, info) || originalParent.Mode() != info.Mode() || !originalParent.ModTime().Equal(info.ModTime()) {
					t.Fatalf("original archive parent metadata changed: %v", err)
				}
				if raw, err := os.ReadFile(foreignPath); err != nil || !bytes.Equal(raw, foreignRaw) {
					t.Fatalf("foreign archive bytes changed: %v", err)
				}
				if info, err := os.Stat(foreignPath); err != nil || !os.SameFile(foreignInfo, info) || foreignInfo.Mode() != info.Mode() || !foreignInfo.ModTime().Equal(info.ModTime()) {
					t.Fatalf("foreign archive metadata changed: %v", err)
				}
				if info, err := os.Stat(foreignDirectory); err != nil || !os.SameFile(foreignParent, info) || foreignParent.Mode() != info.Mode() || !foreignParent.ModTime().Equal(info.ModTime()) {
					t.Fatalf("foreign archive parent metadata changed: %v", err)
				}
				if replacement == "parent-alias" {
					if targetPath, err := os.Readlink(target); err != nil || targetPath != foreign {
						t.Fatalf("foreign alias changed: %q %v", targetPath, err)
					}
				}
				if residue, err := filepath.Glob(filepath.Join(filepath.Dir(native), ".agent-marketplace-restore-*")); err != nil || len(residue) != 0 {
					t.Fatalf("selection refusal left output stage: %v %v", residue, err)
				}
				switch replacement {
				case "parent-alias":
					if err := os.Remove(target); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(retained, target); err != nil {
						t.Fatal(err)
					}
				case "parent-directory":
					if err := os.Rename(target, foreign); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(retained, target); err != nil {
						t.Fatal(err)
					}
				case "archive":
					if err := os.Rename(archive, foreignArchive); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(retained, archive); err != nil {
						t.Fatal(err)
					}
				}
				restored, err = restore()
				if err != nil || restored == "" {
					t.Fatalf("original selection healthy recovery refused: %q %v", restored, err)
				}
				if raw, err := os.ReadFile(native); err != nil || string(raw) != "inert original selected archive payload" {
					t.Fatalf("original selected archive did not genuinely recover: %q %v", raw, err)
				}
				if info, err := os.Stat(native); err != nil || info.Mode().Perm() != 0o640 {
					t.Fatalf("original selected archive recovery mode changed: %v", err)
				}
			})
		}
	}
}

func TestOrdinaryRestoreSelectionInitialAliasesAndMissingBackup(t *testing.T) {
	for _, targetAlias := range []bool{false, true} {
		t.Run(fmt.Sprint(targetAlias), func(t *testing.T) {
			home, root := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			if restored, err := Restore(hcc.TargetFactory, root); err == nil || restored != "" {
				t.Fatalf("missing backup unexpectedly restored: %q %v", restored, err)
			}
			native := filepath.Join(root, "native.txt")
			if err := os.WriteFile(native, []byte("inert original alias selection archive"), 0o640); err != nil {
				t.Fatal(err)
			}
			if err := Create(map[hcc.Target]ManagedPaths{hcc.TargetFactory: {Root: root, RelPaths: []string{"native.txt"}}}); err != nil {
				t.Fatal(err)
			}
			selected := filepath.Join(home, ".agent-marketplace", "backups")
			if targetAlias {
				selected = filepath.Join(selected, "factory")
			}
			retained := filepath.Join(t.TempDir(), "selected-alias-directory")
			if err := os.Rename(selected, retained); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(retained, selected); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(native, []byte("inert changed alias selection bytes"), 0o640); err != nil {
				t.Fatal(err)
			}
			if restored, err := Restore(hcc.TargetFactory, root); err != nil || restored == "" {
				t.Fatalf("supported initial restore archive alias refused: %q %v", restored, err)
			}
			if raw, err := os.ReadFile(native); err != nil || string(raw) != "inert original alias selection archive" {
				t.Fatalf("alias selection did not really recover archived bytes: %q %v", raw, err)
			}
			if link, err := os.Readlink(selected); err != nil || link != retained {
				t.Fatalf("initial restore selection alias changed: %q %v", link, err)
			}
		})
	}
}
