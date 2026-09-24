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
	"github.com/Adyen/agent-marketplace/internal/marketplacelock"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

func installV4MarketplaceLayer(t *testing.T, repo, configPath string, entry config.MarketplaceEntry, pins ...[]config.MarketplaceResourcePin) {
	t.Helper()
	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		for _, batch := range pins {
			if err := m.IncludePins(entry, batch); err != nil {
				return err
			}
		}
		return nil
	}, nil); err != nil {
		t.Fatalf("install: %v", err)
	}
}

func TestUpdateMarketplaceV4ReplacesLockEvidence(t *testing.T) {
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
	installV4MarketplaceLayer(t, repo, configPath, entry, layerPreparePins(t, fake, "skill", "review"))

	oldDoc := layerMarketplaceLockDoc(t, lockPath)
	oldRecord := marketplaceLockResource(oldDoc, "skill", "review")
	if oldRecord == nil {
		t.Fatal("installed lock missing review record")
	}
	configBytes, configStat := layerFreezeFile(t, configPath)
	fake.setSkill(t, registry, "review", "review body v2")

	result, err := Update(context.Background(), UpdateOptions{RepoRoot: repo, DependencyReconcile: testMarketplaceUpdateReconcile})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if result.UpdatedMarketplacePins != 1 || result.SkippedMarketplacePins != 0 {
		t.Fatalf("Marketplace result = %#v, want one update", result)
	}
	layerAssertFileUnchanged(t, configPath, configBytes, configStat)
	doc := layerMarketplaceLockDoc(t, lockPath)
	record := marketplaceLockResource(doc, "skill", "review")
	if record == nil {
		t.Fatalf("updated lock resources = %#v", doc.Marketplaces[0].Resources)
	}
	if record.Revision == oldRecord.Revision || string(record.ContentDigest) == string(oldRecord.ContentDigest) {
		t.Fatalf("lock evidence did not advance: %#v", record)
	}
	latest := fake.resources["source/skill/review"].revision
	if string(record.ContentDigest) != string(latest.ContentDigest) {
		t.Fatalf("lock content digest = %q, want latest %q", record.ContentDigest, latest.ContentDigest)
	}
}

func TestUpdateMarketplaceV4SkipsExplicitRevision(t *testing.T) {
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
	installV4MarketplaceLayer(t, repo, configPath, entry, layerPreparePins(t, fake, "skill", "review"))

	doc := layerMarketplaceLockDoc(t, lockPath)
	record := marketplaceLockResource(doc, "skill", "review")
	if record == nil {
		t.Fatal("installed lock missing review record")
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	constrained := strings.Replace(string(data), "source/skill/review", "source/skill/review: {revision: "+record.Revision+"}", 1)
	if constrained == string(data) {
		t.Fatalf("config request not found:\n%s", data)
	}
	writeFileMode(t, configPath, constrained, 0o600)
	configBytes, configStat := layerFreezeFile(t, configPath)
	lockBytes, lockStat := layerFreezeFile(t, lockPath)
	fake.setSkill(t, registry, "review", "review body v2")

	result, err := Update(context.Background(), UpdateOptions{RepoRoot: repo, DependencyReconcile: testMarketplaceUpdateReconcile})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if result.UpdatedMarketplacePins != 0 || result.SkippedMarketplacePins != 1 {
		t.Fatalf("Marketplace result = %#v, want one skip", result)
	}
	if result.MarketplacePins[0].Reason != "request pins an explicit revision" {
		t.Fatalf("skip reason = %q", result.MarketplacePins[0].Reason)
	}
	layerAssertFileUnchanged(t, configPath, configBytes, configStat)
	layerAssertFileUnchanged(t, lockPath, lockBytes, lockStat)
}

func TestUpdateMarketplaceV4CollectionUpdate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeFileMode(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n", 0o600)
	fake := newLayerMarketplaceServer(t)
	entry := config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}
	installV4MarketplaceLayer(t, repo, configPath, entry, layerCollectionPins(t, fake, "review", "bundle"))

	oldDoc := layerMarketplaceLockDoc(t, lockPath)
	oldCollection := marketplaceLockResource(oldDoc, "collection", "bundle")
	if oldCollection == nil {
		t.Fatal("installed lock missing collection record")
	}
	configBytes, configStat := layerFreezeFile(t, configPath)

	registry := fake.registries["source"]
	fake.setSkill(t, registry, "review", "review body v2")
	member := fake.resources["source/skill/review"].revision
	fake.addCollection(t, registry, "bundle", []hccartifact.CollectionMemberEntry{
		{Kind: "skill", Name: "review", RegistryID: string(registry.ID), Revision: string(member.ContentDigest)},
	})

	result, err := Update(context.Background(), UpdateOptions{RepoRoot: repo, DependencyReconcile: testMarketplaceUpdateReconcile})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if result.UpdatedMarketplacePins != 1 {
		t.Fatalf("Marketplace result = %#v, want one collection update", result)
	}
	layerAssertFileUnchanged(t, configPath, configBytes, configStat)
	doc := layerMarketplaceLockDoc(t, lockPath)
	collection := marketplaceLockResource(doc, "collection", "bundle")
	if collection == nil || string(collection.ContentDigest) == string(oldCollection.ContentDigest) {
		t.Fatalf("collection lock record = %#v", collection)
	}
	leaf := marketplaceLockResource(doc, "skill", "review")
	if leaf == nil || len(leaf.SelectedBy) != 1 || leaf.SelectedBy[0].Name != "bundle" || string(leaf.SelectedBy[0].Revision) != collection.Revision {
		t.Fatalf("leaf selection edges = %#v", leaf)
	}
	if string(leaf.ContentDigest) != string(member.ContentDigest) {
		t.Fatalf("leaf content digest = %q, want %q", leaf.ContentDigest, member.ContentDigest)
	}
}

func TestUpdateMarketplaceV4ComposesMultiplePlansOnOneLock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeFileMode(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n", 0o600)
	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "one")
	fake.addSkill(t, registry, "two")
	entry := config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}
	installV4MarketplaceLayer(t, repo, configPath, entry,
		layerPreparePins(t, fake, "skill", "one"), layerPreparePins(t, fake, "skill", "two"))

	fake.setSkill(t, registry, "one", "one body v2")
	fake.setSkill(t, registry, "two", "two body v2")
	result, err := Update(context.Background(), UpdateOptions{RepoRoot: repo, DependencyReconcile: testMarketplaceUpdateReconcile})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if result.UpdatedMarketplacePins != 2 {
		t.Fatalf("Marketplace result = %#v, want two updates", result)
	}
	doc := layerMarketplaceLockDoc(t, lockPath)
	for _, name := range []string{"one", "two"} {
		record := marketplaceLockResource(doc, "skill", name)
		latest := fake.resources["source/skill/"+name].revision
		if record == nil || string(record.ContentDigest) != string(latest.ContentDigest) {
			t.Fatalf("lock record for %q = %#v", name, record)
		}
	}
}

func TestReplacePinsToV4RejectsStaleExpectedAndPreservesUnrelated(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	writeFileMode(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n", 0o600)
	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "review")
	entry := config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}
	installV4MarketplaceLayer(t, repo, configPath, entry, layerPreparePins(t, fake, "skill", "review"))

	v4Cfg, err := config.LoadV4ConfigIfPresent(configPath, config.UserPersonal)
	if err != nil || v4Cfg == nil {
		t.Fatalf("load v4: %v", err)
	}
	doc := layerMarketplaceLockDoc(t, filepath.Join(home, ".agents", "lock.yaml"))
	oid, err := lockfile.ParseGitOID("sha256:" + strings.Repeat("e", 64))
	if err != nil {
		t.Fatal(err)
	}
	doc.Registries = append(doc.Registries, lockfile.GitRegistry{URL: "https://example.com/r.git", Ref: "main", Commit: oid})
	oldPin := layerPreparePins(t, fake, "skill", "review")[0]

	fake.setSkill(t, registry, "review", "review body v2")
	newPin := layerPreparePins(t, fake, "skill", "review")[0]

	staleDoc := doc
	staleDoc.Marketplaces = append([]lockfile.MarketplaceLock(nil), doc.Marketplaces...)
	staleDoc.Marketplaces[0].Resources = append([]lockfile.MarketplaceResourceLock(nil), doc.Marketplaces[0].Resources...)
	staleDoc.Marketplaces[0].Resources[0].Revision = "sha256:" + strings.Repeat("f", 64)
	before, err := lockfile.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := marketplacelock.ReplacePinsToV4(v4Cfg, staleDoc, entry, oldPin, []config.MarketplaceResourcePin{newPin}, marketplace.DefaultCache()); err == nil {
		t.Fatal("stale lock evidence accepted")
	} else {
		var v4err *config.V4ValidationError
		if !errors.As(err, &v4err) || v4err.Code != "v4.marketplace-lock-mismatch" {
			t.Fatalf("stale error = %v", err)
		}
	}
	if _, err := marketplacelock.ReplacePinsToV4(v4Cfg, doc, entry, oldPin, []config.MarketplaceResourcePin{newPin}, nil); err == nil {
		t.Fatal("nil cache accepted")
	}
	if after, _ := lockfile.Encode(doc); string(after) != string(before) {
		t.Fatal("failed replace mutated the input document")
	}

	merged, err := marketplacelock.ReplacePinsToV4(v4Cfg, doc, entry, oldPin, []config.MarketplaceResourcePin{newPin}, marketplace.DefaultCache())
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if after, _ := lockfile.Encode(doc); string(after) != string(before) {
		t.Fatal("successful replace mutated the input document")
	}
	if len(merged.Registries) != 1 || merged.Registries[0].Commit != oid {
		t.Fatalf("git records lost: %#v", merged.Registries)
	}
	record := marketplaceLockResource(merged, "skill", "review")
	if record == nil || string(record.ContentDigest) != string(newPin.Digest) {
		t.Fatalf("replaced record = %#v", record)
	}
}

func TestUpdateMarketplaceV4SkipsExplicitCollectionRevision(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeFileMode(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n", 0o600)
	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "review")
	fake.addSkill(t, registry, "lint")
	fake.addCollection(t, registry, "bundle", []hccartifact.CollectionMemberEntry{
		{Kind: "skill", Name: "review", RegistryID: string(registry.ID), Revision: string(fake.resources["source/skill/review"].revision.ContentDigest)},
		{Kind: "skill", Name: "lint", RegistryID: string(registry.ID), Revision: string(fake.resources["source/skill/lint"].revision.ContentDigest)},
	})
	entry := config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}
	resolver := &marketplace.Resolver{}
	plan, err := resolver.PrepareLocator(context.Background(), entry, marketplace.MarketplaceLocator{URL: fake.URL, Registry: "source", Kind: "collection", Name: "bundle"}, nil)
	if err != nil {
		t.Fatalf("prepare collection: %v", err)
	}
	installV4MarketplaceLayer(t, repo, configPath, entry, marketplaceInstallPins(plan))

	doc := layerMarketplaceLockDoc(t, lockPath)
	collection := marketplaceLockResource(doc, "collection", "bundle")
	if collection == nil {
		t.Fatal("installed lock missing collection record")
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	constrained := strings.Replace(string(data), "source/collection/bundle", "source/collection/bundle: {revision: "+collection.Revision+"}", 1)
	if constrained == string(data) {
		t.Fatalf("config request not found:\n%s", data)
	}
	writeFileMode(t, configPath, constrained, 0o600)
	configBytes, configStat := layerFreezeFile(t, configPath)
	lockBytes, lockStat := layerFreezeFile(t, lockPath)

	fake.setSkill(t, registry, "review", "review body v2")
	fake.setSkill(t, registry, "lint", "lint body v2")
	fake.addCollection(t, registry, "bundle", []hccartifact.CollectionMemberEntry{
		{Kind: "skill", Name: "review", RegistryID: string(registry.ID), Revision: string(fake.resources["source/skill/review"].revision.ContentDigest)},
		{Kind: "skill", Name: "lint", RegistryID: string(registry.ID), Revision: string(fake.resources["source/skill/lint"].revision.ContentDigest)},
	})
	fake.requests.Store(0)

	result, err := Update(context.Background(), UpdateOptions{RepoRoot: repo, DependencyReconcile: testMarketplaceUpdateReconcile})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := fake.requests.Load(); got != 0 {
		t.Fatalf("update made %d marketplace requests for a pinned collection", got)
	}
	if result.UpdatedMarketplacePins != 0 || result.SkippedMarketplacePins != 3 {
		t.Fatalf("Marketplace result = %#v, want three skips", result)
	}
	reasons := map[string]string{}
	for _, pin := range result.MarketplacePins {
		reasons[pin.OldPin.Kind+"/"+pin.OldPin.Name] = pin.Reason
	}
	if reasons["collection/bundle"] != "request pins an explicit revision" {
		t.Fatalf("collection skip reason = %q", reasons["collection/bundle"])
	}
	for _, name := range []string{"skill/review", "skill/lint"} {
		if reasons[name] != "leaf is pinned by a collection revision" {
			t.Fatalf("%s skip reason = %q", name, reasons[name])
		}
	}
	layerAssertFileUnchanged(t, configPath, configBytes, configStat)
	layerAssertFileUnchanged(t, lockPath, lockBytes, lockStat)
}
