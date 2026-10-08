package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

type adoptionSearchResult struct {
	Items []struct {
		RegistryID        string `json:"registry_id"`
		Kind              string `json:"kind"`
		Name              string `json:"name"`
		InstallationCount *int64 `json:"installation_count"`
	} `json:"items"`
	Total int `json:"total"`
}

func adoptionSearchFixture(t *testing.T, samples []inventoryMetricSample) (*httptest.Server, *metricsFixture) {
	t.Helper()
	public, fixture, _, _ := adoptionSearchSetup(t, samples, true)
	return public, fixture
}

func adoptionSearchSetup(t *testing.T, samples []inventoryMetricSample, seed ...bool) (*httptest.Server, *metricsFixture, *Server, *sqlite.Store) {
	t.Helper()
	ctx := context.Background()

	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = meta.Close() })

	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "mp", Name: "Marketplace"}); err != nil {
		t.Fatal(err)
	}
	registries := []store.Registry{
		{ID: "rA", Slug: "rA", Listed: true, Acquisition: store.AcquisitionMirror, Origins: []string{"https://example.test/team/alpha.git"}},
		{ID: "rB", Slug: "rB", Listed: true, Acquisition: store.AcquisitionMirror, Origins: []string{"https://example.test/team/beta.git"}},
		{ID: "rL", Slug: "rL", Listed: true, Acquisition: store.AcquisitionMirror, Origins: []string{"file:///srv/private-registry"}},
	}
	for _, registry := range registries {
		if err := meta.PutRegistry(ctx, registry); err != nil {
			t.Fatal(err)
		}
	}
	resources := []struct{ registry, name string }{
		{"rA", "alpha"},
		{"rA", "preview"},
		{"rB", "beta"},
		{"rB", "zulu"},
		{"rB", "preview-extra"},
		{"rL", "local-only"},
	}
	for i, resource := range resources {
		publishAssessedTestRevision(ctx, t, meta, store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: store.RegistryID(resource.registry), Kind: KindSkill, Name: resource.name},
			ContentDigest: store.RevisionDigest(digestOf(fmt.Sprintf("%x", i))),
			ArchiveDigest: store.ArchiveDigest(digestOf(fmt.Sprintf("%x", i+6))),
		}, store.AssessmentDecisionAllow, store.RevisionDigest(digestOf("d")), strings.Repeat("e", 40))
	}

	fixture := newMetricsFixture(t, func() string { return prometheusVector(samples, adoptionFixedNow) })
	cfg := config.Config{
		Instance:            config.Instance{ID: "mp", Name: "Marketplace", CanonicalURL: "https://example.com"},
		Listen:              "127.0.0.1:0",
		OperationsListen:    "127.0.0.1:0",
		InstallationMetrics: config.InstallationMetrics{URL: fixture.server.URL},
	}
	server := NewServer(cfg, meta, fsblob.New(t.TempDir()))
	server.installations.now = func() time.Time { return adoptionFixedNow }
	if len(seed) > 0 && seed[0] {
		registries, err := server.registryIndex(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = server.installations.refresh(ctx, registries)
	}
	public := httptest.NewServer(server.PublicHandler())
	t.Cleanup(public.Close)
	return public, fixture, server, meta
}

func adoptionSearchSamples() []inventoryMetricSample {
	fresh := adoptionFixedNow.Add(-time.Hour)
	var samples []inventoryMetricSample
	report := map[string][]string{
		telemetryAlpha: {"alpha", "preview"},
		telemetryBeta:  {"beta", "zulu", "preview-extra"},
	}
	for i := 1; i <= 9; i++ {
		installation := fmt.Sprintf("install-%d", i)
		fingerprint := fmt.Sprintf("gen-%d", i)
		samples = append(samples, inventorySnapshotSample(installation, fingerprint, fresh))
		for identity, names := range report {
			for _, name := range names {
				limit := map[string]int{"alpha": 1, "preview": 2, "beta": 3, "zulu": 3, "preview-extra": 9}[name]
				if i <= limit {
					samples = append(samples, inventoryResourceSample(installation, fingerprint, identity, "skill", name))
				}
			}
		}
	}
	return samples
}

func getSearch(t *testing.T, base, query string) (adoptionSearchResult, string) {
	t.Helper()
	resp, err := http.Get(base + "/api/v1/search" + query)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	body := string(raw)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search %q status = %d: %s", query, resp.StatusCode, body)
	}
	var result adoptionSearchResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	return result, body
}

func searchNames(result adoptionSearchResult) []string {
	names := make([]string, 0, len(result.Items))
	for _, item := range result.Items {
		names = append(names, item.Name)
	}
	return names
}

// spec: marketplace-api/rule-observations-preserve-selectable-resource-adoption
func TestSearchInstallationsRuleObservationPreservesSkillAdoption(t *testing.T) {
	public, fixture := adoptionSearchFixture(t, installationRuleAdoptionSamples())
	result, _ := getSearch(t, public.URL, "?sort=installations")
	if hits := fixture.hits.Load(); hits != 1 {
		t.Fatalf("metrics backend hits = %d, want one real reader request", hits)
	}

	foundSkill := false
	for _, item := range result.Items {
		if item.Kind == "rule" {
			t.Fatalf("search returned a rule resource: %+v", item)
		}
		if item.RegistryID == "rA" && item.Name == "alpha" {
			foundSkill = true
			if item.InstallationCount == nil || *item.InstallationCount != 1 {
				t.Fatalf("skill installation_count = %v, want 1", item.InstallationCount)
			}
		}
	}
	if !foundSkill {
		t.Fatal("search response omitted the skill control")
	}
}

// spec: marketplace-api/popularity-applies-before-pagination-with-relevant-search-matches
func TestSearchInstallationsOrdersBeforePagination(t *testing.T) {
	public, _ := adoptionSearchFixture(t, adoptionSearchSamples())

	result, body := getSearch(t, public.URL, "?sort=installations")
	want := []string{"preview-extra", "beta", "zulu", "preview", "alpha", "local-only"}
	if !slices.Equal(searchNames(result), want) {
		t.Fatalf("installations order = %v, want %v", searchNames(result), want)
	}
	wantCounts := map[string]int64{"preview-extra": 9, "beta": 3, "zulu": 3, "preview": 2, "alpha": 1}
	for _, item := range result.Items {
		if item.Name == "local-only" {
			if item.InstallationCount != nil {
				t.Fatalf("unknown-registry item served a fabricated count: %+v", item)
			}
			continue
		}
		if item.InstallationCount == nil {
			t.Fatalf("known-registry item %q missing installation_count", item.Name)
		}
		if *item.InstallationCount != wantCounts[item.Name] {
			t.Fatalf("installation_count[%q] = %d, want %d", item.Name, *item.InstallationCount, wantCounts[item.Name])
		}
	}
	if strings.Contains(body, "install-") {
		t.Fatalf("public response leaks an installation ID: %s", body)
	}

	page, _ := getSearch(t, public.URL, "?sort=installations&limit=2")
	if !slices.Equal(searchNames(page), []string{"preview-extra", "beta"}) {
		t.Fatalf("page 1 order = %v", searchNames(page))
	}
	page, _ = getSearch(t, public.URL, "?sort=installations&limit=2&offset=2")
	if !slices.Equal(searchNames(page), []string{"zulu", "preview"}) {
		t.Fatalf("page 2 order = %v; ties must be deterministic across pages", searchNames(page))
	}
	page, _ = getSearch(t, public.URL, "?sort=installations&limit=2&offset=4")
	if !slices.Equal(searchNames(page), []string{"alpha", "local-only"}) {
		t.Fatalf("page 3 order = %v", searchNames(page))
	}
	if page.Total != 6 {
		t.Fatalf("total = %d, want 6", page.Total)
	}
}

// spec: marketplace-api/popularity-applies-before-pagination-with-relevant-search-matches
func TestSearchInstallationsTextRelevancePrecedesAdoption(t *testing.T) {
	public, _ := adoptionSearchFixture(t, adoptionSearchSamples())

	result, _ := getSearch(t, public.URL, "?sort=installations&q=preview")
	want := []string{"preview", "preview-extra"}
	if !slices.Equal(searchNames(result), want) {
		t.Fatalf("text search order = %v, want %v (relevance before count)", searchNames(result), want)
	}
}

// spec: marketplace-api/popularity-applies-before-pagination-with-relevant-search-matches
func TestSearchInstallationsKeepsKindAndRegistryFilters(t *testing.T) {
	public, _, _, meta := adoptionSearchSetup(t, adoptionSearchSamples(), true)
	publishAssessedTestRevision(context.Background(), t, meta, store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "rA", Kind: KindCommand, Name: "deploy-preview"},
		ContentDigest: store.RevisionDigest(digestOf("c")),
		ArchiveDigest: store.ArchiveDigest(digestOf("7")),
	}, store.AssessmentDecisionAllow, store.RevisionDigest(digestOf("d")), strings.Repeat("e", 40))

	result, _ := getSearch(t, public.URL, "?sort=installations&registry=rB")
	if !slices.Equal(searchNames(result), []string{"preview-extra", "beta", "zulu"}) {
		t.Fatalf("registry-filtered order = %v", searchNames(result))
	}
	result, _ = getSearch(t, public.URL, "?sort=installations&kind=command")
	if !slices.Equal(searchNames(result), []string{"deploy-preview"}) {
		t.Fatalf("kind=command results = %v, want the command resource", searchNames(result))
	}
	if len(result.Items) == 1 && result.Items[0].InstallationCount == nil {
		t.Fatal("known-registry command omitted installation_count")
	}
	result, _ = getSearch(t, public.URL, "?sort=installations&kind=skill")
	if !slices.Equal(searchNames(result), []string{"preview-extra", "beta", "zulu", "preview", "alpha", "local-only"}) {
		t.Fatalf("kind=skill control order = %v", searchNames(result))
	}
}

// spec: marketplace-api/metrics-configuration-and-requests-preserve-the-server-boundary
func TestSearchInstallationsResponseExcludesCredentialAndIDs(t *testing.T) {
	public, fixture, server, _ := adoptionSearchSetup(t, adoptionSearchSamples(), false)
	server.installations.tokenEnv = "MARKETPLACE_METRICS_TEST_SEARCH"
	t.Setenv("MARKETPLACE_METRICS_TEST_SEARCH", "search-secret-token")
	registries, err := server.registryIndex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.installations.refresh(context.Background(), registries); err != nil {
		t.Fatalf("refresh() error = %v", err)
	}

	result, body := getSearch(t, public.URL, "?sort=installations")
	if len(result.Items) == 0 {
		t.Fatal("search returned no items")
	}
	if got := (<-fixture.requests).Auth; got != "Bearer search-secret-token" {
		t.Fatalf("metrics request authorization = %q, want configured bearer", got)
	}
	for _, leaked := range []string{"search-secret-token", "install-"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("public search response leaked %q: %s", leaked, body)
		}
	}
}

// spec: marketplace-api/existing-clients-and-invalid-sorting-remain-predictable
func TestSearchWithoutSortKeepsLegacyOrderAndSkipsMetrics(t *testing.T) {
	public, fixture, _, _ := adoptionSearchSetup(t, adoptionSearchSamples())

	result, body := getSearch(t, public.URL, "")
	want := []string{"alpha", "preview", "beta", "preview-extra", "zulu", "local-only"}
	if !slices.Equal(searchNames(result), want) {
		t.Fatalf("legacy order = %v, want %v", searchNames(result), want)
	}
	if strings.Contains(body, "installation_count") {
		t.Fatalf("legacy search emitted installation_count: %s", body)
	}
	if hits := fixture.hits.Load(); hits != 0 {
		t.Fatalf("legacy search contacted metrics backend %d times", hits)
	}
}

// spec: marketplace-api/existing-clients-and-invalid-sorting-remain-predictable
func TestSearchRejectsUnsupportedSort(t *testing.T) {
	public, fixture, _, _ := adoptionSearchSetup(t, adoptionSearchSamples())

	resp, err := http.Get(public.URL + "/api/v1/search?sort=popularity")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("sort=popularity status = %d, want 400", resp.StatusCode)
	}
	if hits := fixture.hits.Load(); hits != 0 {
		t.Fatalf("invalid sort still contacted metrics backend %d times", hits)
	}
}

// spec: marketplace-api/metrics-unavailable-does-not-break-resource-discovery
func TestSearchInstallationsFallsBackWithoutCounts(t *testing.T) {
	public, fixture, _, _ := adoptionSearchSetup(t, adoptionSearchSamples())
	fixture.handler.Store(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})

	result, body := getSearch(t, public.URL, "?sort=installations")
	want := []string{"alpha", "preview", "beta", "preview-extra", "zulu", "local-only"}
	if !slices.Equal(searchNames(result), want) {
		t.Fatalf("fallback order = %v, want legacy %v", searchNames(result), want)
	}
	if strings.Contains(body, "installation_count") {
		t.Fatalf("failed metrics still emitted counts: %s", body)
	}
	if hits := fixture.hits.Load(); hits != 0 {
		t.Fatalf("search issued telemetry I/O for missing counts: hits = %d", hits)
	}
}

// spec: marketplace-api/hosted-adoption-is-isolated-and-deduplicated
func TestSearchInstallationsCountsHostedCoordinatesForServingMarketplace(t *testing.T) {
	fresh := adoptionFixedNow.Add(-time.Hour)
	older := adoptionFixedNow.Add(-2 * time.Hour)
	samples := []inventoryMetricSample{
		inventorySnapshotSample("install-git-and-hosted", "gen-1", fresh),
		inventoryResourceSample("install-git-and-hosted", "gen-1", telemetryAlpha, "skill", "alpha"),
		hostedInventoryResourceSample("install-git-and-hosted", "gen-1", "mp", "rA", "skill", "alpha"),
		inventorySnapshotSample("install-hosted-again", "gen-2", fresh),
		hostedInventoryResourceSample("install-hosted-again", "gen-2", "mp", "rA", "skill", "alpha"),
		inventorySnapshotSample("install-foreign", "gen-3", fresh),
		hostedInventoryResourceSample("install-foreign", "gen-3", "other-marketplace", "rA", "skill", "alpha"),
		inventorySnapshotSample("install-other-registry", "gen-4", fresh),
		hostedInventoryResourceSample("install-other-registry", "gen-4", "mp", "rB", "skill", "alpha"),
		inventorySnapshotSample("install-local-registry", "gen-5", fresh),
		hostedInventoryResourceSample("install-local-registry", "gen-5", "mp", "rL", "skill", "local-only"),
		inventorySnapshotSample("install-removed", "gen-old", older),
		hostedInventoryResourceSample("install-removed", "gen-old", "mp", "rA", "skill", "alpha"),
		inventorySnapshotSample("install-removed", "gen-new", fresh),
	}
	public, _, _, meta := adoptionSearchSetup(t, samples, true)
	publishAssessedTestRevision(context.Background(), t, meta, store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "rB", Kind: KindSkill, Name: "alpha"},
		ContentDigest: store.RevisionDigest(digestOf("c")),
		ArchiveDigest: store.ArchiveDigest(digestOf("d")),
	}, store.AssessmentDecisionAllow, store.RevisionDigest(digestOf("f")), strings.Repeat("e", 40))

	result, body := getSearch(t, public.URL, "?sort=installations")
	wantCounts := map[string]int64{"rA/alpha": 2, "rB/alpha": 1, "rL/local-only": 1}
	for _, item := range result.Items {
		key := item.RegistryID + "/" + item.Name
		if want, ok := wantCounts[key]; ok {
			if item.InstallationCount == nil {
				t.Errorf("hosted installation_count[%s] is missing, want %d", key, want)
			} else if got := *item.InstallationCount; got != want {
				t.Errorf("hosted installation_count[%s] = %d, want %d", key, got, want)
			}
			delete(wantCounts, key)
		}
	}
	if len(wantCounts) != 0 {
		t.Fatalf("search omitted hosted coordinates: %v", wantCounts)
	}
	if strings.Contains(body, "source_kind") || strings.Contains(body, "marketplace_id") {
		t.Fatalf("public search changed response shape with telemetry labels: %s", body)
	}
}

// spec: marketplace-api/invalid-hosted-identity-preserves-safe-omission
func TestSearchInstallationsFallsBackForInvalidSourceIdentity(t *testing.T) {
	fresh := adoptionFixedNow.Add(-time.Hour)
	tests := []struct {
		name          string
		sourceKind    string
		marketplaceID string
	}{
		{name: "unknown source kind", sourceKind: "future"},
		{name: "incoherent legacy pair", marketplaceID: "mp"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invalid := inventoryResourceSample("install-invalid", "gen-invalid", telemetryAlpha, "skill", "alpha")
			if test.sourceKind != "" {
				invalid.Metric["source_kind"] = test.sourceKind
			}
			if test.marketplaceID != "" {
				invalid.Metric["marketplace_id"] = test.marketplaceID
			}
			samples := []inventoryMetricSample{
				inventorySnapshotSample("install-control", "gen-control", fresh),
				inventoryResourceSample("install-control", "gen-control", telemetryAlpha, "skill", "alpha"),
				inventorySnapshotSample("install-invalid", "gen-invalid", fresh),
				invalid,
			}
			public, fixture := adoptionSearchFixture(t, samples)
			result, body := getSearch(t, public.URL, "?sort=installations")
			want := []string{"alpha", "preview", "beta", "preview-extra", "zulu", "local-only"}
			if !slices.Equal(searchNames(result), want) {
				t.Fatalf("fallback order = %v, want legacy %v", searchNames(result), want)
			}
			if strings.Contains(body, "installation_count") {
				t.Fatalf("invalid metrics still emitted counts: %s", body)
			}
			if hits := fixture.hits.Load(); hits != 1 {
				t.Fatalf("metrics backend hits = %d, want 1", hits)
			}
		})
	}
}
