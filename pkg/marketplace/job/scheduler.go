package job

import (
	"context"
	"fmt"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// Enqueue adds a durable job to the pending queue.
func Enqueue(ctx context.Context, s store.MetadataStore, job Job) error {
	return s.EnqueueJob(ctx, job)
}

// Dequeue is an alias for [Lease] and acquires the next available job.
func Dequeue(ctx context.Context, s store.MetadataStore, kind, workerID string, leaseDuration time.Duration) (Job, error) {
	return Lease(ctx, s, kind, workerID, leaseDuration)
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
	if j.Status != Leased || j.WorkerID != workerID {
		return fmt.Errorf("%w: cannot fail job %q", store.ErrLeaseExpired, id)
	}
	backoff := NextRetryDelay(j.Attempt)
	return s.FailJob(ctx, id, workerID, backoff)
}
