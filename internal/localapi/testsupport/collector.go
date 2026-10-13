package testsupport

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"

	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"

	metricsexportpb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	traceexportpb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type Span struct {
	Name       string            `json:"name"`
	Attributes map[string]string `json:"attributes"`
}

type Metric struct {
	Name       string            `json:"name"`
	Attributes map[string]string `json:"attributes"`
	Value      int64             `json:"value,omitempty"`
	Count      uint64            `json:"count,omitempty"`
}

type Capture struct {
	Spans    []Span   `json:"spans"`
	Metrics  []Metric `json:"metrics"`
	Requests Requests `json:"requests"`
}

type Requests struct {
	Traces  int `json:"traces"`
	Metrics int `json:"metrics"`
}

type Collector struct {
	server    *httptest.Server
	mu        sync.Mutex
	spans     []Span
	metrics   []Metric
	requests  Requests
	packets   [][]byte
	decodeErr error
}

func NewCollector() *Collector {
	collector := &Collector{}
	collector.server = httptest.NewServer(http.HandlerFunc(collector.serveHTTP))
	return collector
}

func (c *Collector) URL() string { return c.server.URL }

func (c *Collector) Close() { c.server.Close() }

func (c *Collector) serveHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		c.setError(err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	switch r.URL.Path {
	case "/v1/metrics":
		var request metricsexportpb.ExportMetricsServiceRequest
		if err := proto.Unmarshal(body, &request); err != nil {
			c.setError(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		packet, err := protojson.Marshal(&request)
		if err != nil {
			c.setError(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		c.mu.Lock()
		c.requests.Metrics++
		c.packets = append(c.packets, packet)
		for _, resource := range request.ResourceMetrics {
			for _, scope := range resource.ScopeMetrics {
				for _, metric := range scope.Metrics {
					before := len(c.metrics)
					if sum := metric.GetSum(); sum != nil {
						for _, point := range sum.DataPoints {
							c.metrics = append(c.metrics, Metric{Name: metric.Name, Attributes: stringAttributes(point.Attributes), Value: point.GetAsInt()})
						}
					}
					if histogram := metric.GetHistogram(); histogram != nil {
						for _, point := range histogram.DataPoints {
							c.metrics = append(c.metrics, Metric{Name: metric.Name, Attributes: stringAttributes(point.Attributes), Count: point.GetCount()})
						}
					}
					if gauge := metric.GetGauge(); gauge != nil {
						for _, point := range gauge.DataPoints {
							c.metrics = append(c.metrics, Metric{Name: metric.Name, Attributes: stringAttributes(point.Attributes)})
						}
					}
					if len(c.metrics) == before {
						c.metrics = append(c.metrics, Metric{Name: metric.Name, Attributes: map[string]string{}})
					}
				}
			}
		}
		c.mu.Unlock()
	case "/v1/traces":
		var request traceexportpb.ExportTraceServiceRequest
		if err := proto.Unmarshal(body, &request); err != nil {
			c.setError(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		packet, err := protojson.Marshal(&request)
		if err != nil {
			c.setError(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		c.mu.Lock()
		c.requests.Traces++
		c.packets = append(c.packets, packet)
		for _, resource := range request.ResourceSpans {
			for _, scope := range resource.ScopeSpans {
				for _, span := range scope.Spans {
					c.spans = append(c.spans, Span{Name: span.Name, Attributes: stringAttributes(span.Attributes)})
				}
			}
		}
		c.mu.Unlock()
	default:
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (c *Collector) setError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.decodeErr == nil {
		c.decodeErr = err
	}
}

func (c *Collector) ContainsPrivate(values ...string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, packet := range c.packets {
		for _, value := range values {
			if value != "" && bytes.Contains(packet, []byte(value)) {
				return true
			}
		}
	}
	return false
}

func (c *Collector) RawSnapshot() (Capture, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	capture := Capture{Spans: make([]Span, 0, len(c.spans)), Metrics: make([]Metric, 0, len(c.metrics))}
	capture.Requests.Traces = c.requests.Traces
	capture.Requests.Metrics = c.requests.Metrics
	for _, span := range c.spans {
		capture.Spans = append(capture.Spans, Span{Name: span.Name, Attributes: cloneAttributes(span.Attributes)})
	}
	for _, metric := range c.metrics {
		capture.Metrics = append(capture.Metrics, Metric{Name: metric.Name, Attributes: cloneAttributes(metric.Attributes), Value: metric.Value, Count: metric.Count})
	}
	return capture, c.decodeErr
}

func (c *Collector) Snapshot() (Capture, error) {
	raw, err := c.RawSnapshot()
	capture := Capture{Spans: []Span{}, Metrics: []Metric{}}
	capture.Requests = raw.Requests
	if err != nil {
		return capture, err
	}
	for _, span := range raw.Spans {
		var allowed []string
		switch span.Name {
		case "agent_marketplace.command":
			allowed = []string{"command.name", "command.surface"}
		case "agent_marketplace.gui.action":
			allowed = []string{"action", "command.surface", "view", "resource.kind", "modal", "outcome"}
		default:
			continue
		}
		capture.Spans = append(capture.Spans, Span{Name: span.Name, Attributes: selectAttributes(span.Attributes, allowed)})
	}
	for _, metric := range raw.Metrics {
		if metric.Name != string(tmcfg.CommandCounterName) && metric.Name != string(tmcfg.CommandDurationName) {
			continue
		}
		capture.Metrics = append(capture.Metrics, Metric{
			Name:       metric.Name,
			Attributes: selectAttributes(metric.Attributes, []string{"command.name", "harnesses", "managed", "outcome.code"}),
			Value:      metric.Value,
			Count:      metric.Count,
		})
	}
	return capture, nil
}

func (c *Collector) WriteCapture(path string) error {
	capture, err := c.Snapshot()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(capture, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func stringAttributes(values []*commonpb.KeyValue) map[string]string {
	attributes := make(map[string]string, len(values))
	for _, value := range values {
		if value.Value != nil {
			attributes[value.Key] = value.Value.GetStringValue()
		}
	}
	return attributes
}

func cloneAttributes(attributes map[string]string) map[string]string {
	out := make(map[string]string, len(attributes))
	for key, value := range attributes {
		out[key] = value
	}
	return out
}

func selectAttributes(attributes map[string]string, keys []string) map[string]string {
	selected := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := attributes[key]; ok && safeTelemetryAttribute(key, value) {
			selected[key] = value
		}
	}
	return selected
}

func safeTelemetryAttribute(key, value string) bool {
	switch key {
	case "action":
		return value == "browse" || value == "sync" || value == "update" || value == "modal_outcome"
	case "command.name":
		return value == "gui"
	case "command.surface":
		return value == "gui" || value == "tui"
	case "view":
		return value == "detail"
	case "resource.kind":
		switch value {
		case "skill", "command", "subagent", "hook", "mcp", "collection":
			return true
		}
	case "modal":
		return value == "sync+quit" || value == "toggle"
	case "outcome":
		return value == "cancelled"
	case "harnesses":
		if value == "" {
			return true
		}
		for _, harness := range strings.Split(value, ",") {
			switch harness {
			case "claude", "codex", "cursor", "devin", "factory", "gemini", "opencode", "windsurf":
			default:
				return false
			}
		}
		return true
	case "managed":
		return value == "true" || value == "false"
	case "outcome.code":
		return value == "success" || value == "error"
	}
	return false
}
