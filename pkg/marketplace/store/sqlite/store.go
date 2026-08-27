package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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
// timeout through modernc.org/sqlite's supported _pragma DSN options, and
// applies any pending migrations. SQLite is deliberately kept to one pooled
// connection: this serializes writes and prevents each connection from seeing
// a separate :memory: database.
func (s *Store) Open(ctx context.Context) error {
	if strings.TrimSpace(s.path) == "" {
		return fmt.Errorf("%w: missing database path", store.ErrTopology)
	}

	dsn, err := buildDSN(s.path)
	if err != nil {
		return store.SanitizeDSNError(fmt.Errorf("build sqlite DSN: %w", err), s.path)
	}
	if path := sqliteFilePath(s.path); path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return store.SanitizeDSNError(fmt.Errorf("create database directory: %w", err), s.path)
		}
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return store.SanitizeDSNError(fmt.Errorf("open sqlite: %w", err), s.path)
	}
	// modernc connections carry connection-local PRAGMA state. A single
	// connection is required for :memory: and also avoids SQLite write-lock
	// races between database/sql pool connections.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s.db = db
	s.q = db

	if err := runMigrations(ctx, db); err != nil {
		_ = db.Close()
		return store.SanitizeDSNError(fmt.Errorf("run migrations: %w", err), s.path)
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return store.SanitizeDSNError(fmt.Errorf("ping sqlite: %w", err), s.path)
	}
	return nil
}

func buildDSN(path string) (string, error) {
	if path == ":memory:" {
		path = "file::memory:"
	} else if !strings.HasPrefix(path, "file:") {
		// Escape the filename as a SQLite URI path. In particular, a '?' in a
		// filesystem path must not become the beginning of the DSN query.
		path = "file:" + url.PathEscape(path)
	}

	u, err := url.Parse(path)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("_txlock", "immediate")
	q.Del("_fk")
	q.Del("_busy_timeout")
	q.Del("_journal")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(ON)")
	q.Add("_pragma", "journal_mode(WAL)")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// sqliteFilePath returns the filesystem path to create before opening a file
// database. An empty result means that the DSN is an in-memory database.
func sqliteFilePath(path string) string {
	if path == ":memory:" || strings.HasPrefix(path, "file::memory:") {
		return ""
	}
	if !strings.HasPrefix(path, "file:") {
		return path
	}
	u, err := url.Parse(path)
	if err != nil || u.Host != "" {
		return ""
	}
	if u.Path != "" {
		return u.Path
	}
	if u.Opaque != "" {
		return u.Opaque
	}
	return ""
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
		fmt.Sprintf(`SELECT source_id, ref, roots, revision_commit, manifest_digest, acquisition,
		        mirrored_artifact_digest, occurred_at
		 FROM source_occurrences WHERE source_id=? ORDER BY occurred_at DESC, revision_commit DESC
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

/* Legacy analyzer-coupled resource implementation retained only in history.
// -- Resource revisions --

// rowScanner is satisfied by both *sql.Row and *sql.Rows so a single scan
// implementation serves point lookups and iteration alike.
type rowScanner interface {
	Scan(dest ...any) error
}

// resourceRevisionColumns is the single definition of the resource revision
// projection. Every read path selects exactly these columns in this order, so
// adding a column cannot silently drop out of one query site.
const resourceRevisionColumns = `marketplace_id, source_id, kind, name, content_digest, archive_digest,
	        analyzer_version, findings, license, description, provenance, revocation`

// qualifiedResourceRevisionColumns is resourceRevisionColumns aliased for the
// joined search query.
const qualifiedResourceRevisionColumns = `rr.marketplace_id, rr.source_id, rr.kind, rr.name, rr.content_digest, rr.archive_digest,
	       rr.analyzer_version, rr.findings, rr.license, rr.description, rr.provenance, rr.revocation`

func (s *Store) GetResourceRevision(ctx context.Context, coord store.ResourceCoordinate, digest store.RevisionDigest) (store.ResourceRevision, error) {
	row := s.q.QueryRowContext(ctx,
		`SELECT `+resourceRevisionColumns+`
		 FROM resource_revisions
		 WHERE marketplace_id=? AND source_id=? AND kind=? AND name=? AND content_digest=?`,
		coord.Marketplace, coord.Source, coord.Kind, coord.Name, digest,
	)
	return s.scanResourceRevision(row)
}

func (s *Store) scanResourceRevision(sc rowScanner) (store.ResourceRevision, error) {
	var rev store.ResourceRevision
	var archiveDigest, license, analyzer, description, provenanceJSON sql.NullString
	var findingsJSON string
	var revocationJSON []byte

	err := sc.Scan(
		&rev.Coordinate.Marketplace, &rev.Coordinate.Source, &rev.Coordinate.Kind, &rev.Coordinate.Name,
		&rev.ContentDigest, &archiveDigest, &analyzer, &findingsJSON, &license, &description,
		&provenanceJSON, &revocationJSON,
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
	rev.Description = description.String

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
		  analyzer_version, findings, license, description, provenance, revocation, created_at)
		 VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, NULLIF(?, ''), ?, ?)`,
		rev.Coordinate.Marketplace, rev.Coordinate.Source, rev.Coordinate.Kind, rev.Coordinate.Name,
		rev.ContentDigest, archive, rev.AnalyzerVersion, findingsJSON, rev.License,
		store.BoundDescription(rev.Description), provenanceJSON, revocationJSON,
		timeToMicro(time.Now().UTC()),
	)
	return err
}

func (s *Store) ListResourceRevisions(ctx context.Context, coord store.ResourceCoordinate) ([]store.ResourceRevision, error) {
	rows, err := s.q.QueryContext(ctx,
		`SELECT `+resourceRevisionColumns+`
		 FROM resource_revisions
		 WHERE marketplace_id=? AND source_id=? AND kind=? AND name=?
		 ORDER BY content_digest`,
		coord.Marketplace, coord.Source, coord.Kind, coord.Name,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return s.collectResourceRevisions(rows)
}

// collectResourceRevisions drains rows produced by a resourceRevisionColumns
// projection.
func (s *Store) collectResourceRevisions(rows *sql.Rows) ([]store.ResourceRevision, error) {
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

func (s *Store) ListResourceRevisionsByArchiveDigest(ctx context.Context, marketplace store.MarketplaceID, digest store.ArchiveDigest) ([]store.ResourceRevision, error) {
	rows, err := s.q.QueryContext(ctx,
		`SELECT `+resourceRevisionColumns+`
		 FROM resource_revisions
		 WHERE marketplace_id=? AND archive_digest=?
		 ORDER BY source_id, kind, name, content_digest`,
		marketplace, digest,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return s.collectResourceRevisions(rows)
}

func (s *Store) ListResourceRevisionsByContentDigest(ctx context.Context, marketplace store.MarketplaceID, digest store.RevisionDigest) ([]store.ResourceRevision, error) {
	rows, err := s.q.QueryContext(ctx,
		`SELECT `+resourceRevisionColumns+`
		 FROM resource_revisions
		 WHERE marketplace_id=? AND content_digest=?
		 ORDER BY source_id, kind, name`, marketplace, digest)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return s.collectResourceRevisions(rows)
}

func (s *Store) GetLatestResourceRevision(ctx context.Context, coord store.ResourceCoordinate) (store.ResourceRevision, error) {
	row := s.q.QueryRowContext(ctx,
		`SELECT `+resourceRevisionColumns+`
		 FROM resource_revisions
		 WHERE marketplace_id=? AND source_id=? AND kind=? AND name=?
		 ORDER BY created_at DESC, content_digest DESC
		 LIMIT 1`,
		coord.Marketplace, coord.Source, coord.Kind, coord.Name,
	)
	return s.scanResourceRevision(row)
}

func (s *Store) SearchResourceRevisions(ctx context.Context, filter store.ResourceSearchFilter) ([]store.ResourceRevision, error) {
	args := []any{string(filter.Marketplace)}
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
		where += " AND rr.name LIKE ?"
		args = append(args, "%"+filter.Query+"%")
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM resource_revisions rr
		JOIN sources s ON s.id = rr.source_id
		WHERE %s
		ORDER BY rr.source_id, rr.kind, rr.name, rr.created_at DESC, rr.content_digest DESC`,
		qualifiedResourceRevisionColumns, where)

	rows, err := s.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return s.collectResourceRevisions(rows)
}
*/
