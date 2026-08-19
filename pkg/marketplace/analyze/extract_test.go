package analyze

import (
	"encoding/json"
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

func TestExtractInterfaceMapKeys(t *testing.T) {
	extra := map[string]any{
		"yamlMap": map[any]any{
			"nested": map[any]any{
				"answer": "42",
			},
		},
	}
	s := ir.NewSkill("review", "Review", "Body", nil, extra, ir.SourceRef{})

	samples := extractResourceSamples(s)
	paths := make(map[string]bool)
	for _, sample := range samples {
		paths[sample.path] = true
	}
	if !paths["skill/review/Extra/yamlMap/nested/answer"] {
		t.Errorf("missing nested interface-map sample, got paths %v", paths)
	}
}

func TestExtractByteAndRawMessageValues(t *testing.T) {
	extra := map[string]any{
		"raw":  []byte("byte content"),
		"json": json.RawMessage(`{"msg":"hello"}`),
	}
	s := ir.NewSkill("review", "Review", "Body", nil, extra, ir.SourceRef{})

	samples := extractResourceSamples(s)
	contents := make(map[string]string)
	for _, sample := range samples {
		contents[sample.path] = sample.content
	}
	if got := contents["skill/review/Extra/raw"]; got != "byte content" {
		t.Errorf("raw bytes: got %q, want %q", got, "byte content")
	}
	if got := contents["skill/review/Extra/json"]; got != `{"msg":"hello"}` {
		t.Errorf("json.RawMessage: got %q, want %q", got, `{"msg":"hello"}`)
	}
}
