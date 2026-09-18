package sqlite_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

// BenchmarkListDueRegistryObservations measures the scheduler scan including
// SQLite I/O for a realistic 200-registry deployment fixture.
func BenchmarkListDueRegistryObservations(b *testing.B) {
	ctx := context.Background()
	s := sqlite.New(filepath.Join(b.TempDir(), "observation-bench.db"))
	if err := s.Open(ctx); err != nil {
		b.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	base := time.Unix(1000, 0).UTC()
	for i := 0; i < 200; i++ {
		registryID := store.RegistryID(fmt.Sprintf("registry-%03d", i))
		if err := s.PutRegistry(ctx, store.Registry{
			ID:          registryID,
			Slug:        string(registryID),
			Ref:         "main",
			Acquisition: store.AcquisitionMirror,
			Listed:      true,
		}); err != nil {
			b.Fatal(err)
		}
		if err := s.PutRegistryObservation(ctx, store.RegistryObservation{
			RegistryID:      registryID,
			NextProbeAt:     base.Add(time.Duration(i) * time.Minute),
			NextReconcileAt: base.Add(24 * time.Hour),
		}); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.ListDueRegistryObservations(ctx, base.Add(30*time.Minute), 200); err != nil {
			b.Fatal(err)
		}
	}
}
