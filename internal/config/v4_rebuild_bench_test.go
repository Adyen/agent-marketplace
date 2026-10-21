package config

import (
	"fmt"
	"strings"
	"testing"
)

func v4RebuildDocument(tb testing.TB, count int, wildcard bool) []byte {
	tb.Helper()
	var data strings.Builder
	data.WriteString("apiVersion: agent-marketplace/v4\nsources: {g: {git: https://example.test/tools.git, ref: main}}\nresources:\n  g:\n    include:\n")
	for i := range count {
		fmt.Fprintf(&data, "      - skill/item-%04d\n", i)
	}
	data.WriteString("    exclude:\n")
	if wildcard {
		data.WriteString("      - '*'\n")
	} else {
		for i := range count {
			fmt.Fprintf(&data, "      - skill/item-%04d\n", i)
		}
	}
	if int64(data.Len()) > MaxEffectiveConfigSize {
		tb.Fatal("fixture exceeds the existing configuration file limit")
	}
	return []byte(data.String())
}

// CPU-only: fixtures exclude disk/network work; only ParseV4 times YAML parsing.
func BenchmarkV4RebuildExactExclusionsCPU(b *testing.B) {
	for _, count := range []int{1, 32, 256, 1024} {
		b.Run(fmt.Sprintf("requests=%d", count), func(b *testing.B) {
			cfg, err := ParseV4(v4RebuildDocument(b, count, false))
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				cfg.rebuildRequests()
			}
			if len(cfg.Requests) != 0 || len(cfg.Decisions) != 2*count {
				b.Fatal("request rebuilding changed exact exclusions")
			}
		})
	}
}

func BenchmarkParseV4ExclusionsCPU(b *testing.B) {
	for _, count := range []int{1, 32, 256, 1024} {
		b.Run(fmt.Sprintf("exact/requests=%d", count), func(b *testing.B) {
			data := v4RebuildDocument(b, count, false)
			var cfg *V4Config
			var err error
			b.ReportAllocs()
			for b.Loop() {
				cfg, err = ParseV4(data)
			}
			if err != nil || len(cfg.Requests) != 0 || len(cfg.Decisions) != 2*count {
				b.Fatalf("exact-exclusion parsing changed: %v", err)
			}
		})
	}
	b.Run("wildcard/requests=1024", func(b *testing.B) {
		data := v4RebuildDocument(b, 1024, true)
		var cfg *V4Config
		var err error
		b.ReportAllocs()
		for b.Loop() {
			cfg, err = ParseV4(data)
		}
		if err != nil || len(cfg.Requests) != 0 || len(cfg.Decisions) != 1025 {
			b.Fatalf("wildcard-exclusion parsing changed: %v", err)
		}
	})
}
