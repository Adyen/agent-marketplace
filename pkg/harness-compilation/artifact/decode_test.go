package artifact

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestDecodeResourceRoundTrip(t *testing.T) {
	skillExtra := map[string]any{"category": "review", "tags": []any{"security", "performance"}}
	commandExtra := map[string]any{"shell": "bash", "nested": map[string]any{"key": "value"}}
	subagentExtra := map[string]any{"owner": "platform"}
	hookExtra := map[string]any{"priority": float64(1)}
	mcpExtra := map[string]any{"transport": "stdio"}
	ruleExtra := map[string]any{"owner": "security"}
	rule := ir.NewRule(
		"secure-coding",
		"Apply secure coding practices",
		"# Secure coding\n",
		[]string{"**/*.go"},
		false,
		ruleExtra,
		ir.SourceRef{File: "rules/secure-coding.md", Line: 2},
	)
	rule.Trigger = "glob"

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
		{
			name: "rule",
			r:    rule,
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
