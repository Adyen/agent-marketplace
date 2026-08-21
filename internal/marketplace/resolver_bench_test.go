package marketplace

import (
	"context"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func BenchmarkValidateResourceRevision(b *testing.B) {
	digest := "sha256:" + strings.Repeat("a", 64)
	entry := config.MarketplaceEntry{InstanceID: "mp"}
	pin := config.MarketplaceResourcePin{SourceID: "src", Kind: "skill", Name: "review", Revision: digest, Digest: digest}
	rev := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(digest),
		ArchiveDigest: store.ArchiveDigest("sha256:" + strings.Repeat("b", 64)),
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := validateResourceRevision(entry, pin, rev); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkResolveCachedNoFetch(b *testing.B) {
	cache := NewCache(b.TempDir())
	resource := ir.NewSkill("review", "Review code", "Review the code carefully.", nil, nil, ir.SourceRef{})
	artifact, err := hccartifact.FromIR(resource)
	if err != nil {
		b.Fatal(err)
	}
	contentDigest := artifact.Manifest.ContentDigest
	artifact.Manifest.SourceID = "src"
	artifact.Manifest.Revision = contentDigest
	raw, archiveDigest, err := hccartifact.Encode(artifact)
	if err != nil {
		b.Fatal(err)
	}
	if _, err := cache.WriteArtifact(archiveDigest, raw); err != nil {
		b.Fatal(err)
	}
	if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
		b.Fatal(err)
	}
	entry := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"}
	pin := config.MarketplaceResourcePin{SourceID: "src", Kind: "skill", Name: "review", Revision: contentDigest, Digest: contentDigest}
	if _, err := cache.WriteResourceRevision(entry.URL, pin.SourceID, pin.Kind, pin.Name, pin.Digest, store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(contentDigest),
		ArchiveDigest: store.ArchiveDigest(archiveDigest),
	}); err != nil {
		b.Fatal(err)
	}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: entry.URL, InstanceID: entry.InstanceID, Resources: []config.MarketplaceResourcePin{pin}}}}
	r := &Resolver{Cache: cache}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolved, errs := r.Resolve(ctx, cfg, []hcc.Target{hcc.TargetClaude}, resolver.MarketplaceResolveModeCachedNoFetch)
		if len(errs) != 0 || len(resolved) != 1 {
			b.Fatalf("Resolve = %d artifacts, errors %v", len(resolved), errs)
		}
	}
}
