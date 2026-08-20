package receiver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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

func makeMetricsRequest(installationID, commandName, harnesses string) *metricspb.ExportMetricsServiceRequest {
	cmdAttr := strKV(string(tmcfg.ResourceAttrCommandName), commandName)
	harnessAttr := strKV(string(tmcfg.ResourceAttrHarnesses), harnesses)
	return &metricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*mpb.ResourceMetrics{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{
						strKV(string(tmcfg.ResourceAttrSchemaVersion), tmcfg.SchemaVersion),
						strKV(string(tmcfg.ResourceAttrInstallationID), installationID),
					},
				},
				ScopeMetrics: []*mpb.ScopeMetrics{
					{
						Metrics: []*mpb.Metric{
							{
								Name: string(tmcfg.CommandCounterName),
								Data: &mpb.Metric_Sum{
									Sum: &mpb.Sum{
										DataPoints: []*mpb.NumberDataPoint{
											{
												Value:      &mpb.NumberDataPoint_AsInt{AsInt: 1},
												Attributes: []*commonpb.KeyValue{cmdAttr, harnessAttr},
											},
										},
									},
								},
							},
							{
								Name: string(tmcfg.CommandDurationName),
								Data: &mpb.Metric_Histogram{
									Histogram: &mpb.Histogram{
										DataPoints: []*mpb.HistogramDataPoint{
											{
												Count:      1,
												Attributes: []*commonpb.KeyValue{cmdAttr, harnessAttr},
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
	r, err := http.NewRequest(http.MethodPost, url+"/v1/metrics", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	r.Header.Set("Content-Type", "application/x-protobuf")
	resp, err := client.Do(r)
	if err != nil {
		t.Fatalf("post metrics: %v", err)
	}
	return resp
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
	if !strings.Contains(string(data), "persist-1") {
		t.Fatalf("record file missing installation id: %s", data)
	}
	if !strings.Contains(string(data), "\"metric_count\":2") {
		t.Fatalf("record file missing metric count: %s", data)
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
