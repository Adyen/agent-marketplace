package hookpolicy_test

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/hookpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

const (
	decodeRefusalMatcher   = `^(mcp_call_tool|mcp__blackboard__.*)$`
	coverageRefusalMatcher = `^mcp_call_tool$`
	validMCPMatcher        = `mcp__blackboard__.*`

	decodeRefusalMessage   = "The source matcher cannot be decoded for cross-harness translation; hook suppressed"
	decodeRefusalHint      = "Use exact source tool names or a supported MCP pattern, or author a target-specific hook"
	coverageRefusalMessage = "The target tool catalog cannot exactly cover the source matcher; hook suppressed"
	coverageRefusalHint    = "Author a target-specific hook or explicitly narrow the selected harnesses; preview cleanup with agent-marketplace sync --check"
)

func resolveDevinMatcherForFactory(matcher string) hookpolicy.Resolved {
	hook := ir.NewHookWithMatcherOptions("pre-tool", "PreToolUse", matcher, "echo guard", "command", nil, ir.SourceRef{File: ".devin/hooks.v1.json", Line: 3}, ir.WithHookSource(string(target.TargetDevin)))
	return hookpolicy.Resolve(hook, target.TargetFactory, hookpolicy.ScopeRepository, "")
}

// spec: hook-sync-diagnostics/matcher-refusal-identifies-its-translation-stage
func TestResolveMatcherRefusalIdentifiesTranslationStage(t *testing.T) {
	tests := []struct {
		name        string
		matcher     string
		wantMessage string
		wantHint    string
	}{
		{"undecodable source matcher is a decode refusal", decodeRefusalMatcher, decodeRefusalMessage, decodeRefusalHint},
		{"decoded matcher the target cannot cover is a coverage refusal", coverageRefusalMatcher, coverageRefusalMessage, coverageRefusalHint},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := resolveDevinMatcherForFactory(tt.matcher)
			if !res.Failed() || len(res.Diagnostics) != 1 {
				t.Fatalf("resolution = failed %v, diagnostics %+v; want one suppressing refusal", res.Failed(), res.Diagnostics)
			}
			d := res.Diagnostics[0]
			if d.Code != diag.CodeHookMatcherUntranslatable || d.Severity != diag.SeverityError {
				t.Fatalf("code/severity = %s/%v, want %s/error", d.Code, d.Severity, diag.CodeHookMatcherUntranslatable)
			}
			if d.Target != string(target.TargetFactory) || d.Resource != "pre-tool" || d.Source.File != ".devin/hooks.v1.json" || d.Source.Line != 3 {
				t.Fatalf("target/resource/source = %q/%q/%+v, want factory/pre-tool/.devin/hooks.v1.json:3", d.Target, d.Resource, d.Source)
			}
			if d.Message != tt.wantMessage || d.Hint != tt.wantHint {
				t.Fatalf("message/hint = %q/%q, want %q/%q", d.Message, d.Hint, tt.wantMessage, tt.wantHint)
			}
			for _, payload := range []string{"mcp_call_tool", "blackboard", "^(", "echo guard"} {
				if strings.Contains(d.Message+d.Hint, payload) {
					t.Fatalf("refusal text %q quotes the value %q", d.Message+" "+d.Hint, payload)
				}
			}
		})
	}
}

// spec: hook-sync-diagnostics/matcher-refusal-identifies-its-translation-stage
func TestResolveMatcherRefusalControlsRetainTranslation(t *testing.T) {
	same := hookpolicy.Resolve(ir.NewHookWithMatcherOptions("pre-tool", "PreToolUse", decodeRefusalMatcher, "echo guard", "command", nil, ir.SourceRef{}, ir.WithHookSource(string(target.TargetDevin))), target.TargetDevin, hookpolicy.ScopeRepository, "")
	if same.Failed() || same.Matcher != decodeRefusalMatcher {
		t.Fatalf("same-harness Devin hook = matcher %q, diagnostics %+v; want the original matcher preserved", same.Matcher, same.Diagnostics)
	}

	valid := resolveDevinMatcherForFactory(validMCPMatcher)
	if valid.Failed() || valid.Matcher != validMCPMatcher {
		t.Fatalf("valid MCP pattern matcher = %q, diagnostics %+v; want %q translated unchanged", valid.Matcher, valid.Diagnostics, validMCPMatcher)
	}
}

// spec: hook-sync-diagnostics/matcher-refusal-names-the-unknown-tool
func TestResolveMatcherRefusalNamesCatalogTool(t *testing.T) {
	hook := ir.NewHookWithMatcherOptions("post-tool", "PostToolUse", "Edit|Create|MultiEdit", "echo guard", "command", nil, ir.SourceRef{File: "hooks/hooks.json", Line: 4}, ir.WithHookSource(string(target.TargetFactory)))
	res := hookpolicy.Resolve(hook, target.TargetDevin, hookpolicy.ScopeRepository, "")
	if !res.Failed() || len(res.Diagnostics) != 1 {
		t.Fatalf("resolution = failed %v, diagnostics %+v; want one suppressing refusal", res.Failed(), res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Code != diag.CodeHookMatcherUntranslatable {
		t.Fatalf("code = %s, want %s", d.Code, diag.CodeHookMatcherUntranslatable)
	}
	const wantMessage = `Tool "MultiEdit" is a claude tool name, not a factory one; hook suppressed`
	if d.Message != wantMessage {
		t.Fatalf("message = %q, want %q", d.Message, wantMessage)
	}
	for _, other := range []string{"Create", "echo guard", "Edit|"} {
		if strings.Contains(d.Message+d.Hint, other) {
			t.Fatalf("refusal text %q quotes %q beyond the catalog tool name", d.Message+" "+d.Hint, other)
		}
	}
}

// spec: hook-sync-diagnostics/names-outside-every-catalog-are-not-quoted
func TestResolveMatcherRefusalKeepsGenericMessageForUncatalogedTool(t *testing.T) {
	hook := ir.NewHookWithMatcherOptions("post-tool", "PostToolUse", "Edit|zqxNoSuchTool", "echo guard", "command", nil, ir.SourceRef{}, ir.WithHookSource(string(target.TargetFactory)))
	res := hookpolicy.Resolve(hook, target.TargetDevin, hookpolicy.ScopeRepository, "")
	if !res.Failed() || len(res.Diagnostics) != 1 {
		t.Fatalf("resolution = failed %v, diagnostics %+v; want one suppressing refusal", res.Failed(), res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Message != decodeRefusalMessage || d.Hint != decodeRefusalHint {
		t.Fatalf("message/hint = %q/%q, want the generic decode refusal", d.Message, d.Hint)
	}
	if strings.Contains(d.Message+d.Hint, "zqxNoSuchTool") {
		t.Fatalf("refusal text quotes a name no catalog declares: %q", d.Message)
	}
}
