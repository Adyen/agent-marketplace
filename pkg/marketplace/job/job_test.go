package job_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/job"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func newTestStore(t *testing.T) *sqlite.Store {
	t.Helper()
	s := sqlite.New(filepath.Join(t.TempDir(), "jobs.db"))
	if err := s.Open(context.Background()); err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestNextRetryDelay(t *testing.T) {
	for attempt := 1; attempt <= 5; attempt++ {
		d := job.NextRetryDelay(attempt)
		if d < 0 {
			t.Fatalf("attempt %d returned negative delay %v", attempt, d)
		}
		if d > job.MaxBackoff {
			t.Fatalf("attempt %d returned delay %v > MaxBackoff", attempt, d)
		}
	}

	// High attempts should cap at MaxBackoff.
	d := job.NextRetryDelay(100)
	if d < 0 || d > job.MaxBackoff {
		t.Fatalf("attempt 100 returned delay %v", d)
	}

	// Delay should grow with attempt on average.
	const trials = 100
	var sum1, sum2 int64
	for i := 0; i < trials; i++ {
		sum1 += int64(job.NextRetryDelay(1))
		sum2 += int64(job.NextRetryDelay(3))
	}
	if sum2 <= sum1 {
		t.Fatalf("delay did not grow with attempt: sum1=%d sum2=%d", sum1, sum2)
	}
}

func TestJobLeaseAndComplete(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.EnqueueJob(ctx, store.Job{
		ID:          store.JobID("job-1"),
		Kind:        "index-source",
		Payload:     []byte(`{}`),
		MaxAttempts: 3,
	}); err != nil {
		t.Fatalf("EnqueueJob: %v", err)
	}

	j, err := job.Lease(ctx, s, "index-source", "worker-a", time.Minute)
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	if j.Status != job.Leased || j.WorkerID != "worker-a" || j.Attempt != 1 {
		t.Fatalf("Lease returned %+v", j)
	}

	if err := job.Complete(ctx, s, j.ID, "worker-a"); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	completed, err := s.GetJob(ctx, j.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if completed.Status != job.Completed {
		t.Fatalf("status = %q, want completed", completed.Status)
	}
}

func TestJobLeaseExpiryAndRecovery(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.EnqueueJob(ctx, store.Job{
		ID:          store.JobID("job-1"),
		Kind:        "index-source",
		Payload:     []byte(`{}`),
		MaxAttempts: 3,
	}); err != nil {
		t.Fatalf("EnqueueJob: %v", err)
	}

	j, err := job.Lease(ctx, s, "index-source", "worker-a", 2*time.Millisecond)
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}

	time.Sleep(5 * time.Millisecond)

	recovered, err := job.Lease(ctx, s, "index-source", "worker-b", time.Minute)
	if err != nil {
		t.Fatalf("Lease recovery: %v", err)
	}
	if recovered.ID != j.ID {
		t.Fatalf("recovered job id %q, want %q", recovered.ID, j.ID)
	}
	if recovered.WorkerID != "worker-b" {
		t.Fatalf("recovered worker %q, want worker-b", recovered.WorkerID)
	}
	if recovered.Attempt != 2 {
		t.Fatalf("recovered attempt %d, want 2", recovered.Attempt)
	}
}

func TestJobRetryBackoff(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.EnqueueJob(ctx, store.Job{
		ID:          store.JobID("job-1"),
		Kind:        "index-source",
		Payload:     []byte(`{}`),
		MaxAttempts: 3,
	}); err != nil {
		t.Fatalf("EnqueueJob: %v", err)
	}

	j, err := job.Lease(ctx, s, "index-source", "worker-a", time.Minute)
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}

	if err := job.Fail(ctx, s, j.ID, "worker-a"); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	failed, err := s.GetJob(ctx, j.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if failed.Status != job.Failed {
		t.Fatalf("status = %q, want failed", failed.Status)
	}
	if failed.Backoff <= 0 || failed.Backoff > job.MaxBackoff {
		t.Fatalf("backoff = %v, want (0, %v]", failed.Backoff, job.MaxBackoff)
	}
	if failed.LeaseUntil.IsZero() {
		t.Fatalf("failed job has no retry time")
	}
	// With attempt=1 and max=3, the job can be retried after the backoff window.
	// Lease with a long duration should succeed once backoff has passed.
	time.Sleep(failed.Backoff + time.Millisecond)
	retried, err := job.Lease(ctx, s, "index-source", "worker-b", time.Minute)
	if err != nil {
		t.Fatalf("Lease after backoff: %v", err)
	}
	if retried.Attempt != 2 {
		t.Fatalf("retried attempt %d, want 2", retried.Attempt)
	}
}

func TestJobLeaseHeldByOtherWorker(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.EnqueueJob(ctx, store.Job{
		ID:          store.JobID("job-1"),
		Kind:        "index-source",
		Payload:     []byte(`{}`),
		MaxAttempts: 3,
	}); err != nil {
		t.Fatalf("EnqueueJob: %v", err)
	}

	if _, err := job.Lease(ctx, s, "index-source", "worker-a", time.Minute); err != nil {
		t.Fatalf("Lease: %v", err)
	}

	// A different worker cannot complete a held job.
	err := job.Complete(ctx, s, store.JobID("job-1"), "worker-b")
	if !errors.Is(err, store.ErrLeaseExpired) {
		t.Fatalf("Complete by other worker error = %v, want ErrLeaseExpired", err)
	}
}
