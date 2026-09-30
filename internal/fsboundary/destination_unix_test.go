//go:build unix

package fsboundary

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink %s -> %s: %v", link, target, err)
	}
}

func TestCaptureDestinationDirectFile(t *testing.T) {
	root, rootPath := tempRoot(t)

	t.Run("existing singly linked regular file", func(t *testing.T) {
		path := filepath.Join(rootPath, "config.yaml")
		mustWrite(t, path, []byte("a: b\n"), 0o640)
		dest, err := CaptureDestination(root, "config.yaml", LinkPolicyRepository)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()
		if !dest.Exists() {
			t.Fatal("destination should exist")
		}
		if dest.Link() != nil {
			t.Fatal("direct file must not record a link")
		}
		if !dest.Identity().SameState(identityFromFileInfo(t, statOf(t, path))) {
			t.Fatal("captured identity does not match the destination")
		}
		if dest.Path() != filepath.Join(rootPath, "config.yaml") {
			t.Fatalf("effective path = %q", dest.Path())
		}
	})

	t.Run("absent leaf in existing parent", func(t *testing.T) {
		dest, err := CaptureDestination(root, "new.yaml", LinkPolicyRepository)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()
		if dest.Exists() {
			t.Fatal("absent destination must capture as not existing")
		}
	})

	t.Run("missing parent reports not-exist", func(t *testing.T) {
		_, err := CaptureDestination(root, "absent-dir/file.yaml", LinkPolicyRepository)
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("error = %v, want fs.ErrNotExist", err)
		}
	})

	t.Run("directory leaf refused", func(t *testing.T) {
		mustMkdir(t, filepath.Join(rootPath, "leafdir"))
		_, err := CaptureDestination(root, "leafdir", LinkPolicyRepository)
		if !errors.Is(err, ErrUnsafeFile) {
			t.Fatalf("error = %v, want ErrUnsafeFile", err)
		}
	})

	t.Run("multiply linked leaf refused", func(t *testing.T) {
		path := filepath.Join(rootPath, "hardlinked")
		mustWrite(t, path, []byte("x"), 0o600)
		if err := os.Link(path, filepath.Join(rootPath, "hardlinked-alias")); err != nil {
			t.Fatal(err)
		}
		_, err := CaptureDestination(root, "hardlinked", LinkPolicyRepository)
		if !errors.Is(err, ErrUnsafeFile) {
			t.Fatalf("error = %v, want ErrUnsafeFile", err)
		}
	})

	t.Run("escaping relative path refused", func(t *testing.T) {
		_, err := CaptureDestination(root, "../escape.yaml", LinkPolicyRepository)
		if !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("error = %v, want ErrUnsafePath", err)
		}
	})

	t.Run("symlinked intermediate ancestor refused", func(t *testing.T) {
		outside := t.TempDir()
		mustSymlink(t, outside, filepath.Join(rootPath, "ancestorlink"))
		_, err := CaptureDestination(root, "ancestorlink/file.yaml", LinkPolicyUser)
		if !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("error = %v, want ErrUnsafePath", err)
		}
	})
}

// userLinkFixture builds a user-scope root (<home>/.agents) and an external
// dotfiles directory outside it.
func userLinkFixture(t *testing.T) (root *Dir, agentsDir, dotfilesDir string) {
	t.Helper()
	home := t.TempDir()
	root, _ = captureRoot(t, home)
	agentsDir = filepath.Join(root.Path(), ".agents")
	dotfilesDir = filepath.Join(root.Path(), "dotfiles")
	mustMkdir(t, agentsDir)
	mustMkdir(t, dotfilesDir)
	return root, agentsDir, dotfilesDir
}

func TestCaptureDestinationUserLink(t *testing.T) {
	t.Run("absolute link to current-user-owned singly linked regular target", func(t *testing.T) {
		root, agentsDir, dotfilesDir := userLinkFixture(t)
		target := filepath.Join(dotfilesDir, "config.yaml")
		mustWrite(t, target, []byte("dotfile: true\n"), 0o640)
		link := filepath.Join(agentsDir, "config.yaml")
		mustSymlink(t, target, link)

		dest, err := CaptureDestination(root, ".agents/config.yaml", LinkPolicyUser)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()
		if !dest.Exists() {
			t.Fatal("linked destination should exist")
		}
		linkInfo := dest.Link()
		if linkInfo == nil {
			t.Fatal("approved user link must be recorded")
		}
		if linkInfo.Name != "config.yaml" {
			t.Fatalf("link name = %q", linkInfo.Name)
		}
		if !dest.Identity().SameState(identityFromFileInfo(t, statOf(t, target))) {
			t.Fatal("destination identity must bind to the captured target")
		}
		if dest.Path() != target {
			t.Fatalf("effective path = %q, want target %q", dest.Path(), target)
		}
	})

	t.Run("relative link escaping the root is allowed for user policy", func(t *testing.T) {
		root, agentsDir, dotfilesDir := userLinkFixture(t)
		target := filepath.Join(dotfilesDir, "relative.yaml")
		mustWrite(t, target, []byte("x\n"), 0o600)
		mustSymlink(t, "../dotfiles/relative.yaml", filepath.Join(agentsDir, "relative.yaml"))

		dest, err := CaptureDestination(root, ".agents/relative.yaml", LinkPolicyUser)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()
		if !dest.Exists() || dest.Link() == nil {
			t.Fatalf("relative user link should capture, exists=%v link=%v", dest.Exists(), dest.Link())
		}
	})

	t.Run("chained links resolve to a regular target", func(t *testing.T) {
		root, agentsDir, dotfilesDir := userLinkFixture(t)
		target := filepath.Join(dotfilesDir, "chained.yaml")
		mustWrite(t, target, []byte("x\n"), 0o600)
		second := filepath.Join(agentsDir, "second.yaml")
		mustSymlink(t, target, second)
		mustSymlink(t, "second.yaml", filepath.Join(agentsDir, "first.yaml"))

		dest, err := CaptureDestination(root, ".agents/first.yaml", LinkPolicyUser)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()
		if !dest.Exists() || dest.Link() == nil || dest.Link().Name != "first.yaml" {
			t.Fatalf("chained user link should capture the first link, got %+v", dest.Link())
		}
	})

	t.Run("unsafe user link shapes", func(t *testing.T) {
		cases := []struct {
			name    string
			setup   func(t *testing.T, agentsDir, dotfilesDir string) (rel string)
			wantErr error
		}{
			{
				name: "target is a directory",
				setup: func(t *testing.T, agentsDir, dotfilesDir string) string {
					mustSymlink(t, dotfilesDir, filepath.Join(agentsDir, "dir.yaml"))
					return ".agents/dir.yaml"
				},
				wantErr: ErrUnsafeFile,
			},
			{
				name: "target is multiply linked",
				setup: func(t *testing.T, agentsDir, dotfilesDir string) string {
					target := filepath.Join(dotfilesDir, "hard.yaml")
					mustWrite(t, target, []byte("x"), 0o600)
					if err := os.Link(target, filepath.Join(dotfilesDir, "hard-alias.yaml")); err != nil {
						t.Fatal(err)
					}
					mustSymlink(t, target, filepath.Join(agentsDir, "hard.yaml"))
					return ".agents/hard.yaml"
				},
				wantErr: ErrUnsafeFile,
			},
			{
				name: "dangling link",
				setup: func(t *testing.T, agentsDir, dotfilesDir string) string {
					mustSymlink(t, filepath.Join(dotfilesDir, "missing.yaml"), filepath.Join(agentsDir, "dangling.yaml"))
					return ".agents/dangling.yaml"
				},
				wantErr: ErrUnsafePath,
			},
			{
				name: "link loop",
				setup: func(t *testing.T, agentsDir, dotfilesDir string) string {
					mustSymlink(t, "loop-b.yaml", filepath.Join(agentsDir, "loop-a.yaml"))
					mustSymlink(t, "loop-a.yaml", filepath.Join(agentsDir, "loop-b.yaml"))
					return ".agents/loop-a.yaml"
				},
				wantErr: ErrUnsafePath,
			},
			{
				name: "target is a FIFO",
				setup: func(t *testing.T, agentsDir, dotfilesDir string) string {
					fifo := filepath.Join(dotfilesDir, "fifo.yaml")
					if err := unix.Mkfifo(fifo, 0o600); err != nil {
						t.Fatal(err)
					}
					mustSymlink(t, fifo, filepath.Join(agentsDir, "fifo.yaml"))
					return ".agents/fifo.yaml"
				},
				wantErr: ErrUnsafeFile,
			},
			{
				name: "link through a symlinked intermediate directory",
				setup: func(t *testing.T, agentsDir, dotfilesDir string) string {
					mustSymlink(t, dotfilesDir, filepath.Join(agentsDir, "linked-dir"))
					return ".agents/linked-dir/anything.yaml"
				},
				wantErr: ErrUnsafePath,
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				root, agentsDir, dotfilesDir := userLinkFixture(t)
				rel := tc.setup(t, agentsDir, dotfilesDir)
				dest, err := CaptureDestination(root, rel, LinkPolicyUser)
				if err == nil {
					_ = dest.Close()
					t.Fatalf("CaptureDestination(%s) succeeded, want %v", rel, tc.wantErr)
				}
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("error = %v, want %v", err, tc.wantErr)
				}
			})
		}
	})
}

// repoLinkFixture builds a repository root with .agents and shared
// directories, both inside the captured canonical repository.
func repoLinkFixture(t *testing.T) (root *Dir, agentsDir, sharedDir string) {
	t.Helper()
	root, repoPath := tempRoot(t)
	agentsDir = filepath.Join(repoPath, ".agents")
	sharedDir = filepath.Join(repoPath, "shared")
	mustMkdir(t, agentsDir)
	mustMkdir(t, sharedDir)
	return root, agentsDir, sharedDir
}

func TestCaptureDestinationRepositoryLink(t *testing.T) {
	t.Run("contained relative link", func(t *testing.T) {
		root, agentsDir, sharedDir := repoLinkFixture(t)
		target := filepath.Join(sharedDir, "config.yaml")
		mustWrite(t, target, []byte("shared: true\n"), 0o644)
		mustSymlink(t, "../shared/config.yaml", filepath.Join(agentsDir, "config.yaml"))

		dest, err := CaptureDestination(root, ".agents/config.yaml", LinkPolicyRepository)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()
		if !dest.Exists() || dest.Link() == nil {
			t.Fatalf("contained repository link should capture, exists=%v", dest.Exists())
		}
		if dest.Path() != target {
			t.Fatalf("effective path = %q, want %q", dest.Path(), target)
		}
	})

	t.Run("contained absolute link", func(t *testing.T) {
		root, agentsDir, sharedDir := repoLinkFixture(t)
		target := filepath.Join(sharedDir, "absolute.yaml")
		mustWrite(t, target, []byte("x\n"), 0o644)
		mustSymlink(t, target, filepath.Join(agentsDir, "absolute.yaml"))

		dest, err := CaptureDestination(root, ".agents/absolute.yaml", LinkPolicyRepository)
		if err != nil {
			t.Fatalf("CaptureDestination: %v", err)
		}
		defer func() { _ = dest.Close() }()
		if !dest.Exists() || dest.Link() == nil {
			t.Fatal("contained absolute repository link should capture")
		}
	})

	t.Run("unsafe repository link shapes", func(t *testing.T) {
		cases := []struct {
			name    string
			setup   func(t *testing.T, rootPath, agentsDir, sharedDir string) string
			wantErr error
		}{
			{
				name: "relative escape",
				setup: func(t *testing.T, rootPath, agentsDir, sharedDir string) string {
					mustWrite(t, filepath.Join(filepath.Dir(rootPath), "outside.yaml"), []byte("x"), 0o600)
					mustSymlink(t, "../../outside.yaml", filepath.Join(agentsDir, "escape.yaml"))
					return ".agents/escape.yaml"
				},
				wantErr: ErrUnsafePath,
			},
			{
				name: "absolute escape",
				setup: func(t *testing.T, rootPath, agentsDir, sharedDir string) string {
					outside := filepath.Join(t.TempDir(), "abs-outside.yaml")
					mustWrite(t, outside, []byte("x"), 0o600)
					mustSymlink(t, outside, filepath.Join(agentsDir, "abs-escape.yaml"))
					return ".agents/abs-escape.yaml"
				},
				wantErr: ErrUnsafePath,
			},
			{
				name: "intermediate escape after normalization",
				setup: func(t *testing.T, rootPath, agentsDir, sharedDir string) string {
					mustWrite(t, filepath.Join(filepath.Dir(rootPath), "norm-outside.yaml"), []byte("x"), 0o600)
					mustSymlink(t, "../shared/../../norm-outside.yaml", filepath.Join(agentsDir, "norm-escape.yaml"))
					return ".agents/norm-escape.yaml"
				},
				wantErr: ErrUnsafePath,
			},
			{
				name: "dangling contained link",
				setup: func(t *testing.T, rootPath, agentsDir, sharedDir string) string {
					mustSymlink(t, "../shared/missing.yaml", filepath.Join(agentsDir, "dangling.yaml"))
					return ".agents/dangling.yaml"
				},
				wantErr: ErrUnsafePath,
			},
			{
				name: "contained link loop",
				setup: func(t *testing.T, rootPath, agentsDir, sharedDir string) string {
					mustSymlink(t, "loop-b.yaml", filepath.Join(agentsDir, "loop-a.yaml"))
					mustSymlink(t, "loop-a.yaml", filepath.Join(agentsDir, "loop-b.yaml"))
					return ".agents/loop-a.yaml"
				},
				wantErr: ErrUnsafePath,
			},
			{
				name: "multiply linked target",
				setup: func(t *testing.T, rootPath, agentsDir, sharedDir string) string {
					target := filepath.Join(sharedDir, "hard.yaml")
					mustWrite(t, target, []byte("x"), 0o600)
					if err := os.Link(target, filepath.Join(sharedDir, "hard-alias.yaml")); err != nil {
						t.Fatal(err)
					}
					mustSymlink(t, "../shared/hard.yaml", filepath.Join(agentsDir, "hard.yaml"))
					return ".agents/hard.yaml"
				},
				wantErr: ErrUnsafeFile,
			},
			{
				name: "directory target",
				setup: func(t *testing.T, rootPath, agentsDir, sharedDir string) string {
					mustSymlink(t, "../shared", filepath.Join(agentsDir, "dirtarget.yaml"))
					return ".agents/dirtarget.yaml"
				},
				wantErr: ErrUnsafeFile,
			},
			{
				name: "symlinked intermediate on the target path",
				setup: func(t *testing.T, rootPath, agentsDir, sharedDir string) string {
					mustSymlink(t, "shared", filepath.Join(rootPath, "alias"))
					mustSymlink(t, "../alias/via-alias.yaml", filepath.Join(agentsDir, "via-alias.yaml"))
					return ".agents/via-alias.yaml"
				},
				wantErr: ErrUnsafePath,
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				root, agentsDir, sharedDir := repoLinkFixture(t)
				rel := tc.setup(t, root.Path(), agentsDir, sharedDir)
				dest, err := CaptureDestination(root, rel, LinkPolicyRepository)
				if err == nil {
					_ = dest.Close()
					t.Fatalf("CaptureDestination(%s) succeeded, want %v", rel, tc.wantErr)
				}
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("error = %v, want %v", err, tc.wantErr)
				}
			})
		}
	})
}
