package e2e_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Explicit per-invocation environment overrides are not exposed by table steps.
// spec: claude-native-configuration/machine-selections-install-under-the-configuration-directory
func TestClaudeConfigurationDirectory(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	custom := filepath.Join(filepath.Dir(env.home), "claude-account")
	override := map[string]string{"CLAUDE_CONFIG_DIR": custom}
	defaultRaw := `{"numStartups":42,"mcpServers":{"mine-home":{"command":"true"}}}`
	customRaw := `{"numStartups":7,"mcpServers":{"mine-alt":{"command":"true"}}}`
	env.WriteFile("/root/.claude.json", defaultRaw)
	if err := os.MkdirAll(custom, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(custom, ".claude.json"), []byte(customRaw), 0o600); err != nil {
		t.Fatal(err)
	}
	env.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
	env.WriteRepoConfig("apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
	registry := env.CreateBareRepo("claude-account", RegistryFilesEx(
		[]Skill{{Name: "hello"}}, nil, nil, []MCPServer{{Name: "demo", Command: "echo"}},
	))
	run := func(args ...string) {
		t.Helper()
		result := env.RunAMWithEnv(override, args...)
		if result.ExitCode != 0 {
			t.Fatalf("%v: exit=%d\n%s", args, result.ExitCode, result.Combined)
		}
	}
	run("install", "--from", registry, "--scope", "user")
	run("sync")
	for _, rel := range []string{"skills/hello/SKILL.md", ".claude-plugin/plugin.json", ".claude.json"} {
		if _, err := os.Stat(filepath.Join(custom, rel)); err != nil {
			t.Fatalf("relocated %s: %v", rel, err)
		}
	}
	after, err := os.ReadFile(filepath.Join(custom, ".claude.json"))
	if err != nil || !bytes.Contains(after, []byte(`"demo"`)) || !bytes.Contains(after, []byte(`"mine-alt"`)) || !bytes.Contains(after, []byte(`"numStartups":7`)) {
		t.Fatalf("relocated MCP was lost: %s (%v)", after, err)
	}
	run("sync", "--check")
	run("update")
	run("sync", "--check")
	run("remove", "demo", "--scope", "user")
	run("sync")
	after, err = os.ReadFile(filepath.Join(custom, ".claude.json"))
	if err != nil || bytes.Contains(after, []byte(`"demo"`)) || !bytes.Contains(after, []byte(`"mine-alt"`)) {
		t.Fatalf("last MCP removal lost account state: %s (%v)", after, err)
	}
	got, err := os.ReadFile(filepath.Join(env.home, ".claude.json"))
	if err != nil || string(got) != defaultRaw {
		t.Fatalf("default account changed: %s (%v)", got, err)
	}
	if _, err := os.Stat(filepath.Join(env.home, ".claude")); !os.IsNotExist(err) {
		t.Fatalf("default root was created: %v", err)
	}
}
