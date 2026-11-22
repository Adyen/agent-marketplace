package harnesscompile

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// spec: harness-compilation/declared-kind-without-an-implementation-fails-validation
func TestValidateKindRegistrations(t *testing.T) {
	tests := []struct {
		name               string
		kind               ir.ResourceKind
		compilerRegistered bool
		ingesterRegistered bool
		wantError          bool
	}{
		{name: "missing compiler", kind: ir.KindHook, ingesterRegistered: true, wantError: true},
		{name: "missing ingester", kind: ir.KindHook, compilerRegistered: true, wantError: true},
		{name: "plugin needs no ingester", kind: ir.KindPlugin, compilerRegistered: true},
		{name: "all registrations present", kind: ir.KindSkill, compilerRegistered: true, ingesterRegistered: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			definition := targetmeta.Definition{
				Target:        targetmeta.Target("partial"),
				AcceptedKinds: []ir.ResourceKind{tt.kind},
				Compiler:      true,
				Ingester:      true,
			}
			errs := validateKindRegistrations(
				[]targetmeta.Definition{definition},
				func(target targetmeta.Target) bool {
					return target == definition.Target && tt.compilerRegistered
				},
				func(target targetmeta.Target) bool {
					return target == definition.Target && tt.ingesterRegistered
				},
			)
			if !tt.wantError {
				if len(errs) != 0 {
					t.Fatalf("validateKindRegistrations returned unexpected errors: %v", errs)
				}
				return
			}
			if len(errs) != 1 {
				t.Fatalf("validateKindRegistrations returned %d errors, want 1: %v", len(errs), errs)
			}
			for _, name := range []string{"partial", tt.kind.String()} {
				if !strings.Contains(errs[0].Error(), name) {
					t.Errorf("validation error %q does not name %q", errs[0], name)
				}
			}
		})
	}
}
