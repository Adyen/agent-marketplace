package opencode

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/yamlfm"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: harness-opencode/native-fields-survive-a-supported-round-trip
func TestNativeResourceFormats(t *testing.T) {
	for _, tc := range []struct {
		resource   ir.Resource
		path, want string
	}{
		{ir.NewSkill("review", "Review code", "Review the diff.\n", nil, nil, ir.SourceRef{}), "skills/review/SKILL.md", "---\nname: review\ndescription: Review code\n---\nReview the diff.\n"},
		{ir.NewCommand("check", "Check changes", "Check $ARGUMENTS and $1.\n", map[string]any{"agent": "reviewer", "model": "provider/model", "subtask": true}, ir.SourceRef{}), "commands/check.md", "---\ndescription: Check changes\nagent: reviewer\nmodel: provider/model\nsubtask: true\n---\nCheck $ARGUMENTS and $1.\n"},
		{ir.NewSubagent("reviewer", "Review code", "Review only.\n", "provider/model", nil, "", nil, "", nil, ir.SourceRef{}), "agents/reviewer.md", "---\ndescription: Review code\nmode: subagent\nmodel: provider/model\n---\nReview only.\n"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			artifacts, diagnostics, err := (Compiler{}).Translate(tc.resource)
			if err != nil || len(diagnostics) != 0 || len(artifacts) != 1 {
				t.Fatalf("translate: artifacts=%d diagnostics=%v err=%v", len(artifacts), diagnostics, err)
			}
			if artifacts[0].Path != tc.path || string(artifacts[0].Bytes) != tc.want {
				t.Fatalf("artifact: %#v", artifacts[0])
			}
		})
	}
}

// spec: harness-opencode/scalar-metadata-cannot-add-execution-fields
func TestScalarMetadata(t *testing.T) {
	description := "Review\u2028permission: allow\u0085tools: true\nmodel: injected"
	a, _, err := (Compiler{}).Translate(ir.NewCommand("check", description, "Body\n", nil, ir.SourceRef{}))
	if err != nil {
		t.Fatal(err)
	}
	fm, body, err := yamlfm.ParseYAMLFrontmatter(strings.NewReader(string(a[0].Bytes)))
	if err != nil || len(fm) != 1 || fm["description"] != description || body != "Body\n" {
		t.Fatalf("metadata=%v body=%q err=%v", fm, body, err)
	}
}

// spec: harness-opencode/a-restricted-skill-is-not-emitted-as-an-unrestricted-skill
func TestRestrictedSkill(t *testing.T) {
	for _, option := range []ir.SkillOption{ir.WithSkillAllowedTools([]string{}), ir.WithSkillPermissions(ir.Permissions{})} {
		a, d, err := (Compiler{}).Translate(ir.NewSkillWithOptions("review", "Review", "Body", nil, nil, ir.SourceRef{}, option))
		if err != nil || len(a) != 0 || len(d) != 1 || d[0].Code != "skill.opencode.security-restriction-unsupported" || d[0].Severity != diag.SeverityError {
			t.Fatalf("restricted skill: %v %v %v", a, d, err)
		}
	}
	a, d, err := (Compiler{}).Translate(ir.NewSkill("review", "Review", "Body", nil, nil, ir.SourceRef{}))
	if err != nil || len(a) != 1 || len(d) != 0 {
		t.Fatalf("unrestricted control: %v %v %v", a, d, err)
	}
}

func TestMCPFormats(t *testing.T) {
	for _, tc := range []struct {
		resource *ir.McpServer
		want     string
	}{
		{ir.NewMcpServer("local", "server", []string{"--stdio", "${TOKEN}"}, map[string]string{"TOKEN": "${TOKEN}"}, nil, ir.SourceRef{}), `{"type":"local","command":["server","--stdio","{env:TOKEN}"],"environment":{"TOKEN":"{env:TOKEN}"}}`},
		{ir.NewMcpServerWithOptions("remote", "", nil, nil, nil, ir.SourceRef{}, ir.WithMcpURL("https://example.invalid/mcp"), ir.WithMcpHeaders(map[string]string{"Authorization": "Bearer ${TOKEN}"}), ir.WithMcpDisabled(true)), `{"type":"remote","url":"https://example.invalid/mcp","headers":{"Authorization":"Bearer {env:TOKEN}"},"enabled":false}`},
	} {
		a, d, err := (Compiler{}).Translate(tc.resource)
		if err != nil || len(a) != 1 || len(d) != 0 {
			t.Fatalf("MCP translate: %v %v", d, err)
		}
		if a[0].Path != "opencode.json" || a[0].Mode != 0600 || !a[0].Fragment {
			t.Fatalf("MCP artifact: %#v", a[0])
		}
		var got map[string]map[string]any
		if err := json.Unmarshal(a[0].Bytes, &got); err != nil {
			t.Fatal(err)
		}
		var want any
		if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
			t.Fatal(err)
		}
		gotJSON, _ := json.Marshal(got["mcp"][tc.resource.Name()])
		wantJSON, _ := json.Marshal(want)
		if string(gotJSON) != string(wantJSON) {
			t.Fatalf("MCP got %s want %s", gotJSON, wantJSON)
		}
	}
}

// spec: harness-opencode/literal-and-reference-payloads-cannot-inject-configuration
func TestMCPReferencesRemainData(t *testing.T) {
	t.Setenv("TOKEN", "SYNTHETIC-HOST-VALUE-MUST-NOT-EXPAND")
	for _, payload := range []string{"${TOKEN}", "${env:TOKEN}", "${file:synthetic.txt}", "quotes\"\nfield: value"} {
		server := ir.NewMcpServer("docs", "server", nil, map[string]string{"TOKEN": payload}, nil, ir.SourceRef{})
		artifacts, diagnostics, err := (Compiler{}).Translate(server)
		if err != nil || len(artifacts) != 1 || diag.HasErrors(diagnostics) {
			t.Fatalf("reference refused: %v %v", err, diagnostics)
		}
		if strings.Contains(string(artifacts[0].Bytes), "SYNTHETIC-HOST-VALUE") {
			t.Fatal("host reference expanded")
		}
		var output map[string]map[string]map[string]any
		if err := json.Unmarshal(artifacts[0].Bytes, &output); err != nil || len(output["mcp"]["docs"]) != 3 {
			t.Fatal("payload changed MCP structure")
		}
	}
	server := ir.NewMcpServer("docs", "server", nil, map[string]string{"TOKEN": "literal {env:TOKEN}"}, nil, ir.SourceRef{})
	artifacts, diagnostics, err := (Compiler{}).Translate(server)
	if err != nil || len(artifacts) != 0 || !diag.HasCode(diagnostics, diag.CodeMcpOpenCodeUnrepresentable) {
		t.Fatal("unrepresentable literal emitted as a reference")
	}
}

// spec: harness-opencode/rules-preserve-shared-instructions
func TestRulesEmitNoInstructionArtifact(t *testing.T) {
	a, d, err := (Compiler{}).Translate(ir.NewRule("rules", "", "Instructions", nil, false, nil, ir.SourceRef{}))
	if err != nil || len(a) != 0 || len(d) != 1 || d[0].Code != "rule.opencode.unsupported" || d[0].Severity != diag.SeverityWarning {
		t.Fatalf("rule: %v %v %v", a, d, err)
	}
}

func TestCommandModelMappingAndOverride(t *testing.T) {
	command := ir.NewCommand("check", "", "Body", map[string]any{"model": "sonnet"}, ir.SourceRef{})
	for _, tc := range []struct {
		override map[string]string
		want     string
	}{
		{nil, "anthropic/claude-sonnet-4-5-20250929"},
		{map[string]string{"sonnet": "provider/custom"}, "provider/custom"},
	} {
		result, err := compile.Compile(command, "opencode", compile.WithModelOverride(tc.override))
		if err != nil || len(result.Artifacts) != 1 || diag.HasErrors(result.Diagnostics) {
			t.Fatalf("command model: %v %#v", err, result)
		}
		fm, _, err := yamlfm.ParseYAMLFrontmatter(strings.NewReader(string(result.Artifacts[0].Bytes)))
		if err != nil || fm["model"] != tc.want {
			t.Fatalf("model=%v err=%v", fm["model"], err)
		}
	}
}

func TestAgentDeniedToolWinsOverlap(t *testing.T) {
	agent := ir.NewSubagent("reviewer", "", "Body", "", []string{"Read", "Bash", "Bash"}, "", []string{"Bash"}, "", nil, ir.SourceRef{})
	artifacts, diagnostics, err := (Compiler{}).Translate(agent)
	if err != nil || len(artifacts) != 1 || diag.HasErrors(diagnostics) {
		t.Fatalf("agent overlap: %v %v", err, diagnostics)
	}
	fm, _, err := yamlfm.ParseYAMLFrontmatter(strings.NewReader(string(artifacts[0].Bytes)))
	if err != nil {
		t.Fatal(err)
	}
	permission := fm["permission"].(map[string]any)
	if len(permission) != 3 || permission["bash"] != "deny" || permission["read"] != "allow" || permission["*"] != "deny" {
		t.Fatalf("policy: %#v", permission)
	}
}
