package ir

import (
	"testing"
)

func TestResourceKindString(t *testing.T) {
	tests := []struct {
		kind ResourceKind
		want string
	}{
		{KindSkill, "skill"},
		{KindCommand, "command"},
		{KindSubagent, "subagent"},
		{KindHook, "hook"},
		{KindMcpServer, "mcpserver"},
		{KindCollection, "collection"},
		{KindRule, "rule"},
		{ResourceKind(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("ResourceKind(%d).String() = %q, want %q", int(tt.kind), got, tt.want)
		}
	}
}

func TestSkillImplementsResource(t *testing.T) {
	s := NewSkill("my-skill", "A test skill", "# Skill body", []string{"click"}, map[string]any{"foo": "bar"}, SourceRef{File: "skills/my-skill/SKILL.md", Line: 1})
	var r Resource = s
	if r.Kind() != KindSkill {
		t.Errorf("Skill.Kind() = %v, want %v", r.Kind(), KindSkill)
	}
	if r.Name() != "my-skill" {
		t.Errorf("Skill.Name() = %q, want %q", r.Name(), "my-skill")
	}
}

func TestCommandImplementsResource(t *testing.T) {
	c := NewCommand("build", "Build the project", "make build", map[string]any{}, SourceRef{File: "commands/build.md", Line: 1})
	var r Resource = c
	if r.Kind() != KindCommand {
		t.Errorf("Command.Kind() = %v, want %v", r.Kind(), KindCommand)
	}
	if r.Name() != "build" {
		t.Errorf("Command.Name() = %q, want %q", r.Name(), "build")
	}
}

func TestSubagentImplementsResource(t *testing.T) {
	sa := NewSubagent("reviewer", "Reviews code", "Review the changes", "sonnet", []string{"Read", "Write"}, "high", []string{"Bash"}, "worktree", map[string]any{}, SourceRef{File: "agents/reviewer.md", Line: 1})
	var r Resource = sa
	if r.Kind() != KindSubagent {
		t.Errorf("Subagent.Kind() = %v, want %v", r.Kind(), KindSubagent)
	}
	if r.Name() != "reviewer" {
		t.Errorf("Subagent.Name() = %q, want %q", r.Name(), "reviewer")
	}
}

func TestHookImplementsResource(t *testing.T) {
	h := NewHook("pre-commit", "pre-commit", "lint.sh", "command", map[string]any{}, SourceRef{File: "hooks/hooks.json", Line: 5})
	var r Resource = h
	if r.Kind() != KindHook {
		t.Errorf("Hook.Kind() = %v, want %v", r.Kind(), KindHook)
	}
	if r.Name() != "pre-commit" {
		t.Errorf("Hook.Name() = %q, want %q", r.Name(), "pre-commit")
	}
}

func TestMcpServerImplementsResource(t *testing.T) {
	m := NewMcpServer("my-server", "npx", []string{"-y", "my-mcp-server"}, map[string]string{"API_KEY": "secret"}, map[string]any{}, SourceRef{File: ".mcp.json", Line: 3})
	var r Resource = m
	if r.Kind() != KindMcpServer {
		t.Errorf("McpServer.Kind() = %v, want %v", r.Kind(), KindMcpServer)
	}
	if r.Name() != "my-server" {
		t.Errorf("McpServer.Name() = %q, want %q", r.Name(), "my-server")
	}
}

func TestRuleImplementsResource(t *testing.T) {
	r := NewRule("typescript", "TypeScript conventions", "# Rules\nUse interfaces", []string{"*.ts", "*.tsx"}, true, map[string]any{}, SourceRef{File: "rules/typescript.mdc", Line: 1})
	var res Resource = r
	if res.Kind() != KindRule {
		t.Errorf("Rule.Kind() = %v, want %v", res.Kind(), KindRule)
	}
	if res.Name() != "typescript" {
		t.Errorf("Rule.Name() = %q, want %q", res.Name(), "typescript")
	}
	if r.Description != "TypeScript conventions" {
		t.Errorf("Rule.Description = %q, want %q", r.Description, "TypeScript conventions")
	}
	if len(r.Globs) != 2 || r.Globs[0] != "*.ts" {
		t.Errorf("Rule.Globs = %v, want [*.ts *.tsx]", r.Globs)
	}
	if !r.AlwaysApply {
		t.Errorf("Rule.AlwaysApply = false, want true")
	}
	r.SetName("golang")
	if r.Name() != "golang" {
		t.Errorf("after SetName: got %q, want %q", r.Name(), "golang")
	}
}

func TestSetName(t *testing.T) {
	s := NewSkill("old", "", "", nil, nil, SourceRef{})
	s.SetName("new")
	if s.Name() != "new" {
		t.Errorf("after SetName: got %q, want %q", s.Name(), "new")
	}
}

func TestHookPromptIRPreservesPromptWithoutCommand(t *testing.T) {
	hook := NewHookWithOptions("prompt", "UserPromptSubmit", "", "prompt", nil, SourceRef{}, WithHookPrompt("Review this"))
	if hook.Command != "" || hook.Prompt != "Review this" || hook.Type != "prompt" {
		t.Fatalf("prompt hook IR = %#v", hook)
	}
}

// Compile-time guards: the stable constructors must keep fixed signatures.
var (
	_ func(string, string, string, []string, map[string]any, SourceRef) *Skill                                      = NewSkill
	_ func(string, string, string, string, []string, string, []string, string, map[string]any, SourceRef) *Subagent = NewSubagent
	_ func(string, string, string, string, map[string]any, SourceRef) *Hook                                         = NewHook
	_ func(string, string, string, string, string, map[string]any, SourceRef) *Hook                                 = NewHookWithMatcher
	_ func(string, string, []string, map[string]string, map[string]any, SourceRef) *McpServer                       = NewMcpServer
)

func TestStableConstructorsKeepFixedSignatures(t *testing.T) {
	skill := NewSkillWithOptions("rich", "", "", nil, nil, SourceRef{}, WithSkillAllowedTools([]string{}), WithSkillPermissions(Permissions{Allow: []string{}}))
	if skill.AllowedTools == nil || skill.Permissions.Allow == nil || !skill.PermissionsSet {
		t.Fatalf("rich constructor lost explicit empty security lists: %#v", skill)
	}
}

func TestSourceRefZero(t *testing.T) {
	s := SourceRef{}
	if s.File != "" {
		t.Errorf("zero SourceRef.File = %q, want empty", s.File)
	}
	if s.Line != 0 {
		t.Errorf("zero SourceRef.Line = %d, want 0", s.Line)
	}
}
