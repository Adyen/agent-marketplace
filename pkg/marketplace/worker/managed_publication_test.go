package worker

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/acquire"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/admin"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

type withoutAttemptRecorder struct {
	store.MetadataStore
	store.ManagedConfigurationStore
	store.PublicationStore
}

type publicationRaceBlobs struct {
	store.BlobStore
	beforePut func()
}

func (s *publicationRaceBlobs) Put(ctx context.Context, d store.ArchiveDigest, b []byte) error {
	if s.beforePut != nil {
		hook := s.beforePut
		s.beforePut = nil
		hook()
	}
	return s.BlobStore.Put(ctx, d, b)
}

// spec: claude-enterprise-delivery/publication-completion-enqueues-delivery
func TestManagedPublicationFencesRaceAfterPreflight(t *testing.T) {
	for _, scenario := range []string{"success", "pause", "lease"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			deps, svc := managedFixture(t)
			metadata := deps.MetadataStore.(*sqlite.Store)
			original, err := metadata.GetRegistry(ctx, "src-test")
			if err != nil {
				t.Fatal(err)
			}
			repo, cleanup := createTestRepo(t)
			defer cleanup()
			commit, err := testCommit(repo)
			if err != nil {
				t.Fatal(err)
			}
			if err := scheduleManaged(ctx, deps, "race"); err != nil {
				t.Fatal(err)
			}
			j, err := metadata.LeaseJob(ctx, "managed-refresh", "worker", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			cloned := false
			clone := func(ctx context.Context, origin, ref string, cred acquire.Credentials, limits acquire.Limits, timeout time.Duration, roots ...string) (*acquire.Result, error) {
				cloned = true
				if origin != "https://git.example/repository" || ref != "main" || limits.RepositoryBytes != 100*1024*1024 {
					t.Fatalf("clone inputs: %s %s %+v", origin, ref, limits)
				}
				return &acquire.Result{Dir: repo, Commit: commit}, nil
			}
			if scenario == "success" {
				deps.Config.ClaudeEnterprise = &config.ClaudeEnterprise{
					Enabled:  true,
					APIKey:   config.ClaudeEnterpriseAPIKeyReference{Env: "CLAUDE_ENTERPRISE_TEST_KEY"},
					APIKeyID: "apikey_test",
				}
			}
			hookRan := false
			deps.BlobStore = &publicationRaceBlobs{BlobStore: deps.BlobStore, beforePut: func() {
				hookRan = true
				switch scenario {
				case "pause":
					_, err := svc.Apply(ctx, "test-mp", admin.Mutation{Action: "pause", RegistryID: "src-test", ExpectedRevision: 1, Actor: store.ManagedActor{Issuer: "test", Subject: "admin"}, Reason: "pause", RequestID: "pause", IdempotencyKey: "pause"})
					if err != nil {
						t.Fatal(err)
					}
				case "lease":
					if err := metadata.ReleaseJob(ctx, j.ID, "worker"); err != nil {
						t.Fatal(err)
					}
				}
			}}
			err = processManagedRefresh(ctx, deps, j, "worker", clone)
			if !cloned || !hookRan {
				t.Fatal("pipeline did not reach publication upload")
			}
			pub, pubErr := metadata.CurrentRegistryPublication(ctx, "src-test")
			if scenario == "success" {
				if err != nil || pubErr != nil || pub.ID == "" || pub.PolicyRevision != 1 || pub.RegistryGeneration != 1 || pub.Occurrence.Commit != commit {
					t.Fatalf("publication=%+v err=%v/%v", pub, err, pubErr)
				}
				if _, err := metadata.LeaseJob(ctx, "claude-delivery", "delivery-worker", time.Minute); err != nil {
					t.Fatalf("publication did not enqueue Claude delivery: %v", err)
				}
				events, err := metadata.ListManagedRefreshAttempts(ctx, store.ManagedRefreshAttemptQuery{Marketplace: "test-mp", JobID: j.ID, Limit: 10})
				if err != nil || len(events) != 2 || events[0].Outcome != "succeeded" || events[0].PublicationID != pub.ID || events[1].Outcome != "started" {
					t.Fatalf("attempt outcomes: %+v %v", events, err)
				}
				if err := scheduleManaged(ctx, deps, "second"); err != nil {
					t.Fatal(err)
				}
				secondJob, err := metadata.LeaseJob(ctx, "managed-refresh", "second", time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				if err := processManagedRefresh(ctx, deps, secondJob, "second", clone); err != nil {
					t.Fatal(err)
				}
				second, err := metadata.CurrentRegistryPublication(ctx, "src-test")
				if err != nil || second.ID == pub.ID || second.Occurrence.Commit != pub.Occurrence.Commit {
					t.Fatalf("same-commit reindex did not advance publication: %+v %v", second, err)
				}
				if _, err := metadata.GetRegistryPublication(ctx, "src-test", pub.ID); err != nil {
					t.Fatal(err)
				}
				if err := scheduleManaged(ctx, deps, "failure"); err != nil {
					t.Fatal(err)
				}
				failedJob, err := metadata.LeaseJob(ctx, "managed-refresh", "failed", time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				err = processManagedRefresh(ctx, deps, failedJob, "failed", func(context.Context, string, string, acquire.Credentials, acquire.Limits, time.Duration, ...string) (*acquire.Result, error) {
					return nil, ErrManagedRefresh
				})
				if !errors.Is(err, ErrManagedRefresh) {
					t.Fatalf("failed acquisition: %v", err)
				}
				state, err := metadata.GetManagedRegistryRefreshState(ctx, "test-mp", "src-test")
				if err != nil || state.LastSuccess == nil || state.LastSuccess.PublicationID != second.ID || state.LastAttempt == nil || state.LastAttempt.Outcome != "failed" {
					t.Fatalf("failure lost last success: %+v %v", state, err)
				}
			} else {
				if err == nil || !errors.Is(pubErr, store.ErrNotFound) {
					t.Fatalf("stale publication: %v %v", err, pubErr)
				}
				events, err := metadata.ListManagedRefreshAttempts(ctx, store.ManagedRefreshAttemptQuery{Marketplace: "test-mp", JobID: j.ID, Limit: 10})
				if err != nil {
					t.Fatal(err)
				}
				for _, event := range events {
					if event.Outcome == "succeeded" {
						t.Fatal("losing worker reported success")
					}
				}
			}
			after, readErr := metadata.GetRegistry(ctx, original.ID)
			if readErr != nil || !reflect.DeepEqual(original, after) {
				t.Fatalf("worker rewrote registry status/config: %v\nbefore=%+v\nafter=%+v", readErr, original, after)
			}
		})
	}
}

type claudeDeliverySchedulingStore struct {
	store.MetadataStore
	enqueued chan store.Job
}

func (s *claudeDeliverySchedulingStore) EnqueueJob(ctx context.Context, job store.Job) error {
	if err := s.MetadataStore.EnqueueJob(ctx, job); err != nil {
		return err
	}
	if job.Kind == "claude-delivery" {
		select {
		case s.enqueued <- job:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (s *claudeDeliverySchedulingStore) LeaseClaudeDeliveryJob(context.Context, store.MarketplaceID, store.JobID, string, time.Duration) (store.Job, error) {
	return store.Job{}, store.ErrNotFound
}

func TestClaudeDeliveryRepairIntervalEnqueuesRepair(t *testing.T) {
	deps, _ := managedFixture(t)
	interval := config.Duration(10 * time.Millisecond)
	deps.Config.ClaudeEnterprise = &config.ClaudeEnterprise{
		Enabled: true,
		APIKey: config.ClaudeEnterpriseAPIKeyReference{
			Env: "CLAUDE_ENTERPRISE_TEST_KEY",
		},
		APIKeyID: "apikey_test",
		Interval: &interval,
	}
	tracker := &claudeDeliverySchedulingStore{
		MetadataStore: deps.MetadataStore,
		enqueued:      make(chan store.Job, 4),
	}
	deps.MetadataStore = tracker
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runClaudeDeliveryWorker(ctx, deps)
		close(done)
	}()
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		cancel()
		<-done
		stopped = true
	}
	defer stop()

	jobs := make([]store.Job, 0, 2)
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	for len(jobs) < 2 {
		select {
		case job := <-tracker.enqueued:
			jobs = append(jobs, job)
		case <-timeout.C:
			t.Fatalf("enqueued %d Claude delivery jobs, want configuration and one repair job", len(jobs))
		}
	}
	stop()
	if !strings.Contains(jobs[0].IdempotencyKey, ":configuration-") ||
		!strings.Contains(jobs[1].IdempotencyKey, ":repair-") {
		t.Fatalf("first Claude delivery jobs = %+v, want configuration then repair", jobs)
	}
	for _, job := range jobs {
		stored, err := deps.MetadataStore.GetJob(context.Background(), job.ID)
		if err != nil || stored.Kind != "claude-delivery" {
			t.Fatalf("Claude delivery job %s was not persisted: %+v, %v", job.ID, stored, err)
		}
	}
}

func TestManagedLeaseLossCancelsAcquisition(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	deps, _ := managedFixture(t)
	if err := scheduleManaged(ctx, deps, "cancel"); err != nil {
		t.Fatal(err)
	}
	j, err := deps.MetadataStore.LeaseJob(ctx, "managed-refresh", "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- processManagedRefresh(ctx, deps, j, "worker", func(ctx context.Context, _ string, _ string, _ acquire.Credentials, _ acquire.Limits, _ time.Duration, _ ...string) (*acquire.Result, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		})
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("acquisition did not start")
	}
	if err := deps.MetadataStore.ReleaseJob(ctx, j.ID, "worker"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if !errors.Is(err, store.ErrLeaseExpired) {
			t.Fatalf("lost attempt: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("lost lease did not cancel acquisition")
	}
	if _, err := deps.MetadataStore.(store.PublicationStore).CurrentRegistryPublication(ctx, "src-test"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("canceled acquisition published: %v", err)
	}
}

func TestManagedMissingAttemptCapabilityFailsBeforeAcquisition(t *testing.T) {
	ctx := context.Background()
	deps, _ := managedFixture(t)
	deps.MetadataStore = withoutAttemptRecorder{deps.MetadataStore, deps.MetadataStore.(store.ManagedConfigurationStore), deps.MetadataStore.(store.PublicationStore)}
	if err := scheduleManaged(ctx, deps, "missing"); err != nil {
		t.Fatal(err)
	}
	j, err := deps.MetadataStore.LeaseJob(ctx, "managed-refresh", "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	err = processManagedRefresh(ctx, deps, j, "worker", func(context.Context, string, string, acquire.Credentials, acquire.Limits, time.Duration, ...string) (*acquire.Result, error) {
		t.Fatal("acquired without attempt persistence")
		return nil, nil
	})
	if !errors.Is(err, store.ErrTopology) {
		t.Fatalf("missing capability: %v", err)
	}
	failed, err := deps.MetadataStore.GetJob(ctx, j.ID)
	if err != nil || failed.Status != store.JobStatusFailed {
		t.Fatalf("job outcome: %+v %v", failed, err)
	}
	if _, err := deps.MetadataStore.LeaseJob(ctx, "managed-refresh", "retry", time.Minute); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("terminal job retried: %v", err)
	}
}
