package materialize

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func managedLinkFixture(t *testing.T) (root, rel, target string) {
	t.Helper()
	root = t.TempDir()
	target = filepath.Join(t.TempDir(), "cached", "remove")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("cached"), 0o600); err != nil {
		t.Fatal(err)
	}
	rel = filepath.Join("skills", "remove")
	if err := os.MkdirAll(filepath.Join(root, "skills"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, rel)); err != nil {
		t.Fatal(err)
	}
	return root, rel, target
}

func TestManagedLinkEmptyQuarantineCleanup(t *testing.T) {
	for _, scenario := range []string{"owned", "replaced", "nonempty"} {
		t.Run(scenario, func(t *testing.T) {
			path := t.TempDir()
			parent, err := os.OpenRoot(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = parent.Close() }()
			if err := parent.Mkdir("quarantine", 0o700); err != nil {
				t.Fatal(err)
			}
			captured, err := parent.Lstat("quarantine")
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "replaced" {
				if err := parent.Rename("quarantine", "original"); err != nil {
					t.Fatal(err)
				}
				if err := parent.Mkdir("quarantine", 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "nonempty" {
				if err := parent.WriteFile("quarantine/foreign", []byte("preserve"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			err = removeEmptyManagedQuarantine(parent, "quarantine", captured)
			if scenario == "owned" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := parent.Lstat("quarantine"); !os.IsNotExist(err) {
					t.Fatalf("directory remains: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("unsafe quarantine cleanup succeeded")
				}
				if scenario == "replaced" && !errors.Is(err, ErrManagedLinkCleanupConflict) {
					t.Fatalf("error=%v", err)
				}
				if _, err := parent.Lstat("quarantine"); err != nil {
					t.Fatalf("replacement lost: %v", err)
				}
				if scenario == "nonempty" {
					if got := readFileString(t, filepath.Join(path, "quarantine", "foreign")); got != "preserve" {
						t.Fatalf("bytes=%q", got)
					}
				}
			}
		})
	}
}

func TestManagedLinkSnapshotAndRemove(t *testing.T) {
	root, rel, target := managedLinkFixture(t)
	candidate, err := snapshotManagedLink(root, rel, target)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := candidate.open()
	if err != nil {
		t.Fatal(err)
	}
	if err := candidate.remove(parent, nil, nil); err != nil {
		t.Fatal(err)
	}
	_ = parent.Close()
	if _, err := os.Lstat(filepath.Join(root, rel)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed link remains: %v", err)
	}
	if raw, err := os.ReadFile(filepath.Join(target, "SKILL.md")); err != nil || !bytes.Equal(raw, []byte("cached")) {
		t.Fatalf("cache target changed: %q err=%v", raw, err)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(filepath.Join(root, rel)), ".agent-marketplace-cleanup-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("quarantine residue: %v err=%v", matches, err)
	}
}

func TestManagedLinkRejectsRetargetedPaths(t *testing.T) {
	root, rel, target := managedLinkFixture(t)
	candidate, err := snapshotManagedLink(root, rel, target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, rel)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, rel), []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	parent, err := candidate.open()
	if !errors.Is(err, ErrManagedLinkCleanupConflict) || parent != nil {
		t.Fatalf("open error=%v parent=%v", err, parent)
	}
	if raw, err := os.ReadFile(filepath.Join(root, rel)); err != nil || !bytes.Equal(raw, []byte("foreign")) {
		t.Fatalf("replacement changed: %q err=%v", raw, err)
	}
}

func TestManagedLinkRejectsInvalidShapeAndTarget(t *testing.T) {
	root, rel, target := managedLinkFixture(t)
	if _, err := snapshotManagedLink(root, rel, "relative-target"); !errors.Is(err, ErrManagedLinkCleanupConflict) {
		t.Fatalf("relative target error=%v", err)
	}
	if _, err := snapshotManagedLink(root, rel, filepath.Join(target, "other")); !errors.Is(err, ErrManagedLinkCleanupConflict) {
		t.Fatalf("wrong target error=%v", err)
	}
	if _, _, err := openManagedLinkParent(root, ".", nil); !errors.Is(err, ErrManagedLinkCleanupConflict) {
		t.Fatalf("dot rel error=%v", err)
	}
}

func TestManagedLinkRejectsLeafShapes(t *testing.T) {
	for _, shape := range []string{"file", "symlink", "directory", "fifo"} {
		t.Run(shape, func(t *testing.T) {
			root, rel, target := managedLinkFixture(t)
			path := filepath.Join(root, rel)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			switch shape {
			case "file":
				if err := os.WriteFile(path, []byte("foreign"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(filepath.Join(t.TempDir(), "other"), path); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := unix.Mkfifo(path, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := snapshotManagedLink(root, rel, target); !errors.Is(err, ErrManagedLinkCleanupConflict) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestManagedLinkRootReplacementRefuses(t *testing.T) {
	for _, direct := range []bool{false, true} {
		t.Run(fmt.Sprintf("direct=%t", direct), func(t *testing.T) {
			root, rel, target := managedLinkFixture(t)
			if direct {
				if err := os.Rename(filepath.Join(root, rel), filepath.Join(root, "remove")); err != nil {
					t.Fatal(err)
				}
				rel = "remove"
			}
			candidate, err := snapshotManagedLink(root, rel, target)
			if err != nil {
				t.Fatal(err)
			}
			saved := filepath.Join(t.TempDir(), "saved")
			if err := os.Rename(root, saved); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(root, 0o700); err != nil {
				t.Fatal(err)
			}
			component := "skills"
			if direct {
				component = "remove"
			}
			if err := os.Rename(filepath.Join(saved, component), filepath.Join(root, component)); err != nil {
				t.Fatal(err)
			}
			if parent, err := candidate.open(); !errors.Is(err, ErrManagedLinkCleanupConflict) || parent != nil {
				t.Fatalf("root replacement accepted: %v", err)
			}
			if got, err := os.Readlink(filepath.Join(root, rel)); err != nil || got != target {
				t.Fatalf("replacement link changed: %q %v", got, err)
			}
		})
	}
}

func TestManagedLinkRealParentReplacementRefuses(t *testing.T) {
	root, rel, target := managedLinkFixture(t)
	candidate, err := snapshotManagedLink(root, rel, target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "skills"), filepath.Join(root, "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "skills"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "old", "remove"), filepath.Join(root, rel)); err != nil {
		t.Fatal(err)
	}
	if parent, err := candidate.open(); !errors.Is(err, ErrManagedLinkCleanupConflict) || parent != nil {
		t.Fatalf("parent replacement accepted: %v", err)
	}
	if got, err := os.Readlink(filepath.Join(root, rel)); err != nil || got != target {
		t.Fatalf("replacement changed: %q %v", got, err)
	}
}

func TestManagedLinkHookFailuresLeaveNoResidue(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(fmt.Sprintf("after=%t", after), func(t *testing.T) {
			root, rel, target := managedLinkFixture(t)
			candidate, err := snapshotManagedLink(root, rel, target)
			if err != nil {
				t.Fatal(err)
			}
			parent, err := candidate.open()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = parent.Close() }()
			sentinel := errors.New("hook failed")
			var beforeHook, afterHook func() error
			if after {
				afterHook = func() error { return sentinel }
			} else {
				beforeHook = func() error { return sentinel }
			}
			if err := candidate.remove(parent, beforeHook, afterHook); !errors.Is(err, sentinel) || !errors.Is(err, ErrManagedLinkCleanupConflict) {
				t.Fatalf("hook error=%v", err)
			}
			if got, err := os.Readlink(filepath.Join(root, rel)); err != nil || got != target {
				t.Fatalf("original link not preserved: %q %v", got, err)
			}
			matches, err := filepath.Glob(filepath.Join(filepath.Dir(filepath.Join(root, rel)), ".agent-marketplace-cleanup-*"))
			if err != nil || len(matches) != 0 {
				t.Fatalf("residue=%v error=%v", matches, err)
			}
		})
	}
}

func TestManagedLinkRestoresMovedForeignFile(t *testing.T) {
	root, rel, target := managedLinkFixture(t)
	candidate, err := snapshotManagedLink(root, rel, target)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := candidate.open()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = parent.Close() }()
	foreign := []byte("foreign preserved")
	moved := false
	err = candidate.remove(parent, func() error {
		if err := os.Remove(filepath.Join(root, rel)); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(root, rel), foreign, 0o600)
	}, func() error {
		moved = true
		if _, err := os.Lstat(filepath.Join(root, rel)); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("source was not moved: %v", err)
		}
		return nil
	})
	if !moved || !errors.Is(err, ErrManagedLinkCleanupConflict) {
		t.Fatalf("moved=%t error=%v", moved, err)
	}
	if raw, err := os.ReadFile(filepath.Join(root, rel)); err != nil || !bytes.Equal(raw, foreign) {
		t.Fatalf("foreign bytes lost: %q %v", raw, err)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(filepath.Join(root, rel)), ".agent-marketplace-cleanup-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("residue=%v error=%v", matches, err)
	}
}

func TestManagedLinkRejectsForeignParentDescriptor(t *testing.T) {
	root, rel, target := managedLinkFixture(t)
	candidate, err := snapshotManagedLink(root, rel, target)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := candidate.open()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = parent.Close() }()
	if err := parent.Link("remove", "twin"); err != nil {
		t.Fatal(err)
	}
	foreign := t.TempDir()
	if err := os.Rename(filepath.Join(root, "skills", "twin"), filepath.Join(foreign, "remove")); err != nil {
		t.Fatal(err)
	}
	other, err := os.OpenRoot(foreign)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Close() }()
	if err := candidate.remove(other, nil, nil); !errors.Is(err, ErrManagedLinkCleanupConflict) {
		t.Fatalf("wrong descriptor accepted: %v", err)
	}
	for _, path := range []string{filepath.Join(root, rel), filepath.Join(foreign, "remove")} {
		if got, err := os.Readlink(path); err != nil || got != target {
			t.Fatalf("link changed: %q %v", got, err)
		}
	}
}

func TestManagedLinkScopeRootAlias(t *testing.T) {
	root, rel, target := managedLinkFixture(t)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	candidate, err := snapshotManagedLink(alias, rel, target)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := candidate.open()
	if err != nil {
		t.Fatal(err)
	}
	if err := candidate.remove(parent, nil, nil); err != nil {
		t.Fatal(err)
	}
	_ = parent.Close()
}

func TestManagedLinkParentReplacementRefuses(t *testing.T) {
	root, rel, target := managedLinkFixture(t)
	candidate, err := snapshotManagedLink(root, rel, target)
	if err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(root, "skills")
	external := filepath.Join(t.TempDir(), "skills")
	if err := os.Mkdir(external, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(old, filepath.Join(root, "skills-old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, old); err != nil {
		t.Fatal(err)
	}
	if _, err := candidate.open(); !errors.Is(err, ErrManagedLinkCleanupConflict) {
		t.Fatalf("error=%v", err)
	}
}

func TestManagedLinkRestoreConflictPreservesBothReplacements(t *testing.T) {
	root, rel, target := managedLinkFixture(t)
	candidate, err := snapshotManagedLink(root, rel, target)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := candidate.open()
	if err != nil {
		t.Fatal(err)
	}
	foreignOne := []byte("foreign-one")
	foreignTwo := []byte("foreign-two")
	err = candidate.remove(parent, func() error {
		if err := os.Remove(filepath.Join(root, rel)); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(root, rel), foreignOne, 0o600)
	}, func() error {
		if _, err := os.Lstat(filepath.Join(root, rel)); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("original name was not absent: %v", err)
		}
		matches, err := filepath.Glob(filepath.Join(filepath.Dir(filepath.Join(root, rel)), ".agent-marketplace-cleanup-*"))
		if err != nil || len(matches) != 1 {
			return fmt.Errorf("quarantine matches=%v error=%v", matches, err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), foreignTwo, 0o600); err != nil {
			return err
		}
		return nil
	})
	_ = parent.Close()
	if !errors.Is(err, ErrManagedLinkCleanupConflict) {
		t.Fatalf("error=%v, want conflict", err)
	}
	if raw, readErr := os.ReadFile(filepath.Join(root, rel)); readErr != nil || !bytes.Equal(raw, foreignTwo) {
		t.Fatalf("foreign replacement = %q err=%v", raw, readErr)
	}
	matches, globErr := filepath.Glob(filepath.Join(filepath.Dir(filepath.Join(root, rel)), ".agent-marketplace-cleanup-*"))
	if globErr != nil || len(matches) != 1 {
		t.Fatalf("quarantine retention = %v err=%v", matches, globErr)
	}
	entryPath := filepath.Join(matches[0], "entry")
	if !strings.Contains(err.Error(), entryPath) {
		t.Fatalf("missing retained location in error: %v", err)
	}
	if retained, readErr := os.ReadFile(entryPath); readErr != nil || !bytes.Equal(retained, foreignOne) {
		t.Fatalf("retained quarantined bytes = %q err=%v", retained, readErr)
	}
}
