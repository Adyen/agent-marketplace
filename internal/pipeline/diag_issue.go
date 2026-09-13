package pipeline

import (
	"crypto/sha256"
	"encoding/binary"
	"strconv"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/diagnosticidentity"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// Rendering budgets. Message, Resource, Source.File and Hint are REGISTRY
// CONTENT: a skill's frontmatter, a directory name, a path inside a cloned repo,
// and a remediation hint. A registry the user does not control can therefore put
// arbitrary bytes in all four. `sync` prints one line per error and a summary
// that says how many there are, so an unbounded field lets one diagnostic scroll
// the summary and every sibling failure off the screen. The remediation hint is
// printed on its own line and is bounded for the same reason.
//
// The budgets are per FIELD rather than per line so a long message can never
// squeeze the resource name — the identity the whole bead is about — out of the
// line. Their sum is the guaranteed bound on a rendered line.
const (
	maxDiagMessageLen  = 1024
	maxDiagCodeLen     = 256
	maxDiagResourceLen = 256
	maxDiagSourceLen   = 256
	maxDiagHintLen     = 512
	maxDiagIdentityLen = 4096
)

// diagIssue is the error value a materialize diagnostic is promoted to,
// whatever its severity. ONE type serves the warning and the error path on
// purpose: bead agent-marketplace-oc2r exists because they had diverged —
// warnings kept d.Resource and were deduplicated, while errors were flattened
// with fmt.Errorf("%s: %s", d.Code, d.Message) and printed once per occurrence.
// A beta tester's sync therefore printed three byte-identical lines for three
// DIFFERENT skills, and neither they nor support could tell which of ~40
// selected resources were at fault. Any future divergence has to be a
// deliberate second type, not an accident of two call sites.
//
// What the rendered line may contain is not a style choice. The reporting
// policy in pkg/harness-compilation/diag/codes.go (see the Devin ingest and
// ownership code blocks) restricts every diagnostic, hint and log attribute to
// FIELD NAMES, the fixed event/tool vocabulary, the resource name and the
// source location. Naming the resource and its source file is in-policy;
// quoting the offending value is not. So this type renders exactly
// d.Code, d.Message, d.Resource, d.Source and d.Hint — the emitting compiler
// has already decided what is safe to say — and never inspects the artifact,
// the allowed-tools entry, the hook payload, the matcher, the timeout, the env
// value, the header value or the OAuth member behind them.
// TestDiagIssueReportsNothingBeyondTheDiagnostic pins that.
type diagIssue struct {
	code             string
	resource         string
	source           string
	hint             string
	msg              string
	message          string
	presentationKey  string
	presentationText string
	// key is the dedup identity, computed once at construction. It is a field
	// rather than a computation in DedupKey() because DedupKey() is called
	// inside dedupIssues' loop and the identity cannot change afterwards.
	key string
}

// newDiagIssue promotes one diagnostic. It runs once per entry of
// materialize.Report.Diags on every sync, so the source location is formatted
// ONCE and threaded into the message rather than formatted again there
// (BenchmarkNewDiagIssue is the guard).
func newDiagIssue(d diag.Diagnostic) *diagIssue {
	code := sanitizeDiagText(d.Code, maxDiagCodeLen)
	source := sanitizeDiagText(formatDiagSource(d.Source), maxDiagSourceLen)
	resource := sanitizeDiagText(d.Resource, maxDiagResourceLen)
	message := sanitizeDiagText(d.Message, maxDiagMessageLen)
	hint := sanitizeDiagText(d.Hint, maxDiagHintLen)
	issue := &diagIssue{
		code:             code,
		resource:         resource,
		source:           source,
		hint:             hint,
		msg:              renderDiagMessage(code, message, resource, source),
		message:          message,
		presentationText: renderDiagMessage(code, message, "", ""),
	}
	issue.key = diagDedupKey(d, issue)
	if code != "" && resource != "" {
		presentationIdentity := diagnosticidentity.Canonical(
			diagnosticidentity.Field{Label: "code", Value: diagnosticidentity.FullText(d.Code)},
			diagnosticidentity.Field{Label: "hint", Value: diagnosticidentity.FullText(d.Hint)},
			diagnosticidentity.Field{Label: "message", Value: diagnosticidentity.NormalizeMessage(diagnosticidentity.FullText(d.Message))},
		)
		// PresentationText is the bounded display form; the grouping identity
		// must not retain an arbitrarily large registry message either.
		issue.presentationKey = hashedDiagIdentity(presentationIdentity)
	}
	return issue
}

// diagDedupKey computes the identity described on DedupKey.
//
// The structured key carries the message with target-specific absolute paths
// folded away by diagnosticidentity.NormalizeMessage. The FULL, untruncated message is
// used, never issue.msg: two 8 KB messages that differ only past the render
// budget are still two different failures, and keying on the truncated render
// would collapse them.
//
// Every identity field is full, sanitized, and explicitly framed. Target and
// severity are always present, including empty/default values, without entering
// the user-facing line. Long identities use a stable digest so hostile registry
// content cannot grow memory or output bounds.
func diagDedupKey(d diag.Diagnostic, issue *diagIssue) string {
	fullIdentity := diagnosticidentity.Canonical(
		diagnosticidentity.Field{Label: "code", Value: diagnosticidentity.FullText(d.Code)},
		diagnosticidentity.Field{Label: "resource", Value: diagnosticidentity.FullText(d.Resource)},
		diagnosticidentity.Field{Label: "source.file", Value: diagnosticidentity.FullText(d.Source.File)},
		diagnosticidentity.Field{Label: "source.line", Value: strconv.Itoa(d.Source.Line)},
		diagnosticidentity.Field{Label: "hint", Value: diagnosticidentity.FullText(d.Hint)},
		// Path normalization is intentionally scoped to this one identity
		// component; producer-provided scope/root fields are not involved here.
		diagnosticidentity.Field{Label: "message", Value: diagnosticidentity.NormalizeMessage(diagnosticidentity.FullText(d.Message))},
		diagnosticidentity.Field{Label: "target", Value: diagnosticidentity.FullText(d.Target)},
		diagnosticidentity.Field{Label: "severity", Value: strconv.Itoa(int(d.Severity))},
	)
	// Preserve the historical resource-less default fallback's rendered key,
	// while every non-default target/severity value still gets an explicit
	// framed identity. Structured diagnostics always use all framed fields.
	if issue.code == "" || issue.resource == "" {
		if d.Target == "" && d.Severity == diag.SeverityError {
			return boundedDiagIdentity(renderDiagMessage(issue.code, diagnosticidentity.FullText(d.Message), issue.resource, issue.source))
		}
	}
	return hashedDiagIdentity(fullIdentity)
}

func hashedDiagIdentity(identity string) string {
	sum := sha256.Sum256([]byte(identity))
	parts := make([]string, 0, 4)
	for offset := 0; offset < len(sum); offset += 8 {
		parts = append(parts, strconv.FormatUint(binary.BigEndian.Uint64(sum[offset:offset+8]), 10))
	}
	return strings.Join(parts, ":")
}

func boundedDiagIdentity(identity string) string {
	if len(identity) <= maxDiagIdentityLen {
		return identity
	}
	return hashedDiagIdentity(identity)
}

// PromoteDiagnostic is the ONE way anything outside this package turns a
// materialize diagnostic into an error value.
//
// It exists because "one type serves both severities" (see diagIssue) was only
// half true: Sync went through diagIssue while internal/tui's update path kept
// its own fmt.Errorf("%s: %s", d.Code, d.Message), so the TUI still printed the
// anonymous, resource-less, hint-less lines bead agent-marketplace-oc2r exists
// to remove. Exporting the promotion is what makes the invariant enforceable
// instead of aspirational.
func PromoteDiagnostic(d diag.Diagnostic) error { return newDiagIssue(d) }

// DedupIssues drops later occurrences of the same underlying problem while
// preserving order. Errors that expose DedupKey() are collapsed on that
// identity; everything else falls back to its message.
//
// It lives beside the type whose identity it consumes so every surface that
// promotes a diagnostic — `sync`, the TUI — collapses repeats the same way. A
// DedupKey() must include whatever distinguishes two real failures, because
// collapsing distinct failures hides them instead of clarifying them.
func DedupIssues(issues []error) []error {
	type dedupKeyer interface{ DedupKey() string }

	deduped := make([]error, 0, len(issues))
	seen := make(map[string]struct{}, len(issues))
	for _, issue := range issues {
		key := issue.Error()
		if keyer, ok := issue.(dedupKeyer); ok {
			key = keyer.DedupKey()
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, issue)
	}
	return deduped
}

func (i *diagIssue) Error() string { return i.msg }

// Resource identifies the catalog resource for grouped CLI warning output.
func (i *diagIssue) Resource() string          { return i.resource }
func (i *diagIssue) Source() string            { return i.source }
func (i *diagIssue) PresentationKey() string   { return i.presentationKey }
func (i *diagIssue) PresentationText() string  { return i.presentationText }
func (i *diagIssue) DiagnosticCode() string    { return i.code }
func (i *diagIssue) DiagnosticMessage() string { return i.message }

// Remediation satisfies resolver.Remediable so the CLI renders the
// compiler-authored Hint the same way it renders resolver remediation, for
// warnings and errors alike. Before bead agent-marketplace-oc2r the Hint was
// never printed at all: remediation the compiler had already written was
// dropped on the floor.
func (i *diagIssue) Remediation() string { return i.hint }

// DedupKey returns the identity of the underlying failure so the sync output
// layer can collapse repeats.
//
// The key INCLUDES the resource and its source location, and that is the whole
// point: the same code and message legitimately occurs once per resource, so a
// key built from the message alone would collapse three distinct failing skills
// into one line — the exact bug this type was rewritten to fix. Two
// diagnostics collapse only when they name the same code and the same resource
// at the same source location, which is also what keeps a skill and a
// same-named command apart, since both report skill.devin.tool-unknown.
//
// The message is part of the key with ABSOLUTE PATHS FOLDED AWAY
// (diagnosticidentity.NormalizeMessage). Both halves of that sentence are load-bearing
// and each fixes a different failure mode:
//
//   - Excluding the message entirely under-reports. Several emitters build the
//     message with fmt.Sprintf over a distinguishing FIELD NAME while passing a
//     CONSTANT hint literal: compile/devin's droppedFieldDiagnostics (one
//     diagnostic per dropped MCP field), mcpInterpolator.reportUnsupportedInterpolation
//     (which keeps an `unsupported map[string]bool` precisely so each field is
//     reported once), and materialize/devin's satisfied-by-unmanaged warning
//     (one per hook container). Every one of them has identical
//     code+resource+source+hint across materially different facts, so a key
//     without the message reported the first field and silently dropped the
//     rest. TestDiagIssueSeparatesDistinctDroppedFieldsUnderOneCode and its two
//     siblings pin that.
//   - Including the message VERBATIM over-reports. internal/materialize builds
//     several messages with fmt.Sprintf over an output directory, so the same
//     failure of the same resource reads differently once per harness target
//     and would print once per target. Folding absolute paths to a placeholder
//     removes exactly that variance and nothing else: an absolute path is the
//     only part of a message that is a property of the OUTPUT ROOT rather than
//     of the failure.
//
// KNOWN GAP (bead agent-marketplace-oc2r, adversarial review): folding absolute
// paths means two diagnostics whose messages differ ONLY by an absolute path
// still collapse. That is right for a marketplace resource reported once per
// target, and wrong for the consumer-side emitters whose Resource is itself an
// output-root-relative FILE PATH — internal/materialize's managedMergePruneDiag
// names `settings.json` for both `<repo>/.claude/settings.json` and
// `~/.claude/settings.json`. Those are two files, two failures and one printed
// line. diag.Diagnostic carries no output-scope field, so the distinction does
// not exist at this layer and the fix belongs in the emitter, which must make
// the scope part of the diagnostic's identity.
// TestDiagIssueKeepsSameFileInTwoOutputScopesApart is the skipped repro.
//
// The HINT is part of the key too. One code can cover two materially
// different failures of the same resource at the same source location: the
// Devin compiler reports both "an allowed-tools entry carries a scoped tool
// specifier" and "an explicitly allowed tool has no Devin equivalent" under
// skill.devin.tool-unknown for one skill, each with its own remediation.
// Keying on code+resource+source alone dropped the second one, so the user
// fixed one reason, re-ran, and met the other — the whack-a-mole this bead
// exists to end. Every Hint in the tree is a static literal (no emitter builds
// one with fmt.Sprintf), so it is target-invariant and cannot re-expand the
// cross-target collapse the path folding buys.
// TestDiagIssueDedupKeySeparatesDistinctReasonsUnderOneCode pins that.
//
// A RESOURCE is required before the structured key is used at all. The
// structured key is a claim that code+resource+source name one failure, and for
// a resource-less diagnostic nothing does: internal/materialize's devinDiag
// emits aggregate failures with no Resource and no Hint, once per output scope,
// so code+source alone would collapse "mcp_config.json is not an object" and
// "mcp_config.json top-level value is not an object" into one line and
// under-count Errors N. Those diagnostics fall back to the rendered message,
// which is what they did before this type existed.
// TestDiagIssueDedupKeyKeepsResourcelessDiagnosticsApart pins that, and
// TestDiagIssueDedupFallbackBoundary pins exactly where the boundary sits.
func (i *diagIssue) DedupKey() string { return i.key }

// renderDiagMessage renders a diagnostic as "<code>: <message>" plus the
// identity suffix that tells the user WHICH resource failed and WHERE it is
// authored. Both halves of the suffix are optional because not every emitter
// populates them; a diagnostic with neither degrades to the historical shape.
// message, resource and source arrive already sanitized and bounded, so this
// function never has to think about hostile bytes and never formats twice.
func renderDiagMessage(code, message, resource, source string) string {
	var b strings.Builder
	b.Grow(len(code) + len(message) + len(resource) + len(source) + 16)
	b.WriteString(code)
	b.WriteString(": ")
	b.WriteString(message)
	if resource == "" && source == "" {
		return b.String()
	}
	b.WriteString(" (")
	if resource != "" {
		b.WriteString("resource ")
		b.WriteString(strconv.Quote(resource))
		if source != "" {
			b.WriteString(", ")
		}
	}
	b.WriteString(source)
	b.WriteString(")")
	return b.String()
}

// formatDiagSource renders a source location as "<file>" or "<file>:<line>".
// The file is the source path the diagnostic already carries (registry-relative),
// never a target output path, so it is stable across harness targets and can
// take part in the dedup identity.
//
// A Line with no File still renders, as "line <n>". Returning "" for it dropped
// the only thing distinguishing two failures of one resource at two different
// lines, so they shared a dedup key and one of them was never printed.
// TestDiagIssueSourceLineSurvivesWithoutAFile pins that.
func formatDiagSource(source diag.SourceRef) string {
	if source.File == "" {
		if source.Line > 0 {
			return "line " + strconv.Itoa(source.Line)
		}
		return ""
	}
	if source.Line > 0 {
		return source.File + ":" + strconv.Itoa(source.Line)
	}
	return source.File
}

// sanitizeDiagText is kept as the pipeline-local name for the shared
// diagnostic policy. Materialization reports use the same implementation so
// identity and command output cannot disagree about hostile bytes or bounds.
func sanitizeDiagText(s string, limit int) string {
	return diag.SanitizeText(s, limit)
}
