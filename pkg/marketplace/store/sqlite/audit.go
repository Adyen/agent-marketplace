package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

const resourceAuditColumns = `
	ra.marketplace_id, ra.registry_id, ra.kind, ra.name, ra.subject_digest,
	ra.analyzer_id, ra.analyzer_version, ra.config_digest, ra.result_digest,
	ra.role, ra.verdict, ra.summary, ra.findings, ra.duration_ns, ra.error, ra.assessed_at`

func (s *Store) PutResourceAudit(ctx context.Context, audit store.ResourceAudit) error {
	finalized, err := store.FinalizeResourceAudit(audit)
	if err != nil {
		return err
	}
	findings, err := marshalJSON(finalized.Findings)
	if err != nil {
		return err
	}
	execRes, err := s.q.ExecContext(ctx, `INSERT OR IGNORE INTO resource_audits
		(marketplace_id, registry_id, kind, name, subject_digest, analyzer_id, analyzer_version, config_digest,
		 result_digest, role, verdict, summary, findings, duration_ns, error, assessed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		finalized.Coordinate.Marketplace, finalized.Coordinate.Registry, finalized.Coordinate.Kind, finalized.Coordinate.Name,
		finalized.SubjectDigest, finalized.AnalyzerID, finalized.AnalyzerVersion, finalized.ConfigDigest,
		finalized.ResultDigest, finalized.Role, finalized.Verdict, finalized.Summary, findings,
		finalized.Duration.Nanoseconds(), finalized.Error, timeToMicro(finalized.AssessedAt))
	if err != nil {
		return err
	}
	if n, _ := execRes.RowsAffected(); n == 0 {
		exact, exactErr := s.getResourceAudit(ctx, finalized.ResourceAuditKey, finalized.ResultDigest)
		if exactErr == nil {
			if exact.Equal(finalized) {
				return nil
			}
			return fmt.Errorf("%w: resource audit already has a different result for the logical key", store.ErrConflict)
		}
		if !errors.Is(exactErr, store.ErrNotFound) {
			return exactErr
		}
		// The exact row is not present, so a non-error uniqueness conflict stopped the insert.
		cached, cachedErr := s.GetCachedResourceAudit(ctx, finalized.ResourceAuditKey)
		if cachedErr == nil {
			if cached.Equal(finalized) {
				return nil
			}
			return fmt.Errorf("%w: resource audit already has a different non-error result for the logical key", store.ErrConflict)
		}
		if !errors.Is(cachedErr, store.ErrNotFound) {
			return cachedErr
		}
		return exactErr
	}
	existing, err := s.getResourceAudit(ctx, finalized.ResourceAuditKey, finalized.ResultDigest)
	if err != nil {
		return err
	}
	if !existing.Equal(finalized) {
		return fmt.Errorf("%w: resource audit already has a different result for the logical key", store.ErrConflict)
	}
	return nil
}

func (s *Store) GetResourceAudit(ctx context.Context, key store.ResourceAuditKey, resultDigest store.AuditDigest) (store.ResourceAudit, error) {
	if err := key.Validate(); err != nil {
		return store.ResourceAudit{}, err
	}
	if err := store.ValidateSHA256Digest("result digest", string(resultDigest)); err != nil {
		return store.ResourceAudit{}, err
	}
	return s.getResourceAudit(ctx, key, resultDigest)
}

func (s *Store) getResourceAudit(ctx context.Context, key store.ResourceAuditKey, resultDigest store.AuditDigest) (store.ResourceAudit, error) {
	return scanResourceAudit(s.q.QueryRowContext(ctx, `SELECT`+resourceAuditColumns+`
		FROM resource_audits ra
		WHERE ra.marketplace_id=? AND ra.registry_id=? AND ra.kind=? AND ra.name=? AND ra.subject_digest=?
		  AND ra.analyzer_id=? AND ra.analyzer_version=? AND ra.config_digest=? AND ra.result_digest=?`,
		key.Coordinate.Marketplace, key.Coordinate.Registry, key.Coordinate.Kind, key.Coordinate.Name,
		key.SubjectDigest, key.AnalyzerID, key.AnalyzerVersion, key.ConfigDigest, resultDigest))
}

func (s *Store) GetCachedResourceAudit(ctx context.Context, key store.ResourceAuditKey) (store.ResourceAudit, error) {
	if err := key.Validate(); err != nil {
		return store.ResourceAudit{}, err
	}
	row := s.q.QueryRowContext(ctx, `SELECT`+resourceAuditColumns+`
		FROM resource_audits ra
		WHERE ra.marketplace_id=? AND ra.registry_id=? AND ra.kind=? AND ra.name=? AND ra.subject_digest=?
		  AND ra.analyzer_id=? AND ra.analyzer_version=? AND ra.config_digest=?
		  AND ra.verdict <> 'error'`,
		key.Coordinate.Marketplace, key.Coordinate.Registry, key.Coordinate.Kind, key.Coordinate.Name,
		key.SubjectDigest, key.AnalyzerID, key.AnalyzerVersion, key.ConfigDigest)
	audit, err := scanResourceAudit(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.ResourceAudit{}, store.ErrNotFound
		}
		return store.ResourceAudit{}, err
	}
	return audit, nil
}

func (s *Store) LinkResourceAssessmentAudit(ctx context.Context, link store.AssessmentAuditLink) error {
	if err := link.Validate(); err != nil {
		return err
	}
	assessment, err := s.GetResourceAssessment(ctx, link.AssessmentDigest)
	if err != nil {
		return err
	}
	if _, err := s.GetResourceAudit(ctx, link.AuditKey, link.ResultDigest); err != nil {
		return err
	}
	if assessment.Coordinate != link.AuditKey.Coordinate {
		return fmt.Errorf("%w: assessment coordinate does not match linked audit coordinate", store.ErrConflict)
	}
	execRes, err := s.q.ExecContext(ctx, `INSERT OR IGNORE INTO assessment_audits
		(assessment_digest, ordinal, marketplace_id, registry_id, kind, name, subject_digest,
		 analyzer_id, analyzer_version, config_digest, result_digest)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		link.AssessmentDigest, link.Ordinal,
		link.AuditKey.Coordinate.Marketplace, link.AuditKey.Coordinate.Registry, link.AuditKey.Coordinate.Kind, link.AuditKey.Coordinate.Name,
		link.AuditKey.SubjectDigest, link.AuditKey.AnalyzerID, link.AuditKey.AnalyzerVersion, link.AuditKey.ConfigDigest,
		link.ResultDigest)
	if err != nil {
		return err
	}
	if n, _ := execRes.RowsAffected(); n == 0 {
		existing, byOrdinalErr := s.getAssessmentAuditLink(ctx, link.AssessmentDigest, link.Ordinal)
		if byOrdinalErr == nil {
			if existing.Equal(link) {
				return nil
			}
			return fmt.Errorf("%w: assessment audit link ordinal already used", store.ErrConflict)
		}
		if !errors.Is(byOrdinalErr, store.ErrNotFound) {
			return byOrdinalErr
		}
		// The ordinal is free, so the unique (assessment, analyzer) constraint fired.
		byAnalyzer, byAnalyzerErr := s.getAssessmentAuditLinkByAnalyzer(ctx, link.AssessmentDigest, link.AuditKey.AnalyzerID)
		if byAnalyzerErr == nil {
			if byAnalyzer.Equal(link) {
				return nil
			}
			return fmt.Errorf("%w: assessment audit link analyzer already used", store.ErrConflict)
		}
		if !errors.Is(byAnalyzerErr, store.ErrNotFound) {
			return byAnalyzerErr
		}
		return byOrdinalErr
	}
	existing, err := s.getAssessmentAuditLink(ctx, link.AssessmentDigest, link.Ordinal)
	if err != nil {
		return err
	}
	if !existing.Equal(link) {
		return fmt.Errorf("%w: assessment audit link already has different contents", store.ErrConflict)
	}
	return nil
}

func (s *Store) getAssessmentAuditLinkByAnalyzer(ctx context.Context, digest store.AssessmentDigest, analyzerID string) (store.AssessmentAuditLink, error) {
	var link store.AssessmentAuditLink
	err := s.q.QueryRowContext(ctx, `SELECT assessment_digest, ordinal, marketplace_id, registry_id, kind, name, subject_digest,
		analyzer_id, analyzer_version, config_digest, result_digest
		FROM assessment_audits
		WHERE assessment_digest=? AND analyzer_id=?`, digest, analyzerID).Scan(
		&link.AssessmentDigest, &link.Ordinal,
		&link.AuditKey.Coordinate.Marketplace, &link.AuditKey.Coordinate.Registry, &link.AuditKey.Coordinate.Kind, &link.AuditKey.Coordinate.Name,
		&link.AuditKey.SubjectDigest, &link.AuditKey.AnalyzerID, &link.AuditKey.AnalyzerVersion, &link.AuditKey.ConfigDigest,
		&link.ResultDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return store.AssessmentAuditLink{}, store.ErrNotFound
	}
	if err != nil {
		return store.AssessmentAuditLink{}, err
	}
	return link, nil
}

func (s *Store) getAssessmentAuditLink(ctx context.Context, digest store.AssessmentDigest, ordinal int) (store.AssessmentAuditLink, error) {
	var link store.AssessmentAuditLink
	err := s.q.QueryRowContext(ctx, `SELECT assessment_digest, ordinal, marketplace_id, registry_id, kind, name, subject_digest,
		analyzer_id, analyzer_version, config_digest, result_digest
		FROM assessment_audits
		WHERE assessment_digest=? AND ordinal=?`, digest, ordinal).Scan(
		&link.AssessmentDigest, &link.Ordinal,
		&link.AuditKey.Coordinate.Marketplace, &link.AuditKey.Coordinate.Registry, &link.AuditKey.Coordinate.Kind, &link.AuditKey.Coordinate.Name,
		&link.AuditKey.SubjectDigest, &link.AuditKey.AnalyzerID, &link.AuditKey.AnalyzerVersion, &link.AuditKey.ConfigDigest,
		&link.ResultDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return store.AssessmentAuditLink{}, store.ErrNotFound
	}
	if err != nil {
		return store.AssessmentAuditLink{}, err
	}
	return link, nil
}

func (s *Store) ListResourceAuditsForAssessment(ctx context.Context, digest store.AssessmentDigest) ([]store.ResourceAudit, error) {
	if err := store.ValidateSHA256Digest("assessment digest", string(digest)); err != nil {
		return nil, err
	}
	rows, err := s.q.QueryContext(ctx, `SELECT`+resourceAuditColumns+`
		FROM resource_audits ra
		JOIN assessment_audits aa
		  ON aa.marketplace_id=ra.marketplace_id AND aa.registry_id=ra.registry_id
		 AND aa.kind=ra.kind AND aa.name=ra.name AND aa.subject_digest=ra.subject_digest
		 AND aa.analyzer_id=ra.analyzer_id AND aa.analyzer_version=ra.analyzer_version
		 AND aa.config_digest=ra.config_digest AND aa.result_digest=ra.result_digest
		WHERE aa.assessment_digest=?
		ORDER BY aa.ordinal`, digest)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]store.ResourceAudit, 0)
	for rows.Next() {
		audit, err := scanResourceAudit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, audit)
	}
	return out, rows.Err()
}

func (s *Store) GetResourceAuditForAssessment(ctx context.Context, digest store.AssessmentDigest, analyzerID string) (store.ResourceAudit, error) {
	if err := store.ValidateSHA256Digest("assessment digest", string(digest)); err != nil {
		return store.ResourceAudit{}, err
	}
	if err := store.ValidateAnalyzerIdentifier(analyzerID); err != nil {
		return store.ResourceAudit{}, err
	}
	row := s.q.QueryRowContext(ctx, `SELECT`+resourceAuditColumns+`
		FROM resource_audits ra
		JOIN assessment_audits aa
		  ON aa.marketplace_id=ra.marketplace_id AND aa.registry_id=ra.registry_id
		 AND aa.kind=ra.kind AND aa.name=ra.name AND aa.subject_digest=ra.subject_digest
		 AND aa.analyzer_id=ra.analyzer_id AND aa.analyzer_version=ra.analyzer_version
		 AND aa.config_digest=ra.config_digest AND aa.result_digest=ra.result_digest
		WHERE aa.assessment_digest=? AND aa.analyzer_id=?`, digest, analyzerID)
	return scanResourceAudit(row)
}

func scanResourceAudit(sc rowScanner) (store.ResourceAudit, error) {
	var audit store.ResourceAudit
	var findings string
	var duration int64
	var assessedAt int64
	err := sc.Scan(
		&audit.Coordinate.Marketplace, &audit.Coordinate.Registry, &audit.Coordinate.Kind, &audit.Coordinate.Name,
		&audit.SubjectDigest, &audit.AnalyzerID, &audit.AnalyzerVersion, &audit.ConfigDigest, &audit.ResultDigest,
		&audit.Role, &audit.Verdict, &audit.Summary, &findings, &duration, &audit.Error, &assessedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return store.ResourceAudit{}, store.ErrNotFound
	}
	if err != nil {
		return store.ResourceAudit{}, err
	}
	audit.Duration = time.Duration(duration)
	audit.AssessedAt = microToTime(assessedAt)
	if err := unmarshalJSON([]byte(findings), &audit.Findings); err != nil {
		return store.ResourceAudit{}, fmt.Errorf("unmarshal audit findings: %w", err)
	}
	if err := audit.Validate(); err != nil {
		return store.ResourceAudit{}, fmt.Errorf("validate resource audit: %w", err)
	}
	return audit, nil
}
