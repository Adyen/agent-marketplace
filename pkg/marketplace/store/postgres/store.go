package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// queryer captures the methods used from *sql.DB and *sql.Tx so that
// transaction-scoped stores can share the same implementation.
type queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Store is a PostgreSQL-backed [store.MetadataStore].
type Store struct {
	dsn    string
	schema string
	db     *sql.DB
	q      queryer
	tx     *sql.Tx
}

// New returns a PostgreSQL metadata store for the given DSN.
func New(dsn string) *Store {
	return &Store{dsn: dsn}
}

// NewWithSchema returns a PostgreSQL metadata store that creates and uses the
// named schema. This is useful for isolating concurrent test runs against a
// shared database.
func NewWithSchema(dsn, schema string) *Store {
	return &Store{dsn: withSearchPath(dsn, schema), schema: schema}
}

func withSearchPath(dsn, schema string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err == nil {
			q := u.Query()
			q.Set("search_path", schema)
			u.RawQuery = q.Encode()
			return u.String()
		}
	}

	// Key/value form.
	re := regexp.MustCompile(`\bsearch_path=[^ ]*`)
	if re.MatchString(dsn) {
		return re.ReplaceAllString(dsn, "search_path="+schema)
	}
	return dsn + " search_path=" + schema
}

// Open opens the database, creates the configured schema if necessary, and
// applies any pending migrations.
func (s *Store) Open(ctx context.Context) error {
	if s.dsn == "" {
		return fmt.Errorf("%w: missing database DSN", store.ErrTopology)
	}

	db, err := sql.Open("pgx", s.dsn)
	if err != nil {
		return store.SanitizeDSNError(fmt.Errorf("open postgres: %w", err), s.dsn)
	}
	s.db = db
	s.q = db

	if s.schema != "" {
		if err = ensureSchema(ctx, db, s.schema); err != nil {
			_ = db.Close()
			return store.SanitizeDSNError(fmt.Errorf("create schema: %w", err), s.dsn)
		}
	}

	if err := runMigrations(ctx, db); err != nil {
		_ = db.Close()
		return store.SanitizeDSNError(fmt.Errorf("run migrations: %w", err), s.dsn)
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return store.SanitizeDSNError(fmt.Errorf("ping postgres: %w", err), s.dsn)
	}
	return nil
}

func ensureSchema(ctx context.Context, db *sql.DB, schema string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	rollback := func(err error) error {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx,
		"SELECT pg_advisory_xact_lock(hashtext($1)::bigint)",
		"agent-marketplace:metadata-schema:"+schema,
	); err != nil {
		return rollback(err)
	}
	if _, err := tx.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS "+pgx.Identifier{schema}.Sanitize()); err != nil {
		return rollback(err)
	}
	return tx.Commit()
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

// WithTx implements [store.MetadataStore.WithTx].
func (s *Store) WithTx(ctx context.Context, fn func(context.Context, store.MetadataStore) error) error {
	return s.withTx(ctx, func(tx *Store) error { return fn(ctx, tx) })
}

func (s *Store) withTx(ctx context.Context, fn func(*Store) error) error {
	if s.tx != nil {
		return fn(s)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	txStore := &Store{dsn: s.dsn, schema: s.schema, db: s.db, q: tx, tx: tx}
	if err := fn(txStore); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// -- Marketplace --

func (s *Store) GetMarketplace(ctx context.Context, id store.MarketplaceID) (store.Marketplace, error) {
	row := s.q.QueryRowContext(ctx,
		"SELECT id, name, canonical_url, instance_metadata FROM marketplaces WHERE id=$1",
		id,
	)
	var m store.Marketplace
	var mdJSON string
	err := row.Scan(&m.ID, &m.Name, &m.CanonicalURL, &mdJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Marketplace{}, store.ErrNotFound
	}
	if err != nil {
		return store.Marketplace{}, err
	}
	m.InstanceMetadata = make(map[string]any)
	if err := unmarshalJSON([]byte(mdJSON), &m.InstanceMetadata); err != nil {
		return store.Marketplace{}, fmt.Errorf("unmarshal instance_metadata: %w", err)
	}
	return m, nil
}

func (s *Store) PutMarketplace(ctx context.Context, m store.Marketplace) error {
	return s.withTx(ctx, func(tx *Store) error { return tx.putMarketplace(ctx, m) })
}

func (s *Store) putMarketplace(ctx context.Context, m store.Marketplace) error {
	mdJSON, err := marshalJSON(m.InstanceMetadata)
	if err != nil {
		return err
	}
	_, err = s.q.ExecContext(ctx,
		`INSERT INTO marketplaces (id, name, canonical_url, instance_metadata)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (id) DO UPDATE SET
		   name = EXCLUDED.name,
		   canonical_url = EXCLUDED.canonical_url,
		   instance_metadata = EXCLUDED.instance_metadata`,
		m.ID, m.Name, m.CanonicalURL, mdJSON,
	)
	if err != nil {
		return err
	}
	return s.advanceCatalogVersion(ctx)
}

// -- Registries --

type sourceScanner interface {
	Scan(dest ...any) error
}

func (s *Store) GetRegistry(ctx context.Context, id store.RegistryID) (store.Registry, error) {
	row := s.q.QueryRowContext(ctx,
		`SELECT id, slug, aliases, origins, ref, roots, acquisition, publisher,
		        license, credentials_ref, policy_overrides, listed,
		        refresh_status, refresh_error, refreshed_at, created_at, updated_at
		 FROM registries WHERE id=$1`,
		id,
	)
	return s.scanRegistry(row)
}

func (s *Store) scanRegistry(sc sourceScanner) (store.Registry, error) {
	var registry store.Registry
	var aliasesJSON, originsJSON, rootsJSON, publisherJSON, policyJSON string
	var refreshStatus string
	var refreshError sql.NullString
	var listed bool
	var created, updated, refreshedAt int64
	err := sc.Scan(
		&registry.ID, &registry.Slug, &aliasesJSON, &originsJSON, &registry.Ref, &rootsJSON,
		&registry.Acquisition, &publisherJSON, &registry.License, &registry.CredentialsRef,
		&policyJSON, &listed,
		&refreshStatus, &refreshError, &refreshedAt,
		&created, &updated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Registry{}, store.ErrNotFound
	}
	if err != nil {
		return store.Registry{}, err
	}
	registry.Listed = listed
	registry.RefreshStatus = store.RegistryRefreshStatus(refreshStatus)
	if refreshError.Valid {
		registry.RefreshError = refreshError.String
	}
	registry.RefreshedAt = microToTime(refreshedAt)
	registry.CreatedAt = microToTime(created)
	registry.UpdatedAt = microToTime(updated)

	for _, p := range []struct {
		data string
		v    any
	}{
		{aliasesJSON, &registry.Aliases},
		{originsJSON, &registry.Origins},
		{rootsJSON, &registry.Roots},
		{publisherJSON, &registry.Publisher},
		{policyJSON, &registry.PolicyOverrides},
	} {
		if p.data == "" {
			p.data = "{}"
		}
		if err := unmarshalJSON([]byte(p.data), p.v); err != nil {
			return store.Registry{}, fmt.Errorf("unmarshal registry field: %w", err)
		}
	}

	if registry.PolicyOverrides == nil {
		registry.PolicyOverrides = map[string]any{}
	}
	return registry, nil
}

func (s *Store) PutRegistry(ctx context.Context, registry store.Registry) error {
	return s.withTx(ctx, func(tx *Store) error { return tx.putRegistry(ctx, registry) })
}

func (s *Store) putRegistry(ctx context.Context, registry store.Registry) error {
	if err := registry.Acquisition.Validate(); err != nil {
		return err
	}
	if registry.RefreshStatus == "" {
		registry.RefreshStatus = store.RegistryRefreshStatusUnknown
	}
	now := time.Now().UTC()
	if registry.CreatedAt.IsZero() {
		registry.CreatedAt = now
	}
	registry.UpdatedAt = now

	if registry.Aliases == nil {
		registry.Aliases = []string{}
	}
	aliasesJSON, err := marshalJSON(registry.Aliases)
	if err != nil {
		return err
	}
	originsJSON, err := marshalJSON(registry.Origins)
	if err != nil {
		return err
	}
	rootsJSON, err := marshalJSON(registry.Roots)
	if err != nil {
		return err
	}
	publisherJSON, err := marshalJSON(registry.Publisher)
	if err != nil {
		return err
	}
	policyJSON, err := marshalJSON(registry.PolicyOverrides)
	if err != nil {
		return err
	}

	_, err = s.q.ExecContext(ctx,
		`INSERT INTO registries
		 (id, slug, aliases, origins, ref, roots, acquisition, publisher,
		  license, credentials_ref, policy_overrides, listed,
		  refresh_status, refresh_error, refreshed_at, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		 ON CONFLICT (id) DO UPDATE SET
		   slug = EXCLUDED.slug,
		   aliases = EXCLUDED.aliases,
		   origins = EXCLUDED.origins,
		   ref = EXCLUDED.ref,
		   roots = EXCLUDED.roots,
		   acquisition = EXCLUDED.acquisition,
		   publisher = EXCLUDED.publisher,
		   license = EXCLUDED.license,
		   credentials_ref = EXCLUDED.credentials_ref,
		   policy_overrides = EXCLUDED.policy_overrides,
		   listed = EXCLUDED.listed,
		   refresh_status = EXCLUDED.refresh_status,
		   refresh_error = EXCLUDED.refresh_error,
		   refreshed_at = EXCLUDED.refreshed_at,
		   created_at = EXCLUDED.created_at,
		   updated_at = EXCLUDED.updated_at`,
		registry.ID, registry.Slug, aliasesJSON, originsJSON, registry.Ref, rootsJSON,
		registry.Acquisition, publisherJSON, registry.License, registry.CredentialsRef,
		policyJSON, registry.Listed,
		string(registry.RefreshStatus), registry.RefreshError, timeToMicro(registry.RefreshedAt),
		timeToMicro(registry.CreatedAt), timeToMicro(registry.UpdatedAt),
	)
	if err != nil {
		return err
	}
	return s.advanceCatalogVersion(ctx)
}

func (s *Store) ListRegistries(ctx context.Context) ([]store.Registry, error) {
	rows, err := s.q.QueryContext(ctx,
		`SELECT id, slug, aliases, origins, ref, roots, acquisition, publisher,
		        license, credentials_ref, policy_overrides, listed,
		        refresh_status, refresh_error, refreshed_at, created_at, updated_at
		 FROM registries WHERE listed=true ORDER BY slug`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []store.Registry
	for rows.Next() {
		registry, err := s.scanRegistry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, registry)
	}
	return out, rows.Err()
}

func (s *Store) GetRegistryBySlug(ctx context.Context, slug string) (store.Registry, error) {
	row := s.q.QueryRowContext(ctx,
		`SELECT id, slug, aliases, origins, ref, roots, acquisition, publisher,
		        license, credentials_ref, policy_overrides, listed,
		        refresh_status, refresh_error, refreshed_at, created_at, updated_at
		 FROM registries
		 WHERE slug=$1 OR EXISTS (
		   SELECT 1 FROM jsonb_array_elements_text(
		     CASE jsonb_typeof(aliases::jsonb)
		       WHEN 'array' THEN aliases::jsonb
		       ELSE '[]'::jsonb
		     END
		   ) AS value WHERE value=$2
		 )`,
		slug, slug,
	)
	return s.scanRegistry(row)
}

func (s *Store) RegistryHistory(ctx context.Context, id store.RegistryID) ([]store.RegistryOccurrence, error) {
	rows, err := s.q.QueryContext(ctx,
		fmt.Sprintf(`SELECT registry_id, ref, roots, revision_commit, manifest_digest, acquisition,
		        mirrored_artifact_digest, source_origin, occurred_at
		 FROM all_registry_occurrences WHERE registry_id=$1 ORDER BY occurred_at DESC, occurrence_key DESC
		 LIMIT %d`, store.MaxRegistryHistoryEntries),
		id,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]store.RegistryOccurrence, 0)
	for rows.Next() {
		occ, err := s.scanRegistryOccurrence(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, occ)
	}
	return out, rows.Err()
}

// -- Resource revisions --

const assessedResourceColumns = `rr.marketplace_id, rr.registry_id, rr.kind, rr.name,
	rr.content_digest, rr.archive_digest, rr.license, rr.description,
	ra.assessment_digest, ra.analyzer_version, ra.policy_digest, ra.decision,
	ra.findings, ra.inspection_manifest_digest, ra.assessed_at,
	ro.revision_commit, ro.manifest_digest, ro.source_path,
	rv.kind, rv.target_id, rv.reason, rv.applied_at,
	rc.kind, rc.target_id, rc.reason, rc.applied_at,
	rg.kind, rg.target_id, rg.reason, rg.applied_at`

const assessedResourceJoins = `
	JOIN current_resource_assessments cra
	  ON cra.marketplace_id=rr.marketplace_id AND cra.registry_id=rr.registry_id
	 AND cra.kind=rr.kind AND cra.name=rr.name AND cra.content_digest=rr.content_digest
	JOIN resource_assessments ra
	  ON ra.marketplace_id=cra.marketplace_id AND ra.registry_id=cra.registry_id
	 AND ra.kind=cra.kind AND ra.name=cra.name AND ra.content_digest=cra.content_digest
	 AND ra.assessment_digest=cra.assessment_digest
	JOIN LATERAL (
	  SELECT candidate.revision_commit, candidate.manifest_digest, candidate.source_path, so.occurred_at
	  FROM all_resource_occurrences candidate
	  LEFT JOIN all_registry_occurrences so
	    ON so.registry_id=candidate.registry_id AND so.occurrence_key=candidate.occurrence_key
	  WHERE candidate.marketplace_id=rr.marketplace_id AND candidate.registry_id=rr.registry_id
	    AND candidate.kind=rr.kind AND candidate.name=rr.name
	    AND candidate.content_digest=rr.content_digest
	  ORDER BY so.occurred_at DESC NULLS LAST, candidate.occurrence_key DESC
	  LIMIT 1
	) ro ON true
	LEFT JOIN revocations rv
	  ON rv.kind='revision'
	 AND rv.target_id=(rr.marketplace_id || '/' || rr.registry_id || '/' || rr.kind || '/' || rr.name || '@' || rr.content_digest)
	LEFT JOIN revocations rc
	  ON rc.kind='coordinate'
	 AND rc.target_id=(rr.marketplace_id || '/' || rr.registry_id || '/' || rr.kind || '/' || rr.name)
	LEFT JOIN revocations rg
	  ON rg.kind='registry' AND rg.target_id=rr.registry_id`

const currentAssessedResourceJoins = `
	JOIN current_resource_occurrences ro
	  ON ro.marketplace_id=rr.marketplace_id AND ro.registry_id=rr.registry_id
	 AND ro.kind=rr.kind AND ro.name=rr.name AND ro.content_digest=rr.content_digest
	JOIN current_resource_assessments cra
	  ON cra.marketplace_id=rr.marketplace_id AND cra.registry_id=rr.registry_id
	 AND cra.kind=rr.kind AND cra.name=rr.name AND cra.content_digest=rr.content_digest
	JOIN resource_assessments ra
	  ON ra.marketplace_id=cra.marketplace_id AND ra.registry_id=cra.registry_id
	 AND ra.kind=cra.kind AND ra.name=cra.name AND ra.content_digest=cra.content_digest
	 AND ra.assessment_digest=cra.assessment_digest
	LEFT JOIN revocations rv
	  ON rv.kind='revision'
	 AND rv.target_id=(rr.marketplace_id || '/' || rr.registry_id || '/' || rr.kind || '/' || rr.name || '@' || rr.content_digest)
	LEFT JOIN revocations rc
	  ON rc.kind='coordinate'
	 AND rc.target_id=(rr.marketplace_id || '/' || rr.registry_id || '/' || rr.kind || '/' || rr.name)
	LEFT JOIN revocations rg
	  ON rg.kind='registry' AND rg.target_id=rr.registry_id`

func (s *Store) GetResourceRevision(ctx context.Context, coord store.ResourceCoordinate, digest store.RevisionDigest) (store.AssessedResourceRevision, error) {
	row := s.q.QueryRowContext(ctx, `SELECT `+assessedResourceColumns+`
		FROM resource_revisions rr`+assessedResourceJoins+`
		WHERE rr.marketplace_id=$1 AND rr.registry_id=$2 AND rr.kind=$3 AND rr.name=$4 AND rr.content_digest=$5`,
		coord.Marketplace, coord.Registry, coord.Kind, coord.Name, digest)
	return s.scanAssessedResourceRevision(row)
}

func (s *Store) scanAssessedResourceRevision(sc sourceScanner) (store.AssessedResourceRevision, error) {
	var out store.AssessedResourceRevision
	var archive, license, sourcePath sql.NullString
	var findings string
	var assessedAt int64
	var revocationKind, targetID, reason sql.NullString
	var appliedAt sql.NullInt64
	var coordinateKind, coordinateTarget, coordinateReason sql.NullString
	var coordinateAt sql.NullInt64
	var registryKind, registryTarget, registryReason sql.NullString
	var registryAt sql.NullInt64
	err := sc.Scan(
		&out.Coordinate.Marketplace, &out.Coordinate.Registry, &out.Coordinate.Kind, &out.Coordinate.Name,
		&out.ContentDigest, &archive, &license, &out.Description,
		&out.Assessment.AssessmentDigest, &out.Assessment.AnalyzerVersion, &out.Assessment.PolicyDigest,
		&out.Assessment.Decision, &findings, &out.Assessment.InspectionManifestDigest, &assessedAt,
		&out.Occurrence.Commit, &out.Occurrence.ManifestDigest, &sourcePath,
		&revocationKind, &targetID, &reason, &appliedAt,
		&coordinateKind, &coordinateTarget, &coordinateReason, &coordinateAt,
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
		{revocationKind, targetID, reason, appliedAt},
		{coordinateKind, coordinateTarget, coordinateReason, coordinateAt},
		{registryKind, registryTarget, registryReason, registryAt},
	} {
		if candidate.kind.Valid {
			out.Revocation = &store.Revocation{Kind: store.RevocationKind(candidate.kind.String), TargetID: candidate.target.String, Reason: candidate.reason.String, AppliedAt: microToTime(candidate.at.Int64)}
			break
		}
	}
	return out, nil
}

func (s *Store) getIntrinsicResourceRevision(ctx context.Context, coord store.ResourceCoordinate, digest store.RevisionDigest) (store.ResourceRevision, error) {
	var rev store.ResourceRevision
	var archive, license sql.NullString
	err := s.q.QueryRowContext(ctx, `SELECT marketplace_id, registry_id, kind, name, content_digest,
		archive_digest, license, description FROM resource_revisions
		WHERE marketplace_id=$1 AND registry_id=$2 AND kind=$3 AND name=$4 AND content_digest=$5`,
		coord.Marketplace, coord.Registry, coord.Kind, coord.Name, digest).Scan(
		&rev.Coordinate.Marketplace, &rev.Coordinate.Registry, &rev.Coordinate.Kind, &rev.Coordinate.Name,
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

func (s *Store) PutResourceRevision(ctx context.Context, rev store.ResourceRevision) error {
	return s.withTx(ctx, func(tx *Store) error { return tx.putResourceRevision(ctx, rev) })
}

func (s *Store) putResourceRevision(ctx context.Context, rev store.ResourceRevision) error {
	rev.Description = store.BoundDescription(rev.Description)
	_, err := s.q.ExecContext(ctx, `INSERT INTO resource_revisions
		(marketplace_id, registry_id, kind, name, content_digest, archive_digest, license, description)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), $8)
		ON CONFLICT (marketplace_id, registry_id, kind, name, content_digest) DO NOTHING`,
		rev.Coordinate.Marketplace, rev.Coordinate.Registry, rev.Coordinate.Kind, rev.Coordinate.Name,
		rev.ContentDigest, string(rev.ArchiveDigest), rev.License, rev.Description)
	if err != nil {
		return err
	}
	existing, err := s.getIntrinsicResourceRevision(ctx, rev.Coordinate, rev.ContentDigest)
	if err != nil {
		return err
	}
	if existing != rev {
		return fmt.Errorf("%w: resource revision %s@%s already has different immutable fields", store.ErrConflict, rev.Coordinate, rev.ContentDigest)
	}
	// An intrinsic revision is not catalog-visible until both a current
	// occurrence and assessment point at it.
	return nil
}

func (s *Store) PutResourceOccurrence(ctx context.Context, occurrence store.ResourceOccurrence) error {
	return s.withTx(ctx, func(tx *Store) error { return tx.putResourceOccurrence(ctx, occurrence) })
}

func (s *Store) putResourceOccurrence(ctx context.Context, occurrence store.ResourceOccurrence) error {
	normalized, err := store.NormalizeResourceOccurrence(occurrence)
	if err != nil {
		return err
	}
	occurrence = normalized
	result, err := s.q.ExecContext(ctx, `INSERT INTO resource_occurrences
		(marketplace_id, registry_id, kind, name, content_digest, revision_commit, manifest_digest, source_path)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''))
		ON CONFLICT (marketplace_id, registry_id, kind, name, content_digest, revision_commit) DO NOTHING`,
		occurrence.Coordinate.Marketplace, occurrence.Coordinate.Registry, occurrence.Coordinate.Kind,
		occurrence.Coordinate.Name, occurrence.ContentDigest, occurrence.Commit, occurrence.ManifestDigest, occurrence.SourcePath)
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
	err = s.q.QueryRowContext(ctx, `SELECT marketplace_id, registry_id, kind, name, content_digest,
		revision_commit, manifest_digest, source_path FROM resource_occurrences
		WHERE marketplace_id=$1 AND registry_id=$2 AND kind=$3 AND name=$4
		  AND content_digest=$5 AND revision_commit=$6`,
		occurrence.Coordinate.Marketplace, occurrence.Coordinate.Registry, occurrence.Coordinate.Kind,
		occurrence.Coordinate.Name, occurrence.ContentDigest, occurrence.Commit).Scan(
		&existing.Coordinate.Marketplace, &existing.Coordinate.Registry, &existing.Coordinate.Kind,
		&existing.Coordinate.Name, &existing.ContentDigest, &existing.Commit, &existing.ManifestDigest, &sourcePath)
	if err != nil {
		return err
	}
	existing.SourcePath = sourcePath.String
	core := existing
	core.SourcePath = occurrence.SourcePath
	if !occurrence.Equal(core) {
		return fmt.Errorf("%w: resource occurrence %s@%s already has different immutable fields", store.ErrConflict, occurrence.Coordinate, occurrence.Commit)
	}
	if existing.SourcePath != "" && occurrence.SourcePath != "" && existing.SourcePath != occurrence.SourcePath {
		return fmt.Errorf("%w: resource occurrence %s@%s already has a different source path", store.ErrConflict, occurrence.Coordinate, occurrence.Commit)
	}
	if existing.SourcePath == "" && occurrence.SourcePath != "" {
		result, err := s.q.ExecContext(ctx, `UPDATE resource_occurrences SET source_path=$1 WHERE marketplace_id=$2 AND registry_id=$3 AND kind=$4 AND name=$5 AND content_digest=$6 AND revision_commit=$7 AND source_path IS NULL`, occurrence.SourcePath, occurrence.Coordinate.Marketplace, occurrence.Coordinate.Registry, occurrence.Coordinate.Kind, occurrence.Coordinate.Name, occurrence.ContentDigest, occurrence.Commit)
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
	_, err = s.q.ExecContext(ctx, `INSERT INTO resource_assessments
		(marketplace_id, registry_id, kind, name, content_digest, assessment_digest,
		 analyzer_version, policy_digest, decision, findings, inspection_manifest_digest, assessed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (marketplace_id, registry_id, kind, name, content_digest, assessment_digest) DO NOTHING`,
		finalized.Coordinate.Marketplace, finalized.Coordinate.Registry, finalized.Coordinate.Kind,
		finalized.Coordinate.Name, finalized.ContentDigest, finalized.AssessmentDigest,
		finalized.AnalyzerVersion, finalized.PolicyDigest, finalized.Decision, findings,
		finalized.InspectionManifestDigest, timeToMicro(finalized.AssessedAt))
	return err
}

func (s *Store) GetResourceAssessment(ctx context.Context, digest store.AssessmentDigest) (store.ResourceAssessment, error) {
	row := s.q.QueryRowContext(ctx, `SELECT marketplace_id, registry_id, kind, name, content_digest,
		assessment_digest, analyzer_version, policy_digest, decision, findings,
		inspection_manifest_digest, assessed_at FROM resource_assessments WHERE assessment_digest=$1`, digest)
	var assessment store.ResourceAssessment
	var findings string
	var assessedAt int64
	err := row.Scan(&assessment.Coordinate.Marketplace, &assessment.Coordinate.Registry,
		&assessment.Coordinate.Kind, &assessment.Coordinate.Name, &assessment.ContentDigest,
		&assessment.AssessmentDigest, &assessment.AnalyzerVersion, &assessment.PolicyDigest,
		&assessment.Decision, &findings, &assessment.InspectionManifestDigest, &assessedAt)
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

func (s *Store) SetCurrentResourceAssessment(ctx context.Context, assessment store.ResourceAssessment) error {
	return s.withTx(ctx, func(tx *Store) error { return tx.setCurrentResourceAssessment(ctx, assessment) })
}

func (s *Store) setCurrentResourceAssessment(ctx context.Context, assessment store.ResourceAssessment) error {
	if err := assessment.Validate(); err != nil {
		return err
	}
	res, err := s.q.ExecContext(ctx, `INSERT INTO current_resource_assessments
		(marketplace_id, registry_id, kind, name, content_digest, assessment_digest)
		SELECT marketplace_id, registry_id, kind, name, content_digest, assessment_digest
		FROM resource_assessments
		WHERE marketplace_id=$1 AND registry_id=$2 AND kind=$3 AND name=$4
		  AND content_digest=$5 AND assessment_digest=$6
		ON CONFLICT (marketplace_id, registry_id, kind, name, content_digest) DO UPDATE
		SET assessment_digest=EXCLUDED.assessment_digest`,
		assessment.Coordinate.Marketplace, assessment.Coordinate.Registry, assessment.Coordinate.Kind,
		assessment.Coordinate.Name, assessment.ContentDigest, assessment.AssessmentDigest)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return s.advanceCatalogVersion(ctx)
}

func (s *Store) ListResourceAssessments(ctx context.Context, coord store.ResourceCoordinate, digest store.RevisionDigest) ([]store.ResourceAssessment, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT marketplace_id, registry_id, kind, name, content_digest,
		assessment_digest, analyzer_version, policy_digest, decision, findings,
		inspection_manifest_digest, assessed_at FROM resource_assessments
		WHERE marketplace_id=$1 AND registry_id=$2 AND kind=$3 AND name=$4 AND content_digest=$5
		ORDER BY assessed_at DESC, assessment_digest DESC`,
		coord.Marketplace, coord.Registry, coord.Kind, coord.Name, digest)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]store.ResourceAssessment, 0)
	for rows.Next() {
		var assessment store.ResourceAssessment
		var findings string
		var assessedAt int64
		if err := rows.Scan(&assessment.Coordinate.Marketplace, &assessment.Coordinate.Registry,
			&assessment.Coordinate.Kind, &assessment.Coordinate.Name, &assessment.ContentDigest,
			&assessment.AssessmentDigest, &assessment.AnalyzerVersion, &assessment.PolicyDigest,
			&assessment.Decision, &findings, &assessment.InspectionManifestDigest, &assessedAt); err != nil {
			return nil, err
		}
		assessment.AssessedAt = microToTime(assessedAt)
		if err := unmarshalJSON([]byte(findings), &assessment.Findings); err != nil {
			return nil, fmt.Errorf("unmarshal assessment findings: %w", err)
		}
		out = append(out, assessment)
	}
	return out, rows.Err()
}

func (s *Store) ListResourceRevisions(ctx context.Context, coord store.ResourceCoordinate) ([]store.AssessedResourceRevision, error) {
	return s.queryAssessedResourceRevisions(ctx, `SELECT `+assessedResourceColumns+`
		FROM resource_revisions rr`+assessedResourceJoins+`
		WHERE rr.marketplace_id=$1 AND rr.registry_id=$2 AND rr.kind=$3 AND rr.name=$4
		ORDER BY ro.occurred_at DESC NULLS LAST, rr.content_digest DESC`,
		coord.Marketplace, coord.Registry, coord.Kind, coord.Name)
}

func (s *Store) ListResourceRevisionsByArchiveDigest(ctx context.Context, marketplace store.MarketplaceID, digest store.ArchiveDigest) ([]store.AssessedResourceRevision, error) {
	return s.queryAssessedResourceRevisions(ctx, `SELECT `+assessedResourceColumns+`
		FROM resource_revisions rr`+assessedResourceJoins+`
		WHERE rr.marketplace_id=$1 AND rr.archive_digest=$2
		ORDER BY rr.registry_id, rr.kind, rr.name, ro.occurred_at DESC NULLS LAST, rr.content_digest DESC`, marketplace, digest)
}

func (s *Store) ListResourceRevisionsByContentDigest(ctx context.Context, marketplace store.MarketplaceID, digest store.RevisionDigest) ([]store.AssessedResourceRevision, error) {
	return s.queryAssessedResourceRevisions(ctx, `SELECT `+assessedResourceColumns+`
		FROM resource_revisions rr`+assessedResourceJoins+`
		WHERE rr.marketplace_id=$1 AND rr.content_digest=$2
		ORDER BY rr.registry_id, rr.kind, rr.name`, marketplace, digest)
}

func (s *Store) GetLatestResourceRevision(ctx context.Context, coord store.ResourceCoordinate) (store.AssessedResourceRevision, error) {
	row := s.q.QueryRowContext(ctx, `SELECT `+assessedResourceColumns+`
		FROM resource_revisions rr`+currentAssessedResourceJoins+`
		WHERE rr.marketplace_id=$1 AND rr.registry_id=$2 AND rr.kind=$3 AND rr.name=$4
		ORDER BY rr.content_digest DESC LIMIT 1`, coord.Marketplace, coord.Registry, coord.Kind, coord.Name)
	return s.scanAssessedResourceRevision(row)
}

func (s *Store) SearchResourceRevisions(ctx context.Context, filter store.ResourceSearchFilter) ([]store.AssessedResourceRevision, error) {
	args := []any{string(filter.Marketplace)}
	where := "rr.marketplace_id=$1 AND r.listed"
	registryValues := filter.RegistryValues()
	registryClauses := make([]string, 0, len(registryValues))
	for _, registry := range registryValues {
		if registry == "" {
			continue
		}
		registryClauses = append(registryClauses, fmt.Sprintf("(rr.registry_id=$%d OR r.slug=$%d)", len(args)+1, len(args)+2))
		args = append(args, registry, registry)
	}
	if len(registryClauses) > 0 {
		where += " AND (" + strings.Join(registryClauses, " OR ") + ")"
	}
	if filter.Kind != "" {
		where += fmt.Sprintf(" AND rr.kind=$%d", len(args)+1)
		args = append(args, filter.Kind)
	}
	if filter.Query != "" {
		where += fmt.Sprintf(" AND rr.name ILIKE $%d", len(args)+1)
		args = append(args, "%"+filter.Query+"%")
	}
	query := fmt.Sprintf(`SELECT %s FROM resource_revisions rr%s
		JOIN registries r ON r.id=rr.registry_id WHERE %s
		ORDER BY rr.registry_id, rr.kind, rr.name, rr.content_digest DESC`, assessedResourceColumns, currentAssessedResourceJoins, where)
	return s.queryAssessedResourceRevisions(ctx, query, args...)
}

func (s *Store) queryAssessedResourceRevisions(ctx context.Context, query string, args ...any) ([]store.AssessedResourceRevision, error) {
	rows, err := s.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]store.AssessedResourceRevision, 0)
	for rows.Next() {
		rev, err := s.scanAssessedResourceRevision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rev)
	}
	return out, rows.Err()
}
