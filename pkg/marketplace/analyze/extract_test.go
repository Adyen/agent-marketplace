package analyze

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestExtractResourceSamples(t *testing.T) {
	s := ir.NewSkill("review", "Review", "Body", []string{"user"}, map[string]any{
		"nested": map[string]any{
			"deep": "hidden string",
		},
	}, ir.SourceRef{})
	s.AllowedTools = []string{"read"}

	samples := extractResourceSamples(s)
	if len(samples) == 0 {
		t.Fatal("expected samples")
	}

	paths := make(map[string]bool)
	for _, sample := range samples {
		paths[sample.path] = true
	}

	if !paths["skill/review/name"] {
		t.Error("missing name sample")
	}
	if !paths["skill/review/Description"] {
		t.Error("missing Description sample")
	}
	if !paths["skill/review/AllowedTools/0"] {
		t.Error("missing AllowedTools sample")
	}
	if !paths["skill/review/Extra/nested/deep"] {
		t.Errorf("missing nested extra sample, got paths %v", paths)
	}
}

func TestExtractCollectionSamples(t *testing.T) {
	c := ir.NewCollection("defaults", "1.0.0", "Desc", []ir.CollectionMember{
		{Kind: ir.KindSkill, Name: "review", Registry: "github.com/example/repo", Path: "skills/review"},
	}, []string{"base"}, nil, ir.SourceRef{})

	samples := extractResourceSamples(c)
	paths := make(map[string]bool)
	for _, s := range samples {
		paths[s.path] = true
	}
	if !paths["collection/defaults/name"] {
		t.Error("missing collection name")
	}
	if !paths["collection/defaults/Members/0/Name"] {
		t.Error("missing member name")
	}
	if !paths["collection/defaults/Members/0/Registry"] {
		t.Error("missing member registry")
	}
	if !paths["collection/defaults/Includes/0"] {
		t.Error("missing include")
	}
}
