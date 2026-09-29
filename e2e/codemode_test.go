package e2e_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

func TestCodeModeJourney(t *testing.T) {
	env := newEnv(t)

	env.WriteFile("/workspace/registry/skills/greeter/SKILL.md",
		"---\nname: greeter\ndescription: Greeting skill\n---\n\n# greeter\nSay hello.\n")
	env.WriteFile("/workspace/registry/skills/code-mode/SKILL.md",
		"---\nname: code-mode\ndescription: Code-mode bootstrap skill\n---\n\n# code-mode\n")

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
		fmt.Sprintf(`{"mcpServers":{"fake":{"command":"sh","args":["registry/fake-mcp.sh"]},"agent-marketplace":{"command":%q,"args":["mcp"]}}}`, env.binary))

	env.WriteFile("/workspace/registry/collections/code-mode/COLLECTION.yaml",
		"name: code-mode\nresources:\n  - name: code-mode\n    kind: skill\n  - name: agent-marketplace\n    kind: mcp\n")

	env.WriteRepoConfig(`apiVersion: agent-marketplace/v4
harnesses: [claude, codex, devin]
sources:
  reg:
    directory: ./registry
resources:
  reg:
    include: [skill/greeter]
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

	env.MustRunAM("install", "code-mode")
	env.MustRunAM("sync")
	for f := range owned {
		if !containsOwned(f) {
			t.Fatalf("owned code-mode entry missing from %s", f)
		}
	}
	if !env.CheckFileExists("/workspace/.claude/skills/code-mode/SKILL.md") {
		t.Fatal("collection skill member not materialized")
	}
	env.AssertFileContains("/workspace/.mcp.json", "handwritten")

	session := env.Run(fmt.Sprintf(`printf '%%s\n' '%s' '%s' '%s' '%s' | %q mcp`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"search","arguments":{"code":"async () => catalog.map(r => r.ref)"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"execute","arguments":{"servers":["local:reg/mcp/fake"],"code":"async () => [(await skills.load(\"local:reg/skill/greeter\")).digest.slice(0,7), (await mcp.fake.ping()).text]"}}}`,
		env.binary))
	if session.ExitCode != 0 {
		t.Fatalf("mcp session failed (exit %d):\n%s", session.ExitCode, session.Combined)
	}
	for _, want := range []string{`"search"`, `"execute"`, "local:reg/skill/greeter", "pong"} {
		if !strings.Contains(session.Stdout, want) {
			t.Fatalf("mcp session output missing %q:\n%s", want, session.Stdout)
		}
	}

	log := env.MustRun(`cat "$XDG_STATE_HOME/agent-marketplace/mcp-sessions/"*.jsonl`)
	for _, want := range []string{"skill.load", "server.start", "local:reg/skill/greeter", "local:reg/mcp/fake"} {
		if !strings.Contains(log.Stdout, want) {
			t.Fatalf("session log missing %q:\n%s", want, log.Stdout)
		}
	}

	// spec: code-mode-mcp/recorded-refs-remain-promotable
	registryDir := env.mustResolvePath("/workspace/registry")
	writeCodeModeSessionEntry(env, "sess-old-label", "skill.load",
		"local:registry/skill/greeter", "", "", registryDir)
	env.MustRunAM("promote", "local:reg/skill/greeter", "--scope", "repo")
	env.MustRunAM("promote", "local:registry/skill/greeter", "--scope", "repo")
	env.AssertFileContains("/workspace/.agents/config.yaml", "greeter")

	env.MustRunAM("remove", "code-mode")
	env.MustRunAM("sync")
	env.AssertFileContains("/workspace/.mcp.json", "handwritten")
	for f := range owned {
		if containsOwned(f) {
			t.Fatalf("owned code-mode entry not pruned: %s", f)
		}
	}
	if env.CheckFileExists("/workspace/.claude/skills/code-mode/SKILL.md") {
		t.Fatal("collection skill member not pruned")
	}
	if !env.CheckFileExists("/workspace/.claude/skills/greeter/SKILL.md") {
		t.Fatal("independently selected greeter skill was removed")
	}
}

func writeCodeModeSessionEntry(env *Env, session, event, ref, revision, digest, source string) {
	env.t.Helper()
	entry := fmt.Sprintf(
		`{"ts":"2026-09-29T00:00:00Z","session":%q,"event":%q,"ref":%q,"revision":%q,"digest":%q,"source":%q}`+"\n",
		session, event, ref, revision, digest, source)
	env.WriteFile("/root/.local/state/agent-marketplace/mcp-sessions/"+session+".jsonl", entry)
}

// spec: code-mode-mcp/retired-enablement-fails-with-migration-guidance
func TestCodeModeLegacyInvocationCoverage(t *testing.T) {
	legacy := newEnv(t)
	legacy.InitGitRepo()
	legacy.WriteRepoConfig("apiVersion: agent-marketplace/v2\nregistries: []\n")
	if r := legacy.RunAM("mcp"); r.ExitCode != 0 {
		t.Fatalf("mcp over stdio with EOF failed (exit %d):\n%s", r.ExitCode, r.Combined)
	}
	for _, args := range [][]string{
		{"mcp", "disable", "--scope", "user"},
		{"mcp", "enable", "--scope", "user"},
		{"init", "--harnesses", "factory", "--code-mode"},
	} {
		if r := legacy.RunAM(args...); r.ExitCode == 0 {
			t.Fatalf("%v unexpectedly succeeded:\n%s", args, r.Combined)
		}
	}

	legacy.WriteRepoConfig("apiVersion: agent-marketplace/v4\nharnesses: [factory]\ncodeMode:\n  enabled: true\n")
	if r := legacy.RunAM("sync"); r.ExitCode == 0 || !strings.Contains(r.Combined, "codeMode") {
		t.Fatalf("expected codeMode migration diagnostic:\n%s", r.Combined)
	}

	promoteEnv := newEnv(t)
	promoteEnv.InitGitRepo()
	promoteEnv.WriteFile("/workspace/registry/skills/greeter/SKILL.md",
		"---\nname: greeter\ndescription: Greeting skill\n---\n\n# greeter\nSay hello.\n")
	registryDir := promoteEnv.mustResolvePath("/workspace/registry")
	promoteEnv.WriteGlobalConfig("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []\n")
	promoteEnv.WriteRepoConfig("apiVersion: agent-marketplace/v2\nregistries:\n  - url: " + registryDir + "\n")
	writeCodeModeSessionEntry(promoteEnv, "sess-legacy", "skill.load",
		"local:registry/skill/greeter", "", "", registryDir)
	promoteEnv.MustRunAM("promote", "local:registry/skill/greeter", "--scope", "repo")
	promoteEnv.AssertFileContains("/workspace/.agents/config.yaml", "greeter")
}

// spec: code-mode-mcp/marketplace-skill-and-declared-server-execute-through-the-gate
func TestCodeModeMarketplaceCatalogJourney(t *testing.T) {
	fakeScript := `while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\s*\([0-9][0-9]*\).*/\1/p')
  [ -z "$id" ] && continue
  case "$line" in
    *tools/list*) printf '{"jsonrpc":"2.0","id":%s,"result":{"tools":[{"name":"ping","description":"pong","inputSchema":{"type":"object","properties":{}}}]}}\n' "$id" ;;
    *tools/call*) printf '{"jsonrpc":"2.0","id":%s,"result":{"content":[{"type":"text","text":"pong"}]}}\n' "$id" ;;
    *) printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"fake","version":"0"}}}\n' "$id" ;;
  esac
done`
	files := marketplaceBehaviorFiles("Marketplace journey skill", false)
	files[".mcp.json"] = MergeMCPFile([]MCPServer{{Name: "fake", Command: "sh", Args: []string{"-c", fakeScript}}})
	fixture := newMarketplaceBehaviorFixture(t, files)
	catalogCfg := fixture.cfg
	catalogCfg.CatalogProjection.Enabled = true
	fixture.setHandler(mpapi.NewServer(catalogCfg, fixture.meta, fixture.blobs).PublicHandler())

	consumer := fixture.newConsumer(t, fixture.server.URL)

	session := consumer.Run(fmt.Sprintf(
		`{ printf '%%s\n' '%s' '%s'; sleep 5; printf '%%s\n' '%s' '%s'; } | %q mcp`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"search","arguments":{"code":"async () => catalog.map(r => r.ref)"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"search","arguments":{"code":"async () => catalog.map(r => r.ref)"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"execute","arguments":{"servers":["marketplace:behavior-source/mcp/fake"],"code":"async () => [(await skills.load(\"marketplace:behavior-source/skill/skill-one\")).content.includes(\"Marketplace journey skill\"), (await mcp.fake.listTools())[0].inputSchema.type, (await mcp.fake.ping()).text]"}}}`,
		consumer.binary))
	if session.ExitCode != 0 {
		t.Fatalf("mcp session failed (exit %d):\n%s", session.ExitCode, session.Combined)
	}
	for _, want := range []string{
		`catalog.stale`,
		`marketplace:behavior-source/skill/skill-one`,
		`marketplace:behavior-source/mcp/fake`,
		`[true,\"object\",\"pong\"]`,
	} {
		if !strings.Contains(session.Stdout, want) {
			t.Fatalf("mcp session output missing %q:\n%s", want, session.Stdout)
		}
	}

	log := consumer.MustRun(`cat "$XDG_STATE_HOME/agent-marketplace/mcp-sessions/"*.jsonl`)
	for _, want := range []string{"skill.load", "server.start", "marketplace:" + fixture.server.URL} {
		if !strings.Contains(log.Stdout, want) {
			t.Fatalf("session log missing %q:\n%s", want, log.Stdout)
		}
	}

	// A second session reuses the persisted complete catalog before refresh.
	second := consumer.Run(fmt.Sprintf(
		`printf '%%s\n' '%s' | %q mcp`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":{"code":"async () => catalog.map(r => r.ref)"}}}`,
		consumer.binary))
	if second.ExitCode != 0 {
		t.Fatalf("second mcp session failed (exit %d):\n%s", second.ExitCode, second.Combined)
	}
	if !strings.Contains(second.Stdout, `marketplace:behavior-source/skill/skill-one`) {
		t.Fatalf("cached catalog not reused at startup:\n%s", second.Stdout)
	}
}

// spec: code-mode-mcp/marketplace-findings-gate-on-consent
func TestCodeModePromoteMarketplaceRiskFlags(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("Promote risk flags", false))
	skillDigest := fixture.resourceDigest(t, "skill", "skill-one")
	commandDigest := fixture.resourceDigest(t, "command", "command-one")

	legacy := fixture.newConsumer(t, fixture.server.URL)
	writeCodeModeSessionEntry(legacy, "sess-mp", "skill.load",
		"behavior:behavior-source/skill/skill-one", skillDigest, skillDigest, "marketplace:"+fixture.server.URL)
	legacy.MustRunAM("promote", "behavior:behavior-source/skill/skill-one", "--accept-risk")
	if !legacy.CheckFileExists("/workspace/.factory/skills/skill-one/SKILL.md") && !legacy.CheckFileExists("/root/.factory/skills/skill-one/SKILL.md") {
		t.Fatal("promoted skill was not materialized for the legacy consumer")
	}
	writeCodeModeSessionEntry(legacy, "sess-mp-cmd", "skill.load",
		"behavior:behavior-source/command/command-one", commandDigest, commandDigest, "marketplace:"+fixture.server.URL)
	legacy.MustRunAM("promote", "behavior:behavior-source/command/command-one", "--yes")

	v4c := newV4MarketplaceConsumer(t, fixture)
	writeCodeModeSessionEntry(v4c, "sess-mp-v4", "skill.load",
		"org:behavior-source/skill/skill-one", skillDigest, skillDigest, "marketplace:"+fixture.server.URL)
	v4c.MustRunAM("promote", "org:behavior-source/skill/skill-one", "--accept-risk")
	if !v4c.CheckFileExists("/workspace/.factory/skills/skill-one/SKILL.md") && !v4c.CheckFileExists("/root/.factory/skills/skill-one/SKILL.md") {
		t.Fatal("promoted skill was not materialized for the v4 consumer")
	}
	writeCodeModeSessionEntry(v4c, "sess-mp-v4-cmd", "skill.load",
		"org:behavior-source/command/command-one", commandDigest, commandDigest, "marketplace:"+fixture.server.URL)
	v4c.MustRunAM("promote", "org:behavior-source/command/command-one", "--yes")
}

// spec: code-mode-mcp/environment-references-expanded-before-start
func TestCodeModeRemoteHeaderInterpolationAndTruncation(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "initialize":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"remote"}}}`, req.ID)
		case "tools/list":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"tools":[{"name":"auth"},{"name":"boom"}]}}`, req.ID)
		case "tools/call":
			if req.Params.Name == "boom" {
				fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32000,"message":"remote refused"}}`, req.ID)
				return
			}
			auth, _ := json.Marshal(r.Header.Get("Authorization"))
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"content":[{"type":"text","text":%s}]}}`, req.ID, auth)
		default:
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"unknown"}}`, req.ID)
		}
	}))
	defer remote.Close()

	env := newEnv(t)
	env.WriteFile("/workspace/registry/.mcp.json",
		fmt.Sprintf(`{"mcpServers":{"remote":{"url":%q,"headers":{"Authorization":"Bearer ${AM_E2E_TOKEN}"}}}}`, remote.URL))
	env.WriteRepoConfig(`apiVersion: agent-marketplace/v4
harnesses: [claude]
sources:
  reg:
    directory: ./registry
`)

	session := env.Run(fmt.Sprintf(`printf '%%s\n' '%s' '%s' '%s' | AM_E2E_TOKEN=e2e-secret %q mcp`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"execute","arguments":{"servers":["local:reg/mcp/remote"],"code":"async () => { const auth = (await mcp.remote.auth()).text; let caught = \"missed\"; try { await mcp.remote.boom() } catch (e) { caught = [e instanceof Error, e.name, e.code, e.Code === undefined].join(\"|\") } return [auth, caught] }"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"execute","arguments":{"servers":["local:reg/mcp/remote"],"code":"async () => \"x\".repeat(2 * 1024 * 1024)"}}}`,
		env.binary))
	if session.ExitCode != 0 {
		t.Fatalf("mcp session failed (exit %d):\n%s", session.ExitCode, session.Combined)
	}
	for _, want := range []string{
		`Bearer e2e-secret`,
		`true|CodeModeError|mcp.tool-error|true`,
		`\"truncated\":true`,
		`\"bytes\":`,
	} {
		if !strings.Contains(session.Stdout, want) {
			t.Fatalf("mcp session output missing %q:\n%s", want, session.Stdout)
		}
	}
}
