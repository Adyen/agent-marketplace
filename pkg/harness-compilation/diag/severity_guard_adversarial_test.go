package diag

import "testing"

// This file records the blind spots of the retired line-based regex matcher in
// severity_guard_test.go. Each case is a plausible reintroduction of the
// zero-value-severity bug and asserts whether that matcher sees it. The active
// repository guard is the AST scan in severity_astguard_test.go.

// bypass is one reintroduction attempt.
type bypass struct {
	name string
	// line is the source line (or the first line of a multi-line form).
	lines []string
	// caught records whether the retired matcher flags the construct.
	// A `false` here documents why the AST replacement is required.
	caught bool
	why    string
}

// TestSeverityGuardBypasses enumerates constructs that reintroduce the bug.
// It asserts the retired matcher's behavior so the rationale for the AST guard
// cannot rot.
func TestSeverityGuardBypasses(t *testing.T) {
	bypasses := []bypass{
		{
			name: "single-line canonical regression (the original bug)",
			lines: []string{
				"\t\tif d.Severity >= diag.SeverityError {",
			},
			caught: true,
			why:    "the shape the guard was written for",
		},
		{
			name: "comparison split across two lines by gofmt-legal wrapping",
			lines: []string{
				"\t\tif d.Severity >=",
				"\t\t\tdiag.SeverityError {",
			},
			caught: false,
			why:    "the scan is line-based; neither line contains both operator and operand",
		},
		{
			name: "operand on the next line after a binary operator",
			lines: []string{
				"\t\tbroken := d.Resource != \"\" &&",
				"\t\t\td.Severity >=",
				"\t\t\t\tdiag.SeverityError",
			},
			caught: false,
			why:    "same line-based blind spot; `gofmt` will not rejoin these",
		},
		{
			name: "named constant aliasing SeverityError",
			lines: []string{
				"\t\tconst errThreshold = diag.SeverityError",
				"\t\tif d.Severity >= errThreshold {",
			},
			caught: false,
			why:    "the guard keys off the identifier SeverityError, which the comparison no longer mentions",
		},
		{
			name: "variable holding SeverityError",
			lines: []string{
				"\t\tthreshold := diag.SeverityError",
				"\t\tif d.Severity < threshold {",
			},
			caught: false,
			why:    "identifier-based scan defeated by one assignment",
		},
		{
			name: "helper predicate called with SeverityError",
			lines: []string{
				"func atLeast(a, b diag.Severity) bool { return a >= b }",
				"\t\tif atLeast(d.Severity, diag.SeverityError) {",
			},
			caught: false,
			why:    "the broken `>=` lives in a helper whose operands are parameters",
		},
		{
			name: "numeric literal instead of the constant",
			lines: []string{
				"\t\tif d.Severity >= 0 {",
			},
			caught: false,
			why:    "identical bug, zero mention of SeverityError",
		},
		{
			name: "int conversion",
			lines: []string{
				"\t\tif int(d.Severity) >= int(diag.SeverityError) {",
			},
			caught: false,
			why:    "`int(` between the operator and the constant breaks the regex's `>=\\s*SeverityError` shape",
		},
		{
			name: "real comparison masked by an earlier // inside a string literal",
			lines: []string{
				"\t\tif hint := \"see https://wiki/severity\"; d.Severity >= diag.SeverityError {",
			},
			caught: false,
			why:    "comment stripping is naive strings.Index(line, \"//\") and truncates at the URL",
		},
		{
			name: "sort by severity ordinal (descending reads as most-severe-first, is not)",
			lines: []string{
				"\t\tsort.Slice(ds, func(i, j int) bool { return ds[i].Severity > ds[j].Severity })",
			},
			caught: false,
			why:    "no SeverityError token; sorts info-first while reading as errors-first",
		},
		{
			name: "min/max over severities",
			lines: []string{
				"\t\tif d.Severity > worst { worst = d.Severity }",
			},
			caught: false,
			why:    "`worst` is really the least severe; no constant named",
		},
		{
			name: "switch on the ordinal with a numeric range",
			lines: []string{
				"\t\tswitch { case d.Severity <= 0: return true }",
			},
			caught: false,
			why:    "numeric threshold, no constant",
		},
	}

	for _, b := range bypasses {
		t.Run(b.name, func(t *testing.T) {
			caught := false
			for _, line := range b.lines {
				if hasRawSeverityErrorComparison(line) {
					caught = true
				}
			}
			if caught != b.caught {
				t.Fatalf("guard caught=%v, documented caught=%v for %q (%s)", caught, b.caught, b.lines, b.why)
			}
		})
	}
}

// TestSeverityGuardBlockCommentFalsePositive records that the retired matcher
// strips only `//` comments and therefore trips on block-comment prose.
func TestSeverityGuardBlockCommentFalsePositive(t *testing.T) {
	line := "\t/* d.Severity >= diag.SeverityError is true for every severity. */"
	if !hasRawSeverityErrorComparison(line) {
		t.Skip("matcher no longer false-positives on block comments; update this test")
	}
	t.Log("CONFIRMED baseline false positive: block-comment prose trips the retired matcher")
}
