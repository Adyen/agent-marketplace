package dependencytxn

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkReadSnapshotRegularFile(b *testing.B) {
	path := filepath.Join(b.TempDir(), "config.yaml")
	data := bytes.Repeat([]byte("x"), 4096)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		b.Fatal(err)
	}
	want := Generation{Exists: true, Digest: Digest(data)}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		raw, generation, mode, err := ReadSnapshot(path)
		if err != nil || !bytes.Equal(raw, data) || generation != want || mode != 0o600 {
			b.Fatalf("snapshot generation=%#v mode=%o error=%v", generation, mode, err)
		}
	}
}

func BenchmarkCheckPending(b *testing.B) {
	state, err := filepath.EvalSymlinks(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := CheckPending(state); err != nil {
			b.Fatal(err)
		}
	}
}

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
