package devin

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

var testSource = ir.SourceRef{File: "skills/demo/SKILL.md", Line: 1}

// wantDiag is one expected diagnostic identity. Messages are asserted only
// where the exact wording is part of the contract.
type wantDiag struct {
	code     string
	severity diag.Severity
}

func gotDiags(t *testing.T, diagnostics []diag.Diagnostic) []wantDiag {
	t.Helper()
	got := make([]wantDiag, 0, len(diagnostics))
	for _, d := range diagnostics {
		got = append(got, wantDiag{code: d.Code, severity: d.Severity})
	}
	return got
}

func assertDiags(t *testing.T, diagnostics []diag.Diagnostic, want []wantDiag) {
	t.Helper()
	got := gotDiags(t, diagnostics)
	if len(got) != len(want) {
		t.Fatalf("diagnostics = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("diagnostic[%d] = %+v, want %+v", i, got[i], want[i])
		}
		if diagnostics[i].Source.File != testSource.File || diagnostics[i].Source.Line != testSource.Line {
			t.Errorf("diagnostic[%d] source = %+v, want %+v", i, diagnostics[i].Source, testSource)
		}
		if diagnostics[i].Target != "devin" {
			t.Errorf("diagnostic[%d] target = %q, want %q", i, diagnostics[i].Target, "devin")
		}
	}
}

// TestSkillFieldMatrix is the Devin skill field matrix: every modeled Skill IR
// field with its exact rendered artifact and diagnostics.
func TestSkillFieldMatrix(t *testing.T) {
	tests := []struct {
		name      string
		skill     *ir.Skill
		wantPath  string
		wantBytes string
		wantDiags []wantDiag
	}{
		{
			name:     "minimal skill",
			skill:    ir.NewSkill("demo", "Demo skill", "Body\n", nil, nil, testSource),
			wantPath: "skills/demo/SKILL.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo skill\n" +
				"---\n" +
				"Body\n",
		},
		{
			name: "every representable field",
			skill: ir.NewSkillWithOptions("demo", "Demo skill", "Body\n", []string{"user", "model"}, nil, testSource,
				ir.WithSkillArgumentHint("<path>"),
				ir.WithSkillModel("sonnet"),
				ir.WithSkillAllowedTools([]string{"Read", "Bash"}),
				ir.WithSkillPermissions(ir.Permissions{Allow: []string{"Read(**)"}, Deny: []string{"Bash(rm:*)"}}),
				ir.WithSkillSubagent("explore"),
				ir.WithSkillAgent("reviewer"),
			),
			wantPath: "skills/demo/SKILL.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo skill\n" +
				"triggers:\n" +
				"  - user\n" +
				"  - model\n" +
				"argument-hint: <path>\n" +
				"model: sonnet\n" +
				"allowed-tools:\n" +
				"  - read\n" +
				"  - exec\n" +
				"permissions:\n" +
				"  allow:\n" +
				"      - Read(**)\n" +
				"  deny:\n" +
				"      - Bash(rm:*)\n" +
				"subagent: explore\n" +
				"agent: reviewer\n" +
				"---\n" +
				"Body\n",
		},
		{
			name: "explicit empty allowed tools and permissions stay explicit",
			skill: ir.NewSkillWithOptions("demo", "Demo skill", "Body\n", []string{}, nil, testSource,
				ir.WithSkillAllowedTools([]string{}),
				ir.WithSkillPermissions(ir.Permissions{}),
			),
			wantPath: "skills/demo/SKILL.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo skill\n" +
				"triggers: []\n" +
				"allowed-tools: []\n" +
				"permissions: {}\n" +
				"---\n" +
				"Body\n",
		},
		{
			name: "duplicate source tools collapse to one Devin tool",
			skill: ir.NewSkillWithOptions("demo", "Demo skill", "Body\n", nil, nil, testSource,
				ir.WithSkillAllowedTools([]string{"Read", "Read"}),
			),
			wantPath: "skills/demo/SKILL.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo skill\n" +
				"allowed-tools:\n" +
				"  - read\n" +
				"---\n" +
				"Body\n",
		},
		{
			name: "unmappable model is omitted with a warning",
			skill: ir.NewSkillWithOptions("demo", "Demo skill", "Body\n", nil, nil, testSource,
				ir.WithSkillModel("claude-sonnet-4-5-20250929"),
			),
			wantPath: "skills/demo/SKILL.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo skill\n" +
				"---\n" +
				"Body\n",
			wantDiags: []wantDiag{{diag.CodeSkillDevinModelUnmappable, diag.SeverityWarning}},
		},
		{
			name:     "unmappable trigger is dropped with a warning",
			skill:    ir.NewSkill("demo", "Demo skill", "Body\n", []string{"user", "click"}, nil, testSource),
			wantPath: "skills/demo/SKILL.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo skill\n" +
				"triggers:\n" +
				"  - user\n" +
				"---\n" +
				"Body\n",
			wantDiags: []wantDiag{{diag.CodeSkillDevinTriggerUnmappable, diag.SeverityWarning}},
		},
		// The three trigger branches. Activation must never end up broader than
		// the source asked for: Devin reads an absent `triggers` key as its
		// user+model default, so the key may only be omitted when the source
		// expressed no opinion at all.
		{
			name:     "nil triggers omit the field and inherit Devin's default",
			skill:    ir.NewSkill("demo", "Demo skill", "Body\n", nil, nil, testSource),
			wantPath: "skills/demo/SKILL.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo skill\n" +
				"---\n" +
				"Body\n",
		},
		{
			name:     "partially mappable triggers emit the mapped subset",
			skill:    ir.NewSkill("demo", "Demo skill", "Body\n", []string{"model", "click"}, nil, testSource),
			wantPath: "skills/demo/SKILL.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo skill\n" +
				"triggers:\n" +
				"  - model\n" +
				"---\n" +
				"Body\n",
			wantDiags: []wantDiag{{diag.CodeSkillDevinTriggerUnmappable, diag.SeverityWarning}},
		},
		{
			name:     "all triggers unmappable emits an explicit empty list",
			skill:    ir.NewSkill("demo", "Demo skill", "Body\n", []string{"click"}, nil, testSource),
			wantPath: "skills/demo/SKILL.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo skill\n" +
				"triggers: []\n" +
				"---\n" +
				"Body\n",
			wantDiags: []wantDiag{{diag.CodeSkillDevinTriggerUnmappable, diag.SeverityWarning}},
		},
		{
			name:     "unknown extras are preserved after canonical fields",
			skill:    ir.NewSkill("demo", "Demo skill", "Body\n", nil, map[string]any{"zeta": "z", "alpha": map[string]any{"nested": true}}, testSource),
			wantPath: "skills/demo/SKILL.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo skill\n" +
				"alpha:\n" +
				"  nested: true\n" +
				"zeta: z\n" +
				"---\n" +
				"Body\n",
		},
		{
			name:     "canonical field wins over conflicting extra",
			skill:    ir.NewSkill("demo", "Demo skill", "Body\n", []string{"user"}, map[string]any{"triggers": []any{"model"}, "keep": "me"}, testSource),
			wantPath: "skills/demo/SKILL.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo skill\n" +
				"triggers:\n" +
				"  - user\n" +
				"keep: me\n" +
				"---\n" +
				"Body\n",
			wantDiags: []wantDiag{{diag.CodeCanonicalFieldOverridesExtra, diag.SeverityWarning}},
		},
		{
			name:     "command provenance marker is reserved for converted commands",
			skill:    ir.NewSkill("demo", "Demo skill", "Body\n", nil, map[string]any{kindExtensionKey: "command"}, testSource),
			wantPath: "skills/demo/SKILL.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo skill\n" +
				"---\n" +
				"Body\n",
			wantDiags: []wantDiag{{diag.CodeCanonicalFieldOverridesExtra, diag.SeverityWarning}},
		},
		{
			// The typed field is absent, so nothing is overridden: the
			// extension is consumed, mapped through the Devin vocabulary, and
			// emitted in canonical position with no conflict warning.
			name:     "skill allowed-tools living only in Extra is strictly mapped",
			skill:    ir.NewSkill("demo", "Demo skill", "Body\n", nil, map[string]any{allowedToolsKey: []any{"Read", "Bash"}, "keep": "me"}, testSource),
			wantPath: "skills/demo/SKILL.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo skill\n" +
				"allowed-tools:\n" +
				"  - read\n" +
				"  - exec\n" +
				"keep: me\n" +
				"---\n" +
				"Body\n",
		},
		{
			// Here the typed field really does override the extension, so the
			// conflict warning is honest.
			name: "typed allowed-tools overrides an allowed-tools extra with a warning",
			skill: ir.NewSkillWithOptions("demo", "Demo skill", "Body\n", nil, map[string]any{allowedToolsKey: []any{"Bash"}}, testSource,
				ir.WithSkillAllowedTools([]string{"Read"}),
			),
			wantPath: "skills/demo/SKILL.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo skill\n" +
				"allowed-tools:\n" +
				"  - read\n" +
				"---\n" +
				"Body\n",
			wantDiags: []wantDiag{{diag.CodeCanonicalFieldOverridesExtra, diag.SeverityWarning}},
		},
		{
			name:     "a permissions extension is reserved and never passed through",
			skill:    ir.NewSkill("demo", "Demo skill", "Body\n", nil, map[string]any{"permissions": map[string]any{"allow": []any{"Read(**)"}}}, testSource),
			wantPath: "skills/demo/SKILL.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo skill\n" +
				"---\n" +
				"Body\n",
			wantDiags: []wantDiag{{diag.CodeCanonicalFieldOverridesExtra, diag.SeverityWarning}},
		},
		{
			name: "support files are reported as deferred",
			skill: func() *ir.Skill {
				s := ir.NewSkill("demo", "Demo skill", "Body\n", nil, nil, testSource)
				s.SupportFiles = []string{"scripts/run.sh"}
				return s
			}(),
			wantPath: "skills/demo/SKILL.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo skill\n" +
				"---\n" +
				"Body\n",
			wantDiags: []wantDiag{{diag.CodeSkillSupportFilesDeferred, diag.SeverityWarning}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compile.Compile(tt.skill, compile.TargetDevin)
			if err != nil {
				t.Fatalf("Compile(skill, Devin): %v", err)
			}
			if len(result.Artifacts) != 1 {
				t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
			}
			if got := result.Artifacts[0].Path; got != tt.wantPath {
				t.Errorf("artifact path = %q, want %q", got, tt.wantPath)
			}
			if got := string(result.Artifacts[0].Bytes); got != tt.wantBytes {
				t.Errorf("artifact bytes =\n%s\nwant\n%s", got, tt.wantBytes)
			}
			assertDiags(t, result.Diagnostics, tt.wantDiags)
		})
	}
}

// TestCommandFieldMatrix pins the command-as-skill conversion: identical
// directory shape, stable provenance marker, and user-only activation.
func TestCommandFieldMatrix(t *testing.T) {
	tests := []struct {
		name      string
		command   *ir.Command
		wantPath  string
		wantBytes string
		wantDiags []wantDiag
	}{
		{
			name:     "minimal command",
			command:  ir.NewCommand("demo", "Demo command", "Body\n", nil, testSource),
			wantPath: "skills/demo/SKILL.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo command\n" +
				"x-agent-marketplace-kind: command\n" +
				"triggers:\n" +
				"  - user\n" +
				"---\n" +
				"Body\n",
		},
		{
			name:     "argument metadata and extras ride along",
			command:  ir.NewCommand("demo", "Demo command", "Body\n", map[string]any{"argument-hint": "<pr>", "model": "sonnet"}, testSource),
			wantPath: "skills/demo/SKILL.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo command\n" +
				"x-agent-marketplace-kind: command\n" +
				"triggers:\n" +
				"  - user\n" +
				"argument-hint: <pr>\n" +
				"model: sonnet\n" +
				"---\n" +
				"Body\n",
		},
		{
			name:     "extras cannot override user-only activation or provenance",
			command:  ir.NewCommand("demo", "Demo command", "Body\n", map[string]any{"triggers": []any{"model"}, kindExtensionKey: "skill"}, testSource),
			wantPath: "skills/demo/SKILL.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo command\n" +
				"x-agent-marketplace-kind: command\n" +
				"triggers:\n" +
				"  - user\n" +
				"---\n" +
				"Body\n",
			wantDiags: []wantDiag{
				{diag.CodeCanonicalFieldOverridesExtra, diag.SeverityWarning},
				{diag.CodeCanonicalFieldOverridesExtra, diag.SeverityWarning},
			},
		},
		{
			name:     "allowed-tools extension is strictly mapped, never passed through",
			command:  ir.NewCommand("demo", "Demo command", "Body\n", map[string]any{allowedToolsKey: []any{"Read", "Bash"}}, testSource),
			wantPath: "skills/demo/SKILL.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo command\n" +
				"x-agent-marketplace-kind: command\n" +
				"triggers:\n" +
				"  - user\n" +
				"allowed-tools:\n" +
				"  - read\n" +
				"  - exec\n" +
				"---\n" +
				"Body\n",
			// No canonical-override warning: nothing was overridden. The
			// extension field was consumed and mapped, not discarded.
		},
		{
			name:     "comma separated allowed-tools extension is strictly mapped",
			command:  ir.NewCommand("demo", "Demo command", "Body\n", map[string]any{allowedToolsKey: "Read, Grep"}, testSource),
			wantPath: "skills/demo/SKILL.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo command\n" +
				"x-agent-marketplace-kind: command\n" +
				"triggers:\n" +
				"  - user\n" +
				"allowed-tools:\n" +
				"  - read\n" +
				"  - grep\n" +
				"---\n" +
				"Body\n",
		},
		{
			name:     "empty-string allowed-tools extension is an explicit empty restriction",
			command:  ir.NewCommand("demo", "Demo command", "Body\n", map[string]any{allowedToolsKey: ""}, testSource),
			wantPath: "skills/demo/SKILL.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo command\n" +
				"x-agent-marketplace-kind: command\n" +
				"triggers:\n" +
				"  - user\n" +
				"allowed-tools: []\n" +
				"---\n" +
				"Body\n",
		},
		{
			name:     "a permissions extension is reserved and never passed through",
			command:  ir.NewCommand("demo", "Demo command", "Body\n", map[string]any{"permissions": map[string]any{"allow": []any{"Read(**)"}}}, testSource),
			wantPath: "skills/demo/SKILL.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo command\n" +
				"x-agent-marketplace-kind: command\n" +
				"triggers:\n" +
				"  - user\n" +
				"---\n" +
				"Body\n",
			wantDiags: []wantDiag{{diag.CodeCanonicalFieldOverridesExtra, diag.SeverityWarning}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compile.Compile(tt.command, compile.TargetDevin)
			if err != nil {
				t.Fatalf("Compile(command, Devin): %v", err)
			}
			if len(result.Artifacts) != 1 {
				t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
			}
			if got := result.Artifacts[0].Path; got != tt.wantPath {
				t.Errorf("artifact path = %q, want %q", got, tt.wantPath)
			}
			if got := string(result.Artifacts[0].Bytes); got != tt.wantBytes {
				t.Errorf("artifact bytes =\n%s\nwant\n%s", got, tt.wantBytes)
			}
			assertDiags(t, result.Diagnostics, tt.wantDiags)
		})
	}
}

// TestUnrepresentableAllowedToolsOmitTheKey replaces
// TestSecurityFieldFailuresSuppressArtifacts, which pinned the previous contract
// ("If any explicitly allowed tool is unmappable, the resource produces an error
// diagnostic and no artifact"). Bead agent-marketplace-pu6b reversed that
// deliberately: the resource is now published with NO allowed-tools key and a
// WARNING per offending entry.
//
// The old assertions are not deleted so much as inverted — every case below is
// one of the cases that used to be asserted to produce zero artifacts, and each
// now asserts the exact emitted bytes, so a silent return to suppression fails
// here. The reversal's rationale lives in compile/devin/fields.go and
// diag/codes.md; scoped `Bash(...)` is what the openspec generator emits, and
// Devin cannot REPRESENT that restriction at all — not "Devin does not enforce
// allowed-tools", which fields.go records as false.
//
// Gemini and Cursor still fail closed on their own tool-unknown codes; this test
// is only Devin and Factory on purpose.
func TestUnrepresentableAllowedToolsOmitTheKey(t *testing.T) {
	const skillBytes = "---\n" +
		"name: demo\n" +
		"description: Demo skill\n" +
		"---\n" +
		"Body\n"
	const commandBytes = "---\n" +
		"name: demo\n" +
		"description: Demo command\n" +
		"x-agent-marketplace-kind: command\n" +
		"triggers:\n" +
		"  - user\n" +
		"---\n" +
		"Body\n"

	tests := []struct {
		name      string
		resource  ir.Resource
		wantPath  string
		wantBytes string
		wantDiags []wantDiag
		// wantMessages asserts exact wording where several distinct losses share
		// one diagnostic code.
		wantMessages []string
	}{
		{
			name: "skill with one unmappable allowed tool",
			resource: ir.NewSkillWithOptions("demo", "Demo skill", "Body\n", nil, nil, testSource,
				ir.WithSkillAllowedTools([]string{"Read", "WebSearch"}),
			),
			wantPath: "skills/demo/SKILL.md",
			// The mappable `read` is NOT emitted on its own: a partial list is
			// narrower than the source asked for.
			wantBytes: skillBytes,
			wantDiags: []wantDiag{{diag.CodeSkillDevinToolUnknown, diag.SeverityWarning}},
		},
		{
			name: "skill with several unmappable allowed tools reports each",
			resource: ir.NewSkillWithOptions("demo", "Demo skill", "Body\n", nil, nil, testSource,
				ir.WithSkillAllowedTools([]string{"Write", "WebFetch"}),
			),
			wantPath:  "skills/demo/SKILL.md",
			wantBytes: skillBytes,
			wantDiags: []wantDiag{
				{diag.CodeSkillDevinToolUnknown, diag.SeverityWarning},
				{diag.CodeSkillDevinToolUnknown, diag.SeverityWarning},
			},
		},
		{
			name:      "command with an unmappable allowed-tools extension",
			resource:  ir.NewCommand("demo", "Demo command", "Body\n", map[string]any{allowedToolsKey: []any{"Write"}}, testSource),
			wantPath:  "skills/demo/SKILL.md",
			wantBytes: commandBytes,
			wantDiags: []wantDiag{{diag.CodeSkillDevinToolUnknown, diag.SeverityWarning}},
		},
		{
			// The three distinct messages behind skill.devin.tool-unknown are
			// asserted explicitly because assertDiags compares code+severity
			// only and would not tell them apart.
			name:         "command with an unverifiable allowed-tools shape",
			resource:     ir.NewCommand("demo", "Demo command", "Body\n", map[string]any{allowedToolsKey: map[string]any{"read": true}}, testSource),
			wantPath:     "skills/demo/SKILL.md",
			wantBytes:    commandBytes,
			wantDiags:    []wantDiag{{diag.CodeSkillDevinToolUnknown, diag.SeverityWarning}},
			wantMessages: []string{"explicit allowed-tools restriction is not a list of tool names; the allowed-tools key was omitted and Devin's default tool set applies"},
		},
		{
			name: "skill with a scoped tool specifier keeps the skill",
			resource: ir.NewSkillWithOptions("demo", "Demo skill", "Body\n", nil, nil, testSource,
				ir.WithSkillAllowedTools([]string{"Bash(git diff:*)"}),
			),
			wantPath:     "skills/demo/SKILL.md",
			wantBytes:    skillBytes,
			wantDiags:    []wantDiag{{diag.CodeSkillDevinToolUnknown, diag.SeverityWarning}},
			wantMessages: []string{"an allowed-tools entry carries a scoped tool specifier, which has no Devin equivalent; the allowed-tools key was omitted and Devin's default tool set applies"},
		},
		{
			// This is the exact shape the openspec skills carry:
			// `allowed-tools: Bash(openspec:*)` as a scalar extension field.
			// It was the whole reason for the reversal, so it is pinned here.
			name:      "openspec-style scoped Bash extension keeps the command",
			resource:  ir.NewCommand("demo", "Demo command", "Body\n", map[string]any{allowedToolsKey: "Bash(openspec:*)"}, testSource),
			wantPath:  "skills/demo/SKILL.md",
			wantBytes: commandBytes,
			wantDiags: []wantDiag{{diag.CodeSkillDevinToolUnknown, diag.SeverityWarning}},
		},
		{
			// The comma-split mangles `Bash(git add, git commit)` into
			// fragments; every fragment still carries a parenthesis, so no
			// fragment is ever rewritten into a bare tool name.
			name:      "command with a comma-bearing scoped specifier drops the whole key",
			resource:  ir.NewCommand("demo", "Demo command", "Body\n", map[string]any{allowedToolsKey: "Bash(git add, git commit), Read"}, testSource),
			wantPath:  "skills/demo/SKILL.md",
			wantBytes: commandBytes,
			wantDiags: []wantDiag{
				{diag.CodeSkillDevinToolUnknown, diag.SeverityWarning},
				{diag.CodeSkillDevinToolUnknown, diag.SeverityWarning},
			},
			wantMessages: []string{
				"an allowed-tools entry carries a scoped tool specifier, which has no Devin equivalent; the allowed-tools key was omitted and Devin's default tool set applies",
				"an allowed-tools entry carries a scoped tool specifier, which has no Devin equivalent; the allowed-tools key was omitted and Devin's default tool set applies",
			},
		},
		{
			// No diagnostic or log may quote the entry: allowed-tools carries
			// command strings and permission values. Unchanged by the reversal.
			name:      "unmappable tool diagnostics never quote the tool",
			resource:  ir.NewCommand("demo", "Demo command", "Body\n", map[string]any{allowedToolsKey: []any{"WebSearch"}}, testSource),
			wantPath:  "skills/demo/SKILL.md",
			wantBytes: commandBytes,
			wantDiags: []wantDiag{{diag.CodeSkillDevinToolUnknown, diag.SeverityWarning}},
			wantMessages: []string{
				"an explicitly allowed tool has no Devin equivalent; the allowed-tools key was omitted and Devin's default tool set applies",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compile.Compile(tt.resource, compile.TargetDevin)
			if err != nil {
				t.Fatalf("Compile(resource, Devin): %v", err)
			}
			if len(result.Artifacts) != 1 {
				t.Fatalf("artifacts = %d, want 1 (an unrepresentable allowed-tools restriction must not suppress the resource)", len(result.Artifacts))
			}
			if got := result.Artifacts[0].Path; got != tt.wantPath {
				t.Errorf("artifact path = %q, want %q", got, tt.wantPath)
			}
			if got := string(result.Artifacts[0].Bytes); got != tt.wantBytes {
				t.Errorf("artifact bytes =\n%s\nwant\n%s", got, tt.wantBytes)
			}
			if strings.Contains(string(result.Artifacts[0].Bytes), allowedToolsKey) {
				t.Errorf("artifact carries an %s key:\n%s", allowedToolsKey, result.Artifacts[0].Bytes)
			}
			if diag.HasErrors(result.Diagnostics) {
				t.Errorf("diagnostics contain an error: %+v", result.Diagnostics)
			}
			assertDiags(t, result.Diagnostics, tt.wantDiags)
			for i, want := range tt.wantMessages {
				if got := result.Diagnostics[i].Message; got != want {
					t.Errorf("diagnostic[%d] message = %q, want %q", i, got, want)
				}
			}
		})
	}
}

// TestUnrepresentableAllowedToolsDiagnosticsNeverQuoteTheSpecifier keeps the
// property the reversal must not cost: a diagnostic that now rides along with a
// PUBLISHED artifact still may not echo the specifier, because allowed-tools
// entries carry command strings such as `Bash(git push --force:*)`.
func TestUnrepresentableAllowedToolsDiagnosticsNeverQuoteTheSpecifier(t *testing.T) {
	resource := ir.NewSkillWithOptions("demo", "Demo skill", "Body\n", nil, nil, testSource,
		ir.WithSkillAllowedTools([]string{"Bash(git push --force:*)", "mcp__internal__deploy"}),
	)
	result, err := compile.Compile(resource, compile.TargetDevin)
	if err != nil {
		t.Fatalf("Compile(skill, Devin): %v", err)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
	}
	if len(result.Diagnostics) == 0 {
		t.Fatal("diagnostics = 0, want the omitted-restriction warnings")
	}
	forbidden := []string{"git push --force", "Bash(", "mcp__internal__deploy"}
	for _, d := range result.Diagnostics {
		for _, value := range forbidden {
			if strings.Contains(d.Message, value) {
				t.Errorf("diagnostic %s message %q leaks %q", d.Code, d.Message, value)
			}
			if strings.Contains(d.Hint, value) {
				t.Errorf("diagnostic %s hint %q leaks %q", d.Code, d.Hint, value)
			}
		}
	}
}

// TestParseToolList pins every shape the allowed-tools extension field can
// take. It is a security boundary: a shape this function accepts is a shape
// that determines an explicit tool restriction.
func TestParseToolList(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  []string
		wantO bool
	}{
		{name: "typed string slice", value: []string{"Read", "Bash"}, want: []string{"Read", "Bash"}, wantO: true},
		{name: "any slice of strings", value: []any{"Read", "Bash"}, want: []string{"Read", "Bash"}, wantO: true},
		{name: "any slice with a non-string element is rejected", value: []any{"Read", 7}, want: nil, wantO: false},
		{name: "empty any slice is an explicit empty restriction", value: []any{}, want: []string{}, wantO: true},
		{name: "empty string is an explicit empty restriction", value: "", want: []string{}, wantO: true},
		{name: "nil is not a list shape", value: nil, want: nil, wantO: false},
		{name: "blank comma fields are skipped", value: "a,,b", want: []string{"a", "b"}, wantO: true},
		{name: "scoped specifier survives splitting as parenthesized fragments", value: "Bash(a, b)", want: []string{"Bash(a", "b)"}, wantO: true},
		{name: "map shape is rejected", value: map[string]any{"read": true}, want: nil, wantO: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseToolList(tt.value)
			if ok != tt.wantO {
				t.Fatalf("ok = %v, want %v", ok, tt.wantO)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("tools = %#v, want %#v (nil and empty are distinct restrictions)", got, tt.want)
			}
		})
	}
}

// TestToolOverrideIsHonored checks that the documented per-invocation override
// escape hatch still applies to the closed Devin vocabulary, so consumers are
// not blocked on a library release for a new Devin tool.
func TestToolOverrideIsHonored(t *testing.T) {
	skill := ir.NewSkillWithOptions("demo", "Demo skill", "Body\n", nil, nil, testSource,
		ir.WithSkillAllowedTools([]string{"WebSearch"}),
	)
	result, err := compile.Compile(skill, compile.TargetDevin, compile.WithToolOverride(map[string]string{"WebSearch": "exec"}))
	if err != nil {
		t.Fatalf("Compile(skill, Devin): %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v, want none", result.Diagnostics)
	}
	want := "---\nname: demo\ndescription: Demo skill\nallowed-tools:\n  - exec\n---\nBody\n"
	if len(result.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
	}
	if got := string(result.Artifacts[0].Bytes); got != want {
		t.Errorf("artifact bytes =\n%s\nwant\n%s", got, want)
	}
}

// TestSkillAndCommandShareOnePath documents the collision precondition that
// harnesscompile's bundle guard resolves.
func TestSkillAndCommandShareOnePath(t *testing.T) {
	skill, err := compile.Compile(ir.NewSkill("review", "Skill", "Body\n", nil, nil, testSource), compile.TargetDevin)
	if err != nil {
		t.Fatalf("Compile(skill, Devin): %v", err)
	}
	command, err := compile.Compile(ir.NewCommand("review", "Command", "Body\n", nil, testSource), compile.TargetDevin)
	if err != nil {
		t.Fatalf("Compile(command, Devin): %v", err)
	}
	if len(skill.Artifacts) != 1 || len(command.Artifacts) != 1 {
		t.Fatalf("skill artifacts = %d, command artifacts = %d, want 1 each", len(skill.Artifacts), len(command.Artifacts))
	}
	if skill.Artifacts[0].Path != command.Artifacts[0].Path {
		t.Fatalf("skill path %q and command path %q differ; the bundle collision guard would be unreachable",
			skill.Artifacts[0].Path, command.Artifacts[0].Path)
	}
	if skill.Artifacts[0].Path != "skills/review/SKILL.md" {
		t.Errorf("shared path = %q, want %q", skill.Artifacts[0].Path, "skills/review/SKILL.md")
	}
}
