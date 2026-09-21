package pipeline

import (
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func BenchmarkFilterInstallWarnings(b *testing.B) {
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(previous) })
	for _, mixed := range []bool{false, true} {
		name := "requirements"
		if mixed {
			name = "mixed"
		}
		b.Run(name, func(b *testing.B) {
			var warnings []error
			var installed []resourceIdentity
			for i := range 100 {
				resource := fmt.Sprintf("resource-%03d", i)
				warnings = append(warnings, &resolver.UnmetRequirementWarning{ResourceKind: ir.KindSkill, ResourceName: resource, Requirement: catalog.Requirement{Kind: ir.KindMcpServer, Name: "dependency"}})
				if mixed {
					warnings = append(warnings, &resolver.NameCollisionWarning{Kind: ir.KindSkill, Name: resource, Winner: "winner", RunnersUp: []string{"other"}})
				}
				if i < 10 {
					installed = append(installed, resourceIdentity{kind: ir.KindSkill, name: resource})
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if result := filterInstallWarnings(warnings, installed); len(result) < 10 {
					b.Fatalf("lost selected-resource warnings: %d", len(result))
				}
			}
		})
	}
}
