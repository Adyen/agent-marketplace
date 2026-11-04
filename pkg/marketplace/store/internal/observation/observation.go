package observation

import (
	"database/sql"
	"errors"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

const Columns = `registry_id, version, observed_commit, observed_at, probe_status,
 probe_error, next_probe_at, next_reconcile_at, consecutive_probe_failures,
 work_lease_owner, work_lease_epoch, work_lease_until, desired_commit, desired_generation,
 active_refresh_job_id, active_refresh_trigger, successful_commit,
 successful_generation, last_successful_index_at`

func Scan(scanner interface{ Scan(...any) error }) (store.RegistryObservation, error) {
	var o store.RegistryObservation
	var version, leaseEpoch, observedAt, nextProbe, nextReconcile, leaseUntil, indexedAt int64
	var activeJobID string
	err := scanner.Scan(&o.RegistryID, &version, &o.ObservedCommit, &observedAt, &o.ProbeStatus,
		&o.ProbeError, &nextProbe, &nextReconcile, &o.ConsecutiveProbeFailures,
		&o.WorkLeaseOwner, &leaseEpoch, &leaseUntil, &o.DesiredCommit, &o.DesiredGeneration,
		&activeJobID, &o.ActiveRefreshTrigger, &o.SuccessfulCommit, &o.SuccessfulGeneration, &indexedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return store.RegistryObservation{}, store.ErrNotFound
	}
	if err != nil {
		return store.RegistryObservation{}, err
	}
	o.Version = uint64(version)
	o.WorkLeaseEpoch = uint64(leaseEpoch)
	o.ObservedAt = microToTime(observedAt)
	o.NextProbeAt = microToTime(nextProbe)
	o.NextReconcileAt = microToTime(nextReconcile)
	o.WorkLeaseUntil = microToTime(leaseUntil)
	o.ActiveRefreshJobID = store.JobID(activeJobID)
	o.LastSuccessfulIndexAt = microToTime(indexedAt)
	return o, nil
}

func Lease(observation store.RegistryObservation) store.RegistryLease {
	return store.RegistryLease{Owner: observation.WorkLeaseOwner, Epoch: observation.WorkLeaseEpoch, Until: observation.WorkLeaseUntil}
}

func JobActive(job store.Job, now time.Time) bool {
	switch job.Status {
	case store.JobStatusPending:
		return true
	case store.JobStatusLeased:
		return job.Attempt < job.MaxAttempts || job.LeaseUntil.After(now)
	case store.JobStatusFailed:
		return job.Attempt < job.MaxAttempts
	default:
		return false
	}
}

func microToTime(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.UnixMicro(value).UTC()
}
