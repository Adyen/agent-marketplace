package artifact

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: marketplace-api/canonical-null-extra-is-not-a-native-extension
func TestCanonicalHookNullExtraIsNotUnknownMetadata(t *testing.T) {
	hook := ir.NewHook("guard", "PreToolUse", "echo safe", "command", nil, ir.SourceRef{})
	canonical, err := FromIR(hook)
	if err != nil {
		t.Fatal(err)
	}
	resource, err := DecodeResource("hook", canonical.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if extra := resource.(*ir.Hook).Extra; len(extra) != 0 {
		t.Fatalf("canonical scaffolding became an unknown native extension: %#v", extra)
	}
}

func TestDecodeResourceRoundTrip(t *testing.T) {
	skillExtra := map[string]any{"category": "review", "tags": []any{"security", "performance"}}
	commandExtra := map[string]any{"shell": "bash", "nested": map[string]any{"key": "value"}}
	subagentExtra := map[string]any{"owner": "platform"}
	hookExtra := map[string]any{"priority": float64(1)}
	mcpExtra := map[string]any{"custom-transport": "stdio"}

	cases := []struct {
		name string
		r    ir.Resource
	}{
		{
			name: "skill",
			r: ir.NewSkillWithOptions(
				"review",
				"Review code",
				"Review the code carefully.",
				[]string{"user", "model"},
				skillExtra,
				ir.SourceRef{File: "skills/review.md", Line: 12},
				ir.WithSkillArgumentHint("<path>"),
				ir.WithSkillModel("sonnet"),
				ir.WithSkillAllowedTools([]string{"@read-file", "@grep"}),
				ir.WithSkillPermissions(ir.Permissions{Allow: []string{"@read-file"}, Ask: []string{"@write-file"}}),
				ir.WithSkillSubagent("coder"),
				ir.WithSkillAgent("reviewer"),
			),
		},
		{
			name: "command",
			r: ir.NewCommand(
				"lint",
				"Run linter",
				"golangci-lint run ./...",
				commandExtra,
				ir.SourceRef{File: "commands/lint.md", Line: 5},
			),
		},
		{
			name: "subagent",
			r: ir.NewSubagentWithOptions(
				"planner",
				"Plan tasks",
				"Break the request into steps.",
				"sonnet",
				[]string{"@plan"},
				"high",
				[]string{"@dangerous"},
				"worktree",
				subagentExtra,
				ir.SourceRef{File: "subagents/planner.md", Line: 8},
				ir.WithSubagentMaxNesting(3),
			),
		},
		{
			name: "hook",
			r: ir.NewHookWithOptions(
				"pre-tool",
				"PreToolUse",
				"echo pre-tool",
				"command",
				hookExtra,
				ir.SourceRef{File: "hooks/pre-tool.md", Line: 3},
				ir.WithHookPrompt("Review the tool call."),
				ir.WithHookTimeout(30),
			),
		},
		{
			name: "mcpserver",
			r: ir.NewMcpServerWithOptions(
				"server",
				"npx",
				[]string{"-y", "@server/mcp"},
				map[string]string{"API_KEY": "${API_KEY}"},
				mcpExtra,
				ir.SourceRef{File: "mcps/server.md", Line: 7},
				ir.WithMcpURL("https://example.com/mcp"),
				ir.WithMcpTransport("sse"),
				ir.WithMcpHeaders(map[string]string{"Authorization": "Bearer ${TOKEN}"}),
				ir.WithMcpDisabled(true),
				ir.WithMcpOAuth("client-id", "client-secret", "resource"),
			),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a1, err := FromIR(tc.r)
			if err != nil {
				t.Fatalf("FromIR: %v", err)
			}

			r2, err := DecodeResource(a1.Manifest.Kind, a1.Payload)
			if err != nil {
				t.Fatalf("DecodeResource: %v", err)
			}
			if got, want := r2.Kind().String(), a1.Manifest.Kind; got != want {
				t.Fatalf("DecodeResource kind = %q, want %q", got, want)
			}
			if got, want := r2.Name(), tc.r.Name(); got != want {
				t.Fatalf("DecodeResource name = %q, want %q", got, want)
			}

			a2, err := FromIR(r2)
			if err != nil {
				t.Fatalf("FromIR round-trip: %v", err)
			}
			if !bytes.Equal(a1.Payload, a2.Payload) {
				t.Fatalf("payload mismatch after round-trip\nfirst:  %s\nsecond: %s", a1.Payload, a2.Payload)
			}
			if a1.Manifest.ContentDigest != a2.Manifest.ContentDigest {
				t.Fatalf("content digest mismatch: %s vs %s", a1.Manifest.ContentDigest, a2.Manifest.ContentDigest)
			}
		})
	}
}

func TestDecodeResourceErrors(t *testing.T) {
	payload := []byte(`{"name":"review","Description":"Review code","Body":"body"}`)

	if _, err := DecodeResource("unknown-kind", payload); err == nil {
		t.Fatalf("expected unsupported kind error")
	}
	if _, err := DecodeResource("skill", []byte("{invalid")); err == nil {
		t.Fatalf("expected JSON parse error")
	}
	if _, err := DecodeResource("skill", []byte("{}")); err == nil {
		t.Fatalf("expected missing name error")
	}
	if _, err := DecodeResource("skill", []byte(`{"name":"1invalid"}`)); err == nil {
		t.Fatalf("expected invalid name error")
	}
}

func TestDecodeResourcePreservesExtra(t *testing.T) {
	r := ir.NewSkill("review", "Review code", "Review the code carefully.", []string{"user"}, map[string]any{"custom": "value"}, ir.SourceRef{})
	a, err := FromIR(r)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}

	decoded, err := DecodeResource(a.Manifest.Kind, a.Payload)
	if err != nil {
		t.Fatalf("DecodeResource: %v", err)
	}

	s, ok := decoded.(*ir.Skill)
	if !ok {
		t.Fatalf("decoded resource is %T, want *ir.Skill", decoded)
	}
	if s.Extra == nil || s.Extra["custom"] != "value" {
		t.Fatalf("extra not preserved: %#v", s.Extra)
	}
}

func TestDecodeResourceJSONFieldSurvivesUnmarshal(t *testing.T) {
	// Ensure the struct unmarshal path handles a field with a JSON tag in a
	// nested sidecar while the top-level Extra map is rebuilt.
	r := ir.NewMcpServerWithOptions(
		"server",
		"npx",
		[]string{"-y"},
		nil,
		nil,
		ir.SourceRef{},
		ir.WithMcpURL("https://example.com/mcp"),
		ir.WithMcpDisabled(true),
	)
	a, err := FromIR(r)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}

	decoded, err := DecodeResource(a.Manifest.Kind, a.Payload)
	if err != nil {
		t.Fatalf("DecodeResource: %v", err)
	}
	m, ok := decoded.(*ir.McpServer)
	if !ok {
		t.Fatalf("decoded resource is %T, want *ir.McpServer", decoded)
	}
	if m.URL != "https://example.com/mcp" {
		t.Fatalf("URL not preserved: %q", m.URL)
	}
	if !m.Disabled {
		t.Fatalf("Disabled not preserved: %v", m.Disabled)
	}
	if !m.DisabledSet {
		t.Fatalf("DisabledSet not preserved: %v", m.DisabledSet)
	}

	var payloadMap map[string]any
	if err := json.Unmarshal(a.Payload, &payloadMap); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payloadMap["URL"] != "https://example.com/mcp" {
		t.Fatalf("URL missing from payload: %v", payloadMap)
	}
}

func TestDecodeResourceRejectsCaseCollidingKnownAliases(t *testing.T) {
	cases := []struct {
		name   string
		kind   string
		data   string
		secret string
	}{
		{name: "hook command", kind: "hook", data: `{"name":"probe","command":"safe","COMMAND":"command-secret"}`, secret: "command-secret"},
		{name: "hook command reverse", kind: "hook", data: `{"name":"probe","COMMAND":"command-secret","command":"safe"}`, secret: "command-secret"},
		{name: "hook empty command", kind: "hook", data: `{"name":"probe","Command":"","command":"override-secret"}`, secret: "override-secret"},
		{name: "exact command", kind: "hook", data: `{"name":"probe","command":"safe","command":"exact-secret"}`, secret: "exact-secret"},
		{name: "nested source", kind: "hook", data: `{"name":"probe","Source":{"File":"safe","file":"source-secret"}}`, secret: "source-secret"},
		{name: "nested permissions", kind: "skill", data: `{"name":"probe","Permissions":{"allow":["safe"],"Allow":["permissions-secret"]}}`, secret: "permissions-secret"},
		{name: "name alias", kind: "hook", data: `{"name":"probe","Name":"name-secret"}`, secret: "name-secret"},
		{name: "hook source alias", kind: "hook", data: `{"name":"probe","HookSource":"safe","hooksource":"source-secret"}`, secret: "source-secret"},
		{name: "tool source alias", kind: "subagent", data: `{"name":"probe","ToolSource":"safe","toolsource":"source-secret"}`, secret: "source-secret"},
		{name: "skill tool source alias", kind: "skill", data: `{"name":"probe","ToolSource":"safe","toolsource":"source-secret"}`, secret: "source-secret"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeResource(tt.kind, []byte(tt.data))
			var coded *diag.CodedError
			if !errors.As(err, &coded) || coded.Code != diag.CodeJSONDuplicateKey {
				t.Fatalf("error = %v, want coded duplicate-key error", err)
			}
			if strings.Contains(err.Error(), tt.secret) {
				t.Fatalf("error %q leaked payload value", err)
			}
		})
	}
}

func TestDecodeResourceAcceptsSingletonAliasesAndExtras(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{name: "lowercase", data: `{"name":"probe","command":"echo","event":"PreToolUse","type":"command"}`},
		{name: "capitalized", data: `{"name":"probe","Command":"echo","Event":"PreToolUse","Type":"command"}`},
		{name: "extra", data: `{"name":"probe","command":"echo","custom":"preserved"}`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			r, err := DecodeResource("hook", []byte(tt.data))
			if err != nil {
				t.Fatalf("DecodeResource: %v", err)
			}
			hook, ok := r.(*ir.Hook)
			if !ok || hook.Command != "echo" {
				t.Fatalf("decoded hook = %#v", r)
			}
			if tt.name == "extra" && hook.Extra["custom"] != "preserved" {
				t.Fatalf("extra = %#v", hook.Extra)
			}
		})
	}
}

func TestDecodeResourcePreservesCaseDistinctMCPEnvKeys(t *testing.T) {
	r, err := DecodeResource("mcpserver", []byte(`{"name":"probe","Command":"echo","Env":{"TOKEN":"one","token":"two"}}`))
	if err != nil {
		t.Fatalf("DecodeResource: %v", err)
	}
	server, ok := r.(*ir.McpServer)
	if !ok || server.Env["TOKEN"] != "one" || server.Env["token"] != "two" {
		t.Fatalf("decoded env = %#v", server)
	}
}

func TestFromIRRejectsCaseCollidingHookCommandAliases(t *testing.T) {
	cases := []struct {
		name    string
		command string
		secret  string
	}{
		{name: "populated", command: "echo typed", secret: "echo extra"},
		{name: "empty", command: "", secret: "echo extra"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := ir.NewHook(tc.name, "PreToolUse", tc.command, "command", map[string]any{"command": tc.secret}, ir.SourceRef{})
			_, err := FromIR(r)
			var coded *diag.CodedError
			if !errors.As(err, &coded) || coded.Code != diag.CodeJSONDuplicateKey {
				t.Fatalf("error = %v, want coded duplicate-key error", err)
			}
			if strings.Contains(err.Error(), tc.secret) {
				t.Fatalf("error %q leaked payload value", err)
			}
		})
	}
}

func TestFromIRRejectsCaseCollidingMCPTransportAliases(t *testing.T) {
	secret := "transport extra secret"
	r := ir.NewMcpServerWithOptions(
		"server",
		"echo",
		nil,
		nil,
		map[string]any{"transport": secret},
		ir.SourceRef{},
		ir.WithMcpTransport("sse"),
	)
	_, err := FromIR(r)
	var coded *diag.CodedError
	if !errors.As(err, &coded) || coded.Code != diag.CodeJSONDuplicateKey {
		t.Fatalf("error = %v, want coded duplicate-key error", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error %q leaked payload value", err)
	}
}
