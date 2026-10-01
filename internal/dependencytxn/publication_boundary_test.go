package dependencytxn

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

type destinationEvidence struct {
	mode      os.FileMode
	modTimeNS int64
	inode     uint64
}

func observeDestination(t *testing.T, path string) destinationEvidence {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("stat payload for %s", path)
	}
	return destinationEvidence{mode: info.Mode().Perm(), modTimeNS: info.ModTime().UnixNano(), inode: stat.Ino}
}

// spec: config-mutation/repository-edit-refuses-an-external-configuration-target
func TestManagedPublicationRefusesExternalLinks(t *testing.T) {
	for _, path := range []string{"config.yaml", "lock.yaml", "config.local.yaml", "lock.local.yaml"} {
		t.Run(path, func(t *testing.T) {
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			repo := filepath.Join(base, "repo")
			agents := filepath.Join(repo, ".agents")
			if err := os.MkdirAll(agents, 0o755); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(base, "outside-"+path)
			before := []byte("bytes must not change\n")
			if err := os.WriteFile(outside, before, 0o640); err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(agents, path)
			if err := os.Symlink(outside, destination); err != nil {
				t.Fatal(err)
			}
			oldTarget := observeDestination(t, outside)
			oldLink := observeDestination(t, destination)
			expected, err := ReadGeneration(destination)
			if !errors.Is(err, ErrUnsafePath) {
				t.Fatalf("generation reader followed an external link: %v", err)
			}
			if _, err := Commit(t.Context(), t.TempDir(), []DesiredFile{{Path: destination, Expected: expected, Bytes: []byte("replacement\n"), Mode: 0o640}}); !errors.Is(err, ErrUnsafePath) {
				t.Fatalf("transaction followed an external link: %v", err)
			}
			if data, err := os.ReadFile(outside); err != nil || string(data) != string(before) {
				t.Fatalf("external bytes changed: %q %v", data, err)
			}
			if got := observeDestination(t, outside); got != oldTarget {
				t.Fatalf("external identity or metadata changed: %+v", got)
			}
			if got := observeDestination(t, destination); got.inode != oldLink.inode {
				t.Fatal("publication replaced the link")
			}
			assertNoStagingResidue(t, agents)
		})
	}
}
