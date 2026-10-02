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

const catalogPageKeys = `WITH ranked AS MATERIALIZED (
    SELECT rr.marketplace_id, rr.registry_id, rr.kind, rr.name, rr.content_digest,
           ca.assessment_digest, ro.revision_commit, ro.manifest_digest,
           ro.source_path, ro.source_origin, r.slug AS registry_slug,
           ROW_NUMBER() OVER (
               PARTITION BY rr.marketplace_id, rr.registry_id, rr.kind, rr.name
               ORDER BY rr.content_digest DESC
           ) AS rank
    FROM resource_revisions rr
    JOIN registries r ON r.id=rr.registry_id AND r.listed
    JOIN current_resource_occurrences ro
      ON ro.marketplace_id=rr.marketplace_id AND ro.registry_id=rr.registry_id
     AND ro.kind=rr.kind AND ro.name=rr.name AND ro.content_digest=rr.content_digest
    JOIN current_resource_assessments ca
      ON ca.marketplace_id=rr.marketplace_id AND ca.registry_id=rr.registry_id
     AND ca.kind=rr.kind AND ca.name=rr.name AND ca.content_digest=rr.content_digest
    JOIN resource_assessments eligible
      ON eligible.marketplace_id=ca.marketplace_id AND eligible.registry_id=ca.registry_id
     AND eligible.kind=ca.kind AND eligible.name=ca.name AND eligible.content_digest=ca.content_digest
     AND eligible.assessment_digest=ca.assessment_digest
    WHERE rr.marketplace_id=$1
      AND rr.kind IN ('skill', 'command', 'subagent', 'hook', 'mcpserver', 'collection')
      {{AFTER}}
), page_keys AS MATERIALIZED (
    SELECT marketplace_id, registry_id, kind, name, content_digest,
           assessment_digest, revision_commit, manifest_digest, source_path, source_origin, registry_slug
    FROM ranked WHERE rank=1
    ORDER BY registry_id, kind, name
    LIMIT {{LIMIT}}
)`

const catalogPageSelect = `
SELECT p.marketplace_id, p.registry_id, p.kind, p.name, p.content_digest, rr.archive_digest, rr.license, rr.description,
       ra.assessment_digest, ra.analyzer_version, ra.policy_digest, ra.decision, ra.findings, ra.inspection_manifest_digest, ra.assessed_at,
       p.revision_commit, p.manifest_digest, p.source_path, p.registry_slug,
       rv.kind, rv.target_id, rv.reason, rv.applied_at,
       rc.kind, rc.target_id, rc.reason, rc.applied_at,
       rg.kind, rg.target_id, rg.reason, rg.applied_at,
       p.source_origin
FROM page_keys p
JOIN resource_revisions rr
  ON rr.marketplace_id=p.marketplace_id AND rr.registry_id=p.registry_id
 AND rr.kind=p.kind AND rr.name=p.name AND rr.content_digest=p.content_digest
JOIN resource_assessments ra
  ON ra.marketplace_id=p.marketplace_id AND ra.registry_id=p.registry_id
 AND ra.kind=p.kind AND ra.name=p.name AND ra.content_digest=p.content_digest
 AND ra.assessment_digest=p.assessment_digest
LEFT JOIN revocations rv ON rv.kind='revision' AND rv.target_id=(p.marketplace_id || '/' || p.registry_id || '/' || p.kind || '/' || p.name || '@' || p.content_digest)
LEFT JOIN revocations rc ON rc.kind='coordinate' AND rc.target_id=(p.marketplace_id || '/' || p.registry_id || '/' || p.kind || '/' || p.name)
LEFT JOIN revocations rg ON rg.kind='registry' AND rg.target_id=p.registry_id
ORDER BY p.registry_id, p.kind, p.name`

const catalogIntegritySelect = `
SELECT ra.findings, ra.assessed_at, rv.applied_at, rc.applied_at, rg.applied_at,
       c.id, c.registry_id, c.manifest_digest, c.members, c.includes,
       p.registry_id, p.content_digest
FROM page_keys p
JOIN resource_assessments ra
  ON ra.marketplace_id=p.marketplace_id AND ra.registry_id=p.registry_id
 AND ra.kind=p.kind AND ra.name=p.name AND ra.content_digest=p.content_digest
 AND ra.assessment_digest=p.assessment_digest
LEFT JOIN revocations rv ON rv.kind='revision' AND rv.target_id=(p.marketplace_id || '/' || p.registry_id || '/' || p.kind || '/' || p.name || '@' || p.content_digest)
LEFT JOIN revocations rc ON rc.kind='coordinate' AND rc.target_id=(p.marketplace_id || '/' || p.registry_id || '/' || p.kind || '/' || p.name)
LEFT JOIN revocations rg ON rg.kind='registry' AND rg.target_id=p.registry_id
LEFT JOIN collection_revisions c ON p.kind='collection' AND c.id=p.content_digest
ORDER BY p.registry_id, p.kind, p.name`

func catalogProjectionQuery(selectSuffix, after, limit string) string {
	return strings.NewReplacer("{{AFTER}}", after, "{{LIMIT}}", limit).Replace(catalogPageKeys + selectSuffix)
}

func (s *Store) OwnsCatalogProjectionStore(meta store.MetadataStore) bool {
	other, ok := meta.(*Store)
	return ok && other == s && s.tx == nil
}

func (s *Store) ReadCatalogProjectionPage(ctx context.Context, req store.CatalogProjectionPageRequest) (store.CatalogProjectionPage, error) {
	page, _, err := s.ReadCatalogProjectionPageConditional(ctx, req, nil)
	return page, err
}

func (s *Store) ReadCatalogProjectionPageConditional(ctx context.Context, req store.CatalogProjectionPageRequest, unchanged func(store.CatalogVersion) bool) (store.CatalogProjectionPage, bool, error) {
	if s.tx != nil {
		page, _, err := s.readCatalogProjectionPageConditional(ctx, req, nil)
		return page, false, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return store.CatalogProjectionPage{}, false, fmt.Errorf("begin catalog projection snapshot: %w", err)
	}
	txStore := &Store{q: tx, tx: tx}
	page, matched, err := txStore.readCatalogProjectionPageConditional(ctx, req, unchanged)
	if err != nil {
		_ = tx.Rollback()
		return store.CatalogProjectionPage{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return store.CatalogProjectionPage{}, false, fmt.Errorf("commit catalog projection snapshot: %w", err)
	}
	return page, matched, nil
}

func (s *Store) readCatalogProjectionPageConditional(ctx context.Context, req store.CatalogProjectionPageRequest, unchanged func(store.CatalogVersion) bool) (store.CatalogProjectionPage, bool, error) {
	if req.Marketplace == "" {
		return store.CatalogProjectionPage{}, false, fmt.Errorf("catalog projection requires a marketplace")
	}
	if req.Limit <= 0 {
		return store.CatalogProjectionPage{}, false, fmt.Errorf("catalog projection limit must be positive")
	}
	version, err := s.currentCatalogVersion(ctx)
	if err != nil {
		return store.CatalogProjectionPage{}, false, err
	}

	if req.After == nil && unchanged != nil && unchanged(version) {
		rows, err := s.q.QueryContext(ctx, catalogProjectionQuery(catalogIntegritySelect, "", "$2"), req.Marketplace, req.Limit)
		if err != nil {
			return store.CatalogProjectionPage{}, false, fmt.Errorf("read catalog projection integrity: %w", err)
		}
		if err := store.ReadCatalogProjectionIntegrity(rows, true); err != nil {
			return store.CatalogProjectionPage{}, false, err
		}
		return store.CatalogProjectionPage{CatalogVersion: version}, true, nil
	}

	args := []any{req.Marketplace}
	after := ""
	if req.After != nil {
		if req.After.Coordinate.Marketplace != req.Marketplace {
			return store.CatalogProjectionPage{}, false, fmt.Errorf("catalog cursor marketplace does not match request")
		}
		after = ` AND (rr.registry_id, rr.kind, rr.name) > ($2,$3,$4)`
		args = append(args, req.After.Coordinate.Registry, req.After.Coordinate.Kind, req.After.Coordinate.Name)
	}
	limit := fmt.Sprintf("$%d", len(args)+1)
	args = append(args, req.Limit+1)
	rows, err := s.q.QueryContext(ctx, catalogProjectionQuery(catalogPageSelect, after, limit), args...)
	if err != nil {
		return store.CatalogProjectionPage{}, false, fmt.Errorf("read catalog projection: %w", err)
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
			return store.CatalogProjectionPage{}, false, err
		}
		page.Resources = append(page.Resources, resource)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return store.CatalogProjectionPage{}, false, err
	}
	if err := rows.Close(); err != nil {
		return store.CatalogProjectionPage{}, false, err
	}
	if err := s.loadCatalogCollections(ctx, page.Resources); err != nil {
		return store.CatalogProjectionPage{}, false, err
	}
	return page, false, nil
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
			JOIN current_resource_occurrences ro ON ro.marketplace_id=rr.marketplace_id AND ro.registry_id=rr.registry_id
			  AND ro.kind=rr.kind AND ro.name=rr.name AND ro.content_digest=rr.content_digest
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
