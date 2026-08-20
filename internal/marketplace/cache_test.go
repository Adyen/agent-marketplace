package marketplace

import (
	"os"
	"path/filepath"
	"testing"

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
}

func TestCacheResourceRevisionRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	c := NewCache(tmp)

	rev := store.ResourceRevision{
		Coordinate: store.ResourceCoordinate{
			Marketplace: store.MarketplaceID("mp"),
			Source:      store.SourceID("src"),
			Kind:        "skill",
			Name:        "review",
		},
		ContentDigest: store.RevisionDigest("sha256:3333333333333333333333333333333333333333333333333333333333333333"),
		ArchiveDigest: store.ArchiveDigest("sha256:4444444444444444444444444444444444444444444444444444444444444444"),
		License:       "MIT",
	}

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

	rev := store.ResourceRevision{
		Coordinate: store.ResourceCoordinate{
			Marketplace: store.MarketplaceID("mp"),
			Source:      store.SourceID("src/with/slash"),
			Kind:        "skill",
			Name:        "review",
		},
		ContentDigest: store.RevisionDigest("sha256:6666666666666666666666666666666666666666666666666666666666666666"),
	}

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
	rev := store.ResourceRevision{
		Coordinate: store.ResourceCoordinate{
			Marketplace: store.MarketplaceID("mp"),
			Source:      store.SourceID("src"),
			Kind:        "skill",
			Name:        "review",
		},
		ContentDigest: store.RevisionDigest("sha256:7777777777777777777777777777777777777777777777777777777777777777"),
		ArchiveDigest: store.ArchiveDigest("sha256:8888888888888888888888888888888888888888888888888888888888888888"),
		License:       "MIT",
		Provenance: &store.Provenance{
			SourceID:       store.SourceID("test"),
			Commit:         "abc123",
			ManifestDigest: store.RevisionDigest("sha256:9999999999999999999999999999999999999999999999999999999999999999"),
		},
	}
	for i := 0; i < b.N; i++ {
		if _, err := c.WriteResourceRevision("https://marketplace.example", "src", "skill", "review", "sha256:7777777777777777777777777777777777777777777777777777777777777777", rev); err != nil {
			b.Fatalf("WriteResourceRevision: %v", err)
		}
		if _, _, err := c.ReadResourceRevision("https://marketplace.example", "src", "skill", "review", "sha256:7777777777777777777777777777777777777777777777777777777777777777"); err != nil {
			b.Fatalf("ReadResourceRevision: %v", err)
		}
	}
}
