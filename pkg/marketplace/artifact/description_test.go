package artifact

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestExtractDescriptionCoversEveryResourceKind(t *testing.T) {
	const description = "Analyze user retention and engagement patterns by cohort."

	for _, test := range []struct {
		name     string
		resource ir.Resource
		want     string
	}{
		{
			name:     "skill",
			resource: ir.NewSkill("retention", description, "body", nil, nil, ir.SourceRef{}),
			want:     description,
		},
		{
			name:     "command",
			resource: ir.NewCommand("retention", description, "body", nil, ir.SourceRef{}),
			want:     description,
		},
		{
			name:     "rule",
			resource: ir.NewRule("retention", description, "body", nil, false, nil, ir.SourceRef{}),
			want:     description,
		},
		{
			name:     "subagent",
			resource: ir.NewSubagent("retention", description, "body", "sonnet", nil, "", nil, "", nil, ir.SourceRef{}),
			want:     description,
		},
		{
			name:     "collection",
			resource: ir.NewCollection("bundle", "1.0.0", description, nil, nil, nil, ir.SourceRef{}),
			want:     description,
		},
		{
			// A hook declares no description in the IR. Synthesizing one from
			// Event/Type would publish internal wiring as prose.
			name:     "hook has no description",
			resource: ir.NewHook("guard", "PreToolUse", "echo hi", "command", nil, ir.SourceRef{}),
			want:     "",
		},
		{
			// An MCP server's nearest field is its executable command line.
			// That must never surface as a user-facing description.
			name:     "mcp server does not leak its command",
			resource: ir.NewMcpServer("server", "/usr/bin/secret-tool --token abc", nil, nil, nil, ir.SourceRef{}),
			want:     "",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ExtractDescription(test.resource); got != test.want {
				t.Fatalf("ExtractDescription = %q, want %q", got, test.want)
			}
		})
	}
}

func TestExtractDescriptionHandlesMissingResources(t *testing.T) {
	if got := ExtractDescription(nil); got != "" {
		t.Fatalf("ExtractDescription(nil) = %q, want empty", got)
	}

	// A typed-nil pointer arrives as a non-nil interface and must not panic.
	for name, resource := range map[string]ir.Resource{
		"skill":      (*ir.Skill)(nil),
		"command":    (*ir.Command)(nil),
		"rule":       (*ir.Rule)(nil),
		"subagent":   (*ir.Subagent)(nil),
		"collection": (*ir.Collection)(nil),
	} {
		if got := ExtractDescription(resource); got != "" {
			t.Fatalf("ExtractDescription(typed-nil %s) = %q, want empty", name, got)
		}
	}
}

func TestExtractDescriptionBoundsUntrustedContent(t *testing.T) {
	hostile := "line one\nline two\x1b[31m" + strings.Repeat("x", store.MaxDescriptionBytes*2)
	got := ExtractDescription(ir.NewSkill("hostile", hostile, "body", nil, nil, ir.SourceRef{}))

	if len(got) > store.MaxDescriptionBytes {
		t.Fatalf("extracted length = %d, want <= %d", len(got), store.MaxDescriptionBytes)
	}
	if strings.ContainsAny(got, "\n\x1b") {
		t.Fatalf("extracted description retained control characters: %q", got[:32])
	}
}
