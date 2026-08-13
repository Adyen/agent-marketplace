package devin

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

var subagentSource = ir.SourceRef{File: "agents/demo.md", Line: 1}

// newSubagent builds a Devin-representable subagent: an explicit stable model,
// no restrictions Devin cannot express. Every test that is not about the model
// starts here so the mandatory empty-model warning does not mask the assertion
// under test.
func newSubagent(options ...ir.SubagentOption) *ir.Subagent {
	return ir.NewSubagentWithOptions("demo", "Demo subagent", "Body\n", "sonnet", nil, "", nil, "", nil, subagentSource, options...)
}

// assertSubagentDiags is assertDiags for the subagent source ref; it also pins
// Resource, which is the only field that localizes a value-free diagnostic.
func assertSubagentDiags(t *testing.T, diagnostics []diag.Diagnostic, want []wantDiag, resource string) {
	t.Helper()
	got := gotDiags(t, diagnostics)
	if len(got) != len(want) {
		t.Fatalf("diagnostics = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("diagnostic[%d] = %+v, want %+v", i, got[i], want[i])
		}
		if diagnostics[i].Target != "devin" {
			t.Errorf("diagnostic[%d] target = %q, want %q", i, diagnostics[i].Target, "devin")
		}
		if diagnostics[i].Resource != resource {
			t.Errorf("diagnostic[%d] resource = %q, want %q", i, diagnostics[i].Resource, resource)
		}
		if diagnostics[i].Source.File != subagentSource.File || diagnostics[i].Source.Line != subagentSource.Line {
			t.Errorf("diagnostic[%d] source = %+v, want %+v", i, diagnostics[i].Source, subagentSource)
		}
	}
}

// TestSubagentFieldMatrix is the Devin subagent field matrix: every modeled
// Subagent IR field with its exact artifact path, exact rendered bytes, and
// exact diagnostics.
func TestSubagentFieldMatrix(t *testing.T) {
	tests := []struct {
		name      string
		subagent  *ir.Subagent
		wantPath  string
		wantBytes string
		wantDiags []wantDiag
	}{
		{
			name:     "minimal subagent with an explicit model",
			subagent: newSubagent(),
			wantPath: "agents/demo.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo subagent\n" +
				"model: sonnet\n" +
				"---\n" +
				"Body\n",
		},
		{
			name: "every representable field",
			subagent: ir.NewSubagentWithOptions("demo", "Demo subagent", "Body\n", "opus", []string{"Read", "Bash"}, "", nil, "", nil, subagentSource,
				ir.WithSubagentMaxNesting(2),
			),
			wantPath: "agents/demo.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo subagent\n" +
				"model: opus\n" +
				"allowed-tools:\n" +
				"  - read\n" +
				"  - exec\n" +
				"max-nesting: 2\n" +
				"---\n" +
				"Body\n",
		},
		{
			// The plan is explicit that an absent model is its own loss case:
			// "An empty subagent model stays empty and emits a warning because
			// Devin interprets absence as its default subagent model, not
			// parent-model inheritance."
			name:     "empty model stays empty with the default-model warning",
			subagent: ir.NewSubagent("demo", "Demo subagent", "Body\n", "", nil, "", nil, "", nil, subagentSource),
			wantPath: "agents/demo.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo subagent\n" +
				"---\n" +
				"Body\n",
			wantDiags: []wantDiag{{diag.CodeSubagentDevinModelDefault, diag.SeverityWarning}},
		},
		{
			name:     "unmappable model is omitted with its own warning",
			subagent: ir.NewSubagent("demo", "Demo subagent", "Body\n", "claude-sonnet-4-5-20250929", nil, "", nil, "", nil, subagentSource),
			wantPath: "agents/demo.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo subagent\n" +
				"---\n" +
				"Body\n",
			// Exactly one diagnostic: the unmappable-model warning already says
			// no model was written, so the default-model warning would be noise
			// on the same decision.
			wantDiags: []wantDiag{{diag.CodeSubagentDevinModelUnmappable, diag.SeverityWarning}},
		},
		{
			name:     "effort is dropped with a warning",
			subagent: ir.NewSubagent("demo", "Demo subagent", "Body\n", "sonnet", nil, "high", nil, "", nil, subagentSource),
			wantPath: "agents/demo.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo subagent\n" +
				"model: sonnet\n" +
				"---\n" +
				"Body\n",
			wantDiags: []wantDiag{{diag.CodeSubagentDevinEffortDropped, diag.SeverityWarning}},
		},
		{
			name:     "nil tools omit the restriction and inherit Devin's default",
			subagent: newSubagent(),
			wantPath: "agents/demo.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo subagent\n" +
				"model: sonnet\n" +
				"---\n" +
				"Body\n",
		},
		{
			// An explicit empty list is the narrowest representable value and
			// must survive as `[]`; omitting the key would restore Devin's full
			// default tool set.
			name:     "explicit empty tools stay an explicit empty restriction",
			subagent: ir.NewSubagent("demo", "Demo subagent", "Body\n", "sonnet", []string{}, "", nil, "", nil, subagentSource),
			wantPath: "agents/demo.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo subagent\n" +
				"model: sonnet\n" +
				"allowed-tools: []\n" +
				"---\n" +
				"Body\n",
		},
		{
			name:     "duplicate source tools collapse to one Devin tool",
			subagent: ir.NewSubagent("demo", "Demo subagent", "Body\n", "sonnet", []string{"Read", "Read"}, "", nil, "", nil, subagentSource),
			wantPath: "agents/demo.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo subagent\n" +
				"model: sonnet\n" +
				"allowed-tools:\n" +
				"  - read\n" +
				"---\n" +
				"Body\n",
		},
		{
			name:     "unknown extras are preserved after canonical fields",
			subagent: ir.NewSubagent("demo", "Demo subagent", "Body\n", "sonnet", nil, "", nil, "", map[string]any{"zeta": "z", "alpha": map[string]any{"nested": true}}, subagentSource),
			wantPath: "agents/demo.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo subagent\n" +
				"model: sonnet\n" +
				"alpha:\n" +
				"  nested: true\n" +
				"zeta: z\n" +
				"---\n" +
				"Body\n",
		},
		{
			// The typed field is absent, so nothing is overridden: the
			// extension is consumed, mapped through the closed Devin
			// vocabulary, and emitted in canonical position with no conflict
			// warning. D4's rule: the conflict warning must not fire for a
			// field that was consumed and mapped rather than overridden.
			name:     "allowed-tools living only in Extra is strictly mapped",
			subagent: ir.NewSubagent("demo", "Demo subagent", "Body\n", "sonnet", nil, "", nil, "", map[string]any{allowedToolsKey: []any{"Read", "Bash"}, "keep": "me"}, subagentSource),
			wantPath: "agents/demo.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo subagent\n" +
				"model: sonnet\n" +
				"allowed-tools:\n" +
				"  - read\n" +
				"  - exec\n" +
				"keep: me\n" +
				"---\n" +
				"Body\n",
		},
		{
			name:     "typed tools override an allowed-tools extra with a warning",
			subagent: ir.NewSubagent("demo", "Demo subagent", "Body\n", "sonnet", []string{"Read"}, "", nil, "", map[string]any{allowedToolsKey: []any{"Bash"}}, subagentSource),
			wantPath: "agents/demo.md",
			wantBytes: "---\n" +
				"name: demo\n" +
				"description: Demo subagent\n" +
				"model: sonnet\n" +
				"allowed-tools:\n" +
				"  - read\n" +
				"---\n" +
				"Body\n",
			wantDiags: []wantDiag{{diag.CodeCanonicalFieldOverridesExtra, diag.SeverityWarning}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compile.Compile(tt.subagent, compile.TargetDevin)
			if err != nil {
				t.Fatalf("Compile(subagent, Devin): %v", err)
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
			assertSubagentDiags(t, result.Diagnostics, tt.wantDiags, "demo")
		})
	}
}

// TestSubagentMaxNestingRoundTrips pins the plan's typed-field requirement:
// "The Subagent IR gains a typed MaxNesting field. Devin round-trips it."
// The rendered value must be a YAML integer (never a quoted string), and an
// explicit 0 — the strictest limit — must not be mistaken for absence.
func TestSubagentMaxNestingRoundTrips(t *testing.T) {
	tests := []struct {
		name     string
		nesting  *int
		wantLine string
	}{
		{name: "absent", nesting: nil, wantLine: ""},
		{name: "zero is explicit", nesting: intPtr(0), wantLine: "max-nesting: 0\n"},
		{name: "one", nesting: intPtr(1), wantLine: "max-nesting: 1\n"},
		{name: "large", nesting: intPtr(64), wantLine: "max-nesting: 64\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compile.Compile(newSubagent(ir.WithSubagentMaxNestingPtr(tt.nesting)), compile.TargetDevin)
			if err != nil {
				t.Fatalf("Compile(subagent, Devin): %v", err)
			}
			if len(result.Artifacts) != 1 {
				t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
			}
			got := string(result.Artifacts[0].Bytes)
			if tt.wantLine == "" {
				if strings.Contains(got, maxNestingKey) {
					t.Fatalf("artifact bytes =\n%s\nwant no %s key", got, maxNestingKey)
				}
				return
			}
			if !strings.Contains(got, tt.wantLine) {
				t.Fatalf("artifact bytes =\n%s\nwant line %q", got, tt.wantLine)
			}
		})
	}
}

// TestSubagentFailuresSuppressArtifacts covers the promoted subagent loss cases
// that must produce an error and no artifact:
//
//	"Isolation: error; no artifact"
//	"DisallowedTools: error; no artifact"
//	"built-in name conflict: error; no artifact"
//
// The fourth clause of the promoted plan — "If any explicitly allowed tool is
// unmappable, the resource produces an error diagnostic and no artifact" — was
// REVERSED by bead agent-marketplace-pu6b and is no longer asserted here. Its
// replacement is TestUnrepresentableSubagentToolsOmitTheKey below, which asserts
// the opposite outcome and the reason.
//
// The three clauses above are deliberately NOT reversed: Devin has no isolation
// and no disallowedTools field at all, and a built-in name is silently shadowed,
// so emitting those profiles would advertise controls that do not exist.
func TestSubagentFailuresSuppressArtifacts(t *testing.T) {
	tests := []struct {
		name      string
		subagent  *ir.Subagent
		resource  string
		wantDiags []wantDiag
		// wantMessages asserts exact wording where one code covers several
		// distinct failures.
		wantMessages []string
	}{
		{
			name:      "isolation",
			subagent:  ir.NewSubagent("demo", "Demo subagent", "Body\n", "sonnet", nil, "", nil, "worktree", nil, subagentSource),
			resource:  "demo",
			wantDiags: []wantDiag{{diag.CodeSubagentSecurityRestrictionUnsupported, diag.SeverityError}},
			wantMessages: []string{
				"Devin cannot represent isolation; subagent artifact suppressed",
			},
		},
		{
			name:      "disallowed tools",
			subagent:  ir.NewSubagent("demo", "Demo subagent", "Body\n", "sonnet", nil, "", []string{"Bash"}, "", nil, subagentSource),
			resource:  "demo",
			wantDiags: []wantDiag{{diag.CodeSubagentSecurityRestrictionUnsupported, diag.SeverityError}},
			wantMessages: []string{
				"Devin cannot represent disallowedTools; subagent artifact suppressed",
			},
		},
		{
			name:     "both restrictions report both reasons in a fixed order",
			subagent: ir.NewSubagent("demo", "Demo subagent", "Body\n", "sonnet", nil, "", []string{"Bash"}, "worktree", nil, subagentSource),
			resource: "demo",
			wantDiags: []wantDiag{
				{diag.CodeSubagentSecurityRestrictionUnsupported, diag.SeverityError},
				{diag.CodeSubagentSecurityRestrictionUnsupported, diag.SeverityError},
			},
			wantMessages: []string{
				"Devin cannot represent disallowedTools; subagent artifact suppressed",
				"Devin cannot represent isolation; subagent artifact suppressed",
			},
		},
		{
			name:      "built-in explore name",
			subagent:  ir.NewSubagent("subagent_explore", "Demo subagent", "Body\n", "sonnet", nil, "", nil, "", nil, subagentSource),
			resource:  "subagent_explore",
			wantDiags: []wantDiag{{diag.CodeSubagentDevinBuiltinNameConflict, diag.SeverityError}},
		},
		{
			name:      "built-in general name",
			subagent:  ir.NewSubagent("subagent_general", "Demo subagent", "Body\n", "sonnet", nil, "", nil, "", nil, subagentSource),
			resource:  "subagent_general",
			wantDiags: []wantDiag{{diag.CodeSubagentDevinBuiltinNameConflict, diag.SeverityError}},
		},
		{
			// A case-insensitive filesystem resolves agents/Subagent_Explore.md
			// to the same file as the built-in's name, so a differently-cased
			// name must be rejected too.
			name:      "built-in name in a different case",
			subagent:  ir.NewSubagent("Subagent_Explore", "Demo subagent", "Body\n", "sonnet", nil, "", nil, "", nil, subagentSource),
			resource:  "Subagent_Explore",
			wantDiags: []wantDiag{{diag.CodeSubagentDevinBuiltinNameConflict, diag.SeverityError}},
		},
		{
			// An unmappable tool no longer belongs in this test's own right
			// (see TestUnrepresentableSubagentToolsOmitTheKey). It stays here
			// only as a rider on a subagent that fails for a REAL reason, to
			// pin that every finding is still reported in one compile and in a
			// fixed order: built-in name, disallowedTools, isolation, tools.
			name:     "all failure reasons are collected in a fixed order",
			subagent: ir.NewSubagent("subagent_explore", "Demo subagent", "Body\n", "sonnet", []string{"WebSearch"}, "", []string{"Bash"}, "worktree", nil, subagentSource),
			resource: "subagent_explore",
			wantDiags: []wantDiag{
				{diag.CodeSubagentDevinBuiltinNameConflict, diag.SeverityError},
				{diag.CodeSubagentSecurityRestrictionUnsupported, diag.SeverityError},
				{diag.CodeSubagentSecurityRestrictionUnsupported, diag.SeverityError},
				// A warning riding along with the blocking errors: the artifact
				// is suppressed by the other three, not by this one.
				{diag.CodeSubagentDevinToolUnknown, diag.SeverityWarning},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compile.Compile(tt.subagent, compile.TargetDevin)
			if err != nil {
				t.Fatalf("Compile(subagent, Devin): %v", err)
			}
			if len(result.Artifacts) != 0 {
				t.Fatalf("artifacts = %d, want 0", len(result.Artifacts))
			}
			assertSubagentDiags(t, result.Diagnostics, tt.wantDiags, tt.resource)
			for i, want := range tt.wantMessages {
				if got := result.Diagnostics[i].Message; got != want {
					t.Errorf("diagnostic[%d] message = %q, want %q", i, got, want)
				}
			}
		})
	}
}

// TestUnrepresentableSubagentToolsOmitTheKey is the subagent half of the
// agent-marketplace-pu6b reversal, and the reason resolveAllowedTools is shared:
// a rule applied to skills and commands must not drift for subagents.
//
// Every case here used to be an error with zero artifacts. Each now asserts the
// exact emitted profile, so a silent return to suppression fails. `model` still
// renders, which is the point: the profile is intact except for the one key
// Devin cannot express. (`max-nesting` round-trips on the same path and is
// covered by TestSubagentMaxNestingRoundTrips.)
func TestUnrepresentableSubagentToolsOmitTheKey(t *testing.T) {
	const profile = "---\n" +
		"name: demo\n" +
		"description: Demo subagent\n" +
		"model: sonnet\n" +
		"---\n" +
		"Body\n"

	tests := []struct {
		name         string
		subagent     *ir.Subagent
		wantDiags    []wantDiag
		wantMessages []string
	}{
		{
			name:      "one unmappable tool",
			subagent:  ir.NewSubagent("demo", "Demo subagent", "Body\n", "sonnet", []string{"Read", "WebSearch"}, "", nil, "", nil, subagentSource),
			wantDiags: []wantDiag{{diag.CodeSubagentDevinToolUnknown, diag.SeverityWarning}},
			wantMessages: []string{
				"an explicitly allowed tool has no Devin equivalent; the allowed-tools key was omitted and Devin's default tool set applies",
			},
		},
		{
			name:     "several unmappable tools report each",
			subagent: ir.NewSubagent("demo", "Demo subagent", "Body\n", "sonnet", []string{"Write", "WebFetch"}, "", nil, "", nil, subagentSource),
			wantDiags: []wantDiag{
				{diag.CodeSubagentDevinToolUnknown, diag.SeverityWarning},
				{diag.CodeSubagentDevinToolUnknown, diag.SeverityWarning},
			},
		},
		{
			// Mapping a scoped specifier to bare `exec` would invent a broader
			// restriction than the source wrote, so the key is dropped whole —
			// exactly as it is for skills.
			name:      "scoped tool specifier drops the key, not the profile",
			subagent:  ir.NewSubagent("demo", "Demo subagent", "Body\n", "sonnet", []string{"Bash(openspec:*)"}, "", nil, "", nil, subagentSource),
			wantDiags: []wantDiag{{diag.CodeSubagentDevinToolUnknown, diag.SeverityWarning}},
			wantMessages: []string{
				"an allowed-tools entry carries a scoped tool specifier, which has no Devin equivalent; the allowed-tools key was omitted and Devin's default tool set applies",
			},
		},
		{
			name:      "unverifiable allowed-tools extension shape",
			subagent:  ir.NewSubagent("demo", "Demo subagent", "Body\n", "sonnet", nil, "", nil, "", map[string]any{allowedToolsKey: map[string]any{"read": true}}, subagentSource),
			wantDiags: []wantDiag{{diag.CodeSubagentDevinToolUnknown, diag.SeverityWarning}},
			wantMessages: []string{
				"explicit allowed-tools restriction is not a list of tool names; the allowed-tools key was omitted and Devin's default tool set applies",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compile.Compile(tt.subagent, compile.TargetDevin)
			if err != nil {
				t.Fatalf("Compile(subagent, Devin): %v", err)
			}
			if len(result.Artifacts) != 1 {
				t.Fatalf("artifacts = %d, want 1 (an unrepresentable allowed-tools restriction must not suppress the profile)", len(result.Artifacts))
			}
			if got := result.Artifacts[0].Path; got != "agents/demo.md" {
				t.Errorf("artifact path = %q, want %q", got, "agents/demo.md")
			}
			if got := string(result.Artifacts[0].Bytes); got != profile {
				t.Errorf("artifact bytes =\n%s\nwant\n%s", got, profile)
			}
			if diag.HasErrors(result.Diagnostics) {
				t.Errorf("diagnostics contain an error: %+v", result.Diagnostics)
			}
			assertSubagentDiags(t, result.Diagnostics, tt.wantDiags, "demo")
			for i, want := range tt.wantMessages {
				if got := result.Diagnostics[i].Message; got != want {
					t.Errorf("diagnostic[%d] message = %q, want %q", i, got, want)
				}
			}
		})
	}
}

// TestSubagentFailureDiagnosticsNeverQuoteRestrictedValues enforces D4's rule
// for the subagent path: no tool identifier, permission value, or isolation
// mode may appear in a diagnostic message, because those strings carry scoped
// permission and command content.
func TestSubagentFailureDiagnosticsNeverQuoteRestrictedValues(t *testing.T) {
	subagent := ir.NewSubagent("demo", "Demo subagent", "Body\n", "sonnet",
		[]string{"Bash(git push --force:*)"}, "", []string{"WebFetch(https://internal:*)"}, "worktree-secretmode", nil, subagentSource)

	result, err := compile.Compile(subagent, compile.TargetDevin)
	if err != nil {
		t.Fatalf("Compile(subagent, Devin): %v", err)
	}
	if len(result.Artifacts) != 0 {
		t.Fatalf("artifacts = %d, want 0", len(result.Artifacts))
	}
	if len(result.Diagnostics) == 0 {
		t.Fatal("diagnostics = 0, want the suppression reasons")
	}
	forbidden := []string{"git push --force", "WebFetch", "https://internal", "worktree-secretmode", "Bash("}
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

// TestSubagentReservedExtrasAreNeverPassedThrough covers the extension keys that
// must never reach the artifact verbatim. Each one would otherwise let an
// unverified extension value relax a limit or advertise a restriction Devin does
// not enforce.
func TestSubagentReservedExtrasAreNeverPassedThrough(t *testing.T) {
	reserved := []struct {
		key   string
		value any
	}{
		{key: "model", value: "claude-sonnet-4-5-20250929"},
		{key: maxNestingKey, value: 99},
		{key: "tools", value: []any{"WebSearch"}},
		{key: "disallowedTools", value: []any{"Bash"}},
		{key: "isolation", value: "worktree"},
		{key: "effort", value: "high"},
		{key: "name", value: "impostor"},
		{key: "description", value: "impostor"},
	}
	for _, tt := range reserved {
		t.Run(tt.key, func(t *testing.T) {
			subagent := ir.NewSubagent("demo", "Demo subagent", "Body\n", "sonnet", nil, "", nil, "", map[string]any{tt.key: tt.value}, subagentSource)
			result, err := compile.Compile(subagent, compile.TargetDevin)
			if err != nil {
				t.Fatalf("Compile(subagent, Devin): %v", err)
			}
			if len(result.Artifacts) != 1 {
				t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
			}
			want := "---\nname: demo\ndescription: Demo subagent\nmodel: sonnet\n---\nBody\n"
			if got := string(result.Artifacts[0].Bytes); got != want {
				t.Errorf("artifact bytes =\n%s\nwant\n%s", got, want)
			}
			assertSubagentDiags(t, result.Diagnostics, []wantDiag{{diag.CodeCanonicalFieldOverridesExtra, diag.SeverityWarning}}, "demo")
		})
	}
}

// TestSubagentEmptyModelReservesModelExtra is the reason `model` is reserved
// unconditionally for subagents while the skill compiler keys it on the IR
// field: an absent subagent model is a documented decision (Devin's default
// subagent model), so an extension field must not quietly supply an
// unvalidated model and turn that diagnostic into a lie.
func TestSubagentEmptyModelReservesModelExtra(t *testing.T) {
	subagent := ir.NewSubagent("demo", "Demo subagent", "Body\n", "", nil, "", nil, "", map[string]any{"model": "gpt-5.6-terra"}, subagentSource)
	result, err := compile.Compile(subagent, compile.TargetDevin)
	if err != nil {
		t.Fatalf("Compile(subagent, Devin): %v", err)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
	}
	want := "---\nname: demo\ndescription: Demo subagent\n---\nBody\n"
	if got := string(result.Artifacts[0].Bytes); got != want {
		t.Errorf("artifact bytes =\n%s\nwant\n%s", got, want)
	}
	// The model decision is reported before the extras scan runs, so the
	// order is model-default then canonical-override.
	assertSubagentDiags(t, result.Diagnostics, []wantDiag{
		{diag.CodeSubagentDevinModelDefault, diag.SeverityWarning},
		{diag.CodeCanonicalFieldOverridesExtra, diag.SeverityWarning},
	}, "demo")
}

// TestSubagentToolOverrideIsHonored checks that the documented per-invocation
// override escape hatch reaches the subagent path too, so consumers are not
// blocked on a library release for a new Devin tool.
func TestSubagentToolOverrideIsHonored(t *testing.T) {
	subagent := ir.NewSubagent("demo", "Demo subagent", "Body\n", "sonnet", []string{"WebSearch"}, "", nil, "", nil, subagentSource)
	result, err := compile.Compile(subagent, compile.TargetDevin, compile.WithToolOverride(map[string]string{"WebSearch": "exec"}))
	if err != nil {
		t.Fatalf("Compile(subagent, Devin): %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v, want none", result.Diagnostics)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
	}
	want := "---\nname: demo\ndescription: Demo subagent\nmodel: sonnet\nallowed-tools:\n  - exec\n---\nBody\n"
	if got := string(result.Artifacts[0].Bytes); got != want {
		t.Errorf("artifact bytes =\n%s\nwant\n%s", got, want)
	}
}

// TestSubagentModelOverrideIsHonored covers the same escape hatch for models.
func TestSubagentModelOverrideIsHonored(t *testing.T) {
	subagent := ir.NewSubagent("demo", "Demo subagent", "Body\n", "swe-next", nil, "", nil, "", nil, subagentSource)
	result, err := compile.Compile(subagent, compile.TargetDevin, compile.WithModelOverride(map[string]string{"swe-next": "swe"}))
	if err != nil {
		t.Fatalf("Compile(subagent, Devin): %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v, want none", result.Diagnostics)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
	}
	want := "---\nname: demo\ndescription: Demo subagent\nmodel: swe\n---\nBody\n"
	if got := string(result.Artifacts[0].Bytes); got != want {
		t.Errorf("artifact bytes =\n%s\nwant\n%s", got, want)
	}
}

// TestSubagentArtifactShapeSupportsFileSymlink expresses the current-version
// symlink requirement at the layer that can be tested here.
//
// Plan, target contract: "Skills use directory symlinks and flat subagents use
// file symlinks. Both shapes are release requirements", and acceptance criterion
// 15: "Current-version probes pass for project/user skill-directory and
// flat-subagent symlinks; failure blocks release."
//
// Materialization is D10 and the live 3000.3 probe is D12, so this test pins the
// COMPILE-SIDE precondition those packages depend on: the subagent artifact is
// exactly one path, it is a single file directly inside the canonical agents
// directory, it has no intermediate directory component, and it carries the
// documented .md suffix. A regression that emitted agents/<name>/AGENT.md or a
// nested path would silently turn the release-gated file symlink into a
// directory symlink, so it must fail here rather than in D10.
func TestSubagentArtifactShapeSupportsFileSymlink(t *testing.T) {
	result, err := compile.Compile(newSubagent(), compile.TargetDevin)
	if err != nil {
		t.Fatalf("Compile(subagent, Devin): %v", err)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want exactly 1 file to symlink", len(result.Artifacts))
	}
	got := result.Artifacts[0].Path
	if got != "agents/demo.md" {
		t.Fatalf("artifact path = %q, want %q", got, "agents/demo.md")
	}
	segments := strings.Split(got, "/")
	if len(segments) != 2 {
		t.Fatalf("artifact path %q has %d segments, want 2 (a flat file in the agents directory)", got, len(segments))
	}
	if segments[0] != subagentsDir {
		t.Errorf("artifact directory = %q, want %q", segments[0], subagentsDir)
	}
	if !strings.HasSuffix(segments[1], subagentSuffix) {
		t.Errorf("artifact file %q does not end in %q", segments[1], subagentSuffix)
	}
	// Devin also accepts agents/<name>/AGENT.md, but the plan pins the flat
	// form as canonical compiler output because that is the shape D10
	// materializes as a file symlink.
	if strings.Contains(got, "AGENT.md") {
		t.Errorf("artifact path %q uses the nested directory form; the canonical Devin output is flat", got)
	}
}

// TestSubagentAndSkillPathsCannotCollide documents why the flat subagent path
// needs no cross-kind collision guard of its own: subagents live under
// agents/ while skills and converted commands live under skills/, so one name
// can be used by both kinds without either overwriting the other.
func TestSubagentAndSkillPathsCannotCollide(t *testing.T) {
	subagent, err := compile.Compile(ir.NewSubagent("review", "Subagent", "Body\n", "sonnet", nil, "", nil, "", nil, subagentSource), compile.TargetDevin)
	if err != nil {
		t.Fatalf("Compile(subagent, Devin): %v", err)
	}
	skill, err := compile.Compile(ir.NewSkill("review", "Skill", "Body\n", nil, nil, testSource), compile.TargetDevin)
	if err != nil {
		t.Fatalf("Compile(skill, Devin): %v", err)
	}
	if len(subagent.Artifacts) != 1 || len(skill.Artifacts) != 1 {
		t.Fatalf("subagent artifacts = %d, skill artifacts = %d, want 1 each", len(subagent.Artifacts), len(skill.Artifacts))
	}
	if subagent.Artifacts[0].Path == skill.Artifacts[0].Path {
		t.Fatalf("subagent and skill share path %q", subagent.Artifacts[0].Path)
	}
}

// TestSubagentNestedNameIsRejectedAtTheFlatLayoutBoundary covers the plan's
// flat-layout requirement: "if the IR expresses nesting that the flat layout
// cannot represent" the compiler must not silently flatten a collision.
//
// ir.ValidateName currently rejects separators, so compile.Compile never reaches
// this branch — the first subtest proves exactly that, so the guard's status as
// defence in depth is recorded rather than assumed. The remaining subtests drive
// translateSubagent directly, which is the only layer that turns a name into a
// path and therefore the layer that must fail closed if the name grammar is ever
// relaxed.
func TestSubagentNestedNameIsRejectedAtTheFlatLayoutBoundary(t *testing.T) {
	t.Run("the public compile boundary rejects a nesting name outright", func(t *testing.T) {
		nested := ir.NewSubagent("demo", "Demo subagent", "Body\n", "sonnet", nil, "", nil, "", nil, subagentSource)
		nested.SetName("team/reviewer")
		if _, err := compile.Compile(nested, compile.TargetDevin); err == nil {
			t.Fatal("Compile returned nil error for a nesting subagent name")
		}
	})

	names := []string{"team/reviewer", `team\reviewer`, "team/sub/reviewer", ".", ".."}
	for _, name := range names {
		t.Run("translator rejects "+name, func(t *testing.T) {
			nested := ir.NewSubagent("demo", "Demo subagent", "Body\n", "sonnet", nil, "", nil, "", nil, subagentSource)
			nested.SetName(name)
			artifacts, diagnostics, err := translateSubagent(nested, compile.Options{})
			if err != nil {
				t.Fatalf("translateSubagent: %v", err)
			}
			if len(artifacts) != 0 {
				t.Fatalf("artifacts = %d, want 0", len(artifacts))
			}
			assertSubagentDiags(t, diagnostics, []wantDiag{{diag.CodeSubagentDevinNestedNameUnsupported, diag.SeverityError}}, name)
		})
	}

	t.Run("a flat name with hyphens and underscores is accepted", func(t *testing.T) {
		flat := ir.NewSubagent("team_code-reviewer", "Demo subagent", "Body\n", "sonnet", nil, "", nil, "", nil, subagentSource)
		result, err := compile.Compile(flat, compile.TargetDevin)
		if err != nil {
			t.Fatalf("Compile(subagent, Devin): %v", err)
		}
		if len(result.Artifacts) != 1 {
			t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
		}
		if got := result.Artifacts[0].Path; got != "agents/team_code-reviewer.md" {
			t.Errorf("artifact path = %q, want %q", got, "agents/team_code-reviewer.md")
		}
	})
}

func intPtr(v int) *int { return &v }
