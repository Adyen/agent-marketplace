package cache

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
)

const compiledCacheDirName = "compiled"

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
	return filepath.Join(CacheDir(), HashURL(url), cacheRefComponent(ref))
}

const maxCacheRefComponentLength = 200

func cacheRefComponent(ref string) string {
	component := base64.RawURLEncoding.EncodeToString([]byte(ref))
	if len(component) <= maxCacheRefComponentLength {
		return component
	}

	sum := sha256.Sum256([]byte(ref))
	return "@" + hex.EncodeToString(sum[:])
}

func HashURL(url string) string {
	sum := sha256.Sum256([]byte(url))
	return hex.EncodeToString(sum[:])[:16]
}

type GCResult struct {
	Removed []string
}

func GarbageCollect(dryRun bool, cfgs ...*config.Config) (GCResult, error) {
	result := GCResult{}
	active := activeRegistryHashes(cfgs...)

	entries, err := os.ReadDir(CacheDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return result, nil
		}
		return result, fmt.Errorf("read cache directory %q: %w", CacheDir(), err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if entry.Name() == compiledCacheDirName {
			continue
		}

		if _, ok := active[entry.Name()]; ok {
			continue
		}

		cachePath := filepath.Join(CacheDir(), entry.Name())
		result.Removed = append(result.Removed, cachePath)
		if dryRun {
			continue
		}

		if err := os.RemoveAll(cachePath); err != nil {
			return result, fmt.Errorf("remove cache directory %q: %w", cachePath, err)
		}
	}

	slices.Sort(result.Removed)

	return result, nil
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
