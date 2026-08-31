package receiver

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

func BenchmarkParseResourceMetrics(b *testing.B) {
	s, err := New(Config{StorageDir: b.TempDir(), Logger: discardLogger()})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = s.Close() })
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
	for _, series := range []int{14, 9998} {
		b.Run(fmt.Sprintf("series-%d", series), func(b *testing.B) {
			s, err := New(Config{StorageDir: b.TempDir(), Logger: discardLogger()})
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = s.Close() })
			commands := 1
			if series > 14 {
				commands = 769
			}
			for i := range commands {
				parsed, err := s.parseResourceMetrics(makeMetricsRequest("benchmark", fmt.Sprintf("command-%d", i), "factory").ResourceMetrics[0])
				if err != nil {
					b.Fatal(err)
				}
				s.updateCounts(parsed, time.Now())
			}
			if got := s.prometheusSeriesUsedLocked(); got != series {
				b.Fatalf("series = %d, want %d", got, series)
			}

			request := httptest.NewRequest("GET", "/metrics", nil)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s.handlePrometheusMetrics(httptest.NewRecorder(), request)
			}
		})
	}
}
