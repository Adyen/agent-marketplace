package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	if hits := fixture.hits.Load(); hits != 1 {
		t.Fatalf("metrics endpoint hits = %d, want 1", hits)
	}
	now = adoptionFixedNow.Add(5*time.Minute - time.Nanosecond)
	if _, ok := reader.lookup(registries); !ok {
		t.Fatal("lookup() dropped a snapshot inside its freshness window")
	}
	if hits := fixture.hits.Load(); hits != 1 {
		t.Fatalf("metrics endpoint hits inside cache window = %d, want 1", hits)
	}
	now = adoptionFixedNow.Add(5 * time.Minute)
	if _, ok := reader.lookup(registries); ok {
		t.Fatal("lookup() served a snapshot at its exact max_age expiry boundary")
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
	if _, ok := reader.lookup(registries); ok {
		t.Fatal("lookup() after expiry with failing metrics still served counts")
	}
	if hits := fixture.hits.Load(); hits != 2 {
		t.Fatalf("metrics endpoint hits = %d, want 2", hits)
	}
	if delay := reader.attempt(context.Background(), staticRegistryIndex(registries), true); delay != reader.timing.RetryInterval {
		t.Fatalf("hint inside failure backoff delay = %v, want remaining backoff %v", delay, reader.timing.RetryInterval)
	}
	if hits := fixture.hits.Load(); hits != 2 {
		t.Fatalf("failure backoff did not hold for 30s: hits = %d, want 2", hits)
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
	counts, ok := reader.lookup(registries)
	if !ok {
		t.Fatal("lookup() after refresh returned no snapshot")
	}
	if counts.ValidUntil.After(now.Add(5 * time.Minute)) {
		t.Fatalf("ValidUntil %v beyond five-minute freshness", counts.ValidUntil)
	}
	now = now.Add(3 * time.Minute)
	if _, ok := reader.lookup(registries); ok {
		t.Fatal("lookup() reused an aggregate past its inventory validity horizon")
	}
	reader.attempt(context.Background(), staticRegistryIndex(registries), false)
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
	if hits := fixture.hits.Load(); hits != 1 {
		t.Fatalf("identity-mismatched lookup issued telemetry: hits = %d, want 1", hits)
	}
	fixture.handler.Store(func(w http.ResponseWriter, _ *http.Request) {
		samples := []inventoryMetricSample{
			aggregateResponseSample("fresh", 1, nil),
			aggregateResponseSample("valid_until", float64(adoptionFixedNow.UnixMicro()), nil),
		}
		_, _ = fmt.Fprint(w, prometheusVector(samples, adoptionFixedNow))
	})
	if _, err := reader.refresh(context.Background(), changed); err != nil {
		t.Fatalf("refresh() after origin change error = %v", err)
	}
	_, ok := reader.lookup(changed)
	if !ok {
		t.Fatal("lookup() after origin change refresh returned no snapshot")
	}
	if hits := fixture.hits.Load(); hits != 2 {
		t.Fatalf("origin change did not invalidate aggregate cache: hits = %d, want 2", hits)
	}
	<-fixture.requests
	query := (<-fixture.requests).Form.Get("query")
	if !strings.Contains(query, "team/moved-alpha") {
		t.Fatalf("query after origin change omitted moved identity: %q", query)
	}
	if strings.Contains(query, "team/alpha") {
		t.Fatalf("query after origin change retained old identity example.test/team/alpha: %q", query)
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
	if _, err := slowReader.refresh(context.Background(), adoptionTestRegistries()); err == nil {
		t.Fatal("refresh() against hung endpoint = nil, want timeout error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("metrics request exceeded bound: %v", elapsed)
	}
}
