package pluginpolicy

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// Expansion is a single pass over both tokens: ${PLUGIN_DATA} inside the
// replacement text is output, not a new expansion opportunity.
func TestExpandPluginRuntimeSinglePass(t *testing.T) {
	srv := &ir.McpServer{Args: []string{"${PLUGIN_ROOT}/x"}}
	out := expandPluginRuntime(srv, Roots{Package: "/gen/${PLUGIN_DATA}", Data: "/data/demo"})
	if out.Args[0] != "/gen/${PLUGIN_DATA}/x" {
		t.Fatalf("arg = %q, want the literal single-pass result %q", out.Args[0], "/gen/${PLUGIN_DATA}/x")
	}
}
