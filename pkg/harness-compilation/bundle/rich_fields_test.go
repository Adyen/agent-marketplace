package bundle

import (
	"testing"
	"testing/fstest"
)

func TestRichFieldsLoadTypedAndPreserveUnknowns(t *testing.T) {
	src := fstest.MapFS{
		"skills/rich/SKILL.md": {Data: []byte("---\nname: rich\ndescription: rich skill\nargument-hint: <file>\nmodel: sonnet\nallowed-tools: [Read, Bash]\npermissions:\n  allow: [Read]\n  ask: [Bash]\n  deny: [Write]\ntriggers: [user, model]\nsubagent: reviewer\nagent: named-agent\nunknown: keep\n---\nbody")},
		"agents/reviewer.md":   {Data: []byte("---\nname: reviewer\nmax-nesting: 0\nunknown: keep\n---\nprompt")},
	}
	skill, err := LoadSkill(src, "skills/rich")
	if err != nil {
		t.Fatal(err)
	}
	if skill.ArgumentHint != "<file>" || skill.Model != "sonnet" || skill.Subagent != "reviewer" || skill.Agent != "named-agent" {
		t.Fatalf("skill typed fields: %+v", skill)
	}
	if len(skill.AllowedTools) != 2 || len(skill.Permissions.Deny) != 1 || len(skill.Triggers) != 2 {
		t.Fatalf("skill collections: %+v", skill)
	}
	if _, ok := skill.Extra["unknown"]; !ok {
		t.Fatal("unknown skill field was not preserved")
	}

	sub, err := LoadSubagent(src, "agents/reviewer.md")
	if err != nil {
		t.Fatal(err)
	}
	if sub.MaxNesting == nil || *sub.MaxNesting != 0 {
		t.Fatalf("max nesting presence/value: %#v", sub.MaxNesting)
	}
	if _, ok := sub.Extra["unknown"]; !ok {
		t.Fatal("unknown subagent field was not preserved")
	}
	absentSrc := fstest.MapFS{"agent.md": {Data: []byte("---\nname: agent\n---\nbody")}}
	absent, err := LoadSubagent(absentSrc, "agent.md")
	if err != nil {
		t.Fatal(err)
	}
	if absent.MaxNesting != nil {
		t.Fatalf("absent max nesting should remain nil: %#v", absent.MaxNesting)
	}
}

func TestRichHookAndMCPFields(t *testing.T) {
	src := fstest.MapFS{
		"hooks.json": {Data: []byte(`{"hooks":{"UserPromptSubmit":[{"type":"prompt","prompt":"check safely","timeout":0,"matcher":"*","unknown":true}]}}`)},
		"mcp.json":   {Data: []byte(`{"mcpServers":{"remote":{"url":"https://example.invalid/mcp","transport":"sse","headers":{"X-Token":"${TOKEN}"},"disabled":false,"oauthClientId":"client","oauthClientSecret":"secret-value","oauthResource":"https://example.invalid","unknown":true}}}`)},
	}
	hooks, _, err := LoadHooks(src, "hooks.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(hooks) != 1 || hooks[0].Prompt != "check safely" || hooks[0].Timeout == nil || *hooks[0].Timeout != 0 {
		t.Fatalf("hook fields: %+v", hooks)
	}
	if _, ok := hooks[0].Extra["unknown"]; !ok {
		t.Fatal("unknown hook field was not preserved")
	}

	servers, err := LoadMcpServers(src, "mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 {
		t.Fatal("expected one MCP server")
	}
	server := servers[0]
	if server.URL == "" || server.Transport != "sse" || server.Disabled || server.OAuthClientID != "client" || server.OAuthClientSecret != "secret-value" || server.OAuthResource == "" {
		t.Fatalf("MCP fields: %+v", server)
	}
	if !server.DisabledSet {
		t.Fatal("explicit disabled=false lost presence")
	}
	if _, ok := server.Extra["unknown"]; !ok {
		t.Fatal("unknown MCP field was not preserved")
	}
}
