//go:build !unix

package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGitCommandUsesAbsoluteCacheDirectory(t *testing.T) {
	cacheRoot := filepath.Join(t.TempDir(), "cache")
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relativeRoot, err := filepath.Rel(workingDir, cacheRoot)
	if err != nil {
		t.Fatal(err)
	}

	hashName := HashURL("https://example.com/registry.git")
	store, err := openCacheStore(relativeRoot, hashName)
	if err != nil {
		t.Fatalf("open cache store: %v", err)
	}
	defer store.close()

	cmd, err := gitCommand(store.dir(), []string{"--version"})
	if err != nil {
		t.Fatalf("create git command: %v", err)
	}
	if !filepath.IsAbs(cmd.Dir) {
		t.Fatalf("git command directory %q is not absolute", cmd.Dir)
	}
	if got, want := filepath.Clean(cmd.Dir), filepath.Clean(filepath.Join(cacheRoot, hashName)); got != want {
		t.Fatalf("git command directory = %q, want %q", got, want)
	}
}
