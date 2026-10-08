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

// spec: gui-shell/launch-directory-does-not-change-the-gui-workspace
func TestAppResolvesUserWorkspaceFromAnyDirectory(t *testing.T) {
	home, repo := newHostFixture(t)
	orgPath := filepath.Join(t.TempDir(), "missing-org.yaml")
	config.SetTestOrganizationConfigPath(t, orgPath)
	repoConfig := filepath.Join(repo, ".agents", "config.yaml")
	repoLocal := filepath.Join(repo, ".agents", "config.local.yaml")
	repoBefore, _ := os.ReadFile(repoConfig)
	localBefore, _ := os.ReadFile(repoLocal)

	app, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.close)

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

	type rowDTO struct {
		Name  string `json:"name"`
		Scope string `json:"scope"`
	}
	var finalRows []rowDTO
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
			finalRows = event.Payload.Rows
			break
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range finalRows {
		if row.Name == "solo" && row.Scope == "user" {
			found = true
		}
	}
	if !found {
		t.Fatalf("user-scope row missing from final snapshot: %+v", finalRows)
	}
	if after, _ := os.ReadFile(repoConfig); string(after) != string(repoBefore) {
		t.Fatal("repo-committed config changed")
	}
	if after, _ := os.ReadFile(repoLocal); string(after) != string(localBefore) {
		t.Fatal("personal-local config changed")
	}
	if _, err := os.Stat(orgPath); !os.IsNotExist(err) {
		t.Fatal("organization config path was created")
	}
	_ = home
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
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/snapshot", nil)
	app.handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", recorder.Code)
	}
}

type recordedWindow struct {
	unminimised bool
	shown       bool
	event       string
	args        []string
}

func (w *recordedWindow) Unminimise() { w.unminimised = true }
func (w *recordedWindow) Show()       { w.shown = true }
func (w *recordedWindow) Emit(event string, args ...string) {
	w.event = event
	w.args = args
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
