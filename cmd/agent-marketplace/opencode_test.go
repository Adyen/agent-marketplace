package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// spec: opencode-hooks/declaration-changes-are-listed-for-review
// spec: opencode-hooks/hook-support-is-installed-beside-the-adapter
func TestSyncOpenCodeHookReviewAndSupport(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, "config"))
	t.Setenv("OPENCODE_CONFIG", "")
	t.Setenv("OPENCODE_CONFIG_DIR", "")
	config.SetTestV3OrganizationConfigPath(t, filepath.Join(homeDir, "nonexistent-org.yaml"))

	registry := t.TempDir()
	definition := filepath.Join(registry, "hooks", "hooks.json")
	write := func(command string) {
		writeFile(t, definition, `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"/bin/sh \"${CLAUDE_PLUGIN_ROOT}/hooks/check.sh\" `+command+`"}]}]}`+"\n")
	}
	write("opencode-review-old-3a9e")
	writeFile(t, filepath.Join(registry, "hooks", "check.sh"), "exit 0\n")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
harnesses: [opencode]
sources:
  hooks:
    directory: `+registry+`
    source_harness: claude
resources:
  hooks: {include: [hook/PreToolUse-command]}
`)
	withWorkingDir(t, repoRoot)
	declaration := filepath.Join(repoRoot, ".opencode", "hooks", "agent-marketplace", "PreToolUse-command.json")

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	for _, want := range []string{"Hook changes:", "opencode (repository) add ", "opencode-review-old-3a9e", "materialize.hook.review"} {
		if !strings.Contains(output, want) {
			t.Fatalf("initial sync output = %q, missing %q", output, want)
		}
	}
	supports, _ := filepath.Glob(filepath.Join(repoRoot, ".opencode", "hook-support", "*", "PreToolUse-command", "hooks", "check.sh"))
	data, readErr := os.ReadFile(declaration)
	if len(supports) != 1 || readErr != nil || !strings.Contains(string(data), `$AGENT_MARKETPLACE_PROJECT_DIR/.opencode/hook-support/`) || strings.Contains(string(data), registry) {
		t.Fatalf("support %v / declaration %s (%v) do not reference installed support", supports, data, readErr)
	}

	output, err = executeCommand("sync")
	if err != nil || strings.Contains(output, "Hook changes") || strings.Contains(output, "materialize.hook.review") {
		t.Fatalf("repeat sync listed hook changes: %v\n%s", err, output)
	}

	write("opencode-review-new-5d1c")
	stateRoot := filepath.Join(homeDir, "state")
	repoSnap := cliFilesystemSnapshot(t, repoRoot)
	stateSnap := cliFilesystemSnapshot(t, stateRoot)
	output, checkErr := executeCommand("sync", "--check")
	if checkErr == nil {
		t.Fatalf("sync --check succeeded with a pending hook change: %s", output)
	}
	for _, want := range []string{"Hook changes (pending, not written):", "opencode (repository) replace ", "opencode-review-old-3a9e", "opencode-review-new-5d1c", "materialize.hook.review-pending"} {
		if !strings.Contains(output, want) {
			t.Fatalf("sync --check output = %q, missing %q", output, want)
		}
	}
	assertCliFilesystemSnapshot(t, "sync --check repository", repoRoot, repoSnap)
	assertCliFilesystemSnapshot(t, "sync --check state", stateRoot, stateSnap)

	output, err = executeCommand("sync")
	if err != nil || !strings.Contains(output, "opencode (repository) replace ") || !strings.Contains(output, "opencode-review-new-5d1c") {
		t.Fatalf("update sync: %v\n%s", err, output)
	}
}
