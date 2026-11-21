package target

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: harness-compilation/compiled-targets-declare-the-observed-kind-matrix
func TestCompiledAcceptedKindsMatrix(t *testing.T) {
	common := []ir.ResourceKind{
		ir.KindSkill,
		ir.KindCommand,
		ir.KindSubagent,
		ir.KindHook,
		ir.KindMcpServer,
		ir.KindPlugin,
	}
	withRules := append(append([]ir.ResourceKind(nil), common...), ir.KindRule)
	want := map[Target][]ir.ResourceKind{
		TargetFactory:  withRules,
		TargetClaude:   common,
		TargetCursor:   append([]ir.ResourceKind(nil), withRules...),
		TargetWindsurf: append([]ir.ResourceKind(nil), withRules...),
		TargetGemini:   common,
		TargetDevin:    common,
		TargetCodex:    common,
		TargetOpenCode: common,
	}

	for _, definition := range Definitions() {
		expected, ok := want[definition.Target]
		if !ok {
			t.Fatalf("unexpected compiled target %q", definition.Target)
		}
		if !reflect.DeepEqual(definition.AcceptedKinds, expected) {
			t.Errorf("%s accepted kinds = %v, want %v", definition.Target, definition.AcceptedKinds, expected)
		}
		delete(want, definition.Target)
	}
	if len(want) != 0 {
		t.Errorf("compiled target definitions missing: %v", want)
	}
}

// spec: harness-compilation/declared-kind-without-an-implementation-fails-validation
func TestDeclaredKindWithoutAnImplementationFailsValidation(t *testing.T) {
	tests := []struct {
		name       string
		kinds      []ir.ResourceKind
		compiler   bool
		wantErrFor []string
	}{
		{name: "missing compiler", kinds: []ir.ResourceKind{ir.KindHook}, wantErrFor: []string{"partial", "hook"}},
		{name: "empty kinds", compiler: true, wantErrFor: []string{"partial", "accepted kind"}},
		{name: "collection", kinds: []ir.ResourceKind{ir.KindCollection}, compiler: true, wantErrFor: []string{"partial", "collection"}},
		{name: "duplicate kind", kinds: []ir.ResourceKind{ir.KindHook, ir.KindHook}, compiler: true, wantErrFor: []string{"partial", "hook"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDefinition(Definition{
				Target:           Target("partial"),
				PartitionedRoots: true,
				AcceptedKinds:    tt.kinds,
				Compiler:         tt.compiler,
			})
			if err == nil {
				t.Fatal("ValidateDefinition accepted an invalid accepted-kind declaration")
			}
			for _, name := range tt.wantErrFor {
				if !strings.Contains(err.Error(), name) {
					t.Errorf("validation error %q does not name %q", err, name)
				}
			}
		})
	}
}
