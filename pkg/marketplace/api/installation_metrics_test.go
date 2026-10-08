package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"gopkg.in/yaml.v3"
)

type metricsRequest struct {
	Form        url.Values
	Auth        string
	Path        string
	Method      string
	ContentType string
}

type metricsFixture struct {
	server   *httptest.Server
	hits     *atomic.Int32
	requests chan metricsRequest
	handler  *atomic.Value // func(w http.ResponseWriter, r *http.Request)
}

func newMetricsFixture(t *testing.T, payload func() string) *metricsFixture {
	t.Helper()
	fixture := &metricsFixture{
		hits:     &atomic.Int32{},
		requests: make(chan metricsRequest, 64),
		handler:  &atomic.Value{},
	}
	fixture.handler.Store(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, payload())
	})
	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.hits.Add(1)
		_ = r.ParseForm()
		fixture.requests <- metricsRequest{Form: r.Form, Auth: r.Header.Get("Authorization"), Path: r.URL.Path, Method: r.Method, ContentType: r.Header.Get("Content-Type")}
		fixture.handler.Load().(func(http.ResponseWriter, *http.Request))(w, r)
	}))
	t.Cleanup(fixture.server.Close)
	return fixture
}

func prometheusVector(samples []inventoryMetricSample, observedAt time.Time) string {
	type sample struct {
		Metric map[string]string `json:"metric"`
		Value  [2]any            `json:"value"`
	}
	result := make([]sample, 0, len(samples))
	for _, s := range samples {
		result = append(result, sample{Metric: s.Metric, Value: [2]any{observedAt.Unix(), fmt.Sprintf("%v", s.Value)}})
	}
	body, _ := json.Marshal(map[string]any{
		"status": "success",
		"data":   map[string]any{"resultType": "vector", "result": result},
	})
	return string(body)
}

func adoptionSamples() []inventoryMetricSample {
	fresh := adoptionFixedNow.Add(-time.Hour)
	return []inventoryMetricSample{
		inventorySnapshotSample("install-1", "gen-1", fresh),
		inventoryResourceSample("install-1", "gen-1", telemetryAlpha, "skill", "res-a"),
	}
}

func newTestReader(t *testing.T, fixture *metricsFixture) *installationMetricsReader {
	t.Helper()
	return newInstallationMetricsReader(config.InstallationMetrics{
		URL:    fixture.server.URL,
		Labels: map[string]string{"namespace": "ops"},
	}, "mp")
}

// spec: marketplace-api/superseded-inventory-history-does-not-exhaust-the-response-budget
func TestInstallationMetricsQueryLatestInventoryProjection(t *testing.T) {
	const wantNamespace = `label_replace((max by (installation_id, config_fingerprint) (last_over_time(agent_marketplace_config_snapshot{namespace="ops"}[30d])) == on (installation_id) group_left max by (installation_id) (max by (installation_id, config_fingerprint) (last_over_time(agent_marketplace_config_snapshot{namespace="ops"}[30d])))), "inventory_type", "snapshot", "", "") or label_replace((max by (installation_id, config_fingerprint, source_kind, marketplace_id, registry_id, resource_kind, resource_name) (last_over_time(agent_marketplace_config_resource{namespace="ops",resource_kind!="rule"}[30d])) and on (installation_id, config_fingerprint) (max by (installation_id, config_fingerprint) (last_over_time(agent_marketplace_config_snapshot{namespace="ops"}[30d])) == on (installation_id) group_left max by (installation_id) (max by (installation_id, config_fingerprint) (last_over_time(agent_marketplace_config_snapshot{namespace="ops"}[30d]))))), "inventory_type", "resource", "", "")`
	const wantEmpty = `label_replace((max by (installation_id, config_fingerprint) (last_over_time(agent_marketplace_config_snapshot{}[30d])) == on (installation_id) group_left max by (installation_id) (max by (installation_id, config_fingerprint) (last_over_time(agent_marketplace_config_snapshot{}[30d])))), "inventory_type", "snapshot", "", "") or label_replace((max by (installation_id, config_fingerprint, source_kind, marketplace_id, registry_id, resource_kind, resource_name) (last_over_time(agent_marketplace_config_resource{resource_kind!="rule"}[30d])) and on (installation_id, config_fingerprint) (max by (installation_id, config_fingerprint) (last_over_time(agent_marketplace_config_snapshot{}[30d])) == on (installation_id) group_left max by (installation_id) (max by (installation_id, config_fingerprint) (last_over_time(agent_marketplace_config_snapshot{}[30d]))))), "inventory_type", "resource", "", "")`
	const escapedLabels = `alpha="first",zeta="quote\n\"line\""`

	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{name: "namespace", labels: map[string]string{"namespace": "ops"}, want: wantNamespace},
		{name: "empty labels", labels: map[string]string{}, want: wantEmpty},
		{
			name:   "sorted and escaped labels",
			labels: map[string]string{"zeta": "quote\n\"line\"", "alpha": "first"},
			want:   strings.ReplaceAll(wantNamespace, `namespace="ops"`, escapedLabels),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := installationMetricsQuery(test.labels); got != test.want {
				t.Errorf("installationMetricsQuery() = %q, want %q", got, test.want)
			}
		})
	}

	data, err := os.ReadFile("installation_query_test.yaml")
	if err != nil {
		t.Fatalf("read query evaluator fixture: %v", err)
	}
	var fixture struct {
		Tests []struct {
			PromQLExprTest []struct {
				Expr string `yaml:"expr"`
			} `yaml:"promql_expr_test"`
		} `yaml:"tests"`
	}
	if err := yaml.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("unmarshal query evaluator fixture: %v", err)
	}
	if len(fixture.Tests) == 0 || len(fixture.Tests[0].PromQLExprTest) == 0 {
		t.Fatal("query evaluator fixture has no PromQL expression test")
	}
	if got := fixture.Tests[0].PromQLExprTest[0].Expr; got != wantNamespace {
		t.Errorf("query evaluator expression = %q, want %q", got, wantNamespace)
	}
	if got := installationMetricsQuery(map[string]string{"namespace": "ops"}); got != wantNamespace {
		t.Errorf("installationMetricsQuery(namespace=ops) = %q, want %q", got, wantNamespace)
	}
}

// spec: marketplace-api/superseded-inventory-history-does-not-exhaust-the-response-budget
func TestInstallationMetricsProjectedInventoryReaderCounts(t *testing.T) {
	body, err := os.ReadFile("installation_query_result_test.json")
	if err != nil {
		t.Fatalf("read frozen evaluator result: %v", err)
	}
	fixture := newMetricsFixture(t, func() string { return string(body) })
	reader := newTestReader(t, fixture)
	reader.now = func() time.Time { return time.Unix(3456000, 0) }

	counts, err := reader.counts(context.Background(), adoptionTestRegistries())
	if err != nil {
		t.Fatalf("reader counts() error = %v", err)
	}
	assertAdoptionCounts(t, counts, map[installationResourceKey]int64{
		adoptionKey("rA", "skill", "alpha"): 1,
	}, nil)
	if hits := fixture.hits.Load(); hits != 1 {
		t.Fatalf("metrics reader hits = %d, want 1", hits)
	}
}

// spec: marketplace-api/metrics-configuration-and-requests-preserve-the-server-boundary
func TestInstallationMetricsReaderRequestShapeAndCounts(t *testing.T) {
	fixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), adoptionFixedNow) })
	reader := newTestReader(t, fixture)
	reader.now = func() time.Time { return adoptionFixedNow }
	t.Setenv("MARKETPLACE_METRICS_TEST_TOKEN", "fixture-token")
	reader.tokenEnv = "MARKETPLACE_METRICS_TEST_TOKEN"

	counts, err := reader.counts(context.Background(), adoptionTestRegistries())
	if err != nil {
		t.Fatalf("counts() error = %v", err)
	}
	if got := counts.Counts[installationResourceKey{Registry: "rA", Kind: "skill", Name: "res-a"}]; got != 1 {
		t.Fatalf("count for res-a = %d, want 1", got)
	}
	if !counts.KnownRegistries["rA"] {
		t.Fatal("rA should be a known registry")
	}

	got := <-fixture.requests
	if got.Method != http.MethodPost {
		t.Fatalf("metrics request method = %q, want POST", got.Method)
	}
	if got.ContentType != "application/x-www-form-urlencoded" {
		t.Fatalf("metrics request content type = %q, want form encoding", got.ContentType)
	}
	if got.Path != "/api/v1/query" {
		t.Fatalf("metrics request path = %q, want /api/v1/query", got.Path)
	}
	if got.Auth != "Bearer fixture-token" {
		t.Fatalf("authorization = %q, want bearer from environment", got.Auth)
	}
	if !strings.Contains(got.Form.Get("query"), `agent_marketplace_config_snapshot{namespace="ops"}`) {
		t.Fatalf("query form missing selector: %q", got.Form.Get("query"))
	}
	if got.Form.Get("time") != fmt.Sprintf("%d", adoptionFixedNow.Unix()) {
		t.Fatalf("time form = %q, want %d", got.Form.Get("time"), adoptionFixedNow.Unix())
	}
	if ct := got.Form.Get("query"); strings.Contains(ct, "install-1") {
		t.Fatal("query leaked an installation ID")
	}
}

// spec: marketplace-api/metrics-configuration-and-requests-preserve-the-server-boundary
func TestInstallationMetricsReaderCachesAggregatesFiveMinutes(t *testing.T) {
	fixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), adoptionFixedNow) })
	reader := newTestReader(t, fixture)
	now := adoptionFixedNow
	reader.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if _, err := reader.counts(context.Background(), adoptionTestRegistries()); err != nil {
			t.Fatalf("counts() call %d error = %v", i, err)
		}
	}
	if hits := fixture.hits.Load(); hits != 1 {
		t.Fatalf("metrics endpoint hits = %d, want 1", hits)
	}
	now = now.Add(4 * time.Minute)
	if _, err := reader.counts(context.Background(), adoptionTestRegistries()); err != nil {
		t.Fatalf("counts() within freshness error = %v", err)
	}
	if hits := fixture.hits.Load(); hits != 1 {
		t.Fatalf("metrics endpoint hits inside cache window = %d, want 1", hits)
	}
}

// spec: marketplace-api/metrics-unavailable-does-not-break-resource-discovery
func TestInstallationMetricsReaderExpiryFailureAndBackoff(t *testing.T) {
	fixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), adoptionFixedNow) })
	reader := newTestReader(t, fixture)
	now := adoptionFixedNow
	reader.now = func() time.Time { return now }
	registries := adoptionTestRegistries()

	if _, err := reader.counts(context.Background(), registries); err != nil {
		t.Fatalf("initial counts() error = %v", err)
	}
	fixture.handler.Store(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	now = now.Add(6 * time.Minute)
	if _, err := reader.counts(context.Background(), registries); err == nil {
		t.Fatal("counts() after expiry with failing metrics = nil, want unavailable")
	}
	if hits := fixture.hits.Load(); hits != 2 {
		t.Fatalf("metrics endpoint hits = %d, want 2", hits)
	}
	if _, err := reader.counts(context.Background(), registries); err == nil {
		t.Fatal("counts() inside failure backoff = nil, want unavailable")
	}
	if hits := fixture.hits.Load(); hits != 2 {
		t.Fatalf("failure backoff did not hold for 30s: hits = %d, want 2", hits)
	}
}

// spec: marketplace-api/metrics-unavailable-does-not-break-resource-discovery
func TestInstallationMetricsReaderObservationHorizonExpiresEarlier(t *testing.T) {
	almostStale := adoptionFixedNow.Add(-30*24*time.Hour + 2*time.Minute)
	samples := []inventoryMetricSample{
		inventorySnapshotSample("install-1", "gen-1", almostStale),
		inventoryResourceSample("install-1", "gen-1", telemetryAlpha, "skill", "res-a"),
	}
	fixture := newMetricsFixture(t, func() string { return prometheusVector(samples, adoptionFixedNow) })
	reader := newTestReader(t, fixture)
	now := adoptionFixedNow
	reader.now = func() time.Time { return now }
	registries := adoptionTestRegistries()

	counts, err := reader.counts(context.Background(), registries)
	if err != nil {
		t.Fatalf("counts() error = %v", err)
	}
	if counts.ValidUntil.After(now.Add(installationMetricsFreshness)) {
		t.Fatalf("ValidUntil %v beyond five-minute freshness", counts.ValidUntil)
	}
	now = now.Add(3 * time.Minute)
	if _, err := reader.counts(context.Background(), registries); err == nil {
		t.Fatal("counts() reused an aggregate past its inventory validity horizon")
	}
	if hits := fixture.hits.Load(); hits != 2 {
		t.Fatalf("metrics endpoint hits = %d, want 2 after validity horizon", hits)
	}
}

// spec: marketplace-api/registry-identity-matching-does-not-conflate-resources
func TestInstallationMetricsReaderOriginChangeInvalidatesCache(t *testing.T) {
	fixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), adoptionFixedNow) })
	reader := newTestReader(t, fixture)
	reader.now = func() time.Time { return adoptionFixedNow }

	registries := adoptionTestRegistries()
	if _, err := reader.counts(context.Background(), registries); err != nil {
		t.Fatalf("counts() error = %v", err)
	}
	changed := adoptionTestRegistries()
	rA := changed["rA"]
	rA.Origins = []string{"https://example.test/team/moved-alpha.git"}
	changed["rA"] = rA
	counts, err := reader.counts(context.Background(), changed)
	if err != nil {
		t.Fatalf("counts() after origin change error = %v", err)
	}
	if hits := fixture.hits.Load(); hits != 2 {
		t.Fatalf("origin change did not invalidate aggregate cache: hits = %d, want 2", hits)
	}
	if got := counts.Counts[installationResourceKey{Registry: "rA", Kind: "skill", Name: "res-a"}]; got != 0 {
		t.Fatalf("moved origin still contributed a count to rA: %d", got)
	}
}

// spec: marketplace-api/superseded-inventory-history-does-not-exhaust-the-response-budget
// spec: marketplace-api/metrics-unavailable-does-not-break-resource-discovery
func TestInstallationMetricsReaderRejectsUnusableResponses(t *testing.T) {
	good := prometheusVector(adoptionSamples(), adoptionFixedNow)
	tests := []struct {
		name    string
		respond func(w http.ResponseWriter, r *http.Request)
	}{
		{name: "non-200", respond: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }},
		{name: "status error", respond: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, `{"status":"error","data":{"resultType":"vector","result":[]}}`)
		}},
		{name: "warnings present", respond: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprintf(w, `{"status":"success","warnings":["partial"],"data":{"resultType":"vector","result":%s}}`, "[]")
		}},
		{name: "non-vector result", respond: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[]}}`)
		}},
		{name: "malformed json", respond: func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, `{`) }},
		{name: "nonfinite value", respond: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"inventory_type":"snapshot","installation_id":"i1","config_fingerprint":"g"},"value":[0,"NaN"]}]}}`)
		}},
		{name: "empty result", respond: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[]}}`)
		}},
		{name: "oversized body", respond: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[`))
			_, _ = w.Write([]byte(strings.Repeat(" ", installationMetricsMaxBody)))
			_, _ = w.Write([]byte(`]}}`))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newMetricsFixture(t, func() string { return good })
			fixture.handler.Store(test.respond)
			reader := newTestReader(t, fixture)
			reader.now = func() time.Time { return adoptionFixedNow }
			counts, err := reader.counts(context.Background(), adoptionTestRegistries())
			if err == nil {
				t.Fatalf("counts() = %+v, want an unavailable error", counts)
			}
			if strings.Contains(err.Error(), "install-1") {
				t.Fatalf("error leaks installation ID: %v", err)
			}
		})
	}
}

// spec: marketplace-api/metrics-unavailable-does-not-break-resource-discovery
func TestInstallationMetricsReaderRejectsAmbiguousJSON(t *testing.T) {
	good := prometheusVector(adoptionSamples(), adoptionFixedNow)
	withExtra := strings.Replace(good, `"status":"success"`, `"status":"success","server":{"x":1}`, 1)
	withExtra = strings.Replace(withExtra, `"resultType":"vector"`, `"extra":true,"resultType":"vector"`, 1)
	withExtra = strings.TrimSuffix(withExtra, "}") + `,"warnings_extra":null}`
	cases := []struct {
		name  string
		body  string
		valid bool
	}{
		{name: "positive control", body: good, valid: true},
		{name: "unrelated keys accepted", body: withExtra, valid: true},
		{name: "duplicate root key", body: `{"status":"success","status":"success","data":{"resultType":"vector","result":[]}}`},
		{name: "duplicate data key", body: `{"status":"success","data":{"resultType":"vector","result":[],"result":[]}}`},
		{name: "duplicate row key", body: `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"a":"b"},"metric":{"a":"b"},"value":[0,"1"]}]}}`},
		{name: "duplicate metric label", body: `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"a":"1","a":"2"},"value":[0,"1"]}]}}`},
		{name: "alias root key", body: `{"Status":"success","data":{"resultType":"vector","result":[]}}`},
		{name: "alias data key", body: `{"status":"success","data":{"ResultType":"vector","result":[]}}`},
		{name: "alias row key", body: `{"status":"success","data":{"resultType":"vector","result":[{"Metric":{"a":"b"},"value":[0,"1"]}]}}`},
		{name: "null timestamp", body: `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"a":"b"},"value":[null,"1"]}]}}`},
		{name: "string timestamp", body: `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"a":"b"},"value":["0","1"]}]}}`},
		{name: "object timestamp", body: `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"a":"b"},"value":[{},"1"]}]}}`},
		{name: "numeric value", body: `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"a":"b"},"value":[0,1]}]}}`},
		{name: "nonfinite timestamp", body: `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"a":"b"},"value":[1e999,"1"]}]}}`},
		{name: "jsonc comment", body: "{\"status\":\"success\",//note\n\"data\":{\"resultType\":\"vector\",\"result\":[]}}"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newMetricsFixture(t, func() string { return test.body })
			reader := newTestReader(t, fixture)
			reader.now = func() time.Time { return adoptionFixedNow }
			counts, err := reader.counts(context.Background(), adoptionTestRegistries())
			if test.valid {
				if err != nil {
					t.Fatalf("counts() error = %v, want acceptance", err)
				}
				return
			}
			if !errors.Is(err, errInvalidInstallationMetrics) {
				t.Fatalf("counts() error = %v, counts = %+v, want errInvalidInstallationMetrics", err, counts)
			}
			if strings.Contains(err.Error(), "install-1") {
				t.Fatalf("error leaks installation ID: %v", err)
			}
		})
	}
}

// spec: marketplace-api/metrics-configuration-and-requests-preserve-the-server-boundary
func TestInstallationMetricsReaderRedirectForwardsNoCredentials(t *testing.T) {
	secondHits := &atomic.Int32{}
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondHits.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Error("redirect target received credentials")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer second.Close()

	fixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), adoptionFixedNow) })
	fixture.handler.Store(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, second.URL+"/api/v1/query", http.StatusFound)
	})
	reader := newTestReader(t, fixture)
	reader.now = func() time.Time { return adoptionFixedNow }
	reader.tokenEnv = "MARKETPLACE_METRICS_TEST_TOKEN"
	t.Setenv("MARKETPLACE_METRICS_TEST_TOKEN", "fixture-token")

	if _, err := reader.counts(context.Background(), adoptionTestRegistries()); err == nil {
		t.Fatal("counts() on redirect = nil, want unavailable (no redirect following)")
	}
	if got := secondHits.Load(); got != 0 {
		t.Fatalf("redirect target received %d requests, want 0", got)
	}
}

// spec: marketplace-api/metrics-unavailable-does-not-break-resource-discovery
func TestInstallationMetricsReaderUnsetTokenAndTimeout(t *testing.T) {
	fixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), adoptionFixedNow) })
	reader := newTestReader(t, fixture)
	reader.now = func() time.Time { return adoptionFixedNow }
	reader.tokenEnv = "MARKETPLACE_METRICS_TEST_UNSET"
	t.Setenv("MARKETPLACE_METRICS_TEST_UNSET", "")
	if _, err := reader.counts(context.Background(), adoptionTestRegistries()); err == nil {
		t.Fatal("counts() with unset bearer env = nil, want unavailable")
	}
	if hits := fixture.hits.Load(); hits != 0 {
		t.Fatalf("request was sent without resolvable credential: hits = %d", hits)
	}

	newlineFixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), adoptionFixedNow) })
	newlineReader := newTestReader(t, newlineFixture)
	newlineReader.now = func() time.Time { return adoptionFixedNow }
	newlineReader.tokenEnv = "MARKETPLACE_METRICS_TEST_UNSET"
	t.Setenv("MARKETPLACE_METRICS_TEST_UNSET", "line1\nline2")
	if _, err := newlineReader.counts(context.Background(), adoptionTestRegistries()); err == nil {
		t.Fatal("counts() with newline token = nil, want unavailable")
	}
	if hits := newlineFixture.hits.Load(); hits != 0 {
		t.Fatalf("request was sent with a malformed credential: hits = %d", hits)
	}

	validFixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), adoptionFixedNow) })
	validReader := newTestReader(t, validFixture)
	validReader.now = func() time.Time { return adoptionFixedNow }
	validReader.tokenEnv = "MARKETPLACE_METRICS_TEST_UNSET"
	t.Setenv("MARKETPLACE_METRICS_TEST_UNSET", "fixture-token")
	if _, err := validReader.counts(context.Background(), adoptionTestRegistries()); err != nil {
		t.Fatalf("counts() with a valid token error = %v", err)
	}
	if hits := validFixture.hits.Load(); hits != 1 {
		t.Fatalf("valid token did not reach the metrics endpoint once: hits = %d", hits)
	}

	slow := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), adoptionFixedNow) })
	slow.handler.Store(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(3 * time.Second)
		w.WriteHeader(http.StatusOK)
	})
	slowReader := newInstallationMetricsReader(config.InstallationMetrics{URL: slow.server.URL}, "mp")
	slowReader.now = func() time.Time { return adoptionFixedNow }
	start := time.Now()
	if _, err := slowReader.counts(context.Background(), adoptionTestRegistries()); err == nil {
		t.Fatal("counts() against hung endpoint = nil, want timeout error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("metrics request exceeded bound: %v", elapsed)
	}
}
