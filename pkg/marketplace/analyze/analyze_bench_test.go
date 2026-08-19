package analyze

import (
	"context"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func BenchmarkEngineAnalyzeSkill(b *testing.B) {
	opts := DefaultOptions()
	s := ir.NewSkill("review", "Review", "Review the code carefully.", []string{"user"}, nil, ir.SourceRef{})
	s.AllowedTools = []string{"read_file"}
	files := FileSet{"README.md": []byte("# Read me\n")}
	engine := NewEngine()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := engine.Analyze(context.Background(), s, files, opts)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkScanObfuscatedInstructions(b *testing.B) {
	content := "eval(" + base64Of("javascript:alert(1)") + "/etc/passwd"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ScanObfuscatedInstructions(content, "file", "test")
	}
}

func BenchmarkScanSuspiciousPaths(b *testing.B) {
	content := "cd ../etc\n~/ssh/config\nC:\\Windows\\System32\n//cdn.example.com"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ScanSuspiciousPaths(content, "file", "test")
	}
}

func BenchmarkScanDangerousURLs(b *testing.B) {
	content := "safe text https://example.com javascript%3aalert(1) data&#x3a;text/html"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ScanDangerousURLs(content, "file", "test")
	}
}

func BenchmarkScanTerminalControl(b *testing.B) {
	content := "hello\tworld\n\x1b[31mred\x1b[0m \x7f"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ScanTerminalControl(content, "file", "test")
	}
}

func BenchmarkExtractResourceSamples(b *testing.B) {
	extra := map[string]any{
		"yamlMap": map[any]any{
			"nested": map[any]any{
				"answer": "42",
			},
		},
		"raw":  []byte("byte content"),
		"list": []string{"a", "b", "c"},
	}
	s := ir.NewSkill("review", "Review", "Body", []string{"user"}, extra, ir.SourceRef{})
	s.AllowedTools = []string{"read_file"}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = extractResourceSamples(s)
	}
}

func BenchmarkScanAssets(b *testing.B) {
	files := FileSet{
		"clean.txt":    []byte("hello world"),
		"script.sh":    []byte("echo hi"),
		"app.exe":      []byte("not a binary"),
		"lib.so.1":     []byte{0x00},
		"../traversal": []byte("bad"),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ScanAssets(files, "test")
	}
}

func BenchmarkTruncateFindings(b *testing.B) {
	findings := make([]Finding, 1000)
	for i := range findings {
		switch i % 4 {
		case 0:
			findings[i] = Finding{Code: FindingExecutableAsset, Severity: OutcomeReject, PolicyOutcome: OutcomeReject}
		case 1, 2:
			findings[i] = Finding{Code: FindingRemoteReference, Severity: OutcomeWarn, PolicyOutcome: OutcomeWarn}
		default:
			findings[i] = Finding{Code: FindingToolGrant, Severity: "info", PolicyOutcome: OutcomeAllow}
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = truncateFindings(findings, 100)
	}
}
