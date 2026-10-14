package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// spec: hook-sync-diagnostics/hook-errors-retain-target-and-source
// spec: hook-sync-diagnostics/matcher-refusal-identifies-its-translation-stage
func TestSyncHookMatcherRefusalNamesTargetAndStage(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	tests := []struct {
		name        string
		matcher     string
		wantMessage string
		wantHint    string
	}{
		{
			name:        "undecodable source matcher",
			matcher:     `^(mcp_call_tool|mcp__blackboard__.*)$`,
			wantMessage: "The source matcher cannot be decoded for cross-harness translation; hook suppressed",
			wantHint:    "Use exact source tool names or a supported MCP pattern, or author a target-specific hook",
		},
		{
			name:        "decoded matcher the target cannot cover",
			matcher:     `^mcp_call_tool$`,
			wantMessage: "The target tool catalog cannot exactly cover the source matcher; hook suppressed",
			wantHint:    "Author a target-specific hook or explicitly narrow the selected harnesses; preview cleanup with agent-marketplace sync --check",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, "config"))
			config.SetTestV3OrganizationConfigPath(t, filepath.Join(homeDir, "nonexistent-org.yaml"))

			registry := t.TempDir()
			writeFile(t, filepath.Join(registry, ".devin", "hooks.v1.json"), fmt.Sprintf(`{"PreToolUse":[{"matcher":%q,"hooks":[{"type":"command","command":"echo guard-check-5d1a"}]}]}`+"\n", tt.matcher))

			repoRoot := filepath.Join(t.TempDir(), "repo")
			writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
harnesses: [devin, factory, cursor]
sources:
  hooks:
    directory: `+registry+`
    source_harness: devin
resources:
  hooks: {include: [hook/PreToolUse-command]}
`)
			withWorkingDir(t, repoRoot)

			output, err := executeCommand("sync")
			if err == nil {
				t.Fatalf("sync succeeded with a suppressed factory hook:\n%s", output)
			}
			assertMatcherRefusalDiagnostics(t, err.Error()+"\n"+output, tt.matcher, tt.wantMessage, tt.wantHint)

			stateRoot := filepath.Join(homeDir, "state")
			repoSnap := cliFilesystemSnapshot(t, repoRoot)
			registrySnap := cliFilesystemSnapshot(t, registry)
			stateSnap := cliFilesystemSnapshot(t, stateRoot)
			checkOutput, checkErr := executeCommand("sync", "--check")
			if checkErr == nil {
				t.Fatalf("sync --check succeeded with a suppressed factory hook:\n%s", checkOutput)
			}
			assertMatcherRefusalDiagnostics(t, checkErr.Error()+"\n"+checkOutput, tt.matcher, tt.wantMessage, tt.wantHint)
			assertCliFilesystemSnapshot(t, "sync --check repository", repoRoot, repoSnap)
			assertCliFilesystemSnapshot(t, "sync --check registry", registry, registrySnap)
			assertCliFilesystemSnapshot(t, "sync --check state", stateRoot, stateSnap)
		})
	}
}

func assertMatcherRefusalDiagnostics(t *testing.T, text, matcher, message, hint string) {
	t.Helper()
	targets := []string{"factory", "cursor"}
	found := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, "hook.matcher-untranslatable") {
			continue
		}
		for _, payload := range []string{matcher, "mcp_call_tool", "blackboard", "guard-check-5d1a"} {
			if strings.Contains(line, payload) {
				t.Fatalf("refusal line quotes %q:\n%s", payload, line)
			}
		}
		for _, target := range targets {
			if strings.Contains(line, "target "+target) && strings.Contains(line, `resource "PreToolUse-command"`) &&
				strings.Contains(line, ".devin/hooks.v1.json") && strings.Contains(line, message) {
				found[target] = true
			}
		}
	}
	for _, target := range targets {
		if !found[target] {
			t.Fatalf("no hook.matcher-untranslatable line names target %s, resource, source, and %q:\n%s", target, message, text)
		}
	}
	if !strings.Contains(text, hint) {
		t.Fatalf("output missing remediation %q:\n%s", hint, text)
	}
}
