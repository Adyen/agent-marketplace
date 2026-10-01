package harnesscompile

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func BenchmarkCompileWindsurfUnicodeScalar(b *testing.B) {
	resource := ir.NewCommand("safe-workflow", "literal\u2028auto_execution_mode:\u2028 always", "inert workflow body", nil, ir.SourceRef{})
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Compile(resource, TargetWindsurf); err != nil {
			b.Fatal(err)
		}
	}
}
