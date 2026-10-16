package e2e_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Ordered policy parsing and repeat-sync byte checks are not expressible in
// the declarative table. These checks do not prove vendor runtime enforcement.
func TestNativeSubagentToolPolicyInstallSync(t *testing.T) {
	for _, native := range []struct {
		harness, sourcePath, userPath, key string
		tools                              []string
	}{
		{"factory", ".factory/droids/reader.md", ".factory/droids/reader.md", "tools", []string{"Read", "Grep", "Glob", "LS", "FetchUrl", "ToolSearch", "mcp__reader__lookup"}},
		{"claude", ".claude/agents/reader.md", ".claude/agents/reader.md", "tools", []string{"Read", "Grep", "mcp__reader__lookup"}},
		{"cursor", ".cursor/agents/reader.md", ".cursor/agents/reader.md", "tools", []string{"Read", "Grep", "MCP:lookup"}},
		{"gemini", ".gemini/agents/reader.md", ".gemini/agents/reader.md", "tools", []string{"read_file", "grep_search", "mcp_reader_lookup", "mcp_*"}},
		{"devin", ".devin/agents/reader.md", ".config/devin/agents/reader.md", "allowed-tools", []string{"read", "grep", "glob"}},
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
					body := fmt.Sprintf("---\nname: reader\ndescription: Reader\n%s:\n", native.key)
					for _, tool := range native.tools {
						body += "  - " + tool + "\n"
					}
					body += "---\nRead only.\n"
					url := env.CreateBareRepo("native-reader", map[string]string{native.sourcePath: body})
					env.MustRunAM("install", "reader", "--kind", "subagent", "--from", url, "--scope", scope)
					dst, otherDst := "/workspace/"+native.sourcePath, "/root/"+native.userPath
					if scope == "user" {
						dst, otherDst = otherDst, dst
					}
					env.AssertFileAbsent(otherDst)
					before := env.ReadFile(dst)
					assertDeliveredToolPolicy(t, before, native.key, native.tools)
					env.MustRunAM("sync", "--locked")
					env.MustRunAM("sync", "--check")
					if after := env.ReadFile(dst); after != before {
						t.Fatal("repeat synchronization rewrote the native tool policy")
					}
					env.AssertFileAbsent(otherDst)
				})
			}
		}
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
