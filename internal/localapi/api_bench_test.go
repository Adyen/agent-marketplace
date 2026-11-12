package localapi

import (
	"context"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/session"
)

func BenchmarkSnapshotPublish(b *testing.B) {
	ws, home := newFixture(b)
	api, err := New(context.Background(), ws, home, session.DependencyLifecycle{}, nil)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(api.Close)
	snapshot := session.ReadSnapshotForWorkspace(context.Background(), ws, home, session.CatalogLoadCached)
	snapshot.Generation = 1
	api.mu.Lock()
	dto := api.snapshotPresentationLocked(snapshot, nil)
	api.mu.Unlock()
	if len(dto.Rows) == 0 {
		b.Fatal("benchmark fixture produced no rows")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		api.mu.Lock()
		api.emitLocked("snapshot", 0, nil, &dto)
		api.mu.Unlock()
	}
}
