package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkCatalogSourcePathRule measures the disk-backed workspace/direct
// source disambiguation. Its timed path intentionally includes os.Stat calls.
func BenchmarkCatalogSourcePathRule(b *testing.B) {
	root := b.TempDir()
	rule := filepath.Join(root, ".devin", "rules", "secure-coding.md")
	if err := os.MkdirAll(filepath.Dir(rule), 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(rule, []byte("body\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if got := catalogSourcePath(root, HarnessDevin, filepath.Join("rules", "secure-coding.md")); got != rule {
			b.Fatalf("catalogSourcePath = %q, want %q", got, rule)
		}
	}
}
