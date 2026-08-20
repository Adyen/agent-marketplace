package marketplace

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
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
