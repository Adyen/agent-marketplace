package worker

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// backfillMeta lists archive digests in order, one page at a time.
type backfillMeta struct {
	store.MetadataStore
	digests []store.ArchiveDigest
}

func (m backfillMeta) ListResourceArchiveDigests(_ context.Context, _ store.MarketplaceID, after store.ArchiveDigest, limit int) ([]store.ArchiveDigest, error) {
	var page []store.ArchiveDigest
	for _, digest := range m.digests {
		if digest > after && len(page) < limit {
			page = append(page, digest)
		}
	}
	return page, nil
}

// backfillBlobs is an in-memory blob store.
type backfillBlobs struct {
	store.BlobStore
	data map[store.ArchiveDigest][]byte
}

func (b backfillBlobs) Get(_ context.Context, digest store.ArchiveDigest) ([]byte, error) {
	data, ok := b.data[digest]
	if !ok {
		return nil, store.ErrNotFound
	}
	return data, nil
}

func (b backfillBlobs) Exists(_ context.Context, digest store.ArchiveDigest) (bool, error) {
	_, ok := b.data[digest]
	return ok, nil
}

func (b backfillBlobs) Put(_ context.Context, digest store.ArchiveDigest, data []byte) error {
	b.data[digest] = data
	return nil
}

// spec: marketplace-api/worker-backfills-placeholder-archives
func TestWorkerBackfillsPlaceholderArchives(t *testing.T) {
	ctx := context.Background()
	data := make(map[store.ArchiveDigest][]byte)
	var digests []store.ArchiveDigest
	for _, version := range []string{"go1.26", "go1.27"} {
		final, err := os.ReadFile(filepath.Join("..", "..", "harness-compilation", "artifact", "testdata", "toolchain", version, "mcpserver.tar.gz"))
		if err != nil {
			t.Fatal(err)
		}
		a, _, err := hccartifact.Decode(bytes.NewReader(final))
		if err != nil {
			t.Fatal(err)
		}
		digest := store.ArchiveDigest(a.Manifest.ArchiveDigest)
		data[digest] = final
		digests = append(digests, digest)
	}
	sort.Slice(digests, func(i, j int) bool { return digests[i] < digests[j] })
	meta := backfillMeta{digests: digests}
	blobs := backfillBlobs{data: data}

	first, err := backfillPlaceholderArchives(ctx, meta, blobs, "mp-test")
	if err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if first.Examined != 2 || first.Derived != 1 || first.Unreproducible != 1 {
		t.Fatalf("first pass report = %+v, want one derived and one unreproducible archive", first)
	}
	for _, digest := range digests {
		placeholder, stored := data[store.PlaceholderArchiveKey(digest)]
		if !stored {
			continue
		}
		if _, err := hccartifact.VerifyPlaceholderArchive(placeholder, string(digest)); err != nil {
			t.Fatalf("derived placeholder for %s does not verify: %v", digest, err)
		}
	}

	second, err := backfillPlaceholderArchives(ctx, meta, blobs, "mp-test")
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if second.Present != 1 || second.Derived != 0 || second.Unreproducible != 1 {
		t.Fatalf("second pass report = %+v, want the derived archive present and the other unreproducible", second)
	}
}
