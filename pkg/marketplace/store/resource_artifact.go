package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

type ResourceArtifact struct {
	Coordinate    ResourceCoordinate         `json:"coordinate"`
	ContentDigest RevisionDigest             `json:"content_digest"`
	Reference     artifact.ArtifactReference `json:"reference"`
	CreatedAt     time.Time                  `json:"created_at"`
}

type ArtifactCoverage struct {
	LegacyRevisions int64
	V2Revisions     int64
}

type ArtifactCoverageStore interface {
	ArtifactCoverage(context.Context, MarketplaceID) (ArtifactCoverage, error)
}

func ReadArtifactCoverage(ctx context.Context, q ManagedSQLQueries, marketplace MarketplaceID) (ArtifactCoverage, error) {
	var coverage ArtifactCoverage
	err := q.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM resource_revisions WHERE marketplace_id=$1 AND archive_digest IS NOT NULL AND archive_digest<>''), (SELECT count(*) FROM resource_artifacts WHERE marketplace_id=$1 AND schema_version='v2')`, marketplace).Scan(&coverage.LegacyRevisions, &coverage.V2Revisions)
	return coverage, err
}

type ResourceArtifactStore interface {
	PutResourceArtifact(context.Context, ResourceArtifact) error
	ListResourceArtifacts(context.Context, ResourceCoordinate, RevisionDigest) ([]ResourceArtifact, error)
	ListResourceArtifactsByBlob(context.Context, MarketplaceID, artifact.BlobDigest) ([]ResourceArtifact, error)
}

const ResourceArtifactSchema = `
CREATE TABLE resource_artifacts (
 marketplace_id TEXT NOT NULL,
 registry_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK (kind IN ('skill','command','subagent','hook','mcpserver')),
 name TEXT NOT NULL,
 content_digest TEXT NOT NULL,
 schema_version TEXT NOT NULL CHECK (schema_version='v2'),
 artifact_digest TEXT NOT NULL CHECK (length(artifact_digest)=71),
 blob_digest TEXT NOT NULL CHECK (length(blob_digest)=71),
 media_type TEXT NOT NULL CHECK (media_type='application/vnd.agent-marketplace.artifact.v2.tar+gzip'),
 created_at BIGINT NOT NULL,
 PRIMARY KEY (marketplace_id,registry_id,kind,name,content_digest,schema_version),
 FOREIGN KEY (marketplace_id,registry_id,kind,name,content_digest) REFERENCES resource_revisions(marketplace_id,registry_id,kind,name,content_digest)
);
CREATE INDEX resource_artifacts_blob_owners ON resource_artifacts(marketplace_id,blob_digest);
`

func (a ResourceArtifact) Validate() error {
	if a.Coordinate.Marketplace == "" || a.Coordinate.Registry == "" || a.Coordinate.Name == "" || !validInspectionDigest(string(a.ContentDigest)) {
		return artifact.ErrInvalidReference
	}
	switch a.Coordinate.Kind {
	case "skill", "command", "subagent", "hook", "mcpserver":
	default:
		return artifact.ErrInvalidReference
	}
	return a.Reference.Validate()
}

func WriteResourceArtifact(ctx context.Context, q ManagedSQLQueries, a ResourceArtifact) error {
	if err := a.Validate(); err != nil {
		return err
	}
	_, err := q.ExecContext(ctx, `INSERT INTO resource_artifacts (marketplace_id,registry_id,kind,name,content_digest,schema_version,artifact_digest,blob_digest,media_type,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT (marketplace_id,registry_id,kind,name,content_digest,schema_version) DO NOTHING`, a.Coordinate.Marketplace, a.Coordinate.Registry, a.Coordinate.Kind, a.Coordinate.Name, a.ContentDigest, a.Reference.SchemaVersion, a.Reference.ArtifactDigest, a.Reference.BlobDigest, a.Reference.MediaType, time.Now().UTC().UnixMicro())
	if err != nil {
		return fmt.Errorf("insert resource artifact: %w", err)
	}
	stored, err := ReadResourceArtifacts(ctx, q, a.Coordinate, a.ContentDigest)
	if err != nil {
		return err
	}
	for _, old := range stored {
		if old.Reference.SchemaVersion == a.Reference.SchemaVersion {
			if old.Reference != a.Reference {
				return ErrConflict
			}
			return nil
		}
	}
	return ErrConflict
}

const resourceArtifactColumns = `marketplace_id,registry_id,kind,name,content_digest,schema_version,artifact_digest,blob_digest,media_type,created_at`

func scanResourceArtifacts(rows *sql.Rows) ([]ResourceArtifact, error) {
	defer func() { _ = rows.Close() }()
	out := make([]ResourceArtifact, 0)
	for rows.Next() {
		var a ResourceArtifact
		var created int64
		if err := rows.Scan(&a.Coordinate.Marketplace, &a.Coordinate.Registry, &a.Coordinate.Kind, &a.Coordinate.Name, &a.ContentDigest, &a.Reference.SchemaVersion, &a.Reference.ArtifactDigest, &a.Reference.BlobDigest, &a.Reference.MediaType, &created); err != nil {
			return nil, err
		}
		if err := a.Validate(); err != nil {
			return nil, fmt.Errorf("stored resource artifact: %w", err)
		}
		a.CreatedAt = time.UnixMicro(created).UTC()
		out = append(out, a)
	}
	return out, rows.Err()
}
func ReadResourceArtifacts(ctx context.Context, q ManagedSQLQueries, c ResourceCoordinate, d RevisionDigest) ([]ResourceArtifact, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+resourceArtifactColumns+` FROM resource_artifacts WHERE marketplace_id=$1 AND registry_id=$2 AND kind=$3 AND name=$4 AND content_digest=$5 ORDER BY schema_version`, c.Marketplace, c.Registry, c.Kind, c.Name, d)
	if err != nil {
		return nil, fmt.Errorf("read resource artifacts: %w", err)
	}
	return scanResourceArtifacts(rows)
}
func ReadResourceArtifactsByBlob(ctx context.Context, q ManagedSQLQueries, m MarketplaceID, d artifact.BlobDigest) ([]ResourceArtifact, error) {
	if m == "" || !validInspectionDigest(string(d)) {
		return nil, artifact.ErrInvalidReference
	}
	rows, err := q.QueryContext(ctx, `SELECT `+resourceArtifactColumns+` FROM resource_artifacts WHERE marketplace_id=$1 AND blob_digest=$2 ORDER BY registry_id,kind,name,content_digest LIMIT $3`, m, d, BlobOwnerReadLimit+1)
	if err != nil {
		return nil, fmt.Errorf("read artifact blob owners: %w", err)
	}
	out, err := scanResourceArtifacts(rows)
	if err != nil {
		return nil, err
	}
	if len(out) > BlobOwnerReadLimit {
		return nil, ErrBlobOwnershipInvalid
	}
	return out, nil
}

func putResourceArtifacts(ctx context.Context, meta MetadataStore, artifacts []ResourceArtifact) error {
	if len(artifacts) == 0 {
		return nil
	}
	target, ok := meta.(ResourceArtifactStore)
	if !ok {
		return fmt.Errorf("%w: artifact associations unsupported", ErrConflict)
	}
	for _, a := range artifacts {
		if err := target.PutResourceArtifact(ctx, a); err != nil {
			return err
		}
	}
	return nil
}

func CheckResourceArtifactIntegrity(ctx context.Context, q ManagedSQLQueries, blobs BlobStore) error {
	rows, err := q.QueryContext(ctx, `SELECT `+resourceArtifactColumns+` FROM resource_artifacts ORDER BY marketplace_id,registry_id,kind,name,content_digest`)
	if err != nil {
		return err
	}
	all, err := scanResourceArtifacts(rows)
	if err != nil {
		return err
	}
	for _, a := range all {
		raw, err := readArtifactBlob(ctx, blobs, ArchiveDigest(a.Reference.BlobDigest))
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("%w: %s", ErrBlobMissing, a.Reference.BlobDigest)
		}
		if err != nil {
			return err
		}
		decoded, err := artifact.VerifyV2(raw, a.Reference, nil)
		if err != nil {
			return err
		}
		if err := decoded.VerifyIdentity(a.Coordinate.Kind, a.Coordinate.Name, string(a.ContentDigest)); err != nil {
			return err
		}
	}
	return nil
}

func readArtifactBlob(ctx context.Context, blobs BlobStore, d ArchiveDigest) ([]byte, error) {
	const limit = 128 << 20
	if bounded, ok := blobs.(BoundedBlobStore); ok {
		return bounded.GetBounded(ctx, d, limit)
	}
	data, err := blobs.Get(ctx, d)
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, ErrBlobTooLarge
	}
	return data, nil
}
