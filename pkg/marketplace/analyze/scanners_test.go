package analyze

import (
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestScanHTMLComments(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    int
	}{
		{"single", "before <!-- hidden --> after", 1},
		{"multiline", "<!--\nsecret\n-->", 1},
		{"none", "no comments here", 0},
		{"two", "<!-- one --> <!-- two -->", 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ScanHTMLComments(tc.content, "file", "test")
			if len(got) != tc.want {
				t.Fatalf("got %d findings, want %d", len(got), tc.want)
			}
			for _, f := range got {
				if f.Code != FindingHTMLComment {
					t.Errorf("code = %q, want %q", f.Code, FindingHTMLComment)
				}
			}
		})
	}
}

func TestScanZeroWidth(t *testing.T) {
	content := "visible" + string('\u200B') + string('\u200D') + "after"
	got := ScanZeroWidth(content, "file", "test")
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1", len(got))
	}
	if got[0].Code != FindingZeroWidth {
		t.Errorf("code = %q", got[0].Code)
	}
	if got[0].SourceRange == nil || got[0].SourceRange.Line != 1 || got[0].SourceRange.Column != 8 {
		t.Errorf("unexpected range %+v", got[0].SourceRange)
	}
}

func TestScanUnsafeBidi(t *testing.T) {
	content := "left-to-right" + string('\u202E') + "override"
	got := ScanUnsafeBidi(content, "file", "test")
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1", len(got))
	}
	if got[0].Code != FindingUnsafeBidi {
		t.Errorf("code = %q", got[0].Code)
	}
}

func TestScanTerminalControl(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    int
	}{
		{"bell", "hello\x07world", 1},
		{"ansi", "\x1b[31mred\x1b[0m", 2},
		{"osc", "\x1b]0;title\x07", 1},
		{"clean", "hello\tworld\n", 0},
		{"c0", "\x00\x01\x02", 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ScanTerminalControl(tc.content, "file", "test")
			if len(got) != tc.want {
				t.Fatalf("got %d findings, want %d: %v", len(got), tc.want, got)
			}
		})
	}
}

func TestScanDangerousURLs(t *testing.T) {
	cases := []struct {
		content string
		want    int
	}{
		{"javascript:alert(1)", 1},
		{"data:text/html,<script>", 1},
		{"file:///etc/passwd", 1},
		{"vbscript:msgbox", 1},
		{"https://example.com", 0},
		{"jAvAsCrIpT:doIt()", 1},
	}

	for _, tc := range cases {
		t.Run(tc.content, func(t *testing.T) {
			got := ScanDangerousURLs(tc.content, "file", "test")
			if len(got) != tc.want {
				t.Fatalf("got %d findings, want %d", len(got), tc.want)
			}
		})
	}
}

func TestScanRemoteReferences(t *testing.T) {
	cases := []struct {
		content string
		want    int
	}{
		{"https://example.com", 1},
		{"http://example.com", 1},
		{"ftp://files.example.com", 1},
		{"//cdn.example.com/lib.js", 1},
		{"// comment", 0},
		{"http://example.com//path", 2},
		{"no urls", 0},
	}

	for _, tc := range cases {
		t.Run(tc.content, func(t *testing.T) {
			got := ScanRemoteReferences(tc.content, "file", "test")
			if len(got) != tc.want {
				t.Fatalf("got %d findings, want %d", len(got), tc.want)
			}
		})
	}
}

func TestScanSuspiciousPaths(t *testing.T) {
	cases := []struct {
		content string
		want    int
	}{
		{"cd ../etc", 1},
		{"~/ssh/config", 1},
		{"C:\\Windows\\System32", 1},
		{"\\\\server\\share", 1},
		{"/etc/passwd", 1},
		{"/etc/shadow", 1},
		{"~/.ssh", 1},
		{"https://example.com/path", 0},
		{"//cdn.example.com/lib.js", 0},
		{"a normal sentence", 0},
	}

	for _, tc := range cases {
		t.Run(tc.content, func(t *testing.T) {
			got := ScanSuspiciousPaths(tc.content, "file", "test")
			if len(got) != tc.want {
				t.Fatalf("got %d findings, want %d: %v", len(got), tc.want, got)
			}
		})
	}
}

func TestScanObfuscatedInstructions(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"eval", "eval("},
		{"system", "system("},
		{"base64", base64Of("javascript:alert(1)")},
		{"hex", hexOf("eval")},
		{"percent", "%65%76%61%6c"},
		{"htmlentity", "&#x65;&#x76;&#x61;&#x6c;"},
		{"backslash", "\\x65\\x76\\x61\\x6c"},
		{"homoglyph", "Аpple"}, // Cyrillic А + Latin pple
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ScanObfuscatedInstructions(tc.content, "file", "test")
			if len(got) == 0 {
				t.Fatalf("expected at least one finding, got none")
			}
			for _, f := range got {
				if f.Code != FindingObfuscatedInstruction {
					t.Errorf("code = %q, want %q", f.Code, FindingObfuscatedInstruction)
				}
			}
		})
	}
}

func base64Of(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func hexOf(s string) string {
	return "0x" + hex.EncodeToString([]byte(s))
}
