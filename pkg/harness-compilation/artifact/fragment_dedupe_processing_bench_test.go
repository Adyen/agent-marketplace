package artifact_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/devin"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func compiledHookFragments(tb testing.TB, count int, duplicates bool) []artifact.Artifact {
	tb.Helper()
	fragments := make([]artifact.Artifact, 0, count)
	for i := 0; i < count; i++ {
		command := fmt.Sprintf("echo guard-%d", i)
		if duplicates {
			command = "echo guard"
		}
		hook := ir.NewHookWithMatcher(fmt.Sprintf("guard-%d", i), "PreToolUse", "Read", command, "command", nil, ir.SourceRef{File: "hooks/hooks.json", Line: 1})
		result, err := compile.Compile(hook, compile.TargetDevin)
		if err != nil || len(result.Artifacts) != 1 || len(result.Diagnostics) != 0 || !result.Artifacts[0].DedupeFragmentEntries {
			tb.Fatalf("compile hook: %v, result=%+v", err, result)
		}
		fragments = append(fragments, result.Artifacts[0])
	}
	return fragments
}

// Measure the public merger on actual compiler outputs. Compilation, fixture
// creation, validation of expected output, disk and network are excluded.
func BenchmarkFragmentDedupeCompiledHooks(b *testing.B) {
	for _, tc := range []struct {
		name       string
		count      int
		duplicates bool
		dedupe     bool
	}{
		{"distinct-1", 1, false, true},
		{"distinct-16", 16, false, true},
		{"distinct-128", 128, false, true},
		{"distinct-512", 512, false, true},
		{"duplicate-16", 16, true, true},
		{"duplicate-128", 128, true, true},
		{"duplicate-512", 512, true, true},
		{"no-dedupe-control-128", 128, true, false},
	} {
		b.Run(tc.name, func(b *testing.B) {
			fragments := compiledHookFragments(b, tc.count, tc.duplicates)
			for i := range fragments {
				fragments[i].DedupeFragmentEntries = tc.dedupe
			}
			result, err := artifact.MergeFragments(fragments)
			if err != nil || len(result) != 1 {
				b.Fatalf("merge fragments: %v, result=%+v", err, result)
			}
			var doc map[string][]any
			if err := json.Unmarshal(result[0].Bytes, &doc); err != nil {
				b.Fatal(err)
			}
			want := tc.count
			if tc.duplicates && tc.dedupe {
				want = 1
			}
			if len(doc["PreToolUse"]) != want {
				b.Fatalf("hook count = %d, want %d", len(doc["PreToolUse"]), want)
			}
			b.ReportAllocs()
			for b.Loop() {
				merged, err := artifact.MergeFragments(fragments)
				if err != nil || len(merged) != 1 {
					b.Fatalf("merge fragments: %v, result=%+v", err, merged)
				}
			}
		})
	}
}
