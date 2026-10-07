package tui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/localapi"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/session"
)

type localAPIGateFixture struct {
	api        *localapi.API
	gate       *session.OperationGate
	model      Model
	configPath string
}

func newLocalAPIGateFixture(t *testing.T, lifecycle session.DependencyLifecycle) localAPIGateFixture {
	t.Helper()
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
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	if err := os.WriteFile(configPath, []byte("apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  local: {directory: "+source+"}\nresources:\n  local: {all: true}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".agents", "config.yaml"), []byte("apiVersion: agent-marketplace/v4\nharnesses: [devin]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	config.SetTestOrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
	workspace, err := pipeline.NewWorkspace(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	gate := session.NewOperationGate()
	api, err := localapi.New(context.Background(), workspace, home, lifecycle, gate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	model := NewModel(repo, home, WithWorkspace(workspace), WithDependencyLifecycle(lifecycle), WithOperationGate(gate))
	model.loading = false
	return localAPIGateFixture{api: api, gate: gate, model: model, configPath: configPath}
}

func (f localAPIGateFixture) post(t *testing.T, path, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("X-Agent-Marketplace-Token", f.api.Credential())
	recorder := httptest.NewRecorder()
	f.api.Handler().ServeHTTP(recorder, req)
	var response struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return recorder.Code, response.Code
}

func (f localAPIGateFixture) seedSnapshot(t *testing.T) {
	t.Helper()
	if status, _ := f.post(t, "/api/snapshot", ""); status != http.StatusOK {
		t.Fatalf("seeding snapshot = %d, want 200", status)
	}
}

func useReadyUpdateSnapshot(t *testing.T) {
	t.Helper()
	previous := readSnapshotForUpdate
	readSnapshotForUpdate = func(context.Context, pipeline.Workspace, string, catalogLoadMode) snapshotLoadedMsg {
		return snapshotLoadedMsg{stage: snapshotStageFinal, mergedCfg: &config.Config{}, driftAssessment: driftReady}
	}
	t.Cleanup(func() { readSnapshotForUpdate = previous })
}

func firstBatchCommand(t *testing.T, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	commands := batchCmds(cmd)
	if len(commands) == 0 || commands[0] == nil {
		t.Fatal("operation did not return its lifecycle command")
	}
	return commands[0]
}

// spec: gui-local-api/concurrent-mutation-is-rejected-as-busy
func TestLocalAPISharesOperationGateWithTUI(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	entered := make(chan struct{})
	var enteredOnce sync.Once
	lifecycle := session.DependencyLifecycle{
		Sync: func(context.Context, pipeline.Workspace) ([]error, error) {
			enteredOnce.Do(func() { close(entered) })
			<-release
			return nil, nil
		},
		Update: func(context.Context, pipeline.Workspace) ([]error, error) { return nil, nil },
	}
	fixture := newLocalAPIGateFixture(t, lifecycle)
	model := fixture.model

	operation, ok := model.beginOperation(operationMutation)
	if !ok {
		t.Fatal("TUI operation was blocked")
	}
	if status, code := fixture.post(t, "/api/actions", `{"action":"sync"}`); status != http.StatusConflict || code != "localapi.busy" {
		t.Fatalf("API action during TUI operation = %d %q, want 409 localapi.busy", status, code)
	}
	model.finishOperation(operation)

	fixture.seedSnapshot(t)
	if status, _ := fixture.post(t, "/api/actions", `{"action":"sync"}`); status != http.StatusAccepted {
		t.Fatalf("API action after TUI release = %d, want 202", status)
	}
	<-entered
	if _, ok := model.beginOperation(operationMutation); ok {
		t.Fatal("TUI operation started while the API held the shared gate")
	}

	model.finishOperation(operationID(1 << 30))
	if _, _, active := fixture.gate.Active(); !active {
		t.Fatal("stale TUI completion released the API's gate reservation")
	}
	if _, ok := model.beginOperation(operationMutation); ok {
		t.Fatal("TUI operation started after a stale completion")
	}

	unblock()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, active := fixture.gate.Active(); !active {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, _, active := fixture.gate.Active(); active {
		t.Fatal("API operation never released the gate")
	}
	if _, ok := model.beginOperation(operationMutation); !ok {
		t.Fatal("TUI operation blocked after the API operation released the gate")
	}
}

func assertAPIBusy(t *testing.T, fixture localAPIGateFixture, action string) {
	t.Helper()
	status, code := fixture.post(t, "/api/actions", `{"action":"`+action+`"}`)
	if status != http.StatusConflict || code != "localapi.busy" {
		t.Fatalf("API %s during a shared operation = %d %q, want 409 localapi.busy", action, status, code)
	}
}

func assertGateActive(t *testing.T, gate *session.OperationGate, want session.OperationKind) operationID {
	t.Helper()
	id, kind, active := gate.Active()
	if !active || kind != want {
		t.Fatalf("shared gate = (%d, %q, %v), want active %q", id, kind, active, want)
	}
	return id
}

func assertBlockedRouteCommand(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("busy route returned no command")
	}
	result := cmd()
	if batch, ok := result.(tea.BatchMsg); ok {
		if len(batch) > 0 && batch[0] != nil {
			batch[0]()
		}
		t.Errorf("busy route scheduled %d operation commands instead of a blocked toast", len(batch))
		return
	}
	toast, ok := result.(toastMsg)
	if !ok || toast.text != "another operation is in progress" {
		t.Errorf("busy route command = %#v, want operation-blocked toast", result)
	}
}

// spec: gui-local-api/concurrent-mutation-is-rejected-as-busy
func TestTUIDependencyRoutesRejectWhenSharedGateBusy(t *testing.T) {
	for _, route := range []struct {
		name    string
		action  rune
		confirm bool
	}{
		{name: "sync", action: 's'},
		{name: "update", action: 'u'},
		{name: "confirmed sync+quit", confirm: true},
	} {
		t.Run(route.name, func(t *testing.T) {
			configPath := ""
			var syncCalls, updateCalls atomic.Int32
			writeConfig := func(calls *atomic.Int32) session.DependencyLifecycleFunc {
				return func(context.Context, pipeline.Workspace) ([]error, error) {
					calls.Add(1)
					return nil, os.WriteFile(configPath, []byte("unexpected lifecycle write"), 0o644)
				}
			}
			fixture := newLocalAPIGateFixture(t, session.DependencyLifecycle{
				Sync:   writeConfig(&syncCalls),
				Update: writeConfig(&updateCalls),
			})
			configPath = fixture.configPath
			useReadyUpdateSnapshot(t)
			beforeBytes, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}

			model := fixture.model
			model.driftAssessment = driftFailed
			if route.confirm {
				model.pane = paneConfirm
				model.driftReport = materialize.DriftReport{Missing: []materialize.DriftItem{{Name: "solo", Reason: materialize.DriftMissing}}}
			}
			beforeRequests := model.requests
			beforeProgress := model.progress
			beforeProgressLabel := model.progressLabel
			beforePhase := model.operationPhase
			beforeOperationNext := model.operations.Next
			beforeHadLocalOperation := model.operations.Active != nil
			beforeExternalOperation := model.externalOperation
			beforePane := model.pane
			beforeSyncing := model.syncing
			beforeRefreshing := model.refreshing
			beforeQuitAfterSync := model.quitAfterSync

			held, ok := fixture.gate.Begin(session.OperationMutation)
			if !ok {
				t.Fatal("could not hold shared gate")
			}
			defer fixture.gate.Finish(held)

			var updated tea.Model
			var cmd tea.Cmd
			if route.confirm {
				updated, cmd = model.confirmSyncQuitYes()
			} else {
				updated, cmd = model.Update(keyPress(route.action))
			}
			got := updated.(Model)
			if got.syncing != beforeSyncing || got.refreshing != beforeRefreshing || got.quitAfterSync != beforeQuitAfterSync || got.pane != beforePane {
				t.Errorf("busy route changed TUI state: syncing=%t refreshing=%t quitAfterSync=%t pane=%v", got.syncing, got.refreshing, got.quitAfterSync, got.pane)
			}
			if got.requests != beforeRequests || got.progress != beforeProgress || got.progressLabel != beforeProgressLabel || got.operationPhase != beforePhase {
				t.Errorf("busy route changed request/progress state: requests=%+v progress=%p label=%q phase=%q", got.requests, got.progress, got.progressLabel, got.operationPhase)
			}
			if got.operations.Next != beforeOperationNext || (got.operations.Active != nil) != beforeHadLocalOperation || got.externalOperation != beforeExternalOperation {
				t.Errorf("busy route changed local operation state: next=%d active=%t external=%d", got.operations.Next, got.operations.Active != nil, got.externalOperation)
			}
			assertBlockedRouteCommand(t, cmd)
			if syncCalls.Load() != 0 || updateCalls.Load() != 0 {
				t.Errorf("busy route invoked lifecycle callbacks: sync=%d update=%d", syncCalls.Load(), updateCalls.Load())
			}
			afterBytes, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(afterBytes) != string(beforeBytes) {
				t.Errorf("busy route changed config bytes: %q", afterBytes)
			}
			if id, kind, active := fixture.gate.Active(); !active || id != held || kind != session.OperationMutation {
				t.Errorf("busy route changed held gate: (%d, %q, %v)", id, kind, active)
			}
		})
	}
}

// spec: gui-local-api/concurrent-mutation-is-rejected-as-busy
func TestTUISyncGateHeldThroughAuthoritativeReload(t *testing.T) {
	for _, failReload := range []bool{false, true} {
		name := "accepted reload"
		if failReload {
			name = "failing reload"
		}
		t.Run(name, func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			var enteredOnce sync.Once
			var syncCalls atomic.Int32
			lifecycle := session.DependencyLifecycle{
				Sync: func(context.Context, pipeline.Workspace) ([]error, error) {
					syncCalls.Add(1)
					enteredOnce.Do(func() { close(entered) })
					<-release
					return nil, nil
				},
				Update: func(context.Context, pipeline.Workspace) ([]error, error) { return nil, nil },
			}
			fixture := newLocalAPIGateFixture(t, lifecycle)
			fixture.seedSnapshot(t)
			model := fixture.model
			model.driftAssessment = driftFailed

			updated, cmd := model.Update(keyPress('s'))
			model = updated.(Model)
			assertGateActive(t, fixture.gate, session.OperationSync)
			assertAPIBusy(t, fixture, "sync")
			commands := batchCmds(cmd)
			if len(commands) == 0 || commands[0] == nil {
				t.Fatal("TUI sync did not schedule its lifecycle command")
			}
			messages := runCmdsAsync([]tea.Cmd{commands[0]})
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("TUI sync callback did not start")
			}
			if syncCalls.Load() != 1 {
				t.Fatalf("sync callback calls = %d, want 1", syncCalls.Load())
			}
			assertAPIBusy(t, fixture, "update")
			unblock()

			var done syncDoneMsg
			select {
			case msg := <-messages:
				var ok bool
				done, ok = msg.(syncDoneMsg)
				if !ok {
					t.Fatalf("TUI sync command returned %T, want syncDoneMsg", msg)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("TUI sync callback did not complete")
			}
			updated, _ = model.Update(done)
			model = updated.(Model)
			if model.syncReload == 0 || !model.syncing {
				t.Fatalf("sync completion did not keep the operation through reload: reload=%d syncing=%t", model.syncReload, model.syncing)
			}
			assertGateActive(t, fixture.gate, session.OperationSync)
			assertAPIBusy(t, fixture, "sync")

			reload := snapshotLoadedMsg{generation: model.syncReload, stage: snapshotStageFinal}
			if failReload {
				reload.errs = []error{errors.New("authoritative reload failed")}
				reload.driftAssessment = driftFailed
			} else {
				reload.mergedCfg = &config.Config{}
				reload.driftAssessment = driftReady
			}
			updated, _ = model.Update(reload)
			model = updated.(Model)
			if _, _, active := fixture.gate.Active(); active {
				t.Fatal("sync reload settlement left the shared gate active")
			}
			if model.syncing || model.syncReload != 0 {
				t.Fatalf("sync reload settlement left state active: syncing=%t reload=%d", model.syncing, model.syncReload)
			}
		})
	}
}

// spec: gui-local-api/concurrent-mutation-is-rejected-as-busy
func TestTUIUpdateDirectFailureReleasesSharedGateForHealthyRetry(t *testing.T) {
	useReadyUpdateSnapshot(t)
	var calls atomic.Int32
	failure := errors.New("one-time update failure")
	lifecycle := session.DependencyLifecycle{
		Sync: func(context.Context, pipeline.Workspace) ([]error, error) { return nil, nil },
		Update: func(context.Context, pipeline.Workspace) ([]error, error) {
			if calls.Add(1) == 1 {
				return nil, failure
			}
			return nil, nil
		},
	}
	fixture := newLocalAPIGateFixture(t, lifecycle)
	fixture.seedSnapshot(t)
	model := fixture.model
	model.driftAssessment = driftReady

	for attempt := int32(1); attempt <= 2; attempt++ {
		updated, cmd := model.Update(keyPress('u'))
		model = updated.(Model)
		assertGateActive(t, fixture.gate, session.OperationUpdate)
		if attempt == 1 {
			assertAPIBusy(t, fixture, "update")
			if calls.Load() != 0 {
				t.Fatalf("update callback started before its command: %d", calls.Load())
			}
		}
		done, ok := firstBatchCommand(t, cmd)().(updateDoneMsg)
		if !ok {
			t.Fatal("update command did not return updateDoneMsg")
		}
		if attempt == 1 && !errors.Is(done.err, failure) {
			t.Fatalf("first update error = %v, want %v", done.err, failure)
		}
		if attempt == 2 && done.err != nil {
			t.Fatalf("healthy update retry failed: %v", done.err)
		}
		updated, _ = model.Update(done)
		model = updated.(Model)
		if _, _, active := fixture.gate.Active(); active {
			t.Fatalf("update attempt %d left shared gate active", attempt)
		}
		if model.syncing {
			t.Fatalf("update attempt %d left TUI syncing", attempt)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("update callbacks = %d, want initial failure plus healthy retry", calls.Load())
	}
}

// spec: gui-local-api/concurrent-mutation-is-rejected-as-busy
func TestTUIStaleUpdateKeepsSharedGateUntilSupersedingReloadSettles(t *testing.T) {
	for _, failReload := range []bool{false, true} {
		name := "accepted superseding reload"
		if failReload {
			name = "failing superseding reload"
		}
		t.Run(name, func(t *testing.T) {
			useReadyUpdateSnapshot(t)
			var updateCalls atomic.Int32
			fixture := newLocalAPIGateFixture(t, session.DependencyLifecycle{
				Sync: func(context.Context, pipeline.Workspace) ([]error, error) { return nil, nil },
				Update: func(context.Context, pipeline.Workspace) ([]error, error) {
					updateCalls.Add(1)
					return nil, nil
				},
			})
			fixture.seedSnapshot(t)
			model := fixture.model
			model.driftAssessment = driftReady

			updated, cmd := model.Update(keyPress('u'))
			model = updated.(Model)
			assertGateActive(t, fixture.gate, session.OperationUpdate)
			assertAPIBusy(t, fixture, "update")
			updateGeneration := model.requests.Active(requestSnapshot)
			updated, reloadCmd := model.Update(keyPress('r'))
			model = updated.(Model)
			if reloadCmd == nil {
				t.Fatal("reload key did not issue the superseding snapshot request")
			}
			supersedingGeneration := model.requests.Active(requestSnapshot)
			if supersedingGeneration <= updateGeneration {
				t.Fatalf("reload generation = %d, want newer than update generation %d", supersedingGeneration, updateGeneration)
			}

			done, ok := firstBatchCommand(t, cmd)().(updateDoneMsg)
			if !ok {
				t.Fatal("update command did not return updateDoneMsg")
			}
			updated, _ = model.Update(done)
			model = updated.(Model)
			if model.updateReload == 0 || model.updateReload <= supersedingGeneration || !model.syncing {
				t.Fatalf("stale update did not retain a newer authoritative reload: updateReload=%d superseding=%d syncing=%t", model.updateReload, supersedingGeneration, model.syncing)
			}
			assertGateActive(t, fixture.gate, session.OperationUpdate)
			assertAPIBusy(t, fixture, "sync")
			if updateCalls.Load() != 1 {
				t.Fatalf("update callbacks = %d, want 1", updateCalls.Load())
			}

			stale := snapshotLoadedMsg{generation: supersedingGeneration, stage: snapshotStageFinal, mergedCfg: &config.Config{}, driftAssessment: driftReady}
			updated, _ = model.Update(stale)
			model = updated.(Model)
			assertGateActive(t, fixture.gate, session.OperationUpdate)
			if model.updateReload == 0 || !model.syncing {
				t.Fatal("stale reload released the update gate")
			}

			settled := snapshotLoadedMsg{generation: model.updateReload, stage: snapshotStageFinal}
			if failReload {
				settled.errs = []error{errors.New("superseding reload failed")}
				settled.driftAssessment = driftFailed
			} else {
				settled.mergedCfg = &config.Config{}
				settled.driftAssessment = driftReady
			}
			updated, _ = model.Update(settled)
			model = updated.(Model)
			if _, _, active := fixture.gate.Active(); active {
				t.Fatal("settled superseding reload left shared gate active")
			}
			if model.syncing || model.updateReload != 0 {
				t.Fatalf("settled update left state active: syncing=%t reload=%d", model.syncing, model.updateReload)
			}
		})
	}
}

// spec: session-interactions/confirmation-retains-all-user-choices
// spec: gui-local-api/concurrent-mutation-is-rejected-as-busy
func TestConfirmedSyncQuitReleasesSharedGateOnCompletion(t *testing.T) {
	for _, syncErr := range []error{nil, errors.New("sync failed")} {
		name := "successful sync"
		if syncErr != nil {
			name = "failed sync"
		}
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			fixture := newLocalAPIGateFixture(t, session.DependencyLifecycle{
				Sync: func(context.Context, pipeline.Workspace) ([]error, error) {
					calls.Add(1)
					return nil, syncErr
				},
				Update: func(context.Context, pipeline.Workspace) ([]error, error) { return nil, nil },
			})
			fixture.seedSnapshot(t)
			model := fixture.model
			model.pane = paneConfirm
			model.driftReport = materialize.DriftReport{Missing: []materialize.DriftItem{{Name: "solo", Reason: materialize.DriftMissing}}}

			updated, cmd := model.confirmSyncQuitYes()
			model = updated.(Model)
			assertGateActive(t, fixture.gate, session.OperationSync)
			if !model.syncing || !model.quitAfterSync {
				t.Fatal("confirmed sync+quit did not preserve its running state")
			}
			assertAPIBusy(t, fixture, "sync")
			done, ok := firstBatchCommand(t, cmd)().(syncDoneMsg)
			if !ok {
				t.Fatal("sync+quit command did not return syncDoneMsg")
			}
			if !errors.Is(done.err, syncErr) {
				t.Fatalf("sync+quit result error = %v, want %v", done.err, syncErr)
			}
			updated, quitCmd := model.Update(done)
			model = updated.(Model)
			if !cmdProducesQuitMsg(quitCmd) {
				t.Fatal("sync+quit completion did not quit")
			}
			if _, _, active := fixture.gate.Active(); active {
				t.Fatal("sync+quit completion left shared gate active")
			}
			if model.syncing || model.quitAfterSync || calls.Load() != 1 {
				t.Fatalf("sync+quit completion state: syncing=%t quit=%t calls=%d", model.syncing, model.quitAfterSync, calls.Load())
			}
		})
	}
}
