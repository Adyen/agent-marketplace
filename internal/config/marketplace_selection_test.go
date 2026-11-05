package config

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func selectionTestPin(source, kind, name, digest string) MarketplaceResourcePin {
	return MarketplaceResourcePin{RegistryID: source, RegistrySlug: source, Kind: kind, Name: name, Revision: digest, Digest: digest, Acquisition: MarketplaceAcquisitionMirror}
}

func selectionGraphEvidence() lockfile.Document {
	a := lockfile.Digest("sha256:" + strings.Repeat("a", 64))
	b := lockfile.Digest("sha256:" + strings.Repeat("b", 64))
	return lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo, Marketplaces: []lockfile.MarketplaceLock{{
		URL: "https://market.example", InstanceID: "mp",
		RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "collections", RegistryID: "collections"}, {Name: "leaf", RegistryID: "leaf"}},
		Resources: []lockfile.MarketplaceResourceLock{
			{RegistryName: "collections", RegistryID: "collections", Kind: "collection", Name: "alpha", Revision: string(a), ContentDigest: a, CollectionManifestDigest: a, Acquisition: "mirror"},
			{RegistryName: "collections", RegistryID: "collections", Kind: "collection", Name: "beta", Revision: string(b), ContentDigest: b, CollectionManifestDigest: b, Acquisition: "mirror"},
			{RegistryName: "leaf", RegistryID: "leaf", Kind: "skill", Name: "review", Revision: string(b), ContentDigest: b, ArchiveDigest: b, Acquisition: "mirror", SelectedBy: []lockfile.MarketplaceSelectionLock{{RegistryID: "collections", Name: "alpha", Revision: a}, {RegistryID: "collections", Name: "beta", Revision: b}}},
		},
	}}}
}

func selectionBundleEvidence(collection, leaf string) lockfile.Document {
	c := lockfile.Digest(collection)
	l := lockfile.Digest(leaf)
	return lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo, Marketplaces: []lockfile.MarketplaceLock{{
		URL: "https://market.example", InstanceID: "mp",
		RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "collections", RegistryID: "collections"}, {Name: "source", RegistryID: "source"}},
		Resources: []lockfile.MarketplaceResourceLock{
			{RegistryName: "collections", RegistryID: "collections", Kind: "collection", Name: "bundle", Revision: collection, ContentDigest: c, CollectionManifestDigest: c, Acquisition: "mirror"},
			{RegistryName: "source", RegistryID: "source", Kind: "command", Name: "lint", Revision: leaf, ContentDigest: l, ArchiveDigest: l, Acquisition: "mirror"},
			{RegistryName: "source", RegistryID: "source", Kind: "skill", Name: "review", Revision: leaf, ContentDigest: l, ArchiveDigest: l, Acquisition: "mirror", SelectedBy: []lockfile.MarketplaceSelectionLock{{RegistryID: "collections", Name: "bundle", Revision: c}}},
		},
	}}}
}

func runtimeSelectionPins(t *testing.T, path string, evidence lockfile.Document) []MarketplaceResourcePin {
	t.Helper()
	v4, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := lockfile.Encode(evidence)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err = lockfile.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := v4.ToLegacyConfigWithLock(repoLayer, &evidence)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Marketplaces) != 1 {
		t.Fatalf("runtime registrations = %#v", cfg.Marketplaces)
	}
	return cfg.Marketplaces[0].Resources
}

func removeMarketplaceRequests(endpoint string, pins ...MarketplaceResourcePin) func(*LayerEditor) error {
	return func(editor *LayerEditor) error {
		binding, found, err := editor.LookupMarketplaceSource(endpoint)
		if err != nil {
			return err
		}
		if !found {
			return v4err("config.edit.request-missing", endpoint, 0, "Marketplace source is absent")
		}
		return editor.RemoveMarketplacePins(binding, pins)
	}
}

func TestMarketplaceSelectionMutationPreservesDirectAndOtherCollections(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	writeMarketplaceV4(t, path, "sources:\n  public: {marketplace: https://market.example}\nresources:\n  public: [collections/collection/alpha, collections/collection/beta, leaf/skill/review]\n")
	evidence := selectionGraphEvidence()
	resources := runtimeSelectionPins(t, path, evidence)
	wantEdges := []MarketplaceSelection{{RegistryID: "collections", Name: "alpha", Revision: "sha256:" + strings.Repeat("a", 64)}, {RegistryID: "collections", Name: "beta", Revision: "sha256:" + strings.Repeat("b", 64)}}
	if len(resources) != 3 || !resources[2].Direct || !slices.Equal(resources[2].SelectedBy.List(), wantEdges) {
		t.Fatalf("initial direct/collection graph = %#v", resources)
	}
	if _, _, err := commitMarketplaceEdit(path, RepoCommitted, removeMarketplaceRequests("https://market.example", resources[0])); err != nil {
		t.Fatal(err)
	}
	// The authored edit must not silently accept an unreconciled lock containing an orphan root.
	v4, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = v4.ToLegacyConfigWithLock(repoLayer, &evidence)
	requireMarketplaceV4Code(t, err, "v4.marketplace-lock-mismatch")
	evidence.Marketplaces[0].Resources = evidence.Marketplaces[0].Resources[1:]
	evidence.Marketplaces[0].Resources[1].SelectedBy = []lockfile.MarketplaceSelectionLock{{RegistryID: "collections", Name: "beta", Revision: lockfile.Digest("sha256:" + strings.Repeat("b", 64))}}
	resources = runtimeSelectionPins(t, path, evidence)
	if len(resources) != 2 || !resources[1].Direct || !slices.Equal(resources[1].SelectedBy.List(), wantEdges[1:]) {
		t.Fatalf("after alpha removal = %#v", resources)
	}
	if _, _, err := commitMarketplaceEdit(path, RepoCommitted, removeMarketplaceRequests("https://market.example", resources[1])); err != nil {
		t.Fatal(err)
	}
	resources = runtimeSelectionPins(t, path, evidence)
	if len(resources) != 2 || resources[1].Direct || !slices.Equal(resources[1].SelectedBy.List(), wantEdges[1:]) {
		t.Fatalf("direct removal lost beta edge = %#v", resources)
	}
	if _, _, err := commitMarketplaceEdit(path, RepoCommitted, removeMarketplaceRequests("https://market.example", resources[0])); err != nil {
		t.Fatal(err)
	}
	evidence.Marketplaces[0].Resources = nil
	if resources := runtimeSelectionPins(t, path, evidence); len(resources) != 0 {
		t.Fatalf("leaf retained without any selector = %#v", resources)
	}
}

func TestMarketplaceCollectionReplacementReplacesEdgesInOneCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	writeMarketplaceV4(t, path, "# retain exact rollback bytes\nsources:\n  public: {marketplace: https://market.example}\nresources:\n  public: [collections/collection/bundle, source/command/lint]\n")
	a, b := "sha256:"+strings.Repeat("a", 64), "sha256:"+strings.Repeat("b", 64)
	c, d := "sha256:"+strings.Repeat("c", 64), "sha256:"+strings.Repeat("d", 64)
	old := runtimeSelectionPins(t, path, selectionBundleEvidence(a, c))
	if len(old) != 3 || old[2].Direct || old[2].SelectedBy.List()[0].Revision != a {
		t.Fatalf("old graph = %#v", old)
	}
	before := marketplaceState(t, path)
	newRoot := selectionTestPin("collections", "collection", "bundle", b)
	// Replacing the root request is one candidate transaction; exact revised
	// occurrences and edges are independently supplied by the refreshed lock.
	snapshot, commit, err := commitMarketplaceEdit(path, RepoCommitted, func(editor *LayerEditor) error {
		binding, err := editor.EnsureMarketplaceSource("https://market.example")
		if err != nil {
			return err
		}
		if err := editor.RemoveMarketplacePins(binding, []MarketplaceResourcePin{old[0]}); err != nil {
			return err
		}
		return editor.IncludeMarketplacePins(binding, []MarketplaceResourcePin{newRoot})
	})
	if err != nil {
		t.Fatal(err)
	}
	updatedEvidence := selectionBundleEvidence(b, d)
	// The unrelated direct command keeps its original exact occurrence.
	updatedEvidence.Marketplaces[0].Resources[1].Revision = c
	updatedEvidence.Marketplaces[0].Resources[1].ContentDigest = lockfile.Digest(c)
	updatedEvidence.Marketplaces[0].Resources[1].ArchiveDigest = lockfile.Digest(c)
	updated := runtimeSelectionPins(t, path, updatedEvidence)
	if len(updated) != 3 || updated[0].Revision != b || updated[0].Digest != b || updated[1].Digest != c || !updated[1].Direct || updated[2].Digest != d || updated[2].Direct || !slices.Equal(updated[2].SelectedBy.List(), []MarketplaceSelection{{RegistryID: "collections", Name: "bundle", Revision: b}}) {
		t.Fatalf("replacement graph = %#v", updated)
	}
	if snapshot == nil || commit.Generation == "" || bytes.Equal(commit.Bytes, before.data) {
		t.Fatal("replacement did not produce a real candidate commit")
	}
	if err := snapshot.RestoreIfCurrent(commit); err != nil {
		t.Fatal(err)
	}
	after := marketplaceState(t, path)
	if !bytes.Equal(before.data, after.data) || before.info.Mode() != after.info.Mode() {
		t.Fatal("rollback lost bytes or permissions")
	}
	if restored := runtimeSelectionPins(t, path, selectionBundleEvidence(a, c)); restored[0].Digest != a {
		t.Fatalf("rollback graph = %#v", restored)
	}
}

func TestMarketplaceCollectionRemovalDoesNotSkipAdjacentLeaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	writeMarketplaceV4(t, path, "sources:\n  public: {marketplace: https://market.example}\nresources:\n  public: [collections/collection/bundle]\n")
	digest := "sha256:" + strings.Repeat("a", 64)
	evidence := selectionBundleEvidence(digest, digest)
	evidence.Marketplaces[0].Resources = append(evidence.Marketplaces[0].Resources[:1],
		lockfile.MarketplaceResourceLock{RegistryName: "source", RegistryID: "source", Kind: "skill", Name: "one", Revision: digest, ContentDigest: lockfile.Digest(digest), ArchiveDigest: lockfile.Digest(digest), Acquisition: "mirror", SelectedBy: []lockfile.MarketplaceSelectionLock{{RegistryID: "collections", Name: "bundle", Revision: lockfile.Digest(digest)}}},
		lockfile.MarketplaceResourceLock{RegistryName: "source", RegistryID: "source", Kind: "skill", Name: "two", Revision: digest, ContentDigest: lockfile.Digest(digest), ArchiveDigest: lockfile.Digest(digest), Acquisition: "mirror", SelectedBy: []lockfile.MarketplaceSelectionLock{{RegistryID: "collections", Name: "bundle", Revision: lockfile.Digest(digest)}}},
		lockfile.MarketplaceResourceLock{RegistryName: "source", RegistryID: "source", Kind: "skill", Name: "three", Revision: digest, ContentDigest: lockfile.Digest(digest), ArchiveDigest: lockfile.Digest(digest), Acquisition: "mirror", SelectedBy: []lockfile.MarketplaceSelectionLock{{RegistryID: "collections", Name: "bundle", Revision: lockfile.Digest(digest)}}})
	if resources := runtimeSelectionPins(t, path, evidence); len(resources) != 4 {
		t.Fatalf("non-vacuous leaf graph = %#v", resources)
	}
	if _, _, err := commitMarketplaceEdit(path, RepoCommitted, removeMarketplaceRequests("https://market.example", selectionTestPin("collections", "collection", "bundle", digest))); err != nil {
		t.Fatal(err)
	}
	v4, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(v4.Requests) != 0 {
		t.Fatalf("requests after root removal = %#v", v4.Requests)
	}
	_, err = v4.ToLegacyConfigWithLock(repoLayer, &evidence)
	requireMarketplaceV4Code(t, err, "v4.marketplace-lock-mismatch")
	evidence.Marketplaces[0].Resources = nil
	if resources := runtimeSelectionPins(t, path, evidence); len(resources) != 0 {
		t.Fatalf("adjacent leaves remained = %#v", resources)
	}
}

func TestMarketplaceCollectionMutationRejectsDuplicateRoots(t *testing.T) {
	for _, remove := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		writeMarketplaceV4(t, path, "sources:\n  public: {marketplace: https://market.example}\nresources:\n  public: [collections/collection/bundle, collections/collection/bundle]\n")
		before := marketplaceState(t, path)
		root := selectionTestPin("collections", "collection", "bundle", "sha256:"+strings.Repeat("a", 64))
		edit := marketplaceIncludeEdit("https://market.example", root)
		if remove {
			edit = removeMarketplaceRequests("https://market.example", root)
		}
		_, _, err := commitMarketplaceEdit(path, RepoCommitted, edit)
		requireMarketplaceV4Code(t, err, "v4.duplicate-request")
		requireMarketplaceUnchanged(t, path, before)
	}
	// Identical lock evidence has its own deduplication contract, independent
	// of duplicate declarations in one authored v4 include list.
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	writeMarketplaceV4(t, path, "sources:\n  public: {marketplace: https://market.example}\nresources:\n  public: [collections/collection/bundle]\n")
	digest := "sha256:" + strings.Repeat("a", 64)
	evidence := selectionBundleEvidence(digest, digest)
	evidence.Marketplaces[0].Resources = evidence.Marketplaces[0].Resources[:1]
	evidence.Marketplaces[0].Resources = append(evidence.Marketplaces[0].Resources, evidence.Marketplaces[0].Resources[0])
	if pins := runtimeSelectionPins(t, path, evidence); len(pins) != 1 || !pins[0].Direct {
		t.Fatalf("identical lock root dedup = %#v", pins)
	}
}

func TestMarketplaceCollectionReplacementRequiresRootIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	writeMarketplaceV4(t, path, "sources:\n  public: {marketplace: https://market.example}\nresources:\n  public: [collections/collection/bundle]\n")
	before := marketplaceState(t, path)
	wrong := selectionTestPin("other-source", "collection", "different-bundle", "sha256:"+strings.Repeat("a", 64))
	_, _, err := commitMarketplaceEdit(path, RepoCommitted, removeMarketplaceRequests("https://market.example", wrong))
	requireMarketplaceV4Code(t, err, "config.edit.request-missing")
	requireMarketplaceUnchanged(t, path, before)
}

func TestMarketplaceCollectionReplacementRejectsIndependentContentConflicts(t *testing.T) {
	for _, body := range []string{
		"sources:\n  public: {marketplace: https://market.example}\nresources:\n  public: [collections/collection/bundle, source/skill/review]\n",
		"sources:\n  public: {marketplace: https://market.example}\nresources:\n  public: [collections/collection/bundle, collections/collection/other]\n",
	} {
		path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		writeMarketplaceV4(t, path, body)
		before := marketplaceState(t, path)
		a, c, d := "sha256:"+strings.Repeat("a", 64), "sha256:"+strings.Repeat("c", 64), "sha256:"+strings.Repeat("d", 64)
		doc := selectionBundleEvidence(a, c)
		doc.Marketplaces[0].Resources = append(doc.Marketplaces[0].Resources[:1], doc.Marketplaces[0].Resources[2])
		if strings.Contains(body, "collection/other") {
			other := doc.Marketplaces[0].Resources[0]
			other.Name = "other"
			doc.Marketplaces[0].Resources = append(doc.Marketplaces[0].Resources, other)
			doc.Marketplaces[0].Resources[1].SelectedBy = append(doc.Marketplaces[0].Resources[1].SelectedBy, lockfile.MarketplaceSelectionLock{RegistryID: "collections", Name: "other", Revision: lockfile.Digest(a)})
		}
		if pins := runtimeSelectionPins(t, path, doc); len(pins) < 2 {
			t.Fatal("conflict control did not select a leaf")
		}
		conflictingLeaf := doc.Marketplaces[0].Resources[1]
		conflictingLeaf.Revision, conflictingLeaf.ContentDigest, conflictingLeaf.ArchiveDigest = d, lockfile.Digest(d), lockfile.Digest(d)
		doc.Marketplaces[0].Resources = append(doc.Marketplaces[0].Resources, conflictingLeaf)
		v4, err := LoadV4ConfigFromPath(path)
		if err != nil {
			t.Fatal(err)
		}
		_, err = v4.ToLegacyConfigWithLock(repoLayer, &doc)
		requireMarketplaceV4Code(t, err, "v4.marketplace-lock-mismatch")
		requireMarketplaceUnchanged(t, path, before)
	}
}

func TestMarketplaceCollectionReplacementRejectsMalformedSelections(t *testing.T) {
	pin := selectionTestPin("source", "skill", "review", "sha256:"+strings.Repeat("a", 64))
	pin.SelectedBy = MarketplaceSelections("malformed")
	if err := ValidateMarketplaceResourcePin(pin); err == nil || !strings.Contains(err.Error(), "selected_by") {
		t.Fatalf("malformed selection error = %v", err)
	}
}

func TestMarketplaceCollectionReplacementPreservesDirectAndOtherCollectionEdges(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	writeMarketplaceV4(t, path, "sources:\n  public: {marketplace: https://market.example}\nresources:\n  public: [collections/collection/alpha, collections/collection/beta, leaf/skill/review]\n")
	evidence := selectionGraphEvidence()
	before := runtimeSelectionPins(t, path, evidence)
	if !before[2].Direct || len(before[2].SelectedBy.List()) != 2 {
		t.Fatalf("initial graph = %#v", before)
	}
	newDigest := "sha256:" + strings.Repeat("c", 64)
	updatedRoot := selectionTestPin("collections", "collection", "alpha", newDigest)
	if _, _, err := commitMarketplaceEdit(path, RepoCommitted, func(editor *LayerEditor) error {
		binding, err := editor.EnsureMarketplaceSource("https://market.example")
		if err != nil {
			return err
		}
		if err := editor.RemoveMarketplacePins(binding, []MarketplaceResourcePin{before[0]}); err != nil {
			return err
		}
		return editor.IncludeMarketplacePins(binding, []MarketplaceResourcePin{updatedRoot})
	}); err != nil {
		t.Fatal(err)
	}
	evidence.Marketplaces[0].Resources[0].Revision = newDigest
	evidence.Marketplaces[0].Resources[0].ContentDigest = lockfile.Digest(newDigest)
	evidence.Marketplaces[0].Resources[0].CollectionManifestDigest = lockfile.Digest(newDigest)
	evidence.Marketplaces[0].Resources[2].SelectedBy[0].Revision = lockfile.Digest(newDigest)
	got := runtimeSelectionPins(t, path, evidence)[2]
	want := []MarketplaceSelection{{RegistryID: "collections", Name: "alpha", Revision: newDigest}, {RegistryID: "collections", Name: "beta", Revision: "sha256:" + strings.Repeat("b", 64)}}
	if !got.Direct || got.Digest != "sha256:"+strings.Repeat("b", 64) || !slices.Equal(got.SelectedBy.List(), want) {
		t.Fatalf("replacement lost independent selectors = %#v", got)
	}
}

func TestMarketplaceCollectionReplacementRejectsStaleExactEdges(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	writeMarketplaceV4(t, path, "sources:\n  public: {marketplace: https://market.example}\nresources:\n  public: [collections/collection/alpha, collections/collection/beta, leaf/skill/review]\n")
	before := marketplaceState(t, path)
	doc := selectionGraphEvidence()
	if got := runtimeSelectionPins(t, path, doc); len(got[2].SelectedBy.List()) != 2 {
		t.Fatalf("edge control = %#v", got)
	}
	doc.Marketplaces[0].Resources[2].SelectedBy[0].Revision = lockfile.Digest("sha256:" + strings.Repeat("c", 64))
	v4, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = v4.ToLegacyConfigWithLock(repoLayer, &doc)
	requireMarketplaceV4Code(t, err, "v4.marketplace-lock-mismatch")
	requireMarketplaceUnchanged(t, path, before)
}

func TestMarketplaceSelectionEvidenceDeduplicatesExactEdges(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	writeMarketplaceV4(t, path, "sources:\n  public: {marketplace: https://market.example}\nresources:\n  public: [collections/collection/alpha, collections/collection/beta]\n")
	doc := selectionGraphEvidence()
	doc.Marketplaces[0].Resources[2].SelectedBy = []lockfile.MarketplaceSelectionLock{
		{RegistryID: "collections", Name: "beta", Revision: lockfile.Digest("sha256:" + strings.Repeat("b", 64))},
		{RegistryID: "collections", Name: "alpha", Revision: lockfile.Digest("sha256:" + strings.Repeat("a", 64))},
		{RegistryID: "collections", Name: "beta", Revision: lockfile.Digest("sha256:" + strings.Repeat("b", 64))},
	}
	pins := runtimeSelectionPins(t, path, doc)
	want := []MarketplaceSelection{{RegistryID: "collections", Name: "alpha", Revision: "sha256:" + strings.Repeat("a", 64)}, {RegistryID: "collections", Name: "beta", Revision: "sha256:" + strings.Repeat("b", 64)}}
	if len(pins) != 3 || pins[2].Direct || !slices.Equal(pins[2].SelectedBy.List(), want) {
		t.Fatalf("normalized exact collection edges = %#v", pins)
	}
}

// spec: consumer-config-format/v4-fragments-preserve-include-authority
func TestMarketplaceSelectionEditsRetainIncludeProvenance(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	writeMarketplaceV4(t, path, "include: child.yaml\n")
	child := filepath.Join(filepath.Dir(path), "child.yaml")
	fragment := []byte("sources:\n  public: {marketplace: https://market.example}\nresources:\n  public: [source-slug/skill/review]\n")
	if err := os.WriteFile(child, fragment, 0o600); err != nil {
		t.Fatal(err)
	}
	rootBefore, childBefore := marketplaceState(t, path), marketplaceState(t, child)
	pin := testMarketplacePin()
	pin.Revision = pin.Digest
	_, _, err := commitMarketplaceEdit(path, RepoCommitted, removeMarketplaceRequests("https://market.example", pin))
	requireMarketplaceV4Code(t, err, "config.edit.include-owned")
	requireMarketplaceUnchanged(t, path, rootBefore)
	requireMarketplaceUnchanged(t, child, childBefore)
	pin.Name = "audit"
	if _, _, err := commitMarketplaceEdit(path, RepoCommitted, marketplaceIncludeEdit("HTTPS://MARKET.example/", pin)); err != nil {
		t.Fatal(err)
	}
	requireMarketplaceUnchanged(t, child, childBefore)
	v4, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(marketplaceRequestLocators(v4), []string{"source-slug/skill/audit", "source-slug/skill/review"}) {
		t.Fatalf("include/root selection = %#v", v4.Requests)
	}
	rootCanonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	childCanonical, err := filepath.EvalSymlinks(child)
	if err != nil {
		t.Fatal(err)
	}
	if len(v4.Requests[0].Provenance) != 1 || v4.Requests[0].Provenance[0].File != rootCanonical || len(v4.Requests[1].Provenance) != 1 || v4.Requests[1].Provenance[0].File != childCanonical || v4.Sources["public"].File != childCanonical {
		t.Fatalf("selection/source provenance = %#v/%#v", v4.Requests, v4.Sources)
	}
	// The lock stays at the logical root even though its source is included.
	doc := marketplaceTestEvidence()
	doc.Marketplaces[0].Resources[0].Revision = testMarketplaceDigest
	audit := doc.Marketplaces[0].Resources[0]
	audit.Name = "audit"
	doc.Marketplaces[0].Resources = append(doc.Marketplaces[0].Resources, audit)
	data, err := lockfile.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "lock.yaml"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Marketplaces) != 1 || len(cfg.Marketplaces[0].Resources) != 2 || cfg.Marketplaces[0].Source != RepoCommitted || cfg.Marketplaces[0].Resources[0].Name != "audit" || cfg.Marketplaces[0].Resources[1].Name != "review" {
		t.Fatalf("logical-root lock binding = %#v", cfg.Marketplaces)
	}
}

func TestMarketplaceCollectionReplacementRollbackPreservesConcurrentMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	writeMarketplaceV4(t, path, "sources:\n  public: {marketplace: https://market.example}\nresources:\n  public: [collections/collection/bundle]\n")
	before := marketplaceState(t, path)
	root := selectionTestPin("collections", "collection", "bundle", "sha256:"+strings.Repeat("a", 64))
	snapshot, commit, err := commitMarketplaceEdit(path, RepoCommitted, removeMarketplaceRequests("https://market.example", root))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(commit.Bytes, before.data) {
		t.Fatal("rollback control edit was a no-op")
	}
	concurrent := []byte("apiVersion: agent-marketplace/v4\n# concurrent selector\nsources:\n  public: {marketplace: https://market.example}\nresources:\n  public: [collections/collection/other]\n")
	if err := os.WriteFile(path, concurrent, 0o600); err != nil {
		t.Fatal(err)
	}
	concurrentState := marketplaceState(t, path)
	if err := snapshot.RestoreIfCurrent(commit); err == nil {
		t.Fatal("rollback overwrote a concurrent config")
	}
	requireMarketplaceUnchanged(t, path, concurrentState)
}
