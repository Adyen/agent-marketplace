package marketplace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestCacheArtifactRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	c := NewCache(tmp)

	digest := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	data := []byte("artifact-bytes")

	p, err := c.WriteArtifact(digest, data)
	if err != nil {
		t.Fatalf("WriteArtifact: %v", err)
	}
	if p != c.ArtifactPath(digest) {
		t.Fatalf("path mismatch: %s vs %s", p, c.ArtifactPath(digest))
	}

	raw, verified, err := c.ReadArtifact(digest)
	if err != nil {
		t.Fatalf("ReadArtifact: %v", err)
	}
	if verified {
		t.Fatalf("artifact should not be verified yet")
	}
	if string(raw) != string(data) {
		t.Fatalf("artifact data mismatch")
	}

	if err := c.MarkArtifactVerified(digest); err != nil {
		t.Fatalf("MarkArtifactVerified: %v", err)
	}
	raw, verified, err = c.ReadArtifact(digest)
	if err != nil {
		t.Fatalf("ReadArtifact after verify: %v", err)
	}
	if !verified {
		t.Fatalf("artifact should be verified after marker")
	}
	if string(raw) != string(data) {
		t.Fatalf("artifact data mismatch after verify")
	}

	if _, err := c.WriteArtifact(digest, []byte("replacement")); err != nil {
		t.Fatalf("WriteArtifact replacement: %v", err)
	}
	raw, verified, err = c.ReadArtifact(digest)
	if err != nil {
		t.Fatalf("ReadArtifact after replacement: %v", err)
	}
	if verified {
		t.Fatalf("replacement artifact must not retain stale verification marker")
	}
	if string(raw) != "replacement" {
		t.Fatalf("replacement artifact = %q", raw)
	}
}

func TestIndexOnlyCacheSeparatesOccurrences(t *testing.T) {
	cache := NewCache(t.TempDir())
	identityA := IndexOnlyIdentity{Marketplace: "marketplace", SourceID: "source", Commit: strings.Repeat("a", 40), ManifestDigest: "sha256:" + strings.Repeat("1", 64)}
	identityB := identityA
	identityB.ManifestDigest = "sha256:" + strings.Repeat("2", 64)
	contentDigest := "sha256:" + strings.Repeat("3", 64)
	if IndexOnlyKey(identityA) == IndexOnlyKey(identityB) {
		t.Fatal("different source occurrence identities share a cache key")
	}
	if _, err := cache.WriteIndexOnlyArtifact(identityA, contentDigest, []byte("A")); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.WriteIndexOnlyArtifact(identityB, contentDigest, []byte("B")); err != nil {
		t.Fatal(err)
	}
	for identity, want := range map[IndexOnlyIdentity]string{identityA: "A", identityB: "B"} {
		got, _, err := cache.ReadIndexOnlyArtifact(identity, contentDigest)
		if err != nil || string(got) != want {
			t.Fatalf("occurrence cache read = %q/%v, want %q", got, err, want)
		}
	}
}

func TestCacheArtifactMissing(t *testing.T) {
	tmp := t.TempDir()
	c := NewCache(tmp)

	raw, verified, err := c.ReadArtifact("sha256:1111111111111111111111111111111111111111111111111111111111111111")
	if err != nil {
		t.Fatalf("ReadArtifact missing: %v", err)
	}
	if raw != nil {
		t.Fatalf("expected nil data for missing artifact")
	}
	if verified {
		t.Fatalf("missing artifact cannot be verified")
	}
}

func TestCacheCollectionManifestRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	c := NewCache(tmp)

	digest := "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	data := []byte(`{"members":[],"version":"1"}`)

	p, err := c.WriteCollectionManifest(digest, data)
	if err != nil {
		t.Fatalf("WriteCollectionManifest: %v", err)
	}
	if p != c.CollectionManifestPath(digest) {
		t.Fatalf("path mismatch: %s vs %s", p, c.CollectionManifestPath(digest))
	}

	raw, verified, err := c.ReadCollectionManifest(digest)
	if err != nil {
		t.Fatalf("ReadCollectionManifest: %v", err)
	}
	if verified {
		t.Fatalf("collection should not be verified yet")
	}
	if string(raw) != string(data) {
		t.Fatalf("collection data mismatch")
	}

	if err := c.MarkCollectionVerified(digest); err != nil {
		t.Fatalf("MarkCollectionVerified: %v", err)
	}
	_, verified, err = c.ReadCollectionManifest(digest)
	if err != nil {
		t.Fatalf("ReadCollectionManifest after verify: %v", err)
	}
	if !verified {
		t.Fatalf("collection should be verified after marker")
	}

	if _, err := c.WriteCollectionManifest(digest, []byte(`{"members":["review"],"version":"1"}`)); err != nil {
		t.Fatalf("WriteCollectionManifest replacement: %v", err)
	}
	raw, verified, err = c.ReadCollectionManifest(digest)
	if err != nil {
		t.Fatalf("ReadCollectionManifest after replacement: %v", err)
	}
	if verified {
		t.Fatalf("replacement collection must not retain stale verification marker")
	}
	if string(raw) != `{"members":["review"],"version":"1"}` {
		t.Fatalf("replacement collection = %q", raw)
	}
}

func TestCacheResourceRevisionRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	c := NewCache(tmp)

	rev := assessedRevision(store.ResourceRevision{
		Coordinate: store.ResourceCoordinate{
			Marketplace: store.MarketplaceID("mp"),
			Source:      store.SourceID("src"),
			Kind:        "skill",
			Name:        "review",
		},
		ContentDigest: store.RevisionDigest("sha256:3333333333333333333333333333333333333333333333333333333333333333"),
		ArchiveDigest: store.ArchiveDigest("sha256:4444444444444444444444444444444444444444444444444444444444444444"),
		License:       "MIT",
	})

	p, err := c.WriteResourceRevision("https://marketplace.example", "src", "skill", "review", "sha256:3333333333333333333333333333333333333333333333333333333333333333", rev)
	if err != nil {
		t.Fatalf("WriteResourceRevision: %v", err)
	}
	if p != c.MetadataPath("https://marketplace.example", "src", "skill", "review", "sha256:3333333333333333333333333333333333333333333333333333333333333333") {
		t.Fatalf("metadata path mismatch")
	}

	got, ok, err := c.ReadResourceRevision("https://marketplace.example", "src", "skill", "review", "sha256:3333333333333333333333333333333333333333333333333333333333333333")
	if err != nil {
		t.Fatalf("ReadResourceRevision: %v", err)
	}
	if !ok {
		t.Fatalf("expected metadata to be found")
	}
	if got.ContentDigest != rev.ContentDigest || got.License != rev.License {
		t.Fatalf("revision mismatch: %+v", got)
	}
}

func TestCacheResourceRevisionMissing(t *testing.T) {
	tmp := t.TempDir()
	c := NewCache(tmp)

	rev, ok, err := c.ReadResourceRevision("https://marketplace.example", "src", "skill", "review", "sha256:5555555555555555555555555555555555555555555555555555555555555555")
	if err != nil {
		t.Fatalf("ReadResourceRevision missing: %v", err)
	}
	if ok {
		t.Fatalf("expected missing metadata")
	}
	if !rev.Coordinate.IsZero() {
		t.Fatalf("expected zero revision")
	}
}

func TestCacheDefaultRoot(t *testing.T) {
	c := DefaultCache()
	if c.root == "" {
		t.Fatalf("default cache root empty")
	}
	if !filepath.IsAbs(c.root) && !filepath.IsLocal(c.root) {
		t.Fatalf("default cache root not a valid path: %s", c.root)
	}
}

func TestCacheSanitizesSourceIDWithSlash(t *testing.T) {
	tmp := t.TempDir()
	c := NewCache(tmp)

	rev := assessedRevision(store.ResourceRevision{
		Coordinate: store.ResourceCoordinate{
			Marketplace: store.MarketplaceID("mp"),
			Source:      store.SourceID("src/with/slash"),
			Kind:        "skill",
			Name:        "review",
		},
		ContentDigest: store.RevisionDigest("sha256:6666666666666666666666666666666666666666666666666666666666666666"),
	})

	p, err := c.WriteResourceRevision("https://marketplace.example", "src/with/slash", "skill", "review", "sha256:6666666666666666666666666666666666666666666666666666666666666666", rev)
	if err != nil {
		t.Fatalf("WriteResourceRevision: %v", err)
	}

	if _, err := os.Stat(p); err != nil {
		t.Fatalf("metadata path does not exist: %s", p)
	}

	_, ok, err := c.ReadResourceRevision("https://marketplace.example", "src/with/slash", "skill", "review", "sha256:6666666666666666666666666666666666666666666666666666666666666666")
	if err != nil {
		t.Fatalf("ReadResourceRevision: %v", err)
	}
	if !ok {
		t.Fatalf("expected sanitized metadata to be found")
	}
}

func TestCacheWriteMethodsRejectSymlinkedDirectories(t *testing.T) {
	artifactDigest := "sha256:" + strings.Repeat("1", 64)
	collectionDigest := "sha256:" + strings.Repeat("2", 64)
	metadataRevision := "sha256:" + strings.Repeat("3", 64)
	baseURL := "https://marketplace.example"
	sourceID, kind, name := "src", "skill", "review"
	revision := assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: store.SourceID(sourceID), Kind: kind, Name: name},
		ContentDigest: store.RevisionDigest(metadataRevision),
	})

	tests := []struct {
		name        string
		setup       func(root, external string) error
		write       func(*Cache) error
		expectedOut func(external string) string
	}{
		{
			name: "cache root",
			setup: func(root, external string) error {
				if err := os.Remove(root); err != nil {
					return err
				}
				return os.Symlink(external, root)
			},
			write: func(c *Cache) error {
				_, err := c.WriteArtifact(artifactDigest, []byte("artifact"))
				return err
			},
			expectedOut: func(external string) string {
				return filepath.Join(external, "artifacts", safeDigest(artifactDigest), "artifact.tar.gz")
			},
		},
		{
			name: "artifact root",
			setup: func(root, external string) error {
				return os.Symlink(external, filepath.Join(root, "artifacts"))
			},
			write: func(c *Cache) error {
				_, err := c.WriteArtifact(artifactDigest, []byte("artifact"))
				return err
			},
			expectedOut: func(external string) string {
				return filepath.Join(external, safeDigest(artifactDigest), "artifact.tar.gz")
			},
		},
		{
			name: "artifact digest directory",
			setup: func(root, external string) error {
				digestDir := filepath.Join(root, "artifacts", safeDigest(artifactDigest))
				if err := os.MkdirAll(filepath.Dir(digestDir), 0o755); err != nil {
					return err
				}
				return os.Symlink(external, digestDir)
			},
			write: func(c *Cache) error {
				_, err := c.WriteArtifact(artifactDigest, []byte("artifact"))
				return err
			},
			expectedOut: func(external string) string { return filepath.Join(external, "artifact.tar.gz") },
		},
		{
			name: "collection root",
			setup: func(root, external string) error {
				return os.Symlink(external, filepath.Join(root, "collections"))
			},
			write: func(c *Cache) error {
				_, err := c.WriteCollectionManifest(collectionDigest, []byte("{}"))
				return err
			},
			expectedOut: func(external string) string {
				return filepath.Join(external, safeDigest(collectionDigest), "manifest.json")
			},
		},
		{
			name: "collection digest directory",
			setup: func(root, external string) error {
				digestDir := filepath.Join(root, "collections", safeDigest(collectionDigest))
				if err := os.MkdirAll(filepath.Dir(digestDir), 0o755); err != nil {
					return err
				}
				return os.Symlink(external, digestDir)
			},
			write: func(c *Cache) error {
				_, err := c.WriteCollectionManifest(collectionDigest, []byte("{}"))
				return err
			},
			expectedOut: func(external string) string { return filepath.Join(external, "manifest.json") },
		},
		{
			name: "metadata root",
			setup: func(root, external string) error {
				return os.Symlink(external, filepath.Join(root, "metadata"))
			},
			write: func(c *Cache) error {
				_, err := c.WriteResourceRevision(baseURL, sourceID, kind, name, metadataRevision, revision)
				return err
			},
			expectedOut: func(external string) string {
				return filepath.Join(external, cache.HashURL(baseURL), sourceID, kind, name, safeDigest(metadataRevision)+".json")
			},
		},
		{
			name: "metadata parent directory",
			setup: func(root, external string) error {
				hashDir := filepath.Join(root, "metadata", cache.HashURL(baseURL))
				if err := os.MkdirAll(filepath.Dir(hashDir), 0o755); err != nil {
					return err
				}
				return os.Symlink(external, hashDir)
			},
			write: func(c *Cache) error {
				_, err := c.WriteResourceRevision(baseURL, sourceID, kind, name, metadataRevision, revision)
				return err
			},
			expectedOut: func(external string) string {
				return filepath.Join(external, sourceID, kind, name, safeDigest(metadataRevision)+".json")
			},
		},
		{
			name: "metadata digest parent directory",
			setup: func(root, external string) error {
				digestDir := filepath.Join(root, "metadata", cache.HashURL(baseURL), sourceID, kind, name)
				if err := os.MkdirAll(filepath.Dir(digestDir), 0o755); err != nil {
					return err
				}
				return os.Symlink(external, digestDir)
			},
			write: func(c *Cache) error {
				_, err := c.WriteResourceRevision(baseURL, sourceID, kind, name, metadataRevision, revision)
				return err
			},
			expectedOut: func(external string) string {
				return filepath.Join(external, safeDigest(metadataRevision)+".json")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, external := t.TempDir(), t.TempDir()
			if err := tt.setup(root, external); err != nil {
				t.Fatalf("setup symlink: %v", err)
			}
			if err := tt.write(NewCache(root)); err == nil {
				t.Fatal("expected symlinked cache path to be rejected")
			}
			expectedOut := tt.expectedOut(external)
			if _, err := os.Lstat(expectedOut); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("symlinked cache write reached external path %q, stat err=%v", expectedOut, err)
			}
		})
	}
}

func TestCacheReadMethodsRejectSymlinkedFilesAndDirectories(t *testing.T) {
	artifactDigest := "sha256:" + strings.Repeat("1", 64)
	collectionDigest := "sha256:" + strings.Repeat("2", 64)
	metadataRevision := "sha256:" + strings.Repeat("3", 64)
	baseURL := "https://marketplace.example"

	tests := []struct {
		name string
		read func(*Cache) ([]byte, bool, error)
		path func(*Cache) string
		data []byte
	}{
		{
			name: "artifact file",
			read: func(c *Cache) ([]byte, bool, error) { return c.ReadArtifact(artifactDigest) },
			path: func(c *Cache) string { return c.ArtifactPath(artifactDigest) },
			data: []byte("external artifact"),
		},
		{
			name: "collection manifest file",
			read: func(c *Cache) ([]byte, bool, error) { return c.ReadCollectionManifest(collectionDigest) },
			path: func(c *Cache) string { return c.CollectionManifestPath(collectionDigest) },
			data: []byte("external collection"),
		},
		{
			name: "resource revision file",
			read: func(c *Cache) ([]byte, bool, error) {
				_, ok, err := c.ReadResourceRevision(baseURL, "src", "skill", "review", metadataRevision)
				return []byte{}, ok, err
			},
			path: func(c *Cache) string {
				return c.MetadataPath(baseURL, "src", "skill", "review", metadataRevision)
			},
			data: []byte(`{"external":true}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, external := t.TempDir(), t.TempDir()
			c := NewCache(root)
			p := tt.path(c)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatalf("mkdir cache path: %v", err)
			}
			externalPath := filepath.Join(external, filepath.Base(p))
			if err := os.WriteFile(externalPath, tt.data, 0o644); err != nil {
				t.Fatalf("write external file: %v", err)
			}
			if err := os.Symlink(externalPath, p); err != nil {
				t.Fatalf("symlink cache file: %v", err)
			}

			raw, _, err := tt.read(c)
			if err == nil {
				t.Fatal("expected symlinked cache file to be rejected")
			}
			if string(raw) == string(tt.data) {
				t.Fatal("read returned bytes from external symlink target")
			}
		})
	}

	t.Run("artifact directory", func(t *testing.T) {
		root, external := t.TempDir(), t.TempDir()
		c := NewCache(root)
		externalArtifact := filepath.Join(external, "artifact.tar.gz")
		if err := os.WriteFile(externalArtifact, []byte("external artifact"), 0o644); err != nil {
			t.Fatalf("write external artifact: %v", err)
		}
		digestDir := filepath.Dir(c.ArtifactPath(artifactDigest))
		if err := os.MkdirAll(filepath.Dir(digestDir), 0o755); err != nil {
			t.Fatalf("mkdir artifact parent: %v", err)
		}
		if err := os.Symlink(external, digestDir); err != nil {
			t.Fatalf("symlink artifact directory: %v", err)
		}

		raw, _, err := c.ReadArtifact(artifactDigest)
		if err == nil {
			t.Fatal("expected symlinked artifact directory to be rejected")
		}
		if string(raw) == "external artifact" {
			t.Fatal("read returned bytes from external artifact directory")
		}
	})
}

func TestCacheReadRejectsSymlinkedVerificationMarkers(t *testing.T) {
	tests := []struct {
		name  string
		write func(*Cache) error
		read  func(*Cache) error
		mark  func(*Cache) string
	}{
		{
			name: "artifact",
			write: func(c *Cache) error {
				_, err := c.WriteArtifact("sha256:"+strings.Repeat("4", 64), []byte("artifact"))
				return err
			},
			read: func(c *Cache) error {
				_, _, err := c.ReadArtifact("sha256:" + strings.Repeat("4", 64))
				return err
			},
			mark: func(c *Cache) string {
				return verifiedMarkerPath(filepath.Dir(c.ArtifactPath("sha256:" + strings.Repeat("4", 64))))
			},
		},
		{
			name: "collection",
			write: func(c *Cache) error {
				_, err := c.WriteCollectionManifest("sha256:"+strings.Repeat("5", 64), []byte("{}"))
				return err
			},
			read: func(c *Cache) error {
				_, _, err := c.ReadCollectionManifest("sha256:" + strings.Repeat("5", 64))
				return err
			},
			mark: func(c *Cache) string {
				return verifiedMarkerPath(filepath.Dir(c.CollectionManifestPath("sha256:" + strings.Repeat("5", 64))))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, external := t.TempDir(), t.TempDir()
			c := NewCache(root)
			if err := tt.write(c); err != nil {
				t.Fatalf("write cache entry: %v", err)
			}
			marker := tt.mark(c)
			externalMarker := filepath.Join(external, "verified.marker")
			if err := os.WriteFile(externalMarker, []byte("verified externally"), 0o644); err != nil {
				t.Fatalf("write external marker: %v", err)
			}
			if err := os.Symlink(externalMarker, marker); err != nil {
				t.Fatalf("symlink marker: %v", err)
			}

			if err := tt.read(c); err == nil {
				t.Fatal("expected symlinked verification marker to be rejected")
			}
		})
	}
}

func BenchmarkCacheWriteReadArtifact(b *testing.B) {
	tmp := b.TempDir()
	c := NewCache(tmp)
	digest := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	data := []byte("benchmark artifact bytes")
	for i := 0; i < b.N; i++ {
		if _, err := c.WriteArtifact(digest, data); err != nil {
			b.Fatalf("WriteArtifact: %v", err)
		}
		if _, _, err := c.ReadArtifact(digest); err != nil {
			b.Fatalf("ReadArtifact: %v", err)
		}
	}
}

func BenchmarkCacheWriteReadResourceRevision(b *testing.B) {
	tmp := b.TempDir()
	c := NewCache(tmp)
	rev := assessedRevision(store.ResourceRevision{
		Coordinate: store.ResourceCoordinate{
			Marketplace: store.MarketplaceID("mp"),
			Source:      store.SourceID("src"),
			Kind:        "skill",
			Name:        "review",
		},
		ContentDigest: store.RevisionDigest("sha256:7777777777777777777777777777777777777777777777777777777777777777"),
		ArchiveDigest: store.ArchiveDigest("sha256:8888888888888888888888888888888888888888888888888888888888888888"),
		License:       "MIT",
	})
	for i := 0; i < b.N; i++ {
		if _, err := c.WriteResourceRevision("https://marketplace.example", "src", "skill", "review", "sha256:7777777777777777777777777777777777777777777777777777777777777777", rev); err != nil {
			b.Fatalf("WriteResourceRevision: %v", err)
		}
		if _, _, err := c.ReadResourceRevision("https://marketplace.example", "src", "skill", "review", "sha256:7777777777777777777777777777777777777777777777777777777777777777"); err != nil {
			b.Fatalf("ReadResourceRevision: %v", err)
		}
	}
}

func TestCacheGarbageCollectPreservesReferencedMirrorArchive(t *testing.T) {
	c := NewCache(t.TempDir())
	contentDigest := "sha256:" + strings.Repeat("1", 64)
	referencedArchive := "sha256:" + strings.Repeat("2", 64)
	orphanArchive := "sha256:" + strings.Repeat("3", 64)
	pin := config.MarketplaceResourcePin{SourceID: "src", Kind: "skill", Name: "review", Revision: "v1", Digest: contentDigest}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: "https://marketplace.example", InstanceID: "mp", Resources: []config.MarketplaceResourcePin{pin}}}}
	rev := assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(contentDigest),
		ArchiveDigest: store.ArchiveDigest(referencedArchive),
	})
	if _, err := c.WriteResourceRevision(cfg.Marketplaces[0].URL, pin.SourceID, pin.Kind, pin.Name, pin.Digest, rev); err != nil {
		t.Fatalf("WriteResourceRevision: %v", err)
	}
	if _, err := c.WriteArtifact(referencedArchive, []byte("referenced")); err != nil {
		t.Fatalf("WriteArtifact referenced: %v", err)
	}
	if err := c.MarkArtifactVerified(referencedArchive); err != nil {
		t.Fatalf("MarkArtifactVerified: %v", err)
	}
	if _, err := c.WriteArtifact(orphanArchive, []byte("orphan")); err != nil {
		t.Fatalf("WriteArtifact orphan: %v", err)
	}
	if err := c.MarkArtifactVerified(orphanArchive); err != nil {
		t.Fatalf("MarkArtifactVerified orphan: %v", err)
	}
	collectionDigest := "sha256:" + strings.Repeat("4", 64)
	collectionPath, err := c.WriteCollectionManifest(collectionDigest, []byte(`{"members":[]}`))
	if err != nil {
		t.Fatalf("WriteCollectionManifest: %v", err)
	}

	result, err := c.GarbageCollect(false, cfg)
	if err != nil {
		t.Fatalf("GarbageCollect: %v", err)
	}
	if len(result.Removed) != 1 || result.Removed[0] != filepath.Dir(c.ArtifactPath(orphanArchive)) {
		t.Fatalf("removed = %v, want orphan artifact directory", result.Removed)
	}
	if _, err := os.Stat(c.ArtifactPath(referencedArchive)); err != nil {
		t.Fatalf("referenced archive was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(c.ArtifactPath(referencedArchive)), "verified.marker")); err != nil {
		t.Fatalf("referenced verification marker was removed: %v", err)
	}
	if _, err := os.Stat(collectionPath); err != nil {
		t.Fatalf("collection manifest was removed: %v", err)
	}
}

func TestCacheGarbageCollectMissingMetadataFailsClosed(t *testing.T) {
	c := NewCache(t.TempDir())
	archiveDigest := "sha256:" + strings.Repeat("a", 64)
	if _, err := c.WriteArtifact(archiveDigest, []byte("orphan")); err != nil {
		t.Fatalf("WriteArtifact: %v", err)
	}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{
		URL: "https://marketplace.example", InstanceID: "mp",
		Resources: []config.MarketplaceResourcePin{{SourceID: "src", Kind: "skill", Name: "review", Revision: "v1", Digest: "sha256:" + strings.Repeat("b", 64)}},
	}}}
	result, err := c.GarbageCollect(false, cfg)
	if err == nil {
		t.Fatal("expected missing metadata error")
	}
	if len(result.Removed) != 0 {
		t.Fatalf("failed-closed GC reported removals: %v", result.Removed)
	}
	if _, statErr := os.Stat(c.ArtifactPath(archiveDigest)); statErr != nil {
		t.Fatalf("missing metadata GC mutated artifact cache: %v", statErr)
	}
}

func TestCacheGarbageCollectMalformedMetadataFailsClosed(t *testing.T) {
	c := NewCache(t.TempDir())
	contentDigest := "sha256:" + strings.Repeat("e", 64)
	archiveDigest := "sha256:" + strings.Repeat("f", 64)
	if _, err := c.WriteArtifact(archiveDigest, []byte("orphan")); err != nil {
		t.Fatalf("WriteArtifact: %v", err)
	}
	pin := config.MarketplaceResourcePin{SourceID: "src", Kind: "skill", Name: "review", Revision: "v1", Digest: contentDigest}
	metadataPath := c.MetadataPath("https://marketplace.example", pin.SourceID, pin.Kind, pin.Name, pin.Digest)
	if err := os.MkdirAll(filepath.Dir(metadataPath), 0o755); err != nil {
		t.Fatalf("mkdir metadata: %v", err)
	}
	if err := os.WriteFile(metadataPath, []byte("not-json"), 0o644); err != nil {
		t.Fatalf("write malformed metadata: %v", err)
	}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: "https://marketplace.example", InstanceID: "mp", Resources: []config.MarketplaceResourcePin{pin}}}}
	result, err := c.GarbageCollect(false, cfg)
	if err == nil {
		t.Fatal("expected malformed metadata error")
	}
	if len(result.Removed) != 0 {
		t.Fatalf("malformed metadata GC reported removals: %v", result.Removed)
	}
	if _, statErr := os.Stat(c.ArtifactPath(archiveDigest)); statErr != nil {
		t.Fatalf("malformed metadata GC mutated artifact cache: %v", statErr)
	}
}

func TestCacheGarbageCollectDryRunDoesNotMutate(t *testing.T) {
	c := NewCache(t.TempDir())
	archiveDigest := "sha256:" + strings.Repeat("c", 64)
	if _, err := c.WriteArtifact(archiveDigest, []byte("orphan")); err != nil {
		t.Fatalf("WriteArtifact: %v", err)
	}
	result, err := c.GarbageCollect(true, &config.Config{})
	if err != nil {
		t.Fatalf("GarbageCollect dry-run: %v", err)
	}
	if len(result.Removed) != 1 || result.Removed[0] != filepath.Dir(c.ArtifactPath(archiveDigest)) {
		t.Fatalf("dry-run removed = %v", result.Removed)
	}
	if _, err := os.Stat(c.ArtifactPath(archiveDigest)); err != nil {
		t.Fatalf("dry-run removed artifact: %v", err)
	}
}

func TestCacheGarbageCollectPreservesMalformedAndUnknownArtifactEntries(t *testing.T) {
	c := NewCache(t.TempDir())
	unknown := filepath.Join(c.root, "artifacts", "not-a-digest")
	malformed := filepath.Join(c.root, "artifacts", "sha256-"+strings.Repeat("d", 64))
	if err := os.MkdirAll(unknown, 0o755); err != nil {
		t.Fatalf("mkdir unknown: %v", err)
	}
	if err := os.MkdirAll(malformed, 0o755); err != nil {
		t.Fatalf("mkdir malformed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(malformed, "unexpected"), []byte("keep"), 0o644); err != nil {
		t.Fatalf("write malformed: %v", err)
	}
	if _, err := c.GarbageCollect(false, &config.Config{}); err != nil {
		t.Fatalf("GarbageCollect: %v", err)
	}
	for _, path := range []string{unknown, malformed} {
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				t.Fatalf("GC removed preserved entry %q", path)
			}
			t.Fatalf("stat preserved entry %q: %v", path, err)
		}
	}
}

func TestCacheGarbageCollectRejectsSymlinkedRoot(t *testing.T) {
	targetRoot := t.TempDir()
	linkParent := t.TempDir()
	linkRoot := filepath.Join(linkParent, "marketplace")
	cache := NewCache(targetRoot)
	archiveDigest := "sha256:" + strings.Repeat("1", 64)
	if _, err := cache.WriteArtifact(archiveDigest, []byte("orphan")); err != nil {
		t.Fatalf("WriteArtifact: %v", err)
	}
	if err := os.Symlink(targetRoot, linkRoot); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	result, err := NewCache(linkRoot).GarbageCollect(false, &config.Config{})
	if err == nil {
		t.Fatal("expected symlinked root to fail closed")
	}
	if len(result.Removed) != 0 {
		t.Fatalf("symlinked root reported removals: %v", result.Removed)
	}
	if _, err := os.Stat(cache.ArtifactPath(archiveDigest)); err != nil {
		t.Fatalf("symlinked root removed artifact: %v", err)
	}
}

func TestCacheGarbageCollectRejectsSymlinkedArtifactsDirectory(t *testing.T) {
	root := t.TempDir()
	externalRoot := t.TempDir()
	externalCache := NewCache(externalRoot)
	archiveDigest := "sha256:" + strings.Repeat("2", 64)
	if _, err := externalCache.WriteArtifact(archiveDigest, []byte("orphan")); err != nil {
		t.Fatalf("WriteArtifact: %v", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	if err := os.Symlink(filepath.Join(externalRoot, "artifacts"), filepath.Join(root, "artifacts")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	result, err := NewCache(root).GarbageCollect(false, &config.Config{})
	if err == nil {
		t.Fatal("expected symlinked artifacts directory to fail closed")
	}
	if len(result.Removed) != 0 {
		t.Fatalf("symlinked artifacts directory reported removals: %v", result.Removed)
	}
	if _, err := os.Stat(externalCache.ArtifactPath(archiveDigest)); err != nil {
		t.Fatalf("symlinked artifacts directory removed external artifact: %v", err)
	}
}

func TestCacheGarbageCollectRejectsSymlinkedArtifactDirectory(t *testing.T) {
	root := t.TempDir()
	cache := NewCache(root)
	externalRoot := t.TempDir()
	externalCache := NewCache(externalRoot)
	archiveDigest := "sha256:" + strings.Repeat("3", 64)
	if _, err := externalCache.WriteArtifact(archiveDigest, []byte("orphan")); err != nil {
		t.Fatalf("WriteArtifact: %v", err)
	}
	artifactDir := filepath.Dir(cache.ArtifactPath(archiveDigest))
	if err := os.MkdirAll(filepath.Dir(artifactDir), 0o755); err != nil {
		t.Fatalf("mkdir artifacts: %v", err)
	}
	if err := os.Symlink(filepath.Dir(externalCache.ArtifactPath(archiveDigest)), artifactDir); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	result, err := cache.GarbageCollect(false, &config.Config{})
	if err == nil {
		t.Fatal("expected symlinked artifact directory to fail closed")
	}
	if len(result.Removed) != 0 {
		t.Fatalf("symlinked artifact directory reported removals: %v", result.Removed)
	}
	if _, err := os.Stat(externalCache.ArtifactPath(archiveDigest)); err != nil {
		t.Fatalf("symlinked artifact directory removed external artifact: %v", err)
	}
}

func TestCacheGarbageCollectReportsOnlySuccessfulRemovals(t *testing.T) {
	root := t.TempDir()
	cache := NewCache(root)
	firstDigest := "sha256:" + strings.Repeat("1", 64)
	failedDigest := "sha256:" + strings.Repeat("2", 64)
	if _, err := cache.WriteArtifact(firstDigest, []byte("first")); err != nil {
		t.Fatalf("WriteArtifact first: %v", err)
	}
	if _, err := cache.WriteArtifact(failedDigest, []byte("failed")); err != nil {
		t.Fatalf("WriteArtifact failed: %v", err)
	}
	failedDir := filepath.Dir(cache.ArtifactPath(failedDigest))
	if err := os.Chmod(failedDir, 0o555); err != nil {
		t.Fatalf("chmod failed artifact: %v", err)
	}
	defer func() { _ = os.Chmod(failedDir, 0o755) }()

	result, err := cache.GarbageCollect(false, &config.Config{})
	if err == nil {
		t.Skip("filesystem permitted deletion of a read-only artifact directory")
	}
	firstDir := filepath.Dir(cache.ArtifactPath(firstDigest))
	if len(result.Removed) != 1 || result.Removed[0] != firstDir {
		t.Fatalf("removed = %v, want only successfully removed path %q", result.Removed, firstDir)
	}
	if _, err := os.Stat(firstDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("successful removal left first artifact, stat err=%v", err)
	}
	if _, err := os.Stat(failedDir); err != nil {
		t.Fatalf("failed removal did not preserve later artifact: %v", err)
	}
}
