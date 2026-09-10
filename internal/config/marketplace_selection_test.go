package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func selectionTestPin(source, kind, name, digest string) MarketplaceResourcePin {
	return MarketplaceResourcePin{RegistryID: source, Kind: kind, Name: name, Revision: digest, Digest: digest, Acquisition: MarketplaceAcquisitionMirror}
}

func TestMarketplaceSelectionMutationPreservesDirectAndOtherCollections(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	a := "sha256:" + strings.Repeat("a", 64)
	b := "sha256:" + strings.Repeat("b", 64)
	leaf := selectionTestPin("leaf", "skill", "review", b)
	collectionA := selectionTestPin("collections", "collection", "alpha", a)
	collectionB := selectionTestPin("collections", "collection", "beta", b)
	leaf.Direct = true
	leaf.SelectedBy = MarketplaceSelectionsFrom([]MarketplaceSelection{marketplaceCollectionSelection(collectionA), marketplaceCollectionSelection(collectionB)})
	if _, _, err := EnsureMarketplacePinsInConfigWithSnapshotAndCommit(path, "https://market.example", "mp", "public", []MarketplaceResourcePin{collectionA, collectionB, leaf}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := RemoveMarketplaceCollectionWithSnapshotAndCommit(path, "https://market.example", "mp", collectionA); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Marketplaces[0].Resources) != 2 {
		t.Fatalf("resources after collection removal = %#v", cfg.Marketplaces[0].Resources)
	}
	got := cfg.Marketplaces[0].Resources[1]
	if !got.Direct || len(got.SelectedBy.List()) != 1 || got.SelectedBy.List()[0].Name != "beta" {
		t.Fatalf("leaf selectors after collection removal = %#v", got)
	}
	if _, _, err := RemoveMarketplaceDirectPinWithSnapshotAndCommit(path, "https://market.example", "mp", got); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Marketplaces[0].Resources) != 2 || cfg.Marketplaces[0].Resources[1].Direct {
		t.Fatalf("direct removal removed unrelated collection selector: %#v", cfg.Marketplaces[0].Resources)
	}
	if _, _, err := RemoveMarketplaceCollectionWithSnapshotAndCommit(path, "https://market.example", "mp", collectionB); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Marketplaces[0].Resources) != 0 {
		t.Fatalf("leaf remained after final selector removal: %#v", cfg.Marketplaces[0].Resources)
	}
}

func TestMarketplaceCollectionReplacementReplacesEdgesInOneCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	oldDigest := "sha256:" + strings.Repeat("a", 64)
	newDigest := "sha256:" + strings.Repeat("b", 64)
	leafOldDigest := "sha256:" + strings.Repeat("c", 64)
	leafNewDigest := "sha256:" + strings.Repeat("d", 64)
	oldCollection := selectionTestPin("collections", "collection", "bundle", oldDigest)
	oldLeaf := selectionTestPin("source", "skill", "review", leafOldDigest)
	oldLeaf.SelectedBy = MarketplaceSelectionsFrom([]MarketplaceSelection{marketplaceCollectionSelection(oldCollection)})
	other := selectionTestPin("source", "command", "lint", leafOldDigest)
	newCollection := selectionTestPin("collections", "collection", "bundle", newDigest)
	newLeaf := selectionTestPin("source", "skill", "review", leafNewDigest)
	newLeaf.SelectedBy = MarketplaceSelectionsFrom([]MarketplaceSelection{marketplaceCollectionSelection(newCollection)})
	if _, _, err := EnsureMarketplacePinsInConfigWithSnapshotAndCommit(path, "https://market.example", "mp", "public", []MarketplaceResourcePin{oldCollection, oldLeaf, other}); err != nil {
		t.Fatal(err)
	}
	snapshot, commit, err := EnsureMarketplaceCollectionWithSnapshotAndCommit(path, "https://market.example", "mp", oldCollection, []MarketplaceResourcePin{newCollection, newLeaf})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot == nil || commit.Generation == "" {
		t.Fatalf("missing collection mutation CAS state: %#v/%#v", snapshot, commit)
	}
	cfg, err := LoadConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	resources := cfg.Marketplaces[0].Resources
	if len(resources) != 3 {
		t.Fatalf("replacement did not retain unrelated pin: %#v", resources)
	}
	var foundOther bool
	for _, pin := range resources {
		if pin.Name == "lint" {
			foundOther = true
		}
	}
	if !foundOther {
		t.Fatalf("replacement did not retain unrelated pin: %#v", resources)
	}
	var foundLeaf, foundCollection bool
	for _, pin := range resources {
		if pin.Kind == "collection" && pin.Revision == newDigest {
			foundCollection = true
		}
		if pin.Kind == "skill" && pin.Name == "review" && pin.Revision == leafNewDigest && len(pin.SelectedBy.List()) == 1 && pin.SelectedBy.List()[0].Revision == newDigest {
			foundLeaf = true
		}
	}
	if !foundCollection || !foundLeaf {
		t.Fatalf("replacement pins = %#v", resources)
	}
	if err := snapshot.RestoreIfCurrent(commit); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Marketplaces[0].Resources[0].Revision != oldDigest {
		t.Fatalf("rollback did not restore old collection: %#v", cfg.Marketplaces[0].Resources)
	}
}

func TestMarketplaceCollectionRemovalDoesNotSkipAdjacentLeaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	digest := "sha256:" + strings.Repeat("a", 64)
	collection := selectionTestPin("collections", "collection", "bundle", digest)
	leaves := []MarketplaceResourcePin{
		selectionTestPin("source", "skill", "one", digest),
		selectionTestPin("source", "skill", "two", digest),
		selectionTestPin("source", "skill", "three", digest),
	}
	for i := range leaves {
		leaves[i].SelectedBy = MarketplaceSelectionsFrom([]MarketplaceSelection{marketplaceCollectionSelection(collection)})
	}
	pins := append([]MarketplaceResourcePin{collection}, leaves...)
	if _, _, err := EnsureMarketplacePinsInConfigWithSnapshotAndCommit(path, "https://market.example", "mp", "public", pins); err != nil {
		t.Fatal(err)
	}
	if _, _, err := RemoveMarketplaceCollectionWithSnapshotAndCommit(path, "https://market.example", "mp", collection); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Marketplaces[0].Resources; len(got) != 0 {
		t.Fatalf("adjacent collection leaves remained after removal: %#v", got)
	}
}

func TestMarketplaceCollectionMutationRejectsDuplicateRoots(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	collection := selectionTestPin("collections", "collection", "bundle", digest)
	data := []byte("apiVersion: agent-marketplace/v2\nmarketplaces:\n  - url: https://market.example\n    instance_id: mp\n    resources:\n      - registry_id: collections\n        kind: collection\n        name: bundle\n        revision: " + digest + "\n        digest: " + digest + "\n        acquisition: mirror\n      - registry_id: collections\n        kind: collection\n        name: bundle\n        revision: " + digest + "\n        digest: " + digest + "\n        acquisition: mirror\n")
	for name, mutate := range map[string]func(string, MarketplaceResourcePin) error{
		"remove": func(path string, pin MarketplaceResourcePin) error {
			_, _, err := RemoveMarketplaceCollectionWithSnapshotAndCommit(path, "https://market.example", "mp", pin)
			return err
		},
		"replace": func(path string, pin MarketplaceResourcePin) error {
			_, _, err := EnsureMarketplaceCollectionWithSnapshotAndCommit(path, "https://market.example", "mp", pin, []MarketplaceResourcePin{pin})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := mutate(path, collection); err == nil || !strings.Contains(err.Error(), "duplicate Marketplace collection roots") {
				t.Fatalf("mutation error = %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatalf("duplicate-root mutation changed config")
			}
		})
	}
}

func TestMarketplaceCollectionReplacementRequiresRootIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	digest := "sha256:" + strings.Repeat("a", 64)
	oldCollection := selectionTestPin("collections", "collection", "bundle", digest)
	if _, _, err := EnsureMarketplacePinsInConfigWithSnapshotAndCommit(path, "https://market.example", "mp", "public", []MarketplaceResourcePin{oldCollection}); err != nil {
		t.Fatal(err)
	}
	replacement := selectionTestPin("other-source", "collection", "different-bundle", digest)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := EnsureMarketplaceCollectionWithSnapshotAndCommit(path, "https://market.example", "mp", oldCollection, []MarketplaceResourcePin{replacement}); err == nil || !strings.Contains(err.Error(), "root identity") {
		t.Fatalf("replacement error = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("identity-rejected replacement changed config")
	}
}

func TestMarketplaceCollectionReplacementRejectsIndependentContentConflicts(t *testing.T) {
	digest := func(char string) string { return "sha256:" + strings.Repeat(char, 64) }
	for _, test := range []struct {
		name     string
		makeLeaf func(MarketplaceResourcePin) MarketplaceResourcePin
	}{
		{
			name: "direct pin",
			makeLeaf: func(leaf MarketplaceResourcePin) MarketplaceResourcePin {
				leaf.Direct = true
				return leaf
			},
		},
		{
			name: "other collection",
			makeLeaf: func(leaf MarketplaceResourcePin) MarketplaceResourcePin {
				other := selectionTestPin("collections", "collection", "other", digest("e"))
				leaf.SelectedBy = MarketplaceSelectionsFrom([]MarketplaceSelection{marketplaceCollectionSelection(other)})
				return leaf
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
			oldCollection := selectionTestPin("collections", "collection", "bundle", digest("a"))
			leaf := test.makeLeaf(selectionTestPin("source", "skill", "review", digest("c")))
			newCollection := selectionTestPin("collections", "collection", "bundle", digest("b"))
			newLeaf := selectionTestPin("source", "skill", "review", digest("d"))
			if _, _, err := EnsureMarketplacePinsInConfigWithSnapshotAndCommit(path, "https://market.example", "mp", "public", []MarketplaceResourcePin{oldCollection, leaf}); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := EnsureMarketplaceCollectionWithSnapshotAndCommit(path, "https://market.example", "mp", oldCollection, []MarketplaceResourcePin{newCollection, newLeaf}); err == nil || !strings.Contains(err.Error(), "conflicts with another selection") {
				t.Fatalf("replacement error = %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatalf("conflicting replacement changed config")
			}
		})
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
	oldDigest := "sha256:" + strings.Repeat("a", 64)
	newDigest := "sha256:" + strings.Repeat("b", 64)
	leafDigest := "sha256:" + strings.Repeat("c", 64)
	oldCollection := selectionTestPin("collections", "collection", "bundle", oldDigest)
	otherCollection := selectionTestPin("collections", "collection", "other", oldDigest)
	leaf := selectionTestPin("source", "skill", "review", leafDigest)
	leaf.Direct = true
	leaf.SelectedBy = MarketplaceSelectionsFrom([]MarketplaceSelection{marketplaceCollectionSelection(oldCollection), marketplaceCollectionSelection(otherCollection)})
	newCollection := selectionTestPin("collections", "collection", "bundle", newDigest)
	newLeaf := selectionTestPin("source", "skill", "review", leafDigest)
	if _, _, err := EnsureMarketplacePinsInConfigWithSnapshotAndCommit(path, "https://market.example", "mp", "public", []MarketplaceResourcePin{oldCollection, otherCollection, leaf}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := EnsureMarketplaceCollectionWithSnapshotAndCommit(path, "https://market.example", "mp", oldCollection, []MarketplaceResourcePin{newCollection, newLeaf}); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	var got MarketplaceResourcePin
	for _, pin := range loaded.Marketplaces[0].Resources {
		if pin.Kind == "skill" && pin.Name == "review" {
			got = pin
		}
	}
	selectors := got.SelectedBy.List()
	if !got.Direct || len(selectors) != 2 || selectors[0].Name != "other" || selectors[1].Name != "bundle" || selectors[1].Revision != newDigest {
		t.Fatalf("replacement did not preserve direct/other selectors: %#v", got)
	}
}

func TestMarketplaceCollectionReplacementRejectsStaleCASIncludingEdges(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	digest := "sha256:" + strings.Repeat("a", 64)
	collection := selectionTestPin("collections", "collection", "bundle", digest)
	if _, _, err := EnsureMarketplacePinsInConfigWithSnapshotAndCommit(path, "https://market.example", "mp", "public", []MarketplaceResourcePin{collection}); err != nil {
		t.Fatal(err)
	}
	stale := collection
	stale.Direct = true
	if _, _, err := EnsureMarketplaceCollectionWithSnapshotAndCommit(path, "https://market.example", "mp", stale, []MarketplaceResourcePin{collection}); err == nil || !strings.Contains(err.Error(), "compare-and-swap") {
		t.Fatalf("stale CAS error = %v", err)
	}
}

func TestMarketplaceCollectionReplacementRollbackPreservesConcurrentMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	digest := "sha256:" + strings.Repeat("a", 64)
	collection := selectionTestPin("collections", "collection", "bundle", digest)
	if _, _, err := EnsureMarketplacePinsInConfigWithSnapshotAndCommit(path, "https://market.example", "mp", "public", []MarketplaceResourcePin{collection}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, commit, err := EnsureMarketplaceCollectionWithSnapshotAndCommit(path, "https://market.example", "mp", collection, []MarketplaceResourcePin{collection})
	if err != nil {
		t.Fatal(err)
	}
	concurrent := []byte("apiVersion: agent-marketplace/v2\n# concurrent selector\n")
	if err := os.WriteFile(path, concurrent, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.RestoreIfCurrent(commit); err == nil {
		t.Fatal("rollback unexpectedly overwrote concurrent config")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(concurrent) || string(got) == string(before) {
		t.Fatalf("concurrent config after guarded rollback = %q", got)
	}
}
