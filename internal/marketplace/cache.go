package marketplace

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// Cache stores Marketplace artifact blobs, collection manifests, and resource
// revision metadata on disk under a single root.
type Cache struct {
	root string
}

// NewCache returns a Cache rooted at root.
func NewCache(root string) *Cache {
	return &Cache{root: root}
}

// DefaultCache returns a Cache rooted at the default marketplace cache path.
func DefaultCache() *Cache {
	return NewCache(filepath.Join(cache.CacheDir(), "marketplace"))
}

// GCResult reports Marketplace artifact directories selected for removal.
// Collection manifests and resource metadata are intentionally never removed by
// this garbage collector.
type GCResult struct {
	Removed []string
}

// GarbageCollect removes unreferenced Marketplace artifact directories. A pin is
// considered referenced only after its cached ResourceRevision metadata has
// been found and validated, including its archive digest. If the effective
// configuration or metadata needed to establish reachability is unavailable or
// malformed, this method fails closed and performs no deletion.
func (c *Cache) GarbageCollect(dryRun bool, cfg *config.Config) (GCResult, error) {
	return cache.WithCacheMutationLock(func() (GCResult, error) {
		return c.garbageCollect(dryRun, cfg)
	})
}

func (c *Cache) garbageCollect(dryRun bool, cfg *config.Config) (GCResult, error) {
	result := GCResult{}
	defer slices.Sort(result.Removed)
	slog.Info("marketplace cache garbage collection started", "root", c.root, "dry_run", dryRun)

	if cfg == nil {
		err := errors.New("marketplace cache garbage collection requires effective configuration")
		slog.Warn("marketplace cache garbage collection failed closed", "reason", err)
		return result, err
	}
	if errs := config.ValidateMarketplaceEntries(cfg.Marketplaces); len(errs) > 0 {
		err := errors.Join(errs...)
		slog.Warn("marketplace cache garbage collection failed closed", "reason", err)
		return result, fmt.Errorf("validate marketplace configuration: %w", err)
	}

	referenced, err := c.referencedArchiveDigests(cfg)
	if err != nil {
		slog.Warn("marketplace cache garbage collection failed closed", "reason", err)
		return result, err
	}

	artifactsRoot, exists, err := validateArtifactCacheDirectories(c.root)
	if err != nil {
		err = fmt.Errorf("validate marketplace artifact cache: %w", err)
		slog.Error("marketplace cache garbage collection failed", "error", err)
		return result, err
	}
	if !exists {
		slog.Info("marketplace cache garbage collection completed", "removed", 0, "dry_run", dryRun)
		return result, nil
	}

	entries, err := os.ReadDir(artifactsRoot)
	if err != nil {
		err = fmt.Errorf("read marketplace artifact cache %q: %w", artifactsRoot, err)
		slog.Error("marketplace cache garbage collection failed", "error", err)
		return result, err
	}

	for _, entry := range entries {
		artifactDir := filepath.Join(artifactsRoot, entry.Name())
		if !validArchiveDigestKey(entry.Name()) {
			if entry.IsDir() {
				slog.Debug("marketplace cache garbage collection preserved unknown artifact entry", "path", artifactDir)
			}
			continue
		}

		info, err := os.Lstat(artifactDir)
		if err != nil {
			err = fmt.Errorf("inspect marketplace artifact directory %q: %w", artifactDir, err)
			slog.Error("marketplace cache garbage collection failed", "error", err)
			return result, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			err = fmt.Errorf("marketplace artifact directory %q is a symlink", artifactDir)
			slog.Error("marketplace cache garbage collection failed", "error", err)
			return result, err
		}
		if !info.IsDir() {
			continue
		}
		if err := cache.ValidateDirectoryAncestry(c.root, artifactDir); err != nil {
			err = fmt.Errorf("validate marketplace artifact directory %q: %w", artifactDir, err)
			slog.Error("marketplace cache garbage collection failed", "error", err)
			return result, err
		}

		removable, err := removableArtifactDirectory(artifactDir)
		if err != nil {
			err = fmt.Errorf("inspect marketplace artifact directory %q: %w", artifactDir, err)
			slog.Error("marketplace cache garbage collection failed", "error", err)
			return result, err
		}
		if !removable {
			slog.Debug("marketplace cache garbage collection preserved malformed artifact entry", "path", artifactDir)
			continue
		}
		if _, ok := referenced[entry.Name()]; ok {
			slog.Debug("marketplace cache garbage collection preserved referenced artifact", "path", artifactDir)
			continue
		}

		slog.Info("marketplace cache garbage collection selected orphan artifact", "path", artifactDir, "dry_run", dryRun)
		if dryRun {
			result.Removed = append(result.Removed, artifactDir)
			continue
		}
		if err := removeArtifactDirectory(c.root, artifactDir); err != nil {
			err = fmt.Errorf("remove marketplace artifact directory %q: %w", artifactDir, err)
			slog.Error("marketplace cache garbage collection failed", "error", err)
			return result, err
		}
		result.Removed = append(result.Removed, artifactDir)
	}

	slog.Info("marketplace cache garbage collection completed", "removed", len(result.Removed), "dry_run", dryRun)
	return result, nil
}

func (c *Cache) referencedArchiveDigests(cfg *config.Config) (map[string]struct{}, error) {
	referenced := make(map[string]struct{})
	for entryIndex, configuredEntry := range cfg.Marketplaces {
		entry, err := canonicalMarketplaceEntry(configuredEntry)
		if err != nil {
			return nil, fmt.Errorf("canonicalize marketplace %d: %w", entryIndex, err)
		}
		for pinIndex, pin := range entry.Resources {
			acquisition := pin.Acquisition
			if acquisition == "" {
				acquisition = config.MarketplaceAcquisitionMirror
			}
			if acquisition != config.MarketplaceAcquisitionMirror || pin.Kind == "collection" {
				// Index-only pins and collection manifests are outside this
				// artifact GC slice. Leave their cache entries untouched.
				slog.Debug("marketplace cache garbage collection skipped non-artifact pin", "marketplace", entry.InstanceID, "pin_index", pinIndex, "kind", pin.Kind, "acquisition", acquisition)
				continue
			}

			rev, ok, err := c.ReadResourceRevision(entry.URL, pin.SourceID, pin.Kind, pin.Name, pin.Digest)
			if err != nil {
				return nil, fmt.Errorf("read metadata for marketplace %d pin %d: %w", entryIndex, pinIndex, err)
			}
			if !ok {
				return nil, fmt.Errorf("metadata for active marketplace %d pin %d is missing", entryIndex, pinIndex)
			}
			if err := validateResourceRevision(entry, pin, rev); err != nil {
				return nil, fmt.Errorf("validate metadata for marketplace %d pin %d: %w", entryIndex, pinIndex, err)
			}
			if rev.ArchiveDigest == "" {
				return nil, fmt.Errorf("metadata for active marketplace %d pin %d has no archive digest", entryIndex, pinIndex)
			}
			if err := validateSHA256Digest(string(rev.ArchiveDigest)); err != nil {
				return nil, fmt.Errorf("metadata for active marketplace %d pin %d has invalid archive digest: %w", entryIndex, pinIndex, err)
			}
			referenced[safeDigest(string(rev.ArchiveDigest))] = struct{}{}
		}
	}
	return referenced, nil
}

func validArchiveDigestKey(name string) bool {
	if len(name) != len("sha256-")+64 || !strings.HasPrefix(name, "sha256-") {
		return false
	}
	for _, ch := range name[len("sha256-"):] {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

func removableArtifactDirectory(path string) (bool, error) {
	if err := requireRegularDirectory(path); err != nil {
		return false, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	if len(entries) == 0 {
		return false, nil
	}
	for _, entry := range entries {
		if entry.Name() != "artifact.tar.gz" && entry.Name() != "verified.marker" {
			return false, nil
		}
		info, err := os.Lstat(filepath.Join(path, entry.Name()))
		if err != nil {
			return false, err
		}
		if !info.Mode().IsRegular() {
			return false, nil
		}
	}
	for _, entry := range entries {
		if entry.Name() == "artifact.tar.gz" {
			return true, nil
		}
	}
	return false, nil
}

func validateArtifactCacheDirectories(root string) (string, bool, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", false, err
	}
	if err := cache.ValidateDirectoryAncestry(rootAbs, rootAbs); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}

	artifactsRoot := filepath.Join(rootAbs, "artifacts")
	info, err := os.Lstat(artifactsRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return artifactsRoot, false, nil
		}
		return "", false, err
	}
	if err := requireRegularDirectoryInfo(artifactsRoot, info); err != nil {
		return "", false, err
	}
	if err := cache.ValidateDirectoryAncestry(rootAbs, artifactsRoot); err != nil {
		return "", false, err
	}
	return artifactsRoot, true, nil
}

func requireRegularDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	return requireRegularDirectoryInfo(path, info)
}

func requireRegularDirectoryInfo(path string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("path %q is a symlink", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("path %q is not a regular directory", path)
	}
	return nil
}

func removeArtifactDirectory(root, path string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing path outside Marketplace cache root")
	}
	if err := cache.ValidateDirectoryAncestry(rootAbs, pathAbs); err != nil {
		return err
	}
	removable, err := removableArtifactDirectory(pathAbs)
	if err != nil {
		return err
	}
	if !removable {
		return fmt.Errorf("refusing malformed artifact directory %q", path)
	}
	return os.RemoveAll(pathAbs)
}

// ArtifactPath returns the path for an artifact archive given its digest.
func (c *Cache) ArtifactPath(digest string) string {
	return filepath.Join(c.root, "artifacts", safeDigest(digest), "artifact.tar.gz")
}

// WriteArtifact writes the artifact archive bytes and returns the path.
func (c *Cache) WriteArtifact(digest string, raw []byte) (string, error) {
	return cache.WithCacheMutationLock(func() (string, error) {
		return c.writeArtifact(digest, raw)
	})
}

func (c *Cache) writeArtifact(digest string, raw []byte) (string, error) {
	p := c.ArtifactPath(digest)
	if err := ensureCacheDirectory(c.root, filepath.Dir(p)); err != nil {
		return "", fmt.Errorf("prepare artifact cache %q: %w", p, err)
	}
	if err := writeAtomic(c.root, p, raw, 0o644, verifiedMarkerPath(filepath.Dir(p))); err != nil {
		return "", fmt.Errorf("write artifact cache %q: %w", p, err)
	}
	return p, nil
}

// ReadArtifact reads the cached artifact bytes and reports whether the
// verified.marker file is present.
func (c *Cache) ReadArtifact(digest string) ([]byte, bool, error) {
	p := c.ArtifactPath(digest)
	raw, verified, err := readVerifiedCacheFile(c.root, p)
	if err != nil {
		return nil, false, fmt.Errorf("read artifact cache %q: %w", p, err)
	}
	return raw, verified, nil
}

// MarkArtifactVerified writes a marker indicating the artifact has been
// cryptographically verified.
func (c *Cache) MarkArtifactVerified(digest string) error {
	return cache.WithCacheMutation(func() error {
		p := verifiedMarkerPath(filepath.Dir(c.ArtifactPath(digest)))
		return writeMarker(c.root, p)
	})
}

// InvalidateArtifactVerified removes the verification marker for an artifact.
func (c *Cache) InvalidateArtifactVerified(digest string) {
	_ = cache.WithCacheMutation(func() error {
		dir := filepath.Dir(c.ArtifactPath(digest))
		if err := validateCacheDirectory(c.root, dir); err != nil {
			return err
		}
		return os.Remove(verifiedMarkerPath(dir))
	})
}

// CollectionManifestPath returns the path for a collection manifest given its digest.
func (c *Cache) CollectionManifestPath(digest string) string {
	return filepath.Join(c.root, "collections", safeDigest(digest), "manifest.json")
}

// WriteCollectionManifest writes the collection manifest bytes and returns the path.
func (c *Cache) WriteCollectionManifest(digest string, raw []byte) (string, error) {
	return cache.WithCacheMutationLock(func() (string, error) {
		return c.writeCollectionManifest(digest, raw)
	})
}

func (c *Cache) writeCollectionManifest(digest string, raw []byte) (string, error) {
	p := c.CollectionManifestPath(digest)
	if err := ensureCacheDirectory(c.root, filepath.Dir(p)); err != nil {
		return "", fmt.Errorf("prepare collection cache %q: %w", p, err)
	}
	if err := writeAtomic(c.root, p, raw, 0o644, verifiedMarkerPath(filepath.Dir(p))); err != nil {
		return "", fmt.Errorf("write collection cache %q: %w", p, err)
	}
	return p, nil
}

// ReadCollectionManifest reads the cached collection manifest bytes and reports
// whether the verified.marker file is present.
func (c *Cache) ReadCollectionManifest(digest string) ([]byte, bool, error) {
	p := c.CollectionManifestPath(digest)
	raw, verified, err := readVerifiedCacheFile(c.root, p)
	if err != nil {
		return nil, false, fmt.Errorf("read collection cache %q: %w", p, err)
	}
	return raw, verified, nil
}

// MarkCollectionVerified writes a marker indicating the collection manifest has
// been cryptographically verified.
func (c *Cache) MarkCollectionVerified(digest string) error {
	return cache.WithCacheMutation(func() error {
		p := verifiedMarkerPath(filepath.Dir(c.CollectionManifestPath(digest)))
		return writeMarker(c.root, p)
	})
}

// MetadataPath returns the path for a cached resource revision JSON document.
func (c *Cache) MetadataPath(baseURL, sourceID, kind, name, revision string) string {
	return filepath.Join(
		c.root,
		"metadata",
		cache.HashURL(baseURL),
		safePathSegment(sourceID),
		safePathSegment(kind),
		safePathSegment(name),
		safeDigest(revision)+".json",
	)
}

// WriteResourceRevision caches a resource revision JSON document.
func (c *Cache) WriteResourceRevision(baseURL, sourceID, kind, name, revision string, rev store.ResourceRevision) (string, error) {
	return cache.WithCacheMutationLock(func() (string, error) {
		return c.writeResourceRevision(baseURL, sourceID, kind, name, revision, rev)
	})
}

func (c *Cache) writeResourceRevision(baseURL, sourceID, kind, name, revision string, rev store.ResourceRevision) (string, error) {
	p := c.MetadataPath(baseURL, sourceID, kind, name, revision)
	raw, err := json.Marshal(rev)
	if err != nil {
		return "", fmt.Errorf("marshal resource revision: %w", err)
	}
	if err := ensureCacheDirectory(c.root, filepath.Dir(p)); err != nil {
		return "", fmt.Errorf("prepare metadata cache %q: %w", p, err)
	}
	if err := writeAtomic(c.root, p, raw, 0o644, ""); err != nil {
		return "", fmt.Errorf("write metadata cache %q: %w", p, err)
	}
	return p, nil
}

// ReadResourceRevision reads a cached resource revision JSON document.
func (c *Cache) ReadResourceRevision(baseURL, sourceID, kind, name, revision string) (store.ResourceRevision, bool, error) {
	var rev store.ResourceRevision
	p := c.MetadataPath(baseURL, sourceID, kind, name, revision)
	exists, err := validateCacheRegularFile(c.root, p)
	if err != nil {
		return rev, false, fmt.Errorf("validate metadata cache %q: %w", p, err)
	}
	if !exists {
		return rev, false, nil
	}

	// The path check is intentionally a best-effort process-local guard; it is
	// not a cross-process or openat-style atomic guarantee.
	raw, err := os.ReadFile(p)
	if err != nil {
		return rev, false, fmt.Errorf("read metadata cache %q: %w", p, err)
	}
	if err := json.Unmarshal(raw, &rev); err != nil {
		return rev, true, fmt.Errorf("decode metadata cache %q: %w", p, err)
	}
	return rev, true, nil
}

func safeDigest(digest string) string {
	return strings.NewReplacer(":", "-", "/", "_", "\\", "_").Replace(digest)
}

func safePathSegment(s string) string {
	return url.PathEscape(strings.NewReplacer(string(filepath.Separator), "_").Replace(s))
}

func verifiedMarkerPath(dir string) string {
	return filepath.Join(dir, "verified.marker")
}

// readVerifiedCacheFile validates the cache file and its optional marker before
// reading. The path checks are intentionally best-effort process-local guards;
// they are not cross-process or openat-style atomic guarantees.
func readVerifiedCacheFile(root, path string) ([]byte, bool, error) {
	exists, err := validateCacheRegularFile(root, path)
	if err != nil {
		return nil, false, fmt.Errorf("validate cache file: %w", err)
	}
	if !exists {
		return nil, false, nil
	}

	marker := verifiedMarkerPath(filepath.Dir(path))
	verified, err := validateCacheRegularFile(root, marker)
	if err != nil {
		return nil, false, fmt.Errorf("validate verification marker %q: %w", marker, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	return raw, verified, nil
}

// validateCacheRegularFile checks a cache file without following symlinks.
func validateCacheRegularFile(root, path string) (bool, error) {
	if err := cache.ValidateDirectoryAncestry(root, filepath.Dir(path)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}

	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("path %q is a symlink", path)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("path %q is not a regular file", path)
	}
	return true, nil
}

func validateCacheDirectory(root, dir string) error {
	if err := cache.ValidateDirectoryAncestry(root, dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("validate cache directory %q: %w", dir, err)
	}
	return nil
}

func ensureCacheDirectory(root, dir string) error {
	if err := validateCacheDirectory(root, dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := cache.ValidateDirectoryAncestry(root, dir); err != nil {
		return fmt.Errorf("validate cache directory %q after create: %w", dir, err)
	}
	return nil
}

func writeMarker(root, p string) error {
	if err := ensureCacheDirectory(root, filepath.Dir(p)); err != nil {
		return fmt.Errorf("prepare verified marker %q: %w", p, err)
	}
	if err := writeAtomic(root, p, []byte("verified"), 0o644, ""); err != nil {
		return fmt.Errorf("write verified marker %q: %w", p, err)
	}
	return nil
}

// writeAtomic rechecks the path before writing and renaming. These path-based
// checks and the process-local cache lock do not provide cross-process or
// openat-style atomic guarantees.
func writeAtomic(root, path string, raw []byte, mode os.FileMode, invalidateMarker string) error {
	if err := cache.ValidateDirectoryAncestry(root, filepath.Dir(path)); err != nil {
		return fmt.Errorf("validate cache directory %q before write: %w", filepath.Dir(path), err)
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("cache file %q is a symlink", path)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := cache.ValidateDirectoryAncestry(root, filepath.Dir(path)); err != nil {
		return fmt.Errorf("validate cache directory %q before rename: %w", filepath.Dir(path), err)
	}
	if invalidateMarker != "" {
		if err := os.Remove(invalidateMarker); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return nil
}
