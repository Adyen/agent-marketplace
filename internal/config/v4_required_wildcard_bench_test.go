package config

import (
	"fmt"
	"strings"
	"testing"
)

func v4RequiredWildcardDocument(tb testing.TB, count int, required bool) []byte {
	tb.Helper()
	var text strings.Builder
	fmt.Fprintf(&text, "apiVersion: agent-marketplace/v4\nsources: {g: {marketplace: https://example.test/api}}\nresources:\n  g:\n    required: %t\n    include:\n", required)
	for i := range count {
		fmt.Fprintf(&text, "      - reg-%04d/skill/item\n", i)
	}
	text.WriteString("      - kept/command/kept\n    exclude:\n")
	for i := range count {
		fmt.Fprintf(&text, "      - reg-%04d/skill/*\n", i)
	}
	if int64(text.Len()) > MaxEffectiveConfigSize {
		tb.Fatal("fixture exceeds the existing file limit")
	}
	return []byte(text.String())
}

func checkV4RequiredWildcardResult(tb testing.TB, cfg *V4Config, count int, required bool) {
	tb.Helper()
	want := 1
	if required {
		want += count
	}
	if cfg == nil || len(cfg.Requests) != want || cfg.Requests[0].Locator != "kept/command/kept" ||
		len(cfg.Decisions) != 2*count+1 {
		tb.Fatal("protected selections, healthy command, or authored history changed")
	}
	for _, request := range cfg.Requests {
		if request.Required != required {
			tb.Fatal("required metadata changed")
		}
	}
}

// CPU-only rebuilding; fixture parsing and result checks are not timed.
func BenchmarkV4RebuildRequiredWildcardsCPU(b *testing.B) {
	for _, required := range []bool{true, false} {
		for _, count := range []int{1, 256, 1024} {
			b.Run(fmt.Sprintf("required=%t/requests=%d", required, count), func(b *testing.B) {
				cfg, err := ParseV4(v4RequiredWildcardDocument(b, count, required))
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				for b.Loop() {
					cfg.rebuildRequests()
				}
				checkV4RequiredWildcardResult(b, cfg, count, required)
			})
		}
	}
}

// Includes public parsing and normalization, but no acquisition or lock I/O.
func BenchmarkParseV4RequiredWildcardsCPU(b *testing.B) {
	for _, required := range []bool{true, false} {
		for _, count := range []int{1, 256, 1024} {
			b.Run(fmt.Sprintf("required=%t/requests=%d", required, count), func(b *testing.B) {
				data := v4RequiredWildcardDocument(b, count, required)
				var cfg *V4Config
				var err error
				b.ReportAllocs()
				for b.Loop() {
					cfg, err = ParseV4(data)
				}
				if err != nil {
					b.Fatal(err)
				}
				checkV4RequiredWildcardResult(b, cfg, count, required)
			})
		}
	}
}
