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
		{name: "valid single-kind declaration passes", kinds: []ir.ResourceKind{ir.KindSkill}, compiler: true},
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
			if len(tt.wantErrFor) == 0 {
				if err != nil {
					t.Fatalf("ValidateDefinition returned unexpected error: %v", err)
				}
				return
			}
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

// spec: harness-compilation/declared-kind-without-an-implementation-fails-validation
func TestMissingKindRegistrations(t *testing.T) {
	tests := []struct {
		name          string
		acceptedKinds []ir.ResourceKind
		required      []ir.ResourceKind
		registered    bool
		wantErrors    []string
		wantCalls     int
	}{
		{
			name:          "declared hook needs a catalog",
			acceptedKinds: []ir.ResourceKind{ir.KindHook},
			required:      []ir.ResourceKind{ir.KindHook},
			wantErrors:    []string{`target "partial" kind "hook" requires a hook catalog but none is registered`},
			wantCalls:     1,
		},
		{
			name:          "undeclared hook needs no catalog",
			acceptedKinds: []ir.ResourceKind{ir.KindSkill},
			required:      []ir.ResourceKind{ir.KindHook},
		},
		{
			name:          "nil requires every accepted kind",
			acceptedKinds: []ir.ResourceKind{ir.KindSkill, ir.KindHook},
			wantErrors: []string{
				`target "partial" kind "skill" requires a hook catalog but none is registered`,
				`target "partial" kind "hook" requires a hook catalog but none is registered`,
			},
			wantCalls: 1,
		},
		{
			name:          "registered catalog passes",
			acceptedKinds: []ir.ResourceKind{ir.KindHook},
			required:      []ir.ResourceKind{ir.KindHook},
			registered:    true,
			wantCalls:     1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			errs := MissingKindRegistrations(
				[]Definition{{Target: Target("partial"), AcceptedKinds: tt.acceptedKinds}},
				tt.required,
				"a hook catalog",
				func(Target) bool {
					calls++
					return tt.registered
				},
			)
			if calls != tt.wantCalls {
				t.Errorf("registration checks = %d, want %d", calls, tt.wantCalls)
			}
			if len(errs) != len(tt.wantErrors) {
				t.Fatalf("MissingKindRegistrations returned %d errors, want %d: %v", len(errs), len(tt.wantErrors), errs)
			}
			for i, want := range tt.wantErrors {
				if errs[i].Error() != want {
					t.Errorf("error %d = %q, want %q", i, errs[i], want)
				}
			}
		})
	}
}
