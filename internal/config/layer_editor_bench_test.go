package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func BenchmarkEditV4ResourceSelection(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := []byte("apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: [skill/alpha]\n")
	if err := os.WriteFile(path, data, 0o640); err != nil {
		b.Fatal(err)
	}
	entry := RegistryEntry{URL: "https://example.test/repo.git", Ref: "main"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := EditV4ResourceSelection(path, path, entry, ir.KindSkill, "alpha", V4SelectionEnable); err != nil {
			b.Fatal(err)
		}
	}
}
