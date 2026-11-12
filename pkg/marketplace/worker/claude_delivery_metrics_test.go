package worker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/claudeplugins"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
)

// spec: claude-enterprise-delivery/status-reports-a-failure-by-code-without-content
func TestClaudeDeliveryMetricsCaptureOutcomesAndSanitizedAPICalls(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/organizations/plugins" || r.Header.Get("x-api-key") != "sk-ant-metric-secret" {
			http.Error(w, `{"error":"unexpected request"}`, http.StatusBadRequest)
			return
		}
		if attempts.Add(1) == 1 {
			w.Header().Set("retry-after", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"details":{"error_code":"rate_limited"}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[],"next_page":null}`))
	}))
	defer server.Close()

	metrics := NewMetrics()
	registry := prometheus.NewRegistry()
	registry.MustRegister(metrics.Collectors()...)
	client, err := claudeplugins.NewClient(claudeplugins.Options{
		BaseURL: server.URL, APIKey: claudeplugins.NewAPIKey("sk-ant-metric-secret"),
		OnResponse: func(endpoint string, status int) {
			metrics.RecordClaudeAPIRequest(endpoint, status)
			if status == http.StatusTooManyRequests {
				metrics.RecordClaudeDeliveryThrottled()
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListPlugins(context.Background(), claudeplugins.PluginListQuery{}); err != nil {
		t.Fatalf("list plugins: %v", err)
	}
	metrics.RecordClaudeDeliveryReconcile("succeeded", 250*time.Millisecond)
	metrics.SetClaudeDeliveryPluginStates(map[string]int{
		"published": 3, "retired": 1, "skipped": 0, "conflict": 0, "failed": 1,
	})

	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]*dto.MetricFamily, len(families))
	for _, family := range families {
		byName[family.GetName()] = family
	}
	assertMetric := func(name string, labels map[string]string) *dto.Metric {
		t.Helper()
		family := byName[name]
		if family == nil {
			t.Fatalf("metric family %q is absent", name)
		}
		for _, metric := range family.Metric {
			got := make(map[string]string, len(metric.Label))
			for _, label := range metric.Label {
				got[label.GetName()] = label.GetValue()
			}
			if reflect.DeepEqual(got, labels) {
				return metric
			}
		}
		t.Fatalf("metric %q with labels %v is absent", name, labels)
		return nil
	}
	if got := assertMetric("agent_marketplace_claude_delivery_reconciles_total", map[string]string{"outcome": "succeeded"}).GetCounter().GetValue(); got != 1 {
		t.Fatalf("reconcile outcome count = %v, want 1", got)
	}
	if got := assertMetric("agent_marketplace_claude_delivery_plugins", map[string]string{"state": "published"}).GetGauge().GetValue(); got != 3 {
		t.Fatalf("published plugin count = %v, want 3", got)
	}
	for _, status := range []string{"429", "200"} {
		if got := assertMetric("agent_marketplace_claude_plugins_api_requests_total", map[string]string{
			"endpoint": "plugins", "status": status,
		}).GetCounter().GetValue(); got != 1 {
			t.Fatalf("API request count for status %s = %v, want 1", status, got)
		}
	}
	if got := testutil.ToFloat64(metrics.claudeDeliveryThrottled); got != 1 {
		t.Fatalf("throttled request count = %v, want 1", got)
	}
	duration := assertMetric("agent_marketplace_claude_delivery_pass_duration_seconds", map[string]string{}).GetHistogram()
	if duration.GetSampleCount() != 1 || duration.GetSampleSum() != 0.25 {
		t.Fatalf("pass duration histogram = %+v", duration)
	}
	for _, family := range families {
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				if strings.Contains(label.GetName(), "resource") || strings.Contains(label.GetName(), "registry") ||
					strings.Contains(label.GetName(), "plugin") || strings.Contains(label.GetValue(), "sk-ant") {
					t.Fatalf("metric %s contains an identity or credential label: %s=%s", family.GetName(), label.GetName(), label.GetValue())
				}
			}
		}
	}
}
