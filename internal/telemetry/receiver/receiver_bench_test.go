package receiver

import (
	"net/http/httptest"
	"testing"
	"time"
)

func BenchmarkParseResourceMetrics(b *testing.B) {
	s, err := New(Config{StorageDir: b.TempDir(), Logger: discardLogger()})
	if err != nil {
		b.Fatal(err)
	}
	metrics := makeMetricsRequest("benchmark", "sync", "factory,devin").ResourceMetrics[0]

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.parseResourceMetrics(metrics); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPrometheusMetrics(b *testing.B) {
	s, err := New(Config{StorageDir: b.TempDir(), Logger: discardLogger()})
	if err != nil {
		b.Fatal(err)
	}
	parsed, err := s.parseResourceMetrics(makeMetricsRequest("benchmark", "sync", "factory,devin").ResourceMetrics[0])
	if err != nil {
		b.Fatal(err)
	}
	s.updateCounts(parsed, time.Now())

	request := httptest.NewRequest("GET", "/metrics", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.handlePrometheusMetrics(httptest.NewRecorder(), request)
	}
}
