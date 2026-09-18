package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sort"
)

type blobOwnerSource struct {
	kind     string
	registry RegistryID
	id       string
	digest   ArchiveDigest
}

type blobResourceOwner struct {
	coordinate ResourceCoordinate
	digest     RevisionDigest
}

type blobOwnerEdge struct {
	kind   string
	digest ArchiveDigest
	owner  blobResourceOwner
}

type blobOwnershipPlan struct {
	resources  []blobOwnerEdge
	registries []RegistryID
}

func ReadBlobOwners(ctx context.Context, q ManagedSQLQueries, meta MetadataStore, marketplace MarketplaceID, kind string, digest ArchiveDigest) ([]AssessedResourceRevision, []RegistryID, error) {
	if marketplace == "" || !validInspectionDigest(string(digest)) || (kind != "inspection" && kind != "content" && kind != "manifest") {
		return nil, nil, ErrBlobOwnershipInvalid
	}
	rows, err := q.QueryContext(ctx, `SELECT 'coverage' AS owner_type, '' AS registry_id, '' AS resource_kind, '' AS name, '' AS content_digest
 FROM blob_owner_coverage WHERE version=1 AND NOT EXISTS (SELECT 1 FROM blob_owner_sources WHERE complete=0)
 UNION ALL SELECT 'matching_kind','','','','' WHERE
 EXISTS (SELECT 1 FROM blob_resource_owners WHERE blob_kind=$1 AND blob_digest=$2 AND marketplace_id=$3)
 OR EXISTS (SELECT 1 FROM blob_registry_owners WHERE blob_kind=$1 AND blob_digest=$2)
 UNION ALL SELECT DISTINCT 'resource',registry_id,resource_kind,name,content_digest FROM blob_resource_owners
 WHERE blob_digest=$2 AND marketplace_id=$3
 UNION ALL SELECT DISTINCT 'registry',registry_id,'','','' FROM blob_registry_owners WHERE blob_digest=$2
 LIMIT $4`, kind, digest, marketplace, BlobOwnerReadLimit+3)
	if err != nil {
		return nil, nil, fmt.Errorf("read blob owners: %w", err)
	}
	complete, matchingKind := false, false
	owners := make([]blobResourceOwner, 0)
	registries := make([]RegistryID, 0)
	for rows.Next() {
		var ownerType string
		var owner blobResourceOwner
		if err := rows.Scan(&ownerType, &owner.coordinate.Registry, &owner.coordinate.Kind, &owner.coordinate.Name, &owner.digest); err != nil {
			_ = rows.Close()
			return nil, nil, err
		}
		switch ownerType {
		case "coverage":
			complete = true
		case "matching_kind":
			matchingKind = true
		case "registry":
			registries = append(registries, owner.coordinate.Registry)
		case "resource":
			owner.coordinate.Marketplace = marketplace
			owners = append(owners, owner)
		default:
			_ = rows.Close()
			return nil, nil, ErrBlobOwnersIncomplete
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}
	if !complete || len(owners)+len(registries) > BlobOwnerReadLimit {
		return nil, nil, ErrBlobOwnersIncomplete
	}
	if !matchingKind || len(owners)+len(registries) == 0 {
		return nil, nil, ErrNotFound
	}
	sort.Slice(owners, func(i, j int) bool {
		if owners[i].coordinate != owners[j].coordinate {
			return owners[i].coordinate.String() < owners[j].coordinate.String()
		}
		return owners[i].digest < owners[j].digest
	})
	sort.Slice(registries, func(i, j int) bool { return registries[i] < registries[j] })
	revisions := make([]AssessedResourceRevision, 0, len(owners))
	for _, owner := range owners {
		revision, err := meta.GetResourceRevision(ctx, owner.coordinate, owner.digest)
		if errors.Is(err, ErrNotFound) {
			return nil, nil, ErrBlobOwnersIncomplete
		}
		if err != nil {
			return nil, nil, fmt.Errorf("resolve blob owner: %w", err)
		}
		revisions = append(revisions, revision)
	}
	return revisions, registries, nil
}

func RecordPublicationBlobOwnership(ctx context.Context, q ManagedSQLQueries, meta MetadataStore, req RegistryRevisionPublication, uploads []BlobUpload) error {
	payloads := make(map[ArchiveDigest][]byte, len(uploads))
	for _, upload := range uploads {
		payloads[upload.Digest] = upload.Data
	}
	sources := []blobOwnerSource{{kind: "registry", registry: req.Occurrence.RegistryID, id: string(req.Occurrence.RegistryRevisionManifestDigest)}}
	for _, assessment := range req.Assessments {
		finalized, err := FinalizeResourceAssessment(assessment)
		if err != nil {
			return err
		}
		sources = append(sources, blobOwnerSource{kind: "inspection", registry: assessment.Coordinate.Registry, id: string(finalized.AssessmentDigest)})
	}
	if req.Collection != nil {
		sources = append(sources, blobOwnerSource{kind: "collection", registry: req.Collection.RegistryID, id: req.Collection.ID})
	}
	for _, collection := range req.Collections {
		sources = append(sources, blobOwnerSource{kind: "collection", registry: collection.RegistryID, id: collection.ID})
	}
	for _, source := range sources {
		err := indexBlobOwnerSource(ctx, q, meta, source, func(ctx context.Context, digest ArchiveDigest) ([]byte, error) {
			data, ok := payloads[digest]
			if !ok {
				return nil, ErrBlobOwnersIncomplete
			}
			return data, nil
		})
		if errors.Is(err, ErrBlobOwnersIncomplete) || errors.Is(err, ErrBlobOwnershipInvalid) || errors.Is(err, ErrNotFound) {
			slog.WarnContext(ctx, "blob ownership source remains incomplete", "source_kind", source.kind, "registry", source.registry)
			continue
		}
		if err != nil {
			return err
		}
	}
	if req.ManagedFence != nil {
		return lockPublicationFence(ctx, q, req)
	}
	return nil
}

func BackfillBlobOwnership(ctx context.Context, q ManagedSQLQueries, meta MetadataStore, blobs BlobStore, limit int) (BlobOwnerBackfillResult, error) {
	result := BlobOwnerBackfillResult{}
	if blobs == nil || limit < 1 || limit > BlobOwnerBackfillLimit {
		return result, ErrBlobOwnershipInvalid
	}
	bounded, ok := blobs.(BoundedBlobStore)
	if !ok {
		return result, ErrTopology
	}
	rows, err := q.QueryContext(ctx, `SELECT source_kind,registry_id,source_id FROM blob_owner_sources WHERE complete=0 ORDER BY source_kind,registry_id,source_id LIMIT $1`, limit)
	if err != nil {
		return result, err
	}
	sources := make([]blobOwnerSource, 0, limit)
	for rows.Next() {
		var source blobOwnerSource
		if err := rows.Scan(&source.kind, &source.registry, &source.id); err != nil {
			_ = rows.Close()
			return result, err
		}
		sources = append(sources, source)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return result, err
	}
	if err := rows.Close(); err != nil {
		return result, err
	}
	for _, source := range sources {
		get := func(ctx context.Context, digest ArchiveDigest) ([]byte, error) {
			maximum := int64(BlobOwnerManifestLimit)
			if source.kind == "inspection" {
				maximum = 1 << 20
			}
			return bounded.GetBounded(ctx, digest, maximum)
		}
		if err := indexBlobOwnerSource(ctx, q, meta, source, get); err != nil {
			return BlobOwnerBackfillResult{}, fmt.Errorf("backfill %s source for registry %s: %w", source.kind, source.registry, err)
		}
		result.Processed++
	}
	var pending int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT 1 FROM blob_owner_sources WHERE complete=0 LIMIT 1) pending`).Scan(&pending); err != nil {
		return BlobOwnerBackfillResult{}, err
	}
	result.Remaining = pending != 0
	return result, nil
}

func indexBlobOwnerSource(ctx context.Context, q ManagedSQLQueries, meta MetadataStore, source blobOwnerSource, get func(context.Context, ArchiveDigest) ([]byte, error)) error {
	result, err := q.ExecContext(ctx, `UPDATE blob_owner_sources SET complete=complete WHERE source_kind=$1 AND registry_id=$2 AND source_id=$3`, source.kind, source.registry, source.id)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrBlobOwnersIncomplete
	}
	if err := q.QueryRowContext(ctx, `SELECT payload_digest FROM blob_owner_sources WHERE source_kind=$1 AND registry_id=$2 AND source_id=$3`, source.kind, source.registry, source.id).Scan(&source.digest); err != nil {
		return err
	}
	if !validInspectionDigest(string(source.digest)) {
		return ErrBlobOwnershipInvalid
	}
	payload, err := get(ctx, source.digest)
	if err != nil {
		return err
	}
	if len(payload) == 0 || len(payload) > BlobOwnerManifestLimit {
		return ErrBlobOwnershipInvalid
	}
	var plan blobOwnershipPlan
	switch source.kind {
	case "inspection":
		plan, err = inspectionBlobOwnership(ctx, q, meta, source, payload)
	case "registry":
		plan, err = registryBlobOwnership(ctx, q, source, payload)
	case "collection":
		plan, err = collectionBlobOwnership(ctx, q, source, payload)
	default:
		err = ErrBlobOwnershipInvalid
	}
	if err != nil {
		return err
	}
	for _, edge := range plan.resources {
		_, err = q.ExecContext(ctx, `INSERT INTO blob_resource_owners (blob_kind,blob_digest,marketplace_id,registry_id,resource_kind,name,content_digest) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, edge.kind, edge.digest, edge.owner.coordinate.Marketplace, edge.owner.coordinate.Registry, edge.owner.coordinate.Kind, edge.owner.coordinate.Name, edge.owner.digest)
		if err != nil {
			return fmt.Errorf("record blob resource owner: %w", err)
		}
	}
	for _, registry := range plan.registries {
		if _, err := q.ExecContext(ctx, `INSERT INTO blob_registry_owners (blob_kind,blob_digest,registry_id) VALUES ('manifest',$1,$2) ON CONFLICT DO NOTHING`, source.digest, registry); err != nil {
			return fmt.Errorf("record blob registry owner: %w", err)
		}
	}
	_, err = q.ExecContext(ctx, `UPDATE blob_owner_sources SET complete=1 WHERE source_kind=$1 AND registry_id=$2 AND source_id=$3`, source.kind, source.registry, source.id)
	return err
}

func readBlobOwnerRevisions(ctx context.Context, q ManagedSQLQueries, query string, args ...any) ([]ResourceRevision, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	revisions := make([]ResourceRevision, 0)
	for rows.Next() {
		if len(revisions) >= 4096 {
			return nil, ErrBlobOwnersIncomplete
		}
		var revision ResourceRevision
		var archive sql.NullString
		if err := rows.Scan(&revision.Coordinate.Marketplace, &revision.Coordinate.Registry, &revision.Coordinate.Kind, &revision.Coordinate.Name, &revision.ContentDigest, &archive); err != nil {
			return nil, err
		}
		revision.ArchiveDigest = ArchiveDigest(archive.String)
		revisions = append(revisions, revision)
	}
	return revisions, rows.Err()
}
