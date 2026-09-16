package windsurf

import (
	"testing"

	compilepkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestTranslateHookTimeoutSuppressesArtifact(t *testing.T) {
	withTimeout := ir.NewHookWithMatcherOptions("PreToolUse-Bash-command", "PreToolUse", "Bash", "echo timeout", "command", nil, ir.SourceRef{}, ir.WithHookTimeout(30))
	result, err := compilepkg.Compile(withTimeout, compilepkg.TargetWindsurf)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 0 {
		t.Fatalf("artifacts = %#v, want none", result.Artifacts)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != diag.CodeHookWindsurfTimeoutUnsupported || result.Diagnostics[0].Severity != diag.SeverityError {
		t.Fatalf("diagnostics = %#v, want timeout error", result.Diagnostics)
	}

	withoutTimeout := ir.NewHookWithMatcher("PreToolUse-Bash-command", "PreToolUse", "Bash", "echo valid", "command", nil, ir.SourceRef{})
	valid, err := compilepkg.Compile(withoutTimeout, compilepkg.TargetWindsurf)
	if err != nil || len(valid.Artifacts) != 1 || len(valid.Diagnostics) != 0 {
		t.Fatalf("valid hook result = %#v, %v", valid, err)
	}
}
