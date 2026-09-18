package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

const observationColumns = `registry_id, observation_version, observed_commit, observed_at, probe_status,
 probe_error, next_probe_at, next_reconcile_at, consecutive_probe_failures,
 work_lease_owner, work_lease_epoch, work_lease_until, desired_commit, desired_generation,
 active_refresh_job_id, active_refresh_trigger, successful_commit,
 successful_generation, last_successful_index_at`

func (s *Store) GetRegistryObservation(ctx context.Context, id store.RegistryID) (store.RegistryObservation, error) {
	return scanObservation(s.q.QueryRowContext(ctx, `SELECT `+observationColumns+` FROM registry_observations WHERE registry_id=?`, id))
}

func scanObservation(sc interface{ Scan(...any) error }) (store.RegistryObservation, error) {
	var o store.RegistryObservation
	var observationVersion, workLeaseEpoch int64
	var observedAt, nextProbe, nextReconcile, leaseUntil, indexedAt int64
	var jobID string
	err := sc.Scan(&o.RegistryID, &observationVersion, &o.ObservedCommit, &observedAt, &o.ProbeStatus,
		&o.ProbeError, &nextProbe, &nextReconcile, &o.ConsecutiveProbeFailures,
		&o.WorkLeaseOwner, &workLeaseEpoch, &leaseUntil, &o.DesiredCommit, &o.DesiredGeneration,
		&jobID, &o.ActiveRefreshTrigger, &o.SuccessfulCommit, &o.SuccessfulGeneration,
		&indexedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return store.RegistryObservation{}, store.ErrNotFound
	}
	if err != nil {
		return store.RegistryObservation{}, err
	}
	o.ObservationVersion = uint64(observationVersion)
	o.WorkLeaseEpoch = uint64(workLeaseEpoch)
	o.ObservedAt = microToTime(observedAt)
	o.NextProbeAt = microToTime(nextProbe)
	o.NextReconcileAt = microToTime(nextReconcile)
	o.WorkLeaseUntil = microToTime(leaseUntil)
	o.LastSuccessfulIndexAt = microToTime(indexedAt)
	o.ActiveRefreshJobID = store.JobID(jobID)
	return o, nil
}

func (s *Store) PutRegistryObservation(ctx context.Context, observation store.RegistryObservation) error {
	return s.withTx(ctx, func(tx *Store) error { return tx.putRegistryObservation(ctx, observation) })
}

func (s *Store) putRegistryObservation(ctx context.Context, o store.RegistryObservation) error {
	if o.RegistryID == "" {
		return fmt.Errorf("registry observation requires registry id")
	}
	result, err := s.q.ExecContext(ctx, `INSERT INTO registry_observations (`+observationColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(registry_id) DO NOTHING`,
		o.RegistryID, int64(1), o.ObservedCommit, timeToMicro(o.ObservedAt), o.ProbeStatus, o.ProbeError,
		timeToMicro(o.NextProbeAt), timeToMicro(o.NextReconcileAt), o.ConsecutiveProbeFailures,
		o.WorkLeaseOwner, int64(o.WorkLeaseEpoch), timeToMicro(o.WorkLeaseUntil), o.DesiredCommit, o.DesiredGeneration,
		o.ActiveRefreshJobID, o.ActiveRefreshTrigger, o.SuccessfulCommit, o.SuccessfulGeneration,
		timeToMicro(o.LastSuccessfulIndexAt))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("%w: registry observation %q already exists", store.ErrConflict, o.RegistryID)
	}
	return nil
}

func (s *Store) ListDueRegistryObservations(ctx context.Context, now time.Time, limit int) ([]store.RegistryObservation, error) {
	if limit <= 0 {
		return []store.RegistryObservation{}, nil
	}
	nowMicro := timeToMicro(now)
	rows, err := s.q.QueryContext(ctx, `SELECT `+observationColumns+` FROM registry_observations
		JOIN registries ON registries.id=registry_observations.registry_id
		WHERE registries.listed<>0 AND (next_probe_at=0 OR next_probe_at<=? OR next_reconcile_at=0 OR next_reconcile_at<=?)
		ORDER BY CASE WHEN next_probe_at=0 OR next_reconcile_at=0 THEN 0 WHEN next_reconcile_at<next_probe_at THEN next_reconcile_at ELSE next_probe_at END,
		 registry_observations.registry_id LIMIT ?`, nowMicro, nowMicro, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]store.RegistryObservation, 0)
	for rows.Next() {
		o, err := scanObservation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) CompareAndSwapRegistryObservation(ctx context.Context, id store.RegistryID, expectedVersion uint64, next store.RegistryObservation) error {
	return s.withTx(ctx, func(tx *Store) error {
		result, err := tx.q.ExecContext(ctx, `UPDATE registry_observations SET
		 observation_version=observation_version+1, observed_commit=?, observed_at=?, probe_status=?, probe_error=?, next_probe_at=?, next_reconcile_at=?,
		 consecutive_probe_failures=?, work_lease_owner=?, work_lease_epoch=?, work_lease_until=?, desired_commit=?, desired_generation=?,
		 active_refresh_job_id=?, active_refresh_trigger=?, successful_commit=?, successful_generation=?, last_successful_index_at=?
		 WHERE registry_id=? AND observation_version=?`,
			next.ObservedCommit, timeToMicro(next.ObservedAt), next.ProbeStatus, next.ProbeError,
			timeToMicro(next.NextProbeAt), timeToMicro(next.NextReconcileAt), next.ConsecutiveProbeFailures,
			next.WorkLeaseOwner, int64(next.WorkLeaseEpoch), timeToMicro(next.WorkLeaseUntil), next.DesiredCommit, next.DesiredGeneration,
			next.ActiveRefreshJobID, next.ActiveRefreshTrigger, next.SuccessfulCommit, next.SuccessfulGeneration,
			timeToMicro(next.LastSuccessfulIndexAt), id, int64(expectedVersion))
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			if _, getErr := tx.GetRegistryObservation(ctx, id); errors.Is(getErr, store.ErrNotFound) {
				return getErr
			} else if getErr != nil {
				return getErr
			}
			return fmt.Errorf("%w: registry observation %q changed", store.ErrConflict, id)
		}
		return nil
	})
}

func (s *Store) AcquireRegistryLease(ctx context.Context, id store.RegistryID, owner string, duration time.Duration) (store.RegistryObservation, error) {
	var out store.RegistryObservation
	err := s.withTx(ctx, func(tx *Store) error {
		if owner == "" || duration <= 0 {
			return store.ErrTopology
		}
		now := time.Now().UTC()
		result, err := tx.q.ExecContext(ctx, `UPDATE registry_observations SET observation_version=observation_version+1, work_lease_owner=?, work_lease_epoch=work_lease_epoch+1, work_lease_until=?
			WHERE registry_id=? AND (work_lease_owner='' OR work_lease_owner=? OR work_lease_until<=?)`, owner, timeToMicro(now.Add(duration)), id, owner, timeToMicro(now))
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			if _, err := tx.GetRegistryObservation(ctx, id); errors.Is(err, store.ErrNotFound) {
				return err
			}
			return fmt.Errorf("%w: registry observation %q lease held", store.ErrConflict, id)
		}
		out, err = tx.GetRegistryObservation(ctx, id)
		return err
	})
	return out, err
}

func (s *Store) ReleaseRegistryLease(ctx context.Context, id store.RegistryID, owner string, leaseEpoch uint64) error {
	return s.withTx(ctx, func(tx *Store) error {
		current, err := tx.GetRegistryObservation(ctx, id)
		if err != nil {
			return err
		}
		if current.WorkLeaseOwner != owner || current.WorkLeaseEpoch != leaseEpoch {
			return store.ErrConflict
		}
		now := time.Now().UTC()
		result, err := tx.q.ExecContext(ctx, `UPDATE registry_observations SET observation_version=observation_version+1, work_lease_owner='', work_lease_until=0
			WHERE registry_id=? AND work_lease_owner=? AND work_lease_epoch=? AND work_lease_until>?`, id, owner, int64(leaseEpoch), timeToMicro(now))
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return store.ErrLeaseExpired
		}
		return nil
	})
}

func (s *Store) SetDesiredRefresh(ctx context.Context, id store.RegistryID, desired store.DesiredRefresh) (store.RegistryObservation, error) {
	var out store.RegistryObservation
	err := s.withTx(ctx, func(tx *Store) error {
		current, err := tx.GetRegistryObservation(ctx, id)
		if err != nil {
			return err
		}
		activeJobLive := false
		if current.ActiveRefreshJobID != "" {
			activeJob, jobErr := tx.GetJob(ctx, current.ActiveRefreshJobID)
			switch {
			case errors.Is(jobErr, store.ErrNotFound):
			case jobErr != nil:
				return jobErr
			default:
				activeJobLive = !activeJob.Status.IsTerminal()
			}
		}
		if !activeJobLive || current.DesiredCommit != desired.Commit || current.DesiredGeneration != desired.Generation {
			desiredJob, err := tx.GetJob(ctx, desired.JobID)
			if err != nil {
				return err
			}
			if desiredJob.Status.IsTerminal() {
				return fmt.Errorf("%w: desired refresh job %q is terminal", store.ErrConflict, desired.JobID)
			}
			_, err = tx.q.ExecContext(ctx, `UPDATE registry_observations SET observation_version=observation_version+1, desired_commit=?, desired_generation=?, active_refresh_job_id=?, active_refresh_trigger=? WHERE registry_id=?`, desired.Commit, desired.Generation, desired.JobID, desired.Trigger, id)
			if err != nil {
				return err
			}
		}
		out, err = tx.GetRegistryObservation(ctx, id)
		return err
	})
	return out, err
}

func (s *Store) ClearActiveRefresh(ctx context.Context, id store.RegistryID, jobID store.JobID) (store.RegistryObservation, error) {
	var out store.RegistryObservation
	err := s.withTx(ctx, func(tx *Store) error {
		_, err := tx.q.ExecContext(ctx, `UPDATE registry_observations SET observation_version=observation_version+1, active_refresh_job_id='', active_refresh_trigger='' WHERE registry_id=? AND active_refresh_job_id=?`, id, jobID)
		if err != nil {
			return err
		}
		out, err = tx.GetRegistryObservation(ctx, id)
		return err
	})
	return out, err
}

func (s *Store) AcquireHostBudget(ctx context.Context, host, owner string, acquisition bool, limit store.HostBudgetLimit, duration time.Duration) (store.HostBudgetLease, error) {
	var out store.HostBudgetLease
	err := s.withTx(ctx, func(tx *Store) error {
		if host == "" || owner == "" || duration <= 0 || limit.MaxOperations <= 0 || (acquisition && limit.MaxAcquisitions <= 0) {
			return store.ErrTopology
		}
		now := time.Now().UTC()
		if _, err := tx.q.ExecContext(ctx, `INSERT OR IGNORE INTO host_budgets(host) VALUES (?)`, host); err != nil {
			return err
		}
		var existingAcq, existingEpoch int64
		var existingExpired bool
		err := tx.q.QueryRowContext(ctx, `SELECT acquisition, lease_epoch, lease_until<=? FROM host_budget_leases WHERE host=? AND owner=?`, timeToMicro(now), host, owner).Scan(&existingAcq, &existingEpoch, &existingExpired)
		if err == nil && !existingExpired {
			if (existingAcq != 0) != acquisition {
				return store.ErrConflict
			}
			out = store.HostBudgetLease{Host: host, Owner: owner, Acquisition: acquisition, LeaseEpoch: uint64(existingEpoch + 1), LeaseUntil: now.Add(duration)}
			_, err = tx.q.ExecContext(ctx, `UPDATE host_budget_leases SET acquisition=?, lease_until=?, lease_epoch=? WHERE host=? AND owner=?`, acquisition, timeToMicro(out.LeaseUntil), int64(out.LeaseEpoch), host, owner)
			return err
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			if _, err := tx.q.ExecContext(ctx, `DELETE FROM host_budget_leases WHERE host=? AND owner=?`, host, owner); err != nil {
				return err
			}
		}
		var operations, acquisitions int
		if err := tx.q.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(acquisition),0) FROM host_budget_leases WHERE host=? AND lease_until>?`, host, timeToMicro(now)).Scan(&operations, &acquisitions); err != nil {
			return err
		}
		if operations >= limit.MaxOperations || (acquisition && acquisitions >= limit.MaxAcquisitions) {
			return store.ErrConflict
		}
		out = store.HostBudgetLease{Host: host, Owner: owner, Acquisition: acquisition, LeaseEpoch: uint64(existingEpoch + 1), LeaseUntil: now.Add(duration)}
		_, err = tx.q.ExecContext(ctx, `INSERT INTO host_budget_leases(host, owner, acquisition, lease_epoch, lease_until) VALUES (?, ?, ?, ?, ?)`, host, owner, acquisition, int64(out.LeaseEpoch), timeToMicro(out.LeaseUntil))
		return err
	})
	return out, err
}

func (s *Store) ReleaseHostBudget(ctx context.Context, host, owner string, leaseEpoch uint64) error {
	return s.withTx(ctx, func(tx *Store) error {
		result, err := tx.q.ExecContext(ctx, `DELETE FROM host_budget_leases WHERE host=? AND owner=? AND lease_epoch=?`, host, owner, int64(leaseEpoch))
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			var currentEpoch int64
			err := tx.q.QueryRowContext(ctx, `SELECT lease_epoch FROM host_budget_leases WHERE host=? AND owner=?`, host, owner).Scan(&currentEpoch)
			if errors.Is(err, sql.ErrNoRows) {
				return store.ErrNotFound
			}
			if err != nil {
				return err
			}
			return store.ErrConflict
		}
		return nil
	})
}
