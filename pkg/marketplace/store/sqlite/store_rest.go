package sqlite

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
		 FROM source_occurrences WHERE source_id=? AND revision_commit=?`,
		source, commit,
	)
	return s.scanSourceOccurrenceRow(row)
}

func (s *Store) scanSourceOccurrence(rows *sql.Rows) (store.SourceOccurrence, error) {
	var occ store.SourceOccurrence
	var mirrored sql.NullString
	var occurred int64
	err := rows.Scan(
		&occ.SourceID, &occ.Ref, &occ.Commit, &occ.SourceRevisionManifestDigest,
		&occ.Acquisition, &mirrored, &occurred,
	)
	if err != nil {
		return store.SourceOccurrence{}, err
	}
	occ.MirroredArtifactDigest = store.ArchiveDigest(mirrored.String)
	occ.OccurredAt = microToTime(occurred)
	return occ, nil
}

func (s *Store) scanSourceOccurrenceRow(row *sql.Row) (store.SourceOccurrence, error) {
	var occ store.SourceOccurrence
	var mirrored sql.NullString
	var occurred int64
	err := row.Scan(
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
	err := execWithBusyRetry(ctx, s.q,
		`INSERT OR IGNORE INTO source_occurrences
		 (source_id, ref, revision_commit, manifest_digest, acquisition, mirrored_artifact_digest, occurred_at)
		 VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), ?)`,
		occ.SourceID, occ.Ref, occ.Commit, occ.SourceRevisionManifestDigest,
		occ.Acquisition, mirrored, timeToMicro(occ.OccurredAt),
	)
	if err != nil {
		return err
	}

	existing, err := s.GetSourceOccurrence(ctx, occ.SourceID, occ.Commit)
	if err != nil {
		return err
	}
	if !occ.Equal(existing) {
		return fmt.Errorf("%w: source occurrence %q already has different immutable fields", store.ErrConflict, occ.Commit)
	}
	return nil
}

func (s *Store) LatestSourceOccurrence(ctx context.Context, source store.SourceID) (store.SourceOccurrence, error) {
	row := s.q.QueryRowContext(ctx,
		`SELECT source_id, ref, revision_commit, manifest_digest, acquisition,
		        mirrored_artifact_digest, occurred_at
		 FROM source_occurrences WHERE source_id=? ORDER BY occurred_at DESC, revision_commit DESC LIMIT 1`,
		source,
	)
	return s.scanSourceOccurrenceRow(row)
}

// -- Collection revisions --

func (s *Store) GetCollectionRevision(ctx context.Context, id string) (store.CollectionRevision, error) {
	row := s.q.QueryRowContext(ctx,
		`SELECT id, source_id, manifest_digest, members, created_at
		 FROM collection_revisions WHERE id=?`,
		id,
	)
	return s.scanCollectionRevision(row)
}

func (s *Store) scanCollectionRevision(row *sql.Row) (store.CollectionRevision, error) {
	var cr store.CollectionRevision
	var membersJSON string
	var created int64
	err := row.Scan(&cr.ID, &cr.SourceID, &cr.ManifestDigest, &membersJSON, &created)
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
		`INSERT OR IGNORE INTO collection_revisions
		 (id, source_id, manifest_digest, members, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		cr.ID, cr.SourceID, cr.ManifestDigest, membersJSON, timeToMicro(cr.CreatedAt),
	)
	if err != nil {
		return err
	}
	existing, err := s.GetCollectionRevision(ctx, cr.ID)
	if err != nil {
		return err
	}
	if !cr.Equal(existing) {
		return fmt.Errorf("%w: collection revision %q already has different immutable metadata", store.ErrConflict, cr.ID)
	}
	return nil
}

func (s *Store) ListCollectionRevisions(ctx context.Context, source store.SourceID) ([]store.CollectionRevision, error) {
	rows, err := s.q.QueryContext(ctx,
		`SELECT id, source_id, manifest_digest, members, created_at
		 FROM collection_revisions WHERE source_id=? ORDER BY created_at DESC`,
		source,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []store.CollectionRevision
	for rows.Next() {
		cr, err := s.scanCollectionRevisionFromRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, cr)
	}
	return out, rows.Err()
}

func (s *Store) scanCollectionRevisionFromRows(rows *sql.Rows) (store.CollectionRevision, error) {
	var cr store.CollectionRevision
	var membersJSON string
	var created int64
	err := rows.Scan(&cr.ID, &cr.SourceID, &cr.ManifestDigest, &membersJSON, &created)
	if err != nil {
		return store.CollectionRevision{}, err
	}
	cr.CreatedAt = microToTime(created)
	if err := unmarshalJSON([]byte(membersJSON), &cr.Members); err != nil {
		return store.CollectionRevision{}, fmt.Errorf("unmarshal members: %w", err)
	}
	return cr, nil
}

// -- Revocations --

func (s *Store) GetRevocation(ctx context.Context, kind store.RevocationKind, targetID string) (store.Revocation, error) {
	row := s.q.QueryRowContext(ctx,
		`SELECT kind, target_id, reason, applied_at FROM revocations WHERE kind=? AND target_id=?`,
		kind, targetID,
	)
	return s.scanRevocation(row)
}

func (s *Store) scanRevocation(row *sql.Row) (store.Revocation, error) {
	var r store.Revocation
	var applied int64
	err := row.Scan(&r.Kind, &r.TargetID, &r.Reason, &applied)
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
		`INSERT OR REPLACE INTO revocations (kind, target_id, reason, applied_at)
		 VALUES (?, ?, ?, ?)`,
		r.Kind, r.TargetID, r.Reason, timeToMicro(r.AppliedAt),
	)
	return err
}

func (s *Store) DeleteRevocation(ctx context.Context, kind store.RevocationKind, targetID string) error {
	_, err := s.q.ExecContext(ctx, `DELETE FROM revocations WHERE kind=? AND target_id=?`, kind, targetID)
	return err
}

func (s *Store) ListRevocations(ctx context.Context, kind store.RevocationKind) ([]store.Revocation, error) {
	rows, err := s.q.QueryContext(ctx,
		`SELECT kind, target_id, reason, applied_at FROM revocations WHERE kind=? ORDER BY applied_at DESC`,
		kind,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []store.Revocation
	for rows.Next() {
		r, err := s.scanRevocationFromRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) scanRevocationFromRows(rows *sql.Rows) (store.Revocation, error) {
	var r store.Revocation
	var applied int64
	err := rows.Scan(&r.Kind, &r.TargetID, &r.Reason, &applied)
	if err != nil {
		return store.Revocation{}, err
	}
	r.AppliedAt = microToTime(applied)
	return r, nil
}

// -- Jobs --

func (s *Store) GetJob(ctx context.Context, id store.JobID) (store.Job, error) {
	return s.getJob(ctx, id)
}

func (s *Store) getJob(ctx context.Context, id store.JobID) (store.Job, error) {
	row := s.q.QueryRowContext(ctx,
		`SELECT id, kind, payload, status, lease_until, attempt, max_attempts, backoff,
		        idempotency_key, worker_id, created_at, updated_at
		 FROM jobs WHERE id=?`,
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

	if job.IdempotencyKey != "" {
		var existing string
		err := s.q.QueryRowContext(ctx,
			"SELECT id FROM jobs WHERE kind=? AND idempotency_key=?",
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
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
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
			 WHERE kind=?
			   AND attempt < max_attempts
			   AND (status=? OR status=? OR (status=? AND lease_until <= ?))
			   AND (lease_until IS NULL OR lease_until <= ?)
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
			 SET status=?, worker_id=?, lease_until=?, attempt=attempt+1, updated_at=?
			 WHERE id=? AND kind=? AND attempt < max_attempts
			   AND (status=? OR status=? OR (status=? AND lease_until <= ?))
			   AND (lease_until IS NULL OR lease_until <= ?)`,
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
		 SET lease_until=?, updated_at=?
		 WHERE id=? AND worker_id=? AND status=? AND lease_until > ?`,
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
		 SET status=?, worker_id=NULL, lease_until=NULL,
		     attempt=CASE WHEN attempt > 0 THEN attempt-1 ELSE 0 END,
		     updated_at=?
		 WHERE id=? AND worker_id=? AND status=? AND lease_until > ?`,
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
		 SET status=?, worker_id=NULL, lease_until=NULL, updated_at=?
		 WHERE id=? AND worker_id=? AND status=? AND lease_until > ?`,
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
		 SET status=?, worker_id=NULL, lease_until=?, backoff=?, updated_at=?
		 WHERE id=? AND worker_id=? AND status=? AND lease_until > ?`,
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
		 WHERE mirrored_artifact_digest IS NOT NULL AND mirrored_artifact_digest != ''
		UNION
		 SELECT DISTINCT manifest_digest AS digest
		 FROM collection_revisions
		 WHERE manifest_digest IS NOT NULL AND manifest_digest != ''`,
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
