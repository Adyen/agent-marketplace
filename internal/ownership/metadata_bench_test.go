package ownership

import (
	"encoding/json"
	"os"
	"testing"
)

func BenchmarkOwnershipStateLoadDisk(b *testing.B) {
	store, rel, _ := benchStore(b)
	record := &Record{Version: stateVersion, CanonicalRoot: store.CanonicalRoot(), RootKey: store.RootKey(), Generation: 1, Files: map[string]FileState{rel: {Entries: benchEntries(25)}}}
	if err := os.MkdirAll(store.recordDir(), 0o700); err != nil {
		b.Fatal(err)
	}
	data, err := json.Marshal(record)
	if err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(store.StateFile(), data, 0o600); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if record, err := store.Load(); err != nil || record == nil {
			b.Fatalf("state load: %v", err)
		}
	}
}
