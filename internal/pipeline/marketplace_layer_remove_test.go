package pipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

func layerCollectionPins(t *testing.T, fake *layerMarketplaceServer, skillName, collName string) []config.MarketplaceResourcePin {
	t.Helper()
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, skillName)
	member := fake.resources["source/skill/"+skillName]
	fake.addCollection(t, registry, collName, []hccartifact.CollectionMemberEntry{
		{Kind: "skill", Name: skillName, RegistryID: string(registry.ID), Revision: string(member.revision.ContentDigest)},
	})
	resolver := &marketplace.Resolver{}
	plan, err := resolver.PrepareLocator(context.Background(), config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID},
		marketplace.MarketplaceLocator{URL: fake.URL, Registry: "source", Kind: "collection", Name: collName}, nil)
	if err != nil {
		t.Fatalf("PrepareLocator: %v", err)
	}
	return marketplaceInstallPins(plan)
}

func layerPreparePinsIn(t *testing.T, fake *layerMarketplaceServer, registrySlug, kind, name string) []config.MarketplaceResourcePin {
	t.Helper()
	resolver := &marketplace.Resolver{}
	plan, err := resolver.PrepareLocator(context.Background(), config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID},
		marketplace.MarketplaceLocator{URL: fake.URL, Registry: registrySlug, Kind: kind, Name: name},
		[]hcc.Target{hcc.TargetDevin})
	if err != nil {
		t.Fatalf("PrepareLocator: %v", err)
	}
	return marketplaceInstallPins(plan)
}

func layerCollectionPin(t *testing.T, pins []config.MarketplaceResourcePin) config.MarketplaceResourcePin {
	t.Helper()
	for _, pin := range pins {
		if pin.Kind == "collection" {
			return pin
		}
	}
	t.Fatal("no collection pin")
	return config.MarketplaceResourcePin{}
}

func TestMutateMarketplaceLayerV4RemoveDirectPin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeFileMode(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n", 0o600)
	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "review")
	entry := config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}
	pins := layerPreparePins(t, fake, "skill", "review")

	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.IncludePins(entry, pins)
	}, nil); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.RemoveDirectPin(entry, pins[0])
	}, nil); err != nil {
		t.Fatalf("remove: %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "source/skill/review") {
		t.Fatalf("request survived removal:\n%s", data)
	}
	if !strings.Contains(string(data), "marketplace:") {
		t.Fatalf("endpoint source dropped:\n%s", data)
	}
	doc := layerMarketplaceLockDoc(t, lockPath)
	if len(doc.Marketplaces) != 1 || doc.Marketplaces[0].InstanceID != fake.instanceID {
		t.Fatalf("endpoint record = %#v", doc.Marketplaces)
	}
	if len(doc.Marketplaces[0].Resources) != 0 {
		t.Fatalf("stale lock resources = %#v", doc.Marketplaces[0].Resources)
	}
}

func TestMutateMarketplaceLayerV4RemoveDirectPinRetainsCollectionLeaf(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeFileMode(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n", 0o600)
	fake := newLayerMarketplaceServer(t)
	entry := config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}
	collectionPins := layerCollectionPins(t, fake, "review", "bundle")
	directPins := layerPreparePins(t, fake, "skill", "review")

	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		if err := m.IncludePins(entry, collectionPins); err != nil {
			return err
		}
		return m.IncludePins(entry, directPins)
	}, nil); err != nil {
		t.Fatalf("install: %v", err)
	}
	var merged config.MarketplaceResourcePin
	for _, pin := range collectionPins {
		if pin.Kind == "skill" && pin.Name == "review" {
			merged = pin
		}
	}
	if merged.Name == "" || len(merged.SelectedBy.List()) == 0 {
		t.Fatalf("collection member pin missing selection edge: %#v", collectionPins)
	}
	merged.Direct = true
	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.RemoveDirectPin(entry, merged)
	}, nil); err != nil {
		t.Fatalf("remove: %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "source/skill/review") {
		t.Fatalf("direct request survived:\n%s", data)
	}
	if !strings.Contains(string(data), "source/collection/bundle") {
		t.Fatalf("collection request lost:\n%s", data)
	}
	doc := layerMarketplaceLockDoc(t, lockPath)
	leaf := marketplaceLockResource(doc, "skill", "review")
	if leaf == nil || len(leaf.SelectedBy) != 1 || leaf.SelectedBy[0].Name != "bundle" {
		t.Fatalf("collection leaf evidence lost: %#v", doc.Marketplaces[0].Resources)
	}
}

func TestMutateMarketplaceLayerV4RemoveCollection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeFileMode(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n", 0o600)
	fake := newLayerMarketplaceServer(t)
	entry := config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}
	collectionPins := layerCollectionPins(t, fake, "review", "bundle")
	registry := fake.addRegistry(t, "tools")
	fake.addSkill(t, registry, "lint")
	lintPins := layerPreparePinsIn(t, fake, "tools", "skill", "lint")

	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		if err := m.IncludePins(entry, collectionPins); err != nil {
			return err
		}
		return m.IncludePins(entry, lintPins)
	}, nil); err != nil {
		t.Fatalf("install: %v", err)
	}
	commitOID, err := lockfile.ParseGitOID("sha1:" + strings.Repeat("f", 40))
	if err != nil {
		t.Fatal(err)
	}
	doc := layerMarketplaceLockDoc(t, lockPath)
	doc.Registries = append(doc.Registries, lockfile.GitRegistry{URL: "https://example.com/git-registry.git", Ref: "main", Commit: commitOID})
	lockData, err := lockfile.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	writeFileMode(t, lockPath, string(lockData), 0o644)

	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.RemoveCollection(entry, layerCollectionPin(t, collectionPins))
	}, nil); err != nil {
		t.Fatalf("remove: %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "collection/bundle") {
		t.Fatalf("collection request survived:\n%s", data)
	}
	if !strings.Contains(string(data), "tools/skill/lint") {
		t.Fatalf("unrelated request lost:\n%s", data)
	}
	parsed, err := config.ParseV4Config(data, configPath, true)
	if err != nil {
		t.Fatalf("parse layer after removal: %v", err)
	}
	for name, group := range parsed.Groups {
		if len(group.Exclude) != 0 {
			t.Fatalf("group %s recorded exclusions %v, want none for a Marketplace collection removal", name, group.Exclude)
		}
		for _, request := range group.Include {
			if request.Kind == "collection" {
				t.Fatalf("group %s still requests collection %s", name, request.Name)
			}
		}
	}
	doc = layerMarketplaceLockDoc(t, lockPath)
	if len(doc.Registries) != 1 {
		t.Fatalf("git records lost: %#v", doc.Registries)
	}
	if marketplaceLockResource(doc, "collection", "bundle") != nil || marketplaceLockResource(doc, "skill", "review") != nil {
		t.Fatalf("collection evidence retained: %#v", doc.Marketplaces[0].Resources)
	}
	if marketplaceLockResource(doc, "skill", "lint") == nil {
		t.Fatalf("unrelated resource pruned: %#v", doc.Marketplaces[0].Resources)
	}
	if len(doc.Marketplaces) != 1 || doc.Marketplaces[0].InstanceID != fake.instanceID {
		t.Fatalf("endpoint record = %#v", doc.Marketplaces)
	}
}

func TestMutateMarketplaceLayerV4RemoveIncludeOwnedRequest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "review")
	entry := config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}
	pins := layerPreparePins(t, fake, "skill", "review")

	fragment := filepath.Join(home, ".agents", "fragment.yaml")
	writeFileMode(t, fragment, "resources:\n  hosted:\n    include: [source/skill/review]\n", 0o600)
	writeFileMode(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\ninclude: [fragment.yaml]\nsources:\n  hosted: {marketplace: "+fake.URL+"}\n", 0o600)
	configBefore, _ := os.ReadFile(configPath)

	_, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.RemoveDirectPin(entry, pins[0])
	}, nil)
	if err == nil {
		t.Fatal("include-owned request removed")
	}
	var v4err *config.V4ValidationError
	if !errors.As(err, &v4err) || v4err.Code != "config.edit.include-owned" {
		t.Fatalf("error = %v", err)
	}
	if after, _ := os.ReadFile(configPath); string(after) != string(configBefore) {
		t.Fatal("include-owned refusal changed config")
	}
}

func TestMutateMarketplaceLayerV4RemoveMissingRequestFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	writeFileMode(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n", 0o600)
	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "review")
	entry := config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}
	pins := layerPreparePins(t, fake, "skill", "review")
	configBefore, _ := os.ReadFile(configPath)

	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.RemoveDirectPin(entry, pins[0])
	}, nil); err == nil {
		t.Fatal("remove of undeclared endpoint accepted")
	}
	if after, _ := os.ReadFile(configPath); string(after) != string(configBefore) {
		t.Fatal("missing-source refusal changed config")
	}
}

func TestMutateMarketplaceLayerV4RemoveSyncFailureRollback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeFileMode(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n", 0o600)
	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "review")
	entry := config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}
	pins := layerPreparePins(t, fake, "skill", "review")

	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.IncludePins(entry, pins)
	}, nil); err != nil {
		t.Fatalf("install: %v", err)
	}
	configBefore, _ := os.ReadFile(configPath)
	lockBefore, _ := os.ReadFile(lockPath)
	syncErr := errors.New("sync failed")
	_, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.RemoveDirectPin(entry, pins[0])
	}, func() ([]error, error) { return nil, syncErr })
	if !errors.Is(err, syncErr) {
		t.Fatalf("remove error = %v", err)
	}
	if after, _ := os.ReadFile(configPath); string(after) != string(configBefore) {
		t.Fatal("sync failure left mutated config")
	}
	if after, _ := os.ReadFile(lockPath); string(after) != string(lockBefore) {
		t.Fatal("sync failure left mutated lock")
	}
}
