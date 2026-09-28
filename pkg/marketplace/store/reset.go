package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

type RegistryCatalogResetter interface {
	ResetRegistryCatalog(context.Context, RegistryID, bool) (map[string]int64, error)
}

func ResetRegistryCatalogSQL(ctx context.Context, q ManagedSQLQueries, id RegistryID, dryRun bool) (map[string]int64, error) {
	if strings.TrimSpace(string(id)) == "" {
		return nil, fmt.Errorf("reset registry: %w", ErrNotFound)
	}
	var exists string
	if err := q.QueryRowContext(ctx, `SELECT id FROM registries WHERE id=$1`, id).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if !dryRun {
		var active int64
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE status=$1 AND lease_until>$2`, JobStatusLeased, time.Now().UTC().UnixMicro()).Scan(&active); err != nil {
			return nil, err
		}
		if active > 0 {
			return nil, fmt.Errorf("%w: %d live job leases; stop all API/workers and wait for leases to expire before resetting", ErrConflict, active)
		}
	}
	tables := []string{
		"managed_registry_refresh_status",
		"blob_resource_owners", "blob_registry_owners", "blob_owner_sources",
		"current_registry_publications", "publication_resources", "registry_publications",
		"registry_heads", "resource_occurrences", "registry_occurrences",
		"current_resource_assessments", "resource_assessments", "resource_revisions", "collection_revisions",
	}
	counts := make(map[string]int64, len(tables))
	var total int64
	for _, table := range tables {
		var count int64
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE registry_id=$1`, id).Scan(&count); err != nil {
			return nil, fmt.Errorf("count %s for reset: %w", table, err)
		}
		counts[table] = count
		total += count
	}
	slog.Info("registry catalog reset planned", "registry", id, "rows", total, "dry_run", dryRun)
	if dryRun {
		return counts, nil
	}
	for _, table := range tables {
		if counts[table] == 0 {
			continue
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM `+table+` WHERE registry_id=$1`, id); err != nil {
			return nil, fmt.Errorf("reset %s: %w", table, err)
		}
	}
	result, err := q.ExecContext(ctx, `UPDATE registries SET refresh_status='unknown', refresh_error=NULL, refreshed_at=0
		WHERE id=$1 AND (refresh_status<>'unknown' OR refresh_error IS NOT NULL OR refreshed_at<>0)`, id)
	if err != nil {
		return nil, fmt.Errorf("reset registry refresh status: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if total+changed > 0 {
		if _, err := q.ExecContext(ctx, `UPDATE catalog_versions SET version=version+1 WHERE singleton=1`); err != nil {
			return nil, fmt.Errorf("invalidate reset catalog: %w", err)
		}
	}
	return counts, nil
}
