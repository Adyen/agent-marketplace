package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"errors"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/marketplacelock"
)

func TestMutateMarketplaceLayerV4MissingRootEndpoint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	fake := newLayerMarketplaceServer(t)
	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.EnsureEndpoint(config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID})
	}, nil); err != nil {
		t.Fatalf("MutateMarketplaceLayer: %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "apiVersion: agent-marketplace/v4") || !strings.Contains(string(data), "marketplace: "+fake.URL) {
		t.Fatalf("missing-root config:\n%s", data)
	}
	if strings.Contains(string(data), "marketplaces:") {
		t.Fatalf("missing-root config gained legacy key:\n%s", data)
	}
	doc := layerMarketplaceLockDoc(t, lockPath)
	if doc.APIVersion != lockfile.APIVersionV2 || len(doc.Marketplaces) != 1 || doc.Marketplaces[0].InstanceID != fake.instanceID {
		t.Fatalf("missing-root lock = %#v", doc)
	}
}

func TestMutateMarketplaceLayerV4SecondPinMerges(t *testing.T) {
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
	entry := config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}

	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.IncludePins(entry, layerPreparePins(t, fake, "skill", "review"))
	}, nil); err != nil {
		t.Fatalf("first install: %v", err)
	}
	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.IncludePins(entry, layerPreparePins(t, fake, "skill", "lint"))
	}, nil); err != nil {
		t.Fatalf("second install: %v", err)
	}
	doc := layerMarketplaceLockDoc(t, lockPath)
	if marketplaceLockResource(doc, "skill", "review") == nil || marketplaceLockResource(doc, "skill", "lint") == nil {
		t.Fatalf("lock resources = %#v", doc.Marketplaces[0].Resources)
	}
	data, _ := os.ReadFile(configPath)
	if !strings.Contains(string(data), "source/skill/review") || !strings.Contains(string(data), "source/skill/lint") {
		t.Fatalf("config requests:\n%s", data)
	}
}

func TestMutateMarketplaceLayerV4ConflictingInstanceRefuses(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "review")
	writeFileMode(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  hosted: {marketplace: "+fake.URL+"}\n", 0o600)
	existing := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeUser,
		Marketplaces: []lockfile.MarketplaceLock{{URL: fake.URL, InstanceID: "other-instance"}}}
	lockData, err := lockfile.Encode(existing)
	if err != nil {
		t.Fatal(err)
	}
	writeFileMode(t, lockPath, string(lockData), 0o600)
	configBefore, _ := os.ReadFile(configPath)
	lockBefore, _ := os.ReadFile(lockPath)

	_, err = MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.IncludePins(config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}, layerPreparePins(t, fake, "skill", "review"))
	}, nil)
	if err == nil {
		t.Fatal("conflicting instance accepted")
	}
	if after, _ := os.ReadFile(configPath); string(after) != string(configBefore) {
		t.Fatal("conflicting instance mutated config")
	}
	if after, _ := os.ReadFile(lockPath); string(after) != string(lockBefore) {
		t.Fatal("conflicting instance mutated lock")
	}
}

func TestMutateMarketplaceLayerV4PrunesStaleLockRecords(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "review")
	writeFileMode(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  hosted: {marketplace: "+fake.URL+"}\n", 0o600)
	stale := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeUser,
		Marketplaces: []lockfile.MarketplaceLock{{
			URL: fake.URL, InstanceID: fake.instanceID,
			RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "old", RegistryID: "old-id"}},
			Resources: []lockfile.MarketplaceResourceLock{{
				RegistryName: "old", RegistryID: "old-id", Kind: "skill", Name: "stale",
				Revision: "sha256:" + strings.Repeat("a", 64), ContentDigest: lockfile.Digest("sha256:" + strings.Repeat("a", 64)),
				ArchiveDigest: lockfile.Digest("sha256:" + strings.Repeat("b", 64)), Acquisition: lockfile.MarketplaceAcquisitionMirror,
			}},
		}}}
	lockData, err := lockfile.Encode(stale)
	if err != nil {
		t.Fatal(err)
	}
	writeFileMode(t, lockPath, string(lockData), 0o600)

	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.IncludePins(config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}, layerPreparePins(t, fake, "skill", "review"))
	}, nil); err != nil {
		t.Fatalf("MutateMarketplaceLayer: %v", err)
	}
	doc := layerMarketplaceLockDoc(t, lockPath)
	if marketplaceLockResource(doc, "skill", "stale") != nil {
		t.Fatalf("stale record survived: %#v", doc.Marketplaces[0].Resources)
	}
	if marketplaceLockResource(doc, "skill", "review") == nil {
		t.Fatalf("new record missing: %#v", doc.Marketplaces[0].Resources)
	}
	for _, binding := range doc.Marketplaces[0].RegistryBindings {
		if binding.Name == "old" {
			t.Fatalf("stale binding survived: %#v", doc.Marketplaces[0].RegistryBindings)
		}
	}
}

func TestMutateMarketplaceLayerV4EndpointOnlyRetained(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	fake := newLayerMarketplaceServer(t)
	second := newLayerMarketplaceServer(t)
	writeFileMode(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  hosted: {marketplace: "+fake.URL+"}\n", 0o600)
	existing := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeUser,
		Marketplaces: []lockfile.MarketplaceLock{{URL: fake.URL, InstanceID: fake.instanceID}}}
	lockData, err := lockfile.Encode(existing)
	if err != nil {
		t.Fatal(err)
	}
	writeFileMode(t, lockPath, string(lockData), 0o600)
	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.EnsureEndpoint(config.MarketplaceEntry{URL: second.URL, InstanceID: second.instanceID})
	}, nil); err != nil {
		t.Fatalf("MutateMarketplaceLayer: %v", err)
	}
	doc := layerMarketplaceLockDoc(t, lockPath)
	if len(doc.Marketplaces) != 2 {
		t.Fatalf("lock endpoints = %#v", doc.Marketplaces)
	}
	var hosted *lockfile.MarketplaceLock
	for i := range doc.Marketplaces {
		if doc.Marketplaces[i].URL == fake.URL {
			hosted = &doc.Marketplaces[i]
		}
	}
	if hosted == nil || hosted.InstanceID != fake.instanceID || len(hosted.Resources) != 0 {
		t.Fatalf("endpoint-only lock = %#v", doc.Marketplaces)
	}
}

func TestMutateMarketplaceLayerV4LocalConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	sub := filepath.Join(repo, "services", "one")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFileMode(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n", 0o600)
	configPath := filepath.Join(sub, ".agents", "config.local.yaml")
	lockPath := filepath.Join(sub, ".agents", "lock.local.yaml")
	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "review")

	if _, err := MutateMarketplaceLayer(context.Background(), repo, sub, configPath, config.RepoPersonal, func(m *MarketplaceLayerMutation) error {
		return m.IncludePins(config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}, layerPreparePins(t, fake, "skill", "review"))
	}, nil); err != nil {
		t.Fatalf("MutateMarketplaceLayer: %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "apiVersion: agent-marketplace/v4") || !strings.Contains(string(data), "source/skill/review") {
		t.Fatalf("local config:\n%s", data)
	}
	doc := layerMarketplaceLockDoc(t, lockPath)
	if doc.Scope != lockfile.ScopeLocal || len(doc.Marketplaces) != 1 || len(doc.Marketplaces[0].Resources) != 1 {
		t.Fatalf("local lock = %#v", doc)
	}
}

func layerFreezeFile(t *testing.T, path string) (data []byte, stat os.FileInfo) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	frozen := time.Unix(1700000000, 0)
	if err := os.Chtimes(path, frozen, frozen); err != nil {
		t.Fatal(err)
	}
	stat, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return data, stat
}

func layerAssertFileUnchanged(t *testing.T, path string, data []byte, before os.FileInfo) {
	t.Helper()
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(data) || stat.Mode() != before.Mode() || !stat.ModTime().Equal(before.ModTime()) {
		t.Fatalf("%s changed: mode %o->%o mtime %v->%v", path, before.Mode(), stat.Mode(), before.ModTime(), stat.ModTime())
	}
}

func TestMutateMarketplaceLayerV4EndpointRepairsAbsentLock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	fake := newLayerMarketplaceServer(t)
	writeFileMode(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  hosted: {marketplace: "+fake.URL+"}\n", 0o600)
	configBytes, configStat := layerFreezeFile(t, configPath)

	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.EnsureEndpoint(config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID})
	}, nil); err != nil {
		t.Fatalf("MutateMarketplaceLayer: %v", err)
	}
	layerAssertFileUnchanged(t, configPath, configBytes, configStat)
	doc := layerMarketplaceLockDoc(t, lockPath)
	if len(doc.Marketplaces) != 1 || doc.Marketplaces[0].InstanceID != fake.instanceID {
		t.Fatalf("repaired lock = %#v", doc.Marketplaces)
	}
}

func TestMutateMarketplaceLayerV4RepairsAbsentLock(t *testing.T) {
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
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	configBytes, configStat := layerFreezeFile(t, configPath)

	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.IncludePins(entry, pins)
	}, nil); err != nil {
		t.Fatalf("repair: %v", err)
	}
	layerAssertFileUnchanged(t, configPath, configBytes, configStat)
	doc := layerMarketplaceLockDoc(t, lockPath)
	if marketplaceLockResource(doc, "skill", "review") == nil {
		t.Fatalf("repaired lock resources = %#v", doc.Marketplaces[0].Resources)
	}
}

func TestMutateMarketplaceLayerV4NoopWhenLockCurrent(t *testing.T) {
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
	configBytes, configStat := layerFreezeFile(t, configPath)
	lockBytes, lockStat := layerFreezeFile(t, lockPath)

	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.IncludePins(entry, pins)
	}, nil); err != nil {
		t.Fatalf("repeat: %v", err)
	}
	layerAssertFileUnchanged(t, configPath, configBytes, configStat)
	layerAssertFileUnchanged(t, lockPath, lockBytes, lockStat)
}

func TestCommitPreparedMarketplaceInstallRepairsLockWithoutRefetch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	writeFileMode(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n", 0o600)
	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "review")
	opts := InstallOptions{RepoRoot: repo, Args: []string{fake.URL + "/r/source/skill/review"}, Scope: ScopeUser, AcceptRisk: true}
	prepared, err := PrepareMarketplaceInstall(context.Background(), opts)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := CommitPreparedMarketplaceInstall(context.Background(), prepared); err != nil {
		t.Fatalf("commit: %v", err)
	}
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	prepared, err = PrepareMarketplaceInstall(context.Background(), opts)
	if err != nil {
		t.Fatalf("re-prepare: %v", err)
	}
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	fake.requests.Store(0)
	if _, err := CommitPreparedMarketplaceInstall(context.Background(), prepared); err != nil {
		t.Fatalf("repair commit: %v", err)
	}
	if got := fake.requests.Load(); got != 0 {
		t.Fatalf("repair commit made %d marketplace requests", got)
	}
	doc := layerMarketplaceLockDoc(t, lockPath)
	if marketplaceLockResource(doc, "skill", "review") == nil {
		t.Fatalf("repaired lock resources = %#v", doc.Marketplaces[0].Resources)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "devin", "skills", "review")); err != nil {
		t.Fatalf("review was not materialized: %v", err)
	}
}

func TestMergePinsToV4PreservesInputDocument(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	writeFileMode(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n", 0o600)
	fake := newLayerMarketplaceServer(t)
	second := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "review")
	entry := config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}
	pins := layerPreparePins(t, fake, "skill", "review")

	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.IncludePins(entry, pins)
	}, nil); err != nil {
		t.Fatalf("install: %v", err)
	}
	v4Cfg, err := config.LoadV4ConfigIfPresent(configPath, config.UserPersonal)
	if err != nil || v4Cfg == nil {
		t.Fatalf("load v4: %v", err)
	}
	oid, err := lockfile.ParseGitOID("sha256:" + strings.Repeat("e", 64))
	if err != nil {
		t.Fatal(err)
	}
	current := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeUser,
		Registries: []lockfile.GitRegistry{{URL: "https://example.com/r.git", Ref: "main", Commit: oid}},
		Marketplaces: []lockfile.MarketplaceLock{{
			URL: second.URL, InstanceID: second.instanceID,
			RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "other", RegistryID: "other-id"}},
			Resources: []lockfile.MarketplaceResourceLock{{
				RegistryName: "other", RegistryID: "other-id", Kind: "collection", Name: "bundle",
				Revision: "sha256:" + strings.Repeat("a", 64), ContentDigest: lockfile.Digest("sha256:" + strings.Repeat("a", 64)),
				CollectionManifestDigest: lockfile.Digest("sha256:" + strings.Repeat("a", 64)), Acquisition: lockfile.MarketplaceAcquisitionMirror,
			}, {
				RegistryName: "other", RegistryID: "other-id", Kind: "skill", Name: "member",
				Revision: "sha256:" + strings.Repeat("b", 64), ContentDigest: lockfile.Digest("sha256:" + strings.Repeat("b", 64)),
				ArchiveDigest: lockfile.Digest("sha256:" + strings.Repeat("c", 64)), Acquisition: lockfile.MarketplaceAcquisitionMirror,
				SelectedBy: []lockfile.MarketplaceSelectionLock{{RegistryID: "other-id", Name: "bundle", Revision: lockfile.Digest("sha256:" + strings.Repeat("a", 64))}},
			}},
		}}}
	before, err := lockfile.Encode(current)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.EnsureEndpoint(config.MarketplaceEntry{URL: second.URL, InstanceID: second.instanceID})
	}, nil); err != nil {
		t.Fatalf("add second source: %v", err)
	}
	v4Cfg, err = config.LoadV4ConfigIfPresent(configPath, config.UserPersonal)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := marketplacelock.MergePinsToV4(v4Cfg, current, entry, pins, marketplace.DefaultCache()); err != nil {
		t.Fatalf("merge: %v", err)
	}
	after, err := lockfile.Encode(current)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("successful merge mutated the input document")
	}

	conflicting := current
	conflicting.Marketplaces = append(conflicting.Marketplaces, lockfile.MarketplaceLock{URL: fake.URL, InstanceID: "other-instance"})
	conflictBefore, _ := lockfile.Encode(conflicting)
	if _, err := marketplacelock.MergePinsToV4(v4Cfg, conflicting, entry, pins, marketplace.DefaultCache()); err == nil {
		t.Fatal("conflicting instance accepted")
	}
	conflictAfter, _ := lockfile.Encode(conflicting)
	if string(conflictAfter) != string(conflictBefore) {
		t.Fatal("failed merge mutated the input document")
	}
}

func TestMergePinsToV4RejectsConflictingEvidence(t *testing.T) {
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

	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.IncludePins(entry, pins)
	}, nil); err != nil {
		t.Fatalf("install: %v", err)
	}
	v4Cfg, err := config.LoadV4ConfigIfPresent(configPath, config.UserPersonal)
	if err != nil {
		t.Fatal(err)
	}
	doc := layerMarketplaceLockDoc(t, filepath.Join(home, ".agents", "lock.yaml"))

	doc.Marketplaces[0].Resources[0].Revision = "sha256:" + strings.Repeat("d", 64)
	if _, err := marketplacelock.MergePinsToV4(v4Cfg, doc, entry, pins, marketplace.DefaultCache()); err == nil {
		t.Fatal("conflicting evidence accepted")
	} else {
		var v4err *config.V4ValidationError
		if !errors.As(err, &v4err) || v4err.Code != "v4.marketplace-lock-mismatch" {
			t.Fatalf("conflict error = %v", err)
		}
	}

	doc.Marketplaces[0].Resources[0].Revision = pins[0].Revision
	scoped := entry
	scoped.Source = config.RepoCommitted
	if _, err := marketplacelock.MergePinsToV4(v4Cfg, doc, scoped, pins, marketplace.DefaultCache()); err == nil {
		t.Fatal("wrong-scope lock accepted")
	} else {
		var v4err *config.V4ValidationError
		if !errors.As(err, &v4err) || v4err.Code != "v4.marketplace-lock-mismatch" {
			t.Fatalf("scope error = %v", err)
		}
	}
}

func TestCommitPreparedMarketplaceInstallSurfacesOverlayErrors(t *testing.T) {
	t.Run("malformed v4 config", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		repo := t.TempDir()
		configPath := filepath.Join(home, ".agents", "config.yaml")
		writeFileMode(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n", 0o600)
		fake := newLayerMarketplaceServer(t)
		registry := fake.addRegistry(t, "source")
		fake.addSkill(t, registry, "review")
		opts := InstallOptions{RepoRoot: repo, Args: []string{fake.URL + "/r/source/skill/review"}, Scope: ScopeUser, AcceptRisk: true}
		prepared, err := PrepareMarketplaceInstall(context.Background(), opts)
		if err != nil {
			t.Fatalf("prepare: %v", err)
		}
		writeFileMode(t, configPath, "apiVersion: agent-marketplace/v4\nsources: {broken: [\n", 0o600)
		if _, err := CommitPreparedMarketplaceInstall(context.Background(), prepared); err == nil {
			t.Fatal("malformed config committed")
		} else if strings.Contains(err.Error(), "marketplace-lock-required") {
			t.Fatalf("fell through to missing-lock failure: %v", err)
		}
	})

	t.Run("conflicting merge evidence", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		repo := t.TempDir()
		configPath := filepath.Join(home, ".agents", "config.yaml")
		lockPath := filepath.Join(home, ".agents", "lock.yaml")
		fake := newLayerMarketplaceServer(t)
		second := newLayerMarketplaceServer(t)
		registry := fake.addRegistry(t, "source")
		fake.addSkill(t, registry, "review")
		writeFileMode(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n", 0o600)
		if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
			return m.EnsureEndpoint(config.MarketplaceEntry{URL: second.URL, InstanceID: second.instanceID})
		}, nil); err != nil {
			t.Fatalf("register second endpoint: %v", err)
		}
		opts := InstallOptions{RepoRoot: repo, Args: []string{fake.URL + "/r/source/skill/review"}, Scope: ScopeUser, AcceptRisk: true}
		prepared, err := PrepareMarketplaceInstall(context.Background(), opts)
		if err != nil {
			t.Fatalf("prepare: %v", err)
		}
		if err := os.Remove(lockPath); err != nil {
			t.Fatal(err)
		}
		_, err = CommitPreparedMarketplaceInstall(context.Background(), prepared)
		if err == nil {
			t.Fatal("conflicting merge committed")
		}
		var v4err *config.V4ValidationError
		if !errors.As(err, &v4err) || v4err.Code != "v4.marketplace-lock-mismatch" {
			t.Fatalf("error = %v", err)
		}
		if strings.Contains(err.Error(), "marketplace-lock-required") {
			t.Fatalf("fell through to missing-lock failure: %v", err)
		}
	})
}
