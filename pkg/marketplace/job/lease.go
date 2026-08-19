package job

import (
	"context"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
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
