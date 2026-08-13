package cache

import (
	"fmt"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// BenchmarkActiveRegistryHashes measures the cost of building the active-hash
// set consumed by GarbageCollect. GC runs every time a user invokes the CLI's
// cache-cleaning path; the function is linear in total registry count across
// layers but allocation-heavy because of the per-URL SHA-256.
func BenchmarkActiveRegistryHashes(b *testing.B) {
	const registriesPerLayer = 16

	makeConfig := func(prefix string) *config.Config {
		out := &config.Config{}
		for i := 0; i < registriesPerLayer; i++ {
			out.Registries = append(out.Registries, config.RegistryEntry{
				URL: fmt.Sprintf("git@example.com:%s-%d.git", prefix, i),
				Ref: "main",
			})
		}
		// Sprinkle some non-git entries that must be skipped.
		out.Registries = append(out.Registries,
			config.RegistryEntry{URL: "/local/skills"},
			config.RegistryEntry{URL: "   "},
		)
		return out
	}

	cfgs := []*config.Config{
		makeConfig("org"),
		makeConfig("user"),
		makeConfig("repo"),
		nil, // exercise the nil-skip branch
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = activeRegistryHashes(cfgs...)
	}
}
