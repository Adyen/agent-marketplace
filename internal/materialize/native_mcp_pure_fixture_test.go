package materialize

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestNativeMCPPureFixtureNamesSourceAndTarget(t *testing.T) {
	resources := nativeMCPResources(hcc.TargetFactory, config.RepoCommitted)
	if len(resources) != 2 {
		t.Fatalf("pure MCP fixture resources = %d, want two", len(resources))
	}
	for i, name := range []string{"first", "second"} {
		got := resources[i]
		if got.Name != name || got.Kind != ir.KindMcpServer || got.Source != config.RepoCommitted {
			t.Fatalf("resource %d = name %q kind %v source %v, want %q MCP repo", i, got.Name, got.Kind, got.Source, name)
		}
		if len(got.Harnesses) != 1 || got.Harnesses[0] != hcc.TargetFactory {
			t.Fatalf("resource %d harnesses = %v, want factory", i, got.Harnesses)
		}
	}
}
