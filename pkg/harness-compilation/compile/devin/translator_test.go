package devin

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestCompilerRegistration(t *testing.T) {
	compiler, ok := compile.Lookup(compile.TargetDevin)
	if !ok {
		t.Fatal("Devin compiler is not registered")
	}
	if compiler.Target() != compile.TargetDevin {
		t.Errorf("compiler target = %q, want %q", compiler.Target(), compile.TargetDevin)
	}
}

// TestCanonicalPathConventions locks the canonical Devin 3000.3 path
// conventions this package owns (see docs/devin-cli-harness-spec-3000.3.md,
// "Canonical roots").
func TestCanonicalPathConventions(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"skills dir", skillsDir, "skills"},
		{"skill file", skillFile, "SKILL.md"},
		{"subagents dir", subagentsDir, "agents"},
		{"subagent suffix", subagentSuffix, ".md"},
		{"project hooks file", projectHooks, "hooks.v1.json"},
		{"user config file", userConfig, "config.json"},
		{"mcp config file", mcpConfig, "mcp_config.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestCompilerEmitsEverySelectableKind is the replacement for the deleted
// TestCompilerDefersRemainingResourceKinds. D7 landed the last deferred kind
// (MCP servers) and removed the transient `compile.devin.resource-pending`
// scaffold entirely, so NO marketplace-selectable kind may return zero
// artifacts on its happy path any more.
func TestCompilerEmitsEverySelectableKind(t *testing.T) {
	source := ir.SourceRef{File: "resources/demo", Line: 1}
	tests := []struct {
		name     string
		resource ir.Resource
		wantPath string
	}{
		{"skill", ir.NewSkill("demo", "Demo", "Body\n", nil, nil, source), "skills/demo/SKILL.md"},
		{"command", ir.NewCommand("demo-cmd", "Demo", "Body\n", nil, source), "skills/demo-cmd/SKILL.md"},
		{"subagent", ir.NewSubagent("demo-agent", "Demo", "Body\n", "sonnet", nil, "", nil, "", nil, source), "agents/demo-agent.md"},
		{"hook", ir.NewHook("demo-hook", "PreToolUse", "check.sh", "command", nil, source), "hooks.v1.json"},
		{"mcpserver", ir.NewMcpServer("demo", "node", []string{"server.js"}, nil, nil, source), "mcp_config.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compile.Compile(tt.resource, compile.TargetDevin)
			if err != nil {
				t.Fatalf("Compile(%s, Devin): %v", tt.name, err)
			}
			if len(result.Artifacts) != 1 {
				t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
			}
			if got := result.Artifacts[0].Path; got != tt.wantPath {
				t.Errorf("artifact path = %q, want %q", got, tt.wantPath)
			}
		})
	}
}

// TestCompilerScaffoldCodeIsGone proves the transient
// `compile.devin.resource-pending` code no longer exists in the catalog. The
// constant is deleted, so the assertion uses the literal string a stale
// consumer would still be keying off.
func TestCompilerScaffoldCodeIsGone(t *testing.T) {
	for _, code := range diag.AllCodes() {
		if code == "compile.devin.resource-pending" {
			t.Fatal("compile.devin.resource-pending is still registered; D7 removes it")
		}
	}
}

// TestCompilerRejectsUnknownKind pins the replacement for the deleted catch-all
// warning branch: an unrecognized kind is a programming error and must fail the
// call instead of being absorbed as a lossy-but-successful compile.
func TestCompilerRejectsUnknownKind(t *testing.T) {
	collection := ir.NewCollection("demo-collection", "1.0.0", "Demo", nil, nil, nil, ir.SourceRef{File: "collections/demo.yaml", Line: 1})
	artifacts, diagnostics, err := Compiler{}.Translate(collection)
	if err == nil {
		t.Fatal("Translate(collection) returned nil error, want unsupported-kind error")
	}
	if artifacts != nil || diagnostics != nil {
		t.Errorf("artifacts = %#v, diagnostics = %#v, want nil, nil", artifacts, diagnostics)
	}
}

// TestCompilerDropsRulesAsAnOutOfScopeWarning pins the outcome of bead
// agent-marketplace-5exj.22: a rule still produces no Devin artifact, but the
// diagnostic is a WARNING describing a scope decision, not an ERROR claiming a
// Devin capability limit. The D12 probe
// (internal/devinprobe.TestProbeRulesDiscoveryUnderDevinRoots) falsified the
// capability claim on 3000.3.27, so the message must not restate it.
func TestCompilerDropsRulesAsAnOutOfScopeWarning(t *testing.T) {
	tests := []struct {
		name string
		rule *ir.Rule
	}{
		{
			name: "bare rule",
			rule: ir.NewRule("conventions", "", "Use interfaces", nil, false, nil,
				ir.SourceRef{File: "rules/conventions.mdc", Line: 1}),
		},
		{
			name: "rule with description, globs and alwaysApply",
			rule: ir.NewRule("typescript", "TypeScript conventions", "# Body\n",
				[]string{"*.ts"}, true, nil, ir.SourceRef{File: ".cursor/rules/typescript.mdc", Line: 1}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compile.Compile(tt.rule, compile.TargetDevin)
			if err != nil {
				t.Fatalf("Compile(rule, Devin): %v", err)
			}
			if len(result.Artifacts) != 0 {
				t.Errorf("artifacts = %d, want 0: rules remain out of scope for Devin", len(result.Artifacts))
			}
			if len(result.Diagnostics) != 1 {
				t.Fatalf("diagnostics = %d, want 1", len(result.Diagnostics))
			}
			d := result.Diagnostics[0]
			if d.Code != diag.CodeRuleDevinUnsupported {
				t.Errorf("code = %q, want %q", d.Code, diag.CodeRuleDevinUnsupported)
			}
			// The severity has to agree with the catalog, and the catalog has
			// to agree with the Claude and Gemini rule codes: the same class of
			// loss on three targets cannot carry three different severities.
			doc, ok := diag.DocForCode(diag.CodeRuleDevinUnsupported)
			if !ok {
				t.Fatalf("%s is not in the diagnostic catalog", diag.CodeRuleDevinUnsupported)
			}
			if d.Severity != doc.Severity {
				t.Errorf("severity = %v, want the catalogued %v", d.Severity, doc.Severity)
			}
			if d.Severity != diag.SeverityWarning {
				t.Errorf("severity = %v, want %v (a scope decision must not fail the run)", d.Severity, diag.SeverityWarning)
			}
			for _, sibling := range []string{diag.CodeRuleClaudeNoPrimitive, diag.CodeRuleGeminiNoPrimitive} {
				siblingDoc, ok := diag.DocForCode(sibling)
				if !ok {
					t.Fatalf("%s is not in the diagnostic catalog", sibling)
				}
				if siblingDoc.Severity != d.Severity {
					t.Errorf("severity = %v but %s is %v; the rule-dropped codes must agree", d.Severity, sibling, siblingDoc.Severity)
				}
			}
			// The falsified rationale must not come back in any form.
			for _, phrase := range []string{"no rules directory primitive", "has no rules"} {
				if strings.Contains(strings.ToLower(d.Message+" "+d.Hint), phrase) {
					t.Errorf("diagnostic restates the falsified capability claim %q: message=%q hint=%q", phrase, d.Message, d.Hint)
				}
			}
			if !strings.Contains(strings.ToLower(d.Message), "out of scope") {
				t.Errorf("message = %q, want it to name the scope decision", d.Message)
			}
			if d.Target != string(compile.TargetDevin) {
				t.Errorf("target = %q, want %q", d.Target, compile.TargetDevin)
			}
			if d.Resource != tt.rule.Name() {
				t.Errorf("resource = %q, want %q", d.Resource, tt.rule.Name())
			}
			if d.Source.File != tt.rule.Source.File {
				t.Errorf("source file = %q, want %q", d.Source.File, tt.rule.Source.File)
			}
			// A rule body is user content and must never reach a diagnostic.
			if strings.Contains(d.Message+d.Hint, tt.rule.Body) {
				t.Errorf("diagnostic leaked the rule body: message=%q hint=%q", d.Message, d.Hint)
			}
		})
	}
}

// TestCompilerRejectsInvalidResources covers the ir.ValidateResource error path:
// the compiler must return an error and no diagnostics rather than emitting a
// diagnostic for an unusable resource.
func TestCompilerRejectsInvalidResources(t *testing.T) {
	tests := []struct {
		name     string
		resource ir.Resource
	}{
		{"nil resource", nil},
		{"typed nil skill", (*ir.Skill)(nil)},
		{"empty name", ir.NewSkill("", "", "body", nil, nil, ir.SourceRef{})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			artifacts, diagnostics, err := Compiler{}.Translate(tt.resource)
			if err == nil {
				t.Fatal("Translate returned nil error, want validation error")
			}
			if artifacts != nil {
				t.Errorf("artifacts = %#v, want nil", artifacts)
			}
			if diagnostics != nil {
				t.Errorf("diagnostics = %#v, want nil", diagnostics)
			}
		})
	}
}
