package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// spec: claude-native-configuration/status-doctor-and-backup-resolve-the-relocated-root
// spec: claude-native-configuration/removing-the-last-mcp-server-keeps-the-configuration-file
func TestClaudeConfigurationDirectoryCommands(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home, repo, source, custom := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", custom)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	config.SetTestV3OrganizationConfigPath(t, filepath.Join(home, "no-org.yaml"))
	writeSkill(t, source, "hello", "Hello fixture", nil)
	writeFile(t, filepath.Join(source, ".claude-plugin/plugin.json"), `{"name":"fixture","version":"1.0.0"}`)
	writeFile(t, filepath.Join(source, ".mcp.json"), `{"mcpServers":{"demo":{"command":"echo","args":["hello"]}}}`)
	writeConfig(t, filepath.Join(home, ".agents/config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
	writeConfig(t, filepath.Join(repo, ".agents/config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
	defaultFile := filepath.Join(home, ".claude.json")
	defaultRaw := `{"numStartups":42,"mcpServers":{"default":{"command":"true"}}}`
	writeFile(t, defaultFile, defaultRaw)
	file := filepath.Join(custom, ".claude.json")
	original := `{"numStartups":7,"mcpServers":{"manual":{"command":"true"}}}`
	writeFile(t, file, original)
	withWorkingDir(t, repo)
	run := func(args ...string) string {
		t.Helper()
		output, err := executeCommand(args...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, output)
		}
		return output
	}
	run("install", "--from", source, "--scope", "user")
	run("sync")
	synced, err := os.ReadFile(file)
	if err != nil || !bytes.Contains(synced, []byte(`"demo"`)) || !bytes.Contains(synced, []byte(`"manual"`)) {
		t.Fatalf("relocated MCP = %s (%v)", synced, err)
	}
	skill, err := os.ReadFile(filepath.Join(custom, "skills/hello/SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	run("sync", "--check")
	for _, command := range []string{"status", "doctor"} {
		output := run(command)
		if !strings.Contains(output, custom) || strings.Contains(output, filepath.Join(home, ".claude")) {
			t.Fatalf("%s resolves wrong root:\n%s", command, output)
		}
	}
	run("backup", "create")
	writeFile(t, file, `{"changed":true}`)
	run("backup", "restore", "--target", "claude")
	if got, err := os.ReadFile(file); err != nil || !bytes.Equal(got, synced) {
		t.Fatalf("restored MCP = %s (%v)", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(custom, "skills/hello/SKILL.md")); err != nil || !bytes.Equal(got, skill) {
		t.Fatalf("restored skill = %s (%v)", got, err)
	}
	run("remove", "demo", "--scope", "user")
	run("sync")
	after, err := os.ReadFile(file)
	if err != nil || bytes.Contains(after, []byte(`"demo"`)) || !bytes.Contains(after, []byte(`"manual"`)) || !bytes.Contains(after, []byte(`"numStartups":7`)) {
		t.Fatalf("last MCP removal = %s (%v)", after, err)
	}
	if got, err := os.ReadFile(defaultFile); err != nil || string(got) != defaultRaw {
		t.Fatalf("default account changed = %s (%v)", got, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
		t.Fatalf("default root created: %v", err)
	}
}

// spec: materialization/relative-claude-configuration-directory-is-a-hard-error-in-summaries
func TestClaudeConfigurationDirectoryRelativeSummaries(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home, repo := t.TempDir(), t.TempDir()
	source := t.TempDir()
	writeSkill(t, source, "hello", "Relative root control", nil)
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "relative-claude")
	config.SetTestV3OrganizationConfigPath(t, filepath.Join(home, "no-org.yaml"))
	writeConfig(t, filepath.Join(home, ".agents/config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  fixture:\n    directory: "+source+"\nresources:\n  fixture: {include: [skill/hello]}\n")
	writeConfig(t, filepath.Join(repo, ".agents/config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
	withWorkingDir(t, repo)
	for _, args := range [][]string{{"status"}, {"doctor"}, {"backup", "create"}, {"backup", "restore", "--target", "claude"}} {
		output, err := executeCommand(args...)
		if err == nil || !strings.Contains(err.Error()+output, "CLAUDE_CONFIG_DIR must be absolute") {
			t.Fatalf("%v accepted relative root: %v\n%s", args, err, output)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
		t.Fatalf("relative refusal created default root: %v", err)
	}
}

// spec: claude-native-configuration/repository-scope-ignores-the-variable
func TestClaudeConfigurationRepositoryScope(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home, repo, source := t.TempDir(), t.TempDir(), t.TempDir()
	custom := filepath.Join(t.TempDir(), "unused-account")
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", custom)
	config.SetTestV3OrganizationConfigPath(t, filepath.Join(home, "no-org.yaml"))
	writeSkill(t, source, "hello", "Repository root control", nil)
	writeFile(t, filepath.Join(source, ".claude-plugin/plugin.json"), `{"name":"fixture","version":"1.0.0"}`)
	writeFile(t, filepath.Join(source, ".mcp.json"), `{"mcpServers":{"demo":{"command":"echo"}}}`)
	writeConfig(t, filepath.Join(home, ".agents/config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
	writeConfig(t, filepath.Join(repo, ".agents/config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  fixture:\n    directory: "+source+"\nresources:\n  fixture: {include: [skill/hello, mcp/demo]}\n")
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("repository sync: %v\n%s", err, output)
	}
	for _, file := range []string{".claude/skills/hello/SKILL.md", ".mcp.json"} {
		if _, err := os.Stat(filepath.Join(repo, file)); err != nil {
			t.Fatalf("missing repository output %s: %v", file, err)
		}
	}
	if _, err := os.Stat(custom); !os.IsNotExist(err) {
		t.Fatalf("repository sync created machine output: %v", err)
	}
}
