package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// fakeStructuredRefusal stands in for a refusal whose Error text embeds its
// own remediation lines, like the update selection refusal.
type fakeStructuredRefusal struct{}

func (fakeStructuredRefusal) Error() string {
	return "skill \"beta-skill\" was selected but not found in /reg\n  → run 'agent-marketplace remove --kind skill beta-skill' before rerunning update\nupdate left the lock and harness outputs unchanged"
}
func (fakeStructuredRefusal) DiagnosticCause() string {
	return "skill \"beta-skill\" was selected but not found in /reg"
}
func (fakeStructuredRefusal) DiagnosticRemediation() string {
	return "run 'agent-marketplace remove --kind skill beta-skill' before rerunning update; update left the lock and harness outputs unchanged"
}

// A refusal that carries its own cause and remediation renders as one
// diagnostic line and one recovery line, keeping the wrapping operation's
// prefix, instead of a flattened line with literal \x0A escapes followed by
// the generic recovery copy.
// spec: tui-error-recovery/structured-refusals-render-their-own-remediation
func TestStructuredRefusalRendersItsOwnRemediation(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.loading = false
	m.width, m.height = 200, 40
	err := fmt.Errorf("update refresh failed: %w", fakeStructuredRefusal{})
	m.recordOperationFailure(operationUpdate, diagnosticUpdate, err)
	view := ansi.Strip(renderErrors(m))
	for _, want := range []string{
		"• update refresh failed: skill \"beta-skill\" was selected but not found in /reg\n",
		"  → run 'agent-marketplace remove --kind skill beta-skill' before rerunning update; update left the lock and harness outputs unchanged\n",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("errors view lacks %q:\n%s", want, view)
		}
	}
	for _, unwanted := range []string{`\x0A`, operationRecoveryCopy} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("errors view contains %q:\n%s", unwanted, view)
		}
	}
	if footer := ansi.Strip(renderFooter(m)); strings.Contains(footer, `\x0A`) {
		t.Fatalf("footer shows a flattened line break: %q", footer)
	}
}
