package config

import (
	"strings"
	"testing"
)

func BenchmarkValidateMarketplaceEntries(b *testing.B) {
	entries := []MarketplaceEntry{{
		URL:        "https://market.example",
		InstanceID: "01JEXAMPLE",
		Resources: []MarketplaceResourcePin{{
			RegistryID: "01JSOURCE",
			Kind:       "skill",
			Name:       "review",
			Revision:   "v1.0.0",
			Digest:     "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		}},
	}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidateMarketplaceEntries(entries)
	}
}

func BenchmarkMarketplaceCatalogKey(b *testing.B) {
	entry := MarketplaceEntry{URL: "https://market.example", InstanceID: "market-instance"}
	pin := MarketplaceResourcePin{RegistryID: "registry", Kind: "mcpserver", Name: "server", Revision: "main", Digest: "sha256:" + strings.Repeat("a", 64), Acquisition: MarketplaceAcquisitionMirror}
	b.ReportAllocs()
	for b.Loop() {
		_ = MarketplaceCatalogKey(entry, pin)
	}
}

func BenchmarkValidateMarketplaceURLs(b *testing.B) {
	entries := []MarketplaceEntry{
		{URL: "https://market.example.com/skills"},
		{URL: "https://market.example.com/commands"},
	}
	patterns := []string{`market\.example\.com`}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidateMarketplaceURLs(entries, patterns)
	}
}
