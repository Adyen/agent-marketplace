package e2e_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// These journeys parse the delivered ordered policy and verify repeat-sync
// bytes across native layouts, rather than substring-only declarative checks.
// spec: harness-compilation/native-policies-survive-installation-and-synchronization
func TestNativeSubagentToolPolicyInstallSync(t *testing.T) {
	for _, native := range []struct {
		harness, sourcePath, installedPath, key string
		tools                                   []string
	}{
		{"factory", ".factory/droids/reader.md", ".factory/droids/reader.md", "tools", []string{"Read", "Grep", "Glob", "LS", "FetchUrl", "ToolSearch", "native_reader___lookup"}},
		{"claude", ".claude/agents/reader.md", ".claude/agents/reader.md", "tools", []string{"Read", "Grep", "native_reader___lookup"}},
		{"cursor", ".cursor/agents/reader.md", ".cursor/agents/reader.md", "tools", []string{"Read", "Grep", "native_reader___lookup"}},
		{"gemini", ".gemini/agents/reader.md", ".gemini/agents/reader.md", "tools", []string{"Read", "Grep", "native_reader___lookup"}},
		{"devin", ".devin/agents/reader.md", ".devin/agents/reader.md", "allowed-tools", []string{"read", "grep", "glob"}},
	} {
		for _, scope := range []string{"repo", "user"} {
			for _, mode := range []string{"symlink", "file"} {
				t.Run(native.harness+"/"+scope+"/"+mode, func(t *testing.T) {
					t.Parallel()
					env := newEnv(t)
					env.InitGitRepo()
					cfg := fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [%s]\nmaterialization:\n  mode: %s\n", native.harness, mode)
					env.WriteGlobalConfig(cfg)
					if scope == "repo" {
						env.WriteRepoConfig(cfg)
					}
					body := fmt.Sprintf("---\nname: reader\ndescription: Reader\nmodel: sonnet\n%s:\n", native.key)
					for _, tool := range native.tools {
						body += "  - " + tool + "\n"
					}
					body += "---\nRead only.\n"
					url := env.CreateBareRepo("native-reader", map[string]string{native.sourcePath: body})
					env.MustRunAM("install", "reader", "--kind", "subagent", "--from", url, "--scope", scope)
					root, other := "/workspace/", "/root/"
					if scope == "user" {
						root, other = other, root
					}
					dst := root + native.installedPath
					otherDst := other + native.installedPath
					if scope == "repo" && native.harness == "devin" {
						otherDst = "/root/.config/devin/agents/reader.md"
					}
					env.AssertFileAbsent(otherDst)
					if scope == "user" && native.harness == "devin" {
						dst = "/root/.config/devin/agents/reader.md"
					}
					before := env.ReadFile(dst)
					assertDeliveredToolPolicy(t, before, native.key, native.tools)
					env.MustRunAM("sync", "--locked")
					env.MustRunAM("sync", "--check")
					if after := env.ReadFile(dst); after != before {
						t.Fatal("repeat synchronization rewrote the native tool policy")
					}
				})
			}
		}
	}
}

func TestFactoryNativeSubagentToolPolicyConversion(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	env.WriteFile("/workspace/plugin/.factory-plugin/plugin.json", `{"name":"reader-plugin","version":"1.0.0"}`)
	const body = "---\nname: reader\ndescription: Reader\nmodel: inherit\ntools: [Read, Grep, Glob, LS, FetchUrl, ToolSearch, native_reader___lookup]\n---\nRead only.\n"
	env.WriteFile("/workspace/plugin/droids/reader.md", body)
	env.MustRunAM("convert", "--from", "factory", "--to", "factory", "--dry-run", "--out", env.mustResolvePath("/workspace/check-out"), env.mustResolvePath("/workspace/plugin"))
	env.AssertFileAbsent("/workspace/check-out")
	env.MustRunAM("convert", "--from", "factory", "--to", "factory", "--out", env.mustResolvePath("/workspace/out"), env.mustResolvePath("/workspace/plugin"))
	assertDeliveredToolPolicy(t, env.ReadFile("/workspace/out/droids/reader.md"), "tools", []string{"Read", "Grep", "Glob", "LS", "FetchUrl", "ToolSearch", "native_reader___lookup"})
	if got := env.ReadFile("/workspace/plugin/droids/reader.md"); got != body {
		t.Fatal("conversion changed its source policy")
	}
}

func assertDeliveredToolPolicy(t *testing.T, body, key string, want []string) {
	t.Helper()
	parts := strings.SplitN(body, "---\n", 3)
	if len(parts) != 3 || parts[0] != "" {
		t.Fatalf("missing native frontmatter:\n%s", body)
	}
	var fields map[string]any
	if err := yaml.Unmarshal([]byte(parts[1]), &fields); err != nil {
		t.Fatal(err)
	}
	expected := make([]any, len(want))
	for i, tool := range want {
		expected[i] = tool
	}
	if !reflect.DeepEqual(fields[key], expected) {
		t.Fatalf("delivered %s = %#v, want exactly %#v", key, fields[key], expected)
	}
}
