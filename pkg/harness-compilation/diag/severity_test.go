package diag

import "testing"

// TestSeverityOrdinalsAreWireStable pins the ordinal values. They are
// wire-visible and consumer UIs key off codes.md, so the fix for a
// wrong-direction comparison is never to renumber these.
func TestSeverityOrdinalsAreWireStable(t *testing.T) {
	for _, tt := range []struct {
		severity Severity
		want     int
	}{
		{SeverityError, 0},
		{SeverityWarning, 1},
		{SeverityInfo, 2},
	} {
		if got := int(tt.severity); got != tt.want {
			t.Fatalf("%v ordinal = %d, want %d: severity ordinals are wire-visible and must not be renumbered", tt.severity, got, tt.want)
		}
	}
}

func TestSeverityAtLeastAsSevereAs(t *testing.T) {
	tests := []struct {
		name      string
		severity  Severity
		threshold Severity
		want      bool
	}{
		{name: "error meets error", severity: SeverityError, threshold: SeverityError, want: true},
		{name: "warning does not meet error", severity: SeverityWarning, threshold: SeverityError, want: false},
		{name: "info does not meet error", severity: SeverityInfo, threshold: SeverityError, want: false},

		{name: "error meets warning", severity: SeverityError, threshold: SeverityWarning, want: true},
		{name: "warning meets warning", severity: SeverityWarning, threshold: SeverityWarning, want: true},
		{name: "info does not meet warning", severity: SeverityInfo, threshold: SeverityWarning, want: false},

		{name: "error meets info", severity: SeverityError, threshold: SeverityInfo, want: true},
		{name: "warning meets info", severity: SeverityWarning, threshold: SeverityInfo, want: true},
		{name: "info meets info", severity: SeverityInfo, threshold: SeverityInfo, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.severity.AtLeastAsSevereAs(tt.threshold); got != tt.want {
				t.Fatalf("%v.AtLeastAsSevereAs(%v) = %v, want %v", tt.severity, tt.threshold, got, tt.want)
			}
		})
	}
}

func TestDiagnosticIsError(t *testing.T) {
	tests := []struct {
		severity Severity
		want     bool
	}{
		{severity: SeverityError, want: true},
		{severity: SeverityWarning, want: false},
		{severity: SeverityInfo, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.severity.String(), func(t *testing.T) {
			d := Diagnostic{Code: CodeSubagentDevinEffortDropped, Severity: tt.severity}
			if got := d.IsError(); got != tt.want {
				t.Fatalf("Diagnostic{Severity: %v}.IsError() = %v, want %v", tt.severity, got, tt.want)
			}
		})
	}
}

// TestZeroDiagnosticIsError documents the footgun that motivated the predicate:
// the zero Diagnostic is an error, which is exactly why an ordinal `>=
// SeverityError` comparison is meaningless.
func TestZeroDiagnosticIsError(t *testing.T) {
	if !(Diagnostic{}).IsError() {
		t.Fatal("zero Diagnostic.IsError() = false, want true: SeverityError is the zero value of Severity")
	}
}

func TestHasErrorsUsesTheSamePredicate(t *testing.T) {
	tests := []struct {
		name        string
		diagnostics []Diagnostic
		want        bool
	}{
		{name: "nil", diagnostics: nil, want: false},
		{name: "warnings only", diagnostics: []Diagnostic{{Severity: SeverityWarning}, {Severity: SeverityInfo}}, want: false},
		{name: "one error", diagnostics: []Diagnostic{{Severity: SeverityWarning}, {Severity: SeverityError}}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasErrors(tt.diagnostics); got != tt.want {
				t.Fatalf("HasErrors(%+v) = %v, want %v", tt.diagnostics, got, tt.want)
			}
		})
	}
}
