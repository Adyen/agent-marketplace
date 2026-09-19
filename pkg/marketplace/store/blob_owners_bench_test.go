package store_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func BenchmarkListBlobOwners(b *testing.B) {
	ctx := context.Background()
	meta := sqlite.New(filepath.Join(b.TempDir(), "owners.db"))
	blobs := fsblob.New(filepath.Join(b.TempDir(), "blobs"))
	if err := meta.Open(ctx); err != nil {
		b.Fatal(err)
	}
	defer func() { _ = meta.Close() }()
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "managed-test", Name: "Benchmark", CanonicalURL: "https://example.test"}); err != nil {
		b.Fatal(err)
	}
	var digest store.ArchiveDigest
	for _, registry := range []store.RegistryID{"first", "second"} {
		putBlobOwnershipRegistry(b, meta, registry)
		var req store.RegistryRevisionPublication
		req, digest = blobOwnershipPublication(b, registry, "publication", "# Shared bytes")
		if err := store.PublishRegistryRevision(ctx, meta, blobs, req); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		owners, registries, err := meta.ListBlobOwners(ctx, "managed-test", "content", digest)
		if err != nil || len(owners) != 2 || len(registries) != 0 {
			b.Fatalf("lookup: owners=%d registries=%d error=%v", len(owners), len(registries), err)
		}
	}
}
