package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

type ManagedSQLQueries interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func ReadManagedConfiguration(ctx context.Context, q ManagedSQLQueries, id MarketplaceID) (ManagedChange, error) {
	var revision int64
	var record sql.NullString
	err := q.QueryRowContext(ctx, `SELECT h.revision, c.record FROM managed_configuration_heads h
		LEFT JOIN managed_configuration_changes c ON c.marketplace_id=h.marketplace_id AND c.revision=h.revision
		WHERE h.marketplace_id=$1`, id).Scan(&revision, &record)
	if errors.Is(err, sql.ErrNoRows) {
		return ManagedChange{}, ErrNotFound
	}
	if err != nil {
		return ManagedChange{}, fmt.Errorf("read managed configuration: %w", err)
	}
	if !record.Valid {
		return ManagedChange{}, ErrManagedStateCorrupt
	}
	return decodeManagedChange(record.String, id, revision)
}

func ReadManagedConfigurationHistory(ctx context.Context, q ManagedSQLQueries, id MarketplaceID, before int64, limit int) ([]ManagedChange, error) {
	if before < 0 || limit < 1 || limit > 100 {
		return nil, ErrManagedChangeInvalid
	}
	rows, err := q.QueryContext(ctx, `SELECT revision, record FROM managed_configuration_changes
		WHERE marketplace_id=$1 AND ($2=0 OR revision<$2) ORDER BY revision DESC LIMIT $3`, id, before, limit)
	if err != nil {
		return nil, fmt.Errorf("read managed history: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]ManagedChange, 0, limit)
	for rows.Next() {
		var revision int64
		var record string
		if err := rows.Scan(&revision, &record); err != nil {
			return nil, fmt.Errorf("scan managed history: %w", err)
		}
		change, err := decodeManagedChange(record, id, revision)
		if err != nil {
			return nil, err
		}
		out = append(out, change)
	}
	return out, rows.Err()
}

func ReadManagedChange(ctx context.Context, q ManagedSQLQueries, id MarketplaceID, actor ManagedActor, operation, key string) (ManagedChange, error) {
	var revision int64
	var record string
	err := q.QueryRowContext(ctx, `SELECT revision, record FROM managed_configuration_changes WHERE marketplace_id=$1 AND actor_issuer=$2 AND actor_subject=$3 AND operation=$4 AND idempotency_key=$5`, id, actor.Issuer, actor.Subject, operation, key).Scan(&revision, &record)
	if errors.Is(err, sql.ErrNoRows) {
		return ManagedChange{}, ErrNotFound
	}
	if err != nil {
		return ManagedChange{}, fmt.Errorf("read managed idempotency: %w", err)
	}
	return decodeManagedChange(record, id, revision)
}

func decodeManagedChange(record string, id MarketplaceID, revision int64) (ManagedChange, error) {
	var change ManagedChange
	if err := json.Unmarshal([]byte(record), &change); err != nil || change.Marketplace != id || change.Revision != revision || revision < 1 || change.ParentRevision != revision-1 || !json.Valid(change.Snapshot) {
		return ManagedChange{}, ErrManagedStateCorrupt
	}
	return change, nil
}

func CommitManagedConfiguration(ctx context.Context, q ManagedSQLQueries, tx MetadataStore, req ManagedChangeRequest) (ManagedChange, error) {
	change := req.Change
	if err := validateManagedChange(req); err != nil {
		return ManagedChange{}, err
	}
	initialized := false
	if change.ParentRevision == 0 {
		result, err := q.ExecContext(ctx, `INSERT INTO managed_configuration_heads (marketplace_id, revision)
			VALUES ($1, 0) ON CONFLICT (marketplace_id) DO NOTHING`, change.Marketplace)
		if err != nil {
			return ManagedChange{}, fmt.Errorf("initialize managed authority: %w", err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return ManagedChange{}, err
		}
		initialized = n == 1
	}
	result, err := q.ExecContext(ctx, `UPDATE managed_configuration_heads SET revision=revision WHERE marketplace_id=$1`, change.Marketplace)
	if err != nil {
		return ManagedChange{}, fmt.Errorf("lock managed authority: %w", err)
	}
	if n, err := result.RowsAffected(); err != nil {
		return ManagedChange{}, err
	} else if n != 1 {
		return ManagedChange{}, ErrManagedRevisionConflict
	}
	prior, err := ReadManagedChange(ctx, q, change.Marketplace, change.Actor, change.Operation, change.IdempotencyKey)
	if err == nil {
		if prior.RequestDigest != change.RequestDigest {
			return ManagedChange{}, ErrManagedIdempotencyConflict
		}
		return prior, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return ManagedChange{}, err
	}
	var revision int64
	if err := q.QueryRowContext(ctx, `SELECT revision FROM managed_configuration_heads WHERE marketplace_id=$1`, change.Marketplace).Scan(&revision); err != nil {
		return ManagedChange{}, fmt.Errorf("read managed revision: %w", err)
	}
	if revision == 0 && !initialized {
		return ManagedChange{}, ErrManagedStateCorrupt
	}
	if revision != change.ParentRevision {
		return ManagedChange{}, ErrManagedRevisionConflict
	}
	var previous *ManagedChange
	if revision > 0 {
		current, err := ReadManagedConfiguration(ctx, q, change.Marketplace)
		if err != nil {
			return ManagedChange{}, err
		}
		previous = &current
	}
	if err := ProjectManagedRegistries(ctx, q, change, previous); err != nil {
		return ManagedChange{}, err
	}
	change.Revision = revision + 1
	change.CreatedAt = time.Now().UTC()
	change.JobIDs = make([]JobID, 0, len(req.Jobs))
	for _, job := range req.Jobs {
		if err := tx.EnqueueJob(ctx, job); err != nil {
			return ManagedChange{}, fmt.Errorf("persist managed work intent: %w", err)
		}
		change.JobIDs = append(change.JobIDs, job.ID)
	}
	encoded, err := json.Marshal(change)
	if err != nil {
		return ManagedChange{}, fmt.Errorf("encode managed change: %w", err)
	}
	if _, err := q.ExecContext(ctx, `INSERT INTO managed_configuration_changes
		(marketplace_id, revision, parent_revision, actor_issuer, actor_subject, operation, idempotency_key, request_digest, record)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, change.Marketplace, change.Revision, change.ParentRevision,
		change.Actor.Issuer, change.Actor.Subject, change.Operation, change.IdempotencyKey, change.RequestDigest, string(encoded)); err != nil {
		return ManagedChange{}, fmt.Errorf("persist managed audit: %w", err)
	}
	if _, err := q.ExecContext(ctx, `UPDATE managed_configuration_heads SET revision=$1 WHERE marketplace_id=$2`, change.Revision, change.Marketplace); err != nil {
		return ManagedChange{}, fmt.Errorf("advance managed revision: %w", err)
	}
	if _, err := q.ExecContext(ctx, `UPDATE catalog_versions SET version=version+1 WHERE singleton=1`); err != nil {
		return ManagedChange{}, fmt.Errorf("advance managed catalog version: %w", err)
	}
	return decodeManagedChange(string(encoded), change.Marketplace, change.Revision)
}

func validateManagedChange(req ManagedChangeRequest) error {
	c := req.Change
	if c.ParentRevision < 0 || c.ParentRevision == math.MaxInt64 || len(c.Snapshot) > 1<<20 || !json.Valid(c.Snapshot) || len(req.Jobs) > 100 {
		return ErrManagedChangeInvalid
	}
	for _, field := range []string{string(c.Marketplace), c.Actor.Issuer, c.Actor.Subject, c.Operation, c.Reason, c.RequestID, c.IdempotencyKey, c.RequestDigest} {
		if strings.TrimSpace(field) == "" || len(field) > 1024 || strings.ContainsAny(field, "\x00\r\n") {
			return ErrManagedChangeInvalid
		}
	}
	seen := make(map[JobID]bool, len(req.Jobs))
	for _, job := range req.Jobs {
		if job.ID == "" || job.Kind == "" || job.IdempotencyKey == "" || seen[job.ID] {
			return ErrManagedChangeInvalid
		}
		seen[job.ID] = true
	}
	return nil
}
