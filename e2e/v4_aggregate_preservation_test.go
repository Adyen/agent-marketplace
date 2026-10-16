package e2e_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// v4 consumer selection must preserve handwritten native entries, existing
// ownership, and independently seeded historical plugin-style output.
func TestV4AggregateOwnedAndHandwrittenPreservation(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteGlobalConfig("harnesses: [claude, gemini, devin, codex]\n")
	env.WriteFile("/workspace/vendor/registry/skills/review/SKILL.md", (Skill{Name: "review"}).SkillFile())
	env.WriteFile("/workspace/vendor/registry/.mcp.json", `{"mcpServers":{"team-server":{"command":"echo","args":["team"]}}}`)
	const handwritten = `{"command":"echo","args":["mine"]}`
	aggregates := map[string]string{
		"claude": "/workspace/.mcp.json",
		"gemini": "/workspace/.gemini/settings.json",
		"devin":  "/workspace/.devin/mcp_config.json",
	}
	for harness, path := range aggregates {
		body := `{"mcpServers":{"handwritten":` + handwritten + `}}`
		if harness == "gemini" {
			body = `{"theme":"Default","mcpServers":{"handwritten":` + handwritten + `}}`
		}
		env.WriteFile(path, body)
	}
	const codexPath = "/workspace/.codex/config.toml"
	env.WriteFile(codexPath, "# my own Codex server\n[mcp_servers.handwritten]\ncommand = \"echo\"\nargs = [\"mine\"]\n")
	pluginOutputs := map[string]string{
		"/workspace/.claude/.mcp.json": `{"mcpServers":{"team-server":{"command":"historical-plugin"}}}`,
		"/workspace/.claude/.claude-plugin/plugin.json": `{"name":"historical-review-plugin",
  "skills":[
    {
      "_managed_by": "agent-marketplace",
      "description": "review skill",
      "name": "review"
    }
  ]}`,
		"/workspace/.gemini/gemini-extension.json": `{"name":"historical","mcpServers":{"team-server":{"command":"historical-plugin"}}}`,
	}
	for path, body := range pluginOutputs {
		env.WriteFile(path, body)
	}
	env.WriteRepoConfig("sources: {team: {directory: ./vendor/registry}}\nresources: {team: {include: [skill/review, mcp/team-server]}}\n")
	assertAggregates := func(stage string, managed bool) {
		t.Helper()
		for harness, path := range aggregates {
			data := env.ReadFile(path)
			var doc struct {
				Theme      string                     `json:"theme"`
				MCPServers map[string]json.RawMessage `json:"mcpServers"`
			}
			if err := json.Unmarshal([]byte(data), &doc); err != nil {
				t.Fatalf("%s %s invalid JSON: %v\n%s", stage, harness, err, data)
			}
			var mine struct {
				Command string
				Args    []string
			}
			if err := json.Unmarshal(doc.MCPServers["handwritten"], &mine); err != nil || mine.Command != "echo" || len(mine.Args) != 1 || mine.Args[0] != "mine" {
				t.Fatalf("%s %s lost handwritten server:\n%s", stage, harness, data)
			}
			if harness == "gemini" && doc.Theme != "Default" {
				t.Fatalf("%s lost unrelated Gemini setting", stage)
			}
			if _, exists := doc.MCPServers["team-server"]; exists != managed {
				t.Fatalf("%s %s owned server present=%t want=%t", stage, harness, exists, managed)
			}
		}
		codex := env.ReadFile(codexPath)
		if !strings.Contains(codex, "# my own Codex server\n[mcp_servers.handwritten]") || !strings.Contains(codex, "args = [\"mine\"]") {
			t.Fatalf("%s lost Codex comment or handwritten entry:\n%s", stage, codex)
		}
		if strings.Contains(codex, "team-server") != managed {
			t.Fatalf("%s Codex owned entry presence is wrong:\n%s", stage, codex)
		}
		for path, want := range pluginOutputs {
			if got := env.ReadFile(path); got != want {
				t.Fatalf("%s changed historical plugin output %s:\nbefore:\n%s\nafter:\n%s", stage, path, want, got)
			}
		}
	}
	env.MustRunAM("sync")
	assertAggregates("initial sync", true)
	env.MustRunAM("sync")
	assertAggregates("owned resync", true)
	env.MustRunAM("sync", "--check")
	env.MustRunAM("remove", "team-server", "--kind", "mcp", "--scope", "repo")
	assertAggregates("owned removal", false)
	env.MustRunAM("sync")
	assertAggregates("after removal resync", false)
	env.AssertFileContains("/workspace/.claude/skills/review/SKILL.md", "name: review")
	env.MustRunAM("sync", "--check")
}
