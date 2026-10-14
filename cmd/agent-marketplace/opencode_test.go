package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// spec: harness-opencode/opencode-selection-reaches-consumer-workflows
// spec: opencode-native-configuration/creation-and-conversion-retain-private-modes
func TestConvertOpenCodeNativeResources(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, flag := range []string{"from", "to"} {
		if !strings.Contains(newConvertCmd().Flags().Lookup(flag).Usage, "opencode") {
			t.Fatalf("%s help omits OpenCode", flag)
		}
	}
	source := t.TempDir()
	writeClaudePluginSkill(t, source, "demo")
	writeFile(t, filepath.Join(source, "commands", "check.md"), "---\ndescription: Check changes\n---\nCheck $ARGUMENTS and $1.\n")
	writeFile(t, filepath.Join(source, "agents", "reviewer.md"), "---\nname: reviewer\ndescription: Review changes\nmodel: sonnet\n---\nReview carefully.\n")
	writeClaudePluginSharedConfig(t, source, `{"hooks":{"PreToolUse":[{"type":"command","matcher":"Bash","command":"printf '{}'"}]}}`, `{"mcpServers":{"docs":{"command":"docs-server"}}}`)
	out := filepath.Join(t.TempDir(), "native")
	output, err := executeCommand("convert", "--from", "claude", "--to", "opencode", "--out", out, source)
	if err != nil {
		t.Fatalf("convert: %v\n%s", err, output)
	}
	for _, rel := range []string{"skills/demo/SKILL.md", "commands/check.md", "agents/reviewer.md", "plugins/agent-marketplace.js", "opencode.json"} {
		assertFileExists(t, filepath.Join(out, rel))
	}
	info, err := os.Stat(filepath.Join(out, "opencode.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("MCP mode: %v %v", info, err)
	}
	command, _ := os.ReadFile(filepath.Join(out, "commands/check.md"))
	if !strings.Contains(string(command), "$ARGUMENTS and $1") {
		t.Fatal("command placeholders changed")
	}
	back := filepath.Join(t.TempDir(), "claude")
	output, err = executeCommand("convert", "--from", "opencode", "--to", "claude", "--out", back, out)
	if err != nil {
		t.Fatalf("reverse convert: %v\n%s", err, output)
	}
	assertFileExists(t, filepath.Join(back, "commands", "check.md"))
}

func TestConvertOpenCodeRefusesExistingSharedConfiguration(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, name := range []string{"opencode.json", "opencode.jsonc"} {
		t.Run(name, func(t *testing.T) {
			source, out := t.TempDir(), t.TempDir()
			writeClaudePluginSkill(t, source, "demo")
			file := filepath.Join(out, name)
			writeFile(t, file, "handwritten\n")
			output, err := executeCommand("convert", "--from", "claude", "--to", "opencode", "--out", out, source)
			if err == nil || !strings.Contains(output+err.Error(), "empty destination") {
				t.Fatalf("expected refusal: %v %s", err, output)
			}
			data, _ := os.ReadFile(file)
			if string(data) != "handwritten\n" {
				t.Fatal("shared configuration changed")
			}
			assertPathExistence(t, filepath.Join(out, "skills"), false)
		})
	}
}

func TestOpenCodeConversionStageErrorsNameOpenCode(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	stage := t.TempDir()
	writeFile(t, filepath.Join(stage, "opencode.json"), "{")
	err := validateConversionStage("OpenCode", stage, []string{"opencode.json"})
	if err == nil || !strings.Contains(err.Error(), "staged OpenCode aggregate") || strings.Contains(err.Error(), "Devin") {
		t.Fatalf("stage error = %v", err)
	}
}

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
		writeFile(t, definition, `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"/bin/sh \"${CLAUDE_PLUGIN_ROOT}/check.sh\" `+command+`"}]}]}`+"\n")
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
	supports, _ := filepath.Glob(filepath.Join(repoRoot, ".opencode", "hook-support", "*", "PreToolUse-command", "check.sh"))
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
