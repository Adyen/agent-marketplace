package factory

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

func TestLoadSkill(t *testing.T) {
	src := fstest.MapFS{
		"skills/my-skill/SKILL.md": {
			Data: []byte("Do the thing\n"),
		},
	}

	skill, err := LoadSkill(src, "skills/my-skill")
	if err != nil {
		t.Fatalf("LoadSkill: %v", err)
	}
	if skill.Name() != "my-skill" {
		t.Fatalf("Name() = %q, want %q", skill.Name(), "my-skill")
	}
	if skill.Body != "Do the thing\n" {
		t.Fatalf("Body = %q, want %q", skill.Body, "Do the thing\n")
	}
}

func TestLoadCommand(t *testing.T) {
	src := fstest.MapFS{
		"commands/my-cmd.md": {
			Data: []byte("echo hi\n"),
		},
	}

	cmd, err := LoadCommand(src, "commands/my-cmd.md")
	if err != nil {
		t.Fatalf("LoadCommand: %v", err)
	}
	if cmd.Name() != "my-cmd" {
		t.Fatalf("Name() = %q, want %q", cmd.Name(), "my-cmd")
	}
	if cmd.Body != "echo hi\n" {
		t.Fatalf("Body = %q, want %q", cmd.Body, "echo hi\n")
	}
}

func TestLoadSubagent(t *testing.T) {
	src := fstest.MapFS{
		"droids/reviewer.md": {
			Data: []byte("---\nname: reviewer\ndescription: Reviews code\nmodel: claude-sonnet-4-5-20250929\ntools:\n  - Read\n  - Edit\n  - Create\nreasoningEffort: high\nisolation: worktree\ncustomFlag: true\n---\nReview code\n"),
		},
	}

	subagent, diags, err := LoadSubagent(src, "droids/reviewer.md")
	if err != nil {
		t.Fatalf("LoadSubagent: %v", err)
	}
	if subagent.Name() != "reviewer" {
		t.Fatalf("Name() = %q, want %q", subagent.Name(), "reviewer")
	}
	if subagent.Model != "sonnet" {
		t.Fatalf("Model = %q, want %q", subagent.Model, "sonnet")
	}
	if got, want := strings.Join(subagent.Tools, ","), "Read,Write"; got != want {
		t.Fatalf("Tools = %q, want %q", got, want)
	}
	if subagent.Effort != "high" {
		t.Fatalf("Effort = %q, want %q", subagent.Effort, "high")
	}
	if subagent.Isolation != "worktree" {
		t.Fatalf("Isolation = %q, want %q", subagent.Isolation, "worktree")
	}
	if subagent.Extra["customFlag"] != true {
		t.Fatalf("Extra[customFlag] = %v, want true", subagent.Extra["customFlag"])
	}
	if !diag.HasCode(diags, diag.CodeIngestFactoryReasoningEffortMappedToModel) {
		t.Fatalf("missing diagnostic %q", diag.CodeIngestFactoryReasoningEffortMappedToModel)
	}
}

func TestLoadSubagent_StrictSecurityFields(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{"disallowed tools", "---\nname: reviewer\ndisallowedTools: []\n---\nBody\n", ""},
		{"malformed disallowed tools", "---\nname: reviewer\ndisallowedTools: [true]\n---\nBody\n", "invalid disallowedTools"},
		{"malformed isolation", "---\nname: reviewer\nisolation: true\n---\nBody\n", "invalid isolation"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := fstest.MapFS{"droids/reviewer.md": {Data: []byte(tt.data)}}
			subagent, _, err := LoadSubagent(src, "droids/reviewer.md")
			if tt.want != "" {
				if err == nil || !strings.Contains(err.Error(), tt.want) {
					t.Fatalf("LoadSubagent error = %v, want substring %q", err, tt.want)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadSubagent: %v", err)
			}
			if subagent.DisallowedTools == nil {
				t.Fatal("DisallowedTools = nil, want explicit empty list")
			}
		})
	}
}

func TestLoadHooks(t *testing.T) {
	src := fstest.MapFS{
		"hooks/hooks.json": {
			Data: []byte(`{"PreToolUse":[{"matcher":"Read","hooks":[{"type":"command","command":"${DROID_PLUGIN_ROOT}/check.sh"}]}]}`),
		},
	}

	hooks, diags, err := LoadHooks(src, "hooks/hooks.json")
	if err != nil {
		t.Fatalf("LoadHooks: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("LoadHooks diagnostics = %v, want 0", diags)
	}
	if len(hooks) != 1 {
		t.Fatalf("len(hooks) = %d, want 1", len(hooks))
	}
	if hooks[0].Command != "${CLAUDE_PLUGIN_ROOT}/check.sh" {
		t.Fatalf("Command = %q, want %q", hooks[0].Command, "${CLAUDE_PLUGIN_ROOT}/check.sh")
	}
	if hooks[0].Matcher != "Read" {
		t.Fatalf("Matcher = %q, want %q", hooks[0].Matcher, "Read")
	}
}

func TestLoadMcpServers(t *testing.T) {
	src := fstest.MapFS{
		"mcp.json": {
			Data: []byte(`{"mcpServers":{"docs":{"command":"npx","args":["-y","docs-server"]}}}`),
		},
	}

	servers, err := LoadMcpServers(src, "mcp.json")
	if err != nil {
		t.Fatalf("LoadMcpServers: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("len(servers) = %d, want 1", len(servers))
	}
	if servers[0].Name() != "docs" {
		t.Fatalf("Name() = %q, want %q", servers[0].Name(), "docs")
	}
}
