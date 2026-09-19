package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const ManagedPreviewSchema = `
CREATE TABLE managed_previews (
 marketplace_id TEXT NOT NULL, id TEXT NOT NULL,
 status TEXT NOT NULL CHECK (status IN ('pending','running','succeeded','failed')),
 created_at BIGINT NOT NULL, record TEXT NOT NULL,
 PRIMARY KEY (marketplace_id, id)
);
CREATE INDEX managed_previews_created ON managed_previews(marketplace_id, created_at);
`

func validManagedPreviewStatus(status string) bool {
	switch status {
	case ManagedPreviewPending, ManagedPreviewRunning, ManagedPreviewSucceeded, ManagedPreviewFailed:
		return true
	}
	return false
}

func encodeManagedPreview(p ManagedPreview) (string, error) {
	if !managedToken(p.ID) || !managedToken(string(p.Marketplace)) || !validManagedPreviewStatus(p.Status) || p.BaseRevision < 1 || len(p.Registry) == 0 || len(p.Registry) > 1<<16 || !json.Valid(p.Registry) || p.Actor.Subject == "" || p.Actor.Issuer == "" || p.Terminal() == (p.Result == nil && p.Failure == nil) || (p.Result != nil && p.Failure != nil) {
		return "", ErrManagedPreviewInvalid
	}
	encoded, err := json.Marshal(p)
	if err != nil || len(encoded) > 1<<20 {
		return "", ErrManagedPreviewInvalid
	}
	return string(encoded), nil
}

func decodeManagedPreview(record string, marketplace MarketplaceID, id string) (ManagedPreview, error) {
	var p ManagedPreview
	if len(record) > 1<<20 || json.Unmarshal([]byte(record), &p) != nil || p.Marketplace != marketplace || p.ID != id || !validManagedPreviewStatus(p.Status) {
		return ManagedPreview{}, ErrManagedStateCorrupt
	}
	return p, nil
}

func InsertManagedPreview(ctx context.Context, q ManagedSQLQueries, tx MetadataStore, p ManagedPreview, job Job) (ManagedPreview, error) {
	if job.ID == "" || job.Kind != "managed-preview" || job.IdempotencyKey == "" || p.Terminal() {
		return ManagedPreview{}, ErrManagedPreviewInvalid
	}
	p.JobID, p.Status, p.CreatedAt = job.ID, ManagedPreviewPending, time.Now().UTC()
	record, err := encodeManagedPreview(p)
	if err != nil {
		return ManagedPreview{}, err
	}
	if err := tx.EnqueueJob(ctx, job); err != nil {
		return ManagedPreview{}, fmt.Errorf("persist preview work intent: %w", err)
	}
	if _, err := q.ExecContext(ctx, `INSERT INTO managed_previews (marketplace_id,id,status,created_at,record) VALUES ($1,$2,$3,$4,$5)`, p.Marketplace, p.ID, p.Status, p.CreatedAt.UnixMicro(), record); err != nil {
		return ManagedPreview{}, fmt.Errorf("persist managed preview: %w", err)
	}
	return p, nil
}

func ReadManagedPreview(ctx context.Context, q ManagedSQLQueries, marketplace MarketplaceID, id string) (ManagedPreview, error) {
	if !managedToken(id) || marketplace == "" {
		return ManagedPreview{}, ErrNotFound
	}
	var record string
	err := q.QueryRowContext(ctx, `SELECT record FROM managed_previews WHERE marketplace_id=$1 AND id=$2`, marketplace, id).Scan(&record)
	if errors.Is(err, sql.ErrNoRows) {
		return ManagedPreview{}, ErrNotFound
	}
	if err != nil {
		return ManagedPreview{}, err
	}
	return decodeManagedPreview(record, marketplace, id)
}

func ReplaceManagedPreview(ctx context.Context, q ManagedSQLQueries, p ManagedPreview) error {
	record, err := encodeManagedPreview(p)
	if err != nil {
		return err
	}
	result, err := q.ExecContext(ctx, `UPDATE managed_previews SET status=$1, record=$2 WHERE marketplace_id=$3 AND id=$4 AND status IN ('pending','running')`, p.Status, record, p.Marketplace, p.ID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		if _, err := ReadManagedPreview(ctx, q, p.Marketplace, p.ID); err != nil {
			return err
		}
		return ErrConflict
	}
	return nil
}

func DeleteManagedPreviewsBefore(ctx context.Context, q ManagedSQLQueries, marketplace MarketplaceID, before time.Time) error {
	_, err := q.ExecContext(ctx, `DELETE FROM managed_previews WHERE marketplace_id=$1 AND created_at<$2`, marketplace, before.UnixMicro())
	return err
}
