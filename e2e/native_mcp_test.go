package e2e_test

import "fmt"

func init() {
	for _, target := range []string{"claude", "gemini"} {
		for _, scope := range []string{"repo", "user"} {
			for _, mode := range []string{"symlink", "file"} {
				native, legacy := "/workspace/.mcp.json", "/workspace/.claude/.mcp.json"
				if scope == "user" {
					native, legacy = "/root/.claude.json", "/root/.claude/.mcp.json"
				}
				if target == "gemini" {
					native, legacy = "/workspace/.gemini/settings.json", "/workspace/.gemini/gemini-extension.json"
					if scope == "user" {
						native, legacy = "/root/.gemini/settings.json", "/root/.gemini/gemini-extension.json"
					}
				}
				cfg := fmt.Sprintf("apiVersion: agent-marketplace/v2\nharnesses: [%s]\nmaterialization:\n  mode: %s\nregistries:\n  - url: {{REG:native-mcp}}\n    ref: main\n    mcp: [native-first, native-second]\n", target, mode)
				fixture := RegistryDef{Name: "native-mcp", MCPServers: []MCPServer{{Name: "native-first", Command: "echo", Args: []string{"first"}}, {Name: "native-second", Command: "echo", Args: []string{"second"}}}}
				if target == "gemini" {
					fixture.Files = map[string]string{"hooks/hooks.json": `{"hooks":{"PostToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo generated-audit"}]}]}}`}
					cfg += "    hooks: [PostToolUse-command]\n"
				}
				tc := TestCase{Name: "native_mcp/" + target + "/" + scope + "/" + mode, Desc: "Given native settings and two selected MCP servers, When sync, check, status, and removal run, Then native files preserve handwritten settings and only managed servers are pruned", Registries: []RegistryDef{fixture}, GlobalCfg: "apiVersion: agent-marketplace/v2\nharnesses: [" + target + "]\n"}
				if scope == "user" {
					tc.GlobalCfg = cfg
				} else {
					tc.RepoCfg = cfg
				}
				original := `{"theme":"dark","mcpServers":{"manual":{"command":"manual-tool"}},"hooks":{"AfterTool":[{"matcher":"manual","hooks":[{"type":"command","command":"echo handwritten"}]}]}}`
				tc.Steps = []Step{
					{Action: ActionWriteFile, Path: native, Body: original},
					{Args: []string{"sync"}, MustPass: true, RegularFiles: []string{native}, FilesAbsent: []string{legacy}, FileContains: map[string]string{native: "native-first"}},
					{Args: []string{"sync"}, MustPass: true, PathsUnchanged: []string{native}, FileContains: map[string]string{native: "native-second"}},
					{Args: []string{"sync", "--check"}, MustPass: true, PathsUnchanged: []string{native}},
					{Args: []string{"status"}, MustPass: true, OutContains: []string{"native-first", "native-second"}, PathsUnchanged: []string{native}},
					{Args: []string{"remove", "native-first", "--kind", "mcp", "--scope", scope}, MustPass: true, FileNotContains: map[string]string{native: "native-first"}, FileContains: map[string]string{native: "native-second"}},
					{Args: []string{"remove", "native-second", "--kind", "mcp", "--scope", scope}, MustPass: true, FileNotContains: map[string]string{native: "native-second"}, FileContains: map[string]string{native: "manual-tool"}},
					{Args: []string{"sync", "--check"}, MustPass: true, PathsUnchanged: []string{native}, FilesAbsent: []string{legacy}, FileContains: map[string]string{native: "echo handwritten"}},
				}
				if target == "gemini" {
					tc.Steps = append(tc.Steps, Step{FileContains: map[string]string{native: "echo generated-audit"}})
				}
				testTable = append(testTable, tc)
			}
		}
	}
}
