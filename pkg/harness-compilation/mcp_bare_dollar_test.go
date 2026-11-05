package harnesscompile

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func canonicalBareDollarServer() *ir.McpServer {
	return ir.NewMcpServerWithOptions("fixture", "", nil, nil, nil, ir.SourceRef{}, ir.WithMcpURL("https://example.invalid/mcp"), ir.WithMcpHeaders(map[string]string{"X-Literal": "$NOT_A_REF"}))
}

func mcpArtifactBytes(artifacts []Artifact) string {
	var out strings.Builder
	for _, a := range artifacts {
		out.Write(a.Bytes)
	}
	return out.String()
}

// spec: harness-compilation/windsurf-keeps-a-bare-dollar-name-literal
func TestWindsurfKeepsBareDollarNameLiteral(t *testing.T) {
	t.Setenv("NOT_A_REF", "must-not-read-host-value")
	t.Setenv("SYNTHETIC_KEY", "must-not-read-host-value")
	canonical, err := Compile(canonicalBareDollarServer(), TargetWindsurf)
	if err != nil {
		t.Fatal(err)
	}
	codex, err := Convert(codexLiteralFixture("$SYNTHETIC_KEY"), HarnessCodex, HarnessWindsurf)
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		result *Result
		value  string
	}{"canonical": {canonical, "$NOT_A_REF"}, "codex": {codex, "$SYNTHETIC_KEY"}} {
		out := mcpArtifactBytes(tc.result.Artifacts)
		if tc.result.HasErrors() || !strings.Contains(out, `"`+tc.value+`"`) || strings.Contains(out, "${env:") || strings.Contains(out, "must-not-read-host-value") {
			t.Fatalf("%s: bare dollar literal changed on Windsurf: %s %+v", name, out, tc.result.Diagnostics)
		}
	}
}

// spec: harness-compilation/gemini-bare-references-become-canonical-references
func TestGeminiBareReferencesBecomeCanonicalReferences(t *testing.T) {
	src := fstest.MapFS{".gemini/settings.json": {Data: []byte(`{"mcpServers":{"fixture":{"command":"server","env":{"TOKEN":"$API_TOKEN"}}}}`)}}
	for target, want := range map[Target]string{
		TargetWindsurf: `"${env:API_TOKEN}"`,
		TargetClaude:   `"${API_TOKEN}"`,
		TargetGemini:   `"${API_TOKEN}"`,
	} {
		result, err := Convert(src, HarnessGemini, target)
		if err != nil {
			t.Fatal(err)
		}
		if out := mcpArtifactBytes(result.Artifacts); result.HasErrors() || !strings.Contains(out, want) {
			t.Fatalf("%s: Gemini reference not kept, want %s in %s %+v", target, want, out, result.Diagnostics)
		}
	}
}

// spec: harness-compilation/gemini-refuses-a-literal-it-would-expand
func TestGeminiRefusesLiteralItWouldExpand(t *testing.T) {
	canonical, err := Compile(canonicalBareDollarServer(), TargetGemini)
	if err != nil {
		t.Fatal(err)
	}
	codex, err := Convert(codexLiteralFixture("$SYNTHETIC_KEY"), HarnessCodex, HarnessGemini)
	if err != nil {
		t.Fatal(err)
	}
	for name, result := range map[string]*Result{"canonical": canonical, "codex": codex} {
		if out := mcpArtifactBytes(result.Artifacts); strings.Contains(out, "mcpServers") || !diag.HasCode(result.Diagnostics, diag.CodeMcpGeminiInterpolationUnrepresentable) {
			t.Fatalf("%s: Gemini accepted a literal it would expand: %s %+v", name, out, result.Diagnostics)
		}
	}
	safe := ir.NewMcpServerWithOptions("fixture", "", nil, nil, nil, ir.SourceRef{}, ir.WithMcpURL("https://example.invalid/mcp"), ir.WithMcpHeaders(map[string]string{"X-Reference": "${API_TOKEN}", "X-Literal": "ordinary"}))
	control, err := Compile(safe, TargetGemini)
	if err != nil {
		t.Fatal(err)
	}
	if out := mcpArtifactBytes(control.Artifacts); control.HasErrors() || !strings.Contains(out, "mcpServers") || !strings.Contains(out, `"${API_TOKEN}"`) {
		t.Fatalf("Gemini refused a server without a bare dollar name: %s %+v", out, control.Diagnostics)
	}
}
