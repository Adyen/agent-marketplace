package worker

import (
	"context"
	"testing"
	"time"
)

func TestStartPlaceholderBackfillReportsWhenItStops(t *testing.T) {
	meta, blobs := openTestStores(t)
	defer func() { _ = meta.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	deps := Deps{Config: createTestConfig(t, t.TempDir()), MetadataStore: meta, BlobStore: blobs}
	deps.Marketplace.ID = "test-mp"
	select {
	case <-StartPlaceholderBackfill(ctx, deps):
	case <-time.After(10 * time.Second):
		t.Fatal("a cancelled placeholder backfill did not report that it stopped")
	}
}
