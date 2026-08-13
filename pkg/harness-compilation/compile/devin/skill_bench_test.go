package devin

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// Skill and command translation runs once per selected resource on every sync,
// convert, and status invocation, so it sits on a user-visible latency path.
// Each shape gets its own sub-benchmark: a regression in the rich-field path
// (tool mapping, permissions rendering, extras) must not hide behind the
// minimal path.

func BenchmarkTranslateSkill(b *testing.B) {
	source := ir.SourceRef{File: "skills/demo/SKILL.md", Line: 1}
	cases := []struct {
		name  string
		skill *ir.Skill
	}{
		{
			name:  "minimal",
			skill: ir.NewSkill("demo", "Demo skill", "Body\n", nil, nil, source),
		},
		{
			name: "rich",
			skill: ir.NewSkillWithOptions("demo", "Demo skill", "Body\n", []string{"user", "model"}, nil, source,
				ir.WithSkillArgumentHint("<path>"),
				ir.WithSkillModel("sonnet"),
				ir.WithSkillAllowedTools([]string{"Read", "Edit", "Grep", "Glob", "Bash"}),
				ir.WithSkillPermissions(ir.Permissions{Allow: []string{"Read(**)"}, Ask: []string{"Edit(**)"}, Deny: []string{"Bash(rm:*)"}}),
				ir.WithSkillSubagent("explore"),
				ir.WithSkillAgent("reviewer"),
			),
		},
		{
			name:  "with-extras",
			skill: ir.NewSkill("demo", "Demo skill", "Body\n", []string{"user"}, map[string]any{"alpha": "a", "beta": map[string]any{"nested": true}, "gamma": []any{"x", "y"}}, source),
		},
		{
			name: "unmappable-tool",
			skill: ir.NewSkillWithOptions("demo", "Demo skill", "Body\n", nil, nil, source,
				ir.WithSkillAllowedTools([]string{"Read", "WebSearch"}),
			),
		},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, _, err := translateSkill(c.skill, compile.Options{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkTranslateCommand(b *testing.B) {
	source := ir.SourceRef{File: "commands/demo.md", Line: 1}
	cases := []struct {
		name    string
		command *ir.Command
	}{
		{
			name:    "minimal",
			command: ir.NewCommand("demo", "Demo command", "Body\n", nil, source),
		},
		{
			name:    "with-argument-metadata",
			command: ir.NewCommand("demo", "Demo command", "Body\n", map[string]any{"argument-hint": "<pr>"}, source),
		},
		{
			name:    "with-allowed-tools-extension",
			command: ir.NewCommand("demo", "Demo command", "Body\n", map[string]any{allowedToolsKey: []any{"Read", "Bash"}}, source),
		},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, _, err := translateCommand(c.command, compile.Options{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
