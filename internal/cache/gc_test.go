package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestGarbageCollectReturnsEmptyWhenCacheDirMissing(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// No ~/.cache/agent-marketplace directory exists yet.
	res, err := GarbageCollect(false)
	if err != nil {
		t.Fatalf("expected no error when cache dir missing, got %v", err)
	}
	if len(res.Removed) != 0 {
		t.Fatalf("expected no removed entries, got %#v", res.Removed)
	}
}

func TestGarbageCollectEmptyCacheDirReturnsNothing(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	if err := os.MkdirAll(CacheDir(), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	res, err := GarbageCollect(false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Removed) != 0 {
		t.Fatalf("expected no removed entries, got %#v", res.Removed)
	}
}

func TestGarbageCollectKeepsActiveAndRemovesInactive(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	activeURL := "git@example.com:active.git"
	inactiveURL := "git@example.com:stale.git"
	activeHash := HashURL(activeURL)
	inactiveHash := HashURL(inactiveURL)

	// Seed both cache directories.
	activePath := filepath.Join(CacheDir(), activeHash)
	inactivePath := filepath.Join(CacheDir(), inactiveHash)
	if err := os.MkdirAll(filepath.Join(activePath, "main"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(inactivePath, "main"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cfg := &config.Config{
		Registries: []config.RegistryEntry{{URL: activeURL, Ref: "main"}},
	}

	res, err := GarbageCollect(false, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res.Removed) != 1 {
		t.Fatalf("expected 1 removed path, got %#v", res.Removed)
	}
	if res.Removed[0] != inactivePath {
		t.Fatalf("expected removed=%q, got %q", inactivePath, res.Removed[0])
	}

	// Active should still exist; inactive should be gone.
	if _, err := os.Stat(activePath); err != nil {
		t.Fatalf("active cache directory should still exist, got stat err %v", err)
	}
	if _, err := os.Stat(inactivePath); !os.IsNotExist(err) {
		t.Fatalf("inactive cache directory should have been removed, got stat err %v", err)
	}
}

func TestGarbageCollectDryRunReportsButDoesNotDelete(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	inactiveHash := HashURL("git@example.com:stale.git")
	inactivePath := filepath.Join(CacheDir(), inactiveHash)
	if err := os.MkdirAll(inactivePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	res, err := GarbageCollect(true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res.Removed) != 1 || res.Removed[0] != inactivePath {
		t.Fatalf("expected dry-run to list %q, got %#v", inactivePath, res.Removed)
	}

	// Path must still exist under dry-run.
	if _, err := os.Stat(inactivePath); err != nil {
		t.Fatalf("expected dry-run to preserve directory, stat err: %v", err)
	}
}

func TestGarbageCollectIgnoresFilesInCacheDir(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	if err := os.MkdirAll(CacheDir(), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A stray file directly under the cache dir must be ignored.
	strayPath := filepath.Join(CacheDir(), "README.txt")
	if err := os.WriteFile(strayPath, []byte("notes"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	res, err := GarbageCollect(false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Removed) != 0 {
		t.Fatalf("expected no removed entries, got %#v", res.Removed)
	}
	// Stray file should still exist.
	if _, err := os.Stat(strayPath); err != nil {
		t.Fatalf("expected stray file to be untouched, stat err: %v", err)
	}
}

func TestGarbageCollectHandlesNilAndEmptyConfigs(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	inactiveHash := HashURL("git@example.com:orphan.git")
	inactivePath := filepath.Join(CacheDir(), inactiveHash)
	if err := os.MkdirAll(inactivePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// nil config + empty config: still sweeps the orphan.
	res, err := GarbageCollect(false, nil, &config.Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Removed) != 1 || res.Removed[0] != inactivePath {
		t.Fatalf("expected orphan removed, got %#v", res.Removed)
	}
}

func TestGarbageCollectSkipsLocalAndBlankRegistries(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Create a cache entry that would only stay if local/blank registries were
	// (incorrectly) hashed into the active set.
	staleHash := HashURL("git@example.com:stale.git")
	stalePath := filepath.Join(CacheDir(), staleHash)
	if err := os.MkdirAll(stalePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "/local/skills"},  // local: skipped
			{URL: "  "},             // blank: skipped
			{URL: "./rel", Ref: ""}, // local: skipped
		},
	}

	res, err := GarbageCollect(false, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No active git registries → stale is removed.
	if len(res.Removed) != 1 || res.Removed[0] != stalePath {
		t.Fatalf("expected stale removed, got %#v", res.Removed)
	}
}

func TestGarbageCollectResultIsSorted(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Seed multiple inactive directories; their names must be sorted in result.
	hashes := []string{
		HashURL("git@example.com:zzz.git"),
		HashURL("git@example.com:aaa.git"),
		HashURL("git@example.com:mmm.git"),
	}
	for _, h := range hashes {
		p := filepath.Join(CacheDir(), h)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	res, err := GarbageCollect(true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Removed) != 3 {
		t.Fatalf("expected 3 paths, got %#v", res.Removed)
	}
	if !slices.IsSorted(res.Removed) {
		t.Fatalf("expected sorted output, got %#v", res.Removed)
	}
}

func TestGarbageCollectDedupesSameHashAcrossConfigs(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	url := "git@example.com:active.git"
	activePath := filepath.Join(CacheDir(), HashURL(url))
	if err := os.MkdirAll(activePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cfgA := &config.Config{Registries: []config.RegistryEntry{{URL: url, Ref: "main"}}}
	cfgB := &config.Config{Registries: []config.RegistryEntry{{URL: url, Ref: "v1"}}}

	res, err := GarbageCollect(false, cfgA, cfgB)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Removed) != 0 {
		t.Fatalf("expected active registry (present in both configs) to survive, got removed=%#v", res.Removed)
	}
	if _, err := os.Stat(activePath); err != nil {
		t.Fatalf("expected active path to remain, stat err: %v", err)
	}
}

func TestGarbageCollectPreservesMarketplaceCacheNamespace(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(fmt.Sprintf("dry_run_%t", dryRun), func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)

			marketplacePath := filepath.Join(CacheDir(), marketplaceCacheDirName, "artifacts", "sha256-deadbeef")
			stalePath := filepath.Join(CacheDir(), "stale")
			if err := os.MkdirAll(marketplacePath, 0o755); err != nil {
				t.Fatalf("mkdir marketplace cache: %v", err)
			}
			if err := os.MkdirAll(stalePath, 0o755); err != nil {
				t.Fatalf("mkdir stale cache: %v", err)
			}

			result, err := GarbageCollect(dryRun)
			if err != nil {
				t.Fatalf("GarbageCollect: %v", err)
			}
			if len(result.Removed) != 1 || result.Removed[0] != stalePath {
				t.Fatalf("removed = %v, want only stale path %q", result.Removed, stalePath)
			}
			if _, err := os.Stat(marketplacePath); err != nil {
				t.Fatalf("marketplace cache should remain after dry_run=%t: %v", dryRun, err)
			}
			if _, err := os.Stat(stalePath); dryRun && err != nil {
				t.Fatalf("stale cache should remain during dry-run: %v", err)
			} else if !dryRun && !os.IsNotExist(err) {
				t.Fatalf("stale cache should be removed after non-dry run, stat err: %v", err)
			}
		})
	}
}
