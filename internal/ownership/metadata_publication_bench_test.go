package ownership

import (
	"os"
	"testing"
)

func BenchmarkOwnershipMetadataPublicationDisk(b *testing.B) {
	store, rel, _ := benchStore(b)
	if err := os.MkdirAll(store.recordDir(), 0o700); err != nil {
		b.Fatal(err)
	}
	record := &Record{Version: stateVersion, CanonicalRoot: store.CanonicalRoot(), RootKey: store.RootKey(), Generation: 1, Files: map[string]FileState{rel: {Entries: benchEntries(25)}}}
	b.ReportAllocs()
	for b.Loop() {
		if err := store.commitRecord(record); err != nil {
			b.Fatal(err)
		}
	}
}
