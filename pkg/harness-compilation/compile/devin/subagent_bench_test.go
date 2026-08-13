package devin

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// Subagent translation runs once per selected subagent on every sync, convert,
// and status invocation, so it sits on the same user-visible latency path as
// skill translation. Each shape gets its own sub-benchmark so a regression in
// one path cannot hide behind another: the rich path exercises model mapping,
// tool mapping, MaxNesting, and extras, while the failure paths exercise the
// fail-closed branches that allocate diagnostics instead of an artifact.

func BenchmarkTranslateSubagent(b *testing.B) {
	source := ir.SourceRef{File: "agents/demo.md", Line: 1}
	cases := []struct {
		name     string
		subagent *ir.Subagent
	}{
		{
			name:     "minimal",
			subagent: ir.NewSubagent("demo", "Demo subagent", "Body\n", "sonnet", nil, "", nil, "", nil, source),
		},
		{
			name:     "default-model",
			subagent: ir.NewSubagent("demo", "Demo subagent", "Body\n", "", nil, "", nil, "", nil, source),
		},
		{
			name: "rich",
			subagent: ir.NewSubagentWithOptions("demo", "Demo subagent", "Body\n", "opus",
				[]string{"Read", "Edit", "Grep", "Glob", "Bash"}, "high", nil, "", map[string]any{"alpha": "a", "beta": map[string]any{"nested": true}}, source,
				ir.WithSubagentMaxNesting(3),
			),
		},
		{
			name:     "unmappable-tool",
			subagent: ir.NewSubagent("demo", "Demo subagent", "Body\n", "sonnet", []string{"Read", "WebSearch"}, "", nil, "", nil, source),
		},
		{
			name:     "unsupported-restrictions",
			subagent: ir.NewSubagent("demo", "Demo subagent", "Body\n", "sonnet", nil, "", []string{"Bash"}, "worktree", nil, source),
		},
		{
			name:     "builtin-name-conflict",
			subagent: ir.NewSubagent("subagent_explore", "Demo subagent", "Body\n", "sonnet", nil, "", nil, "", nil, source),
		},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, _, err := translateSubagent(c.subagent, compile.Options{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
