package pipeline

import (
	"errors"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// scopedToolDiagnostic is the diagnostic behind the beta-tester report in bead
// agent-marketplace-oc2r: three skills each carrying a scoped allowed-tools
// specifier produced three byte-identical error lines because the promoted
// error dropped Resource and Source.
func scopedToolDiagnostic(resource, file string, line int) diag.Diagnostic {
	return diag.Diagnostic{
		Code:     diag.CodeSkillDevinToolUnknown,
		Severity: diag.SeverityError,
		Target:   "devin",
		Resource: resource,
		Message:  "an allowed-tools entry carries a scoped tool specifier, which has no Devin equivalent; artifact suppressed",
		Hint:     "Devin's allowed tools (read, edit, grep, glob, exec) cannot be scoped to a command; remove the specifier or drop the explicit allowed-tools restriction",
		Source:   diag.SourceRef{File: file, Line: line},
	}
}

// TestDiagIssueNamesResourceAndSource pins the rendered shape of a promoted
// materialize diagnostic. The rendering is EXACT on purpose: the only
// diagnostic fields that may reach the user are the code, the message, the
// resource name and the source location (see the reporting policy in
// pkg/harness-compilation/diag/codes.go), so an exact match is what keeps a
// future edit from smuggling a tool specifier or a payload into the line.
func TestDiagIssueNamesResourceAndSource(t *testing.T) {
	tests := []struct {
		name string
		d    diag.Diagnostic
		want string
	}{
		{
			name: "resource and source line",
			d:    scopedToolDiagnostic("alpha", "skills/alpha/SKILL.md", 4),
			want: `skill.devin.tool-unknown: an allowed-tools entry carries a scoped tool specifier, which has no Devin equivalent; artifact suppressed (resource "alpha", skills/alpha/SKILL.md:4)`,
		},
		{
			name: "resource and source without a line number",
			d:    scopedToolDiagnostic("beta", "skills/beta/SKILL.md", 0),
			want: `skill.devin.tool-unknown: an allowed-tools entry carries a scoped tool specifier, which has no Devin equivalent; artifact suppressed (resource "beta", skills/beta/SKILL.md)`,
		},
		{
			name: "resource without a source",
			d:    scopedToolDiagnostic("gamma", "", 0),
			want: `skill.devin.tool-unknown: an allowed-tools entry carries a scoped tool specifier, which has no Devin equivalent; artifact suppressed (resource "gamma")`,
		},
		{
			name: "source without a resource",
			d:    scopedToolDiagnostic("", "skills/delta/SKILL.md", 2),
			want: `skill.devin.tool-unknown: an allowed-tools entry carries a scoped tool specifier, which has no Devin equivalent; artifact suppressed (skills/delta/SKILL.md:2)`,
		},
		{
			name: "neither resource nor source keeps the bare code and message",
			d:    scopedToolDiagnostic("", "", 0),
			want: `skill.devin.tool-unknown: an allowed-tools entry carries a scoped tool specifier, which has no Devin equivalent; artifact suppressed`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := newDiagIssue(tt.d).Error(); got != tt.want {
				t.Fatalf("Error() =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

// TestDiagIssueRendersHintAsRemediation asserts the compiler-authored Hint
// reaches the user through the same resolver.Remediable surface the CLI already
// renders, for warnings and errors alike.
func TestDiagIssueSanitizesCodeAndIdentityFields(t *testing.T) {
	d := scopedToolDiagnostic("alpha\nresource", "skills/alpha\u2028SKILL.md", 4)
	d.Code = "materialize.bad\r\x1b[31m"
	d.Hint = "repair\u0085\u202e this"
	d.Message = "failure\x00\xff\u2029here"
	issue := newDiagIssue(d)
	for name, rendered := range map[string]string{"error": issue.Error(), "key": issue.DedupKey(), "hint": issue.Remediation()} {
		if strings.ContainsAny(rendered, "\x01\x1b\r\n\x7f") || strings.Contains(rendered, "\u0085") || strings.Contains(rendered, "\u202e") || strings.Contains(rendered, "\u2029") || strings.Contains(rendered, "\ufffd") {
			t.Errorf("%s retained unsafe diagnostic text: %q", name, rendered)
		}
	}
	if strings.Contains(issue.DiagnosticCode(), "\r") {
		t.Fatalf("DiagnosticCode retained a control: %q", issue.DiagnosticCode())
	}
}

func TestDiagIssueRendersHintAsRemediation(t *testing.T) {
	d := scopedToolDiagnostic("alpha", "skills/alpha/SKILL.md", 4)

	var r resolver.Remediable
	promoted := error(newDiagIssue(d))
	if !errors.As(promoted, &r) {
		t.Fatalf("promoted diagnostic does not implement resolver.Remediable, so its Hint can never be printed")
	}
	if got := r.Remediation(); got != d.Hint {
		t.Fatalf("Remediation() = %q, want the diagnostic hint %q", got, d.Hint)
	}

	warning := scopedToolDiagnostic("alpha", "skills/alpha/SKILL.md", 4)
	warning.Severity = diag.SeverityWarning
	var wr resolver.Remediable
	if !errors.As(error(newDiagIssue(warning)), &wr) {
		t.Fatalf("promoted warning does not implement resolver.Remediable")
	}
	if got := wr.Remediation(); got != warning.Hint {
		t.Fatalf("warning Remediation() = %q, want %q", got, warning.Hint)
	}

	noHint := scopedToolDiagnostic("alpha", "skills/alpha/SKILL.md", 4)
	noHint.Hint = ""
	if got := newDiagIssue(noHint).Remediation(); got != "" {
		t.Fatalf("Remediation() = %q for a diagnostic without a hint, want empty", got)
	}
}

// TestDiagIssueDedupKeyIncludesResource asserts BOTH directions of the dedup
// contract. Collapsing identical code+message lines is the point of the dedup;
// collapsing them ACROSS resources would recreate the very bug bead
// agent-marketplace-oc2r fixes, because one message legitimately occurs once
// per resource.
func TestDiagIssueDedupKeyIncludesResource(t *testing.T) {
	alpha := newDiagIssue(scopedToolDiagnostic("alpha", "skills/alpha/SKILL.md", 4))
	alphaAgain := newDiagIssue(scopedToolDiagnostic("alpha", "skills/alpha/SKILL.md", 4))
	beta := newDiagIssue(scopedToolDiagnostic("beta", "skills/beta/SKILL.md", 4))
	gamma := newDiagIssue(scopedToolDiagnostic("gamma", "skills/gamma/SKILL.md", 4))

	if alpha.DedupKey() != alphaAgain.DedupKey() {
		t.Fatalf("two identical diagnostics for one resource must collapse: %q vs %q", alpha.DedupKey(), alphaAgain.DedupKey())
	}
	keys := map[string]string{}
	for _, issue := range []*diagIssue{alpha, beta, gamma} {
		if prior, dup := keys[issue.DedupKey()]; dup {
			t.Fatalf("distinct resources share dedup key %q (also used by %q); distinct failures would be hidden", issue.DedupKey(), prior)
		}
		keys[issue.DedupKey()] = issue.Error()
	}

	// A command and a skill of the same name share the skill.devin.tool-unknown
	// code, so the source location has to participate in the identity too.
	sameNameSkill := newDiagIssue(scopedToolDiagnostic("deploy", "skills/deploy/SKILL.md", 4))
	sameNameCommand := newDiagIssue(scopedToolDiagnostic("deploy", "commands/deploy.md", 4))
	if sameNameSkill.DedupKey() == sameNameCommand.DedupKey() {
		t.Fatalf("a skill and a command with the same name share dedup key %q", sameNameSkill.DedupKey())
	}

	// Two diagnostics with the same code and resource but target-specific
	// absolute paths in the message still collapse: that is the cross-target
	// behaviour the warning path already had and must not lose.
	perTargetA := scopedToolDiagnostic("alpha", "skills/alpha/SKILL.md", 4)
	perTargetA.Message = "the aggregate at /home/a/.devin/hooks.v1.json could not be patched"
	perTargetB := perTargetA
	perTargetB.Message = "the aggregate at /home/b/.devin/hooks.v1.json could not be patched"
	if newDiagIssue(perTargetA).DedupKey() != newDiagIssue(perTargetB).DedupKey() {
		t.Fatalf("diagnostics for one resource that differ only in a target-specific path must still collapse")
	}
}

// TestDiagIssueReportsNothingBeyondTheDiagnostic pins the hard constraint from
// pkg/harness-compilation/diag/codes.go: only field names, the fixed
// event/tool vocabulary, the resource name and the source location are
// reportable. The promoted error must not add anything of its own, and must not
// echo a value even when one is handed to it in a field it does not render.
func TestDiagIssueReportsNothingBeyondTheDiagnostic(t *testing.T) {
	d := scopedToolDiagnostic("alpha", "skills/alpha/SKILL.md", 4)
	// Target is a harness name, not a value, but it is deliberately not part of
	// the user-facing line; nothing else on the diagnostic may leak either.
	d.Target = "devin"

	issue := newDiagIssue(d)
	rendered := issue.Error() + "\n" + issue.Remediation() + "\n" + issue.DedupKey()

	for _, forbidden := range []string{
		"Bash(",
		"git push",
		"--force",
		"${env:",
		"Authorization",
		"client_secret",
	} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("rendered diagnostic %q leaks %q", rendered, forbidden)
		}
	}

	// Every rendered word must come from a whitelisted diagnostic field or from
	// the fixed label vocabulary this file owns. Anything else is data the
	// promotion invented, which is exactly what the policy forbids.
	// Source.File is stripped before Resource because it embeds the resource
	// name; stripping in the other order would leave the path shredded.
	for _, allowed := range []string{d.Code, d.Message, d.Hint, d.Source.File, d.Resource, "resource "} {
		rendered = strings.ReplaceAll(rendered, allowed, "")
	}
	if strings.ContainsAny(rendered, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		t.Fatalf("rendered diagnostic carries text that is not the code, message, hint, resource or source file: %q", rendered)
	}
}

// TestDiagIssueDedupKeySeparatesDistinctReasonsUnderOneCode is the third dedup
// direction, and the one the first cut of bead agent-marketplace-oc2r got
// wrong. ONE code can cover two materially different failures of the SAME
// resource at the SAME source location. pkg/harness-compilation/compile/devin's
// resolveAllowedTools emits exactly that for one skill that carries both a
// scoped specifier and an unmappable bare tool: two skill.devin.tool-unknown
// errors, two different messages, two different remediations.
//
// Keying on code+resource+source alone collapsed them, so `sync` printed one
// reason, counted `Errors 1`, and the user only met the second reason after
// fixing the first — the whack-a-mole this bead exists to end.
func TestDiagIssueDedupKeySeparatesDistinctReasonsUnderOneCode(t *testing.T) {
	scoped := scopedToolDiagnostic("delta", "skills/delta/SKILL.md", 1)

	unmappable := scoped
	unmappable.Message = "an explicitly allowed tool has no Devin equivalent; artifact suppressed"
	unmappable.Hint = "Use one of Devin's allowed tools (read, edit, grep, glob, exec) or remove the explicit allowed-tools restriction"

	if newDiagIssue(scoped).DedupKey() == newDiagIssue(unmappable).DedupKey() {
		t.Fatalf("two distinct failures of one resource under %s share a dedup key; the second reason is hidden and Errors N under-counts", scoped.Code)
	}

	// The cross-target collapse the message exclusion buys must survive: two
	// diagnostics that differ ONLY in a target-specific path inside the message
	// carry the same static hint, so they still collapse to one line.
	perTargetA := scoped
	perTargetA.Message = "the aggregate at /home/a/.devin/hooks.v1.json could not be patched"
	perTargetB := perTargetA
	perTargetB.Message = "the aggregate at /home/b/.devin/hooks.v1.json could not be patched"
	if newDiagIssue(perTargetA).DedupKey() != newDiagIssue(perTargetB).DedupKey() {
		t.Fatalf("adding the hint to the key re-expanded the cross-target collapse; one failing resource would print once per harness target")
	}
}

// TestDiagIssueDedupKeyKeepsResourcelessDiagnosticsApart is the fourth dedup
// direction. Not every emitter names a resource: internal/materialize's
// devinDiag reports aggregate-file failures with an empty Resource, an empty
// Hint and only the relative file name as Source, once per output scope. For
// those, the MESSAGE is the only thing that distinguishes two materially
// different failures, so the structured key — which deliberately drops the
// message — must not be used at all.
func TestDiagIssueDedupKeyKeepsResourcelessDiagnosticsApart(t *testing.T) {
	shape := diag.Diagnostic{
		Code:     diag.CodeMaterializeDevinAggregateShape,
		Severity: diag.SeverityError,
		Message:  `mcp_config.json: "mcpServers" is not an object`,
		Source:   diag.SourceRef{File: "mcp_config.json"},
	}
	other := shape
	other.Message = "mcp_config.json: top-level value is not an object"

	if newDiagIssue(shape).DedupKey() == newDiagIssue(other).DedupKey() {
		t.Fatalf("two resource-less failures of one file share a dedup key; one of them is hidden and Errors N under-counts")
	}
	if got, want := newDiagIssue(shape).DedupKey(), newDiagIssue(shape).Error(); got != want {
		t.Fatalf("resource-less diagnostic keys on %q, want its full message %q", got, want)
	}

	// The identical failure repeated (same message, e.g. once per scope) still
	// collapses, which is the behaviour the message fallback has always had.
	repeat := shape
	if newDiagIssue(shape).DedupKey() != newDiagIssue(repeat).DedupKey() {
		t.Fatalf("an identical resource-less diagnostic must still collapse")
	}
}
