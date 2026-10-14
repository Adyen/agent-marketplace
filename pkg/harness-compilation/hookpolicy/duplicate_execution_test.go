package hookpolicy_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/hookpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func TestDuplicateExecutionRisks(t *testing.T) {
	cursorLoadsClaude := hookpolicy.HookLoadRisk{Loader: target.TargetCursor, Loaded: target.TargetClaude}
	devinLoadsClaude := hookpolicy.HookLoadRisk{Loader: target.TargetDevin, Loaded: target.TargetClaude}
	tests := []struct {
		name     string
		selected []target.Target
		want     []hookpolicy.HookLoadRisk
	}{
		{name: "nil selection"},
		{name: "empty selection", selected: []target.Target{}},
		{name: "single target", selected: []target.Target{target.TargetCursor}},
		{
			name:     "Claude and Cursor",
			selected: []target.Target{target.TargetClaude, target.TargetCursor},
			want:     []hookpolicy.HookLoadRisk{cursorLoadsClaude},
		},
		{
			name:     "reversed input is stable",
			selected: []target.Target{target.TargetCursor, target.TargetClaude},
			want:     []hookpolicy.HookLoadRisk{cursorLoadsClaude},
		},
		{
			name:     "duplicate inputs collapse",
			selected: []target.Target{target.TargetDevin, target.TargetClaude, target.TargetCursor, target.TargetClaude, target.TargetCursor},
			want:     []hookpolicy.HookLoadRisk{cursorLoadsClaude, devinLoadsClaude},
		},
		{
			name:     "all selected loaders sort by direction",
			selected: []target.Target{target.TargetClaude, target.TargetCursor, target.TargetDevin},
			want:     []hookpolicy.HookLoadRisk{cursorLoadsClaude, devinLoadsClaude},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := hookpolicy.DuplicateExecutionRisks(tc.selected)
			if err != nil {
				t.Fatalf("DuplicateExecutionRisks: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("DuplicateExecutionRisks(%v) = %#v, want %#v", tc.selected, got, tc.want)
			}
		})
	}
}

func TestDuplicateExecutionRisksUnknownTarget(t *testing.T) {
	const unknown target.Target = "synthetic-unknown"
	risks, err := hookpolicy.DuplicateExecutionRisks([]target.Target{target.TargetClaude, unknown})
	if err == nil || !strings.Contains(err.Error(), string(unknown)) {
		t.Fatalf("DuplicateExecutionRisks unknown target error = %v, want target name %q", err, unknown)
	}
	if risks != nil {
		t.Fatalf("DuplicateExecutionRisks unknown target risks = %#v, want nil", risks)
	}
}
