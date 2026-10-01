package dependencytxn

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkReadManagedConfig(b *testing.B) {
	root, err := filepath.EvalSymlinks(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	directory := filepath.Join(root, ".agents")
	if err := os.Mkdir(directory, 0o700); err != nil {
		b.Fatal(err)
	}
	path := filepath.Join(directory, "config.yaml")
	payload := bytes.Repeat([]byte("x"), 4096)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for b.Loop() {
		data, generation, mode, err := ReadSnapshot(path)
		if err != nil || !bytes.Equal(data, payload) || !generation.Exists || mode != 0o600 {
			b.Fatalf("managed snapshot failed: %v", err)
		}
	}
}

func BenchmarkCommitManagedUnchanged(b *testing.B) {
	root, err := filepath.EvalSymlinks(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	directory := filepath.Join(root, ".agents")
	if err := os.Mkdir(directory, 0o700); err != nil {
		b.Fatal(err)
	}
	path := filepath.Join(directory, "lock.yaml")
	payload := []byte("apiVersion: agent-marketplace-lock/v1\nscope: repo\nregistries: []\n")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		b.Fatal(err)
	}
	file := DesiredFile{Path: path, Expected: Generation{Exists: true, Digest: Digest(payload)}, Bytes: payload, Mode: 0o600}
	state := filepath.Join(root, "state")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := Commit(context.Background(), state, []DesiredFile{file}); err != nil {
			b.Fatal(err)
		}
	}
}
