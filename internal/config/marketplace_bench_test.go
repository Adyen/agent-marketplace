package config

import "testing"

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
