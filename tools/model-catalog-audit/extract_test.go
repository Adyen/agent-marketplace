package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestInventoryAdapters(t *testing.T) {
	for _, tt := range []struct {
		kind string
		body string
		want []observedModel
	}{
		{"claude", "# Models\n| Feature | Opus | Sonnet |\n| Claude API ID | `claude-opus-5-5` | `claude-sonnet-5` |\n| Amazon Bedrock ID | `anthropic.claude-opus-5-5` | `anthropic.claude-sonnet-5` |", []observedModel{{ID: "claude-opus-5-5"}, {ID: "claude-sonnet-5"}}},
		{"factory", "# Available Models\n| Model | Model ID | Multiplier | Reasoning |\n| --- | --- | --- | --- |\n| Opus | `claude-opus-5` | 2x | `low`, `medium`, `high` (default) |\n| Luna | `gpt-5.6-luna` | 1x | `none`, `high` |", []observedModel{{ID: "claude-opus-5", Efforts: []string{"high", "low", "medium"}}, {ID: "gpt-5.6-luna", Efforts: []string{"high", "none"}}}},
		{"codex", "# Models\n<ModelDetails\n name=\"Astra\"\n slug=\"gpt-6-astra\"\n />\n<ModelDetails slug=\"gpt-6-luna\" />\nExample: `gpt-example-not-in-inventory`", []observedModel{{ID: "gpt-6-astra"}, {ID: "gpt-6-luna"}}},
		{"gemini", "export const PREVIEW_GEMINI_MODEL = 'gemini-3-pro-preview';\nexport const CUSTOM_MODEL =\n 'gemini-3.1-pro-preview-customtools';\nexport let DEFAULT_GEMINI_FLASH_MODEL = 'gemini-3.8-flash';\nexport const RETIRED_MODEL = 'none';\nexport const GEMINI_MODEL_ALIAS_PRO = 'pro';\nexport const EMBEDDING_MODEL = 'gemini-embedding-001';\nexport const FLASH_ALIAS = DEFAULT_GEMINI_FLASH_MODEL;\nexport const VALID_GEMINI_MODELS = new Set([PREVIEW_GEMINI_MODEL, CUSTOM_MODEL, DEFAULT_GEMINI_FLASH_MODEL, FLASH_ALIAS, RETIRED_MODEL]);\nconst example = 'gemini-example';", []observedModel{{ID: "gemini-3-pro-preview"}, {ID: "gemini-3.1-pro-preview-customtools"}, {ID: "gemini-3.8-flash"}}},
	} {
		t.Run(tt.kind, func(t *testing.T) {
			got, err := extract(source{Kind: tt.kind}, []byte(tt.body))
			if err != nil || !reflect.DeepEqual(got.Models, tt.want) {
				t.Fatalf("models = %#v, error = %v; want %#v", got.Models, err, tt.want)
			}
		})
	}
}

func TestExtractFailsClosed(t *testing.T) {
	for _, kind := range []string{"claude", "factory", "codex", "gemini", "watch", "unknown"} {
		for _, body := range []string{"", "<html>Sign in</html>", "# Models\nNew format without IDs"} {
			_, err := extract(source{Kind: kind, Fields: []string{"model"}}, []byte(body))
			if !errors.Is(err, errSourceFormat) {
				t.Fatalf("%s accepted %q: %v", kind, body, err)
			}
		}
	}
	_, err := extract(source{Kind: "factory"}, []byte("| Model | Model ID | Multiplier | Reasoning |\n| --- | --- | --- | --- |\n| One | `a` | 1 | `high` |\n| Two | missing-id | 1 | `low` |"))
	if !errors.Is(err, errSourceFormat) {
		t.Fatalf("partially parsed inventory accepted: %v", err)
	}
}

func TestCodexRejectsPartiallyParsedInventory(t *testing.T) {
	_, err := extract(source{Kind: "codex"}, []byte(`<ModelDetails slug="gpt-6-astra" /><ModelDetails modelId="gpt-6-luna" />`))
	if !errors.Is(err, errSourceFormat) {
		t.Fatalf("partial inventory was accepted: %v", err)
	}
}

func TestGeminiRejectsUnknownAndCyclicConstants(t *testing.T) {
	for _, body := range []string{
		"export const VALID_GEMINI_MODELS = new Set([MISSING_MODEL]);",
		"export const A_MODEL = B_MODEL;\nexport const B_MODEL = A_MODEL;\nexport const VALID_GEMINI_MODELS = new Set([A_MODEL]);",
	} {
		if _, err := extract(source{Kind: "gemini"}, []byte(body)); !errors.Is(err, errSourceFormat) {
			t.Fatalf("incomplete constants accepted: %v", err)
		}
	}
}

func TestWatchSelectsModelEvidenceOnly(t *testing.T) {
	s := source{Kind: "watch", Fields: []string{"model", "effort"}, Sections: []string{"Model configuration"}}
	body := "# Subagents\n| Field | Description |\n| `model` | `inherit` or ID |\n| `effort` | `high` |\n| `tools` | old tools |\n### Model configuration\nUse `model`.\n#### Parameters\n`[effort=high]`\n### Examples\nUnrelated content."
	got, err := extract(s, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	want := "| `model` | `inherit` or ID |\n| `effort` | `high` |\n### Model configuration\nUse `model`.\n#### Parameters\n`[effort=high]`"
	if got.Text != want {
		t.Fatalf("text = %q; want %q", got.Text, want)
	}
	other, err := extract(s, []byte(strings.ReplaceAll(body, "old tools", "new tools")))
	if err != nil || other.Text != got.Text {
		t.Fatalf("unrelated tools change affected model evidence: %v", err)
	}
	_, err = extract(s, []byte(strings.ReplaceAll(body, "### Model configuration", "### Changed heading")))
	if !errors.Is(err, errSourceFormat) {
		t.Fatalf("missing required section accepted: %v", err)
	}
}

func TestCursorNamesIgnorePricesButWatchNotes(t *testing.T) {
	s := source{Kind: "cursor"}
	body := "# Models & Pricing\n| Model | Provider | Input | Notes |\n| --- | --- | --- | --- |\n| [Composer 2.5](https://cursor.com/blog/composer-2-5) | Cursor | $1 | Fast available |\n| GPT-6 Astra | OpenAI | $4 | `high` |\n\n| Plan | Price |\n| Pro | $20 |"
	got, err := extract(s, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "Composer 2.5 | Cursor | Fast available\nGPT-6 Astra | OpenAI | `high`" || len(got.Models) != 0 {
		t.Fatalf("unexpected display-name inventory: %#v", got)
	}
	other, err := extract(s, []byte(strings.ReplaceAll(body, "$4", "$8")))
	if err != nil || other.Text != got.Text {
		t.Fatalf("price-only change counted as drift: %v", err)
	}
}
