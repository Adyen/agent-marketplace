package devin

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
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

func TestCompilerDirectTranslationGuardsNativeCodexSecurity(t *testing.T) {
	server := ir.NewMcpServerWithOptions("native", "", nil, nil, nil, ir.SourceRef{}, ir.WithMcpURL("https://example.test/mcp"))
	server.Ext.Codex = &ext.Codex{BearerTokenEnvVar: "TOKEN"}

	artifacts, diagnostics, err := (Compiler{}).Translate(server)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("artifacts = %d, want none for dropped native authentication", len(artifacts))
	}
	if !diag.HasErrors(diagnostics) || !diag.HasCode(diagnostics, diag.CodeMcpSecurityRestrictionUnsupported) {
		t.Fatalf("diagnostics = %#v, want %s error", diagnostics, diag.CodeMcpSecurityRestrictionUnsupported)
	}
}

func TestCompilerGuardDoesNotDuplicateNativeCodexWarnings(t *testing.T) {
	agent := ir.NewSubagent("native", "Native", "Review.", "", nil, "", nil, "", nil, ir.SourceRef{})
	agent.Ext.Codex = &ext.Codex{ReasoningEffort: "xhigh"}

	result, err := compile.Compile(agent, compile.TargetDevin)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == diag.CodeSubagentCodexEffortDropped {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%s diagnostics = %d, want 1: %#v", diag.CodeSubagentCodexEffortDropped, count, result.Diagnostics)
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

func TestCompilerEmitsNativeRules(t *testing.T) {
	tests := []struct {
		name     string
		rule     *ir.Rule
		wantDiag string
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
			wantDiag: diag.CodeRuleDevinGlobsDropped,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compile.Compile(tt.rule, compile.TargetDevin)
			if err != nil {
				t.Fatalf("Compile(rule, Devin): %v", err)
			}
			if len(result.Artifacts) != 1 {
				t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
			}
			if result.Artifacts[0].Path != "rules/"+tt.rule.Name()+".md" {
				t.Errorf("path = %q", result.Artifacts[0].Path)
			}
			if !strings.Contains(string(result.Artifacts[0].Bytes), tt.rule.Body) {
				t.Errorf("body = %q, want content %q", result.Artifacts[0].Bytes, tt.rule.Body)
			}
			if tt.wantDiag == "" && len(result.Diagnostics) != 0 {
				t.Fatalf("diagnostics = %v, want none", result.Diagnostics)
			}
			if tt.wantDiag != "" && (len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != tt.wantDiag) {
				t.Fatalf("diagnostics = %v, want %s", result.Diagnostics, tt.wantDiag)
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
		{"typed nil subagent", (*ir.Subagent)(nil)},
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
