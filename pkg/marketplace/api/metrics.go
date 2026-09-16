package api

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	publicListener     = "public"
	operationsListener = "operations"
	unmatchedRoute     = "unmatched"
)

type serverMetrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	active   *prometheus.GaugeVec
	scrape   http.Handler
}

func newServerMetrics() *serverMetrics {
	metrics := &serverMetrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "agent_marketplace_requests_total", Help: "Total HTTP requests."}, []string{"listener", "method", "route", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "agent_marketplace_request_duration_seconds", Help: "HTTP request duration in seconds.", Buckets: prometheus.DefBuckets}, []string{"listener", "method", "route"}),
		active:   prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "agent_marketplace_active_requests", Help: "Active HTTP requests."}, []string{"listener"}),
	}
	registry := prometheus.NewRegistry()
	registry.MustRegister(metrics.requests, metrics.duration, metrics.active, collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	metrics.scrape = promhttp.HandlerFor(registry, promhttp.HandlerOpts{EnableOpenMetrics: true})
	return metrics
}

func (m *serverMetrics) register(mux *http.ServeMux, listener, pattern string, handler http.Handler) {
	mux.Handle(pattern, m.instrument(listener, pattern, handler))
}

func (m *serverMetrics) registerUnmatched(mux *http.ServeMux, listener string) {
	mux.Handle("/", m.instrument(listener, unmatchedRoute, http.NotFoundHandler()))
}

func (m *serverMetrics) instrument(listener, route string, handler http.Handler) http.Handler {
	labels := prometheus.Labels{"listener": listener, "route": route}
	return promhttp.InstrumentHandlerInFlight(
		m.active.WithLabelValues(listener),
		promhttp.InstrumentHandlerDuration(
			m.duration.MustCurryWith(labels),
			promhttp.InstrumentHandlerCounter(m.requests.MustCurryWith(labels), handler),
		),
	)
}
