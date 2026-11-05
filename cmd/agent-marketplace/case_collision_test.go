package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
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

// spec: harness-compilation/required-resource-keeps-a-case-only-collision
func TestSyncRequiredResourceKeepsCaseOnlyCollision(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	orgOrigin, orgWorking := createBareRepoWithWorkingClone(t, "org.git")
	writeCommand(t, orgWorking, "deploy", "Mandatory organization command", "echo org")
	commitAndPushAll(t, orgWorking, "organization command")
	// OpenCode names a command after its file, so the name keeps its case.
	userOrigin, userWorking := createBareRepoWithWorkingClone(t, "user.git")
	writeFile(t, filepath.Join(userWorking, ".opencode", "commands", "Deploy.md"), "---\ndescription: User selected command\n---\nRun the user command.\n")
	commitAndPushAll(t, userWorking, "user command")
	orgPath := filepath.Join(t.TempDir(), "org.yaml")
	writeConfig(t, orgPath, "apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  org:\n    git: file://"+orgOrigin+"\n    ref: main\nresources:\n  org:\n    all: true\n    required: true\n")
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  user:\n    git: file://"+userOrigin+"\n    ref: main\n    source_harness: opencode\nresources:\n  user:\n    include: [command/Deploy]\n")
	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, repoRoot)
	if output, err := executeCommand("lock", "--scope", "organization"); err != nil {
		t.Fatalf("seed organization lock: %v\n%s", err, output)
	}

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, output)
	}
	commands := filepath.Join(home, ".claude", "commands")
	entries, err := os.ReadDir(commands)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if strings.Join(names, ",") != "deploy.md" {
		t.Fatalf("installed commands = %v, want only deploy.md\n%s", names, output)
	}
	data, err := os.ReadFile(filepath.Join(commands, "deploy.md"))
	if err != nil || !strings.Contains(string(data), "Mandatory organization command") {
		t.Fatalf("deploy.md does not hold the required command: %v\n%s", err, data)
	}
	if !strings.Contains(output, "materialize.collision.shared-path") || !strings.Contains(output, `command "Deploy"`) || strings.Contains(output, "bundle.conflict.duplicate-artifact-path") {
		t.Fatalf("sync must report the displaced command, not refuse both:\n%s", output)
	}
	if output, err := executeCommand("sync", "--check"); err != nil {
		t.Fatalf("sync --check after sync: %v\n%s", err, output)
	}
}
