package harnesscompile

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/yamlfm"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestD2SkillSecurityListsPreserveAbsentEmptyAndNonEmpty(t *testing.T) {
	cases := []struct {
		name           string
		allowed        []string
		permissions    ir.Permissions
		permissionsSet bool
		wantAllowed    string
		wantPerm       string
	}{
		{name: "absent"},
		{name: "empty", allowed: []string{}, permissions: ir.Permissions{Allow: []string{}}, permissionsSet: true, wantAllowed: "allowed-tools: []", wantPerm: "allow: []"},
		{name: "non-empty", allowed: []string{"Read"}, permissions: ir.Permissions{Deny: []string{"Bash"}}, permissionsSet: true, wantAllowed: "Read", wantPerm: "Bash"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			skill := ir.NewSkillWithOptions("security", "", "body", nil, nil, ir.SourceRef{}, ir.WithSkillAllowedTools(test.allowed), ir.WithSkillPermissions(test.permissions))
			if !test.permissionsSet {
				skill = ir.NewSkill("security", "", "body", nil, nil, ir.SourceRef{})
			}
			result, err := compile.Compile(skill, compile.TargetClaude)
			if err != nil {
				t.Fatal(err)
			}
			text := string(result.Artifacts[0].Bytes)
			if (test.wantAllowed == "") == strings.Contains(text, "allowed-tools:") {
				t.Fatalf("allowed-tools presence = %t, output=%s", strings.Contains(text, "allowed-tools:"), text)
			}
			if test.wantAllowed != "" && !strings.Contains(text, test.wantAllowed) {
				t.Fatalf("missing allowed-tools value %q: %s", test.wantAllowed, text)
			}
			if (test.wantPerm == "") == strings.Contains(text, "permissions:") {
				t.Fatalf("permissions presence = %t, output=%s", strings.Contains(text, "permissions:"), text)
			}
			if test.wantPerm != "" && !strings.Contains(text, test.wantPerm) {
				t.Fatalf("missing permission value %q: %s", test.wantPerm, text)
			}
		})
	}
}

func TestD2RemoteMCPDoesNotFabricateStdio(t *testing.T) {
	server := ir.NewMcpServerWithOptions("remote", "", nil, nil, nil, ir.SourceRef{}, ir.WithMcpURL("https://example.test/mcp"), ir.WithMcpTransport("streamable-http"), ir.WithMcpHeaders(map[string]string{"Authorization": "Bearer ${TOKEN}"}))
	for _, target := range []compile.Target{compile.TargetClaude, compile.TargetFactory, compile.TargetCursor, compile.TargetGemini, compile.TargetWindsurf} {
		result, err := compile.Compile(server, target)
		if err != nil {
			t.Fatalf("Compile(%s): %v", target, err)
		}
		var document map[string]any
		if err := json.Unmarshal(result.Artifacts[0].Bytes, &document); err != nil {
			t.Fatalf("Compile(%s) JSON: %v", target, err)
		}
		servers := document["mcpServers"].(map[string]any)
		entry := servers["remote"].(map[string]any)
		for _, key := range []string{"command", "args", "env"} {
			if _, ok := entry[key]; ok {
				t.Errorf("Compile(%s) emitted empty remote-only %s: %s", target, key, result.Artifacts[0].Bytes)
			}
		}
	}
}

// spec: hook-resources/prompt-hooks-compile-where-the-catalog-declares-them
// spec: hook-resources/prompt-hooks-are-refused-where-undeclared
func TestD2PromptHookSupportAndSuppression(t *testing.T) {
	hook := ir.NewHookWithOptions("prompt", "UserPromptSubmit", "", "prompt", nil, ir.SourceRef{}, ir.WithHookPrompt("Review this"))
	for _, target := range []compile.Target{compile.TargetClaude, compile.TargetCursor} {
		result, err := compile.Compile(hook, target)
		if err != nil || len(result.Artifacts) != 1 {
			t.Fatalf("Compile(%s) prompt hook: artifacts=%d err=%v", target, len(result.Artifacts), err)
		}
		if strings.Contains(string(result.Artifacts[0].Bytes), `"command"`) {
			t.Errorf("Compile(%s) fabricated command: %s", target, result.Artifacts[0].Bytes)
		}
		if !strings.Contains(string(result.Artifacts[0].Bytes), "Review this") {
			t.Errorf("Compile(%s) omitted prompt: %s", target, result.Artifacts[0].Bytes)
		}
	}
	for _, target := range []compile.Target{compile.TargetFactory, compile.TargetWindsurf, compile.TargetGemini} {
		result, err := compile.Compile(hook, target)
		if err != nil || len(result.Artifacts) != 0 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != diag.CodeHookPromptUnsupported {
			t.Fatalf("Compile(%s) unsupported prompt hook = %#v, err=%v", target, result, err)
		}
	}
}

func TestD2AbsentTypedFieldsDoNotSuppressExtras(t *testing.T) {
	for _, target := range []compile.Target{compile.TargetClaude, compile.TargetFactory, compile.TargetCursor, compile.TargetGemini, compile.TargetWindsurf} {
		skill := ir.NewSkill("demo", "", "body", nil, map[string]any{"permissions": map[string]any{"custom": true}}, ir.SourceRef{})
		result, err := compile.Compile(skill, target)
		if err != nil {
			t.Fatalf("Compile(%s): %v", target, err)
		}
		if len(result.Diagnostics) != 0 {
			t.Fatalf("Compile(%s) diagnostics = %#v, want extras preserved without conflicts", target, result.Diagnostics)
		}
		if len(result.Artifacts) == 0 || !strings.Contains(string(result.Artifacts[0].Bytes), "permissions") {
			t.Fatalf("Compile(%s) omitted absent-field extra: %#v", target, result.Artifacts)
		}
	}
}

func TestD2McpLiteralCredentialDiagnosticIsValueFree(t *testing.T) {
	secret := "do-not-log-this"
	server := ir.NewMcpServerWithOptions("remote", "", nil, map[string]string{"API_TOKEN": secret}, nil, ir.SourceRef{}, ir.WithMcpHeaders(map[string]string{"Authorization": "Bearer " + secret}), ir.WithMcpOAuth("client", secret, "resource"))
	result, err := compile.Compile(server, compile.TargetClaude)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != diag.CodeMcpLiteralCredential {
		t.Fatalf("diagnostics = %#v", result.Diagnostics)
	}
	if strings.Contains(result.Diagnostics[0].Message, secret) || strings.Contains(result.Diagnostics[0].Hint, secret) {
		t.Fatalf("credential leaked in diagnostic: %#v", result.Diagnostics[0])
	}
}

func TestD2CanonicalExtraConflictIsStableAndValueFree(t *testing.T) {
	skill := ir.NewSkill("demo", "canonical", "body", nil, map[string]any{"description": "secret-description", "z-extra": true, "a-extra": false}, ir.SourceRef{})
	result, err := compile.Compile(skill, compile.TargetClaude)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != diag.CodeCanonicalFieldOverridesExtra {
		t.Fatalf("diagnostics = %#v", result.Diagnostics)
	}
	if strings.Contains(result.Diagnostics[0].Message, "secret-description") || strings.Contains(result.Diagnostics[0].Hint, "secret-description") {
		t.Fatalf("diagnostic leaked extra value: %#v", result.Diagnostics[0])
	}
	text := string(result.Artifacts[0].Bytes)
	if strings.Count(text, "description:") != 1 || !strings.Contains(text, "a-extra:") || !strings.Contains(text, "z-extra:") {
		t.Fatalf("unexpected deterministic frontmatter: %s", text)
	}
}

func TestD2PublicCompileRejectsUnsafeResourcesWithoutPanic(t *testing.T) {
	valid := ir.NewSkill("safe-name", "", "body", nil, nil, ir.SourceRef{})
	var typedNil *ir.Skill
	var nilResource Resource
	mismatched := d2CustomResource{kind: ir.KindSkill, name: "custom"}
	cases := []struct {
		name     string
		resource Resource
		wantErr  string
	}{
		{name: "nil interface", resource: nilResource, wantErr: "nil"},
		{name: "typed nil", resource: typedNil, wantErr: "typed nil"},
		{name: "mismatched custom kind", resource: mismatched, wantErr: "compiler rejected"},
		{name: "path traversal", resource: ir.NewSkill("../escape", "", "body", nil, nil, ir.SourceRef{}), wantErr: "resource name"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("Compile panicked: %v", recovered)
				}
			}()
			_, err := Compile(test.resource, TargetClaude)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Compile error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
	if _, err := Compile(valid, TargetClaude, nil); err != nil {
		t.Fatalf("Compile with nil option: %v", err)
	}
}

type d2CustomResource struct {
	kind ir.ResourceKind
	name string
}

func (r d2CustomResource) Kind() ir.ResourceKind { return r.kind }
func (r d2CustomResource) Name() string          { return r.name }

func TestD2CursorAndGeminiFrontmatterIsParserSafeAndDeterministic(t *testing.T) {
	skill := ir.NewSkill("safe", "line one\nline two: yes", "body", []string{"x: y", "# comment"}, map[string]any{
		"bad:key": map[string]any{"nested": []any{"a: b", "line\nvalue"}},
	}, ir.SourceRef{})
	for _, target := range []Target{TargetCursor, TargetGemini} {
		t.Run(string(target), func(t *testing.T) {
			first, err := Compile(skill, target)
			if err != nil {
				t.Fatal(err)
			}
			second, err := Compile(skill, target)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(first.Artifacts[0].Bytes, second.Artifacts[0].Bytes) {
				t.Fatal("frontmatter output is not deterministic")
			}
			frontmatter, _, err := yamlfm.ParseYAMLFrontmatter(bytes.NewReader(first.Artifacts[0].Bytes))
			if err != nil {
				t.Fatalf("parse emitted frontmatter: %v\n%s", err, first.Artifacts[0].Bytes)
			}
			if got := frontmatter["description"]; got != "line one\nline two: yes" {
				t.Fatalf("description = %#v", got)
			}
			if _, ok := frontmatter["bad:key"]; !ok {
				t.Fatalf("quoted extra key missing: %#v", frontmatter)
			}
		})
	}
}

func TestD2GeminiSupportFilesRemainVisibleAsDeferredDiagnostics(t *testing.T) {
	skill := ir.NewSkill("support", "", "body", nil, nil, ir.SourceRef{})
	skill.SupportFiles = []string{"scripts/run.sh"}
	result, err := Compile(skill, TargetGemini)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != diag.CodeSkillSupportFilesDeferred {
		t.Fatalf("diagnostics = %#v, want deferred support-file diagnostic", result.Diagnostics)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want body artifact", len(result.Artifacts))
	}
}

func TestD2GeminiSubagentEffortIsOmittedWithWarning(t *testing.T) {
	subagent := ir.NewSubagent("reviewer", "Review", "body", "", nil, "high", nil, "", nil, ir.SourceRef{})
	result, err := Compile(subagent, TargetGemini)
	if err != nil {
		t.Fatal(err)
	}
	frontmatter, _, err := yamlfm.ParseYAMLFrontmatter(bytes.NewReader(result.Artifacts[0].Bytes))
	if err != nil {
		t.Fatal(err)
	}
	if got := frontmatter["effort"]; got != nil {
		t.Fatalf("unsupported effort emitted: %#v", got)
	}
	if !diag.HasCode(result.Diagnostics, diag.CodeModelReasoningOmitted) || diag.HasErrors(result.Diagnostics) {
		t.Fatalf("expected warning-only effort loss: %+v", result.Diagnostics)
	}
}

func TestD2UnsupportedTargetsSuppressCommandAndPromptHooks(t *testing.T) {
	hook := ir.NewHookWithOptions("mixed", "UserPromptSubmit", "echo command", "command", nil, ir.SourceRef{}, ir.WithHookPrompt("also prompt"))
	for _, target := range []Target{TargetFactory, TargetCursor, TargetWindsurf} {
		result, err := Compile(hook, target)
		if err != nil {
			t.Fatalf("Compile(%s): %v", target, err)
		}
		if len(result.Artifacts) != 0 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != diag.CodeHookPromptUnsupported {
			t.Fatalf("Compile(%s) = %#v, want suppression diagnostic", target, result)
		}
	}
}
