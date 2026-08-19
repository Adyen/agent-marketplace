package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func BenchmarkLeaseJob(b *testing.B) {
	ctx := context.Background()
	s := sqlite.New(filepath.Join(b.TempDir(), "bench.db"))
	if err := s.Open(ctx); err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.EnqueueJob(ctx, store.Job{
		ID:          store.JobID("job-bench"),
		Kind:        "index-source",
		Payload:     []byte(`{}`),
		MaxAttempts: 100,
	}); err != nil {
		b.Fatalf("EnqueueJob: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.LeaseJob(ctx, "index-source", "worker", time.Hour); err != nil {
			b.Fatalf("LeaseJob: %v", err)
		}
		if err := s.ReleaseJob(ctx, store.JobID("job-bench"), "worker"); err != nil {
			b.Fatalf("ReleaseJob: %v", err)
		}
	}
}
