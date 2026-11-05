package target

import (
	"path"
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: hook-resources/every-harness-target-receives-hooks-in-its-native-file
func TestHookProjectionMatchesNativeAggregateLayout(t *testing.T) {
	if err := ValidateHookProjections(); err != nil {
		t.Fatalf("validate hook projections: %v", err)
	}
	for _, target := range []Target{
		TargetClaude, TargetFactory, TargetCursor, TargetWindsurf,
		TargetGemini, TargetDevin, TargetCodex,
	} {
		t.Run(string(target), func(t *testing.T) {
			partition, aggregate, ok := nativeAggregate(target, ir.KindHook)
			if !ok {
				t.Fatalf("%s has no native hook aggregate", target)
			}
			projection, ok := LookupHookProjection(target)
			if !ok {
				t.Fatalf("%s has no hook projection", target)
			}
			if projection.Shape != aggregate.Shape || projection.SupportDir != path.Join(partition.Prefix, aggregate.SupportDir) {
				t.Fatalf("%s projection shape/support = %q/%q, want %q/%q", target, projection.Shape, projection.SupportDir, aggregate.Shape, path.Join(partition.Prefix, aggregate.SupportDir))
			}
			for _, machineScope := range []bool{false, true} {
				want, ok := NativeAggregateFileForScope(target, ir.KindHook, machineScope)
				if !ok {
					t.Fatalf("%s has no scope=%t native hook file", target, machineScope)
				}
				got := projection.File(machineScope)
				if got.Path != want.Path || got.Member != want.Member || !reflect.DeepEqual(got.Envelope, want.Envelope) {
					t.Errorf("%s scope=%t projection file = %+v, native layout = %+v", target, machineScope, got, want)
				}
				if len(got.Envelope) > 0 {
					var key string
					var value any
					for key, value = range got.Envelope {
						break
					}
					got.Envelope[key] = "mutated"
					if projection.File(machineScope).Envelope[key] != value {
						t.Errorf("%s scope=%t projection envelope aliases its declaration", target, machineScope)
					}
				}
			}
			definition, ok := Lookup(string(target))
			if !ok || definition.HookProjection == nil {
				t.Fatalf("%s Definition has no derived hook projection", target)
			}
			if len(definition.HookProjection.Project.Envelope) > 0 {
				definition.HookProjection.Project.Envelope["version"] = "mutated"
				fresh, ok := Lookup(string(target))
				if !ok || fresh.HookProjection.Project.Envelope["version"] == "mutated" {
					t.Errorf("%s Definition hook projection aliases its declaration", target)
				}
			}
		})
	}
}
