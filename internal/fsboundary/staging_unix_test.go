//go:build unix

package fsboundary

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirCreateTemp(t *testing.T) {
	t.Run("creates a uniquely named singly linked regular file", func(t *testing.T) {
		root, rootPath := tempRoot(t)

		first, firstName, err := root.CreateTemp(".stage-*.tmp")
		if err != nil {
			t.Fatalf("CreateTemp: %v", err)
		}
		defer func() { _ = first.Close() }()
		second, secondName, err := root.CreateTemp(".stage-*.tmp")
		if err != nil {
			t.Fatalf("CreateTemp second: %v", err)
		}
		defer func() { _ = second.Close() }()

		if firstName == secondName {
			t.Fatalf("temp names collide: %q", firstName)
		}
		if strings.Contains(firstName, "/") || !strings.HasPrefix(firstName, ".stage-") || !strings.HasSuffix(firstName, ".tmp") {
			t.Fatalf("temp name %q is not a pattern-shaped leaf", firstName)
		}
		for file, name := range map[*os.File]string{first: firstName, second: secondName} {
			info, err := os.Lstat(filepath.Join(rootPath, name))
			if err != nil {
				t.Fatalf("lstat staged %s: %v", name, err)
			}
			if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
				t.Fatalf("staged mode = %v", info.Mode())
			}
			id, err := FileIdentity(file)
			if err != nil {
				t.Fatalf("identity: %v", err)
			}
			if id.Links != 1 {
				t.Fatalf("staged links = %d, want 1", id.Links)
			}
		}
	})

	t.Run("stays bound to the captured directory object", func(t *testing.T) {
		root, rootPath := tempRoot(t)

		replaced := rootPath + "-old"
		if err := os.Rename(rootPath, replaced); err != nil {
			t.Fatal(err)
		}
		mustMkdir(t, rootPath)

		file, name, err := root.CreateTemp(".stage-*.tmp")
		if err != nil {
			t.Fatalf("CreateTemp: %v", err)
		}
		_ = file.Close()

		if _, err := os.Lstat(filepath.Join(replaced, name)); err != nil {
			t.Fatalf("staged file is not below the captured directory: %v", err)
		}
		if _, err := os.Lstat(filepath.Join(rootPath, name)); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("staged file leaked into the replacement tree: %v", err)
		}
	})
}

func TestDirMkdirAndUnlink(t *testing.T) {
	t.Run("create and remove below the captured directory", func(t *testing.T) {
		root, rootPath := tempRoot(t)
		if err := root.Mkdir(".agents", 0o700); err != nil {
			t.Fatalf("Mkdir: %v", err)
		}
		info, err := os.Lstat(filepath.Join(rootPath, ".agents"))
		if err != nil || !info.IsDir() {
			t.Fatalf("mkdir result: %v, %v", info, err)
		}
		if err := root.Mkdir(".agents", 0o700); !errors.Is(err, fs.ErrExist) {
			t.Fatalf("second Mkdir = %v, want fs.ErrExist", err)
		}
		mustWrite(t, filepath.Join(rootPath, ".agents", "stale.tmp"), []byte("x"), 0o600)
		if err := root.Unlink(filepath.Join(".agents", "stale.tmp")); err == nil {
			t.Fatal("Unlink accepted a nested name")
		}
		sub, err := OpenDir(filepath.Join(rootPath, ".agents"))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = sub.Close() }()
		if err := sub.Unlink("stale.tmp"); err != nil {
			t.Fatalf("Unlink: %v", err)
		}
		if err := sub.Unlink("stale.tmp"); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("second Unlink = %v, want fs.ErrNotExist", err)
		}
	})

	t.Run("stay bound to the captured directory object", func(t *testing.T) {
		root, rootPath := tempRoot(t)
		replaced := rootPath + "-old"
		if err := os.Rename(rootPath, replaced); err != nil {
			t.Fatal(err)
		}
		mustMkdir(t, rootPath)

		if err := root.Mkdir(".agents", 0o700); err != nil {
			t.Fatalf("Mkdir: %v", err)
		}
		if _, err := os.Lstat(filepath.Join(replaced, ".agents")); err != nil {
			t.Fatalf("directory not created below the captured object: %v", err)
		}
		if _, err := os.Lstat(filepath.Join(rootPath, ".agents")); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("directory leaked into the replacement tree: %v", err)
		}
	})
}

func TestDestinationStagingDir(t *testing.T) {
	t.Run("returns a descriptor-bound handle of the logical parent", func(t *testing.T) {
		root, rootPath := tempRoot(t)
		mustMkdir(t, filepath.Join(rootPath, ".agents"))
		dest, err := CaptureDestination(root, ".agents/config.yaml", LinkPolicyRepository)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()

		staging, err := dest.StagingDir()
		if err != nil {
			t.Fatalf("StagingDir: %v", err)
		}
		defer func() { _ = staging.Close() }()
		if staging.Path() != filepath.Join(rootPath, ".agents") {
			t.Fatalf("staging path = %q", staging.Path())
		}

		file, name, err := staging.CreateTemp(".stage-*.tmp")
		if err != nil {
			t.Fatalf("CreateTemp: %v", err)
		}
		_ = file.Close()
		if _, err := os.Lstat(filepath.Join(rootPath, ".agents", name)); err != nil {
			t.Fatalf("staged file missing below the logical parent: %v", err)
		}
	})

	t.Run("conflicts when the parent was replaced since capture", func(t *testing.T) {
		root, rootPath := tempRoot(t)
		layer := filepath.Join(rootPath, ".agents")
		mustMkdir(t, layer)
		dest, err := CaptureDestination(root, ".agents/config.yaml", LinkPolicyRepository)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()

		old := filepath.Join(rootPath, ".agents-old")
		if err := os.Rename(layer, old); err != nil {
			t.Fatal(err)
		}
		mustMkdir(t, layer)

		if _, err := dest.StagingDir(); !errors.Is(err, ErrGenerationConflict) {
			t.Fatalf("StagingDir error = %v, want ErrGenerationConflict", err)
		}
	})
}

func TestDestinationReadTarget(t *testing.T) {
	t.Run("reads a direct destination", func(t *testing.T) {
		root, rootPath := tempRoot(t)
		mustWrite(t, filepath.Join(rootPath, "lock.yaml"), []byte("locked\n"), 0o640)
		dest, err := CaptureDestination(root, "lock.yaml", LinkPolicyRepository)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()

		data, id, err := dest.ReadTarget(1 << 20)
		if err != nil {
			t.Fatalf("ReadTarget: %v", err)
		}
		if string(data) != "locked\n" {
			t.Fatalf("data = %q", data)
		}
		if id != dest.Identity() {
			t.Fatalf("read identity %+v does not match capture identity %+v", id, dest.Identity())
		}
	})

	t.Run("reads an honored user link target", func(t *testing.T) {
		root, rootPath := tempRoot(t)
		external := filepath.Join(t.TempDir(), "dotfiles")
		mustMkdir(t, external)
		target := filepath.Join(external, "config.yaml")
		mustWrite(t, target, []byte("linked\n"), 0o600)
		mustSymlink(t, target, filepath.Join(rootPath, "config.yaml"))

		dest, err := CaptureDestination(root, "config.yaml", LinkPolicyUser)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()

		data, _, err := dest.ReadTarget(1 << 20)
		if err != nil {
			t.Fatalf("ReadTarget: %v", err)
		}
		if string(data) != "linked\n" {
			t.Fatalf("data = %q", data)
		}
	})

	t.Run("reports an absent destination as not-exist", func(t *testing.T) {
		root, _ := tempRoot(t)
		dest, err := CaptureDestination(root, "missing.yaml", LinkPolicyRepository)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()

		if _, _, err := dest.ReadTarget(1 << 20); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("ReadTarget error = %v, want fs.ErrNotExist", err)
		}
	})

	t.Run("conflicts when the leaf changed since capture", func(t *testing.T) {
		root, rootPath := tempRoot(t)
		leaf := filepath.Join(rootPath, "config.yaml")
		mustWrite(t, leaf, []byte("short\n"), 0o644)
		dest, err := CaptureDestination(root, "config.yaml", LinkPolicyRepository)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()

		mustWrite(t, leaf, []byte("a longer replacement payload\n"), 0o644)

		if _, _, err := dest.ReadTarget(1 << 20); !errors.Is(err, ErrGenerationConflict) {
			t.Fatalf("ReadTarget error = %v, want ErrGenerationConflict", err)
		}
	})

	t.Run("conflicts when the parent was replaced since capture", func(t *testing.T) {
		root, rootPath := tempRoot(t)
		layer := filepath.Join(rootPath, ".agents")
		mustMkdir(t, layer)
		mustWrite(t, filepath.Join(layer, "config.yaml"), []byte("original\n"), 0o644)
		dest, err := CaptureDestination(root, ".agents/config.yaml", LinkPolicyRepository)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()

		old := filepath.Join(rootPath, ".agents-old")
		if err := os.Rename(layer, old); err != nil {
			t.Fatal(err)
		}
		mustMkdir(t, layer)
		mustWrite(t, filepath.Join(layer, "config.yaml"), []byte("replacement tree\n"), 0o644)

		if _, _, err := dest.ReadTarget(1 << 20); !errors.Is(err, ErrGenerationConflict) {
			t.Fatalf("ReadTarget error = %v, want ErrGenerationConflict", err)
		}
		if got := mustRead(t, filepath.Join(layer, "config.yaml")); got != "replacement tree\n" {
			t.Fatalf("replacement tree content = %q", got)
		}
	})

	t.Run("enforces the read limit", func(t *testing.T) {
		root, rootPath := tempRoot(t)
		mustWrite(t, filepath.Join(rootPath, "config.yaml"), []byte("0123456789\n"), 0o644)
		dest, err := CaptureDestination(root, "config.yaml", LinkPolicyRepository)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()

		if _, _, err := dest.ReadTarget(4); !errors.Is(err, ErrTooLarge) {
			t.Fatalf("ReadTarget error = %v, want ErrTooLarge", err)
		}
	})
}
