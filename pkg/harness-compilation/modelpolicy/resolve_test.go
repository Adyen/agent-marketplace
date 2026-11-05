package modelpolicy

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// spec: harness-model-catalogs/resolved-model-emits-the-native-spelling
// spec: harness-model-catalogs/omitted-model-uses-the-surface-s-native-semantics
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
		{"explicit family pin", "", "factory", "sonnet", "high", "claude-sonnet-5", "high", []LossKind{SelectorPinned}},
		{"factory opus pin", "", "factory", "opus", "", "claude-opus-5-5", "", []LossKind{SelectorPinned}},
		{"factory haiku pin", "", "factory", "haiku", "", "claude-haiku-4-5-20251001", "", []LossKind{SelectorPinned}},
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

func TestFactoryCatalogDocumentedInventory(t *testing.T) {
	documented := []string{
		"claude-fable-5.1", "claude-fable-5",
		"claude-opus-5-5", "claude-opus-5-5-fast", "claude-opus-5", "claude-opus-5-fast",
		"claude-opus-4-8", "claude-opus-4-8-fast", "claude-opus-4-7", "claude-opus-4-6",
		"claude-opus-4-5-20251101", "claude-sonnet-5", "claude-sonnet-4-6",
		"claude-sonnet-4-5-20250929", "claude-haiku-4-5-20251001",
		"gpt-6-astra", "gpt-6-sol", "gpt-6-luna",
		"gpt-5.6-sol", "gpt-5.6-sol-fast", "gpt-5.6-terra", "gpt-5.6-luna",
		"gpt-5.5", "gpt-5.5-fast", "gpt-5.5-pro",
		"gpt-5.4", "gpt-5.4-fast", "gpt-5.4-mini", "gpt-5.4-mini-fast",
		"gpt-5.3-codex", "gpt-5.3-codex-fast", "gpt-5.2",
		"gemini-3.1-pro-preview", "gemini-3.8-flash", "gemini-3.7-flash",
		"gemini-3.6-flash", "gemini-3.5-flash", "gemini-3-flash-preview",
		"grok-4.7", "grok-4.6", "grok-4.5",
		"inkling", "mistral-medium-3.5", "glm-5.3-flash", "glm-5.3", "glm-5.2",
		"glm-5.2-fast", "kimi-k3", "qwen3.8-max", "nemotron-3-ultra",
		"deepseek-v4.1-flash", "deepseek-v4-flash-0731", "deepseek-v4-pro",
		"minimax-m3", "minimax-m2.7", "kimi-k2.5", "glm-5.1",
	}
	r, err := shipped()
	if err != nil {
		t.Fatal(err)
	}
	c := r.catalogs[target.TargetFactory]
	present := map[string]bool{}
	for _, record := range c.Models {
		present[record.Model] = true
	}
	for _, id := range documented {
		if !present[id] {
			t.Errorf("documented Factory model %q missing from shipped catalog", id)
		}
	}
}

func TestFactoryReasoningSubset(t *testing.T) {
	r, err := shipped()
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"low": true, "medium": true, "high": true}
	for id, record := range r.catalogs[target.TargetFactory].Models {
		for level, encoding := range record.Reasoning {
			if !allowed[level] || encoding.Effort != level {
				t.Errorf("factory model %s maps unsupported reasoning %q -> %+v", id, level, encoding)
			}
		}
	}
}

// spec: harness-model-catalogs/every-registered-harness-target-ships-a-catalog
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

// spec: harness-model-catalogs/malformed-catalogs-are-rejected
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

// spec: harness-model-catalogs/same-harness-supported-native-effort-values-remain-unchanged
// spec: harness-model-catalogs/same-harness-family-selectors-use-pinned-reasoning-capabilities
func TestSameHarnessNativeReasoningEncodings(t *testing.T) {
	files := fstest.MapFS{
		"factory.yaml": {Data: []byte(`
schema: 1
harness: factory
evidence: {checked: "2026-10-28", refs: [test]}
surfaces:
  subagent: {model_field: model, effort_field: reasoningEffort, omitted_model: inherit, inherit_model: inherit}
models:
  example/pinned:
    model: native-pinned
    aliases: [native-alias]
    reasoning: {max: {effort: high}}
families:
  example/model: {pin: example/pinned}
`)},
	}
	r, err := loadCatalogs(files, "*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, model, effort, wantEffort string
		losses                          []LossKind
	}{
		{"native encoding value", "native-pinned", "high", "high", nil},
		{"alias encoding value", "native-alias", "high", "high", nil},
		{"canonical name is not a native value", "native-alias", "max", "", []LossKind{EffortOmitted}},
		{"recognized family", "example/model", "high", "high", nil},
		{"unsupported family effort", "example/model", "max", "", []LossKind{EffortOmitted}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := r.resolve(Request{Source: target.TargetFactory, Target: target.TargetFactory, Model: tc.model, Effort: tc.effort})
			var losses []LossKind
			for _, loss := range got.Losses {
				losses = append(losses, loss.Kind)
			}
			if got.Model != tc.model || got.Effort != tc.wantEffort || !slices.Equal(losses, tc.losses) {
				t.Fatalf("got model=%q effort=%q losses=%v, want model=%q effort=%q losses=%v", got.Model, got.Effort, losses, tc.model, tc.wantEffort, tc.losses)
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
