package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkLoadCodexBackupManifestDisk(b *testing.B) {
	dir := b.TempDir()
	operation := "2026-10-03-120000"
	manifest := codexSetManifest{Version: 1, Operation: operation, Partitions: []codexSetManifestPartition{{ID: "machine-agents", RootKey: codexRootKey(dir), Archive: operation + "-machine-agents-" + codexRootKey(dir) + ".tar.gz", RelPaths: []string{"skills"}}, {ID: "machine-codex", RootKey: codexRootKey(dir), Archive: operation + "-machine-codex-" + codexRootKey(dir) + ".tar.gz", RelPaths: []string{"agents", "hooks.json", "config.toml"}}}}
	raw, err := json.Marshal(manifest)
	if err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, operation+".set.json"), raw, 0o644); err != nil {
		b.Fatal(err)
	}
	for _, part := range manifest.Partitions {
		if err := os.WriteFile(filepath.Join(dir, part.Archive), []byte("inert archive placeholder"), 0o600); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		sets, err := loadCompleteCodexSets(dir)
		if err != nil || len(sets) != 1 {
			b.Fatalf("manifest control absent: %v %v", sets, err)
		}
	}
}
