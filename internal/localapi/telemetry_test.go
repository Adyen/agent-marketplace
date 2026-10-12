package localapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	amconfig "github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/localapi/testsupport"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"
)

func newLocalTelemetryCollector(t *testing.T) *testsupport.Collector {
	t.Helper()
	collector := testsupport.NewCollector()
	t.Cleanup(collector.Close)
	return collector
}

func localActionSpans(collector *testsupport.Collector) ([]testsupport.Span, error) {
	capture, err := collector.RawSnapshot()
	if err != nil {
		return nil, err
	}
	var spans []testsupport.Span
	for _, span := range capture.Spans {
		if span.Name == "agent_marketplace.gui.action" {
			spans = append(spans, span)
		}
	}
	return spans, nil
}

type localTelemetryHarness struct {
	collector      *testsupport.Collector
	runtime        *telemetry.Telemetry
	state          telemetry.TelemetryState
	commandContext context.Context
	rootSpan       telemetry.Span
	startedAt      time.Time
	capturePath    string
}

func newLocalTelemetryHarness(t *testing.T, home, level, capturePath string) (context.Context, *localTelemetryHarness, error) {
	t.Helper()
	if level != tmcfg.LevelOff && level != tmcfg.LevelBasic && level != tmcfg.LevelDetailed {
		return nil, nil, errors.New("unsupported telemetry test mode")
	}
	for _, key := range []string{telemetry.EnvEndpoint, telemetry.EnvLevel, telemetry.EnvCommunityReporting} {
		t.Setenv(key, "")
	}
	collector := newLocalTelemetryCollector(t)
	path := filepath.Join(home, ".agents", "config.yaml")
	current, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	body := string(current)
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	body += "telemetry:\n  level: " + level + "\n  endpoint: " + collector.URL() + "\n  community_reporting: false\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return nil, nil, err
	}
	startedAt := time.Now()
	state, err := telemetry.LoadAndResolve(context.Background())
	if err != nil {
		return nil, nil, err
	}
	harness := &localTelemetryHarness{collector: collector, state: state, startedAt: startedAt, capturePath: capturePath}
	ctx := context.Background()
	if state.EffectiveLevel == tmcfg.LevelBasic || state.EffectiveLevel == tmcfg.LevelDetailed {
		harness.runtime, err = telemetry.New(ctx, state, slog.New(slog.NewTextHandler(io.Discard, nil)), telemetry.WithVersion("dev"))
		if err != nil {
			return nil, nil, err
		}
		if state.EffectiveLevel == tmcfg.LevelDetailed {
			ctx, harness.rootSpan = harness.runtime.StartSpan(ctx, "agent_marketplace.command", map[string]string{
				string(tmcfg.ResourceAttrCommandName): "gui",
				"command.surface":                     "gui",
			})
		}
		ctx = telemetry.NewContext(ctx, harness.runtime, "gui", startedAt, state.HarnessesString(), state.Managed, harness.rootSpan, false)
		harness.commandContext = ctx
	}
	return ctx, harness, nil
}

func (h *localTelemetryHarness) finish(t *testing.T) {
	t.Helper()
	if h.runtime != nil {
		h.rootSpan.End()
		attributes := map[string]string{
			string(tmcfg.ResourceAttrCommandName): "gui",
			string(tmcfg.ResourceAttrHarnesses):   h.state.HarnessesString(),
			string(tmcfg.ResourceAttrManaged):     strconv.FormatBool(h.state.Managed),
			string(tmcfg.ResourceAttrOutcomeCode): "success",
		}
		h.runtime.CommandCounter().Add(h.commandContext, 1, attributes)
		h.runtime.CommandDuration().Record(h.commandContext, time.Since(h.startedAt).Milliseconds(), attributes)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		if err := h.runtime.Shutdown(shutdownCtx); err != nil {
			t.Error("test telemetry shutdown failed")
		}
	}
	if h.capturePath != "" {
		if err := h.collector.WriteCapture(h.capturePath); err != nil {
			t.Error("write sanitized telemetry capture")
		}
	}
}

func newLocalTelemetryAPI(t *testing.T, userOnly bool) (*API, *telemetry.Telemetry, *testsupport.Collector, string) {
	t.Helper()
	home, repo := newUserAPIFixture(t, true)
	collector := newLocalTelemetryCollector(t)
	user := amconfig.Settings{Telemetry: &tmcfg.TelemetryConfig{Level: tmcfg.LevelDetailed, Endpoint: collector.URL()}}
	state, err := telemetry.ResolveTelemetryConfig(amconfig.Settings{}, user, "", "")
	if err != nil {
		t.Fatal("resolve test telemetry state")
	}
	tel, err := telemetry.New(context.Background(), state, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal("create test telemetry runtime")
	}
	ctx := telemetry.NewContext(context.Background(), tel, "gui", time.Now(), state.HarnessesString(), state.Managed, telemetry.Span{}, false)
	var api *API
	if userOnly {
		api, err = NewUser(ctx, home, session.DependencyLifecycle{}, nil)
	} else {
		var workspace pipeline.Workspace
		workspace, err = pipeline.NewWorkspace(repo, repo)
		if err == nil {
			api, err = New(ctx, workspace, home, session.DependencyLifecycle{}, nil)
		}
	}
	if err != nil {
		_ = tel.Shutdown(context.Background())
		t.Fatal("create telemetry test API")
	}
	t.Cleanup(func() {
		api.Close()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tel.Shutdown(shutdownCtx)
	})
	return api, tel, collector, home
}

func sendTelemetryRequest(t *testing.T, api *API, method, target, body string, credential, origin bool) (int, []byte) {
	t.Helper()
	var input io.Reader
	if body != "" {
		input = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, input)
	if credential {
		req.Header.Set(tokenHeader, api.Credential())
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if origin {
		req.Header.Set("Origin", "https://foreign.example")
	}
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, req)
	return recorder.Code, recorder.Body.Bytes()
}

// spec: gui-shell/gui-actions-use-existing-telemetry-policy
func TestTelemetryEventRouteAcceptsOnlyCanonicalCurrentEvents(t *testing.T) {
	api, tel, collector, home := newLocalTelemetryAPI(t, true)
	userConfigPath := filepath.Join(home, ".agents", "config.yaml")
	before, err := os.ReadFile(userConfigPath)
	if err != nil {
		t.Fatal("read user root before event requests")
	}

	status, body := sendTelemetryRequest(t, api, http.MethodPost, "/api/snapshot", "", true, false)
	if status != http.StatusOK {
		t.Fatalf("snapshot status = %d, want 200", status)
	}
	var snapshot struct {
		Rows []rowDTO `json:"rows"`
	}
	if err := json.Unmarshal(body, &snapshot); err != nil || len(snapshot.Rows) == 0 {
		t.Fatal("authoritative snapshot has no test row")
	}
	row := snapshot.Rows[0]
	browse, err := json.Marshal(map[string]string{"action": "browse", "rowId": row.ID})
	if err != nil {
		t.Fatal("encode browse event")
	}
	if status, _ := sendTelemetryRequest(t, api, http.MethodPost, "/api/telemetry", string(browse), true, false); status != http.StatusNoContent {
		t.Fatalf("browse event status = %d, want 204", status)
	}
	if status, _ := sendTelemetryRequest(t, api, http.MethodPost, "/api/telemetry", `{"action":"modal_outcome","modal":"sync+quit","outcome":"cancelled"}`, true, false); status != http.StatusNoContent {
		t.Fatalf("modal event status = %d, want 204", status)
	}

	invalidBodies := []string{
		`{}`,
		`{"action":"browse"}`,
		`{"action":"browse","rowId":null}`,
		`{"action":"browse","rowId":4}`,
		`{"action":"browse","rowId":"x","resource.kind":"skill"}`,
		`{"action":"browse","rowId":"x","action":"browse"}`,
		`{"Action":"browse","rowId":"x"}`,
		`{"action":"browse","rowId":"x"} {}`,
		`{"action":"modal_outcome","modal":"sync+quit"}`,
		`{"action":"modal_outcome","modal":"sync+quit","outcome":null}`,
		`{"action":"modal_outcome","modal":"sync","outcome":"cancelled"}`,
		`{"action":"modal_outcome","modal":"sync+quit","outcome":"cancelled","rowId":"x"}`,
	}
	for _, invalid := range invalidBodies {
		if status, _ := sendTelemetryRequest(t, api, http.MethodPost, "/api/telemetry", invalid, true, false); status != http.StatusBadRequest {
			t.Fatalf("invalid telemetry event status = %d, want 400", status)
		}
	}
	if status, _ := sendTelemetryRequest(t, api, http.MethodPost, "/api/telemetry?extra=1", `{"action":"modal_outcome","modal":"sync+quit","outcome":"cancelled"}`, true, false); status != http.StatusBadRequest {
		t.Fatalf("query telemetry event status = %d, want 400", status)
	}
	if status, _ := sendTelemetryRequest(t, api, http.MethodPost, "/api/telemetry", string(browse), false, false); status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated telemetry status = %d, want 401", status)
	}
	if status, _ := sendTelemetryRequest(t, api, http.MethodPost, "/api/telemetry", string(browse), true, true); status != http.StatusForbidden {
		t.Fatalf("foreign-origin telemetry status = %d, want 403", status)
	}
	if status, _ := sendTelemetryRequest(t, api, http.MethodGet, "/api/telemetry", "", true, false); status != http.StatusMethodNotAllowed {
		t.Fatalf("GET telemetry status = %d, want 405", status)
	}
	if status, _ := sendTelemetryRequest(t, api, http.MethodPost, "/api/telemetry", `{"action":"browse","rowId":"unknown-row"}`, true, false); status != http.StatusBadRequest {
		t.Fatalf("unknown row telemetry status = %d, want 400", status)
	}

	api.mu.Lock()
	staleSnapshot := *api.current
	staleSnapshot.Rows = []rowDTO{}
	api.current = &staleSnapshot
	api.mu.Unlock()
	if status, _ := sendTelemetryRequest(t, api, http.MethodPost, "/api/telemetry", string(browse), true, false); status != http.StatusBadRequest {
		t.Fatalf("stale row telemetry status = %d, want 400", status)
	}
	api.mu.Lock()
	ambiguousSnapshot := *api.current
	ambiguousSnapshot.Rows = []rowDTO{row, row}
	api.current = &ambiguousSnapshot
	api.mu.Unlock()
	if status, _ := sendTelemetryRequest(t, api, http.MethodPost, "/api/telemetry", string(browse), true, false); status != http.StatusBadRequest {
		t.Fatalf("ambiguous row telemetry status = %d, want 400", status)
	}
	if after, err := os.ReadFile(userConfigPath); err != nil || !bytes.Equal(after, before) {
		t.Fatal("telemetry requests changed user configuration bytes")
	}

	api.Close()
	if status, _ := sendTelemetryRequest(t, api, http.MethodPost, "/api/telemetry", string(browse), true, false); status != http.StatusServiceUnavailable {
		t.Fatalf("closed telemetry status = %d, want 503", status)
	}
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatal("shutdown telemetry")
	}
	spans, err := localActionSpans(collector)
	if err != nil {
		t.Fatal("private collector failed to decode spans")
	}
	if len(spans) != 2 {
		t.Fatalf("GUI action spans = %d, want exactly browse and modal cancellation", len(spans))
	}
	want := map[string]map[string]string{
		"browse":        {"action": "browse", "command.surface": "gui", "view": "detail", "resource.kind": row.Kind},
		"modal_outcome": {"action": "modal_outcome", "command.surface": "gui", "modal": "sync+quit", "outcome": "cancelled"},
	}
	for _, span := range spans {
		action := span.Attributes["action"]
		if !reflect.DeepEqual(span.Attributes, want[action]) {
			t.Fatalf("%s event attributes differ from the canonical map", action)
		}
		if _, ok := span.Attributes["rowId"]; ok {
			t.Fatalf("%s event included an opaque row id", action)
		}
		if _, ok := span.Attributes["resource.name"]; ok {
			t.Fatalf("%s event included a Resource name", action)
		}
	}
}

// spec: gui-shell/gui-actions-use-existing-telemetry-policy
func TestTelemetryEventRouteIsNotSupportedInWorkspaceMode(t *testing.T) {
	api, tel, collector, _ := newLocalTelemetryAPI(t, false)
	status, body := sendTelemetryRequest(t, api, http.MethodPost, "/api/telemetry", `{"action":"modal_outcome","modal":"sync+quit","outcome":"cancelled"}`, true, false)
	if status != http.StatusBadRequest {
		t.Fatalf("workspace telemetry status = %d, want 400", status)
	}
	var failure map[string]string
	if err := json.Unmarshal(body, &failure); err != nil || failure["code"] != codeNotSupported {
		t.Fatal("workspace telemetry response did not report localapi.not_supported")
	}
	api.Close()
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatal("shutdown telemetry")
	}
	spans, err := localActionSpans(collector)
	if err != nil || len(spans) != 0 {
		t.Fatal("unsupported telemetry event was exported")
	}
}
