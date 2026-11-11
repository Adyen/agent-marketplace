package analyze

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: marketplace-analysis/unknown-non-utf-8-support-files-receive-a-rejecting-finding
func TestEngineRejectsUnknownNonUTF8Support(t *testing.T) {
	resources := []ir.Resource{
		ir.NewSkill("review", "Review", "Review carefully.", nil, nil, ir.SourceRef{}),
		ir.NewCommand("review", "Review", "Review carefully.", nil, ir.SourceRef{}),
		ir.NewSubagent("review", "Review", "Review carefully.", "", nil, "", nil, "", nil, ir.SourceRef{}),
		ir.NewHook("review", "PreToolUse", "echo ok", "command", nil, ir.SourceRef{}),
		ir.NewMcpServer("review", "server", nil, nil, nil, ir.SourceRef{}),
		ir.NewCollection("review", "1.0.0", "Review", nil, nil, nil, ir.SourceRef{}),
	}
	for _, resource := range resources {
		t.Run(resource.Kind().String(), func(t *testing.T) {
			for _, tc := range []struct {
				name, path, content, validCode string
			}{
				{"plain", "run", "# reviewed\n", ""},
				{"terminal", "run", "# \x1b]8;;https://example.invalid\x07hidden\x1b]8;;\x07\n", FindingTerminalControl},
				{"bidi", "run", "# reviewed \u202e)lin(\n", FindingUnsafeBidi},
				{"encoded", "run", "# ZXZhbChhdG9iKCJoZWxsbyIpKQ==\n", FindingObfuscatedInstruction},
				{"image-extension", "image.png", "# reviewed\n", ""},
				{"font-extension", "font.woff2", "# reviewed\n", ""},
				{"nul", "run", "# \x00\n", FindingTerminalControl},
			} {
				t.Run(tc.name, func(t *testing.T) {
					control, err := NewEngine().Analyze(t.Context(), resource, FileSet{tc.path: []byte(tc.content + "echo ok\n")}, DefaultOptions())
					if err != nil {
						t.Fatal(err)
					}
					if tc.validCode != "" && (!control.Rejected || !hasFinding(control.Findings, tc.validCode, OutcomeReject)) {
						t.Fatalf("valid UTF-8 control lost hidden-content rejection: %+v", control)
					}
					if tc.validCode == "" && control.Rejected {
						t.Fatalf("benign UTF-8 control rejected: %+v", control)
					}
					data := []byte(tc.content + "# \xff\necho ok\n")
					original := bytes.Clone(data)
					report, err := NewEngine().Analyze(t.Context(), resource, FileSet{tc.path: data}, DefaultOptions())
					if err != nil {
						t.Fatal(err)
					}
					if !report.Rejected || !hasFinding(report.Findings, "marketplace.find.invalid-utf8", OutcomeReject) {
						t.Fatalf("invalid UTF-8 support bypassed rejection: %+v", report)
					}
					for _, finding := range report.Findings {
						if finding.Code == "marketplace.find.invalid-utf8" &&
							(finding.SourceRange == nil || finding.SourceRange.File != tc.path ||
								finding.Analyzer == "" || finding.AnalyzerVersion != AnalyzerVersionV1) {
							t.Errorf("invalid UTF-8 finding lost source/analyzer identity: %+v", finding)
						}
					}
					if !bytes.Equal(data, original) {
						t.Fatal("analysis changed original support bytes")
					}
				})
			}
		})
	}
}

// spec: marketplace-analysis/recognized-binaries-retain-their-text-scan-exemption
func TestEngineNonUTF8BinaryExemption(t *testing.T) {
	skill := ir.NewSkill("review", "Review", "Review carefully.", nil, nil, ir.SourceRef{})
	for _, tc := range []struct {
		name, signature string
	}{
		{"ico", "\x00\x00\x01\x00"},
		{"bmp", "BM"},
		{"gif", "GIF89a"},
		{"webp", "RIFF\x00\x00\x00\x00WEBPVP"},
		{"png", "\x89PNG\r\n\x1a\n"},
		{"jpeg", "\xff\xd8\xff"},
		{"ttf", "\x00\x01\x00\x00"},
		{"otf", "OTTO"},
		{"ttc", "ttcf"},
		{"woff", "wOFF"},
		{"woff2", "wOF2"},
		{"eot", strings.Repeat("\x00", 34) + "LP"},
		{"aiff", "FORM\x00\x00\x00\x00AIFF"},
		{"mp3", "ID3"},
		{"ogg", "OggS\x00"},
		{"midi", "MThd\x00\x00\x00\x06"},
		{"avi", "RIFF\x00\x00\x00\x00AVI "},
		{"wav", "RIFF\x00\x00\x00\x00WAVE"},
		{"mp4", "\x00\x00\x00\x10ftypmp42\x00\x00\x00\x00"},
		{"webm", "\x1a\x45\xdf\xa3"},
		{"pdf", "%PDF-"},
		{"gzip", "\x1f\x8b\x08"},
		{"zip", "PK\x03\x04"},
		{"rar", "Rar!\x1a\x07\x00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(tc.signature + "\xff\x1b[31m \u202e ZXZhbChhdG9iKCJoZWxsbyIpKQ==")
			report, err := NewEngine().Analyze(t.Context(), skill, FileSet{"asset": data}, DefaultOptions())
			if err != nil {
				t.Fatal(err)
			}
			if report.Rejected || len(report.Findings) != 0 {
				t.Fatalf("recognized binary produced text findings: %+v", report)
			}
			report, err = NewEngine().Analyze(t.Context(), skill, FileSet{"../asset.exe": data}, DefaultOptions())
			if err != nil {
				t.Fatal(err)
			}
			if !report.Rejected || !hasFinding(report.Findings, FindingExecutableAsset, OutcomeReject) ||
				!hasFinding(report.Findings, FindingAssetPathEscape, OutcomeReject) {
				t.Fatalf("binary exemption disabled independent asset checks: %+v", report)
			}
		})
	}
}

func TestEngineRejectsNonUTF8ActiveAndUnknownFormats(t *testing.T) {
	skill := ir.NewSkill("review", "Review", "Review carefully.", nil, nil, ir.SourceRef{})
	for _, prefix := range []string{
		"", "\x00", "\xff\xfe", "\xfe\xff", "<html>", "<?xml", "<svg>",
		"%!PS-Adobe-", "\x00asm", "\x7fELF", "\xcf\xfa\xed\xfe",
	} {
		t.Run(prefix, func(t *testing.T) {
			report, err := NewEngine().Analyze(t.Context(), skill, FileSet{"asset": []byte(prefix + "\xff")}, DefaultOptions())
			if err != nil {
				t.Fatal(err)
			}
			if !report.Rejected || !hasFinding(report.Findings, "marketplace.find.invalid-utf8", OutcomeReject) {
				t.Fatalf("unrecognized or active format bypassed rejection: %+v", report)
			}
		})
	}
}
