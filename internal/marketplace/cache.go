package marketplace

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/cache"
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

// ArtifactPath returns the path for an artifact archive given its digest.
func (c *Cache) ArtifactPath(digest string) string {
	return filepath.Join(c.root, "artifacts", safeDigest(digest), "artifact.tar.gz")
}

// WriteArtifact writes the artifact archive bytes and returns the path.
func (c *Cache) WriteArtifact(digest string, raw []byte) (string, error) {
	p := c.ArtifactPath(digest)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", fmt.Errorf("mkdir artifact cache %q: %w", p, err)
	}
	if err := writeAtomic(p, raw, 0o644, verifiedMarkerPath(filepath.Dir(p))); err != nil {
		return "", fmt.Errorf("write artifact cache %q: %w", p, err)
	}
	return p, nil
}

// ReadArtifact reads the cached artifact bytes and reports whether the
// verified.marker file is present.
func (c *Cache) ReadArtifact(digest string) ([]byte, bool, error) {
	p := c.ArtifactPath(digest)
	raw, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read artifact cache %q: %w", p, err)
	}
	verified := exists(verifiedMarkerPath(filepath.Dir(p)))
	return raw, verified, nil
}

// MarkArtifactVerified writes a marker indicating the artifact has been
// cryptographically verified.
func (c *Cache) MarkArtifactVerified(digest string) error {
	p := verifiedMarkerPath(filepath.Dir(c.ArtifactPath(digest)))
	return writeMarker(p)
}

// InvalidateArtifactVerified removes the verification marker for an artifact.
func (c *Cache) InvalidateArtifactVerified(digest string) {
	_ = os.Remove(verifiedMarkerPath(filepath.Dir(c.ArtifactPath(digest))))
}

// CollectionManifestPath returns the path for a collection manifest given its digest.
func (c *Cache) CollectionManifestPath(digest string) string {
	return filepath.Join(c.root, "collections", safeDigest(digest), "manifest.json")
}

// WriteCollectionManifest writes the collection manifest bytes and returns the path.
func (c *Cache) WriteCollectionManifest(digest string, raw []byte) (string, error) {
	p := c.CollectionManifestPath(digest)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", fmt.Errorf("mkdir collection cache %q: %w", p, err)
	}
	if err := writeAtomic(p, raw, 0o644, verifiedMarkerPath(filepath.Dir(p))); err != nil {
		return "", fmt.Errorf("write collection cache %q: %w", p, err)
	}
	return p, nil
}

// ReadCollectionManifest reads the cached collection manifest bytes and reports
// whether the verified.marker file is present.
func (c *Cache) ReadCollectionManifest(digest string) ([]byte, bool, error) {
	p := c.CollectionManifestPath(digest)
	raw, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read collection cache %q: %w", p, err)
	}
	verified := exists(verifiedMarkerPath(filepath.Dir(p)))
	return raw, verified, nil
}

// MarkCollectionVerified writes a marker indicating the collection manifest has
// been cryptographically verified.
func (c *Cache) MarkCollectionVerified(digest string) error {
	p := verifiedMarkerPath(filepath.Dir(c.CollectionManifestPath(digest)))
	return writeMarker(p)
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
	p := c.MetadataPath(baseURL, sourceID, kind, name, revision)
	raw, err := json.Marshal(rev)
	if err != nil {
		return "", fmt.Errorf("marshal resource revision: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", fmt.Errorf("mkdir metadata cache %q: %w", p, err)
	}
	if err := writeAtomic(p, raw, 0o644, ""); err != nil {
		return "", fmt.Errorf("write metadata cache %q: %w", p, err)
	}
	return p, nil
}

// ReadResourceRevision reads a cached resource revision JSON document.
func (c *Cache) ReadResourceRevision(baseURL, sourceID, kind, name, revision string) (store.ResourceRevision, bool, error) {
	var rev store.ResourceRevision
	p := c.MetadataPath(baseURL, sourceID, kind, name, revision)
	raw, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return rev, false, nil
		}
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

func writeMarker(p string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("mkdir verified marker %q: %w", p, err)
	}
	if err := writeAtomic(p, []byte("verified"), 0o644, ""); err != nil {
		return fmt.Errorf("write verified marker %q: %w", p, err)
	}
	return nil
}

func writeAtomic(path string, raw []byte, mode os.FileMode, invalidateMarker string) error {
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

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
