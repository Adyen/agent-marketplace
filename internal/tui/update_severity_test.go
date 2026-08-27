package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// TestRunUpdateCmdSucceedsWithWarningOnlyDiagnostics pins TUI `update`:
// diag.SeverityError is the ZERO value of diag.Severity, so an ordinal
// comparison turned every diagnostic — including a lossy-but-successful
// warning — into an update failure. A Windsurf target has no subagent
// primitive, so materializing a subagent for it emits exactly one
// `subagent.windsurf.no-primitive` WARNING and nothing else. Update must
// still succeed.
func TestRunUpdateCmdSucceedsWithWarningOnlyDiagnostics(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	registryRoot := t.TempDir()
	t.Setenv("HOME", homeDir)

	if err := os.MkdirAll(filepath.Join(repoRoot, ".agents"), 0o755); err != nil {
		t.Fatalf("mkdir repo agents: %v", err)
	}
	// repoV2Header, not v2Header: this test exercises a project config without
	// its own target harness list, so the target set comes from the global config
	// written below.
	if err := os.WriteFile(filepath.Join(repoRoot, ".agents", "config.yaml"), []byte(repoV2Header), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	subagentPath := filepath.Join(registryRoot, ".agents", "droids", "worker.md")
	if err := os.MkdirAll(filepath.Dir(subagentPath), 0o755); err != nil {
		t.Fatalf("mkdir registry droids: %v", err)
	}
	if err := os.WriteFile(subagentPath, []byte("---\nname: worker\ndescription: Do work\n---\nWork\n"), 0o644); err != nil {
		t.Fatalf("write subagent: %v", err)
	}

	globalConfig := `apiVersion: agent-marketplace/v2
harnesses:
  - windsurf
registries:
  - url: ` + registryRoot + `
    path: .agents
    source_harness: factory
    subagents:
      - worker
`
	if err := os.MkdirAll(filepath.Join(homeDir, ".agents"), 0o755); err != nil {
		t.Fatalf("mkdir home agents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".agents", "config.yaml"), []byte(globalConfig), 0o644); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) {
		return nil, nil
	})
	defer restoreOrg()
	restoreGlobal := swapLoadGlobalConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(homeDir, ".agents", "config.yaml"))
	})
	defer restoreGlobal()
	restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(repoRoot, ".agents", "config.yaml"))
	})
	defer restoreRepo()
	restoreLocal := swapLoadLocalConfig(func() (*config.LocalConfig, error) {
		return config.LoadLocalConfigFromPath(filepath.Join(repoRoot, ".agents", "config.local.yaml"))
	})
	defer restoreLocal()

	msg := runUpdateCmd(context.Background(), repoRoot, homeDir, 1)()
	if done, ok := msg.(updateDoneMsg); ok {
		t.Fatalf("runUpdateCmd failed on warning-only diagnostics: %v", done.err)
	}
	reload, ok := msg.(reloadMsg)
	if !ok {
		t.Fatalf("runUpdateCmd returned %T, want reloadMsg", msg)
	}
	var warningText strings.Builder
	for _, w := range reload.warnings {
		warningText.WriteString(w.Error())
	}
	if !strings.Contains(warningText.String(), "subagent.windsurf.no-primitive") {
		t.Fatalf("expected the warning-only update to emit subagent.windsurf.no-primitive, got warnings: %v", reload.warnings)
	}
}
