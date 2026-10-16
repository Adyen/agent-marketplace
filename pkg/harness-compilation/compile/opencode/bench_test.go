package opencode

import (
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"testing"
)

func BenchmarkTranslate(b *testing.B) {
	resources := []ir.Resource{
		ir.NewSkill("review", "Review", "Review the change\n", nil, nil, ir.SourceRef{}),
		ir.NewCommand("check", "Check", "Check $ARGUMENTS\n", nil, ir.SourceRef{}),
		ir.NewSubagent("reviewer", "Review", "Review\n", "sonnet", []string{"Read", "Bash"}, "", nil, "", nil, ir.SourceRef{}),
		ir.NewMcpServer("docs", "docs-server", []string{"--stdio"}, map[string]string{"TOKEN": "${TOKEN}"}, nil, ir.SourceRef{}),
		ir.NewHookWithMatcher("guard", "PreToolUse", "Bash", "true", "command", nil, ir.SourceRef{}),
	}
	for _, resource := range resources {
		b.Run(resource.Kind().String(), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				artifacts, diagnostics, err := (Compiler{}).Translate(resource)
				if err != nil || len(artifacts) == 0 || len(diagnostics) != 0 {
					b.Fatalf("translate: %v %v", err, diagnostics)
				}
			}
		})
	}
}
