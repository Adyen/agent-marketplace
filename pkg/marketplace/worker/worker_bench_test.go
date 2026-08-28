package worker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/job"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type slowUnrelatedSourceStore struct {
	store.MetadataStore
	delay time.Duration
}

func (s slowUnrelatedSourceStore) LeaseJob(ctx context.Context, kind, workerID string, leaseDuration time.Duration) (store.Job, error) {
	time.Sleep(s.delay)
	return s.MetadataStore.LeaseJob(ctx, kind, workerID, leaseDuration)
}

func BenchmarkReindexWithUnrelatedBacklog(b *testing.B) {
	ctx := context.Background()
	repoDir, cleanup := createTestRepo(b)
	defer cleanup()
	cfg := createTestConfig(b, repoDir)
	meta, blobs := openTestStores(b)
	defer func() { _ = meta.Close() }()
	deps := Deps{Config: cfg, MetadataStore: slowUnrelatedSourceStore{MetadataStore: meta, delay: 100 * time.Millisecond}, BlobStore: blobs}

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		for j := range 10 {
			id := store.JobID(fmt.Sprintf("unrelated-%d-%d", i, j))
			if err := job.Enqueue(ctx, meta, job.Job{ID: id, Kind: "refresh", Payload: []byte(`{"source_id":"missing-source"}`), MaxAttempts: 1}); err != nil {
				b.Fatalf("enqueue unrelated job: %v", err)
			}
		}
		b.StartTimer()
		if err := Reindex(ctx, deps, cfg, cfg.Sources[0].Slug); err != nil {
			b.Fatalf("Reindex: %v", err)
		}
	}
}

func BenchmarkRefresh(b *testing.B) {
	ctx := context.Background()
	repoDir, cleanup := createTestRepo(b)
	defer cleanup()

	cfg := createTestConfig(b, repoDir)
	meta, blobs := openTestStores(b)
	defer func() { _ = meta.Close() }()

	deps := Deps{Config: cfg, MetadataStore: meta, BlobStore: blobs}
	if err := EnsureMarketplace(ctx, &deps); err != nil {
		b.Fatalf("EnsureMarketplace: %v", err)
	}

	src := store.Source{ID: store.SourceID(cfg.Sources[0].ID)}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Make each iteration unique by touching a file so the commit changes.
		marker := fmt.Sprintf("bench-%d", i)
		if err := os.WriteFile(repoDir+"/"+marker, []byte("x"), 0o644); err != nil {
			b.Fatalf("write marker: %v", err)
		}
		cmd := exec.Command("git", "add", marker)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			b.Fatalf("git add: %v\n%s", err, out)
		}
		cmd = exec.Command("git", "commit", "-q", "-m", marker, "--allow-empty")
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			b.Fatalf("git commit: %v\n%s", err, out)
		}

		if err := Refresh(ctx, deps, src); err != nil {
			b.Fatalf("Refresh: %v", err)
		}
	}
}
