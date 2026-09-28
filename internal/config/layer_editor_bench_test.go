package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func BenchmarkLayerEditorSelection(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := []byte("apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: [skill/alpha]\n")
	if err := os.WriteFile(path, data, 0o640); err != nil {
		b.Fatal(err)
	}
	entry := RegistryEntry{URL: "https://example.test/repo.git", Ref: "main"}
	target := LayerTarget{Source: RepoCommitted, StagedPath: path, DeclaringPath: path}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		editor, err := OpenLayerEditor(target)
		if err != nil {
			b.Fatal(err)
		}
		binding, err := editor.EnsureRegistry(entry)
		if err != nil {
			b.Fatal(err)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionInclude, Locators: []ResourceLocator{{Kind: ir.KindSkill, Name: "alpha"}}}); err != nil {
			b.Fatal(err)
		}
		if _, err := editor.Render(); err != nil {
			b.Fatal(err)
		}
	}
}
