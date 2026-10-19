package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// spec: harness-compilation/case-only-name-collisions-are-refused-on-install
func TestSyncRefusesCaseOnlyNameCollisions(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("CODEX_HOME", filepath.Join(homeDir, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	repoRoot := filepath.Join(t.TempDir(), "repo")
	for file, name := range map[string]string{"a.toml": "Foo", "b.toml": "foo", "c.toml": "bar"} {
		writeFile(t, filepath.Join(repoRoot, "src", ".codex", "agents", file), "name = \""+name+"\"\ndescription = \"Agent\"\ndeveloper_instructions = \"Inert.\"\n")
	}
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
harnesses: [codex]
sources:
  local:
    directory: ./src
    source_harness: codex
resources:
  local:
    include:
      - subagent/Foo
      - subagent/foo
      - subagent/bar
`)
	withWorkingDir(t, repoRoot)

	output, _ := executeCommand("sync")
	if !strings.Contains(output, "bundle.conflict.duplicate-artifact-path") || !strings.Contains(output, `"Foo"`) || !strings.Contains(output, `"foo"`) {
		t.Fatalf("sync did not refuse the case-only collision:\n%s", output)
	}
	entries, err := os.ReadDir(filepath.Join(repoRoot, ".codex", "agents"))
	if err != nil {
		t.Fatalf("read installed agents: %v\n%s", err, output)
	}
	var installed []string
	for _, entry := range entries {
		installed = append(installed, entry.Name())
	}
	if strings.Join(installed, ",") != "bar.toml" {
		t.Fatalf("installed agents = %v, want only bar.toml\n%s", installed, output)
	}
}
