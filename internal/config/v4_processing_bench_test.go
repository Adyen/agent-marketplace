package config

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func v4LockChainFixture(tb testing.TB, count int) (*V4Config, lockfile.Document) {
	tb.Helper()
	digest := func(i int) lockfile.Digest { return lockfile.Digest(fmt.Sprintf("sha256:%064x", i+1)) }
	name := func(i int) string { return fmt.Sprintf("node-%04d", i) }
	var resources []lockfile.MarketplaceResourceLock
	for i := range count {
		r := v4LockResource("tools", "tools-id", "collection", name(i), string(digest(i)), string(digest(i)))
		r.CollectionManifestDigest = digest(i)
		if i+1 < count {
			r.SelectedBy = []lockfile.MarketplaceSelectionLock{{RegistryID: "tools-id", Name: name(i + 1), Revision: digest(i + 1)}}
		}
		resources = append(resources, r)
	}
	doc, err := lockfile.Normalize(lockfile.Document{
		APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo,
		Marketplaces: []lockfile.MarketplaceLock{{
			URL: "https://market.example", InstanceID: "market-id",
			RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "tools", RegistryID: "tools-id"}},
			Resources:        resources,
		}},
	})
	if err != nil {
		tb.Fatal(err)
	}
	data, err := lockfile.Encode(doc)
	if err != nil || int64(len(data)) > lockfile.MaxFileSize {
		tb.Fatalf("lock fixture: %d bytes, %v", len(data), err)
	}
	cfg, err := ParseV4([]byte(fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources: {m: {marketplace: https://market.example}}\nresources:\n  m:\n    include: [tools/collection/%s]\n", name(count-1))))
	if err != nil {
		tb.Fatal(err)
	}
	return cfg, doc
}

func v4ExactExclusionFixture(tb testing.TB, count int) *V4Config {
	tb.Helper()
	var data strings.Builder
	data.WriteString("apiVersion: agent-marketplace/v4\nsources: {g: {git: https://example.test/tools.git, ref: main}}\nresources:\n  g:\n    include:\n")
	for i := range count {
		fmt.Fprintf(&data, "      - skill/item-%04d\n", i)
	}
	data.WriteString("    exclude:\n")
	for i := range count {
		fmt.Fprintf(&data, "      - skill/item-%04d\n", i)
	}
	if int64(data.Len()) > MaxEffectiveConfigSize {
		tb.Fatal("configuration fixture exceeds existing file limit")
	}
	cfg, err := ParseV4([]byte(data.String()))
	if err != nil {
		tb.Fatal(err)
	}
	return cfg
}

// Both benchmarks time public adapters, including lock normalization for R52,
// but exclude fixture parsing/encoding and all disk/network work.
func BenchmarkV4LockChainAdaptation(b *testing.B) {
	for _, count := range []int{1, 32, 128, 512} {
		b.Run(fmt.Sprintf("resources=%d", count), func(b *testing.B) {
			cfg, doc := v4LockChainFixture(b, count)
			b.ReportAllocs()
			for b.Loop() {
				out, err := cfg.ToLegacyConfigWithLock(repoLayer, &doc)
				if err != nil || len(out.Marketplaces) != 1 || len(out.Marketplaces[0].Resources) != count {
					b.Fatalf("adaptation failed: %v", err)
				}
			}
		})
	}
}

func BenchmarkV4ExactExclusionAdaptation(b *testing.B) {
	for _, count := range []int{1, 16, 64, 128} {
		b.Run(fmt.Sprintf("exclusions=%d", count), func(b *testing.B) {
			cfg := v4ExactExclusionFixture(b, count)
			b.ReportAllocs()
			for b.Loop() {
				out, err := cfg.ToLegacyConfig(repoLayer)
				if err != nil || len(out.Registries) != 1 || len(out.Registries[0].Skills.Exclude) != count {
					b.Fatalf("adaptation failed: %v", err)
				}
			}
		})
	}
}
