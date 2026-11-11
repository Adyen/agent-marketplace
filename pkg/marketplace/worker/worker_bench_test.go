package worker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/acquire"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/job"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

type slowUnrelatedRegistryStore struct {
	store.MetadataStore
	delay time.Duration
}

func (s slowUnrelatedRegistryStore) LeaseJob(ctx context.Context, kind, workerID string, leaseDuration time.Duration) (store.Job, error) {
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
	deps := Deps{Config: cfg, MetadataStore: slowUnrelatedRegistryStore{MetadataStore: meta, delay: 100 * time.Millisecond}, BlobStore: blobs}

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		for j := range 10 {
			id := store.JobID(fmt.Sprintf("unrelated-%d-%d", i, j))
			if err := job.Enqueue(ctx, meta, job.Job{ID: id, Kind: "refresh", Payload: []byte(`{"registry_id":"missing-registry"}`), MaxAttempts: 1}); err != nil {
				b.Fatalf("enqueue unrelated job: %v", err)
			}
		}
		b.StartTimer()
		if err := Reindex(ctx, deps, cfg, cfg.Registries[0].Slug); err != nil {
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

	src := store.Registry{ID: store.RegistryID(cfg.Registries[0].ID)}

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

func BenchmarkObservationCycle200Registries(b *testing.B) {
	repo, cleanup := createTestRepo(b)
	defer cleanup()
	commit, err := testCommit(repo)
	if err != nil {
		b.Fatal(err)
	}
	cfg := createTestConfig(b, repo)
	template := cfg.Registries[0]
	cfg.Registries = make([]config.Registry, 200)
	for i := range cfg.Registries {
		src := template
		src.ID = fmt.Sprintf("registry-%03d", i)
		src.Slug = src.ID
		src.Origin = fmt.Sprintf("https://registry-%03d.example.test", i)
		cfg.Registries[i] = src
	}
	meta, blobs := openTestStores(b)
	defer func() { _ = meta.Close() }()
	deps := Deps{
		Config:        cfg,
		MetadataStore: meta,
		BlobStore:     blobs,
		Metrics:       NewMetrics(),
		ResolveRef: func(context.Context, string, string, acquire.Credentials, time.Duration) (string, error) {
			return commit, nil
		},
	}
	if err := EnsureMarketplace(context.Background(), &deps); err != nil {
		b.Fatal(err)
	}
	if err := initializeObservationState(context.Background(), deps, cfg); err != nil {
		b.Fatal(err)
	}
	now := time.Now().UTC()
	for _, src := range cfg.Registries {
		generation, err := config.IndexingGeneration(cfg, src)
		if err != nil {
			b.Fatal(err)
		}
		observation, err := meta.GetRegistryObservation(context.Background(), store.RegistryID(src.ID))
		if err != nil {
			b.Fatal(err)
		}
		observation.ObservedCommit = commit
		observation.SuccessfulCommit = commit
		observation.SuccessfulGeneration = generation
		observation.ObservedAt = now
		observation.LastSuccessfulIndexAt = now
		observation.ProbeStatus = store.RegistryProbeStatusOK
		observation.NextReconcileAt = now.Add(time.Hour)
		if _, err := meta.UpdateRegistryObservation(context.Background(), observation.Version, observation); err != nil {
			b.Fatal(err)
		}
	}

	observer := newObserver(deps, cfg, time.Now)
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(previousLogger) })
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		for _, src := range cfg.Registries {
			observation, err := meta.GetRegistryObservation(ctx, store.RegistryID(src.ID))
			if err != nil {
				b.Fatal(err)
			}
			observation.NextProbeAt = time.Time{}
			if _, err := meta.UpdateRegistryObservation(ctx, observation.Version, observation); err != nil {
				b.Fatal(err)
			}
		}
		b.StartTimer()
		for batch := 0; batch < 2; batch++ {
			if err := observer.runCycle(ctx); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkDueObservationSelection200Registries(b *testing.B) {
	meta := sqlite.New(filepath.Join(b.TempDir(), "marketplace.db"))
	if err := meta.Open(context.Background()); err != nil {
		b.Fatal(err)
	}
	defer func() { _ = meta.Close() }()
	for i := 0; i < 200; i++ {
		id := store.RegistryID(fmt.Sprintf("registry-%03d", i))
		if _, err := meta.CreateRegistryObservation(context.Background(), store.RegistryObservation{RegistryID: id}); err != nil {
			b.Fatal(err)
		}
	}
	now := time.Now().UTC()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		due, err := meta.ListDueRegistryObservations(context.Background(), now, 256)
		if err != nil {
			b.Fatal(err)
		}
		if len(due) != 200 {
			b.Fatalf("due observations = %d, want 200", len(due))
		}
	}
}
