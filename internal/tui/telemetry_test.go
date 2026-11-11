package tui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	amconfig "github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	tea "charm.land/bubbletea/v2"
	metricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	tracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracev1pb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

type testSpanExporter struct {
	mu                 sync.Mutex
	spans              []*tracev1pb.Span
	inventoryHarnesses []string
	server             *httptest.Server
}

func newTestSpanExporter(t *testing.T) *testSpanExporter {
	t.Helper()
	e := &testSpanExporter{}
	e.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/metrics" {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read metrics: %v", err)
			} else {
				var req metricspb.ExportMetricsServiceRequest
				if err := proto.Unmarshal(body, &req); err != nil {
					t.Errorf("unmarshal metrics: %v", err)
				} else {
					e.mu.Lock()
					for _, resource := range req.ResourceMetrics {
						for _, scope := range resource.ScopeMetrics {
							for _, metric := range scope.Metrics {
								if metric.Name == string(tmcfg.InventorySnapshotName) {
									for _, point := range metric.GetGauge().GetDataPoints() {
										e.inventoryHarnesses = append(e.inventoryHarnesses, attrValue(point.Attributes, "harnesses"))
									}
								}
							}
						}
					}
					e.mu.Unlock()
				}
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path != "/v1/traces" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			w.WriteHeader(http.StatusOK)
			return
		}
		var req tracepb.ExportTraceServiceRequest
		if err := proto.Unmarshal(body, &req); err != nil {
			t.Errorf("unmarshal traces: %v", err)
			w.WriteHeader(http.StatusOK)
			return
		}
		e.mu.Lock()
		for _, rs := range req.ResourceSpans {
			for _, ils := range rs.ScopeSpans {
				e.spans = append(e.spans, ils.Spans...)
			}
		}
		e.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(e.server.Close)
	return e
}

func (e *testSpanExporter) findSpan(name string) *tracev1pb.Span {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, s := range e.spans {
		if s.Name == name {
			return s
		}
	}
	return nil
}

func (e *testSpanExporter) spansNamed(name string) []*tracev1pb.Span {
	e.mu.Lock()
	defer e.mu.Unlock()
	var spans []*tracev1pb.Span
	for _, s := range e.spans {
		if s.Name == name {
			spans = append(spans, s)
		}
	}
	return spans
}

// inventorySnapshotHarnesses returns the harnesses attribute of each exported
// inventory snapshot, in export order.
func (e *testSpanExporter) inventorySnapshotHarnesses() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.inventoryHarnesses...)
}

func (e *testSpanExporter) waitForSpan(name string, timeout time.Duration) *tracev1pb.Span {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if span := e.findSpan(name); span != nil {
			return span
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

func attrValue(attrs []*commonpb.KeyValue, key string) string {
	for _, a := range attrs {
		if a.Key == key && a.Value != nil {
			return a.Value.GetStringValue()
		}
	}
	return ""
}

func setupTuiTelemetry(t *testing.T, e *testSpanExporter) (context.Context, *telemetry.Telemetry) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	user := amconfig.Settings{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelDetailed,
			Endpoint: e.server.URL,
		},
	}

	state, err := telemetry.ResolveTelemetryConfig(amconfig.Settings{}, user, "", "")
	if err != nil {
		t.Fatalf("resolve telemetry config: %v", err)
	}

	tel, err := telemetry.New(context.Background(), state, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}

	ctx := telemetry.NewContext(context.Background(), tel, "tui", time.Now(), "factory", false, telemetry.Span{}, false)
	return ctx, tel
}

func TestRecordActionSpanEmits(t *testing.T) {
	e := newTestSpanExporter(t)
	ctx, tel := setupTuiTelemetry(t, e)
	defer func() { _ = tel.Shutdown(context.Background()) }()

	recordActionSpan(ctx, "tui.action.test", map[string]string{"resource.kind": "skill"})

	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	s := e.findSpan("agent_marketplace.tui.action")
	if s == nil {
		t.Fatal("expected agent_marketplace.tui.action span")
	}
	if got := attrValue(s.Attributes, "action"); got != "tui.action.test" {
		t.Fatalf("expected action attribute %q, got %q", "tui.action.test", got)
	}
	if got := attrValue(s.Attributes, "resource.kind"); got != "skill" {
		t.Fatalf("expected resource.kind attribute %q, got %q", "skill", got)
	}
}

func TestRecordActionSpanExportsBeforeTUIShutdown(t *testing.T) {
	e := newTestSpanExporter(t)
	ctx, tel := setupTuiTelemetry(t, e)
	defer func() { _ = tel.Shutdown(context.Background()) }()

	ctx, commandSpan := tel.StartSpan(ctx, "agent_marketplace.command", nil)
	defer commandSpan.End()
	ctx, sessionSpan := tel.StartSpan(ctx, "agent_marketplace.tui.session", nil)
	defer sessionSpan.End()

	recordActionSpan(ctx, "sync", nil)

	if e.waitForSpan("agent_marketplace.tui.action", 5*time.Second) == nil {
		t.Fatal("expected TUI action span to export while the TUI remains running")
	}
}

func TestRecordActionSpanNoTelemetry(t *testing.T) {
	ctx := context.Background()
	recordActionSpan(ctx, "tui.action.test", nil)
}

func snapshotForHarness(target hcc.Target) *pipeline.ResolveResult {
	return &pipeline.ResolveResult{
		MergedCfg:           &amconfig.Config{},
		TargetHarnesses:     []hcc.Target{target},
		TargetHarnessSource: amconfig.UserPersonal,
	}
}

func applyFinalSnapshot(m Model, generation requestGeneration, result *pipeline.ResolveResult) Model {
	updated, _ := m.Update(snapshotLoadedMsg{generation: generation, mergedCfg: result.MergedCfg, inventoryResult: result})
	return updated.(Model)
}

// deliverFinalSnapshots sends each accepted target as its own current
// generation, then a stale final snapshot that must never become pending.
func deliverFinalSnapshots(m Model, stale hcc.Target, accepted ...hcc.Target) Model {
	staleGeneration := m.beginRequest(requestSnapshot)
	for _, target := range accepted {
		m = applyFinalSnapshot(m, m.beginRequest(requestSnapshot), snapshotForHarness(target))
	}
	return applyFinalSnapshot(m, staleGeneration, snapshotForHarness(stale))
}

// spec: telemetry/interactive-edits-record-only-the-final-configuration
func TestModelRecordsNoInventoryBeforeSessionFlush(t *testing.T) {
	e := newTestSpanExporter(t)
	ctx, tel := setupTuiTelemetry(t, e)
	m := NewModel(t.TempDir(), t.TempDir())
	m.ctx = ctx
	m = deliverFinalSnapshots(m, hcc.TargetClaude, hcc.TargetFactory, hcc.TargetDevin, hcc.TargetCursor)
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if got := e.inventorySnapshotHarnesses(); len(got) != 0 {
		t.Fatalf("inventory recorded before the session flushed: %v", got)
	}
}

// spec: telemetry/interactive-edits-record-only-the-final-configuration
func TestModelFlushRecordsOnlyLastAcceptedInventory(t *testing.T) {
	e := newTestSpanExporter(t)
	ctx, tel := setupTuiTelemetry(t, e)
	m := NewModel(t.TempDir(), t.TempDir())
	m.ctx = ctx
	m = deliverFinalSnapshots(m, hcc.TargetClaude, hcc.TargetFactory, hcc.TargetDevin, hcc.TargetCursor)
	m.pendingInventory.flush(ctx)
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if got := e.inventorySnapshotHarnesses(); len(got) != 1 || got[0] != "cursor" {
		t.Fatalf("inventory harnesses = %v, want one snapshot for the last accepted configuration [cursor]", got)
	}
}

// spec: telemetry/failed-final-snapshot-records-nothing
func TestModelFlushRecordsNothingAfterFailedFinalSnapshot(t *testing.T) {
	e := newTestSpanExporter(t)
	ctx, tel := setupTuiTelemetry(t, e)
	m := NewModel(t.TempDir(), t.TempDir())
	m.ctx = ctx
	m = applyFinalSnapshot(m, m.beginRequest(requestSnapshot), snapshotForHarness(hcc.TargetFactory))
	failed := snapshotForHarness(hcc.TargetCursor)
	failed.Errors = []error{errors.New("resolution failed")}
	m = applyFinalSnapshot(m, m.beginRequest(requestSnapshot), failed)
	m.pendingInventory.flush(ctx)
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if got := e.inventorySnapshotHarnesses(); len(got) != 0 {
		t.Fatalf("failed final snapshot recorded inventory: %v", got)
	}
}

// spec: telemetry/failed-final-snapshot-records-nothing
func TestModelFlushRecordsNothingAfterConfigLoadFailure(t *testing.T) {
	cases := []struct {
		name string
		wrap func(snapshotLoadedMsg) tea.Msg
	}{
		{"snapshot", func(s snapshotLoadedMsg) tea.Msg { return s }},
		{"refresh", func(s snapshotLoadedMsg) tea.Msg { return refreshLoadedMsg{s} }},
		{"reload", func(s snapshotLoadedMsg) tea.Msg { return reloadMsg(s) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestSpanExporter(t)
			ctx, tel := setupTuiTelemetry(t, e)
			m := NewModel(t.TempDir(), t.TempDir())
			m.ctx = ctx
			m = applyFinalSnapshot(m, m.beginRequest(requestSnapshot), snapshotForHarness(hcc.TargetFactory))
			failed := snapshotLoadedMsg{generation: m.beginRequest(requestSnapshot), errs: []error{errors.New("config load failed")}}
			updated, _ := m.Update(tc.wrap(failed))
			m = updated.(Model)
			m.pendingInventory.flush(ctx)
			if err := tel.Shutdown(context.Background()); err != nil {
				t.Fatalf("shutdown: %v", err)
			}
			if got := e.inventorySnapshotHarnesses(); len(got) != 0 {
				t.Fatalf("config load failure recorded inventory: %v", got)
			}
		})
	}
}

// inventoryExitProbe stands in for the startup load: Init delivers scripted
// accepted final snapshots, checks that session work records nothing, then quits.
type inventoryExitProbe struct {
	Model
	snapshots []*pipeline.ResolveResult
}

type scriptedSnapshotMsg struct{ result *pipeline.ResolveResult }

func (p inventoryExitProbe) Init() tea.Cmd {
	var cmds []tea.Cmd
	for _, result := range p.snapshots {
		cmds = append(cmds, func() tea.Msg { return scriptedSnapshotMsg{result: result} })
	}
	pipeline.RecordResolvedInventory(p.ctx, *snapshotForHarness(hcc.TargetClaude))
	return tea.Sequence(append(cmds, tea.Quit)...)
}

func (p inventoryExitProbe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if scripted, ok := msg.(scriptedSnapshotMsg); ok {
		msg = snapshotLoadedMsg{generation: p.beginRequest(requestSnapshot), mergedCfg: scripted.result.MergedCfg, inventoryResult: scripted.result}
	}
	next, cmd := p.Model.Update(msg)
	p.Model = next.(Model)
	return p, cmd
}

// spec: telemetry/interactive-edits-record-only-the-final-configuration
// spec: telemetry/session-operations-do-not-record-inventory
func TestRunRecordsFinalInventoryOnExit(t *testing.T) {
	e := newTestSpanExporter(t)
	ctx, tel := setupTuiTelemetry(t, e)
	snapshots := []*pipeline.ResolveResult{snapshotForHarness(hcc.TargetFactory), snapshotForHarness(hcc.TargetCursor)}
	err := runSession(ctx, t.TempDir(), t.TempDir(), func(model tea.Model) error {
		return runTUIModel(inventoryExitProbe{Model: model.(Model), snapshots: snapshots}, tea.WithInput(nil), tea.WithOutput(io.Discard))
	})
	if err != nil {
		t.Fatalf("run session: %v", err)
	}
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if got := e.inventorySnapshotHarnesses(); len(got) != 1 || got[0] != "cursor" {
		t.Fatalf("inventory harnesses = %v, want one snapshot for the last accepted configuration [cursor]", got)
	}
}

func TestRecordConfigSnapshotSpansEmitsExactNamesAndSanitizedRegistries(t *testing.T) {
	e := newTestSpanExporter(t)
	ctx, tel := setupTuiTelemetry(t, e)
	defer func() { _ = tel.Shutdown(context.Background()) }()

	remoteURL := "https://user:secret@gitlab.example.com/team/skills.git?token=hidden#fragment"
	scpURL := "git@gitlab.example.com:team/commands.git"
	localURL := "/Users/example/private-skills.git"
	recordConfigSnapshotSpans(ctx, snapshotLoadedMsg{
		targetHarnesses:     []hcc.Target{hcc.TargetFactory, hcc.TargetDevin},
		targetHarnessSource: amconfig.RepoCommitted,
		mergedCfg:           &amconfig.Config{},
		configuredRegistries: []amconfig.RegistryEntry{
			{URL: remoteURL, Ref: "main", Path: "resources/skills", Required: true, Source: amconfig.UserPersonal},
			{URL: scpURL, Ref: "release", Source: amconfig.RepoCommitted},
			{URL: localURL, Source: amconfig.RepoPersonal},
		},
		resolved: []resolver.ResolvedArtifact{
			{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "smith"}, RegistryURL: remoteURL, RegistryPath: "resources/skills", Ref: "main", Source: amconfig.UserPersonal, Required: true},
			{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "audit"}, RegistryURL: remoteURL, RegistryPath: "resources/skills", Ref: "main", Source: amconfig.UserPersonal, Required: true},
			{Entry: catalog.Entry{Kind: ir.KindCommand, Name: "review"}, RegistryURL: scpURL, Ref: "release", Source: amconfig.RepoCommitted},
			{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "private"}, RegistryURL: localURL, Source: amconfig.RepoPersonal},
		},
	})

	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	summary := e.findSpan("agent_marketplace.tui.config")
	if summary == nil {
		t.Fatal("expected config summary span")
	}
	if got := attrValue(summary.Attributes, "harnesses"); got != "devin,factory" {
		t.Fatalf("harnesses = %q, want devin,factory", got)
	}
	if got := attrValue(summary.Attributes, "harness.scope"); got != "repo" {
		t.Fatalf("harness.scope = %q, want repo", got)
	}
	if got := attrValue(summary.Attributes, "registry.count"); got != "3" {
		t.Fatalf("registry.count = %q, want 3", got)
	}
	if got := attrValue(summary.Attributes, "resource.count"); got != "4" {
		t.Fatalf("resource.count = %q, want 4", got)
	}

	registrySpans := e.spansNamed("agent_marketplace.tui.config_registry")
	if len(registrySpans) != 3 {
		t.Fatalf("registry spans = %d, want 3", len(registrySpans))
	}
	locations := make(map[string]*tracev1pb.Span, len(registrySpans))
	for _, span := range registrySpans {
		locations[attrValue(span.Attributes, "registry.location")] = span
	}
	for _, location := range []string{"https://gitlab.example.com/team/skills.git", "gitlab.example.com:team/commands.git", "local"} {
		if locations[location] == nil {
			t.Errorf("missing sanitized registry location %q", location)
		}
	}
	encoded, err := json.Marshal(registrySpans)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"user:secret", "secret", "token=hidden", "fragment", localURL} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("registry telemetry contains forbidden value %q", forbidden)
		}
	}

	resourceSpans := e.spansNamed("agent_marketplace.tui.config_resources")
	if len(resourceSpans) != 3 {
		t.Fatalf("resource spans = %d, want 3", len(resourceSpans))
	}
	var skillNames []string
	for _, span := range resourceSpans {
		if attrValue(span.Attributes, "resource.kind") == "skill" && attrValue(span.Attributes, "registry.location") == "https://gitlab.example.com/team/skills.git" {
			if err := json.Unmarshal([]byte(attrValue(span.Attributes, "resource.names")), &skillNames); err != nil {
				t.Fatalf("decode resource.names: %v", err)
			}
		}
	}
	if len(skillNames) != 2 || skillNames[0] != "audit" || skillNames[1] != "smith" {
		t.Fatalf("skill names = %#v, want [audit smith]", skillNames)
	}
}

func TestRecordConfigSnapshotSpansRejectsInvalidInventory(t *testing.T) {
	e := newTestSpanExporter(t)
	ctx, tel := setupTuiTelemetry(t, e)
	defer func() { _ = tel.Shutdown(context.Background()) }()

	recordConfigSnapshotSpans(ctx, snapshotLoadedMsg{
		configuredRegistries: []amconfig.RegistryEntry{{URL: "credential@second-secret@git.example:team/repo.git"}},
		resolved: []resolver.ResolvedArtifact{{
			Entry: catalog.Entry{Kind: ir.KindSkill, Name: "invalid-control"}, RegistryURL: "credential@second-secret@git.example:team/repo.git",
		}},
	})
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	for _, name := range []string{"agent_marketplace.tui.config", "agent_marketplace.tui.config_registry", "agent_marketplace.tui.config_resources"} {
		if span := e.findSpan(name); span != nil {
			t.Errorf("invalid inventory emitted %q span", name)
		}
	}
}

func TestRecordConfigSnapshotSpansChunksLargeResourceSets(t *testing.T) {
	e := newTestSpanExporter(t)
	ctx, tel := setupTuiTelemetry(t, e)
	defer func() { _ = tel.Shutdown(context.Background()) }()

	resolved := make([]resolver.ResolvedArtifact, 65)
	for i := range resolved {
		resolved[i] = resolver.ResolvedArtifact{
			Entry:       catalog.Entry{Kind: ir.KindSkill, Name: "skill-" + strconv.Itoa(i)},
			RegistryURL: "https://git.example.com/team/resources.git",
			Ref:         "main",
			Source:      amconfig.UserPersonal,
		}
	}
	recordConfigSnapshotSpans(ctx, snapshotLoadedMsg{mergedCfg: &amconfig.Config{}, resolved: resolved})
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	spans := e.spansNamed("agent_marketplace.tui.config_resources")
	if len(spans) != 2 {
		t.Fatalf("resource spans = %d, want 2", len(spans))
	}
	seen := make(map[string]bool, len(resolved))
	for _, span := range spans {
		if got := attrValue(span.Attributes, "chunk.count"); got != "2" {
			t.Errorf("chunk.count = %q, want 2", got)
		}
		var names []string
		if err := json.Unmarshal([]byte(attrValue(span.Attributes, "resource.names")), &names); err != nil {
			t.Fatalf("decode resource.names: %v", err)
		}
		for _, name := range names {
			seen[name] = true
		}
	}
	if len(seen) != len(resolved) {
		t.Fatalf("emitted resource names = %d, want %d", len(seen), len(resolved))
	}
}

func TestRecordControlledErrorSpanEmits(t *testing.T) {
	e := newTestSpanExporter(t)
	ctx, tel := setupTuiTelemetry(t, e)
	defer func() { _ = tel.Shutdown(context.Background()) }()

	recordControlledErrorSpan(ctx, "sync", errors.New("boom"))

	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	s := e.findSpan("agent_marketplace.tui.controlled_error")
	if s == nil {
		t.Fatal("expected agent_marketplace.tui.controlled_error span")
	}
	if got := attrValue(s.Attributes, "action"); got != "sync" {
		t.Fatalf("expected action attribute %q, got %q", "sync", got)
	}
}
