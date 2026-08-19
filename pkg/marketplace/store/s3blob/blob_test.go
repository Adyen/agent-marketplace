package s3blob_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/s3blob"
)

func testStore(t *testing.T) *s3blob.Store {
	t.Helper()
	endpoint := os.Getenv("S3_TEST_ENDPOINT")
	bucket := os.Getenv("S3_TEST_BUCKET")
	region := os.Getenv("S3_TEST_REGION")
	if endpoint == "" || bucket == "" || region == "" {
		t.Skip("S3_TEST_ENDPOINT, S3_TEST_BUCKET, and S3_TEST_REGION must be set for S3 tests")
	}

	ctx := context.Background()
	s, err := s3blob.New(ctx, bucket, region, s3blob.WithEndpoint(endpoint))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestBlobRoundTrip(t *testing.T) {
	ctx := context.Background()
	bs := testStore(t)

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

func TestBlobMissing(t *testing.T) {
	ctx := context.Background()
	bs := testStore(t)

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

func TestBlobOverwriteAndDelete(t *testing.T) {
	ctx := context.Background()
	bs := testStore(t)

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

func TestBlobLargeObject(t *testing.T) {
	ctx := context.Background()
	bs := testStore(t)

	digest := store.ArchiveDigest("sha256:large")
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
}
