package materialize

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkCodexMCPSections(b *testing.B) {
	var source strings.Builder
	source.WriteString("model = 'gpt-5.6-terra'\n")
	for i := range 20 {
		fmt.Fprintf(&source, "\n[mcp_servers.\"server-%02d\"]\ncommand = 'npx'\nargs = ['server-%02d']\n", i, i)
	}
	raw := []byte(source.String())
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := codexMCPSections(raw); err != nil {
			b.Fatal(err)
		}
	}
}
