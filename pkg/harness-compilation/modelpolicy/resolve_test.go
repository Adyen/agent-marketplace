package modelpolicy

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func TestResolve(t *testing.T) {
	for _, tc := range []struct {
		name                                 string
		source, target                       target.Target
		model, effort, wantModel, wantEffort string
		losses                               []LossKind
	}{
		{"unknown", "", "claude", "future-model", "", "", "", []LossKind{ModelOmitted}},
		{"opaque native", "claude", "claude", "custom-native", "future", "custom-native", "future", nil},
		{"opaque cross target", "claude", "codex", "custom-native", "", "", "", []LossKind{ModelOmitted}},
		{"native devin", "devin", "devin", "gpt-5-6-luna-medium", "", "gpt-5-6-luna-medium", "", nil},
		{"native default", "devin", "devin", "", "", "", "", nil},
		{"parent to default", "claude", "devin", "inherit", "", "", "", []LossKind{SelectionChanged}},
		{"default to parent", "devin", "claude", "", "", "", "", []LossKind{SelectionChanged}},
		{"implicit parent to default", "", "devin", "", "", "", "", []LossKind{SelectionChanged}},
		{"codex parent", "", "codex", "inherit", "high", "", "high", nil},
		{"factory parent", "", "factory", "inherit", "high", "inherit", "", []LossKind{EffortOmitted}},
		{"cursor embedded", "", "cursor", "claude-opus-5", "high", "claude-opus-5[effort=high]", "", nil},
		{"cursor reverse", "cursor", "claude", "claude-opus-5[effort=high]", "", "claude-opus-5", "high", nil},
		{"cursor native", "cursor", "cursor", "claude-opus-5[effort=high]", "", "claude-opus-5[effort=high]", "", nil},
		{"cursor conflict", "cursor", "claude", "claude-opus-5[effort=high]", "low", "claude-opus-5", "low", []LossKind{EffortConflict}},
		{"cursor extra parameters", "cursor", "claude", "claude-opus-5[effort=high,context=300k]", "", "", "", []LossKind{ModelOmitted}},
		{"gemini effort", "", "gemini", "gemini-3-preview", "high", "gemini-3-preview", "", []LossKind{EffortOmitted}},
		{"no cross provider approximation", "", "gemini", "sonnet", "", "", "", []LossKind{ModelOmitted}},
		{"exact does not float", "factory", "devin", "claude-sonnet-4-5-20250929", "", "", "", []LossKind{ModelOmitted}},
		{"floating alias", "", "devin", "sonnet", "", "sonnet", "", nil},
		{"explicit family pin", "", "factory", "sonnet", "high", "claude-sonnet-4-5-20250929", "high", []LossKind{SelectorPinned}},
		{"concrete portable provider name", "claude", "codex", "gpt-5.6-luna", "medium", "gpt-5.6-luna", "medium", nil},
		{"qualified provider identity", "", "codex", "openai/gpt-5.6-terra", "high", "gpt-5.6-terra", "high", nil},
		{"unsupported level", "codex", "factory", "gpt-5.6-luna", "max", "gpt-5.6-luna", "", []LossKind{EffortOmitted}},
		{"unknown with inherited effort", "", "codex", "missing-model", "high", "", "high", []LossKind{ModelOmitted}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Resolve(Request{Source: tc.source, Target: tc.target, Model: tc.model, Effort: tc.effort})
			if err != nil {
				t.Fatal(err)
			}
			var losses []LossKind
			for _, loss := range result.Losses {
				losses = append(losses, loss.Kind)
			}
			if result.Model != tc.wantModel || result.Effort != tc.wantEffort || !slices.Equal(losses, tc.losses) {
				t.Fatalf("got %+v, want model=%q effort=%q losses=%v", result, tc.wantModel, tc.wantEffort, tc.losses)
			}
		})
	}
}

func TestResolveOverrides(t *testing.T) {
	for _, tc := range []struct {
		model, replacement, effort, wantEffort string
		loss                                   bool
	}{
		{"sonnet", "custom-native", "high", "", true},
		{"unknown", "claude-opus-5", "high", "high", false},
		{"sonnet", "", "", "", false},
	} {
		result, err := Resolve(Request{Target: target.TargetClaude, Model: tc.model, Effort: tc.effort, Overrides: map[string]string{tc.model: tc.replacement}})
		if err != nil || result.Model != tc.replacement || result.Effort != tc.wantEffort || (len(result.Losses) > 0) != tc.loss {
			t.Fatalf("override %+v: result=%+v err=%v", tc, result, err)
		}
	}
}

func TestShippedCatalogs(t *testing.T) {
	r, err := shipped()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range target.Targets() {
		if _, ok := r.catalogs[name]; !ok {
			t.Errorf("missing catalog %s", name)
		}
	}
}

func TestRejectInvalidCatalog(t *testing.T) {
	const valid = "schema: 1\nharness: claude\nevidence:\n  checked: '2026-09-21'\n  refs: [test]\nsurfaces:\n  subagent:\n    model_field: model\n    effort_field: effort\n    omitted_model: inherit\n    inherit_model: inherit\nmodels:\n  example/model:\n    model: native-model\n    reasoning:\n      high: {effort: high}\n"
	load := func(data string) error {
		_, err := loadCatalogs(fstest.MapFS{"claude.yaml": {Data: []byte(data)}}, "*.yaml")
		return err
	}
	if err := load(valid); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, old, replacement string }{
		{"unknown key", "schema: 1", "schema: 1\nother: value"},
		{"duplicate key", "schema: 1", "schema: 1\nschema: 1"},
		{"case collision", "schema: 1", "schema: 1\nSchema: 1"},
		{"version", "schema: 1", "schema: 2"},
		{"wrong harness", "harness: claude", "harness: codex"},
		{"empty native", "model: native-model", "model: ''"},
		{"bad surface", "omitted_model: inherit", "omitted_model: guess"},
		{"no effort field", "effort_field: effort", "effort_field: ''"},
		{"wrong native field", "effort_field: effort", "effort_field: reasoningEffort"},
		{"invalid date", "2026-09-21", "yesterday"},
		{"native ambiguity", "high: {effort: high}", "high: {model: native-model}"},
		{"multiple documents", "schema: 1", "---\nschema: 1\n---\nschema: 1"},
		{"bad family pin", "models:\n", "families:\n  example/family:\n    pin: example/missing\nmodels:\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := load(strings.Replace(valid, tc.old, tc.replacement, 1)); err == nil {
				t.Fatal("accepted invalid catalog")
			}
		})
	}
}

func BenchmarkResolve(b *testing.B) {
	for _, req := range []Request{
		{Target: target.TargetDevin, Model: "sonnet"},
		{Target: target.TargetDevin, Model: "unknown"},
		{Target: target.TargetCodex, Model: "inherit", Effort: "high"},
		{Source: target.TargetCursor, Target: target.TargetClaude, Model: "claude-opus-5[effort=high]"},
	} {
		b.Run(req.Model, func(b *testing.B) {
			if err := ValidateCatalogs(); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Resolve(req); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
