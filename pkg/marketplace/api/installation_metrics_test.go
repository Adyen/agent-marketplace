package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
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
		requests: make(chan metricsRequest, 1024),
		handler:  &atomic.Value{},
	}
	fixture.handler.Store(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, operandVector(payload(), r.Form.Get("query")))
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

var operandInventoryType = regexp.MustCompile(`"inventory_type", "([a-z_]+)"`)

// operandVector answers one operand request the way the backend would: only the rows of the
// requested inventory_type. Bodies that are not a vector of inventory rows pass through unchanged.
func operandVector(body, query string) string {
	type row struct {
		Metric map[string]string `json:"metric"`
		Value  []any             `json:"value"`
	}
	var decoded struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []row  `json:"result"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(body), &decoded) != nil || decoded.Status != "success" || decoded.Data.ResultType != "vector" || len(decoded.Data.Result) == 0 {
		return body
	}
	match := operandInventoryType.FindStringSubmatch(query)
	rows := make([]row, 0, len(decoded.Data.Result))
	for _, entry := range decoded.Data.Result {
		inventoryType, ok := entry.Metric["inventory_type"]
		if !ok || match == nil {
			return body
		}
		if inventoryType == match[1] {
			rows = append(rows, entry)
		}
	}
	encoded, _ := json.Marshal(map[string]any{
		"status": "success",
		"data":   map[string]any{"resultType": "vector", "result": rows},
	})
	return string(encoded)
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
	return []inventoryMetricSample{
		aggregateResponseSample("count", 1, map[string]string{"am_registry": "rA", "resource_kind": "skill", "resource_name": "res-a"}),
		aggregateResponseSample("fresh", 1, nil),
		aggregateResponseSample("valid_until", float64(adoptionFixedNow.Add(-time.Hour).UnixMicro()), nil),
	}
}

func newTestReader(t *testing.T, fixture *metricsFixture) *installationMetricsReader {
	t.Helper()
	return newInstallationMetricsReader(config.InstallationMetrics{
		URL:    fixture.server.URL,
		Labels: map[string]string{"namespace": "ops"},
	}, "mp")
}

// spec: marketplace-api/metrics-configuration-and-requests-preserve-the-server-boundary
func TestInstallationMetricsReaderRequestShapeAndCounts(t *testing.T) {
	fixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), adoptionFixedNow) })
	reader := newTestReader(t, fixture)
	reader.now = func() time.Time { return adoptionFixedNow }
	t.Setenv("MARKETPLACE_METRICS_TEST_TOKEN", "fixture-token")
	reader.tokenEnv = "MARKETPLACE_METRICS_TEST_TOKEN"

	if _, err := reader.refresh(context.Background(), adoptionTestRegistries()); err != nil {
		t.Fatalf("refresh() error = %v", err)
	}
	snapshot, ok := reader.lookup(adoptionTestRegistries())
	if !ok {
		t.Fatal("lookup() after refresh returned no snapshot")
	}
	if got := snapshot.Counts[installationResourceKey{Registry: "rA", Kind: "skill", Name: "res-a"}]; got != 1 {
		t.Fatalf("count for res-a = %d, want 1", got)
	}
	if !snapshot.KnownRegistries["rA"] {
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
	registries := adoptionTestRegistries()

	if _, err := reader.refresh(context.Background(), registries); err != nil {
		t.Fatalf("refresh() error = %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, ok := reader.lookup(registries); !ok {
			t.Fatalf("lookup() call %d missed the published snapshot", i)
		}
	}
	if hits := fixture.hits.Load(); hits != adoptionRefreshRequests {
		t.Fatalf("metrics endpoint hits = %d, want %d", hits, adoptionRefreshRequests)
	}
	now = adoptionFixedNow.Add(5*time.Minute - time.Nanosecond)
	if _, fresh := reader.lookup(registries); !fresh {
		t.Fatal("lookup() dropped a snapshot inside its freshness window")
	}
	if hits := fixture.hits.Load(); hits != adoptionRefreshRequests {
		t.Fatalf("metrics endpoint hits inside cache window = %d, want %d", hits, adoptionRefreshRequests)
	}
	now = adoptionFixedNow.Add(5 * time.Minute)
	if _, fresh := reader.lookup(registries); fresh {
		t.Fatal("lookup() reported a snapshot as fresh at its exact max_age expiry boundary")
	}
}

// spec: marketplace-api/metrics-unavailable-does-not-break-resource-discovery
func TestInstallationMetricsReaderExpiryFailureAndBackoff(t *testing.T) {
	fixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), adoptionFixedNow) })
	reader := newTestReader(t, fixture)
	var now atomic.Int64
	now.Store(adoptionFixedNow.UnixNano())
	reader.now = func() time.Time { return time.Unix(0, now.Load()) }
	registries := adoptionTestRegistries()

	if _, err := reader.refresh(context.Background(), registries); err != nil {
		t.Fatalf("initial refresh() error = %v", err)
	}
	failedAt := adoptionFixedNow.Add(6 * time.Minute)
	fixture.handler.Store(func(w http.ResponseWriter, _ *http.Request) {
		now.Store(failedAt.Add(3 * time.Second).UnixNano())
		w.WriteHeader(http.StatusInternalServerError)
	})
	now.Store(failedAt.UnixNano())
	if delay := reader.attempt(context.Background(), staticRegistryIndex(registries), false); delay != reader.timing.RetryInterval {
		t.Fatalf("failed attempt delay = %v, want retry interval %v", delay, reader.timing.RetryInterval)
	}
	if want := failedAt.Add(3*time.Second + reader.timing.RetryInterval); !reader.backoffUntil.Equal(want) {
		t.Fatalf("backoffUntil = %v, want attempt completion + retry interval = %v", reader.backoffUntil, want)
	}
	if counts, fresh := reader.lookup(registries); counts == nil || fresh {
		t.Fatal("lookup() after expiry with failing metrics did not serve stale last-good counts")
	}
	if hits := fixture.hits.Load(); hits != 2*adoptionRefreshRequests {
		t.Fatalf("metrics endpoint hits = %d, want %d", hits, 2*adoptionRefreshRequests)
	}
	if delay := reader.attempt(context.Background(), staticRegistryIndex(registries), true); delay != reader.timing.RetryInterval {
		t.Fatalf("hint inside failure backoff delay = %v, want remaining backoff %v", delay, reader.timing.RetryInterval)
	}
	if hits := fixture.hits.Load(); hits != 2*adoptionRefreshRequests {
		t.Fatalf("failure backoff did not hold for 30s: hits = %d, want %d", hits, 2*adoptionRefreshRequests)
	}
}

// spec: marketplace-api/metrics-unavailable-does-not-break-resource-discovery
func TestInstallationMetricsReaderObservationHorizonExpiresEarlier(t *testing.T) {
	almostStale := adoptionFixedNow.Add(-30*24*time.Hour + 2*time.Minute)
	samples := []inventoryMetricSample{
		aggregateResponseSample("count", 1, map[string]string{"am_registry": "rA", "resource_kind": "skill", "resource_name": "res-a"}),
		aggregateResponseSample("fresh", 1, nil),
		aggregateResponseSample("valid_until", float64(almostStale.UnixMicro()), nil),
	}
	fixture := newMetricsFixture(t, func() string { return prometheusVector(samples, adoptionFixedNow) })
	reader := newTestReader(t, fixture)
	now := adoptionFixedNow
	reader.now = func() time.Time { return now }
	registries := adoptionTestRegistries()

	if _, err := reader.refresh(context.Background(), registries); err != nil {
		t.Fatalf("refresh() error = %v", err)
	}
	counts, fresh := reader.lookup(registries)
	if counts == nil || !fresh {
		t.Fatal("lookup() after refresh returned no fresh snapshot")
	}
	if counts.ValidUntil.After(now.Add(5 * time.Minute)) {
		t.Fatalf("ValidUntil %v beyond five-minute freshness", counts.ValidUntil)
	}
	now = now.Add(3 * time.Minute)
	if _, fresh := reader.lookup(registries); fresh {
		t.Fatal("lookup() reported an aggregate fresh past its inventory validity horizon")
	}
	reader.attempt(context.Background(), staticRegistryIndex(registries), false)
	if hits := fixture.hits.Load(); hits != 2*adoptionRefreshRequests {
		t.Fatalf("metrics endpoint hits = %d, want %d after validity horizon", hits, 2*adoptionRefreshRequests)
	}
}

// spec: marketplace-api/registry-identity-matching-does-not-conflate-resources
func TestInstallationMetricsReaderOriginChangeInvalidatesCache(t *testing.T) {
	fixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), adoptionFixedNow) })
	reader := newTestReader(t, fixture)
	reader.now = func() time.Time { return adoptionFixedNow }

	registries := adoptionTestRegistries()
	if _, err := reader.refresh(context.Background(), registries); err != nil {
		t.Fatalf("refresh() error = %v", err)
	}
	changed := adoptionTestRegistries()
	rA := changed["rA"]
	rA.Origins = []string{"https://example.test/team/moved-alpha.git"}
	changed["rA"] = rA
	if _, ok := reader.lookup(changed); ok {
		t.Fatal("lookup() served a stale signature after origin change")
	}
	if hits := fixture.hits.Load(); hits != adoptionRefreshRequests {
		t.Fatalf("identity-mismatched lookup issued telemetry: hits = %d, want %d", hits, adoptionRefreshRequests)
	}
	fixture.handler.Store(func(w http.ResponseWriter, r *http.Request) {
		samples := []inventoryMetricSample{
			aggregateResponseSample("fresh", 1, nil),
			aggregateResponseSample("valid_until", float64(adoptionFixedNow.UnixMicro()), nil),
		}
		_, _ = fmt.Fprint(w, operandVector(prometheusVector(samples, adoptionFixedNow), r.Form.Get("query")))
	})
	if _, err := reader.refresh(context.Background(), changed); err != nil {
		t.Fatalf("refresh() after origin change error = %v", err)
	}
	if _, fresh := reader.lookup(changed); !fresh {
		t.Fatal("lookup() after origin change refresh returned no snapshot")
	}
	if hits := fixture.hits.Load(); hits != 2*adoptionRefreshRequests {
		t.Fatalf("origin change did not invalidate aggregate cache: hits = %d, want %d", hits, 2*adoptionRefreshRequests)
	}
	for range adoptionRefreshRequests {
		<-fixture.requests
	}
	movedSeen := false
	for range adoptionRefreshRequests {
		query := (<-fixture.requests).Form.Get("query")
		movedSeen = movedSeen || strings.Contains(query, "team/moved-alpha")
		if strings.Contains(query, "team/alpha") {
			t.Fatalf("query after origin change retained old identity example.test/team/alpha: %q", query)
		}
	}
	if !movedSeen {
		t.Fatal("queries after origin change omitted moved identity")
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
			counts, err := reader.refresh(context.Background(), adoptionTestRegistries())
			if err == nil {
				t.Fatalf("refresh() = %+v, want an unavailable error", counts)
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
			counts, err := reader.refresh(context.Background(), adoptionTestRegistries())
			if test.valid {
				if err != nil {
					t.Fatalf("refresh() error = %v, want acceptance", err)
				}
				return
			}
			if !errors.Is(err, errInvalidInstallationMetrics) {
				t.Fatalf("refresh() error = %v, counts = %+v, want errInvalidInstallationMetrics", err, counts)
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

	if _, err := reader.refresh(context.Background(), adoptionTestRegistries()); err == nil {
		t.Fatal("refresh() on redirect = nil, want unavailable (no redirect following)")
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
	if _, err := reader.refresh(context.Background(), adoptionTestRegistries()); err == nil {
		t.Fatal("refresh() with unset bearer env = nil, want unavailable")
	}
	if hits := fixture.hits.Load(); hits != 0 {
		t.Fatalf("request was sent without resolvable credential: hits = %d", hits)
	}

	newlineFixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), adoptionFixedNow) })
	newlineReader := newTestReader(t, newlineFixture)
	newlineReader.now = func() time.Time { return adoptionFixedNow }
	newlineReader.tokenEnv = "MARKETPLACE_METRICS_TEST_UNSET"
	t.Setenv("MARKETPLACE_METRICS_TEST_UNSET", "line1\nline2")
	if _, err := newlineReader.refresh(context.Background(), adoptionTestRegistries()); err == nil {
		t.Fatal("refresh() with newline token = nil, want unavailable")
	}
	if hits := newlineFixture.hits.Load(); hits != 0 {
		t.Fatalf("request was sent with a malformed credential: hits = %d", hits)
	}

	validFixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), adoptionFixedNow) })
	validReader := newTestReader(t, validFixture)
	validReader.now = func() time.Time { return adoptionFixedNow }
	validReader.tokenEnv = "MARKETPLACE_METRICS_TEST_UNSET"
	t.Setenv("MARKETPLACE_METRICS_TEST_UNSET", "fixture-token")
	if _, err := validReader.refresh(context.Background(), adoptionTestRegistries()); err != nil {
		t.Fatalf("refresh() with a valid token error = %v", err)
	}
	if hits := validFixture.hits.Load(); hits != adoptionRefreshRequests {
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
	if _, err := slowReader.refresh(context.Background(), adoptionTestRegistries()); err == nil {
		t.Fatal("refresh() against hung endpoint = nil, want timeout error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("metrics request exceeded bound: %v", elapsed)
	}
}

// spec: marketplace-api/aggregate-is-evaluated-with-concurrent-backend-requests
func TestInstallationMetricsFetchEvaluatesOperandsConcurrently(t *testing.T) {
	var arrived atomic.Int32
	allArrived := make(chan struct{})
	fixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), adoptionFixedNow) })
	fixture.handler.Store(func(w http.ResponseWriter, r *http.Request) {
		if arrived.Add(1) == adoptionRefreshRequests {
			close(allArrived)
		}
		select {
		case <-allArrived:
		case <-time.After(5 * time.Second):
			http.Error(w, "operands arrived sequentially", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, operandVector(prometheusVector(adoptionSamples(), adoptionFixedNow), r.Form.Get("query")))
	})
	reader := newTestReader(t, fixture)
	registries := adoptionTestRegistries()
	evalTime := adoptionFixedNow.Add(500 * time.Millisecond)

	if _, err := reader.fetch(context.Background(), registries, evalTime); err != nil {
		t.Fatalf("fetch() error = %v, want operands evaluated concurrently", err)
	}
	expected := installationMetricsQueries(reader.labels, newInstallationQueryPlan(registries, reader.marketplaceID))
	seen := make(map[string]bool)
	for i := 0; i < len(expected); i++ {
		request := <-fixture.requests
		if request.Form.Get("time") != fmt.Sprintf("%d", evalTime.Unix()) {
			t.Fatalf("operand time = %q, want shared evaluation time %d", request.Form.Get("time"), evalTime.Unix())
		}
		query := request.Form.Get("query")
		if !slices.Contains(expected, query) {
			t.Fatalf("request carried a query that is not one operand: %q", query)
		}
		seen[query] = true
	}
	if len(seen) != len(expected) {
		t.Fatalf("distinct operands requested = %d, want %d", len(seen), len(expected))
	}
}

// spec: marketplace-api/aggregate-is-evaluated-with-concurrent-backend-requests
func TestInstallationMetricsFetchBudgetAppliesToCombinedResponses(t *testing.T) {
	perOperand := installationMetricsMaxBody/int(adoptionRefreshRequests) + 1
	tests := []struct {
		name    string
		padding int
		wantErr bool
	}{
		{name: "combined responses within budget", padding: installationMetricsMaxBody / (2 * int(adoptionRefreshRequests))},
		{name: "combined responses over budget", padding: perOperand, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), adoptionFixedNow) })
			fixture.handler.Store(func(w http.ResponseWriter, r *http.Request) {
				body := operandVector(prometheusVector(adoptionSamples(), adoptionFixedNow), r.Form.Get("query"))
				_, _ = fmt.Fprint(w, strings.Replace(body, "{", "{"+strings.Repeat(" ", test.padding), 1))
			})
			reader := newTestReader(t, fixture)
			_, err := reader.fetch(context.Background(), adoptionTestRegistries(), adoptionFixedNow)
			if test.wantErr {
				if !errors.Is(err, errInstallationMetricsUnavailable) {
					t.Fatalf("fetch() error = %v, want errInstallationMetricsUnavailable for combined bodies over budget", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("fetch() error = %v, want combined bodies within budget to be accepted", err)
			}
		})
	}
}

// spec: marketplace-api/aggregate-is-evaluated-with-concurrent-backend-requests
func TestInstallationMetricsFetchOperandErrorPrecedence(t *testing.T) {
	good := prometheusVector(adoptionSamples(), adoptionFixedNow)
	tests := []struct {
		name    string
		respond func(w http.ResponseWriter, query string)
		want    error
	}{
		{name: "malformed beats unavailable", want: errInvalidInstallationMetrics, respond: func(w http.ResponseWriter, query string) {
			switch operandInventoryType.FindStringSubmatch(query)[1] {
			case "fresh":
				w.WriteHeader(http.StatusInternalServerError)
			case "invalid":
				_, _ = fmt.Fprint(w, `{`)
			default:
				_, _ = fmt.Fprint(w, operandVector(good, query))
			}
		}},
		{name: "one unavailable operand fails the refresh", want: errInstallationMetricsUnavailable, respond: func(w http.ResponseWriter, query string) {
			if operandInventoryType.FindStringSubmatch(query)[1] == "fresh" {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_, _ = fmt.Fprint(w, operandVector(good, query))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newMetricsFixture(t, func() string { return good })
			fixture.handler.Store(func(w http.ResponseWriter, r *http.Request) {
				test.respond(w, r.Form.Get("query"))
			})
			reader := newTestReader(t, fixture)
			reader.now = func() time.Time { return adoptionFixedNow }
			for i := 0; i < 5; i++ {
				if _, err := reader.fetch(context.Background(), adoptionTestRegistries(), adoptionFixedNow); !errors.Is(err, test.want) {
					t.Fatalf("fetch() error = %v, want %v", err, test.want)
				}
			}
		})
	}
}

// spec: marketplace-api/aggregate-is-evaluated-with-concurrent-backend-requests
func TestInstallationMetricsFailedOperandKeepsPublishedSnapshot(t *testing.T) {
	fixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), adoptionFixedNow) })
	reader := newTestReader(t, fixture)
	now := adoptionFixedNow
	reader.now = func() time.Time { return now }
	registries := adoptionTestRegistries()
	if _, err := reader.refresh(context.Background(), registries); err != nil {
		t.Fatalf("refresh() error = %v", err)
	}
	before := reader.snapshot.Load()
	fixture.handler.Store(func(w http.ResponseWriter, r *http.Request) {
		if operandInventoryType.FindStringSubmatch(r.Form.Get("query"))[1] == "fresh" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = fmt.Fprint(w, operandVector(prometheusVector(adoptionSamples(), adoptionFixedNow), r.Form.Get("query")))
	})
	now = adoptionFixedNow.Add(reader.timing.MaxAge + time.Minute)
	if _, err := reader.refresh(context.Background(), registries); !errors.Is(err, errInstallationMetricsUnavailable) {
		t.Fatalf("refresh() error = %v, want errInstallationMetricsUnavailable", err)
	}
	if reader.snapshot.Load() != before {
		t.Fatal("failed operand replaced the published snapshot")
	}
}
