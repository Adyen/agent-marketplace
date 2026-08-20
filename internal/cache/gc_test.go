package cache

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

type cancelAfterChecksContext struct {
	checks   atomic.Int32
	cancelAt int32
}

func (c *cancelAfterChecksContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *cancelAfterChecksContext) Done() <-chan struct{}       { return nil }
func (c *cancelAfterChecksContext) Err() error {
	if c.checks.Add(1) >= c.cancelAt {
		return context.Canceled
	}
	return nil
}
func (c *cancelAfterChecksContext) Value(any) any { return nil }

func TestLegacyGarbageCollectHonorsCancellationBeforeEachRemoval(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	first := filepath.Join(CacheDir(), "first")
	second := filepath.Join(CacheDir(), "second")
	for _, path := range []string{first, second} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ctx := &cancelAfterChecksContext{cancelAt: 5}
	result, err := garbageCollectWithContext(ctx, false, &config.Config{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("legacy GC error = %v, want context.Canceled", err)
	}
	if len(result.Removed) != 1 || result.Removed[0] != first {
		t.Fatalf("cancelled legacy GC removals = %v, want only %q", result.Removed, first)
	}
	if _, statErr := os.Stat(first); !os.IsNotExist(statErr) {
		t.Fatalf("first candidate was not removed before cancellation: %v", statErr)
	}
	if _, statErr := os.Stat(second); statErr != nil {
		t.Fatalf("cancellation did not stop before the second candidate: %v", statErr)
	}
}

func TestGarbageCollectReturnsEmptyWhenCacheDirMissing(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// No ~/.cache/agent-marketplace directory exists yet.
	res, err := GarbageCollect(false, &config.Config{})
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

	res, err := GarbageCollect(false, &config.Config{})
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

	res, err := GarbageCollect(true, &config.Config{})
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

	res, err := GarbageCollect(false, &config.Config{})
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

func TestGarbageCollectRequiresConfigFailsClosed(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	inactiveHash := HashURL("git@example.com:orphan.git")
	inactivePath := filepath.Join(CacheDir(), inactiveHash)
	if err := os.MkdirAll(inactivePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	res, err := GarbageCollect(false)
	if err == nil {
		t.Fatal("expected missing effective config error")
	}
	if len(res.Removed) != 0 {
		t.Fatalf("missing-config GC reported removals: %#v", res.Removed)
	}
	if _, statErr := os.Stat(inactivePath); statErr != nil {
		t.Fatalf("missing-config GC mutated cache: %v", statErr)
	}
}

func TestGarbageCollectIgnoresNilConfigWhenEffectiveConfigSupplied(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	inactiveHash := HashURL("git@example.com:orphan.git")
	inactivePath := filepath.Join(CacheDir(), inactiveHash)
	if err := os.MkdirAll(inactivePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// A nil layer is harmless when a nonnil effective config is supplied.
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

	res, err := GarbageCollect(true, &config.Config{})
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

func TestGarbageCollectRejectsSymlinkedRootAndPreservesExternalTarget(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	externalRoot := t.TempDir()
	externalPath := filepath.Join(externalRoot, "stale")
	if err := os.MkdirAll(externalPath, 0o755); err != nil {
		t.Fatalf("mkdir external cache: %v", err)
	}
	cacheRoot := CacheDir()
	if err := os.MkdirAll(filepath.Dir(cacheRoot), 0o755); err != nil {
		t.Fatalf("mkdir cache parent: %v", err)
	}
	if err := os.Symlink(externalRoot, cacheRoot); err != nil {
		t.Fatalf("symlink cache root: %v", err)
	}

	result, err := GarbageCollect(false, &config.Config{})
	if err == nil {
		t.Fatal("expected symlinked cache root error")
	}
	if len(result.Removed) != 0 {
		t.Fatalf("symlinked root reported removals: %v", result.Removed)
	}
	if _, err := os.Stat(externalPath); err != nil {
		t.Fatalf("symlinked root touched external target: %v", err)
	}
}

func TestGarbageCollectRejectsSymlinkedCacheAncestorAndPreservesExternalTarget(t *testing.T) {
	homeTarget := t.TempDir()
	homeLinkParent := t.TempDir()
	homeLink := filepath.Join(homeLinkParent, "home")
	t.Setenv("HOME", homeLink)
	externalPath := filepath.Join(homeTarget, ".cache", "agent-marketplace", "stale")
	if err := os.MkdirAll(externalPath, 0o755); err != nil {
		t.Fatalf("mkdir external cache: %v", err)
	}
	if err := os.Symlink(homeTarget, homeLink); err != nil {
		t.Fatalf("symlink home ancestor: %v", err)
	}

	result, err := GarbageCollect(false, &config.Config{})
	if err == nil {
		t.Fatal("expected symlinked cache ancestor error")
	}
	if len(result.Removed) != 0 {
		t.Fatalf("symlinked ancestor reported removals: %v", result.Removed)
	}
	if _, err := os.Stat(externalPath); err != nil {
		t.Fatalf("symlinked ancestor touched external target: %v", err)
	}
}

func TestGarbageCollectRejectsNonDirectoryCacheRoot(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	cacheRoot := CacheDir()
	if err := os.MkdirAll(filepath.Dir(cacheRoot), 0o755); err != nil {
		t.Fatalf("mkdir cache parent: %v", err)
	}
	if err := os.WriteFile(cacheRoot, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write cache root: %v", err)
	}

	result, err := GarbageCollect(false, &config.Config{})
	if err == nil {
		t.Fatal("expected non-directory cache root error")
	}
	if len(result.Removed) != 0 {
		t.Fatalf("non-directory root reported removals: %v", result.Removed)
	}
	if raw, readErr := os.ReadFile(cacheRoot); readErr != nil || string(raw) != "not a directory" {
		t.Fatalf("non-directory cache root was changed: raw=%q err=%v", raw, readErr)
	}
}

func TestExactGitObjectGCReachability(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	objectPath := func(source string) string {
		return filepath.Join(CacheDir(), "exact-git", "objects", fmt.Sprintf("%x.git", sha256.Sum256([]byte(source))))
	}
	locked := objectPath("https://example.com/locked")
	configured := objectPath("https://example.com/configured")
	included := objectPath("https://example.com/included")
	orphan := objectPath("https://example.com/orphan")
	local := objectPath("/local/catalog")
	root := filepath.Dir(orphan)
	unknown := []string{filepath.Join(root, "notes.git"), filepath.Join(root, strings.Repeat("1", 64)), filepath.Join(root, strings.Repeat("A", 64)+".git"), filepath.Join(root, strings.Repeat("1", 63)+".git"), filepath.Join(root, strings.Repeat("1", 64)+".git.extra")}
	retained := append([]string{locked, configured, included}, unknown...)
	unlockedSnapshot := filepath.Join(CacheDir(), "exact-git", "snapshots", fmt.Sprintf("%x", sha256.Sum256([]byte("https://example.com/configured\x00sha1:"+strings.Repeat("1", 40)))))
	removed := []string{orphan, local, unlockedSnapshot, filepath.Join(root, ".objects-stage-orphan")}
	slices.Sort(removed)
	for _, path := range append(append([]string{}, retained...), removed...) {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	coordination := []string{filepath.Join(CacheDir(), "exact-git", "locks", "persistent.lock"), CacheDir() + ".registry-persistent.lock"}
	for _, path := range coordination {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("persistent\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	oid, err := ParseGitOID("sha1:" + strings.Repeat("1", 40))
	if err != nil {
		t.Fatal(err)
	}
	docs := []lockfile.Document{{APIVersion: lockfile.APIVersionV1, Scope: lockfile.ScopeRepo, Registries: []lockfile.GitRegistry{{URL: "https://EXAMPLE.com/locked.git", Ref: "main", Commit: oid}}}}
	configs := []*config.Config{nil, {Registries: []config.RegistryEntry{{URL: "https://EXAMPLE.com/configured.git", Ref: "main"}, {URL: "/local/catalog"}}}, {Registries: []config.RegistryEntry{{URL: "https://example.com/included", Ref: "other"}}}}
	for _, dryRun := range []bool{true, false} {
		result, err := GarbageCollectWithLocks(dryRun, docs, configs...)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(result.Removed, removed) {
			t.Fatalf("dryRun=%v: removed=%v, want %v", dryRun, result.Removed, removed)
		}
		for _, path := range retained {
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("retained %q: %v", path, err)
			}
		}
		for _, path := range removed {
			_, err := os.Stat(path)
			if dryRun && err != nil || !dryRun && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("dryRun=%v candidate %q: %v", dryRun, path, err)
			}
		}
		for _, path := range coordination {
			raw, err := os.ReadFile(path)
			if err != nil || string(raw) != "persistent\n" {
				t.Fatalf("coordination changed %q: %q, %v", path, raw, err)
			}
		}
	}
}

func TestExactGitObjectGCRetainsAbsoluteGitConfigSources(t *testing.T) {
	rawPath := filepath.Join(t.TempDir(), "catalog # space.git")
	fileURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(rawPath)}).String()
	canonical := (&url.URL{Scheme: "file", Path: filepath.ToSlash(strings.TrimSuffix(rawPath, ".git"))}).String()
	if _, err := lockfile.CanonicalGitURL(rawPath); err == nil {
		t.Fatal("lock wire canonicalization accepted a raw filesystem path")
	}
	for _, input := range []string{rawPath, fileURL} {
		t.Run(input, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			retained := filepath.Join(CacheDir(), "exact-git", "objects", fmt.Sprintf("%x.git", sha256.Sum256([]byte(canonical))))
			orphan := exactGitObjectStorePath("https://example.com/orphan")
			for _, path := range []string{retained, orphan} {
				if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			cfg := &config.Config{Registries: []config.RegistryEntry{{URL: input, Ref: "main"}}}
			for _, dryRun := range []bool{true, false} {
				result, err := GarbageCollectWithLocks(dryRun, nil, cfg)
				if err != nil || !slices.Equal(result.Removed, []string{orphan}) {
					t.Fatalf("dryRun=%v: result=%+v err=%v", dryRun, result, err)
				}
				if _, err := os.Stat(retained); err != nil {
					t.Fatalf("active file source removed: %v", err)
				}
				_, err = os.Stat(orphan)
				if dryRun && err != nil || !dryRun && !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("dryRun=%v orphan: %v", dryRun, err)
				}
			}
		})
	}
}

func TestExactGitObjectGCRejectsMalformedReachability(t *testing.T) {
	for _, invalidLock := range []bool{false, true} {
		t.Run(fmt.Sprint(invalidLock), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			object := exactGitObjectStorePath("https://example.com/orphan")
			legacy := filepath.Join(CacheDir(), "legacy-orphan")
			for _, path := range []string{object, legacy} {
				if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			cfg := &config.Config{}
			var docs []lockfile.Document
			wantCode := lockfile.CodeInvalidGitURL
			if invalidLock {
				docs = []lockfile.Document{{APIVersion: "invalid"}}
				wantCode = lockfile.CodeUnsupportedVersion
			} else {
				cfg.Registries = []config.RegistryEntry{{URL: "https://example.com/%zz"}}
			}
			for _, dryRun := range []bool{true, false} {
				result, err := GarbageCollectWithLocks(dryRun, docs, cfg)
				var codedErr *lockfile.Error
				if !errors.As(err, &codedErr) || codedErr.Code != wantCode || len(result.Removed) != 0 {
					t.Fatalf("invalid inventory: result=%+v err=%v, want %s", result, err, wantCode)
				}
				for _, path := range []string{object, legacy} {
					if _, err := os.Stat(path); err != nil {
						t.Fatalf("invalid inventory removed %q: %v", path, err)
					}
				}
			}
		})
	}
}

func TestExactGitObjectGCRejectsUnsafeCandidates(t *testing.T) {
	for _, symlink := range []bool{true, false} {
		t.Run(fmt.Sprint(symlink), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			candidate := exactGitObjectStorePath("https://example.com/orphan")
			external := filepath.Join(t.TempDir(), "user-bytes")
			if err := os.WriteFile(external, []byte("keep\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(candidate), 0o700); err != nil {
				t.Fatal(err)
			}
			if symlink {
				if err := os.Symlink(filepath.Dir(external), candidate); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(candidate, []byte("keep\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, dryRun := range []bool{true, false} {
				result, err := GarbageCollectWithLocks(dryRun, nil, &config.Config{})
				if err == nil || len(result.Removed) != 0 {
					t.Fatalf("unsafe candidate: result=%+v err=%v", result, err)
				}
				if _, err := os.Lstat(candidate); err != nil {
					t.Fatal(err)
				}
				raw, err := os.ReadFile(external)
				if err != nil || string(raw) != "keep\n" {
					t.Fatalf("external bytes changed: %q, %v", raw, err)
				}
			}
		})
	}
}

func TestGarbageCollectWithLocksSeparatesCommitsFromSameSource(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	source := "https://git.example.com/catalog"
	reachableOID, err := ParseGitOID("sha1:" + strings.Repeat("1", 40))
	if err != nil {
		t.Fatal(err)
	}
	orphanOID, err := ParseGitOID("sha1:" + strings.Repeat("2", 40))
	if err != nil {
		t.Fatal(err)
	}
	reachable := exactGitSnapshotPath(source, reachableOID)
	orphan := exactGitSnapshotPath(source, orphanOID)
	objectStore := exactGitObjectStorePath(source)
	for _, path := range []string{reachable, orphan, objectStore} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	commit, err := lockfile.ParseGitOID(reachableOID.String())
	if err != nil {
		t.Fatal(err)
	}
	doc := lockfile.Document{APIVersion: lockfile.APIVersionV1, Scope: lockfile.ScopeUser, Registries: []lockfile.GitRegistry{{URL: source, Ref: "main", Commit: commit}}}

	result, err := GarbageCollectWithLocks(false, []lockfile.Document{doc}, &config.Config{})
	if err != nil {
		t.Fatalf("GarbageCollectWithLocks: %v", err)
	}
	if !slices.Contains(result.Removed, orphan) {
		t.Fatalf("removed = %v, want orphan snapshot %q", result.Removed, orphan)
	}
	for _, path := range []string{reachable, objectStore} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("reachable exact Git state %q was removed: %v", path, err)
		}
	}
}

func TestGarbageCollectReloadsReachabilityUnderExclusiveLease(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	source := "https://git.example.com/catalog.git"
	reachableOID, err := ParseGitOID("sha1:" + strings.Repeat("1", 40))
	if err != nil {
		t.Fatal(err)
	}
	orphanOID, err := ParseGitOID("sha1:" + strings.Repeat("2", 40))
	if err != nil {
		t.Fatal(err)
	}
	reachable := exactGitSnapshotPath(source, reachableOID)
	orphan := exactGitSnapshotPath(source, orphanOID)
	for _, path := range []string{reachable, orphan} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	commit, err := lockfile.ParseGitOID(reachableOID.String())
	if err != nil {
		t.Fatal(err)
	}
	doc := lockfile.Document{APIVersion: lockfile.APIVersionV1, Scope: lockfile.ScopeUser, Registries: []lockfile.GitRegistry{{URL: source, Ref: "main", Commit: commit}}}
	entered := make(chan struct{})
	releaseReader := make(chan struct{})
	reader := func(ctx context.Context) (GCReachability, error) {
		close(entered)
		select {
		case <-releaseReader:
			return GCReachability{Locks: []lockfile.Document{doc}, Configs: []*config.Config{{}}}, nil
		case <-ctx.Done():
			return GCReachability{}, ctx.Err()
		}
	}
	gcDone := make(chan error, 1)
	go func() {
		_, err := GarbageCollectWithReachabilityAndContext(context.Background(), false, reader)
		gcDone <- err
	}()
	<-entered
	publisherAcquired := make(chan struct{})
	publisherDone := make(chan error, 1)
	go func() {
		release, err := AcquireExactGitNamespaceLeaseWithContext(context.Background())
		if err == nil {
			close(publisherAcquired)
			release()
		}
		publisherDone <- err
	}()
	select {
	case <-publisherAcquired:
		t.Fatal("lock publisher acquired shared lease while GC revalidated reachability")
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseReader)
	if err := <-gcDone; err != nil {
		t.Fatal(err)
	}
	if err := <-publisherDone; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(reachable); err != nil {
		t.Fatalf("revalidated reachable snapshot was removed: %v", err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan snapshot survived GC: %v", err)
	}
}

func TestGarbageCollectReloadsConcurrentNewLayerUnderExclusiveLease(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	source := "https://git.example.com/new-layer"
	newOID, err := ParseGitOID("sha1:" + strings.Repeat("3", 40))
	if err != nil {
		t.Fatal(err)
	}
	orphanOID, err := ParseGitOID("sha1:" + strings.Repeat("4", 40))
	if err != nil {
		t.Fatal(err)
	}
	newSnapshot := exactGitSnapshotPath(source, newOID)
	orphanSnapshot := exactGitSnapshotPath(source, orphanOID)
	objectStore := exactGitObjectStorePath(source)
	activeLegacy := filepath.Join(CacheDir(), HashURL(source), "main")
	for _, path := range []string{newSnapshot, orphanSnapshot, activeLegacy, objectStore} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	commit, err := lockfile.ParseGitOID(newOID.String())
	if err != nil {
		t.Fatal(err)
	}
	newLayerLock := lockfile.Document{APIVersion: lockfile.APIVersionV1, Scope: lockfile.ScopeUser, Registries: []lockfile.GitRegistry{{URL: source, Ref: "main", Commit: commit}}}

	// This shared lease represents config-plus-lock publication. GC may start,
	// but it cannot load its authoritative inventory until publication closes.
	releasePublication, err := AcquireExactGitNamespaceLeaseWithContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	publicationLeaseHeld := true
	defer func() {
		if publicationLeaseHeld {
			releasePublication()
		}
	}()
	started := make(chan struct{})
	readerEntered := make(chan struct{})
	gcDone := make(chan error, 1)
	go func() {
		close(started)
		_, err := GarbageCollectWithReachabilityAndContext(context.Background(), false, func(context.Context) (GCReachability, error) {
			close(readerEntered)
			return GCReachability{Locks: []lockfile.Document{newLayerLock}, Configs: []*config.Config{{Registries: []config.RegistryEntry{{URL: source, Ref: "main"}}}}}, nil
		})
		gcDone <- err
	}()
	<-started
	select {
	case <-readerEntered:
		t.Fatal("GC reloaded reachability before concurrent publication completed")
	case <-time.After(100 * time.Millisecond):
	}
	releasePublication()
	publicationLeaseHeld = false
	select {
	case <-readerEntered:
	case <-time.After(10 * time.Second):
		t.Fatal("GC never reloaded reachability after concurrent publication")
	}
	if err := <-gcDone; err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{newSnapshot, activeLegacy, objectStore} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("newly published layer reachability %q was removed: %v", path, err)
		}
	}
	if _, err := os.Stat(orphanSnapshot); !os.IsNotExist(err) {
		t.Fatalf("orphan snapshot survived GC: %v", err)
	}
}

func TestGarbageCollectWithLocksPreservesSnapshotsAcrossLayersAndSortsDryRun(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	source := "https://git.example.com/catalog"
	var documents []lockfile.Document
	for i, scope := range []lockfile.Scope{lockfile.ScopeUser, lockfile.ScopeRepo} {
		oid, err := ParseGitOID(fmt.Sprintf("sha1:%s", strings.Repeat(fmt.Sprint(i+1), 40)))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(exactGitSnapshotPath(source, oid), 0o755); err != nil {
			t.Fatal(err)
		}
		commit, err := lockfile.ParseGitOID(oid.String())
		if err != nil {
			t.Fatal(err)
		}
		documents = append(documents, lockfile.Document{APIVersion: lockfile.APIVersionV1, Scope: scope, Registries: []lockfile.GitRegistry{{URL: source, Ref: fmt.Sprintf("branch-%d", i), Commit: commit}}})
	}
	stageA := filepath.Join(exactGitRoot(), "objects", ".objects-stage-z")
	stageB := filepath.Join(exactGitRoot(), "snapshots", ".snapshot-stage-a")
	objectStore := exactGitObjectStorePath(source)
	for _, path := range []string{stageA, stageB, objectStore} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	result, err := GarbageCollectWithLocks(true, documents, &config.Config{})
	if err != nil {
		t.Fatalf("GarbageCollectWithLocks dry-run: %v", err)
	}
	if !slices.Equal(result.Removed, []string{stageA, stageB}) {
		t.Fatalf("dry-run removed = %v, want sorted stages", result.Removed)
	}
	for _, path := range []string{stageA, stageB} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("dry-run removed %q: %v", path, err)
		}
	}
}

func TestGarbageCollectWaitsForLiveRegistryDiscovery(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stalePath := filepath.Join(CacheDir(), "stale")
	if err := os.MkdirAll(stalePath, 0o755); err != nil {
		t.Fatal(err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	var discovery sync.WaitGroup
	discovery.Add(1)
	go func() {
		defer discovery.Done()
		_, err := WithCacheReadLock(func() (struct{}, error) {
			close(entered)
			<-release
			return struct{}{}, nil
		})
		if err != nil {
			t.Errorf("read lease: %v", err)
		}
	}()
	<-entered

	gcDone := make(chan error, 1)
	go func() {
		_, err := GarbageCollect(false, &config.Config{})
		gcDone <- err
	}()
	select {
	case err := <-gcDone:
		t.Fatalf("garbage collection ran during discovery: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	discovery.Wait()
	if err := <-gcDone; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("stale cache survived after discovery lease: %v", err)
	}
}

func TestGarbageCollectReportsOnlySuccessfulRemovals(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	firstPath := filepath.Join(CacheDir(), "a-stale")
	failedPath := filepath.Join(CacheDir(), "b-stale")
	if err := os.MkdirAll(filepath.Join(firstPath, "main"), 0o755); err != nil {
		t.Fatalf("mkdir first cache: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(failedPath, "main"), 0o755); err != nil {
		t.Fatalf("mkdir failed cache: %v", err)
	}
	if err := os.Chmod(failedPath, 0o555); err != nil {
		t.Fatalf("chmod failed cache: %v", err)
	}
	defer func() { _ = os.Chmod(failedPath, 0o755) }()

	result, err := GarbageCollect(false, &config.Config{})
	if err == nil {
		t.Skip("filesystem permitted deletion of a read-only cache directory")
	}
	if len(result.Removed) != 1 || result.Removed[0] != firstPath {
		t.Fatalf("removed = %v, want only successfully removed path %q", result.Removed, firstPath)
	}
	if _, err := os.Stat(firstPath); !os.IsNotExist(err) {
		t.Fatalf("successful removal left first cache, stat err=%v", err)
	}
	if _, err := os.Stat(failedPath); err != nil {
		t.Fatalf("failed removal did not preserve later cache: %v", err)
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

			result, err := GarbageCollect(dryRun, &config.Config{})
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

func TestGarbageCollectRejectsSymlinkedRootAndPreservesExternalTarget(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	externalRoot := t.TempDir()
	externalPath := filepath.Join(externalRoot, "stale")
	if err := os.MkdirAll(externalPath, 0o755); err != nil {
		t.Fatalf("mkdir external cache: %v", err)
	}
	cacheRoot := CacheDir()
	if err := os.MkdirAll(filepath.Dir(cacheRoot), 0o755); err != nil {
		t.Fatalf("mkdir cache parent: %v", err)
	}
	if err := os.Symlink(externalRoot, cacheRoot); err != nil {
		t.Fatalf("symlink cache root: %v", err)
	}

	result, err := GarbageCollect(false, &config.Config{})
	if err == nil {
		t.Fatal("expected symlinked cache root error")
	}
	if len(result.Removed) != 0 {
		t.Fatalf("symlinked root reported removals: %v", result.Removed)
	}
	if _, err := os.Stat(externalPath); err != nil {
		t.Fatalf("symlinked root touched external target: %v", err)
	}
}

func TestGarbageCollectRejectsSymlinkedCacheAncestorAndPreservesExternalTarget(t *testing.T) {
	homeTarget := t.TempDir()
	homeLinkParent := t.TempDir()
	homeLink := filepath.Join(homeLinkParent, "home")
	t.Setenv("HOME", homeLink)
	externalPath := filepath.Join(homeTarget, ".cache", "agent-marketplace", "stale")
	if err := os.MkdirAll(externalPath, 0o755); err != nil {
		t.Fatalf("mkdir external cache: %v", err)
	}
	if err := os.Symlink(homeTarget, homeLink); err != nil {
		t.Fatalf("symlink home ancestor: %v", err)
	}

	result, err := GarbageCollect(false, &config.Config{})
	if err == nil {
		t.Fatal("expected symlinked cache ancestor error")
	}
	if len(result.Removed) != 0 {
		t.Fatalf("symlinked ancestor reported removals: %v", result.Removed)
	}
	if _, err := os.Stat(externalPath); err != nil {
		t.Fatalf("symlinked ancestor touched external target: %v", err)
	}
}

func TestGarbageCollectRejectsNonDirectoryCacheRoot(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	cacheRoot := CacheDir()
	if err := os.MkdirAll(filepath.Dir(cacheRoot), 0o755); err != nil {
		t.Fatalf("mkdir cache parent: %v", err)
	}
	if err := os.WriteFile(cacheRoot, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write cache root: %v", err)
	}

	result, err := GarbageCollect(false, &config.Config{})
	if err == nil {
		t.Fatal("expected non-directory cache root error")
	}
	if len(result.Removed) != 0 {
		t.Fatalf("non-directory root reported removals: %v", result.Removed)
	}
	if raw, readErr := os.ReadFile(cacheRoot); readErr != nil || string(raw) != "not a directory" {
		t.Fatalf("non-directory cache root was changed: raw=%q err=%v", raw, readErr)
	}
}

func TestGarbageCollectReportsOnlySuccessfulRemovals(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	firstPath := filepath.Join(CacheDir(), "a-stale")
	failedPath := filepath.Join(CacheDir(), "b-stale")
	if err := os.MkdirAll(filepath.Join(firstPath, "main"), 0o755); err != nil {
		t.Fatalf("mkdir first cache: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(failedPath, "main"), 0o755); err != nil {
		t.Fatalf("mkdir failed cache: %v", err)
	}
	if err := os.Chmod(failedPath, 0o555); err != nil {
		t.Fatalf("chmod failed cache: %v", err)
	}
	defer func() { _ = os.Chmod(failedPath, 0o755) }()

	result, err := GarbageCollect(false, &config.Config{})
	if err == nil {
		t.Skip("filesystem permitted deletion of a read-only cache directory")
	}
	if len(result.Removed) != 1 || result.Removed[0] != firstPath {
		t.Fatalf("removed = %v, want only successfully removed path %q", result.Removed, firstPath)
	}
	if _, err := os.Stat(firstPath); !os.IsNotExist(err) {
		t.Fatalf("successful removal left first cache, stat err=%v", err)
	}
	if _, err := os.Stat(failedPath); err != nil {
		t.Fatalf("failed removal did not preserve later cache: %v", err)
	}
}
