package store

import (
	"context"
	"fmt"
)

// ListArtifactArchiveDigestsSQL returns one stable page of distinct historical
// artifact digests for proof backfill.
func ListArtifactArchiveDigestsSQL(ctx context.Context, q ManagedSQLQueries, marketplace MarketplaceID, after ArchiveDigest, limit int) ([]ArchiveDigest, error) {
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT archive_digest
		FROM resource_revisions
		WHERE marketplace_id = $1 AND archive_digest IS NOT NULL AND archive_digest != '' AND archive_digest > $2
		ORDER BY archive_digest
		LIMIT $3`, marketplace, after, limit)
	if err != nil {
		return nil, fmt.Errorf("list artifact archive digests: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var digests []ArchiveDigest
	for rows.Next() {
		var digest ArchiveDigest
		if err := rows.Scan(&digest); err != nil {
			return nil, fmt.Errorf("scan artifact archive digest: %w", err)
		}
		digests = append(digests, digest)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list artifact archive digests: %w", err)
	}
	return digests, nil
}
