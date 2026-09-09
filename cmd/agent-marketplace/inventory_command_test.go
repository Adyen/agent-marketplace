package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"

	metricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"
)

type inventoryCommandExporter struct {
	mu        sync.Mutex
	snapshots []map[string]string
	server    *httptest.Server
}

func newInventoryCommandExporter(t *testing.T) *inventoryCommandExporter {
	t.Helper()
	exporter := &inventoryCommandExporter{}
	exporter.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/metrics" {
			w.WriteHeader(http.StatusOK)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read metrics: %v", err)
			w.WriteHeader(http.StatusOK)
			return
		}
		var request metricspb.ExportMetricsServiceRequest
		if err := proto.Unmarshal(body, &request); err != nil {
			t.Errorf("unmarshal metrics: %v", err)
			w.WriteHeader(http.StatusOK)
			return
		}
		exporter.mu.Lock()
		defer exporter.mu.Unlock()
		for _, resource := range request.ResourceMetrics {
			for _, scope := range resource.ScopeMetrics {
				for _, metric := range scope.Metrics {
					if metric.Name != string(tmcfg.InventorySnapshotName) {
						continue
					}
					for _, point := range inventoryNumberPoints(metric) {
						attrs := make(map[string]string, len(point.Attributes))
						for _, attr := range point.Attributes {
							attrs[attr.Key] = attr.Value.GetStringValue()
						}
						exporter.snapshots = append(exporter.snapshots, attrs)
					}
				}
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(exporter.server.Close)
	return exporter
}

func inventoryNumberPoints(metric *metricpb.Metric) []*metricpb.NumberDataPoint {
	if gauge := metric.GetGauge(); gauge != nil {
		return gauge.DataPoints
	}
	return nil
}

func (e *inventoryCommandExporter) context(t *testing.T, command string) (context.Context, *telemetry.Telemetry) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	state, err := telemetry.ResolveTelemetryConfig(nil, &config.Config{Telemetry: &tmcfg.TelemetryConfig{
		Level: tmcfg.LevelDetailed, Endpoint: e.server.URL,
	}}, "", "")
	if err != nil {
		t.Fatalf("resolve telemetry config: %v", err)
	}
	tel, err := telemetry.New(context.Background(), state, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	ctx := telemetry.NewContext(context.Background(), tel, command, time.Now(), "factory", false, telemetry.Span{}, false)
	return ctx, tel
}

func (e *inventoryCommandExporter) shutdown(t *testing.T, tel *telemetry.Telemetry) []map[string]string {
	t.Helper()
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown telemetry: %v", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]map[string]string(nil), e.snapshots...)
}

func TestDeinitPurgeConfigReportsOneFinalInventory(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	tests := []struct {
		name  string
		scope deinitScope
	}{
		{name: "user", scope: deinitScopeUser},
		{name: "repo", scope: deinitScopeRepo},
		{name: "local", scope: deinitScopeLocal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			repoRoot := filepath.Join(t.TempDir(), "repo")
			registryRoot := filepath.Join(t.TempDir(), "registry")
			writeSkill(t, registryRoot, "configured-skill", "configured", nil)
			writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "registries: []")
			writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "registries: []")
			writeFile(t, filepath.Join(repoRoot, ".agents", "config.local.yaml"), "registries: []\n")

			configPath, err := configFilesForScope(test.scope, repoRoot)
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, configPath[0], "apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - path: "+registryRoot+"\n    source_harness: claude\n    skills: [configured-skill]\n")
			withWorkingDir(t, repoRoot)

			exporter := newInventoryCommandExporter(t)
			ctx, tel := exporter.context(t, "deinit")
			if err := runDeinit(ctx, io.Discard, test.scope, true, false); err != nil {
				t.Fatalf("runDeinit: %v", err)
			}
			snapshots := exporter.shutdown(t, tel)
			if len(snapshots) != 1 {
				t.Fatalf("snapshot count = %d, want exactly one final generation: %#v", len(snapshots), snapshots)
			}
			if got := snapshots[0][string(tmcfg.MetricAttrRegistryCount)]; got != "0" {
				t.Fatalf("final registry count = %q, want 0 after purge", got)
			}
		})
	}
}

func TestDeinitPurgeConfigDryRunDoesNotReportMutation(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "registries: []")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "registries: []")
	withWorkingDir(t, repoRoot)

	exporter := newInventoryCommandExporter(t)
	ctx, tel := exporter.context(t, "deinit")
	if err := runDeinit(ctx, io.Discard, deinitScopeUser, true, true); err != nil {
		t.Fatalf("runDeinit dry-run: %v", err)
	}
	if snapshots := exporter.shutdown(t, tel); len(snapshots) != 0 {
		t.Fatalf("dry-run snapshot count = %d, want 0: %#v", len(snapshots), snapshots)
	}
}

func TestSyncPruneStaleReportsExactlyOneGeneration(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	tests := []struct {
		name          string
		selection     string
		wantResources int
	}{
		{name: "prune mutation uses recursive final sync", selection: "missing-skill", wantResources: 0},
		{name: "no mutation records computed generation", selection: "present-skill", wantResources: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			repoRoot := filepath.Join(t.TempDir(), "repo")
			registryRoot := filepath.Join(t.TempDir(), "registry")
			writeSkill(t, registryRoot, "present-skill", "present", nil)
			writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "harnesses: [factory]\nregistries: []")
			writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "registries:\n  - path: "+registryRoot+"\n    source_harness: claude\n    skills: ["+test.selection+"]")
			withWorkingDir(t, repoRoot)

			exporter := newInventoryCommandExporter(t)
			ctx, tel := exporter.context(t, "sync")
			if err := runSync(ctx, io.Discard, false, false, true, false); err != nil {
				t.Fatalf("runSync: %v", err)
			}
			snapshots := exporter.shutdown(t, tel)
			if len(snapshots) != 1 {
				t.Fatalf("snapshot count = %d, want exactly one: %#v", len(snapshots), snapshots)
			}
			want := strconv.Itoa(test.wantResources)
			if got := snapshots[0][string(tmcfg.MetricAttrResourceCount)]; got != want {
				t.Fatalf("resource count = %q, want %q", got, want)
			}
		})
	}
}

func TestDeinitFinalInventoryResolutionFailureIsBestEffort(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "registries: []")
	repoConfig := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, repoConfig, "registries: []")
	withWorkingDir(t, repoRoot)

	exporter := newInventoryCommandExporter(t)
	ctx, tel := exporter.context(t, "deinit")
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	var output bytes.Buffer
	if err := runDeinit(ctx, &output, deinitScopeRepo, true, false); err != nil {
		t.Fatalf("inventory resolution failure changed deinit exit: %v", err)
	}
	snapshots := exporter.shutdown(t, tel)
	if _, err := os.Stat(repoConfig); !os.IsNotExist(err) {
		t.Fatalf("repo config was not purged: %v", err)
	}
	got := output.String()
	if !strings.Contains(got, "No managed artifacts to remove\n") || !strings.Contains(got, "Removed config ") || strings.Contains(got, "warning:") {
		t.Fatalf("inventory resolution failure changed deinit output:\n%q", got)
	}
	if len(snapshots) != 0 {
		t.Fatalf("failed final resolution emitted snapshots: %#v", snapshots)
	}
}
