package harnesscompile

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestModelPolicySkillExtrasCannotRestoreModel(t *testing.T) {
	for _, target := range []Target{TargetClaude, TargetDevin} {
		t.Run(string(target), func(t *testing.T) {
			skill := ir.NewSkillWithOptions("review", "Review", "Keep body", nil, map[string]any{"MODEL": "injected", "ModelSource": "devin"}, ir.SourceRef{}, ir.WithSkillModel("unknown-model"))
			result, err := Compile(skill, target)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, a := range result.Artifacts {
				if !strings.HasSuffix(a.Path, "SKILL.md") {
					continue
				}
				found = true
				body := strings.ToLower(string(a.Bytes))
				if strings.Contains(body, "model:") || strings.Contains(body, "modelsource:") || !strings.Contains(body, "keep body") {
					t.Fatalf("model/provenance bypass: %s", a.Bytes)
				}
			}
			if !found {
				t.Fatal("skill artifact was suppressed")
			}
		})
	}
}
