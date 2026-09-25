package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestEditV4ResourceSelectionClearRootExclude(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "registry")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yaml")
	data := "apiVersion: agent-marketplace/v4\nsources:\n  local: {directory: " + source + "}\nresources:\n  local:\n    exclude: [skill/alpha, skill/sibling]\n"
	if err := os.WriteFile(path, []byte(data), 0o640); err != nil {
		t.Fatal(err)
	}
	handled, err := EditV4ResourceSelection(path, path, RegistryEntry{URL: source, V4Kind: V4SourceDirectory}, ir.KindSkill, "alpha", V4SelectionClearExclude)
	if !handled || err != nil {
		t.Fatalf("clear = handled %v err %v", handled, err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "skill/alpha") || !strings.Contains(string(got), "skill/sibling") {
		t.Fatalf("clear removed wrong exclusions: %s", got)
	}
}

func TestIncludeMarketplacePinsPreservesCollectionMemberExclusion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := "apiVersion: agent-marketplace/v4\nsources:\n  mp: {marketplace: https://mp.example.test}\nresources:\n  mp: {include: [src/collection/bundle], exclude: [src/skill/review]}\n"
	layerEditorWrite(t, path, original)
	editor := openTestEditor(t, RepoPersonal, path, path)
	binding, found, err := editor.LookupMarketplaceSource("https://mp.example.test")
	if err != nil || !found {
		t.Fatalf("lookup: found=%v err=%v", found, err)
	}
	pin := testMarketplacePin()
	pin.RegistrySlug = "src"
	pin.Revision = pin.Digest
	pin.SelectedBy = MarketplaceSelectionsFrom([]MarketplaceSelection{{RegistryID: "src", Name: "bundle", Revision: pin.Revision}})
	if err := editor.IncludeMarketplacePins(binding, []MarketplaceResourcePin{pin}); err != nil {
		t.Fatal(err)
	}
	candidate, err := editor.Render()
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Changed || string(candidate.Bytes) != original {
		t.Fatalf("collection member exclusion mutated:\n%s", candidate.Bytes)
	}
	if len(editor.RenderedV4().Requests) != 1 || editor.RenderedV4().Requests[0].Locator != "src/collection/bundle" {
		t.Fatalf("collection request lost: %#v", editor.RenderedV4().Requests)
	}
}

func TestIncludeMarketplacePinsReincludesAfterInheritedExclusion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	child := filepath.Join(dir, "child.yaml")
	fragment := "resources:\n  mp: {exclude: [src/skill/review]}\n"
	layerEditorWrite(t, child, fragment)
	layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\ninclude: child.yaml\nsources:\n  mp: {marketplace: https://mp.example.test}\n")
	editor := openTestEditor(t, RepoPersonal, path, path)
	binding, found, err := editor.LookupMarketplaceSource("https://mp.example.test")
	if err != nil || !found {
		t.Fatalf("lookup: found=%v err=%v", found, err)
	}
	pin := testMarketplacePin()
	pin.RegistrySlug = "src"
	pin.Revision = pin.Digest
	pin.Direct = true
	if err := editor.IncludeMarketplacePins(binding, []MarketplaceResourcePin{pin}); err != nil {
		t.Fatal(err)
	}
	if _, err := editor.Render(); err != nil {
		t.Fatal(err)
	}
	if len(editor.RenderedV4().Requests) != 1 || editor.RenderedV4().Requests[0].Locator != "src/skill/review" {
		t.Fatalf("later root include did not override child exclusion: %#v", editor.RenderedV4().Requests)
	}
	childData, err := os.ReadFile(child)
	if err != nil || string(childData) != fragment {
		t.Fatalf("included fragment changed: %q, %v", childData, err)
	}
}

func TestIncludeMarketplacePinsRefusesRootWildcardSuppression(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := "apiVersion: agent-marketplace/v4\nsources:\n  mp: {marketplace: https://mp.example.test}\nresources:\n  mp: {exclude: [src/skill/*, src/skill/review]}\n"
	layerEditorWrite(t, path, original)
	editor := openTestEditor(t, RepoPersonal, path, path)
	binding, found, err := editor.LookupMarketplaceSource("https://mp.example.test")
	if err != nil || !found {
		t.Fatalf("lookup: found=%v err=%v", found, err)
	}
	pin := testMarketplacePin()
	pin.RegistrySlug = "src"
	pin.Revision = pin.Digest
	pin.Direct = true
	err = editor.IncludeMarketplacePins(binding, []MarketplaceResourcePin{pin})
	var diagnostic *V4ValidationError
	if !errors.As(err, &diagnostic) || diagnostic.Code != "v4.edit-exclusion" {
		t.Fatalf("suppressed direct request error = %v, want v4.edit-exclusion", err)
	}
	candidate, err := editor.Render()
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Changed || string(candidate.Bytes) != original {
		t.Fatalf("rejected include mutated config:\n%s", candidate.Bytes)
	}
}

func TestIncludeMarketplacePinsClearsRootExactExclusion(t *testing.T) {
	for _, tc := range []struct {
		name     string
		include  string
		selected bool
	}{
		{name: "new direct request"},
		{name: "existing direct request", include: "    include: [src/skill/review]\n"},
		{name: "direct and collection selected", selected: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\nsources:\n  mp: {marketplace: https://mp.example.test}\nresources:\n  mp:\n"+tc.include+"    exclude: [src/skill/review, src/skill/other]\n")
			editor := openTestEditor(t, RepoPersonal, path, path)
			binding, found, err := editor.LookupMarketplaceSource("https://mp.example.test")
			if err != nil || !found {
				t.Fatalf("lookup: found=%v err=%v", found, err)
			}
			pin := testMarketplacePin()
			pin.RegistrySlug = "src"
			pin.Revision = pin.Digest
			pin.Direct = true
			if tc.selected {
				pin.SelectedBy = MarketplaceSelectionsFrom([]MarketplaceSelection{{RegistryID: "src", Name: "bundle", Revision: pin.Revision}})
			}
			if err := editor.IncludeMarketplacePins(binding, []MarketplaceResourcePin{pin}); err != nil {
				t.Fatal(err)
			}
			candidate, err := editor.Render()
			if err != nil {
				t.Fatal(err)
			}
			if !candidate.Changed || len(editor.RenderedV4().Requests) != 1 || editor.RenderedV4().Requests[0].Locator != "src/skill/review" {
				t.Fatalf("effective requests = %#v; changed=%v; config:\n%s", editor.RenderedV4().Requests, candidate.Changed, candidate.Bytes)
			}
			group := editor.RenderedV4().Groups["mp"]
			if len(group.Exclude) != 1 || group.Exclude[0] != "src/skill/other" {
				t.Fatalf("unrelated exclusion lost: %#v; config:\n%s", group.Exclude, candidate.Bytes)
			}
		})
	}
}
