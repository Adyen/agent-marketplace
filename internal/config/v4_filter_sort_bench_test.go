package config

import (
	"fmt"
	"strings"
	"testing"
)

func v4FilterSortDocument(tb testing.TB, count, excluded int) []byte {
	tb.Helper()
	var data strings.Builder
	data.WriteString("apiVersion: agent-marketplace/v4\nsources: {g: {git: https://example.test/tools.git, ref: main}}\nresources:\n  g:\n    include:\n      - command/kept\n")
	for i := range count {
		fmt.Fprintf(&data, "      - skill/item-%04d\n", i)
	}
	if excluded > 0 {
		data.WriteString("    exclude:\n")
		for i := range excluded {
			fmt.Fprintf(&data, "      - skill/item-%04d\n", i)
		}
	}
	if int64(data.Len()) > MaxEffectiveConfigSize {
		tb.Fatal("fixture exceeds the existing configuration file limit")
	}
	return []byte(data.String())
}

func checkV4FilterSortResult(tb testing.TB, cfg *V4Config, count, excluded int) {
	tb.Helper()
	if cfg == nil || len(cfg.Requests) != count-excluded+1 || len(cfg.Decisions) != count+excluded+1 {
		tb.Fatal("filtering changed selected request or authored decision counts")
	}
	if cfg.Requests[0].Locator != "command/kept" || cfg.Requests[0].Required {
		tb.Fatal("filtering removed the unrelated command or changed selection order")
	}
	for i, request := range cfg.Requests[1:] {
		if request.Locator != fmt.Sprintf("skill/item-%04d", excluded+i) || request.Required {
			tb.Fatalf("filtering changed retained selection %d: %#v", i, request)
		}
	}
}

// CPU-only: inputs are parsed before timing, without disk or network work.
func BenchmarkV4FilterSortRebuildCPU(b *testing.B) {
	for _, count := range []int{1, 256, 1024} {
		for _, percent := range []int{0, 50, 100} {
			if count == 1 && percent == 50 {
				continue
			}
			excluded := count * percent / 100
			b.Run(fmt.Sprintf("skills=%d/excluded=%d", count, excluded), func(b *testing.B) {
				cfg, err := ParseV4(v4FilterSortDocument(b, count, excluded))
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				for b.Loop() {
					cfg.rebuildRequests()
				}
				checkV4FilterSortResult(b, cfg, count, excluded)
			})
		}
	}
}

// CPU-only: includes complete public parsing, but no disk or network work.
func BenchmarkParseV4FilterSortCPU(b *testing.B) {
	for _, count := range []int{1, 256, 1024} {
		for _, percent := range []int{0, 50, 100} {
			if count == 1 && percent == 50 {
				continue
			}
			excluded := count * percent / 100
			b.Run(fmt.Sprintf("skills=%d/excluded=%d", count, excluded), func(b *testing.B) {
				data := v4FilterSortDocument(b, count, excluded)
				var cfg *V4Config
				var err error
				b.ReportAllocs()
				for b.Loop() {
					cfg, err = ParseV4(data)
				}
				if err != nil {
					b.Fatal(err)
				}
				checkV4FilterSortResult(b, cfg, count, excluded)
			})
		}
	}
}
