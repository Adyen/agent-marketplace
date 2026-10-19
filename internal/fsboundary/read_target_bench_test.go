package fsboundary

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkDirOpenFileDiskIONoNetwork(b *testing.B) {
	path := b.TempDir()
	if err := os.WriteFile(filepath.Join(path, "payload"), []byte("inert regular payload"), 0o640); err != nil {
		b.Fatal(err)
	}
	root, err := OpenDir(path)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := root.Close(); err != nil {
			b.Error(err)
		}
	})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		file, _, err := root.OpenFile("payload")
		if err != nil {
			b.Fatal(err)
		}
		if err := file.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDestinationReadTargetDisk includes verified reading disk I/O.
func BenchmarkDestinationReadTargetDisk(b *testing.B) {
	path := b.TempDir()
	if err := os.WriteFile(filepath.Join(path, "marker.md"), bytes.Repeat([]byte("inert content\n"), 80), 0o640); err != nil {
		b.Fatal(err)
	}
	root, err := OpenDir(path)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := root.Close(); err != nil {
			b.Error(err)
		}
	})
	destination, err := CaptureDestination(root, "marker.md", LinkPolicyRepository)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := destination.Close(); err != nil {
			b.Error(err)
		}
	})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, _, err := destination.ReadTarget(2048); err != nil {
			b.Fatal(err)
		}
	}
}
