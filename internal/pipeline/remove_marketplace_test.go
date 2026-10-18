package pipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestRemoveMarketplaceCollectionRollbackRestoresExactSelectors(t *testing.T) {
	entry, _ := prepareRollbackMarketplace(t)
	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "review")
	fake.addCollection(t, registry, "bundle", []hccartifact.CollectionMemberEntry{{RegistryID: string(registry.ID), Kind: "skill", Name: "review", Revision: string(fake.resources["source/skill/review"].revision.ContentDigest)}})
	entry.URL, entry.InstanceID = fake.URL, fake.instanceID
	pins := layerPreparePins(t, fake, "collection", "bundle")
	collection := pins[0]
	repoRoot := t.TempDir()
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	if _, err := mutateRollbackMarketplace(configPath, entry, pins, nil); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = MutateMarketplaceLayer(context.Background(), repoRoot, repoRoot, configPath, config.RepoCommitted, func(m *MarketplaceLayerMutation) error {
		return m.RemoveCollection(entry, collection)
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
	isolateConsumerPipeline(t)
	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "canonical")
	fake.addSkill(t, registry, "review-copy")
	pins := layerPreparePins(t, fake, "skill", "canonical")
	pins[0].InstalledName = "review-copy"
	pins = append(pins, layerPreparePins(t, fake, "skill", "review-copy")...)
	repo := t.TempDir()
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	if _, err := mutateRollbackMarketplace(configPath, config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}, pins, nil); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
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
	if got, _, ok := marketplaceCollectionOnlyPinByName(cfg, "shared", ir.KindSkill, true); !ok || got.Kind != "skill" {
		t.Fatalf("collection-only skill lookup = %#v, %v", got, ok)
	}
	if _, _, ok := marketplaceCollectionOnlyPinByName(cfg, "shared", ir.KindCommand, true); ok {
		t.Fatal("collection-only skill matched --kind command")
	}
}
