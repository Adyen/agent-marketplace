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
	for _, source := range []config.Source{config.RepoCommitted, config.UserPersonal} {
		t.Run(source.String(), func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			repoRoot := t.TempDir()
			external := t.TempDir()
			sentinel := filepath.Join(external, "sentinel")
			if err := os.WriteFile(sentinel, []byte("must survive"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(external, filepath.Join(rootForSource(repoRoot, homeDir, source), ".factory")); err != nil {
				t.Fatal(err)
			}

			resolved := []resolver.ResolvedArtifact{{
				Entry:     catalog.Entry{Name: "root-escape", Resource: ir.NewSkill("root-escape", "Root escape", "Body", nil, nil, ir.SourceRef{})},
				Source:    source,
				Harnesses: []hcc.Target{hcc.TargetFactory},
			}}
			report, err := Run(context.Background(), repoRoot, resolved)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if len(report.Diags) == 0 {
				t.Fatal("expected a diagnostic for the symlinked harness root")
			}
			assertUnchangedFile(t, sentinel, []byte("must survive"), 0o600)
		})
	}
}

func rootForSource(repoRoot, homeDir string, source config.Source) string {
	if source.IsMachineScope() {
		return homeDir
	}
	return repoRoot
}

func assertUnchangedFile(t *testing.T, path string, want []byte, wantMode os.FileMode) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sentinel: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("sentinel bytes changed: got %q, want %q", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat sentinel: %v", err)
	}
	if info.Mode().Perm() != wantMode.Perm() {
		t.Fatalf("sentinel mode changed: got %o, want %o", info.Mode().Perm(), wantMode.Perm())
	}
}

func TestRunRejectsSymlinkedNestedHookDirectory(t *testing.T) {
	for _, mode := range []config.MaterializationMode{
		config.MaterializationModeSymlink,
		config.MaterializationModeFile,
	} {
		t.Run(string(mode), func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			repoRoot := t.TempDir()
			external := t.TempDir()
			sentinel := filepath.Join(external, "sentinel")
			if err := os.WriteFile(sentinel, []byte("must survive"), 0o600); err != nil {
				t.Fatal(err)
			}
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
			report, err := Run(context.Background(), repoRoot, resolved, WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode}))
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if len(report.Diags) == 0 {
				t.Fatal("expected a diagnostic for the symlinked hook directory")
			}
			assertUnchangedFile(t, sentinel, []byte("must survive"), 0o600)
		})
	}
}
