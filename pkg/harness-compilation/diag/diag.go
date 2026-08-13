// Package diag defines structured diagnostics emitted during compilation of
// AI-agent configuration resources. Each diagnostic carries a machine-readable
// Code, a Severity, and an optional Hint for remediation.
package diag

// Severity classifies the gravity of a diagnostic.
type Severity int

const (
	SeverityError   Severity = iota // Source is malformed or violates safety
	SeverityWarning                 // Resource translated lossily or dropped
	SeverityInfo                    // Translation succeeded with notable transformation
)

// String returns a human-readable label for the severity.
func (s Severity) String() string {
	switch s {
	case SeverityError:
		return "error"
	case SeverityWarning:
		return "warning"
	case SeverityInfo:
		return "info"
	default:
		return "unknown"
	}
}

// AtLeastAsSevereAs reports whether s is at least as grave as threshold.
//
// The name says "severe", not "at least", on purpose: the Severity ordinals
// count DOWN in gravity (SeverityError is the ZERO value, then SeverityWarning,
// then SeverityInfo), so an ordinal reading of the name would be backwards. A
// raw `s >= SeverityError` reads like "is an error" but is true for EVERY
// severity, and `s < SeverityError` is never true. Express severity thresholds
// through this method (or Diagnostic.IsError) so the ordinal direction lives in
// exactly one place.
//
// The ordinals are wire-visible and consumer UIs key off diag/codes.md, so they
// must not be reordered to make comparisons read naturally.
func (s Severity) AtLeastAsSevereAs(threshold Severity) bool {
	switch s {
	case SeverityError, SeverityWarning, SeverityInfo:
		switch threshold {
		case SeverityError, SeverityWarning, SeverityInfo:
			return s <= threshold
		}
	}
	// Out-of-range severities are treated as errors (fail-closed).
	return true
}

// SourceRef points to the original source location of a diagnostic.
// This is a local type to keep the diag package independent of the ir package
// and avoid circular imports.
type SourceRef struct {
	File string // File path relative to the bundle root
	Line int    // 1-based line number, 0 if unknown
}

// Diagnostic represents a structured diagnostic emitted during compilation.
type Diagnostic struct {
	Code     string    // Machine-readable code, e.g. "subagent.windsurf.no-primitive"
	Severity Severity  // Error, Warning, or Info
	Target   string    // Target harness name (optional, empty if target-agnostic)
	Resource string    // Resource name that produced this diagnostic
	Message  string    // Human-readable description
	Hint     string    // Suggested remediation (optional)
	Source   SourceRef // Original source location
}

// HasCode reports whether diagnostics contain an entry with the given stable
// diagnostic code. Consumer UIs and tests should key off codes rather than
// matching free-text messages.
func HasCode(diagnostics []Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

// IsError reports whether the diagnostic is a fail-closed error.
//
// Use this instead of comparing Severity ordinals: SeverityError is the ZERO
// value of Severity, so `d.Severity >= SeverityError` is true for every
// diagnostic and `d.Severity < SeverityError` is true for none.
func (d Diagnostic) IsError() bool {
	return d.Severity.AtLeastAsSevereAs(SeverityError)
}

// HasErrors reports whether diagnostics contain a fail-closed error.
func HasErrors(diagnostics []Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.IsError() {
			return true
		}
	}
	return false
}
