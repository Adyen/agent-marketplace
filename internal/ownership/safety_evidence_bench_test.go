package ownership

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkSafetyEvidenceDisk(b *testing.B) {
	store, rel, _ := benchStore(b)
	dir := store.SafetyBackupDir(rel)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		path := filepath.Join(dir, fmt.Sprintf("20261004T01000%d.000000000Z-inert.bak", i))
		if err := os.WriteFile(path, []byte("inert payload"), 0o600); err != nil {
			b.Fatal(err)
		}
		data, err := json.Marshal(BackupManifest{Version: 1, File: rel, RootKey: store.RootKey(), CanonicalRoot: store.CanonicalRoot(), Generation: uint64(i + 1), ManagedEntries: benchEntries(25)})
		if err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(metaPathFor(path), data, 0o600); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		if backups, err := store.SafetyBackups(rel); err != nil || len(backups) != 5 {
			b.Fatalf("safety evidence listing: %v %v", backups, err)
		}
	}
}
