package telemetry

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	amconfig "github.com/Adyen/agent-marketplace/internal/config"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"

	metricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"
)

func TestInventoryRoutesOnlyToDetailedPrimary(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var primaryMu, communityMu sync.Mutex
	var primaryNames, communityNames []string
	primary := metricNamesServer(t, &primaryMu, &primaryNames)
	defer primary.Close()
	community := metricNamesServer(t, &communityMu, &communityNames)
	defer community.Close()

	user := amconfig.Settings{Telemetry: &tmcfg.TelemetryConfig{
		Level:              tmcfg.LevelDetailed,
		Endpoint:           primary.URL,
		CommunityReporting: boolPtr(true),
	}}
	state, err := ResolveTelemetryConfig(amconfig.Settings{}, user, community.URL, "")
	if err != nil {
		t.Fatalf("resolve telemetry: %v", err)
	}
	if state.CommunityReportingFoldedIntoPrimary {
		t.Fatal("test requires independent routes")
	}

	ctx := context.Background()
	tel, err := New(ctx, state, nil)
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	tel.CommandCounter().Add(ctx, 1, map[string]string{"command.name": "sync"})
	tel.CommandDuration().Record(ctx, 42, map[string]string{"command.name": "sync"})
	tel.RecordInventory(ctx, InventorySnapshot{
		Fingerprint:  "abc123",
		Harnesses:    "claude,factory",
		HarnessScope: "repo",
		Registries: []InventoryRegistry{{
			ID: "github.com/adyen/example", Location: "https://github.com/Adyen/example.git",
			Path: "catalog", Ref: "main", ConfigScope: "organization", Required: true, Default: true,
		}},
		Resources: []InventoryResource{{
			RegistryID: "github.com/adyen/example", RegistryLocation: "https://github.com/Adyen/example.git",
			RegistryPath: "catalog", RegistryRef: "main", Kind: "skill", Name: "review",
			ConfigScope: "repo", Required: true,
		}},
	})
	if err := tel.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	primaryMu.Lock()
	gotPrimary := append([]string(nil), primaryNames...)
	primaryMu.Unlock()
	communityMu.Lock()
	gotCommunity := append([]string(nil), communityNames...)
	communityMu.Unlock()
	sort.Strings(gotPrimary)
	sort.Strings(gotCommunity)

	wantPrimary := []string{
		string(tmcfg.CommandCounterName),
		string(tmcfg.CommandDurationName),
		string(tmcfg.InventoryRegistryName),
		string(tmcfg.InventoryResourceName),
		string(tmcfg.InventorySnapshotName),
	}
	sort.Strings(wantPrimary)
	if !equalStrings(gotPrimary, wantPrimary) {
		t.Fatalf("primary metrics = %v, want %v", gotPrimary, wantPrimary)
	}
	wantCommunity := []string{string(tmcfg.CommandCounterName), string(tmcfg.CommandDurationName)}
	sort.Strings(wantCommunity)
	if !equalStrings(gotCommunity, wantCommunity) {
		t.Fatalf("community metrics = %v, want basic command metrics only %v", gotCommunity, wantCommunity)
	}
}

func TestInventoryBoundsContract(t *testing.T) {
	if MaxInventoryRegistries != 1_000 || MaxInventoryResources != 10_000 || MaxInventoryAttributeBytes != 512 {
		t.Fatalf("inventory bounds = %d/%d/%d", MaxInventoryRegistries, MaxInventoryResources, MaxInventoryAttributeBytes)
	}
	if InventoryCardinalityLimit <= MaxInventoryResources {
		t.Fatalf("inventory cardinality limit = %d, must preserve %d resources plus overflow capacity", InventoryCardinalityLimit, MaxInventoryResources)
	}

	within := InventorySnapshot{
		Fingerprint: strings.Repeat("é", MaxInventoryAttributeBytes/len("é")),
		Registries:  make([]InventoryRegistry, MaxInventoryRegistries),
		Resources:   make([]InventoryResource, MaxInventoryResources),
	}
	if !InventorySnapshotWithinBounds(within) {
		t.Fatal("snapshot exactly at count and UTF-8 byte bounds was rejected")
	}
	tooManyRegistries := within
	tooManyRegistries.Registries = make([]InventoryRegistry, MaxInventoryRegistries+1)
	if InventorySnapshotWithinBounds(tooManyRegistries) {
		t.Fatal("snapshot above registry bound was accepted")
	}
	tooManyResources := within
	tooManyResources.Resources = make([]InventoryResource, MaxInventoryResources+1)
	if InventorySnapshotWithinBounds(tooManyResources) {
		t.Fatal("snapshot above resource bound was accepted")
	}
	tooLong := within
	tooLong.Fingerprint += "é"
	if InventorySnapshotWithinBounds(tooLong) {
		t.Fatal("snapshot above UTF-8 byte bound was accepted")
	}
}

func TestInventoryProviderOverridesEnvironmentCardinalityLimit(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("OTEL_GO_X_CARDINALITY_LIMIT", "1")

	var mu sync.Mutex
	var captured *metricspb.ExportMetricsServiceRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req metricspb.ExportMetricsServiceRequest
		if err := proto.Unmarshal(body, &req); err != nil {
			t.Errorf("unmarshal metrics: %v", err)
		}
		if findMetric(&req, string(tmcfg.InventoryResourceName)) != nil {
			mu.Lock()
			captured = &req
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	user := amconfig.Settings{Telemetry: &tmcfg.TelemetryConfig{Level: tmcfg.LevelDetailed, Endpoint: srv.URL}}
	state, err := ResolveTelemetryConfig(amconfig.Settings{}, user, "", "")
	if err != nil {
		t.Fatalf("resolve telemetry: %v", err)
	}
	tel, err := New(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	tel.RecordInventory(context.Background(), InventorySnapshot{
		Fingerprint: "cardinality",
		Resources: []InventoryResource{
			{Name: "one", Kind: "skill"},
			{Name: "two", Kind: "skill"},
		},
	})
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	mu.Lock()
	req := captured
	mu.Unlock()
	if req == nil {
		t.Fatal("no resource inventory export captured")
	}
	points := findMetric(req, string(tmcfg.InventoryResourceName)).GetGauge().GetDataPoints()
	if len(points) != 2 {
		t.Fatalf("resource points = %d, want 2 despite environment cardinality limit", len(points))
	}
	for _, point := range points {
		if attrs := stringAttributes(point.GetAttributes()); attrs["otel.metric.overflow"] != "" {
			t.Fatalf("unexpected overflow point: %v", attrs)
		}
	}
}

func TestStopInventoryWorkerReturnsAtDeadline(t *testing.T) {
	cancelled := make(chan struct{})
	var cancelOnce sync.Once
	tel := &Telemetry{
		inventoryQueue: make(chan inventoryObservation),
		inventoryDone:  make(chan struct{}),
		inventoryCancel: func() {
			cancelOnce.Do(func() { close(cancelled) })
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	if tel.stopInventoryWorker(ctx) {
		t.Fatal("worker reported stopped while done remained blocked")
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("worker stop took %v after deadline", elapsed)
	}
	select {
	case <-cancelled:
	default:
		t.Fatal("worker cancellation was not requested")
	}
	close(tel.inventoryDone)
}

func TestShutdownPersistsEveryAcceptedInventoryObservation(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var mu sync.Mutex
	seen := make(map[string]bool)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req metricspb.ExportMetricsServiceRequest
		_ = proto.Unmarshal(body, &req)
		if metric := findMetric(&req, string(tmcfg.InventorySnapshotName)); metric != nil {
			mu.Lock()
			for _, point := range metric.GetGauge().GetDataPoints() {
				seen[stringAttributes(point.GetAttributes())["config.fingerprint"]] = true
			}
			mu.Unlock()
		}
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	user := amconfig.Settings{Telemetry: &tmcfg.TelemetryConfig{Level: tmcfg.LevelDetailed, Endpoint: srv.URL}}
	state, err := ResolveTelemetryConfig(amconfig.Settings{}, user, "", "")
	if err != nil {
		t.Fatalf("resolve telemetry: %v", err)
	}
	tel, err := New(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	for _, fingerprint := range []string{"one", "two", "three"} {
		tel.RecordInventory(context.Background(), InventorySnapshot{Fingerprint: fingerprint})
	}
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, fingerprint := range []string{"one", "two", "three"} {
		if !seen[fingerprint] {
			t.Errorf("accepted inventory %q never reached the exporter", fingerprint)
		}
	}
	if got := tel.queue.Count(); got == 0 {
		t.Fatal("failed shutdown export was not durably queued")
	}
}

func TestConcurrentRecordInventoryAndRepeatedShutdown(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	user := amconfig.Settings{Telemetry: &tmcfg.TelemetryConfig{Level: tmcfg.LevelDetailed, Endpoint: srv.URL}}
	state, err := ResolveTelemetryConfig(amconfig.Settings{}, user, "", "")
	if err != nil {
		t.Fatalf("resolve telemetry: %v", err)
	}
	tel, err := New(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			for i := 0; i < 50; i++ {
				tel.RecordInventory(context.Background(), InventorySnapshot{Fingerprint: fmt.Sprintf("%d-%d", worker, i)})
			}
		}(worker)
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := tel.Shutdown(context.Background()); err != nil {
				t.Errorf("shutdown: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("repeated shutdown: %v", err)
	}
}

func TestRecordInventoryDoesNotBlockOnExport(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req metricspb.ExportMetricsServiceRequest
		_ = proto.Unmarshal(body, &req)
		if findMetric(&req, string(tmcfg.InventorySnapshotName)) != nil {
			once.Do(func() { close(started) })
			<-release
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	user := amconfig.Settings{Telemetry: &tmcfg.TelemetryConfig{Level: tmcfg.LevelDetailed, Endpoint: srv.URL}}
	state, err := ResolveTelemetryConfig(amconfig.Settings{}, user, "", "")
	if err != nil {
		t.Fatalf("resolve telemetry: %v", err)
	}
	tel, err := New(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	returned := make(chan struct{})
	go func() {
		tel.RecordInventory(context.Background(), InventorySnapshot{Fingerprint: "async"})
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("RecordInventory blocked on network export")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background inventory export did not start promptly")
	}
	close(release)
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestSequentialInventorySnapshotsExportOnceInOrder(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	type inventoryExport struct {
		fingerprint string
		observedAt  int64
	}
	var mu sync.Mutex
	var exports []inventoryExport
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req metricspb.ExportMetricsServiceRequest
		if err := proto.Unmarshal(body, &req); err != nil {
			t.Errorf("unmarshal metrics: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		metric := findMetric(&req, string(tmcfg.InventorySnapshotName))
		if metric != nil {
			mu.Lock()
			for _, point := range metric.GetGauge().GetDataPoints() {
				exports = append(exports, inventoryExport{
					fingerprint: stringAttributes(point.GetAttributes())["config.fingerprint"],
					observedAt:  point.GetAsInt(),
				})
			}
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	user := amconfig.Settings{Telemetry: &tmcfg.TelemetryConfig{Level: tmcfg.LevelDetailed, Endpoint: srv.URL}}
	state, err := ResolveTelemetryConfig(amconfig.Settings{}, user, "", "")
	if err != nil {
		t.Fatalf("resolve telemetry: %v", err)
	}
	tel, err := New(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	tel.RecordInventory(context.Background(), InventorySnapshot{Fingerprint: "A"})
	tel.RecordInventory(context.Background(), InventorySnapshot{Fingerprint: "B"})
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	mu.Lock()
	got := append([]inventoryExport(nil), exports...)
	mu.Unlock()
	sort.Slice(got, func(i, j int) bool { return got[i].observedAt < got[j].observedAt })
	if len(got) != 2 {
		t.Fatalf("inventory observations = %v, want exactly one per snapshot", got)
	}
	if got[0].fingerprint != "A" || got[1].fingerprint != "B" {
		t.Fatalf("inventory fingerprints = %q then %q, want A then B without retained series", got[0].fingerprint, got[1].fingerprint)
	}
	if got[0].observedAt <= 0 || got[1].observedAt <= got[0].observedAt {
		t.Fatalf("observation values = %d then %d, want strictly increasing ordering keys", got[0].observedAt, got[1].observedAt)
	}
}

func TestInventoryGaugeValuesAndAttributes(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var mu sync.Mutex
	var captured *metricspb.ExportMetricsServiceRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req metricspb.ExportMetricsServiceRequest
		if err := proto.Unmarshal(body, &req); err != nil {
			t.Errorf("unmarshal metrics: %v", err)
		}
		if findMetric(&req, string(tmcfg.InventorySnapshotName)) != nil {
			mu.Lock()
			captured = &req
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	user := amconfig.Settings{Telemetry: &tmcfg.TelemetryConfig{Level: tmcfg.LevelDetailed, Endpoint: srv.URL}}
	state, err := ResolveTelemetryConfig(amconfig.Settings{}, user, "", "")
	if err != nil {
		t.Fatalf("resolve telemetry: %v", err)
	}
	tel, err := New(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	tel.RecordInventory(context.Background(), InventorySnapshot{
		Fingerprint: "fingerprint", Harnesses: "claude,factory", HarnessScope: "repo",
		Registries: []InventoryRegistry{{ID: "github.com/adyen/example", Location: "https://github.com/Adyen/example.git", Path: "catalog", Ref: "main", ConfigScope: "organization", Required: true, Default: false}},
		Resources:  []InventoryResource{{RegistryID: "github.com/adyen/example", RegistryLocation: "https://github.com/Adyen/example.git", RegistryPath: "catalog", RegistryRef: "main", Kind: "skill", Name: "review", ConfigScope: "repo", Required: true}},
	})
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	mu.Lock()
	req := captured
	mu.Unlock()
	if req == nil {
		t.Fatal("no metric request captured")
	}

	assertGauge := func(name string, wantValue int64, want map[string]string) {
		t.Helper()
		metric := findMetric(req, name)
		if metric == nil {
			t.Fatalf("metric %q not found", name)
		}
		points := metric.GetGauge().GetDataPoints()
		if len(points) != 1 || points[0].GetAsInt() != wantValue {
			t.Fatalf("metric %q points = %v, want one point with value %d", name, points, wantValue)
		}
		got := stringAttributes(points[0].GetAttributes())
		if len(got) != len(want) {
			t.Fatalf("metric %q attributes = %v, want %v", name, got, want)
		}
		for key, value := range want {
			if got[key] != value {
				t.Fatalf("metric %q attribute %q = %q, want %q", name, key, got[key], value)
			}
		}
	}
	snapshotValue := findMetric(req, string(tmcfg.InventorySnapshotName)).GetGauge().GetDataPoints()[0].GetAsInt()
	if snapshotValue <= 0 {
		t.Fatalf("snapshot observation value = %d, want positive Unix-microsecond ordering key", snapshotValue)
	}
	assertGauge(string(tmcfg.InventorySnapshotName), snapshotValue, map[string]string{
		"config.fingerprint": "fingerprint", "harnesses": "claude,factory", "harness.scope": "repo", "registry.count": "1", "resource.count": "1",
	})
	assertGauge(string(tmcfg.InventoryRegistryName), 1, map[string]string{
		"config.fingerprint": "fingerprint", "registry.id": "github.com/adyen/example", "registry.location": "https://github.com/Adyen/example.git", "registry.path": "catalog", "registry.ref": "main", "config.scope": "organization", "registry.required": "true", "registry.default": "false",
	})
	assertGauge(string(tmcfg.InventoryResourceName), 1, map[string]string{
		"config.fingerprint": "fingerprint", "registry.id": "github.com/adyen/example", "registry.location": "https://github.com/Adyen/example.git", "registry.path": "catalog", "registry.ref": "main", "resource.kind": "skill", "resource.name": "review", "config.scope": "repo", "resource.required": "true",
	})
}

func TestInventoryOmissionCounterHasBoundedReason(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var mu sync.Mutex
	var captured *metricspb.ExportMetricsServiceRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req metricspb.ExportMetricsServiceRequest
		if err := proto.Unmarshal(body, &req); err != nil {
			t.Errorf("unmarshal metrics: %v", err)
		}
		if findMetric(&req, string(tmcfg.InventoryOmissionsName)) != nil {
			mu.Lock()
			captured = &req
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	user := amconfig.Settings{Telemetry: &tmcfg.TelemetryConfig{Level: tmcfg.LevelDetailed, Endpoint: srv.URL}}
	state, err := ResolveTelemetryConfig(amconfig.Settings{}, user, "", "")
	if err != nil {
		t.Fatalf("resolve telemetry: %v", err)
	}
	tel, err := New(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	tel.RecordInventoryOmission(context.Background(), InventoryOmissionReason("unbounded-user-value"))
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	mu.Lock()
	req := captured
	mu.Unlock()
	if req == nil {
		t.Fatal("no omission metric captured")
	}
	points := findMetric(req, string(tmcfg.InventoryOmissionsName)).GetSum().GetDataPoints()
	if len(points) != 1 || points[0].GetAsInt() != 1 {
		t.Fatalf("omission points = %v, want one increment", points)
	}
	if got := stringAttributes(points[0].GetAttributes())["reason"]; got != string(InventoryOmissionInvalid) {
		t.Fatalf("omission reason = %q, want bounded fallback %q", got, InventoryOmissionInvalid)
	}
}

func TestInventoryNoOpAtBasicLevel(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var mu sync.Mutex
	var names []string
	srv := metricNamesServer(t, &mu, &names)
	defer srv.Close()
	user := amconfig.Settings{Telemetry: &tmcfg.TelemetryConfig{Level: tmcfg.LevelBasic, Endpoint: srv.URL}}
	state, err := ResolveTelemetryConfig(amconfig.Settings{}, user, "", "")
	if err != nil {
		t.Fatalf("resolve telemetry: %v", err)
	}
	tel, err := New(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	tel.RecordInventory(context.Background(), InventorySnapshot{Fingerprint: "should-not-export"})
	tel.RecordInventoryOmission(context.Background(), InventoryOmissionTooLarge)
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(names) != 0 {
		t.Fatalf("basic telemetry exported inventory: %v", names)
	}
}

func metricNamesServer(t *testing.T, mu *sync.Mutex, names *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/metrics" {
			w.WriteHeader(http.StatusOK)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read metrics: %v", err)
		}
		var req metricspb.ExportMetricsServiceRequest
		if err := proto.Unmarshal(body, &req); err != nil {
			t.Errorf("unmarshal metrics: %v", err)
		}
		mu.Lock()
		for _, rm := range req.ResourceMetrics {
			for _, sm := range rm.ScopeMetrics {
				for _, metric := range sm.Metrics {
					*names = append(*names, metric.Name)
				}
			}
		}
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
}

func findMetric(req *metricspb.ExportMetricsServiceRequest, name string) *metricpb.Metric {
	for _, rm := range req.ResourceMetrics {
		for _, sm := range rm.ScopeMetrics {
			for _, metric := range sm.Metrics {
				if metric.Name == name {
					return metric
				}
			}
		}
	}
	return nil
}

func stringAttributes(attrs []*commonpb.KeyValue) map[string]string {
	out := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		out[attr.Key] = attr.Value.GetStringValue()
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
