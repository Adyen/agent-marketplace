package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/session"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

type acquisitionHostRow struct {
	startupHostRow
	Status string `json:"status"`
}
type acquisitionHostPayload struct {
	Stage string               `json:"stage"`
	Drift string               `json:"drift"`
	Rows  []acquisitionHostRow `json:"rows"`
}
type acquisitionHostEvent struct {
	Type       string                 `json:"type"`
	Sequence   uint64                 `json:"sequence"`
	Generation uint64                 `json:"generation"`
	Payload    acquisitionHostPayload `json:"payload"`
}

func requireAcquisition(t *testing.T, ok bool, message string) {
	t.Helper()
	if !ok {
		t.Fatal(message)
	}
}
func blockedWork47Marketplace(t *testing.T) (string, <-chan struct{}, func(), func()) {
	t.Helper()
	entered, releaseCh := make(chan struct{}), make(chan struct{})
	var enteredOnce, releaseOnce, closeOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", "http://"+r.Host))
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/registries/tools" {
			http.NotFound(w, r)
			return
		}
		enteredOnce.Do(func() { close(entered) })
		select {
		case <-releaseCh:
		case <-r.Context().Done():
		}
		http.NotFound(w, r)
	}))
	release := func() { releaseOnce.Do(func() { close(releaseCh) }) }
	closeServer := func() { closeOnce.Do(func() { server.Close() }) }
	t.Cleanup(func() { release(); closeServer() })
	return server.URL, entered, release, closeServer
}
func streamAcquisitionHostSnapshots(ctx context.Context, body io.Reader, snapshots chan<- acquisitionHostEvent, failures chan<- struct{}, done chan<- struct{}) {
	defer close(done)
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event acquisitionHostEvent
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event); err != nil {
			failures <- struct{}{}
			return
		}
		if event.Type == "snapshot" {
			select {
			case snapshots <- event:
			case <-ctx.Done():
				return
			}
		}
	}
	if scanner.Err() != nil {
		failures <- struct{}{}
	}
}
func waitAcquisitionHostSnapshot(t *testing.T, ctx context.Context, snapshots <-chan acquisitionHostEvent, failures <-chan struct{}, done <-chan struct{}) acquisitionHostEvent {
	t.Helper()
	select {
	case event := <-snapshots:
		return event
	case <-failures:
		t.Fatal("decode startup SSE snapshot")
	case <-done:
		t.Fatal("startup SSE ended before provisional snapshot")
	case <-ctx.Done():
		t.Fatal("timed out waiting for provisional snapshot")
	}
	return acquisitionHostEvent{}
}

// spec: session-startup/provisional-config-publishes-configured-pins-before-acquisition
// spec: session-startup/provisional-rows-render-while-transient-acquisition-is-blocked
// spec: session-startup/provisional-rows-show-configured-selection-state
func TestGuiHostPublishesConfiguredRowsWhileAcquisitionBlocked(t *testing.T) {
	paths := startupHostFixture(t, func(source string) string {
		return fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [devin]\ntelemetry:\n  level: off\nsources:\n  local:\n    directory: %q\nresources:\n  local: {all: true}\n", source)
	})
	marketURL, entered, release, closeMarket := blockedWork47Marketplace(t)
	orgDir := t.TempDir()
	orgPath := filepath.Join(orgDir, "config.yaml")
	orgConfig := fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [devin]\ntelemetry:\n  level: off\nsources:\n  mp: {marketplace: %q}\nresources:\n  mp: {include: [tools/skill/remote]}\n", marketURL)
	if err := os.WriteFile(orgPath, []byte(orgConfig), 0o600); err != nil {
		t.Fatal("write owned organization fixture")
	}
	config.SetTestOrganizationConfigPath(t, orgPath)
	paths = append(paths, orgDir, orgPath, config.Organization.LockPath(orgPath))
	before := captureProtectedHostPaths(t, paths)
	app, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal("create GUI host for blocked acquisition")
	}
	guiServer := httptest.NewServer(app.handler())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	var response *http.Response
	var readerDone chan struct{}
	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			release()
			cancel()
			if response != nil {
				_ = response.Body.Close()
			}
			app.close()
			guiServer.Close()
			closeMarket()
			if readerDone != nil {
				select {
				case <-readerDone:
				case <-time.After(5 * time.Second):
					t.Error("startup SSE reader did not stop after release")
				}
			}
		})
	}
	t.Cleanup(cleanup)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, guiServer.URL+"/api/events", nil)
	if err != nil {
		t.Fatal("create authenticated GUI event request")
	}
	request.Header.Set("X-Agent-Marketplace-Token", app.Bootstrap().Credential)
	response, err = guiServer.Client().Do(request)
	if err != nil {
		t.Fatal("open authenticated GUI event stream")
	}
	if response.StatusCode != http.StatusOK {
		t.Fatal("GUI event stream returned unexpected status")
	}
	snapshots := make(chan acquisitionHostEvent, 5)
	failures := make(chan struct{}, 1)
	readerDone = make(chan struct{})
	go streamAcquisitionHostSnapshots(ctx, response.Body, snapshots, failures, readerDone)
	first := waitAcquisitionHostSnapshot(t, ctx, snapshots, failures, readerDone)
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("transient Marketplace acquisition did not reach the blocking endpoint")
	}
	requireAcquisition(t, first.Type == "snapshot" && first.Generation != 0 && first.Payload.Stage == "provisional" && first.Payload.Drift == "pending", "first host snapshot was not the provisional pending stage")
	requireAcquisition(t, len(first.Payload.Rows) == 3, "provisional host snapshot did not contain the three configured rows")
	byName, ids := make(map[string]acquisitionHostRow, 3), make(map[string]bool, 3)
	for _, row := range first.Payload.Rows {
		requireAcquisition(t, row.ID != "" && !ids[row.ID], "provisional host rows did not have unique opaque IDs")
		ids[row.ID] = true
		byName[row.Name] = row
	}
	for _, name := range []string{"alpha", "beta"} {
		row := byName[name]
		requireAcquisition(t, row.ID != "" && row.Kind == "skill" && row.Registry == "local" && row.Scope == "user" && row.Checked && row.PendingVerification && row.ResourceAction == "none", "provisional Directory row did not reflect configured user selection")
	}
	remote := byName["remote"]
	requireAcquisition(t, remote.ID != "" && remote.Kind == "skill" && remote.Scope == "organization" && remote.Checked && remote.PendingVerification && remote.ResourceAction == "none" && strings.Contains(remote.Status, "verification pending") && strings.Contains(remote.Status, "enabled in config"), "provisional organization Marketplace row did not reflect configured selection")
	configStage := waitAcquisitionHostSnapshot(t, ctx, snapshots, failures, readerDone)
	requireAcquisition(t, configStage.Type == "snapshot" && configStage.Payload.Stage == "config" && configStage.Generation == first.Generation && configStage.Sequence > first.Sequence, "configured-row snapshot was not a later config stage in the same generation")
	requireAcquisition(t, reflect.DeepEqual(configStage.Payload.Rows, first.Payload.Rows), "config stage did not retain the provisional configured rows")
	lastSequence := configStage.Sequence
	for {
		select {
		case extra := <-snapshots:
			requireAcquisition(t, extra.Generation == first.Generation && extra.Sequence > lastSequence, "additional config snapshot changed generation or sequence order")
			if extra.Payload.Stage != "config" || !reflect.DeepEqual(extra.Payload.Rows, first.Payload.Rows) {
				t.Fatalf("unexpected snapshot stage %q while transient acquisition was blocked", extra.Payload.Stage)
			}
			lastSequence = extra.Sequence
		case <-failures:
			t.Fatal("startup SSE failed while acquisition was blocked")
		case <-readerDone:
			t.Fatal("startup SSE ended while acquisition was blocked")
		default:
			goto snapshotsChecked
		}
	}
snapshotsChecked:
	assertProtectedHostPathsUnchanged(t, paths, before)
	cleanup()
	assertProtectedHostPathsUnchanged(t, paths, before)
}
