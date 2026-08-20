package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

func TestTUIRecoversAfterConfigIsRepaired(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	t.Setenv("HOME", home)
	userPath := filepath.Join(home, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte("apiVersion: agent-marketplace/v3\nharnesses: ["), 0o644); err != nil {
		t.Fatal(err)
	}
	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) { return nil, nil })
	t.Cleanup(restoreOrg)
	restoreGlobal := swapLoadGlobalConfig(func() (*config.Config, error) { return config.LoadConfigFromPath(userPath) })
	t.Cleanup(restoreGlobal)
	restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) { return nil, nil })
	t.Cleanup(restoreRepo)
	restoreLocal := swapLoadLocalConfig(func() (*config.LocalConfig, error) { return nil, nil })
	t.Cleanup(restoreLocal)

	m := NewModel(repo, home)
	seedTestTerminalSize(&m)
	failed := readSnapshot(repo, home, catalogLoadCached)
	failed.generation = m.requests.active(requestSnapshot)
	updated, _ := m.Update(failed)
	m = updated.(Model)
	if m.diagnostics.count(diagnosticError) == 0 || m.driftAssessment != driftUnknown {
		t.Fatalf("invalid config was not reported: diagnostics=%v drift=%v", m.diagnostics.all(), m.driftAssessment)
	}

	if err := os.WriteFile(userPath, []byte("apiVersion: agent-marketplace/v3\nharnesses: [factory]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	generation := m.beginRequest(requestSnapshot)
	recovered := readSnapshot(repo, home, catalogLoadCached)
	recovered.generation = generation
	updated, _ = m.Update(recovered)
	m = updated.(Model)
	if m.diagnostics.count(diagnosticError) != 0 || m.driftAssessment != driftReady {
		t.Fatalf("repaired config did not recover: diagnostics=%v drift=%v", m.diagnostics.all(), m.driftAssessment)
	}
}

func TestTUIRecoversAfterRegistryIsRepaired(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	registry := filepath.Join(t.TempDir(), "registry")
	t.Setenv("HOME", home)
	userPath := filepath.Join(home, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	configData := "apiVersion: agent-marketplace/v3\nharnesses: [factory]\nregistries:\n  - url: " + registry + "\n    skills: [audit]\n"
	if err := os.WriteFile(userPath, []byte(configData), 0o644); err != nil {
		t.Fatal(err)
	}
	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) { return nil, nil })
	t.Cleanup(restoreOrg)
	restoreGlobal := swapLoadGlobalConfig(func() (*config.Config, error) { return config.LoadConfigFromPath(userPath) })
	t.Cleanup(restoreGlobal)
	restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) { return nil, nil })
	t.Cleanup(restoreRepo)
	restoreLocal := swapLoadLocalConfig(func() (*config.LocalConfig, error) { return nil, nil })
	t.Cleanup(restoreLocal)

	failed := readSnapshot(repo, home, catalogLoadRefresh)
	if len(diagnosticErrors(failed.diagnostics)) == 0 {
		t.Fatal("missing registry did not produce an error")
	}
	skillDir := filepath.Join(registry, "skills", "audit")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: audit\ndescription: Audit\n---\n# Audit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	recovered := readSnapshot(repo, home, catalogLoadRefresh)
	if errs := diagnosticErrors(recovered.diagnostics); len(errs) != 0 {
		t.Fatalf("repaired registry still failed: %v", errs)
	}
	if len(recovered.rows) == 0 || recovered.rows[0].Artifact.Name != "audit" {
		t.Fatalf("normal registry navigation state did not resume: %+v", recovered.rows)
	}
}

func TestTUIRecoversAfterMaterializationConditionIsRepaired(t *testing.T) {
	induced := errors.New("materialization unavailable")
	fail := true
	previous := runMaterialize
	runMaterialize = func(context.Context, string, []resolver.ResolvedArtifact, ...materialize.RunOption) (*materialize.Report, error) {
		if fail {
			return nil, induced
		}
		return &materialize.Report{}, nil
	}
	t.Cleanup(func() { runMaterialize = previous })
	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) { return nil, nil })
	t.Cleanup(restoreOrg)
	restoreGlobal := swapLoadGlobalConfig(func() (*config.Config, error) { return nil, nil })
	t.Cleanup(restoreGlobal)
	restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) { return nil, nil })
	t.Cleanup(restoreRepo)
	restoreLocal := swapLoadLocalConfig(func() (*config.LocalConfig, error) { return nil, nil })
	t.Cleanup(restoreLocal)

	repo, home := t.TempDir(), t.TempDir()
	m := NewModel(repo, home)
	seedTestTerminalSize(&m)
	failed := readSnapshot(repo, home, catalogLoadCached)
	failed.generation = m.requests.active(requestSnapshot)
	updated, _ := m.Update(failed)
	m = updated.(Model)
	if m.driftAssessment != driftFailed || m.diagnostics.count(diagnosticError) == 0 {
		t.Fatalf("materialization failure was not retained: drift=%v diagnostics=%v", m.driftAssessment, m.diagnostics.all())
	}

	fail = false
	generation := m.beginRequest(requestSnapshot)
	recovered := readSnapshot(repo, home, catalogLoadCached)
	recovered.generation = generation
	updated, _ = m.Update(recovered)
	m = updated.(Model)
	if m.driftAssessment != driftReady || m.diagnostics.count(diagnosticError) != 0 {
		t.Fatalf("materialization recovery did not clear stale state: drift=%v diagnostics=%v", m.driftAssessment, m.diagnostics.all())
	}
}
