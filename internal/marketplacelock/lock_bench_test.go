package marketplacelock

import (
	"fmt"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func BenchmarkReachableResourcesWithSharedMembers(b *testing.B) {
	const count = 500
	collectionDigest := lockfile.Digest("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	v4 := &config.V4Config{Sources: map[string]config.V4Source{"hosted": {Kind: config.V4SourceMarketplace}}, Requests: []config.V4ResourceRequest{{SourceName: "hosted", Registry: "tools", Kind: "collection", Name: "shared"}}}
	records := make([]lockfile.MarketplaceResourceLock, 0, count+2)
	records = append(records, lockfile.MarketplaceResourceLock{RegistryName: "tools", RegistryID: "registry-id", Kind: "collection", Name: "shared", ContentDigest: collectionDigest})
	for i := range count {
		records = append(records, lockfile.MarketplaceResourceLock{RegistryName: "tools", RegistryID: "registry-id", Kind: "skill", Name: fmt.Sprintf("member-%04d", i), SelectedBy: []lockfile.MarketplaceSelectionLock{{RegistryID: "registry-id", Name: "shared", Revision: collectionDigest}}})
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := reachableResources(v4, "hosted", records); err != nil {
			b.Fatal(err)
		}
	}
}
