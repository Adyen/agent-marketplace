package lifecycle

import (
	"errors"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// The refusal keeps its CLI text, and offers the TUI its cause (each miss and
// load note) and its remediation (each remediation line and the closing line)
// as single lines.
// spec: tui-error-recovery/structured-refusals-render-their-own-remediation
func TestUpdateSelectionRefusalSplitsCauseAndRemediation(t *testing.T) {
	refusal := &UpdateSelectionRefusedError{
		misses: []*resolver.SelectedNotFoundError{
			{Kind: ir.KindSkill, Name: "beta-skill", URL: "file:///reg.git", Ref: "main"},
			{Kind: ir.KindSkill, Name: "org-review", URL: "file:///reg.git", Ref: "main", Required: true},
		},
		LoadErrors: []SelectionPriorLoadError{{LockPath: "/repo/.agents/lock.yaml", Err: errors.New("corrupt")}},
	}
	text := refusal.Error()
	cause, remediation := refusal.DiagnosticCause(), refusal.DiagnosticRemediation()
	for name, value := range map[string]string{"cause": cause, "remediation": remediation} {
		if strings.Contains(value, "\n") {
			t.Fatalf("%s spans lines: %q", name, value)
		}
	}
	for _, miss := range refusal.misses {
		if !strings.Contains(cause, miss.Error()) || !strings.Contains(text, miss.Error()) {
			t.Fatalf("cause or text lacks %q:\ncause: %s\ntext: %s", miss.Error(), cause, text)
		}
		line := strings.TrimPrefix(MissRemediationLine(miss), "  → ")
		if !strings.Contains(remediation, line) || strings.Contains(cause, line) {
			t.Fatalf("remediation must carry %q and the cause must not:\ncause: %s\nremediation: %s", line, cause, remediation)
		}
	}
	if !strings.Contains(cause, "could not load the previously locked revision for /repo/.agents/lock.yaml") {
		t.Fatalf("cause lacks the load note: %s", cause)
	}
	if !strings.HasSuffix(remediation, UpdateSelectionRefusalHint) || strings.Contains(cause, UpdateSelectionRefusalHint) {
		t.Fatalf("remediation must end with the closing line:\n%s", remediation)
	}
	if !strings.HasSuffix(text, "\n"+UpdateSelectionRefusalHint) {
		t.Fatalf("the CLI text changed:\n%s", text)
	}
}
