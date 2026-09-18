package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

const observationColumns = `registry_id, observed_commit, observed_at, probe_status,
 probe_error, next_probe_at, next_reconcile_at, consecutive_probe_failures,
 work_lease_owner, work_lease_until, desired_commit, desired_generation,
 active_refresh_job_id, active_refresh_trigger, successful_commit,
 successful_generation, last_successful_index_at`

func (s *Store) GetRegistryObservation(ctx context.Context, id store.RegistryID) (store.RegistryObservation, error) {
	return scanObservation(s.q.QueryRowContext(ctx, `SELECT `+observationColumns+` FROM registry_observations WHERE registry_id=?`, id))
}

func scanObservation(sc interface{ Scan(...any) error }) (store.RegistryObservation, error) {
	var o store.RegistryObservation
	var observedAt, nextProbe, nextReconcile, leaseUntil, indexedAt int64
	var jobID string
	err := sc.Scan(&o.RegistryID, &o.ObservedCommit, &observedAt, &o.ProbeStatus,
		&o.ProbeError, &nextProbe, &nextReconcile, &o.ConsecutiveProbeFailures,
		&o.WorkLeaseOwner, &leaseUntil, &o.DesiredCommit, &o.DesiredGeneration,
		&jobID, &o.ActiveRefreshTrigger, &o.SuccessfulCommit, &o.SuccessfulGeneration,
		&indexedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return store.RegistryObservation{}, store.ErrNotFound
	}
	if err != nil {
		return store.RegistryObservation{}, err
	}
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
	_, err := s.q.ExecContext(ctx, `INSERT INTO registry_observations (`+observationColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(registry_id) DO UPDATE SET
		 observed_commit=excluded.observed_commit, observed_at=excluded.observed_at,
		 probe_status=excluded.probe_status, probe_error=excluded.probe_error,
		 next_probe_at=excluded.next_probe_at, next_reconcile_at=excluded.next_reconcile_at,
		 consecutive_probe_failures=excluded.consecutive_probe_failures,
		 work_lease_owner=excluded.work_lease_owner, work_lease_until=excluded.work_lease_until,
		 desired_commit=excluded.desired_commit, desired_generation=excluded.desired_generation,
		 active_refresh_job_id=excluded.active_refresh_job_id,
		 active_refresh_trigger=excluded.active_refresh_trigger,
		 successful_commit=excluded.successful_commit, successful_generation=excluded.successful_generation,
		 last_successful_index_at=excluded.last_successful_index_at`,
		o.RegistryID, o.ObservedCommit, timeToMicro(o.ObservedAt), o.ProbeStatus, o.ProbeError,
		timeToMicro(o.NextProbeAt), timeToMicro(o.NextReconcileAt), o.ConsecutiveProbeFailures,
		o.WorkLeaseOwner, timeToMicro(o.WorkLeaseUntil), o.DesiredCommit, o.DesiredGeneration,
		o.ActiveRefreshJobID, o.ActiveRefreshTrigger, o.SuccessfulCommit, o.SuccessfulGeneration,
		timeToMicro(o.LastSuccessfulIndexAt))
	return err
}

func (s *Store) ListDueRegistryObservations(ctx context.Context, now time.Time, limit int) ([]store.RegistryObservation, error) {
	if limit <= 0 {
		return []store.RegistryObservation{}, nil
	}
	nowMicro := timeToMicro(now)
	rows, err := s.q.QueryContext(ctx, `SELECT `+observationColumns+` FROM registry_observations
		WHERE (next_probe_at=0 OR next_probe_at<=? OR next_reconcile_at=0 OR next_reconcile_at<=?)
		ORDER BY CASE WHEN next_probe_at=0 OR next_reconcile_at=0 THEN 0 WHEN next_reconcile_at<next_probe_at THEN next_reconcile_at ELSE next_probe_at END,
		 registry_id LIMIT ?`, nowMicro, nowMicro, limit)
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

func (s *Store) CompareAndSwapRegistryObservation(ctx context.Context, id store.RegistryID, expectedObservedAt time.Time, next store.RegistryObservation) error {
	return s.withTx(ctx, func(tx *Store) error {
		next.RegistryID = id
		result, err := tx.q.ExecContext(ctx, `UPDATE registry_observations SET
		 observed_commit=?, observed_at=?, probe_status=?, probe_error=?, next_probe_at=?, next_reconcile_at=?,
		 consecutive_probe_failures=?, work_lease_owner=?, work_lease_until=?, desired_commit=?, desired_generation=?,
		 active_refresh_job_id=?, active_refresh_trigger=?, successful_commit=?, successful_generation=?, last_successful_index_at=?
		 WHERE registry_id=? AND observed_at=?`,
			next.ObservedCommit, timeToMicro(next.ObservedAt), next.ProbeStatus, next.ProbeError,
			timeToMicro(next.NextProbeAt), timeToMicro(next.NextReconcileAt), next.ConsecutiveProbeFailures,
			next.WorkLeaseOwner, timeToMicro(next.WorkLeaseUntil), next.DesiredCommit, next.DesiredGeneration,
			next.ActiveRefreshJobID, next.ActiveRefreshTrigger, next.SuccessfulCommit, next.SuccessfulGeneration,
			timeToMicro(next.LastSuccessfulIndexAt), id, timeToMicro(expectedObservedAt))
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
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
		result, err := tx.q.ExecContext(ctx, `UPDATE registry_observations SET work_lease_owner=?, work_lease_until=?
			WHERE registry_id=? AND (work_lease_owner='' OR work_lease_until<=?)`, owner, timeToMicro(now.Add(duration)), id, timeToMicro(now))
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

func (s *Store) ReleaseRegistryLease(ctx context.Context, id store.RegistryID, owner string) error {
	return s.withTx(ctx, func(tx *Store) error {
		result, err := tx.q.ExecContext(ctx, `UPDATE registry_observations SET work_lease_owner='', work_lease_until=0
			WHERE registry_id=? AND work_lease_owner=? AND work_lease_until>?`, id, owner, timeToMicro(time.Now().UTC()))
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
		if current.ActiveRefreshJobID == "" || current.DesiredCommit != desired.Commit || current.DesiredGeneration != desired.Generation {
			_, err = tx.q.ExecContext(ctx, `UPDATE registry_observations SET desired_commit=?, desired_generation=?, active_refresh_job_id=?, active_refresh_trigger=? WHERE registry_id=?`, desired.Commit, desired.Generation, desired.JobID, desired.Trigger, id)
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
		_, err := tx.q.ExecContext(ctx, `UPDATE registry_observations SET active_refresh_job_id='', active_refresh_trigger='' WHERE registry_id=? AND active_refresh_job_id=?`, id, jobID)
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
		if _, err := tx.q.ExecContext(ctx, `DELETE FROM host_budget_leases WHERE host=? AND lease_until<=?`, host, timeToMicro(now)); err != nil {
			return err
		}
		var existingAcq int
		err := tx.q.QueryRowContext(ctx, `SELECT acquisition FROM host_budget_leases WHERE host=? AND owner=?`, host, owner).Scan(&existingAcq)
		if err == nil {
			if (existingAcq != 0) != acquisition {
				return store.ErrConflict
			}
			out = store.HostBudgetLease{Host: host, Owner: owner, Acquisition: existingAcq != 0, LeaseUntil: now.Add(duration)}
			_, err = tx.q.ExecContext(ctx, `UPDATE host_budget_leases SET lease_until=? WHERE host=? AND owner=?`, timeToMicro(out.LeaseUntil), host, owner)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var operations, acquisitions int
		if err := tx.q.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(acquisition),0) FROM host_budget_leases WHERE host=?`, host).Scan(&operations, &acquisitions); err != nil {
			return err
		}
		if operations >= limit.MaxOperations || (acquisition && acquisitions >= limit.MaxAcquisitions) {
			return store.ErrConflict
		}
		out = store.HostBudgetLease{Host: host, Owner: owner, Acquisition: acquisition, LeaseUntil: now.Add(duration)}
		_, err = tx.q.ExecContext(ctx, `INSERT INTO host_budget_leases(host, owner, acquisition, lease_until) VALUES (?, ?, ?, ?)`, host, owner, acquisition, timeToMicro(out.LeaseUntil))
		return err
	})
	return out, err
}

func (s *Store) ReleaseHostBudget(ctx context.Context, host, owner string) error {
	return s.withTx(ctx, func(tx *Store) error {
		result, err := tx.q.ExecContext(ctx, `DELETE FROM host_budget_leases WHERE host=? AND owner=?`, host, owner)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return store.ErrNotFound
		}
		return nil
	})
}
