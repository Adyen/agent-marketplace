package main

import (
	"io"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

// BenchmarkWriteUpdateOutputOrganizationPins measures default summary formatting
// for 15 skipped organization-managed pins, with output discarded and no disk or
// network I/O in the timed loop.
func BenchmarkWriteUpdateOutputOrganizationPins(b *testing.B) {
	pins := make([]pipeline.MarketplacePinUpdate, 15)
	for i := range pins {
		pins[i] = pipeline.MarketplacePinUpdate{
			Entry:   config.MarketplaceEntry{URL: "https://market.example"},
			OldPin:  config.MarketplaceResourcePin{RegistryID: "shared", Kind: "skill", Name: "required"},
			Source:  config.Organization,
			Skipped: true,
		}
	}
	result := &pipeline.UpdateResult{UpdatedRegistries: 1, MarketplacePins: pins, SkippedMarketplacePins: len(pins)}
	b.ReportAllocs()
	for b.Loop() {
		if err := writeUpdateOutput(io.Discard, result, false, false); err != nil {
			b.Fatal(err)
		}
	}
}
