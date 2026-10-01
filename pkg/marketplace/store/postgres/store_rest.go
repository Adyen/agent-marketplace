package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// -- Registry occurrences --

func (s *Store) GetRegistryOccurrence(ctx context.Context, registry store.RegistryID, commit string) (store.RegistryOccurrence, error) {
	row := s.q.QueryRowContext(ctx,
		`SELECT registry_id, ref, roots, revision_commit, manifest_digest, acquisition,
		        mirrored_artifact_digest, source_origin, occurred_at
		 FROM all_registry_occurrences WHERE registry_id=$1 AND revision_commit=$2
		 ORDER BY CASE WHEN occurrence_key LIKE 'legacy:%' THEN 0 ELSE 1 END, occurred_at DESC, occurrence_key DESC LIMIT 1`,
		registry, commit,
	)
	return s.scanRegistryOccurrence(row)
}

func (s *Store) scanRegistryOccurrence(sc sourceScanner) (store.RegistryOccurrence, error) {
	var occ store.RegistryOccurrence
	var mirrored, sourceOrigin sql.NullString
	var rootsJSON string
	var occurred int64
	err := sc.Scan(
		&occ.RegistryID, &occ.Ref, &rootsJSON, &occ.Commit, &occ.RegistryRevisionManifestDigest,
		&occ.Acquisition, &mirrored, &sourceOrigin, &occurred,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return store.RegistryOccurrence{}, store.ErrNotFound
	}
	if err != nil {
		return store.RegistryOccurrence{}, err
	}
	if err := unmarshalJSON([]byte(rootsJSON), &occ.Roots); err != nil {
		return store.RegistryOccurrence{}, fmt.Errorf("unmarshal occurrence roots: %w", err)
	}
	occ.MirroredArtifactDigest = store.ArchiveDigest(mirrored.String)
	occ.SourceOrigin = sourceOrigin.String
	occ.OccurredAt = microToTime(occurred)
	return occ, nil
}

func (s *Store) PutRegistryOccurrence(ctx context.Context, occ store.RegistryOccurrence) error {
	return s.withTx(ctx, func(tx *Store) error { return tx.putRegistryOccurrence(ctx, occ) })
}

func (s *Store) putRegistryOccurrence(ctx context.Context, occ store.RegistryOccurrence) error {
	occ = store.NormalizeRegistryOccurrence(occ)
	if err := occ.Acquisition.Validate(); err != nil {
		return err
	}
	if occ.OccurredAt.IsZero() {
		occ.OccurredAt = time.Now().UTC()
	}
	mirrored := string(occ.MirroredArtifactDigest)
	roots := occ.Roots
	if roots == nil {
		roots = []store.RegistryRoot{}
	}
	rootsJSON, err := marshalJSON(roots)
	if err != nil {
		return err
	}
	result, err := s.q.ExecContext(ctx,
		`INSERT INTO registry_occurrences
		 (registry_id, ref, roots, revision_commit, manifest_digest, acquisition, mirrored_artifact_digest, source_origin, occurred_at)
		 VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), NULLIF($8, ''), $9)
		 ON CONFLICT (registry_id, revision_commit) DO NOTHING`,
		occ.RegistryID, occ.Ref, rootsJSON, occ.Commit, occ.RegistryRevisionManifestDigest,
		occ.Acquisition, mirrored, occ.SourceOrigin, timeToMicro(occ.OccurredAt),
	)
	if err != nil {
		return err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	changed := inserted > 0
	existing, err := s.GetRegistryOccurrence(ctx, occ.RegistryID, occ.Commit)
	if err != nil {
		return err
	}
	core := existing
	core.SourceOrigin = occ.SourceOrigin
	if !occ.Equal(core) {
		return fmt.Errorf("%w: registry occurrence %q already has different immutable fields", store.ErrConflict, occ.Commit)
	}
	if existing.SourceOrigin != "" && occ.SourceOrigin != "" && existing.SourceOrigin != occ.SourceOrigin {
		return fmt.Errorf("%w: registry occurrence %q already has different source origin", store.ErrConflict, occ.Commit)
	}
	if existing.SourceOrigin == "" && occ.SourceOrigin != "" {
		result, err := s.q.ExecContext(ctx, `UPDATE registry_occurrences SET source_origin=$1 WHERE registry_id=$2 AND revision_commit=$3 AND source_origin IS NULL`, occ.SourceOrigin, occ.RegistryID, occ.Commit)
		if err != nil {
			return err
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return err
		}
		changed = changed || updated > 0
	}
	result, err = s.q.ExecContext(ctx, `INSERT INTO registry_heads (registry_id, revision_commit) VALUES ($1, $2) ON CONFLICT (registry_id) DO NOTHING`, occ.RegistryID, occ.Commit)
	if err != nil {
		return err
	}
	inserted, err = result.RowsAffected()
	if err != nil {
		return err
	}
	changed = changed || inserted > 0
	if !changed {
		return nil
	}
	return s.advanceCatalogVersion(ctx)
}

func (s *Store) FenceRegistryPublication(ctx context.Context, req store.RegistryRevisionPublication) (bool, error) {
	var current bool
	err := s.withTx(ctx, func(tx *Store) error {
		if _, err := tx.q.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`, "agent-marketplace:registry-head:"+req.Occurrence.RegistryID.String()); err != nil {
			return err
		}
		var err error
		current, err = store.FenceRegistryPublication(ctx, tx.q, tx, req)
		return err
	})
	return current, err
}

func (s *Store) SetRegistryHead(ctx context.Context, registry store.RegistryID, commit, expectedCommit string) error {
	return s.withTx(ctx, func(tx *Store) error {
		if _, err := tx.q.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`, "agent-marketplace:registry-head:"+registry.String()); err != nil {
			return err
		}
		var current string
		err := tx.q.QueryRowContext(ctx, `SELECT revision_commit FROM registry_heads WHERE registry_id=$1 FOR UPDATE`, registry).Scan(&current)
		if errors.Is(err, sql.ErrNoRows) {
			if expectedCommit != "" {
				return fmt.Errorf("%w: registry %q head changed", store.ErrConflict, registry)
			}
			_, err = tx.q.ExecContext(ctx, `INSERT INTO registry_heads (registry_id, revision_commit) VALUES ($1, $2)`, registry, commit)
			if err != nil {
				return err
			}
			return tx.advanceCatalogVersion(ctx)
		}
		if err != nil {
			return err
		}
		if current == commit {
			return nil
		}
		if current != expectedCommit {
			return fmt.Errorf("%w: registry %q head changed from %q to %q", store.ErrConflict, registry, expectedCommit, current)
		}
		result, err := tx.q.ExecContext(ctx, `UPDATE registry_heads SET revision_commit=$1 WHERE registry_id=$2 AND revision_commit=$3`, commit, registry, expectedCommit)
		if err != nil {
			return err
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if updated != 1 {
			return fmt.Errorf("%w: registry %q head changed", store.ErrConflict, registry)
		}
		return tx.advanceCatalogVersion(ctx)
	})
}

func (s *Store) LatestRegistryOccurrence(ctx context.Context, registry store.RegistryID) (store.RegistryOccurrence, error) {
	row := s.q.QueryRowContext(ctx,
		`SELECT so.registry_id, so.ref, so.roots, so.revision_commit, so.manifest_digest, so.acquisition,
		        so.mirrored_artifact_digest, so.source_origin, so.occurred_at
		 FROM effective_registry_heads sh JOIN all_registry_occurrences so
		   ON so.registry_id=sh.registry_id AND so.occurrence_key=sh.occurrence_key
		 WHERE sh.registry_id=$1`, registry)
	return s.scanRegistryOccurrence(row)
}

// -- Collection revisions --

func (s *Store) GetCollectionRevision(ctx context.Context, id string) (store.CollectionRevision, error) {
	row := s.q.QueryRowContext(ctx,
		`SELECT id, registry_id, manifest_digest, members, includes, created_at
		 FROM collection_revisions WHERE id=$1`,
		id,
	)
	return s.scanCollectionRevision(row)
}

func (s *Store) scanCollectionRevision(sc sourceScanner) (store.CollectionRevision, error) {
	var cr store.CollectionRevision
	var membersJSON string
	var includesJSON string
	var created int64
	err := sc.Scan(&cr.ID, &cr.RegistryID, &cr.ManifestDigest, &membersJSON, &includesJSON, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return store.CollectionRevision{}, store.ErrNotFound
	}
	if err != nil {
		return store.CollectionRevision{}, err
	}
	cr.CreatedAt = microToTime(created)
	if err := unmarshalJSON([]byte(membersJSON), &cr.Members); err != nil {
		return store.CollectionRevision{}, fmt.Errorf("unmarshal members: %w", err)
	}
	if err := unmarshalJSON([]byte(includesJSON), &cr.Includes); err != nil {
		return store.CollectionRevision{}, fmt.Errorf("unmarshal includes: %w", err)
	}
	return cr, nil
}

func (s *Store) PutCollectionRevision(ctx context.Context, cr store.CollectionRevision) error {
	return s.withTx(ctx, func(tx *Store) error { return tx.putCollectionRevision(ctx, cr) })
}

func (s *Store) putCollectionRevision(ctx context.Context, cr store.CollectionRevision) error {
	if cr.CreatedAt.IsZero() {
		cr.CreatedAt = time.Now().UTC()
	}
	membersJSON, err := marshalJSON(cr.Members)
	if err != nil {
		return err
	}
	includesJSON, err := marshalJSON(cr.Includes)
	if err != nil {
		return err
	}
	result, err := s.q.ExecContext(ctx,
		`INSERT INTO collection_revisions
		 (id, registry_id, manifest_digest, members, includes, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (id) DO NOTHING`,
		cr.ID, cr.RegistryID, cr.ManifestDigest, membersJSON, includesJSON, timeToMicro(cr.CreatedAt),
	)
	if err != nil {
		return err
	}
	existing, err := s.GetCollectionRevision(ctx, cr.ID)
	if err != nil {
		return err
	}
	if !cr.Equal(existing) {
		legacy := existing
		legacy.Includes = cr.Includes
		if len(existing.Includes) == 0 && len(cr.Includes) > 0 && cr.Equal(legacy) {
			update, err := s.q.ExecContext(ctx, `UPDATE collection_revisions SET includes=$1 WHERE id=$2 AND includes='[]'`, includesJSON, cr.ID)
			if err != nil {
				return err
			}
			updated, err := update.RowsAffected()
			if err != nil {
				return err
			}
			if updated == 1 {
				if err := s.advanceCatalogVersion(ctx); err != nil {
					return err
				}
				existing, err = s.GetCollectionRevision(ctx, cr.ID)
				if err != nil {
					return err
				}
			}
		}
		if !cr.Equal(existing) {
			return fmt.Errorf("%w: collection revision %q already has different immutable metadata", store.ErrConflict, cr.ID)
		}
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if inserted == 0 {
		return nil
	}
	return s.advanceCatalogVersion(ctx)
}

func (s *Store) ListCollectionRevisions(ctx context.Context, registry store.RegistryID) ([]store.CollectionRevision, error) {
	rows, err := s.q.QueryContext(ctx,
		`SELECT id, registry_id, manifest_digest, members, includes, created_at
		 FROM collection_revisions WHERE registry_id=$1 ORDER BY created_at DESC`,
		registry,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []store.CollectionRevision
	for rows.Next() {
		cr, err := s.scanCollectionRevision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, cr)
	}
	return out, rows.Err()
}

// -- Revocations --

func (s *Store) GetRevocation(ctx context.Context, kind store.RevocationKind, targetID string) (store.Revocation, error) {
	row := s.q.QueryRowContext(ctx,
		`SELECT kind, target_id, reason, applied_at FROM revocations WHERE kind=$1 AND target_id=$2`,
		kind, targetID,
	)
	return s.scanRevocation(row)
}

func (s *Store) scanRevocation(sc sourceScanner) (store.Revocation, error) {
	var r store.Revocation
	var applied int64
	err := sc.Scan(&r.Kind, &r.TargetID, &r.Reason, &applied)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Revocation{}, store.ErrNotFound
	}
	if err != nil {
		return store.Revocation{}, err
	}
	r.AppliedAt = microToTime(applied)
	return r, nil
}

func (s *Store) PutRevocation(ctx context.Context, r store.Revocation) error {
	return s.withTx(ctx, func(tx *Store) error { return tx.putRevocation(ctx, r) })
}

func (s *Store) putRevocation(ctx context.Context, r store.Revocation) error {
	if r.AppliedAt.IsZero() {
		r.AppliedAt = time.Now().UTC()
	}
	_, err := s.q.ExecContext(ctx,
		`INSERT INTO revocations (kind, target_id, reason, applied_at)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (kind, target_id) DO UPDATE SET
		   reason = EXCLUDED.reason,
		   applied_at = EXCLUDED.applied_at`,
		r.Kind, r.TargetID, r.Reason, timeToMicro(r.AppliedAt),
	)
	if err != nil {
		return err
	}
	return s.advanceCatalogVersion(ctx)
}

func (s *Store) DeleteRevocation(ctx context.Context, kind store.RevocationKind, targetID string) error {
	return s.withTx(ctx, func(tx *Store) error {
		result, err := tx.q.ExecContext(ctx, `DELETE FROM revocations WHERE kind=$1 AND target_id=$2`, kind, targetID)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil || changed == 0 {
			return err
		}
		return tx.advanceCatalogVersion(ctx)
	})
}

func (s *Store) ListRevocations(ctx context.Context, kind store.RevocationKind) ([]store.Revocation, error) {
	rows, err := s.q.QueryContext(ctx,
		`SELECT kind, target_id, reason, applied_at FROM revocations WHERE kind=$1 ORDER BY applied_at DESC`,
		kind,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []store.Revocation
	for rows.Next() {
		r, err := s.scanRevocation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// -- Jobs --

func (s *Store) GetJob(ctx context.Context, id store.JobID) (store.Job, error) {
	return s.getJob(ctx, id)
}

func (s *Store) getJob(ctx context.Context, id store.JobID) (store.Job, error) {
	row := s.q.QueryRowContext(ctx,
		`SELECT id, kind, payload, status, lease_until, attempt, max_attempts, backoff,
		        idempotency_key, worker_id, created_at, updated_at
		 FROM jobs WHERE id=$1`,
		id,
	)
	var j store.Job
	var lease sql.NullInt64
	var idempotencyKey, workerID sql.NullString
	var created, updated int64

	err := row.Scan(
		&j.ID, &j.Kind, &j.Payload, &j.Status, &lease, &j.Attempt, &j.MaxAttempts, &j.Backoff,
		&idempotencyKey, &workerID, &created, &updated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Job{}, store.ErrNotFound
	}
	if err != nil {
		return store.Job{}, err
	}
	if lease.Valid {
		j.LeaseUntil = microToTime(lease.Int64)
	}
	j.IdempotencyKey = idempotencyKey.String
	j.WorkerID = workerID.String
	j.CreatedAt = microToTime(created)
	j.UpdatedAt = microToTime(updated)
	j.Payload = append([]byte(nil), j.Payload...)
	return j, nil
}

func (s *Store) EnqueueJob(ctx context.Context, job store.Job) error {
	return s.withTx(ctx, func(tx *Store) error { return tx.enqueueJob(ctx, job) })
}

func (s *Store) enqueueJob(ctx context.Context, job store.Job) error {
	if job.ID == "" {
		job.ID = store.JobID(generateID())
	}
	if job.Status == "" {
		job.Status = store.JobStatusPending
	}
	if job.MaxAttempts <= 0 {
		job.MaxAttempts = 1
	}
	now := time.Now().UTC()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	if job.UpdatedAt.IsZero() {
		job.UpdatedAt = now
	}
	if job.Payload == nil {
		job.Payload = []byte{}
	}

	if job.IdempotencyKey != "" {
		var existing string
		err := s.q.QueryRowContext(ctx,
			"SELECT id FROM jobs WHERE kind=$1 AND idempotency_key=$2",
			job.Kind, job.IdempotencyKey,
		).Scan(&existing)
		if err == nil {
			return fmt.Errorf("%w: idempotency key %q already used by job %s", store.ErrConflict, job.IdempotencyKey, existing)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}

	_, err := s.q.ExecContext(ctx,
		`INSERT INTO jobs (id, kind, payload, status, lease_until, attempt, max_attempts,
		  backoff, idempotency_key, worker_id, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		job.ID, job.Kind, job.Payload, job.Status, timeToMicro(job.LeaseUntil), job.Attempt,
		job.MaxAttempts, int64(job.Backoff), job.IdempotencyKey, job.WorkerID,
		timeToMicro(job.CreatedAt), timeToMicro(job.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert job: %w", err)
	}
	return nil
}

func (s *Store) LeaseJob(ctx context.Context, kind, workerID string, leaseDuration time.Duration) (store.Job, error) {
	var leased store.Job
	err := s.withTx(ctx, func(tx *Store) error {
		var err error
		leased, err = tx.leaseJob(ctx, kind, workerID, leaseDuration)
		return err
	})
	return leased, err
}

func (s *Store) leaseJob(ctx context.Context, kind, workerID string, leaseDuration time.Duration) (store.Job, error) {
	const maxAttempts = 100
	for i := 0; i < maxAttempts; i++ {
		nowMicro := timeToMicro(time.Now().UTC())
		var id store.JobID
		row := s.q.QueryRowContext(ctx,
			`SELECT id FROM jobs
			 WHERE kind=$1
			   AND attempt < max_attempts
			   AND (status=$2 OR status=$3 OR (status=$4 AND lease_until <= $5))
			   AND (lease_until IS NULL OR lease_until <= $6)
			 ORDER BY created_at ASC, id ASC
			 LIMIT 1`,
			kind, store.JobStatusPending, store.JobStatusFailed, store.JobStatusLeased, nowMicro, nowMicro,
		)
		err := row.Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return store.Job{}, store.ErrNotFound
		}
		if err != nil {
			return store.Job{}, err
		}
		leased, err := s.leaseJobByID(ctx, id, workerID, leaseDuration)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		return leased, err
	}
	return store.Job{}, store.ErrNotFound
}

func (s *Store) LeaseJobByID(ctx context.Context, id store.JobID, workerID string, leaseDuration time.Duration) (store.Job, error) {
	var leased store.Job
	err := s.withTx(ctx, func(tx *Store) error {
		var err error
		leased, err = tx.leaseJobByID(ctx, id, workerID, leaseDuration)
		return err
	})
	return leased, err
}

func (s *Store) leaseJobByID(ctx context.Context, id store.JobID, workerID string, leaseDuration time.Duration) (store.Job, error) {
	now := time.Now().UTC()
	nowMicro := timeToMicro(now)
	res, err := s.q.ExecContext(ctx,
		`UPDATE jobs
		 SET status=$1, worker_id=$2, lease_until=$3, attempt=attempt+1, updated_at=$4
		 WHERE id=$5 AND attempt < max_attempts
		   AND (status=$6 OR status=$7 OR (status=$8 AND lease_until <= $9))
		   AND (lease_until IS NULL OR lease_until <= $10)`,
		store.JobStatusLeased, workerID, timeToMicro(now.Add(leaseDuration)), nowMicro,
		id, store.JobStatusPending, store.JobStatusFailed, store.JobStatusLeased, nowMicro, nowMicro,
	)
	if err != nil {
		return store.Job{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return store.Job{}, err
	}
	if n == 0 {
		return store.Job{}, store.ErrNotFound
	}
	return s.getJob(ctx, id)
}

func (s *Store) ExtendLease(ctx context.Context, id store.JobID, workerID string, d time.Duration) error {
	return s.withTx(ctx, func(tx *Store) error { return tx.extendLease(ctx, id, workerID, d) })
}

func (s *Store) extendLease(ctx context.Context, id store.JobID, workerID string, d time.Duration) error {
	now := time.Now().UTC()
	nowMicro := timeToMicro(now)
	newLease := timeToMicro(now.Add(d))
	updated := timeToMicro(now)

	res, err := s.q.ExecContext(ctx,
		`UPDATE jobs
		 SET lease_until=$1, updated_at=$2
		 WHERE id=$3 AND worker_id=$4 AND status=$5 AND lease_until > $6`,
		newLease, updated, id, workerID, store.JobStatusLeased, nowMicro,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return s.leaseError(ctx, id, workerID, store.ErrLeaseExpired)
	}
	return nil
}

func (s *Store) ReleaseJob(ctx context.Context, id store.JobID, workerID string) error {
	return s.withTx(ctx, func(tx *Store) error { return tx.releaseJob(ctx, id, workerID) })
}

func (s *Store) releaseJob(ctx context.Context, id store.JobID, workerID string) error {
	now := time.Now().UTC()
	nowMicro := timeToMicro(now)
	updated := nowMicro

	res, err := s.q.ExecContext(ctx,
		`UPDATE jobs
		 SET status=$1, worker_id=NULL, lease_until=NULL,
		     attempt=CASE WHEN attempt > 0 THEN attempt-1 ELSE 0 END,
		     updated_at=$2
		 WHERE id=$3 AND worker_id=$4 AND status=$5 AND lease_until > $6`,
		store.JobStatusPending, updated, id, workerID, store.JobStatusLeased, nowMicro,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return s.leaseError(ctx, id, workerID, store.ErrLeaseExpired)
	}
	return nil
}

func (s *Store) CompleteJob(ctx context.Context, id store.JobID, workerID string) error {
	return s.withTx(ctx, func(tx *Store) error { return tx.completeJob(ctx, id, workerID) })
}

func (s *Store) completeJob(ctx context.Context, id store.JobID, workerID string) error {
	now := time.Now().UTC()
	res, err := s.q.ExecContext(ctx,
		`UPDATE jobs
		 SET status=$1, worker_id=NULL, lease_until=NULL, updated_at=$2
		 WHERE id=$3 AND worker_id=$4 AND status=$5 AND lease_until > $6`,
		store.JobStatusCompleted, timeToMicro(now), id, workerID, store.JobStatusLeased, timeToMicro(now),
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return s.leaseError(ctx, id, workerID, store.ErrLeaseExpired)
	}
	return nil
}

func (s *Store) FailJob(ctx context.Context, id store.JobID, workerID string, backoff time.Duration) error {
	return s.withTx(ctx, func(tx *Store) error { return tx.failJob(ctx, id, workerID, backoff) })
}

func (s *Store) failJob(ctx context.Context, id store.JobID, workerID string, backoff time.Duration) error {
	now := time.Now().UTC()
	nowMicro := timeToMicro(now)
	retryAt := nowMicro
	if backoff > 0 {
		retryAt = timeToMicro(now.Add(backoff))
	}

	res, err := s.q.ExecContext(ctx,
		`UPDATE jobs
		 SET status=$1, worker_id=NULL, lease_until=$2, backoff=$3, updated_at=$4
		 WHERE id=$5 AND worker_id=$6 AND status=$7 AND lease_until > $8`,
		store.JobStatusFailed, retryAt, int64(backoff), nowMicro,
		id, workerID, store.JobStatusLeased, nowMicro,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return s.leaseError(ctx, id, workerID, store.ErrLeaseExpired)
	}
	return nil
}

func (s *Store) leaseError(ctx context.Context, id store.JobID, workerID string, sentinel error) error {
	_, err := s.getJob(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return store.ErrNotFound
	}
	if err != nil {
		return err
	}
	return sentinel
}

// -- Integrity --

func (s *Store) CheckIntegrity(ctx context.Context, blobs store.BlobStore) error {
	rows, err := s.q.QueryContext(ctx,
		`SELECT DISTINCT archive_digest AS digest, 0 AS inspection
		 FROM resource_revisions
		 WHERE archive_digest IS NOT NULL AND archive_digest != ''
		UNION
		 SELECT DISTINCT manifest_digest AS digest, 0 AS inspection
		 FROM all_registry_occurrences
		 WHERE manifest_digest IS NOT NULL AND manifest_digest != ''
		UNION
		 SELECT DISTINCT mirrored_artifact_digest AS digest, 0 AS inspection
		 FROM all_registry_occurrences
		 WHERE mirrored_artifact_digest IS NOT NULL AND mirrored_artifact_digest != ''
		UNION
		 SELECT DISTINCT manifest_digest AS digest, 0 AS inspection
		 FROM collection_revisions
		 WHERE manifest_digest IS NOT NULL AND manifest_digest != ''
		UNION
		 SELECT DISTINCT inspection_manifest_digest AS digest, 1 AS inspection
		 FROM resource_assessments
		 WHERE inspection_manifest_digest IS NOT NULL AND inspection_manifest_digest != ''`,
	)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var digest string
		var inspection int
		if err := rows.Scan(&digest, &inspection); err != nil {
			return err
		}
		exists, err := blobs.Exists(ctx, store.ArchiveDigest(digest))
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("%w: %s", store.ErrBlobMissing, digest)
		}
		if inspection == 1 {
			if err := store.CheckInspectionBlobs(ctx, blobs, store.RevisionDigest(digest)); err != nil {
				return err
			}
		}
	}
	return rows.Err()
}
