package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/admin"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/postgres"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

type optInConditionalStore struct {
	store.MetadataStore
	inner       store.MetadataStore
	conditional atomic.Int64
	ordinary    atomic.Int64
}

func (s *optInConditionalStore) OwnsCatalogProjectionStore(meta store.MetadataStore) bool {
	return meta == s
}

func (s *optInConditionalStore) ReadCatalogProjectionPageConditional(ctx context.Context, req store.CatalogProjectionPageRequest, unchanged func(store.CatalogVersion) bool) (store.CatalogProjectionPage, bool, error) {
	s.conditional.Add(1)
	return s.inner.(store.ConditionalCatalogProjectionStore).ReadCatalogProjectionPageConditional(ctx, req, unchanged)
}

func (s *optInConditionalStore) ReadCatalogProjectionPage(ctx context.Context, req store.CatalogProjectionPageRequest) (store.CatalogProjectionPage, error) {
	s.ordinary.Add(1)
	return s.inner.ReadCatalogProjectionPage(ctx, req)
}

func (s *optInConditionalStore) CurrentManagedConfiguration(ctx context.Context, id store.MarketplaceID) (store.ManagedChange, error) {
	return s.inner.(store.ManagedConfigurationStore).CurrentManagedConfiguration(ctx, id)
}

type sqliteOverrideStore struct {
	*sqlite.Store
	ordinary atomic.Int64
}

func (s *sqliteOverrideStore) ReadCatalogProjectionPage(ctx context.Context, req store.CatalogProjectionPageRequest) (store.CatalogProjectionPage, error) {
	s.ordinary.Add(1)
	return s.Store.ReadCatalogProjectionPage(ctx, req)
}

type postgresOverrideStore struct {
	*postgres.Store
	ordinary atomic.Int64
}

func (s *postgresOverrideStore) ReadCatalogProjectionPage(ctx context.Context, req store.CatalogProjectionPageRequest) (store.CatalogProjectionPage, error) {
	s.ordinary.Add(1)
	return s.Store.ReadCatalogProjectionPage(ctx, req)
}

type conditionalBackend struct {
	name      string
	meta      store.MetadataStore
	exec      func(ctx context.Context, query string, args ...any) error
	queryInt  func(ctx context.Context, query string) (int64, error)
	queryRows func(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	table     func(name string) string
	ph        func(n int) string
	override  func(root store.MetadataStore) (store.MetadataStore, *atomic.Int64)
}

func openConditionalBackend(t *testing.T, name string) conditionalBackend {
	t.Helper()
	ctx := context.Background()

	if name == "postgres" {
		dsn := os.Getenv("POSTGRES_DSN")
		if dsn == "" {
			t.Skip("POSTGRES_DSN is required for the PostgreSQL backend")
		}
		schema := fmt.Sprintf("am_t_%d", time.Now().UnixNano())
		pg := postgres.NewWithSchema(dsn, schema)
		if err := pg.Open(ctx); err != nil {
			t.Fatalf("open postgres: %v", err)
		}
		pgDB, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Fatalf("open raw postgres: %v", err)
		}
		quoted := pgx.Identifier{schema}.Sanitize()
		t.Cleanup(func() {
			_ = pg.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, _ = pgDB.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+quoted+" CASCADE")
			_ = pgDB.Close()
		})
		return conditionalBackend{
			name: "postgres",
			meta: pg,
			exec: func(ctx context.Context, query string, args ...any) error {
				_, err := pgDB.ExecContext(ctx, query, args...)
				return err
			},
			queryInt: func(ctx context.Context, query string) (int64, error) {
				var value int64
				err := pgDB.QueryRowContext(ctx, query).Scan(&value)
				return value, err
			},
			queryRows: pgDB.QueryContext,
			table:     func(name string) string { return quoted + "." + name },
			ph:        func(n int) string { return fmt.Sprintf("$%d", n) },
			override: func(root store.MetadataStore) (store.MetadataStore, *atomic.Int64) {
				w := &postgresOverrideStore{Store: root.(*postgres.Store)}
				return w, &w.ordinary
			},
		}
	}

	path := filepath.Join(t.TempDir(), "metadata.db")
	lite := sqlite.New(path)
	if err := lite.Open(ctx); err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	liteDB, err := sql.Open("sqlite", "file:"+url.PathEscape(path)+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open raw sqlite: %v", err)
	}
	t.Cleanup(func() { _ = lite.Close(); _ = liteDB.Close() })
	return conditionalBackend{
		name: "sqlite",
		meta: lite,
		exec: func(ctx context.Context, query string, args ...any) error {
			_, err := liteDB.ExecContext(ctx, query, args...)
			return err
		},
		queryInt: func(ctx context.Context, query string) (int64, error) {
			var value int64
			err := liteDB.QueryRowContext(ctx, query).Scan(&value)
			return value, err
		},
		queryRows: liteDB.QueryContext,
		table:     func(name string) string { return name },
		ph:        func(int) string { return "?" },
		override: func(root store.MetadataStore) (store.MetadataStore, *atomic.Int64) {
			w := &sqliteOverrideStore{Store: root.(*sqlite.Store)}
			return w, &w.ordinary
		},
	}
}

func (b conditionalBackend) tableRows(t *testing.T, ctx context.Context, name string) []string {
	t.Helper()
	rows, err := b.queryRows(ctx, "SELECT * FROM "+b.table(name))
	if err != nil {
		t.Fatalf("dump %s: %v", name, err)
	}
	defer func() { _ = rows.Close() }()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatalf("columns %s: %v", name, err)
	}
	var out []string
	for rows.Next() {
		values := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			t.Fatalf("scan %s: %v", name, err)
		}
		data, err := json.Marshal(values)
		if err != nil {
			t.Fatalf("marshal %s row: %v", name, err)
		}
		out = append(out, string(data))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows %s: %v", name, err)
	}
	sort.Strings(out)
	return out
}

func catalogTestConfig() config.Config {
	return config.Config{
		Instance:          config.Instance{ID: "mp"},
		CatalogProjection: config.CatalogProjection{Enabled: true},
	}
}

func catalogGet(t *testing.T, handler http.Handler, path, etag string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

// spec: marketplace-catalog-performance/supported-catalog-page-sizes-and-conditional-responses
// spec: marketplace-catalog-performance/validators-preserve-matching-and-rejection-behavior
func TestCatalogConditionalSkipsDecoratedRead(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"sqlite", "postgres"} {
		t.Run(name+"/opt-in", func(t *testing.T) {
			backend := openConditionalBackend(t, name)
			seedCatalog(t, ctx, backend.meta, []string{KindCollection, KindCommand, KindHook, KindMcpServer, KindSkill, KindSubagent})
			wrapped := &optInConditionalStore{MetadataStore: backend.meta, inner: backend.meta}
			handler := NewServer(catalogTestConfig(), wrapped, fsblob.New(t.TempDir())).PublicHandler()

			for _, limit := range []int{1, 100, 500} {
				response := catalogGet(t, handler, fmt.Sprintf("/api/v1/catalog?limit=%d", limit), "")
				if response.Code != http.StatusOK {
					t.Fatalf("limit %d = %d: %s", limit, response.Code, response.Body.String())
				}
				var page CatalogPage
				if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
					t.Fatalf("limit %d body: %v", limit, err)
				}
				wantRows, wantComplete := 6, true
				if limit == 1 {
					wantRows, wantComplete = 1, false
				}
				if len(page.Resources) != wantRows || page.Complete != wantComplete {
					t.Fatalf("limit %d = %d resources complete=%v, want %d/%v", limit, len(page.Resources), page.Complete, wantRows, wantComplete)
				}
				if page.NextCursor == "" != wantComplete {
					t.Fatalf("limit %d cursor presence does not match complete=%v", limit, wantComplete)
				}
			}

			first := catalogGet(t, handler, "/api/v1/catalog?limit=1", "")
			var firstPage CatalogPage
			if first.Code != http.StatusOK || json.Unmarshal(first.Body.Bytes(), &firstPage) != nil || firstPage.NextCursor == "" {
				t.Fatalf("limit-1 page = %d: %s", first.Code, first.Body.String())
			}
			etag := first.Header().Get("ETag")
			if etag == "" {
				t.Fatal("initial page has no ETag")
			}
			continued := catalogGet(t, handler, "/api/v1/catalog?limit=5&after="+url.QueryEscape(string(firstPage.NextCursor)), "")
			if continued.Code != http.StatusOK {
				t.Fatalf("continuation = %d: %s", continued.Code, continued.Body.String())
			}
			var rest CatalogPage
			if err := json.Unmarshal(continued.Body.Bytes(), &rest); err != nil || len(rest.Resources) != 5 || !rest.Complete {
				t.Fatalf("continuation body = %d resources complete=%v, err %v", len(rest.Resources), rest.Complete, err)
			}
			if got := catalogGet(t, handler, "/api/v1/catalog?after="+url.QueryEscape(string(firstPage.NextCursor)), etag).Code; got != http.StatusBadRequest {
				t.Fatalf("continuation validator = %d, want 400", got)
			}

			wrapped.ordinary.Store(0)
			wrapped.conditional.Store(0)
			for name, validator := range map[string]string{
				"strong":   etag,
				"weak":     "W/" + etag,
				"list":     `"other", ` + etag,
				"wildcard": "*",
			} {
				response := catalogGet(t, handler, "/api/v1/catalog", validator)
				if response.Code != http.StatusNotModified || response.Body.Len() != 0 {
					t.Fatalf("%s validator = %d/%q, want bodyless 304", name, response.Code, response.Body.String())
				}
				assertCatalogHeaders(t, response)
			}
			head := httptest.NewRecorder()
			headReq := httptest.NewRequest(http.MethodHead, "/api/v1/catalog", nil)
			headReq.Header.Set("If-None-Match", etag)
			handler.ServeHTTP(head, headReq)
			if head.Code != http.StatusNotModified || head.Body.Len() != 0 {
				t.Fatalf("HEAD conditional = %d/%q, want bodyless 304", head.Code, head.Body.String())
			}
			assertCatalogHeaders(t, head)
			if wrapped.conditional.Load() != 5 {
				t.Fatalf("conditional store method ran %d times, want 5", wrapped.conditional.Load())
			}
			if wrapped.ordinary.Load() != 0 {
				t.Fatalf("ordinary decorated read ran %d times under matching validators", wrapped.ordinary.Load())
			}

			stale := catalogGet(t, handler, "/api/v1/catalog", `"stale-validator"`)
			if stale.Code != http.StatusOK {
				t.Fatalf("non-matching validator = %d, want full 200", stale.Code)
			}
			var page CatalogPage
			if err := json.Unmarshal(stale.Body.Bytes(), &page); err != nil || len(page.Resources) != 6 {
				t.Fatalf("non-matching body = %d resources, err %v", len(page.Resources), err)
			}
			if got := catalogGet(t, handler, "/api/v1/catalog", `"unclosed`).Code; got != http.StatusBadRequest {
				t.Fatalf("malformed validator = %d, want 400", got)
			}
		})

		t.Run(name+"/wrapper-without-opt-in", func(t *testing.T) {
			backend := openConditionalBackend(t, name)
			seedCatalog(t, ctx, backend.meta, []string{KindSkill})
			wrapped, ordinary := backend.override(backend.meta)
			handler := NewServer(catalogTestConfig(), wrapped, fsblob.New(t.TempDir())).PublicHandler()
			first := catalogGet(t, handler, "/api/v1/catalog", "")
			if first.Code != http.StatusOK {
				t.Fatalf("initial = %d", first.Code)
			}
			ordinary.Store(0)
			matching := catalogGet(t, handler, "/api/v1/catalog", first.Header().Get("ETag"))
			if matching.Code != http.StatusNotModified || matching.Body.Len() != 0 {
				t.Fatalf("wrapper matching validator = %d, want 304", matching.Code)
			}
			if ordinary.Load() == 0 {
				t.Fatal("wrapper ordinary override was bypassed by a promoted conditional method")
			}
			fresh := catalogGet(t, handler, "/api/v1/catalog", `"stale"`)
			if fresh.Code != http.StatusOK || ordinary.Load() != 2 {
				t.Fatalf("wrapper non-matching = %d with %d ordinary reads, want 200 and 2", fresh.Code, ordinary.Load())
			}
		})

		t.Run(name+"/empty", func(t *testing.T) {
			backend := openConditionalBackend(t, name)
			if err := backend.meta.PutMarketplace(ctx, store.Marketplace{ID: "mp", Name: "Empty", CanonicalURL: "https://marketplace.test"}); err != nil {
				t.Fatal(err)
			}
			if err := backend.meta.PutRegistry(ctx, store.Registry{ID: "src", Slug: "source", Ref: "main", Listed: true, Acquisition: store.AcquisitionMirror}); err != nil {
				t.Fatal(err)
			}
			handler := NewServer(catalogTestConfig(), backend.meta, fsblob.New(t.TempDir())).PublicHandler()
			response := catalogGet(t, handler, "/api/v1/catalog", "")
			if response.Code != http.StatusOK {
				t.Fatalf("empty catalog = %d: %s", response.Code, response.Body.String())
			}
			var page CatalogPage
			if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if len(page.Resources) != 0 || !page.Complete || page.NextCursor != "" {
				t.Fatalf("empty catalog page = %+v", page)
			}
		})
	}
}

// spec: marketplace-catalog-performance/mutation-and-unchanged-version-corruption-cannot-produce-stale-304
func TestCatalogConditionalIntegrityFailures(t *testing.T) {
	ctx := context.Background()
	kinds := []string{KindSkill, KindCollection}
	skillCoordinate := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: KindSkill, Name: KindSkill}
	skillDigest := store.RevisionDigest("sha256:" + strings.Repeat("a", 64))
	nulMember := `[{"marketplace":"mp","registry":"src","kind":"skill","name":"bad\u0000name"}]`

	for _, backendName := range []string{"sqlite", "postgres"} {
		cases := []struct {
			name          string
			plain         int
			plainBy       map[string]int
			conditional   map[string]int
			conditionalOr int
		}{
			{name: "findings syntax", plain: http.StatusInternalServerError, conditionalOr: http.StatusInternalServerError},
			{name: "findings type", plain: http.StatusInternalServerError, conditionalOr: http.StatusInternalServerError},
			{name: "collection member type", plain: http.StatusInternalServerError, conditionalOr: http.StatusInternalServerError},
			{name: "collection includes syntax", plain: http.StatusInternalServerError, conditionalOr: http.StatusInternalServerError},
			{name: "recognized reference NUL", plain: http.StatusOK,
				plainBy:       map[string]int{"postgres": http.StatusInternalServerError},
				conditional:   map[string]int{"postgres": http.StatusInternalServerError},
				conditionalOr: http.StatusNotModified},
			{name: "recognized reference NUL wrong registry", plain: http.StatusOK, conditionalOr: http.StatusNotModified},
			{name: "recognized reference NUL wrong manifest", plain: http.StatusOK, conditionalOr: http.StatusNotModified},
			{name: "malformed members wrong registry", plain: http.StatusInternalServerError, conditionalOr: http.StatusInternalServerError},
		}
		if backendName == "sqlite" {
			cases = append(cases, struct {
				name          string
				plain         int
				plainBy       map[string]int
				conditional   map[string]int
				conditionalOr int
			}{name: "assessed_at text", plain: http.StatusInternalServerError, conditionalOr: http.StatusInternalServerError})
		}

		for _, tc := range cases {
			t.Run(backendName+"/"+tc.name, func(t *testing.T) {
				fb := openConditionalBackend(t, backendName)
				seedCatalog(t, ctx, fb.meta, kinds)
				freshAssessed, err := fb.meta.GetResourceRevision(ctx, skillCoordinate, skillDigest)
				if err != nil {
					t.Fatal(err)
				}
				freshCollections, err := fb.meta.ListCollectionRevisions(ctx, "src")
				if err != nil || len(freshCollections) != 1 {
					t.Fatal(err)
				}
				digest := string(freshAssessed.Assessment.AssessmentDigest)
				cid := freshCollections[0].ID
				handler := NewServer(catalogTestConfig(), fb.meta, fsblob.New(t.TempDir())).PublicHandler()
				valid := catalogGet(t, handler, "/api/v1/catalog", "")
				if valid.Code != http.StatusOK {
					t.Fatalf("valid control = %d", valid.Code)
				}
				etag := valid.Header().Get("ETag")
				corrupt := func(query string, args ...any) {
					t.Helper()
					if err := fb.exec(ctx, query, args...); err != nil {
						t.Fatal(err)
					}
				}
				switch tc.name {
				case "findings syntax":
					corrupt(fmt.Sprintf("UPDATE %s SET findings=%s WHERE assessment_digest=%s", fb.table("resource_assessments"), fb.ph(1), fb.ph(2)), `{`, digest)
				case "findings type":
					corrupt(fmt.Sprintf("UPDATE %s SET findings=%s WHERE assessment_digest=%s", fb.table("resource_assessments"), fb.ph(1), fb.ph(2)), `[{"message":1}]`, digest)
				case "collection member type":
					corrupt(fmt.Sprintf("UPDATE %s SET members=%s WHERE id=%s", fb.table("collection_revisions"), fb.ph(1), fb.ph(2)), `[{"name":1}]`, cid)
				case "collection includes syntax":
					corrupt(fmt.Sprintf("UPDATE %s SET includes=%s WHERE id=%s", fb.table("collection_revisions"), fb.ph(1), fb.ph(2)), `{`, cid)
				case "recognized reference NUL":
					corrupt(fmt.Sprintf("UPDATE %s SET members=%s WHERE id=%s", fb.table("collection_revisions"), fb.ph(1), fb.ph(2)), nulMember, cid)
				case "recognized reference NUL wrong registry":
					corrupt(fmt.Sprintf("UPDATE %s SET registry_id=%s, members=%s WHERE id=%s", fb.table("collection_revisions"), fb.ph(1), fb.ph(2), fb.ph(3)), "other-reg", nulMember, cid)
				case "recognized reference NUL wrong manifest":
					corrupt(fmt.Sprintf("UPDATE %s SET manifest_digest=%s, members=%s WHERE id=%s", fb.table("collection_revisions"), fb.ph(1), fb.ph(2), fb.ph(3)), "sha256:"+strings.Repeat("0", 64), nulMember, cid)
				case "malformed members wrong registry":
					corrupt(fmt.Sprintf("UPDATE %s SET registry_id=%s, members=%s WHERE id=%s", fb.table("collection_revisions"), fb.ph(1), fb.ph(2), fb.ph(3)), "other-reg", `{`, cid)
				case "assessed_at text":
					corrupt(fmt.Sprintf("UPDATE %s SET assessed_at=%s WHERE assessment_digest=%s", fb.table("resource_assessments"), fb.ph(1), fb.ph(2)), "not-a-timestamp", digest)
				}
				plain := catalogGet(t, handler, "/api/v1/catalog", "")
				wantPlain := tc.plain
				if override, ok := tc.plainBy[backendName]; ok {
					wantPlain = override
				}
				if plain.Code != wantPlain {
					t.Fatalf("ordinary read under corruption = %d, want %d: %s", plain.Code, wantPlain, plain.Body.String())
				}
				wantConditional := tc.conditionalOr
				if override, ok := tc.conditional[backendName]; ok {
					wantConditional = override
				}
				matching := catalogGet(t, handler, "/api/v1/catalog", etag)
				if matching.Code != wantConditional {
					t.Fatalf("matching validator under corruption = %d, want %d: %s", matching.Code, wantConditional, matching.Body.String())
				}
			})
		}

		t.Run(backendName+"/revocation applied_at text", func(t *testing.T) {
			if backendName != "sqlite" {
				t.Skip("SQLite scalar corruption is backend-specific")
			}
			backend := openConditionalBackend(t, backendName)
			seedCatalog(t, ctx, backend.meta, kinds)
			if err := backend.meta.PutRevocation(ctx, store.Revocation{Kind: store.RevocationCoordinate, TargetID: skillCoordinate.String(), Reason: "fixture", AppliedAt: time.Unix(1, 0).UTC()}); err != nil {
				t.Fatal(err)
			}
			handler := NewServer(catalogTestConfig(), backend.meta, fsblob.New(t.TempDir())).PublicHandler()
			valid := catalogGet(t, handler, "/api/v1/catalog", "")
			if valid.Code != http.StatusOK {
				t.Fatalf("valid control = %d", valid.Code)
			}
			etag := valid.Header().Get("ETag")
			if err := backend.exec(ctx, "UPDATE revocations SET applied_at=? WHERE kind=? AND target_id=?", "not-a-timestamp", "coordinate", skillCoordinate.String()); err != nil {
				t.Fatal(err)
			}
			if got := catalogGet(t, handler, "/api/v1/catalog", "").Code; got != http.StatusInternalServerError {
				t.Fatalf("ordinary read under revocation corruption = %d, want 500", got)
			}
			if got := catalogGet(t, handler, "/api/v1/catalog", etag).Code; got != http.StatusInternalServerError {
				t.Fatalf("matching validator under revocation corruption = %d, want 500", got)
			}
		})

		t.Run(backendName+"/closed store outage", func(t *testing.T) {
			fb := openConditionalBackend(t, backendName)
			seedCatalog(t, ctx, fb.meta, kinds)
			handler := NewServer(catalogTestConfig(), fb.meta, fsblob.New(t.TempDir())).PublicHandler()
			valid := catalogGet(t, handler, "/api/v1/catalog", "")
			etag := valid.Header().Get("ETag")
			if err := fb.meta.Close(); err != nil {
				t.Fatal(err)
			}
			if got := catalogGet(t, handler, "/api/v1/catalog", etag).Code; got == http.StatusNotModified {
				t.Fatal("closed store produced a 304")
			}
		})
	}

	t.Run("missing managed snapshot fails before catalog", func(t *testing.T) {
		meta := newManagedSQLite(t)
		cfg := managedCatalogConfig()
		handler := NewServer(cfg, meta, nil).PublicHandler()
		response := catalogGet(t, handler, "/api/v1/catalog", `"catalog-1-x"`)
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("unadopted managed catalog = %d, want 503", response.Code)
		}
	})
}

// spec: marketplace-catalog-performance/mutation-and-unchanged-version-corruption-cannot-produce-stale-304
// spec: marketplace-catalog-performance/invalid-data-is-rejected-and-valid-sources-remain-sanitized
func TestCatalogConditionalDeferredDigestMismatch(t *testing.T) {
	ctx := context.Background()
	skillCoordinate := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: KindSkill, Name: KindSkill}
	skillDigest := store.RevisionDigest("sha256:" + strings.Repeat("a", 64))
	for _, name := range []string{"sqlite", "postgres"} {
		t.Run(name, func(t *testing.T) {
			backend := openConditionalBackend(t, name)
			seedCatalog(t, ctx, backend.meta, []string{KindSkill, KindCollection})
			handler := NewServer(catalogTestConfig(), backend.meta, fsblob.New(t.TempDir())).PublicHandler()
			valid := catalogGet(t, handler, "/api/v1/catalog", "")
			if valid.Code != http.StatusOK {
				t.Fatalf("valid control = %d: %s", valid.Code, valid.Body.String())
			}
			etag := valid.Header().Get("ETag")
			assessed, err := backend.meta.GetResourceRevision(ctx, skillCoordinate, skillDigest)
			if err != nil {
				t.Fatal(err)
			}
			if err := backend.exec(ctx, fmt.Sprintf("UPDATE %s SET analyzer_version=%s WHERE assessment_digest=%s", backend.table("resource_assessments"), backend.ph(1), backend.ph(2)), "tampered/v2", string(assessed.Assessment.AssessmentDigest)); err != nil {
				t.Fatal(err)
			}
			if got := catalogGet(t, handler, "/api/v1/catalog", "").Code; got != http.StatusInternalServerError {
				t.Fatalf("ordinary read with tampered analyzer_version = %d, want 500", got)
			}
			if got := catalogGet(t, handler, "/api/v1/catalog", etag).Code; got != http.StatusNotModified {
				t.Fatalf("matching validator with deferred digest mismatch = %d, want 304", got)
			}
		})
	}
}

// spec: marketplace-catalog-performance/mutation-and-unchanged-version-corruption-cannot-produce-stale-304
func TestCatalogConditionalInvalidation(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"sqlite", "postgres"} {
		t.Run(name, func(t *testing.T) {
			backend := openConditionalBackend(t, name)
			seedCatalog(t, ctx, backend.meta, []string{KindSkill})
			manifest := store.RevisionDigest("sha256:" + strings.Repeat("f", 64))
			commitA := strings.Repeat("a", 40)
			anchor := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: KindCollection, Name: "bundle"}
			anchorDigest := store.RevisionDigest("sha256:" + strings.Repeat("e", 64))
			anchorRevision := store.ResourceRevision{Coordinate: anchor, ContentDigest: anchorDigest}
			skillCoordinate := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: KindSkill, Name: KindSkill}
			skillDigest := store.RevisionDigest("sha256:" + strings.Repeat("a", 64))
			mustDo := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			mustDo(backend.meta.PutResourceRevision(ctx, anchorRevision))
			mustDo(backend.meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{Coordinate: anchor, ContentDigest: anchorDigest, Commit: commitA, ManifestDigest: manifest}))
			anchorAssessment := testRevisionAssessment(t, anchorRevision, store.AssessmentDecisionAllow)
			mustDo(backend.meta.PutResourceAssessment(ctx, anchorAssessment))
			mustDo(backend.meta.SetCurrentResourceAssessment(ctx, anchorAssessment))

			handler := NewServer(catalogTestConfig(), backend.meta, fsblob.New(t.TempDir())).PublicHandler()
			readPage := func(etag string) (*httptest.ResponseRecorder, CatalogPage) {
				response := catalogGet(t, handler, "/api/v1/catalog", etag)
				if response.Code != http.StatusOK {
					t.Fatalf("read = %d: %s", response.Code, response.Body.String())
				}
				var page CatalogPage
				if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
					t.Fatalf("fresh body unreadable: %v", err)
				}
				return response, page
			}
			etagOf := func() string {
				response, _ := readPage("")
				return response.Header().Get("ETag")
			}
			assertStale := func(old string) CatalogPage {
				t.Helper()
				_, page := readPage(old)
				return page
			}

			etag := etagOf()

			nextCommit := strings.Repeat("b", 40)
			mustDo(backend.meta.PutRegistryOccurrence(ctx, store.RegistryOccurrence{RegistryID: "src", Ref: "main", Commit: nextCommit, RegistryRevisionManifestDigest: manifest, Acquisition: store.AcquisitionMirror, SourceOrigin: "https://git.example/internal/resources.git"}))
			mustDo(backend.meta.SetRegistryHead(ctx, "src", nextCommit, commitA))
			for _, seeded := range []struct {
				coordinate store.ResourceCoordinate
				digest     store.RevisionDigest
			}{{anchor, anchorDigest}, {skillCoordinate, skillDigest}} {
				mustDo(backend.meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{Coordinate: seeded.coordinate, ContentDigest: seeded.digest, Commit: nextCommit, ManifestDigest: manifest}))
			}
			assertStale(etag)
			etag = etagOf()

			mustDo(backend.meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{
				Coordinate: skillCoordinate, ContentDigest: skillDigest, Commit: nextCommit, ManifestDigest: manifest,
				SourcePath: "resources/skill.yaml",
			}))
			page := assertStale(etag)
			var enriched *CatalogResource
			for i := range page.Resources {
				if page.Resources[i].Kind == KindSkill {
					enriched = &page.Resources[i]
				}
			}
			if enriched == nil || enriched.Source == nil || enriched.Source.Path != "resources/skill.yaml" ||
				enriched.Source.RepositoryURL != "https://git.example/internal/resources.git" ||
				enriched.Source.Commit != nextCommit || enriched.Availability != AvailabilityAvailable || enriched.Revision != skillDigest.String() {
				t.Fatalf("enriched skill = %+v", enriched)
			}
			for _, resource := range page.Resources {
				if resource.Availability == AvailabilityRevoked || resource.RegistryID != "src" {
					t.Fatalf("unexpected revocation or unlisted row: %+v", resource)
				}
			}
			etag = etagOf()

			current, err := backend.meta.GetResourceRevision(ctx, skillCoordinate, skillDigest)
			if err != nil {
				t.Fatal(err)
			}
			changed := current.Assessment
			changed.AssessmentDigest = ""
			changed.Decision = store.AssessmentDecisionWarn
			changed.AssessedAt = time.Unix(2, 0).UTC()
			reassessed, err := store.FinalizeResourceAssessment(changed)
			if err != nil {
				t.Fatal(err)
			}
			mustDo(backend.meta.PutResourceAssessment(ctx, reassessed))
			mustDo(backend.meta.SetCurrentResourceAssessment(ctx, reassessed))
			assertStale(etag)
			etag = etagOf()

			mustDo(backend.meta.PutCollectionRevision(ctx, store.CollectionRevision{
				ID: anchorDigest.String(), RegistryID: "src", ManifestDigest: anchorDigest,
				Members: []store.ResourceCoordinate{skillCoordinate},
			}))
			page = assertStale(etag)
			var collection *CatalogResource
			for i := range page.Resources {
				if page.Resources[i].Kind == KindCollection {
					collection = &page.Resources[i]
				}
			}
			if collection == nil || collection.Collection == nil || len(collection.Collection.Resources) != 1 ||
				collection.Collection.Resources[0].Kind != KindSkill || collection.Collection.Resources[0].Name != KindSkill {
				t.Fatalf("collection members = %+v", collection)
			}
			etag = etagOf()

			registry, err := backend.meta.GetRegistry(ctx, "src")
			if err != nil {
				t.Fatal(err)
			}
			registry.Listed = false
			mustDo(backend.meta.PutRegistry(ctx, registry))
			assertStale(etag)
			registry.Listed = true
			mustDo(backend.meta.PutRegistry(ctx, registry))
			etag = etagOf()

			pubCommit := strings.Repeat("d", 40)
			pubManifest := store.RevisionDigest("sha256:" + strings.Repeat("9", 64))
			newSkillDigest := store.RevisionDigest("sha256:" + strings.Repeat("0", 64))
			newSkillRevision := store.ResourceRevision{Coordinate: skillCoordinate, ContentDigest: newSkillDigest, License: "MIT"}
			newSkillAssessment := testRevisionAssessment(t, newSkillRevision, store.AssessmentDecisionAllow)
			publication := store.RegistryRevisionPublication{
				PublicationID:      "publication-invalidation",
				ExpectedHeadCommit: nextCommit,
				Occurrence:         store.RegistryOccurrence{RegistryID: "src", Ref: "main", Commit: pubCommit, RegistryRevisionManifestDigest: pubManifest, Acquisition: store.AcquisitionMirror},
				Resources:          []store.ResourceRevision{anchorRevision, newSkillRevision},
				ResourceOccurrences: []store.ResourceOccurrence{
					{Coordinate: anchor, ContentDigest: anchorDigest, Commit: pubCommit, ManifestDigest: pubManifest},
					{Coordinate: skillCoordinate, ContentDigest: newSkillDigest, Commit: pubCommit, ManifestDigest: pubManifest},
				},
				Assessments: []store.ResourceAssessment{anchorAssessment, newSkillAssessment},
			}
			mustDo(backend.meta.(store.PublicationStore).PublishRegistryPublication(ctx, publication))
			page = assertStale(etag)
			seen := map[string]string{}
			for _, resource := range page.Resources {
				seen[resource.Kind+"/"+resource.Name] = resource.Revision
			}
			if len(page.Resources) != 2 || seen["collection/bundle"] != anchorDigest.String() || seen["skill/skill"] != newSkillDigest.String() {
				t.Fatalf("post-publication page = %+v", page.Resources)
			}
			etag = etagOf()

			mustDo(backend.meta.PutRevocation(ctx, store.Revocation{Kind: store.RevocationCoordinate, TargetID: skillCoordinate.String(), Reason: "fixture"}))
			assertStale(etag)
			etag = etagOf()

			restricted := catalogTestConfig()
			restricted.Revocations = config.Revocations{Coordinates: []string{skillCoordinate.String()}}
			other := NewServer(restricted, backend.meta, fsblob.New(t.TempDir())).PublicHandler()
			response := catalogGet(t, other, "/api/v1/catalog", "")
			if response.Code != http.StatusOK {
				t.Fatalf("restricted control = %d", response.Code)
			}
			if response.Header().Get("ETag") == etag {
				t.Fatal("configured revocations produced an unchanged ETag")
			}
		})
	}

	t.Run("managed policy and withdrawal change the validator", func(t *testing.T) {
		underlying, _, cfg, _ := seedManagedCatalogFixture(t, ctx)
		if !underlying.OwnsCatalogProjectionStore(underlying) {
			t.Fatal("root store does not confirm its own conditional marker")
		}
		handler := NewServer(cfg, underlying, nil).PublicHandler()

		first := catalogGet(t, handler, "/api/v1/catalog", "")
		if first.Code != http.StatusOK {
			t.Fatalf("managed control = %d: %s", first.Code, first.Body.String())
		}
		etag := first.Header().Get("ETag")
		matching := catalogGet(t, handler, "/api/v1/catalog", etag)
		if matching.Code != http.StatusNotModified || matching.Body.Len() != 0 {
			t.Fatalf("root store conditional = %d, want bodyless 304", matching.Code)
		}

		current, err := underlying.CurrentManagedConfiguration(ctx, "market")
		if err != nil {
			t.Fatal(err)
		}
		var snapshot admin.Snapshot
		if err := json.Unmarshal(current.Snapshot, &snapshot); err != nil {
			t.Fatal(err)
		}
		snapshot.PolicyRevision = 2
		snapshot.LicensePolicy.Allowlist = []string{"Apache-2.0"}
		second := managedChangeRequest(1, "policy-tighten")
		second.Change.Snapshot, err = json.Marshal(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		accepted, err := underlying.ApplyManagedConfiguration(ctx, second)
		if err != nil {
			t.Fatal(err)
		}
		if accepted.Revision != 2 {
			t.Fatalf("managed revision = %d, want 2", accepted.Revision)
		}
		response := catalogGet(t, handler, "/api/v1/catalog", etag)
		if response.Code != http.StatusOK {
			t.Fatalf("managed policy change left validator matching: %d", response.Code)
		}
		if next := response.Header().Get("ETag"); next == etag || !strings.Contains(next, "-managed-2\"") {
			t.Fatalf("updated ETag = %q, want managed-2", next)
		}
		blocked := catalogPage(t, response)
		if len(blocked.Resources) != 1 || blocked.Resources[0].Availability != AvailabilityBlocked {
			t.Fatalf("policy-tightened page = %+v", blocked.Resources)
		}
		secondETag := response.Header().Get("ETag")

		var withdrawn admin.Snapshot
		if err := json.Unmarshal(accepted.Snapshot, &withdrawn); err != nil {
			t.Fatal(err)
		}
		for i := range withdrawn.Registries {
			withdrawn.Registries[i].Generation++
			withdrawn.Registries[i].Lifecycle = "withdrawn"
			withdrawn.Registries[i].Withdrawn = true
		}
		third := managedChangeRequest(2, "withdraw")
		third.Change.Snapshot, err = json.Marshal(withdrawn)
		if err != nil {
			t.Fatal(err)
		}
		accepted, err = underlying.ApplyManagedConfiguration(ctx, third)
		if err != nil {
			t.Fatal(err)
		}
		if accepted.Revision != 3 {
			t.Fatalf("withdrawal revision = %d, want 3", accepted.Revision)
		}
		response = catalogGet(t, handler, "/api/v1/catalog", secondETag)
		if response.Code != http.StatusOK {
			t.Fatalf("managed withdrawal left validator matching: %d", response.Code)
		}
		if next := response.Header().Get("ETag"); !strings.Contains(next, "-managed-3\"") {
			t.Fatalf("withdrawn ETag = %q, want managed-3", next)
		}
		revoked := catalogPage(t, response)
		if len(revoked.Resources) != 1 || revoked.Resources[0].Availability != AvailabilityRevoked {
			t.Fatalf("withdrawn page = %+v", revoked.Resources)
		}
	})
}

// spec: marketplace-catalog-performance/reads-do-not-mutate-authoritative-state-or-escape-the-marketplace
func TestCatalogReadOnlyScope(t *testing.T) {
	ctx := context.Background()
	tables := []string{"marketplaces", "registries", "registry_occurrences", "registry_heads", "resource_revisions", "resource_occurrences", "resource_assessments", "current_resource_occurrences", "current_resource_assessments", "collection_revisions", "revocations", "catalog_versions"}
	for _, name := range []string{"sqlite", "postgres"} {
		t.Run(name, func(t *testing.T) {
			backend := openConditionalBackend(t, name)
			seedCatalog(t, ctx, backend.meta, []string{KindSkill})
			if got, err := backend.queryInt(ctx, "SELECT 7"); err != nil || got != 7 {
				t.Fatalf("queryInt self-check = %d, %v, want 7", got, err)
			}
			other := store.ResourceCoordinate{Marketplace: "other-mp", Registry: "other-src", Kind: KindSkill, Name: "foreign"}
			mustDo := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			mustDo(backend.meta.PutMarketplace(ctx, store.Marketplace{ID: "other-mp", Name: "Other", CanonicalURL: "https://other.test"}))
			mustDo(backend.meta.PutRegistry(ctx, store.Registry{ID: "other-src", Slug: "other", Ref: "main", Acquisition: store.AcquisitionMirror, Listed: true}))
			mustDo(backend.meta.PutRegistryOccurrence(ctx, store.RegistryOccurrence{RegistryID: "other-src", Ref: "main", Commit: "cccccccccccccccccccccccccccccccccccccccc", RegistryRevisionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("f", 64)), Acquisition: store.AcquisitionMirror}))
			mustDo(backend.meta.PutResourceRevision(ctx, store.ResourceRevision{Coordinate: other, ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("7", 64))}))
			mustDo(backend.meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{Coordinate: other, ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("7", 64)), Commit: "cccccccccccccccccccccccccccccccccccccccc"}))
			otherRevision := store.ResourceRevision{Coordinate: other, ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("7", 64))}
			assessment := testRevisionAssessment(t, otherRevision, store.AssessmentDecisionAllow)
			mustDo(backend.meta.PutResourceAssessment(ctx, assessment))
			mustDo(backend.meta.SetCurrentResourceAssessment(ctx, assessment))
			mustDo(backend.meta.SetRegistryHead(ctx, "other-src", "cccccccccccccccccccccccccccccccccccccccc", ""))

			snapshot := func() map[string][]string {
				out := map[string][]string{}
				for _, name := range tables {
					out[name] = backend.tableRows(t, ctx, name)
				}
				version, err := backend.queryInt(ctx, fmt.Sprintf("SELECT version FROM %s WHERE singleton=1", backend.table("catalog_versions")))
				if err != nil {
					t.Fatal(err)
				}
				out["catalog_version"] = []string{fmt.Sprint(version)}
				return out
			}
			before := snapshot()
			if len(before["resource_revisions"]) != 2 {
				t.Fatalf("before resource_revisions = %d rows, want 2 known resources", len(before["resource_revisions"]))
			}
			if v, _ := strconv.Atoi(before["catalog_version"][0]); v <= 0 {
				t.Fatalf("before catalog version = %q", before["catalog_version"][0])
			}

			blobs := &countingBlobStore{BlobStore: fsblob.New(t.TempDir())}
			handler := NewServer(catalogTestConfig(), backend.meta, blobs).PublicHandler()
			ok := catalogGet(t, handler, "/api/v1/catalog", "")
			if ok.Code != http.StatusOK {
				t.Fatalf("success = %d", ok.Code)
			}
			var page CatalogPage
			if err := json.Unmarshal(ok.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if len(page.Resources) == 0 {
				t.Fatal("own marketplace page is empty")
			}
			for _, resource := range page.Resources {
				if resource.Name == "foreign" {
					t.Fatal("catalog leaked the other Marketplace's resource")
				}
			}
			if got := catalogGet(t, handler, "/api/v1/catalog", ok.Header().Get("ETag")).Code; got != http.StatusNotModified {
				t.Fatalf("conditional = %d", got)
			}
			if got := catalogGet(t, handler, "/api/v1/catalog?limit=nope", "").Code; got != http.StatusBadRequest {
				t.Fatalf("rejected = %d", got)
			}
			cancelled, cancel := context.WithCancel(context.Background())
			cancel()
			failReq := httptest.NewRequest(http.MethodGet, "/api/v1/catalog", nil).WithContext(cancelled)
			failResponse := httptest.NewRecorder()
			handler.ServeHTTP(failResponse, failReq)
			if failResponse.Code != http.StatusServiceUnavailable {
				t.Fatalf("failed request = %d, want 503", failResponse.Code)
			}
			if blobs.gets != 0 {
				t.Fatalf("catalog requests read %d blobs", blobs.gets)
			}
			if blobs.puts != 0 || blobs.deletes != 0 {
				t.Fatalf("catalog requests wrote blobs: puts=%d deletes=%d", blobs.puts, blobs.deletes)
			}
			if after := snapshot(); !reflect.DeepEqual(after, before) {
				t.Fatalf("declared tables changed: before=%v after=%v", before, after)
			}
			foreign, err := backend.meta.ReadCatalogProjectionPage(ctx, store.CatalogProjectionPageRequest{Marketplace: "other-mp", Limit: 10})
			if err != nil || len(foreign.Resources) != 1 || foreign.Resources[0].Coordinate.Name != "foreign" {
				t.Fatalf("other marketplace page = %+v, %v", foreign, err)
			}
		})
	}
}
