package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// spec: command-error-output/malformed-resource-details-cannot-control-the-terminal
func TestSyncVerboseMalformedDetailCannotControlTerminal(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeFile(t, filepath.Join(repoRoot, "bundle", ".devin", "hooks.v1.json"), "{\"\\u001b[2JFORGED\\nLINE\":}")
	writeFile(t, filepath.Join(repoRoot, "bundle", ".devin", "skills", "ok", "SKILL.md"), "---\nname: ok\ndescription: Inert skill\n---\nInert body.\n")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
sources:
  diag:
    directory: ./bundle
    source_harness: devin
resources:
  diag:
    include:
      - skill/ok
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync", "--verbose")
	if err != nil {
		t.Fatalf("expected sync --verbose to succeed: %v\n%s", err, output)
	}
	if strings.ContainsRune(output, '\x1b') {
		t.Fatalf("malformed detail wrote a raw escape sequence:\n%q", output)
	}
	var entry string
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "  - hooks.v1.json") {
			entry = line
		}
	}
	if entry == "" {
		t.Fatalf("malformed detail entry missing:\n%s", output)
	}
	if !strings.Contains(entry, `\x1b[2JFORGED\nLINE`) {
		t.Fatalf("malformed detail did not keep the forged key on its own line as visible escapes: %q", entry)
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "LINE") {
			t.Fatalf("parser error forged a separate output line:\n%s", output)
		}
	}
}
