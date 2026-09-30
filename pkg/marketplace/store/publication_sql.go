package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"
)

var ErrPublicationFence = errors.New("marketplace/store: publication fence rejected")

func FenceRegistryPublication(ctx context.Context, q ManagedSQLQueries, meta MetadataStore, req RegistryRevisionPublication) (bool, error) {
	err := assertRegistryHead(ctx, q, req.Occurrence.RegistryID, req.ExpectedHeadCommit)
	if err == nil || !errors.Is(err, ErrConflict) {
		return false, err
	}
	current, readErr := legacyPublicationAlreadyCurrent(ctx, q, meta, req)
	if readErr != nil && !errors.Is(readErr, ErrNotFound) && !errors.Is(readErr, sql.ErrNoRows) {
		return false, readErr
	}
	if current {
		return true, nil
	}
	return false, err
}

func legacyPublicationAlreadyCurrent(ctx context.Context, q ManagedSQLQueries, meta MetadataStore, req RegistryRevisionPublication) (bool, error) {
	head, err := meta.LatestRegistryOccurrence(ctx, req.Occurrence.RegistryID)
	if err != nil || !head.Equal(NormalizeRegistryOccurrence(req.Occurrence)) {
		return false, err
	}
	for _, expected := range req.Resources {
		var matches bool
		err := q.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM resource_revisions WHERE marketplace_id=$1 AND registry_id=$2 AND kind=$3 AND name=$4 AND content_digest=$5 AND COALESCE(archive_digest,'')=$6 AND COALESCE(license,'')=$7 AND COALESCE(description,'')=$8)`, expected.Coordinate.Marketplace, expected.Coordinate.Registry, expected.Coordinate.Kind, expected.Coordinate.Name, expected.ContentDigest, expected.ArchiveDigest, expected.License, BoundDescription(expected.Description)).Scan(&matches)
		if err != nil || !matches {
			return false, err
		}
	}
	for _, expected := range req.ResourceOccurrences {
		expected, err := NormalizeResourceOccurrence(expected)
		if err != nil {
			return false, err
		}
		var matches bool
		err = q.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM all_resource_occurrences WHERE marketplace_id=$1 AND registry_id=$2 AND kind=$3 AND name=$4 AND content_digest=$5 AND revision_commit=$6 AND manifest_digest=$7 AND COALESCE(source_path,'')=$8)`, expected.Coordinate.Marketplace, expected.Coordinate.Registry, expected.Coordinate.Kind, expected.Coordinate.Name, expected.ContentDigest, expected.Commit, expected.ManifestDigest, expected.SourcePath).Scan(&matches)
		if err != nil || !matches {
			return false, err
		}
	}
	for _, expected := range req.Assessments {
		var matches bool
		err := q.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM current_resource_assessments WHERE marketplace_id=$1 AND registry_id=$2 AND kind=$3 AND name=$4 AND content_digest=$5 AND assessment_digest=$6)`, expected.Coordinate.Marketplace, expected.Coordinate.Registry, expected.Coordinate.Kind, expected.Coordinate.Name, expected.ContentDigest, expected.AssessmentDigest).Scan(&matches)
		if err != nil || !matches {
			return false, err
		}
	}
	collections := slices.Clone(req.Collections)
	if req.Collection != nil {
		collections = append(collections, *req.Collection)
	}
	for _, expected := range collections {
		current, err := meta.GetCollectionRevision(ctx, expected.ID)
		if err != nil || !current.Equal(expected) {
			return false, err
		}
	}
	return true, nil
}

func assertRegistryHead(ctx context.Context, q ManagedSQLQueries, registry RegistryID, expectedCommit string) error {
	if _, err := q.ExecContext(ctx, `UPDATE registries SET id=id WHERE id=$1`, registry); err != nil {
		return fmt.Errorf("lock publication registry: %w", err)
	}
	var current string
	err := q.QueryRowContext(ctx, `SELECT revision_commit FROM effective_registry_heads WHERE registry_id=$1`, registry).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) && expectedCommit == "" {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read publication head: %w", err)
	}
	if errors.Is(err, sql.ErrNoRows) || current != expectedCommit {
		return fmt.Errorf("%w: registry head expectation changed", ErrConflict)
	}
	return nil
}

func ReadRegistryPublication(ctx context.Context, q ManagedSQLQueries, registry RegistryID, id PublicationID) (RegistryPublication, error) {
	var record string
	query := `SELECT record FROM registry_publications WHERE registry_id=$1 AND publication_id=$2`
	if id == "" {
		query = `SELECT p.record FROM registry_publications p JOIN current_registry_publications h ON h.registry_id=p.registry_id AND h.publication_id=p.publication_id WHERE p.registry_id=$1 AND $2=''`
	}
	err := q.QueryRowContext(ctx, query, registry, id).Scan(&record)
	if errors.Is(err, sql.ErrNoRows) {
		return RegistryPublication{}, ErrNotFound
	}
	if err != nil {
		return RegistryPublication{}, fmt.Errorf("read registry publication: %w", err)
	}
	var out RegistryPublication
	if json.Unmarshal([]byte(record), &out) != nil || out.ID == "" || out.Occurrence.RegistryID != registry {
		return RegistryPublication{}, ErrManagedStateCorrupt
	}
	return out, nil
}

func CommitRegistryPublication(ctx context.Context, q ManagedSQLQueries, tx MetadataStore, req RegistryRevisionPublication) error {
	if req.PublicationID == "" || req.Occurrence.RegistryID == "" || req.Occurrence.Commit == "" {
		return ErrPublicationFence
	}
	if err := validateRegistryRevisionPublication(req); err != nil {
		return err
	}
	occurrence := NormalizeRegistryOccurrence(req.Occurrence)
	req.Occurrence = occurrence
	if err := lockPublicationFence(ctx, q, req); err != nil {
		return err
	}
	result, err := q.ExecContext(ctx, `UPDATE registries SET id=id WHERE id=$1`, occurrence.RegistryID)
	if err != nil {
		return fmt.Errorf("lock publication registry: %w", err)
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrNotFound
	}
	current, err := ReadRegistryPublication(ctx, q, occurrence.RegistryID, "")
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if req.ManagedFence != nil && current.ID != req.ManagedFence.ExpectedPublicationID {
		return ErrPublicationFence
	}
	if req.ManagedFence == nil && req.ExpectedHeadCommit != "" {
		var commit string
		err := q.QueryRowContext(ctx, `SELECT revision_commit FROM effective_registry_heads WHERE registry_id=$1`, occurrence.RegistryID).Scan(&commit)
		if err != nil || commit != req.ExpectedHeadCommit {
			return ErrPublicationFence
		}
	}
	pub := RegistryPublication{ID: req.PublicationID, Occurrence: occurrence, Resources: make([]ResourceOccurrence, 0, len(req.ResourceOccurrences)), Assessments: append([]ResourceAssessment(nil), req.Assessments...)}
	if req.ManagedFence != nil {
		pub.RegistryGeneration = req.ManagedFence.RegistryGeneration
		pub.PolicyRevision = req.ManagedFence.PolicyRevision
		pub.JobID = req.ManagedFence.JobID
		pub.WorkerID = req.ManagedFence.WorkerID
		pub.Attempt = req.ManagedFence.Attempt
	}
	seen := make(map[ResourceCoordinate]bool, len(req.ResourceOccurrences))
	for _, member := range req.ResourceOccurrences {
		member, err = NormalizeResourceOccurrence(member)
		if err != nil {
			return err
		}
		if member.Coordinate.Registry != occurrence.RegistryID || member.Commit != occurrence.Commit || member.ManifestDigest != occurrence.RegistryRevisionManifestDigest || seen[member.Coordinate] {
			return ErrPublicationFence
		}
		if req.ManagedFence != nil && member.Coordinate.Marketplace != req.ManagedFence.Marketplace {
			return ErrPublicationFence
		}
		seen[member.Coordinate] = true
		if _, ok := resourceRevisionFor(req.Resources, member.Coordinate, member.ContentDigest); !ok {
			return ErrPublicationFence
		}
		found := false
		for _, assessment := range req.Assessments {
			if assessment.Coordinate == member.Coordinate && assessment.ContentDigest == member.ContentDigest {
				found = true
			}
		}
		if !found {
			return ErrPublicationFence
		}
		pub.Resources = append(pub.Resources, member)
	}
	for i, assessment := range pub.Assessments {
		if !seen[assessment.Coordinate] {
			return ErrPublicationFence
		}
		pub.Assessments[i], err = FinalizeResourceAssessment(assessment)
		if err != nil {
			return err
		}
	}
	if len(pub.Assessments) != len(pub.Resources) || len(req.Resources) != len(pub.Resources) {
		return ErrPublicationFence
	}
	sort.Slice(pub.Resources, func(i, j int) bool {
		return pub.Resources[i].Coordinate.String() < pub.Resources[j].Coordinate.String()
	})
	sort.Slice(pub.Assessments, func(i, j int) bool {
		return pub.Assessments[i].Coordinate.String() < pub.Assessments[j].Coordinate.String()
	})
	prior, err := ReadRegistryPublication(ctx, q, occurrence.RegistryID, req.PublicationID)
	if err == nil {
		pub.Occurrence.OccurredAt = prior.Occurrence.OccurredAt
		for i := range pub.Assessments {
			for _, old := range prior.Assessments {
				if old.AssessmentDigest == pub.Assessments[i].AssessmentDigest {
					pub.Assessments[i].AssessedAt = old.AssessedAt
				}
			}
		}
		a, _ := json.Marshal(pub)
		b, _ := json.Marshal(prior)
		if string(a) != string(b) {
			return ErrConflict
		}
		if current.ID != pub.ID {
			return ErrPublicationFence
		}
		for _, rev := range req.Resources {
			if err := tx.PutResourceRevision(ctx, rev); err != nil {
				return err
			}
		}
		return nil
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	pub.Occurrence.OccurredAt = time.Now().UTC()
	for _, rev := range req.Resources {
		if !seen[rev.Coordinate] {
			return ErrPublicationFence
		}
		if err := tx.PutResourceRevision(ctx, rev); err != nil {
			return err
		}
	}
	for _, assessment := range pub.Assessments {
		if err := tx.PutResourceAssessment(ctx, assessment); err != nil {
			return err
		}
		if err := tx.SetCurrentResourceAssessment(ctx, assessment); err != nil {
			return err
		}
	}
	if req.Collection != nil {
		if err := tx.PutCollectionRevision(ctx, *req.Collection); err != nil {
			return err
		}
	}
	for _, collection := range req.Collections {
		if err := tx.PutCollectionRevision(ctx, collection); err != nil {
			return err
		}
	}
	record, err := json.Marshal(pub)
	if err != nil {
		return err
	}
	roots, err := json.Marshal(occurrence.Roots)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `INSERT INTO registry_publications (registry_id,publication_id,record,ref,roots,revision_commit,manifest_digest,acquisition,mirrored_artifact_digest,source_origin,occurred_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),NULLIF($10,''),$11)`, occurrence.RegistryID, pub.ID, string(record), occurrence.Ref, string(roots), occurrence.Commit, occurrence.RegistryRevisionManifestDigest, occurrence.Acquisition, occurrence.MirroredArtifactDigest, occurrence.SourceOrigin, pub.Occurrence.OccurredAt.UnixMicro())
	if err != nil {
		return fmt.Errorf("insert registry publication: %w", err)
	}
	for _, member := range pub.Resources {
		_, err = q.ExecContext(ctx, `INSERT INTO publication_resources (registry_id,publication_id,marketplace_id,kind,name,content_digest,revision_commit,manifest_digest,source_path) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''))`, member.Coordinate.Registry, pub.ID, member.Coordinate.Marketplace, member.Coordinate.Kind, member.Coordinate.Name, member.ContentDigest, member.Commit, member.ManifestDigest, member.SourcePath)
		if err != nil {
			return fmt.Errorf("insert publication member: %w", err)
		}
	}
	if err := lockPublicationFence(ctx, q, req); err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `INSERT INTO current_registry_publications (registry_id,publication_id) VALUES ($1,$2) ON CONFLICT(registry_id) DO UPDATE SET publication_id=excluded.publication_id`, occurrence.RegistryID, pub.ID)
	if err != nil {
		return fmt.Errorf("advance publication: %w", err)
	}
	_, err = q.ExecContext(ctx, `UPDATE catalog_versions SET version=version+1 WHERE singleton=1`)
	return err
}

func lockPublicationFence(ctx context.Context, q ManagedSQLQueries, req RegistryRevisionPublication) error {
	f := req.ManagedFence
	if f == nil {
		var count int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM managed_configuration_heads`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return ErrPublicationFence
		}
		return nil
	}
	if f.Marketplace == "" || f.RegistryGeneration < 1 || f.PolicyRevision < 1 || f.JobID == "" || f.WorkerID == "" || f.Attempt < 1 {
		return ErrPublicationFence
	}
	result, err := q.ExecContext(ctx, `UPDATE managed_configuration_heads SET revision=revision WHERE marketplace_id=$1`, f.Marketplace)
	if err != nil {
		return fmt.Errorf("lock publication authority: %w", err)
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrPublicationFence
	}
	change, err := ReadManagedConfiguration(ctx, q, f.Marketplace)
	if err != nil {
		return err
	}
	var snapshot struct {
		Version        int   `json:"version"`
		PolicyRevision int64 `json:"policy_revision"`
		Registries     []struct {
			ID         RegistryID `json:"id"`
			Generation int64      `json:"generation"`
			Lifecycle  string     `json:"lifecycle"`
			Paused     bool       `json:"paused"`
			Withdrawn  bool       `json:"withdrawn"`
			Retired    bool       `json:"retired"`
		} `json:"registries"`
	}
	if json.Unmarshal(change.Snapshot, &snapshot) != nil || snapshot.Version != 1 {
		return ErrManagedStateCorrupt
	}
	if snapshot.PolicyRevision != f.PolicyRevision {
		return ErrPublicationFence
	}
	matched := 0
	for _, r := range snapshot.Registries {
		if r.ID == req.Occurrence.RegistryID {
			matched++
			if r.Generation != f.RegistryGeneration || r.Lifecycle != "active" || r.Paused || r.Withdrawn || r.Retired {
				return ErrPublicationFence
			}
		}
	}
	if matched != 1 {
		return ErrPublicationFence
	}
	result, err = q.ExecContext(ctx, `UPDATE jobs SET attempt=attempt WHERE id=$1`, f.JobID)
	if err != nil {
		return fmt.Errorf("lock publication lease: %w", err)
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrPublicationFence
	}
	now := time.Now().UTC().UnixMicro()
	var valid int
	err = q.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE id=$1 AND worker_id=$2 AND attempt=$3 AND status='leased' AND lease_until>$4`, f.JobID, f.WorkerID, f.Attempt, now).Scan(&valid)
	if err != nil {
		return fmt.Errorf("check publication lease: %w", err)
	}
	if valid != 1 {
		return ErrPublicationFence
	}
	return nil
}
