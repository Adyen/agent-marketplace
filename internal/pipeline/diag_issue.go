package pipeline

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

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
	maxDiagResourceLen = 256
	maxDiagSourceLen   = 256
	maxDiagHintLen     = 512
	diagTruncatedMark  = "…(truncated)"
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
	source := sanitizeDiagText(formatDiagSource(d.Source), maxDiagSourceLen)
	resource := sanitizeDiagText(d.Resource, maxDiagResourceLen)
	message := sanitizeDiagText(d.Message, maxDiagMessageLen)
	hint := sanitizeDiagText(d.Hint, maxDiagHintLen)
	target := ""
	if d.Severity == diag.SeverityError && d.Target != "" && strings.HasPrefix(d.Code, "hook.") {
		target = sanitizeDiagText(d.Target, maxDiagResourceLen)
	}
	issue := &diagIssue{
		code:             d.Code,
		resource:         resource,
		source:           source,
		hint:             hint,
		msg:              renderDiagMessage(d.Code, message, target, resource, source),
		message:          message,
		presentationText: renderDiagMessage(d.Code, message, "", "", ""),
	}
	issue.key = diagDedupKey(d, issue)
	if target != "" {
		issue.key += "\x00" + target
		issue.message = issue.msg
	}
	if d.Code != "" && resource != "" && target == "" {
		issue.presentationKey = strings.Join([]string{d.Code, strings.ReplaceAll(d.Hint, "\x00", ""), normalizeDiagMessageForKey(d.Message)}, "\x00")
	}
	return issue
}

// diagDedupKey computes the identity described on DedupKey.
//
// The structured key carries the message with target-specific absolute paths
// folded away (normalizeDiagMessageForKey). The FULL, untruncated message is
// used, never issue.msg: two 8 KB messages that differ only past the render
// budget are still two different failures, and keying on the truncated render
// would collapse them.
//
// The fallback is issue.msg — the rendered line — because that is exactly what
// the pre-diagIssue code deduplicated on for a diagnostic with no resource, and
// TestDiagIssueDedupKeyKeepsResourcelessDiagnosticsApart pins the equivalence.
func diagDedupKey(d diag.Diagnostic, issue *diagIssue) string {
	if d.Code == "" || issue.resource == "" {
		return issue.msg
	}
	return strings.Join([]string{
		d.Code,
		issue.resource,
		issue.source,
		strings.ReplaceAll(d.Hint, "\x00", ""),
		normalizeDiagMessageForKey(d.Message),
	}, "\x00")
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

func IsPromotedDiagnostic(err error) bool {
	var promoted *diagIssue
	return errors.As(err, &promoted)
}

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
// (normalizeDiagMessageForKey). Both halves of that sentence are load-bearing
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
func renderDiagMessage(code, message, target, resource, source string) string {
	var b strings.Builder
	b.Grow(len(code) + len(message) + len(target) + len(resource) + len(source) + 16)
	b.WriteString(code)
	b.WriteString(": ")
	b.WriteString(message)
	if target == "" && resource == "" && source == "" {
		return b.String()
	}
	b.WriteString(" (")
	sep := ""
	if target != "" {
		b.WriteString("target ")
		b.WriteString(target)
		sep = ", "
	}
	if resource != "" {
		b.WriteString(sep)
		b.WriteString("resource ")
		b.WriteString(strconv.Quote(resource))
		sep = ", "
	}
	if source != "" {
		b.WriteString(sep)
		b.WriteString(source)
	}
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

// sanitizeDiagText makes one registry-supplied field safe to put on a terminal
// line, and bounds it.
//
// `sync` prints `Errors N` and then one line per error (errors.Join separates
// with "\n"). The user, `e2e`'s OutContains assertions and support reading a
// pasted transcript all count lines. Message and Source.File reach the rendered
// line UNQUOTED — Resource goes through strconv.Quote and is already safe — so a
// newline in either forges an extra line, and a forged line can carry a
// convincing `Error:` or `warning:` prefix and claim a failure that never
// happened or downgrade one that did. A carriage return does the same thing
// invisibly by rewriting the line in place.
//
// Unicode bidi controls (RLO and friends) are dropped rather than escaped: they
// reorder the glyphs AROUND them, so leaving one in a message lets a registry
// make `resource "alpha"` render as some other resource's name.
//
// Nothing here can add information: it only removes bytes the diagnostic policy
// never authorized a field to carry.
func sanitizeDiagText(s string, limit int) string {
	if s == "" {
		return ""
	}
	if !needsDiagSanitizing(s) {
		return truncateDiagText(s, limit)
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case isDiagBidiControl(r):
			// Dropped entirely; see above.
		case r == utf8.RuneError, r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	return truncateDiagText(b.String(), limit)
}

// needsDiagSanitizing is the fast path: almost every diagnostic in a real run
// is printable ASCII, and this runs three times per diagnostic on every sync
// (BenchmarkNewDiagIssue is the guard). It therefore scans BYTES, not runes, and
// only pays for a rune decode once it leaves ASCII. UTF-8 continuation bytes are
// always >= 0x80, so the single-byte C0/DEL test can never fire inside a
// multi-byte character.
func needsDiagSanitizing(s string) bool {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b < 0x20 || b == 0x7f {
			return true
		}
		if b >= 0x80 {
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError || (r >= 0x80 && r <= 0x9f) || isDiagBidiControl(r) {
				return true
			}
			i += size - 1
		}
	}
	return false
}

// isDiagBidiControl reports whether r is one of the Unicode bidirectional
// formatting characters: the embedding/override block U+202A..U+202E and the
// isolate block U+2066..U+2069.
func isDiagBidiControl(r rune) bool {
	return (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069)
}

// truncateDiagText cuts s to at most limit bytes on a rune boundary and marks
// the cut, so a reader can tell a truncated field from a short one.
func truncateDiagText(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit - len(diagTruncatedMark)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + diagTruncatedMark
}

// diagKeyPathPlaceholder stands in for an absolute path inside a dedup key.
const diagKeyPathPlaceholder = "<path>"

// normalizeDiagMessageForKey folds absolute filesystem paths in a message to a
// placeholder so the message can take part in the dedup identity without
// re-expanding the cross-target collapse.
//
// An absolute path in a diagnostic message is always a property of the OUTPUT
// ROOT the run is writing to (internal/materialize builds several messages with
// fmt.Sprintf over an output directory), never of the failure itself: the same
// resource failing the same way for two harnesses differs only there. Every
// other part of a message — the field name, the tool name, the hook event,
// the reason — is a property of the failure and stays in the key.
//
// Only whole space-delimited tokens are folded, and only after stripping the
// punctuation Go's %q and English prose wrap them in, so a registry-relative
// path such as `skills/alpha/SKILL.md` (no leading separator) is untouched.
func normalizeDiagMessageForKey(message string) string {
	if !strings.Contains(message, "/") && !strings.Contains(message, `\`) {
		return message
	}
	fields := strings.Split(message, " ")
	folded := false
	for i, field := range fields {
		lead := 0
		for lead < len(field) && strings.IndexByte(`"'`+"`"+`(<[`, field[lead]) >= 0 {
			lead++
		}
		trail := len(field)
		for trail > lead && strings.IndexByte(`"'`+"`"+`)>],.:;`, field[trail-1]) >= 0 {
			trail--
		}
		if !isAbsolutePathToken(field[lead:trail]) {
			continue
		}
		fields[i] = field[:lead] + diagKeyPathPlaceholder + field[trail:]
		folded = true
	}
	if !folded {
		return message
	}
	return strings.Join(fields, " ")
}

// isAbsolutePathToken reports whether token is a POSIX absolute path, a Windows
// drive-letter path, or a `~`-rooted home path.
func isAbsolutePathToken(token string) bool {
	switch {
	case len(token) > 1 && token[0] == '/':
		return true
	case len(token) > 2 && token[0] == '~' && (token[1] == '/' || token[1] == '\\'):
		return true
	case len(token) > 2 && token[1] == ':' && (token[2] == '\\' || token[2] == '/'):
		return true
	default:
		return false
	}
}
