package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureMarketplacePinsPersistsCollectionAndLeafSelectorsAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	collectionDigest := "sha256:" + strings.Repeat("a", 64)
	leafDigest := "sha256:" + strings.Repeat("b", 64)
	pins := []MarketplaceResourcePin{
		{RegistryID: "src", Kind: "collection", Name: "bundle", Revision: collectionDigest, Digest: collectionDigest, Acquisition: MarketplaceAcquisitionMirror},
		{RegistryID: "src", Kind: "skill", Name: "review", Revision: leafDigest, Digest: leafDigest, Acquisition: MarketplaceAcquisitionMirror, SelectedBy: MarketplaceSelectionsFrom([]MarketplaceSelection{{RegistryID: "src", Name: "bundle", Revision: collectionDigest}})},
	}
	if _, _, err := EnsureMarketplacePinsInConfigWithSnapshotAndCommit(path, "https://market.example/", "mp", "public", pins); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Marketplaces) != 1 || len(loaded.Marketplaces[0].Resources) != 2 {
		t.Fatalf("loaded marketplaces = %#v", loaded.Marketplaces)
	}
	if got := loaded.Marketplaces[0].Resources[1].SelectedBy.List(); len(got) != 1 || got[0].Name != "bundle" || got[0].Revision != collectionDigest {
		t.Fatalf("selected_by = %#v", got)
	}
}

func TestEnsureMarketplacePinsRejectsConflictingIncomingDuplicateBeforeWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	original := []byte("apiVersion: agent-marketplace/v2\nmarketplaces: []\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	digestA := "sha256:" + strings.Repeat("a", 64)
	digestB := "sha256:" + strings.Repeat("b", 64)
	pins := []MarketplaceResourcePin{
		{RegistryID: "src", Kind: "skill", Name: "review", Revision: digestA, Digest: digestA, Acquisition: MarketplaceAcquisitionMirror},
		{RegistryID: "src", Kind: "skill", Name: "review", Revision: digestB, Digest: digestB, Acquisition: MarketplaceAcquisitionMirror},
	}
	if _, _, err := EnsureMarketplacePinsInConfigWithSnapshotAndCommit(path, "https://market.example", "mp", "public", pins); err == nil || !strings.Contains(err.Error(), "conflicting duplicate") {
		t.Fatalf("expected incoming duplicate conflict, got %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("config changed after incoming duplicate conflict: %q", got)
	}
}

func TestEnsureMarketplacePinsDoesNotWriteAfterLaterPinConflict(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	original := []byte("apiVersion: agent-marketplace/v2\nmarketplaces: []\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	existing := MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "one", Revision: digest, Digest: digest, Acquisition: MarketplaceAcquisitionMirror}
	if err := EnsureMarketplacePinInConfig(path, "https://market.example", "mp", "public", existing); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pins := []MarketplaceResourcePin{
		{RegistryID: "src", Kind: "skill", Name: "new", Revision: digest, Digest: digest, Acquisition: MarketplaceAcquisitionMirror},
		{RegistryID: "src", Kind: "skill", Name: "one", Revision: "sha256:" + strings.Repeat("c", 64), Digest: "sha256:" + strings.Repeat("d", 64), Acquisition: MarketplaceAcquisitionMirror},
	}
	if _, _, err := EnsureMarketplacePinsInConfigWithSnapshotAndCommit(path, "https://market.example", "mp", "public", pins); err == nil {
		t.Fatal("expected conflicting pin error")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(before) {
		t.Fatalf("config changed after failed batch: %q", got)
	}
}
