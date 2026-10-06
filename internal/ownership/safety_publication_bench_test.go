package ownership

import (
	"os"
	"testing"
)

func BenchmarkBackupOnlyPublicationDisk(b *testing.B) {
	discardBenchLogs(b)
	store, rel, absolute := benchStore(b)
	if err := os.WriteFile(absolute, benchAggregate(25), 0o600); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if path, err := store.BackupOnly(rel, absolute, ReasonSync); err != nil || path == "" {
			b.Fatalf("actual safety publication: %s %v", path, err)
		}
	}
}
