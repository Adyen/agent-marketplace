package main

import (
	"bytes"
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
	"github.com/Adyen/agent-marketplace/internal/localapi"
	"github.com/Adyen/agent-marketplace/internal/session"
)

func guiPost(t *testing.T, server *httptest.Server, credential, path, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Agent-Marketplace-Token", credential)
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("%s returned invalid JSON: %v", path, err)
	}
	return resp.StatusCode, decoded
}

func guiInitialize(t *testing.T, api *localapi.API, body string) int {
	t.Helper()
	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)

	post := func(path, payload string) (int, map[string]any) {
		return guiPost(t, server, api.Credential(), path, payload)
	}

	status, decoded := post("/api/configuration/actions", body)
	if status != http.StatusAccepted {
		return status
	}
	operation, _ := decoded["operation"].(map[string]any)
	if operation["kind"] != "mutation" {
		t.Fatalf("initialize operation kind = %v, want mutation", operation["kind"])
	}

	deadline := time.Now().Add(30 * time.Second)
	for {
		status, snapshot := post("/api/snapshot", "")
		if status == http.StatusOK {
			if op, present := snapshot["operation"]; present && op == nil && snapshot["stage"] == "final" {
				return http.StatusAccepted
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("initialize operation did not settle")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func runInteractiveInit(t *testing.T, home, answers string) []byte {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(home, ".factory"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	prev := isTerminal
	isTerminal = func(fd uintptr) bool { return true }
	defer func() { isTerminal = prev }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	go func() {
		_, _ = w.WriteString(answers)
		_ = w.Close()
	}()

	var out bytes.Buffer
	if err := runInit(context.Background(), &out, r, nil); err != nil {
		t.Fatalf("runInit failed: %v\noutput: %s", err, out.String())
	}
	data, err := os.ReadFile(filepath.Join(home, ".agents", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func guiInitializeBytes(t *testing.T, home, body string) []byte {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(home, ".factory"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	api, err := localapi.NewUser(context.Background(), home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()

	if status := guiInitialize(t, api, body); status != http.StatusAccepted {
		t.Fatalf("GUI initialize status = %d, want 202", status)
	}
	data, err := os.ReadFile(filepath.Join(home, ".agents", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// spec: gui-first-run/harness-targets-and-consent-match-init-output
// spec: gui-first-run/unmanaged-first-run-presents-init-equivalent-consent
// spec: gui-first-run/organization-telemetry-level-skips-consent
func TestGUIInitializationMatchesInteractiveInit(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}

	for _, tc := range []struct {
		name     string
		org      string
		answers  string
		body     string
		wantFile string
	}{
		{
			name:     "affirmative",
			answers:  "y\ny\n",
			body:     `{"action":"initialize","targets":["factory"],"consent":true}`,
			wantFile: "apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\ntelemetry:\n  level: basic\n",
		},
		{
			name:     "declined",
			answers:  "y\nn\n",
			body:     `{"action":"initialize","targets":["factory"],"consent":false}`,
			wantFile: "apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\n",
		},
		{
			name:     "org-level-off",
			org:      "apiVersion: agent-marketplace/v4\ntelemetry: {level: off}\n",
			answers:  "y\n",
			body:     `{"action":"initialize","targets":["factory"]}`,
			wantFile: "apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\n",
		},
		{
			name:     "org-level-basic",
			org:      "apiVersion: agent-marketplace/v4\ntelemetry: {level: basic}\n",
			answers:  "y\n",
			body:     `{"action":"initialize","targets":["factory"]}`,
			wantFile: "apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\n",
		},
		{
			name:     "org-endpoint-only",
			org:      "apiVersion: agent-marketplace/v4\ntelemetry: {endpoint: https://org.example.com}\n",
			answers:  "y\nn\n",
			body:     `{"action":"initialize","targets":["factory"],"consent":false}`,
			wantFile: "apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.org != "" {
				orgPath := filepath.Join(t.TempDir(), "org.yaml")
				if err := os.WriteFile(orgPath, []byte(tc.org), 0o600); err != nil {
					t.Fatal(err)
				}
				config.SetTestOrganizationConfigPath(t, orgPath)
			}

			cliHome := t.TempDir()
			cliBytes := runInteractiveInit(t, cliHome, tc.answers)

			guiHome := t.TempDir()
			guiBytes := guiInitializeBytes(t, guiHome, tc.body)

			if !bytes.Equal(guiBytes, cliBytes) {
				t.Fatalf("GUI and interactive init bytes differ:\nGUI:\n%s\nCLI:\n%s", guiBytes, cliBytes)
			}
			if string(cliBytes) != tc.wantFile {
				t.Fatalf("interactive init bytes = %q, want %q", cliBytes, tc.wantFile)
			}
			if string(guiBytes) != tc.wantFile {
				t.Fatalf("GUI init bytes = %q, want %q", guiBytes, tc.wantFile)
			}
		})
	}
}
