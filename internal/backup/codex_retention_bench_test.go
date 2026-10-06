package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkCodexRetentionDisk(b *testing.B) {
	home := b.TempDir()
	b.Setenv("HOME", home)
	dir := filepath.Join(home, ".agent-marketplace", "backups", "codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		for day := 1; day <= 7; day++ {
			op := fmt.Sprintf("2026-01-%02d-000000", day)
			manifest := codexSetManifest{Version: 1, Operation: op}
			for _, id := range []string{"machine-agents", "machine-codex"} {
				archive := op + "-" + id + ".tar.gz"
				if err := os.WriteFile(filepath.Join(dir, archive), []byte("inert archive"), 0o600); err != nil {
					b.Fatal(err)
				}
				manifest.Partitions = append(manifest.Partitions, codexSetManifestPartition{ID: id, Archive: archive})
			}
			raw, err := json.Marshal(manifest)
			if err != nil {
				b.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, op+".set.json"), raw, 0o644); err != nil {
				b.Fatal(err)
			}
		}
		if err := Rotate(Config{MaxDays: 5, MaxPerDay: 3}); err != nil {
			b.Fatal(err)
		}
	}
}
