package marketplace

import (
	"bytes"
	"cmp"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

// CachedCollectionLeaves returns the leaf resources of a cached, locally
// verified collection manifest for display before acquisition. It prefers
// revision and otherwise uses the most recently cached revision. It never
// fetches, and the result authorizes nothing; a missing or invalid cache entry
// reports false.
func (c *Cache) CachedCollectionLeaves(baseURL, registryID, name, revision string) ([]hccartifact.CollectionMemberEntry, bool) {
	for _, candidate := range c.cachedCollectionRevisions(baseURL, registryID, name, revision) {
		leaves, err := c.verifiedCollectionLeaves(baseURL, registryID, name, candidate)
		if err == nil {
			return leaves, true
		}
		slog.Debug("cached collection manifest unusable", "registry", registryID, "collection", name, "revision", candidate, "error", err)
	}
	return nil, false
}

// cachedCollectionRevisions lists revision first, then every cached revision
// newest first by the time its metadata was last written.
func (c *Cache) cachedCollectionRevisions(baseURL, registryID, name, revision string) []string {
	dir := filepath.Dir(c.MetadataPath(baseURL, registryID, "collection", name, revision))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{revision}
	}
	type cached struct {
		revision string
		modified int64
	}
	var found []cached
	for _, entry := range entries {
		file, ok := strings.CutSuffix(entry.Name(), ".json")
		if !ok || !strings.HasPrefix(file, "sha256-") || !entry.Type().IsRegular() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		found = append(found, cached{revision: strings.Replace(file, "-", ":", 1), modified: info.ModTime().UnixNano()})
	}
	slices.SortStableFunc(found, func(a, b cached) int { return cmp.Compare(b.modified, a.modified) })
	revisions := []string{revision}
	for _, candidate := range found {
		if candidate.revision != revision {
			revisions = append(revisions, candidate.revision)
		}
	}
	return revisions
}

func (c *Cache) verifiedCollectionLeaves(baseURL, registryID, name, revision string) ([]hccartifact.CollectionMemberEntry, error) {
	rev, found, err := c.ReadResourceRevision(baseURL, registryID, "collection", name, revision)
	if err != nil || !found {
		return nil, errOrMissing(err, "revision metadata")
	}
	digest := string(rev.ContentDigest)
	raw, verified, err := c.ReadCollectionManifest(digest)
	if err != nil || raw == nil || !verified {
		return nil, errOrMissing(err, "verified collection manifest")
	}
	manifest, payload, err := hccartifact.DecodeCollectionManifest(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	if err := hccartifact.VerifyCollectionManifest(manifest, payload); err != nil {
		return nil, err
	}
	if manifest.ContentDigest != digest || manifest.Name != name {
		return nil, errOrMissing(nil, "matching collection manifest")
	}
	leaves := manifest.Leaves
	if len(leaves) == 0 {
		leaves = manifest.Members
	}
	return slices.DeleteFunc(slices.Clone(leaves), func(leaf hccartifact.CollectionMemberEntry) bool { return leaf.Kind == "collection" }), nil
}

func errOrMissing(err error, what string) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("%s is not cached", what)
}
