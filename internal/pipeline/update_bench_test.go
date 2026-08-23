package pipeline

import (
	"context"
	"strconv"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func BenchmarkMarketplaceUpdatePlanningSkippedPins(b *testing.B) {
	pins := make([]config.MarketplaceResourcePin, 64)
	for i := range pins {
		pins[i] = config.MarketplaceResourcePin{
			SourceID: "source-" + strconv.Itoa(i), Kind: "skill", Name: "skill-" + strconv.Itoa(i),
			Revision: "revision", Digest: "sha256:" + benchmarkDigest,
			Acquisition: config.MarketplaceAcquisitionIndexOnly,
		}
	}
	ec := &config.EffectiveConfig{
		Merged:          &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: "https://market.example", InstanceID: "mp", Source: config.Organization, Resources: pins}}},
		TargetHarnesses: []string{"factory"},
	}
	opts := UpdateOptions{RepoRoot: b.TempDir()}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := planMarketplaceUpdates(context.Background(), opts, ec); err != nil {
			b.Fatal(err)
		}
	}
}

const benchmarkDigest = "0000000000000000000000000000000000000000000000000000000000000000"
