//go:build unix

package fsboundary

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// stageFile writes a staged payload in dir and captures dir as the staging
// parent.
func stageFile(t *testing.T, dir, name string, data []byte) *Dir {
	t.Helper()
	mustWrite(t, filepath.Join(dir, name), data, 0o640)
	staged, err := OpenDir(dir)
	if err != nil {
		t.Fatalf("OpenDir(staging %s): %v", dir, err)
	}
	t.Cleanup(func() { _ = staged.Close() })
	return staged
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestPublishDirectDestination(t *testing.T) {
	t.Run("publish creates an absent destination", func(t *testing.T) {
		root, rootPath := tempRoot(t)
		mustMkdir(t, filepath.Join(rootPath, "staging"))
		dest, err := CaptureDestination(root, "config.yaml", LinkPolicyRepository)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()
		if dest.Exists() {
			t.Fatal("destination should be absent")
		}

		staged := stageFile(t, filepath.Join(rootPath, "staging"), "payload", []byte("published: true\n"))
		if err := dest.Publish(staged, "payload"); err != nil {
			t.Fatalf("Publish: %v", err)
		}
		if got := mustRead(t, filepath.Join(rootPath, "config.yaml")); got != "published: true\n" {
			t.Fatalf("published content = %q", got)
		}
		if _, err := os.Lstat(filepath.Join(rootPath, "staging", "payload")); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("staged name should move on success, lstat error = %v", err)
		}
	})

	t.Run("publish replaces an existing destination", func(t *testing.T) {
		root, rootPath := tempRoot(t)
		mustMkdir(t, filepath.Join(rootPath, "staging"))
		path := filepath.Join(rootPath, "config.yaml")
		mustWrite(t, path, []byte("old\n"), 0o600)
		dest, err := CaptureDestination(root, "config.yaml", LinkPolicyRepository)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()

		staged := stageFile(t, filepath.Join(rootPath, "staging"), "payload", []byte("new\n"))
		if err := dest.Publish(staged, "payload"); err != nil {
			t.Fatalf("Publish: %v", err)
		}
		if got := mustRead(t, path); got != "new\n" {
			t.Fatalf("published content = %q", got)
		}
	})
}

func TestPublishThroughLinks(t *testing.T) {
	t.Run("user link target replaced and link preserved", func(t *testing.T) {
		root, agentsDir, dotfilesDir := userLinkFixture(t)
		target := filepath.Join(dotfilesDir, "config.yaml")
		mustWrite(t, target, []byte("old\n"), 0o640)
		link := filepath.Join(agentsDir, "config.yaml")
		mustSymlink(t, target, link)
		mustMkdir(t, filepath.Join(root.Path(), "staging"))

		dest, err := CaptureDestination(root, ".agents/config.yaml", LinkPolicyUser)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()
		staged := stageFile(t, filepath.Join(root.Path(), "staging"), "payload", []byte("new\n"))
		if err := dest.Publish(staged, "payload"); err != nil {
			t.Fatalf("Publish: %v", err)
		}
		if got := mustRead(t, target); got != "new\n" {
			t.Fatalf("target content = %q", got)
		}
		info := lstatOf(t, link)
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatal("logical leaf must remain a symlink after publishing")
		}
		text, err := os.Readlink(link)
		if err != nil || text != target {
			t.Fatalf("link text = %q, %v", text, err)
		}
	})

	t.Run("repository link target replaced and link preserved", func(t *testing.T) {
		root, agentsDir, sharedDir := repoLinkFixture(t)
		target := filepath.Join(sharedDir, "config.yaml")
		mustWrite(t, target, []byte("old\n"), 0o644)
		link := filepath.Join(agentsDir, "config.yaml")
		mustSymlink(t, "../shared/config.yaml", link)
		mustMkdir(t, filepath.Join(root.Path(), "staging"))

		dest, err := CaptureDestination(root, ".agents/config.yaml", LinkPolicyRepository)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()
		staged := stageFile(t, filepath.Join(root.Path(), "staging"), "payload", []byte("new\n"))
		if err := dest.Publish(staged, "payload"); err != nil {
			t.Fatalf("Publish: %v", err)
		}
		if got := mustRead(t, target); got != "new\n" {
			t.Fatalf("target content = %q", got)
		}
		if lstatOf(t, link).Mode()&os.ModeSymlink == 0 {
			t.Fatal("repository link must survive publishing")
		}
	})
}

func TestRemoveLeaf(t *testing.T) {
	t.Run("direct leaf is removed", func(t *testing.T) {
		root, rootPath := tempRoot(t)
		path := filepath.Join(rootPath, "config.yaml")
		mustWrite(t, path, []byte("x\n"), 0o600)
		dest, err := CaptureDestination(root, "config.yaml", LinkPolicyRepository)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()
		if err := dest.RemoveLeaf(); err != nil {
			t.Fatalf("RemoveLeaf: %v", err)
		}
		if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("leaf should be gone, lstat error = %v", err)
		}
	})

	t.Run("absent leaf removes as a no-op", func(t *testing.T) {
		root, _ := tempRoot(t)
		dest, err := CaptureDestination(root, "missing.yaml", LinkPolicyRepository)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()
		if err := dest.RemoveLeaf(); err != nil {
			t.Fatalf("RemoveLeaf: %v", err)
		}
	})

	t.Run("user link removal preserves the target", func(t *testing.T) {
		root, agentsDir, dotfilesDir := userLinkFixture(t)
		target := filepath.Join(dotfilesDir, "config.yaml")
		mustWrite(t, target, []byte("keep me\n"), 0o640)
		link := filepath.Join(agentsDir, "config.yaml")
		mustSymlink(t, target, link)
		before := identityFromFileInfo(t, statOf(t, target))

		dest, err := CaptureDestination(root, ".agents/config.yaml", LinkPolicyUser)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()
		if err := dest.RemoveLeaf(); err != nil {
			t.Fatalf("RemoveLeaf: %v", err)
		}
		if _, err := os.Lstat(link); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("link should be gone, lstat error = %v", err)
		}
		if got := mustRead(t, target); got != "keep me\n" {
			t.Fatalf("target content = %q", got)
		}
		if !identityFromFileInfo(t, statOf(t, target)).SameState(before) {
			t.Fatal("target identity changed while removing the link")
		}
	})

	t.Run("repository link removal preserves the target", func(t *testing.T) {
		root, agentsDir, sharedDir := repoLinkFixture(t)
		target := filepath.Join(sharedDir, "config.yaml")
		mustWrite(t, target, []byte("keep me\n"), 0o644)
		link := filepath.Join(agentsDir, "config.yaml")
		mustSymlink(t, "../shared/config.yaml", link)

		dest, err := CaptureDestination(root, ".agents/config.yaml", LinkPolicyRepository)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()
		if err := dest.RemoveLeaf(); err != nil {
			t.Fatalf("RemoveLeaf: %v", err)
		}
		if _, err := os.Lstat(link); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("link should be gone, lstat error = %v", err)
		}
		if got := mustRead(t, target); got != "keep me\n" {
			t.Fatalf("target content = %q", got)
		}
	})

	t.Run("replaced leaf refuses and preserves the replacement", func(t *testing.T) {
		root, rootPath := tempRoot(t)
		path := filepath.Join(rootPath, "config.yaml")
		mustWrite(t, path, []byte("original\n"), 0o600)
		dest, err := CaptureDestination(root, "config.yaml", LinkPolicyRepository)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()
		mustWrite(t, filepath.Join(rootPath, "replacement"), []byte("replacement\n"), 0o600)
		if err := os.Rename(filepath.Join(rootPath, "replacement"), path); err != nil {
			t.Fatal(err)
		}

		if err := dest.RemoveLeaf(); !errors.Is(err, ErrGenerationConflict) {
			t.Fatalf("RemoveLeaf error = %v, want ErrGenerationConflict", err)
		}
		if got := mustRead(t, path); got != "replacement\n" {
			t.Fatalf("replacement content = %q", got)
		}
	})
}

func TestPublishRefusalPreservesState(t *testing.T) {
	t.Run("staged directory refuses and preserves everything", func(t *testing.T) {
		root, rootPath := tempRoot(t)
		path := filepath.Join(rootPath, "config.yaml")
		mustWrite(t, path, []byte("original\n"), 0o600)
		before := identityFromFileInfo(t, statOf(t, path))
		mustMkdir(t, filepath.Join(rootPath, "staging", "payload"))
		staging, err := OpenDir(filepath.Join(rootPath, "staging"))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = staging.Close() }()

		dest, err := CaptureDestination(root, "config.yaml", LinkPolicyRepository)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()
		if err := dest.Publish(staging, "payload"); !errors.Is(err, ErrUnsafeFile) {
			t.Fatalf("Publish error = %v, want ErrUnsafeFile", err)
		}
		if !identityFromFileInfo(t, statOf(t, path)).SameState(before) {
			t.Fatal("destination changed despite refusal")
		}
		if got := mustRead(t, path); got != "original\n" {
			t.Fatalf("destination content = %q", got)
		}
		if !statOf(t, filepath.Join(rootPath, "staging", "payload")).IsDir() {
			t.Fatal("staged directory must survive the refusal")
		}
	})

	t.Run("destination appeared after absent capture refuses", func(t *testing.T) {
		root, rootPath := tempRoot(t)
		mustMkdir(t, filepath.Join(rootPath, "staging"))
		dest, err := CaptureDestination(root, "config.yaml", LinkPolicyRepository)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()
		mustWrite(t, filepath.Join(rootPath, "config.yaml"), []byte("appeared\n"), 0o600)

		staged := stageFile(t, filepath.Join(rootPath, "staging"), "payload", []byte("staged\n"))
		if err := dest.Publish(staged, "payload"); !errors.Is(err, ErrGenerationConflict) {
			t.Fatalf("Publish error = %v, want ErrGenerationConflict", err)
		}
		if got := mustRead(t, filepath.Join(rootPath, "config.yaml")); got != "appeared\n" {
			t.Fatalf("appeared file content = %q", got)
		}
		if got := mustRead(t, filepath.Join(rootPath, "staging", "payload")); got != "staged\n" {
			t.Fatalf("staged content = %q", got)
		}
	})

	t.Run("destination appeared as a directory refuses", func(t *testing.T) {
		root, rootPath := tempRoot(t)
		mustMkdir(t, filepath.Join(rootPath, "staging"))
		dest, err := CaptureDestination(root, "config.yaml", LinkPolicyRepository)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()
		mustMkdir(t, filepath.Join(rootPath, "config.yaml"))

		staged := stageFile(t, filepath.Join(rootPath, "staging"), "payload", []byte("staged\n"))
		if err := dest.Publish(staged, "payload"); !errors.Is(err, ErrGenerationConflict) {
			t.Fatalf("Publish error = %v, want ErrGenerationConflict", err)
		}
		if !statOf(t, filepath.Join(rootPath, "config.yaml")).IsDir() {
			t.Fatal("appeared directory must survive the refusal")
		}
		if got := mustRead(t, filepath.Join(rootPath, "staging", "payload")); got != "staged\n" {
			t.Fatalf("staged content = %q", got)
		}
	})

	t.Run("repointed user link refuses and preserves both targets", func(t *testing.T) {
		root, agentsDir, dotfilesDir := userLinkFixture(t)
		first := filepath.Join(dotfilesDir, "first.yaml")
		second := filepath.Join(dotfilesDir, "second.yaml")
		mustWrite(t, first, []byte("first\n"), 0o640)
		mustWrite(t, second, []byte("second\n"), 0o640)
		link := filepath.Join(agentsDir, "config.yaml")
		mustSymlink(t, first, link)
		mustMkdir(t, filepath.Join(root.Path(), "staging"))

		dest, err := CaptureDestination(root, ".agents/config.yaml", LinkPolicyUser)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
		mustSymlink(t, second, link)

		staged := stageFile(t, filepath.Join(root.Path(), "staging"), "payload", []byte("staged\n"))
		if err := dest.Publish(staged, "payload"); !errors.Is(err, ErrGenerationConflict) {
			t.Fatalf("Publish error = %v, want ErrGenerationConflict", err)
		}
		if got := mustRead(t, first); got != "first\n" {
			t.Fatalf("first target content = %q", got)
		}
		if got := mustRead(t, second); got != "second\n" {
			t.Fatalf("second target content = %q", got)
		}
		if got := mustRead(t, filepath.Join(root.Path(), "staging", "payload")); got != "staged\n" {
			t.Fatalf("staged content = %q", got)
		}
	})

	t.Run("replaced link target refuses and preserves the replacement", func(t *testing.T) {
		root, agentsDir, dotfilesDir := userLinkFixture(t)
		target := filepath.Join(dotfilesDir, "config.yaml")
		mustWrite(t, target, []byte("original\n"), 0o640)
		link := filepath.Join(agentsDir, "config.yaml")
		mustSymlink(t, target, link)
		mustMkdir(t, filepath.Join(root.Path(), "staging"))

		dest, err := CaptureDestination(root, ".agents/config.yaml", LinkPolicyUser)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()
		mustWrite(t, filepath.Join(dotfilesDir, "replacement"), []byte("replacement\n"), 0o600)
		if err := os.Rename(filepath.Join(dotfilesDir, "replacement"), target); err != nil {
			t.Fatal(err)
		}

		staged := stageFile(t, filepath.Join(root.Path(), "staging"), "payload", []byte("staged\n"))
		if err := dest.Publish(staged, "payload"); !errors.Is(err, ErrGenerationConflict) {
			t.Fatalf("Publish error = %v, want ErrGenerationConflict", err)
		}
		if got := mustRead(t, target); got != "replacement\n" {
			t.Fatalf("replacement content = %q", got)
		}
		if got := mustRead(t, filepath.Join(root.Path(), "staging", "payload")); got != "staged\n" {
			t.Fatalf("staged content = %q", got)
		}
		if lstatOf(t, link).Mode()&os.ModeSymlink == 0 {
			t.Fatal("link must survive the refusal")
		}
	})
}

// TestDescriptorLeafReplacementBarrier replaces the leaf after capture and
// requires every operation to fail first: nothing may be written or removed,
// the replacement's bytes stay exact, and caller-owned staging survives.
func TestDescriptorLeafReplacementBarrier(t *testing.T) {
	root, rootPath := tempRoot(t)
	path := filepath.Join(rootPath, "leaf.yaml")
	mustWrite(t, path, []byte("original\n"), 0o600)
	mustMkdir(t, filepath.Join(rootPath, "staging"))

	dest, err := CaptureDestination(root, "leaf.yaml", LinkPolicyRepository)
	if err != nil {
		t.Fatalf("CaptureDestination: %v", err)
	}
	defer func() { _ = dest.Close() }()

	mustWrite(t, filepath.Join(rootPath, "swapped"), []byte("swapped\n"), 0o644)
	if err := os.Rename(filepath.Join(rootPath, "swapped"), path); err != nil {
		t.Fatal(err)
	}
	swapped := identityFromFileInfo(t, statOf(t, path))

	staged := stageFile(t, filepath.Join(rootPath, "staging"), "payload", []byte("staged\n"))
	if err := dest.Publish(staged, "payload"); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("Publish error = %v, want ErrGenerationConflict", err)
	}
	if got := mustRead(t, path); got != "swapped\n" {
		t.Fatalf("replacement content = %q", got)
	}
	if !identityFromFileInfo(t, statOf(t, path)).SameState(swapped) {
		t.Fatal("replacement identity changed despite refusal")
	}
	if got := mustRead(t, filepath.Join(rootPath, "staging", "payload")); got != "staged\n" {
		t.Fatalf("staged content = %q", got)
	}

	if err := dest.RemoveLeaf(); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("RemoveLeaf error = %v, want ErrGenerationConflict", err)
	}
	if got := mustRead(t, path); got != "swapped\n" {
		t.Fatalf("replacement content after removal attempt = %q", got)
	}
}

// TestDescriptorParentReplacementBarrier replaces the destination's parent
// directory after capture and requires every operation to fail first: the
// captured (renamed-away) parent and the replacement tree must both stay
// byte-exact, and caller-owned staging survives.
func TestDescriptorParentReplacementBarrier(t *testing.T) {
	root, rootPath := tempRoot(t)
	sub := filepath.Join(rootPath, "sub")
	mustMkdir(t, sub)
	mustWrite(t, filepath.Join(sub, "leaf.yaml"), []byte("original\n"), 0o600)
	mustMkdir(t, filepath.Join(rootPath, "staging"))

	dest, err := CaptureDestination(root, "sub/leaf.yaml", LinkPolicyRepository)
	if err != nil {
		t.Fatalf("CaptureDestination: %v", err)
	}
	defer func() { _ = dest.Close() }()

	oldSub := filepath.Join(rootPath, "sub-old")
	if err := os.Rename(sub, oldSub); err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, sub)
	mustWrite(t, filepath.Join(sub, "leaf.yaml"), []byte("replacement tree\n"), 0o600)

	staged := stageFile(t, filepath.Join(rootPath, "staging"), "payload", []byte("staged\n"))
	if err := dest.Publish(staged, "payload"); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("Publish error = %v, want ErrGenerationConflict", err)
	}
	if got := mustRead(t, filepath.Join(sub, "leaf.yaml")); got != "replacement tree\n" {
		t.Fatalf("replacement tree content = %q", got)
	}
	if got := mustRead(t, filepath.Join(oldSub, "leaf.yaml")); got != "original\n" {
		t.Fatalf("captured parent content = %q", got)
	}
	if got := mustRead(t, filepath.Join(rootPath, "staging", "payload")); got != "staged\n" {
		t.Fatalf("staged content = %q", got)
	}

	if err := dest.RemoveLeaf(); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("RemoveLeaf error = %v, want ErrGenerationConflict", err)
	}
	if got := mustRead(t, filepath.Join(sub, "leaf.yaml")); got != "replacement tree\n" {
		t.Fatalf("replacement tree content after removal attempt = %q", got)
	}
	if got := mustRead(t, filepath.Join(oldSub, "leaf.yaml")); got != "original\n" {
		t.Fatalf("captured parent content after removal attempt = %q", got)
	}
}
