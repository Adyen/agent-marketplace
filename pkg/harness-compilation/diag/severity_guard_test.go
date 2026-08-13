package diag

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// rawSeverityErrorComparison matches an ordinal magnitude comparison against
// SeverityError in either operand order, with or without a package qualifier
// (`diag.SeverityError`, `hcc.SeverityError`, bare `SeverityError`).
//
// The other operand is deliberately not constrained: `d.Severity`, a bare
// `sev` local, or a struct field all count, because the footgun is the operator
// next to SeverityError and nothing else. A channel send (`ch <- SeverityError`)
// is not a comparison and does not match, because `-` cannot start an operand.
//
// Equality comparisons (`== SeverityError`, `!= SeverityError`) are correct and
// deliberately not matched; only `>=`, `<=`, `>` and `<` are.
var rawSeverityErrorComparison = regexp.MustCompile(
	`(?:(?:>=|<=|>|<)\s*(?:[A-Za-z_][A-Za-z0-9_]*\.)?SeverityError\b)` +
		`|(?:\bSeverityError\s*(?:>=|<=|>|<))`,
)

// hasRawSeverityErrorComparison reports whether a single source line contains a
// raw ordinal comparison against SeverityError in code. Comments are stripped
// first so that documentation of the footgun (including this package's own doc
// comments) does not trip the guard.
func hasRawSeverityErrorComparison(line string) bool {
	if idx := strings.Index(line, "//"); idx >= 0 {
		line = line[:idx]
	}
	return rawSeverityErrorComparison.MatchString(line)
}

// TestSeverityGuardRegexDetectsKnownFootguns pins the retired matcher's
// behavior as a baseline for the AST guard's adversarial coverage.
func TestSeverityGuardRegexDetectsKnownFootguns(t *testing.T) {
	shouldMatch := []string{
		"if d.Severity >= diag.SeverityError {",
		"if d.Severity >= hcc.SeverityError && d.Resource != \"\" {",
		"if d.Severity < diag.SeverityError || d.Resource == \"\" {",
		"if diagnostic.Severity <= SeverityError {",
		"if d.Severity > SeverityError {",
		"if diag.SeverityError < d.Severity {",
		"if d.Severity >= SeverityError { // regression",
		// The left operand may be spelled any way at all: a local, a
		// renamed field, or a bare Severity value inside this package.
		"if sev >= diag.SeverityError {",
		"if sev := d.Severity; sev >= SeverityError {",
		"if diag.SeverityError <= sev {",
		"if minSeverity > SeverityError {",
		"switch { case s < SeverityError:",
	}
	for _, line := range shouldMatch {
		if !hasRawSeverityErrorComparison(line) {
			t.Errorf("guard scan missed a known footgun: %q", line)
		}
	}

	shouldNotMatch := []string{
		"if d.Severity == diag.SeverityError {",
		"if d.Severity != diag.SeverityError {",
		"if d.IsError() {",
		"if d.Severity.AtLeastAsSevereAs(diag.SeverityError) {",
		// Comparisons against other thresholds are legitimate; only the
		// SeverityError zero-value case is a guaranteed logic error.
		"if d.Severity > diag.SeverityWarning {",
		"if d.Severity <= diag.SeverityWarning {",
		"SeverityError   Severity = iota // Source is malformed or violates safety",
		// A channel send and an unrelated magnitude comparison on the same
		// line as an equality test are not severity comparisons.
		"severities <- diag.SeverityError",
		"if i < len(diags) && diags[i].Severity == diag.SeverityError {",
		"diags = append(diags, Diagnostic{Severity: SeverityError})",
		// Prose describing the footgun must not trip the guard.
		"// value of Severity, so `d.Severity >= SeverityError` is true for every",
		"\tif d.IsError() { // never d.Severity >= diag.SeverityError",
	}
	for _, line := range shouldNotMatch {
		if hasRawSeverityErrorComparison(line) {
			t.Errorf("guard scan false positive: %q", line)
		}
	}
}

// repoRoot walks up from the working directory to the module root (the nearest
// ancestor holding go.mod).
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found above %s", dir)
		}
		dir = parent
	}
}
