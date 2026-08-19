package fsblob_test

import (
	"context"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
)

func BenchmarkBlobPutGet1KiB(b *testing.B) {
	benchmarkBlobPutGet(b, 1024)
}

func BenchmarkBlobPutGet1MiB(b *testing.B) {
	benchmarkBlobPutGet(b, 1024*1024)
}

func benchmarkBlobPutGet(b *testing.B, size int) {
	ctx := context.Background()
	bs := fsblob.New(b.TempDir())
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}
	digest := store.ArchiveDigest("sha256:0000000000000000000000000000000000000000000000000000000000000000")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := bs.Put(ctx, digest, data); err != nil {
			b.Fatalf("Put: %v", err)
		}
		if _, err := bs.Get(ctx, digest); err != nil {
			b.Fatalf("Get: %v", err)
		}
	}
}
