package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConvertCommandConvertsClaudeSkillToFactory(t *testing.T) {
	srcDir := t.TempDir()
	writeClaudePluginSkill(t, srcDir, "demo-skill")

	outDir := filepath.Join(t.TempDir(), "factory-out")
	output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--out", outDir, srcDir)
	if err != nil {
		t.Fatalf("expected convert command to succeed: %v\n%s", err, output)
	}

	if !strings.Contains(output, "Converted claude -> factory") {
		t.Fatalf("expected conversion summary, got:\n%s", output)
	}

	assertFileExists(t, filepath.Join(outDir, "skills", "demo-skill", "SKILL.md"))
}

func TestConvertCommandRejectsMissingFrom(t *testing.T) {
	output, err := executeCommand("convert", "--to", "factory", t.TempDir())
	if err == nil {
		t.Fatalf("expected convert command to fail without --from, got output:\n%s", output)
	}
	if !strings.Contains(output, "--from is required") {
		t.Fatalf("expected missing --from error, got:\n%s", output)
	}
}

func TestConvertCommandCanonicalizesHarnessFlags(t *testing.T) {
	srcDir := t.TempDir()
	writeClaudePluginSkill(t, srcDir, "demo-skill")

	outDir := filepath.Join(t.TempDir(), "factory-out")
	output, err := executeCommand("convert", "--from", " claude ", "--to", " factory ", "--out", outDir, srcDir)
	if err != nil {
		t.Fatalf("expected padded harness flags to succeed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Converted claude -> factory") {
		t.Fatalf("expected canonical conversion summary, got:\n%s", output)
	}
	assertFileExists(t, filepath.Join(outDir, "skills", "demo-skill", "SKILL.md"))
}

func writeClaudePluginSkill(t *testing.T, root, name string) {
	t.Helper()
	writeFile(t, filepath.Join(root, ".claude-plugin", "plugin.json"), `{"name":"test-plugin","version":"1.0.0"}`)
	writeFile(t, filepath.Join(root, "skills", name, "SKILL.md"), `---
name: `+name+`
description: Demo skill
metadata:
  tags:
    - demo
---

# Demo skill
`)
	if _, err := os.Stat(filepath.Join(root, ".claude-plugin", "plugin.json")); err != nil {
		t.Fatalf("expected Claude plugin marker: %v", err)
	}
}
