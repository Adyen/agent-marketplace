package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// -- Source occurrences --

func (s *Store) GetSourceOccurrence(ctx context.Context, source store.SourceID, commit string) (store.SourceOccurrence, error) {
	row := s.q.QueryRowContext(ctx,
		`SELECT source_id, ref, revision_commit, manifest_digest, acquisition,
		        mirrored_artifact_digest, occurred_at
		 FROM source_occurrences WHERE source_id=$1 AND revision_commit=$2`,
		source, commit,
	)
	return s.scanSourceOccurrence(row)
}

func (s *Store) scanSourceOccurrence(sc sourceScanner) (store.SourceOccurrence, error) {
	var occ store.SourceOccurrence
	var mirrored sql.NullString
	var occurred int64
	err := sc.Scan(
		&occ.SourceID, &occ.Ref, &occ.Commit, &occ.SourceRevisionManifestDigest,
		&occ.Acquisition, &mirrored, &occurred,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return store.SourceOccurrence{}, store.ErrNotFound
	}
	if err != nil {
		return store.SourceOccurrence{}, err
	}
	occ.MirroredArtifactDigest = store.ArchiveDigest(mirrored.String)
	occ.OccurredAt = microToTime(occurred)
	return occ, nil
}

func (s *Store) PutSourceOccurrence(ctx context.Context, occ store.SourceOccurrence) error {
	if err := occ.Acquisition.Validate(); err != nil {
		return err
	}
	if occ.OccurredAt.IsZero() {
		occ.OccurredAt = time.Now().UTC()
	}
	mirrored := string(occ.MirroredArtifactDigest)
	_, err := s.q.ExecContext(ctx,
		`INSERT INTO source_occurrences
		 (source_id, ref, revision_commit, manifest_digest, acquisition, mirrored_artifact_digest, occurred_at)
		 VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7)
		 ON CONFLICT (source_id, revision_commit) DO UPDATE SET
		   ref = EXCLUDED.ref,
		   manifest_digest = EXCLUDED.manifest_digest,
		   acquisition = EXCLUDED.acquisition,
		   mirrored_artifact_digest = EXCLUDED.mirrored_artifact_digest,
		   occurred_at = EXCLUDED.occurred_at`,
		occ.SourceID, occ.Ref, occ.Commit, occ.SourceRevisionManifestDigest,
		occ.Acquisition, mirrored, timeToMicro(occ.OccurredAt),
	)
	return err
}

func (s *Store) LatestSourceOccurrence(ctx context.Context, source store.SourceID) (store.SourceOccurrence, error) {
	row := s.q.QueryRowContext(ctx,
		`SELECT source_id, ref, revision_commit, manifest_digest, acquisition,
		        mirrored_artifact_digest, occurred_at
		 FROM source_occurrences WHERE source_id=$1 ORDER BY occurred_at DESC, revision_commit DESC LIMIT 1`,
		source,
	)
	return s.scanSourceOccurrence(row)
}

// -- Collection revisions --

func (s *Store) GetCollectionRevision(ctx context.Context, id string) (store.CollectionRevision, error) {
	row := s.q.QueryRowContext(ctx,
		`SELECT id, source_id, manifest_digest, members, created_at
		 FROM collection_revisions WHERE id=$1`,
		id,
	)
	return s.scanCollectionRevision(row)
}

func (s *Store) scanCollectionRevision(sc sourceScanner) (store.CollectionRevision, error) {
	var cr store.CollectionRevision
	var membersJSON string
	var created int64
	err := sc.Scan(&cr.ID, &cr.SourceID, &cr.ManifestDigest, &membersJSON, &created)
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
	return cr, nil
}

func (s *Store) PutCollectionRevision(ctx context.Context, cr store.CollectionRevision) error {
	if cr.CreatedAt.IsZero() {
		cr.CreatedAt = time.Now().UTC()
	}
	membersJSON, err := marshalJSON(cr.Members)
	if err != nil {
		return err
	}
	_, err = s.q.ExecContext(ctx,
		`INSERT INTO collection_revisions
		 (id, source_id, manifest_digest, members, created_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (id) DO UPDATE SET
		   source_id = EXCLUDED.source_id,
		   manifest_digest = EXCLUDED.manifest_digest,
		   members = EXCLUDED.members,
		   created_at = EXCLUDED.created_at`,
		cr.ID, cr.SourceID, cr.ManifestDigest, membersJSON, timeToMicro(cr.CreatedAt),
	)
	return err
}

func (s *Store) ListCollectionRevisions(ctx context.Context, source store.SourceID) ([]store.CollectionRevision, error) {
	rows, err := s.q.QueryContext(ctx,
		`SELECT id, source_id, manifest_digest, members, created_at
		 FROM collection_revisions WHERE source_id=$1 ORDER BY created_at DESC`,
		source,
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
	return err
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
	now := time.Now().UTC()
	nowMicro := timeToMicro(now)
	leaseUntil := now.Add(leaseDuration)
	leaseMicro := timeToMicro(leaseUntil)
	updatedMicro := timeToMicro(now)

	const maxAttempts = 100
	for i := 0; i < maxAttempts; i++ {
		var id string
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

		res, err := s.q.ExecContext(ctx,
			`UPDATE jobs
			 SET status=$1, worker_id=$2, lease_until=$3, attempt=attempt+1, updated_at=$4
			 WHERE id=$5 AND kind=$6 AND attempt < max_attempts
			   AND (status=$7 OR status=$8 OR (status=$9 AND lease_until <= $10))
			   AND (lease_until IS NULL OR lease_until <= $11)`,
			store.JobStatusLeased, workerID, leaseMicro, updatedMicro,
			id, kind, store.JobStatusPending, store.JobStatusFailed, store.JobStatusLeased, nowMicro, nowMicro,
		)
		if err != nil {
			return store.Job{}, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return store.Job{}, err
		}
		if n == 0 {
			continue
		}
		return s.getJob(ctx, store.JobID(id))
	}
	return store.Job{}, store.ErrNotFound
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

// -- Readiness --

func (s *Store) ReadyCheck(ctx context.Context, blobs store.BlobStore) error {
	rows, err := s.q.QueryContext(ctx,
		`SELECT DISTINCT archive_digest AS digest
		 FROM resource_revisions
		 WHERE archive_digest IS NOT NULL AND archive_digest != ''
		UNION
		 SELECT DISTINCT manifest_digest AS digest
		 FROM source_occurrences
		 WHERE manifest_digest IS NOT NULL AND manifest_digest != ''
		UNION
		 SELECT DISTINCT mirrored_artifact_digest AS digest
		 FROM source_occurrences
		 WHERE mirrored_artifact_digest IS NOT NULL AND mirrored_artifact_digest != ''`,
	)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var digest string
		if err := rows.Scan(&digest); err != nil {
			return err
		}
		exists, err := blobs.Exists(ctx, store.ArchiveDigest(digest))
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("%w: %s", store.ErrBlobMissing, digest)
		}
	}
	return rows.Err()
}
