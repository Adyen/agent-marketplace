package resolver

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func BenchmarkRequirementRemediation(b *testing.B) {
	warning := &UnmetRequirementWarning{Requirement: catalog.Requirement{Kind: ir.KindMcpServer, Name: "audit$(inert-value)", Registry: "https://example.com/team$(inert-origin).git", Path: "team's $(inert-path)"}}
	b.ReportAllocs()
	for b.Loop() {
		_ = warning.Remediation()
	}
}
