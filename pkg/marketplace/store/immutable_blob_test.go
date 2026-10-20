package store_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
)

// immutableReadProbe counts blob bytes returned by bounded and unbounded reads.
type immutableReadProbe struct {
	*fsblob.Store
	unbounded int
	read      int
}

func (p *immutableReadProbe) Get(ctx context.Context, digest store.ArchiveDigest) ([]byte, error) {
	p.unbounded++
	data, err := p.Store.Get(ctx, digest)
	p.read += len(data)
	return data, err
}

func (p *immutableReadProbe) GetBounded(ctx context.Context, digest store.ArchiveDigest, limit int64) ([]byte, error) {
	data, err := p.Store.GetBounded(ctx, digest, limit)
	p.read += len(data)
	return data, err
}

func TestPutImmutableBlobComparesExistingBlobsWithinTheUploadSize(t *testing.T) {
	ctx := context.Background()
	const digest = store.ArchiveDigest("sha256:0000000000000000000000000000000000000000000000000000000000000001")
	upload := []byte("upload")
	for _, tc := range []struct {
		name     string
		existing []byte
		data     []byte
		conflict bool
	}{
		{"missing", nil, upload, false},
		{"equal", upload, upload, false},
		{"longer", bytes.Repeat([]byte("x"), 1<<20), upload, true},
		{"same size, different", []byte("uplo4d"), upload, true},
		{"empty upload over an empty blob", []byte{}, []byte{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			blobs := &immutableReadProbe{Store: fsblob.New(t.TempDir())}
			if tc.existing != nil {
				if err := blobs.Put(ctx, digest, tc.existing); err != nil {
					t.Fatal(err)
				}
			}
			err := store.PutImmutableBlob(ctx, blobs, digest, tc.data)
			if tc.conflict != errors.Is(err, store.ErrConflict) || (!tc.conflict && err != nil) {
				t.Fatalf("PutImmutableBlob error = %v, want conflict %t", err, tc.conflict)
			}
			if len(tc.data) > 0 && (blobs.unbounded != 0 || blobs.read > len(tc.data)) {
				t.Fatalf("existing blob read: unbounded=%d bytes=%d, want bounded reads of at most %d bytes", blobs.unbounded, blobs.read, len(tc.data))
			}
			if stored, err := blobs.Store.Get(ctx, digest); err != nil || (!tc.conflict && !bytes.Equal(stored, tc.data)) || (tc.conflict && !bytes.Equal(stored, tc.existing)) {
				t.Fatalf("stored blob = %q, %v", stored, err)
			}
		})
	}
}
