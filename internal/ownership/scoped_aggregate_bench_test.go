package ownership

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkScopedAggregateReadDisk(b *testing.B) {
	root := b.TempDir()
	path := filepath.Join(root, "aggregate.json")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 4096), 0o600); err != nil {
		b.Fatal(err)
	}
	store, err := Open(root, WithStateRoot(b.TempDir()))
	if err != nil {
		b.Fatal(err)
	}
	for _, scoped := range []bool{false, true} {
		name := "legacy"
		if scoped {
			name = "captured"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var data []byte
				var err error
				if scoped {
					data, err = store.ReadAggregate("aggregate.json")
				} else {
					data, err = ReadAggregateFile(path)
				}
				if err != nil || len(data) != 4096 {
					b.Fatalf("aggregate read control failed: %d %v", len(data), err)
				}
			}
		})
	}
}
