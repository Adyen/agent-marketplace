package localapi

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/session"
)

func newUserAPIFixture(t *testing.T, writeRoot bool) (home, repo string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	config.SetTestOrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))

	source := t.TempDir()
	writeFile(t, filepath.Join(source, "solo", "SKILL.md"), fixtureSkillBody)
	if writeRoot {
		writeFile(t, filepath.Join(home, ".agents", "config.yaml"),
			"apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  local: {directory: "+source+"}\nresources:\n  local: {all: true}\n")
	}

	repo = t.TempDir()
	writeFile(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: [broken\n")
	writeFile(t, filepath.Join(repo, ".agents", "config.local.yaml"), "registries: [broken\n")
	t.Chdir(repo)
	return home, repo
}

func newUserAPI(t *testing.T, lifecycle session.DependencyLifecycle) (*API, string, string) {
	t.Helper()
	home, repo := newUserAPIFixture(t, true)
	api, err := NewUser(context.Background(), home, lifecycle, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	return api, home, repo
}

func streamEvents(t *testing.T, api *API) <-chan sessionEvent {
	t.Helper()
	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Agent-Marketplace-Token", api.Credential())
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	events := make(chan sessionEvent, 64)
	go func() {
		defer close(events)
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 1<<20), 1<<20)
		var raw strings.Builder
		for scanner.Scan() {
			line := scanner.Text()
			if line != "" {
				if strings.HasPrefix(line, "data:") {
					raw.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
				}
				continue
			}
			payload := raw.String()
			raw.Reset()
			if payload == "" {
				continue
			}
			var event sessionEvent
			if err := json.Unmarshal([]byte(payload), &event); err != nil {
				continue
			}
			events <- event
		}
	}()
	return events
}

func nextEvent(t *testing.T, events <-chan sessionEvent) sessionEvent {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("event stream closed")
		}
		return event
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for event")
		return sessionEvent{}
	}
}

func (e sessionEvent) snapshotStage() string {
	data, err := json.Marshal(e.Payload)
	if err != nil {
		return ""
	}
	var snapshot snapshotDTO
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return ""
	}
	return snapshot.Stage
}

func (e sessionEvent) snapshotPayload(t *testing.T) snapshotDTO {
	t.Helper()
	data, err := json.Marshal(e.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot snapshotDTO
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func postSnapshot(t *testing.T, api *API) snapshotDTO {
	t.Helper()
	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/snapshot", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Agent-Marketplace-Token", api.Credential())
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var snapshot snapshotDTO
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func requestSnapshot(api *API) (snapshotDTO, error) {
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/snapshot", nil)
	if err != nil {
		return snapshotDTO{}, err
	}
	req.Header.Set("X-Agent-Marketplace-Token", api.Credential())
	resp, err := server.Client().Do(req)
	if err != nil {
		return snapshotDTO{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var snapshot snapshotDTO
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		return snapshotDTO{}, err
	}
	return snapshot, nil
}

func postAction(t *testing.T, api *API, action string) int {
	t.Helper()
	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/actions", strings.NewReader(`{"action":"`+action+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Agent-Marketplace-Token", api.Credential())
	req.Header.Set("Content-Type", "application/json")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

// spec: gui-shell/refresh-and-focus-reload-use-ordinary-snapshot-behavior
func TestUserAPIEventsStartProgressiveInitialRead(t *testing.T) {
	api, _, repo := newUserAPI(t, session.DependencyLifecycle{})
	repoConfig := filepath.Join(repo, ".agents", "config.yaml")
	repoLocal := filepath.Join(repo, ".agents", "config.local.yaml")
	orgPath := filepath.Join(t.TempDir(), "missing-org.yaml")
	config.SetTestOrganizationConfigPath(t, orgPath)
	repoBefore, _ := os.ReadFile(repoConfig)
	localBefore, _ := os.ReadFile(repoLocal)

	events := streamEvents(t, api)
	var stages []string
	var generation uint64
	var final snapshotDTO
	for {
		event := nextEvent(t, events)
		if event.Type != "snapshot" {
			t.Fatalf("unexpected event type %q", event.Type)
		}
		stage := event.snapshotStage()
		stages = append(stages, stage)
		if generation == 0 {
			generation = event.Generation
		} else if event.Generation != generation {
			t.Fatalf("generation changed mid-stream: %d then %d", generation, event.Generation)
		}
		if stage == "final" {
			final = event.snapshotPayload(t)
			break
		}
	}
	wantStages := []string{"provisional", "config", "inventory", "final"}
	if strings.Join(stages, ",") != strings.Join(wantStages, ",") {
		t.Fatalf("stage sequence = %v, want %v", stages, wantStages)
	}
	found := false
	for _, row := range final.Rows {
		if row.Name == "solo" {
			found = true
		}
	}
	if !found {
		t.Fatal("final snapshot did not carry the user resource row")
	}
	if len(final.Harnesses) == 0 {
		t.Fatal("final snapshot carried no target harnesses")
	}

	refresh := postSnapshot(t, api)
	if refresh.Stage != "final" || len(refresh.Rows) != len(final.Rows) {
		t.Fatalf("ordinary reload stage=%q rows=%d, want final/%d", refresh.Stage, len(refresh.Rows), len(final.Rows))
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
}

// spec: gui-shell/external-file-changes-require-a-refresh-trigger
func TestUserAPIRefreshPicksUpExternalConfigChange(t *testing.T) {
	api, home, _ := newUserAPI(t, session.DependencyLifecycle{})

	events := streamEvents(t, api)
	for nextEvent(t, events).snapshotStage() != "final" {
	}

	userConfig := filepath.Join(home, ".agents", "config.yaml")
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "added", "SKILL.md"), "---\nname: added\ndescription: Added skill\n---\n# Added\n")
	writeFile(t, userConfig,
		"apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  local: {directory: "+filepath.Join(home, "unused")+"}\n  extra: {directory: "+source+"}\nresources:\n  local: {all: true}\n  extra: {all: true}\n")

	refresh := postSnapshot(t, api)
	found := false
	for _, row := range refresh.Rows {
		if row.Name == "added" {
			found = true
		}
	}
	if !found {
		t.Fatal("external config change not loaded after explicit refresh")
	}
}

// spec: gui-shell/refresh-and-focus-reload-use-ordinary-snapshot-behavior
func TestUserAPIBlockedReloadRetainsAcceptedRowsUntilFinal(t *testing.T) {
	api, _, _ := newUserAPI(t, session.DependencyLifecycle{})

	events := streamEvents(t, api)
	var accepted snapshotDTO
	for {
		event := nextEvent(t, events)
		if event.snapshotStage() == "final" {
			accepted = event.snapshotPayload(t)
			break
		}
	}
	if len(accepted.Rows) == 0 {
		t.Fatal("no accepted rows in initial final")
	}

	release := make(chan struct{})
	realDiscover := session.DiscoverCatalog
	session.DiscoverCatalog = func(dir, sourceHarness string) (catalog.LoadedCatalog, error) {
		<-release
		return realDiscover(dir, sourceHarness)
	}
	t.Cleanup(func() { session.DiscoverCatalog = realDiscover })

	done := make(chan struct {
		snapshot snapshotDTO
		err      error
	}, 1)
	go func() {
		snapshot, err := requestSnapshot(api)
		done <- struct {
			snapshot snapshotDTO
			err      error
		}{snapshot, err}
	}()

	select {
	case event := <-events:
		t.Fatalf("event delivered while reload blocked: %q/%s", event.Type, event.snapshotStage())
	case <-time.After(200 * time.Millisecond):
	}
	close(release)

	var streamed snapshotDTO
	for {
		event := nextEvent(t, events)
		if event.Type == "snapshot" && event.snapshotStage() == "final" {
			streamed = event.snapshotPayload(t)
			break
		}
	}
	result := <-done
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.snapshot.Stage != "final" || streamed.Stage != "final" {
		t.Fatalf("reload did not settle to final: %q/%q", result.snapshot.Stage, streamed.Stage)
	}
	if len(result.snapshot.Rows) != len(accepted.Rows) {
		t.Fatalf("row count changed: %d -> %d", len(accepted.Rows), len(result.snapshot.Rows))
	}
}

func TestNewUserRejectsNonHome(t *testing.T) {
	home, _ := newUserAPIFixture(t, true)
	if _, err := NewUser(context.Background(), t.TempDir(), session.DependencyLifecycle{}, nil); err == nil {
		t.Fatal("non-HOME root accepted")
	}
	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	api.Close()
}

func countingLifecycle(syncCalls, updateCalls *atomic.Int32) session.DependencyLifecycle {
	return session.DependencyLifecycle{
		Sync: func(context.Context, pipeline.Workspace) ([]error, error) {
			syncCalls.Add(1)
			return nil, nil
		},
		Update: func(context.Context, pipeline.Workspace) ([]error, error) {
			updateCalls.Add(1)
			return nil, nil
		},
	}
}

func TestUserAPIRefusesMutationsWithoutRoot(t *testing.T) {
	var syncCalls, updateCalls atomic.Int32
	home, _ := newUserAPIFixture(t, false)
	api, err := NewUser(context.Background(), home, countingLifecycle(&syncCalls, &updateCalls), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)

	events := streamEvents(t, api)
	for nextEvent(t, events).snapshotStage() != "final" {
	}

	if status := postAction(t, api, "sync"); status != http.StatusBadRequest {
		t.Fatalf("sync without user root status = %d, want 400", status)
	}
	if status := postAction(t, api, "update"); status != http.StatusBadRequest {
		t.Fatalf("update without user root status = %d, want 400", status)
	}
	if got := syncCalls.Load() + updateCalls.Load(); got != 0 {
		t.Fatalf("lifecycle invoked %d times after refusal", got)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "config.yaml")); !os.IsNotExist(err) {
		t.Fatal("user root appeared after refused sync")
	}
	if _, err := os.Stat(filepath.Join(home, ".devin")); !os.IsNotExist(err) {
		t.Fatal("target output appeared after refused sync")
	}
}

func TestUserAPIRefusesMutationsWithoutTargets(t *testing.T) {
	var syncCalls, updateCalls atomic.Int32
	home, repo := newUserAPIFixture(t, true)
	writeFile(t, filepath.Join(home, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nharnesses: []\n")
	api, err := NewUser(context.Background(), home, countingLifecycle(&syncCalls, &updateCalls), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	_ = repo

	events := streamEvents(t, api)
	for nextEvent(t, events).snapshotStage() != "final" {
	}

	if status := postAction(t, api, "sync"); status != http.StatusBadRequest {
		t.Fatalf("sync with empty target set status = %d, want 400", status)
	}
	if got := syncCalls.Load() + updateCalls.Load(); got != 0 {
		t.Fatalf("lifecycle invoked %d times after refusal", got)
	}
}

// spec: gui-shell/refresh-and-focus-reload-use-ordinary-snapshot-behavior
func TestUserAPIAcceptsMutationWithTargets(t *testing.T) {
	var syncCalls, updateCalls atomic.Int32
	api, home, _ := newUserAPI(t, countingLifecycle(&syncCalls, &updateCalls))

	events := streamEvents(t, api)
	for nextEvent(t, events).snapshotStage() != "final" {
	}

	if status := postAction(t, api, "sync"); status != http.StatusAccepted {
		t.Fatalf("sync with valid user root status = %d, want 202", status)
	}
	for {
		event := nextEvent(t, events)
		if event.Type == "notification" {
			break
		}
	}
	if got := syncCalls.Load(); got != 1 {
		t.Fatalf("lifecycle sync invoked %d times, want 1", got)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "config.yaml")); err != nil {
		t.Fatal("user root missing after accepted sync")
	}
}
