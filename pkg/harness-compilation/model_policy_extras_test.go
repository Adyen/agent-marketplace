package harnesscompile

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestModelPolicyExtrasCannotRestoreDroppedFields(t *testing.T) {
	for _, target := range []Target{TargetClaude, TargetFactory, TargetCursor, TargetGemini, TargetDevin, TargetCodex} {
		t.Run(string(target), func(t *testing.T) {
			r := ir.NewSubagent("review", "Review", "Keep this body.\n", "unknown-model", nil, "unknown-level", nil, "", map[string]any{"Model": "injected", "EFFORT": "high", "reasoningEffort": "high", "model_reasoning_effort": "high", "ModelSource": "devin"}, ir.SourceRef{})
			result, err := Compile(r, target)
			if err != nil || diag.HasErrors(result.Diagnostics) {
				t.Fatalf("compile: %v %+v", err, result)
			}
			fields := modelPolicyFields(t, result.Artifacts)
			for _, field := range []string{"Model", "model", "EFFORT", "effort", "reasoningEffort", "model_reasoning_effort", "ModelSource"} {
				if _, exists := fields[field]; exists {
					t.Errorf("dropped field %s resurrected: %v", field, fields)
				}
			}
		})
	}
}
