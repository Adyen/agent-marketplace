package fsblob_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
)

func TestGetBounded(t *testing.T) {
	ctx := context.Background()
	bs := fsblob.New(t.TempDir())
	digest := store.ArchiveDigest("sha256:bounded")
	if err := bs.Put(ctx, digest, []byte("12345")); err != nil {
		t.Fatal(err)
	}
	if data, err := bs.GetBounded(ctx, digest, 5); err != nil || string(data) != "12345" {
		t.Fatalf("exact bound: %q %v", data, err)
	}
	if data, err := bs.GetBounded(ctx, digest, 4); !errors.Is(err, store.ErrBlobTooLarge) || data != nil {
		t.Fatalf("oversize: %q %v", data, err)
	}
	if _, err := bs.GetBounded(ctx, "sha256:absent", 5); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestBlobRoundTrip(t *testing.T) {
	ctx := context.Background()
	bs := fsblob.New(t.TempDir())

	digest := store.ArchiveDigest("sha256:abcdef1234567890")
	data := []byte("hello marketplace blob")

	if err := bs.Put(ctx, digest, data); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := bs.Get(ctx, digest)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("Get returned %q, want %q", got, data)
	}

	exists, err := bs.Exists(ctx, digest)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Fatalf("Exists returned false for existing blob")
	}
}

func TestProbe(t *testing.T) {
	ctx := context.Background()

	existingDir := t.TempDir()
	missingRoot := filepath.Join(t.TempDir(), "blobs")
	fileRoot := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(fileRoot, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		root    string
		wantErr error
	}{
		{name: "existing directory", root: existingDir},
		{name: "missing root", root: missingRoot},
		{name: "regular file root", root: fileRoot, wantErr: store.ErrTopology},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := fsblob.New(tc.root).Probe(ctx)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("Probe: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Probe error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestBlobMissing(t *testing.T) {
	ctx := context.Background()
	bs := fsblob.New(t.TempDir())

	digest := store.ArchiveDigest("sha256:missing")
	_, err := bs.Get(ctx, digest)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get missing blob error = %v, want ErrNotFound", err)
	}

	exists, err := bs.Exists(ctx, digest)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Fatalf("Exists returned true for missing blob")
	}

	if err := bs.Delete(ctx, digest); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Delete missing blob error = %v, want ErrNotFound", err)
	}
}

func TestBlobContentAddressedLayout(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	bs := fsblob.New(root)

	digest := store.ArchiveDigest("sha256:0123456789abcdef")
	if err := bs.Put(ctx, digest, []byte("x")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	wantPath := filepath.Join(root, "sh", "a256:0123456789abcdef")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected blob at %s: %v", wantPath, err)
	}
}

func TestBlobImmutableWrite(t *testing.T) {
	ctx := context.Background()
	bs := fsblob.New(t.TempDir())
	digest := store.ArchiveDigest("sha256:immutable")
	if err := bs.PutImmutable(ctx, digest, []byte("first")); err != nil {
		t.Fatalf("PutImmutable first: %v", err)
	}
	if err := bs.PutImmutable(ctx, digest, []byte("first")); err != nil {
		t.Fatalf("PutImmutable retry: %v", err)
	}
	if err := bs.PutImmutable(ctx, digest, []byte("second")); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("PutImmutable conflict = %v, want ErrConflict", err)
	}
	got, err := bs.Get(ctx, digest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first" {
		t.Fatalf("immutable blob changed to %q", got)
	}
}

func TestBlobOverwriteAndDelete(t *testing.T) {
	ctx := context.Background()
	bs := fsblob.New(t.TempDir())

	digest := store.ArchiveDigest("sha256:overwrite")
	if err := bs.Put(ctx, digest, []byte("first")); err != nil {
		t.Fatalf("Put first: %v", err)
	}
	if err := bs.Put(ctx, digest, []byte("second")); err != nil {
		t.Fatalf("Put second: %v", err)
	}

	got, err := bs.Get(ctx, digest)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "second" {
		t.Fatalf("Get returned %q, want %q", got, "second")
	}

	if err := bs.Delete(ctx, digest); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := bs.Get(ctx, digest); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get after delete error = %v, want ErrNotFound", err)
	}
}

func TestBlobAtomicWrite(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	bs := fsblob.New(root)

	digest := store.ArchiveDigest("sha256:atomic")
	data := make([]byte, 1024*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}

	if err := bs.Put(ctx, digest, data); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := bs.Get(ctx, digest)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got) != len(data) {
		t.Fatalf("Get returned %d bytes, want %d", len(got), len(data))
	}
	for i := range data {
		if got[i] != data[i] {
			t.Fatalf("byte %d mismatch", i)
		}
	}

	// No temporary files should remain.
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			t.Fatalf("unexpected temp file %q in blob root", e.Name())
		}
	}
}
