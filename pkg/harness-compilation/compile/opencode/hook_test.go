package opencode

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: opencode-hooks/hostile-declaration-strings-remain-inert-adapter-data
func TestHookDeclarationsAreData(t *testing.T) {
	first := ir.NewHookWithMatcher("check", "PreToolUse", "Bash", "echo 'quotes'; ${TOKEN}; `code`; \u2028comment", "command", nil, ir.SourceRef{})
	second := ir.NewHookWithMatcher("check", "PreToolUse", "Bash", "completely different code", "command", nil, ir.SourceRef{})
	a, d, err := (Compiler{}).Translate(first)
	if err != nil || len(d) != 0 || len(a) != 2 {
		t.Fatalf("compile: %v %v", d, err)
	}
	b, _, _ := (Compiler{}).Translate(second)
	if a[0].Path != "hooks/agent-marketplace/check.json" || a[1].Path != "plugins/agent-marketplace.js" || !bytes.Equal(a[1].Bytes, b[1].Bytes) {
		t.Fatal("resource payload changed executable adapter")
	}
	var declaration map[string]any
	if err := json.Unmarshal(a[0].Bytes, &declaration); err != nil {
		t.Fatal(err)
	}
	if declaration["command"] != first.Command || declaration["timeout"] != float64(60) {
		t.Fatalf("declaration: %v", declaration)
	}
}

// spec: opencode-hooks/claimed-tool-consumer-paths-share-the-proven-contract
// spec: opencode-hooks/unsupported-hook-semantics-fail-visibly
// spec: opencode-hooks/runner-budgets-refuse-excess-without-truncation
func TestHookUnsupportedForms(t *testing.T) {
	for _, tc := range []struct {
		event, matcher, kind string
		timeout              int
		want                 bool
	}{
		{"PreToolUse", "Bash|Read", "command", 600, true},
		{"PreToolUse", "*", "command", 1, true},
		{"PostToolUse", "Bash", "command", 60, false},
		{"PreToolUse", "Bash(git:*)", "command", 60, false},
		{"PreToolUse", "Bash", "prompt", 60, false},
		{"PreToolUse", "Task", "command", 60, false},
		{"PreToolUse", "mcp_docs_read", "command", 60, false},
		{"PreToolUse", "Bash", "command", 0, false},
		{"PreToolUse", "Bash", "command", 601, false},
	} {
		h := ir.NewHookWithMatcherOptions("check", tc.event, tc.matcher, "true", tc.kind, nil, ir.SourceRef{}, ir.WithHookTimeout(tc.timeout))
		a, d, err := (Compiler{}).Translate(h)
		if err != nil {
			t.Fatal(err)
		}
		if tc.want {
			if len(a) != 2 || len(d) != 0 {
				t.Fatalf("valid hook refused: %v", d)
			}
		} else if len(a) != 0 || !diag.HasCode(d, diag.CodeHookOpenCodeUnsupported) || slices.ContainsFunc(d, func(d diag.Diagnostic) bool { return d.Severity != diag.SeverityError }) {
			t.Fatalf("unsupported hook emitted: %v %v", a, d)
		}
	}
}
