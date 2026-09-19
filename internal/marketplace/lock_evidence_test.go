package marketplace

import (
	"context"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func evidenceEntry() config.MarketplaceEntry {
	return config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"}
}

func evidenceArtifact(t *testing.T, kind, name string) (raw []byte, archiveDigest, contentDigest string) {
	t.Helper()
	canonical, err := hccartifact.FromIR(ir.NewSkill(name, "Title", "body", nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatal(err)
	}
	canonical.Manifest.Kind = kind
	contentDigest = canonical.Manifest.ContentDigest
	canonical.Manifest.RegistryID = "src"
	canonical.Manifest.Revision = contentDigest
	raw, archiveDigest, err = hccartifact.Encode(canonical)
	if err != nil {
		t.Fatal(err)
	}
	return raw, archiveDigest, contentDigest
}

func evidenceRevision(coord store.ResourceCoordinate, contentDigest, archiveDigest string) store.AssessedResourceRevision {
	return assessedRevision(store.ResourceRevision{
		Coordinate:    coord,
		ContentDigest: store.RevisionDigest(contentDigest),
		ArchiveDigest: store.ArchiveDigest(archiveDigest),
	})
}

func TestVerifiedMarketplaceLockResourceMirror(t *testing.T) {
	cache := NewCache(t.TempDir())
	entry := evidenceEntry()
	raw, archiveDigest, contentDigest := evidenceArtifact(t, "skill", "review")
	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"}
	if _, err := cache.WriteResourceRevision(entry.URL, "src", "skill", "review", contentDigest, evidenceRevision(coord, contentDigest, archiveDigest)); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.WriteArtifact(archiveDigest, raw); err != nil {
		t.Fatal(err)
	}
	if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
		t.Fatal(err)
	}
	pin := config.MarketplaceResourcePin{
		RegistryID: "src", RegistrySlug: "tools", Kind: "skill", Name: "review",
		Revision: contentDigest, Digest: contentDigest,
		SelectedBy: config.MarketplaceSelectionsFrom([]config.MarketplaceSelection{
			{RegistryID: "cols", Name: "onboarding", Revision: "sha256:" + strings.Repeat("e", 64)},
		}),
	}
	lock, err := cache.VerifiedMarketplaceLockResource(entry, pin)
	if err != nil {
		t.Fatal(err)
	}
	if lock.RegistryName != "tools" || lock.RegistryID != "src" || lock.Kind != "skill" || lock.Name != "review" ||
		lock.Revision != contentDigest || string(lock.ContentDigest) != contentDigest ||
		string(lock.ArchiveDigest) != archiveDigest || lock.Acquisition != "mirror" ||
		lock.CollectionManifestDigest != "" || lock.SourcePath != "" {
		t.Fatalf("lock record = %#v", lock)
	}
	if len(lock.SelectedBy) != 1 || lock.SelectedBy[0].RegistryID != "cols" || lock.SelectedBy[0].Name != "onboarding" ||
		lock.SelectedBy[0].Revision.String() != "sha256:"+strings.Repeat("e", 64) {
		t.Fatalf("selected_by = %#v", lock.SelectedBy)
	}
}

func TestVerifiedMarketplaceLockResourceLegacyEmptyManifestFields(t *testing.T) {
	cache := NewCache(t.TempDir())
	entry := evidenceEntry()
	canonical, err := hccartifact.FromIR(ir.NewSkill("review", "Title", "body", nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatal(err)
	}
	canonical.Manifest.Kind = "skill"
	contentDigest := canonical.Manifest.ContentDigest
	canonical.Manifest.RegistryID = ""
	canonical.Manifest.Revision = ""
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatal(err)
	}
	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"}
	if _, err := cache.WriteResourceRevision(entry.URL, "src", "skill", "review", contentDigest, evidenceRevision(coord, contentDigest, archiveDigest)); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.WriteArtifact(archiveDigest, raw); err != nil {
		t.Fatal(err)
	}
	if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
		t.Fatal(err)
	}
	pin := config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", Revision: contentDigest, Digest: contentDigest}
	lock, err := cache.VerifiedMarketplaceLockResource(entry, pin)
	if err != nil {
		t.Fatal(err)
	}
	if lock.RegistryID != "src" || lock.Revision != contentDigest || string(lock.ArchiveDigest) != archiveDigest {
		t.Fatalf("lock record = %#v", lock)
	}
}

func TestVerifiedMarketplaceLockResourceIndexOnly(t *testing.T) {
	cache := NewCache(t.TempDir())
	entry := evidenceEntry()
	raw, archiveDigest, contentDigest := evidenceArtifact(t, "skill", "review")
	commit := strings.Repeat("a", 40)
	manifestDigest := "sha256:" + strings.Repeat("b", 64)
	identity := IndexOnlyIdentity{Marketplace: "mp", RegistryID: "src", Commit: commit, ManifestDigest: manifestDigest}
	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"}
	rev := evidenceRevision(coord, contentDigest, archiveDigest)
	rev.Occurrence = store.ResourceOccurrence{
		Coordinate: coord, ContentDigest: store.RevisionDigest(contentDigest),
		Commit: commit, ManifestDigest: store.RevisionDigest(manifestDigest), SourcePath: "skills/review",
	}
	if _, err := cache.WriteIndexOnlyResourceRevision(identity, "src", "skill", "review", contentDigest, rev); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.WriteIndexOnlyArtifact(identity, contentDigest, raw); err != nil {
		t.Fatal(err)
	}
	if err := cache.MarkIndexOnlyArtifactVerified(identity, contentDigest); err != nil {
		t.Fatal(err)
	}
	pin := config.MarketplaceResourcePin{
		RegistryID: "src", Kind: "skill", Name: "review",
		Revision: contentDigest, Digest: contentDigest,
		Acquisition:    config.MarketplaceAcquisitionIndexOnly,
		RegistryCommit: commit, RegistryManifestDigest: manifestDigest,
	}
	lock, err := cache.VerifiedMarketplaceLockResource(entry, pin)
	if err != nil {
		t.Fatal(err)
	}
	if lock.RegistryName != "src" || lock.Acquisition != "index-only" || lock.RegistryCommit != commit ||
		lock.RegistryManifestDigest.String() != manifestDigest || lock.SourcePath != "skills/review" ||
		lock.ArchiveDigest.String() != archiveDigest {
		t.Fatalf("index-only lock record = %#v", lock)
	}
}

func TestVerifiedMarketplaceLockResourceCollection(t *testing.T) {
	cache := NewCache(t.TempDir())
	entry := evidenceEntry()
	payload, digest, err := hccartifact.EncodeCollectionManifest(hccartifact.CollectionManifest{
		SchemaVersion: hccartifact.CollectionSchemaV1,
		Name:          "onboarding",
		Members: []hccartifact.CollectionMemberEntry{
			{Kind: "skill", Name: "review", RegistryID: "src", Revision: "sha256:" + strings.Repeat("a", 64)},
		},
		SupportedSchemas: []string{hccartifact.CollectionSchemaV1},
	})
	if err != nil {
		t.Fatal(err)
	}
	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "cols", Kind: "collection", Name: "onboarding"}
	if _, err := cache.WriteResourceRevision(entry.URL, "cols", "collection", "onboarding", digest, evidenceRevision(coord, digest, "")); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.WriteCollectionManifest(digest, payload); err != nil {
		t.Fatal(err)
	}
	if err := cache.MarkCollectionVerified(digest); err != nil {
		t.Fatal(err)
	}
	pin := config.MarketplaceResourcePin{RegistryID: "cols", Kind: "collection", Name: "onboarding", Revision: digest, Digest: digest}
	lock, err := cache.VerifiedMarketplaceLockResource(entry, pin)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Acquisition != "mirror" || lock.CollectionManifestDigest.String() != digest || lock.ArchiveDigest != "" {
		t.Fatalf("collection lock record = %#v", lock)
	}
}

func TestVerifiedMarketplaceLockResourceRevocationIsNotRejected(t *testing.T) {
	cache := NewCache(t.TempDir())
	entry := evidenceEntry()
	raw, archiveDigest, contentDigest := evidenceArtifact(t, "skill", "review")
	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"}
	rev := evidenceRevision(coord, contentDigest, archiveDigest)
	rev.Revocation = &store.Revocation{Kind: store.RevocationRevision, TargetID: "x", Reason: "test"}
	if _, err := cache.WriteResourceRevision(entry.URL, "src", "skill", "review", contentDigest, rev); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.WriteArtifact(archiveDigest, raw); err != nil {
		t.Fatal(err)
	}
	if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
		t.Fatal(err)
	}
	pin := config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", Revision: contentDigest, Digest: contentDigest}
	if _, err := cache.VerifiedMarketplaceLockResource(entry, pin); err != nil {
		t.Fatalf("immutable verified record rejected for revocation object: %v", err)
	}
}

func TestVerifiedMarketplaceLockResourceCanonicalizesEntryURL(t *testing.T) {
	cache := NewCache(t.TempDir())
	raw, archiveDigest, contentDigest := evidenceArtifact(t, "skill", "review")
	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"}
	// Evidence is cached under the canonical endpoint spelling.
	if _, err := cache.WriteResourceRevision("https://market.example", "src", "skill", "review", contentDigest, evidenceRevision(coord, contentDigest, archiveDigest)); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.WriteArtifact(archiveDigest, raw); err != nil {
		t.Fatal(err)
	}
	if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
		t.Fatal(err)
	}
	pin := config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", Revision: contentDigest, Digest: contentDigest}
	lock, err := cache.VerifiedMarketplaceLockResource(config.MarketplaceEntry{URL: "HTTPS://market.example/", InstanceID: "mp"}, pin)
	if err != nil {
		t.Fatalf("canonical-equivalent entry URL rejected: %v", err)
	}
	if lock.RegistryID != "src" || lock.Name != "review" {
		t.Fatalf("lock record = %#v", lock)
	}
}

func TestVerifiedMarketplaceLockResourceFailures(t *testing.T) {
	entry := evidenceEntry()
	raw, archiveDigest, contentDigest := evidenceArtifact(t, "skill", "review")
	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"}
	commit := strings.Repeat("a", 40)
	manifestDigest := "sha256:" + strings.Repeat("b", 64)
	identity := IndexOnlyIdentity{Marketplace: "mp", RegistryID: "src", Commit: commit, ManifestDigest: manifestDigest}
	mirrorPin := config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", Revision: contentDigest, Digest: contentDigest}
	indexPin := config.MarketplaceResourcePin{
		RegistryID: "src", Kind: "skill", Name: "review", Revision: contentDigest, Digest: contentDigest,
		Acquisition: config.MarketplaceAcquisitionIndexOnly, RegistryCommit: commit, RegistryManifestDigest: manifestDigest,
	}
	indexRevision := func() store.AssessedResourceRevision {
		rev := evidenceRevision(coord, contentDigest, archiveDigest)
		rev.Occurrence = store.ResourceOccurrence{
			Coordinate: coord, ContentDigest: store.RevisionDigest(contentDigest),
			Commit: commit, ManifestDigest: store.RevisionDigest(manifestDigest), SourcePath: "skills/review",
		}
		return rev
	}
	collectionPayload, collectionDigest, err := hccartifact.EncodeCollectionManifest(hccartifact.CollectionManifest{
		SchemaVersion:    hccartifact.CollectionSchemaV1,
		Name:             "onboarding",
		Members:          []hccartifact.CollectionMemberEntry{{Kind: "skill", Name: "review", RegistryID: "src", Revision: contentDigest}},
		SupportedSchemas: []string{hccartifact.CollectionSchemaV1},
	})
	if err != nil {
		t.Fatal(err)
	}
	collectionPin := config.MarketplaceResourcePin{RegistryID: "cols", Kind: "collection", Name: "onboarding", Revision: collectionDigest, Digest: collectionDigest}
	collectionCoord := store.ResourceCoordinate{Marketplace: "mp", Registry: "cols", Kind: "collection", Name: "onboarding"}

	cases := map[string]struct {
		pin   config.MarketplaceResourcePin
		stage func(t *testing.T, cache *Cache)
	}{
		"missing metadata": {mirrorPin, func(t *testing.T, cache *Cache) {}},
		"coordinate mismatch": {mirrorPin, func(t *testing.T, cache *Cache) {
			bad := coord
			bad.Name = "other"
			if _, err := cache.WriteResourceRevision(entry.URL, "src", "skill", "review", contentDigest, evidenceRevision(bad, contentDigest, archiveDigest)); err != nil {
				t.Fatal(err)
			}
		}},
		"metadata digest mismatch": {mirrorPin, func(t *testing.T, cache *Cache) {
			rev := evidenceRevision(coord, "sha256:"+strings.Repeat("9", 64), archiveDigest)
			if _, err := cache.WriteResourceRevision(entry.URL, "src", "skill", "review", contentDigest, rev); err != nil {
				t.Fatal(err)
			}
		}},
		"missing archive digest": {mirrorPin, func(t *testing.T, cache *Cache) {
			if _, err := cache.WriteResourceRevision(entry.URL, "src", "skill", "review", contentDigest, evidenceRevision(coord, contentDigest, "")); err != nil {
				t.Fatal(err)
			}
		}},
		"artifact missing": {mirrorPin, func(t *testing.T, cache *Cache) {
			if _, err := cache.WriteResourceRevision(entry.URL, "src", "skill", "review", contentDigest, evidenceRevision(coord, contentDigest, archiveDigest)); err != nil {
				t.Fatal(err)
			}
		}},
		"artifact unverified": {mirrorPin, func(t *testing.T, cache *Cache) {
			if _, err := cache.WriteResourceRevision(entry.URL, "src", "skill", "review", contentDigest, evidenceRevision(coord, contentDigest, archiveDigest)); err != nil {
				t.Fatal(err)
			}
			if _, err := cache.WriteArtifact(archiveDigest, raw); err != nil {
				t.Fatal(err)
			}
		}},
		"artifact tampered": {mirrorPin, func(t *testing.T, cache *Cache) {
			if _, err := cache.WriteResourceRevision(entry.URL, "src", "skill", "review", contentDigest, evidenceRevision(coord, contentDigest, archiveDigest)); err != nil {
				t.Fatal(err)
			}
			if _, err := cache.WriteArtifact(archiveDigest, tamperCanonicalArchiveDigest(t, raw)); err != nil {
				t.Fatal(err)
			}
			if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
				t.Fatal(err)
			}
		}},
		"artifact manifest registry id mismatch": {
			config.MarketplaceResourcePin{RegistryID: "other", Kind: "skill", Name: "review", Revision: contentDigest, Digest: contentDigest},
			func(t *testing.T, cache *Cache) {
				otherCoord := store.ResourceCoordinate{Marketplace: "mp", Registry: "other", Kind: "skill", Name: "review"}
				if _, err := cache.WriteResourceRevision(entry.URL, "other", "skill", "review", contentDigest, evidenceRevision(otherCoord, contentDigest, archiveDigest)); err != nil {
					t.Fatal(err)
				}
				if _, err := cache.WriteArtifact(archiveDigest, raw); err != nil {
					t.Fatal(err)
				}
				if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
					t.Fatal(err)
				}
			},
		},
		"artifact manifest revision mismatch": {
			config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", Revision: "rev-9", Digest: contentDigest},
			func(t *testing.T, cache *Cache) {
				if _, err := cache.WriteResourceRevision(entry.URL, "src", "skill", "review", contentDigest, evidenceRevision(coord, contentDigest, archiveDigest)); err != nil {
					t.Fatal(err)
				}
				if _, err := cache.WriteArtifact(archiveDigest, raw); err != nil {
					t.Fatal(err)
				}
				if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
					t.Fatal(err)
				}
			},
		},
		"collection manifest noncanonical bytes": {collectionPin, func(t *testing.T, cache *Cache) {
			if _, err := cache.WriteResourceRevision(entry.URL, "cols", "collection", "onboarding", collectionDigest, evidenceRevision(collectionCoord, collectionDigest, "")); err != nil {
				t.Fatal(err)
			}
			if _, err := cache.WriteCollectionManifest(collectionDigest, append(append([]byte(nil), collectionPayload...), '\n')); err != nil {
				t.Fatal(err)
			}
			if err := cache.MarkCollectionVerified(collectionDigest); err != nil {
				t.Fatal(err)
			}
		}},
		"index-only occurrence commit mismatch": {indexPin, func(t *testing.T, cache *Cache) {
			rev := indexRevision()
			rev.Occurrence.Commit = strings.Repeat("f", 40)
			if _, err := cache.WriteIndexOnlyResourceRevision(identity, "src", "skill", "review", contentDigest, rev); err != nil {
				t.Fatal(err)
			}
		}},
		"index-only occurrence registry mismatch": {indexPin, func(t *testing.T, cache *Cache) {
			rev := indexRevision()
			rev.Occurrence.Coordinate.Registry = "other"
			if _, err := cache.WriteIndexOnlyResourceRevision(identity, "src", "skill", "review", contentDigest, rev); err != nil {
				t.Fatal(err)
			}
		}},
		"index-only missing source path": {indexPin, func(t *testing.T, cache *Cache) {
			rev := indexRevision()
			rev.Occurrence.SourcePath = ""
			if _, err := cache.WriteIndexOnlyResourceRevision(identity, "src", "skill", "review", contentDigest, rev); err != nil {
				t.Fatal(err)
			}
			if _, err := cache.WriteIndexOnlyArtifact(identity, contentDigest, raw); err != nil {
				t.Fatal(err)
			}
			if err := cache.MarkIndexOnlyArtifactVerified(identity, contentDigest); err != nil {
				t.Fatal(err)
			}
		}},
		"index-only artifact unverified": {indexPin, func(t *testing.T, cache *Cache) {
			if _, err := cache.WriteIndexOnlyResourceRevision(identity, "src", "skill", "review", contentDigest, indexRevision()); err != nil {
				t.Fatal(err)
			}
			if _, err := cache.WriteIndexOnlyArtifact(identity, contentDigest, raw); err != nil {
				t.Fatal(err)
			}
		}},
		"index-only collection rejected": {
			config.MarketplaceResourcePin{
				RegistryID: "cols", Kind: "collection", Name: "onboarding", Revision: collectionDigest, Digest: collectionDigest,
				Acquisition: config.MarketplaceAcquisitionIndexOnly, RegistryCommit: commit, RegistryManifestDigest: manifestDigest,
			},
			func(t *testing.T, cache *Cache) {},
		},
		"collection manifest missing": {collectionPin, func(t *testing.T, cache *Cache) {
			if _, err := cache.WriteResourceRevision(entry.URL, "cols", "collection", "onboarding", collectionDigest, evidenceRevision(collectionCoord, collectionDigest, "")); err != nil {
				t.Fatal(err)
			}
		}},
		"collection manifest unverified": {collectionPin, func(t *testing.T, cache *Cache) {
			if _, err := cache.WriteResourceRevision(entry.URL, "cols", "collection", "onboarding", collectionDigest, evidenceRevision(collectionCoord, collectionDigest, "")); err != nil {
				t.Fatal(err)
			}
			if _, err := cache.WriteCollectionManifest(collectionDigest, collectionPayload); err != nil {
				t.Fatal(err)
			}
		}},
		"collection name mismatch": {collectionPin, func(t *testing.T, cache *Cache) {
			other, otherDigest, err := hccartifact.EncodeCollectionManifest(hccartifact.CollectionManifest{
				SchemaVersion:    hccartifact.CollectionSchemaV1,
				Name:             "other",
				Members:          []hccartifact.CollectionMemberEntry{{Kind: "skill", Name: "review", RegistryID: "src", Revision: contentDigest}},
				SupportedSchemas: []string{hccartifact.CollectionSchemaV1},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := cache.WriteResourceRevision(entry.URL, "cols", "collection", "onboarding", collectionDigest, evidenceRevision(collectionCoord, collectionDigest, "")); err != nil {
				t.Fatal(err)
			}
			// The cache key is the pin digest; store a mismatched manifest there
			// by writing the other payload under the expected digest's sibling
			// through WriteCollectionManifest of its own digest is impossible,
			// so verify the decode boundary rejects name mismatch via a tampered
			// write under the pin digest path.
			_ = otherDigest
			if _, err := cache.WriteCollectionManifest(collectionDigest, other); err != nil {
				t.Fatal(err)
			}
			if err := cache.MarkCollectionVerified(collectionDigest); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cache := NewCache(t.TempDir())
			tc.stage(t, cache)
			if _, err := cache.VerifiedMarketplaceLockResource(entry, tc.pin); err == nil {
				t.Fatal("expected evidence failure")
			}
		})
	}
}

func TestVerifiedMarketplaceLockResourceRejectsMissingInstance(t *testing.T) {
	cache := NewCache(t.TempDir())
	entry := evidenceEntry()
	raw, archiveDigest, contentDigest := evidenceArtifact(t, "skill", "review")
	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"}
	if _, err := cache.WriteResourceRevision(entry.URL, "src", "skill", "review", contentDigest, evidenceRevision(coord, contentDigest, archiveDigest)); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.WriteArtifact(archiveDigest, raw); err != nil {
		t.Fatal(err)
	}
	if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
		t.Fatal(err)
	}
	pin := config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", Revision: contentDigest, Digest: contentDigest}
	if _, err := cache.VerifiedMarketplaceLockResource(config.MarketplaceEntry{URL: entry.URL}, pin); err == nil || !strings.Contains(err.Error(), "instance_id") {
		t.Fatalf("missing instance ID: err = %v", err)
	}
	if _, err := cache.VerifiedMarketplaceLockResource(config.MarketplaceEntry{URL: entry.URL, InstanceID: "bad/id"}, pin); err == nil || !strings.Contains(err.Error(), "instance_id") {
		t.Fatalf("unsafe instance ID: err = %v", err)
	}
}

func TestValidateResourceRevisionLockFields(t *testing.T) {
	entry := evidenceEntry()
	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"}
	rev := evidenceRevision(coord, "sha256:"+strings.Repeat("a", 64), "sha256:"+strings.Repeat("b", 64))
	rev.Occurrence.SourcePath = "skills/review"
	pin := config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", Revision: "sha256:" + strings.Repeat("a", 64), Digest: "sha256:" + strings.Repeat("a", 64)}
	if err := validateResourceRevision(entry, pin, rev); err != nil {
		t.Fatalf("blank lock fields changed legacy validation: %v", err)
	}
	pin.LockArchiveDigest = "sha256:" + strings.Repeat("b", 64)
	if err := validateResourceRevision(entry, pin, rev); err != nil {
		t.Fatalf("matching lock archive digest rejected: %v", err)
	}
	pin.LockArchiveDigest = "sha256:" + strings.Repeat("c", 64)
	if err := validateResourceRevision(entry, pin, rev); err == nil {
		t.Fatal("lock archive digest mismatch accepted")
	}
	pin.LockArchiveDigest = ""
	pin.LockSourcePath = "skills/review"
	if err := validateResourceRevision(entry, pin, rev); err != nil {
		t.Fatalf("matching lock source path rejected: %v", err)
	}
	pin.LockSourcePath = "skills/other"
	if err := validateResourceRevision(entry, pin, rev); err == nil {
		t.Fatal("lock source path mismatch accepted")
	}
}

func TestCollectionManifestLockDigestBoundary(t *testing.T) {
	cache := NewCache(t.TempDir())
	payload, digest, err := hccartifact.EncodeCollectionManifest(hccartifact.CollectionManifest{
		SchemaVersion:    hccartifact.CollectionSchemaV1,
		Name:             "onboarding",
		Members:          []hccartifact.CollectionMemberEntry{{Kind: "skill", Name: "review", RegistryID: "src", Revision: "sha256:" + strings.Repeat("a", 64)}},
		SupportedSchemas: []string{hccartifact.CollectionSchemaV1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cache.WriteCollectionManifest(digest, payload); err != nil {
		t.Fatal(err)
	}
	if err := cache.MarkCollectionVerified(digest); err != nil {
		t.Fatal(err)
	}
	r := &Resolver{}
	pin := config.MarketplaceResourcePin{Kind: "collection", Name: "onboarding", Digest: digest}
	if _, _, err := r.collectionManifest(context.Background(), nil, cache, digest, resolver.MarketplaceResolveModeCachedNoFetch, pin); err != nil {
		t.Fatalf("blank lock digest rejected: %v", err)
	}
	pin.LockCollectionManifestDigest = digest
	if _, _, err := r.collectionManifest(context.Background(), nil, cache, digest, resolver.MarketplaceResolveModeCachedNoFetch, pin); err != nil {
		t.Fatalf("matching lock digest rejected: %v", err)
	}
	pin.LockCollectionManifestDigest = "sha256:" + strings.Repeat("9", 64)
	if _, _, err := r.collectionManifest(context.Background(), nil, cache, digest, resolver.MarketplaceResolveModeCachedNoFetch, pin); err == nil {
		t.Fatal("lock collection manifest digest mismatch accepted")
	}
}
