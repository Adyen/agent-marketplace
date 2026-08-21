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
		return fmt.Errorf("open postgres: %w", err)
	}
	s.db = db
	s.q = db

	if s.schema != "" {
		_, err = db.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS "+pgx.Identifier{s.schema}.Sanitize())
		if err != nil {
			_ = db.Close()
			return fmt.Errorf("create schema: %w", err)
		}
	}

	for i, m := range migrations {
		if _, err := db.ExecContext(ctx, m); err != nil {
			_ = db.Close()
			return fmt.Errorf("migration %d: %w", i, err)
		}
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("ping postgres: %w", err)
	}
	return nil
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
	return err
}

// -- Sources --

type sourceScanner interface {
	Scan(dest ...any) error
}

func (s *Store) GetSource(ctx context.Context, id store.SourceID) (store.Source, error) {
	row := s.q.QueryRowContext(ctx,
		`SELECT id, slug, aliases, origins, ref, roots, acquisition, publisher,
		        license, credentials_ref, policy_overrides, listed,
		        refresh_status, refresh_error, refreshed_at, created_at, updated_at
		 FROM sources WHERE id=$1`,
		id,
	)
	return s.scanSource(row)
}

func (s *Store) scanSource(sc sourceScanner) (store.Source, error) {
	var src store.Source
	var aliasesJSON, originsJSON, rootsJSON, publisherJSON, policyJSON string
	var refreshStatus string
	var refreshError sql.NullString
	var listed bool
	var created, updated, refreshedAt int64
	err := sc.Scan(
		&src.ID, &src.Slug, &aliasesJSON, &originsJSON, &src.Ref, &rootsJSON,
		&src.Acquisition, &publisherJSON, &src.License, &src.CredentialsRef,
		&policyJSON, &listed,
		&refreshStatus, &refreshError, &refreshedAt,
		&created, &updated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Source{}, store.ErrNotFound
	}
	if err != nil {
		return store.Source{}, err
	}
	src.Listed = listed
	src.RefreshStatus = store.SourceRefreshStatus(refreshStatus)
	if refreshError.Valid {
		src.RefreshError = refreshError.String
	}
	src.RefreshedAt = microToTime(refreshedAt)
	src.CreatedAt = microToTime(created)
	src.UpdatedAt = microToTime(updated)

	for _, p := range []struct {
		data string
		v    any
	}{
		{aliasesJSON, &src.Aliases},
		{originsJSON, &src.Origins},
		{rootsJSON, &src.Roots},
		{publisherJSON, &src.Publisher},
		{policyJSON, &src.PolicyOverrides},
	} {
		if p.data == "" {
			p.data = "{}"
		}
		if err := unmarshalJSON([]byte(p.data), p.v); err != nil {
			return store.Source{}, fmt.Errorf("unmarshal source field: %w", err)
		}
	}

	if src.PolicyOverrides == nil {
		src.PolicyOverrides = map[string]any{}
	}
	return src, nil
}

func (s *Store) PutSource(ctx context.Context, src store.Source) error {
	if err := src.Acquisition.Validate(); err != nil {
		return err
	}
	if src.RefreshStatus == "" {
		src.RefreshStatus = store.SourceRefreshStatusUnknown
	}
	now := time.Now().UTC()
	if src.CreatedAt.IsZero() {
		src.CreatedAt = now
	}
	src.UpdatedAt = now

	aliasesJSON, err := marshalJSON(src.Aliases)
	if err != nil {
		return err
	}
	originsJSON, err := marshalJSON(src.Origins)
	if err != nil {
		return err
	}
	rootsJSON, err := marshalJSON(src.Roots)
	if err != nil {
		return err
	}
	publisherJSON, err := marshalJSON(src.Publisher)
	if err != nil {
		return err
	}
	policyJSON, err := marshalJSON(src.PolicyOverrides)
	if err != nil {
		return err
	}

	_, err = s.q.ExecContext(ctx,
		`INSERT INTO sources
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
		src.ID, src.Slug, aliasesJSON, originsJSON, src.Ref, rootsJSON,
		src.Acquisition, publisherJSON, src.License, src.CredentialsRef,
		policyJSON, src.Listed,
		string(src.RefreshStatus), src.RefreshError, timeToMicro(src.RefreshedAt),
		timeToMicro(src.CreatedAt), timeToMicro(src.UpdatedAt),
	)
	return err
}

func (s *Store) ListSources(ctx context.Context) ([]store.Source, error) {
	rows, err := s.q.QueryContext(ctx,
		`SELECT id, slug, aliases, origins, ref, roots, acquisition, publisher,
		        license, credentials_ref, policy_overrides, listed,
		        refresh_status, refresh_error, refreshed_at, created_at, updated_at
		 FROM sources WHERE listed=true ORDER BY slug`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []store.Source
	for rows.Next() {
		src, err := s.scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

func (s *Store) GetSourceBySlug(ctx context.Context, slug string) (store.Source, error) {
	row := s.q.QueryRowContext(ctx,
		`SELECT id, slug, aliases, origins, ref, roots, acquisition, publisher,
		        license, credentials_ref, policy_overrides, listed,
		        refresh_status, refresh_error, refreshed_at, created_at, updated_at
		 FROM sources
		 WHERE slug=$1 OR EXISTS (
		   SELECT 1 FROM jsonb_array_elements_text(aliases::jsonb) AS value WHERE value=$2
		 )`,
		slug, slug,
	)
	return s.scanSource(row)
}

func (s *Store) SourceHistory(ctx context.Context, id store.SourceID) ([]store.SourceOccurrence, error) {
	rows, err := s.q.QueryContext(ctx,
		fmt.Sprintf(`SELECT source_id, ref, revision_commit, manifest_digest, acquisition,
		        mirrored_artifact_digest, occurred_at
		 FROM source_occurrences WHERE source_id=$1 ORDER BY occurred_at DESC, revision_commit DESC
		 LIMIT %d`, store.MaxSourceHistoryEntries),
		id,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]store.SourceOccurrence, 0)
	for rows.Next() {
		occ, err := s.scanSourceOccurrence(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, occ)
	}
	return out, rows.Err()
}

// -- Resource revisions --

func (s *Store) GetResourceRevision(ctx context.Context, coord store.ResourceCoordinate, digest store.RevisionDigest) (store.ResourceRevision, error) {
	row := s.q.QueryRowContext(ctx,
		`SELECT marketplace_id, source_id, kind, name, content_digest, archive_digest,
		        analyzer_version, findings, license, provenance, revocation
		 FROM resource_revisions
		 WHERE marketplace_id=$1 AND source_id=$2 AND kind=$3 AND name=$4 AND content_digest=$5`,
		coord.Marketplace, coord.Source, coord.Kind, coord.Name, digest,
	)
	return s.scanResourceRevision(row)
}

func (s *Store) scanResourceRevision(sc sourceScanner) (store.ResourceRevision, error) {
	var rev store.ResourceRevision
	var archiveDigest, license, analyzer, provenanceJSON, revocationJSON sql.NullString
	var findingsJSON string

	err := sc.Scan(
		&rev.Coordinate.Marketplace, &rev.Coordinate.Source, &rev.Coordinate.Kind, &rev.Coordinate.Name,
		&rev.ContentDigest, &archiveDigest, &analyzer, &findingsJSON, &license, &provenanceJSON, &revocationJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return store.ResourceRevision{}, store.ErrNotFound
	}
	if err != nil {
		return store.ResourceRevision{}, err
	}

	rev.ArchiveDigest = store.ArchiveDigest(archiveDigest.String)
	rev.AnalyzerVersion = analyzer.String
	rev.License = license.String

	if err := unmarshalJSON([]byte(findingsJSON), &rev.Findings); err != nil {
		return store.ResourceRevision{}, fmt.Errorf("unmarshal findings: %w", err)
	}
	if provenanceJSON.Valid && provenanceJSON.String != "" && provenanceJSON.String != "null" {
		rev.Provenance = &store.Provenance{}
		if err := unmarshalJSON([]byte(provenanceJSON.String), rev.Provenance); err != nil {
			return store.ResourceRevision{}, fmt.Errorf("unmarshal provenance: %w", err)
		}
	}
	if revocationJSON.Valid && revocationJSON.String != "" && revocationJSON.String != "null" {
		rev.Revocation = &store.Revocation{}
		if err := unmarshalJSON([]byte(revocationJSON.String), rev.Revocation); err != nil {
			return store.ResourceRevision{}, fmt.Errorf("unmarshal revocation: %w", err)
		}
	}
	return rev, nil
}

func (s *Store) PutResourceRevision(ctx context.Context, rev store.ResourceRevision) error {
	findingsJSON, err := marshalJSON(rev.Findings)
	if err != nil {
		return err
	}
	var provenanceJSON string
	if rev.Provenance != nil {
		provenanceJSON, err = marshalJSON(rev.Provenance)
		if err != nil {
			return err
		}
	}
	var revocationJSON *string
	if rev.Revocation != nil {
		b, err := marshalJSON(rev.Revocation)
		if err != nil {
			return err
		}
		revocationJSON = &b
	}

	archive := string(rev.ArchiveDigest)

	_, err = s.q.ExecContext(ctx,
		`INSERT INTO resource_revisions
		 (marketplace_id, source_id, kind, name, content_digest, archive_digest,
		  analyzer_version, findings, license, provenance, revocation, created_at)
		 VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8, $9, NULLIF($10, ''), $11, $12)
		 ON CONFLICT (marketplace_id, source_id, kind, name, content_digest) DO UPDATE SET
		   archive_digest = EXCLUDED.archive_digest,
		   analyzer_version = EXCLUDED.analyzer_version,
		   findings = EXCLUDED.findings,
		   license = EXCLUDED.license,
		   provenance = EXCLUDED.provenance,
		   revocation = EXCLUDED.revocation,
		   created_at = EXCLUDED.created_at`,
		rev.Coordinate.Marketplace, rev.Coordinate.Source, rev.Coordinate.Kind, rev.Coordinate.Name,
		rev.ContentDigest, archive, rev.AnalyzerVersion, findingsJSON, rev.License, provenanceJSON, revocationJSON,
		timeToMicro(time.Now().UTC()),
	)
	return err
}

func (s *Store) ListResourceRevisions(ctx context.Context, coord store.ResourceCoordinate) ([]store.ResourceRevision, error) {
	rows, err := s.q.QueryContext(ctx,
		`SELECT marketplace_id, source_id, kind, name, content_digest, archive_digest,
		        analyzer_version, findings, license, provenance, revocation
		 FROM resource_revisions
		 WHERE marketplace_id=$1 AND source_id=$2 AND kind=$3 AND name=$4
		 ORDER BY content_digest`,
		coord.Marketplace, coord.Source, coord.Kind, coord.Name,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []store.ResourceRevision
	for rows.Next() {
		rev, err := s.scanResourceRevision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rev)
	}
	return out, rows.Err()
}

func (s *Store) GetLatestResourceRevision(ctx context.Context, coord store.ResourceCoordinate) (store.ResourceRevision, error) {
	row := s.q.QueryRowContext(ctx,
		`SELECT marketplace_id, source_id, kind, name, content_digest, archive_digest,
		        analyzer_version, findings, license, provenance, revocation
		 FROM resource_revisions
		 WHERE marketplace_id=$1 AND source_id=$2 AND kind=$3 AND name=$4
		 ORDER BY created_at DESC, content_digest DESC
		 LIMIT 1`,
		coord.Marketplace, coord.Source, coord.Kind, coord.Name,
	)
	return s.scanResourceRevision(row)
}

func (s *Store) SearchResourceRevisions(ctx context.Context, filter store.ResourceSearchFilter) ([]store.ResourceRevision, error) {
	args := []any{string(filter.Marketplace)}
	where := "rr.marketplace_id=$1 AND s.listed AND rr.revocation IS NULL"

	if filter.Source != "" {
		where += fmt.Sprintf(" AND (rr.source_id=$%d OR s.slug=$%d)", len(args)+1, len(args)+2)
		args = append(args, filter.Source, filter.Source)
	}
	if filter.Kind != "" {
		where += fmt.Sprintf(" AND rr.kind=$%d", len(args)+1)
		args = append(args, filter.Kind)
	}
	if filter.Query != "" {
		where += fmt.Sprintf(" AND rr.name ILIKE $%d", len(args)+1)
		args = append(args, "%"+filter.Query+"%")
	}

	query := fmt.Sprintf(`
		WITH ranked AS (
			SELECT rr.*,
				ROW_NUMBER() OVER (PARTITION BY rr.source_id, rr.kind, rr.name ORDER BY rr.created_at DESC, rr.content_digest DESC) AS rn
			FROM resource_revisions rr
			JOIN sources s ON s.id = rr.source_id
			WHERE %s
		)
		SELECT marketplace_id, source_id, kind, name, content_digest, archive_digest,
		       analyzer_version, findings, license, provenance, revocation
		FROM ranked
		WHERE rn = 1
		ORDER BY source_id, kind, name`, where)

	rows, err := s.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []store.ResourceRevision
	for rows.Next() {
		rev, err := s.scanResourceRevision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rev)
	}
	return out, rows.Err()
}
