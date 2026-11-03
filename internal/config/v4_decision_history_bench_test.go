package config

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func v4DecisionHistoryFixture(groups, count int, excludeOnly bool) *V4Config {
	cfg := &V4Config{Groups: map[string]V4ResourceGroup{}}
	for n := range groups {
		name := fmt.Sprintf("g%04d", n)
		group := V4ResourceGroup{SourceName: name, Decisions: make([]V4SelectionDecision, count)}
		if !excludeOnly {
			group.Include = make([]V4ResourceRequest, count)
		}
		for i := range count {
			locator := fmt.Sprintf("skill/item-%04d", i)
			provenance := V4Provenance{File: name + ".yaml", Line: i + 1, Precedence: n*count + i}
			decision := V4SelectionDecision{SourceName: name, Provenance: provenance}
			if excludeOnly {
				decision.Action, decision.Exclusion = V4SelectionExclude, locator
			} else {
				request := &group.Include[i]
				*request = V4ResourceRequest{
					SourceName: name, Locator: locator, Required: i%3 == 0,
					Options: V4ResourceOptions{As: fmt.Sprintf("alias-%04d", i), HasAs: true},
					Reason:  V4SelectionExplicit, Provenance: []V4Provenance{provenance},
				}
				decision.Action, decision.Locator, decision.Request = V4SelectionExplicit, locator, request
			}
			group.Decisions[i] = decision
		}
		cfg.Groups[name] = group
	}
	return cfg
}

func BenchmarkV4DecisionHistory(b *testing.B) {
	for _, tc := range []struct {
		name          string
		groups, count int
		excludeOnly   bool
	}{
		{"empty-control", 0, 0, false},
		{"single", 1, 1, false},
		{"includes-256", 1, 256, false},
		{"includes-1024", 1, 1024, false},
		{"excludes-256", 1, 256, true},
		{"excludes-1024", 1, 1024, true},
		{"groups-16", 16, 64, false},
		{"groups-1024-control", 1024, 1, false},
	} {
		b.Run(tc.name, func(b *testing.B) {
			cfg := v4DecisionHistoryFixture(tc.groups, tc.count, tc.excludeOnly)
			requests, decisions := interpretV4RequestsByLocator(cfg.Groups)
			b.ReportAllocs()
			for b.Loop() {
				cfg.rebuildRequests()
			}
			if !reflect.DeepEqual(cfg.Requests, requests) || !reflect.DeepEqual(cfg.Decisions, decisions) {
				b.Fatal("history or selected requests changed")
			}
		})
	}
}

func BenchmarkV4DecisionHistoryEffectiveControl(b *testing.B) {
	for _, count := range []int{256, 1024} {
		b.Run(fmt.Sprintf("requests=%d", count), func(b *testing.B) {
			group := v4DecisionHistoryFixture(1, count, false).Groups["g0000"]
			var requests []V4ResourceRequest
			b.ReportAllocs()
			for b.Loop() {
				requests = effectiveV4Requests(group)
			}
			if !reflect.DeepEqual(requests, group.Include) {
				b.Fatal("unaffected effective-request control changed")
			}
		})
	}
}

func v4DecisionHistoryDocument(tb testing.TB, count int, excludeOnly bool) []byte {
	tb.Helper()
	var data strings.Builder
	data.WriteString("apiVersion: agent-marketplace/v4\nsources:\n")
	if excludeOnly {
		data.WriteString("  g: {marketplace: https://market.example.test}\nresources:\n  g:\n    exclude:\n")
	} else {
		data.WriteString("  g: {git: https://example.test/tools.git, ref: main}\nresources:\n  g:\n    include:\n")
	}
	for i := range count {
		prefix := ""
		if excludeOnly {
			prefix = "tools/"
		}
		fmt.Fprintf(&data, "      - %sskill/item-%04d\n", prefix, i)
	}
	if int64(data.Len()) > MaxEffectiveConfigSize {
		tb.Fatal("fixture exceeds the existing configuration file limit")
	}
	return []byte(data.String())
}

// CPU-only parsing/normalization; no config reads, acquisition, or path-metadata I/O.
func BenchmarkParseV4DecisionHistory(b *testing.B) {
	for _, excludeOnly := range []bool{false, true} {
		for _, count := range []int{1, 256, 1024} {
			b.Run(fmt.Sprintf("exclude-only=%t/decisions=%d", excludeOnly, count), func(b *testing.B) {
				data := v4DecisionHistoryDocument(b, count, excludeOnly)
				var cfg *V4Config
				var err error
				b.ReportAllocs()
				for b.Loop() {
					cfg, err = ParseV4(data)
				}
				wantRequests := count
				if excludeOnly {
					wantRequests = 0
				}
				if err != nil || cfg == nil || len(cfg.Decisions) != count || len(cfg.Requests) != wantRequests {
					b.Fatalf("history/selection parsing changed: %v", err)
				}
			})
		}
	}
}
