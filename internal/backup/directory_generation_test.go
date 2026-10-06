package backup

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// spec: backup-recovery/backup-directory-generation-refuses-foreign-member-import
func TestCreateDirectoryGenerationRefusesForeignMemberImport(t *testing.T) {
	for _, lane := range []string{"ordinary", "codex-set"} {
		for _, boundary := range []string{"admitted", "opened"} {
			t.Run(lane+"/"+boundary, func(t *testing.T) {
				home, root := t.TempDir(), t.TempDir()
				t.Setenv("HOME", home)
				directory := filepath.Join(root, "skills")
				originalFile := filepath.Join(directory, "original.txt")
				if err := os.Mkdir(directory, 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(originalFile, []byte("inert original backup generation"), 0o640); err != nil {
					t.Fatal(err)
				}
				original, err := os.Stat(directory)
				if err != nil {
					t.Fatal(err)
				}
				originalPayload, err := os.Stat(originalFile)
				if err != nil {
					t.Fatal(err)
				}
				retained := filepath.Join(t.TempDir(), "retained-original-directory")
				foreign := filepath.Join(t.TempDir(), "foreign-directory")
				if err := os.Mkdir(foreign, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(foreign, "foreign.txt"), []byte("foreign inert backup member"), 0o600); err != nil {
					t.Fatal(err)
				}
				foreignInfo, err := os.Stat(foreign)
				if err != nil {
					t.Fatal(err)
				}
				foreignPayload, err := os.Stat(filepath.Join(foreign, "foreign.txt"))
				if err != nil {
					t.Fatal(err)
				}
				create := func() error {
					return Create(map[hcc.Target]ManagedPaths{hcc.TargetFactory: {Root: root, RelPaths: []string{"./skills"}}})
				}
				restore := func(destination string) (string, error) { return Restore(hcc.TargetFactory, destination) }
				target := "factory"
				if lane == "codex-set" {
					partitions := []CodexPartition{{ID: "machine-agents", Root: root, RelPaths: []string{"./skills"}}, {ID: "machine-codex", Root: filepath.Join(home, ".codex")}}
					create = func() error { _, err := CreateCodexSet(partitions); return err }
					restore = func(string) (string, error) { return RestoreCodexSet(partitions) }
					target = "codex"
				}
				changed := false
				logger := slog.Default()
				t.Cleanup(func() { slog.SetDefault(logger) })
				mutate := func(path string) {
					if changed {
						return
					}
					changed = true
					observed, err := os.Stat(path)
					if err != nil || !os.SameFile(original, observed) {
						t.Fatalf("backup traversal did not reach admitted source directory: %q %v", path, err)
					}
					if err := os.Rename(directory, retained); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(foreign, directory); err != nil {
						t.Fatal(err)
					}
				}
				observer := codexRestoreObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug})}
				if boundary == "admitted" {
					observer.onDirectory = mutate
				} else {
					observer.onDirectoryOpened = mutate
				}
				slog.SetDefault(slog.New(observer))
				err = create()
				if err == nil {
					destination := root
					if lane == "ordinary" {
						destination = t.TempDir()
					}
					if _, restoreErr := restore(destination); restoreErr != nil {
						t.Fatalf("unsafe archive actual recovery failed: %v", restoreErr)
					}
					raw, readErr := os.ReadFile(filepath.Join(destination, "skills", "foreign.txt"))
					t.Fatalf("backup accepted replaced directory and imported foreign members: hit=%t raw=%q read=%v", changed, raw, readErr)
				}
				if !changed || !errors.Is(err, fsboundary.ErrGenerationConflict) {
					t.Fatalf("backup directory replacement did not refuse generation: hit=%t %v", changed, err)
				}
				if raw, err := os.ReadFile(filepath.Join(directory, "foreign.txt")); err != nil || string(raw) != "foreign inert backup member" {
					t.Fatalf("foreign member bytes changed: %q %v", raw, err)
				}
				if info, err := os.Stat(directory); err != nil || !os.SameFile(foreignInfo, info) || foreignInfo.Mode() != info.Mode() || !foreignInfo.ModTime().Equal(info.ModTime()) {
					t.Fatalf("foreign directory metadata changed: %v", err)
				}
				if info, err := os.Stat(filepath.Join(directory, "foreign.txt")); err != nil || !os.SameFile(foreignPayload, info) || foreignPayload.Mode() != info.Mode() || !foreignPayload.ModTime().Equal(info.ModTime()) {
					t.Fatalf("foreign payload metadata changed: %v", err)
				}
				if raw, err := os.ReadFile(filepath.Join(retained, "original.txt")); err != nil || string(raw) != "inert original backup generation" {
					t.Fatalf("retained original bytes changed: %q %v", raw, err)
				}
				if info, err := os.Stat(retained); err != nil || !os.SameFile(original, info) || original.Mode() != info.Mode() || !original.ModTime().Equal(info.ModTime()) {
					t.Fatalf("original directory metadata changed: %v", err)
				}
				if info, err := os.Stat(filepath.Join(retained, "original.txt")); err != nil || !os.SameFile(originalPayload, info) || originalPayload.Mode() != info.Mode() || !originalPayload.ModTime().Equal(info.ModTime()) {
					t.Fatalf("original payload metadata changed: %v", err)
				}
				if members, err := filepath.Glob(filepath.Join(home, ".agent-marketplace", "backups", target, "*")); err != nil || len(members) != 0 {
					t.Fatalf("refused backup published archive or residue: %v %v", members, err)
				}
				if err := os.Rename(directory, foreign); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(retained, directory); err != nil {
					t.Fatal(err)
				}
				if err := create(); err != nil {
					t.Fatalf("healthy backup generation refused: %v", err)
				}
				if err := os.WriteFile(originalFile, []byte("inert changed live payload"), 0o640); err != nil {
					t.Fatal(err)
				}
				if restored, err := restore(root); err != nil || restored == "" {
					t.Fatalf("healthy actual archive recovery failed: %q %v", restored, err)
				}
				if raw, err := os.ReadFile(originalFile); err != nil || string(raw) != "inert original backup generation" {
					t.Fatalf("valid generation did not really archive/recover original bytes: %q %v", raw, err)
				}
				if _, err := os.Lstat(filepath.Join(directory, "foreign.txt")); !os.IsNotExist(err) {
					t.Fatalf("healthy archive imported foreign member: %v", err)
				}
			})
		}
	}
}

func TestCreateDirectoryRootDotAndInitialAliasRetainActualArchive(t *testing.T) {
	for _, alias := range []bool{false, true} {
		t.Run(fmt.Sprint(alias), func(t *testing.T) {
			home, root := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			file := filepath.Join(root, "original.txt")
			if err := os.WriteFile(file, []byte("inert root-dot archived payload"), 0o640); err != nil {
				t.Fatal(err)
			}
			selected := root
			if alias {
				selected = filepath.Join(t.TempDir(), "selected-source")
				if err := os.Symlink(root, selected); err != nil {
					t.Fatal(err)
				}
			}
			if err := Create(map[hcc.Target]ManagedPaths{hcc.TargetFactory: {Root: selected, RelPaths: []string{"."}}}); err != nil {
				t.Fatalf("root-dot/initial-alias archive refused: %v", err)
			}
			archives, err := filepath.Glob(filepath.Join(home, ".agent-marketplace", "backups", "factory", "*.tar.gz"))
			if err != nil || len(archives) != 1 {
				t.Fatalf("actual root-dot archive missing: %v %v", archives, err)
			}
			archive, err := os.Open(archives[0])
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = archive.Close() }()
			compressed, err := gzip.NewReader(archive)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = compressed.Close() }()
			reader := tar.NewReader(compressed)
			header, err := reader.Next()
			if err != nil || header.Name != "original.txt" {
				t.Fatalf("root-dot omission/header path changed: %v %v", header, err)
			}
			if _, err := reader.Next(); !errors.Is(err, io.EOF) {
				t.Fatalf("root-dot archive emitted unexpected root/member: %v", err)
			}
			if err := compressed.Close(); err != nil {
				t.Fatal(err)
			}
			if err := archive.Close(); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte("inert changed root-dot payload"), 0o640); err != nil {
				t.Fatal(err)
			}
			if restored, err := Restore(hcc.TargetFactory, root); err != nil || restored == "" {
				t.Fatalf("root-dot actual recovery failed: %q %v", restored, err)
			}
			if raw, err := os.ReadFile(file); err != nil || string(raw) != "inert root-dot archived payload" {
				t.Fatalf("root-dot archive did not really recover payload: %q %v", raw, err)
			}
			if alias {
				if target, err := os.Readlink(selected); err != nil || target != root {
					t.Fatalf("initial source alias changed: %q %v", target, err)
				}
			}
		})
	}
}
