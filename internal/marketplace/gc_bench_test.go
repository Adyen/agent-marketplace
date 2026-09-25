package marketplace

import (
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func BenchmarkMarketplaceGarbageCollectReachability(b *testing.B) {
	root := b.TempDir()
	c := NewCache(root)
	// Zero-padded hex keeps every workload index a distinct digest.
	hex := func(i int) string { return fmt.Sprintf("%064x", i+1) }

	writeArtifact := func(archiveDigest string) {
		if _, err := c.WriteArtifact(archiveDigest, []byte("artifact")); err != nil {
			b.Fatalf("WriteArtifact: %v", err)
		}
	}
	writeRevision := func(endpoint, marketplaceID, name string, contentIndex, archiveIndex int) {
		contentDigest := "sha256:" + hex(contentIndex)
		archiveDigest := "sha256:" + hex(archiveIndex)
		if _, err := c.WriteResourceRevision(endpoint, "src", "skill", name, contentDigest, assessedRevision(store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: store.MarketplaceID(marketplaceID), Registry: "src", Kind: "skill", Name: name},
			ContentDigest: store.RevisionDigest(contentDigest),
			ArchiveDigest: store.ArchiveDigest(archiveDigest),
		})); err != nil {
			b.Fatalf("WriteResourceRevision: %v", err)
		}
		writeArtifact(archiveDigest)
	}

	// Two endpoints each of exact pins, locked resources, and unlocked endpoint
	// metadata, eight resources apiece.
	roots := GCRoots{}
	for e := 0; e < 2; e++ {
		entry := config.MarketplaceEntry{URL: fmt.Sprintf("https://marketplace-%d.example", e), InstanceID: fmt.Sprintf("mp%d", e)}
		locked := lockfile.MarketplaceLock{URL: fmt.Sprintf("https://locked-%d.example", e)}
		endpoint := fmt.Sprintf("https://unlocked-%d.example", e)
		for i := 0; i < 8; i++ {
			name := fmt.Sprintf("skill-%d-%d", e, i)
			writeRevision(entry.URL, entry.InstanceID, name, e*16+i, e*16+i+8)
			entry.Resources = append(entry.Resources, config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: name, Revision: "v1", Digest: "sha256:" + hex(e*16+i)})
			lockedDigest := "sha256:" + hex(64+e*8+i)
			locked.Resources = append(locked.Resources, lockfile.MarketplaceResourceLock{
				RegistryID: "src", Kind: "skill", Name: fmt.Sprintf("locked-%d-%d", e, i),
				Acquisition: lockfile.MarketplaceAcquisitionMirror, ArchiveDigest: lockfile.Digest(lockedDigest),
			})
			writeArtifact(lockedDigest)
			writeRevision(endpoint, "ump", fmt.Sprintf("unlocked-%d-%d", e, i), 96+e*8+i, 112+e*8+i)
		}
		roots.ExactPins = append(roots.ExactPins, entry)
		roots.LockedResources = append(roots.LockedResources, locked)
		roots.UnlockedEndpoints = append(roots.UnlockedEndpoints, endpoint)
	}
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(oldLogger)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.GarbageCollect(true, roots); err != nil {
			b.Fatal(err)
		}
	}
}
