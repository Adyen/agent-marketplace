package lockfile

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkReadBytesInfoRegularFile(b *testing.B) {
	path := filepath.Join(b.TempDir(), "lock.yaml")
	data := bytes.Repeat([]byte("x"), 4096)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		raw, info, err := ReadBytesInfo(path)
		if err != nil || !bytes.Equal(raw, data) || info == nil || !info.Mode().IsRegular() || info.Size() != int64(len(data)) || info.Mode().Perm() != 0o600 {
			b.Fatalf("lock read info=%v error=%v", info, err)
		}
	}
}
