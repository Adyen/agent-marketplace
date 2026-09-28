package e2e_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Shared aggregate files mix handwritten entries with MCP servers a pre-v4
// release installed. The candidate must recognize that release's ownership
// evidence: keep the handwritten entries, keep managing its own entries
// through sync and migration, and remove only those when deselected.
func TestPreV4AggregateOwnershipUpgrade(t *testing.T) {
	if os.Getenv("AM_E2E_PRE_V4_BINARY") == "" {
		t.Skip("historical executable is supplied by the dedicated upgrade gate")
	}
	historical, arch := verifiedExecutable(t, "AM_E2E_PRE_V4_BINARY", preV4ReleaseArch)
	world := newHistoricalWorld(t, upgradeImage(t))
	candidate := world.buildCandidate(arch, "e2e")
	home, workspace, registry := world.path("home"), world.path("workspace"), world.path("registry")
	for _, dir := range []string{home, filepath.Join(workspace, ".agents"), filepath.Join(registry, "skills", "review")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(registry, "skills", "review", "SKILL.md"), skillDocument("review", "Review body"))
	write(filepath.Join(registry, ".mcp.json"), `{"mcpServers":{"team-server":{"command":"echo","args":["team"]}}}`)
	const handwritten = `{"command":"echo","args":["mine"]}`
	jsonAggregates := map[string]string{
		"claude": filepath.Join(workspace, ".mcp.json"),
		"gemini": filepath.Join(workspace, ".gemini", "settings.json"),
		"devin":  filepath.Join(workspace, ".devin", "mcp_config.json"),
	}
	write(jsonAggregates["claude"], `{"mcpServers":{"handwritten":`+handwritten+`}}`)
	write(jsonAggregates["gemini"], `{"theme":"Default","mcpServers":{"handwritten":`+handwritten+`}}`)
	write(jsonAggregates["devin"], `{"mcpServers":{"handwritten":`+handwritten+`}}`)
	codexConfig := filepath.Join(workspace, ".codex", "config.toml")
	write(codexConfig, "# my own Codex server\n[mcp_servers.handwritten]\ncommand = \"echo\"\nargs = [\"mine\"]\n")

	must := func(executable string, args ...string) {
		t.Helper()
		world.must(home, workspace, executable, args...)
	}
	// managed lists the harnesses whose native aggregate should currently
	// carry team-server; every native aggregate must keep the handwritten one.
	assertAggregates := func(stage string, managed ...string) {
		t.Helper()
		for harness, path := range jsonAggregates {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%s: read %s aggregate: %v", stage, harness, err)
			}
			var doc struct {
				Theme      string                     `json:"theme"`
				MCPServers map[string]json.RawMessage `json:"mcpServers"`
			}
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Fatalf("%s: %s aggregate is not JSON: %v\n%s", stage, harness, err, data)
			}
			var mine struct {
				Command string   `json:"command"`
				Args    []string `json:"args"`
			}
			if err := json.Unmarshal(doc.MCPServers["handwritten"], &mine); err != nil || mine.Command != "echo" || len(mine.Args) != 1 || mine.Args[0] != "mine" {
				t.Fatalf("%s: %s lost the handwritten server:\n%s", stage, harness, data)
			}
			if harness == "gemini" && doc.Theme != "Default" {
				t.Fatalf("%s: gemini lost an unrelated setting:\n%s", stage, data)
			}
			if _, ok := doc.MCPServers["team-server"]; ok != slices.Contains(managed, harness) {
				t.Fatalf("%s: %s team-server present=%v, want %v:\n%s", stage, harness, ok, !ok, data)
			}
		}
		data, err := os.ReadFile(codexConfig)
		if err != nil {
			t.Fatalf("%s: read codex aggregate: %v", stage, err)
		}
		codex := string(data)
		if !strings.Contains(codex, "# my own Codex server\n[mcp_servers.handwritten]") {
			t.Fatalf("%s: codex lost the handwritten server or its comment:\n%s", stage, codex)
		}
		if got := strings.Contains(codex, "team-server"); got != slices.Contains(managed, "codex") {
			t.Fatalf("%s: codex team-server present=%v:\n%s", stage, got, codex)
		}
	}

	must(historical, "init", "--harnesses", "claude,gemini,devin,codex")
	repoConfig := filepath.Join(workspace, ".agents", "config.yaml")
	write(repoConfig, fmt.Sprintf("apiVersion: agent-marketplace/v2\nregistries:\n  - path: %q\n    skills:\n      include: [review]\n    mcp:\n      include: [team-server]\n", registry))
	must(historical, "sync")
	// v3.1.1 installed Claude and Gemini MCP servers as plugin-style output
	// and only Devin and Codex into their native aggregates.
	assertAggregates("v3.1.1 sync", "devin", "codex")
	pluginOutputs := map[string][]byte{}
	for rel, entry := range map[string]string{
		".claude/.mcp.json":                  "team-server",
		".claude/.claude-plugin/plugin.json": "review",
		".gemini/gemini-extension.json":      "team-server",
	} {
		data, err := os.ReadFile(filepath.Join(workspace, rel))
		if err != nil {
			t.Fatalf("v3.1.1 did not write plugin-style output %s: %v", rel, err)
		}
		if !strings.Contains(string(data), entry) {
			t.Fatalf("v3.1.1 plugin-style output %s lacks %s:\n%s", rel, entry, data)
		}
		pluginOutputs[rel] = data
	}
	assertPluginOutputsUntouched := func(stage string) {
		t.Helper()
		for rel, want := range pluginOutputs {
			if got, err := os.ReadFile(filepath.Join(workspace, rel)); err != nil || string(got) != string(want) {
				t.Fatalf("%s: previously generated plugin-style output %s changed: %q, %v", stage, rel, got, err)
			}
		}
	}

	must(candidate, "sync")
	assertAggregates("candidate legacy sync", "claude", "gemini", "devin", "codex")
	assertPluginOutputsUntouched("candidate legacy sync")
	must(candidate, "sync", "--check")
	must(candidate, "migrate", "--scope", "repo")
	must(candidate, "sync")
	assertAggregates("candidate after repository migration", "claude", "gemini", "devin", "codex")
	must(candidate, "sync", "--check")

	must(candidate, "remove", "team-server", "--kind", "mcp", "--scope", "repo")
	assertAggregates("candidate removal")
	assertPluginOutputsUntouched("candidate removal")
	must(candidate, "sync", "--check")
}
