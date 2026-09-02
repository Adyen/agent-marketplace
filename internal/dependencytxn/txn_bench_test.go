package dependencytxn

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkCommitUnchanged(b *testing.B) {
	base, err := filepath.EvalSymlinks(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	root := filepath.Join(base, "files")
	state := filepath.Join(base, "state")
	if err := os.Mkdir(root, 0o700); err != nil {
		b.Fatal(err)
	}
	path := filepath.Join(root, "lock.yaml")
	data := []byte("version: 1\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		b.Fatal(err)
	}
	file := DesiredFile{Path: path, Expected: Generation{Exists: true, Digest: Digest(data)}, Bytes: data, Mode: 0o600}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := Commit(context.Background(), state, []DesiredFile{file}); err != nil {
			b.Fatal(err)
		}
	}
}
