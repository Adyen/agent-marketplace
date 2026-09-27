package pipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestRemoveMarketplaceCollectionRollbackRestoresExactSelectors(t *testing.T) {
	repoRoot := t.TempDir()
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	digest := "sha256:" + strings.Repeat("a", 64)
	collection := config.MarketplaceResourcePin{RegistryID: "collections", Kind: "collection", Name: "bundle", Revision: digest, Digest: digest, Acquisition: config.MarketplaceAcquisitionMirror}
	leaf := config.MarketplaceResourcePin{RegistryID: "source", Kind: "skill", Name: "review", Revision: digest, Digest: digest, Acquisition: config.MarketplaceAcquisitionMirror, SelectedBy: config.MarketplaceSelectionsFrom([]config.MarketplaceSelection{{RegistryID: collection.RegistryID, Name: collection.Name, Revision: collection.Revision}})}
	if _, _, err := config.EnsureMarketplacePinsInConfigWithSnapshotAndCommit(configPath, "https://market.example", "mp", "public", []config.MarketplaceResourcePin{collection, leaf}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = commitMarketplaceConfigRemoval(configPath, func() (*config.FileSnapshot, config.ConfigFileCommit, error) {
		return config.RemoveMarketplaceCollectionWithSnapshotAndCommit(configPath, "https://market.example", "mp", collection)
	}, func() ([]error, error) {
		return nil, ctx.Err()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("remove error = %v, want cancellation", err)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("failed removal changed exact config selectors:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestMarketplaceRemoveKindDisambiguatesCollectionAndResource(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{Resources: []config.MarketplaceResourcePin{
		{RegistryID: "src", Kind: "collection", Name: "shared", Revision: digest, Digest: digest},
		{RegistryID: "src", Kind: "skill", Name: "shared", Revision: digest, Digest: digest, Direct: true},
	}}}}
	collection, _, ok := marketplaceCollectionByName(cfg, "shared", ir.KindSkill, false)
	if !ok || collection.Kind != "collection" {
		t.Fatalf("unfiltered lookup = %#v, want collection", collection)
	}
	if _, _, ok := marketplaceCollectionByName(cfg, "shared", ir.KindSkill, true); ok {
		t.Fatal("skill kind selected collection")
	}
	pin, _, ok := marketplaceDirectPinByName(cfg, "shared", ir.KindSkill, true)
	if !ok || pin.Kind != "skill" {
		t.Fatalf("skill lookup = %#v, want direct skill", pin)
	}
}

func TestMarketplaceRemoveFindsDirectPinByInstalledNameWithoutAmbiguity(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	aliased := config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "canonical", InstalledName: "review-copy", Revision: digest, Digest: digest, Direct: true}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{Resources: []config.MarketplaceResourcePin{aliased}}}}
	for _, name := range []string{"review-copy", "canonical"} {
		if got, _, ok := marketplaceDirectPinByName(cfg, name, ir.KindSkill, true); !ok || got.Name != "canonical" {
			t.Fatalf("lookup %q = %#v, %v", name, got, ok)
		}
	}
	other := config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review-copy", Revision: digest, Digest: digest, Direct: true}
	cfg.Marketplaces[0].Resources = append(cfg.Marketplaces[0].Resources, other)
	if got, _, ok := marketplaceDirectPinByName(cfg, "review-copy", ir.KindSkill, true); ok {
		t.Fatalf("ambiguous local/canonical name selected %#v", got)
	}
	repo := t.TempDir()
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	if _, _, err := config.EnsureMarketplacePinsInConfigWithSnapshotAndCommit(configPath, "https://market.example", "mp", "public", cfg.Marketplaces[0].Resources); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	config.SetTestOrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
	if _, err := Remove(context.Background(), RemoveOptions{RepoRoot: repo, ResourceName: "review-copy", Kind: ir.KindSkill, HasKind: true}); err == nil || !strings.Contains(err.Error(), "matches 2 Marketplace pins") {
		t.Fatalf("ambiguous removal error = %v", err)
	}
	if after, err := os.ReadFile(configPath); err != nil || string(after) != string(before) {
		t.Fatalf("ambiguous removal changed config: %q, %v", after, err)
	}
}

func TestMarketplaceRemoveRejectsCollectionOnlyLeafAndHonorsKind(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	collection := config.MarketplaceResourcePin{RegistryID: "src", Kind: "collection", Name: "bundle", Revision: digest, Digest: digest}
	leaf := config.MarketplaceResourcePin{
		RegistryID: "src", Kind: "skill", Name: "shared", Revision: digest, Digest: digest,
		SelectedBy: config.MarketplaceSelectionsFrom([]config.MarketplaceSelection{{RegistryID: collection.RegistryID, Name: collection.Name, Revision: collection.Revision}}),
	}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{Resources: []config.MarketplaceResourcePin{collection, leaf}}}}
	if got, ok := marketplaceCollectionOnlyPinByName(cfg, "shared", ir.KindSkill, true); !ok || got.Kind != "skill" {
		t.Fatalf("collection-only skill lookup = %#v, %v", got, ok)
	}
	if _, ok := marketplaceCollectionOnlyPinByName(cfg, "shared", ir.KindCommand, true); ok {
		t.Fatal("collection-only skill matched --kind command")
	}
}
