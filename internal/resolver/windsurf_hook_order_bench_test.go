package resolver

import (
	"fmt"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func BenchmarkResolveWindsurfHookSelectionOrder(b *testing.B) {
	entries := make([]catalog.Entry, 0, 100)
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("hooks-%d", i+2)
		if i == 99 {
			name = "hooks"
		}
		entries = append(entries, catalog.Entry{Kind: ir.KindHook, Name: name, SourceHarness: "windsurf"})
	}
	entry := config.RegistryEntry{Default: true}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		resolveSelectedResources(entry, entries)
	}
}
