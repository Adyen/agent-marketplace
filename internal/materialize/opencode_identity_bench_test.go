package materialize

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/ownership"
)

// BenchmarkOpenCodeIdentityReadDisk includes disk I/O on a small, regular
// configuration. The legacy reader is a retrospective microbenchmark control,
// not a safe production alternative or an end-to-end lifecycle baseline.
func BenchmarkOpenCodeIdentityReadDisk(b *testing.B) {
	home, repo := b.TempDir(), b.TempDir()
	b.Setenv("HOME", home)
	file := filepath.Join(repo, "opencode.json")
	content := []byte(`{"command":{"manual":{"template":"Handwritten"}}}`)
	if err := os.WriteFile(file, content, 0o600); err != nil {
		b.Fatal(err)
	}
	for _, variant := range []string{"legacy-path-read", "captured-read"} {
		b.Run(variant, func(b *testing.B) {
			read := func() ([]byte, error) { return ownership.ReadAggregateFile(file) }
			if variant == "captured-read" {
				read = func() ([]byte, error) { return readOpenCodeIdentityConfig(repo, file) }
			}
			b.ReportAllocs()
			for b.Loop() {
				data, err := read()
				if err != nil || len(data) != len(content) {
					b.Fatalf("identity read: %v", err)
				}
			}
		})
	}
}
