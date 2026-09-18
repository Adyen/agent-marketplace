package marketplace

import (
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/marketplace"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// VerifiedMarketplaceSkill reports the digests assigned to a seeded verified
// Marketplace skill artifact so tests can reference them in assertions.
type VerifiedMarketplaceSkill struct {
	ContentDigest string
	ArchiveDigest string
}

// SeedVerifiedMarketplaceSkill writes one verified mirror skill artifact and
// its assessed revision metadata into the Marketplace cache rooted at
// marketplaceCacheRoot. The canonical artifact intentionally leaves the legacy
// manifest RegistryID and Revision fields empty, matching the real cache
// compatibility case where identity is proven by assessed revision metadata.
// It performs no network or environment access; callers supply the cache root.
func SeedVerifiedMarketplaceSkill(
	t testing.TB,
	marketplaceCacheRoot string,
	endpoint string,
	instanceID string,
	registryID string,
	name string,
) VerifiedMarketplaceSkill {
	t.Helper()

	canonical, err := hccartifact.FromIR(ir.NewSkill(name, "Title", "body", nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatalf("build skill artifact: %v", err)
	}
	canonical.Manifest.Kind = "skill"
	contentDigest := canonical.Manifest.ContentDigest
	canonical.Manifest.RegistryID = ""
	canonical.Manifest.Revision = ""
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatalf("encode skill artifact: %v", err)
	}

	coord := store.ResourceCoordinate{Marketplace: store.MarketplaceID(instanceID), Registry: store.RegistryID(registryID), Kind: "skill", Name: name}
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate:               coord,
		ContentDigest:            store.RevisionDigest(contentDigest),
		AnalyzerVersion:          "test-analyzer",
		PolicyDigest:             store.PolicyDigest("sha256:" + strings.Repeat("c", 64)),
		Decision:                 store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("d", 64)),
		AssessedAt:               time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("finalize assessment: %v", err)
	}

	cache := marketplace.NewCache(marketplaceCacheRoot)
	if _, err := cache.WriteResourceRevision(endpoint, registryID, "skill", name, contentDigest, store.AssessedResourceRevision{
		ResourceRevision: store.ResourceRevision{Coordinate: coord, ContentDigest: store.RevisionDigest(contentDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest)},
		Assessment:       assessment,
		Occurrence: store.ResourceOccurrence{
			Coordinate: coord, ContentDigest: store.RevisionDigest(contentDigest),
			Commit: strings.Repeat("a", 40), ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("b", 64)),
		},
	}); err != nil {
		t.Fatalf("write revision metadata: %v", err)
	}
	if _, err := cache.WriteArtifact(archiveDigest, raw); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
		t.Fatalf("mark artifact verified: %v", err)
	}
	return VerifiedMarketplaceSkill{ContentDigest: contentDigest, ArchiveDigest: archiveDigest}
}
