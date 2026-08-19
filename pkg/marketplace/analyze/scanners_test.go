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
		{"del", "hello\x7fworld", 1},
		{"c1_csi", "abc\u009b31mdef", 1},
		{"c1_raw", "abc\x9bdef", 1},
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
		name    string
		content string
		want    int
	}{
		{"plain", "javascript:alert(1)", 1},
		{"data", "data:text/html,<script>", 1},
		{"file", "file:///etc/passwd", 1},
		{"vbscript", "vbscript:msgbox", 1},
		{"https", "https://example.com", 0},
		{"mixedcase", "jAvAsCrIpT:doIt()", 1},
		{"percent", "javascript%3aalert(1)", 1},
		{"htmlhex", "javascript&#x3a;alert(1)", 1},
		{"htmldec", "javascript&#58;alert(1)", 1},
		{"htmlnamed", "javascript&colon;alert(1)", 1},
		{"backslash", "javascript\\x3aalert(1)", 1},
		{"fullpercent", "%6a%61%76%61%73%63%72%69%70%74%3aalert(1)", 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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
		name    string
		content string
		want    int
	}{
		{"dots", "cd ../etc", 1},
		{"tilde", "~/ssh/config", 1},
		{"windows", "C:\\Windows\\System32", 1},
		{"unc", "\\\\server\\share", 1},
		{"passwd", "/etc/passwd", 1},
		{"shadow", "/etc/shadow", 1},
		{"ssh", "~/.ssh", 1},
		{"https", "https://example.com/path", 0},
		{"cdn", "//cdn.example.com/lib.js", 0},
		{"normal", "a normal sentence", 0},
		{"percentdots", "%2e%2e/%2e%2e/etc/passwd", 3},
		{"backslashdots", "\\x2e\\x2e/etc/passwd", 2},
		{"htmldots", "&#x2e;&#x2e;/etc/passwd", 2},
		{"fullpercent", "%2fetc%2fpasswd", 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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
		// Adversarial bypasses.
		{"percent_script", "jav%61script:alert(1)"},
		{"backslash_eval", "ev\\x61l("},
		{"html_url", "javascript&#x3a;alert(1)"},
		{"short_base64", "ZXZhbA"},
		{"upper_hex", "0X6576616c"},
		{"unicode_ws", "eval\u00a0("},
		{"js_comment", "eval/*x*/("},
		{"eval_dot_call", "eval.call(...)"},
		{"window_eval", "window[\"eval\"]"},
		{"setTimeout", "setTimeout(\"...\",0)"},
		{"new_function", "new Function(...)"},
		{"exec_rm", "exec(\"rm -rf /\")"},
		{"cyrillic_eval", "еval"}, // Cyrillic е + Latin val
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
