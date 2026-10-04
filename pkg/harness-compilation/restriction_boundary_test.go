package harnesscompile

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	compilepkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestFactoryToolRestrictionsFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		tools   []string
		blocked bool
		want    string
	}{
		{"absent", nil, false, ""}, {"empty", []string{}, false, "tools: []"}, {"known", []string{"Read"}, false, "- Read"},
		{"unknown", []string{"MysteryTool"}, true, ""}, {"mixed", []string{"Read", "MysteryTool"}, true, ""}, {"scoped", []string{"Bash(synthetic-only:*)"}, true, ""},
		{"native-create", []string{"Create"}, false, "- Create"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resource := ir.NewSubagent("restricted", "Restricted", "body", "", tc.tools, "", nil, "", nil, ir.SourceRef{})
			result, err := Compile(resource, TargetFactory)
			if err != nil {
				t.Fatal(err)
			}
			if tc.blocked {
				if len(result.Artifacts) != 0 || !result.HasErrors() || !diag.HasCode(result.Diagnostics, diag.CodeSubagentFactoryToolUnknown) {
					t.Fatalf("unrepresentable restriction published: %+v", result)
				}
				return
			}
			if result.HasErrors() || len(result.Artifacts) != 1 {
				t.Fatalf("valid restriction rejected: %+v", result)
			}
			body := string(result.Artifacts[0].Bytes)
			if tc.tools == nil && strings.Contains(body, "tools:") || tc.want != "" && !strings.Contains(body, tc.want) {
				t.Fatalf("restriction changed: %s", body)
			}
			if tc.name == "native-create" && strings.Contains(body, "- Edit") {
				t.Fatal("Create-only permission widened to Edit")
			}
		})
	}
	resource := ir.NewSubagent("override", "Override", "body", "", []string{"CustomSource"}, "", nil, "", nil, ir.SourceRef{})
	mapped, err := compilepkg.Compile(resource, compilepkg.TargetFactory, compilepkg.WithToolOverride(map[string]string{"CustomSource": "Read"}))
	if err != nil || len(mapped.Artifacts) != 1 || diag.HasErrors(mapped.Diagnostics) {
		t.Fatalf("explicit mapping override stopped working: %v %+v", err, mapped)
	}
}

func TestFactoryNativeRestrictedRoundTrip(t *testing.T) {
	for _, tools := range []string{"[]", "[Create]", "[Edit, Create]"} {
		src := fstest.MapFS{"droids/restricted.md": {Data: []byte("---\nname: restricted\ndescription: Restricted\ntools: " + tools + "\n---\nBody\n")}}
		result, err := Convert(src, HarnessFactory, TargetFactory)
		if err != nil || result.HasErrors() || len(result.Artifacts) != 1 {
			t.Fatalf("native restriction rejected: %v %+v", err, result)
		}
		body := string(result.Artifacts[0].Bytes)
		if !strings.Contains(body, "tools:") || tools == "[]" && !strings.Contains(body, "tools: []") || tools == "[Create]" && (strings.Contains(body, "- Edit") || !strings.Contains(body, "- Create")) {
			t.Fatalf("native restriction lost: %s", body)
		}
	}
}

func TestCodexCommandRestrictionsAreNotInertMetadata(t *testing.T) {
	for _, restriction := range []string{"allowed-tools: [Read]", "allowed-tools: []", "allowed-tools: null", "ALLOWED-TOOLS: [Read]", "permissions: {}"} {
		src := fstest.MapFS{"commands/restricted.md": {Data: []byte("---\nname: restricted\ndescription: Restricted\n" + restriction + "\n---\nBody\n")}}
		result, err := Convert(src, HarnessClaude, TargetCodex)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Artifacts) != 0 || !result.HasErrors() || !diag.HasCode(result.Diagnostics, diag.CodeSkillCodexSecurityRestrictionUnsupported) {
			t.Fatalf("command restriction became inert metadata (%s): %+v", restriction, result)
		}
	}
	result, err := Compile(ir.NewCommand("plain", "Plain", "Body", map[string]any{"x-note": "preserved"}, ir.SourceRef{}), TargetCodex)
	if err != nil || result.HasErrors() || len(result.Artifacts) != 1 || !strings.Contains(string(result.Artifacts[0].Bytes), "x-agent-marketplace-kind: command") || !strings.Contains(string(result.Artifacts[0].Bytes), "x-note: preserved") {
		t.Fatalf("unrestricted command changed: %v %+v", err, result)
	}
}

func TestFactoryDocumentedHookEventsRemainSupported(t *testing.T) {
	for _, event := range []string{"PreToolUse", "PostToolUse", "UserPromptSubmit", "Notification", "Stop", "SubagentStop", "PreCompact", "SessionStart", "SessionEnd"} {
		result, err := Compile(ir.NewHook("valid-event", event, "check", "command", nil, ir.SourceRef{}), TargetFactory)
		if err != nil || result.HasErrors() || len(result.Artifacts) != 1 {
			t.Fatalf("documented event %s was rejected: %v %+v", event, err, result)
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal(result.Artifacts[0].Bytes, &body); err != nil {
			t.Fatal(err)
		}
		if _, exists := body[event]; !exists {
			t.Fatalf("documented event %s was remapped", event)
		}
	}
}

// spec: hook-resources/factory-devin-and-codex-suppress-unmappable-events-with-an-error
func TestFactoryHookEventsMustBeRepresentable(t *testing.T) {
	src := fstest.MapFS{".devin/hooks.v1.json": {Data: []byte(`{"PermissionRequest":[{"hooks":[{"type":"command","command":"permission-check"}]}],"PreToolUse":[{"matcher":"exec","hooks":[{"type":"command","command":"valid-check"}]}]}`)}}
	result, err := Convert(src, HarnessDevin, TargetFactory)
	if err != nil {
		t.Fatal(err)
	}
	if !result.HasErrors() || !diag.HasCode(result.Diagnostics, diag.CodeHookEventUnmappable) {
		t.Errorf("unsupported event was not diagnosed: %+v", result.Diagnostics)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("valid sibling hook was suppressed: %+v", result.Artifacts)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(result.Artifacts[0].Bytes, &body); err != nil {
		t.Fatal(err)
	}
	if _, exists := body["PermissionRequest"]; exists || !strings.Contains(string(body["PreToolUse"]), "valid-check") {
		t.Fatalf("wrong event scope: %s", result.Artifacts[0].Bytes)
	}
}
