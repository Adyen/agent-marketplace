package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestV4DirectoryGlobalToggleUsesStagedMutationAndRealDiscovery(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("TMPDIR", t.TempDir())
	source := filepath.Join(t.TempDir(), "registry.git")
	for _, skill := range []struct{ name, content string }{{"alpha", "# Alpha\n"}, {"beta", "# Beta\n"}} {
		path := filepath.Join(source, "skills", skill.name, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(skill.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	globalPath := filepath.Join(home, ".agents", "config.yaml")
	global := "apiVersion: agent-marketplace/v4\nharnesses: [factory, gemini, devin]\nsources:\n  local:\n    directory: " + source + "\n"
	if err := os.MkdirAll(filepath.Dir(globalPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(globalPath, []byte(global), 0o640); err != nil {
		t.Fatal(err)
	}
	originalOrg, originalGlobal := session.LoadOrgConfig, session.LoadGlobalConfig
	originalRepo, originalLocal := session.LoadRepoConfig, session.LoadLocalConfig
	t.Cleanup(func() {
		session.LoadOrgConfig, session.LoadGlobalConfig = originalOrg, originalGlobal
		session.LoadRepoConfig, session.LoadLocalConfig = originalRepo, originalLocal
	})
	session.LoadOrgConfig = func() (*config.Config, error) { return nil, nil }
	session.LoadGlobalConfig = func() (*config.Config, error) { return config.LoadConfigFromPath(globalPath) }
	session.LoadRepoConfig = func() (*config.Config, error) { return nil, nil }
	session.LoadLocalConfig = func(string) (*config.LocalConfig, error) { return nil, nil }

	workspace, err := pipeline.NewWorkspace(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	initial := readSnapshot(context.Background(), repo, home, catalogLoadRefresh)
	if len(initial.errs) > 0 {
		t.Fatalf("initial snapshot errors: %v", initial.errs)
	}
	available := findV4ToggleRow(t, initial.rows, "alpha")
	if !available.IsAvailable || available.EnabledGlobal {
		t.Fatalf("alpha initial state = available %v global %v", available.IsAvailable, available.EnabledGlobal)
	}
	act := scopedToggleAction(available, scopeGlobal)
	reconcile := func(_ context.Context, _ string, _ string, _ bool, edits []pipeline.StagedConfigEdit) (pipeline.DependencyLockView, error) {
		for _, edit := range edits {
			if err := os.WriteFile(edit.Path, edit.Bytes, edit.Mode); err != nil {
				return pipeline.DependencyLockView{}, err
			}
		}
		return pipeline.DependencyLockView{}, nil
	}
	mutate := func(ctx context.Context, ws pipeline.Workspace, mutation func(pipeline.ConfigPathMapper) error) error {
		return pipeline.MutateConfigWithDependencies(ctx, ws.RepoRoot(), ws.LocalConfigDir(), mutation, reconcile)
	}
	msg, ok := runDependencyScopedToggle(context.Background(), workspace, act, mutate)().(configWrittenMsg)
	if !ok || msg.err != nil {
		t.Fatalf("enable message = %#v", msg)
	}
	updatedConfig, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(updatedConfig) == global || !strings.Contains(string(updatedConfig), "skill/alpha") || strings.Contains(string(updatedConfig), "registries:") {
		t.Fatalf("global v4 selection write =\n%s", updatedConfig)
	}
	assertNoConfigStaging(t)
	loadedGlobal, err := config.LoadV4ConfigFromPath(globalPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(loadedGlobal.Sources) != 1 || len(loadedGlobal.Groups["local"].Include) != 1 || loadedGlobal.Groups["local"].Include[0].Locator != "skill/alpha" {
		t.Fatalf("loaded v4 selection = %#v", loadedGlobal)
	}
	selected := readSnapshot(context.Background(), repo, home, catalogLoadCached)
	if len(selected.errs) > 0 {
		t.Fatalf("selected snapshot errors: %v", selected.errs)
	}
	alpha := findV4ToggleRow(t, selected.rows, "alpha")
	if alpha.IsAvailable || !alpha.EnabledGlobal {
		t.Fatalf("alpha selected state = available %v global %v", alpha.IsAvailable, alpha.EnabledGlobal)
	}
	if alpha.Artifact.Kind != ir.KindSkill || alpha.Artifact.V4Kind != config.V4SourceDirectory {
		t.Fatalf("alpha identity = kind %v source kind %q", alpha.Artifact.Kind, alpha.Artifact.V4Kind)
	}

	disable := scopedToggleAction(alpha, scopeGlobal)
	msg, ok = runDependencyScopedToggle(context.Background(), workspace, disable, mutate)().(configWrittenMsg)
	if !ok || msg.err != nil {
		t.Fatalf("disable message = %#v", msg)
	}
	final := readSnapshot(context.Background(), repo, home, catalogLoadCached)
	if len(final.errs) > 0 {
		t.Fatalf("final snapshot errors: %v", final.errs)
	}
	alpha = findV4ToggleRow(t, final.rows, "alpha")
	if !alpha.IsAvailable || alpha.EnabledGlobal {
		t.Fatalf("alpha final state = available %v global %v", alpha.IsAvailable, alpha.EnabledGlobal)
	}
	assertNoConfigStaging(t)
	beta := findV4ToggleRow(t, final.rows, "beta")
	if !beta.IsAvailable {
		t.Fatal("beta should remain available after alpha disable")
	}
	if !strings.Contains(string(mustReadFile(t, globalPath)), "skill/alpha") {
		t.Fatalf("disable should retain explicit exclusion or selection history")
	}

	reenable := scopedToggleAction(alpha, scopeGlobal)
	msg, ok = runDependencyScopedToggle(context.Background(), workspace, reenable, mutate)().(configWrittenMsg)
	if !ok || msg.err != nil {
		t.Fatalf("re-enable message = %#v", msg)
	}
	assertNoConfigStaging(t)
	reenabled := readSnapshot(context.Background(), repo, home, catalogLoadCached)
	if len(reenabled.errs) > 0 {
		t.Fatalf("re-enabled snapshot errors: %v", reenabled.errs)
	}
	alpha = findV4ToggleRow(t, reenabled.rows, "alpha")
	if alpha.IsAvailable || !alpha.EnabledGlobal {
		t.Fatalf("alpha re-enabled state = available %v global %v", alpha.IsAvailable, alpha.EnabledGlobal)
	}
	beta = findV4ToggleRow(t, reenabled.rows, "beta")
	if !beta.IsAvailable {
		t.Fatal("beta should remain available after alpha re-enable")
	}
}

func assertNoConfigStaging(t *testing.T) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(os.Getenv("TMPDIR"), ".agent-marketplace-config-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("config staging residue: %v %v", matches, err)
	}
}

func findV4ToggleRow(t *testing.T, rows []row, name string) row {
	t.Helper()
	for _, candidate := range rows {
		if candidate.Artifact.Name == name && candidate.Artifact.Kind == ir.KindSkill {
			return candidate
		}
	}
	t.Fatalf("resource %q not found", name)
	return row{}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
