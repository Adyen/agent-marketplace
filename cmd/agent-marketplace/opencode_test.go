package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
