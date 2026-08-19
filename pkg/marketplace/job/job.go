package job

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// Job is an alias to the durable job type in [store].
type Job = store.Job

// Status is an alias to [store.JobStatus].
type Status = store.JobStatus

// Status constants.
const (
	Pending   = store.JobStatusPending
	Leased    = store.JobStatusLeased
	Completed = store.JobStatusCompleted
	Failed    = store.JobStatusFailed
)

// Backoff constants.
const (
	BaseBackoff = time.Second
	MaxBackoff  = time.Hour
)

// Lease acquires the oldest pending or recoverable job of kind for workerID
// and returns the leased job. It increments the job's attempt count.
func Lease(ctx context.Context, s store.MetadataStore, kind, workerID string, leaseDuration time.Duration) (Job, error) {
	return s.LeaseJob(ctx, kind, workerID, leaseDuration)
}

// ExtendLease extends an existing lease for jobID held by workerID by d.
func ExtendLease(ctx context.Context, s store.MetadataStore, id store.JobID, workerID string, d time.Duration) error {
	return s.ExtendLease(ctx, id, workerID, d)
}

// Release returns a leased job to the pending queue.
func Release(ctx context.Context, s store.MetadataStore, id store.JobID, workerID string) error {
	return s.ReleaseJob(ctx, id, workerID)
}

// Complete marks a leased job as completed.
func Complete(ctx context.Context, s store.MetadataStore, id store.JobID, workerID string) error {
	return s.CompleteJob(ctx, id, workerID)
}

// Fail marks a leased job as failed and schedules a retry using
// [NextRetryDelay] for the backoff.
func Fail(ctx context.Context, s store.MetadataStore, id store.JobID, workerID string) error {
	j, err := s.GetJob(ctx, id)
	if err != nil {
		return err
	}
	if j.Status != Leased || j.WorkerID != workerID || time.Now().After(j.LeaseUntil) {
		return fmt.Errorf("%w: cannot fail job %q", store.ErrLeaseExpired, id)
	}
	backoff := NextRetryDelay(j.Attempt)
	return s.FailJob(ctx, id, workerID, backoff)
}

// NextRetryDelay returns an exponential backoff with full jitter, capped at
// [MaxBackoff]. The returned duration is in [0, delay] where delay doubles
// with each attempt.
func NextRetryDelay(attempt int) time.Duration {
	delay := baseDelay(attempt)
	if delay <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(delay)))
}

func baseDelay(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	delay := BaseBackoff
	for i := 1; i < attempt; i++ {
		if delay > MaxBackoff/2 {
			return MaxBackoff
		}
		delay *= 2
	}
	if delay > MaxBackoff {
		return MaxBackoff
	}
	return delay
}
