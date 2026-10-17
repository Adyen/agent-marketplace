package harnesscompile

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

const nativeControlFrontmatter = "permissionMode: bypassPermissions\nmcpServers:\n  inert:\n    command: inert-server\nhooks:\n  PreToolUse:\n    - matcher: Bash\n      hooks:\n        - type: command\n          command: inert-hook\n"

const nativeHookFrontmatter = "hooks:\n  PreToolUse:\n    - matcher: Bash\n      hooks:\n        - type: command\n          command: inert-hook\n"

func nativeControlArtifacts(result *Result, name string) string {
	var out strings.Builder
	for _, a := range result.Artifacts {
		if strings.Contains(a.Path, name) {
			out.Write(a.Bytes)
		}
	}
	return out.String()
}

// spec: harness-compilation/foreign-controls-are-omitted-on-claude
func TestForeignControlsAreOmittedOnClaude(t *testing.T) {
	sources := map[Harness]fstest.MapFS{
		HarnessCursor: {".cursor/agents/helper.md": {Data: []byte("---\nname: helper\ndescription: Helper\n" + nativeControlFrontmatter + "---\nHelp.\n")}},
		HarnessFactory: {
			".factory/droids/helper.md":       {Data: []byte("---\nname: helper\ndescription: Helper\n" + nativeControlFrontmatter + "---\nHelp.\n")},
			".factory/skills/review/SKILL.md": {Data: []byte("---\nname: review\ndescription: Review\n" + nativeHookFrontmatter + "---\nReview.\n")},
			".factory/commands/deploy.md":     {Data: []byte("---\ndescription: Deploy\n" + nativeHookFrontmatter + "---\nDeploy.\n")},
		},
	}
	for from, src := range sources {
		t.Run(string(from), func(t *testing.T) {
			result, err := Convert(src, from, TargetClaude)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"helper", "review", "deploy"} {
				out := nativeControlArtifacts(result, name)
				if from == HarnessCursor && name != "helper" {
					continue
				}
				if out == "" {
					t.Fatalf("%s not emitted: %+v", name, result.Artifacts)
				}
				for _, key := range []string{"permissionMode", "mcpServers", "hooks", "inert-hook", "inert-server"} {
					if strings.Contains(out, key) {
						t.Fatalf("%s: foreign control %q became a Claude control:\n%s", name, key, out)
					}
				}
			}
			if !diag.HasCode(result.Diagnostics, diag.CodeCompileClaudeNativeControlOmitted) {
				t.Fatalf("missing %s: %+v", diag.CodeCompileClaudeNativeControlOmitted, result.Diagnostics)
			}
		})
	}
}

// spec: harness-compilation/claude-native-controls-round-trip
func TestClaudeNativeControlsRoundTrip(t *testing.T) {
	src := fstest.MapFS{
		"agents/helper.md":       {Data: []byte("---\nname: helper\ndescription: Helper\n" + nativeControlFrontmatter + "---\nHelp.\n")},
		"skills/review/SKILL.md": {Data: []byte("---\nname: review\ndescription: Review\n" + nativeHookFrontmatter + "---\nReview.\n")},
		"commands/deploy.md":     {Data: []byte("---\ndescription: Deploy\n" + nativeHookFrontmatter + "---\nDeploy.\n")},
	}
	direct, err := Convert(src, HarnessClaude, TargetClaude)
	if err != nil {
		t.Fatal(err)
	}
	if diag.HasCode(direct.Diagnostics, diag.CodeCompileClaudeNativeControlOmitted) {
		t.Fatalf("Claude controls were treated as foreign: %+v", direct.Diagnostics)
	}
	want := map[string][]string{"helper": {"permissionMode: bypassPermissions", "mcpServers:", "inert-server", "hooks:", "inert-hook"}, "review": {"hooks:", "inert-hook"}, "deploy": {"hooks:", "inert-hook"}}
	for name, keys := range want {
		out := nativeControlArtifacts(direct, name)
		for _, key := range keys {
			if !strings.Contains(out, key) {
				t.Fatalf("%s lost Claude control %q:\n%s", name, key, out)
			}
		}
	}
	bundle, err := IngestBundle(src, HarnessClaude)
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range bundle.Resources {
		canonical, err := artifact.FromIR(resource)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := artifact.DecodeResource(resource.Kind().String(), canonical.Payload)
		if err != nil {
			t.Fatal(err)
		}
		viaPayload, err := Compile(decoded, TargetClaude)
		if err != nil {
			t.Fatal(err)
		}
		if diag.HasCode(viaPayload.Diagnostics, diag.CodeCompileClaudeNativeControlOmitted) {
			t.Fatalf("%s: payload round trip treated Claude controls as foreign: %+v", resource.Name(), viaPayload.Diagnostics)
		}
		out := nativeControlArtifacts(viaPayload, resource.Name())
		for _, key := range want[resource.Name()] {
			if !strings.Contains(out, key) {
				t.Fatalf("%s: payload round trip lost Claude control %q:\n%s", resource.Name(), key, out)
			}
		}
	}
}

func readOnlySubagentFrontmatter(t *testing.T, result *Result, name string) string {
	t.Helper()
	for _, a := range result.Artifacts {
		if strings.Contains(a.Path, "/"+name+".") {
			return string(a.Bytes)
		}
	}
	t.Fatalf("subagent %q not emitted: %+v %+v", name, result.Artifacts, result.Diagnostics)
	return ""
}

// spec: harness-compilation/read-only-subagents-map-to-documented-read-only-modes
func TestReadOnlySubagentsMapToDocumentedModes(t *testing.T) {
	cursor := fstest.MapFS{".cursor/agents/reviewer.md": {Data: []byte("---\nname: reviewer\ndescription: Reviewer\nreadonly: true\n---\nReview.\n")}}
	for target, want := range map[Target]string{
		TargetClaude:  "permissionMode: plan",
		TargetCursor:  "readonly: true",
		TargetCodex:   "sandbox_mode = 'read-only'",
		TargetFactory: "tools: read-only",
	} {
		result, err := Convert(cursor, HarnessCursor, target)
		if err != nil {
			t.Fatal(err)
		}
		if out := readOnlySubagentFrontmatter(t, result, "reviewer"); result.HasErrors() || !strings.Contains(out, want) {
			t.Fatalf("%s: read-only restriction not kept, want %q:\n%s %+v", target, want, out, result.Diagnostics)
		}
	}
	claude := fstest.MapFS{"agents/planner.md": {Data: []byte("---\nname: planner\ndescription: Planner\npermissionMode: plan\n---\nPlan.\n")}}
	result, err := Convert(claude, HarnessClaude, TargetCursor)
	if err != nil {
		t.Fatal(err)
	}
	if out := readOnlySubagentFrontmatter(t, result, "planner"); result.HasErrors() || !strings.Contains(out, "readonly: true") {
		t.Fatalf("Claude plan mode not kept on Cursor:\n%s %+v", out, result.Diagnostics)
	}
	factory := fstest.MapFS{".factory/droids/auditor.md": {Data: []byte("---\nname: auditor\ndescription: Auditor\ntools: read-only\n---\nAudit.\n")}}
	for target, want := range map[Target]string{TargetFactory: "tools: read-only", TargetClaude: "permissionMode: plan"} {
		result, err := Convert(factory, HarnessFactory, target)
		if err != nil {
			t.Fatal(err)
		}
		if out := readOnlySubagentFrontmatter(t, result, "auditor"); result.HasErrors() || !strings.Contains(out, want) {
			t.Fatalf("Factory read-only category not kept on %s, want %q:\n%s %+v", target, want, out, result.Diagnostics)
		}
	}
	authored := fstest.MapFS{"agents/planner.md": {Data: []byte("---\nname: planner\ndescription: Planner\npermissionMode: PLAN\n---\nPlan.\n")}}
	result, err = Convert(authored, HarnessClaude, TargetClaude)
	if err != nil {
		t.Fatal(err)
	}
	if out := readOnlySubagentFrontmatter(t, result, "planner"); result.HasErrors() || !strings.Contains(out, "permissionMode: PLAN\n") || strings.Count(out, "permissionMode") != 1 {
		t.Fatalf("authored Claude plan mode changed on Claude:\n%s %+v", out, result.Diagnostics)
	}
	legacyFactory := ir.NewSubagent("auditor", "Auditor", "Audit.\n", "", []string{"read-only"}, "", nil, "", nil, ir.SourceRef{})
	legacyFactory.ToolSource = "factory"
	for target, want := range map[Target]string{TargetFactory: "tools: read-only", TargetClaude: "permissionMode: plan"} {
		compiled, err := Compile(legacyFactory, target)
		if err != nil {
			t.Fatal(err)
		}
		if out := readOnlySubagentFrontmatter(t, compiled, "auditor"); compiled.HasErrors() || !strings.Contains(out, want) {
			t.Fatalf("legacy Factory read-only category not kept on %s, want %q:\n%s %+v", target, want, out, compiled.Diagnostics)
		}
	}
	legacy := ir.NewSubagent("legacy", "Legacy", "Plan.\n", "", nil, "", nil, "", map[string]any{"permissionMode": "plan"}, ir.SourceRef{})
	compiled, err := Compile(legacy, TargetClaude)
	if err != nil {
		t.Fatal(err)
	}
	if out := readOnlySubagentFrontmatter(t, compiled, "legacy"); compiled.HasErrors() || !strings.Contains(out, "permissionMode: plan") {
		t.Fatalf("untyped plan restriction dropped on Claude:\n%s %+v", out, compiled.Diagnostics)
	}
}

// spec: harness-compilation/targets-without-a-read-only-mode-refuse
func TestTargetsWithoutReadOnlyModeRefuse(t *testing.T) {
	sandboxed := ir.NewSubagent("sandboxed", "Sandboxed", "Review.\n", "", nil, "", nil, "", map[string]any{"sandbox_mode": "read-only"}, ir.SourceRef{})
	refused, err := Compile(sandboxed, TargetGemini)
	if err != nil {
		t.Fatal(err)
	}
	if len(refused.Artifacts) != 0 || !diag.HasCode(refused.Diagnostics, diag.CodeSubagentSecurityRestrictionUnsupported) {
		t.Fatalf("untyped read-only sandbox mode was not honored: %+v", refused)
	}
	cursor := fstest.MapFS{".cursor/agents/reviewer.md": {Data: []byte("---\nname: reviewer\ndescription: Reviewer\nreadonly: true\n---\nReview.\n")}}
	withTools := fstest.MapFS{".cursor/agents/reviewer.md": {Data: []byte("---\nname: reviewer\ndescription: Reviewer\nreadonly: true\ntools: [Read]\n---\nReview.\n")}}
	cases := []struct {
		src fstest.MapFS
		to  Target
	}{{cursor, TargetGemini}, {cursor, TargetDevin}, {cursor, TargetOpenCode}, {cursor, TargetWindsurf}, {withTools, TargetFactory}}
	for _, tc := range cases {
		t.Run(string(tc.to), func(t *testing.T) {
			result, err := Convert(tc.src, HarnessCursor, tc.to)
			if err != nil {
				t.Fatal(err)
			}
			for _, a := range result.Artifacts {
				if strings.Contains(a.Path, "reviewer") {
					t.Fatalf("read-only subagent emitted without its restriction: %s\n%s", a.Path, a.Bytes)
				}
			}
			code := diag.CodeSubagentSecurityRestrictionUnsupported
			if tc.to == TargetOpenCode {
				code = diag.CodeSubagentOpenCodeRestrictionUnsupported
			}
			if !diag.HasCode(result.Diagnostics, code) {
				t.Fatalf("missing %s: %+v", code, result.Diagnostics)
			}
		})
	}
}
