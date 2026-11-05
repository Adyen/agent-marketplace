package target

import (
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func windsurfLayoutWithHook(mutate func(*NativeAggregate)) NativeLayout {
	out := nativeLayouts[TargetWindsurf]
	out.Partitions = slices.Clone(out.Partitions)
	for i := range out.Partitions {
		partition := out.Partitions[i]
		partition.Aggregates = slices.Clone(partition.Aggregates)
		for j := range partition.Aggregates {
			if partition.Aggregates[j].Kind == ir.KindHook {
				mutate(&partition.Aggregates[j])
			}
		}
		out.Partitions[i] = partition
	}
	return out
}

func TestWindsurfHookFileFlagsSelectScopes(t *testing.T) {
	hook := windsurfLayoutWithHook(func(*NativeAggregate) {})
	var aggregate NativeAggregate
	for _, partition := range hook.Partitions {
		for _, candidate := range partition.Aggregates {
			if candidate.Kind == ir.KindHook {
				aggregate = candidate
			}
		}
	}
	if aggregate.LegacyMarkedFlatHooks {
		t.Error("Windsurf aggregate must not declare the marked transition for every scope")
	}
	if !aggregate.Project.PreserveLegacyHandwritten || aggregate.User.PreserveLegacyHandwritten {
		t.Errorf("handwritten preservation = project %v user %v, want project only", aggregate.Project.PreserveLegacyHandwritten, aggregate.User.PreserveLegacyHandwritten)
	}
	if aggregate.Project.LegacyMarkedFlatHooks || !aggregate.User.LegacyMarkedFlatHooks {
		t.Errorf("marked transition = project %v user %v, want user only", aggregate.Project.LegacyMarkedFlatHooks, aggregate.User.LegacyMarkedFlatHooks)
	}
	if err := ValidateNativeLayout(TargetWindsurf, hook); err != nil {
		t.Fatalf("declared Windsurf layout is invalid: %v", err)
	}
}

func TestWindsurfHookFileFlagValidation(t *testing.T) {
	for _, tt := range []struct {
		name       string
		mutate     func(*NativeAggregate)
		wantErrHas string
	}{
		{
			name:       "handwritten preservation on the user file",
			mutate:     func(a *NativeAggregate) { a.User.PreserveLegacyHandwritten = true },
			wantErrHas: "preserves legacy handwritten",
		},
		{
			name:       "handwritten preservation without legacy paths",
			mutate:     func(a *NativeAggregate) { a.Project.Legacy = nil },
			wantErrHas: "preserves legacy handwritten",
		},
		{
			name:       "marked transition on a file of an unknown hook shape",
			mutate:     func(a *NativeAggregate) { a.Shape = HookShape("unknown-shape"); a.User.LegacyMarkedFlatHooks = true },
			wantErrHas: "legacy marked flat",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNativeLayout(TargetWindsurf, windsurfLayoutWithHook(tt.mutate))
			if err == nil || !strings.Contains(err.Error(), tt.wantErrHas) {
				t.Fatalf("ValidateNativeLayout error = %v, want one containing %q", err, tt.wantErrHas)
			}
		})
	}
}
