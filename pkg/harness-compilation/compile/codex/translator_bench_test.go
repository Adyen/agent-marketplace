package codex

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func BenchmarkMCPTranslation(b *testing.B) {
	server := ir.NewMcpServer("docs", "${CLAUDE_PLUGIN_ROOT}/server", []string{"--stdio"}, map[string]string{"TOKEN": "${TOKEN}"}, nil, ir.SourceRef{})
	compiler := Compiler{}
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := compiler.Translate(server); err != nil {
			b.Fatal(err)
		}
	}
}
