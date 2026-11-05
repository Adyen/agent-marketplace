package opencode

import (
	"encoding/json"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/opencodehook"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"gopkg.in/yaml.v3"
)

// spec: harness-opencode/agent-restrictions-follow-permission-granularity
func TestAgentRestrictionsFollowPermissionGranularity(t *testing.T) {
	for _, tc := range []struct {
		name            string
		tools, disallow []string
		want            map[string]string
	}{
		{name: "whole edit permission allowed", tools: []string{"Read", "Edit", "Write"}, want: map[string]string{"*": "deny", "read": "allow", "edit": "allow"}},
		{name: "legacy write spelling", tools: []string{"Edit", "Create"}, want: map[string]string{"*": "deny", "edit": "allow"}},
		{name: "whole edit permission denied", disallow: []string{"Edit", "Write"}, want: map[string]string{"edit": "deny"}},
		{name: "allowed write without edit", tools: []string{"Read", "Write"}},
		{name: "allowed edit without write", tools: []string{"Edit"}},
		{name: "denied write without edit", disallow: []string{"Write"}},
		{name: "unknown tool", tools: []string{"NotebookEdit"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := ir.NewSubagent("reviewer", "Review", "Body", "", tc.tools, "", nil, "", nil, ir.SourceRef{})
			agent.DisallowedTools = tc.disallow
			result, err := compile.Compile(agent, "opencode")
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == nil {
				if len(result.Artifacts) != 0 || !diag.HasCode(result.Diagnostics, diag.CodeSubagentOpenCodeRestrictionUnsupported) {
					t.Fatalf("partially representable restriction was emitted: %#v", result)
				}
				return
			}
			if len(result.Artifacts) != 1 || diag.HasErrors(result.Diagnostics) {
				t.Fatalf("representable restriction refused: %#v", result)
			}
			permission, _ := frontmatterField(t, result.Artifacts[0].Bytes)["permission"].(map[string]any)
			if len(permission) != len(tc.want) {
				t.Fatalf("permission=%v, want %v", permission, tc.want)
			}
			for key, action := range tc.want {
				if permission[key] != action {
					t.Fatalf("permission=%v, want %v", permission, tc.want)
				}
			}
			if _, unenforced := permission["write"]; unenforced {
				t.Fatal("write is checked under the edit permission; a write rule is never enforced")
			}
		})
	}

	// Authored order still decides precedence: the catch-all deny comes first.
	agent := ir.NewSubagent("reviewer", "Review", "Body", "", []string{"Bash"}, "", nil, "", nil, ir.SourceRef{})
	result, err := compile.Compile(agent, "opencode")
	if err != nil || len(result.Artifacts) != 1 {
		t.Fatalf("compile: %v %#v", err, result)
	}
	var document struct {
		Permission yaml.Node `yaml:"permission"`
	}
	if _, rest, ok := cutFrontmatter(result.Artifacts[0].Bytes); !ok || yaml.Unmarshal(rest, &document) != nil || len(document.Permission.Content) != 4 || document.Permission.Content[0].Value != "*" || document.Permission.Content[2].Value != "bash" {
		t.Fatalf("permission order: %s", result.Artifacts[0].Bytes)
	}
}

func cutFrontmatter(data []byte) ([]byte, []byte, bool) {
	text := string(data)
	if len(text) < 4 || text[:4] != "---\n" {
		return nil, nil, false
	}
	for i := 4; i+4 <= len(text); i++ {
		if text[i:i+4] == "---\n" && text[i-1] == '\n' {
			return []byte(text[i+4:]), []byte(text[4:i]), true
		}
	}
	return nil, nil, false
}

func declarationOf(t *testing.T, h *ir.Hook, options ...compile.Option) (opencodehook.Declaration, []diag.Diagnostic) {
	t.Helper()
	result, err := compile.Compile(h, "opencode", options...)
	if err != nil {
		t.Fatal(err)
	}
	var declaration opencodehook.Declaration
	for _, a := range result.Artifacts {
		if opencodehook.IsDeclarationPath(a.Path) {
			if err := json.Unmarshal(a.Bytes, &declaration); err != nil {
				t.Fatal(err)
			}
		}
	}
	return declaration, result.Diagnostics
}

// spec: opencode-hooks/hooks-from-other-harnesses-translate-through-the-catalogs
func TestHooksFromOtherHarnessesTranslateThroughCatalogs(t *testing.T) {
	for _, tc := range []struct{ source, event, matcher string }{
		{"claude", "PreToolUse", "Bash"},
		{"gemini", "BeforeTool", "run_shell_command"},
		{"cursor", "preToolUse", "Shell"},
		{"devin", "PreToolUse", "^exec$"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			h := ir.NewHookWithMatcherOptions("check", tc.event, tc.matcher, "true", "command", nil, ir.SourceRef{}, ir.WithHookSource(tc.source))
			declaration, diagnostics := declarationOf(t, h)
			if diag.HasErrors(diagnostics) || declaration.Event != "PreToolUse" || declaration.Matcher != "Bash" {
				t.Fatalf("declaration=%#v diagnostics=%v", declaration, diagnostics)
			}
		})
	}

	// A source deadline in milliseconds is converted to the adapter's seconds.
	timeout := 30000
	h := ir.NewHookWithMatcherOptions("check", "BeforeTool", "run_shell_command", "true", "command", nil, ir.SourceRef{}, ir.WithHookSource("gemini"), ir.WithHookTimeoutPtr(&timeout))
	if declaration, diagnostics := declarationOf(t, h); diag.HasErrors(diagnostics) || declaration.Timeout != 30 {
		t.Fatalf("declaration=%#v diagnostics=%v", declaration, diagnostics)
	}
}

// spec: opencode-hooks/partial-file-modification-matchers-are-refused
func TestPartialFileModificationMatchersAreRefused(t *testing.T) {
	for _, matcher := range []string{"Edit", "Write", "MultiEdit"} {
		declaration, diagnostics := declarationOf(t, ir.NewHookWithMatcher("check", "PreToolUse", matcher, "true", "command", nil, ir.SourceRef{}))
		if declaration.Format != "" || !diag.HasCode(diagnostics, diag.CodeHookOpenCodeUnsupported) {
			t.Fatalf("%s: partial file-modification matcher emitted %#v %v", matcher, declaration, diagnostics)
		}
	}
	declaration, diagnostics := declarationOf(t, ir.NewHookWithMatcher("check", "PreToolUse", "Edit|Write", "true", "command", nil, ir.SourceRef{}))
	if diag.HasErrors(diagnostics) || declaration.Matcher != "Write|Edit|apply_patch" {
		t.Fatalf("declaration=%#v diagnostics=%v", declaration, diagnostics)
	}
}

// spec: opencode-hooks/support-and-project-references-follow-the-adapter-project-directory
func TestSupportAndProjectReferencesTranslate(t *testing.T) {
	for _, tc := range []struct {
		name, command string
		options       []compile.Option
		want          string
	}{
		{"repository support", "${CLAUDE_PLUGIN_ROOT}/check.sh", []compile.Option{compile.WithPluginRoot(".opencode/hook-support/src/check")}, `"$AGENT_MARKETPLACE_PROJECT_DIR"/.opencode/hook-support/src/check/check.sh`},
		{"machine support", "${CLAUDE_PLUGIN_ROOT}/check.sh", []compile.Option{compile.WithPluginRoot("/home/user/.config/opencode/hook-support/src/check"), compile.WithScope(compile.ScopeUser)}, "/home/user/.config/opencode/hook-support/src/check/check.sh"},
		{"project directory", `"$CLAUDE_PROJECT_DIR"/scripts/check.sh`, nil, `"$AGENT_MARKETPLACE_PROJECT_DIR"/scripts/check.sh`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			declaration, diagnostics := declarationOf(t, ir.NewHookWithMatcher("check", "PreToolUse", "Bash", tc.command, "command", nil, ir.SourceRef{}), tc.options...)
			if diag.HasErrors(diagnostics) || declaration.Command != tc.want {
				t.Fatalf("command = %q, want %q (%v)", declaration.Command, tc.want, diagnostics)
			}
		})
	}
	// Without an installed support directory a plugin-root reference has no
	// target and is refused rather than pointed into a cache or source tree.
	for _, options := range [][]compile.Option{nil, {compile.WithScope(compile.ScopeUser)}} {
		result, err := compile.Compile(ir.NewHookWithMatcher("check", "PreToolUse", "Bash", "${CLAUDE_PLUGIN_ROOT}/check.sh", "command", nil, ir.SourceRef{}), "opencode", options...)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Artifacts) != 0 || !diag.HasCode(result.Diagnostics, diag.CodeHookOpenCodeUnsupported) || !diag.HasCode(result.Diagnostics, diag.CodeHookSupportPathUnrepresentable) {
			t.Fatalf("unsupported plugin-root reference emitted: %#v", result)
		}
	}
}

// spec: harness-opencode/a-tool-override-applies-consistently
func TestPermissionOverridesNeverNameARemappedTool(t *testing.T) {
	compileWith := func(tools, disallow []string, override map[string]string) *compile.Result {
		t.Helper()
		agent := ir.NewSubagent("reviewer", "Review", "Body", "", tools, "", nil, "", nil, ir.SourceRef{})
		agent.DisallowedTools = disallow
		result, err := compile.Compile(agent, "opencode", compile.WithToolOverride(override))
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	for _, remapped := range []string{"write", "apply_patch", "list_mcp_resources"} {
		for _, lists := range [][2][]string{{{"X"}, nil}, {nil, {"X"}}} {
			result := compileWith(lists[0], lists[1], map[string]string{"X": remapped})
			if len(result.Artifacts) != 0 || !diag.HasCode(result.Diagnostics, diag.CodeSubagentOpenCodeRestrictionUnsupported) {
				t.Fatalf("override to %s emitted an unenforced rule: %#v", remapped, result)
			}
		}
	}
	// A tool OpenCode checks under its own ID, such as an MCP tool, stays a
	// valid consumer mapping.
	result := compileWith(nil, []string{"X"}, map[string]string{"X": "docs_search"})
	if len(result.Artifacts) != 1 || diag.HasErrors(result.Diagnostics) {
		t.Fatalf("MCP tool override refused: %#v", result)
	}
	if permission, _ := frontmatterField(t, result.Artifacts[0].Bytes)["permission"].(map[string]any); permission["docs_search"] != "deny" {
		t.Fatalf("permission = %v, want docs_search denied", permission)
	}
	// Every tool a permission key governs under another name is remapped.
	for _, permission := range permissionTools {
		for _, id := range permission.tools {
			if id != permission.key && !remappedPermissionTools[id] {
				t.Fatalf("%s is checked under %s but is not refused as an override", id, permission.key)
			}
		}
	}
}
