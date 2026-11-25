package marketplace

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func cacheCollection(t *testing.T, cache *Cache, baseURL, name string, verified bool, modified time.Time, members ...string) string {
	t.Helper()
	manifest := hccartifact.CollectionManifest{SchemaVersion: hccartifact.CollectionSchemaV1, Name: name, SupportedSchemas: []string{hccartifact.CollectionSchemaV1}}
	for _, member := range members {
		manifest.Members = append(manifest.Members, hccartifact.CollectionMemberEntry{Kind: "skill", Name: member, RegistryID: "src", Revision: "sha256:" + strings.Repeat("a", 64)})
	}
	payload, digest, err := hccartifact.EncodeCollectionManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "collection", Name: name}
	path, err := cache.WriteResourceRevision(baseURL, "src", "collection", name, digest, evidenceRevision(coord, digest, ""))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.WriteCollectionManifest(digest, payload); err != nil {
		t.Fatal(err)
	}
	if verified {
		if err := cache.MarkCollectionVerified(digest); err != nil {
			t.Fatal(err)
		}
	}
	return digest
}

func leafNames(leaves []hccartifact.CollectionMemberEntry) []string {
	names := make([]string, 0, len(leaves))
	for _, leaf := range leaves {
		names = append(names, leaf.Name)
	}
	return names
}

// spec: session-startup/cached-collection-leaves-prefer-the-exact-then-newest-verified-revision
func TestCachedCollectionLeavesPrefersExactThenNewestVerifiedRevision(t *testing.T) {
	cache := NewCache(t.TempDir())
	const url = "https://market.example"
	now := time.Now()
	older := cacheCollection(t, cache, url, "bundle", true, now.Add(-time.Hour), "alpha")
	cacheCollection(t, cache, url, "bundle", true, now, "beta")
	cacheCollection(t, cache, url, "bundle", false, now.Add(time.Hour), "unverified")

	placeholder := "sha256:" + strings.Repeat("0", 64)
	if leaves, ok := cache.CachedCollectionLeaves(url, "src", "bundle", placeholder); !ok || !slices.Equal(leafNames(leaves), []string{"beta"}) {
		t.Fatalf("unknown revision leaves = %v ok=%v, want the newest verified manifest [beta]", leafNames(leaves), ok)
	}
	if leaves, ok := cache.CachedCollectionLeaves(url, "src", "bundle", older); !ok || !slices.Equal(leafNames(leaves), []string{"alpha"}) {
		t.Fatalf("exact revision leaves = %v ok=%v, want [alpha]", leafNames(leaves), ok)
	}
	if _, ok := cache.CachedCollectionLeaves(url, "src", "absent", placeholder); ok {
		t.Fatal("uncached collection reported leaves")
	}
}

// spec: session-startup/tampered-cached-manifests-are-rejected
func TestCachedCollectionLeavesRejectsTamperedManifest(t *testing.T) {
	cache := NewCache(t.TempDir())
	const url = "https://market.example"
	digest := cacheCollection(t, cache, url, "bundle", true, time.Now(), "alpha")
	manifestPath := cache.CollectionManifestPath(digest)
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte(strings.Replace(string(raw), "alpha", "omega", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if leaves, ok := cache.CachedCollectionLeaves(url, "src", "bundle", digest); ok {
		t.Fatalf("tampered manifest produced leaves %v", leafNames(leaves))
	}
}
