package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"

	_ "modernc.org/sqlite"
)

// queryer captures the methods used from *sql.DB and *sql.Tx so that
// transaction-scoped stores can share the same implementation.
type queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Store is a SQLite-backed [store.MetadataStore].
type Store struct {
	path string
	db   *sql.DB
	q    queryer
	tx   *sql.Tx
}

// New returns a SQLite metadata store for the given file path. The file and
// parent directories are created on [Store.Open]. A path of ":memory:" is
// supported for testing.
func New(path string) *Store {
	return &Store{path: path}
}

// Open opens the database, enables WAL, foreign keys, and a bounded busy
// timeout, and applies any pending migrations.
func (s *Store) Open(ctx context.Context) error {
	dsn := s.path
	if dsn == "" {
		return fmt.Errorf("%w: missing database path", store.ErrTopology)
	}
	if dsn != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(dsn), 0755); err != nil {
			return fmt.Errorf("create database directory: %w", err)
		}
		dsn = fmt.Sprintf("file:%s?_txlock=immediate&_fk=1&_busy_timeout=5000&_journal=WAL", dsn)
	} else {
		dsn = "file::memory:?_txlock=immediate&_fk=1&_busy_timeout=5000"
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("open sqlite: %w", err)
	}
	s.db = db
	s.q = db

	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL;"); err != nil {
		_ = db.Close()
		return fmt.Errorf("enable WAL: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=ON;"); err != nil {
		_ = db.Close()
		return fmt.Errorf("enable foreign keys: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout=5000;"); err != nil {
		_ = db.Close()
		return fmt.Errorf("set busy timeout: %w", err)
	}

	for i, m := range migrations {
		if _, err := db.ExecContext(ctx, m); err != nil {
			_ = db.Close()
			return fmt.Errorf("migration %d: %w", i, err)
		}
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("ping sqlite: %w", err)
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
	txStore := &Store{path: s.path, db: s.db, q: tx, tx: tx}
	if err := fn(txStore); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// time helpers.
func timeToMicro(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMicro()
}

func microToTime(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.UnixMicro(v).UTC()
}

// json helpers.
func marshalJSON(v any) (string, error) {
	if v == nil {
		return "{}", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func unmarshalJSON(data []byte, v any) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, v)
}

// -- Marketplace --

func (s *Store) GetMarketplace(ctx context.Context, id store.MarketplaceID) (store.Marketplace, error) {
	row := s.q.QueryRowContext(ctx,
		"SELECT id, name, canonical_url, instance_metadata FROM marketplaces WHERE id=?",
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
		`INSERT OR REPLACE INTO marketplaces (id, name, canonical_url, instance_metadata)
		 VALUES (?, ?, ?, ?)`,
		m.ID, m.Name, m.CanonicalURL, mdJSON,
	)
	return err
}

// -- Sources --

func (s *Store) GetSource(ctx context.Context, id store.SourceID) (store.Source, error) {
	row := s.q.QueryRowContext(ctx,
		`SELECT id, slug, aliases, origins, ref, roots, acquisition, publisher,
		        license, credentials_ref, policy_overrides, listed,
		        refresh_status, refresh_error, refreshed_at, created_at, updated_at
		 FROM sources WHERE id=?`,
		id,
	)
	return s.scanSource(row)
}

func (s *Store) scanSource(row *sql.Row) (store.Source, error) {
	var src store.Source
	var aliasesJSON, originsJSON, rootsJSON, publisherJSON, policyJSON string
	var refreshStatus string
	var refreshError sql.NullString
	var listed int
	var created, updated, refreshedAt int64
	err := row.Scan(
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
	src.Listed = listed != 0
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

	listed := 0
	if src.Listed {
		listed = 1
	}

	_, err = s.q.ExecContext(ctx,
		`INSERT OR REPLACE INTO sources
		 (id, slug, aliases, origins, ref, roots, acquisition, publisher,
		  license, credentials_ref, policy_overrides, listed,
		  refresh_status, refresh_error, refreshed_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		src.ID, src.Slug, aliasesJSON, originsJSON, src.Ref, rootsJSON,
		src.Acquisition, publisherJSON, src.License, src.CredentialsRef,
		policyJSON, listed,
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
		 FROM sources WHERE listed=1 ORDER BY slug`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []store.Source
	for rows.Next() {
		src, err := s.scanSourceFromRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

func (s *Store) scanSourceFromRows(rows *sql.Rows) (store.Source, error) {
	var src store.Source
	var aliasesJSON, originsJSON, rootsJSON, publisherJSON, policyJSON string
	var refreshStatus string
	var refreshError sql.NullString
	var listed int
	var created, updated, refreshedAt int64
	err := rows.Scan(
		&src.ID, &src.Slug, &aliasesJSON, &originsJSON, &src.Ref, &rootsJSON,
		&src.Acquisition, &publisherJSON, &src.License, &src.CredentialsRef,
		&policyJSON, &listed,
		&refreshStatus, &refreshError, &refreshedAt,
		&created, &updated,
	)
	if err != nil {
		return store.Source{}, err
	}
	src.Listed = listed != 0
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

func (s *Store) GetSourceBySlug(ctx context.Context, slug string) (store.Source, error) {
	row := s.q.QueryRowContext(ctx,
		`SELECT id, slug, aliases, origins, ref, roots, acquisition, publisher,
		        license, credentials_ref, policy_overrides, listed,
		        refresh_status, refresh_error, refreshed_at, created_at, updated_at
		 FROM sources
		 WHERE slug=? OR EXISTS (SELECT 1 FROM json_each(aliases) WHERE value=?)`,
		slug, slug,
	)
	return s.scanSource(row)
}

func (s *Store) SourceHistory(ctx context.Context, id store.SourceID) ([]store.SourceOccurrence, error) {
	rows, err := s.q.QueryContext(ctx,
		`SELECT source_id, ref, revision_commit, manifest_digest, acquisition,
		        mirrored_artifact_digest, occurred_at
		 FROM source_occurrences WHERE source_id=? ORDER BY occurred_at DESC`,
		id,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []store.SourceOccurrence
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
		 WHERE marketplace_id=? AND source_id=? AND kind=? AND name=? AND content_digest=?`,
		coord.Marketplace, coord.Source, coord.Kind, coord.Name, digest,
	)
	return s.scanResourceRevision(row)
}

func (s *Store) scanResourceRevision(row *sql.Row) (store.ResourceRevision, error) {
	var rev store.ResourceRevision
	var archiveDigest, license, analyzer, provenanceJSON sql.NullString
	var findingsJSON string
	var revocationJSON []byte

	err := row.Scan(
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
	if len(revocationJSON) > 0 && string(revocationJSON) != "null" {
		rev.Revocation = &store.Revocation{}
		if err := unmarshalJSON(revocationJSON, rev.Revocation); err != nil {
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
	if archive == "" {
		archive = "" // stored as NULL when empty below
	}

	_, err = s.q.ExecContext(ctx,
		`INSERT OR REPLACE INTO resource_revisions
		 (marketplace_id, source_id, kind, name, content_digest, archive_digest,
		  analyzer_version, findings, license, provenance, revocation)
		 VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, NULLIF(?, ''), ?)`,
		rev.Coordinate.Marketplace, rev.Coordinate.Source, rev.Coordinate.Kind, rev.Coordinate.Name,
		rev.ContentDigest, archive, rev.AnalyzerVersion, findingsJSON, rev.License, provenanceJSON, revocationJSON,
	)
	return err
}

func (s *Store) ListResourceRevisions(ctx context.Context, coord store.ResourceCoordinate) ([]store.ResourceRevision, error) {
	rows, err := s.q.QueryContext(ctx,
		`SELECT marketplace_id, source_id, kind, name, content_digest, archive_digest,
		        analyzer_version, findings, license, provenance, revocation
		 FROM resource_revisions
		 WHERE marketplace_id=? AND source_id=? AND kind=? AND name=?
		 ORDER BY content_digest`,
		coord.Marketplace, coord.Source, coord.Kind, coord.Name,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []store.ResourceRevision
	for rows.Next() {
		rev, err := s.scanResourceRevisionFromRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rev)
	}
	return out, rows.Err()
}

func (s *Store) scanResourceRevisionFromRows(rows *sql.Rows) (store.ResourceRevision, error) {
	var rev store.ResourceRevision
	var archiveDigest, license, analyzer, provenanceJSON sql.NullString
	var findingsJSON string
	var revocationJSON []byte

	err := rows.Scan(
		&rev.Coordinate.Marketplace, &rev.Coordinate.Source, &rev.Coordinate.Kind, &rev.Coordinate.Name,
		&rev.ContentDigest, &archiveDigest, &analyzer, &findingsJSON, &license, &provenanceJSON, &revocationJSON,
	)
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
	if len(revocationJSON) > 0 && string(revocationJSON) != "null" {
		rev.Revocation = &store.Revocation{}
		if err := unmarshalJSON(revocationJSON, rev.Revocation); err != nil {
			return store.ResourceRevision{}, fmt.Errorf("unmarshal revocation: %w", err)
		}
	}
	return rev, nil
}
