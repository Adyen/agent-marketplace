package ownership

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkReadAggregateFile(b *testing.B) {
	path := filepath.Join(b.TempDir(), "aggregate.json")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 4096), 0600); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		data, err := ReadAggregateFile(path)
		if err != nil || len(data) != 4096 {
			b.Fatalf("read size=%d err=%v", len(data), err)
		}
	}
}
