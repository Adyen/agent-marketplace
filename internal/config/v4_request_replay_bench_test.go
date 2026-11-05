package config

import (
	"fmt"
	"reflect"
	"testing"
)

// CPU-only: exercise include-only fallbacks, excluding parsing and I/O.
func BenchmarkV4RequestFallbackCPU(b *testing.B) {
	for _, count := range []int{1, 256, 1024} {
		b.Run(fmt.Sprintf("skills=%d", count), func(b *testing.B) {
			cfg, err := ParseV4(v4FilterSortDocument(b, count, 0))
			if err != nil {
				b.Fatal(err)
			}
			want := append([]V4ResourceRequest(nil), cfg.Requests...)
			group := cfg.Groups["g"]
			group.Decisions = nil
			cfg.Groups["g"] = group
			b.ReportAllocs()
			for b.Loop() {
				cfg.rebuildRequests()
			}
			if !reflect.DeepEqual(cfg.Requests, want) || len(cfg.Decisions) != 0 {
				b.Fatal("fallback replay changed request values, order, or decisions")
			}
		})
	}
}
