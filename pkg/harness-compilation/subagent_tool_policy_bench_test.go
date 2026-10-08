package harnesscompile

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// BenchmarkSubagentToolPolicy measures pure policy compilation, without disk
// I/O, network requests, or runtime tool discovery.
func BenchmarkSubagentToolPolicy(b *testing.B) {
	for _, target := range []Target{TargetClaude, TargetFactory, TargetCursor, TargetGemini, TargetDevin} {
		b.Run(string(target), func(b *testing.B) {
			sub := ir.NewSubagent("reader", "Reader", "Read only.\n", "sonnet", []string{"Read", "Grep", "Glob"}, "", nil, "", nil, ir.SourceRef{})
			b.ReportAllocs()
			for b.Loop() {
				result, err := Compile(sub, target)
				if err != nil || result.HasErrors() {
					b.Fatalf("Compile: %v %+v", err, result)
				}
			}
		})
	}
}

// BenchmarkNativeSubagentToolPolicy includes native ingestion and same-harness
// compilation, without disk I/O, network requests, or runtime tool discovery.
func BenchmarkNativeSubagentToolPolicy(b *testing.B) {
	for _, from := range []Harness{HarnessClaude, HarnessFactory, HarnessCursor, HarnessGemini, HarnessDevin} {
		b.Run(string(from), func(b *testing.B) {
			tools := []string{"Read", "Grep", "Glob"}
			if from == HarnessDevin {
				tools = []string{"read", "grep", "glob"}
			}
			src := nativeToolPolicyFixture(from, tools)
			b.ReportAllocs()
			for b.Loop() {
				result, err := Convert(src, from, from)
				if err != nil || result.HasErrors() {
					b.Fatalf("Convert: %v %+v", err, result)
				}
			}
		})
	}
}
