package api

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

type metricsResponse struct {
	status int
	header http.Header
	body   []byte
}

func TestServerPrometheusMetrics(t *testing.T) {
	public, operations := newMetricsTestServers(t, "metrics-contract")

	if response := requestMetrics(t, public.Client(), http.MethodGet, public.URL+"/api/v1/capabilities", ""); response.status != http.StatusOK {
		t.Fatalf("capabilities GET status = %d, want %d", response.status, http.StatusOK)
	}
	if response := requestMetrics(t, public.Client(), http.MethodPost, public.URL+"/api/v1/capabilities", ""); response.status != http.StatusMethodNotAllowed {
		t.Fatalf("capabilities POST status = %d, want %d", response.status, http.StatusMethodNotAllowed)
	}
	const rawPath = "/private/raw-path-secret-123"
	if response := requestMetrics(t, public.Client(), http.MethodGet, public.URL+rawPath, ""); response.status != http.StatusNotFound {
		t.Fatalf("unmatched status = %d, want %d", response.status, http.StatusNotFound)
	}
	if response := requestMetrics(t, public.Client(), http.MethodGet, public.URL+"/metrics", ""); response.status != http.StatusNotFound {
		t.Fatalf("public metrics status = %d, want %d", response.status, http.StatusNotFound)
	}

	response := requestMetrics(t, operations.Client(), http.MethodGet, operations.URL+"/metrics", "")
	if response.status != http.StatusOK {
		t.Fatalf("operations metrics status = %d, want %d", response.status, http.StatusOK)
	}
	if contentType := response.header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/plain;") {
		t.Fatalf("operations metrics Content-Type = %q, want Prometheus text", contentType)
	}
	if strings.Contains(string(response.body), rawPath) {
		t.Fatalf("metrics scrape contains raw unmatched path %q", rawPath)
	}
	families := parseMetricFamilies(t, response.body)

	requests := requireMetricFamily(t, families, "agent_marketplace_requests_total")
	requireMetric(t, requests, map[string]string{
		"code": "200", "listener": publicListener, "method": "get", "route": "/api/v1/capabilities",
	})
	requireMetric(t, requests, map[string]string{
		"code": "405", "listener": publicListener, "method": "post", "route": "/api/v1/capabilities",
	})
	requireMetric(t, requests, map[string]string{
		"code": "404", "listener": publicListener, "method": "get", "route": unmatchedRoute,
	})

	duration := requireMetric(t, requireMetricFamily(t, families, "agent_marketplace_request_duration_seconds"), map[string]string{
		"listener": publicListener, "method": "get", "route": "/api/v1/capabilities",
	})
	if duration.GetHistogram().GetSampleCount() == 0 {
		t.Fatal("capabilities duration histogram sample count = 0")
	}
	if len(duration.GetHistogram().GetBucket()) == 0 {
		t.Fatal("capabilities duration histogram has no buckets")
	}

	active := requireMetricFamily(t, families, "agent_marketplace_active_requests")
	if value := requireMetric(t, active, map[string]string{"listener": publicListener}).GetGauge().GetValue(); value != 0 {
		t.Fatalf("public active requests = %v, want 0", value)
	}
	if value := requireMetric(t, active, map[string]string{"listener": operationsListener}).GetGauge().GetValue(); value != 1 {
		t.Fatalf("operations active requests during scrape = %v, want 1", value)
	}

	requireMetricFamily(t, families, "go_goroutines")
	requireMetricFamily(t, families, "process_cpu_seconds_total")
}

func TestServerPrometheusMuxRedirectsInstrumented(t *testing.T) {
	public, operations := newMetricsTestServers(t, "metrics-mux-redirects")

	// Disable redirect following per probe: following the redirect would
	// issue a second request whose observation could mask an uncounted
	// redirect response.
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	// Each probe requests a non-canonical spelling of a registered pattern,
	// so http.ServeMux itself answers with a redirect that never reaches a
	// registered route handler. The redirect is observed under the
	// registered pattern it targets, keeping route labels bounded.
	redirects := []struct {
		listener string
		server   *httptest.Server
		path     string
		route    string
	}{
		{publicListener, public, "/api/v1/resources", "/api/v1/resources/"},
		{publicListener, public, "/api//v1/capabilities", "/api/v1/capabilities"},
		{operationsListener, operations, "//metrics", "/metrics"},
	}
	for _, probe := range redirects {
		response := requestMetrics(t, noFollow, http.MethodGet, probe.server.URL+probe.path, "")
		if response.status != http.StatusTemporaryRedirect {
			t.Fatalf("mux redirect %s%s status = %d, want %d", probe.listener, probe.path, response.status, http.StatusTemporaryRedirect)
		}
	}

	families := parseMetricFamilies(t, requestMetrics(t, operations.Client(), http.MethodGet, operations.URL+"/metrics", "").body)
	requests := requireMetricFamily(t, families, "agent_marketplace_requests_total")
	durations := requireMetricFamily(t, families, "agent_marketplace_request_duration_seconds")
	for _, probe := range redirects {
		if value := requireMetric(t, requests, map[string]string{
			"code": "307", "listener": probe.listener, "method": "get", "route": probe.route,
		}).GetCounter().GetValue(); value != 1 {
			t.Fatalf("%s%s redirect counted %v times, want exactly 1", probe.listener, probe.path, value)
		}
		histogram := requireMetric(t, durations, map[string]string{
			"listener": probe.listener, "method": "get", "route": probe.route,
		}).GetHistogram()
		if count := histogram.GetSampleCount(); count < 1 {
			t.Fatalf("%s%s redirect duration samples = %d, want at least 1", probe.listener, probe.path, count)
		}
	}
}

func TestServerPrometheusNegotiation(t *testing.T) {
	public, operations := newMetricsTestServers(t, "metrics-negotiation")

	response := requestMetrics(t, operations.Client(), http.MethodGet, operations.URL+"/metrics", "application/openmetrics-text")
	if response.status != http.StatusOK {
		t.Fatalf("OpenMetrics status = %d, want %d", response.status, http.StatusOK)
	}
	if contentType := response.header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/openmetrics-text;") {
		t.Fatalf("OpenMetrics Content-Type = %q", contentType)
	}
	if !bytes.HasSuffix(response.body, []byte("# EOF\n")) {
		t.Fatalf("OpenMetrics body does not end with EOF marker: %q", response.body[len(response.body)-min(len(response.body), 32):])
	}

	response = requestMetrics(t, operations.Client(), http.MethodHead, operations.URL+"/metrics", "")
	if response.status != http.StatusOK {
		t.Fatalf("HEAD metrics status = %d, want %d", response.status, http.StatusOK)
	}
	if len(response.body) != 0 {
		t.Fatalf("HEAD metrics body length = %d, want 0", len(response.body))
	}

	response = requestMetrics(t, public.Client(), http.MethodGet, public.URL+"/metrics", "")
	if response.status != http.StatusNotFound {
		t.Fatalf("public metrics status = %d, want %d", response.status, http.StatusNotFound)
	}
}

func TestServerPrometheusMetricsAreIsolated(t *testing.T) {
	firstPublic, firstOperations := newMetricsTestServers(t, "metrics-isolation-first")
	_, secondOperations := newMetricsTestServers(t, "metrics-isolation-second")

	if response := requestMetrics(t, firstPublic.Client(), http.MethodGet, firstPublic.URL+"/api/v1/capabilities", ""); response.status != http.StatusOK {
		t.Fatalf("first capabilities status = %d, want %d", response.status, http.StatusOK)
	}

	labels := map[string]string{
		"code": "200", "listener": publicListener, "method": "get", "route": "/api/v1/capabilities",
	}
	firstFamilies := parseMetricFamilies(t, requestMetrics(t, firstOperations.Client(), http.MethodGet, firstOperations.URL+"/metrics", "").body)
	firstMetric := requireMetric(t, requireMetricFamily(t, firstFamilies, "agent_marketplace_requests_total"), labels)
	if value := firstMetric.GetCounter().GetValue(); value != 1 {
		t.Fatalf("first server capabilities count = %v, want 1", value)
	}

	secondResponse := requestMetrics(t, secondOperations.Client(), http.MethodGet, secondOperations.URL+"/metrics", "")
	secondFamilies := parseMetricFamilies(t, secondResponse.body)
	if family := secondFamilies["agent_marketplace_requests_total"]; matchingMetric(family, labels) != nil {
		t.Fatal("second server contains the first server's capabilities request")
	}
}

func newMetricsTestServers(t *testing.T, instanceID string) (*httptest.Server, *httptest.Server) {
	t.Helper()
	server := NewServer(config.Config{
		Instance:         config.Instance{ID: instanceID},
		Listen:           "127.0.0.1:0",
		OperationsListen: "127.0.0.1:0",
	}, nil, nil)
	public := httptest.NewServer(server.PublicHandler())
	operations := httptest.NewServer(server.OpsHandler())
	t.Cleanup(public.Close)
	t.Cleanup(operations.Close)
	return public, operations
}

func requestMetrics(t *testing.T, client *http.Client, method, url, accept string) metricsResponse {
	t.Helper()
	request, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatalf("create %s request: %v", method, err)
	}
	if accept != "" {
		request.Header.Set("Accept", accept)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("perform %s request: %v", method, err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s response: %v", method, err)
	}
	return metricsResponse{status: response.StatusCode, header: response.Header.Clone(), body: body}
}

func parseMetricFamilies(t *testing.T, body []byte) map[string]*dto.MetricFamily {
	t.Helper()
	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("parse Prometheus metrics: %v", err)
	}
	return families
}

func requireMetricFamily(t *testing.T, families map[string]*dto.MetricFamily, name string) *dto.MetricFamily {
	t.Helper()
	family := families[name]
	if family == nil {
		t.Fatalf("metric family %q is absent", name)
	}
	return family
}

func requireMetric(t *testing.T, family *dto.MetricFamily, labels map[string]string) *dto.Metric {
	t.Helper()
	metric := matchingMetric(family, labels)
	if metric == nil {
		t.Fatalf("metric family %q has no series with labels %v", family.GetName(), labels)
	}
	return metric
}

func matchingMetric(family *dto.MetricFamily, labels map[string]string) *dto.Metric {
	if family == nil {
		return nil
	}
	for _, metric := range family.GetMetric() {
		values := make(map[string]string, len(metric.GetLabel()))
		for _, label := range metric.GetLabel() {
			values[label.GetName()] = label.GetValue()
		}
		if len(values) != len(labels) {
			continue
		}
		matched := true
		for name, value := range labels {
			if values[name] != value {
				matched = false
				break
			}
		}
		if matched {
			return metric
		}
	}
	return nil
}
