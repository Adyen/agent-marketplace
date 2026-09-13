package pipeline

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// ---------------------------------------------------------------------------
// Attack 1: hide a real distinct failure behind the structured dedup key.
// ---------------------------------------------------------------------------

// TestDiagIssueKeepsSameFileInTwoOutputScopesApart is the under-reporting hole
// the structured key opens for the CONSUMER-SIDE emitters.
//
// internal/materialize's managedMergePruneDiag reports a per-file prune refusal
// with Resource AND Source.File both set to the OUTPUT-ROOT-RELATIVE path of the
// offending file, an empty (or catalogue-constant) Hint, and a Message built
// with fmt.Sprintf over the ABSOLUTE path. That function runs once per
// outputKey, i.e. once per (target, machineScope) pair — and the same relative
// name exists in both scopes: `<repo>/.claude/settings.json` and
// `~/.claude/settings.json`, `<repo>/.cursor/mcp.json` and `~/.cursor/mcp.json`.
//
// Two SEPARATE files on disk, each with its own duplicate key, therefore produce
// two diagnostics whose code, resource, source and hint are byte-identical and
// whose messages differ only in the absolute path. The structured key collapses
// them, `Errors N` counts one, and the user fixes the repo file, re-runs, and
// meets the home file — the whack-a-mole this bead exists to end.
//
// KNOWN GAP, deliberately left failing-free by t.Skip rather than "fixed" by
// weakening something else. The two diagnostics below are indistinguishable
// from the pair TestDiagIssueDedupKeyIncludesResource requires to COLLAPSE (one
// resource, one code, one source, one hint, messages differing only in an
// absolute path) — including in Target, which is "devin" for both halves of
// that fixture. The two requirements are therefore contradictory at this layer,
// and no key computable from diag.Diagnostic can satisfy both.
//
// The fix belongs in internal/materialize: a diagnostic whose Resource is an
// output-root-relative FILE PATH must carry the output scope in that identity
// (retargetPruneDiags already rewrites Resource and Source.File and has
// key.machineScope in hand at both call sites). Until it does, `Errors N`
// under-reports this class by one per extra scope.
func TestDiagIssueKeepsSameFileInTwoOutputScopesApart(t *testing.T) {
	t.Skip("KNOWN GAP (agent-marketplace-hyc8): diag.Diagnostic carries no output scope; fix belongs in internal/materialize.retargetPruneDiags; unskip when that issue lands")

	repoScope := diag.Diagnostic{
		Code:     diag.CodeJSONDuplicateKey,
		Severity: diag.SeverityError,
		Target:   "claude",
		Resource: "settings.json",
		Message:  `/work/repo/.claude/settings.json: duplicate object key "hooks" at $ [json.duplicate-key]`,
		Hint:     "Remove the duplicated key and re-run sync",
		Source:   diag.SourceRef{File: "settings.json", Line: 7},
	}
	machineScope := repoScope
	machineScope.Message = `/home/dev/.claude/settings.json: duplicate object key "hooks" at $ [json.duplicate-key]`

	if newDiagIssue(repoScope).DedupKey() == newDiagIssue(machineScope).DedupKey() {
		t.Fatalf("the repo-scope and machine-scope copies of %q share dedup key %q; one real failure is hidden and Errors N under-counts",
			repoScope.Resource, newDiagIssue(repoScope).DedupKey())
	}
}

// TestDiagIssueSeparatesDistinctDroppedFieldsUnderOneCode is the emitter-shaped
// version of the "two reasons under one code" bug the implementer already fixed
// once for skill.devin.tool-unknown. The fix keyed on the Hint, which works only
// when the two reasons carry DIFFERENT hints.
//
// pkg/harness-compilation/compile/devin's droppedFieldDiagnostics and
// mcpInterpolator.reportUnsupportedInterpolation both build the Message with
// fmt.Sprintf over a FIELD NAME while passing a CONSTANT hint literal. Both
// deliberately emit one diagnostic per distinct field (mcpInterpolator even
// keeps an `unsupported map[string]bool` so a map with ten bad values still
// yields one diagnostic per field) — the emitter's whole design says the user
// needs to know each field. code+resource+source+hint is identical for all of
// them, so every field after the first is silently dropped.
func TestDiagIssueSeparatesDistinctDroppedFieldsUnderOneCode(t *testing.T) {
	base := diag.Diagnostic{
		Code:     diag.CodeMcpDevinFieldDropped,
		Severity: diag.SeverityWarning,
		Target:   "devin",
		Resource: "docs",
		Message:  `Devin's documented MCP server forms are disjoint, so the "command" field does not apply to this server and was dropped`,
		Hint:     "Move the field to a server of the form that documents it: stdio servers document command, args, and env; remote servers document url, transport, headers, and the oauth fields",
		Source:   diag.SourceRef{File: "mcp/docs.json", Line: 3},
	}
	other := base
	other.Message = `Devin's documented MCP server forms are disjoint, so the "env" field does not apply to this server and was dropped`

	if newDiagIssue(base).DedupKey() == newDiagIssue(other).DedupKey() {
		t.Fatalf("two different dropped fields of one server share dedup key %q; the second field is never reported", newDiagIssue(base).DedupKey())
	}
}

// TestDiagIssueSeparatesDistinctInterpolationFieldsUnderOneCode is the same
// attack on the emitter that reports an interpolation loss per field. Same
// code, same resource, same source, same constant hint, different field name.
func TestDiagIssueSeparatesDistinctInterpolationFieldsUnderOneCode(t *testing.T) {
	const hint = "Devin documents ${env:VAR} interpolation only; move the file contents into an environment variable"
	base := diag.Diagnostic{
		Code:     diag.CodeMcpInterpolationUnsupported,
		Severity: diag.SeverityWarning,
		Target:   "devin",
		Resource: "docs",
		Message:  `Devin has no equivalent for the file interpolation style used in the "env" field; the value was omitted rather than written empty`,
		Hint:     hint,
		Source:   diag.SourceRef{File: "mcp/docs.json", Line: 3},
	}
	other := base
	other.Message = `Devin has no equivalent for the file interpolation style used in the "headers" field; the value was omitted rather than written empty`

	if newDiagIssue(base).DedupKey() == newDiagIssue(other).DedupKey() {
		t.Fatalf("two different interpolation-loss fields of one server share dedup key %q; the second field is never reported", newDiagIssue(base).DedupKey())
	}
}

// TestDiagIssueKeepsSameResourceApartAcrossHooks attacks the aggregate
// reconciler's satisfied-by-unmanaged warning (internal/materialize/devin.go).
// It sets Resource to the marketplace resource, no Source, a constant Hint, and
// a Message naming the HOOK CONTAINER. One resource can carry several hooks, so
// two containers satisfied by two different handwritten entries collapse into
// one line naming neither.
func TestDiagIssueKeepsSameResourceApartAcrossHooks(t *testing.T) {
	const hint = "No action needed; the existing handwritten entry already provides the behaviour and is neither adopted nor duplicated"
	base := diag.Diagnostic{
		Code:     diag.CodeMaterializeDevinHookSatisfiedByUnmanaged,
		Severity: diag.SeverityWarning,
		Target:   "devin",
		Resource: "guardrails",
		Message:  `a handwritten "PreToolUse" hook in "hooks.v1.json" is semantically identical to this resource's hook; no generated entry was added`,
		Hint:     hint,
	}
	other := base
	other.Message = `a handwritten "PostToolUse" hook in "hooks.v1.json" is semantically identical to this resource's hook; no generated entry was added`

	if newDiagIssue(base).DedupKey() == newDiagIssue(other).DedupKey() {
		t.Fatalf("two different hooks of one resource share dedup key %q; only one is reported", newDiagIssue(base).DedupKey())
	}
}

// TestDiagIssuePreservesCrossTargetCollapseForRealMessages is the noise-side
// control for the same change. Putting the message in the key must not
// re-expand the per-target repeats the warning path has always collapsed, so
// this uses the ACTUAL message shapes internal/materialize emits rather than a
// synthetic one.
func TestDiagIssuePreservesCrossTargetCollapseForRealMessages(t *testing.T) {
	tests := []struct {
		name     string
		resource string
		a, b     string
	}{
		{
			// materialize.go collisionRemoveHint: %q-quoted absolute path.
			name:     "collision.user-owned across two harness targets",
			resource: "alpha",
			a:        `collision: preserved user-owned "alpha" (not managed by agent-marketplace); to resolve, exclude this resource (agent-marketplace remove "alpha") or rename the conflicting file at "/work/repo/.claude/skills/alpha"`,
			b:        `collision: preserved user-owned "alpha" (not managed by agent-marketplace); to resolve, exclude this resource (agent-marketplace remove "alpha") or rename the conflicting file at "/work/repo/.factory/skills/alpha"`,
		},
		{
			// materialize.go: "materialize target %s to %s: %v".
			name:     "write error naming the output directory",
			resource: "alpha",
			a:        "prepare skill cache for target claude: mkdir /work/repo/.claude/skills: permission denied",
			b:        "prepare skill cache for target claude: mkdir /home/dev/.claude/skills: permission denied",
		},
		{
			name:     "home-relative and windows paths fold too",
			resource: "alpha",
			a:        "could not patch ~/.devin/hooks.v1.json",
			b:        `could not patch C:\Users\dev\.devin\hooks.v1.json`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newDiagIssue(diag.Diagnostic{Code: "materialize.collision.user-owned", Resource: tt.resource, Message: tt.a})
			b := newDiagIssue(diag.Diagnostic{Code: "materialize.collision.user-owned", Resource: tt.resource, Message: tt.b})
			if a.DedupKey() != b.DedupKey() {
				t.Fatalf("one failure of one resource now prints once per output root:\n%q\n%q", a.DedupKey(), b.DedupKey())
			}
		})
	}
}

// TestDiagIssueKeyDoesNotFoldRegistryRelativePaths is the other side of the
// folding rule: only ABSOLUTE paths are a property of the output root. A
// registry-relative path is part of the failure's identity and must survive, or
// two resources authored in different directories collapse.
func TestDiagIssueKeyDoesNotFoldRegistryRelativePaths(t *testing.T) {
	a := newDiagIssue(diag.Diagnostic{Code: "c", Resource: "deploy", Message: "could not read skills/deploy/SKILL.md"})
	b := newDiagIssue(diag.Diagnostic{Code: "c", Resource: "deploy", Message: "could not read commands/deploy.md"})
	if a.DedupKey() == b.DedupKey() {
		t.Fatalf("two registry-relative paths folded into one key %q", a.DedupKey())
	}
}

// TestDiagIssueKeyUsesTheUntruncatedMessage guards the interaction between the
// two fixes: the render is bounded, the identity is not. Two 8 KB messages that
// are identical up to the render budget and differ after it are two failures.
func TestDiagIssueKeyUsesTheUntruncatedMessage(t *testing.T) {
	prefix := strings.Repeat("x", 8192)
	a := newDiagIssue(diag.Diagnostic{Code: "c", Resource: "r", Message: prefix + " the env field was dropped"})
	b := newDiagIssue(diag.Diagnostic{Code: "c", Resource: "r", Message: prefix + " the headers field was dropped"})
	if a.Error() != b.Error() {
		t.Fatalf("sanity: both renders should be truncated to the same prefix, got\n%q\n%q", a.Error(), b.Error())
	}
	if a.DedupKey() == b.DedupKey() {
		t.Fatalf("the dedup identity was truncated with the render, so a real failure is hidden")
	}
}

// TestDiagIssueResourcelessErrorKeyUsesTheUntruncatedMessage covers the
// fallback path used by aggregate diagnostics that have no resource. The
// display line is bounded, but two failures that differ only after that bound
// must remain distinct in the dedup identity.
func TestDiagIssueResourcelessErrorKeyUsesTheUntruncatedMessage(t *testing.T) {
	prefix := strings.Repeat("x", maxDiagMessageLen+512)
	base := diag.Diagnostic{
		Code:     diag.CodeMaterializeDevinAggregateShape,
		Severity: diag.SeverityError,
		Source:   diag.SourceRef{File: "mcp_config.json"},
		Message:  prefix + " first shape",
	}
	other := base
	other.Message = prefix + " second shape"
	a := newDiagIssue(base)
	b := newDiagIssue(other)
	if a.Error() != b.Error() {
		t.Fatalf("sanity: bounded renders should match, got %q and %q", a.Error(), b.Error())
	}
	if a.DedupKey() == b.DedupKey() {
		t.Fatalf("resource-less errors differing after the display bound share dedup key %q", a.DedupKey())
	}
	if len(a.DedupKey()) > maxDiagIdentityLen || len(b.DedupKey()) > maxDiagIdentityLen {
		t.Fatalf("resource-less dedup identity exceeded bound: %d, %d", len(a.DedupKey()), len(b.DedupKey()))
	}
}

// TestDiagIssuePresentationIdentityIsBounded keeps warning grouping keys from
// retaining unbounded registry text while preserving exact-message grouping.
func TestDiagIssuePresentationIdentityIsBounded(t *testing.T) {
	message := strings.Repeat("x", maxDiagIdentityLen+1024)
	base := diag.Diagnostic{
		Code:     "materialize.warning",
		Severity: diag.SeverityWarning,
		Resource: "resource",
		Hint:     "repair the resource",
		Message:  message,
	}
	issue := newDiagIssue(base)
	if len(issue.PresentationKey()) > maxDiagIdentityLen {
		t.Fatalf("presentation key retained unbounded registry text: %d bytes", len(issue.PresentationKey()))
	}
	if len(issue.PresentationText()) > maxDiagIdentityLen {
		t.Fatalf("presentation text exceeded display bound: %d bytes", len(issue.PresentationText()))
	}
	if issue.PresentationKey() != newDiagIssue(base).PresentationKey() {
		t.Fatalf("identical warning presentations must retain the same grouping key")
	}
	other := base
	other.Message += "-different"
	if issue.PresentationKey() == newDiagIssue(other).PresentationKey() {
		t.Fatalf("distinct warning presentations must retain distinct grouping keys")
	}
}

// ---------------------------------------------------------------------------
// Attack 2: the fallback boundary.
// ---------------------------------------------------------------------------

// TestDiagIssueDedupFallbackBoundary states exactly where the boundary sits so a
// future edit has to move it deliberately. The structured key is used IFF both
// Code and Resource are non-empty; the presence or absence of Source and Hint
// does not move the boundary, only the key's contents.
func TestDiagIssueDedupFallbackBoundary(t *testing.T) {
	tests := []struct {
		name       string
		d          diag.Diagnostic
		structured bool
	}{
		{"code and resource", diag.Diagnostic{Code: "c", Resource: "r", Message: "m"}, true},
		{"code and resource, no source, no hint", diag.Diagnostic{Code: "c", Resource: "r", Message: "m"}, true},
		{"no resource", diag.Diagnostic{Code: "c", Message: "m"}, false},
		{"no code", diag.Diagnostic{Resource: "r", Message: "m"}, false},
		{"neither", diag.Diagnostic{Message: "m"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := newDiagIssue(tt.d)
			usedMessage := issue.DedupKey() == issue.Error()
			if tt.structured == usedMessage {
				t.Fatalf("structured=%v but DedupKey()==Error() is %v (key %q)", tt.structured, usedMessage, issue.DedupKey())
			}
		})
	}
}

// TestDiagIssueDedupKeyIsNotForgeable attacks the key construction itself.
// DedupKey joins raw, unescaped fields with "\x00". Resource and Source.File are
// registry content, so a resource name that CONTAINS the separator can forge the
// key of an unrelated diagnostic and delete it from the report.
func TestDiagIssueDedupKeyIsNotForgeable(t *testing.T) {
	honest := diag.Diagnostic{
		Code:     "skill.devin.tool-unknown",
		Resource: "alpha",
		Message:  "the real failure",
		Hint:     "fix it",
		Source:   diag.SourceRef{File: "skills/alpha/SKILL.md"},
	}
	// A resource literally named "alpha\x00skills/alpha/SKILL.md" with no source
	// produces the same joined key as the honest one above.
	forged := diag.Diagnostic{
		Code:     "skill.devin.tool-unknown",
		Resource: "alpha\x00skills/alpha/SKILL.md",
		Message:  "an unrelated failure",
		Hint:     "fix it",
	}
	if newDiagIssue(honest).DedupKey() == newDiagIssue(forged).DedupKey() {
		t.Fatalf("a crafted resource name forged the dedup key %q of an unrelated diagnostic, deleting it from the report",
			newDiagIssue(honest).DedupKey())
	}
}

// TestDiagIssueSourceLineSurvivesWithoutAFile pins that a source location whose
// File is empty but whose Line is set does not silently lose the line. Today
// formatDiagSource returns "" for an empty File whatever the Line is, so two
// failures of one resource at two different lines collapse.
func TestDiagIssueSourceLineSurvivesWithoutAFile(t *testing.T) {
	first := diag.Diagnostic{Code: "c", Resource: "r", Message: "line 4 is bad", Source: diag.SourceRef{Line: 4}}
	second := diag.Diagnostic{Code: "c", Resource: "r", Message: "line 9 is bad", Source: diag.SourceRef{Line: 9}}
	if newDiagIssue(first).DedupKey() == newDiagIssue(second).DedupKey() {
		t.Fatalf("two failures of one resource at different lines share dedup key %q because an empty Source.File drops the line",
			newDiagIssue(first).DedupKey())
	}
}

// ---------------------------------------------------------------------------
// Attack 3: break rendering.
// ---------------------------------------------------------------------------

// TestDiagIssueRenderStaysOnOneLine is the line-count-correspondence attack.
// `sync` prints `Errors N` and then errors.Join, which separates errors with
// "\n". Every consumer — the user, the e2e OutContains assertions, support
// reading a paste — reads one line per error. Message, Resource and Source.File
// all come from registry content: a newline in ANY of them forges extra lines,
// and a forged line can impersonate an `Error:` or `warning:` prefix and claim a
// failure was a warning (or invent one that never happened).
//
// Resource is run through strconv.Quote and is safe. Message and Source.File are
// interpolated raw.
func TestDiagIssueRenderStaysOnOneLine(t *testing.T) {
	tests := []struct {
		name string
		d    diag.Diagnostic
	}{
		{
			name: "newline in message forges a warning line",
			d: diag.Diagnostic{
				Code:     "skill.devin.tool-unknown",
				Resource: "alpha",
				Message:  "artifact suppressed\nwarning: nothing else went wrong",
				Source:   diag.SourceRef{File: "skills/alpha/SKILL.md", Line: 4},
			},
		},
		{
			name: "newline in source file forges an error line",
			d: diag.Diagnostic{
				Code:     "skill.devin.tool-unknown",
				Resource: "alpha",
				Message:  "artifact suppressed",
				Source:   diag.SourceRef{File: "skills/alpha/SKILL.md)\nError: your registry is compromised"},
			},
		},
		{
			name: "carriage return in message can overwrite the line on a terminal",
			d: diag.Diagnostic{
				Code:     "skill.devin.tool-unknown",
				Resource: "alpha",
				Message:  "artifact suppressed\rall good",
				Source:   diag.SourceRef{File: "skills/alpha/SKILL.md"},
			},
		},
		{
			name: "NUL in source file reaches the terminal",
			d: diag.Diagnostic{
				Code:     "skill.devin.tool-unknown",
				Resource: "alpha",
				Message:  "artifact suppressed",
				Source:   diag.SourceRef{File: "skills/alpha/SKILL\x00.md"},
			},
		},
		{
			name: "unicode RTL override in message reverses the rendered text",
			d: diag.Diagnostic{
				Code:     "skill.devin.tool-unknown",
				Resource: "alpha",
				Message:  "artifact suppressed \u202e desserppus ton tcafitra",
				Source:   diag.SourceRef{File: "skills/alpha/SKILL.md"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newDiagIssue(tt.d).Error()
			if strings.ContainsAny(got, "\n\r\x00") {
				t.Fatalf("rendered line carries a control character, so it is not one line: %q", got)
			}
			for _, bidi := range []string{"\u202a", "\u202b", "\u202c", "\u202d", "\u202e", "\u2066", "\u2067", "\u2068", "\u2069"} {
				if strings.Contains(got, bidi) {
					t.Fatalf("rendered line carries a unicode bidi override %q, so what the user reads is not what the diagnostic says: %q", bidi, got)
				}
			}
		})
	}
}

// TestDiagIssueRenderIsBounded pins that one diagnostic cannot flood a terminal.
// Message, Resource and Source.File are registry content; 8 KB of text in any of
// them scrolls the `Errors N` summary and every other error off the screen.
func TestDiagIssueRenderIsBounded(t *testing.T) {
	const limit = 4096
	huge := strings.Repeat("A", 8192)
	for _, tt := range []struct {
		name string
		d    diag.Diagnostic
	}{
		{"huge message", diag.Diagnostic{Code: "c", Resource: "r", Message: huge}},
		{"huge resource", diag.Diagnostic{Code: "c", Resource: huge, Message: "m"}},
		{"huge source file", diag.Diagnostic{Code: "c", Resource: "r", Message: "m", Source: diag.SourceRef{File: huge}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := len(newDiagIssue(tt.d).Error()); got > limit {
				t.Fatalf("rendered line is %d bytes, want at most %d; one diagnostic can scroll the whole report away", got, limit)
			}
		})
	}
}

// TestDiagIssueRenderDoesNotReinterpretFormatVerbs is a NEGATIVE control: the
// rendering must never hand attacker text back to a formatter. A resource or
// message containing %s/%d must appear verbatim, never as %!s(MISSING).
func TestDiagIssueRenderDoesNotReinterpretFormatVerbs(t *testing.T) {
	d := diag.Diagnostic{
		Code:     "skill.devin.tool-unknown",
		Resource: "%s-%d-%v",
		Message:  "artifact %s suppressed %d",
		Source:   diag.SourceRef{File: "skills/%s/SKILL.md", Line: 4},
	}
	got := newDiagIssue(d).Error()
	if strings.Contains(got, "%!") {
		t.Fatalf("rendered line reinterpreted a format verb: %q", got)
	}
	if !strings.Contains(got, "artifact %s suppressed %d") {
		t.Fatalf("rendered line mangled the message: %q", got)
	}
}
