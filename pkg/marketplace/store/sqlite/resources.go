package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type rowScanner interface {
	Scan(dest ...any) error
}

const assessedResourceColumns = `
	rr.marketplace_id, rr.source_id, rr.kind, rr.name, rr.content_digest,
	rr.archive_digest, rr.license, rr.description,
	ra.assessment_digest, ra.analyzer_version, ra.policy_digest, ra.decision,
	ra.findings, ra.inspection_manifest_digest, ra.assessed_at,
	ro.revision_commit, ro.manifest_digest,
	rv.kind, rv.target_id, rv.reason, rv.applied_at,
	rc.kind, rc.target_id, rc.reason, rc.applied_at,
	rs.kind, rs.target_id, rs.reason, rs.applied_at`

const assessmentJoins = `
	JOIN current_resource_assessments ca
	  ON ca.marketplace_id=rr.marketplace_id AND ca.source_id=rr.source_id
	 AND ca.kind=rr.kind AND ca.name=rr.name AND ca.content_digest=rr.content_digest
	JOIN resource_assessments ra
	  ON ra.marketplace_id=ca.marketplace_id AND ra.source_id=ca.source_id
	 AND ra.kind=ca.kind AND ra.name=ca.name AND ra.content_digest=ca.content_digest
	 AND ra.assessment_digest=ca.assessment_digest`

const revocationJoins = `
	LEFT JOIN revocations rv
	  ON rv.kind='revision' AND rv.target_id=(rr.marketplace_id || '/' || rr.source_id || '/' || rr.kind || '/' || rr.name || '@' || rr.content_digest)
	LEFT JOIN revocations rc
	  ON rc.kind='coordinate' AND rc.target_id=(rr.marketplace_id || '/' || rr.source_id || '/' || rr.kind || '/' || rr.name)
	LEFT JOIN revocations rs
	  ON rs.kind='source' AND rs.target_id=rr.source_id`

// latestOccurrenceJoins gives historical/addressed reads one deterministic
// occurrence per revision. Discovery reads use the source head instead.
const latestOccurrenceJoins = `
	JOIN resource_occurrences ro
	  ON ro.marketplace_id=rr.marketplace_id AND ro.source_id=rr.source_id
	 AND ro.kind=rr.kind AND ro.name=rr.name AND ro.content_digest=rr.content_digest
	JOIN source_occurrences so
	  ON so.source_id=ro.source_id AND so.revision_commit=ro.revision_commit
	 AND ro.revision_commit = (
		SELECT ro2.revision_commit
		FROM resource_occurrences ro2
		JOIN source_occurrences so2
		  ON so2.source_id=ro2.source_id AND so2.revision_commit=ro2.revision_commit
		WHERE ro2.marketplace_id=rr.marketplace_id AND ro2.source_id=rr.source_id
		  AND ro2.kind=rr.kind AND ro2.name=rr.name AND ro2.content_digest=rr.content_digest
		ORDER BY so2.occurred_at DESC, ro2.revision_commit DESC
		LIMIT 1
	)`

const headOccurrenceJoins = `
	JOIN source_heads sh ON sh.source_id=rr.source_id
	JOIN resource_occurrences ro
	  ON ro.marketplace_id=rr.marketplace_id AND ro.source_id=rr.source_id
	 AND ro.kind=rr.kind AND ro.name=rr.name AND ro.content_digest=rr.content_digest
	 AND ro.revision_commit=sh.revision_commit
	JOIN source_occurrences so
	  ON so.source_id=sh.source_id AND so.revision_commit=sh.revision_commit`

func (s *Store) GetResourceRevision(ctx context.Context, coord store.ResourceCoordinate, digest store.RevisionDigest) (store.AssessedResourceRevision, error) {
	query := `SELECT ` + assessedResourceColumns + ` FROM resource_revisions rr ` + assessmentJoins + latestOccurrenceJoins + revocationJoins + `
		WHERE rr.marketplace_id=? AND rr.source_id=? AND rr.kind=? AND rr.name=? AND rr.content_digest=?`
	return s.scanAssessedResource(s.q.QueryRowContext(ctx, query,
		coord.Marketplace, coord.Source, coord.Kind, coord.Name, digest))
}

func (s *Store) scanAssessedResource(sc rowScanner) (store.AssessedResourceRevision, error) {
	var out store.AssessedResourceRevision
	var archive, license sql.NullString
	var findings string
	var assessedAt int64
	var revKind, revTarget, revReason sql.NullString
	var revAt sql.NullInt64
	var coordKind, coordTarget, coordReason sql.NullString
	var coordAt sql.NullInt64
	var sourceKind, sourceTarget, sourceReason sql.NullString
	var sourceAt sql.NullInt64

	err := sc.Scan(
		&out.Coordinate.Marketplace, &out.Coordinate.Source, &out.Coordinate.Kind, &out.Coordinate.Name,
		&out.ContentDigest, &archive, &license, &out.Description,
		&out.Assessment.AssessmentDigest, &out.Assessment.AnalyzerVersion, &out.Assessment.PolicyDigest,
		&out.Assessment.Decision, &findings, &out.Assessment.InspectionManifestDigest, &assessedAt,
		&out.Occurrence.Commit, &out.Occurrence.ManifestDigest,
		&revKind, &revTarget, &revReason, &revAt,
		&coordKind, &coordTarget, &coordReason, &coordAt,
		&sourceKind, &sourceTarget, &sourceReason, &sourceAt,
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
	for _, candidate := range []struct {
		kind, target, reason sql.NullString
		at                   sql.NullInt64
	}{
		{revKind, revTarget, revReason, revAt},
		{coordKind, coordTarget, coordReason, coordAt},
		{sourceKind, sourceTarget, sourceReason, sourceAt},
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
	rev = normalizedRevision(rev)
	_, err := s.q.ExecContext(ctx, `INSERT OR IGNORE INTO resource_revisions
		(marketplace_id, source_id, kind, name, content_digest, archive_digest, license, description)
		VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?)`,
		rev.Coordinate.Marketplace, rev.Coordinate.Source, rev.Coordinate.Kind, rev.Coordinate.Name,
		rev.ContentDigest, rev.ArchiveDigest, rev.License, rev.Description)
	if err != nil {
		return err
	}
	existing, err := s.getIntrinsicResourceRevision(ctx, rev.Coordinate, rev.ContentDigest)
	if err != nil {
		return err
	}
	if existing != rev {
		return fmt.Errorf("%w: resource revision %q already has different immutable metadata", store.ErrConflict, rev.ContentDigest)
	}
	return nil
}

func (s *Store) getIntrinsicResourceRevision(ctx context.Context, coord store.ResourceCoordinate, digest store.RevisionDigest) (store.ResourceRevision, error) {
	var rev store.ResourceRevision
	var archive, license sql.NullString
	err := s.q.QueryRowContext(ctx, `SELECT marketplace_id, source_id, kind, name, content_digest, archive_digest, license, description
		FROM resource_revisions WHERE marketplace_id=? AND source_id=? AND kind=? AND name=? AND content_digest=?`,
		coord.Marketplace, coord.Source, coord.Kind, coord.Name, digest,
	).Scan(&rev.Coordinate.Marketplace, &rev.Coordinate.Source, &rev.Coordinate.Kind, &rev.Coordinate.Name,
		&rev.ContentDigest, &archive, &license, &rev.Description)
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
	_, err := s.q.ExecContext(ctx, `INSERT OR IGNORE INTO resource_occurrences
		(marketplace_id, source_id, kind, name, content_digest, revision_commit, manifest_digest)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		occ.Coordinate.Marketplace, occ.Coordinate.Source, occ.Coordinate.Kind, occ.Coordinate.Name,
		occ.ContentDigest, occ.Commit, occ.ManifestDigest)
	if err != nil {
		return err
	}
	var manifest store.RevisionDigest
	err = s.q.QueryRowContext(ctx, `SELECT manifest_digest FROM resource_occurrences
		WHERE marketplace_id=? AND source_id=? AND kind=? AND name=? AND content_digest=? AND revision_commit=?`,
		occ.Coordinate.Marketplace, occ.Coordinate.Source, occ.Coordinate.Kind, occ.Coordinate.Name,
		occ.ContentDigest, occ.Commit).Scan(&manifest)
	if err != nil {
		return err
	}
	if manifest != occ.ManifestDigest {
		return fmt.Errorf("%w: resource occurrence for commit %q already has a different manifest", store.ErrConflict, occ.Commit)
	}
	return nil
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
		(marketplace_id, source_id, kind, name, content_digest, assessment_digest,
		 analyzer_version, policy_digest, decision, findings, inspection_manifest_digest, assessed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		finalized.Coordinate.Marketplace, finalized.Coordinate.Source, finalized.Coordinate.Kind, finalized.Coordinate.Name,
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
		(marketplace_id, source_id, kind, name, content_digest, assessment_digest)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(marketplace_id, source_id, kind, name, content_digest)
		DO UPDATE SET assessment_digest=excluded.assessment_digest`,
		finalized.Coordinate.Marketplace, finalized.Coordinate.Source, finalized.Coordinate.Kind, finalized.Coordinate.Name,
		finalized.ContentDigest, finalized.AssessmentDigest)
	return err
}

func (s *Store) GetResourceAssessment(ctx context.Context, digest store.AssessmentDigest) (store.ResourceAssessment, error) {
	return scanResourceAssessment(s.q.QueryRowContext(ctx, `SELECT marketplace_id, source_id, kind, name, content_digest,
		assessment_digest, analyzer_version, policy_digest, decision, findings, inspection_manifest_digest, assessed_at
		FROM resource_assessments WHERE assessment_digest=? LIMIT 1`, digest))
}

func (s *Store) getResourceAssessment(ctx context.Context, coord store.ResourceCoordinate, digest store.RevisionDigest, assessmentDigest store.AssessmentDigest) (store.ResourceAssessment, error) {
	return scanResourceAssessment(s.q.QueryRowContext(ctx, `SELECT marketplace_id, source_id, kind, name, content_digest,
		assessment_digest, analyzer_version, policy_digest, decision, findings, inspection_manifest_digest, assessed_at
		FROM resource_assessments WHERE marketplace_id=? AND source_id=? AND kind=? AND name=? AND content_digest=? AND assessment_digest=?`,
		coord.Marketplace, coord.Source, coord.Kind, coord.Name, digest, assessmentDigest))
}

func scanResourceAssessment(sc rowScanner) (store.ResourceAssessment, error) {
	var assessment store.ResourceAssessment
	var findings string
	var assessedAt int64
	err := sc.Scan(&assessment.Coordinate.Marketplace, &assessment.Coordinate.Source, &assessment.Coordinate.Kind,
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
	rows, err := s.q.QueryContext(ctx, `SELECT marketplace_id, source_id, kind, name, content_digest,
		assessment_digest, analyzer_version, policy_digest, decision, findings, inspection_manifest_digest, assessed_at
		FROM resource_assessments WHERE marketplace_id=? AND source_id=? AND kind=? AND name=? AND content_digest=?
		ORDER BY assessed_at DESC, assessment_digest DESC`, coord.Marketplace, coord.Source, coord.Kind, coord.Name, digest)
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
		WHERE rr.marketplace_id=? AND rr.source_id=? AND rr.kind=? AND rr.name=?
		ORDER BY so.occurred_at DESC, rr.content_digest DESC`
	return s.queryAssessedResources(ctx, query, coord.Marketplace, coord.Source, coord.Kind, coord.Name)
}

func (s *Store) ListResourceRevisionsByArchiveDigest(ctx context.Context, marketplace store.MarketplaceID, digest store.ArchiveDigest) ([]store.AssessedResourceRevision, error) {
	query := `SELECT ` + assessedResourceColumns + ` FROM resource_revisions rr ` + assessmentJoins + latestOccurrenceJoins + revocationJoins + `
		WHERE rr.marketplace_id=? AND rr.archive_digest=?
		ORDER BY rr.source_id, rr.kind, rr.name, so.occurred_at DESC, rr.content_digest DESC`
	return s.queryAssessedResources(ctx, query, marketplace, digest)
}

func (s *Store) ListResourceRevisionsByContentDigest(ctx context.Context, marketplace store.MarketplaceID, digest store.RevisionDigest) ([]store.AssessedResourceRevision, error) {
	query := `SELECT ` + assessedResourceColumns + ` FROM resource_revisions rr ` + assessmentJoins + latestOccurrenceJoins + revocationJoins + `
		WHERE rr.marketplace_id=? AND rr.content_digest=?
		ORDER BY rr.source_id, rr.kind, rr.name`
	return s.queryAssessedResources(ctx, query, marketplace, digest)
}

func (s *Store) GetLatestResourceRevision(ctx context.Context, coord store.ResourceCoordinate) (store.AssessedResourceRevision, error) {
	query := `SELECT ` + assessedResourceColumns + ` FROM resource_revisions rr ` + assessmentJoins + headOccurrenceJoins + revocationJoins + `
		WHERE rr.marketplace_id=? AND rr.source_id=? AND rr.kind=? AND rr.name=?
		ORDER BY rr.content_digest DESC LIMIT 1`
	return s.scanAssessedResource(s.q.QueryRowContext(ctx, query, coord.Marketplace, coord.Source, coord.Kind, coord.Name))
}

func (s *Store) SearchResourceRevisions(ctx context.Context, filter store.ResourceSearchFilter) ([]store.AssessedResourceRevision, error) {
	args := []any{filter.Marketplace}
	where := "rr.marketplace_id=? AND s.listed=1"
	if filter.Source != "" {
		where += " AND (rr.source_id=? OR s.slug=?)"
		args = append(args, filter.Source, filter.Source)
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
		JOIN sources s ON s.id=rr.source_id ` + revocationJoins + ` WHERE ` + where + `
		ORDER BY rr.source_id, rr.kind, rr.name, rr.content_digest DESC`
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
