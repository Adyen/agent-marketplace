package opencode

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/opencodehook"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/yamlfm"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	ext "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
)

func frontmatterField(t *testing.T, artifact []byte) map[string]any {
	t.Helper()
	fm, _, err := yamlfm.ParseYAMLFrontmatter(strings.NewReader(string(artifact)))
	if err != nil {
		t.Fatal(err)
	}
	return fm
}

// spec: harness-opencode/an-unmappable-canonical-model-is-not-emitted
func TestUnmappableCanonicalModelIsNotEmitted(t *testing.T) {
	for _, resource := range []ir.Resource{
		ir.NewCommand("check", "Check", "Body", map[string]any{"model": "haiku"}, ir.SourceRef{}),
		ir.NewCommand("check", "Check", "Body", map[string]any{"model": "claude-haiku-3-5-20241022"}, ir.SourceRef{}),
		ir.NewSubagent("reviewer", "Review", "Body", "gemini-3-flash-preview", nil, "", nil, "", nil, ir.SourceRef{}),
		ir.NewSubagent("reviewer", "Review", "Body", "gemini-3-preview", nil, "", nil, "", nil, ir.SourceRef{}),
	} {
		result, err := compile.Compile(resource, "opencode")
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Artifacts) != 1 {
			t.Fatalf("model loss must retain the resource: %#v", result)
		}
		if strings.Contains(string(result.Artifacts[0].Bytes), "model:") {
			t.Fatalf("unmappable canonical model was emitted: %s", result.Artifacts[0].Bytes)
		}
		if diag.HasErrors(result.Diagnostics) || !diag.HasCode(result.Diagnostics, diag.CodeModelUnresolved) {
			t.Fatalf("model loss was not reported as a stable warning: %#v", result.Diagnostics)
		}
	}

	// Verified native identifiers and explicit model overrides stay intact.
	nativeCommand := ir.NewCommand("check", "Check", "Body", map[string]any{"model": "haiku"}, ir.SourceRef{})
	nativeCommand.Ext.OpenCode = &ext.OpenCode{Model: "provider/native"}
	for _, tc := range []struct {
		name     string
		resource ir.Resource
		options  []compile.Option
		want     string
	}{
		{"mapped canonical model", ir.NewSubagent("reviewer", "Review", "Body", "sonnet", nil, "", nil, "", nil, ir.SourceRef{}), nil, "anthropic/claude-sonnet-4-5-20250929"},
		{"native model", nativeCommand, nil, "provider/native"},
		{"override", ir.NewSubagent("reviewer", "Review", "Body", "haiku", nil, "", nil, "", nil, ir.SourceRef{}), []compile.Option{compile.WithModelOverride(map[string]string{"haiku": "provider/custom"})}, "provider/custom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := compile.Compile(tc.resource, "opencode", tc.options...)
			if err != nil || len(result.Artifacts) != 1 || diag.HasErrors(result.Diagnostics) {
				t.Fatalf("compile: %v %#v", err, result)
			}
			fm := frontmatterField(t, result.Artifacts[0].Bytes)
			if fm["model"] != tc.want {
				t.Fatalf("model=%v, want %q", fm["model"], tc.want)
			}
		})
	}
}

// spec: harness-opencode/a-tool-override-applies-consistently
func TestToolOverrideAppliesConsistently(t *testing.T) {
	for _, tc := range []struct {
		name     string
		matcher  string
		override map[string]string
		want     string
		accepted bool
	}{
		{"canonical matcher", "Bash", nil, "Bash", true},
		{"override extends matcher support", "NotebookEdit", map[string]string{"NotebookEdit": "notebook"}, "notebook", true},
		{"override spelling is used", "WebFetch", map[string]string{"WebFetch": "proxyfetch"}, "proxyfetch", true},
		{"known-native override value fails closed", "Bash", map[string]string{"Bash": "bash"}, "", false},
		{"override to a known native tool fails closed", "NotebookEdit", map[string]string{"NotebookEdit": "read"}, "", false},
		{"Task stays unsupported", "Task", map[string]string{"Task": "subagent"}, "", false},
		{"unmapped tool stays unsupported", "NotebookEdit", nil, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hook := ir.NewHookWithMatcher("check", "PreToolUse", tc.matcher, "true", "command", nil, ir.SourceRef{})
			result, err := compile.Compile(hook, "opencode", compile.WithToolOverride(tc.override))
			if err != nil {
				t.Fatal(err)
			}
			if !tc.accepted {
				if len(result.Artifacts) != 0 || !diag.HasCode(result.Diagnostics, diag.CodeHookOpenCodeUnsupported) {
					t.Fatalf("unsupported mapping was emitted: %#v", result)
				}
				return
			}
			if len(result.Artifacts) != 2 || diag.HasErrors(result.Diagnostics) {
				t.Fatalf("supported matcher was refused: %#v", result)
			}
			var declaration opencodehook.Declaration
			for _, a := range result.Artifacts {
				if a.Path == "hooks/agent-marketplace/check.json" {
					if err := json.Unmarshal(a.Bytes, &declaration); err != nil {
						t.Fatal(err)
					}
				}
			}
			if declaration.Matcher != tc.want {
				t.Fatalf("matcher=%q, want %q", declaration.Matcher, tc.want)
			}
		})
	}

	// Restrictive subagent policies resolve through the same override authority.
	agent := ir.NewSubagent("reviewer", "Review", "Body", "", []string{"NotebookEdit"}, "", nil, "", nil, ir.SourceRef{})
	result, err := compile.Compile(agent, "opencode", compile.WithToolOverride(map[string]string{"NotebookEdit": "read"}))
	if err != nil || len(result.Artifacts) != 1 || diag.HasErrors(result.Diagnostics) {
		t.Fatalf("agent override: %v %#v", err, result)
	}
	permission := frontmatterField(t, result.Artifacts[0].Bytes)["permission"].(map[string]any)
	if permission["read"] != "allow" || permission["*"] != "deny" {
		t.Fatalf("permission=%#v, want the override mapping to carry the restriction", permission)
	}
}
