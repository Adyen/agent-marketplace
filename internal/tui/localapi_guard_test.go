package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/localapi"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/session"
)

// spec: gui-local-api/concurrent-mutation-is-rejected-as-busy
func TestLocalAPISharesOperationGateWithTUI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	t.Setenv("CODEX_HOME", t.TempDir())
	repo := t.TempDir()
	t.Chdir(repo)
	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, "solo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "solo", "SKILL.md"), []byte("---\nname: solo\ndescription: Solo\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".agents", "config.yaml"),
		[]byte("apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  local: {directory: "+source+"}\nresources:\n  local: {all: true}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	config.SetTestOrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
	workspace, err := pipeline.NewWorkspace(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	gate := session.NewOperationGate()

	release := make(chan struct{})
	releaseOnce := func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}
	defer releaseOnce()
	entered := make(chan struct{})
	enteredOnce := func() {
		select {
		case <-entered:
		default:
			close(entered)
		}
	}
	api, err := localapi.New(context.Background(), workspace, home, session.DependencyLifecycle{
		Sync: func(context.Context, pipeline.Workspace) ([]error, error) {
			enteredOnce()
			<-release
			return nil, nil
		},
	}, gate)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	model := NewModel(repo, home, WithWorkspace(workspace), WithOperationGate(gate))

	post := func(action string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/actions", strings.NewReader(`{"action":"`+action+`"}`))
		req.Header.Set("X-Agent-Marketplace-Token", api.Credential())
		recorder := httptest.NewRecorder()
		api.Handler().ServeHTTP(recorder, req)
		return recorder.Code
	}

	operation, ok := model.beginOperation(operationMutation)
	if !ok {
		t.Fatal("TUI operation was blocked")
	}
	if code := post("sync"); code != http.StatusConflict {
		t.Fatalf("API action during TUI operation = %d, want 409", code)
	}
	model.finishOperation(operation)

	req := httptest.NewRequest(http.MethodPost, "/api/snapshot", nil)
	req.Header.Set("X-Agent-Marketplace-Token", api.Credential())
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("seeding snapshot = %d, want 200", recorder.Code)
	}

	if code := post("sync"); code != http.StatusAccepted {
		t.Fatalf("API action after TUI release = %d, want 202", code)
	}
	<-entered
	if _, ok := model.beginOperation(operationMutation); ok {
		t.Fatal("TUI operation started while the API held the shared gate")
	}

	model.finishOperation(operationID(1 << 30))
	if _, _, active := gate.Active(); !active {
		t.Fatal("stale TUI completion released the API's gate reservation")
	}
	if _, ok := model.beginOperation(operationMutation); ok {
		t.Fatal("TUI operation started after a stale completion")
	}

	releaseOnce()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, active := gate.Active(); !active {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, _, active := gate.Active(); active {
		t.Fatal("API operation never released the gate")
	}
	if _, ok := model.beginOperation(operationMutation); !ok {
		t.Fatal("TUI operation blocked after the API operation released the gate")
	}
}
