package telemetry

import (
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkLoadInstallationID(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "installation-id")

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("existing-id\n"), 0o600); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := LoadInstallationID(path); err != nil {
			b.Fatal(err)
		}
	}
}
