package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/localapi/testsupport"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"
)

type guiTelemetryCollector struct {
	*testsupport.Collector
}

func newGUITelemetryCollector(t *testing.T) *guiTelemetryCollector {
	t.Helper()
	collector := testsupport.NewCollector()
	t.Cleanup(collector.Close)
	return &guiTelemetryCollector{Collector: collector}
}

func (c *guiTelemetryCollector) snapshot() ([]testsupport.Span, []testsupport.Metric, map[string]int, error) {
	capture, err := c.RawSnapshot()
	requests := map[string]int{
		"/v1/traces":  capture.Requests.Traces,
		"/v1/metrics": capture.Requests.Metrics,
	}
	return capture.Spans, capture.Metrics, requests, err
}

func (c *guiTelemetryCollector) writeEvidence(t *testing.T, name string) {
	t.Helper()
	directory := os.Getenv("AM_WORK28_TELEMETRY_EVIDENCE_DIR")
	if directory == "" {
		return
	}
	if err := c.WriteCapture(filepath.Join(directory, name)); err != nil {
		t.Error("write sanitized telemetry evidence")
	}
}

func clearGUITelemetryEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{telemetry.EnvEndpoint, telemetry.EnvLevel, telemetry.EnvCommunityReporting} {
		t.Setenv(key, "")
	}
}

func setGUIProjectEndpoint(t *testing.T, endpoint string) {
	t.Helper()
	oldEndpoint, oldRetention := telemetry.DefaultProjectEndpoint, telemetry.DefaultProjectRetentionURL
	telemetry.DefaultProjectEndpoint = endpoint
	telemetry.DefaultProjectRetentionURL = ""
	t.Cleanup(func() {
		telemetry.DefaultProjectEndpoint = oldEndpoint
		telemetry.DefaultProjectRetentionURL = oldRetention
	})
}

func appendGUIUserTelemetry(t *testing.T, home, level, endpoint string, community *bool) {
	t.Helper()
	path := filepath.Join(home, ".agents", "config.yaml")
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("read user configuration")
	}
	var block strings.Builder
	if len(current) > 0 && current[len(current)-1] != '\n' {
		block.WriteByte('\n')
	}
	block.WriteString("telemetry:\n  level: ")
	block.WriteString(level)
	block.WriteByte('\n')
	if endpoint != "" {
		block.WriteString("  endpoint: ")
		block.WriteString(endpoint)
		block.WriteByte('\n')
	}
	if community != nil {
		block.WriteString("  community_reporting: ")
		block.WriteString(strconv.FormatBool(*community))
		block.WriteByte('\n')
	}
	writeFile(t, path, string(current)+block.String())
}

func setOrganizationTelemetryConfig(t *testing.T, level, endpoint string, community *bool) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "org.yaml")
	var content strings.Builder
	content.WriteString("apiVersion: agent-marketplace/v4\ntelemetry:\n  level: ")
	content.WriteString(level)
	content.WriteByte('\n')
	if endpoint != "" {
		content.WriteString("  endpoint: ")
		content.WriteString(endpoint)
		content.WriteByte('\n')
	}
	if community != nil {
		content.WriteString("  community_reporting: ")
		content.WriteString(strconv.FormatBool(*community))
		content.WriteByte('\n')
	}
	writeFile(t, path, content.String())
	config.SetTestOrganizationConfigPath(t, path)
}

func requestGUIAPI(t *testing.T, app *App, method, path, body string, withToken bool) (int, []byte) {
	t.Helper()
	var requestBody io.Reader
	if body != "" {
		requestBody = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, requestBody)
	if withToken {
		req.Header.Set("X-Agent-Marketplace-Token", app.Bootstrap().Credential)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	app.handler().ServeHTTP(recorder, req)
	return recorder.Code, recorder.Body.Bytes()
}

func readGUIRows(t *testing.T, app *App) []struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
} {
	t.Helper()
	status, body := requestGUIAPI(t, app, http.MethodPost, "/api/snapshot", "", true)
	if status != http.StatusOK {
		t.Fatalf("snapshot status = %d, want 200", status)
	}
	var response struct {
		Stage string `json:"stage"`
		Rows  []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Kind string `json:"kind"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode snapshot response: %v", err)
	}
	if response.Stage != "final" {
		t.Fatalf("snapshot stage = %q, want final", response.Stage)
	}
	return response.Rows
}

func startGUIAction(t *testing.T, app *App, action string) uint64 {
	t.Helper()
	status, body := requestGUIAPI(t, app, http.MethodPost, "/api/actions", `{"action":"`+action+`"}`, true)
	if status != http.StatusAccepted {
		t.Fatalf("%s status = %d, want 202", action, status)
	}
	var response struct {
		Operation struct {
			ID   uint64 `json:"id"`
			Kind string `json:"kind"`
		} `json:"operation"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode %s response", action)
	}
	if response.Operation.Kind != action || response.Operation.ID == 0 {
		t.Fatalf("%s operation identity is invalid", action)
	}
	return response.Operation.ID
}

func waitGUIAction(t *testing.T, app *App, id uint64, kind string) {
	t.Helper()
	server := httptest.NewServer(app.handler())
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/events", nil)
	if err != nil {
		t.Fatal("create event request")
	}
	req.Header.Set("X-Agent-Marketplace-Token", app.Bootstrap().Credential)
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal("read event stream")
	}
	defer func() { _ = resp.Body.Close() }()
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event struct {
			Type    string `json:"type"`
			Payload struct {
				Stage            string `json:"stage"`
				SettledOperation *struct {
					ID   uint64 `json:"id"`
					Kind string `json:"kind"`
				} `json:"settledOperation"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event); err != nil {
			continue
		}
		if event.Type == "snapshot" && event.Payload.Stage == "final" && event.Payload.SettledOperation != nil && event.Payload.SettledOperation.ID == id && event.Payload.SettledOperation.Kind == kind {
			return
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read event stream: %v", err)
	}
	t.Fatalf("%s operation %d did not settle", kind, id)
}

func browseOneGUIResource(t *testing.T, app *App) {
	t.Helper()
	rows := readGUIRows(t, app)
	if len(rows) == 0 {
		t.Fatal("fixture has no Resource to browse")
	}
	body, err := json.Marshal(map[string]string{"action": "browse", "rowId": rows[0].ID})
	if err != nil {
		t.Fatal("encode browse event")
	}
	if status, _ := requestGUIAPI(t, app, http.MethodPost, "/api/telemetry", string(body), true); status != http.StatusNoContent {
		t.Fatalf("browse telemetry status = %d, want 204", status)
	}
}

func commandMetricAttributes(harnesses, managed string) map[string]string {
	return map[string]string{
		string(tmcfg.ResourceAttrCommandName): "gui",
		string(tmcfg.ResourceAttrHarnesses):   harnesses,
		string(tmcfg.ResourceAttrManaged):     managed,
		string(tmcfg.ResourceAttrOutcomeCode): "success",
	}
}

func assertGUICommandMetrics(t *testing.T, points []testsupport.Metric, managed string) {
	t.Helper()
	var counter, duration []testsupport.Metric
	for _, point := range points {
		switch point.Name {
		case string(tmcfg.CommandCounterName):
			counter = append(counter, point)
		case string(tmcfg.CommandDurationName):
			duration = append(duration, point)
		}
	}
	if len(counter) != 1 || counter[0].Value != 1 {
		t.Fatal("GUI command counter did not record one invocation")
	}
	if len(duration) != 1 || duration[0].Count != 1 {
		t.Fatal("GUI command duration did not record one observation")
	}
	want := commandMetricAttributes("devin", managed)
	if !reflect.DeepEqual(counter[0].Attributes, want) || !reflect.DeepEqual(duration[0].Attributes, want) {
		t.Fatal("command metric labels differ from the bounded GUI map")
	}
}

func closeGUIAppConcurrently(app *App, count int) {
	var group sync.WaitGroup
	for i := 0; i < count; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			app.close()
		}()
	}
	group.Wait()
}

// spec: gui-shell/gui-actions-use-existing-telemetry-policy
func TestAppTelemetryInteractiveActionCanonicalizesReservedAttributes(t *testing.T) {
	clearGUITelemetryEnvironment(t)
	setGUIProjectEndpoint(t, "")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	collector := newGUITelemetryCollector(t)
	user := config.Settings{
		Harnesses: []string{"devin"},
		Telemetry: &tmcfg.TelemetryConfig{Level: tmcfg.LevelDetailed, Endpoint: collector.URL()},
	}
	state, err := telemetry.ResolveTelemetryConfig(config.Settings{}, user, "", "")
	if err != nil {
		t.Fatal("resolve telemetry test state")
	}
	runtime, err := telemetry.New(context.Background(), state, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal("create telemetry test runtime")
	}
	ctx, rootSpan := runtime.StartSpan(context.Background(), "agent_marketplace.command", map[string]string{
		"command.name":    "gui",
		"command.surface": "gui",
	})
	ctx = telemetry.NewContext(ctx, runtime, "gui", time.Now(), state.HarnessesString(), state.Managed, rootSpan, false)
	attributes := map[string]string{"action": "caller-value", "command.surface": "caller-value", "view": "detail", "resource.kind": "skill"}
	telemetry.RecordInteractiveAction(ctx, "gui", "browse", attributes)
	telemetry.RecordInteractiveAction(ctx, "tui", "sync", attributes)
	telemetry.RecordInteractiveAction(ctx, "other", "ignored", attributes)
	rootSpan.End()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := runtime.Shutdown(shutdownCtx); err != nil {
		t.Fatal("shutdown telemetry test runtime")
	}
	if attributes["action"] != "caller-value" || attributes["command.surface"] != "caller-value" {
		t.Fatal("interactive action mutated caller attributes")
	}

	spans, _, _, err := collector.snapshot()
	if err != nil {
		t.Fatal("private collector could not decode OTLP")
	}
	got := map[string]map[string]string{}
	actionSpans := 0
	for _, span := range spans {
		if strings.HasSuffix(span.Name, ".action") {
			actionSpans++
		}
		if span.Name == "agent_marketplace.gui.action" || span.Name == "agent_marketplace.tui.action" {
			got[span.Name] = span.Attributes
		}
	}
	if actionSpans != 2 {
		t.Fatalf("interactive action spans = %d, want GUI and TUI only", actionSpans)
	}
	if !reflect.DeepEqual(got["agent_marketplace.gui.action"], map[string]string{"action": "browse", "command.surface": "gui", "view": "detail", "resource.kind": "skill"}) {
		t.Fatal("GUI interactive action did not canonicalize reserved attributes")
	}
	if !reflect.DeepEqual(got["agent_marketplace.tui.action"], map[string]string{"action": "sync", "command.surface": "tui", "view": "detail", "resource.kind": "skill"}) {
		t.Fatal("TUI interactive action did not canonicalize reserved attributes")
	}
}

// spec: gui-shell/gui-actions-use-existing-telemetry-policy
func TestAppTelemetryDetailedActionsAndSingleClose(t *testing.T) {
	clearGUITelemetryEnvironment(t)
	setGUIProjectEndpoint(t, "")
	collector := newGUITelemetryCollector(t)
	home, _ := newHostFixture(t)
	community := false
	appendGUIUserTelemetry(t, home, tmcfg.LevelDetailed, collector.URL(), &community)

	syncStarted := make(chan struct{}, 1)
	syncRelease := make(chan struct{})
	var releaseOnce sync.Once
	releaseSync := func() { releaseOnce.Do(func() { close(syncRelease) }) }
	lifecycle := session.DependencyLifecycle{
		Sync: func(ctx context.Context, _ pipeline.Workspace) ([]error, error) {
			syncStarted <- struct{}{}
			select {
			case <-syncRelease:
				return nil, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		Update: func(context.Context, pipeline.Workspace) ([]error, error) { return nil, nil },
	}
	app, err := NewApp(context.Background(), lifecycle)
	if err != nil {
		t.Fatal("create GUI app")
	}
	t.Cleanup(app.close)
	t.Cleanup(releaseSync)

	rows := readGUIRows(t, app)
	var selected *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Kind string `json:"kind"`
	}
	for i := range rows {
		if rows[i].Name == "solo" {
			selected = &rows[i]
			break
		}
	}
	if selected == nil {
		t.Fatal("fixture Resource was not present")
	}
	credential := app.Bootstrap().Credential
	browse, err := json.Marshal(map[string]string{"action": "browse", "rowId": selected.ID})
	if err != nil {
		t.Fatal("encode browse event")
	}
	if status, _ := requestGUIAPI(t, app, http.MethodPost, "/api/telemetry", string(browse), true); status != http.StatusNoContent {
		t.Fatalf("browse telemetry status = %d, want 204", status)
	}

	syncID := startGUIAction(t, app, "sync")
	select {
	case <-syncStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("sync lifecycle did not start")
	}
	if status, _ := requestGUIAPI(t, app, http.MethodPost, "/api/actions", `{"action":"update"}`, true); status != http.StatusConflict {
		t.Fatalf("busy update status = %d, want 409", status)
	}
	releaseSync()
	waitGUIAction(t, app, syncID, "sync")

	updateID := startGUIAction(t, app, "update")
	waitGUIAction(t, app, updateID, "update")
	closeGUIAppConcurrently(app, 4)

	spans, metrics, requests, err := collector.snapshot()
	if err != nil {
		t.Fatal("private collector could not decode OTLP")
	}
	if requests["/v1/traces"] == 0 || requests["/v1/metrics"] == 0 {
		t.Fatalf("detailed telemetry routes missing: %#v", requests)
	}
	var commandSpans, actionSpans []testsupport.Span
	for _, span := range spans {
		switch span.Name {
		case "agent_marketplace.command":
			commandSpans = append(commandSpans, span)
		case "agent_marketplace.gui.action":
			actionSpans = append(actionSpans, span)
		}
	}
	if len(commandSpans) != 1 {
		t.Fatalf("GUI command spans = %d, want one", len(commandSpans))
	}
	if !reflect.DeepEqual(commandSpans[0].Attributes, map[string]string{"command.name": "gui", "command.surface": "gui"}) {
		t.Fatal("GUI command span attributes differ from the settled map")
	}
	if len(actionSpans) != 3 {
		t.Fatalf("GUI action spans = %d, want browse, sync, and update", len(actionSpans))
	}
	wantActions := map[string]map[string]string{
		"browse": {"action": "browse", "command.surface": "gui", "view": "detail", "resource.kind": "skill"},
		"sync":   {"action": "sync", "command.surface": "gui"},
		"update": {"action": "update", "command.surface": "gui"},
	}
	for _, span := range actionSpans {
		action := span.Attributes["action"]
		if !reflect.DeepEqual(span.Attributes, wantActions[action]) {
			t.Fatalf("GUI %s action attributes differ from the settled map", action)
		}
		if _, ok := span.Attributes["resource.name"]; ok {
			t.Fatalf("GUI %s action contains a Resource name attribute", action)
		}
		if _, ok := span.Attributes["rowId"]; ok {
			t.Fatalf("GUI %s action contains a row id attribute", action)
		}
		for _, value := range span.Attributes {
			if value == selected.Name || value == selected.ID {
				t.Fatalf("GUI %s action contains a raw Resource name or ID", action)
			}
		}
	}
	assertGUICommandMetrics(t, metrics, "false")
	if collector.ContainsPrivate(credential, selected.ID) {
		t.Fatal("full OTLP payload contains a launch credential or opaque row identity")
	}
	collector.writeEvidence(t, "host-detailed.json")
}

// spec: gui-shell/gui-actions-use-existing-telemetry-policy
func TestAppTelemetryBasicCommandMetricMap(t *testing.T) {
	clearGUITelemetryEnvironment(t)
	setGUIProjectEndpoint(t, "")
	collector := newGUITelemetryCollector(t)
	home, _ := newHostFixture(t)
	appendGUIUserTelemetry(t, home, tmcfg.LevelBasic, collector.URL(), nil)
	app, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal("create GUI app")
	}
	t.Cleanup(app.close)
	browseOneGUIResource(t, app)
	app.close()

	spans, metrics, requests, err := collector.snapshot()
	if err != nil {
		t.Fatal("private collector could not decode OTLP")
	}
	if requests["/v1/traces"] != 0 || len(spans) != 0 {
		t.Fatal("basic telemetry exported traces")
	}
	if len(metrics) != 2 {
		t.Fatalf("basic telemetry metrics = %d, want command count and duration", len(metrics))
	}
	for _, metric := range metrics {
		if metric.Name != string(tmcfg.CommandCounterName) && metric.Name != string(tmcfg.CommandDurationName) {
			t.Fatalf("unexpected basic metric %q", metric.Name)
		}
		if len(metric.Attributes) != 4 {
			t.Fatal("basic command metric has labels outside the settled four-field map")
		}
	}
	assertGUICommandMetrics(t, metrics, "false")
	collector.writeEvidence(t, "host-basic.json")
}

// spec: gui-shell/gui-actions-use-existing-telemetry-policy
func TestAppTelemetryOffDoesNotCreateStateOrBlockTheGUI(t *testing.T) {
	clearGUITelemetryEnvironment(t)
	setGUIProjectEndpoint(t, "")
	collector := newGUITelemetryCollector(t)
	home, _ := newHostFixture(t)
	appendGUIUserTelemetry(t, home, tmcfg.LevelOff, collector.URL(), nil)
	app, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal("create GUI app")
	}
	t.Cleanup(app.close)
	browseOneGUIResource(t, app)
	app.close()

	stateRoot := filepath.Join(os.Getenv("XDG_STATE_HOME"), "agent-marketplace", "telemetry")
	for _, path := range []string{filepath.Join(stateRoot, "installation-id"), filepath.Join(stateRoot, "queue")} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("telemetry off created state at %s", filepath.Base(path))
		}
	}
	_, _, requests, err := collector.snapshot()
	if err != nil || requests["/v1/traces"] != 0 || requests["/v1/metrics"] != 0 {
		t.Fatal("telemetry off contacted an exporter")
	}
	collector.writeEvidence(t, "host-off.json")
}

// spec: gui-shell/gui-actions-use-existing-telemetry-policy
func TestAppTelemetryPolicyIsResolvedPerLaunch(t *testing.T) {
	clearGUITelemetryEnvironment(t)
	collector := newGUITelemetryCollector(t)
	setGUIProjectEndpoint(t, collector.URL())
	home, _ := newHostFixture(t)
	userConfigPath := filepath.Join(home, ".agents", "config.yaml")
	if err := os.Remove(userConfigPath); err != nil {
		t.Fatal("prepare first-run user configuration")
	}

	first, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal("create first-run GUI app")
	}
	t.Cleanup(first.close)
	first.close()
	stateRoot := filepath.Join(os.Getenv("XDG_STATE_HOME"), "agent-marketplace", "telemetry")
	if _, err := os.Stat(filepath.Join(stateRoot, "installation-id")); !os.IsNotExist(err) {
		t.Fatal("first-run off policy created an installation ID")
	}

	writeFile(t, userConfigPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\ntelemetry:\n  level: basic\n")
	second, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal("create GUI app after telemetry opt-in")
	}
	t.Cleanup(second.close)
	second.close()

	spans, metrics, requests, err := collector.snapshot()
	if err != nil {
		t.Fatal("private collector could not decode OTLP")
	}
	if requests["/v1/traces"] != 0 || len(spans) != 0 {
		t.Fatal("basic telemetry exported traces after the next launch")
	}
	assertGUICommandMetrics(t, metrics, "false")
	collector.writeEvidence(t, "host-per-launch.json")
}

// spec: gui-shell/gui-actions-use-existing-telemetry-policy
func TestAppTelemetryOrganizationPolicyOverridesUserAndEnvironment(t *testing.T) {
	t.Run("off defeats detailed user and environment", func(t *testing.T) {
		clearGUITelemetryEnvironment(t)
		setGUIProjectEndpoint(t, "")
		collector := newGUITelemetryCollector(t)
		home, _ := newHostFixture(t)
		appendGUIUserTelemetry(t, home, tmcfg.LevelDetailed, collector.URL(), nil)
		setOrganizationTelemetryConfig(t, tmcfg.LevelOff, "", nil)
		t.Setenv(telemetry.EnvLevel, tmcfg.LevelDetailed)
		t.Setenv(telemetry.EnvEndpoint, collector.URL())
		app, err := NewApp(context.Background(), session.DependencyLifecycle{})
		if err != nil {
			t.Fatal("create GUI app")
		}
		t.Cleanup(app.close)
		if status, _ := requestGUIAPI(t, app, http.MethodPost, "/api/snapshot", "", true); status != http.StatusOK {
			t.Fatalf("snapshot status = %d with organization telemetry off, want 200", status)
		}
		browseOneGUIResource(t, app)
		app.close()
		stateRoot := filepath.Join(os.Getenv("XDG_STATE_HOME"), "agent-marketplace", "telemetry")
		if _, err := os.Stat(filepath.Join(stateRoot, "installation-id")); !os.IsNotExist(err) {
			t.Fatal("organization off created an installation ID")
		}
		_, _, requests, err := collector.snapshot()
		if err != nil || requests["/v1/traces"] != 0 || requests["/v1/metrics"] != 0 {
			t.Fatal("organization off emitted telemetry")
		}
		collector.writeEvidence(t, "organization-off.json")
	})

	t.Run("basic organization level defeats detailed user and environment", func(t *testing.T) {
		clearGUITelemetryEnvironment(t)
		setGUIProjectEndpoint(t, "")
		organization := newGUITelemetryCollector(t)
		user := newGUITelemetryCollector(t)
		environment := newGUITelemetryCollector(t)
		home, _ := newHostFixture(t)
		appendGUIUserTelemetry(t, home, tmcfg.LevelDetailed, user.URL(), nil)
		community := false
		setOrganizationTelemetryConfig(t, tmcfg.LevelBasic, organization.URL(), &community)
		t.Setenv(telemetry.EnvLevel, tmcfg.LevelDetailed)
		t.Setenv(telemetry.EnvEndpoint, environment.URL())
		app, err := NewApp(context.Background(), session.DependencyLifecycle{})
		if err != nil {
			t.Fatal("create GUI app")
		}
		t.Cleanup(app.close)
		browseOneGUIResource(t, app)
		app.close()

		spans, metrics, _, err := organization.snapshot()
		if err != nil {
			t.Fatal("organization collector could not decode OTLP")
		}
		if len(spans) != 0 {
			t.Fatal("basic organization telemetry exported traces")
		}
		assertGUICommandMetrics(t, metrics, "true")
		organization.writeEvidence(t, "organization-basic.json")
		for _, collector := range []*guiTelemetryCollector{user, environment} {
			_, _, requests, err := collector.snapshot()
			if err != nil || requests["/v1/traces"] != 0 || requests["/v1/metrics"] != 0 {
				t.Fatal("organization endpoint policy was overridden by user or environment configuration")
			}
		}
	})
}

// spec: gui-shell/gui-actions-use-existing-telemetry-policy
func TestAppTelemetryCommunityRouteIsIndependentAndOptIn(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		enabled := enabled
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		t.Run(name, func(t *testing.T) {
			clearGUITelemetryEnvironment(t)
			primary := newGUITelemetryCollector(t)
			communityCollector := newGUITelemetryCollector(t)
			setGUIProjectEndpoint(t, communityCollector.URL())
			home, _ := newHostFixture(t)
			appendGUIUserTelemetry(t, home, tmcfg.LevelDetailed, primary.URL(), &enabled)
			app, err := NewApp(context.Background(), session.DependencyLifecycle{})
			if err != nil {
				t.Fatal("create GUI app")
			}
			t.Cleanup(app.close)
			app.close()

			primarySpans, primaryMetrics, primaryRequests, err := primary.snapshot()
			if err != nil {
				t.Fatal("primary collector could not decode OTLP")
			}
			if primaryRequests["/v1/traces"] == 0 || len(primarySpans) == 0 {
				t.Fatal("detailed primary route received no traces")
			}
			assertGUICommandMetrics(t, primaryMetrics, "false")

			communitySpans, communityMetrics, communityRequests, err := communityCollector.snapshot()
			if err != nil {
				t.Fatal("community collector could not decode OTLP")
			}
			if !enabled {
				if communityRequests["/v1/traces"] != 0 || communityRequests["/v1/metrics"] != 0 {
					t.Fatal("disabled community reporting contacted its collector")
				}
				primary.writeEvidence(t, "community-disabled-primary.json")
				communityCollector.writeEvidence(t, "community-disabled-community.json")
				return
			}
			if communityRequests["/v1/metrics"] == 0 || len(communityMetrics) != 2 {
				t.Fatalf("community metrics = %d, requests=%v, want command counter and duration", len(communityMetrics), communityRequests)
			}
			if communityRequests["/v1/traces"] != 0 || len(communitySpans) != 0 {
				t.Fatal("detailed traces were sent to the community collector")
			}
			for _, metric := range communityMetrics {
				if metric.Name != string(tmcfg.CommandCounterName) && metric.Name != string(tmcfg.CommandDurationName) {
					t.Fatalf("unexpected community metric %q", metric.Name)
				}
			}
			assertGUICommandMetrics(t, communityMetrics, "false")
			primary.writeEvidence(t, "community-enabled-primary.json")
			communityCollector.writeEvidence(t, "community-enabled-community.json")
		})
	}
}

// spec: gui-shell/gui-actions-use-existing-telemetry-policy
func TestAppTelemetryInvalidConfigurationIsNonfatalAndRedacted(t *testing.T) {
	clearGUITelemetryEnvironment(t)
	setGUIProjectEndpoint(t, "")
	collector := newGUITelemetryCollector(t)
	home, _ := newHostFixture(t)
	appendGUIUserTelemetry(t, home, "invalid", "http://user:secret@public.example.invalid/telemetry?token=must-not-log", nil)
	oldLogger := slog.Default()
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(oldLogger) })

	app, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal("invalid telemetry configuration stopped the GUI")
	}
	t.Cleanup(app.close)
	if status, _ := requestGUIAPI(t, app, http.MethodPost, "/api/snapshot", "", true); status != http.StatusOK {
		t.Fatalf("snapshot status = %d, want 200 when telemetry config is invalid", status)
	}
	app.close()
	stateRoot := filepath.Join(os.Getenv("XDG_STATE_HOME"), "agent-marketplace", "telemetry")
	for _, path := range []string{filepath.Join(stateRoot, "installation-id"), filepath.Join(stateRoot, "queue")} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("invalid telemetry configuration created telemetry state")
		}
	}
	if !strings.Contains(logs.String(), "GUI telemetry unavailable; continuing without telemetry") {
		t.Fatal("telemetry configuration failure did not use the stable warning")
	}
	if strings.Contains(logs.String(), "must-not-log") || strings.Contains(logs.String(), "user:secret") {
		t.Fatal("telemetry warning exposed endpoint contents")
	}
	_, _, requests, err := collector.snapshot()
	if err != nil || requests["/v1/traces"] != 0 || requests["/v1/metrics"] != 0 {
		t.Fatal("invalid telemetry configuration contacted an exporter")
	}
	collector.writeEvidence(t, "host-invalid-config.json")
}

func TestAppTelemetryConstructorFailureIsNonfatal(t *testing.T) {
	clearGUITelemetryEnvironment(t)
	setGUIProjectEndpoint(t, "")
	collector := newGUITelemetryCollector(t)
	home, _ := newHostFixture(t)
	appendGUIUserTelemetry(t, home, tmcfg.LevelBasic, collector.URL(), nil)
	stateHome := os.Getenv("XDG_STATE_HOME")
	if err := os.WriteFile(stateHome, []byte("not a directory"), 0o600); err != nil {
		t.Fatal("prepare telemetry state failure")
	}
	oldLogger := slog.Default()
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(oldLogger) })

	app, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal("telemetry constructor failure stopped the GUI")
	}
	t.Cleanup(app.close)
	if status, _ := requestGUIAPI(t, app, http.MethodPost, "/api/snapshot", "", true); status != http.StatusOK {
		t.Fatalf("snapshot status = %d, want 200 after telemetry construction failure", status)
	}
	app.close()
	if !strings.Contains(logs.String(), "GUI telemetry unavailable; continuing without telemetry") {
		t.Fatal("telemetry construction failure did not use the stable warning")
	}
	_, _, requests, err := collector.snapshot()
	if err != nil || requests["/v1/traces"] != 0 || requests["/v1/metrics"] != 0 {
		t.Fatal("failed telemetry construction contacted an exporter")
	}
	collector.writeEvidence(t, "host-constructor-failure.json")
}

type telemetryBuildGateState struct {
	built       chan struct{}
	release     chan struct{}
	shutdown    chan struct{}
	builtOnce   sync.Once
	releaseOnce sync.Once
	closeOnce   sync.Once
}

type telemetryBuildGate struct {
	delegate slog.Handler
	state    *telemetryBuildGateState
}

func (h *telemetryBuildGate) Enabled(ctx context.Context, level slog.Level) bool {
	return h.delegate.Enabled(ctx, level)
}

func (h *telemetryBuildGate) Handle(ctx context.Context, record slog.Record) error {
	if record.Message == "telemetry runtime built" {
		h.state.builtOnce.Do(func() { close(h.state.built) })
		<-h.state.release
	}
	if record.Message == "telemetry shutdown complete" {
		h.state.closeOnce.Do(func() { close(h.state.shutdown) })
	}
	return h.delegate.Handle(ctx, record)
}

func (h *telemetryBuildGate) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &telemetryBuildGate{delegate: h.delegate.WithAttrs(attrs), state: h.state}
}

func (h *telemetryBuildGate) WithGroup(name string) slog.Handler {
	return &telemetryBuildGate{delegate: h.delegate.WithGroup(name), state: h.state}
}

// spec: gui-shell/gui-actions-use-existing-telemetry-policy
func TestAppTelemetryAPIConstructorFailureShutsDownOwnedRuntime(t *testing.T) {
	clearGUITelemetryEnvironment(t)
	setGUIProjectEndpoint(t, "")
	collector := newGUITelemetryCollector(t)
	home, _ := newHostFixture(t)
	appendGUIUserTelemetry(t, home, tmcfg.LevelDetailed, collector.URL(), nil)
	otherHome := t.TempDir()
	oldLogger := slog.Default()
	state := &telemetryBuildGateState{built: make(chan struct{}), release: make(chan struct{}), shutdown: make(chan struct{})}
	gate := &telemetryBuildGate{delegate: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), state: state}
	slog.SetDefault(slog.New(gate))
	finished := make(chan struct {
		app *App
		err error
	}, 1)
	complete := false
	release := func() { state.releaseOnce.Do(func() { close(state.release) }) }
	t.Cleanup(func() {
		release()
		_ = os.Setenv("HOME", home)
		slog.SetDefault(oldLogger)
		if !complete {
			select {
			case <-finished:
			case <-time.After(5 * time.Second):
			}
		}
	})

	go func() {
		app, err := NewApp(context.Background(), session.DependencyLifecycle{})
		finished <- struct {
			app *App
			err error
		}{app: app, err: err}
	}()
	select {
	case <-state.built:
	case <-time.After(10 * time.Second):
		t.Fatal("telemetry runtime did not finish construction")
	}
	if err := os.Setenv("HOME", otherHome); err != nil {
		t.Fatal("change home after telemetry construction")
	}
	release()
	var result struct {
		app *App
		err error
	}
	select {
	case result = <-finished:
		complete = true
	case <-time.After(10 * time.Second):
		t.Fatal("GUI constructor did not unwind after API setup failure")
	}
	_ = os.Setenv("HOME", home)
	if result.err == nil || result.app != nil {
		if result.app != nil {
			result.app.close()
		}
		t.Fatal("API initialization unexpectedly succeeded with a different process home")
	}
	select {
	case <-state.shutdown:
	case <-time.After(5 * time.Second):
		t.Fatal("owned telemetry runtime was not shut down after API initialization failed")
	}
	spans, _, requests, err := collector.snapshot()
	if err != nil || requests["/v1/traces"] == 0 {
		t.Fatal("constructor cleanup did not flush the ended GUI command span")
	}
	var commandSpans int
	for _, span := range spans {
		if span.Name == "agent_marketplace.command" {
			commandSpans++
		}
	}
	if commandSpans != 1 {
		t.Fatalf("constructor cleanup command spans = %d, want one", commandSpans)
	}
	collector.writeEvidence(t, "host-api-init-failure.json")
}
