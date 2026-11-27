package store

import (
	"context"
	"fmt"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
)

// SelectCurrentAssessment updates the current pointer without changing a profile's inspection address.
func SelectCurrentAssessment(ctx context.Context, q ManagedSQLQueries, a ResourceAssessment) error {
	const storedAssessment = `FROM resource_assessments
		WHERE marketplace_id=$1 AND registry_id=$2 AND kind=$3 AND name=$4
		  AND content_digest=$5 AND assessment_digest=$6`
	query := `INSERT INTO current_resource_assessments
		(marketplace_id, registry_id, kind, name, content_digest, assessment_digest)
		SELECT marketplace_id, registry_id, kind, name, content_digest, assessment_digest
		` + storedAssessment + `
		ON CONFLICT (marketplace_id, registry_id, kind, name, content_digest) DO UPDATE
		SET assessment_digest=excluded.assessment_digest`
	args := []any{a.Coordinate.Marketplace, a.Coordinate.Registry, a.Coordinate.Kind, a.Coordinate.Name, a.ContentDigest, a.AssessmentDigest}
	profile := a.Coordinate.Kind == nameutil.TargetProfileKind
	if profile {
		query += ` WHERE (SELECT inspection_manifest_digest FROM resource_assessments
			WHERE assessment_digest=current_resource_assessments.assessment_digest)=$7`
		args = append(args, a.InspectionManifestDigest)
	}
	res, err := q.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil || n != 0 {
		return err
	}
	if profile {
		var stored bool
		if err := q.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 `+storedAssessment+`)`, args[:6]...).Scan(&stored); err != nil {
			return err
		}
		if stored {
			return fmt.Errorf("%w: target profile %s@%s already has a different inspection manifest", ErrConflict, a.Coordinate, a.ContentDigest)
		}
	}
	return ErrNotFound
}
