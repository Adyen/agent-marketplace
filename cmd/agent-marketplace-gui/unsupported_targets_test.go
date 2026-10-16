package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/session"
)

type hostConfigurationMetadata struct {
	UserTargets      []string `json:"userTargets"`
	SupportedTargets []string `json:"supportedTargets"`
	RootPresent      bool     `json:"rootPresent"`
	Editable         bool     `json:"editable"`
	FirstRun         bool     `json:"firstRun"`
	CanInitialize    bool     `json:"canInitialize"`
	ConsentRequired  bool     `json:"consentRequired"`
}

type hostAPIError struct {
	Code string `json:"code"`
}

func hostDirectoryConfig(source string) string {
	return fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [devin]\ntelemetry:\n  level: off\nsources:\n  local:\n    directory: %q\nresources:\n  local: {all: true}\n", source)
}

func newHostAPIServer(t *testing.T, app *App) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(app.handler())
	t.Cleanup(server.Close)
	return server
}

func postHostAPI(t *testing.T, server *httptest.Server, app *App, path string, body []byte) (int, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+path, reader)
	if err != nil {
		t.Fatal("create authenticated host API request")
	}
	req.Header.Set("X-Agent-Marketplace-Token", app.Bootstrap().Credential)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal("send authenticated host API request")
	}
	defer func() { _ = resp.Body.Close() }()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal("read host API response")
	}
	return resp.StatusCode, responseBody
}

func hasTarget(targets []string, wanted string) bool {
	for _, target := range targets {
		if target == wanted {
			return true
		}
	}
	return false
}

func decodeHostConfiguration(t *testing.T, status int, body []byte) hostConfigurationMetadata {
	t.Helper()
	if status != http.StatusOK {
		t.Fatal("configuration endpoint did not return success")
	}
	var metadata hostConfigurationMetadata
	if err := json.Unmarshal(body, &metadata); err != nil {
		t.Fatal("decode configuration metadata")
	}
	return metadata
}

func unsupportedTargetRequest(t *testing.T, action string) []byte {
	t.Helper()
	body, err := json.Marshal(struct {
		Action  string   `json:"action"`
		Targets []string `json:"targets"`
	}{Action: action, Targets: []string{"devin", "work44-unsupported"}})
	if err != nil {
		t.Fatal("encode unsupported-target request")
	}
	return body
}

func requireUnsupportedTargetError(t *testing.T, status int, body []byte) {
	t.Helper()
	if status != http.StatusBadRequest {
		t.Fatalf("unsupported-target response status %d, want 400", status)
	}
	var response hostAPIError
	if err := json.Unmarshal(body, &response); err != nil || response.Code != "localapi.invalid_request" {
		t.Fatal("unsupported target did not return localapi.invalid_request")
	}
}

// spec: session-interactions/newly-introduced-unsupported-harness-target-is-refused
func TestGuiHostRefusesNewUnsupportedTarget(t *testing.T) {
	paths := startupHostFixture(t, hostDirectoryConfig)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal("read fixture home")
	}
	repo, err := os.Getwd()
	if err != nil {
		t.Fatal("read fixture working directory")
	}
	paths = append(paths, filepath.Join(home, ".agents"), filepath.Join(repo, ".agents"))
	before := captureProtectedHostPaths(t, paths)
	app, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal("create supported user-scope host GUI")
	}
	t.Cleanup(app.close)
	stages := readHostStartupStages(t, app)
	assertHostStartupStageOrder(t, stages)
	assertHostSkillRows(t, stages[3].Payload.Rows, false)
	if stages[3].Payload.Drift != "ready" {
		t.Fatal("supported configuration was not ready before target refusal")
	}

	server := newHostAPIServer(t, app)
	status, body := postHostAPI(t, server, app, "/api/configuration", nil)
	metadata := decodeHostConfiguration(t, status, body)
	if !metadata.RootPresent || !metadata.Editable || len(metadata.UserTargets) != 1 || metadata.UserTargets[0] != "devin" ||
		!hasTarget(metadata.SupportedTargets, "devin") || hasTarget(metadata.SupportedTargets, "work44-unsupported") {
		t.Fatal("configuration metadata was not ready with only supported targets")
	}
	request := unsupportedTargetRequest(t, "save-targets")
	status, body = postHostAPI(t, server, app, "/api/configuration/actions", request)
	requireUnsupportedTargetError(t, status, body)
	server.Close()
	app.close()
	assertProtectedHostPathsUnchanged(t, paths, before)
}

// spec: session-interactions/initial-configuration-rejects-unsupported-harness-targets
func TestGuiHostRefusesUnsupportedInitialization(t *testing.T) {
	paths := startupHostFixture(t, hostDirectoryConfig)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal("read fixture home")
	}
	repo, err := os.Getwd()
	if err != nil {
		t.Fatal("read fixture working directory")
	}
	userRoot := filepath.Join(home, ".agents", "config.yaml")
	if err := os.Remove(userRoot); err != nil {
		t.Fatal("remove test-created user root")
	}
	orgDir := t.TempDir()
	orgConfig := filepath.Join(orgDir, "config.yaml")
	writeFile(t, orgConfig, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\ntelemetry:\n  level: off\n")
	config.SetTestOrganizationConfigPath(t, orgConfig)
	paths = append(paths, filepath.Join(home, ".agents"), filepath.Join(repo, ".agents"), orgDir, orgConfig, config.Organization.LockPath(orgConfig))
	before := captureProtectedHostPaths(t, paths)
	app, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal("create supported host GUI with absent user root")
	}
	t.Cleanup(app.close)
	server := newHostAPIServer(t, app)
	status, body := postHostAPI(t, server, app, "/api/configuration", nil)
	metadata := decodeHostConfiguration(t, status, body)
	if metadata.RootPresent || !metadata.FirstRun || !metadata.CanInitialize || metadata.ConsentRequired ||
		!hasTarget(metadata.SupportedTargets, "devin") || hasTarget(metadata.SupportedTargets, "work44-unsupported") {
		t.Fatal("first-run metadata was not ready for supported initialization")
	}
	request := unsupportedTargetRequest(t, "initialize")
	status, body = postHostAPI(t, server, app, "/api/configuration/actions", request)
	requireUnsupportedTargetError(t, status, body)
	server.Close()
	app.close()
	assertProtectedHostPathsUnchanged(t, paths, before)
}
