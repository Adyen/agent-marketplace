package config

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func v4KindWildcardDocument(tb testing.TB, count int) []byte {
	tb.Helper()
	data := v4RebuildDocument(tb, count, true)
	data = bytes.Replace(data, []byte("    exclude:\n"), []byte("      - command/kept\n    exclude:\n"), 1)
	data = bytes.Replace(data, []byte("      - '*'\n"), []byte("      - 'skill/*'\n"), 1)
	if int64(len(data)) > MaxEffectiveConfigSize {
		tb.Fatal("fixture exceeds the existing configuration file limit")
	}
	return data
}

func BenchmarkV4WildcardMatchCPU(b *testing.B) {
	longRegistry := strings.Repeat("registry-", 16)
	for _, tc := range []struct {
		name, selector, locator string
		want                    bool
	}{
		{"kind-match", "skill/*", "skill/item", true},
		{"kind-miss", "skill/*", "command/item", false},
		{"registry", "tools/*", "tools/skill/item", true},
		{"registry-kind", "tools/skill/*", "tools/skill/item", true},
		{"registry-kind-long", longRegistry + "/skill/*", longRegistry + "/skill/item", true},
		{"registry-kind-long-miss", longRegistry + "/command/*", longRegistry + "/skill/item", false},
		{"malformed-control", "tools/*", "tools/skill/item/extra", false},
		{"exact-control", "skill/item", "skill/item", true},
		{"global-control", "*", "tools/skill/item", true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			var got bool
			b.ReportAllocs()
			for b.Loop() {
				got = v4ExclusionMatches(tc.selector, tc.locator)
			}
			if got != tc.want {
				b.Fatal("wildcard match changed")
			}
		})
	}
}

// CPU-only public adaptation; parsing, disk/network, and checks are not timed.
func BenchmarkV4WildcardAdapterCPU(b *testing.B) {
	for _, count := range []int{1, 256, 1024} {
		b.Run(fmt.Sprintf("requests=%d", count), func(b *testing.B) {
			cfg, err := ParseV4(v4KindWildcardDocument(b, count))
			if err != nil {
				b.Fatal(err)
			}
			var out *Config
			b.ReportAllocs()
			for b.Loop() {
				out, err = cfg.ToLegacyConfig(repoLayer)
			}
			if err != nil || len(out.Registries) != 1 ||
				len(out.Registries[0].Skills.Include) != 0 || !out.Registries[0].Skills.ExcludeAll ||
				len(out.Registries[0].Commands.Include) != 1 || out.Registries[0].Commands.Include[0] != "kept" {
				b.Fatalf("kind-wildcard runtime adaptation changed: %v", err)
			}
		})
	}
}

// Includes public YAML parsing and normalization, but no disk/network I/O.
func BenchmarkParseV4KindWildcardCPU(b *testing.B) {
	for _, count := range []int{256, 1024} {
		b.Run(fmt.Sprintf("requests=%d", count), func(b *testing.B) {
			data := v4KindWildcardDocument(b, count)
			var cfg *V4Config
			var err error
			b.ReportAllocs()
			for b.Loop() {
				cfg, err = ParseV4(data)
			}
			if err != nil || len(cfg.Requests) != 1 || cfg.Requests[0].Locator != "command/kept" ||
				len(cfg.Decisions) != count+2 {
				b.Fatalf("kind-wildcard parsing changed: %v", err)
			}
		})
	}
}
