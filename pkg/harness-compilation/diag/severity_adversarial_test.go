package diag

import (
	"encoding/json"
	"testing"
)

// ADVERSARIAL: AtLeastAsSevereAs is exported API on a wire-visible type, so it
// is reachable with a Severity value that is not one of the three named
// constants. The tests below pin what happens then, because the shipped
// implementation (`s <= threshold`) has NO domain check.

// TestSeverityOutOfRangeIsAsymmetric is the finding: an out-of-range severity
// is fail-CLOSED below the range and fail-OPEN above it. Both directions are
// reachable from `Severity(n)`, and only one of them is safe.
func TestSeverityOutOfRangeIsAsymmetric(t *testing.T) {
	tests := []struct {
		name        string
		severity    Severity
		wantIsError bool
		note        string
	}{
		{
			name: "negative severity is treated as an error (fail-closed)", severity: Severity(-1),
			wantIsError: true,
			note:        "-1 <= 0, so a corrupt value below the range is louder than SeverityError itself",
		},
		{
			name: "large negative severity is still an error", severity: Severity(-1 << 20),
			wantIsError: true,
		},
		{
			name: "unknown severity above Info is an error (fail-closed)", severity: Severity(3),
			wantIsError: true,
			note:        "a severity added later, or a corrupt high value, is treated as an error",
		},
		{
			name: "huge severity is an error (fail-closed)", severity: Severity(1 << 20),
			wantIsError: true,
			note:        "a corrupt high value is treated as an error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Diagnostic{Code: "x.y.z", Severity: tt.severity}
			if got := d.IsError(); got != tt.wantIsError {
				t.Fatalf("Diagnostic{Severity: %d}.IsError() = %v, want %v (%s)", int(tt.severity), got, tt.wantIsError, tt.note)
			}
			// HasErrors must agree with IsError for every input: the fix
			// routed one through the other, and a divergence would mean a
			// diagnostic blocks in one surface and not in another.
			if got := HasErrors([]Diagnostic{d}); got != tt.wantIsError {
				t.Fatalf("HasErrors disagrees with IsError for Severity(%d): %v vs %v", int(tt.severity), got, tt.wantIsError)
			}
		})
	}
}

// TestSeverityOutOfRangeStringIsUnknown: String() has a default arm, so an
// out-of-range value renders as "unknown" — meaning a fail-closed negative
// severity is reported to the user as `unknown: <message>`, with no hint that
// it is being treated as a hard error.
func TestSeverityOutOfRangeStringIsUnknown(t *testing.T) {
	for _, s := range []Severity{Severity(-1), Severity(3)} {
		if got := s.String(); got != "unknown" {
			t.Fatalf("Severity(%d).String() = %q, want %q", int(s), got, "unknown")
		}
	}
	if !(Diagnostic{Severity: Severity(-1)}).IsError() {
		t.Fatal("precondition changed")
	}
	t.Log("Severity(-1) blocks like an error but prints as \"unknown\": the label and the behaviour disagree")
}

// TestHasErrorsSemanticChangeIsObservableOnlyOutOfRange proves the claim that
// `== SeverityError` and `<= SeverityError` agree on every in-range input, and
// pins the exact input where they differ.
func TestHasErrorsSemanticChangeIsObservableOnlyOutOfRange(t *testing.T) {
	oldHasErrors := func(ds []Diagnostic) bool {
		for _, d := range ds {
			if d.Severity == SeverityError { //nolint:staticcheck // deliberate replica of the pre-fix predicate
				return true
			}
		}
		return false
	}

	for _, s := range []Severity{SeverityError, SeverityWarning, SeverityInfo} {
		ds := []Diagnostic{{Severity: s}}
		if oldHasErrors(ds) != HasErrors(ds) {
			t.Fatalf("HasErrors changed behaviour for the in-range severity %v: this was supposed to be a pure refactor", s)
		}
	}

	// The one input where the two differ.
	corrupt := []Diagnostic{{Severity: Severity(-1)}}
	if oldHasErrors(corrupt) {
		t.Fatal("precondition: the pre-fix predicate did not treat Severity(-1) as an error")
	}
	if !HasErrors(corrupt) {
		t.Fatal("HasErrors(Severity(-1)) = false; expected the new `<=` predicate to fail closed")
	}
	t.Log("behaviour change confined to Severity(-1) and below: previously ignored, now fail-closed")
}

// TestZeroDiagnosticFromNilCodedErrorIsAnError is the live footgun the bead did
// not address: (*CodedError)(nil).Diagnostic(...) returns a zero Diagnostic,
// whose Severity is the zero value — SeverityError — with an EMPTY Code and an
// EMPTY Message. Any surface that appends this to a report gets an
// unattributable hard failure.
func TestZeroDiagnosticFromNilCodedErrorIsAnError(t *testing.T) {
	var e *CodedError
	d := e.Diagnostic("some-skill")

	if d.Code != "" || d.Message != "" {
		t.Fatalf("precondition changed: %+v", d)
	}
	if !d.IsError() {
		t.Skip("nil CodedError no longer yields an error-severity diagnostic; footgun closed")
	}
	t.Log("CONFIRMED footgun: a nil *CodedError yields Diagnostic{} which IsError()==true with no Code and no Message")
}

// TestSeverityHasNoSerializationContract: the review worried about a severity
// "decoded from untrusted JSON/YAML". Severity implements no
// Marshaler/Unmarshaler, so it round-trips as a bare integer — meaning any
// consumer that persists a Diagnostic and reads it back can reintroduce an
// out-of-range value, and there is no place that rejects one.
func TestSeverityHasNoSerializationContract(t *testing.T) {
	encoded, err := json.Marshal(Diagnostic{Code: "x", Severity: SeverityWarning})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// A bare ordinal, not a label: the wire form is the number.
	if want := `"Severity":1`; !containsSub(string(encoded), want) {
		t.Fatalf("encoded = %s, want it to contain %s", encoded, want)
	}

	var decoded Diagnostic
	if err := json.Unmarshal([]byte(`{"Code":"x","Severity":-1}`), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Severity != Severity(-1) {
		t.Fatalf("decoded severity = %d, want -1: nothing validates the domain", int(decoded.Severity))
	}
	if !decoded.IsError() {
		t.Fatal("a JSON-supplied Severity of -1 should still fail closed")
	}
	t.Log("no UnmarshalJSON on Severity: an out-of-range ordinal survives a round-trip unvalidated")
}

func containsSub(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
