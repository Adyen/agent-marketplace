package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type rowScanner interface {
	Scan(dest ...any) error
}

const assessedResourceColumns = `
	rr.marketplace_id, rr.registry_id, rr.kind, rr.name, rr.content_digest,
	rr.archive_digest, rr.license, rr.description, COALESCE(rr.requires,''),
	ra.assessment_digest, ra.analyzer_version, ra.policy_digest, ra.decision,
	ra.findings, ra.inspection_manifest_digest, ra.assessed_at,
	ro.revision_commit, ro.manifest_digest, ro.source_path,
	rv.kind, rv.target_id, rv.reason, rv.applied_at,
	rc.kind, rc.target_id, rc.reason, rc.applied_at,
	rg.kind, rg.target_id, rg.reason, rg.applied_at`

const assessmentJoins = `
	JOIN current_resource_assessments ca
	  ON ca.marketplace_id=rr.marketplace_id AND ca.registry_id=rr.registry_id
	 AND ca.kind=rr.kind AND ca.name=rr.name AND ca.content_digest=rr.content_digest
	JOIN resource_assessments ra
	  ON ra.marketplace_id=ca.marketplace_id AND ra.registry_id=ca.registry_id
	 AND ra.kind=ca.kind AND ra.name=ca.name AND ra.content_digest=ca.content_digest
	 AND ra.assessment_digest=ca.assessment_digest`

const revocationJoins = `
	LEFT JOIN revocations rv
	  ON rv.kind='revision' AND rv.target_id=(rr.marketplace_id || '/' || rr.registry_id || '/' || rr.kind || '/' || rr.name || '@' || rr.content_digest)
	LEFT JOIN revocations rc
	  ON rc.kind='coordinate' AND rc.target_id=(rr.marketplace_id || '/' || rr.registry_id || '/' || rr.kind || '/' || rr.name)
	LEFT JOIN revocations rg
	  ON rg.kind='registry' AND rg.target_id=rr.registry_id`

// latestOccurrenceJoins gives historical/addressed reads one deterministic
// occurrence per revision. Discovery reads use the registry head instead.
const latestOccurrenceJoins = `
	JOIN all_resource_occurrences ro
	  ON ro.marketplace_id=rr.marketplace_id AND ro.registry_id=rr.registry_id
	 AND ro.kind=rr.kind AND ro.name=rr.name AND ro.content_digest=rr.content_digest
	JOIN all_registry_occurrences so
	  ON so.registry_id=ro.registry_id AND so.occurrence_key=ro.occurrence_key
	 AND ro.occurrence_key = (
		SELECT ro2.occurrence_key
		FROM all_resource_occurrences ro2
		JOIN all_registry_occurrences so2
		  ON so2.registry_id=ro2.registry_id AND so2.occurrence_key=ro2.occurrence_key
		WHERE ro2.marketplace_id=rr.marketplace_id AND ro2.registry_id=rr.registry_id
		  AND ro2.kind=rr.kind AND ro2.name=rr.name AND ro2.content_digest=rr.content_digest
		ORDER BY so2.occurred_at DESC, ro2.occurrence_key DESC
		LIMIT 1
	)`

const headOccurrenceJoins = `
	JOIN current_resource_occurrences ro
	  ON ro.marketplace_id=rr.marketplace_id AND ro.registry_id=rr.registry_id
	 AND ro.kind=rr.kind AND ro.name=rr.name AND ro.content_digest=rr.content_digest
`

func (s *Store) GetResourceRevision(ctx context.Context, coord store.ResourceCoordinate, digest store.RevisionDigest) (store.AssessedResourceRevision, error) {
	query := `SELECT ` + assessedResourceColumns + ` FROM resource_revisions rr ` + assessmentJoins + latestOccurrenceJoins + revocationJoins + `
		WHERE rr.marketplace_id=? AND rr.registry_id=? AND rr.kind=? AND rr.name=? AND rr.content_digest=?`
	return s.scanAssessedResource(s.q.QueryRowContext(ctx, query,
		coord.Marketplace, coord.Registry, coord.Kind, coord.Name, digest))
}

func (s *Store) scanAssessedResource(sc rowScanner) (store.AssessedResourceRevision, error) {
	var out store.AssessedResourceRevision
	var archive, license, sourcePath sql.NullString
	var findings string
	var assessedAt int64
	var revKind, revTarget, revReason sql.NullString
	var revAt sql.NullInt64
	var coordKind, coordTarget, coordReason sql.NullString
	var coordAt sql.NullInt64
	var registryKind, registryTarget, registryReason sql.NullString
	var registryAt sql.NullInt64

	err := sc.Scan(
		&out.Coordinate.Marketplace, &out.Coordinate.Registry, &out.Coordinate.Kind, &out.Coordinate.Name,
		&out.ContentDigest, &archive, &license, &out.Description, &out.Requires,
		&out.Assessment.AssessmentDigest, &out.Assessment.AnalyzerVersion, &out.Assessment.PolicyDigest,
		&out.Assessment.Decision, &findings, &out.Assessment.InspectionManifestDigest, &assessedAt,
		&out.Occurrence.Commit, &out.Occurrence.ManifestDigest, &sourcePath,
		&revKind, &revTarget, &revReason, &revAt,
		&coordKind, &coordTarget, &coordReason, &coordAt,
		&registryKind, &registryTarget, &registryReason, &registryAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return store.AssessedResourceRevision{}, store.ErrNotFound
	}
	if err != nil {
		return store.AssessedResourceRevision{}, err
	}
	out.ArchiveDigest = store.ArchiveDigest(archive.String)
	out.License = license.String
	out.Assessment.Coordinate = out.Coordinate
	out.Assessment.ContentDigest = out.ContentDigest
	out.Assessment.AssessedAt = microToTime(assessedAt)
	if err := unmarshalJSON([]byte(findings), &out.Assessment.Findings); err != nil {
		return store.AssessedResourceRevision{}, fmt.Errorf("unmarshal assessment findings: %w", err)
	}
	out.Occurrence.Coordinate = out.Coordinate
	out.Occurrence.ContentDigest = out.ContentDigest
	out.Occurrence.SourcePath = sourcePath.String
	for _, candidate := range []struct {
		kind, target, reason sql.NullString
		at                   sql.NullInt64
	}{
		{revKind, revTarget, revReason, revAt},
		{coordKind, coordTarget, coordReason, coordAt},
		{registryKind, registryTarget, registryReason, registryAt},
	} {
		if candidate.kind.Valid {
			out.Revocation = &store.Revocation{
				Kind: store.RevocationKind(candidate.kind.String), TargetID: candidate.target.String,
				Reason: candidate.reason.String, AppliedAt: microToTime(candidate.at.Int64),
			}
			break
		}
	}
	return out, nil
}

func normalizedRevision(rev store.ResourceRevision) store.ResourceRevision {
	rev.Description = store.BoundDescription(rev.Description)
	return rev
}

func (s *Store) PutResourceRevision(ctx context.Context, rev store.ResourceRevision) error {
	return s.withTx(ctx, func(tx *Store) error { return tx.putResourceRevision(ctx, rev) })
}

func (s *Store) putResourceRevision(ctx context.Context, rev store.ResourceRevision) error {
	rev = normalizedRevision(rev)
	_, err := s.q.ExecContext(ctx, `INSERT OR IGNORE INTO resource_revisions
		(marketplace_id, registry_id, kind, name, content_digest, archive_digest, license, description, requires)
		VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, NULLIF(?, ''))`,
		rev.Coordinate.Marketplace, rev.Coordinate.Registry, rev.Coordinate.Kind, rev.Coordinate.Name,
		rev.ContentDigest, rev.ArchiveDigest, rev.License, rev.Description, rev.Requires)
	if err != nil {
		return err
	}
	existing, err := s.getIntrinsicResourceRevision(ctx, rev.Coordinate, rev.ContentDigest)
	if err != nil {
		return err
	}
	if existing != rev {
		repaired := existing
		repaired.ArchiveDigest = rev.ArchiveDigest
		fillRequires := existing.Requires == "" && rev.Requires != ""
		if fillRequires {
			repaired.Requires = rev.Requires
		}
		if repaired != rev {
			return fmt.Errorf("%w: resource revision %q already has different immutable metadata", store.ErrConflict, rev.ContentDigest)
		}
		if _, err := s.q.ExecContext(ctx, `UPDATE resource_revisions SET archive_digest=NULLIF(?, '')
			WHERE marketplace_id=? AND registry_id=? AND kind=? AND name=? AND content_digest=?`,
			rev.ArchiveDigest, rev.Coordinate.Marketplace, rev.Coordinate.Registry, rev.Coordinate.Kind,
			rev.Coordinate.Name, rev.ContentDigest); err != nil {
			return err
		}
		if fillRequires {
			if _, err := s.q.ExecContext(ctx, `UPDATE resource_revisions SET requires=?
				WHERE marketplace_id=? AND registry_id=? AND kind=? AND name=? AND content_digest=? AND requires IS NULL`,
				rev.Requires, rev.Coordinate.Marketplace, rev.Coordinate.Registry, rev.Coordinate.Kind,
				rev.Coordinate.Name, rev.ContentDigest); err != nil {
				return err
			}
		}
		for _, digest := range []store.ArchiveDigest{existing.ArchiveDigest, rev.ArchiveDigest} {
			if digest == "" {
				continue
			}
			if _, err := s.q.ExecContext(ctx, `INSERT INTO blob_resource_owners
				(blob_kind,blob_digest,marketplace_id,registry_id,resource_kind,name,content_digest)
				VALUES ('archive',?,?,?,?,?,?) ON CONFLICT DO NOTHING`,
				string(digest), rev.Coordinate.Marketplace, rev.Coordinate.Registry, rev.Coordinate.Kind,
				rev.Coordinate.Name, rev.ContentDigest); err != nil {
				return err
			}
		}
		if existing.ArchiveDigest != rev.ArchiveDigest {
			slog.WarnContext(ctx, "resource revision archive digest repaired",
				"marketplace", rev.Coordinate.Marketplace, "registry", rev.Coordinate.Registry,
				"kind", rev.Coordinate.Kind, "name", rev.Coordinate.Name,
				"content_digest", rev.ContentDigest,
				"old_archive_digest", existing.ArchiveDigest, "new_archive_digest", rev.ArchiveDigest)
		}
		if fillRequires {
			slog.DebugContext(ctx, "resource revision requires filled",
				"marketplace", rev.Coordinate.Marketplace, "registry", rev.Coordinate.Registry,
				"kind", rev.Coordinate.Kind, "name", rev.Coordinate.Name,
				"content_digest", rev.ContentDigest)
		}
		return s.advanceCatalogVersion(ctx)
	}
	// An intrinsic revision is not catalog-visible until both a current
	// occurrence and assessment point at it.
	return nil
}

func (s *Store) getIntrinsicResourceRevision(ctx context.Context, coord store.ResourceCoordinate, digest store.RevisionDigest) (store.ResourceRevision, error) {
	var rev store.ResourceRevision
	var archive, license sql.NullString
	err := s.q.QueryRowContext(ctx, `SELECT marketplace_id, registry_id, kind, name, content_digest, archive_digest, license, description, COALESCE(requires,'')
		FROM resource_revisions WHERE marketplace_id=? AND registry_id=? AND kind=? AND name=? AND content_digest=?`,
		coord.Marketplace, coord.Registry, coord.Kind, coord.Name, digest,
	).Scan(&rev.Coordinate.Marketplace, &rev.Coordinate.Registry, &rev.Coordinate.Kind, &rev.Coordinate.Name,
		&rev.ContentDigest, &archive, &license, &rev.Description, &rev.Requires)
	if errors.Is(err, sql.ErrNoRows) {
		return store.ResourceRevision{}, store.ErrNotFound
	}
	if err != nil {
		return store.ResourceRevision{}, err
	}
	rev.ArchiveDigest = store.ArchiveDigest(archive.String)
	rev.License = license.String
	return rev, nil
}

func (s *Store) PutResourceOccurrence(ctx context.Context, occ store.ResourceOccurrence) error {
	return s.withTx(ctx, func(tx *Store) error { return tx.putResourceOccurrence(ctx, occ) })
}

func (s *Store) putResourceOccurrence(ctx context.Context, occ store.ResourceOccurrence) error {
	normalized, err := store.NormalizeResourceOccurrence(occ)
	if err != nil {
		return err
	}
	occ = normalized
	result, err := s.q.ExecContext(ctx, `INSERT OR IGNORE INTO resource_occurrences
		(marketplace_id, registry_id, kind, name, content_digest, revision_commit, manifest_digest, source_path)
		VALUES (?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''))`,
		occ.Coordinate.Marketplace, occ.Coordinate.Registry, occ.Coordinate.Kind, occ.Coordinate.Name,
		occ.ContentDigest, occ.Commit, occ.ManifestDigest, occ.SourcePath)
	if err != nil {
		return err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	changed := inserted > 0
	var existing store.ResourceOccurrence
	var sourcePath sql.NullString
	err = s.q.QueryRowContext(ctx, `SELECT marketplace_id, registry_id, kind, name, content_digest, revision_commit, manifest_digest, source_path FROM resource_occurrences
		WHERE marketplace_id=? AND registry_id=? AND kind=? AND name=? AND content_digest=? AND revision_commit=?`,
		occ.Coordinate.Marketplace, occ.Coordinate.Registry, occ.Coordinate.Kind, occ.Coordinate.Name,
		occ.ContentDigest, occ.Commit).Scan(&existing.Coordinate.Marketplace, &existing.Coordinate.Registry, &existing.Coordinate.Kind, &existing.Coordinate.Name, &existing.ContentDigest, &existing.Commit, &existing.ManifestDigest, &sourcePath)
	if err != nil {
		return err
	}
	existing.SourcePath = sourcePath.String
	core := existing
	core.SourcePath = occ.SourcePath
	if !occ.Equal(core) {
		repaired := core
		repaired.ManifestDigest = occ.ManifestDigest
		if !occ.Equal(repaired) {
			return fmt.Errorf("%w: resource occurrence for commit %q already has different immutable fields", store.ErrConflict, occ.Commit)
		}
		if _, err := s.q.ExecContext(ctx, `UPDATE resource_occurrences SET manifest_digest=?
			WHERE marketplace_id=? AND registry_id=? AND kind=? AND name=? AND content_digest=? AND revision_commit=?`,
			occ.ManifestDigest, occ.Coordinate.Marketplace, occ.Coordinate.Registry, occ.Coordinate.Kind, occ.Coordinate.Name, occ.ContentDigest, occ.Commit); err != nil {
			return err
		}
		existing.ManifestDigest = occ.ManifestDigest
		changed = true
	}
	if existing.SourcePath != "" && occ.SourcePath != "" && existing.SourcePath != occ.SourcePath {
		return fmt.Errorf("%w: resource occurrence for commit %q already has a different source path", store.ErrConflict, occ.Commit)
	}
	if existing.SourcePath == "" && occ.SourcePath != "" {
		result, err := s.q.ExecContext(ctx, `UPDATE resource_occurrences SET source_path=? WHERE marketplace_id=? AND registry_id=? AND kind=? AND name=? AND content_digest=? AND revision_commit=? AND source_path IS NULL`, occ.SourcePath, occ.Coordinate.Marketplace, occ.Coordinate.Registry, occ.Coordinate.Kind, occ.Coordinate.Name, occ.ContentDigest, occ.Commit)
		if err != nil {
			return err
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return err
		}
		changed = changed || updated > 0
	}
	if !changed {
		return nil
	}
	return s.advanceCatalogVersion(ctx)
}

func (s *Store) PutResourceAssessment(ctx context.Context, assessment store.ResourceAssessment) error {
	finalized, err := store.FinalizeResourceAssessment(assessment)
	if err != nil {
		return err
	}
	findings, err := marshalJSON(finalized.Findings)
	if err != nil {
		return err
	}
	_, err = s.q.ExecContext(ctx, `INSERT OR IGNORE INTO resource_assessments
		(marketplace_id, registry_id, kind, name, content_digest, assessment_digest,
		 analyzer_version, policy_digest, decision, findings, inspection_manifest_digest, assessed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		finalized.Coordinate.Marketplace, finalized.Coordinate.Registry, finalized.Coordinate.Kind, finalized.Coordinate.Name,
		finalized.ContentDigest, finalized.AssessmentDigest, finalized.AnalyzerVersion, finalized.PolicyDigest,
		finalized.Decision, findings, finalized.InspectionManifestDigest, timeToMicro(finalized.AssessedAt))
	if err != nil {
		return err
	}
	existing, err := s.getResourceAssessment(ctx, finalized.Coordinate, finalized.ContentDigest, finalized.AssessmentDigest)
	if err != nil {
		return err
	}
	if !existing.Equal(finalized) {
		return fmt.Errorf("%w: resource assessment %q already has different immutable metadata", store.ErrConflict, finalized.AssessmentDigest)
	}
	return nil
}

func (s *Store) SetCurrentResourceAssessment(ctx context.Context, assessment store.ResourceAssessment) error {
	return s.withTx(ctx, func(tx *Store) error { return tx.setCurrentResourceAssessment(ctx, assessment) })
}

func (s *Store) setCurrentResourceAssessment(ctx context.Context, assessment store.ResourceAssessment) error {
	finalized, err := store.FinalizeResourceAssessment(assessment)
	if err != nil {
		return err
	}
	existing, err := s.getResourceAssessment(ctx, finalized.Coordinate, finalized.ContentDigest, finalized.AssessmentDigest)
	if err != nil {
		return err
	}
	if !existing.Equal(finalized) {
		return fmt.Errorf("%w: current assessment does not match stored assessment %q", store.ErrConflict, finalized.AssessmentDigest)
	}
	_, err = s.q.ExecContext(ctx, `INSERT INTO current_resource_assessments
		(marketplace_id, registry_id, kind, name, content_digest, assessment_digest)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(marketplace_id, registry_id, kind, name, content_digest)
		DO UPDATE SET assessment_digest=excluded.assessment_digest`,
		finalized.Coordinate.Marketplace, finalized.Coordinate.Registry, finalized.Coordinate.Kind, finalized.Coordinate.Name,
		finalized.ContentDigest, finalized.AssessmentDigest)
	if err != nil {
		return err
	}
	return s.advanceCatalogVersion(ctx)
}

func (s *Store) GetResourceAssessment(ctx context.Context, digest store.AssessmentDigest) (store.ResourceAssessment, error) {
	return scanResourceAssessment(s.q.QueryRowContext(ctx, `SELECT marketplace_id, registry_id, kind, name, content_digest,
		assessment_digest, analyzer_version, policy_digest, decision, findings, inspection_manifest_digest, assessed_at
		FROM resource_assessments WHERE assessment_digest=? LIMIT 1`, digest))
}

func (s *Store) getResourceAssessment(ctx context.Context, coord store.ResourceCoordinate, digest store.RevisionDigest, assessmentDigest store.AssessmentDigest) (store.ResourceAssessment, error) {
	return scanResourceAssessment(s.q.QueryRowContext(ctx, `SELECT marketplace_id, registry_id, kind, name, content_digest,
		assessment_digest, analyzer_version, policy_digest, decision, findings, inspection_manifest_digest, assessed_at
		FROM resource_assessments WHERE marketplace_id=? AND registry_id=? AND kind=? AND name=? AND content_digest=? AND assessment_digest=?`,
		coord.Marketplace, coord.Registry, coord.Kind, coord.Name, digest, assessmentDigest))
}

func scanResourceAssessment(sc rowScanner) (store.ResourceAssessment, error) {
	var assessment store.ResourceAssessment
	var findings string
	var assessedAt int64
	err := sc.Scan(&assessment.Coordinate.Marketplace, &assessment.Coordinate.Registry, &assessment.Coordinate.Kind,
		&assessment.Coordinate.Name, &assessment.ContentDigest, &assessment.AssessmentDigest,
		&assessment.AnalyzerVersion, &assessment.PolicyDigest, &assessment.Decision, &findings,
		&assessment.InspectionManifestDigest, &assessedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return store.ResourceAssessment{}, store.ErrNotFound
	}
	if err != nil {
		return store.ResourceAssessment{}, err
	}
	assessment.AssessedAt = microToTime(assessedAt)
	if err := unmarshalJSON([]byte(findings), &assessment.Findings); err != nil {
		return store.ResourceAssessment{}, fmt.Errorf("unmarshal assessment findings: %w", err)
	}
	return assessment, nil
}

func (s *Store) ListResourceAssessments(ctx context.Context, coord store.ResourceCoordinate, digest store.RevisionDigest) ([]store.ResourceAssessment, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT marketplace_id, registry_id, kind, name, content_digest,
		assessment_digest, analyzer_version, policy_digest, decision, findings, inspection_manifest_digest, assessed_at
		FROM resource_assessments WHERE marketplace_id=? AND registry_id=? AND kind=? AND name=? AND content_digest=?
		ORDER BY assessed_at DESC, assessment_digest DESC`, coord.Marketplace, coord.Registry, coord.Kind, coord.Name, digest)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]store.ResourceAssessment, 0)
	for rows.Next() {
		assessment, err := scanResourceAssessment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, assessment)
	}
	return out, rows.Err()
}

func (s *Store) collectAssessedResources(rows *sql.Rows) ([]store.AssessedResourceRevision, error) {
	out := make([]store.AssessedResourceRevision, 0)
	for rows.Next() {
		rev, err := s.scanAssessedResource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rev)
	}
	return out, rows.Err()
}

func (s *Store) ListResourceRevisions(ctx context.Context, coord store.ResourceCoordinate) ([]store.AssessedResourceRevision, error) {
	query := `SELECT ` + assessedResourceColumns + ` FROM resource_revisions rr ` + assessmentJoins + latestOccurrenceJoins + revocationJoins + `
		WHERE rr.marketplace_id=? AND rr.registry_id=? AND rr.kind=? AND rr.name=?
		ORDER BY so.occurred_at DESC, rr.content_digest DESC`
	return s.queryAssessedResources(ctx, query, coord.Marketplace, coord.Registry, coord.Kind, coord.Name)
}

func (s *Store) ListResourceRevisionsByArchiveDigest(ctx context.Context, marketplace store.MarketplaceID, digest store.ArchiveDigest) ([]store.AssessedResourceRevision, error) {
	query := `SELECT ` + assessedResourceColumns + ` FROM resource_revisions rr ` + assessmentJoins + latestOccurrenceJoins + revocationJoins + `
		WHERE rr.marketplace_id=? AND (rr.archive_digest=? OR EXISTS (
			SELECT 1 FROM blob_resource_owners bo
			WHERE bo.blob_kind='archive' AND bo.blob_digest=?
			  AND bo.marketplace_id=rr.marketplace_id AND bo.registry_id=rr.registry_id
			  AND bo.resource_kind=rr.kind AND bo.name=rr.name AND bo.content_digest=rr.content_digest))
		ORDER BY rr.registry_id, rr.kind, rr.name, so.occurred_at DESC, rr.content_digest DESC`
	return s.queryAssessedResources(ctx, query, marketplace, digest, digest)
}

func (s *Store) ListResourceRevisionsByContentDigest(ctx context.Context, marketplace store.MarketplaceID, digest store.RevisionDigest) ([]store.AssessedResourceRevision, error) {
	query := `SELECT ` + assessedResourceColumns + ` FROM resource_revisions rr ` + assessmentJoins + latestOccurrenceJoins + revocationJoins + `
		WHERE rr.marketplace_id=? AND rr.content_digest=?
		ORDER BY rr.registry_id, rr.kind, rr.name`
	return s.queryAssessedResources(ctx, query, marketplace, digest)
}

func (s *Store) GetLatestResourceRevision(ctx context.Context, coord store.ResourceCoordinate) (store.AssessedResourceRevision, error) {
	query := `SELECT ` + assessedResourceColumns + ` FROM resource_revisions rr ` + assessmentJoins + headOccurrenceJoins + revocationJoins + `
		WHERE rr.marketplace_id=? AND rr.registry_id=? AND rr.kind=? AND rr.name=?
		ORDER BY rr.content_digest DESC LIMIT 1`
	return s.scanAssessedResource(s.q.QueryRowContext(ctx, query, coord.Marketplace, coord.Registry, coord.Kind, coord.Name))
}

func (s *Store) SearchResourceRevisions(ctx context.Context, filter store.ResourceSearchFilter) ([]store.AssessedResourceRevision, error) {
	args := []any{filter.Marketplace}
	where := "rr.marketplace_id=? AND r.listed=1"
	registryValues := filter.RegistryValues()
	registryClauses := make([]string, 0, len(registryValues))
	for _, registry := range registryValues {
		if registry == "" {
			continue
		}
		registryClauses = append(registryClauses, "(rr.registry_id=? OR r.slug=?)")
		args = append(args, registry, registry)
	}
	if len(registryClauses) > 0 {
		where += " AND (" + strings.Join(registryClauses, " OR ") + ")"
	}
	if filter.Kind != "" {
		where += " AND rr.kind=?"
		args = append(args, filter.Kind)
	}
	if filter.Query != "" {
		where += " AND instr(lower(rr.name), lower(?)) > 0"
		args = append(args, filter.Query)
	}
	query := `SELECT ` + assessedResourceColumns + ` FROM resource_revisions rr ` + assessmentJoins + headOccurrenceJoins + `
		JOIN registries r ON r.id=rr.registry_id ` + revocationJoins + ` WHERE ` + where + `
		ORDER BY rr.registry_id, rr.kind, rr.name, rr.content_digest DESC`
	return s.queryAssessedResources(ctx, query, args...)
}

func (s *Store) queryAssessedResources(ctx context.Context, query string, args ...any) ([]store.AssessedResourceRevision, error) {
	rows, err := s.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return s.collectAssessedResources(rows)
}
