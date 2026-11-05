package config

import (
	"fmt"
	"strings"
	"testing"
)

// Only validation is timed; no parsing, acquisition, filesystem, or network.
func BenchmarkV4WildcardValidation(b *testing.B) {
	for _, tc := range []struct {
		name      string
		value     string
		kind      V4SourceKind
		generalOK bool
		sourceOK  bool
	}{
		{"all", "*", V4SourceMarketplace, true, true},
		{"kind", "skill/*", V4SourceGit, true, true},
		{"registry", "tools/*", V4SourceMarketplace, true, true},
		{"qualified", "tools/skill/*", V4SourceMarketplace, true, true},
		{"long-registry", strings.Repeat("registry", 80) + "/skill/*", V4SourceMarketplace, true, true},
		{"extra-slash", "tools/skill/extra/*", V4SourceMarketplace, false, false},
		{"invalid-kind", "tools/not-kind/*", V4SourceMarketplace, false, false},
		{"empty-base", "/*", V4SourceMarketplace, true, false},
		{"exact-control", "tools/skill/review", V4SourceMarketplace, true, true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.Run("general", func(b *testing.B) {
				var err error
				b.ReportAllocs()
				for b.Loop() {
					err = validateV4Exclusion(tc.value, "g")
				}
				if (err == nil) != tc.generalOK {
					b.Fatalf("validation = %v, want success %t", err, tc.generalOK)
				}
			})
			b.Run("source", func(b *testing.B) {
				var err error
				b.ReportAllocs()
				for b.Loop() {
					err = validateV4ExclusionForSource(tc.value, tc.kind)
				}
				if (err == nil) != tc.sourceOK {
					b.Fatalf("validation = %v, want success %t", err, tc.sourceOK)
				}
			})
		})
	}
}

// Public Marketplace parsing/normalization, including required protection and
// authored history. No acquisition, configuration reads, or path-metadata I/O.
func BenchmarkParseV4RegistryWildcardValidation(b *testing.B) {
	for _, required := range []bool{true, false} {
		for _, count := range []int{1, 256, 1024} {
			b.Run(fmt.Sprintf("required=%t/requests=%d", required, count), func(b *testing.B) {
				data := []byte(strings.ReplaceAll(string(v4RequiredWildcardDocument(b, count, required)), "/skill/*", "/*"))
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
