package e2e_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevinRepoRootEscapePreservesExternalTree(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteGlobalConfig("harnesses: [devin]\n")
	url := env.CreateBareRepo("devin-containment", RegistryFilesEx([]Skill{{Name: "review"}}, nil, nil, []MCPServer{{Name: "fixture-server", Command: "fixture"}}))
	env.WriteRepoConfig(fmt.Sprintf("harnesses: [devin]\nsources: {team: {git: %q, ref: main}}\nresources: {team: {all: true}}\n", url))
	env.MustRunAM("lock")
	outside := filepath.Join(env.home, "foreign-root")
	if err := os.Mkdir(outside, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "mcp_config.json"), []byte(`{"mcpServers":{"personal":{"command":"personal-server"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(env.workspace, ".devin")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	before, err := pathFingerprint(outside)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"sync", "--locked"}, {"sync", "--check"}} {
		result := env.RunAM(args...)
		after, err := pathFingerprint(outside)
		if err != nil || after != before {
			t.Errorf("outside tree changed: %v", err)
		}
		target, err := os.Readlink(link)
		if err != nil || target != outside {
			t.Fatalf("scope alias changed: %v", err)
		}
		if result.ExitCode != 1 || !strings.Contains(result.Combined, "materialize.destination.error") {
			t.Fatalf("missing scoped refusal: %+v", result)
		}
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	env.MustRunAM("sync", "--locked")
	if _, err := os.Stat(filepath.Join(env.workspace, ".devin", "skills", "review", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(env.workspace, ".devin", "mcp_config.json"))
	if err != nil || !strings.Contains(string(data), "fixture-server") {
		t.Fatalf("valid control did not install MCP: %v", err)
	}
	after, err := pathFingerprint(outside)
	if err != nil || after != before {
		t.Fatal("valid control changed external tree")
	}
}
