package localapi

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/session"
)

type availabilityFileState struct {
	mode    os.FileMode
	modTime time.Time
	digest  [sha256.Size]byte
	link    string
}

func captureAvailabilityTree(t *testing.T, root string) map[string]availabilityFileState {
	t.Helper()
	files := map[string]availabilityFileState{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		state := availabilityFileState{mode: info.Mode(), modTime: info.ModTime()}
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			state.digest = sha256.Sum256(data)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			state.link, err = os.Readlink(path)
			if err != nil {
				return err
			}
		}
		files[rel] = state
		return nil
	})
	if err != nil {
		t.Fatalf("capture %s: %v", root, err)
	}
	return files
}

// spec: gui-shell/update-availability-check-writes-nothing
func TestUserUpdateAvailabilityPreviewWritesNothing(t *testing.T) {
	home := t.TempDir()
	configDir := t.TempDir()
	state := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	config.SetTestOrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))

	bare := newBareGitSource(t)
	configPath := filepath.Join(home, ".agents", "config.yaml")
	writeFile(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  test:\n    git: file://"+filepath.ToSlash(bare)+"\n    ref: main\nresources:\n  test: {all: true}\n")
	ws, err := pipeline.NewWorkspace(home, home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.RuntimeDependencyLifecycle(context.Background(), ws, false); err != nil {
		t.Fatalf("initial user sync: %v", err)
	}

	working := filepath.Join(t.TempDir(), "working")
	cmd := exec.Command("git", "clone", bare, working)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone update source: %v\n%s", err, output)
	}
	writeFile(t, filepath.Join(working, "solo", "SKILL.md"), "---\nname: solo\ndescription: Updated skill\n---\n# Solo\n")
	sourceGit(t, working, "add", ".")
	sourceGit(t, working, "commit", "-m", "update")
	sourceGit(t, working, "push", "origin", "main")

	before := map[string]map[string]availabilityFileState{}
	for name, root := range map[string]string{
		"authored": filepath.Join(home, ".agents"),
		"state":    state,
		"harness":  filepath.Join(home, ".config", "devin"),
	} {
		before[name] = captureAvailabilityTree(t, root)
	}

	count, err := session.RuntimeUpdateAvailability(context.Background(), ws)
	if err != nil {
		t.Fatalf("availability preview: %v", err)
	}
	if count != 1 {
		t.Fatalf("available update count = %d, want one changed resource", count)
	}
	for name, root := range map[string]string{
		"authored": filepath.Join(home, ".agents"),
		"state":    state,
		"harness":  filepath.Join(home, ".config", "devin"),
	} {
		if after := captureAvailabilityTree(t, root); !reflect.DeepEqual(before[name], after) {
			t.Errorf("%s files changed during the preview: before=%v after=%v", name, before[name], after)
		}
	}
}

// spec: gui-shell/update-availability-check-writes-nothing
func TestUserUpdateAvailabilityCountsMarketplaceRevisionWithoutWrites(t *testing.T) {
	home, repo := newUserAPIFixture(t, true)
	fixture := setupWork39MarketplaceFixture(t, home, repo, nil, work39ModeUserInstalled, false, false)
	configBytes, err := os.ReadFile(fixture.userConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	unpinnedConfig := strings.Replace(string(configBytes), ": {revision: "+fixture.contentDigest+"}", "", 1)
	if unpinnedConfig == string(configBytes) {
		t.Fatal("Marketplace fixture did not contain the expected explicit revision")
	}
	writeFile(t, fixture.userConfigPath, unpinnedConfig)
	ws, err := pipeline.NewWorkspace(home, home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.RuntimeDependencyLifecycle(context.Background(), ws, false); err != nil {
		t.Fatalf("initial Marketplace install: %v", err)
	}
	fixture.publishUpdatedRevision(t, "# Review code, updated\n")

	stateRoot := os.Getenv("XDG_STATE_HOME")
	if err := os.MkdirAll(stateRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	trees := map[string]string{
		"authored": filepath.Join(home, ".agents"),
		"state":    stateRoot,
		"harness":  filepath.Join(home, ".config", "devin"),
	}
	before := make(map[string]map[string]availabilityFileState, len(trees))
	for name, root := range trees {
		before[name] = captureAvailabilityTree(t, root)
	}

	count, err := session.RuntimeUpdateAvailability(context.Background(), ws)
	if err != nil {
		t.Fatalf("Marketplace availability preview: %v", err)
	}
	if count != 1 {
		t.Fatalf("available Marketplace update count = %d, want one changed resource", count)
	}
	for name, root := range trees {
		if after := captureAvailabilityTree(t, root); !reflect.DeepEqual(before[name], after) {
			t.Errorf("%s files changed during the Marketplace preview: before=%v after=%v", name, before[name], after)
		}
	}
}

// spec: gui-shell/update-shows-the-available-update-count
func TestUserAPIResourcePlanKeepsAvailableUpdateCount(t *testing.T) {
	state := newWork39APITestState(t, work39ModeAvailable, false, false)
	state.api.lifecycle.UpdateAvailability = func(context.Context, pipeline.Workspace) (int, error) {
		return 3, nil
	}
	handler := state.api.Handler()
	handler.ServeHTTP(httptest.NewRecorder(), authedRequest(t, state.api, "/api/snapshot", `{"checkUpdates":true}`))
	waitForAvailableUpdates(t, state.api, 3)

	status, body := postWork39(t, state.api, "/api/resource-plan", work39PlanRequestBody(state.rowID))
	if status != http.StatusOK {
		t.Fatalf("resource plan status/code = %d/%s", status, decodeWork39ErrorCode(t, body))
	}
	state.api.mu.Lock()
	count := state.api.current.AvailableUpdates
	state.api.mu.Unlock()
	if count != 3 {
		t.Fatalf("available updates after opening a resource plan = %d, want 3", count)
	}
}

// spec: gui-shell/update-availability-result-from-before-a-later-change-is-discarded
func TestUserAPIUpdateAvailabilityDiscardsResultAfterOperationStarts(t *testing.T) {
	api, _, _ := newUserAPI(t, session.DependencyLifecycle{})
	var checks atomic.Int32
	checkStarted := make(chan struct{})
	checkRelease := make(chan struct{})
	checkReturned := make(chan struct{})
	syncStarted := make(chan struct{})
	syncRelease := make(chan struct{})
	api.lifecycle.UpdateAvailability = func(ctx context.Context, _ pipeline.Workspace) (int, error) {
		if checks.Add(1) == 1 {
			close(checkStarted)
			<-checkRelease
			close(checkReturned)
			return 7, nil
		}
		return 0, nil
	}
	api.lifecycle.Sync = func(ctx context.Context, _ pipeline.Workspace) (pipeline.SyncResult, error) {
		close(syncStarted)
		select {
		case <-syncRelease:
			return pipeline.SyncResult{}, nil
		case <-ctx.Done():
			return pipeline.SyncResult{}, ctx.Err()
		}
	}

	handler := api.Handler()
	handler.ServeHTTP(httptest.NewRecorder(), authedRequest(t, api, "/api/snapshot", `{"checkUpdates":true}`))
	select {
	case <-checkStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("initial snapshot did not start the availability check")
	}

	action := httptest.NewRecorder()
	handler.ServeHTTP(action, authedRequest(t, api, "/api/actions", `{"action":"sync"}`))
	if action.Code != http.StatusAccepted {
		t.Fatalf("sync status = %d, want 202: %s", action.Code, strings.TrimSpace(action.Body.String()))
	}
	select {
	case <-syncStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("sync operation did not start")
	}
	api.mu.Lock()
	sequenceBeforeResult := api.sequence
	api.mu.Unlock()
	close(checkRelease)
	select {
	case <-checkReturned:
	case <-time.After(10 * time.Second):
		t.Fatal("availability checker did not return")
	}
	time.Sleep(50 * time.Millisecond)
	api.mu.Lock()
	sequenceAfterResult := api.sequence
	staleCount := api.current.AvailableUpdates
	api.mu.Unlock()
	if staleCount != 0 || sequenceAfterResult != sequenceBeforeResult {
		t.Fatalf("stale availability result was published during sync: count=%d sequence=%d/%d", staleCount, sequenceAfterResult, sequenceBeforeResult)
	}

	close(syncRelease)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		api.mu.Lock()
		settled := api.current.Operation == nil
		api.mu.Unlock()
		if settled {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("sync operation did not settle")
}

// spec: gui-shell/no-update-indicator-at-zero-before-a-check-or-after-a-failed-check
func TestUserAPIUpdateAvailabilityFailureStaysSilent(t *testing.T) {
	checked := make(chan struct{})
	home, _ := newUserAPIFixture(t, true)
	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{
		UpdateAvailability: func(context.Context, pipeline.Workspace) (int, error) {
			close(checked)
			return 3, errors.New("remote unavailable")
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)

	api.Handler().ServeHTTP(httptest.NewRecorder(), authedRequest(t, api, "/api/snapshot", `{"checkUpdates":true}`))
	select {
	case <-checked:
	case <-time.After(10 * time.Second):
		t.Fatal("final snapshot did not start the availability check")
	}
	time.Sleep(25 * time.Millisecond)
	api.mu.Lock()
	count := api.current.AvailableUpdates
	diagnostics := append([]diagnosticDTO(nil), api.current.Diagnostics...)
	api.mu.Unlock()
	if count != 0 {
		t.Fatalf("failed availability check reported %d updates", count)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == "error" {
			t.Fatalf("failed availability check surfaced an error: %+v", diagnostic)
		}
	}
}

func TestUserAPIUpdateAvailabilityRunsOnLaunch(t *testing.T) {
	home, _ := newUserAPIFixture(t, true)
	checked := make(chan struct{}, 1)
	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{
		UpdateAvailability: func(context.Context, pipeline.Workspace) (int, error) {
			checked <- struct{}{}
			return 0, nil
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)

	_ = streamEvents(t, api)
	select {
	case <-checked:
	case <-time.After(10 * time.Second):
		t.Fatal("initial event-stream snapshot did not start the availability check")
	}
}

func TestUserAPIUpdateAvailabilityRequiresInjectedChecker(t *testing.T) {
	api, _, _ := newUserAPI(t, session.DependencyLifecycle{})
	if api.lifecycle.UpdateAvailability != nil {
		t.Fatal("update availability checker was enabled without a host opt-in")
	}
}

func TestUserAPIUpdateAvailabilityFocusReloadDoesNotCheck(t *testing.T) {
	checked := make(chan struct{}, 1)
	home, _ := newUserAPIFixture(t, true)
	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{
		UpdateAvailability: func(context.Context, pipeline.Workspace) (int, error) {
			checked <- struct{}{}
			return 0, nil
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)

	api.Handler().ServeHTTP(httptest.NewRecorder(), authedRequest(t, api, "/api/snapshot", `{"checkUpdates":false}`))
	select {
	case <-checked:
		t.Fatal("focus-style snapshot started an update availability check")
	case <-time.After(150 * time.Millisecond):
	}
}

func TestUserAPIUpdateAvailabilityCancelsWhenMutationStarts(t *testing.T) {
	api, _, _ := newUserAPI(t, session.DependencyLifecycle{})
	var checks atomic.Int32
	checkStarted := make(chan struct{})
	checkCanceled := make(chan struct{})
	syncStarted := make(chan struct{})
	syncRelease := make(chan struct{})
	api.lifecycle.UpdateAvailability = func(ctx context.Context, _ pipeline.Workspace) (int, error) {
		if checks.Add(1) == 1 {
			close(checkStarted)
			<-ctx.Done()
			close(checkCanceled)
			return 0, ctx.Err()
		}
		return 0, nil
	}
	api.lifecycle.Sync = func(ctx context.Context, _ pipeline.Workspace) (pipeline.SyncResult, error) {
		close(syncStarted)
		select {
		case <-syncRelease:
			return pipeline.SyncResult{}, nil
		case <-ctx.Done():
			return pipeline.SyncResult{}, ctx.Err()
		}
	}

	handler := api.Handler()
	handler.ServeHTTP(httptest.NewRecorder(), authedRequest(t, api, "/api/snapshot", `{"checkUpdates":true}`))
	select {
	case <-checkStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("initial snapshot did not start the availability check")
	}
	action := httptest.NewRecorder()
	handler.ServeHTTP(action, authedRequest(t, api, "/api/actions", `{"action":"sync"}`))
	if action.Code != http.StatusAccepted {
		close(syncRelease)
		t.Fatalf("sync status = %d, want 202: %s", action.Code, strings.TrimSpace(action.Body.String()))
	}
	select {
	case <-syncStarted:
	case <-time.After(10 * time.Second):
		close(syncRelease)
		t.Fatal("sync operation did not start")
	}
	select {
	case <-checkCanceled:
	case <-time.After(10 * time.Second):
		close(syncRelease)
		t.Fatal("mutation did not cancel the in-flight availability check")
	}

	close(syncRelease)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		api.mu.Lock()
		settled := api.current.Operation == nil
		api.mu.Unlock()
		if settled && checks.Load() >= 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("mutation did not settle and trigger a fresh availability check")
}

func TestUserAPIUpdateAvailabilityKeepsCountUntilRecheckCompletes(t *testing.T) {
	home, _ := newUserAPIFixture(t, true)
	var checks atomic.Int32
	recheckStarted := make(chan struct{})
	recheckRelease := make(chan struct{})
	defer func() {
		select {
		case <-recheckRelease:
		default:
			close(recheckRelease)
		}
	}()
	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{
		UpdateAvailability: func(context.Context, pipeline.Workspace) (int, error) {
			if checks.Add(1) == 1 {
				return 4, nil
			}
			close(recheckStarted)
			<-recheckRelease
			return 0, errors.New("remote unavailable")
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)

	handler := api.Handler()
	handler.ServeHTTP(httptest.NewRecorder(), authedRequest(t, api, "/api/snapshot", `{"checkUpdates":true}`))
	waitForAvailableUpdates(t, api, 4)

	handler.ServeHTTP(httptest.NewRecorder(), authedRequest(t, api, "/api/snapshot", `{"checkUpdates":true}`))
	select {
	case <-recheckStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("explicit refresh did not start the next availability check")
	}
	api.mu.Lock()
	duringCheck := api.current.AvailableUpdates
	api.mu.Unlock()
	if duringCheck != 4 {
		t.Fatalf("available updates during recheck = %d, want prior result 4", duringCheck)
	}

	select {
	case <-recheckRelease:
	default:
		close(recheckRelease)
	}
	waitForAvailableUpdates(t, api, 0)
}

func waitForAvailableUpdates(t *testing.T, api *API, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		api.mu.Lock()
		count := 0
		if api.current != nil {
			count = api.current.AvailableUpdates
		}
		api.mu.Unlock()
		if count == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if api.current == nil {
		t.Fatal("availability check did not deliver a snapshot")
	}
	t.Fatalf("available updates = %d, want %d", api.current.AvailableUpdates, want)
}

func TestUserAPIUpdateAvailabilityCloseCancelsAndWaits(t *testing.T) {
	home, _ := newUserAPIFixture(t, true)
	checkStarted := make(chan struct{})
	checkCanceled := make(chan struct{})
	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{
		UpdateAvailability: func(ctx context.Context, _ pipeline.Workspace) (int, error) {
			close(checkStarted)
			<-ctx.Done()
			close(checkCanceled)
			return 0, ctx.Err()
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	api.Handler().ServeHTTP(httptest.NewRecorder(), authedRequest(t, api, "/api/snapshot", `{"checkUpdates":true}`))
	select {
	case <-checkStarted:
	case <-time.After(10 * time.Second):
		api.Close()
		t.Fatal("snapshot did not start the availability check")
	}
	api.Close()
	select {
	case <-checkCanceled:
	default:
		t.Fatal("API closed before the in-flight availability check was canceled")
	}
}
