package receiver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/telemetry"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"

	metricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	mpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func strKV(key, value string) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key:   key,
		Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}},
	}
}

func float64Pointer(value float64) *float64 {
	return &value
}

func makeMetricsRequest(installationID, commandName, harnesses string) *metricspb.ExportMetricsServiceRequest {
	if installationID != "" {
		digest := sha256.Sum256([]byte(installationID))
		installationID = hex.EncodeToString(digest[:])
	}
	metricAttrs := []*commonpb.KeyValue{
		strKV(string(tmcfg.ResourceAttrCommandName), commandName),
		strKV(string(tmcfg.ResourceAttrHarnesses), harnesses),
		strKV(string(tmcfg.ResourceAttrManaged), "true"),
		strKV(string(tmcfg.ResourceAttrOutcomeCode), "success"),
	}
	return &metricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*mpb.ResourceMetrics{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{
						strKV(string(tmcfg.ResourceAttrSchemaVersion), tmcfg.SchemaVersion),
						strKV(string(tmcfg.ResourceAttrInstallationID), installationID),
						strKV(string(tmcfg.ResourceAttrCLIVersion), "v3.0.1"),
						strKV(string(tmcfg.ResourceAttrOSName), "darwin"),
						strKV(string(tmcfg.ResourceAttrArch), "arm64"),
					},
				},
				ScopeMetrics: []*mpb.ScopeMetrics{
					{
						Metrics: []*mpb.Metric{
							{
								Name: string(tmcfg.CommandCounterName),
								Data: &mpb.Metric_Sum{
									Sum: &mpb.Sum{
										AggregationTemporality: mpb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA,
										IsMonotonic:            true,
										DataPoints: []*mpb.NumberDataPoint{
											{
												Value:      &mpb.NumberDataPoint_AsInt{AsInt: 1},
												Attributes: metricAttrs,
											},
										},
									},
								},
							},
							{
								Name: string(tmcfg.CommandDurationName),
								Data: &mpb.Metric_Histogram{
									Histogram: &mpb.Histogram{
										AggregationTemporality: mpb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA,
										DataPoints: []*mpb.HistogramDataPoint{
											{
												Count:          1,
												Sum:            float64Pointer(42),
												ExplicitBounds: tmcfg.CommandDurationBucketBoundaries,
												BucketCounts:   []uint64{0, 1, 0, 0, 0, 0, 0, 0, 0, 0},
												Attributes:     metricAttrs,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func postProto(t *testing.T, client *http.Client, url string, req *metricspb.ExportMetricsServiceRequest) *http.Response {
	t.Helper()
	body, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return postBytes(t, client, url, body, "")
}

func postBytes(t *testing.T, client *http.Client, url string, body []byte, requestID string) *http.Response {
	t.Helper()
	r, err := http.NewRequest(http.MethodPost, url+"/v1/metrics", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	r.Header.Set("Content-Type", "application/x-protobuf")
	if requestID != "" {
		r.Header.Set("X-Agent-Marketplace-Request-ID", requestID)
	}
	resp, err := client.Do(r)
	if err != nil {
		t.Fatalf("post metrics: %v", err)
	}
	return resp
}

func getBody(t *testing.T, client *http.Client, url string) string {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get %s: status %d: %s", url, resp.StatusCode, body)
	}
	return string(body)
}

func TestServerDeduplicatesRequestReplay(t *testing.T) {
	dir := t.TempDir()
	requestID := strings.Repeat("a", 64)
	body, err := proto.Marshal(makeMetricsRequest("dedupe", "sync", "factory"))
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 5 * time.Second}

	first, err := New(Config{StorageDir: dir, Logger: discardLogger()})
	if err != nil {
		t.Fatal(err)
	}
	firstServer := httptest.NewServer(first.Handler())
	for range 2 {
		resp := postBytes(t, client, firstServer.URL, body, requestID)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("replay status = %d, want 200", resp.StatusCode)
		}
	}
	if output := getBody(t, client, firstServer.URL+"/metrics"); !strings.Contains(output, `command_name="sync",managed="true",os_name="darwin",outcome_code="success"} 1`) {
		t.Fatalf("duplicate request changed invocation count:\n%s", output)
	}
	firstServer.Close()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := New(Config{StorageDir: dir, Logger: discardLogger()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restarted.Close() }()
	restartedServer := httptest.NewServer(restarted.Handler())
	defer restartedServer.Close()
	resp := postBytes(t, client, restartedServer.URL, body, requestID)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restart replay status = %d, want 200", resp.StatusCode)
	}
	if output := getBody(t, client, restartedServer.URL+"/metrics"); !strings.Contains(output, `command_name="sync",managed="true",os_name="darwin",outcome_code="success"} 1`) {
		t.Fatalf("durable duplicate request changed invocation count:\n%s", output)
	}
}

func TestServerBoundsRequestIDDeduplication(t *testing.T) {
	s, err := New(Config{StorageDir: t.TempDir(), MaxRequestIDs: 1, Logger: discardLogger()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	body, err := proto.Marshal(makeMetricsRequest("request-budget", "sync", "factory"))
	if err != nil {
		t.Fatal(err)
	}
	for i, requestID := range []string{strings.Repeat("e", 64), strings.Repeat("f", 64)} {
		resp := postBytes(t, http.DefaultClient, ts.URL, body, requestID)
		_ = resp.Body.Close()
		want := http.StatusOK
		if i == 1 {
			want = http.StatusTooManyRequests
		}
		if resp.StatusCode != want {
			t.Fatalf("request %d status = %d, want %d", i, resp.StatusCode, want)
		}
	}
}

func TestServerPersistsOnlyValidatedTelemetryFields(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Config{StorageDir: dir, Logger: discardLogger()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	request := makeMetricsRequest("private-fields", "sync", "factory")
	secret := "/Users/alexander/private?token=secret"
	request.ResourceMetrics[0].SchemaUrl = secret
	request.ResourceMetrics[0].ScopeMetrics[0].Scope = &commonpb.InstrumentationScope{Name: secret, Version: secret}
	resp := postProto(t, http.DefaultClient, ts.URL, request)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post status = %d", resp.StatusCode)
	}
	path := filepath.Join(dir, time.Now().UTC().Format("2006-01-02")+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) {
		t.Fatalf("stored record contains ignored OTLP data: %s", data)
	}
}

func TestServerRejectsRequestIDPayloadMismatch(t *testing.T) {
	s, err := New(Config{StorageDir: t.TempDir(), Logger: discardLogger()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := &http.Client{Timeout: 5 * time.Second}
	requestID := strings.Repeat("b", 64)

	for i, command := range []string{"sync", "list"} {
		body, err := proto.Marshal(makeMetricsRequest("conflict", command, "factory"))
		if err != nil {
			t.Fatal(err)
		}
		resp := postBytes(t, client, ts.URL, body, requestID)
		_ = resp.Body.Close()
		want := http.StatusOK
		if i == 1 {
			want = http.StatusConflict
		}
		if resp.StatusCode != want {
			t.Fatalf("request %d status = %d, want %d", i, resp.StatusCode, want)
		}
	}
}

func TestServerRecoversTornFinalRecord(t *testing.T) {
	dir := t.TempDir()
	client := &http.Client{Timeout: 5 * time.Second}
	first, err := New(Config{StorageDir: dir, Logger: discardLogger()})
	if err != nil {
		t.Fatal(err)
	}
	firstServer := httptest.NewServer(first.Handler())
	resp := postProto(t, client, firstServer.URL, makeMetricsRequest("first", "sync", "factory"))
	_ = resp.Body.Close()
	firstServer.Close()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, time.Now().UTC().Format("2006-01-02")+".jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, recordFilePerm)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"received_at":`); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()

	restarted, err := New(Config{StorageDir: dir, Logger: discardLogger()})
	if err != nil {
		t.Fatalf("restart after torn tail: %v", err)
	}
	defer func() { _ = restarted.Close() }()
	restartedServer := httptest.NewServer(restarted.Handler())
	defer restartedServer.Close()
	resp = postProto(t, client, restartedServer.URL, makeMetricsRequest("second", "list", "factory"))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post after recovery status = %d", resp.StatusCode)
	}
	output := getBody(t, client, restartedServer.URL+"/v1/dashboard.json")
	if !strings.Contains(output, `"list":1`) || !strings.Contains(output, `"sync":1`) {
		t.Fatalf("recovered dashboard missing valid records: %s", output)
	}
}

func TestServerPrunesExpiredRecords(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().UTC().AddDate(0, 0, -31)
	installationID := strings.Repeat("c", 64)
	record := requestRecord{
		ReceivedAt: old, InstallationID: installationID, SchemaVersion: tmcfg.SchemaVersion,
		Commands: map[string]int64{"sync": 1}, Harnesses: map[string]int64{"factory": 1}, MetricCount: 2,
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, old.Format("2006-01-02")+".jsonl")
	if err := os.WriteFile(path, append(data, '\n'), recordFilePerm); err != nil {
		t.Fatal(err)
	}
	budget, err := json.Marshal(budgetFile{InstallationIDs: []string{installationID}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, budgetFileName), budget, recordFilePerm); err != nil {
		t.Fatal(err)
	}

	s, err := New(Config{StorageDir: dir, Logger: discardLogger()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expired record file still exists: %v", err)
	}
	if s.budget.Count() != 0 {
		t.Fatalf("expired installation remains in budget: %d", s.budget.Count())
	}
}

func TestServerKeepsThirtyUTCDatePartitions(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 1, 31, 23, 59, 0, 0, time.UTC)
	records := []requestRecord{
		{ReceivedAt: time.Date(2026, 1, 1, 23, 59, 0, 0, time.UTC), InstallationID: strings.Repeat("1", 64), SchemaVersion: tmcfg.SchemaVersion, Commands: map[string]int64{"sync": 1}, Harnesses: map[string]int64{"factory": 1}, MetricCount: 2},
		{ReceivedAt: time.Date(2026, 1, 2, 0, 1, 0, 0, time.UTC), InstallationID: strings.Repeat("2", 64), SchemaVersion: tmcfg.SchemaVersion, Commands: map[string]int64{"list": 1}, Harnesses: map[string]int64{"factory": 1}, MetricCount: 2},
	}
	for i := range records {
		data, err := json.Marshal(&records[i])
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, records[i].ReceivedAt.Format("2006-01-02")+".jsonl")
		if err := os.WriteFile(path, append(data, '\n'), recordFilePerm); err != nil {
			t.Fatal(err)
		}
	}

	s, err := New(Config{StorageDir: dir, Logger: discardLogger(), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	dashboard := getBody(t, http.DefaultClient, ts.URL+"/v1/dashboard.json")
	if strings.Contains(dashboard, `"sync":1`) || !strings.Contains(dashboard, `"list":1`) {
		t.Fatalf("retention boundary dashboard: %s", dashboard)
	}
}

func TestServerPrunesExpiredRecordsWhileRunning(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s, err := New(Config{StorageDir: t.TempDir(), Logger: discardLogger(), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := &http.Client{Timeout: 5 * time.Second}
	body, err := proto.Marshal(makeMetricsRequest("expired", "sync", "factory"))
	if err != nil {
		t.Fatal(err)
	}
	resp := postBytes(t, client, ts.URL, body, strings.Repeat("0", 64))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post status = %d", resp.StatusCode)
	}

	now = now.AddDate(0, 0, 31)
	output := getBody(t, client, ts.URL+"/metrics")
	if !strings.Contains(output, `agent_marketplace_receiver_invocations_total{`) {
		t.Fatalf("retention made a live counter decrease:\n%s", output)
	}
	if !strings.Contains(output, `agent_marketplace_receiver_active_installations{window="30d"} 0`) {
		t.Fatalf("expired installation remains active:\n%s", output)
	}
	if s.budget.Count() != 0 {
		t.Fatalf("expired installation remains in budget: %d", s.budget.Count())
	}
	if len(s.requestDigests) != 0 {
		t.Fatalf("expired request ids remain: %d", len(s.requestDigests))
	}
}

func TestServerSchemaValidation(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Config{StorageDir: dir, Logger: discardLogger()})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	client := &http.Client{Timeout: 5 * time.Second}

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	valid := makeMetricsRequest("id-1", "sync", "factory,devin")
	resp := postProto(t, client, ts.URL, valid)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 for valid request, got %d: %s", resp.StatusCode, body)
	}
	_ = resp.Body.Close()

	missingSchema := makeMetricsRequest("id-2", "sync", "factory")
	missingSchema.ResourceMetrics[0].Resource.Attributes = []*commonpb.KeyValue{
		strKV(string(tmcfg.ResourceAttrInstallationID), "id-2"),
	}
	resp = postProto(t, client, ts.URL, missingSchema)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing schema version, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	wrongSchema := makeMetricsRequest("id-3", "sync", "factory")
	wrongSchema.ResourceMetrics[0].Resource.Attributes[0] = strKV(string(tmcfg.ResourceAttrSchemaVersion), "2")
	resp = postProto(t, client, ts.URL, wrongSchema)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for wrong schema version, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	missingID := makeMetricsRequest("", "sync", "factory")
	resp = postProto(t, client, ts.URL, missingID)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing installation id, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	badMetric := makeMetricsRequest("id-4", "sync", "factory")
	badMetric.ResourceMetrics[0].ScopeMetrics[0].Metrics[0].Name = "unknown.metric"
	resp = postProto(t, client, ts.URL, badMetric)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for disallowed metric name, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestServerRejectsInvalidMetricPayloads(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*metricspb.ExportMetricsServiceRequest)
	}{
		{name: "cumulative counter", mutate: func(req *metricspb.ExportMetricsServiceRequest) {
			req.ResourceMetrics[0].ScopeMetrics[0].Metrics[0].GetSum().AggregationTemporality = mpb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE
		}},
		{name: "non-monotonic counter", mutate: func(req *metricspb.ExportMetricsServiceRequest) {
			req.ResourceMetrics[0].ScopeMetrics[0].Metrics[0].GetSum().IsMonotonic = false
		}},
		{name: "negative counter", mutate: func(req *metricspb.ExportMetricsServiceRequest) {
			req.ResourceMetrics[0].ScopeMetrics[0].Metrics[0].GetSum().DataPoints[0].Value = &mpb.NumberDataPoint_AsInt{AsInt: -1}
		}},
		{name: "wrong counter type", mutate: func(req *metricspb.ExportMetricsServiceRequest) {
			req.ResourceMetrics[0].ScopeMetrics[0].Metrics[0].Data = &mpb.Metric_Histogram{Histogram: &mpb.Histogram{}}
		}},
		{name: "cumulative histogram", mutate: func(req *metricspb.ExportMetricsServiceRequest) {
			req.ResourceMetrics[0].ScopeMetrics[0].Metrics[1].GetHistogram().AggregationTemporality = mpb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE
		}},
		{name: "inconsistent histogram", mutate: func(req *metricspb.ExportMetricsServiceRequest) {
			req.ResourceMetrics[0].ScopeMetrics[0].Metrics[1].GetHistogram().DataPoints[0].BucketCounts[0] = 1
		}},
		{name: "non-finite histogram sum", mutate: func(req *metricspb.ExportMetricsServiceRequest) {
			req.ResourceMetrics[0].ScopeMetrics[0].Metrics[1].GetHistogram().DataPoints[0].Sum = float64Pointer(math.NaN())
		}},
		{name: "invalid installation id", mutate: func(req *metricspb.ExportMetricsServiceRequest) {
			req.ResourceMetrics[0].Resource.Attributes[1] = strKV(string(tmcfg.ResourceAttrInstallationID), "short")
		}},
		{name: "overflowing counter delta", mutate: func(req *metricspb.ExportMetricsServiceRequest) {
			sum := req.ResourceMetrics[0].ScopeMetrics[0].Metrics[0].GetSum()
			max := int64(^uint64(0) >> 1)
			sum.DataPoints[0].Value = &mpb.NumberDataPoint_AsInt{AsInt: max}
			sum.DataPoints = append(sum.DataPoints, &mpb.NumberDataPoint{Value: &mpb.NumberDataPoint_AsInt{AsInt: max}, Attributes: sum.DataPoints[0].Attributes})
		}},
		{name: "overflowing histogram buckets", mutate: func(req *metricspb.ExportMetricsServiceRequest) {
			dp := req.ResourceMetrics[0].ScopeMetrics[0].Metrics[1].GetHistogram().DataPoints[0]
			dp.BucketCounts[0] = ^uint64(0)
			dp.BucketCounts[1] = 1
			dp.Count = ^uint64(0)
		}},
		{name: "invalid command", mutate: func(req *metricspb.ExportMetricsServiceRequest) {
			req.ResourceMetrics[0].ScopeMetrics[0].Metrics[0].GetSum().DataPoints[0].Attributes[0] = strKV(string(tmcfg.ResourceAttrCommandName), "/private/path")
		}},
		{name: "invalid harness", mutate: func(req *metricspb.ExportMetricsServiceRequest) {
			req.ResourceMetrics[0].ScopeMetrics[0].Metrics[0].GetSum().DataPoints[0].Attributes[1] = strKV(string(tmcfg.ResourceAttrHarnesses), "unknown")
		}},
		{name: "duplicate harness", mutate: func(req *metricspb.ExportMetricsServiceRequest) {
			req.ResourceMetrics[0].ScopeMetrics[0].Metrics[0].GetSum().DataPoints[0].Attributes[1] = strKV(string(tmcfg.ResourceAttrHarnesses), "factory,factory")
		}},
		{name: "empty payload", mutate: func(req *metricspb.ExportMetricsServiceRequest) {
			req.ResourceMetrics[0].ScopeMetrics = nil
		}},
		{name: "unknown resource attribute", mutate: func(req *metricspb.ExportMetricsServiceRequest) {
			req.ResourceMetrics[0].Resource.Attributes = append(req.ResourceMetrics[0].Resource.Attributes, strKV("unknown", "value"))
		}},
		{name: "duplicate metric attribute", mutate: func(req *metricspb.ExportMetricsServiceRequest) {
			dp := req.ResourceMetrics[0].ScopeMetrics[0].Metrics[0].GetSum().DataPoints[0]
			dp.Attributes = append(dp.Attributes, strKV(string(tmcfg.ResourceAttrCommandName), "sync"))
		}},
		{name: "non-string metric attribute", mutate: func(req *metricspb.ExportMetricsServiceRequest) {
			dp := req.ResourceMetrics[0].ScopeMetrics[0].Metrics[0].GetSum().DataPoints[0]
			dp.Attributes[2] = &commonpb.KeyValue{Key: string(tmcfg.ResourceAttrManaged), Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: true}}}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := New(Config{StorageDir: t.TempDir(), Logger: discardLogger()})
			if err != nil {
				t.Fatalf("new server: %v", err)
			}
			ts := httptest.NewServer(s.Handler())
			defer ts.Close()
			req := makeMetricsRequest("invalid", "agent-marketplace sync", "factory")
			tt.mutate(req)
			resp := postProto(t, &http.Client{Timeout: 5 * time.Second}, ts.URL, req)
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", resp.StatusCode)
			}
		})
	}
}

func TestServerMetricPersistence(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Config{StorageDir: dir, Logger: discardLogger()})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	req := makeMetricsRequest("persist-1", "list", "claude")
	resp := postProto(t, client, ts.URL, req)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
	_ = resp.Body.Close()

	path := filepath.Join(dir, time.Now().UTC().Format("2006-01-02")+".jsonl")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected daily record file: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected file mode 0o600, got 0o%o", perm)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"installation_id":"`) {
		t.Fatalf("record file missing installation id: %s", data)
	}
	if !strings.Contains(string(data), "\"metric_count\":2") {
		t.Fatalf("record file missing metric count: %s", data)
	}
}

func TestServerRollsBackInstallationOnPersistenceFailure(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Config{StorageDir: dir, Logger: discardLogger()})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	blockedPath := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blockedPath, []byte("not a directory"), recordFilePerm); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}
	s.storage.dir = blockedPath
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp := postProto(t, &http.Client{Timeout: 5 * time.Second}, ts.URL, makeMetricsRequest("rollback", "sync", "factory"))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected persistence failure, got %d", resp.StatusCode)
	}
	if s.budget.Count() != 0 {
		t.Fatalf("failed persistence consumed installation budget: %d", s.budget.Count())
	}
}

func TestServerPrometheusMetrics(t *testing.T) {
	s, err := New(Config{StorageDir: t.TempDir(), MaxInstallations: 10, Logger: discardLogger()})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	resp := postProto(t, client, ts.URL, makeMetricsRequest("private-installation-id", "sync", "factory,devin"))
	_ = resp.Body.Close()

	resp, err = client.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatalf("get metrics: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	output := string(body)
	for _, want := range []string{
		`agent_marketplace_receiver_invocations_total{arch="arm64",cli_version="v3.0.1",command_name="sync",managed="true",os_name="darwin",outcome_code="success"} 1`,
		`agent_marketplace_receiver_harness_invocations_total{harness="factory"} 1`,
		`agent_marketplace_receiver_harness_invocations_total{harness="devin"} 1`,
		`agent_marketplace_receiver_command_duration_milliseconds_bucket{command_name="sync",outcome_code="success",le="10"} 0`,
		`agent_marketplace_receiver_command_duration_milliseconds_bucket{command_name="sync",outcome_code="success",le="50"} 1`,
		`agent_marketplace_receiver_command_duration_milliseconds_bucket{command_name="sync",outcome_code="success",le="+Inf"} 1`,
		`agent_marketplace_receiver_command_duration_milliseconds_sum{command_name="sync",outcome_code="success"} 42`,
		`agent_marketplace_receiver_command_duration_milliseconds_count{command_name="sync",outcome_code="success"} 1`,
		`agent_marketplace_receiver_installation_budget_used 1`,
		`agent_marketplace_receiver_installation_budget_limit 10`,
	} {
		if !strings.Contains(output, want) {
			t.Errorf("metrics output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "private-installation-id") || strings.Contains(output, "installation_id=") {
		t.Fatalf("metrics output exposed installation ID:\n%s", output)
	}
}

func TestServerPrometheusRequestMetrics(t *testing.T) {
	s, err := New(Config{StorageDir: t.TempDir(), MaxInstallations: 1, Logger: discardLogger()})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := &http.Client{Timeout: 5 * time.Second}

	resp := postProto(t, client, ts.URL, makeMetricsRequest("accepted", "sync", "factory"))
	_ = resp.Body.Close()
	resp = postProto(t, client, ts.URL, makeMetricsRequest("over-budget", "sync", "factory"))
	_ = resp.Body.Close()
	invalid := makeMetricsRequest("invalid", "sync", "factory")
	invalid.ResourceMetrics[0].Resource.Attributes[0] = strKV(string(tmcfg.ResourceAttrSchemaVersion), "invalid")
	resp = postProto(t, client, ts.URL, invalid)
	_ = resp.Body.Close()

	resp, err = client.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatalf("get metrics: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`agent_marketplace_receiver_requests_total{reason="",result="accepted"} 1`,
		`agent_marketplace_receiver_requests_total{reason="cardinality_budget",result="rejected"} 1`,
		`agent_marketplace_receiver_requests_total{reason="invalid_payload",result="rejected"} 1`,
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("metrics output missing %q:\n%s", want, body)
		}
	}
}

func TestServerLogsBoundedRejectionReason(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	s, err := New(Config{StorageDir: t.TempDir(), Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/metrics", strings.NewReader("not protobuf"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if !strings.Contains(logs.String(), `"reason":"invalid_payload"`) || !strings.Contains(logs.String(), `"status":400`) {
		t.Fatalf("missing bounded rejection fields: %s", logs.String())
	}
	if strings.Contains(logs.String(), "not protobuf") {
		t.Fatalf("rejection log contains payload: %s", logs.String())
	}
}

func TestServerRejectsAccumulationOverflow(t *testing.T) {
	s, err := New(Config{StorageDir: t.TempDir(), Logger: discardLogger()})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := &http.Client{Timeout: 5 * time.Second}

	first := makeMetricsRequest("overflow", "sync", "factory")
	first.ResourceMetrics[0].ScopeMetrics[0].Metrics[0].GetSum().DataPoints[0].Value = &mpb.NumberDataPoint_AsInt{AsInt: int64(^uint64(0) >> 1)}
	resp := postProto(t, client, ts.URL, first)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected first request accepted, got %d", resp.StatusCode)
	}
	resp = postProto(t, client, ts.URL, makeMetricsRequest("overflow", "sync", "factory"))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected accumulated overflow rejected, got %d", resp.StatusCode)
	}
}

func TestServerPrometheusSeriesBudget(t *testing.T) {
	s, err := New(Config{StorageDir: t.TempDir(), MaxSeries: 14, Logger: discardLogger()})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := &http.Client{Timeout: 5 * time.Second}

	resp := postProto(t, client, ts.URL, makeMetricsRequest("first", "sync", "factory"))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected first request accepted, got %d", resp.StatusCode)
	}
	resp = postProto(t, client, ts.URL, makeMetricsRequest("second", "list", "factory"))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected series budget rejection, got %d", resp.StatusCode)
	}

	resp, err = client.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatalf("get metrics: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`agent_marketplace_receiver_requests_total{reason="series_budget",result="rejected"} 1`,
		`agent_marketplace_receiver_series_budget_used 14`,
		`agent_marketplace_receiver_series_budget_limit 14`,
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("metrics output missing %q:\n%s", want, body)
		}
	}
}

func TestServerSeriesBudgetSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	client := &http.Client{Timeout: 5 * time.Second}
	first, err := New(Config{StorageDir: dir, MaxSeries: 14, Logger: discardLogger()})
	if err != nil {
		t.Fatal(err)
	}
	firstServer := httptest.NewServer(first.Handler())
	resp := postProto(t, client, firstServer.URL, makeMetricsRequest("first", "sync", "factory"))
	_ = resp.Body.Close()
	firstServer.Close()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := New(Config{StorageDir: dir, MaxSeries: 14, Logger: discardLogger()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restarted.Close() }()
	restartedServer := httptest.NewServer(restarted.Handler())
	defer restartedServer.Close()
	resp = postProto(t, client, restartedServer.URL, makeMetricsRequest("second", "list", "factory"))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("new series after restart status = %d, want 429", resp.StatusCode)
	}
	output := getBody(t, client, restartedServer.URL+"/metrics")
	if !strings.Contains(output, "agent_marketplace_receiver_series_budget_used 14") {
		t.Fatalf("series budget was not restored:\n%s", output)
	}
}

func TestServerRejectsReducedSeriesLimitBelowRetainedUsage(t *testing.T) {
	dir := t.TempDir()
	first, err := New(Config{StorageDir: dir, MaxSeries: 14, Logger: discardLogger()})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(first.Handler())
	resp := postProto(t, http.DefaultClient, ts.URL, makeMetricsRequest("series-limit", "sync", "factory"))
	_ = resp.Body.Close()
	ts.Close()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if restarted, err := New(Config{StorageDir: dir, MaxSeries: 13, Logger: discardLogger()}); err == nil {
		_ = restarted.Close()
		t.Fatal("expected retained series above the configured limit to reject startup")
	}
}

func TestServerRestoresAdoptionState(t *testing.T) {
	dir := t.TempDir()
	first, err := New(Config{StorageDir: dir, Logger: discardLogger()})
	if err != nil {
		t.Fatalf("new first server: %v", err)
	}
	firstServer := httptest.NewServer(first.Handler())
	client := &http.Client{Timeout: 5 * time.Second}
	resp := postProto(t, client, firstServer.URL, makeMetricsRequest("restart-id", "sync", "factory"))
	_ = resp.Body.Close()
	firstServer.Close()
	if err := first.Close(); err != nil {
		t.Fatalf("close first server: %v", err)
	}

	restarted, err := New(Config{StorageDir: dir, Logger: discardLogger()})
	if err != nil {
		t.Fatalf("new restarted server: %v", err)
	}
	restartedServer := httptest.NewServer(restarted.Handler())
	defer restartedServer.Close()

	resp, err = client.Get(restartedServer.URL + "/metrics")
	if err != nil {
		t.Fatalf("get metrics: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`agent_marketplace_receiver_active_installations{window="1d"} 1`,
		`agent_marketplace_receiver_active_installations{window="7d"} 1`,
		`agent_marketplace_receiver_active_installations{window="30d"} 1`,
		`agent_marketplace_receiver_installation_budget_used 1`,
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("metrics output missing %q:\n%s", want, body)
		}
	}
	for _, historicalSeries := range []string{
		`agent_marketplace_receiver_invocations_total{arch="arm64",cli_version="v3.0.1",command_name="sync",managed="true",os_name="darwin",outcome_code="success"} 1`,
		`agent_marketplace_receiver_harness_invocations_total{harness="factory"} 1`,
		`agent_marketplace_receiver_command_duration_milliseconds_count{command_name="sync",outcome_code="success"} 1`,
		`agent_marketplace_receiver_requests_total{reason="",result="accepted"} 1`,
		`agent_marketplace_receiver_datapoints_total 2`,
		`agent_marketplace_receiver_series_budget_used 14`,
	} {
		if !strings.Contains(string(body), historicalSeries) {
			t.Fatalf("restart did not restore %q:\n%s", historicalSeries, body)
		}
	}

	resp, err = client.Get(restartedServer.URL + "/v1/dashboard.json")
	if err != nil {
		t.Fatalf("get dashboard: %v", err)
	}
	var dashboard dashboardResponse
	if err := json.NewDecoder(resp.Body).Decode(&dashboard); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	_ = resp.Body.Close()
	if dashboard.Adoption.Commands["sync"] != 1 || dashboard.LastReceive == "never" {
		t.Fatalf("dashboard state was not restored: %+v", dashboard)
	}
}

func TestServerDashboardSummary(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Config{StorageDir: dir, Logger: discardLogger()})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	postProto(t, client, ts.URL, makeMetricsRequest("dash-1", "sync", "factory,devin"))
	postProto(t, client, ts.URL, makeMetricsRequest("dash-2", "list", "claude"))

	resp, err := client.Get(ts.URL + "/v1/dashboard.json")
	if err != nil {
		t.Fatalf("get dashboard: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var dash dashboardResponse
	if err := json.Unmarshal(body, &dash); err != nil {
		t.Fatalf("unmarshal dashboard: %v", err)
	}

	if dash.Adoption.Commands["sync"] != 1 {
		t.Fatalf("expected sync=1, got %d", dash.Adoption.Commands["sync"])
	}
	if dash.Adoption.Commands["list"] != 1 {
		t.Fatalf("expected list=1, got %d", dash.Adoption.Commands["list"])
	}
	if dash.Adoption.Harnesses["factory"] != 1 {
		t.Fatalf("expected factory=1, got %d", dash.Adoption.Harnesses["factory"])
	}
	if dash.Adoption.Harnesses["devin"] != 1 {
		t.Fatalf("expected devin=1, got %d", dash.Adoption.Harnesses["devin"])
	}
	if dash.Adoption.Harnesses["claude"] != 1 {
		t.Fatalf("expected claude=1, got %d", dash.Adoption.Harnesses["claude"])
	}
	if dash.Health.UniqueInstallations != 2 {
		t.Fatalf("expected 2 unique installations, got %d", dash.Health.UniqueInstallations)
	}
	if dash.Health.MetricCount != 4 {
		t.Fatalf("expected metric count 4, got %d", dash.Health.MetricCount)
	}
	if dash.Health.StorageSizeBytes == 0 {
		t.Fatal("expected non-zero storage size")
	}
}

func TestServerCardinalityRejection(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Config{StorageDir: dir, MaxInstallations: 2, Logger: discardLogger()})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	for i := 1; i <= 3; i++ {
		req := makeMetricsRequest(fmt.Sprintf("card-%d", i), "sync", "factory")
		resp := postProto(t, client, ts.URL, req)
		want := http.StatusOK
		if i == 3 {
			want = http.StatusTooManyRequests
		}
		if resp.StatusCode != want {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("request %d: expected %d, got %d: %s", i, want, resp.StatusCode, body)
		}
		_ = resp.Body.Close()
	}

	if s.budget.Count() != 2 {
		t.Fatalf("expected budget count 2, got %d", s.budget.Count())
	}
}

func TestServerRejectsReducedCardinalityLimitBelowRetainedUsage(t *testing.T) {
	dir := t.TempDir()
	first, err := New(Config{StorageDir: dir, MaxInstallations: 2, Logger: discardLogger()})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(first.Handler())
	for _, id := range []string{"first", "second"} {
		resp := postProto(t, http.DefaultClient, ts.URL, makeMetricsRequest(id, "sync", "factory"))
		_ = resp.Body.Close()
	}
	ts.Close()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if restarted, err := New(Config{StorageDir: dir, MaxInstallations: 1, Logger: discardLogger()}); err == nil {
		_ = restarted.Close()
		t.Fatal("expected retained installation usage above the configured limit to reject startup")
	}
}

func TestServerRepairsCorruptCardinalityBudget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, budgetFileName)
	if err := os.WriteFile(path, []byte("{"), recordFilePerm); err != nil {
		t.Fatalf("write corrupt budget: %v", err)
	}

	s, err := New(Config{StorageDir: dir, Logger: discardLogger()})
	if err != nil {
		t.Fatalf("rebuild cardinality budget: %v", err)
	}
	defer func() { _ = s.Close() }()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file budgetFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("repaired cardinality budget is invalid: %v", err)
	}
}

func TestServerRejectsInvalidStoredRecord(t *testing.T) {
	dir := t.TempDir()
	record := `{"received_at":"2026-08-31T10:00:00Z","installation_id":"invalid","schema_version":"1","commands":{"sync":1},"harnesses":{"factory":1},"metric_count":2}` + "\n"
	path := filepath.Join(dir, "2026-08-31.jsonl")
	if err := os.WriteFile(path, []byte(record), recordFilePerm); err != nil {
		t.Fatalf("write record: %v", err)
	}
	if _, err := New(Config{StorageDir: dir, Logger: discardLogger()}); err == nil {
		t.Fatal("expected invalid stored record to reject startup")
	}
}

func TestServerHealth(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Config{StorageDir: dir, Logger: discardLogger()})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("get health: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), `"last_receive":"never"`) && !strings.Contains(string(body), `"last_receive":"`) {
		t.Fatalf("unexpected health body: %s", body)
	}

	postProto(t, client, ts.URL, makeMetricsRequest("health-1", "sync", "factory"))

	resp, err = client.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("get health after post: %v", err)
	}
	body, err = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	var health map[string]string
	if err := json.Unmarshal(body, &health); err != nil {
		t.Fatalf("unmarshal health: %v", err)
	}
	if health["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", health["status"])
	}
	if health["last_receive"] == "" || health["last_receive"] == "never" {
		t.Fatalf("expected last_receive to be set, got %q", health["last_receive"])
	}
}

func TestServerRejectsSharedStorage(t *testing.T) {
	dir := t.TempDir()
	first, err := New(Config{StorageDir: dir, Logger: discardLogger()})
	if err != nil {
		t.Fatalf("new first server: %v", err)
	}
	if _, err := New(Config{StorageDir: dir, Logger: discardLogger()}); err == nil {
		t.Fatal("expected second server using the same storage directory to fail")
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first server: %v", err)
	}
	third, err := New(Config{StorageDir: dir, Logger: discardLogger()})
	if err != nil {
		t.Fatalf("new server after release: %v", err)
	}
	if err := third.Close(); err != nil {
		t.Fatalf("close third server: %v", err)
	}
}

func TestServerStartAndShutdown(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Config{StorageDir: dir, ListenAddr: "localhost:0", Logger: discardLogger()})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := s.Start(ctx); err != nil {
			t.Errorf("start: %v", err)
		}
	}()

	var addr string
	for i := 0; i < 50; i++ {
		addr = s.Addr()
		if addr != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if addr == "" {
		t.Fatal("server did not start")
	}

	client := &http.Client{Timeout: 5 * time.Second}
	postProto(t, client, "http://"+addr, makeMetricsRequest("start-1", "sync", "factory"))

	resp, err := client.Get("http://" + addr + "/v1/dashboard.json")
	if err != nil {
		t.Fatalf("get dashboard: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"status":"ok"`) {
		t.Fatalf("unexpected dashboard body: %s", body)
	}

	cancel()
}

func TestDefaultStorageDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	got, err := DefaultStorageDir()
	if err != nil {
		t.Fatalf("default storage dir: %v", err)
	}
	want := filepath.Join(dir, "agent-marketplace", "telemetry", "receiver")
	if got != want {
		t.Fatalf("DefaultStorageDir = %q, want %q", got, want)
	}
}

func TestDefaultInstallationIDCardinalityBudgetUsed(t *testing.T) {
	if telemetry.DefaultInstallationIDCardinalityBudget != 10000 {
		t.Fatalf("expected budget 10000, got %d", telemetry.DefaultInstallationIDCardinalityBudget)
	}
	dir := t.TempDir()
	s, err := New(Config{StorageDir: dir, MaxInstallations: 0, Logger: discardLogger()})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	for i := 1; i <= 3; i++ {
		req := makeMetricsRequest(strconv.Itoa(i), "sync", "factory")
		resp := postProto(t, client, ts.URL, req)
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("request %d: expected 200, got %d: %s", i, resp.StatusCode, body)
		}
		_ = resp.Body.Close()
	}
}
