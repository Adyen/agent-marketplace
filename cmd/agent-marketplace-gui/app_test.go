package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/session"
)

const fixtureSkillBody = "---\nname: solo\ndescription: Solo skill\n---\n# Solo\n"

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newHostFixture(t *testing.T) (home, repo string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	t.Setenv("CODEX_HOME", t.TempDir())
	config.SetTestOrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))

	source := t.TempDir()
	writeFile(t, filepath.Join(source, "solo", "SKILL.md"), fixtureSkillBody)
	writeFile(t, filepath.Join(home, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  local: {directory: "+source+"}\nresources:\n  local: {all: true}\n")

	repo = t.TempDir()
	writeFile(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: [broken\n")
	writeFile(t, filepath.Join(repo, ".agents", "config.local.yaml"), "registries: [broken\n")
	t.Chdir(repo)
	return home, repo
}

type rowDTO struct {
	Name  string `json:"name"`
	Scope string `json:"scope"`
}

func finalRows(t *testing.T, app *App) []rowDTO {
	t.Helper()
	server := httptest.NewServer(app.handler())
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Agent-Marketplace-Token", app.Bootstrap().Credential)
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
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
			Payload struct {
				Stage string   `json:"stage"`
				Rows  []rowDTO `json:"rows"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event); err != nil {
			continue
		}
		if event.Payload.Stage == "final" {
			return event.Payload.Rows
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	t.Fatal("event stream ended before a final snapshot")
	return nil
}

// spec: gui-shell/launch-directory-does-not-change-the-gui-workspace
func TestAppResolvesUserWorkspaceFromAnyDirectory(t *testing.T) {
	_, repo := newHostFixture(t)
	orgPath := filepath.Join(t.TempDir(), "org.yaml")
	writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
	config.SetTestOrganizationConfigPath(t, orgPath)
	repoConfig := filepath.Join(repo, ".agents", "config.yaml")
	repoLocal := filepath.Join(repo, ".agents", "config.local.yaml")
	repoBefore, _ := os.ReadFile(repoConfig)
	localBefore, _ := os.ReadFile(repoLocal)
	orgBefore, _ := os.ReadFile(orgPath)

	repoApp, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(repoApp.close)
	repoRows := finalRows(t, repoApp)

	t.Chdir(t.TempDir())
	elsewhereApp, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(elsewhereApp.close)
	elsewhereRows := finalRows(t, elsewhereApp)

	if len(repoRows) == 0 {
		t.Fatal("final snapshot from repository cwd carried no rows")
	}
	for _, row := range repoRows {
		if row.Scope != "user" {
			t.Fatalf("non-user row in repository-cwd snapshot: %+v", row)
		}
	}
	if len(repoRows) != len(elsewhereRows) {
		t.Fatalf("rows differ by launch directory: %+v vs %+v", repoRows, elsewhereRows)
	}
	for i := range repoRows {
		if repoRows[i] != elsewhereRows[i] {
			t.Fatalf("rows differ by launch directory: %+v vs %+v", repoRows, elsewhereRows)
		}
	}
	found := false
	for _, row := range repoRows {
		if row.Name == "solo" {
			found = true
		}
	}
	if !found {
		t.Fatalf("user-scope row missing from final snapshot: %+v", repoRows)
	}
	if after, _ := os.ReadFile(repoConfig); string(after) != string(repoBefore) {
		t.Fatal("repo-committed config changed")
	}
	if after, _ := os.ReadFile(repoLocal); string(after) != string(localBefore) {
		t.Fatal("personal-local config changed")
	}
	if after, _ := os.ReadFile(orgPath); string(after) != string(orgBefore) {
		t.Fatal("organization config changed")
	}
}

func TestHostOptionWiring(t *testing.T) {
	newHostFixture(t)
	app, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.close)

	if singleInstanceID != "ef334250-9d55-4fdc-8c94-a6b48b8a3c27" {
		t.Fatalf("single-instance ID changed: %q", singleInstanceID)
	}
	bootstrap := app.Bootstrap()
	if bootstrap.Credential == "" || bootstrap.Credential != app.api.Credential() {
		t.Fatal("bootstrap credential does not match the API credential")
	}
	if bootstrap.Version != version {
		t.Fatalf("bootstrap version = %q, want %q", bootstrap.Version, version)
	}
	if bootstrap.PackageFormat != "" {
		t.Fatalf("default package format = %q, want empty", bootstrap.PackageFormat)
	}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/snapshot", nil)
	app.handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", recorder.Code)
	}
}

type windowEmission struct {
	event string
	args  []string
}

type recordedWindow struct {
	unminimised bool
	shown       bool
	event       string
	args        []string
	events      chan windowEmission
}

func (w *recordedWindow) Unminimise() { w.unminimised = true }
func (w *recordedWindow) Show()       { w.shown = true }
func (w *recordedWindow) Emit(event string, args ...string) {
	if w.events != nil {
		w.events <- windowEmission{event: event, args: args}
		return
	}
	w.event = event
	w.args = args
}

func nextWindowEmission(t *testing.T, window *recordedWindow) windowEmission {
	t.Helper()
	select {
	case event := <-window.events:
		return event
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for window event")
		return windowEmission{}
	}
}

func decodeWindowFrame(t *testing.T, frame string) (kind, stage, message string) {
	t.Helper()
	var event struct {
		Type    string `json:"type"`
		Payload struct {
			Stage   string `json:"stage"`
			Message string `json:"message"`
		} `json:"payload"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(frame, "data:"))), &event); err != nil {
		t.Fatalf("decode window event frame: %v", err)
	}
	return event.Type, event.Payload.Stage, event.Payload.Message
}

// spec: gui-local-api/window-event-stream-requires-the-credential
func TestWindowEventStreamRequiresCredential(t *testing.T) {
	newHostFixture(t)
	app, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.close)
	window := &recordedWindow{}

	app.openEventStream(window, "unauthorized-stream", "wrong")

	if window.event != "am:session-events:end" || len(window.args) != 2 || window.args[0] != "unauthorized-stream" || window.args[1] != "localapi.unauthorized" {
		t.Fatalf("unauthorized stream end = %q %v", window.event, window.args)
	}
}

// spec: gui-local-api/completion-notification-renders-without-further-activity
func TestWindowEventStreamDeliversCompletionAndCloses(t *testing.T) {
	newHostFixture(t)
	app, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.close)
	finalRows(t, app)
	window := &recordedWindow{events: make(chan windowEmission, 64)}
	const streamID = "valid-stream"

	app.openEventStream(window, streamID, app.api.Credential())

	first := nextWindowEmission(t, window)
	if first.event != "am:session-events" || len(first.args) != 2 || first.args[0] != streamID {
		t.Fatalf("initial event = %+v", first)
	}
	if kind, stage, _ := decodeWindowFrame(t, first.args[1]); kind != "snapshot" || stage != "final" {
		t.Fatalf("initial frame = %s/%s, want current final snapshot", kind, stage)
	}

	server := httptest.NewServer(app.handler())
	t.Cleanup(server.Close)
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/actions", strings.NewReader(`{"action":"sync"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Agent-Marketplace-Token", app.api.Credential())
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("sync status = %d, want 202", response.StatusCode)
	}

	var frames []windowEmission
	var finalIndex, notificationIndex int
	for notificationIndex == 0 {
		event := nextWindowEmission(t, window)
		if event.event != "am:session-events" || len(event.args) != 2 || event.args[0] != streamID {
			t.Fatalf("unexpected event before sync completion: %+v", event)
		}
		frames = append(frames, event)
		kind, stage, message := decodeWindowFrame(t, event.args[1])
		if kind == "snapshot" && stage == "final" {
			finalIndex = len(frames)
		}
		if kind == "notification" && message == "Sync complete" {
			notificationIndex = len(frames)
		}
	}
	if finalIndex == 0 || notificationIndex != finalIndex+1 {
		t.Fatalf("completion frames final=%d notification=%d, want adjacent ordered frames", finalIndex, notificationIndex)
	}

	app.closeEventStream(streamID)
	end := nextWindowEmission(t, window)
	if end.event != "am:session-events:end" || len(end.args) != 2 || end.args[0] != streamID || end.args[1] != "" {
		t.Fatalf("normal stream end = %+v", end)
	}
	snapshotRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/snapshot", nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshotRequest.Header.Set("X-Agent-Marketplace-Token", app.api.Credential())
	snapshotResponse, err := server.Client().Do(snapshotRequest)
	if err != nil {
		t.Fatal(err)
	}
	_ = snapshotResponse.Body.Close()
	select {
	case event := <-window.events:
		t.Fatalf("closed stream emitted another event: %+v", event)
	case <-time.After(100 * time.Millisecond):
	}
	app.openEventStream(window, "app-close-stream", app.api.Credential())
	initial := nextWindowEmission(t, window)
	if initial.event != "am:session-events" || len(initial.args) != 2 || initial.args[0] != "app-close-stream" {
		t.Fatalf("app-close stream initial event = %+v", initial)
	}
	app.close()
	closed := nextWindowEmission(t, window)
	if closed.event != "am:session-events:end" || len(closed.args) != 2 || closed.args[0] != "app-close-stream" || closed.args[1] != "" {
		t.Fatalf("app-close stream end = %+v", closed)
	}
}

func TestWindowEventStreamCloseBeforeOpenLeavesNoSubscription(t *testing.T) {
	newHostFixture(t)
	app, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.close)
	finalRows(t, app)
	window := &recordedWindow{events: make(chan windowEmission, 64)}

	app.closeEventStream("early-close")
	app.openEventStream(window, "early-close", app.api.Credential())
	app.openEventStream(window, "unauthorized-close", "wrong")
	if end := nextWindowEmission(t, window); end.event != "am:session-events:end" || len(end.args) != 2 || end.args[0] != "unauthorized-close" {
		t.Fatalf("unauthorized stream end = %+v", end)
	}
	app.closeEventStream("unauthorized-close")
	select {
	case event := <-window.events:
		t.Fatalf("stream closed before open emitted %+v", event)
	case <-time.After(200 * time.Millisecond):
	}

	app.eventStreams.mu.Lock()
	streams, closed := len(app.eventStreams.streams), len(app.eventStreams.closed)
	app.eventStreams.mu.Unlock()
	if streams != 0 || closed != 0 {
		t.Fatalf("retained %d streams and %d close records, want none", streams, closed)
	}
}

func TestWindowEventStreamReplacementDoesNotEndNewStream(t *testing.T) {
	newHostFixture(t)
	app, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.close)
	finalRows(t, app)
	window := &recordedWindow{events: make(chan windowEmission, 64)}
	const streamID = "reused-stream"

	for range 2 {
		app.openEventStream(window, streamID, app.api.Credential())
		initial := nextWindowEmission(t, window)
		if initial.event != "am:session-events" || len(initial.args) != 2 || initial.args[0] != streamID {
			t.Fatalf("initial event = %+v", initial)
		}
	}
	select {
	case event := <-window.events:
		t.Fatalf("replaced stream emitted %+v for the replacement id", event)
	case <-time.After(200 * time.Millisecond):
	}

	app.closeEventStream(streamID)
	end := nextWindowEmission(t, window)
	if end.event != "am:session-events:end" || len(end.args) != 2 || end.args[0] != streamID || end.args[1] != "" {
		t.Fatalf("replacement stream end = %+v", end)
	}
}

// spec: session-interactions/confirmation-retains-all-user-choices
func TestBeforeCloseBlocksUntilApproved(t *testing.T) {
	newHostFixture(t)
	app, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.close)

	window := &recordedWindow{}
	if !app.beforeClose(window, false) {
		t.Fatal("unapproved close was not blocked")
	}
	if window.event != closeRequestEvent {
		t.Fatalf("blocked close emitted %q, want %q", window.event, closeRequestEvent)
	}
	window.event = ""
	if app.beforeClose(window, true) {
		t.Fatal("approved close was blocked")
	}
	if window.event != "" {
		t.Fatalf("approved close emitted %q", window.event)
	}
}

// spec: session-interactions/confirmation-retains-all-user-choices
func TestQuitInvokesInstalledCallbackOnce(t *testing.T) {
	newHostFixture(t)
	app, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.close)

	app.Quit()
	calls := 0
	app.quit = func() { calls++ }
	app.Quit()
	if calls != 1 {
		t.Fatalf("quit callback invoked %d times, want 1", calls)
	}
}

// spec: gui-shell/second-launch-focuses-the-running-instance
func TestSecondLaunchFocusesExistingWindow(t *testing.T) {
	newHostFixture(t)
	app, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.close)

	window := &recordedWindow{}
	app.secondLaunch(window, []string{"agent-marketplace-gui", "--flag"})
	if !window.unminimised || !window.shown {
		t.Fatalf("existing window not focused: %+v", window)
	}
	if window.event != launchRequestEvent {
		t.Fatalf("event = %q, want %q", window.event, launchRequestEvent)
	}
	if len(window.args) != 2 || window.args[1] != "--flag" {
		t.Fatalf("launch args not handed off: %v", window.args)
	}
}
