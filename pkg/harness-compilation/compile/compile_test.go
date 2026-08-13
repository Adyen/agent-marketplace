package compile

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// mockCompiler is a test Compiler that returns fixed artifacts.
type mockCompiler struct {
	target Target
}

func (m *mockCompiler) Target() Target { return m.target }
func (m *mockCompiler) Translate(r ir.Resource) ([]artifact.Artifact, []diag.Diagnostic, error) {
	return []artifact.Artifact{
		{Path: r.Name() + ".out", Bytes: []byte("mock")},
	}, nil, nil
}

// mockResource is a minimal ir.Resource for testing.
type mockResource struct {
	kind ir.ResourceKind
	name string
}

func (m *mockResource) Kind() ir.ResourceKind { return m.kind }
func (m *mockResource) Name() string          { return m.name }

func TestRegisterAndCompile(t *testing.T) {
	// Use a separate registry for this test to avoid polluting the global one.
	orig := registry
	registry = map[Target]Compiler{}
	defer func() { registry = orig }()

	c := &mockCompiler{target: TargetClaude}
	Register(c)

	r := &mockResource{kind: ir.KindSkill, name: "test-skill"}
	res, err := Compile(r, TargetClaude)
	if err != nil {
		t.Fatalf("Compile() error: %v", err)
	}
	if len(res.Artifacts) != 1 {
		t.Fatalf("Compile() artifacts = %d, want 1", len(res.Artifacts))
	}
	if res.Artifacts[0].Path != "test-skill.out" {
		t.Errorf("Compile() artifact path = %q, want %q", res.Artifacts[0].Path, "test-skill.out")
	}
}

func TestRegister_DuplicatePanics(t *testing.T) {
	orig := registry
	registry = map[Target]Compiler{}
	defer func() { registry = orig }()

	c1 := &mockCompiler{target: TargetFactory}
	c2 := &mockCompiler{target: TargetFactory}
	Register(c1)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Register(duplicate) did not panic")
		}
	}()
	Register(c2)
}

func TestCompile_UnregisteredTarget(t *testing.T) {
	orig := registry
	registry = map[Target]Compiler{}
	defer func() { registry = orig }()

	r := &mockResource{kind: ir.KindSkill, name: "test"}
	_, err := Compile(r, TargetGemini)
	if err == nil {
		t.Fatal("Compile(unregistered) should return an error")
	}
}

func TestCompile_WithOptions(t *testing.T) {
	orig := registry
	registry = map[Target]Compiler{}
	defer func() { registry = orig }()

	c := &mockCompiler{target: TargetClaude}
	Register(c)

	r := &mockResource{kind: ir.KindCommand, name: "cmd"}
	res, err := Compile(r, TargetClaude,
		WithModelOverride(map[string]string{"sonnet": "custom-model"}),
		WithToolOverride(map[string]string{"Write": "CustomWrite"}),
	)
	if err != nil {
		t.Fatalf("Compile() with options error: %v", err)
	}
	if len(res.Artifacts) != 1 {
		t.Fatalf("Compile() with options artifacts = %d, want 1", len(res.Artifacts))
	}
}

func TestCompilerInterface(t *testing.T) {
	// Verify the Compiler interface is satisfied by mockCompiler.
	var _ Compiler = &mockCompiler{}
}

func TestMcpCredentialDiagnostics_StrictInterpolation(t *testing.T) {
	tests := []struct {
		name       string
		value      string
		wantSecret bool
	}{
		{name: "environment token", value: "${API_TOKEN}", wantSecret: false},
		{name: "embedded environment token", value: "Bearer ${API_TOKEN}", wantSecret: false},
		{name: "env interpolation", value: "${env:API_TOKEN}", wantSecret: false},
		{name: "file interpolation", value: "${file:/run/secrets/token}", wantSecret: false},
		{name: "bare dollar literal", value: "$literal", wantSecret: true},
		{name: "malformed interpolation", value: "${API TOKEN}", wantSecret: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := ir.NewMcpServer("credentials", "server", nil, map[string]string{"API_TOKEN": tt.value}, nil, ir.SourceRef{})
			diags := McpCredentialDiagnostics(server, TargetClaude)
			if got := len(diags) > 0; got != tt.wantSecret {
				t.Fatalf("diagnostics = %#v, want literal=%v", diags, tt.wantSecret)
			}
			if len(diags) == 0 {
				return
			}
			if diags[0].Code != diag.CodeMcpLiteralCredential {
				t.Fatalf("code = %q, want %q", diags[0].Code, diag.CodeMcpLiteralCredential)
			}
			if strings.Contains(diags[0].Message, tt.value) || strings.Contains(diags[0].Hint, tt.value) {
				t.Fatal("credential value leaked into the diagnostic")
			}
		})
	}
}
