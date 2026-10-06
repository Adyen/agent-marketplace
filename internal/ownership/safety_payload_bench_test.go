package ownership

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkRetainedSafetyPayloadDisk(b *testing.B) {
	store, rel, _ := benchStore(b)
	dir := store.SafetyBackupDir(rel)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		b.Fatal(err)
	}
	path := filepath.Join(dir, "20261004T010000.000000000Z-inert.bak")
	payload := make([]byte, 64<<10)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		b.Fatal(err)
	}
	meta, err := json.Marshal(BackupManifest{Version: 1, File: rel, RootKey: store.RootKey(), CanonicalRoot: store.CanonicalRoot(), PriorSize: int64(len(payload)), PriorDigest: Digest(payload)})
	if err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(metaPathFor(path), meta, 0o600); err != nil {
		b.Fatal(err)
	}
	backups, err := store.SafetyBackups(rel)
	if err != nil || len(backups) != 1 {
		b.Fatalf("actual retained payload: %v %v", backups, err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if data, err := backups[0].RawBytes(); err != nil || len(data) != len(payload) {
			b.Fatalf("actual raw payload read: %v", err)
		}
	}
}
