package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

const v4PluginEditorFixture = `apiVersion: agent-marketplace/v4
sources:
  kit:
    git: https://example.test/plugins.git
    ref: main
    format: claude-marketplace
resources:
  kit:
    include:
      - plugin/review-kit
      - plugin/lint-kit: {delivery: decompose, exclude: [command/legacy-review]}
      - {kind: plugin, name: "1password.tools"}
`

func openPluginEditor(t *testing.T, fixture string) (*LayerEditor, SourceBinding, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	layerEditorWrite(t, path, fixture)
	editor := openTestEditor(t, RepoCommitted, path, path)
	binding, err := editor.EnsureRegistry(RegistryEntry{URL: "https://example.test/plugins.git", Ref: "main"})
	if err != nil || binding.Name != "kit" {
		t.Fatalf("EnsureRegistry binding = %#v, %v", binding, err)
	}
	return editor, binding, path
}

func TestV4EditorWritesShortPluginLocator(t *testing.T) {
	editor, binding, path := openPluginEditor(t, v4PluginEditorFixture)
	if err := editor.Apply(binding, SelectionChange{Operation: SelectionInclude, Locators: []ResourceLocator{loc(ir.KindPlugin, "audit-kit")}}); err != nil {
		t.Fatalf("include: %v", err)
	}
	candidate, err := editor.Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(string(candidate.Bytes), "plugin/audit-kit") {
		t.Fatalf("short plugin locator was not written:\n%s", candidate.Bytes)
	}
	cfg, err := ParseV4Config(candidate.Bytes, path, true)
	if err != nil {
		t.Fatalf("candidate no longer valid v4: %v", err)
	}
	var found bool
	for _, r := range cfg.Groups["kit"].Include {
		if r.Kind == "plugin" && r.Name == "audit-kit" && !r.Structured {
			found = true
		}
	}
	if !found {
		t.Fatal("plugin request did not round-trip as a short locator")
	}
}

func TestV4EditorWritesStructuredEntryWhenNameNeedsIt(t *testing.T) {
	editor, binding, path := openPluginEditor(t, v4PluginEditorFixture)
	if err := editor.Apply(binding, SelectionChange{Operation: SelectionInclude, Locators: []ResourceLocator{loc(ir.KindPlugin, "audit.kit")}}); err != nil {
		t.Fatalf("include: %v", err)
	}
	candidate, err := editor.Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	text := string(candidate.Bytes)
	if !strings.Contains(text, "kind: plugin") || !strings.Contains(text, "audit.kit") || strings.Contains(text, "plugin/audit.kit") {
		t.Fatalf("unspellable plugin name must be written as a structured entry:\n%s", text)
	}
	cfg, err := ParseV4Config(candidate.Bytes, path, true)
	if err != nil {
		t.Fatalf("candidate no longer valid v4: %v", err)
	}
	var found bool
	for _, r := range cfg.Groups["kit"].Include {
		if r.Kind == "plugin" && r.Name == "audit.kit" && r.Structured {
			found = true
		}
	}
	if !found {
		t.Fatal("structured plugin request did not round-trip")
	}
}

func TestV4EditorKeepsExistingStructuredEntry(t *testing.T) {
	editor, binding, _ := openPluginEditor(t, v4PluginEditorFixture)
	// Re-including an already structured entry must not rewrite it as a locator.
	if err := editor.Apply(binding, SelectionChange{Operation: SelectionInclude, Locators: []ResourceLocator{loc(ir.KindPlugin, "1password.tools")}}); err != nil {
		t.Fatalf("include: %v", err)
	}
	candidate, err := editor.Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	text := string(candidate.Bytes)
	if !strings.Contains(text, `{kind: plugin, name: "1password.tools"}`) || strings.Contains(text, "plugin/1password.tools") {
		t.Fatalf("existing structured entry must stay structured:\n%s", text)
	}
	// Exclusions and delivery authored on plugin requests survive a rewrite.
	if !strings.Contains(text, "delivery: decompose") || !strings.Contains(text, "exclude: [command/legacy-review]") {
		t.Fatalf("plugin request options were dropped:\n%s", text)
	}
}

func TestV4EditorRemovesStructuredEntry(t *testing.T) {
	editor, binding, _ := openPluginEditor(t, v4PluginEditorFixture)
	if err := editor.Apply(binding, SelectionChange{Operation: SelectionRemoveInclude, Locators: []ResourceLocator{loc(ir.KindPlugin, "1password.tools")}}); err != nil {
		t.Fatalf("remove include: %v", err)
	}
	candidate, err := editor.Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(string(candidate.Bytes), "1password.tools") {
		t.Fatalf("structured entry was not removed:\n%s", candidate.Bytes)
	}
}
