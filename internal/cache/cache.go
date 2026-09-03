package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

const (
	compiledCacheDirName    = "compiled"
	exactGitCacheDirName    = "exact-git"
	marketplaceCacheDirName = "marketplace"
)

// RegistryKey returns a stable, unique identifier for a registry entry's catalog.
// For git registries it incorporates URL, ref, and path so that entries with
// the same repo but different subdirectories produce distinct keys.
// For local registries it uses the resolved absolute path (which already
// includes any path component via BundlePath).
// The baseDir parameter is used to resolve relative local paths.
func RegistryKey(entry config.RegistryEntry, baseDir string) (string, error) {
	if entry.IsLocal() {
		localPath, err := entry.ResolveLocalPath(baseDir)
		if err != nil {
			return "", err
		}
		return entry.BundlePath(localPath), nil
	}
	return CatalogKeyForEntry(entry.URL, entry.Ref, entry.Path), nil
}

// CatalogKeyForEntry returns a stable, unique catalog key for a git registry
// entry identified by url, ref, and path. Entries with the same URL+ref but
// different path subdirectories produce distinct catalog keys. When path is
// empty, the key is identical to RegistryCachePath (backward compatible).
func CatalogKeyForEntry(url, ref, path string) string {
	base := RegistryCachePath(url, ref)
	p := filepath.Clean(strings.TrimSpace(path))
	if p == "" || p == "." {
		return base
	}
	return base + ":" + HashURL(p)
}

// BaseCachePath returns the real filesystem cache directory for a catalog key.
// For path-specific entries the catalog key contains a synthetic ":HASH"
// suffix (see CatalogKeyForEntry) that is not a real path component.
// BaseCachePath strips this suffix so callers can os.Stat the actual directory.
// For entries without a path suffix, the key is returned unchanged.
//
// Guard: if the path-without-suffix does not exist on disk but the full
// catalogKey does, the suffix is likely part of a real local path (e.g.
// /tmp/registry:deadbeefdeadbeef) and must not be stripped.
func BaseCachePath(catalogKey string) string {
	i := strings.LastIndex(catalogKey, ":")
	if i < 0 {
		return catalogKey
	}
	suffix := catalogKey[i+1:]
	if isHex(suffix) && len(suffix) == hexLen {
		candidate := catalogKey[:i]
		// If the stripped path exists on disk, it's a real cache dir.
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		// If the full key exists but the stripped one doesn't, the
		// suffix is part of the real path — don't strip it.
		if _, err := os.Stat(catalogKey); err == nil {
			return catalogKey
		}
		// Neither exists: the key is likely a git cache key where
		// only the base directory is on disk. Strip the suffix.
		return candidate
	}
	return catalogKey
}

const hexLen = 16 // HashURL produces 16 hex chars

func isHex(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func CacheDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil || homeDir == "" {
		return filepath.Join(".cache", "agent-marketplace")
	}

	return filepath.Join(homeDir, ".cache", "agent-marketplace")
}

func RegistryCachePath(url, ref string) string {
	return filepath.Join(CacheDir(), HashURL(url), ref)
}

func HashURL(url string) string {
	sum := sha256.Sum256([]byte(url))
	return hex.EncodeToString(sum[:])[:16]
}

type GCResult struct {
	Removed []string
}

// WithCacheMutationLock serializes cache writes and garbage collection within
// and across processes sharing the cache root. The lock inode is persistent so
// releasing a lock never creates an unlink/recreate race with another holder.
func WithCacheMutationLock[T any](fn func() (T, error)) (T, error) {
	cacheMutationMu.Lock()
	defer cacheMutationMu.Unlock()
	release, err := acquireCacheFileLock(lockfileExclusive)
	if err != nil {
		var zero T
		return zero, err
	}
	defer release()
	return fn()
}

// WithCacheReadLock keeps a cache-backed discovery operation alive while
// garbage collection is excluded across processes.
func WithCacheReadLock[T any](fn func() (T, error)) (T, error) {
	release, err := acquireCacheFileLock(lockfileShared)
	if err != nil {
		var zero T
		return zero, err
	}
	defer release()
	return fn()
}

func WithCacheMutation(fn func() error) error {
	_, err := WithCacheMutationLock(func() (struct{}, error) {
		return struct{}{}, fn()
	})
	return err
}

var cacheMutationMu sync.Mutex

func GarbageCollect(dryRun bool, cfgs ...*config.Config) (GCResult, error) {
	return WithCacheMutationLock(func() (GCResult, error) {
		return garbageCollect(dryRun, cfgs...)
	})
}

func GarbageCollectWithLocks(dryRun bool, locks []lockfile.Document, cfgs ...*config.Config) (GCResult, error) {
	started := time.Now()
	slog.Debug("registry cache garbage collection started", "dry_run", dryRun, "lock_layers", len(locks))
	result, err := WithCacheMutationLock(func() (GCResult, error) {
		for i := range locks {
			if err := lockfile.Validate(locks[i]); err != nil {
				return GCResult{}, fmt.Errorf("validate layer lock %d: %w", i, err)
			}
		}
		legacy, err := garbageCollect(dryRun, cfgs...)
		if err != nil {
			return legacy, err
		}
		exact, err := garbageCollectExactGit(dryRun, locks)
		legacy.Removed = append(legacy.Removed, exact.Removed...)
		slices.Sort(legacy.Removed)
		return legacy, err
	})
	attrs := []any{"dry_run", dryRun, "lock_layers", len(locks), "removed", len(result.Removed), "duration_ms", time.Since(started).Milliseconds()}
	if err != nil {
		attrs = append(attrs, "error", err)
		slog.Error("registry cache garbage collection failed", attrs...)
	} else {
		slog.Debug("registry cache garbage collection completed", attrs...)
	}
	return result, err
}

func garbageCollect(dryRun bool, cfgs ...*config.Config) (GCResult, error) {
	result := GCResult{}
	defer slices.Sort(result.Removed)

	if !hasEffectiveConfig(cfgs...) {
		err := errors.New("cache garbage collection requires effective configuration")
		slog.Warn("cache garbage collection failed closed", "reason", err)
		return result, err
	}
	active := activeRegistryHashes(cfgs...)
	cacheRoot := CacheDir()
	cacheBoundary := cacheRoot
	if homeDir, homeErr := os.UserHomeDir(); homeErr == nil && homeDir != "" {
		// Validate through HOME so a symlinked home/.cache ancestor cannot
		// redirect this process into an external tree. Stop before unrelated
		// system ancestors such as macOS's commonly symlinked /var.
		cacheBoundary = homeDir
	}
	if err := ValidateDirectoryAncestry(cacheBoundary, cacheRoot); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return result, nil
		}
		return result, fmt.Errorf("validate cache directory %q: %w", cacheRoot, err)
	}

	entries, err := os.ReadDir(cacheRoot)
	if err != nil {
		return result, fmt.Errorf("read cache directory %q: %w", cacheRoot, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if entry.Name() == compiledCacheDirName || entry.Name() == exactGitCacheDirName || entry.Name() == marketplaceCacheDirName {
			slog.Debug("cache garbage collection skipped reserved namespace", "namespace", entry.Name(), "dry_run", dryRun)
			continue
		}

		if _, ok := active[entry.Name()]; ok {
			continue
		}

		cachePath := filepath.Join(cacheRoot, entry.Name())
		if err := ValidateDirectoryAncestry(cacheRoot, cachePath); err != nil {
			return result, fmt.Errorf("validate cache directory %q: %w", cachePath, err)
		}
		if dryRun {
			result.Removed = append(result.Removed, cachePath)
			continue
		}

		if err := os.RemoveAll(cachePath); err != nil {
			return result, fmt.Errorf("remove cache directory %q: %w", cachePath, err)
		}
		result.Removed = append(result.Removed, cachePath)
	}

	return result, nil
}

func hasEffectiveConfig(cfgs ...*config.Config) bool {
	for _, cfg := range cfgs {
		if cfg != nil {
			return true
		}
	}
	return false
}

// ValidateDirectoryAncestry rejects symlinks and non-directories from path up
// through root, inclusive. The check is intentionally path based: it is a
// best-effort process-local guard, not an atomic replacement for openat-style
// filesystem operations.
func ValidateDirectoryAncestry(root, path string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing path outside cache root")
	}

	var missingErr error
	for current := pathAbs; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if missingErr == nil {
				missingErr = err
			}
		} else {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("path %q is a symlink", current)
			}
			if !info.IsDir() {
				return fmt.Errorf("path %q is not a directory", current)
			}
		}
		if current == rootAbs {
			break
		}
	}
	return missingErr
}

func activeRegistryHashes(cfgs ...*config.Config) map[string]struct{} {
	active := make(map[string]struct{})

	for _, cfg := range cfgs {
		if cfg == nil {
			continue
		}

		for _, entry := range cfg.Registries {
			if entry.IsLocal() {
				continue
			}

			url := strings.TrimSpace(entry.URL)
			if url == "" {
				continue
			}

			active[HashURL(url)] = struct{}{}
		}
	}

	return active
}
