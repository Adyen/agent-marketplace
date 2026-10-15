package devin

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// Adversarial coverage for bead agent-marketplace-pu6b. The reversal turned an
// unrepresentable allowed-tools restriction from "error, no artifact" into
// "warning, key omitted, Devin defaults apply". That is a deliberate widening,
// so the only remaining question is whether it widens in cases it was never
// meant to reach. These tests pin the two boundaries where it must not.

// TestBlankOnlyRestrictionStaysExplicitlyEmpty is the empty-vs-absent boundary.
//
// parseToolList already documents the rule for the string shape: "A string that
// contains no tool names after splitting (`""`, `","`) is an explicit empty
// restriction, not a parse failure: it renders as `allowed-tools: []`". A LIST
// whose entries name no tool either says exactly the same thing, so it must
// land on the same side of the boundary.
//
// Before this test, it did not: `allowed-tools: [""]` produced a warning and
// OMITTED the key, i.e. Devin's full default tool set, while the semantically
// identical `allowed-tools: ""` produced `allowed-tools: []`, i.e. no tools at
// all. That is the worst crossing this change could make — an explicit empty
// restriction silently becoming an unrestricted install — and it is decided by
// nothing but the YAML shape the author happened to use.
func TestBlankOnlyRestrictionStaysExplicitlyEmpty(t *testing.T) {
	const want = "---\nname: demo\ndescription: Demo skill\nallowed-tools: []\n---\nBody\n"

	canonical := map[string][]string{
		"one empty entry":     {""},
		"several empty":       {"", ""},
		"whitespace entry":    {"   "},
		"comma entry":         {","},
		"tab entry":           {"\t"},
		"mixed blank shapes":  {"", " ", ","},
		"explicit empty list": {},
	}
	for name, tools := range canonical {
		t.Run("canonical/"+name, func(t *testing.T) {
			skill := ir.NewSkillWithOptions("demo", "Demo skill", "Body\n", nil, nil, testSource,
				ir.WithSkillAllowedTools(tools))
			result, err := compile.Compile(skill, compile.TargetDevin)
			if err != nil {
				t.Fatalf("Compile(skill, Devin): %v", err)
			}
			if len(result.Artifacts) != 1 {
				t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
			}
			if got := string(result.Artifacts[0].Bytes); got != want {
				t.Errorf("a restriction naming no tool crossed the empty-vs-absent boundary.\n got:\n%s\nwant:\n%s\n"+
					"An omitted key inherits Devin's DEFAULT tool set; the source asked for none.", got, want)
			}
		})
	}

	extras := map[string]any{
		"list with one empty string": []any{""},
		"list of empty strings":      []any{"", ""},
		"list with whitespace":       []any{" "},
		"list with a comma":          []any{","},
		"typed slice of blanks":      []string{"", "  "},
	}
	for name, value := range extras {
		t.Run("extra/"+name, func(t *testing.T) {
			cmd := ir.NewCommand("demo", "Demo command", "Body\n", map[string]any{allowedToolsKey: value}, testSource)
			result, err := compile.Compile(cmd, compile.TargetDevin)
			if err != nil {
				t.Fatalf("Compile(command, Devin): %v", err)
			}
			if len(result.Artifacts) != 1 {
				t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
			}
			body := string(result.Artifacts[0].Bytes)
			if !strings.Contains(body, "allowed-tools: []") {
				t.Errorf("a blank-only allowed-tools LIST did not render as an explicit empty restriction, "+
					"while the identical STRING shape does; the key was omitted so Devin's default tool set applies:\n%s", body)
			}
		})
	}
}

// TestWhitespacePaddedToolNameIsNotAWidening is the other shape-dependent
// crossing. parseToolList trims each field of the comma-separated STRING shape
// but not the elements of the LIST shape, so `allowed-tools: " Read "` compiled
// to a read-only profile while `allowed-tools: [" Read "]` — the same
// restriction, written as a list — dropped the key and inherited every default
// tool. Whitespace in a YAML scalar must not decide privilege.
func TestWhitespacePaddedToolNameIsNotAWidening(t *testing.T) {
	for _, tc := range []struct {
		name  string
		tools []string
	}{
		{"trailing space", []string{"Read "}},
		{"leading space", []string{" Read"}},
		{"both", []string{" Read "}},
		{"padded among mappable", []string{"Read ", "Grep"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			skill := ir.NewSkillWithOptions("demo", "Demo skill", "Body\n", nil, nil, testSource,
				ir.WithSkillAllowedTools(tc.tools))
			result, err := compile.Compile(skill, compile.TargetDevin)
			if err != nil {
				t.Fatalf("Compile(skill, Devin): %v", err)
			}
			if len(result.Artifacts) != 1 {
				t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
			}
			body := string(result.Artifacts[0].Bytes)
			if !strings.Contains(body, "- read") {
				t.Errorf("a padded tool name dropped the whole restriction and inherited Devin's default tool set:\n%s", body)
			}
			if diag.HasErrors(result.Diagnostics) {
				t.Errorf("unexpected error diagnostics: %+v", result.Diagnostics)
			}
		})
	}
}

// TestToolOverrideRescuesAScopedSpecifier checks the documented escape hatch on
// the exact input the bead exists for.
//
// compile.WithToolOverride is offered as the way a consumer maps a name the
// curated table cannot. resolveAllowedTools classifies any entry containing a
// parenthesis as scoped BEFORE consulting the override, so a user who
// explicitly declared `Bash(openspec:*) -> exec` was ignored and got Devin's
// full default tool set instead of the narrow restriction they asked for. The
// override is an explicit, per-invocation statement of intent; ignoring it
// silently is a lost restriction, not a conservative default.
func TestToolOverrideRescuesAScopedSpecifier(t *testing.T) {
	skill := ir.NewSkillWithOptions("demo", "Demo skill", "Body\n", nil, nil, testSource,
		ir.WithSkillAllowedTools([]string{"Bash(openspec:*)"}),
	)
	result, err := compile.Compile(skill, compile.TargetDevin,
		compile.WithToolOverride(map[string]string{"Bash(openspec:*)": "exec"}))
	if err != nil {
		t.Fatalf("Compile(skill, Devin): %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Errorf("diagnostics = %+v, want none: an explicit tool override was ignored", result.Diagnostics)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
	}
	want := "---\nname: demo\ndescription: Demo skill\nallowed-tools:\n  - exec\n---\nBody\n"
	if got := string(result.Artifacts[0].Bytes); got != want {
		t.Errorf("artifact bytes =\n%s\nwant\n%s\nan explicit override could not rescue the one input this bead exists for", got, want)
	}
}

// TestNoPartialRestrictionIsEverEmitted attacks the other direction: a mixed
// list must omit the key WHOLE, never the surviving subset. A narrowed list is
// a silent privilege change too, and a resource stripped of exec cannot run.
func TestNoPartialRestrictionIsEverEmitted(t *testing.T) {
	mixed := [][]string{
		{"Read", "WebSearch"},
		{"WebSearch", "Read"},
		{"Read", "Bash(openspec:*)"},
		{"Bash", "Write", "Grep"},
		{"Read", "Grep", "Glob", "Bash", "TodoWrite"},
	}
	for _, tools := range mixed {
		skill := ir.NewSkillWithOptions("demo", "Demo skill", "Body\n", nil, nil, testSource,
			ir.WithSkillAllowedTools(tools))
		result, err := compile.Compile(skill, compile.TargetDevin)
		if err != nil {
			t.Fatalf("Compile(%v, Devin): %v", tools, err)
		}
		if len(result.Artifacts) != 1 {
			t.Fatalf("%v: artifacts = %d, want 1", tools, len(result.Artifacts))
		}
		if body := string(result.Artifacts[0].Bytes); strings.Contains(body, allowedToolsKey) {
			t.Errorf("%v: a PARTIAL restriction reached the artifact:\n%s", tools, body)
		}
	}
}

// TestUnrepresentableToolsNeverLeakAnywhere puts a credential-shaped string
// inside a scoped specifier and greps every output surface — artifact bytes,
// every diagnostic field, and the structured log at DEBUG level, which is the
// surface the existing tests do not cover.
func TestUnrepresentableToolsNeverLeakAnywhere(t *testing.T) {
	const secret = "glpat-SUPERSECRETTOKEN"
	specifier := "Bash(curl -H 'PRIVATE-TOKEN: " + secret + "' https://x:*)"

	resources := map[string]ir.Resource{
		"skill canonical": ir.NewSkillWithOptions("demo", "Demo skill", "Body\n", nil, nil, testSource,
			ir.WithSkillAllowedTools([]string{specifier})),
		"skill extra string": ir.NewSkillWithOptions("demo", "Demo skill", "Body\n", nil,
			map[string]any{allowedToolsKey: specifier}, testSource),
		"skill extra bad shape": ir.NewSkillWithOptions("demo", "Demo skill", "Body\n", nil,
			map[string]any{allowedToolsKey: map[string]any{specifier: true}}, testSource),
		"skill canonical plus raw extra": ir.NewSkillWithOptions("demo", "Demo skill", "Body\n", nil,
			map[string]any{allowedToolsKey: specifier}, testSource,
			ir.WithSkillAllowedTools([]string{"Read"})),
		"command extra": ir.NewCommand("demo", "Demo command", "Body\n",
			map[string]any{allowedToolsKey: []any{specifier}}, testSource),
		"subagent canonical": ir.NewSubagent("demo", "Demo subagent", "Body\n", "sonnet",
			[]string{specifier}, "", nil, "", nil, subagentSource),
		"subagent extra": ir.NewSubagent("demo", "Demo subagent", "Body\n", "sonnet",
			nil, "", nil, "", map[string]any{allowedToolsKey: []any{specifier}}, subagentSource),
	}

	for name, resource := range resources {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			defer slog.SetDefault(previous)

			result, err := compile.Compile(resource, compile.TargetDevin)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			for _, art := range result.Artifacts {
				if strings.Contains(string(art.Bytes), secret) {
					t.Errorf("artifact %s leaks the specifier:\n%s", art.Path, art.Bytes)
				}
			}
			for _, d := range result.Diagnostics {
				for field, text := range map[string]string{"message": d.Message, "hint": d.Hint, "resource": d.Resource, "code": d.Code} {
					if strings.Contains(text, secret) {
						t.Errorf("diagnostic %s %s leaks the specifier: %q", d.Code, field, text)
					}
				}
			}
			if strings.Contains(logs.String(), secret) {
				t.Errorf("structured log leaks the specifier:\n%s", logs.String())
			}
		})
	}
}

// TestFailClosedSubagentStaysSuppressedWithAToolWarning verifies the tool
// warning neither masks nor downgrades a genuine fail-closed error: a subagent
// that is ALSO unrepresentable for isolation or disallowedTools must still
// produce no artifact and still carry the error.
func TestFailClosedSubagentStaysSuppressedWithAToolWarning(t *testing.T) {
	for _, tc := range []struct {
		name            string
		isolation       string
		disallowedTools []string
	}{
		{"isolation", "full", nil},
		{"disallowed tools", "", []string{"Bash"}},
		{"both", "full", []string{"Bash"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sub := ir.NewSubagent("demo", "Demo subagent", "Body\n", "sonnet",
				[]string{"Bash(openspec:*)"}, "", tc.disallowedTools, tc.isolation, nil, subagentSource)
			result, err := compile.Compile(sub, compile.TargetDevin)
			if err != nil {
				t.Fatalf("Compile(subagent, Devin): %v", err)
			}
			if len(result.Artifacts) != 0 {
				t.Errorf("artifacts = %d, want 0: a fail-closed subagent was published anyway:\n%s",
					len(result.Artifacts), result.Artifacts[0].Bytes)
			}
			if !diag.HasErrors(result.Diagnostics) {
				t.Errorf("diagnostics = %+v, want an error: the tool warning replaced the fail-closed error", result.Diagnostics)
			}
			if !diag.HasCode(result.Diagnostics, diag.CodeSubagentDevinToolUnknown) {
				t.Errorf("diagnostics = %+v, want the tool warning alongside the error", result.Diagnostics)
			}
		})
	}
}

// TestSuppressedResourceDoesNotClaimDevinDefaultsApply is a truthfulness check
// on the new message. When the resource is suppressed for another reason, "the
// allowed-tools key was omitted and Devin's default tool set applies" describes
// an artifact that does not exist: nothing is installed, so no tool set applies.
func TestSuppressedResourceDoesNotClaimDevinDefaultsApply(t *testing.T) {
	sub := ir.NewSubagent("demo", "Demo subagent", "Body\n", "sonnet",
		[]string{"Bash(openspec:*)"}, "", nil, "full", nil, subagentSource)
	result, err := compile.Compile(sub, compile.TargetDevin)
	if err != nil {
		t.Fatalf("Compile(subagent, Devin): %v", err)
	}
	if len(result.Artifacts) != 0 {
		t.Fatalf("artifacts = %d, want 0", len(result.Artifacts))
	}
	for _, d := range result.Diagnostics {
		if d.Code != diag.CodeSubagentDevinToolUnknown {
			continue
		}
		if strings.Contains(d.Message, "Devin's default tool set applies") {
			t.Errorf("suppressed resource reports %q, but no artifact was produced so no tool set applies", d.Message)
		}
	}
}
