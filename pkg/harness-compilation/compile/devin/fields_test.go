package devin

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/toolpolicy"
)

// TestResolveAllowedToolsMessagesContainDefaultSetClause verifies that every
// warning produced by resolveAllowedTools uses the shared default-set clause.
// This binds the message producer and the suppressed-tool restatement in
// subagent.go to one source of truth.
func TestResolveAllowedToolsMessagesContainDefaultSetClause(t *testing.T) {
	source := diag.SourceRef{File: "test.md", Line: 1}
	opts := compile.Options{}

	cases := []struct {
		name         string
		canonical    []string
		canonicalSet bool
		extra        map[string]any
		code         string
	}{
		{
			name:         "unsupported extra shape",
			canonical:    nil,
			canonicalSet: false,
			extra:        map[string]any{allowedToolsKey: map[string]any{"read": true}},
			code:         diag.CodeSkillDevinToolUnknown,
		},
		{
			name:         "scoped tool specifier",
			canonical:    []string{"Bash(git commit:*)"},
			canonicalSet: true,
			code:         diag.CodeSkillDevinToolUnknown,
		},
		{
			name:         "unknown tool",
			canonical:    []string{"WebSearch"},
			canonicalSet: true,
			code:         diag.CodeSkillDevinToolUnknown,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, diags := resolveAllowedTools("demo", source, tc.canonical, tc.canonicalSet, tc.extra, opts, "", toolpolicy.SurfaceSubagentTools, tc.code)
			if len(diags) == 0 {
				t.Fatal("expected a diagnostic")
			}
			for _, d := range diags {
				if !strings.Contains(d.Message, AllowedToolsDefaultSetClause) {
					t.Errorf("diagnostic message %q does not contain constant %q", d.Message, AllowedToolsDefaultSetClause)
				}
			}
		})
	}
}
