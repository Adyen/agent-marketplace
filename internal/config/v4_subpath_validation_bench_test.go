package config

import (
	"fmt"
	"strings"
	"testing"
)

// Only the existing path validator is timed; no filesystem or network work.
func BenchmarkV4SubpathValidation(b *testing.B) {
	for _, tc := range []struct {
		name, value string
		valid       bool
	}{
		{"empty", "", true},
		{"single", "resources", true},
		{"nested", "resources/team/skills", true},
		{"deep", strings.Repeat("segment/", 63) + "skills", true},
		{"backslash", `resources\team\skills`, true},
		{"absolute-control", "/resources/team", false},
		{"escape-control", "../resources/team", false},
		{"dot-segment", "resources/./skills", false},
		{"repeated-slash", "resources//skills", false},
		{"trailing-slash", "resources/skills/", false},
	} {
		b.Run(tc.name, func(b *testing.B) {
			var err error
			b.ReportAllocs()
			for b.Loop() {
				err = validateV4Subpath(tc.value)
			}
			if (err == nil) != tc.valid {
				b.Fatalf("validation = %v, want success %t", err, tc.valid)
			}
		})
	}
}

func v4SubpathSourceDocument(count int, shape string) []byte {
	var data strings.Builder
	data.WriteString("apiVersion: agent-marketplace/v4\nsources:\n")
	for i := range count {
		subpath := ""
		if shape != "empty" {
			subpath = fmt.Sprintf("resources/team/source-%04d", i)
			if shape == "backslash" {
				subpath = strings.ReplaceAll(subpath, "/", "\\")
			}
		}
		fmt.Fprintf(&data, "  s%04d: {git: https://example.test/tools.git, ref: main, path: %q}\n", i, subpath)
	}
	data.WriteString("resources:\n")
	for i := range count {
		fmt.Fprintf(&data, "  s%04d: [skill/review]\n", i)
	}
	return []byte(data.String())
}

func checkV4SubpathSourceResult(b *testing.B, cfg *V4Config, count int, shape string) {
	b.Helper()
	if cfg == nil || len(cfg.Sources) != count || len(cfg.Groups) != count ||
		len(cfg.Requests) != count || len(cfg.Decisions) != count {
		b.Fatalf("source/group/request/history counts changed: %#v", cfg)
	}
	for i := range count {
		name := fmt.Sprintf("s%04d", i)
		subpath := ""
		if shape != "empty" {
			subpath = fmt.Sprintf("resources/team/source-%04d", i)
		}
		source := cfg.Sources[name]
		if source.Kind != V4SourceGit || source.Ref != "main" || source.Path != subpath ||
			cfg.Requests[i].SourceName != name || cfg.Requests[i].Locator != "skill/review" {
			b.Fatalf("source %s changed normalized path or sorted binding: %#v, %#v", name, source, cfg.Requests[i])
		}
	}
}

// Public YAML parsing and normalization of Git sources, with no acquisition,
// configuration reads, or directory-source path-metadata operations.
func BenchmarkParseV4GitSubpaths(b *testing.B) {
	for _, shape := range []string{"empty", "nested", "backslash"} {
		for _, count := range []int{1, 256, 1024} {
			b.Run(fmt.Sprintf("%s/sources=%d", shape, count), func(b *testing.B) {
				data := v4SubpathSourceDocument(count, shape)
				var cfg *V4Config
				var err error
				b.ReportAllocs()
				for b.Loop() {
					cfg, err = ParseV4(data)
				}
				if err != nil {
					b.Fatal(err)
				}
				checkV4SubpathSourceResult(b, cfg, count, shape)
			})
		}
	}
}
