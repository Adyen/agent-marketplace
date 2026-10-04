package e2e_test

import (
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func compatibilityResourceProbes(harness string, f compatibilityFixture) []compatibilityNativeProbe {
	var probes []compatibilityNativeProbe
	switch harness {
	case "opencode":
		probes = append(probes,
			compatibilityNativeProbe{kind: "skill", list: []string{"debug", "skill"}, names: []string{f.name("skill")}},
			compatibilityNativeProbe{kind: "command", list: []string{"debug", "config"}, names: []string{f.name("command")}},
			compatibilityNativeProbe{kind: "subagent", list: []string{"agent", "list"}, names: []string{f.name("subagent")}},
		)
	case "devin":
		for _, kind := range []string{"skill", "command"} {
			probes = append(probes, compatibilityNativeProbe{kind: kind, list: []string{"skills", "list"}, names: []string{f.name(kind)}})
		}
	case "gemini":
		probes = append(probes, compatibilityNativeProbe{kind: "skill", list: []string{"skills", "list"}, names: []string{f.name("skill")}})
	case "claude":
		for _, kind := range []string{"skill", "command", "subagent"} {
			probes = append(probes, compatibilityNativeProbe{kind: kind, protocol: "claude-initialize", names: []string{f.name(kind)}})
		}
	case "codex":
		for _, kind := range []string{"skill", "command"} {
			probes = append(probes, compatibilityNativeProbe{kind: kind, protocol: "codex-skills", names: []string{f.name(kind)}})
		}
		probes = append(probes, compatibilityNativeProbe{kind: "hook", protocol: "codex-hooks", names: []string{f.name("hook-a"), f.name("hook-b")}})
	case "factory":
		for _, item := range []struct{ kind, protocol string }{{"skill", "factory-skills"}, {"command", "factory-commands"}, {"subagent", "factory-droids"}} {
			probes = append(probes, compatibilityNativeProbe{kind: item.kind, protocol: item.protocol, names: []string{f.name(item.kind)}})
		}
	case "cursor":
	default:
		return nil
	}
	list := []string{"mcp", "list"}
	if harness == "codex" {
		list = append(list, "--json")
	}
	return append(probes, compatibilityNativeProbe{kind: "mcp", list: list, names: []string{f.name("mcp-a"), f.name("mcp-b")}})
}

func compatibilityNativeGap(harness, kind string) string {
	if !target.IsValid(harness) || !slices.Contains(compatibilityResources, kind) {
		return ""
	}
	if kind == "collection" {
		return "collection is a virtual Marketplace resource; native validation applies to its expanded members"
	}
	if harness == "windsurf" {
		return "Cascade resource management is editor-hosted; no headless resource inventory CLI is documented"
	}
	if kind == "rule" {
		switch harness {
		case "claude", "gemini", "devin", "codex", "opencode":
			return "the compiler explicitly suppresses this target's rule output; omission and diagnostic are tested"
		default:
			return "no credential-free CLI inventory of native rule files is exposed; model compliance is not a listing check"
		}
	}
	return map[string]map[string]string{
		"opencode": {"hook": "plugins have no native hook inventory command; the separate pinned opencode-probe gate executes installed command callbacks"},
		"claude":   {"hook": "CLI initialization lists commands and agents, not installed hooks; SDK hook callbacks are caller-supplied handlers, not discovery"},
		"codex":    {"subagent": "codex agents lists running sessions, not custom agent definitions; the inspected app-server catalogs do not expose a definition inventory"},
		"devin":    {"subagent": "custom profiles are selected by run_subagent; no standalone profile inventory CLI is documented", "hook": "/hooks is an in-session command; no standalone credential-free hook inventory is documented"},
		"factory":  {"hook": "/hooks is an interactive manager; the documented resource RPC surface has no hook inventory method"},
		"gemini":   {"command": "/commands list is session-scoped; headless input authenticates before slash-command dispatch", "subagent": "/agents list is session-scoped; headless input authenticates before slash-command dispatch", "hook": "/hooks list is session-scoped; terminal gemini hooks exposes migration, not listing"},
		"cursor":   {"skill": "skill discovery is session-scoped; ACP requires authentication before session metadata", "command": "command discovery is session-scoped; ACP requires authentication before session metadata", "subagent": "no standalone custom-agent definition inventory is documented; ACP session metadata requires authentication", "hook": "no standalone hook inventory is documented; editor/session handling is not certified by MCP listing"},
	}[harness][kind]
}

func TestCompatibilityEveryResourceHasAProbeOrAnExplicitGap(t *testing.T) {
	for _, definition := range target.Definitions() {
		harness := string(definition.Target)
		probed := make(map[string]bool)
		for _, probe := range compatibilityResourceProbes(harness, compatibilityFixture{prefix: "amcompat-" + harness}) {
			if probed[probe.kind] || len(probe.names) == 0 || len(probe.list) == 0 && probe.protocol == "" {
				t.Fatalf("invalid or duplicate native probe for %s/%s", harness, probe.kind)
			}
			probed[probe.kind] = true
		}
		for _, kind := range []string{"skill", "command", "subagent", "hook", "mcp", "collection", "rule"} {
			gap := compatibilityNativeGap(harness, kind)
			if probed[kind] == (gap != "") {
				t.Errorf("%s/%s must have exactly one native probe or a specific limitation", harness, kind)
			}
		}
	}
	if compatibilityNativeGap("undeclared", "skill") != "" {
		t.Fatal("a new harness must not receive an implicit coverage exemption")
	}
}
