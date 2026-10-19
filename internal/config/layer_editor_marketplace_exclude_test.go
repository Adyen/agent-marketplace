package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLayerEditorExcludeMarketplacePin(t *testing.T) {
	for _, kind := range []string{"skill", "command", "subagent", "hook", "mcpserver"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\n# Preserve preferences.\nsources:\n  company: {marketplace: https://market.example}\nresources:\n  company:\n    include: [tools/skill/keep]\n    exclude: [tools/skill/other]\n")
			editor := openTestEditor(t, UserPersonal, path, path)
			binding, err := editor.EnsureMarketplaceSource("HTTPS://MARKET.example/")
			if err != nil || binding.Name != "company" {
				t.Fatalf("source binding = %#v / %v", binding, err)
			}
			pin := testMarketplacePin()
			pin.Kind, pin.RegistrySlug = kind, "tools"
			pin.InstalledName = "local-alias"
			pin.Direct = false
			pin.SelectedBy = MarketplaceSelectionsFrom([]MarketplaceSelection{{RegistryID: pin.RegistryID, Name: "bundle", Revision: pin.Digest}})
			if err := editor.ExcludeMarketplacePin(binding, pin); err != nil {
				t.Fatal(err)
			}
			candidate, err := editor.Render()
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := ParseV4(candidate.Bytes)
			if err != nil {
				t.Fatal(err)
			}
			locatorKind := kind
			if kind == "mcpserver" {
				locatorKind = "mcp"
			}
			group := cfg.Groups["company"]
			if len(cfg.Sources) != 1 || !slices.Equal(group.Exclude, []string{"tools/skill/other", "tools/" + locatorKind + "/review"}) || len(group.Include) != 1 || group.Include[0].Locator != "tools/skill/keep" {
				t.Fatalf("unexpected exclusion: %#v", cfg)
			}
			if !strings.Contains(string(candidate.Bytes), "# Preserve preferences.") || candidate.Mode.Perm() != 0o640 {
				t.Fatalf("comment or mode lost: %#v", candidate)
			}
			if err := os.WriteFile(path, candidate.Bytes, candidate.Mode); err != nil {
				t.Fatal(err)
			}
			editor = openTestEditor(t, UserPersonal, path, path)
			binding, err = editor.EnsureMarketplaceSource("https://market.example")
			if err != nil {
				t.Fatal(err)
			}
			if err := editor.ExcludeMarketplacePin(binding, pin); err != nil {
				t.Fatal(err)
			}
			repeated, err := editor.Render()
			if err != nil || repeated.Changed || string(repeated.Bytes) != string(candidate.Bytes) {
				t.Fatalf("repeated exclusion is not byte-stable: %#v / %v", repeated, err)
			}
		})
	}
}

func TestLayerEditorExcludeMarketplacePinRefusals(t *testing.T) {
	for _, choice := range []string{"collection", "missing-slug", "invalid-binding", "git-source"} {
		t.Run(choice, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			const original = "apiVersion: agent-marketplace/v4\nsources:\n  company: {marketplace: https://market.example}\n  git: {git: https://git.example/repo, ref: main}\n"
			layerEditorWrite(t, path, original)
			editor := openTestEditor(t, UserPersonal, path, path)
			binding, err := editor.EnsureMarketplaceSource("https://market.example")
			if err != nil {
				t.Fatal(err)
			}
			pin := testMarketplacePin()
			pin.RegistrySlug = "tools"
			wantCode := "v4.marketplace-runtime-unsupported"
			switch choice {
			case "collection":
				pin.Kind = "collection"
			case "missing-slug":
				pin.RegistrySlug, wantCode = "", "v4.registry-slug"
			case "invalid-binding":
				binding, wantCode = SourceBinding{}, "config.edit.invalid-binding"
			case "git-source":
				binding, err = editor.EnsureRegistry(RegistryEntry{URL: "https://git.example/repo", Ref: "main"})
				if err != nil {
					t.Fatal(err)
				}
				wantCode = "v4.source-kind"
			}
			if err := editor.ExcludeMarketplacePin(binding, pin); layerEditorErrCode(err) != wantCode {
				t.Fatalf("error = %v, want %s", err, wantCode)
			}
			candidate, err := editor.Render()
			if err != nil || candidate.Changed || string(candidate.Bytes) != original {
				t.Fatalf("refused edit changed bytes: %#v / %v", candidate, err)
			}
		})
	}
}
