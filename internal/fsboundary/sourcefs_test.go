package fsboundary

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
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

// spec: registry-symlink-containment/source-directory-alias-refusals-retain-payload-shape-checks
func TestSourceFSStatRefusals(t *testing.T) {
	for _, shape := range []string{"escaping", "dangling", "hardlink"} {
		t.Run(shape, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			foreign := filepath.Join(outside, "marker.json")
			if err := os.WriteFile(foreign, []byte("foreign metadata control"), 0o640); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(foreign)
			if err != nil {
				t.Fatal(err)
			}
			leaf, target, want := filepath.Join(root, "marker.json"), foreign, ErrUnsafePath
			if shape == "dangling" {
				target = "missing.json"
			}
			if shape == "hardlink" {
				want = ErrUnsafeFile
				err = os.Link(foreign, leaf)
			} else {
				err = os.Symlink(target, leaf)
			}
			if err != nil {
				t.Fatal(err)
			}
			source, err := OpenSourceFS(root)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = source.Close() })
			if info, err := fs.Stat(source, "marker.json"); info != nil || !errors.Is(err, want) {
				t.Errorf("metadata probe lost refusal identity: %v %v", info, err)
			}
			if !errors.Is(source.Refusal(), want) {
				t.Errorf("metadata probe did not latch refusal: %v", source.Refusal())
			}
			if data, err := os.ReadFile(foreign); err != nil || string(data) != "foreign metadata control" {
				t.Fatalf("metadata probe changed external bytes: %q %v", data, err)
			}
			if after, err := os.Stat(foreign); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
				t.Fatalf("metadata probe changed external metadata: %v", err)
			}
		})
	}
}

// spec: registry-symlink-containment/source-directory-alias-reads-retain-captured-root-authority
func TestSourceFSStatMetadataControls(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "authored", "directory.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(root, "authored", "large.json")
	file, err := os.Create(payload)
	if err != nil {
		t.Fatal(err)
	}
	const payloadSize = 64 << 20
	if err := file.Truncate(payloadSize); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"alias": "authored", "marker.json": "authored/large.json"} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	source, err := OpenSourceFS(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	rootInfo, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := fs.Stat(source, "."); err != nil || !info.IsDir() || info.Sys() == nil || !os.SameFile(rootInfo, info) {
		t.Fatalf("root metadata lost native identity: %v %v", info, err)
	}
	for _, name := range []string{"alias", "alias/directory.json"} {
		if info, err := fs.Stat(source, name); err != nil || !info.IsDir() || info.Sys() == nil || info.Name() != filepath.Base(name) {
			t.Fatalf("contained directory metadata refused: %s %v %v", name, info, err)
		}
	}
	for _, name := range []string{"missing.json", "alias/missing/optional.json"} {
		if _, err := fs.Stat(source, name); !errors.Is(err, fs.ErrNotExist) || errors.Is(err, ErrUnsafePath) {
			t.Fatalf("optional absence became refusal: %s %v", name, err)
		}
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for _, name := range []string{"marker.json", "alias/large.json"} {
		if info, err := fs.Stat(source, name); err != nil || !info.Mode().IsRegular() || info.Size() != payloadSize || info.Sys() == nil || info.Name() != filepath.Base(name) {
			t.Fatalf("contained payload metadata refused: %s %v %v", name, info, err)
		}
	}
	runtime.ReadMemStats(&after)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated >= payloadSize/2 {
		t.Fatalf("metadata probing allocated payload-sized buffers: %d bytes", allocated)
	}
	if source.Refusal() != nil {
		t.Fatalf("valid metadata/absence controls latched refusal: %v", source.Refusal())
	}
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(outside, "alias"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "alias", "large.json"), []byte("replacement"), 0o644); err != nil {
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
	if info, err := fs.Stat(source, "."); err != nil || info.Sys() == nil || !os.SameFile(rootInfo, info) {
		t.Fatalf("root metadata followed replacement scope: %v %v", info, err)
	}
	if info, err := fs.Stat(source, "alias/large.json"); err != nil || info.Size() != payloadSize {
		t.Fatalf("payload metadata followed replacement scope: %v %v", info, err)
	}
}

func TestSourceFSStatCapturedGeneration(t *testing.T) {
	for _, shape := range []string{"payload", "alias"} {
		t.Run(shape, func(t *testing.T) {
			root := t.TempDir()
			for name, data := range map[string]string{"first.json": "first generation", "second.json": "second generation"} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			alias := filepath.Join(root, "marker.json")
			if err := os.Symlink("first.json", alias); err != nil {
				t.Fatal(err)
			}
			source, err := OpenSourceFS(root)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = source.Close() })
			captured, err := source.captureSource("marker.json")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = captured.Close() }()
			if info, err := statCapturedSource(captured); err != nil || info.Size() != int64(len("first generation")) {
				t.Fatalf("captured metadata control failed: %v %v", info, err)
			}
			if shape == "payload" {
				err = os.WriteFile(filepath.Join(root, "first.json"), []byte("changed generation"), 0o644)
			} else {
				if err := os.Remove(alias); err != nil {
					t.Fatal(err)
				}
				err = os.Symlink("second.json", alias)
			}
			if err != nil {
				t.Fatal(err)
			}
			if info, err := statCapturedSource(captured); info != nil || !errors.Is(err, ErrGenerationConflict) {
				t.Fatalf("metadata capture accepted changed generation: %v %v", info, err)
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
