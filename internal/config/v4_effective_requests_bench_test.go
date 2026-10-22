package config

import (
	"bytes"
	"fmt"
	"testing"
)

func v4EffectiveRequestFixture(tb testing.TB, count int) *V4Config {
	tb.Helper()
	data := v4RebuildDocument(tb, count, false)
	data = bytes.Replace(data, []byte("    exclude:\n"), []byte("      - command/kept\n    exclude:\n"), 1)
	if int64(len(data)) > MaxEffectiveConfigSize {
		tb.Fatal("fixture exceeds the existing configuration file limit")
	}
	cfg, err := ParseV4(data)
	if err != nil {
		tb.Fatal(err)
	}
	return cfg
}

// CPU-only: fixture parsing and all disk/network work are outside the loops.
func BenchmarkV4EffectiveRequestsCPU(b *testing.B) {
	for _, count := range []int{1, 32, 256, 1024} {
		b.Run(fmt.Sprintf("exact/exclusions=%d", count), func(b *testing.B) {
			group := v4EffectiveRequestFixture(b, count).Groups["g"]
			var requests []V4ResourceRequest
			b.ReportAllocs()
			for b.Loop() {
				requests = effectiveV4Requests(group)
			}
			if len(requests) != 1 || requests[0].Locator != "command/kept" {
				b.Fatal("effective request exclusions changed")
			}
		})
	}
	b.Run("wildcard/requests=1024", func(b *testing.B) {
		cfg, err := ParseV4(v4RebuildDocument(b, 1024, true))
		if err != nil {
			b.Fatal(err)
		}
		group := cfg.Groups["g"]
		var requests []V4ResourceRequest
		b.ReportAllocs()
		for b.Loop() {
			requests = effectiveV4Requests(group)
		}
		if requests == nil || len(requests) != 0 {
			b.Fatal("wildcard request exclusions changed")
		}
	})
}

func BenchmarkV4EffectiveRequestsAdapterCPU(b *testing.B) {
	for _, count := range []int{1, 32, 256, 1024} {
		b.Run(fmt.Sprintf("exact/exclusions=%d", count), func(b *testing.B) {
			cfg := v4EffectiveRequestFixture(b, count)
			var out *Config
			var err error
			b.ReportAllocs()
			for b.Loop() {
				out, err = cfg.ToLegacyConfig(repoLayer)
			}
			if err != nil || len(out.Registries) != 1 || len(out.Registries[0].Skills.Include) != 0 || len(out.Registries[0].Skills.Exclude) != count ||
				len(out.Registries[0].Commands.Include) != 1 || out.Registries[0].Commands.Include[0] != "kept" {
				b.Fatalf("effective request adaptation changed: %v", err)
			}
		})
	}
	b.Run("wildcard/requests=1024", func(b *testing.B) {
		cfg, err := ParseV4(v4RebuildDocument(b, 1024, true))
		if err != nil {
			b.Fatal(err)
		}
		var out *Config
		b.ReportAllocs()
		for b.Loop() {
			out, err = cfg.ToLegacyConfig(repoLayer)
		}
		if err != nil || len(out.Registries) != 1 || len(out.Registries[0].Skills.Include) != 0 || !out.Registries[0].ExcludeAll {
			b.Fatalf("wildcard request adaptation changed: %v", err)
		}
	})
}
