package materialize

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// spec: materialization/portable-tree-publication-preserves-replacement-generation
func TestPortableTreeReadOnlyModes(t *testing.T) {
	for _, rootMode := range []os.FileMode{0o555, 0o500, 0o510} {
		for _, nestedMode := range []os.FileMode{0o755, 0o555, 0o500, 0o510} {
			t.Run(fmt.Sprintf("%04o/%04o", rootMode, nestedMode), func(t *testing.T) {
				source := portableModeSource(t, rootMode, nestedMode)
				destination := filepath.Join(t.TempDir(), "published")
				resetPortableModeTreeOnCleanup(t, destination)
				if err := publishPortableTree(source, destination, nil, "", nil); err != nil {
					t.Fatalf("read-only source publication (uid %d): %v", os.Geteuid(), err)
				}
				assertPortableModeTree(t, destination, rootMode, nestedMode, "original payload")
				assertPortableModeTree(t, source, rootMode, nestedMode, "original payload")
				assertNoPortableTreeStage(t, filepath.Dir(destination))
			})
		}
	}
}

func TestPortableTreeReadOnlyOwnedUpdateAndNoop(t *testing.T) {
	for _, mode := range []os.FileMode{0o555, 0o500, 0o510} {
		t.Run(fmt.Sprintf("%04o", mode), func(t *testing.T) {
			source := portableModeSource(t, 0o755, 0o755)
			if err := os.Chmod(filepath.Join(source, "nested", "deeper"), 0o755); err != nil {
				t.Fatal(err)
			}
			parent := t.TempDir()
			skills := filepath.Join(parent, "skills")
			destination := filepath.Join(skills, "probe")
			resetPortableModeTreeOnCleanup(t, destination)
			state := &portableState{entries: make(map[string]portableManifestEntry)}
			links := []skillLink{{name: "probe", target: source}}
			boundary := &destinationBoundary{repoRoot: parent, roots: []string{skills}}
			sync := func() TargetReport {
				report, err := syncPortableSkillTrees(skills, links, false, state, hcc.TargetFactory, "skills", make(map[string]bool), boundary)
				if err != nil {
					t.Fatal(err)
				}
				return report
			}
			if report := sync(); len(report.Written) != 1 {
				t.Fatalf("install control: %+v", report)
			}
			if err := os.Chmod(filepath.Join(source, "SKILL.md"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("changed payload"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(filepath.Join(source, "SKILL.md"), 0o444); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(filepath.Join(source, "nested", "deeper"), 0o555); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{source, filepath.Join(source, "nested")} {
				if err := os.Chmod(path, mode); err != nil {
					t.Fatal(err)
				}
			}
			hit := false
			boundary.linkPublicationHook = func(string) { hit = true }
			if report := sync(); len(report.Written) != 1 || !hit {
				t.Fatalf("changed-owned publication did not run: %+v", report)
			}
			assertPortableModeTree(t, destination, mode, mode, "changed payload")
			before := portableModeSnapshot(t, destination)
			hit = false
			if report := sync(); len(report.Written) != 0 || len(report.Skipped) != 1 || hit {
				t.Fatalf("identical read-only repeat rewrote output: %+v", report)
			}
			assertPortableModeSnapshot(t, destination, before)
			assertNoPortableTreeStage(t, skills)
		})
	}
}

func TestPortableTreeReadOnlyRefusalCleanup(t *testing.T) {
	for _, replacement := range []string{"destination", "stage directory", "stage alias"} {
		t.Run(replacement, func(t *testing.T) {
			source := portableModeSource(t, 0o500, 0o510)
			parent := t.TempDir()
			destination := filepath.Join(parent, "published")
			external := portableModeSource(t, 0o555, 0o500)
			externalBefore := portableModeSnapshot(t, external)
			var stage, preserved string
			var before map[string]os.FileInfo
			retained := filepath.Join(parent, "retained")
			resetPortableModeTreeOnCleanup(t, retained)
			resetPortableModeTreeOnCleanup(t, destination)
			boundary := &destinationBoundary{repoRoot: parent, linkPublicationHook: func(string) {
				stages, err := filepath.Glob(filepath.Join(parent, ".agent-marketplace-portable-tree-*"))
				if err != nil || len(stages) != 1 {
					t.Fatalf("stage fixture: %v %v", stages, err)
				}
				stage = stages[0]
				assertPortableModeTree(t, stage, 0o500, 0o510, "original payload")
				if replacement == "destination" {
					if err := os.Mkdir(destination, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(destination, "user.md"), []byte("handwritten bytes"), 0o444); err != nil {
						t.Fatal(err)
					}
					if err := os.Chmod(destination, 0o510); err != nil {
						t.Fatal(err)
					}
					preserved = destination
				} else {
					if err := os.Rename(stage, retained); err != nil {
						t.Fatal(err)
					}
					if replacement == "stage alias" {
						if err := os.Symlink(external, stage); err != nil {
							t.Fatal(err)
						}
						preserved = external
					} else {
						if err := os.Mkdir(stage, 0o755); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filepath.Join(stage, "user.md"), []byte("handwritten bytes"), 0o444); err != nil {
							t.Fatal(err)
						}
						if err := os.Chmod(stage, 0o510); err != nil {
							t.Fatal(err)
						}
						preserved = stage
						t.Cleanup(func() {
							if err := os.Chmod(stage, 0o700); err != nil {
								t.Error(err)
							}
						})
					}
				}
				before = portableModeSnapshot(t, preserved)
			}}
			err := publishPortableTree(source, destination, nil, "", boundary)
			if stage == "" || !errors.Is(err, fsboundary.ErrGenerationConflict) {
				t.Fatalf("post-mode refusal did not run: %v", err)
			}
			assertPortableModeSnapshot(t, preserved, before)
			assertPortableModeSnapshot(t, external, externalBefore)
			if replacement == "stage alias" {
				assertPortableModeTree(t, external, 0o555, 0o500, "original payload")
			} else {
				assertCleanupBytes(t, filepath.Join(preserved, "user.md"), "handwritten bytes")
			}
			if replacement == "destination" {
				assertNoPortableTreeStage(t, parent)
			} else {
				assertPortableModeTree(t, retained, 0o500, 0o510, "original payload")
				if _, err := os.Lstat(destination); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("refusal published destination: %v", err)
				}
			}
		})
	}
}

func TestPortableTreeReadOnlyUmask(t *testing.T) {
	if os.Getenv("AM_PORTABLE_MODE_UMASK_CHILD") == "1" {
		for _, mode := range []int{0o027, 0o077, 0o200} {
			t.Run(fmt.Sprintf("%04o", mode), func(t *testing.T) {
				source := portableModeSource(t, 0o755, 0o555)
				destination := filepath.Join(t.TempDir(), "published")
				resetPortableModeTreeOnCleanup(t, destination)
				mask := syscall.Umask(mode)
				defer syscall.Umask(mask)
				if err := publishPortableTree(source, destination, nil, "", nil); err != nil {
					t.Fatal(err)
				}
				assertPortableModeTree(t, destination, 0o755, 0o555, "original payload")
			})
		}
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPortableTreeReadOnlyUmask$", "-test.timeout=12s")
	cmd.Env = append(os.Environ(), "AM_PORTABLE_MODE_UMASK_CHILD=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("isolated umask control: %v\n%s", err, output)
	}
}

func portableModeSource(t *testing.T, rootMode, nestedMode os.FileMode) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "nested", "deeper"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"SKILL.md": "original payload", "nested/deeper/data.txt": "nested bytes", "nested/run.sh": "#!/bin/sh\nexit 0\n"} {
		mode := os.FileMode(0o444)
		if name == "nested/run.sh" {
			mode = 0o751
		}
		file := filepath.Join(root, filepath.FromSlash(name))
		if err := os.WriteFile(file, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(file, mode); err != nil {
			t.Fatal(err)
		}
	}
	for name, mode := range map[string]os.FileMode{".": rootMode, "nested": nestedMode, "nested/deeper": 0o555} {
		if err := os.Chmod(filepath.Join(root, filepath.FromSlash(name)), mode); err != nil {
			t.Fatal(err)
		}
	}
	resetPortableModeTreeOnCleanup(t, root)
	return root
}

func resetPortableModeTreeOnCleanup(t *testing.T, root string) {
	t.Helper()
	t.Cleanup(func() {
		for _, name := range []string{".", "nested", "nested/deeper"} {
			if err := os.Chmod(filepath.Join(root, filepath.FromSlash(name)), 0o700); err != nil && !errors.Is(err, fs.ErrNotExist) {
				t.Error(err)
			}
		}
	})
}

func assertPortableModeTree(t *testing.T, root string, rootMode, nestedMode os.FileMode, body string) {
	t.Helper()
	for name, mode := range map[string]os.FileMode{".": rootMode, "nested": nestedMode, "nested/deeper": 0o555, "SKILL.md": 0o444, "nested/deeper/data.txt": 0o444, "nested/run.sh": 0o751} {
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("%s mode: %v, want %04o", name, info, mode)
		}
	}
	assertCleanupBytes(t, filepath.Join(root, "SKILL.md"), body)
	assertCleanupBytes(t, filepath.Join(root, "nested", "deeper", "data.txt"), "nested bytes")
	assertCleanupBytes(t, filepath.Join(root, "nested", "run.sh"), "#!/bin/sh\nexit 0\n")
}

func portableModeSnapshot(t *testing.T, root string) map[string]os.FileInfo {
	t.Helper()
	result := make(map[string]os.FileInfo)
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		result[rel] = info
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertPortableModeSnapshot(t *testing.T, root string, before map[string]os.FileInfo) {
	t.Helper()
	after := portableModeSnapshot(t, root)
	if len(after) != len(before) {
		t.Fatalf("snapshot entry count: %d, want %d", len(after), len(before))
	}
	for name, info := range before {
		current := after[name]
		if current == nil || !os.SameFile(info, current) || info.Mode() != current.Mode() || !info.ModTime().Equal(current.ModTime()) {
			t.Fatalf("changed preserved metadata: %s", name)
		}
	}
}

func assertNoPortableTreeStage(t *testing.T, parent string) {
	t.Helper()
	if stages, err := filepath.Glob(filepath.Join(parent, ".agent-marketplace-portable-tree-*")); err != nil || len(stages) != 0 {
		t.Fatalf("owned stage residue: %v %v", stages, err)
	}
}
