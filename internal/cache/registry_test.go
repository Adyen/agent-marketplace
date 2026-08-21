package cache

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestTruncateGitErrorPreservesUTF8AndCharacterLimit(t *testing.T) {
	message := strings.Repeat("a", 199) + "é" + "additional detail"

	got := truncateGitError(message)
	if !utf8.ValidString(got) {
		t.Fatalf("truncateGitError returned invalid UTF-8: %q", got)
	}
	if got != strings.Repeat("a", 199)+"…" {
		t.Fatalf("truncateGitError() = %q, want 199 characters plus an ellipsis", got)
	}
	if gotRunes := len([]rune(got)); gotRunes != maxGitErrorChars {
		t.Fatalf("truncateGitError returned %d characters, want %d", gotRunes, maxGitErrorChars)
	}
}

func TestRegistryKeyReturnsCachePathForGitRegistries(t *testing.T) {
	entry := config.RegistryEntry{URL: "git@example.com:repo.git", Ref: "main"}
	key, err := RegistryKey(entry, "/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := CatalogKeyForEntry("git@example.com:repo.git", "main", "")
	if key != want {
		t.Fatalf("expected key %q, got %q", want, key)
	}
}

func TestRegistryKeyIncludesPathForGitRegistries(t *testing.T) {
	entry := config.RegistryEntry{URL: "git@example.com:repo.git", Ref: "main", Path: "teams/pm"}
	key, err := RegistryKey(entry, "/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := CatalogKeyForEntry("git@example.com:repo.git", "main", "teams/pm")
	if key != want {
		t.Fatalf("expected key %q, got %q", want, key)
	}
	// Must differ from the no-path variant.
	noPathKey := CatalogKeyForEntry("git@example.com:repo.git", "main", "")
	if key == noPathKey {
		t.Fatalf("path-specific key should differ from no-path key, both %q", key)
	}
}

func TestRegistryKeyReturnsSkillPathForLocalRegistries(t *testing.T) {
	entry := config.RegistryEntry{URL: "/local/skills"}
	key, err := RegistryKey(entry, "/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if key != "/local/skills" {
		t.Fatalf("expected key %q, got %q", "/local/skills", key)
	}
}

func TestRegistryKeyIncludesPathSuffixForLocalRegistries(t *testing.T) {
	entry := config.RegistryEntry{URL: "/local/skills", Path: "subdir"}
	key, err := RegistryKey(entry, "/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if key != "/local/skills/subdir" {
		t.Fatalf("expected key %q, got %q", "/local/skills/subdir", key)
	}
}

func TestRegistryKeyResolvesRelativeLocalPaths(t *testing.T) {
	entry := config.RegistryEntry{URL: "./skills"}
	key, err := RegistryKey(entry, "/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if key != "/repo/skills" {
		t.Fatalf("expected key %q, got %q", "/repo/skills", key)
	}
}

func TestCatalogKeyForEntryMatchesCachePathWithoutPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	url := "git@example.com:repo.git"
	ref := "main"

	got := CatalogKeyForEntry(url, ref, "")
	want := RegistryCachePath(url, ref)

	if got != want {
		t.Fatalf("CatalogKeyForEntry with empty path should equal RegistryCachePath, got %q want %q", got, want)
	}
}

func TestCatalogKeyForEntryDiffersByPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	url := "git@example.com:repo.git"
	ref := "main"

	keyNoPath := CatalogKeyForEntry(url, ref, "")
	keyPathA := CatalogKeyForEntry(url, ref, "teams/a")
	keyPathB := CatalogKeyForEntry(url, ref, "teams/b")

	if keyNoPath == keyPathA {
		t.Fatalf("no-path key should differ from path-a key, both %q", keyNoPath)
	}
	if keyPathA == keyPathB {
		t.Fatalf("different paths should produce different keys, both %q", keyPathA)
	}
	// Same path should produce same key.
	if again := CatalogKeyForEntry(url, ref, "teams/a"); again != keyPathA {
		t.Fatalf("same path should produce same key, got %q then %q", keyPathA, again)
	}
}

func TestBaseCachePathStripsHexSuffix(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	base := RegistryCachePath("git@example.com:repo.git", "main")
	key := base + ":" + HashURL("skills")

	got := BaseCachePath(key)
	if got != base {
		t.Fatalf("BaseCachePath(%q) = %q, want %q", key, got, base)
	}
}

func TestBaseCachePathReturnsKeyUnchangedWithoutSuffix(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	key := RegistryCachePath("git@example.com:repo.git", "main")

	got := BaseCachePath(key)
	if got != key {
		t.Fatalf("BaseCachePath(%q) = %q, want %q", key, got, key)
	}
}

func TestBaseCachePathDoesNotStripNonHexSuffix(t *testing.T) {
	key := "/some/path:ref"

	got := BaseCachePath(key)
	if got != key {
		t.Fatalf("BaseCachePath(%q) = %q, want %q (non-hex suffix should be kept)", key, got, key)
	}
}

func TestBaseCachePathDoesNotStripShortHexSuffix(t *testing.T) {
	key := "/some/path:abc"

	got := BaseCachePath(key)
	if got != key {
		t.Fatalf("BaseCachePath(%q) = %q, want %q (short hex suffix should be kept)", key, got, key)
	}
}

func TestBaseCachePathDoesNotStripUppercaseHexSuffix(t *testing.T) {
	key := "/some/path:ABCDEF0123456789"

	got := BaseCachePath(key)
	if got != key {
		t.Fatalf("BaseCachePath(%q) = %q, want %q (uppercase hex suffix should be kept)", key, got, key)
	}
}

func TestBaseCachePathDoesNotStripRealLocalPathWithHexSuffix(t *testing.T) {
	// A real local path like /tmp/registry:deadbeefdeadbeef should NOT be
	// stripped even though the suffix looks like a valid hex16. Create it
	// on disk to simulate the scenario.
	tmpDir := t.TempDir()
	realPath := filepath.Join(tmpDir, "registry:deadbeef01234567")
	if err := os.MkdirAll(realPath, 0o755); err != nil {
		t.Fatal(err)
	}

	got := BaseCachePath(realPath)
	if got != realPath {
		t.Fatalf("BaseCachePath(%q) = %q, want %q (real local path should not be stripped)", realPath, got, realPath)
	}
}

func TestBaseCachePathStripsSuffixWhenBaseDirExists(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	base := RegistryCachePath("git@example.com:repo.git", "main")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	key := base + ":" + HashURL("skills")

	got := BaseCachePath(key)
	if got != base {
		t.Fatalf("BaseCachePath(%q) = %q, want %q", key, got, base)
	}
}

func TestCacheDirUsesHomeCacheDirectory(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	got := CacheDir()
	want := filepath.Join(homeDir, ".cache", "agent-marketplace")

	if got != want {
		t.Fatalf("expected cache dir %q, got %q", want, got)
	}
}

func TestRegistryCachePathIsDeterministic(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	url := "https://example.com/org/skills.git"
	ref := "v1.2.3"

	got := RegistryCachePath(url, ref)
	want := filepath.Join(CacheDir(), HashURL(url), cacheRefComponent(ref))

	if got != want {
		t.Fatalf("expected cache path %q, got %q", want, got)
	}

	if again := RegistryCachePath(url, ref); again != got {
		t.Fatalf("expected cache path to be stable, first %q second %q", got, again)
	}
}

func TestHashURLUsesFirst16HexCharsOfSHA256(t *testing.T) {
	url := "git@example.com:org/skills.git"
	sum := sha256.Sum256([]byte(url))
	want := hex.EncodeToString(sum[:])[:16]

	got := HashURL(url)

	if got != want {
		t.Fatalf("expected hash %q, got %q", want, got)
	}

	if len(got) != 16 {
		t.Fatalf("expected 16 hex chars, got %d", len(got))
	}

	if HashURL(url) != got {
		t.Fatalf("expected hash to be consistent for %q", url)
	}
}

func TestRegistryCachePathDiffersByRef(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	url := "https://example.com/org/skills.git"

	mainPath := RegistryCachePath(url, "main")
	tagPath := RegistryCachePath(url, "v1.0.0")

	if mainPath == tagPath {
		t.Fatalf("expected different refs to produce different cache paths, got %q", mainPath)
	}
}

func TestRegistryCachePathEncodesRefAsOneContainedComponent(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	url := "https://example.com/org/skills.git"
	parent := filepath.Join(CacheDir(), HashURL(url))
	for _, ref := range []string{"feature/security", "refs/tags/v1.2.3", "../../outside"} {
		path := RegistryCachePath(url, ref)
		if filepath.Dir(path) != parent {
			t.Fatalf("RegistryCachePath(%q) parent = %q, want %q", ref, filepath.Dir(path), parent)
		}
		if filepath.Base(path) != cacheRefComponent(ref) {
			t.Fatalf("RegistryCachePath(%q) component = %q, want %q", ref, filepath.Base(path), cacheRefComponent(ref))
		}
		rel, err := filepath.Rel(CacheDir(), path)
		if err != nil {
			t.Fatalf("relate cache path for ref %q: %v", ref, err)
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Fatalf("RegistryCachePath(%q) escaped cache root: %q", ref, path)
		}
	}
}

func TestCloneOrPullSupportsLongValidRefs(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t)
	ref := strings.Repeat("a", 200)
	runGitCommand(t, working, "push", "origin", "HEAD:refs/heads/"+ref)

	cachePath, err := CloneOrPull(origin, ref)
	if err != nil {
		t.Fatalf("expected long valid ref to succeed: %v", err)
	}
	if got := filepath.Base(cachePath); len(got) > 200 {
		t.Fatalf("cache component is not bounded: %d bytes", len(got))
	}
}

func TestCloneOrPullRejectsSymlinkedCacheRoot(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, _ := createBareRepoWithWorkingClone(t)
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "sentinel.txt"), "untouched\n")
	if err := os.MkdirAll(filepath.Dir(CacheDir()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, CacheDir()); err != nil {
		t.Fatal(err)
	}

	_, err := CloneOrPull(origin, "main")
	if err == nil {
		t.Fatal("expected symlinked cache root to be rejected")
	}
	assertFileContents(t, filepath.Join(outside, "sentinel.txt"), "untouched\n")
	if _, statErr := os.Stat(filepath.Join(outside, HashURL(origin))); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("symlinked cache root was used outside CacheDir: %v", statErr)
	}
}

func TestCloneOrPullRejectsSymlinkedCacheHashDirectory(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, _ := createBareRepoWithWorkingClone(t)
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "sentinel.txt"), "untouched\n")
	hashDir := filepath.Join(CacheDir(), HashURL(origin))
	if err := os.MkdirAll(filepath.Dir(hashDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, hashDir); err != nil {
		t.Fatal(err)
	}

	_, err := CloneOrPull(origin, "main")
	if err == nil {
		t.Fatal("expected symlinked cache hash directory to be rejected")
	}
	assertFileContents(t, filepath.Join(outside, "sentinel.txt"), "untouched\n")
	if _, statErr := os.Stat(filepath.Join(outside, cacheRefComponent("main"))); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("symlinked cache hash directory was used outside CacheDir: %v", statErr)
	}
}

// A symlinked ~/.cache is a legitimate user setup (a cache relocated to another
// volume), so it must keep working. The trust boundary is the cache root, not the
// path used to reach it: everything below the resolved root stays contained.
func TestCloneOrPullAcceptsSymlinkedCacheParent(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t)
	writeAndPushCommit(t, working, "skill.txt", "relocated\n", "initial commit")
	relocated := t.TempDir()
	cacheParent := filepath.Dir(CacheDir())
	if err := os.MkdirAll(filepath.Dir(cacheParent), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(relocated, cacheParent); err != nil {
		t.Fatal(err)
	}

	cachePath, err := CloneOrPull(origin, "main")
	if err != nil {
		t.Fatalf("expected symlinked cache parent to be usable: %v", err)
	}
	assertFileContents(t, filepath.Join(cachePath, "skill.txt"), "relocated\n")

	resolvedRoot := filepath.Join(relocated, filepath.Base(CacheDir()))
	if _, statErr := os.Stat(filepath.Join(resolvedRoot, HashURL(origin))); statErr != nil {
		t.Fatalf("expected the cache to be materialized below the resolved root: %v", statErr)
	}
	assertNoEscapedEntries(t, resolvedRoot, relocated)
}

// A symlinked HOME is common (relocated home directories, and /Users on macOS
// resolving through /System/Volumes/Data), so refusing it would break ordinary
// installs while buying no containment: the ref never gets to name any component
// of the path used to reach the cache root.
func TestCloneOrPullAcceptsSymlinkedHome(t *testing.T) {
	realHome := t.TempDir()
	homeDir := filepath.Join(t.TempDir(), "home-link")
	if err := os.Symlink(realHome, homeDir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t)
	writeAndPushCommit(t, working, "skill.txt", "linked home\n", "initial commit")

	cachePath, err := CloneOrPull(origin, "main")
	if err != nil {
		t.Fatalf("expected symlinked home to be usable: %v", err)
	}
	assertFileContents(t, filepath.Join(cachePath, "skill.txt"), "linked home\n")

	resolvedRoot := filepath.Join(realHome, ".cache", "agent-marketplace")
	if _, statErr := os.Stat(filepath.Join(resolvedRoot, HashURL(origin))); statErr != nil {
		t.Fatalf("expected the cache to be materialized below the resolved home: %v", statErr)
	}
	assertNoEscapedEntries(t, resolvedRoot, realHome)
}

// assertNoEscapedEntries fails if anything was created directly under boundary
// other than the resolved cache root, i.e. if a cache operation escaped it.
func assertNoEscapedEntries(t *testing.T, resolvedRoot, boundary string) {
	t.Helper()

	entries, err := os.ReadDir(boundary)
	if err != nil {
		t.Fatalf("read boundary %q: %v", boundary, err)
	}
	allowed := filepath.Base(resolvedRoot)
	if filepath.Dir(resolvedRoot) != boundary {
		// The root is deeper than the boundary (a home directory); only its first
		// component may appear.
		rel, relErr := filepath.Rel(boundary, resolvedRoot)
		if relErr != nil {
			t.Fatalf("relate %q to %q: %v", resolvedRoot, boundary, relErr)
		}
		allowed = strings.Split(rel, string(filepath.Separator))[0]
	}
	for _, entry := range entries {
		if entry.Name() != allowed {
			t.Fatalf("cache operation created %q outside the resolved cache root %q", entry.Name(), resolvedRoot)
		}
	}
}

func TestCloneOrPullRejectsSymlinkedCacheEntry(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t)
	writeFile(t, filepath.Join(working, "sentinel.txt"), "external\n")
	cachePath := RegistryCachePath(origin, "main")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(working, cachePath); err != nil {
		t.Fatal(err)
	}

	_, err := CloneOrPull(origin, "main")
	if err == nil {
		t.Fatal("expected symlinked cache entry to be rejected")
	}
	assertFileContents(t, filepath.Join(working, "sentinel.txt"), "external\n")
}

func TestCloneOrPullLogsValidationErrors(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previous)

	_, err := CloneOrPull("https://example.com/registry.git", "../../outside")
	if err == nil {
		t.Fatal("expected unsafe ref to be rejected")
	}
	if !strings.Contains(logs.String(), "registry ref validation failed") || !strings.Contains(logs.String(), "level=ERROR") {
		t.Fatalf("expected structured validation error log, got %q", logs.String())
	}
}

func TestGarbageCollectPreservesCompiledCache(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	compiledPath := filepath.Join(CacheDir(), compiledCacheDirName, "claude", "abc123", "skill")
	if err := os.MkdirAll(compiledPath, 0o755); err != nil {
		t.Fatalf("mkdir compiled cache: %v", err)
	}
	orphanPath := filepath.Join(CacheDir(), "orphan")
	if err := os.MkdirAll(orphanPath, 0o755); err != nil {
		t.Fatalf("mkdir orphan cache: %v", err)
	}

	result, err := GarbageCollect(false)
	if err != nil {
		t.Fatalf("GarbageCollect: %v", err)
	}
	if _, err := os.Stat(compiledPath); err != nil {
		t.Fatalf("expected compiled cache to remain: %v", err)
	}
	if _, err := os.Stat(orphanPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected orphan cache to be removed, got err=%v", err)
	}
	if len(result.Removed) != 1 || result.Removed[0] != orphanPath {
		t.Fatalf("expected only orphan cache removed, got %v", result.Removed)
	}
}

func TestCloneOrPullClonesAndUpdatesCache(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t)
	writeAndPushCommit(t, working, "skill.txt", "version one\n", "initial commit")

	cachePath, err := CloneOrPull(origin, "main")
	if err != nil {
		t.Fatalf("expected initial clone to succeed: %v", err)
	}

	wantPath := RegistryCachePath(origin, "main")
	if cachePath != wantPath {
		t.Fatalf("expected cache path %q, got %q", wantPath, cachePath)
	}

	assertFileContents(t, filepath.Join(cachePath, "skill.txt"), "version one\n")

	writeAndPushCommit(t, working, "skill.txt", "version two\n", "second commit")

	cachePath, err = CloneOrPull(origin, "main")
	if err != nil {
		t.Fatalf("expected refresh to succeed: %v", err)
	}

	if cachePath != wantPath {
		t.Fatalf("expected cache path to remain %q, got %q", wantPath, cachePath)
	}

	assertFileContents(t, filepath.Join(cachePath, "skill.txt"), "version two\n")
}

func TestCloneOrPullSupportsRefsWithSlashes(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t)
	runGitCommand(t, working, "checkout", "-b", "feature/security")
	writeFile(t, filepath.Join(working, "skill.txt"), "feature version\n")
	runGitCommand(t, working, "add", "skill.txt")
	runGitCommand(t, working, "commit", "-m", "feature commit")
	runGitCommand(t, working, "push", "origin", "HEAD:refs/heads/feature/security")

	cachePath, err := CloneOrPull(origin, "feature/security")
	if err != nil {
		t.Fatalf("expected slash-containing ref to succeed: %v", err)
	}
	if cachePath != RegistryCachePath(origin, "feature/security") {
		t.Fatalf("expected encoded cache path, got %q", cachePath)
	}
	assertFileContents(t, filepath.Join(cachePath, "skill.txt"), "feature version\n")
}

func TestCloneOrPullReturnsStaleCacheWithWarningWhenRegistryUnavailable(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t)
	writeAndPushCommit(t, working, "skill.txt", "cached version\n", "initial commit")

	cachePath, err := CloneOrPull(origin, "main")
	if err != nil {
		t.Fatalf("expected initial clone to succeed: %v", err)
	}

	brokenOrigin := origin + ".offline"
	if err := os.Rename(origin, brokenOrigin); err != nil {
		t.Fatalf("rename origin: %v", err)
	}

	var logs bytes.Buffer
	restoreLog := swapLogOutput(&logs)
	defer restoreLog()

	stalePath, err := CloneOrPull(origin, "main")
	if err != nil {
		t.Fatalf("expected stale cache fallback to succeed: %v", err)
	}

	if stalePath != cachePath {
		t.Fatalf("expected stale cache path %q, got %q", cachePath, stalePath)
	}

	if !strings.Contains(logs.String(), "stale cache") {
		t.Fatalf("expected warning to mention stale cache, got %q", logs.String())
	}

	assertFileContents(t, filepath.Join(stalePath, "skill.txt"), "cached version\n")
}

func TestCloneOrPullFailsWhenRegistryUnavailableAndNoCacheExists(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	missingRepo := filepath.Join(t.TempDir(), "missing.git")

	cachePath, err := CloneOrPull(missingRepo, "main")
	if err == nil {
		t.Fatalf("expected missing registry to fail without cache, got cache path %q", cachePath)
	}
	if _, statErr := os.Stat(cachePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed clone left cache path %q, stat error: %v", cachePath, statErr)
	}
	if _, statErr := os.Stat(CacheDir()); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed clone left cache root %q, stat error: %v", CacheDir(), statErr)
	}
}

func TestCloneOrPullRejectsUnsafeRefBeforeCacheMutation(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	_, err := CloneOrPull(filepath.Join(t.TempDir(), "missing.git"), "../../outside")
	if err == nil || !strings.Contains(err.Error(), "contains '..'") {
		t.Fatalf("expected unsafe ref to be rejected, got %v", err)
	}
	if _, statErr := os.Stat(CacheDir()); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("unsafe ref created cache root %q, stat error: %v", CacheDir(), statErr)
	}
}

func TestCloneOrPullRemovesCacheWhenCheckoutFails(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t)
	writeAndPushCommit(t, working, "skill.txt", "main version\n", "initial commit")

	cachePath, err := CloneOrPull(origin, "missing-ref")
	if err == nil {
		t.Fatalf("expected missing ref checkout to fail, got cache path %q", cachePath)
	}
	if _, statErr := os.Stat(cachePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed checkout left cache path %q, stat error: %v", cachePath, statErr)
	}
	if _, statErr := os.Stat(CacheDir()); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed checkout left cache root %q, stat error: %v", CacheDir(), statErr)
	}
}

func createBareRepoWithWorkingClone(t *testing.T) (string, string) {
	t.Helper()

	baseDir := t.TempDir()
	source := filepath.Join(baseDir, "source")
	origin := filepath.Join(baseDir, "origin.git")
	working := filepath.Join(baseDir, "working")

	runGitCommand(t, "", "init", "--initial-branch=main", source)
	writeFile(t, filepath.Join(source, ".gitkeep"), "seed\n")
	runGitCommand(t, source, "add", ".gitkeep")
	runGitCommand(t, source, "commit", "-m", "seed")

	runGitCommand(t, "", "clone", "--bare", source, origin)
	runGitCommand(t, "", "clone", origin, working)

	return origin, working
}

func writeAndPushCommit(t *testing.T, repoDir, relPath, content, message string) {
	t.Helper()

	writeFile(t, filepath.Join(repoDir, relPath), content)
	runGitCommand(t, repoDir, "add", relPath)
	runGitCommand(t, repoDir, "commit", "-m", message)
	runGitCommand(t, repoDir, "push", "origin", "main")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", path, err)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file %q: %v", path, err)
	}
}

func assertFileContents(t *testing.T, path, want string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file %q: %v", path, err)
	}

	if got := string(data); got != want {
		t.Fatalf("expected file %q to contain %q, got %q", path, want, got)
	}
}

func swapLogOutput(dst *bytes.Buffer) func() {
	previousWriter := log.Writer()
	previousFlags := log.Flags()
	log.SetOutput(dst)
	log.SetFlags(0)

	return func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
	}
}

func runGitCommand(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	// Provide git identity via env vars so no config files are written.
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test User",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test User",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}

	return string(output)
}
