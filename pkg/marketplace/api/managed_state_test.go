package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/admin"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/postgres"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

type overrideManagedStore struct {
	*sqlite.Store
	err          error
	snapshot     json.RawMessage
	managedReads atomic.Int64
	catalogReads atomic.Int64
}

func (s *overrideManagedStore) CurrentManagedConfiguration(ctx context.Context, id store.MarketplaceID) (store.ManagedChange, error) {
	s.managedReads.Add(1)
	change, err := s.Store.CurrentManagedConfiguration(ctx, id)
	if s.err != nil {
		return store.ManagedChange{}, s.err
	}
	if s.snapshot != nil {
		change.Snapshot = s.snapshot
	}
	return change, err
}

func (s *overrideManagedStore) ReadCatalogProjectionPage(ctx context.Context, req store.CatalogProjectionPageRequest) (store.CatalogProjectionPage, error) {
	s.catalogReads.Add(1)
	return s.Store.ReadCatalogProjectionPage(ctx, req)
}

type gatingManagedStore struct {
	*sqlite.Store
	reads    atomic.Int64
	gated    atomic.Bool
	captured chan struct{}
	release  chan struct{}
}

func (s *gatingManagedStore) CurrentManagedConfiguration(ctx context.Context, id store.MarketplaceID) (store.ManagedChange, error) {
	s.reads.Add(1)
	change, err := s.Store.CurrentManagedConfiguration(ctx, id)
	if err == nil && s.gated.CompareAndSwap(false, true) {
		close(s.captured)
		select {
		case <-s.release:
		case <-ctx.Done():
		}
	}
	return change, err
}

type capabilityLimitedStore struct {
	store.MetadataStore
	change store.ManagedChange
}

func (s *capabilityLimitedStore) CurrentManagedConfiguration(context.Context, store.MarketplaceID) (store.ManagedChange, error) {
	return s.change, nil
}

type poolStatsStore struct {
	store.MetadataStore
	stats sql.DBStats
}

func (s *poolStatsStore) PoolStats() sql.DBStats { return s.stats }

func managedCatalogConfig() config.Config {
	return config.Config{
		Instance:          config.Instance{ID: "market", Name: "Market", CanonicalURL: "https://marketplace.test"},
		CatalogProjection: config.CatalogProjection{Enabled: true},
		Policy:            config.Policy{LicenseAllowlist: []string{"MIT"}, LicenseUndeclared: "reject"},
		Management:        config.Management{Mode: "database"},
	}
}

func newManagedSQLitePath(t *testing.T) (*sqlite.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "metadata.db")
	meta := sqlite.New(path)
	if err := meta.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	return meta, path
}

func newManagedSQLite(t *testing.T) *sqlite.Store {
	t.Helper()
	meta, _ := newManagedSQLitePath(t)
	return meta
}

func seedManagedCatalogFixture(t *testing.T, ctx context.Context) (*sqlite.Store, string, config.Config, store.AssessedResourceRevision) {
	t.Helper()
	cfg := managedCatalogConfig()
	cfg.Registries = []config.Registry{{ID: "reg", Slug: "source", Origin: "https://git.example/internal/resources.git", Ref: "main", Acquisition: "mirror", License: "MIT", Roots: []config.RegistryRoot{{Path: "."}}}}
	cfg.Management.AllowedOrigins = []string{"https://git.example"}
	underlying, path := newManagedSQLitePath(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(underlying.PutMarketplace(ctx, store.Marketplace{ID: "market", Name: "Market", CanonicalURL: "https://marketplace.test"}))
	must(underlying.PutRegistry(ctx, store.Registry{ID: "reg", Slug: "source", Origins: []string{"https://git.example/internal/resources.git"}, Ref: "main", Listed: true, Acquisition: store.AcquisitionMirror, License: "MIT", Roots: []store.RegistryRoot{{Path: "."}}}))
	manifest := store.RevisionDigest("sha256:" + strings.Repeat("f", 64))
	commit := strings.Repeat("e", 40)
	must(underlying.PutRegistryOccurrence(ctx, store.RegistryOccurrence{RegistryID: "reg", Ref: "main", Commit: commit, RegistryRevisionManifestDigest: manifest, Acquisition: store.AcquisitionMirror, SourceOrigin: "https://git.example/internal/resources.git", OccurredAt: time.Unix(1, 0).UTC()}))
	revision := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "market", Registry: "reg", Kind: KindSkill, Name: "example"},
		ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("a", 64)),
		ArchiveDigest: store.ArchiveDigest("sha256:" + strings.Repeat("b", 64)),
		License:       "MIT",
	}
	assessed := testAssessedRevision(t, revision, store.AssessmentDecisionAllow)
	must(underlying.PutResourceRevision(ctx, revision))
	must(underlying.PutResourceOccurrence(ctx, assessed.Occurrence))
	must(underlying.PutResourceAssessment(ctx, assessed.Assessment))
	must(underlying.SetCurrentResourceAssessment(ctx, assessed.Assessment))
	must(underlying.SetRegistryHead(ctx, "reg", commit, ""))
	adoptManagedConfiguration(t, underlying, cfg)
	return underlying, path, cfg, assessed
}

func adoptManagedConfiguration(t *testing.T, meta store.MetadataStore, cfg config.Config) {
	t.Helper()
	adopt := cfg
	adopt.Management.Mode = ""
	service := admin.Service{Metadata: meta, Deployment: adopt.Management}
	if _, err := service.Adopt(context.Background(), adopt, admin.AdoptionRequest{
		Actor:  store.ManagedActor{Issuer: "test-proxy", Subject: "operator"},
		Reason: "Adopt approved catalog settings", RequestID: "request-adopt", IdempotencyKey: "adopt",
	}); err != nil {
		t.Fatal(err)
	}
}

func managedChangeRequest(parent int64, key string) store.ManagedChangeRequest {
	return store.ManagedChangeRequest{Change: store.ManagedChange{
		Marketplace: "market", ParentRevision: parent,
		Actor:     store.ManagedActor{Issuer: "test-proxy", Subject: "operator"},
		Operation: "configuration.apply", Reason: "Update approved catalog settings",
		RequestID: "request-" + key, IdempotencyKey: key, RequestDigest: "digest-" + key,
		Snapshot: json.RawMessage(`{"version":1,"policy_revision":1,"registries":[],"license_policy":{"allowlist":["MIT"],"undeclared":"reject"}}`),
	}, Jobs: []store.Job{{ID: store.JobID("job-" + key), Kind: "managed-reconcile", Payload: []byte(`{}`), IdempotencyKey: "work-" + key}}}
}

func catalogPage(t *testing.T, response *httptest.ResponseRecorder) CatalogPage {
	t.Helper()
	var page CatalogPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatalf("catalog page: %d %s", response.Code, response.Body.String())
	}
	return page
}

func TestPublicManagedStateSingleRead(t *testing.T) {
	cfg := managedCatalogConfig()
	underlying := newManagedSQLite(t)
	adoptManagedConfiguration(t, underlying, cfg)
	meta := &overrideManagedStore{Store: underlying}
	handler := NewServer(cfg, meta, nil).PublicHandler()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/catalog?limit=1", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("catalog status = %d: %s", response.Code, response.Body.String())
	}
	if etag := response.Header().Get("ETag"); !strings.Contains(etag, "-managed-1\"") {
		t.Fatalf("ETag %q does not carry managed revision 1", etag)
	}
	if reads := meta.managedReads.Load(); reads != 1 {
		t.Fatalf("managed configuration reads = %d, want exactly 1", reads)
	}
	if reads := meta.catalogReads.Load(); reads != 1 {
		t.Fatalf("catalog page reads = %d, want exactly 1", reads)
	}
}

// spec: marketplace-catalog-performance/concurrent-policy-changes-preserve-request-isolation
func TestPublicManagedStateRequestIsolation(t *testing.T) {
	ctx := context.Background()
	underlying, _, cfg, _ := seedManagedCatalogFixture(t, ctx)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	meta := &gatingManagedStore{Store: underlying, captured: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(func() {
		select {
		case <-meta.release:
		default:
			close(meta.release)
		}
	})
	handler := NewServer(cfg, meta, nil).PublicHandler()

	type outcome struct {
		code int
		etag string
		page CatalogPage
		err  error
	}
	firstResult := make(chan outcome, 1)
	go func() {
		requestCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/catalog?limit=1", nil).WithContext(requestCtx))
		var page CatalogPage
		decodeErr := json.Unmarshal(response.Body.Bytes(), &page)
		firstResult <- outcome{response.Code, response.Header().Get("ETag"), page, decodeErr}
	}()

	select {
	case <-meta.captured:
	case <-time.After(10 * time.Second):
		t.Fatal("first request did not read managed configuration")
	}

	current, err := underlying.CurrentManagedConfiguration(ctx, "market")
	must(err)
	var snapshot admin.Snapshot
	must(json.Unmarshal(current.Snapshot, &snapshot))
	snapshot.PolicyRevision = 2
	snapshot.LicensePolicy.Allowlist = []string{"Apache-2.0"}
	second := managedChangeRequest(1, "second")
	second.Change.Snapshot, err = json.Marshal(snapshot)
	must(err)
	accepted, err := underlying.ApplyManagedConfiguration(ctx, second)
	must(err)
	if accepted.Revision != 2 {
		t.Fatalf("accepted revision = %d, want 2", accepted.Revision)
	}
	var acceptedSnapshot admin.Snapshot
	must(json.Unmarshal(accepted.Snapshot, &acceptedSnapshot))
	if acceptedSnapshot.PolicyRevision != 2 {
		t.Fatalf("accepted policy revision = %d, want 2", acceptedSnapshot.PolicyRevision)
	}

	fresh := httptest.NewRecorder()
	handler.ServeHTTP(fresh, httptest.NewRequest(http.MethodGet, "/api/v1/catalog?limit=1", nil))
	freshPage := catalogPage(t, fresh)
	if fresh.Code != http.StatusOK || len(freshPage.Resources) != 1 || freshPage.Resources[0].Availability != AvailabilityBlocked {
		t.Fatalf("fresh request = %d %+v, want 200 with one blocked resource", fresh.Code, freshPage.Resources)
	}
	if !strings.Contains(fresh.Header().Get("ETag"), "-managed-2\"") {
		t.Fatalf("fresh ETag %q does not carry managed revision 2", fresh.Header().Get("ETag"))
	}

	close(meta.release)
	select {
	case first := <-firstResult:
		if first.err != nil {
			t.Fatalf("in-flight page decode: %v", first.err)
		}
		if first.code != http.StatusOK || len(first.page.Resources) != 1 || first.page.Resources[0].Availability != AvailabilityAvailable {
			t.Fatalf("in-flight request = %d %+v, want 200 with one available resource", first.code, first.page.Resources)
		}
		if !strings.Contains(first.etag, "-managed-1\"") {
			t.Fatalf("in-flight ETag %q does not carry managed revision 1", first.etag)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("first request did not complete after release")
	}

	if reads := meta.reads.Load(); reads != 2 {
		t.Fatalf("managed configuration reads = %d, want one per request", reads)
	}
}

// spec: marketplace-catalog-performance/unavailable-or-invalid-authority-rejects-public-requests
func TestPublicAuthorityFailureModes(t *testing.T) {
	cfg := managedCatalogConfig()
	ordinaryPaths := []string{"/api/v1/catalog", "/api/v1/capabilities"}

	assertRejected := func(t *testing.T, server *Server) {
		t.Helper()
		for _, path := range ordinaryPaths {
			response := httptest.NewRecorder()
			server.PublicHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf("%s = %d, want 503", path, response.Code)
			}
			if response.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatalf("%s cache = %q", path, response.Header().Get("Cache-Control"))
			}
		}
	}

	t.Run("missing adoption", func(t *testing.T) {
		meta := &overrideManagedStore{Store: newManagedSQLite(t)}
		server := NewServer(cfg, meta, nil)
		assertRejected(t, server)
		if reads := meta.catalogReads.Load(); reads != 0 {
			t.Fatalf("rejected requests reached catalog page reads = %d", reads)
		}
		before := meta.managedReads.Load()
		health := httptest.NewRecorder()
		server.PublicHandler().ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if health.Code != http.StatusOK || meta.managedReads.Load() != before {
			t.Fatalf("liveness = %d with %d extra reads", health.Code, meta.managedReads.Load()-before)
		}
		if err := server.Start(context.Background()); !errors.Is(err, store.ErrManagedAdoptionRequired) {
			t.Fatalf("start = %v, want adoption error", err)
		}
		ready := httptest.NewRecorder()
		server.OpsHandler().ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		if ready.Code != http.StatusServiceUnavailable {
			t.Fatalf("readiness = %d", ready.Code)
		}
	})

	t.Run("unsupported safety capability", func(t *testing.T) {
		meta := &capabilityLimitedStore{change: store.ManagedChange{Marketplace: "market", Revision: 1}}
		server := NewServer(cfg, meta, nil)
		assertRejected(t, server)
		if err := server.Start(context.Background()); !errors.Is(err, store.ErrManagedRuntimeUnavailable) {
			t.Fatalf("start = %v, want capability error", err)
		}
	})

	t.Run("corrupt record and storage error", func(t *testing.T) {
		for _, failure := range []error{store.ErrManagedStateCorrupt, errors.New("database unavailable")} {
			underlying := newManagedSQLite(t)
			adoptManagedConfiguration(t, underlying, cfg)
			meta := &overrideManagedStore{Store: underlying, err: failure}
			server := NewServer(cfg, meta, nil)
			assertRejected(t, server)
			admin := httptest.NewRecorder()
			server.PublicHandler().ServeHTTP(admin, httptest.NewRequest(http.MethodGet, "/api/v1/admin/configuration", nil))
			if admin.Code != http.StatusServiceUnavailable {
				t.Fatalf("admin under %v = %d, want authority-guarded 503", failure, admin.Code)
			}
			if reads := meta.catalogReads.Load(); reads != 0 {
				t.Fatalf("%v reached catalog page reads = %d", failure, reads)
			}
		}
	})

	t.Run("corrupt snapshot", func(t *testing.T) {
		underlying := newManagedSQLite(t)
		adoptManagedConfiguration(t, underlying, cfg)
		meta := &overrideManagedStore{Store: underlying, snapshot: json.RawMessage(`{"version":0}`)}
		server := NewServer(cfg, meta, nil)
		assertRejected(t, server)
		if reads := meta.catalogReads.Load(); reads != 0 {
			t.Fatalf("corrupt snapshot reached catalog page reads = %d", reads)
		}
	})
}

// spec: marketplace-catalog-performance/early-failures-remain-observable-without-sensitive-labels
func TestPublicStackMetrics(t *testing.T) {
	ctx := context.Background()
	underlying, dbPath, cfg, assessed := seedManagedCatalogFixture(t, ctx)
	meta := &overrideManagedStore{Store: underlying}
	server := NewServer(cfg, meta, nil)
	public, ops := server.PublicHandler(), server.OpsHandler()

	get := func(path string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		public.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		return response
	}

	if response := get("/api/v1/catalog?limit=1"); response.Code != http.StatusOK {
		t.Fatalf("healthy catalog = %d: %s", response.Code, response.Body.String())
	}
	const querySecret = "raw-query-secret-123"
	if response := get("/api/v1/capabilities?token=" + querySecret); response.Code != http.StatusOK {
		t.Fatalf("healthy capabilities = %d", response.Code)
	}
	const rawPath = "/private/raw-path-secret-123"
	if response := get(rawPath); response.Code != http.StatusNotFound {
		t.Fatalf("unmatched = %d", response.Code)
	}
	if response := get("/api/v1/resources"); response.Code != http.StatusTemporaryRedirect {
		t.Fatalf("redirect = %d", response.Code)
	}
	for _, path := range []string{"/readyz", "/metrics", "/debug/pprof/"} {
		if response := get(path); response.Code != http.StatusNotFound {
			t.Fatalf("public %s = %d", path, response.Code)
		}
	}

	meta.err = errors.New("database unavailable")
	if response := get("/api/v1/catalog"); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("authority outage = %d", response.Code)
	}
	meta.err = nil
	meta.snapshot = json.RawMessage(`{"version":0}`)
	if response := get("/api/v1/catalog"); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("corrupt availability = %d", response.Code)
	}
	meta.snapshot = nil

	raw, err := sql.Open("sqlite", "file:"+url.PathEscape(dbPath)+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(ctx, "UPDATE resource_assessments SET findings=? WHERE assessment_digest=?", "{", string(assessed.Assessment.AssessmentDigest)); err != nil {
		t.Fatal(err)
	}
	if response := get("/api/v1/catalog"); response.Code != http.StatusInternalServerError {
		t.Fatalf("corrupt findings = %d, want 500", response.Code)
	}
	_ = raw.Close()

	scrape := httptest.NewRecorder()
	ops.ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if scrape.Code != http.StatusOK {
		t.Fatalf("scrape = %d", scrape.Code)
	}
	if strings.Contains(scrape.Body.String(), "raw-path-secret") || strings.Contains(scrape.Body.String(), querySecret) {
		t.Fatal("scrape contains a raw path or query secret")
	}
	families := parseMetricFamilies(t, scrape.Body.Bytes())

	stack := requireMetricFamily(t, families, "agent_marketplace_public_request_duration_seconds")
	if metric := requireMetric(t, stack, map[string]string{"method": "get", "route": "/api/v1/catalog", "code": "200"}); metric.GetHistogram().GetSampleCount() != 1 {
		t.Fatalf("healthy catalog samples = %d", metric.GetHistogram().GetSampleCount())
	}
	if metric := requireMetric(t, stack, map[string]string{"method": "get", "route": "/api/v1/catalog", "code": "503"}); metric.GetHistogram().GetSampleCount() != 2 {
		t.Fatalf("failed catalog samples = %d, want 2", metric.GetHistogram().GetSampleCount())
	}
	if metric := requireMetric(t, stack, map[string]string{"method": "get", "route": "/api/v1/catalog", "code": "500"}); metric.GetHistogram().GetSampleCount() != 1 {
		t.Fatalf("corrupt catalog samples = %d, want 1", metric.GetHistogram().GetSampleCount())
	}
	if metric := requireMetric(t, stack, map[string]string{"method": "get", "route": "/api/v1/resources/", "code": "307"}); metric.GetHistogram().GetSampleCount() != 1 {
		t.Fatalf("redirect samples = %d", metric.GetHistogram().GetSampleCount())
	}
	if metric := requireMetric(t, stack, map[string]string{"method": "get", "route": unmatchedRoute, "code": "404"}); metric.GetHistogram().GetSampleCount() != 2 {
		t.Fatalf("unmatched samples = %d, want 2", metric.GetHistogram().GetSampleCount())
	}

	phases := requireMetricFamily(t, families, "agent_marketplace_request_phase_duration_seconds")
	phaseSamples := func(phase string) uint64 {
		return requireMetric(t, phases, map[string]string{"phase": phase}).GetHistogram().GetSampleCount()
	}
	if got := phaseSamples("catalog_read"); got != 2 {
		t.Fatalf("catalog_read samples = %d, want 2", got)
	}
	if got := phaseSamples("catalog_availability"); got != 1 {
		t.Fatalf("catalog_availability samples = %d", got)
	}
	if got := phaseSamples("catalog_response"); got != 1 {
		t.Fatalf("catalog_response samples = %d", got)
	}
	if got := phaseSamples("authority"); got != 10 {
		t.Fatalf("authority samples = %d, want 10", got)
	}
	if got := phaseSamples("availability"); got != 9 {
		t.Fatalf("availability samples = %d, want 9", got)
	}

	requests := requireMetricFamily(t, families, "agent_marketplace_requests_total")
	for _, metric := range requests.GetMetric() {
		for _, label := range metric.GetLabel() {
			if label.GetName() == "code" && label.GetValue() == "503" {
				t.Fatal("mux request counter observed an early failure")
			}
		}
	}
	requireMetric(t, requests, map[string]string{"listener": publicListener, "method": "get", "route": "/api/v1/catalog", "code": "200"})
}

func TestMetadataPoolWaitMetrics(t *testing.T) {
	server := NewServer(config.Config{Instance: config.Instance{ID: "market"}}, &poolStatsStore{
		stats: sql.DBStats{WaitCount: 2, WaitDuration: 4 * time.Millisecond},
	}, nil)
	scrape := httptest.NewRecorder()
	server.OpsHandler().ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	families := parseMetricFamilies(t, scrape.Body.Bytes())
	if value := requireMetric(t, requireMetricFamily(t, families, "agent_marketplace_metadata_pool_waits_total"), nil).GetCounter().GetValue(); value != 2 {
		t.Fatalf("pool waits = %v, want 2", value)
	}
	if value := requireMetric(t, requireMetricFamily(t, families, "agent_marketplace_metadata_pool_wait_seconds_total"), nil).GetCounter().GetValue(); value != 0.004 {
		t.Fatalf("pool wait seconds = %v, want 0.004", value)
	}
}

func TestPoolStatsRealStores(t *testing.T) {
	ctx := context.Background()
	poolStats := func(t *testing.T, meta store.MetadataStore) sql.DBStats {
		pool, ok := meta.(interface{ PoolStats() sql.DBStats })
		if !ok {
			t.Fatal("store does not expose PoolStats")
		}
		return pool.PoolStats()
	}

	t.Run("sqlite", func(t *testing.T) {
		meta := newManagedSQLite(t)
		if stats := poolStats(t, meta); stats.OpenConnections < 1 {
			t.Fatalf("opened sqlite pool stats = %+v", stats)
		}
	})

	t.Run("postgres", func(t *testing.T) {
		dsn := os.Getenv("POSTGRES_DSN")
		if dsn == "" {
			t.Skip("POSTGRES_DSN is required for the PostgreSQL pool smoke")
		}
		meta := postgres.NewWithSchema(dsn, "pool_stats_smoke")
		if stats := poolStats(t, meta); stats != (sql.DBStats{}) {
			t.Fatalf("unopened postgres pool stats = %+v, want zero", stats)
		}
		if err := meta.Open(ctx); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = meta.Close() })
		if stats := poolStats(t, meta); stats.OpenConnections < 1 {
			t.Fatalf("opened postgres pool stats = %+v", stats)
		}
	})
}
