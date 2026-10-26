package config

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// CPU-only directory counterpart of BenchmarkV4SparseExclusionAdapterCPU.
// Parsing/path setup is excluded; neither adapter does disk or network work.
func BenchmarkV4DirectoryRequestReuseCPU(b *testing.B) {
	for _, count := range []int{1, 256, 1024} {
		for _, percent := range []int{0, 50, 100} {
			if count == 1 && percent == 50 {
				continue
			}
			excluded := count * percent / 100
			b.Run(fmt.Sprintf("skills=%d/excluded=%d", count, excluded), func(b *testing.B) {
				data := strings.Replace(string(v4FilterSortDocument(b, count, excluded)),
					"git: https://example.test/tools.git, ref: main", "directory: ./resources", 1)
				cfg, err := ParseV4Config([]byte(data), filepath.Join(b.TempDir(), "config.yaml"), true)
				if err != nil {
					b.Fatal(err)
				}
				var out *Config
				b.ReportAllocs()
				for b.Loop() {
					out, err = cfg.ToLegacyConfig(repoLayer)
				}
				if err != nil || out == nil || len(out.Registries) != 1 {
					b.Fatalf("directory runtime adaptation failed: %v", err)
				}
				entry := out.Registries[0]
				if entry.V4Kind != V4SourceDirectory || entry.AuthoredDirectory != "./resources" ||
					len(entry.Skills.Include) != count-excluded || len(entry.Skills.Exclude) != excluded ||
					!reflect.DeepEqual(entry.Commands.Include, []string{"kept"}) {
					b.Fatal("directory identity, selection counts, or retained command changed")
				}
				for i, name := range entry.Skills.Include {
					if name != fmt.Sprintf("item-%04d", excluded+i) {
						b.Fatal("directory adaptation changed retained skill order")
					}
				}
			})
		}
	}
}

// CPU-only public adaptation of repeated fragment decisions; parsing and
// include-tree I/O are excluded, and the final exact exclusion is superseded.
func BenchmarkV4RequestReuseChurnCPU(b *testing.B) {
	for _, rounds := range []int{64, 1024} {
		b.Run(fmt.Sprintf("rounds=%d", rounds), func(b *testing.B) {
			cfg, err := ParseV4(v4FilterSortDocument(b, 1, 0))
			if err != nil {
				b.Fatal(err)
			}
			group := cfg.Groups["g"]
			request := group.Include[1]
			for range rounds {
				group.Decisions = append(group.Decisions,
					V4SelectionDecision{Action: V4SelectionExclude, Exclusion: request.Locator},
					V4SelectionDecision{Action: V4SelectionExplicit, Locator: request.Locator, Request: &request},
				)
			}
			cfg.Groups["g"] = group
			var out *Config
			b.ReportAllocs()
			for b.Loop() {
				out, err = cfg.ToLegacyConfig(repoLayer)
			}
			if err != nil || out == nil || len(out.Registries) != 1 {
				b.Fatalf("repeated-decision adaptation failed: %v", err)
			}
			entry := out.Registries[0]
			if !reflect.DeepEqual(entry.Skills.Include, []string{"item-0000"}) ||
				len(entry.Skills.Exclude) != 0 || !reflect.DeepEqual(entry.Commands.Include, []string{"kept"}) {
				b.Fatal("repeated decisions changed final selections")
			}
		})
	}
}
