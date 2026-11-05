package config

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func BenchmarkV4ExclusionKindNameCPU(b *testing.B) {
	for _, tc := range []struct {
		name, value, kind, resource string
	}{
		{"global", "*", "*", "*"},
		{"kind-wildcard", "skill/*", "skill", "*"},
		{"exact", "skill/item", "skill", "item"},
		{"marketplace-exact", "tools/skill/item", "", ""},
		{"malformed", "tools/skill/item/extra", "", ""},
		{"long-exact", "skill/" + strings.Repeat("item", 64), "skill", strings.Repeat("item", 64)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			var kind, resource string
			b.ReportAllocs()
			for b.Loop() {
				kind, resource = v4ExclusionKindName(tc.value)
			}
			if kind != tc.kind || resource != tc.resource {
				b.Fatal("exclusion kind/name parsing changed")
			}
		})
	}
}

func v4MarketplaceExclusionKindFixture(tb testing.TB, count int) *V4Config {
	tb.Helper()
	var data strings.Builder
	data.WriteString("apiVersion: agent-marketplace/v4\nsources:\n  g: {git: https://example.test/tools.git, ref: main}\n  m: {marketplace: https://market.example}\nresources:\n  g: {include: [command/kept]}\n  m:\n    exclude:\n")
	for i := range count {
		fmt.Fprintf(&data, "      - tools/skill/item-%04d\n", i)
	}
	if int64(data.Len()) > MaxEffectiveConfigSize {
		tb.Fatal("fixture exceeds the existing file limit")
	}
	cfg, err := ParseV4([]byte(data.String()))
	if err != nil {
		tb.Fatal(err)
	}
	return cfg
}

func checkV4MarketplaceExclusionKindResult(tb testing.TB, out *Config, count int) {
	tb.Helper()
	if out == nil || len(out.MarketplaceExclusions) != count || len(out.Marketplaces) != 0 ||
		len(out.Registries) != 1 || !reflect.DeepEqual(out.Registries[0].Commands.Include, []string{"kept"}) {
		tb.Fatal("exclusions, exclusion-only acquisition, or the healthy Git control changed")
	}
	for i, exclusion := range out.MarketplaceExclusions {
		if exclusion.Locator != fmt.Sprintf("tools/skill/item-%04d", i) || exclusion.URL != "https://market.example" {
			tb.Fatal("Marketplace locator, endpoint, or first-exclusion order changed")
		}
	}
}

// CPU-only public Marketplace exclusion-only adaptation. No acquisition or
// lock is required; parsing and assertions are outside the timed loop.
func BenchmarkV4MarketplaceExclusionKindCPU(b *testing.B) {
	for _, count := range []int{1, 256, 1024} {
		b.Run(fmt.Sprintf("exclusions=%d", count), func(b *testing.B) {
			cfg := v4MarketplaceExclusionKindFixture(b, count)
			var out *Config
			var err error
			b.ReportAllocs()
			for b.Loop() {
				out, err = cfg.ToLegacyConfig(userLayer)
			}
			if err != nil {
				b.Fatal(err)
			}
			checkV4MarketplaceExclusionKindResult(b, out, count)
		})
	}
}
