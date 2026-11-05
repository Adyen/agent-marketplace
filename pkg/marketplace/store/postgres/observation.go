package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	sharedobservation "github.com/Adyen/agent-marketplace/pkg/marketplace/store/internal/observation"
)

func (s *Store) CreateRegistryObservation(ctx context.Context, observation store.RegistryObservation) (store.RegistryObservation, error) {
	var created store.RegistryObservation
	err := s.withTx(ctx, func(tx *Store) error {
		if observation.RegistryID == "" {
			return fmt.Errorf("registry observation requires registry id")
		}
		result, err := tx.q.ExecContext(ctx, `INSERT INTO registry_observations (`+sharedobservation.Columns+`)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
			ON CONFLICT (registry_id) DO NOTHING`,
			observation.RegistryID, 1, observation.ObservedCommit, timeToMicro(observation.ObservedAt), observation.ProbeStatus,
			observation.ProbeError, timeToMicro(observation.NextProbeAt), timeToMicro(observation.NextReconcileAt),
			observation.ConsecutiveProbeFailures, observation.WorkLeaseOwner, observation.WorkLeaseEpoch,
			timeToMicro(observation.WorkLeaseUntil), observation.DesiredCommit, observation.DesiredGeneration,
			observation.ActiveRefreshJobID, observation.ActiveRefreshTrigger, observation.SuccessfulCommit,
			observation.SuccessfulGeneration, timeToMicro(observation.LastSuccessfulIndexAt))
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("%w: registry observation %q already exists", store.ErrConflict, observation.RegistryID)
		}
		created, err = tx.GetRegistryObservation(ctx, observation.RegistryID)
		return err
	})
	return created, err
}

func (s *Store) GetRegistryObservation(ctx context.Context, id store.RegistryID) (store.RegistryObservation, error) {
	return sharedobservation.Scan(s.q.QueryRowContext(ctx, `SELECT `+sharedobservation.Columns+` FROM registry_observations WHERE registry_id=$1`, id))
}

func (s *Store) UpdateRegistryObservation(ctx context.Context, expectedVersion uint64, next store.RegistryObservation) (store.RegistryObservation, error) {
	var updated store.RegistryObservation
	err := s.withTx(ctx, func(tx *Store) error {
		result, err := tx.q.ExecContext(ctx, `UPDATE registry_observations SET
			version=version+1, observed_commit=$1, observed_at=$2, probe_status=$3, probe_error=$4,
			next_probe_at=$5, next_reconcile_at=$6, consecutive_probe_failures=$7, desired_commit=$8,
			desired_generation=$9, successful_commit=$10, successful_generation=$11, last_successful_index_at=$12
			WHERE registry_id=$13 AND version=$14`,
			next.ObservedCommit, timeToMicro(next.ObservedAt), next.ProbeStatus, next.ProbeError,
			timeToMicro(next.NextProbeAt), timeToMicro(next.NextReconcileAt), next.ConsecutiveProbeFailures,
			next.DesiredCommit, next.DesiredGeneration, next.SuccessfulCommit, next.SuccessfulGeneration,
			timeToMicro(next.LastSuccessfulIndexAt), next.RegistryID, expectedVersion)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			if _, err := tx.GetRegistryObservation(ctx, next.RegistryID); errors.Is(err, store.ErrNotFound) {
				return err
			} else if err != nil {
				return err
			}
			return fmt.Errorf("%w: registry observation %q changed", store.ErrConflict, next.RegistryID)
		}
		updated, err = tx.GetRegistryObservation(ctx, next.RegistryID)
		return err
	})
	return updated, err
}

func (s *Store) DeleteRegistryObservation(ctx context.Context, id store.RegistryID) error {
	return s.withTx(ctx, func(tx *Store) error {
		result, err := tx.q.ExecContext(ctx, `DELETE FROM registry_observations WHERE registry_id=$1`, id)
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

func (s *Store) ListDueRegistryObservations(ctx context.Context, now time.Time, limit int) ([]store.RegistryObservation, error) {
	if limit <= 0 {
		return []store.RegistryObservation{}, nil
	}
	nowMicro := timeToMicro(now)
	rows, err := s.q.QueryContext(ctx, `SELECT `+sharedobservation.Columns+` FROM registry_observations
		WHERE (next_probe_at=0 OR next_probe_at<=$1 OR next_reconcile_at=0 OR next_reconcile_at<=$1)
		AND work_lease_until<=$1
		ORDER BY CASE WHEN next_probe_at=0 OR next_reconcile_at=0 THEN 0
			WHEN next_probe_at<next_reconcile_at THEN next_probe_at ELSE next_reconcile_at END,
			registry_id COLLATE "C" LIMIT $2`, nowMicro, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]store.RegistryObservation, 0)
	for rows.Next() {
		observation, err := sharedobservation.Scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, observation)
	}
	return out, rows.Err()
}

func (s *Store) AcquireRegistryLease(ctx context.Context, id store.RegistryID, owner string, ttl time.Duration) (store.RegistryLease, error) {
	var lease store.RegistryLease
	err := s.withTx(ctx, func(tx *Store) error {
		if owner == "" || ttl <= 0 {
			return fmt.Errorf("registry lease requires owner and positive ttl")
		}
		now := time.Now().UTC()
		result, err := tx.q.ExecContext(ctx, `UPDATE registry_observations SET
			version=version+1, work_lease_owner=$1, work_lease_epoch=work_lease_epoch+1, work_lease_until=$2
			WHERE registry_id=$3 AND (work_lease_owner='' OR work_lease_until<=$4)`,
			owner, timeToMicro(now.Add(ttl)), id, timeToMicro(now))
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			if _, err := tx.GetRegistryObservation(ctx, id); errors.Is(err, store.ErrNotFound) {
				return err
			} else if err != nil {
				return err
			}
			return fmt.Errorf("%w: registry lease held for %q", store.ErrConflict, id)
		}
		observation, err := tx.GetRegistryObservation(ctx, id)
		if err != nil {
			return err
		}
		lease = sharedobservation.Lease(observation)
		return nil
	})
	return lease, err
}

func (s *Store) RenewRegistryLease(ctx context.Context, id store.RegistryID, lease store.RegistryLease, ttl time.Duration) (store.RegistryLease, error) {
	var renewed store.RegistryLease
	err := s.withTx(ctx, func(tx *Store) error {
		if lease.Owner == "" || lease.Epoch == 0 || ttl <= 0 {
			return store.ErrLeaseExpired
		}
		now := time.Now().UTC()
		result, err := tx.q.ExecContext(ctx, `UPDATE registry_observations SET
			version=version+1, work_lease_epoch=work_lease_epoch+1, work_lease_until=$1
			WHERE registry_id=$2 AND work_lease_owner=$3 AND work_lease_epoch=$4 AND work_lease_until>$5`,
			timeToMicro(now.Add(ttl)), id, lease.Owner, lease.Epoch, timeToMicro(now))
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			if _, err := tx.GetRegistryObservation(ctx, id); errors.Is(err, store.ErrNotFound) {
				return err
			} else if err != nil {
				return err
			}
			return store.ErrLeaseExpired
		}
		observation, err := tx.GetRegistryObservation(ctx, id)
		if err != nil {
			return err
		}
		renewed = sharedobservation.Lease(observation)
		return nil
	})
	return renewed, err
}

func (s *Store) ReleaseRegistryLease(ctx context.Context, id store.RegistryID, lease store.RegistryLease) error {
	return s.withTx(ctx, func(tx *Store) error {
		now := time.Now().UTC()
		result, err := tx.q.ExecContext(ctx, `UPDATE registry_observations SET
			version=version+1, work_lease_owner='', work_lease_until=0
			WHERE registry_id=$1 AND work_lease_owner=$2 AND work_lease_epoch=$3 AND work_lease_until>$4`,
			id, lease.Owner, lease.Epoch, timeToMicro(now))
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			if _, err := tx.GetRegistryObservation(ctx, id); errors.Is(err, store.ErrNotFound) {
				return err
			} else if err != nil {
				return err
			}
			return store.ErrLeaseExpired
		}
		return nil
	})
}

func (s *Store) RequestRefresh(ctx context.Context, id store.RegistryID, request store.RefreshRequest) (store.RefreshDecision, error) {
	var decision store.RefreshDecision
	err := s.withTx(ctx, func(tx *Store) error {
		// Serialize equivalent requests before reading the active job.
		result, err := tx.q.ExecContext(ctx, `UPDATE registry_observations SET version=version WHERE registry_id=$1`, id)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			if _, err := tx.GetRegistryObservation(ctx, id); errors.Is(err, store.ErrNotFound) {
				return err
			} else if err != nil {
				return err
			}
		}
		current, err := tx.GetRegistryObservation(ctx, id)
		if err != nil {
			return err
		}
		if current.ActiveRefreshJobID != "" {
			active, err := tx.GetJob(ctx, current.ActiveRefreshJobID)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			if err == nil && current.DesiredCommit == request.Commit && current.DesiredGeneration == request.Generation && sharedobservation.JobActive(active, time.Now().UTC()) {
				decision = store.RefreshDecision{Job: active, Coalesced: true}
				return nil
			}
		}
		if _, err := tx.q.ExecContext(ctx, `UPDATE registry_observations SET
			desired_commit=$1, desired_generation=$2
			WHERE registry_id=$3`, request.Commit, request.Generation, id); err != nil {
			return err
		}
		if err := tx.EnqueueJob(ctx, request.Job); err != nil {
			return err
		}
		if _, err := tx.q.ExecContext(ctx, `UPDATE registry_observations SET
			version=version+1, active_refresh_job_id=$1, active_refresh_trigger=$2
			WHERE registry_id=$3`, request.Job.ID, request.Trigger, id); err != nil {
			return err
		}
		decision.Job, err = tx.GetJob(ctx, request.Job.ID)
		return err
	})
	return decision, err
}

func (s *Store) AcquireHostBudget(ctx context.Context, host, owner string, acquisition bool, limit store.HostBudgetLimit, ttl time.Duration) (store.HostBudgetLease, error) {
	var lease store.HostBudgetLease
	err := s.withTx(ctx, func(tx *Store) error {
		if host == "" || owner == "" || ttl <= 0 {
			return fmt.Errorf("host budget requires host, owner, and positive ttl")
		}
		// Serialize admission checks across workers sharing this metadata store.
		if _, err := tx.q.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`, host); err != nil {
			return err
		}
		now := time.Now().UTC()
		nowMicro := timeToMicro(now)
		var operations, acquisitions int
		if err := tx.q.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(CASE WHEN acquisition THEN 1 ELSE 0 END), 0)
			FROM host_budget_leases WHERE host=$1 AND lease_until>$2`, host, nowMicro).Scan(&operations, &acquisitions); err != nil {
			return err
		}
		if operations >= limit.Operations || acquisition && acquisitions >= limit.Acquisitions {
			return fmt.Errorf("%w: host budget exhausted for %q", store.ErrConflict, host)
		}
		var epoch int64
		err := tx.q.QueryRowContext(ctx, `SELECT epoch FROM host_budget_leases WHERE host=$1 AND owner=$2`, host, owner).Scan(&epoch)
		if errors.Is(err, sql.ErrNoRows) {
			epoch = 0
		} else if err != nil {
			return err
		}
		epoch++
		until := now.Add(ttl)
		_, err = tx.q.ExecContext(ctx, `INSERT INTO host_budget_leases (host, owner, acquisition, epoch, lease_until)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (host, owner) DO UPDATE SET acquisition=EXCLUDED.acquisition, epoch=EXCLUDED.epoch, lease_until=EXCLUDED.lease_until`,
			host, owner, acquisition, epoch, timeToMicro(until))
		if err != nil {
			return err
		}
		lease = store.HostBudgetLease{Host: host, Owner: owner, Acquisition: acquisition, Epoch: uint64(epoch), Until: until}
		return nil
	})
	return lease, err
}

func (s *Store) ReleaseHostBudget(ctx context.Context, lease store.HostBudgetLease) error {
	return s.withTx(ctx, func(tx *Store) error {
		result, err := tx.q.ExecContext(ctx, `UPDATE host_budget_leases SET epoch=epoch+1, lease_until=0
			WHERE host=$1 AND owner=$2 AND acquisition=$3 AND epoch=$4 AND lease_until>$5`,
			lease.Host, lease.Owner, lease.Acquisition, lease.Epoch, timeToMicro(time.Now().UTC()))
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return store.ErrLeaseExpired
		}
		return nil
	})
}
