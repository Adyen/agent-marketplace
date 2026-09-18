package analyze

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeStringDropsInvalidUTF8(t *testing.T) {
	// Mixed valid text with an invalid 0xC0 continuation byte and a lone 0x80.
	in := "hello \xC0world \x80end"
	out := SanitizeString(in, 1024)
	if !utf8.ValidString(out) {
		t.Fatalf("SanitizeString produced invalid UTF-8: %q", out)
	}
	if strings.Contains(out, "\xC0") || strings.Contains(out, "\x80") {
		t.Fatalf("invalid bytes survived sanitization: %q", out)
	}
	if !strings.Contains(out, "hello") || !strings.Contains(out, "world") || !strings.Contains(out, "end") {
		t.Fatalf("valid text dropped: %q", out)
	}
}

func TestSanitizeStringRemovesControlCharacters(t *testing.T) {
	in := "a\x00b\x1Bc\x7Fd\x9De\n"
	out := SanitizeString(in, 1024)
	if strings.ContainsAny(out, "\x00\x01\x1b\x7f\n") || strings.ContainsRune(out, '\u009d') {
		t.Fatalf("control characters survived: %q", out)
	}
	want := "abcde"
	if out != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

func TestSanitizeStringBoundsWithoutSplittingRunes(t *testing.T) {
	// Repeat a 4-byte rune so the max-3 boundary falls inside a rune.
	rune4 := "\U0010FF00" // 4-byte rune
	in := strings.Repeat(rune4, 100)
	max := 13 // 3 bytes for ellipsis + room for one full 4-byte rune (4) => 7? use 13 to get 2 runes + ...
	out := SanitizeString(in, max)
	if !utf8.ValidString(out) {
		t.Fatalf("truncated output is not valid UTF-8: %q", out)
	}
	if len(out) > max {
		t.Fatalf("output length %d exceeds max %d", len(out), max)
	}
	if !strings.HasSuffix(out, "...") {
		t.Fatalf("truncated output should end with ellipsis: %q", out)
	}
	// Ensure no rune was split.
	for i := 0; i < len(out)-3; {
		r, size := utf8.DecodeRuneInString(out[i:])
		if r == utf8.RuneError && size == 1 {
			t.Fatalf("split rune at byte %d: %q", i, out)
		}
		i += size
	}
}

func TestSanitizeStringMaxZero(t *testing.T) {
	if got := SanitizeString("anything", 0); got != "" {
		t.Fatalf("max 0 should produce empty string, got %q", got)
	}
}

func TestSanitizeStringSmallMax(t *testing.T) {
	for _, max := range []int{1, 2, 3} {
		got := SanitizeString("hello", max)
		if len(got) > max {
			t.Fatalf("max %d produced %q with length %d", max, got, len(got))
		}
		want := strings.Repeat(".", max)
		if got != want {
			t.Fatalf("max %d should produce %q, got %q", max, want, got)
		}
	}
}

func TestSanitizeAudit(t *testing.T) {
	falseVal := false
	f := Finding{
		Code:    "test.code",
		Message: "bad\x00msg",
		SourceRange: &SourceRange{
			File:   "path/with\x1bescape",
			Line:   1,
			Column: 2,
		},
		Consentable: &falseVal,
	}
	audit := Audit{
		Summary:  "sum\x80mary",
		Error:    "err\xC0or",
		Findings: []Finding{f},
	}
	sanitized := sanitizeAudit(audit)
	if strings.ContainsRune(sanitized.Summary, 0) || strings.ContainsRune(sanitized.Summary, '\u0080') {
		t.Fatalf("summary not sanitized: %q", sanitized.Summary)
	}
	if strings.ContainsRune(sanitized.Error, '\u00c0') {
		t.Fatalf("error not sanitized: %q", sanitized.Error)
	}
	if len(sanitized.Findings) != 1 {
		t.Fatalf("findings count changed: %d", len(sanitized.Findings))
	}
	if strings.ContainsAny(sanitized.Findings[0].Message, "\x00") {
		t.Fatalf("finding message not sanitized: %q", sanitized.Findings[0].Message)
	}
	if strings.ContainsAny(sanitized.Findings[0].SourceRange.File, "\x1b") {
		t.Fatalf("source range file not sanitized: %q", sanitized.Findings[0].SourceRange.File)
	}
	if sanitized.Findings[0].Consentable == nil || *sanitized.Findings[0].Consentable {
		t.Fatalf("consentable flag should be preserved as false")
	}
}

func TestSanitizeAuditBoundsSummaryAndMessage(t *testing.T) {
	audit := Audit{
		Summary: strings.Repeat("x", MaxAuditSummaryBytes+10),
		Error:   strings.Repeat("y", MaxAuditErrorBytes+10),
		Findings: []Finding{{
			Code:    "test.code",
			Message: strings.Repeat("z", MaxFindingMessageBytes+10),
		}},
	}
	sanitized := sanitizeAudit(audit)
	if len(sanitized.Summary) > MaxAuditSummaryBytes {
		t.Fatalf("summary length %d exceeds max %d", len(sanitized.Summary), MaxAuditSummaryBytes)
	}
	if len(sanitized.Error) > MaxAuditErrorBytes {
		t.Fatalf("error length %d exceeds max %d", len(sanitized.Error), MaxAuditErrorBytes)
	}
	if len(sanitized.Findings[0].Message) > MaxFindingMessageBytes {
		t.Fatalf("message length %d exceeds max %d", len(sanitized.Findings[0].Message), MaxFindingMessageBytes)
	}
}

func TestSanitizeFindingNormalizesConsentable(t *testing.T) {
	trueVal := true
	falseVal := false

	nilResult := sanitizeFinding(Finding{Consentable: nil})
	if nilResult.Consentable != nil {
		t.Fatalf("nil consentable should remain nil, got %v", *nilResult.Consentable)
	}

	trueResult := sanitizeFinding(Finding{Consentable: &trueVal})
	if trueResult.Consentable != nil {
		t.Fatalf("explicit true consentable should normalize to nil, got %v", *trueResult.Consentable)
	}

	falseResult := sanitizeFinding(Finding{Consentable: &falseVal})
	if falseResult.Consentable == nil || *falseResult.Consentable != false {
		t.Fatalf("false consentable should be preserved as false")
	}
	if falseResult.Consentable == &falseVal {
		t.Fatalf("false consentable should be deep-cloned, not shared with input")
	}
}

func TestCloneFindingDeepClonesConsentable(t *testing.T) {
	falseVal := false
	f := Finding{
		Code:        "test.code",
		Severity:    "info",
		Message:     "message",
		SourceRange: &SourceRange{File: "file", Line: 1, Column: 2},
		Consentable: &falseVal,
	}
	cloned := cloneFinding(f)

	if cloned.Consentable == f.Consentable {
		t.Fatalf("cloneFinding should not share Consentable pointer")
	}
	if cloned.Consentable == nil || *cloned.Consentable != false {
		t.Fatalf("cloneFinding should preserve false consentable")
	}

	trueVal := true
	f.Consentable = &trueVal
	clonedTrue := cloneFinding(f)
	if clonedTrue.Consentable != nil {
		t.Fatalf("cloneFinding should normalize true consentable to nil")
	}

	if cloned.SourceRange == f.SourceRange {
		t.Fatalf("cloneFinding should not share SourceRange pointer")
	}
}

func TestSanitizeStringSmallMaxMultibyte(t *testing.T) {
	// Use a 4-byte rune repeated enough times that a max of 4 leaves no room for
	// a full rune before the 3-byte ellipsis.
	rune4 := "\U0010FF00"
	tests := []struct {
		max  int
		want string
	}{
		{max: 1, want: "."},
		{max: 2, want: ".."},
		{max: 3, want: "..."},
		{max: 4, want: "..."},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("max%d", tc.max), func(t *testing.T) {
			in := rune4 + "extra"
			out := SanitizeString(in, tc.max)
			if !utf8.ValidString(out) {
				t.Fatalf("max %d produced invalid UTF-8: %q", tc.max, out)
			}
			if len(out) > tc.max {
				t.Fatalf("max %d produced %q with length %d", tc.max, out, len(out))
			}
			if out != tc.want {
				t.Fatalf("max %d produced %q, want %q", tc.max, out, tc.want)
			}
		})
	}
}
