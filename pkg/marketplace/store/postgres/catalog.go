package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func (s *Store) currentCatalogVersion(ctx context.Context) (store.CatalogVersion, error) {
	var version uint64
	err := s.q.QueryRowContext(ctx, `SELECT version FROM catalog_versions WHERE singleton=1`).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("read catalog version: %w", err)
	}
	return store.CatalogVersion(version), nil
}

func (s *Store) advanceCatalogVersion(ctx context.Context) error {
	result, err := s.q.ExecContext(ctx, `UPDATE catalog_versions SET version=version+1 WHERE singleton=1`)
	if err != nil {
		return fmt.Errorf("advance catalog version: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("advance catalog version: %w", err)
	}
	if updated != 1 {
		return fmt.Errorf("advance catalog version: singleton row is missing")
	}
	return nil
}

func (s *Store) ReadCatalogProjectionPage(ctx context.Context, req store.CatalogProjectionPageRequest) (store.CatalogProjectionPage, error) {
	if req.Marketplace == "" {
		return store.CatalogProjectionPage{}, fmt.Errorf("catalog projection requires a marketplace")
	}
	if req.Limit <= 0 {
		return store.CatalogProjectionPage{}, fmt.Errorf("catalog projection limit must be positive")
	}
	version, err := s.currentCatalogVersion(ctx)
	if err != nil {
		return store.CatalogProjectionPage{}, err
	}

	args := []any{req.Marketplace}
	after := ""
	if req.After != nil {
		if req.After.Coordinate.Marketplace != req.Marketplace {
			return store.CatalogProjectionPage{}, fmt.Errorf("catalog cursor marketplace does not match request")
		}
		after = ` AND (rr.registry_id, rr.kind, rr.name) > ($2, $3, $4)`
		args = append(args, req.After.Coordinate.Registry, req.After.Coordinate.Kind, req.After.Coordinate.Name)
	}
	args = append(args, req.Limit+1)
	rows, err := s.q.QueryContext(ctx, `
		SELECT marketplace_id, registry_id, kind, name, content_digest, archive_digest, license, description,
		       assessment_digest, analyzer_version, policy_digest, decision, findings, inspection_manifest_digest, assessed_at,
		       revision_commit, manifest_digest, source_path, registry_slug,
		       revision_revocation_kind, revision_revocation_target, revision_revocation_reason, revision_revocation_at,
		       coordinate_revocation_kind, coordinate_revocation_target, coordinate_revocation_reason, coordinate_revocation_at,
		       registry_revocation_kind, registry_revocation_target, registry_revocation_reason, registry_revocation_at,
		       source_origin
		FROM (
			SELECT rr.marketplace_id, rr.registry_id, rr.kind, rr.name, rr.content_digest, rr.archive_digest, rr.license, rr.description,
			       ra.assessment_digest, ra.analyzer_version, ra.policy_digest, ra.decision, ra.findings, ra.inspection_manifest_digest, ra.assessed_at,
			       ro.revision_commit, ro.manifest_digest, ro.source_path, r.slug AS registry_slug,
			       rv.kind AS revision_revocation_kind, rv.target_id AS revision_revocation_target, rv.reason AS revision_revocation_reason, rv.applied_at AS revision_revocation_at,
			       rc.kind AS coordinate_revocation_kind, rc.target_id AS coordinate_revocation_target, rc.reason AS coordinate_revocation_reason, rc.applied_at AS coordinate_revocation_at,
			       rg.kind AS registry_revocation_kind, rg.target_id AS registry_revocation_target, rg.reason AS registry_revocation_reason, rg.applied_at AS registry_revocation_at,
			       so.source_origin,
			       ROW_NUMBER() OVER (
			         PARTITION BY rr.marketplace_id, rr.registry_id, rr.kind, rr.name
			         ORDER BY rr.content_digest DESC
			       ) AS rank
			FROM resource_revisions rr
			JOIN registries r ON r.id=rr.registry_id AND r.listed
			JOIN registry_heads sh ON sh.registry_id=rr.registry_id
			JOIN resource_occurrences ro ON ro.marketplace_id=rr.marketplace_id AND ro.registry_id=rr.registry_id
			  AND ro.kind=rr.kind AND ro.name=rr.name AND ro.content_digest=rr.content_digest AND ro.revision_commit=sh.revision_commit
			LEFT JOIN registry_occurrences so ON so.registry_id=sh.registry_id AND so.revision_commit=sh.revision_commit
			JOIN current_resource_assessments ca ON ca.marketplace_id=rr.marketplace_id AND ca.registry_id=rr.registry_id
			  AND ca.kind=rr.kind AND ca.name=rr.name AND ca.content_digest=rr.content_digest
			JOIN resource_assessments ra ON ra.marketplace_id=ca.marketplace_id AND ra.registry_id=ca.registry_id
			  AND ra.kind=ca.kind AND ra.name=ca.name AND ra.content_digest=ca.content_digest AND ra.assessment_digest=ca.assessment_digest
			LEFT JOIN revocations rv ON rv.kind='revision' AND rv.target_id=(rr.marketplace_id || '/' || rr.registry_id || '/' || rr.kind || '/' || rr.name || '@' || rr.content_digest)
			LEFT JOIN revocations rc ON rc.kind='coordinate' AND rc.target_id=(rr.marketplace_id || '/' || rr.registry_id || '/' || rr.kind || '/' || rr.name)
			LEFT JOIN revocations rg ON rg.kind='registry' AND rg.target_id=rr.registry_id
			WHERE rr.marketplace_id=$1
			  AND rr.kind IN ('skill', 'command', 'subagent', 'hook', 'mcpserver', 'collection')
			  `+after+`
		) AS catalog
		WHERE rank=1
		ORDER BY registry_id, kind, name
		LIMIT $`+fmt.Sprint(len(args))+``, args...)
	if err != nil {
		return store.CatalogProjectionPage{}, fmt.Errorf("read catalog projection: %w", err)
	}
	defer func() { _ = rows.Close() }()
	page := store.CatalogProjectionPage{CatalogVersion: version, Resources: make([]store.CatalogProjectionResource, 0, req.Limit)}
	for rows.Next() {
		if len(page.Resources) == req.Limit {
			cursor := store.CatalogProjectionCursor{Coordinate: page.Resources[len(page.Resources)-1].Coordinate}
			page.Next = &cursor
			break
		}
		resource, err := scanCatalogResource(rows)
		if err != nil {
			return store.CatalogProjectionPage{}, err
		}
		page.Resources = append(page.Resources, resource)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return store.CatalogProjectionPage{}, err
	}
	if err := rows.Close(); err != nil {
		return store.CatalogProjectionPage{}, err
	}
	if err := s.loadCatalogCollections(ctx, page.Resources); err != nil {
		return store.CatalogProjectionPage{}, err
	}
	return page, nil
}

func scanCatalogResource(sc sourceScanner) (store.CatalogProjectionResource, error) {
	var out store.CatalogProjectionResource
	var archive, license, sourcePath, sourceOrigin sql.NullString
	var findings string
	var assessedAt int64
	var revKind, revTarget, revReason, coordKind, coordTarget, coordReason, registryKind, registryTarget, registryReason sql.NullString
	var revAt, coordAt, registryAt sql.NullInt64
	err := sc.Scan(
		&out.Coordinate.Marketplace, &out.Coordinate.Registry, &out.Coordinate.Kind, &out.Coordinate.Name, &out.ContentDigest, &archive, &license, &out.Description,
		&out.Assessment.AssessmentDigest, &out.Assessment.AnalyzerVersion, &out.Assessment.PolicyDigest, &out.Assessment.Decision, &findings, &out.Assessment.InspectionManifestDigest, &assessedAt,
		&out.Occurrence.Commit, &out.Occurrence.ManifestDigest, &sourcePath, &out.RegistrySlug,
		&revKind, &revTarget, &revReason, &revAt, &coordKind, &coordTarget, &coordReason, &coordAt, &registryKind, &registryTarget, &registryReason, &registryAt, &sourceOrigin,
	)
	if err != nil {
		return store.CatalogProjectionResource{}, err
	}
	out.ArchiveDigest, out.License = store.ArchiveDigest(archive.String), license.String
	out.Assessment.Coordinate, out.Assessment.ContentDigest, out.Assessment.AssessedAt = out.Coordinate, out.ContentDigest, microToTime(assessedAt)
	out.Occurrence.Coordinate, out.Occurrence.ContentDigest, out.Occurrence.SourcePath = out.Coordinate, out.ContentDigest, sourcePath.String
	if err := unmarshalJSON([]byte(findings), &out.Assessment.Findings); err != nil {
		return store.CatalogProjectionResource{}, fmt.Errorf("unmarshal assessment findings: %w", err)
	}
	for _, candidate := range []struct {
		kind, target, reason sql.NullString
		at                   sql.NullInt64
	}{{revKind, revTarget, revReason, revAt}, {coordKind, coordTarget, coordReason, coordAt}, {registryKind, registryTarget, registryReason, registryAt}} {
		if candidate.kind.Valid {
			out.Revocation = &store.Revocation{Kind: store.RevocationKind(candidate.kind.String), TargetID: candidate.target.String, Reason: candidate.reason.String, AppliedAt: microToTime(candidate.at.Int64)}
			break
		}
	}
	out.Availability = store.CatalogAvailabilityAvailable
	if out.Assessment.Decision == store.AssessmentDecisionReject {
		out.Availability = store.CatalogAvailabilityBlocked
	}
	if out.Revocation != nil {
		out.Availability = store.CatalogAvailabilityRevoked
	}
	if sourceOrigin.Valid && sourcePath.Valid {
		out.Source = &store.CatalogProjectionSource{RepositoryURL: sourceOrigin.String, Path: sourcePath.String, Commit: out.Occurrence.Commit}
	}
	return out, nil
}

const catalogBatchSize = 500

type catalogCollectionKey struct {
	registry store.RegistryID
	digest   store.RevisionDigest
}

type catalogCollectionReferences struct {
	members  []store.ResourceCoordinate
	includes []store.ResourceCoordinate
}

func (s *Store) loadCatalogCollections(ctx context.Context, resources []store.CatalogProjectionResource) error {
	indexes := make([]int, 0)
	for i := range resources {
		if resources[i].Coordinate.Kind != "collection" {
			continue
		}
		resources[i].Collection = &store.CatalogCollectionProjection{
			Resources:            make([]store.ResourceCoordinate, 0),
			Includes:             make([]store.ResourceCoordinate, 0),
			UnresolvedReferences: make([]store.CatalogUnresolvedReference, 0),
		}
		indexes = append(indexes, i)
	}
	if len(indexes) == 0 {
		return nil
	}

	collections := make(map[catalogCollectionKey]catalogCollectionReferences, len(indexes))
	for start := 0; start < len(indexes); start += catalogBatchSize {
		end := min(start+catalogBatchSize, len(indexes))
		args := make([]any, 0, end-start)
		values := make([]string, 0, end-start)
		for _, index := range indexes[start:end] {
			resource := resources[index]
			values = append(values, fmt.Sprintf("$%d", len(args)+1))
			args = append(args, resource.ContentDigest)
		}
		// collection_revisions.id is the PRIMARY KEY (the content digest), so
		// no further columns are needed to identify the row.
		rows, err := s.q.QueryContext(ctx, `SELECT id, registry_id, manifest_digest, members, includes
			FROM collection_revisions
			WHERE id IN (`+strings.Join(values, ", ")+`)`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			var registry store.RegistryID
			var digest store.RevisionDigest
			var membersJSON, includesJSON string
			if err := rows.Scan(&id, &registry, &digest, &membersJSON, &includesJSON); err != nil {
				_ = rows.Close()
				return err
			}
			var collection catalogCollectionReferences
			if err := json.Unmarshal([]byte(membersJSON), &collection.members); err != nil {
				_ = rows.Close()
				return fmt.Errorf("unmarshal collection members: %w", err)
			}
			if err := json.Unmarshal([]byte(includesJSON), &collection.includes); err != nil {
				_ = rows.Close()
				return fmt.Errorf("unmarshal collection includes: %w", err)
			}
			collections[catalogCollectionKey{registry: registry, digest: digest}] = collection
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}

	references := make([]store.ResourceCoordinate, 0)
	seen := make(map[store.ResourceCoordinate]struct{})
	for _, index := range indexes {
		resource := resources[index]
		key := catalogCollectionKey{registry: resource.Coordinate.Registry, digest: resource.ContentDigest}
		collection, ok := collections[key]
		if !ok {
			continue
		}
		for i := range collection.members {
			if collection.members[i].Marketplace == "" {
				collection.members[i].Marketplace = resource.Coordinate.Marketplace
			}
			if store.IsMarketplaceResourceKind(collection.members[i].Kind) {
				if _, ok := seen[collection.members[i]]; !ok {
					seen[collection.members[i]] = struct{}{}
					references = append(references, collection.members[i])
				}
			}
		}
		for i := range collection.includes {
			if collection.includes[i].Marketplace == "" {
				collection.includes[i].Marketplace = resource.Coordinate.Marketplace
			}
			if store.IsMarketplaceResourceKind(collection.includes[i].Kind) {
				if _, ok := seen[collection.includes[i]]; !ok {
					seen[collection.includes[i]] = struct{}{}
					references = append(references, collection.includes[i])
				}
			}
		}
		collections[key] = collection
	}
	visible, err := s.catalogCoordinatesVisible(ctx, references)
	if err != nil {
		return err
	}

	for _, index := range indexes {
		resource := &resources[index]
		key := catalogCollectionKey{registry: resource.Coordinate.Registry, digest: resource.ContentDigest}
		collection, ok := collections[key]
		if !ok {
			continue
		}
		out := store.CatalogCollectionProjection{
			Resources:            make([]store.ResourceCoordinate, 0),
			Includes:             make([]store.ResourceCoordinate, 0),
			UnresolvedReferences: make([]store.CatalogUnresolvedReference, 0),
		}
		for _, member := range collection.members {
			if visible[member] {
				out.Resources = append(out.Resources, member)
			} else {
				out.UnresolvedReferences = append(out.UnresolvedReferences, store.CatalogUnresolvedReference{ReferenceType: "resource", Coordinate: member, Reason: "not_found"})
			}
		}
		for _, include := range collection.includes {
			if visible[include] {
				out.Includes = append(out.Includes, include)
			} else {
				out.UnresolvedReferences = append(out.UnresolvedReferences, store.CatalogUnresolvedReference{ReferenceType: "include", Coordinate: include, Reason: "not_found"})
			}
		}
		resource.Collection = &out
	}
	return nil
}

func (s *Store) catalogCoordinatesVisible(ctx context.Context, coordinates []store.ResourceCoordinate) (map[store.ResourceCoordinate]bool, error) {
	visible := make(map[store.ResourceCoordinate]bool, len(coordinates))
	for start := 0; start < len(coordinates); start += catalogBatchSize {
		end := min(start+catalogBatchSize, len(coordinates))
		args := make([]any, 0, 4*(end-start))
		values := make([]string, 0, end-start)
		for _, coordinate := range coordinates[start:end] {
			values = append(values, fmt.Sprintf("($%d, $%d, $%d, $%d)", len(args)+1, len(args)+2, len(args)+3, len(args)+4))
			args = append(args, coordinate.Marketplace, coordinate.Registry, coordinate.Kind, coordinate.Name)
		}
		rows, err := s.q.QueryContext(ctx, `WITH requested (marketplace_id, registry_id, kind, name) AS (VALUES `+strings.Join(values, ", ")+`)
			SELECT DISTINCT requested.marketplace_id, requested.registry_id, requested.kind, requested.name
			FROM requested
			JOIN resource_revisions rr ON rr.marketplace_id=requested.marketplace_id AND rr.registry_id=requested.registry_id
			  AND rr.kind=requested.kind AND rr.name=requested.name
			JOIN registries r ON r.id=rr.registry_id AND r.listed
			JOIN registry_heads sh ON sh.registry_id=rr.registry_id
			JOIN resource_occurrences ro ON ro.marketplace_id=rr.marketplace_id AND ro.registry_id=rr.registry_id
			  AND ro.kind=rr.kind AND ro.name=rr.name AND ro.content_digest=rr.content_digest AND ro.revision_commit=sh.revision_commit
			JOIN current_resource_assessments ca ON ca.marketplace_id=rr.marketplace_id AND ca.registry_id=rr.registry_id
			  AND ca.kind=rr.kind AND ca.name=rr.name AND ca.content_digest=rr.content_digest`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var coordinate store.ResourceCoordinate
			if err := rows.Scan(&coordinate.Marketplace, &coordinate.Registry, &coordinate.Kind, &coordinate.Name); err != nil {
				_ = rows.Close()
				return nil, err
			}
			visible[coordinate] = true
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return visible, nil
}
