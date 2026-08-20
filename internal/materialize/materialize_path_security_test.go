package materialize

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestRunRejectsSymlinkedHarnessRoot(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()
	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(repoRoot, ".factory")); err != nil {
		t.Fatal(err)
	}

	resolved := []resolver.ResolvedArtifact{{
		Entry:     catalog.Entry{Name: "root-escape", Resource: ir.NewSkill("root-escape", "Root escape", "Body", nil, nil, ir.SourceRef{})},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetFactory},
	}}
	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Diags) == 0 {
		t.Fatal("expected a diagnostic for the symlinked harness root")
	}
	if entries, readErr := os.ReadDir(external); readErr != nil {
		t.Fatal(readErr)
	} else if len(entries) != 0 {
		t.Fatalf("materialization wrote outside the harness root: %v", entries)
	}
}

func TestRunRejectsSymlinkedNestedHookDirectory(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()
	external := t.TempDir()
	factoryRoot := filepath.Join(repoRoot, ".factory")
	if err := os.MkdirAll(factoryRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(factoryRoot, "hooks")); err != nil {
		t.Fatal(err)
	}

	sourceDir := t.TempDir()
	hook := ir.NewHookWithMatcher("nested-hook", "PostToolUse", "Edit", "${DROID_PLUGIN_ROOT}/hooks/check.sh", "command", nil, ir.SourceRef{File: filepath.Join(sourceDir, "hooks.json")})
	resolved := []resolver.ResolvedArtifact{{
		Entry:     catalog.Entry{Name: "nested-hook", Path: filepath.Join(sourceDir, "hooks.json"), Resource: hook},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetFactory},
	}}
	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Diags) == 0 {
		t.Fatal("expected a diagnostic for the symlinked hook directory")
	}
	if entries, readErr := os.ReadDir(external); readErr != nil {
		t.Fatal(readErr)
	} else if len(entries) != 0 {
		t.Fatalf("materialization wrote outside the hook root: %v", entries)
	}
}
