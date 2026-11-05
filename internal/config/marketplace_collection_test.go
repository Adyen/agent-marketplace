package config

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func TestEnsureMarketplacePinsPersistsCollectionAndLeafSelectorsAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	writeMarketplaceV4(t, path, "sources:\n  public: {marketplace: https://market.example}\n")
	collectionDigest := "sha256:" + strings.Repeat("a", 64)
	leafDigest := "sha256:" + strings.Repeat("b", 64)
	pins := []MarketplaceResourcePin{
		{RegistryID: "src", RegistrySlug: "tools", Kind: "collection", Name: "bundle", Revision: collectionDigest, Digest: collectionDigest, Acquisition: MarketplaceAcquisitionMirror},
		{RegistryID: "src", RegistrySlug: "tools", Kind: "skill", Name: "review", Revision: leafDigest, Digest: leafDigest, Acquisition: MarketplaceAcquisitionMirror, SelectedBy: MarketplaceSelectionsFrom([]MarketplaceSelection{{RegistryID: "src", Name: "bundle", Revision: collectionDigest}})},
	}
	if _, _, err := commitMarketplaceEdit(path, RepoCommitted, marketplaceIncludeEdit("https://market.example/", pins...)); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(marketplaceRequestLocators(loaded), []string{"tools/collection/bundle"}) {
		t.Fatalf("authored collection requests = %#v", loaded.Requests)
	}
	// Exact graph evidence is owned by the v2 acquisition lock, not the root.
	doc := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo, Marketplaces: []lockfile.MarketplaceLock{{
		URL: "https://market.example", InstanceID: "mp",
		RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "tools", RegistryID: "src"}},
		Resources: []lockfile.MarketplaceResourceLock{
			{RegistryName: "tools", RegistryID: "src", Kind: "collection", Name: "bundle", Revision: collectionDigest, ContentDigest: lockfile.Digest(collectionDigest), CollectionManifestDigest: lockfile.Digest(collectionDigest), Acquisition: "mirror"},
			{RegistryName: "tools", RegistryID: "src", Kind: "skill", Name: "review", Revision: leafDigest, ContentDigest: lockfile.Digest(leafDigest), ArchiveDigest: lockfile.Digest(leafDigest), Acquisition: "mirror", SelectedBy: []lockfile.MarketplaceSelectionLock{{RegistryID: "src", Name: "bundle", Revision: lockfile.Digest(collectionDigest)}}},
		},
	}}}
	lockBytes, err := lockfile.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	doc, err = lockfile.Decode(lockBytes)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := loaded.ToLegacyConfigWithLock(repoLayer, &doc)
	if err != nil {
		t.Fatal(err)
	}
	resources := runtime.Marketplaces[0].Resources
	if len(resources) != 2 || !resources[0].Direct || resources[1].Direct || resources[1].Digest != leafDigest || resources[0].LockCollectionManifestDigest != collectionDigest {
		t.Fatalf("hydrated resources = %#v", resources)
	}
	want := []MarketplaceSelection{{RegistryID: "src", Name: "bundle", Revision: collectionDigest}}
	if got := resources[1].SelectedBy.List(); !slices.Equal(got, want) {
		t.Fatalf("selected_by = %#v, want %#v", got, want)
	}
}

func TestEnsureMarketplacePinsRejectsConflictingIncomingDuplicateBeforeWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	writeMarketplaceV4(t, path, "sources:\n  public: {marketplace: https://market.example}\n")
	before := marketplaceState(t, path)
	pin := testMarketplacePin()
	conflict := pin
	conflict.Revision = "sha256:" + strings.Repeat("b", 64)
	_, _, err := commitMarketplaceEdit(path, RepoCommitted, marketplaceIncludeEdit("https://market.example", pin, conflict))
	requireMarketplaceV4Code(t, err, "v4.duplicate-request")
	requireMarketplaceUnchanged(t, path, before)
	// Identical requests deduplicate; exact digest conflicts remain a lock concern.
	if _, _, err := commitMarketplaceEdit(path, RepoCommitted, marketplaceIncludeEdit("https://market.example", pin, pin)); err != nil {
		t.Fatal(err)
	}
	v4, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(v4.Requests) != 1 {
		t.Fatalf("duplicate includes = %#v", v4.Requests)
	}
	doc := marketplaceTestEvidence()
	other := doc.Marketplaces[0].Resources[0]
	other.ContentDigest = lockfile.Digest("sha256:" + strings.Repeat("b", 64))
	doc.Marketplaces[0].Resources = append(doc.Marketplaces[0].Resources, other)
	_, err = v4.ToLegacyConfigWithLock(repoLayer, &doc)
	requireMarketplaceV4Code(t, err, "v4.marketplace-lock-mismatch")
}

func TestEnsureMarketplacePinsDoesNotWriteAfterLaterPinConflict(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	writeMarketplaceV4(t, path, "sources:\n  public: {marketplace: https://market.example}\nresources:\n  public: [{source-slug/skill/one: {revision: sha256:1111111111111111111111111111111111111111111111111111111111111111}}]\n")
	before := marketplaceState(t, path)
	newPin := testMarketplacePin()
	newPin.Name = "new"
	conflict := testMarketplacePin()
	conflict.Name, conflict.Revision = "one", "sha256:"+strings.Repeat("b", 64)
	editor := openMarketplaceEditor(t, path, RepoCommitted)
	binding, err := editor.EnsureMarketplaceSource("https://market.example")
	if err != nil {
		t.Fatal(err)
	}
	requireMarketplaceV4Code(t, editor.IncludeMarketplacePins(binding, []MarketplaceResourcePin{newPin, conflict}), "v4.duplicate-request")
	candidate, err := editor.Render()
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Changed || string(candidate.Bytes) != string(before.data) {
		t.Fatalf("failed batch changed candidate: %s", candidate.Bytes)
	}
	requireMarketplaceUnchanged(t, path, before)
}
