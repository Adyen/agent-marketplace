package pluginpolicy

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// A catalog lookup failure inside the plugin surface must not fail open on a
// security restriction: the error propagates instead of compiling the member.
func TestApplyPluginSurfacePropagatesCatalogError(t *testing.T) {
	var mp MemberProjection
	res := ir.NewSkill("s", "d", "body", nil, nil, ir.SourceRef{})
	if _, err := applyPluginSurface(target.Target("bogus"), res, &mp); err == nil {
		t.Fatal("an unresolvable catalog must surface an error, not pass the member through")
	}
}

// Expansion is a single pass over both tokens: ${PLUGIN_DATA} inside the
// replacement text is output, not a new expansion opportunity.
func TestExpandPluginRuntimeSinglePass(t *testing.T) {
	srv := &ir.McpServer{Args: []string{"${PLUGIN_ROOT}/x"}}
	out := expandPluginRuntime(srv, Roots{Package: "/gen/${PLUGIN_DATA}", Data: "/data/demo"})
	if out.Args[0] != "/gen/${PLUGIN_DATA}/x" {
		t.Fatalf("arg = %q, want the literal single-pass result %q", out.Args[0], "/gen/${PLUGIN_DATA}/x")
	}
}
