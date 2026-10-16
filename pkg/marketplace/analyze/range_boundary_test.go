package analyze

import (
	"strings"
	"testing"
)

// spec: marketplace-analysis/dense-comment-findings-retain-ordered-rune-locations
func TestDenseCommentFindingsRetainOrderedRuneLocations(t *testing.T) {
	const count = 64
	findings := ScanHTMLComments(strings.Repeat("é<!-- inert note -->\n", count), "inert.md", "fixture")
	if len(findings) != count {
		t.Fatalf("finding count = %d, want %d", len(findings), count)
	}
	for index, finding := range findings {
		if finding.Code != FindingHTMLComment || finding.Analyzer != "fixture" || finding.SourceRange == nil || finding.SourceRange.File != "inert.md" || finding.SourceRange.Line != index+1 || finding.SourceRange.Column != 2 {
			t.Fatalf("finding %d changed identity/order/location: %+v", index, finding)
		}
	}
}

func TestOrderedCharacterScannersRetainRuneLocations(t *testing.T) {
	for _, tc := range []struct {
		name, content, code string
		scan                func(string, string, string) []Finding
	}{
		{"zero-width", "é\u200b x\n🙂\u200b x", FindingZeroWidth, ScanZeroWidth},
		{"bidi", "é\u202e x\n🙂\u202e x", FindingUnsafeBidi, ScanUnsafeBidi},
		{"terminal", "é\x1b[31m x\n🙂\x1b[31m x", FindingTerminalControl, ScanTerminalControl},
	} {
		t.Run(tc.name, func(t *testing.T) {
			findings := tc.scan(tc.content, "inert.md", "fixture")
			if len(findings) != 2 {
				t.Fatalf("finding count = %d, want 2", len(findings))
			}
			for index, finding := range findings {
				if finding.Code != tc.code || finding.SourceRange == nil || finding.SourceRange.Line != index+1 || finding.SourceRange.Column != 2 {
					t.Fatalf("ordered location changed: %+v", finding)
				}
			}
		})
	}
}

func TestStandaloneSourceRangePreservesOffsetBoundaries(t *testing.T) {
	for _, tc := range []struct {
		content              string
		offset, line, column int
	}{
		{"é\nz", -1, 0, 0}, {"é\nz", 0, 1, 1}, {"é\nz", 1, 1, 2}, {"é\nz", 2, 1, 2},
		{"é\nz", 3, 2, 1}, {"é\nz", 4, 2, 2}, {"é\nz", 5, 0, 0}, {"", 0, 1, 1}, {"\xff\na", 2, 2, 1},
	} {
		location := SourceRangeAt("inert.md", tc.content, tc.offset)
		if location.File != "inert.md" || location.Line != tc.line || location.Column != tc.column {
			t.Fatalf("offset %d changed: %+v, want %d:%d", tc.offset, location, tc.line, tc.column)
		}
	}
}
