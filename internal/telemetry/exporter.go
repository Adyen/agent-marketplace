package telemetry

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"

	otlpmetrichttp "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	otlptracehttp "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// exporterCache deduplicates OTLP/HTTP exporters by endpoint, resolved
// headers, CA file, insecure flag, and signal (metrics/traces). It is safe for
// concurrent use.
type exporterCache struct {
	mu      sync.Mutex
	metrics map[cacheKey]sdkmetric.Exporter
	traces  map[cacheKey]sdktrace.SpanExporter
	senders map[cacheKey]*http.Client
}

// transportCacheKey identifies an HTTP transport that can be shared by live
// and drain clients for the same endpoint/TLS configuration.
type transportCacheKey struct {
	endpoint string
	caFile   string
}

var (
	transportCacheMu sync.Mutex
	transportCache   = make(map[transportCacheKey]*http.Transport)
)

type cacheKey struct {
	endpoint   string
	headerHash string
	caFile     string
	insecure   bool
	signal     string
}

func newExporterCache() *exporterCache {
	return &exporterCache{
		metrics: make(map[cacheKey]sdkmetric.Exporter),
		traces:  make(map[cacheKey]sdktrace.SpanExporter),
		senders: make(map[cacheKey]*http.Client),
	}
}

func (c *exporterCache) metricExporter(ctx context.Context, key cacheKey, build func() (sdkmetric.Exporter, *http.Client, error)) (sdkmetric.Exporter, *http.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if e, ok := c.metrics[key]; ok {
		return e, c.senders[key], nil
	}

	e, sender, err := build()
	if err != nil {
		return nil, nil, err
	}

	c.metrics[key] = e
	c.senders[key] = sender
	return e, sender, nil
}

func (c *exporterCache) traceExporter(ctx context.Context, key cacheKey, build func() (sdktrace.SpanExporter, *http.Client, error)) (sdktrace.SpanExporter, *http.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if e, ok := c.traces[key]; ok {
		return e, c.senders[key], nil
	}

	e, sender, err := build()
	if err != nil {
		return nil, nil, err
	}

	c.traces[key] = e
	c.senders[key] = sender
	return e, sender, nil
}

func cacheKeyFor(endpoint string, headers map[string]string, caFile string, allowInsecure bool, signal string) cacheKey {
	return cacheKey{
		endpoint:   endpoint,
		headerHash: headerKey(headers),
		caFile:     caFile,
		insecure:   allowInsecure,
		signal:     signal,
	}
}

// headerKey returns an unambiguous encoding of the header map. It sorts keys
// and emits JSON so keys or values containing delimiters cannot collide.
func headerKey(headers map[string]string) string {
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	type kv struct {
		Key   string `json:"k"`
		Value string `json:"v"`
	}
	pairs := make([]kv, len(keys))
	for i, k := range keys {
		pairs[i] = kv{Key: k, Value: headers[k]}
	}

	b, _ := json.Marshal(pairs)
	return string(b)
}

func resolveHeaders(sources map[string]tmcfg.HeaderSource) (map[string]string, error) {
	out := make(map[string]string, len(sources))
	for name, src := range sources {
		v, err := resolveHeaderValue(src)
		if err != nil {
			return nil, fmt.Errorf("resolve telemetry header %q: %w", name, err)
		}
		out[name] = v
	}
	return out, nil
}

func resolveHeaderValue(src tmcfg.HeaderSource) (string, error) {
	switch kind, ref := src.Source(); kind {
	case "env":
		return os.Getenv(ref), nil
	case "file":
		b, err := os.ReadFile(ref)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	default:
		return "", errors.New("no header source configured")
	}
}

// tlsConfig returns a TLS configuration that uses system trust plus an optional
// CA file. It never enables InsecureSkipVerify.
func tlsConfig(caFile string) (*tls.Config, error) {
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	if caFile == "" {
		return cfg, nil
	}

	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}

	b, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read ca_file: %w", err)
	}

	if !pool.AppendCertsFromPEM(b) {
		return nil, fmt.Errorf("failed to append certificates from ca_file %q", caFile)
	}

	cfg.RootCAs = pool
	return cfg, nil
}

// buildRouteClients builds the HTTP clients used for live exports and for
// draining queued payloads to endpoint. The live client queues failed payloads.
func buildRouteClients(endpoint string, headers map[string]string, caFile string, allowInsecure bool, route, signal string, onFailure func(error), q *Queue) (*http.Client, *http.Client, error) {
	slog.Info("building route clients", "route", route, "signal", signal, "endpoint", endpoint)

	if err := tmcfg.ValidateEndpoint(endpoint, allowInsecure); err != nil {
		slog.Error("invalid telemetry endpoint", "endpoint", endpoint, "error", err)
		return nil, nil, err
	}

	_, err := url.Parse(endpoint)
	if err != nil {
		slog.Error("failed to parse telemetry endpoint", "endpoint", endpoint, "error", err)
		return nil, nil, fmt.Errorf("parse endpoint: %w", err)
	}

	tkey := transportCacheKey{endpoint: endpoint, caFile: caFile}
	transportCacheMu.Lock()
	innerTransport, ok := transportCache[tkey]
	if !ok {
		tlsCfg, err := tlsConfig(caFile)
		if err != nil {
			transportCacheMu.Unlock()
			slog.Error("failed to build tls config", "ca_file", caFile, "error", err)
			return nil, nil, err
		}

		base, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			base = &http.Transport{}
		}
		innerTransport = base.Clone()
		innerTransport.TLSClientConfig = tlsCfg
		transportCache[tkey] = innerTransport
	}
	transportCacheMu.Unlock()

	live := &http.Client{
		Transport: &queueTransport{
			inner:     innerTransport,
			queue:     q,
			route:     route,
			signal:    signal,
			onFailure: onFailure,
		},
	}
	send := &http.Client{
		Transport: innerTransport,
		Timeout:   30 * time.Second,
	}

	slog.Info("route clients built", "route", route, "signal", signal)
	return live, send, nil
}

// buildSignalEndpoint normalizes an endpoint URL with the default path for the
// given signal (metrics or traces).
func buildSignalEndpoint(endpoint, signal string) (string, error) {
	switch signal {
	case queueSubdirMetrics:
		return endpointWithDefaultPath(endpoint, "/v1/metrics")
	case queueSubdirTraces:
		return endpointWithDefaultPath(endpoint, "/v1/traces")
	default:
		return "", fmt.Errorf("unsupported queue signal %q", signal)
	}
}

func buildMetricExporter(ctx context.Context, live *http.Client, endpoint string, headers map[string]string) (sdkmetric.Exporter, error) {
	slog.Debug("building metric exporter", "endpoint", endpoint)

	ep, err := buildSignalEndpoint(endpoint, queueSubdirMetrics)
	if err != nil {
		slog.Error("failed to build metric endpoint", "endpoint", endpoint, "error", err)
		return nil, err
	}

	exp, err := otlpmetrichttp.New(ctx,
		otlpmetrichttp.WithHTTPClient(live),
		otlpmetrichttp.WithEndpointURL(ep),
		otlpmetrichttp.WithHeaders(headers),
		otlpmetrichttp.WithCompression(otlpmetrichttp.NoCompression),
		otlpmetrichttp.WithRetry(otlpmetrichttp.RetryConfig{Enabled: false}),
		otlpmetrichttp.WithTimeout(30*time.Second),
		otlpmetrichttp.WithTemporalitySelector(sdkmetric.DefaultTemporalitySelector),
		otlpmetrichttp.WithAggregationSelector(sdkmetric.DefaultAggregationSelector),
	)
	if err != nil {
		slog.Error("failed to build metric exporter", "endpoint", endpoint, "error", err)
		return nil, err
	}

	slog.Debug("metric exporter built", "endpoint", endpoint)
	return exp, nil
}

func buildTraceExporter(ctx context.Context, live *http.Client, endpoint string, headers map[string]string) (sdktrace.SpanExporter, error) {
	slog.Debug("building trace exporter", "endpoint", endpoint)

	ep, err := buildSignalEndpoint(endpoint, queueSubdirTraces)
	if err != nil {
		slog.Error("failed to build trace endpoint", "endpoint", endpoint, "error", err)
		return nil, err
	}

	exp, err := otlptracehttp.New(ctx,
		otlptracehttp.WithHTTPClient(live),
		otlptracehttp.WithEndpointURL(ep),
		otlptracehttp.WithHeaders(headers),
		otlptracehttp.WithCompression(otlptracehttp.NoCompression),
		otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: false}),
		otlptracehttp.WithTimeout(30*time.Second),
	)
	if err != nil {
		slog.Error("failed to build trace exporter", "endpoint", endpoint, "error", err)
		return nil, err
	}

	slog.Debug("trace exporter built", "endpoint", endpoint)
	return exp, nil
}

func endpointWithDefaultPath(endpoint, defaultPath string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse endpoint: %w", err)
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = defaultPath
	}
	return u.String(), nil
}

// queueTransport intercepts OTLP/HTTP requests. Successful requests pass through
// to the inner RoundTripper. Failed requests are serialized to the durable
// queue for later delivery.
type queueTransport struct {
	inner     http.RoundTripper
	queue     *Queue
	route     string
	signal    string
	onFailure func(error)
}

func (qt *queueTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, errors.New("nil request")
	}

	ctx := req.Context()

	slog.Debug("export round trip", "route", qt.route, "signal", qt.signal, "url_host", req.URL.Host)

	var payload []byte
	if req.Body != nil && req.Body != http.NoBody {
		var err error
		payload, err = io.ReadAll(req.Body)
		if err != nil {
			slog.Error("failed to read request body", "url_host", req.URL.Host, "error", err)
			return nil, err
		}
		_ = req.Body.Close()
	}

	innerReq := req.Clone(ctx)
	innerReq.Body = io.NopCloser(bytes.NewReader(payload))
	innerReq.ContentLength = int64(len(payload))
	innerReq.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	}

	resp, err := qt.inner.RoundTrip(innerReq)
	if err != nil || (resp != nil && resp.StatusCode/100 != 2) {
		exportErr := err
		if exportErr == nil && resp != nil {
			exportErr = fmt.Errorf("server returned %s", resp.Status)
		}

		slog.Error("export failed, enqueueing payload for retry",
			"route", qt.route,
			"signal", qt.signal,
			"url_host", req.URL.Host,
			"error", exportErr,
		)

		if qt.onFailure != nil && exportErr != nil {
			qt.onFailure(exportErr)
		}

		env := Envelope{
			Route:   qt.route,
			Signal:  qt.signal,
			URL:     req.URL.String(),
			Headers: copyHeaderMap(req.Header),
			Payload: base64.StdEncoding.EncodeToString(payload),
			Created: time.Now().Unix(),
		}

		envBytes, marshalErr := json.Marshal(env)
		if marshalErr != nil {
			slog.Error("failed to marshal queued envelope",
				"route", qt.route,
				"signal", qt.signal,
				"url_host", req.URL.Host,
				"error", marshalErr,
			)
			if exportErr != nil {
				return nil, errors.Join(exportErr, marshalErr)
			}
			return nil, marshalErr
		}

		// Queue the payload even if the caller context was canceled; the data
		// still needs to be durably stored for the next attempt.
		qctx := context.WithoutCancel(ctx)
		if qerr := qt.queue.Push(qctx, Route{Name: qt.route, Signal: qt.signal}, envBytes); qerr != nil {
			slog.Error("failed to enqueue export payload",
				"route", qt.route,
				"signal", qt.signal,
				"url_host", req.URL.Host,
				"error", qerr,
			)
			if exportErr != nil {
				return nil, errors.Join(exportErr, qerr)
			}
			return nil, qerr
		}

		slog.Debug("enqueued failed export payload", "route", qt.route, "signal", qt.signal, "url_host", req.URL.Host)

		if resp != nil {
			_ = resp.Body.Close()
		}
		return nil, exportErr
	}

	slog.Debug("export succeeded", "route", qt.route, "signal", qt.signal, "url_host", req.URL.Host)
	return resp, nil
}

func copyHeaderMap(h http.Header) map[string][]string {
	out := make(map[string][]string, len(h))
	for k, vv := range h {
		if len(vv) == 0 {
			continue
		}
		out[k] = append([]string(nil), vv...)
	}
	return out
}
