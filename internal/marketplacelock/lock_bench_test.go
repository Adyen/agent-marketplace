package marketplacelock

import (
	"fmt"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func BenchmarkProvisionalManyMarketplaceSources(b *testing.B) {
	const count = 1_000
	v4 := &config.V4Config{
		Sources:  make(map[string]config.V4Source, count),
		Requests: make([]config.V4ResourceRequest, 0, count),
	}
	for i := range count {
		name := fmt.Sprintf("source-%04d", i)
		v4.Sources[name] = config.V4Source{
			Name:         name,
			Kind:         config.V4SourceMarketplace,
			CanonicalURL: "https://marketplace.example/" + name,
		}
		v4.Requests = append(v4.Requests, config.V4ResourceRequest{
			SourceName: name,
			Registry:   "registry",
			Kind:       "skill",
			Name:       "resource",
		})
	}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := Provisional(v4, lockfile.ScopeOrganization); err != nil {
			b.Fatal(err)
		}
	}
}
