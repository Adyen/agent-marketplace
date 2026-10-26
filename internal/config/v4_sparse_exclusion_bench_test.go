package config

import (
	"fmt"
	"reflect"
	"testing"
)

// CPU-only: parse the authored fixture before timing; no disk or network work.
func BenchmarkV4SparseExclusionEffectiveCPU(b *testing.B) {
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
				checkV4FilterSortResult(b, cfg, count, excluded)
				group := cfg.Groups["g"]
				var requests []V4ResourceRequest
				b.ReportAllocs()
				for b.Loop() {
					requests = effectiveV4Requests(group)
				}
				if !reflect.DeepEqual(requests, cfg.Requests) {
					b.Fatal("effective selections differ from the parsed request stream")
				}
			})
		}
	}
}

// CPU-only: exercise the complete runtime adapter, excluding parsing and I/O.
func BenchmarkV4SparseExclusionAdapterCPU(b *testing.B) {
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
				var out *Config
				b.ReportAllocs()
				for b.Loop() {
					out, err = cfg.ToLegacyConfig(repoLayer)
				}
				if err != nil || out == nil || len(out.Registries) != 1 {
					b.Fatalf("runtime adaptation changed: %v", err)
				}
				entry := out.Registries[0]
				if len(entry.Skills.Include) != count-excluded || len(entry.Skills.Exclude) != excluded ||
					!reflect.DeepEqual(entry.Commands.Include, []string{"kept"}) {
					b.Fatal("runtime adaptation changed selection counts or the retained command")
				}
				for i, name := range entry.Skills.Include {
					if name != fmt.Sprintf("item-%04d", excluded+i) {
						b.Fatal("runtime adaptation changed retained skill order")
					}
				}
			})
		}
	}
}

// CPU-only control for repeated exclusion/re-inclusion in an expanded decision
// stream; this does not measure parsing or include-tree disk reads.
func BenchmarkV4SparseExclusionChurnCPU(b *testing.B) {
	for _, rounds := range []int{64, 1024} {
		for _, interpreter := range []string{"rebuild", "effective"} {
			b.Run(fmt.Sprintf("%s/rounds=%d", interpreter, rounds), func(b *testing.B) {
				cfg, err := ParseV4(v4FilterSortDocument(b, 1, 0))
				if err != nil {
					b.Fatal(err)
				}
				want := append([]V4ResourceRequest(nil), cfg.Requests...)
				group := cfg.Groups["g"]
				request := group.Include[1]
				for range rounds {
					group.Decisions = append(group.Decisions,
						V4SelectionDecision{Action: V4SelectionExclude, Exclusion: request.Locator},
						V4SelectionDecision{Action: V4SelectionExplicit, Locator: request.Locator, Request: &request},
					)
				}
				cfg.Groups["g"] = group
				var requests []V4ResourceRequest
				b.ReportAllocs()
				if interpreter == "rebuild" {
					for b.Loop() {
						cfg.rebuildRequests()
					}
					requests = cfg.Requests
				} else {
					for b.Loop() {
						requests = effectiveV4Requests(group)
					}
				}
				if !reflect.DeepEqual(requests, want) {
					b.Fatal("repeated exclusions and re-inclusions changed the final requests")
				}
			})
		}
	}
}
