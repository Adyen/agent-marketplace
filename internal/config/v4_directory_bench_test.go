package config

import (
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkV4RepositoryDirectory(b *testing.B) {
	repo := b.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".agents"), 0o755); err != nil {
		b.Fatal(err)
	}
	file := filepath.Join(repo, ".agents", "config.yaml")
	data := []byte("apiVersion: agent-marketplace/v4\nsources: {registry: {directory: ./resources/skills}}\nresources: {registry: {all: true}}\n")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := ParseV4Config(data, file, true); err != nil {
			b.Fatal(err)
		}
	}
}
