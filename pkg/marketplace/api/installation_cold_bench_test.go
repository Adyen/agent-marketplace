package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
)

func benchColdInventory(now time.Time) string {
	samples := []inventoryMetricSample{
		aggregateResponseSample("fresh", 1, nil),
		aggregateResponseSample("valid_until", float64(now.Add(-time.Hour).UnixMicro()), nil),
	}
	for i := 0; i < 100; i++ {
		samples = append(samples, aggregateResponseSample("count", 1, map[string]string{
			"am_registry":   "bench-source",
			"resource_kind": "skill",
			"resource_name": fmt.Sprintf("skill-%04d", i),
		}))
	}
	return prometheusVector(samples, now)
}

func BenchmarkSearchHandlerInstallationsCold(b *testing.B) {
	server := benchSearchServer(b, 100)
	body := benchColdInventory(time.Now())
	telemetry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(25 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, body)
	}))
	b.Cleanup(telemetry.Close)
	server.installations = newInstallationMetricsReader(config.InstallationMetrics{URL: telemetry.URL}, "bench-mp")
	handler := server.PublicHandler()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/search?sort=installations", nil)
	validateSearchBenchmark(b, handler, request, 100)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		server.installations = newInstallationMetricsReader(config.InstallationMetrics{URL: telemetry.URL}, "bench-mp")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			b.Fatalf("status %d", recorder.Code)
		}
	}
}
