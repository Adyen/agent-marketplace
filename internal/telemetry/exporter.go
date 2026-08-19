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

func headerKey(headers map[string]string) string {
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('|')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(headers[k])
	}
	return b.String()
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
func buildRouteClients(endpoint string, headers map[string]string, caFile string, allowInsecure bool, route string, q *Queue) (*http.Client, *http.Client, error) {
	if err := tmcfg.ValidateEndpoint(endpoint, allowInsecure); err != nil {
		return nil, nil, err
	}

	_, err := url.Parse(endpoint)
	if err != nil {
		return nil, nil, fmt.Errorf("parse endpoint: %w", err)
	}

	tlsCfg, err := tlsConfig(caFile)
	if err != nil {
		return nil, nil, err
	}

	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		base = &http.Transport{}
	}
	liveTransport := base.Clone()
	liveTransport.DisableKeepAlives = true
	liveTransport.TLSClientConfig = tlsCfg

	sendTransport := base.Clone()
	sendTransport.DisableKeepAlives = true
	sendTransport.TLSClientConfig = tlsCfg

	live := &http.Client{
		Transport: &queueTransport{inner: liveTransport, queue: q, route: route},
	}
	send := &http.Client{
		Transport: sendTransport,
		Timeout:   30 * time.Second,
	}

	return live, send, nil
}

func buildMetricExporter(ctx context.Context, live *http.Client, endpoint string, headers map[string]string) (sdkmetric.Exporter, error) {
	ep, err := endpointWithDefaultPath(endpoint, "/v1/metrics")
	if err != nil {
		return nil, err
	}
	return otlpmetrichttp.New(ctx,
		otlpmetrichttp.WithHTTPClient(live),
		otlpmetrichttp.WithEndpointURL(ep),
		otlpmetrichttp.WithHeaders(headers),
		otlpmetrichttp.WithCompression(otlpmetrichttp.NoCompression),
		otlpmetrichttp.WithRetry(otlpmetrichttp.RetryConfig{Enabled: false}),
	)
}

func buildTraceExporter(ctx context.Context, live *http.Client, endpoint string, headers map[string]string) (sdktrace.SpanExporter, error) {
	ep, err := endpointWithDefaultPath(endpoint, "/v1/traces")
	if err != nil {
		return nil, err
	}
	return otlptracehttp.New(ctx,
		otlptracehttp.WithHTTPClient(live),
		otlptracehttp.WithEndpointURL(ep),
		otlptracehttp.WithHeaders(headers),
		otlptracehttp.WithCompression(otlptracehttp.NoCompression),
		otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: false}),
	)
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
	inner http.RoundTripper
	queue *Queue
	route string
}

func (qt *queueTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, errors.New("nil request")
	}

	ctx := req.Context()

	var payload []byte
	if req.Body != nil && req.Body != http.NoBody {
		var err error
		payload, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		_ = req.Body.Close()
	}

	signal := signalFromPath(req.URL.Path)

	innerReq := req.Clone(ctx)
	innerReq.Body = io.NopCloser(bytes.NewReader(payload))
	innerReq.ContentLength = int64(len(payload))
	innerReq.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	}

	resp, err := qt.inner.RoundTrip(innerReq)
	if err != nil || (resp != nil && resp.StatusCode/100 != 2) {
		env := Envelope{
			Route:   qt.route,
			Signal:  signal,
			URL:     req.URL.String(),
			Headers: copyHeaderMap(req.Header),
			Payload: base64.StdEncoding.EncodeToString(payload),
			Created: time.Now().Unix(),
		}

		envBytes, marshalErr := json.Marshal(env)
		if marshalErr != nil {
			if err != nil {
				return nil, errors.Join(err, marshalErr)
			}
			return nil, marshalErr
		}

		// Queue the payload even if the caller context was canceled; the data
		// still needs to be durably stored for the next attempt.
		qctx := context.WithoutCancel(ctx)
		if qerr := qt.queue.Push(qctx, Route{Name: qt.route, Signal: signal}, envBytes); qerr != nil {
			if err != nil {
				return nil, errors.Join(err, qerr)
			}
			return nil, qerr
		}

		if err != nil {
			return nil, err
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		return nil, fmt.Errorf("server returned %s", resp.Status)
	}

	return resp, nil
}

func signalFromPath(p string) string {
	if strings.HasSuffix(p, "/v1/metrics") {
		return queueSubdirMetrics
	}
	if strings.HasSuffix(p, "/v1/traces") {
		return queueSubdirTraces
	}
	return "unknown"
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
