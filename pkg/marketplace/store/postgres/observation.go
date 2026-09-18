package postgres

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
	return scanObservation(s.q.QueryRowContext(ctx, `SELECT `+observationColumns+` FROM registry_observations WHERE registry_id=$1`, id))
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
	o.ObservedAt, o.NextProbeAt, o.NextReconcileAt = microToTime(observedAt), microToTime(nextProbe), microToTime(nextReconcile)
	o.WorkLeaseUntil, o.LastSuccessfulIndexAt = microToTime(leaseUntil), microToTime(indexedAt)
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
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
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
		WHERE registries.listed=TRUE AND (next_probe_at=0 OR next_probe_at<= $1 OR next_reconcile_at=0 OR next_reconcile_at<=$1)
		ORDER BY CASE WHEN next_probe_at=0 OR next_reconcile_at=0 THEN 0 WHEN next_reconcile_at<next_probe_at THEN next_reconcile_at ELSE next_probe_at END, registry_observations.registry_id COLLATE "C" LIMIT $2`, nowMicro, limit)
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
		next.RegistryID = id
		result, err := tx.q.ExecContext(ctx, `UPDATE registry_observations SET observation_version=observation_version+1, observed_commit=$1, observed_at=$2, probe_status=$3, probe_error=$4,
		 next_probe_at=$5, next_reconcile_at=$6, consecutive_probe_failures=$7, work_lease_owner=$8, work_lease_epoch=$9, work_lease_until=$10,
		 desired_commit=$11, desired_generation=$12, active_refresh_job_id=$13, active_refresh_trigger=$14, successful_commit=$15,
		 successful_generation=$16, last_successful_index_at=$17 WHERE registry_id=$18 AND observation_version=$19`,
			next.ObservedCommit, timeToMicro(next.ObservedAt), next.ProbeStatus, next.ProbeError, timeToMicro(next.NextProbeAt), timeToMicro(next.NextReconcileAt), next.ConsecutiveProbeFailures,
			next.WorkLeaseOwner, int64(next.WorkLeaseEpoch), timeToMicro(next.WorkLeaseUntil), next.DesiredCommit, next.DesiredGeneration, next.ActiveRefreshJobID, next.ActiveRefreshTrigger, next.SuccessfulCommit, next.SuccessfulGeneration, timeToMicro(next.LastSuccessfulIndexAt), id, int64(expectedVersion))
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
		result, err := tx.q.ExecContext(ctx, `UPDATE registry_observations SET observation_version=observation_version+1, work_lease_owner=$1, work_lease_epoch=work_lease_epoch+1, work_lease_until=`+dbNowMicro+`+$2 WHERE registry_id=$3 AND (work_lease_owner='' OR work_lease_owner=$1 OR work_lease_until<=`+dbNowMicro+`)`, owner, duration.Microseconds(), id)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			if _, e := tx.GetRegistryObservation(ctx, id); errors.Is(e, store.ErrNotFound) {
				return e
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
		result, err := tx.q.ExecContext(ctx, `UPDATE registry_observations SET observation_version=observation_version+1, work_lease_owner='', work_lease_until=0 WHERE registry_id=$1 AND work_lease_owner=$2 AND work_lease_epoch=$3 AND work_lease_until>`+dbNowMicro, id, owner, int64(leaseEpoch))
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
		if _, err := tx.q.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`, "agent-marketplace:registry-observation:"+id.String()); err != nil {
			return err
		}
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
			if _, err = tx.q.ExecContext(ctx, `UPDATE registry_observations SET observation_version=observation_version+1, desired_commit=$1, desired_generation=$2, active_refresh_job_id=$3, active_refresh_trigger=$4 WHERE registry_id=$5`, desired.Commit, desired.Generation, desired.JobID, desired.Trigger, id); err != nil {
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
		_, err := tx.q.ExecContext(ctx, `UPDATE registry_observations SET observation_version=observation_version+1, active_refresh_job_id='', active_refresh_trigger='' WHERE registry_id=$1 AND active_refresh_job_id=$2`, id, jobID)
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
		if _, err := tx.q.ExecContext(ctx, `INSERT INTO host_budgets(host) VALUES ($1) ON CONFLICT(host) DO NOTHING`, host); err != nil {
			return err
		}
		var lockedHost string
		if err := tx.q.QueryRowContext(ctx, `SELECT host FROM host_budgets WHERE host=$1 FOR UPDATE`, host).Scan(&lockedHost); err != nil {
			return err
		}
		var existing bool
		var existingEpoch int64
		var existingExpired bool
		err := tx.q.QueryRowContext(ctx, `SELECT acquisition, lease_epoch, lease_until<=`+dbNowMicro+` FROM host_budget_leases WHERE host=$1 AND owner=$2`, host, owner).Scan(&existing, &existingEpoch, &existingExpired)
		if err == nil && !existingExpired {
			if existing != acquisition {
				return store.ErrConflict
			}
			_, err = tx.q.ExecContext(ctx, `UPDATE host_budget_leases SET acquisition=$1, lease_epoch=lease_epoch+1, lease_until=`+dbNowMicro+`+$2 WHERE host=$3 AND owner=$4`, acquisition, duration.Microseconds(), host, owner)
			if err != nil {
				return err
			}
			var leaseUntil int64
			if err := tx.q.QueryRowContext(ctx, `SELECT lease_epoch, lease_until FROM host_budget_leases WHERE host=$1 AND owner=$2`, host, owner).Scan(&existingEpoch, &leaseUntil); err != nil {
				return err
			}
			out = store.HostBudgetLease{Host: host, Owner: owner, Acquisition: acquisition, LeaseEpoch: uint64(existingEpoch), LeaseUntil: microToTime(leaseUntil)}
			return nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			if _, err := tx.q.ExecContext(ctx, `DELETE FROM host_budget_leases WHERE host=$1 AND owner=$2`, host, owner); err != nil {
				return err
			}
		}
		var operations, acquisitions int
		if err := tx.q.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(CASE WHEN acquisition THEN 1 ELSE 0 END),0) FROM host_budget_leases WHERE host=$1 AND lease_until>`+dbNowMicro, host).Scan(&operations, &acquisitions); err != nil {
			return err
		}
		if operations >= limit.MaxOperations || (acquisition && acquisitions >= limit.MaxAcquisitions) {
			return store.ErrConflict
		}
		_, err = tx.q.ExecContext(ctx, `INSERT INTO host_budget_leases(host, owner, acquisition, lease_epoch, lease_until) VALUES ($1,$2,$3,$5,`+dbNowMicro+`+$4)`, host, owner, acquisition, duration.Microseconds(), existingEpoch+1)
		if err != nil {
			return err
		}
		var leaseUntil int64
		if err := tx.q.QueryRowContext(ctx, `SELECT lease_until FROM host_budget_leases WHERE host=$1 AND owner=$2`, host, owner).Scan(&leaseUntil); err != nil {
			return err
		}
		out = store.HostBudgetLease{Host: host, Owner: owner, Acquisition: acquisition, LeaseEpoch: uint64(existingEpoch + 1), LeaseUntil: microToTime(leaseUntil)}
		return nil
	})
	return out, err
}

func (s *Store) ReleaseHostBudget(ctx context.Context, host, owner string, leaseEpoch uint64) error {
	return s.withTx(ctx, func(tx *Store) error {
		result, err := tx.q.ExecContext(ctx, `DELETE FROM host_budget_leases WHERE host=$1 AND owner=$2 AND lease_epoch=$3`, host, owner, int64(leaseEpoch))
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			var currentEpoch int64
			err := tx.q.QueryRowContext(ctx, `SELECT lease_epoch FROM host_budget_leases WHERE host=$1 AND owner=$2`, host, owner).Scan(&currentEpoch)
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
