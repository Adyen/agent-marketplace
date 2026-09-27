package e2e_test

import (
	"fmt"
	"strings"
	"testing"
)

func TestCodeModeJourney(t *testing.T) {
	env := newEnv(t)

	env.WriteFile("/workspace/registry/skills/greeter/SKILL.md",
		"---\nname: greeter\ndescription: Greeting skill\n---\n\n# greeter\nSay hello.\n")

	env.WriteFile("/workspace/registry/fake-mcp.sh", `#!/bin/sh
while IFS= read -r line; do
  tmp=${line#*\"id\":}
  id=${tmp%%[!0-9]*}
  [ -z "$id" ] && continue
  case "$line" in
    *tools/list*) printf '{"jsonrpc":"2.0","id":%s,"result":{"tools":[{"name":"ping","description":"pong"}]}}\n' "$id" ;;
    *tools/call*) printf '{"jsonrpc":"2.0","id":%s,"result":{"content":[{"type":"text","text":"pong"}]}}\n' "$id" ;;
    *) printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"fake","version":"0"}}}\n' "$id" ;;
  esac
done
`)
	env.WriteFile("/workspace/registry/.mcp.json",
		`{"mcpServers":{"fake":{"command":"sh","args":["registry/fake-mcp.sh"]}}}`)

	env.WriteRepoConfig(`apiVersion: agent-marketplace/v4
harnesses: [claude, codex, devin]
sources:
  reg:
    directory: ./registry
resources:
  reg:
    all: true
`)

	env.WriteFile("/workspace/.mcp.json",
		`{"mcpServers":{"handwritten":{"command":"echo","args":["hi"]}}}`)

	owned := map[string]string{
		"/workspace/.mcp.json":              `"agent-marketplace":`,
		"/workspace/.codex/config.toml":     `[mcp_servers."agent-marketplace"]`,
		"/workspace/.devin/mcp_config.json": `"agent-marketplace":`,
	}
	containsOwned := func(f string) bool {
		return env.CheckFileExists(f) && env.CheckFileContains(f, owned[f])
	}

	env.MustRunAM("sync")
	for f := range owned {
		if containsOwned(f) {
			t.Fatalf("code-mode registered without opt-in: %s", f)
		}
	}
	env.AssertFileContains("/workspace/.mcp.json", "handwritten")

	env.MustRunAM("mcp", "enable", "--scope", "repo")
	env.AssertFileContains("/workspace/.agents/config.yaml", "codeMode")
	env.MustRunAM("sync")
	for f := range owned {
		if !containsOwned(f) {
			t.Fatalf("owned code-mode entry missing from %s", f)
		}
	}
	env.AssertFileContains("/workspace/.mcp.json", "handwritten")

	session := env.Run(fmt.Sprintf(`printf '%%s\n' '%s' '%s' '%s' '%s' | %q mcp`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"search","arguments":{"code":"async () => catalog.map(r => r.ref)"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"execute","arguments":{"servers":["local:registry/mcp/fake"],"code":"async () => [(await skills.load(\"local:registry/skill/greeter\")).digest.slice(0,7), (await mcp.fake.ping()).text]"}}}`,
		env.binary))
	if session.ExitCode != 0 {
		t.Fatalf("mcp session failed (exit %d):\n%s", session.ExitCode, session.Combined)
	}
	for _, want := range []string{`"search"`, `"execute"`, "local:registry/skill/greeter", "pong"} {
		if !strings.Contains(session.Stdout, want) {
			t.Fatalf("mcp session output missing %q:\n%s", want, session.Stdout)
		}
	}

	log := env.MustRun(`cat "$XDG_STATE_HOME/agent-marketplace/mcp-sessions/"*.jsonl`)
	for _, want := range []string{"skill.load", "server.start", "local:registry/skill/greeter", "local:registry/mcp/fake"} {
		if !strings.Contains(log.Stdout, want) {
			t.Fatalf("session log missing %q:\n%s", want, log.Stdout)
		}
	}

	env.MustRunAM("promote", "local:registry/skill/greeter", "--scope", "repo")
	env.AssertFileContains("/workspace/.agents/config.yaml", "greeter")

	env.MustRunAM("mcp", "disable", "--scope", "repo")
	env.MustRunAM("sync")
	env.AssertFileContains("/workspace/.mcp.json", "handwritten")
	for f := range owned {
		if containsOwned(f) {
			t.Fatalf("owned code-mode entry not pruned: %s", f)
		}
	}
}
