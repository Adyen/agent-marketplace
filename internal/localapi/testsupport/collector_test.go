package testsupport

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	metricsexportpb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	traceexportpb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestCollectorRawPacketsCaptureUnexpectedSignalsAndPrivateValues(t *testing.T) {
	collector := NewCollector()
	t.Cleanup(collector.Close)
	privateSentinel := "work28-fake-private-sentinel"
	metrics := &metricsexportpb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{{
			ScopeMetrics: []*metricspb.ScopeMetrics{{
				Metrics: []*metricspb.Metric{
					{
						Name: "unexpected.gauge",
						Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{DataPoints: []*metricspb.NumberDataPoint{{
							Attributes: []*commonpb.KeyValue{
								stringAttribute("unexpected.extra", "gauge-extra-value"),
								{Key: "unexpected.number", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: 7}}},
							},
						}}}},
					},
					{Name: "unexpected.exponential", Data: &metricspb.Metric_ExponentialHistogram{ExponentialHistogram: &metricspb.ExponentialHistogram{}}},
					{Name: "unexpected.summary", Data: &metricspb.Metric_Summary{Summary: &metricspb.Summary{}}},
					{Name: "unexpected.nil"},
				},
			}},
		}},
	}
	postOTLP(t, collector, "/v1/metrics", metrics)

	traces := &traceexportpb.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{{
			Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{stringAttribute("resource.sentinel", privateSentinel)}},
			ScopeSpans: []*tracepb.ScopeSpans{{
				Spans: []*tracepb.Span{{
					Name:   "agent_marketplace.gui.action",
					Events: []*tracepb.Span_Event{{Name: privateSentinel}},
				}},
			}},
		}},
	}
	postOTLP(t, collector, "/v1/traces", traces)

	capture, err := collector.RawSnapshot()
	if err != nil {
		t.Fatal("private collector did not decode OTLP")
	}
	if len(capture.Metrics) != 4 {
		t.Fatal("raw snapshot omitted an exported metric signal")
	}
	gaugeFound := false
	metricNames := make(map[string]bool, len(capture.Metrics))
	for _, metric := range capture.Metrics {
		metricNames[metric.Name] = true
		if metric.Name == "unexpected.gauge" {
			extra, hasExtra := metric.Attributes["unexpected.extra"]
			_, hasNumber := metric.Attributes["unexpected.number"]
			gaugeFound = hasExtra && hasNumber && extra == "gauge-extra-value"
		}
	}
	if !gaugeFound || !metricNames["unexpected.exponential"] || !metricNames["unexpected.summary"] || !metricNames["unexpected.nil"] {
		t.Fatal("raw snapshot did not retain the gauge label and all metric names")
	}
	if !collector.ContainsPrivate(privateSentinel) {
		t.Fatal("raw OTLP packets did not contain the injected private sentinel")
	}
	if collector.ContainsPrivate("work28-absent-sentinel") {
		t.Fatal("raw OTLP packets matched an absent sentinel")
	}
	if collector.ContainsPrivate("") {
		t.Fatal("empty private value matched a raw packet")
	}
	sanitized, err := collector.Snapshot()
	if err != nil {
		t.Fatal("private collector could not build sanitized capture")
	}
	data, err := json.Marshal(sanitized)
	if err != nil {
		t.Fatal("marshal sanitized capture")
	}
	if len(sanitized.Metrics) != 0 || bytes.Contains(data, []byte(privateSentinel)) || bytes.Contains(data, []byte("unexpected.gauge")) {
		t.Fatal("sanitized capture retained raw private OTLP values")
	}
}

func postOTLP(t *testing.T, collector *Collector, path string, message proto.Message) {
	t.Helper()
	body, err := proto.Marshal(message)
	if err != nil {
		t.Fatal("marshal private OTLP fixture")
	}
	request, err := http.NewRequest(http.MethodPost, collector.URL()+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal("create private OTLP request")
	}
	request.Header.Set("Content-Type", "application/x-protobuf")
	response, err := collector.server.Client().Do(request)
	if err != nil {
		t.Fatal("send private OTLP request")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatal("private collector rejected the OTLP fixture")
	}
}

func stringAttribute(key, value string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: key, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}}}
}
