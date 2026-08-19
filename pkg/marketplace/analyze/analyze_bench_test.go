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
