package harnesscompile

import (
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestModelPolicySkillSourceProvenance(t *testing.T) {
	for _, tc := range []struct {
		source Harness
		path   string
	}{
		{HarnessClaude, ".claude/skills/review/SKILL.md"},
		{HarnessFactory, ".factory/skills/review/SKILL.md"},
		{HarnessCursor, ".cursor/skills/review/SKILL.md"},
		{HarnessGemini, ".gemini/skills/review/SKILL.md"},
		{HarnessDevin, ".devin/skills/review/SKILL.md"},
		{HarnessCodex, ".agents/skills/review/SKILL.md"},
		{HarnessWindsurf, ".windsurf/skills/review/SKILL.md"},
	} {
		t.Run(string(tc.source), func(t *testing.T) {
			bundle, err := IngestBundle(fstest.MapFS{tc.path: {Data: []byte("---\nname: review\ndescription: Review\nmodel: source-native\n---\nReview\n")}}, tc.source)
			if err != nil {
				t.Fatal(err)
			}
			if len(bundle.Resources) != 1 {
				t.Fatalf("resources=%v diags=%v", bundle.Resources, bundle.Diagnostics)
			}
			s := bundle.Resources[0].(*ir.Skill)
			if s.Model != "source-native" || s.ModelSource != string(tc.source) {
				t.Fatalf("source namespace changed: %+v", s)
			}
		})
	}
}
