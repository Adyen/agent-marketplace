package config

import (
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkSetHarnessesInConfig measures the TUI harness-modal save path:
// parse the destination document (v2/v3/v4), replace the harnesses
// sequence, write, and reload-validate the result on every save.
func BenchmarkSetHarnessesInConfig(b *testing.B) {
	home := b.TempDir()
	b.Setenv("HOME", home)
	configPath := filepath.Join(home, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"), 0o644); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := SetHarnessesInConfig(configPath, []string{"factory", "claude"}); err != nil {
			b.Fatal(err)
		}
	}
}
