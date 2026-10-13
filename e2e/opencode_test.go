package e2e_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// spec: harness-opencode/all-selectable-resource-kinds-reach-the-native-layout
// spec: harness-opencode/opencode-selection-reaches-consumer-workflows
// spec: harness-opencode/executable-source-content-remains-inert-during-consumer-operations
// spec: harness-opencode/existing-locks-and-harness-targets-remain-compatible
// spec: harness-opencode/existing-configurations-retain-their-harness-targets
func TestE2EOpenCodeCollectionLifecycle(t *testing.T) {
	for _, scope := range []string{"repo", "user"} {
		for _, mode := range []string{"symlink", "file"} {
			t.Run(scope+"/"+mode, func(t *testing.T) {
				env := newEnv(t)
				env.InitGitRepo()
				files := RegistryFilesEx([]Skill{{Name: "review"}}, []Command{{Name: "check"}}, []Subagent{{Name: "reviewer"}}, []MCPServer{{Name: "docs", Command: "touch", Args: []string{"mcp-executed"}}})
				files["hooks/hooks.json"] = `{"hooks":{"PreToolUse":[{"type":"command","matcher":"Bash","command":"touch hook-executed"}]}}`
				files["collections/team/COLLECTION.yaml"] = "name: team\nversion: '1.0'\nresources:\n  - {kind: skill, name: review}\n  - {kind: command, name: check}\n  - {kind: subagent, name: reviewer}\n  - {kind: hook, name: PreToolUse-command}\n  - {kind: mcp, name: docs}\n"
				url := env.CreateBareRepo("native-resources", files)
				env.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
				configuration := fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  tools: {git: '%s', ref: main, source_harness: claude}\nresources:\n  tools: {include: [collection/team]}\n", url)
				env.WriteRepoConfig(configuration)
				env.MustRunAM("sync")
				if _, err := os.Lstat(filepath.Join(env.workspace, ".opencode")); !os.IsNotExist(err) {
					t.Fatal("OpenCode was selected implicitly")
				}
				lockFile := filepath.Join(env.workspace, ".agents", "lock.yaml")
				locked := readFile(t, lockFile)
				env.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [opencode, factory]\nmaterialization:\n  mode: " + mode + "\n")
				if scope == "user" {
					env.WriteRepoConfig("apiVersion: agent-marketplace/v4\n")
					env.WriteGlobalConfig("harnesses: [opencode, factory]\nmaterialization:\n  mode: " + mode + "\n" + configuration)
				}
				env.MustRunAM("sync")
				root, aggregate := filepath.Join(env.workspace, ".opencode"), filepath.Join(env.workspace, "opencode.json")
				if scope == "user" {
					root = filepath.Join(env.home, ".config", "opencode")
					aggregate = filepath.Join(root, "opencode.json")
				}
				for _, rel := range []string{"skills/review/SKILL.md", "commands/check.md", "agents/reviewer.md", "hooks/agent-marketplace/PreToolUse-command.json", "plugins/agent-marketplace.js"} {
					if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
						t.Fatalf("native %s: %v", rel, err)
					}
				}
				if data := readFile(t, filepath.Join(root, "agents", "reviewer.md")); !strings.Contains(data, "mode: subagent") {
					t.Fatal("agent role changed")
				}
				info, err := os.Stat(aggregate)
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatalf("MCP privacy: %v %v", info, err)
				}
				env.MustRunAM("sync", "--check")
				env.MustRunAM("status")
				env.MustRunAM("doctor")
				env.MustRunAM("doctor", "--repair", "--yes")
				if scope == "repo" && readFile(t, lockFile) != locked {
					t.Fatal("healthy committed lock changed")
				}
				for _, sentinel := range []string{"mcp-executed", "hook-executed"} {
					if _, err := os.Lstat(filepath.Join(env.workspace, sentinel)); !os.IsNotExist(err) {
						t.Fatal("source resource executed")
					}
				}
				// The exact cached revision must still work with the registry offline.
				if err := os.Rename(strings.TrimPrefix(url, "file://"), strings.TrimPrefix(url, "file://")+"-offline"); err != nil {
					t.Fatal(err)
				}
				env.MustRunAM("sync", "--locked")
				env.MustRunAM("remove", "team", "--kind", "collection", "--scope", scope)
				if _, err := os.Lstat(filepath.Join(root, "hooks", "agent-marketplace", "PreToolUse-command.json")); !os.IsNotExist(err) {
					t.Fatal("removed hook retained")
				}
				if strings.Contains(readFile(t, aggregate), `"docs"`) {
					t.Fatal("removed MCP retained")
				}
			})
		}
	}
}

// spec: opencode-native-configuration/adoption-is-byte-neutral-and-destructive-repair-requires-consent
// spec: opencode-native-configuration/stale-repair-authority-refuses-publication
func TestE2EOpenCodeOwnershipRepair(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	url := env.CreateBareRepo("repair-resources", RegistryFilesEx(nil, nil, nil, []MCPServer{{Name: "exact", Command: "server"}, {Name: "conflict", Command: "desired"}}))
	env.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [opencode]\n")
	env.WriteRepoConfig(fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  tools: {git: '%s', ref: main, source_harness: claude}\nresources:\n  tools: {include: [mcp/exact, mcp/conflict]}\n", url))
	file := filepath.Join(env.workspace, "opencode.jsonc")
	initial := "{ // handwritten\n \"mcp\": {\"exact\": {\"type\":\"local\",\"command\":[\"server\"]}, \"conflict\": {\"type\":\"local\",\"command\":[\"manual\"]}}\n}\n"
	if err := os.WriteFile(file, []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}
	missing := env.RunAM("doctor", "--repair", "--yes")
	if missing.ExitCode == 0 || !strings.Contains(missing.Combined, "lock.missing") {
		t.Fatalf("invalid acquisition accepted: %s", missing.Combined)
	}
	if readFile(t, file) != initial {
		t.Fatal("refused repair changed config")
	}
	env.MustRunAM("lock", "--scope", "repo")
	repair := env.RunAM("doctor", "--repair", "--yes")
	if !strings.Contains(repair.Combined, "adopted") || strings.Contains(readFile(t, file), "desired") || readFile(t, file) != initial {
		t.Fatalf("adoption changed bytes or replaced conflict: %s", repair.Combined)
	}
	confirmed := runAMInTTY(t, env, "doctor", "--repair", "[y/N]", "y\n")
	if confirmed.ExitCode != 0 || !strings.Contains(readFile(t, file), "desired") || !strings.Contains(readFile(t, file), "// handwritten") {
		t.Fatalf("replacement: %s", confirmed.Combined)
	}
}

// spec: opencode-hooks/required-hook-selections-refuse-unenforceable-activation
func TestE2EOpenCodeRequiredHookRefusal(t *testing.T) {
	for _, schema := range []string{"v2", "v4"} {
		t.Run(schema, func(t *testing.T) {
			env := newEnv(t)
			env.InitGitRepo()
			source := filepath.Join(env.repos, "required-hooks")
			env.WriteFile(filepath.Join(source, ".claude-plugin", "plugin.json"), `{"name":"required-hooks"}`)
			env.WriteFile(filepath.Join(source, "hooks", "hooks.json"), `{"hooks":{"PreToolUse":[{"type":"command","matcher":"Bash","command":"touch hook-executed"}]}}`)
			config := func(required bool) string {
				if schema == "v4" {
					return fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [opencode]\nsources:\n  hooks: {directory: %q, source_harness: claude}\nresources:\n  hooks: {include: [hook/PreToolUse-command], required: %v}\n", source, required)
				}
				return fmt.Sprintf("apiVersion: agent-marketplace/v2\nharnesses: [opencode]\nregistries:\n  - {url: %q, source_harness: claude, hooks: [PreToolUse-command], required: %v}\n", source, required)
			}
			env.WriteGlobalConfig(config(false))
			env.MustRunAM("sync")
			file := filepath.Join(env.home, ".config", "opencode", "hooks", "agent-marketplace", "PreToolUse-command.json")
			prior := readFile(t, file)
			info, err := os.Stat(file)
			if err != nil {
				t.Fatal(err)
			}
			env.WriteGlobalConfig(config(true))
			for _, args := range [][]string{{"sync"}, {"sync", "--check"}} {
				result := env.RunAM(args...)
				if result.ExitCode == 0 || !strings.Contains(result.Combined, "hook.opencode.unsupported") {
					t.Fatalf("required-hook activation was not refused: %s", result.Combined)
				}
				after, err := os.Stat(file)
				if err != nil || readFile(t, file) != prior || after.Mode() != info.Mode() || !after.ModTime().Equal(info.ModTime()) {
					t.Fatal("required-hook refusal changed installed output")
				}
			}
			if _, err := os.Lstat(filepath.Join(env.workspace, "hook-executed")); !os.IsNotExist(err) {
				t.Fatal("consumer executed a source hook")
			}
		})
	}
}
