package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestDependencyLifecycleCallbacksAreInvoked(t *testing.T) {
	var syncCalls, updateCalls atomic.Int64
	model := NewModel("/repo", "/home", WithDependencyLifecycle(DependencyLifecycle{
		Sync: func(context.Context, pipeline.Workspace) ([]error, error) {
			syncCalls.Add(1)
			return nil, nil
		},
		Update: func(context.Context, pipeline.Workspace) ([]error, error) {
			updateCalls.Add(1)
			return nil, nil
		},
	}))

	if msg := model.dependencySyncCmd()(); msg.(syncDoneMsg).err != nil {
		t.Fatalf("sync callback failed: %v", msg.(syncDoneMsg).err)
	}
	previousSnapshot := readSnapshotForUpdate
	readSnapshotForUpdate = func(context.Context, pipeline.Workspace, string, catalogLoadMode) snapshotLoadedMsg {
		return snapshotLoadedMsg{driftAssessment: driftReady}
	}
	t.Cleanup(func() { readSnapshotForUpdate = previousSnapshot })
	if msg := model.dependencyUpdateCmd(7)(); msg.(updateDoneMsg).err != nil {
		t.Fatalf("update callback failed: %v", msg.(updateDoneMsg).err)
	}
	if syncCalls.Load() != 1 || updateCalls.Load() != 1 {
		t.Fatalf("lifecycle calls = sync %d, update %d; want one each", syncCalls.Load(), updateCalls.Load())
	}
}

func TestDependencyLifecycleReceivesModelWorkspace(t *testing.T) {
	repo := t.TempDir()
	nested := filepath.Join(repo, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	workspace, err := pipeline.NewWorkspace(repo, nested)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	var gotRepo, gotLocal string
	model := NewModel(repo, t.TempDir(), WithWorkspace(workspace), WithDependencyLifecycle(DependencyLifecycle{
		Sync: func(_ context.Context, ws pipeline.Workspace) ([]error, error) {
			gotRepo, gotLocal = ws.RepoRoot(), ws.LocalConfigDir()
			return nil, nil
		},
	}))

	if msg := model.dependencySyncCmd()(); msg.(syncDoneMsg).err != nil {
		t.Fatalf("sync callback failed: %v", msg.(syncDoneMsg).err)
	}
	if gotRepo != workspace.RepoRoot() || gotLocal != workspace.LocalConfigDir() {
		t.Fatalf("callback workspace = (%q, %q); want (%q, %q)", gotRepo, gotLocal, workspace.RepoRoot(), workspace.LocalConfigDir())
	}
}

func TestConfigMutationInvalidatesCleanDriftBeforeReload(t *testing.T) {
	var syncCalls atomic.Int64
	model := NewModel("/repo", "/home", WithDependencyLifecycle(DependencyLifecycle{
		Sync: func(context.Context, pipeline.Workspace) ([]error, error) {
			syncCalls.Add(1)
			return nil, nil
		},
	}))
	model.loading = false
	model.driftAssessment = driftReady
	operation, ok := model.beginOperation(operationMutation)
	if !ok {
		t.Fatal("mutation operation was not accepted")
	}

	updated, _ := model.Update(configWrittenMsg{operation: operation, skillName: "review", enabled: true})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: 's', Text: string('s')})
	model = updated.(Model)
	for _, command := range batchCmds(command) {
		if _, ok := command().(syncDoneMsg); ok {
			break
		}
	}
	if syncCalls.Load() != 1 || !model.syncing {
		t.Fatalf("immediate post-mutation sync = calls %d, syncing %t; want one active sync", syncCalls.Load(), model.syncing)
	}
}

func TestSuccessfulSyncClearsPendingDriftAfterReload(t *testing.T) {
	model := NewModel("/repo", "/home")
	model.loading = false
	model.syncing = true
	model.driftAssessment = driftReady
	model.driftReport = materialize.DriftReport{Missing: []materialize.DriftItem{{Name: "review", Reason: materialize.DriftMissing}}}

	updated, _ := model.Update(syncDoneMsg{})
	model = updated.(Model)
	updated, _ = model.Update(snapshotLoadedMsg{
		generation:      model.requests.active(requestSnapshot),
		mergedCfg:       &config.Config{},
		driftAssessment: driftReady,
	})
	model = updated.(Model)
	_, command := model.Update(tea.KeyPressMsg{Code: 'q', Text: string('q')})
	if !cmdProducesQuitMsg(command) {
		t.Fatal("successful sync retained stale pending drift and blocked quit")
	}
}

func TestDependencyLifecycleFailuresAreShownWithoutFallback(t *testing.T) {
	failure := errors.New("lock reconciliation failed")
	previousSync := runPipelineSync
	runPipelineSync = func(context.Context, pipeline.ResolveOptions) pipeline.SyncResult {
		t.Fatal("injected lifecycle failure fell back to the unlocked TUI pipeline")
		return pipeline.SyncResult{}
	}
	t.Cleanup(func() { runPipelineSync = previousSync })

	model := NewModel("/repo", "/home", WithDependencyLifecycle(DependencyLifecycle{
		Sync:   func(context.Context, pipeline.Workspace) ([]error, error) { return nil, failure },
		Update: func(context.Context, pipeline.Workspace) ([]error, error) { return nil, failure },
	}))
	model.syncing = true
	updated, _ := model.Update(model.dependencySyncCmd()())
	model = updated.(Model)
	// The pipeline failure is the verdict; it surfaces once the authoritative
	// reload the completion scheduled is accepted.
	updated, _ = model.Update(snapshotLoadedMsg{
		generation:      model.requests.active(requestSnapshot),
		mergedCfg:       &config.Config{},
		driftAssessment: driftReady,
	})
	model = updated.(Model)
	if !strings.Contains(model.toast, failure.Error()) || len(model.errs) == 0 || !errors.Is(model.errs[0], failure) {
		t.Fatalf("sync lock failure not shown: toast %q, errors %v", model.toast, model.errs)
	}

	previousSnapshot := readSnapshotForUpdate
	readSnapshotForUpdate = func(context.Context, pipeline.Workspace, string, catalogLoadMode) snapshotLoadedMsg {
		return snapshotLoadedMsg{driftAssessment: driftReady}
	}
	t.Cleanup(func() { readSnapshotForUpdate = previousSnapshot })
	generation := model.beginRequest(requestSnapshot)
	model.syncing = true
	updated, _ = model.Update(model.dependencyUpdateCmd(generation)())
	model = updated.(Model)
	if !strings.Contains(model.toast, failure.Error()) || len(model.errs) == 0 || !errors.Is(model.errs[len(model.errs)-1], failure) {
		t.Fatalf("update lock failure not shown: toast %q, errors %v", model.toast, model.errs)
	}
}

func TestInitDoesNotInvokeDependencyLifecycle(t *testing.T) {
	var calls atomic.Int64
	callback := func(context.Context, pipeline.Workspace) ([]error, error) {
		calls.Add(1)
		return nil, nil
	}
	model := NewModel(t.TempDir(), t.TempDir(), WithDependencyLifecycle(DependencyLifecycle{Sync: callback, Update: callback}))
	for _, cmd := range batchCmds(model.Init()) {
		cmd()
	}
	if calls.Load() != 0 {
		t.Fatalf("startup invoked mutable dependency lifecycle %d times", calls.Load())
	}
}

// spec: marketplace-api/tui-sync-and-update-report-the-refresh-non-fatally
func TestDependencyLifecycleWarningsSurfaceWithoutFailure(t *testing.T) {
	warning := errors.New("marketplace.archive-evidence-refreshed: src/skill/review locked archive digest sha256:aaaa refreshed to sha256:bbbb for the unchanged content digest")
	second := errors.New("marketplace.archive-evidence-refreshed: src/skill/audit locked archive digest sha256:cccc refreshed to sha256:dddd for the unchanged content digest")
	failureWarning := errors.New("marketplace.archive-evidence-refreshed: failed update retained the old archive digest")
	failure := errors.New("materialization failed")
	failed := false
	model := NewModel("/repo", "/home", WithDependencyLifecycle(DependencyLifecycle{
		Sync: func(context.Context, pipeline.Workspace) ([]error, error) {
			if failed {
				return []error{failureWarning}, failure
			}
			return []error{warning, second}, nil
		},
		Update: func(context.Context, pipeline.Workspace) ([]error, error) {
			return []error{warning}, nil
		},
	}))
	model.loading = false
	model.syncing = true
	done, ok := model.dependencySyncCmd()().(syncDoneMsg)
	if !ok || done.err != nil {
		t.Fatalf("sync done = %#v", done)
	}
	updated, _ := model.Update(done)
	model = updated.(Model)
	if !model.syncing {
		t.Fatal("sync completion ended before its authoritative reload")
	}
	if len(model.warnings) != 0 || strings.Contains(model.toast, "Sync complete") {
		t.Fatalf("sync warnings or success surfaced before reload: warnings %v, toast %q", model.warnings, model.toast)
	}
	generation := model.requests.active(requestSnapshot)
	if generation == 0 {
		t.Fatal("sync completion did not schedule an authoritative reload")
	}
	updated, _ = model.Update(snapshotLoadedMsg{
		generation:      generation,
		mergedCfg:       &config.Config{},
		driftAssessment: driftReady,
	})
	model = updated.(Model)
	if model.syncing {
		t.Fatal("accepted reload left sync active")
	}
	if len(model.errs) != 0 {
		t.Fatalf("non-fatal warnings recorded as errors: %v", model.errs)
	}
	for _, want := range []error{warning, second} {
		if !slices.Contains(model.warnings, want) {
			t.Fatalf("warnings %v missing %v", model.warnings, want)
		}
	}
	if model.toast != "Sync complete with 2 warning(s)" {
		t.Fatalf("warning sync toast = %q; want completion after reload", model.toast)
	}

	previousSnapshot := readSnapshotForUpdate
	readSnapshotForUpdate = func(context.Context, pipeline.Workspace, string, catalogLoadMode) snapshotLoadedMsg {
		return snapshotLoadedMsg{driftAssessment: driftReady}
	}
	t.Cleanup(func() { readSnapshotForUpdate = previousSnapshot })
	generation = model.beginRequest(requestSnapshot)
	model.syncing = true
	update, ok := model.dependencyUpdateCmd(generation)().(updateDoneMsg)
	if !ok || update.err != nil {
		t.Fatalf("update done = %#v", update)
	}
	updated, _ = model.Update(update)
	model = updated.(Model)
	if len(model.errs) != 0 {
		t.Fatalf("update warning recorded as an error: %v", model.errs)
	}
	if !slices.Contains(model.warnings, warning) {
		t.Fatalf("update warnings %v missing %v", model.warnings, warning)
	}
	if model.toast != "Update complete with 1 warning(s)" {
		t.Fatalf("warning update toast = %q; want a non-fatal completion notice", model.toast)
	}

	failed = true
	model.syncing = true
	done, ok = model.dependencySyncCmd()().(syncDoneMsg)
	if !ok || !errors.Is(done.err, failure) {
		t.Fatalf("failed sync done = %#v; want the hard error", done)
	}
	updated, _ = model.Update(done)
	model = updated.(Model)
	if !model.syncing || len(model.errs) != 0 || strings.Contains(model.toast, "sync error") {
		t.Fatalf("failed sync surfaced before its reload: syncing %t, errors %v, toast %q", model.syncing, model.errs, model.toast)
	}
	generation = model.requests.active(requestSnapshot)
	updated, _ = model.Update(snapshotLoadedMsg{
		generation:      generation,
		mergedCfg:       &config.Config{},
		driftAssessment: driftReady,
	})
	model = updated.(Model)
	if !slices.ContainsFunc(model.errs, func(err error) bool { return errors.Is(err, failure) }) || !strings.Contains(model.toast, "sync error") {
		t.Fatalf("hard error masked by accompanying warning: errs %v, toast %q", model.errs, model.toast)
	}
	if len(model.warnings) != 0 {
		t.Fatalf("warnings from failed sync were recorded: %v", model.warnings)
	}
}

func TestDependencyScopedToggleUsesWorkspaceLocalRoot(t *testing.T) {
	repo := t.TempDir()
	nested := filepath.Join(repo, "nested")
	if err := os.MkdirAll(filepath.Join(nested, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	workspace, err := pipeline.NewWorkspace(repo, nested)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	var gotLocal string
	mutate := func(_ context.Context, ws pipeline.Workspace, mutation func(pipeline.ConfigPathMapper) error) error {
		gotLocal = ws.LocalConfigDir()
		return mutation(func(path string) (string, error) { return path, nil })
	}
	act := toggleAction{skillName: "review", kind: ir.KindSkill, scope: scopeLocal, enable: true, registryURL: "https://example.com/one.git", registryRef: "main"}

	written := runDependencyScopedToggle(context.Background(), workspace, act, mutate)().(configWrittenMsg)
	if written.err != nil {
		t.Fatalf("local toggle failed: %v", written.err)
	}
	if gotLocal != workspace.LocalConfigDir() {
		t.Fatalf("mutation workspace local root = %q; want %q", gotLocal, workspace.LocalConfigDir())
	}
	localCfg, err := config.LoadLocalConfigFromPath(config.LocalConfigPath(nested))
	if err != nil || len(localCfg.Registries) != 1 || !slices.Contains(localCfg.Registries[0].Skills.Include, "review") {
		t.Fatalf("nested local config = %#v, %v; want review include", localCfg, err)
	}
	if _, err := os.Stat(config.LocalConfigPath(repo)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("repository local config was written or stat failed: %v", err)
	}
}

func TestProjectToggleReloadDoesNotExposeStaleLock(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".agents", "config.yaml"), []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	projectConfig := "apiVersion: agent-marketplace/v2\nregistries:\n  - url: https://example.com/one.git\n    ref: main\n"
	if err := os.WriteFile(config.ProjectConfigPath(repo), []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	commit, err := lockfile.ParseGitOID("sha1:1111111111111111111111111111111111111111")
	if err != nil {
		t.Fatal(err)
	}
	doc := lockfile.Document{APIVersion: lockfile.APIVersionV1, Scope: lockfile.ScopeRepo, Registries: []lockfile.GitRegistry{{URL: "https://example.com/one.git", Ref: "main", Commit: commit, Roots: []lockfile.GitRoot{{Path: ".", SourceHarness: "factory", RevisionSchema: "v1", RevisionDigest: lockfile.Digest("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")}}}}}
	data, err := lockfile.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".agents", "lock.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	previousOrg, previousGlobal, previousRepo, previousLocal := loadOrgConfig, loadGlobalConfig, loadRepoConfig, loadLocalConfig
	loadOrgConfig = func() (*config.Config, error) { return nil, nil }
	loadGlobalConfig = func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(home, ".agents", "config.yaml"))
	}
	loadRepoConfig = func() (*config.Config, error) { return config.LoadConfigFromPath(config.ProjectConfigPath(repo)) }
	loadLocalConfig = func(string) (*config.LocalConfig, error) { return nil, nil }
	t.Cleanup(func() {
		loadOrgConfig, loadGlobalConfig, loadRepoConfig, loadLocalConfig = previousOrg, previousGlobal, previousRepo, previousLocal
	})

	updatedDoc := doc
	updatedDoc.Registries = append(updatedDoc.Registries, lockfile.GitRegistry{URL: "https://example.com/two.git", Ref: "main", Commit: commit, Roots: []lockfile.GitRoot{{Path: ".", SourceHarness: "factory", RevisionSchema: "v1", RevisionDigest: lockfile.Digest("sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")}}})
	mutate := func(_ context.Context, _ pipeline.Workspace, mutation func(pipeline.ConfigPathMapper) error) error {
		if err := mutation(func(path string) (string, error) { return path, nil }); err != nil {
			return err
		}
		updated, err := lockfile.Encode(updatedDoc)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(repo, ".agents", "lock.yaml"), updated, 0o644)
	}
	act := toggleAction{skillName: "review", kind: ir.KindSkill, scope: scopeProject, enable: true, registryURL: "https://example.com/two.git", registryRef: "main"}
	written := runDependencyScopedToggle(context.Background(), defaultTUIWorkspace(repo), act, mutate)().(configWrittenMsg)
	if written.err != nil {
		t.Fatalf("toggle mutation failed: %v", written.err)
	}
	project, err := config.LoadConfigFromPath(config.ProjectConfigPath(repo))
	if err != nil || len(project.Registries) != 2 {
		t.Fatalf("project toggle did not add the registry: %#v, %v", project, err)
	}
	model := NewModel(repo, home)
	operation, ok := model.beginOperation(operationMutation)
	if !ok {
		t.Fatal("mutation operation was not accepted")
	}
	written.operation = operation
	_, cmd := model.Update(written)
	commands := batchCmds(cmd)
	if len(commands) == 0 {
		t.Fatal("toggle completion did not schedule a reload")
	}
	reloaded := commands[0]()
	snapshot, ok := reloaded.(snapshotLoadedMsg)
	if !ok {
		t.Fatalf("reload returned %T", reloaded)
	}
	for _, snapshotErr := range snapshot.errs {
		var lockErr *pipeline.LockViewError
		if errors.As(snapshotErr, &lockErr) && lockErr.Code == "lock.stale" {
			t.Fatalf("toggle reload exposed stale dependency lock: %v", errors.Join(snapshot.errs...))
		}
	}
}
