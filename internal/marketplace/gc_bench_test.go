package marketplace

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func BenchmarkMarketplaceGarbageCollectReachability(b *testing.B) {
	root := b.TempDir()
	c := NewCache(root)
	contentDigest := "sha256:" + strings.Repeat("1", 64)
	archiveDigest := "sha256:" + strings.Repeat("2", 64)
	pin := config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", Revision: "v1", Digest: contentDigest}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{
		URL: "https://marketplace.example", InstanceID: "mp", Resources: []config.MarketplaceResourcePin{pin},
	}}}
	if _, err := c.WriteResourceRevision(cfg.Marketplaces[0].URL, pin.RegistryID, pin.Kind, pin.Name, pin.Digest, assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(contentDigest),
		ArchiveDigest: store.ArchiveDigest(archiveDigest),
	})); err != nil {
		b.Fatalf("WriteResourceRevision: %v", err)
	}
	if _, err := c.WriteArtifact(archiveDigest, []byte("artifact")); err != nil {
		b.Fatalf("WriteArtifact: %v", err)
	}
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(oldLogger)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.GarbageCollect(true, cfg); err != nil {
			b.Fatal(err)
		}
	}
}
