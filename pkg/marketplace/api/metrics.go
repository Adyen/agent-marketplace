package api

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

const (
	publicListener     = "public"
	operationsListener = "operations"
	unmatchedRoute     = "unmatched"
	unknownLabel       = "unknown"
)

const (
	phaseAuthority           = "authority"
	phaseAvailability        = "availability"
	phaseCatalogRead         = "catalog_read"
	phaseCatalogAvailability = "catalog_availability"
	phaseCatalogResponse     = "catalog_response"
)

var requestPhaseBuckets = []float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

type serverMetrics struct {
	requests       *prometheus.CounterVec
	duration       *prometheus.HistogramVec
	active         *prometheus.GaugeVec
	publicDuration *prometheus.HistogramVec
	phases         *prometheus.HistogramVec

	installationRefresh     *prometheus.CounterVec
	installationRefreshSecs prometheus.Histogram
	installationLastSuccess prometheus.Gauge

	scrape http.Handler
}

func newServerMetrics(meta store.MetadataStore) *serverMetrics {
	metrics := &serverMetrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "agent_marketplace_requests_total", Help: "Total HTTP requests."}, []string{"listener", "method", "route", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "agent_marketplace_request_duration_seconds", Help: "HTTP request duration in seconds.", Buckets: prometheus.DefBuckets}, []string{"listener", "method", "route"}),
		active:   prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "agent_marketplace_active_requests", Help: "Active HTTP requests."}, []string{"listener"}),
		publicDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "agent_marketplace_public_request_duration_seconds", Help: "Full public request stack duration in seconds.", Buckets: requestPhaseBuckets,
		}, []string{"method", "route", "code"}),
		phases: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "agent_marketplace_request_phase_duration_seconds", Help: "Public request phase duration in seconds.", Buckets: requestPhaseBuckets,
		}, []string{"phase"}),
		installationRefresh: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_marketplace_installation_metrics_refresh_total", Help: "Background installation metrics refresh attempts by outcome.",
		}, []string{"outcome"}),
		installationRefreshSecs: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "agent_marketplace_installation_metrics_refresh_duration_seconds", Help: "Background installation metrics refresh duration in seconds.", Buckets: requestPhaseBuckets,
		}),
		installationLastSuccess: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "agent_marketplace_installation_metrics_last_success_timestamp_seconds", Help: "Unix timestamp of the last successful installation metrics evaluation.",
		}),
	}
	registry := prometheus.NewRegistry()
	registry.MustRegister(metrics.requests, metrics.duration, metrics.active, metrics.publicDuration, metrics.phases,
		metrics.installationRefresh, metrics.installationRefreshSecs, metrics.installationLastSuccess,
		collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	if pool, ok := meta.(interface{ PoolStats() sql.DBStats }); ok {
		registry.MustRegister(
			prometheus.NewCounterFunc(prometheus.CounterOpts{Name: "agent_marketplace_metadata_pool_waits_total", Help: "Total metadata connection pool waits."}, func() float64 {
				return float64(pool.PoolStats().WaitCount)
			}),
			prometheus.NewCounterFunc(prometheus.CounterOpts{Name: "agent_marketplace_metadata_pool_wait_seconds_total", Help: "Total metadata connection pool wait seconds."}, func() float64 {
				return pool.PoolStats().WaitDuration.Seconds()
			}),
		)
	}
	metrics.scrape = promhttp.HandlerFor(registry, promhttp.HandlerOpts{EnableOpenMetrics: true})
	return metrics
}

func (m *serverMetrics) observePhase(phase string, start time.Time) {
	m.phases.WithLabelValues(phase).Observe(time.Since(start).Seconds())
}

func (m *serverMetrics) observeInstallationRefresh(outcome string, started, completed, evaluatedAt time.Time) {
	m.installationRefresh.WithLabelValues(outcome).Inc()
	m.installationRefreshSecs.Observe(completed.Sub(started).Seconds())
	if !evaluatedAt.IsZero() {
		m.installationLastSuccess.Set(float64(evaluatedAt.Unix()))
	}
}

func (m *serverMetrics) publicStack(mux *http.ServeMux, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		_, route := mux.Handler(r)
		if route == "" {
			route = unmatchedRoute
		}
		capture := &statusCapture{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(capture, r)
		m.publicDuration.WithLabelValues(methodLabel(r.Method), route, codeLabel(capture.status)).Observe(time.Since(start).Seconds())
	})
}

// handler wraps one listener's mux so every response the server produces is
// observed exactly once, including responses http.ServeMux generates itself —
// trailing-slash and path-cleaning redirects and its built-in not-found reply
// — which never reach a registered route handler. ServeMux reports the
// registered pattern a redirect targets, so redirect observations carry that
// pattern; responses with no matching pattern, such as the built-in
// not-found reply, collapse to the unmatched label. Route label values
// therefore stay bounded to the registered patterns plus "unmatched", and raw
// request paths never become label values.
func (m *serverMetrics) handler(listener string, mux *http.ServeMux) http.Handler {
	active := m.active.WithLabelValues(listener)
	requests := m.requests.MustCurryWith(prometheus.Labels{"listener": listener})
	duration := m.duration.MustCurryWith(prometheus.Labels{"listener": listener})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		active.Inc()
		defer active.Dec()
		start := time.Now()
		capture := &statusCapture{ResponseWriter: w, status: http.StatusOK}
		mux.ServeHTTP(capture, r)
		route := r.Pattern
		if route == "" {
			route = unmatchedRoute
		}
		method := methodLabel(r.Method)
		requests.WithLabelValues(method, route, codeLabel(capture.status)).Inc()
		duration.WithLabelValues(method, route).Observe(time.Since(start).Seconds())
	})
}

// statusCapture records the status code the mux produced so mux-generated
// redirect and not-found responses still carry their code in metrics.
type statusCapture struct {
	http.ResponseWriter
	status int
}

func (c *statusCapture) WriteHeader(status int) {
	c.status = status
	c.ResponseWriter.WriteHeader(status)
}

// methodLabel and codeLabel mirror promhttp's instrument_server
// normalization (client_golang v1.24.1) so label values keep the documented
// contract: known HTTP methods are lowercased and other values collapse to
// "unknown", while status codes within 100-599 keep their decimal spelling
// and an unwritten status counts as 200.
func methodLabel(method string) string {
	switch method {
	case "GET", "get":
		return "get"
	case "PUT", "put":
		return "put"
	case "HEAD", "head":
		return "head"
	case "POST", "post":
		return "post"
	case "DELETE", "delete":
		return "delete"
	case "CONNECT", "connect":
		return "connect"
	case "OPTIONS", "options":
		return "options"
	case "NOTIFY", "notify":
		return "notify"
	case "TRACE", "trace":
		return "trace"
	case "PATCH", "patch":
		return "patch"
	default:
		return unknownLabel
	}
}

func codeLabel(code int) string {
	switch code {
	case 100:
		return "100"
	case 101:
		return "101"

	case 200, 0:
		return "200"
	case 201:
		return "201"
	case 202:
		return "202"
	case 203:
		return "203"
	case 204:
		return "204"
	case 205:
		return "205"
	case 206:
		return "206"

	case 300:
		return "300"
	case 301:
		return "301"
	case 302:
		return "302"
	case 304:
		return "304"
	case 305:
		return "305"
	case 307:
		return "307"

	case 400:
		return "400"
	case 401:
		return "401"
	case 402:
		return "402"
	case 403:
		return "403"
	case 404:
		return "404"
	case 405:
		return "405"
	case 406:
		return "406"
	case 407:
		return "407"
	case 408:
		return "408"
	case 409:
		return "409"
	case 410:
		return "410"
	case 411:
		return "411"
	case 412:
		return "412"
	case 413:
		return "413"
	case 414:
		return "414"
	case 415:
		return "415"
	case 416:
		return "416"
	case 417:
		return "417"
	case 418:
		return "418"

	case 428:
		return "428"
	case 429:
		return "429"
	case 431:
		return "431"

	case 500:
		return "500"
	case 501:
		return "501"
	case 502:
		return "502"
	case 503:
		return "503"
	case 504:
		return "504"
	case 505:
		return "505"
	case 511:
		return "511"

	default:
		if code >= 100 && code <= 599 {
			return strconv.Itoa(code)
		}
		return unknownLabel
	}
}
