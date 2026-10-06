package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkOrdinaryRetentionDisk(b *testing.B) {
	home := b.TempDir()
	b.Setenv("HOME", home)
	dir := filepath.Join(home, ".agent-marketplace", "backups", "factory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		for day := 1; day <= 7; day++ {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("2026-01-%02d-000000.tar.gz", day)), []byte("inert archive"), 0o600); err != nil {
				b.Fatal(err)
			}
		}
		if err := Rotate(Config{MaxDays: 5, MaxPerDay: 3}); err != nil {
			b.Fatal(err)
		}
	}
}
