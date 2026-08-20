package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

// writeUpdateTestConfigs lays down a v2 repo config without harnesses plus
// the given user-personal config, and swaps the four config loaders to read
// exactly those files. It returns repoRoot and homeDir.
func writeUpdateTestConfigs(t testing.TB, globalConfig string) (string, string) {
	t.Helper()
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	if err := os.MkdirAll(filepath.Join(repoRoot, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".agents", "config.yaml"), []byte(repoV2Header), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(homeDir, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	globalPath := filepath.Join(homeDir, ".agents", "config.yaml")
	if err := os.WriteFile(globalPath, []byte(globalConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) { return nil, nil })
	t.Cleanup(restoreOrg)
	restoreGlobal := swapLoadGlobalConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(globalPath)
	})
	t.Cleanup(restoreGlobal)
	restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(repoRoot, ".agents", "config.yaml"))
	})
	t.Cleanup(restoreRepo)
	restoreLocal := swapLoadLocalConfig(func() (*config.LocalConfig, error) {
		return config.LoadLocalConfigFromPath(filepath.Join(repoRoot, ".agents", "config.local.yaml"))
	})
	t.Cleanup(restoreLocal)
	return repoRoot, homeDir
}

func writeRegistrySkill(t testing.TB, registryRoot, name string) {
	t.Helper()
	skillDir := filepath.Join(registryRoot, "skills", name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + name + " skill\n---\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRunUpdateCmdRequiresTargetHarnessSet proves the TUI update runs the
// shared pipeline semantics: the machine-wide target harness set is a hard
// prerequisite (the bespoke update materialized without one).
func TestRunUpdateCmdRequiresTargetHarnessSet(t *testing.T) {
	repoRoot, homeDir := writeUpdateTestConfigs(t, `apiVersion: agent-marketplace/v2
registries: []
`)

	msg := runUpdateCmd(context.Background(), repoRoot, homeDir)()
	done, ok := msg.(updateDoneMsg)
	if !ok {
		t.Fatalf("runUpdateCmd returned %T, want updateDoneMsg", msg)
	}
	if done.err == nil {
		t.Fatal("update succeeded without a target harness set; the shared pipeline requires one")
	}
	if done.stage != updateStageRefresh {
		t.Fatalf("stage = %q, want %q (target-set failure is pre-materialization)", done.stage, updateStageRefresh)
	}
	var missing *pipeline.TargetHarnessSetMissingError
	if !errors.As(done.err, &missing) {
		t.Fatalf("update error = %v, want a TargetHarnessSetMissingError", done.err)
	}
}

// TestRunUpdateCmdEnsuresLocalConfigGitignored proves the update path keeps
// the shared pipeline's gitignore side effect for the personal-local layer.
func TestRunUpdateCmdEnsuresLocalConfigGitignored(t *testing.T) {
	repoRoot, homeDir := writeUpdateTestConfigs(t, v2Header+`registries: []
`)
	if err := os.WriteFile(filepath.Join(repoRoot, ".agents", "config.local.yaml"), []byte("registries: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	msg := runUpdateCmd(context.Background(), repoRoot, homeDir)()
	done, ok := msg.(updateDoneMsg)
	if !ok {
		t.Fatalf("runUpdateCmd returned %T, want updateDoneMsg", msg)
	}
	if done.err != nil {
		t.Fatalf("update failed: %v", done.err)
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, ".gitignore"))
	if err != nil {
		t.Fatalf("update did not maintain .gitignore for the local config layer: %v", err)
	}
	if !strings.Contains(string(data), ".agents/config.local.yaml") {
		t.Fatalf(".gitignore missing the local config entry:\n%s", data)
	}
}

// TestRunUpdateCmdCreatesBackupBeforeMaterializing proves the update path
// keeps the shared pipeline's pre-materialization backup behavior for
// machine-scope targets.
func TestRunUpdateCmdCreatesBackupBeforeMaterializing(t *testing.T) {
	registryRoot := t.TempDir()
	writeRegistrySkill(t, registryRoot, "keep")

	repoRoot, homeDir := writeUpdateTestConfigs(t, v2Header+`registries:
  - url: `+registryRoot+`
    skills:
      - keep
`)

	// A pre-existing machine-scope managed path is what gets archived.
	keepDir := filepath.Join(homeDir, ".factory", "skills", "keep")
	if err := os.MkdirAll(keepDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(keepDir, "SKILL.md"), []byte("old content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	msg := runUpdateCmd(context.Background(), repoRoot, homeDir)()
	done, ok := msg.(updateDoneMsg)
	if !ok {
		t.Fatalf("runUpdateCmd returned %T, want updateDoneMsg", msg)
	}
	if done.err != nil {
		t.Fatalf("update failed: %v", done.err)
	}
	archives, err := filepath.Glob(filepath.Join(homeDir, ".agent-marketplace", "backups", "factory", "*.tar.gz"))
	if err != nil || len(archives) == 0 {
		t.Fatalf("update produced no pre-materialization backup archive: archives=%v err=%v", archives, err)
	}
}

// TestRunUpdateCmdStaleCacheFallbackIsTypedPartialOutcome pins the typed
// stale-cache contract: when a registry fetch fails and the pipeline falls
// back to the warm cache, update must complete as a PARTIAL outcome carrying
// a typed warning — never as a clean success.
func TestRunUpdateCmdStaleCacheFallbackIsTypedPartialOutcome(t *testing.T) {
	cacheDir := t.TempDir()
	writeRegistrySkill(t, cacheDir, "alpha")

	repoRoot, homeDir := writeUpdateTestConfigs(t, v2Header+`registries:
  - url: https://example.com/org/registry.git
    ref: main
    skills:
      - alpha
`)

	restore := swapCloneOrPullDetailed(func(url, ref string) (string, *cache.StaleCacheFallbackWarning, error) {
		return cacheDir, &cache.StaleCacheFallbackWarning{URL: url, Ref: ref, Err: errors.New("network unreachable")}, nil
	})
	defer restore()
	// The post-operation snapshot reloads in cached mode; after the sync
	// refresh (or its stale fallback) the cache is warm, so the plain seam
	// must serve the same warmed content.
	restorePlain := swapCloneOrPull(func(url, ref string) (string, error) {
		return cacheDir, nil
	})
	defer restorePlain()

	msg := runUpdateCmd(context.Background(), repoRoot, homeDir)()
	done, ok := msg.(updateDoneMsg)
	if !ok {
		t.Fatalf("runUpdateCmd returned %T, want updateDoneMsg", msg)
	}
	if done.err != nil {
		t.Fatalf("update with a warm stale cache should complete partially, not fail: %v", done.err)
	}
	if len(done.staleCaches) != 1 {
		t.Fatalf("stale-cache fallbacks = %v, want exactly one typed warning", done.staleCaches)
	}
	var typed *cache.StaleCacheFallbackWarning
	if !errors.As(done.staleCaches[0], &typed) {
		t.Fatalf("stale-cache fallback is not a typed *cache.StaleCacheFallbackWarning: %T %v", done.staleCaches[0], done.staleCaches[0])
	}

	// The post-operation snapshot carries the typed warning as a warning
	// diagnostic, and the stale cache was still materialized.
	foundWarning := false
	for _, diagnostic := range done.snapshot.diagnostics {
		if diagnostic.severity == diagnosticWarning && errors.As(diagnostic.err, &typed) {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Fatalf("stale-cache warning missing from post-operation snapshot diagnostics: %+v", done.snapshot.diagnostics)
	}
	if _, err := os.Lstat(filepath.Join(homeDir, ".factory", "skills", "alpha")); err != nil {
		t.Fatalf("stale cache content was not materialized: %v", err)
	}

	// The handler reports a partial outcome, never the full success copy.
	m := NewModel(repoRoot, homeDir)
	seedTestTerminalSize(&m)
	m.loading = false
	m.syncing = true
	done.snapshot.generation = m.requests.active(requestSnapshot)
	updated, _ := m.Update(done)
	m = updated.(Model)
	if strings.Contains(m.toast, "Update refresh and materialization complete") {
		t.Fatalf("partial update claimed the full success message: %q", m.toast)
	}
	if !strings.Contains(strings.ToLower(m.toast), "stale") {
		t.Fatalf("partial update toast does not mention the stale cache: %q", m.toast)
	}
}

// TestRunUpdateCmdMaterializationFailureSurfacesPostOperationSnapshot drives
// a deterministic materialization failure (Devin refuses file-mode
// materialization) and pins: the failure is attributed to the
// materialization stage, and the post-operation snapshot is still produced
// and applied — rows become inspection-only, drift reports failed.
func TestRunUpdateCmdMaterializationFailureSurfacesPostOperationSnapshot(t *testing.T) {
	registryRoot := t.TempDir()
	writeRegistrySkill(t, registryRoot, "alpha")

	repoRoot, homeDir := writeUpdateTestConfigs(t, `apiVersion: agent-marketplace/v2
harnesses:
  - devin
materialization:
  mode: file
registries:
  - url: `+registryRoot+`
    skills:
      - alpha
`)

	msg := runUpdateCmd(context.Background(), repoRoot, homeDir)()
	done, ok := msg.(updateDoneMsg)
	if !ok {
		t.Fatalf("runUpdateCmd returned %T, want updateDoneMsg", msg)
	}
	if done.err == nil {
		t.Fatal("Devin file-mode update must fail; it was silently degraded")
	}
	if done.stage != updateStageMaterialization {
		t.Fatalf("stage = %q, want %q", done.stage, updateStageMaterialization)
	}
	if !strings.Contains(done.err.Error(), "materialize.devin.symlink-required") {
		t.Fatalf("update error lost the materialization diagnostic identity: %v", done.err)
	}

	m := NewModel(repoRoot, homeDir)
	seedTestTerminalSize(&m)
	m.loading = false
	m.syncing = true
	done.snapshot.generation = m.requests.active(requestSnapshot)
	updated, _ := m.Update(done)
	m = updated.(Model)
	if !m.rowsStale {
		t.Fatal("rows stayed mutable after a snapshot carrying hard materialization errors")
	}
	if m.driftAssessment != driftFailed {
		t.Fatalf("drift assessment = %v, want failed", m.driftAssessment)
	}
	if m.toast == "Update refresh and materialization complete" {
		t.Fatal("failed update claimed success")
	}
	if len(m.diagnostics.bySeverity(diagnosticError)) == 0 {
		t.Fatal("update failure was not recorded as an error diagnostic")
	}
}

// TestUpdateDoneMsgStaleCompletionSchedulesCurrentSnapshot pins the stale
// half of the update contract: a completion whose snapshot generation is no
// longer current applies nothing, clears nothing, claims nothing — it
// schedules a fresh snapshot so the post-update disk state is captured by
// the current generation.
func TestUpdateDoneMsgStaleCompletionSchedulesCurrentSnapshot(t *testing.T) {
	// Shadow HOME so the scheduled snapshot's dry-run materialize sees a
	// clean, deterministic machine scope instead of the host's.
	t.Setenv("HOME", t.TempDir())
	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) { return nil, nil })
	defer restoreOrg()
	restoreGlobal := swapLoadGlobalConfig(func() (*config.Config, error) { return nil, nil })
	defer restoreGlobal()
	restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) { return nil, nil })
	defer restoreRepo()
	restoreLocal := swapLoadLocalConfig(func() (*config.LocalConfig, error) { return nil, nil })
	defer restoreLocal()

	m := newTestModel()
	m.loading = false
	m.syncing = true
	originalRows := m.rows
	m.driftAssessment = driftReady
	m.diagnostics.recordOperationFailure(operationUpdate, diagnosticUpdate, errors.New("earlier update failure"))
	m.toast = "existing toast"

	staleGeneration := m.requests.active(requestSnapshot)
	// A newer request supersedes the update's snapshot while it runs.
	currentGeneration := m.beginRequest(requestSnapshot)

	updated, cmd := m.Update(updateDoneMsg{
		snapshot: snapshotLoadedMsg{generation: staleGeneration, mergedCfg: &config.Config{}},
	})
	m = updated.(Model)

	if m.syncing {
		t.Fatal("stale update completion left syncing set")
	}
	if len(m.rows) != len(originalRows) {
		t.Fatalf("stale update replaced rows: %d -> %d", len(originalRows), len(m.rows))
	}
	if got := len(m.diagnostics.bySeverity(diagnosticError)); got != 1 {
		t.Fatalf("stale update completion altered retained errors: %d", got)
	}
	if m.toast != "existing toast" {
		t.Fatalf("stale update completion claimed or cleared the toast: %q", m.toast)
	}
	if cmd == nil {
		t.Fatal("stale update completion did not schedule a current snapshot")
	}
	freshMsg, ok := cmd().(snapshotLoadedMsg)
	if !ok {
		t.Fatalf("stale update completion scheduled %T, want a snapshot load", cmd())
	}
	if got := m.requests.active(requestSnapshot); got <= currentGeneration {
		t.Fatalf("no newer snapshot generation scheduled: active=%d current=%d", got, currentGeneration)
	}

	// Deterministic reload ordering: the freshly scheduled snapshot is what
	// lands next, replacing rows and the drift verdict; the retained update
	// failure is operation-provenance and survives until a real update
	// success supersedes it.
	updated, _ = m.Update(freshMsg)
	m = updated.(Model)
	if len(m.rows) != 0 {
		t.Fatalf("fresh snapshot did not replace rows: %+v", m.rows)
	}
	if m.rowsStale {
		t.Fatal("fresh clean snapshot left rows marked stale")
	}
	if got := len(m.diagnostics.bySeverity(diagnosticError)); got != 1 {
		t.Fatalf("fresh snapshot erased the retained update failure: %d errors", got)
	}
	if m.toast != "existing toast" {
		t.Fatalf("fresh snapshot changed the toast: %q", m.toast)
	}
}

// TestUpdateDoneMsgSuccessReportedOnlyAfterSnapshotAccepted pins the
// ordering contract: the success copy is produced in the same transition
// that applies the accepted post-operation snapshot — and only then.
func TestUpdateDoneMsgSuccessReportedOnlyAfterSnapshotAccepted(t *testing.T) {
	m := newTestModel()
	m.loading = false
	m.syncing = true
	m.diagnostics.recordOperationFailure(operationUpdate, diagnosticUpdate, errors.New("earlier update failure"))

	newRows := []row{mkRow("fresh", withGlobal, withRegistry("sec.git", "security"))}
	updated, _ := m.Update(updateDoneMsg{
		snapshot: snapshotLoadedMsg{
			generation:      m.requests.active(requestSnapshot),
			rows:            newRows,
			mergedCfg:       &config.Config{},
			driftAssessment: driftReady,
		},
	})
	m = updated.(Model)

	if len(m.rows) != 1 || m.rows[0].Artifact.Name != "fresh" {
		t.Fatalf("accepted post-operation snapshot was not applied: %+v", m.rows)
	}
	if m.toast != "Update refresh and materialization complete" {
		t.Fatalf("success copy = %q", m.toast)
	}
	if got := len(m.diagnostics.bySeverity(diagnosticError)); got != 0 {
		t.Fatalf("accepted update success did not supersede the earlier failure: %d errors retained", got)
	}
	if m.syncing {
		t.Fatal("completed update left syncing set")
	}
}

// TestUpdateFailureWithBrokenConfigPreservesRowsForInspection pins parity
// with the plain reload path: when the post-operation snapshot carries no
// merged config plus hard errors, the previous rows stay on screen for
// inspection — stale, so still not mutable — while the new drift assessment,
// diagnostics, and the update failure all land.
func TestUpdateFailureWithBrokenConfigPreservesRowsForInspection(t *testing.T) {
	m := newTestModel()
	m.loading = false
	m.syncing = true
	originalRows := m.rows

	configErr := errors.New("invalid config")
	updated, _ := m.Update(updateDoneMsg{
		snapshot: snapshotLoadedMsg{
			generation:      m.requests.active(requestSnapshot),
			driftAssessment: driftUnknown,
			diagnostics:     []tuiDiagnostic{snapshotDiagnostic(diagnosticError, diagnosticConfig, configErr)},
		},
		stage: updateStageRefresh,
		err:   errors.New("update refresh failed: " + configErr.Error()),
	})
	m = updated.(Model)

	if len(m.rows) != len(originalRows) {
		t.Fatalf("broken-config update failure replaced rows: %d -> %d", len(originalRows), len(m.rows))
	}
	if !m.rowsStale {
		t.Fatal("rows stayed mutable after a broken-config update failure")
	}
	if m.driftAssessment != driftUnknown {
		t.Fatalf("drift assessment = %v, want unavailable", m.driftAssessment)
	}
	foundConfigError := false
	for _, diagnostic := range m.diagnostics.bySeverity(diagnosticError) {
		if errors.Is(diagnostic.err, configErr) {
			foundConfigError = true
		}
	}
	if !foundConfigError {
		t.Fatalf("config error diagnostic missing: %+v", m.diagnostics.all())
	}
	if m.toast == "Update refresh and materialization complete" {
		t.Fatal("failed update claimed success")
	}
}

// TestUpdateThenSyncAndQuitQueuesSyncAndQuitsOnSyncResult is the state-machine
// test for the u → q → y intent: the confirmed sync cannot run concurrently
// with the in-flight update, so it is queued when the update completes, and
// the program quits on THAT sync's result — never on the update result.
func TestUpdateThenSyncAndQuitQueuesSyncAndQuitsOnSyncResult(t *testing.T) {
	origUpdateCmd, origSyncCmd := runUpdateCmd, runSyncCmd
	runUpdateCmd = func(_ context.Context, repoRoot, homeDir string, generations ...requestGeneration) tea.Cmd {
		return func() tea.Msg {
			return updateDoneMsg{snapshot: snapshotLoadedMsg{generation: generations[0], mergedCfg: &config.Config{}}}
		}
	}
	runSyncCmd = noopSyncCmd
	defer func() { runUpdateCmd, runSyncCmd = origUpdateCmd, origSyncCmd }()

	m := newTestModel()
	m.loading = false
	m.driftAssessment = driftReady
	m.driftReport = materialize.DriftReport{Missing: []materialize.DriftItem{{Name: "a", Reason: materialize.DriftMissing}}}

	// u: update starts.
	updated, cmd := m.Update(keyPress('u'))
	m = updated.(Model)
	if !m.syncing || cmd == nil {
		t.Fatalf("u did not start the update: syncing=%t cmd=%v", m.syncing, cmd)
	}
	updateCmds := batchCmds(cmd)
	if len(updateCmds) == 0 {
		t.Fatal("u returned no update command batch")
	}

	// q: pending drift requires the sync-and-quit confirmation.
	updated, cmd = m.Update(keyPress('q'))
	m = updated.(Model)
	if m.pane != paneConfirm || cmd != nil {
		t.Fatalf("q during update did not open the confirmation: pane=%v cmd=%v", m.pane, cmd)
	}

	// y: confirm sync+quit. The update is still in flight, so no sync
	// starts yet and nothing quits.
	updated, cmd = m.Update(keyPress('y'))
	m = updated.(Model)
	if !m.quitAfterSync || cmd != nil {
		t.Fatalf("y did not record the sync+quit intent: quitAfterSync=%t cmd=%v", m.quitAfterSync, cmd)
	}

	// The update completes successfully (driven through the stubbed
	// operation command captured at 'u'). The confirmed sync must now be
	// queued; the program must NOT quit on the update result.
	updateResult, ok := updateCmds[0]().(updateDoneMsg)
	if !ok {
		t.Fatalf("update command produced %T, want updateDoneMsg", updateCmds[0]())
	}
	updated, cmd = m.Update(updateResult)
	m = updated.(Model)
	if !m.quitAfterSync {
		t.Fatal("update completion dropped the confirmed sync+quit intent")
	}
	if !m.syncing {
		t.Fatal("update completion did not queue the confirmed sync")
	}
	if anyCmdProducesQuitMsg(cmd) {
		t.Fatal("update completion quit before the confirmed sync ran")
	}
	cmds := batchCmds(cmd)
	if len(cmds) == 0 {
		t.Fatal("update completion returned no sync command")
	}
	syncMsg, ok := cmds[0]().(syncDoneMsg)
	if !ok {
		t.Fatalf("queued command produced %T, want syncDoneMsg", cmds[0]())
	}

	// The sync result is what quits the program.
	updated, cmd = m.Update(syncMsg)
	m = updated.(Model)
	if !cmdProducesQuitMsg(cmd) {
		t.Fatal("confirmed sync completion did not quit")
	}
	if m.syncing {
		t.Fatal("sync completion left syncing set")
	}
}

// TestUpdateFailureStillQueuesConfirmedSyncAndQuitsOnSyncResult pins the
// failure half of u → q → y: even when the update fails, the user confirmed
// "sync and quit", so the sync runs and its own result decides the exit.
func TestUpdateFailureStillQueuesConfirmedSyncAndQuitsOnSyncResult(t *testing.T) {
	m := newTestModel()
	m.loading = false
	m.syncing = true
	m.quitAfterSync = true
	generation := m.requests.active(requestSnapshot)

	updated, cmd := m.Update(updateDoneMsg{
		snapshot: snapshotLoadedMsg{generation: generation, mergedCfg: &config.Config{}},
		stage:    updateStageRefresh,
		err:      errors.New("update refresh failed"),
	})
	m = updated.(Model)
	if !m.quitAfterSync || !m.syncing {
		t.Fatalf("failed update did not queue the confirmed sync: quitAfterSync=%t syncing=%t", m.quitAfterSync, m.syncing)
	}
	if anyCmdProducesQuitMsg(cmd) {
		t.Fatal("failed update quit before the confirmed sync ran")
	}

	syncErr := errors.New("sync failed")
	updated, cmd = m.Update(syncDoneMsg{err: syncErr})
	m = updated.(Model)
	if !cmdProducesQuitMsg(cmd) {
		t.Fatal("confirmed sync failure did not quit")
	}
	if !errors.Is(m.syncQuitErr, syncErr) {
		t.Fatalf("sync+quit exit did not propagate the sync result: %v", m.syncQuitErr)
	}
}
