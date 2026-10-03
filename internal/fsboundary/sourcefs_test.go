package fsboundary

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// spec: registry-symlink-containment/source-directory-alias-reads-retain-captured-root-authority
func TestSourceFSContainedDirectoryAliases(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "authored", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "authored", "nested", "data.txt"), []byte("captured alias bytes"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"alias": "authored", "second": "alias", "authored/nested/file-alias.txt": "data.txt"} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	source, err := OpenSourceFS(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	for _, method := range []string{"open", "readfile", "readdir"} {
		t.Run(method, func(t *testing.T) {
			switch method {
			case "open":
				file, err := source.Open("second/nested")
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = file.Close() }()
				if info, err := file.Stat(); err != nil || !info.IsDir() {
					t.Fatalf("directory alias did not open as a directory: %v", err)
				}
				payload, err := source.Open("second/nested/file-alias.txt")
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = payload.Close() }()
				if data, err := io.ReadAll(payload); err != nil || string(data) != "captured alias bytes" {
					t.Fatalf("opened alias snapshot lost payload: %q %v", data, err)
				}
				if info, err := payload.Stat(); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o755 {
					t.Fatalf("opened alias snapshot lost regular executable mode: %v", err)
				}
			case "readdir":
				entries, err := fs.ReadDir(source, "second/nested")
				if err != nil || len(entries) != 2 {
					t.Fatalf("directory alias enumeration failed: %v %v", entries, err)
				}
				for _, entry := range entries {
					if info, err := entry.Info(); err != nil || info.Name() != entry.Name() {
						t.Fatalf("directory entry info lost captured authority: %v %v", info, err)
					}
				}
			case "readfile":
				for _, name := range []string{"alias/nested/data.txt", "second/nested/file-alias.txt"} {
					if data, err := fs.ReadFile(source, name); err != nil || string(data) != "captured alias bytes" {
						t.Fatalf("contained ancestor alias read failed: %q %v", data, err)
					}
				}
			}
		})
	}
	if err := os.MkdirAll(filepath.Join(outside, "second", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "second", "nested", "data.txt"), []byte("foreign alias bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	retained := root + "-retained"
	if err := os.Rename(root, retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(root); err != nil {
			t.Error(err)
		}
		if err := os.Rename(retained, root); err != nil {
			t.Error(err)
		}
	})
	if data, err := fs.ReadFile(source, "second/nested/data.txt"); err != nil || string(data) != "captured alias bytes" {
		t.Fatalf("replacement source root redirected alias read: %q %v", data, err)
	}
	if err := source.Refusal(); err != nil {
		t.Fatalf("contained source aliases recorded a refusal: %v", err)
	}
}

// spec: registry-symlink-containment/source-directory-alias-refusals-retain-payload-shape-checks
func TestSourceFSUnsafeDirectoryAliases(t *testing.T) {
	for _, shape := range []string{"escaped", "dangling", "cycle", "external-return", "hardlink"} {
		t.Run(shape, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "authored"), 0o755); err != nil {
				t.Fatal(err)
			}
			payload := filepath.Join(root, "authored", "data.txt")
			if err := os.WriteFile(payload, []byte("valid sibling data"), 0o755); err != nil {
				t.Fatal(err)
			}
			foreign := filepath.Join(outside, "data.txt")
			if err := os.WriteFile(foreign, []byte("foreign bytes"), 0o640); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(foreign)
			if err != nil {
				t.Fatal(err)
			}
			target := outside
			want := ErrUnsafePath
			switch shape {
			case "dangling":
				target = "missing"
			case "cycle":
				target = "alias"
			case "external-return":
				target = filepath.Join(outside, "return")
				if err := os.Symlink(filepath.Join(root, "authored"), target); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				target = "authored"
				want = ErrUnsafeFile
				if err := os.Link(payload, filepath.Join(root, "authored", "second.txt")); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(target, filepath.Join(root, "alias")); err != nil {
				t.Fatal(err)
			}
			source, err := OpenSourceFS(root)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = source.Close() })
			if data, err := fs.ReadFile(source, "alias/data.txt"); !errors.Is(err, want) || len(data) != 0 {
				t.Fatalf("unsafe alias read returned payload or lost identity: %q %v", data, err)
			}
			if !errors.Is(source.Refusal(), want) {
				t.Fatalf("unsafe alias read was not retained for publication: %v", source.Refusal())
			}
			if shape != "hardlink" {
				if file, err := source.Open("alias"); !errors.Is(err, ErrUnsafePath) || file != nil {
					if file != nil {
						_ = file.Close()
					}
					t.Fatalf("unsafe directory alias opened: %v", err)
				}
				if data, err := fs.ReadFile(source, "authored/data.txt"); err != nil || string(data) != "valid sibling data" {
					t.Fatalf("source refusal suppressed the valid sibling control: %q %v", data, err)
				}
			}
			if data, err := os.ReadFile(foreign); err != nil || string(data) != "foreign bytes" {
				t.Fatalf("source refusal changed external bytes: %q %v", data, err)
			}
			if after, err := os.Stat(foreign); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
				t.Fatalf("source refusal changed external metadata: %v", err)
			}
		})
	}
}

func TestSourceFSKeepsCapturedRootAndDirectoryInfo(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "data.txt"), []byte("captured inert data"), 0o640); err != nil {
		t.Fatal(err)
	}
	source, err := OpenSourceFS(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	entries, err := fs.ReadDir(source, ".")
	if err != nil || len(entries) != 1 {
		t.Fatalf("captured directory control failed: %v %v", entries, err)
	}
	if info, err := entries[0].Info(); err != nil || info.Name() != "data.txt" {
		t.Fatalf("directory info outlived captured owner: %v %v", info, err)
	}
	retained := root + "-retained"
	if err := os.Rename(root, retained); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Rename(retained, root); err != nil {
			t.Errorf("restore fixture root: %v", err)
		}
	})
	data, err := fs.ReadFile(source, "data.txt")
	if err != nil || string(data) != "captured inert data" {
		t.Fatalf("renamed source root redirected read: %q %v", data, err)
	}
	if _, err := fs.ReadFile(source, "../data.txt"); !errors.Is(err, fs.ErrInvalid) {
		t.Fatalf("invalid FS path did not refuse: %v", err)
	}
}
