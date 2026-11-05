package analyze

import (
	"reflect"
	"strings"
	"testing"
)

func TestUnterminatedOSCRetainsEveryControlFinding(t *testing.T) {
	control := ScanTerminalControl("\x1b", "fixture.md", "fixture")
	if len(control) != 1 {
		t.Fatalf("single control findings = %v", control)
	}
	for _, count := range []int{1, 64, 512} {
		content := strings.Repeat("\u00e9\x1b]inert\n", count)
		findings := ScanTerminalControl(content, "fixture.md", "fixture")
		if len(findings) != count {
			t.Fatalf("count %d: got %d findings", count, len(findings))
		}
		for i, finding := range findings {
			want := control[0]
			want.SourceRange = &SourceRange{File: "fixture.md", Line: i + 1, Column: 2}
			if !reflect.DeepEqual(finding, want) {
				t.Fatalf("count %d, finding %d: got %+v, want %+v", count, i, finding, want)
			}
		}
	}
}

func TestOSCTerminationAndInvocationIsolation(t *testing.T) {
	for _, terminator := range []string{"\x07", "\x1b\\"} {
		content := "\u00e9" + strings.Repeat("\x1b]inert", 64) + terminator + "plain"
		findings := ScanTerminalControl(content, "fixture.md", "fixture")
		if len(findings) != 1 || !strings.HasPrefix(findings[0].Message, "OSC escape sequence") {
			t.Fatalf("terminated OSC findings = %+v", findings)
		}
		if got := findings[0].SourceRange; got == nil || got.Line != 1 || got.Column != 2 {
			t.Fatalf("terminated OSC range = %+v", got)
		}
	}
	for _, content := range []string{
		"\x1b]inert plain\x1b[31mplain\x00plain\x85",
		"\x1b]inert",
		"\x1b]title\x07",
		"ordinary text",
		"\x1b]inert",
	} {
		first := ScanTerminalControl(content, "fixture.md", "fixture")
		_ = ScanTerminalControl("\x1b]unterminated", "other.md", "other")
		second := ScanTerminalControl(content, "fixture.md", "fixture")
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("scan state leaked between invocations: %q", content)
		}
	}
}

func TestUnterminatedOSCDoesNotHideOtherTerminalControls(t *testing.T) {
	content := "\x1b]inert \x1b[31mplain \x00plain \x85"
	findings := ScanTerminalControl(content, "fixture.md", "fixture")
	if len(findings) != 4 {
		t.Fatalf("mixed control findings = %+v", findings)
	}
	for i, control := range []struct {
		token, prefix string
	}{
		{"\x1b]", "control character"},
		{"\x1b[31m", "ANSI/CSI escape sequence"},
		{"\x00", "control character"},
		{"\x85", "control character"},
	} {
		location := findings[i].SourceRange
		if !strings.HasPrefix(findings[i].Message, control.prefix) || location == nil ||
			location.Line != 1 || location.Column != strings.Index(content, control.token)+1 {
			t.Fatalf("control %d = %+v at %+v", i, findings[i], location)
		}
	}
}
