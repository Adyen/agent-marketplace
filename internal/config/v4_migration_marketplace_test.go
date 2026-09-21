package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

type migrationEvidenceStub struct {
	records map[string]lockfile.MarketplaceResourceLock
	calls   int
}

func (s *migrationEvidenceStub) VerifiedMarketplaceLockResource(entry MarketplaceEntry, pin MarketplaceResourcePin) (lockfile.MarketplaceResourceLock, error) {
	s.calls++
	record, ok := s.records[pin.Kind+"/"+pin.Name]
	if !ok {
		return lockfile.MarketplaceResourceLock{}, fmt.Errorf("no verified record for %s/%s", pin.Kind, pin.Name)
	}
	return record, nil
}

func marketplaceMigrationRecord(registryName, registryID, kind, name, revision, contentDigest string, mutate func(*lockfile.MarketplaceResourceLock)) lockfile.MarketplaceResourceLock {
	record := lockfile.MarketplaceResourceLock{
		RegistryName:  registryName,
		RegistryID:    registryID,
		Kind:          kind,
		Name:          name,
		Revision:      revision,
		ContentDigest: lockfile.Digest(contentDigest),
		Acquisition:   lockfile.MarketplaceAcquisitionMirror,
	}
	if mutate != nil {
		mutate(&record)
	}
	return record
}

func writeLegacyMarketplaceConfig(t *testing.T, path string, entries string) []byte {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("apiVersion: agent-marketplace/v2\nmarketplaces:\n" + entries)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return content
}

func marketplaceLockPath(t *testing.T, configPath string) string {
	t.Helper()
	canonical, err := canonicalConfigPath(configPath)
	if err != nil {
		t.Fatal(err)
	}
	lockPath, err := lockfile.PathForConfig(canonical)
	if err != nil {
		t.Fatal(err)
	}
	return lockPath
}

func assertFileUnchanged(t *testing.T, path string, before []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != string(before) {
		t.Fatalf("file changed:\nbefore:\n%s\nafter:\n%s", before, got)
	}
}

func assertFileMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be absent, stat err=%v", path, err)
	}
}

func TestMigrateConfigFilesToV4MarketplaceRequiresEvidence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".agents", "config.yaml")
	before := writeLegacyMarketplaceConfig(t, path,
		"  - url: https://market.example\n    instance_id: mp\n    alias: adyen\n    resources:\n      - registry_id: src\n        kind: skill\n        name: review\n        revision: "+v4sha("a")+"\n        digest: "+v4sha("a")+"\n        direct: true\n")
	lockPath := marketplaceLockPath(t, path)

	_, err := MigrateConfigFilesToV4(MigratePaths{Repo: path}, V4MigrationRepo, false)
	if err == nil {
		t.Fatal("expected missing-evidence refusal")
	}
	var v4err *V4ValidationError
	if !errors.As(err, &v4err) || v4err.Code != "v4.migration-marketplace-evidence" {
		t.Fatalf("expected v4.migration-marketplace-evidence, got %v", err)
	}
	assertFileUnchanged(t, path, before)
	assertFileMissing(t, lockPath)
}

func TestMigrateConfigFilesToV4MarketplaceDirectPin(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, ".agents", "config.yaml")
	writeLegacyMarketplaceConfig(t, path,
		"  - url: https://market.example\n    instance_id: mp\n    alias: adyen\n    resources:\n      - registry_id: src\n        registry_slug: tools\n        kind: skill\n        name: review\n        revision: "+v4sha("a")+"\n        digest: "+v4sha("a")+"\n        direct: true\n")

	evidence := &migrationEvidenceStub{records: map[string]lockfile.MarketplaceResourceLock{
		"skill/review": marketplaceMigrationRecord("tools", "src", "skill", "review", v4sha("a"), v4sha("a"), func(r *lockfile.MarketplaceResourceLock) {
			r.ArchiveDigest = lockfile.Digest(v4sha("b"))
		}),
	}}
	result, err := MigrateConfigFilesToV4WithEvidence(context.Background(), MigratePaths{Repo: path}, V4MigrationRepo, false, evidence)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	if !result.RepoMigrated || result.DryRun {
		t.Fatalf("unexpected result %#v", result)
	}
	if evidence.calls != 1 {
		t.Fatalf("evidence calls = %d", evidence.calls)
	}

	parsed, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatalf("parse migrated config: %v", err)
	}
	source, ok := parsed.Sources["adyen"]
	if !ok || source.Marketplace != "https://market.example" || source.Git != "" || source.Directory != "" {
		t.Fatalf("migrated source = %#v", parsed.Sources)
	}
	group := parsed.Groups["adyen"]
	if len(group.Include) != 1 || group.Include[0].Locator != "tools/skill/review" {
		t.Fatalf("include = %#v", group.Include)
	}

	lockBytes, err := os.ReadFile(marketplaceLockPath(t, path))
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	doc, err := lockfile.Decode(lockBytes)
	if err != nil {
		t.Fatalf("decode lock: %v", err)
	}
	if doc.APIVersion != lockfile.APIVersionV2 || doc.Scope != lockfile.ScopeRepo || len(doc.Marketplaces) != 1 {
		t.Fatalf("lock doc = %#v", doc)
	}
	mp := doc.Marketplaces[0]
	if mp.URL != "https://market.example" || mp.InstanceID != "mp" {
		t.Fatalf("lock endpoint = %#v", mp)
	}
	if len(mp.RegistryBindings) != 1 || mp.RegistryBindings[0].Name != "tools" || mp.RegistryBindings[0].RegistryID != "src" {
		t.Fatalf("bindings = %#v", mp.RegistryBindings)
	}
	if len(mp.Resources) != 1 || string(mp.Resources[0].ArchiveDigest) != v4sha("b") {
		t.Fatalf("resources = %#v", mp.Resources)
	}

	cfg, err := LoadConfigFromPath(path)
	if err != nil {
		t.Fatalf("hydrate migrated pair: %v", err)
	}
	if len(cfg.Marketplaces) != 1 || len(cfg.Marketplaces[0].Resources) != 1 {
		t.Fatalf("hydrated marketplaces = %#v", cfg.Marketplaces)
	}
	pin := cfg.Marketplaces[0].Resources[0]
	if !pin.Direct || pin.RegistrySlug != "tools" || pin.LockArchiveDigest != v4sha("b") {
		t.Fatalf("hydrated pin = %#v", pin)
	}
}

func TestMigrateConfigFilesToV4MarketplaceOptions(t *testing.T) {
	t.Run("installed name and distinct revision preserved", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		dir := t.TempDir()
		path := filepath.Join(dir, ".agents", "config.yaml")
		writeLegacyMarketplaceConfig(t, path,
			"  - url: https://market.example\n    instance_id: mp\n    resources:\n      - registry_id: src\n        registry_slug: tools\n        kind: skill\n        name: review\n        revision: "+v4sha("c")+"\n        digest: "+v4sha("a")+"\n        installed_name: chosen-review\n        direct: true\n")
		evidence := &migrationEvidenceStub{records: map[string]lockfile.MarketplaceResourceLock{
			"skill/review": marketplaceMigrationRecord("tools", "src", "skill", "review", v4sha("c"), v4sha("a"), func(r *lockfile.MarketplaceResourceLock) {
				r.ArchiveDigest = lockfile.Digest(v4sha("b"))
			}),
		}}
		if _, err := MigrateConfigFilesToV4WithEvidence(context.Background(), MigratePaths{Repo: path}, V4MigrationRepo, false, evidence); err != nil {
			t.Fatalf("migration failed: %v", err)
		}
		parsed, err := LoadV4ConfigFromPath(path)
		if err != nil {
			t.Fatal(err)
		}
		var sourceName string
		for name, source := range parsed.Sources {
			if source.Marketplace != "" {
				sourceName = name
			}
		}
		if sourceName != "marketplace" {
			t.Fatalf("expected generated marketplace source name, got %q (sources %#v)", sourceName, parsed.Sources)
		}
		req := parsed.Groups[sourceName].Include[0]
		if req.Locator != "tools/skill/review" || req.Options.As != "chosen-review" || req.Options.Revision != v4sha("c") {
			t.Fatalf("request = %#v", req)
		}
	})

	t.Run("noncanonical distinct revision refused", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, ".agents", "config.yaml")
		before := writeLegacyMarketplaceConfig(t, path,
			"  - url: https://market.example\n    instance_id: mp\n    resources:\n      - registry_id: src\n        kind: skill\n        name: review\n        revision: rev-9\n        digest: "+v4sha("a")+"\n        direct: true\n")
		evidence := &migrationEvidenceStub{records: map[string]lockfile.MarketplaceResourceLock{
			"skill/review": marketplaceMigrationRecord("src", "src", "skill", "review", "rev-9", v4sha("a"), func(r *lockfile.MarketplaceResourceLock) {
				r.ArchiveDigest = lockfile.Digest(v4sha("b"))
			}),
		}}
		_, err := MigrateConfigFilesToV4WithEvidence(context.Background(), MigratePaths{Repo: path}, V4MigrationRepo, false, evidence)
		if err == nil {
			t.Fatal("expected revision refusal")
		}
		var v4err *V4ValidationError
		if !errors.As(err, &v4err) || v4err.Code != "v4.migration-marketplace-evidence" {
			t.Fatalf("expected v4.migration-marketplace-evidence, got %v", err)
		}
		assertFileUnchanged(t, path, before)
		assertFileMissing(t, marketplaceLockPath(t, path))
	})
}

func TestMigrateConfigFilesToV4MarketplaceCollections(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, ".agents", "config.yaml")
	writeLegacyMarketplaceConfig(t, path,
		"  - url: https://market.example\n    instance_id: mp\n    resources:\n"+
			"      - registry_id: cols\n        registry_slug: collections\n        kind: collection\n        name: onboarding\n        revision: "+v4sha("c")+"\n        digest: "+v4sha("c")+"\n        direct: true\n"+
			"      - registry_id: src\n        registry_slug: tools\n        kind: skill\n        name: review\n        revision: "+v4sha("a")+"\n        digest: "+v4sha("a")+"\n        selected_by:\n          - registry_id: cols\n            name: onboarding\n            revision: "+v4sha("c")+"\n"+
			"      - registry_id: src\n        registry_slug: tools\n        kind: skill\n        name: extra\n        revision: "+v4sha("d")+"\n        digest: "+v4sha("d")+"\n        direct: true\n        selected_by:\n          - registry_id: cols\n            name: onboarding\n            revision: "+v4sha("c")+"\n")
	edge := []lockfile.MarketplaceSelectionLock{{RegistryID: "cols", Name: "onboarding", Revision: lockfile.Digest(v4sha("c"))}}
	evidence := &migrationEvidenceStub{records: map[string]lockfile.MarketplaceResourceLock{
		"collection/onboarding": marketplaceMigrationRecord("collections", "cols", "collection", "onboarding", v4sha("c"), v4sha("c"), func(r *lockfile.MarketplaceResourceLock) {
			r.CollectionManifestDigest = lockfile.Digest(v4sha("c"))
		}),
		"skill/review": marketplaceMigrationRecord("tools", "src", "skill", "review", v4sha("a"), v4sha("a"), func(r *lockfile.MarketplaceResourceLock) {
			r.ArchiveDigest = lockfile.Digest(v4sha("b"))
			r.SelectedBy = edge
		}),
		"skill/extra": marketplaceMigrationRecord("tools", "src", "skill", "extra", v4sha("d"), v4sha("d"), func(r *lockfile.MarketplaceResourceLock) {
			r.ArchiveDigest = lockfile.Digest(v4sha("e"))
			r.SelectedBy = edge
		}),
	}}
	if _, err := MigrateConfigFilesToV4WithEvidence(context.Background(), MigratePaths{Repo: path}, V4MigrationRepo, false, evidence); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	if evidence.calls != 3 {
		t.Fatalf("evidence calls = %d", evidence.calls)
	}

	parsed, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	var include []string
	for _, group := range parsed.Groups {
		for _, req := range group.Include {
			include = append(include, req.Locator)
		}
	}
	want := map[string]bool{"collections/collection/onboarding": true, "tools/skill/extra": true}
	if len(include) != 2 {
		t.Fatalf("include = %#v", include)
	}
	for _, locator := range include {
		if !want[locator] {
			t.Fatalf("unexpected include %q in %#v", locator, include)
		}
	}

	cfg, err := LoadConfigFromPath(path)
	if err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	pins := map[string]MarketplaceResourcePin{}
	for _, pin := range cfg.Marketplaces[0].Resources {
		pins[pin.Kind+"/"+pin.Name] = pin
	}
	if !pins["collection/onboarding"].Direct || pins["skill/review"].Direct || !pins["skill/extra"].Direct {
		t.Fatalf("direct flags = %#v", pins)
	}
	if len(pins["skill/review"].SelectedBy.List()) != 1 || pins["skill/review"].LockCollectionManifestDigest != "" || pins["collection/onboarding"].LockCollectionManifestDigest != v4sha("c") {
		t.Fatalf("selection/integrity = %#v", pins)
	}
}

func TestMigrateConfigFilesToV4MarketplaceEndpointOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, ".agents", "config.yaml")
	writeLegacyMarketplaceConfig(t, path, "  - url: https://market.example\n    instance_id: mp\n")
	evidence := &migrationEvidenceStub{records: map[string]lockfile.MarketplaceResourceLock{}}
	if _, err := MigrateConfigFilesToV4WithEvidence(context.Background(), MigratePaths{Repo: path}, V4MigrationRepo, false, evidence); err != nil {
		t.Fatalf("endpoint-only migration failed: %v", err)
	}
	if evidence.calls != 0 {
		t.Fatalf("evidence calls = %d", evidence.calls)
	}
	parsed, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Sources) != 1 || len(parsed.Groups) != 0 {
		t.Fatalf("parsed = %#v / %#v", parsed.Sources, parsed.Groups)
	}
	doc, err := lockfile.Decode(mustReadFile(t, marketplaceLockPath(t, path)))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Marketplaces) != 1 || len(doc.Marketplaces[0].Resources) != 0 || doc.Marketplaces[0].InstanceID != "mp" {
		t.Fatalf("lock = %#v", doc.Marketplaces)
	}
	cfg, err := LoadConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Marketplaces) != 1 || len(cfg.Marketplaces[0].Resources) != 0 {
		t.Fatalf("hydrated = %#v", cfg.Marketplaces)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestMigrateConfigFilesToV4MarketplaceExistingLock(t *testing.T) {
	marketplaceYAML := "  - url: https://market.example\n    instance_id: mp\n    resources:\n      - registry_id: src\n        registry_slug: tools\n        kind: skill\n        name: review\n        revision: " + v4sha("a") + "\n        digest: " + v4sha("a") + "\n        direct: true\n"
	evidenceFor := func() *migrationEvidenceStub {
		return &migrationEvidenceStub{records: map[string]lockfile.MarketplaceResourceLock{
			"skill/review": marketplaceMigrationRecord("tools", "src", "skill", "review", v4sha("a"), v4sha("a"), func(r *lockfile.MarketplaceResourceLock) {
				r.ArchiveDigest = lockfile.Digest(v4sha("b"))
			}),
		}}
	}
	gitLock := func(version string) []byte {
		oid, err := lockfile.ParseGitOID("sha1:" + strings.Repeat("1", 40))
		if err != nil {
			t.Fatal(err)
		}
		data, err := lockfile.Encode(lockfile.Document{
			APIVersion: version,
			Scope:      lockfile.ScopeRepo,
			Registries: []lockfile.GitRegistry{{
				URL: "https://git.example/repo", Ref: "main", Commit: oid,
				Roots: []lockfile.GitRoot{{
					Path: ".", SourceHarness: "claude", RevisionSchema: "v1",
					RevisionDigest: lockfile.Digest(v4sha("9")),
					Resources:      []lockfile.GitResource{{Kind: "skill", Name: "audit", ContentDigest: lockfile.Digest(v4sha("8")), ArchiveDigest: lockfile.Digest(v4sha("7"))}},
				}},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	desiredMarketplace := func() []lockfile.MarketplaceResourceLock {
		return []lockfile.MarketplaceResourceLock{marketplaceMigrationRecord("tools", "src", "skill", "review", v4sha("a"), v4sha("a"), func(r *lockfile.MarketplaceResourceLock) {
			r.ArchiveDigest = lockfile.Digest(v4sha("b"))
		})}
	}
	encodeLock := func(t *testing.T, doc lockfile.Document) []byte {
		t.Helper()
		data, err := lockfile.Encode(doc)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	wrongScopeLock := func(t *testing.T, resources []lockfile.MarketplaceResourceLock) []byte {
		t.Helper()
		return encodeLock(t, lockfile.Document{
			APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeUser,
			Marketplaces: []lockfile.MarketplaceLock{{
				URL: "https://market.example", InstanceID: "mp",
				RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "tools", RegistryID: "src"}},
				Resources:        resources,
			}},
		})
	}

	cases := map[string]struct {
		lockBytes  []byte
		wantCode   string
		assertLock func(t *testing.T, lockBytes []byte)
	}{
		"v1 git lock upgrades preserving git records": {
			lockBytes: gitLock(lockfile.APIVersionV1),
			assertLock: func(t *testing.T, data []byte) {
				doc, err := lockfile.Decode(data)
				if err != nil {
					t.Fatal(err)
				}
				if doc.APIVersion != lockfile.APIVersionV2 || len(doc.Registries) != 1 || len(doc.Marketplaces) != 1 {
					t.Fatalf("upgraded lock = %#v", doc)
				}
			},
		},
		"identical v2 lock accepted": {
			lockBytes: encodeLock(t, lockfile.Document{
				APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo,
				Marketplaces: []lockfile.MarketplaceLock{{
					URL: "https://market.example", InstanceID: "mp",
					RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "tools", RegistryID: "src"}},
					Resources:        desiredMarketplace(),
				}},
			}),
			assertLock: func(t *testing.T, data []byte) {
				doc, err := lockfile.Decode(data)
				if err != nil {
					t.Fatal(err)
				}
				if len(doc.Marketplaces) != 1 {
					t.Fatalf("lock = %#v", doc)
				}
			},
		},
		"differing v2 lock refused": {
			lockBytes: encodeLock(t, lockfile.Document{
				APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo,
				Marketplaces: []lockfile.MarketplaceLock{{
					URL: "https://market.example", InstanceID: "mp",
					RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "tools", RegistryID: "src"}},
					Resources: []lockfile.MarketplaceResourceLock{marketplaceMigrationRecord("tools", "src", "skill", "other", v4sha("a"), v4sha("a"), func(r *lockfile.MarketplaceResourceLock) {
						r.ArchiveDigest = lockfile.Digest(v4sha("b"))
					})},
				}},
			}),
			wantCode: "v4.migration-marketplace-lock-conflict",
		},
		"wrong scope refused": {
			lockBytes: wrongScopeLock(t, desiredMarketplace()),
			wantCode:  "v4.migration-marketplace-lock",
		},
		"malformed lock refused": {
			lockBytes: []byte("apiVersion: agent-marketplace-lock/v2\nscope: repo\nmarketplaces: [\n"),
			wantCode:  "v4.migration-marketplace-lock",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			dir := t.TempDir()
			path := filepath.Join(dir, ".agents", "config.yaml")
			before := writeLegacyMarketplaceConfig(t, path, marketplaceYAML)
			lockPath := marketplaceLockPath(t, path)
			if err := os.WriteFile(lockPath, tc.lockBytes, 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := MigrateConfigFilesToV4WithEvidence(context.Background(), MigratePaths{Repo: path}, V4MigrationRepo, false, evidenceFor())
			if tc.wantCode != "" {
				var v4err *V4ValidationError
				if !errors.As(err, &v4err) || v4err.Code != tc.wantCode {
					t.Fatalf("expected code %q, got %v", tc.wantCode, err)
				}
				assertFileUnchanged(t, path, before)
				assertFileUnchanged(t, lockPath, tc.lockBytes)
				return
			}
			if err != nil {
				t.Fatalf("migration failed: %v", err)
			}
			tc.assertLock(t, mustReadFile(t, lockPath))
		})
	}
}

func TestMigrateConfigFilesToV4MarketplacePreservesExistingLockMode(t *testing.T) {
	marketplaceYAML := "  - url: https://market.example\n    instance_id: mp\n    resources:\n      - registry_id: src\n        registry_slug: tools\n        kind: skill\n        name: review\n        revision: " + v4sha("a") + "\n        digest: " + v4sha("a") + "\n        direct: true\n"
	evidence := func() *migrationEvidenceStub {
		return &migrationEvidenceStub{records: map[string]lockfile.MarketplaceResourceLock{
			"skill/review": marketplaceMigrationRecord("tools", "src", "skill", "review", v4sha("a"), v4sha("a"), func(r *lockfile.MarketplaceResourceLock) {
				r.ArchiveDigest = lockfile.Digest(v4sha("b"))
			}),
		}}
	}
	lockMode := func(t *testing.T, path string) os.FileMode {
		t.Helper()
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		return info.Mode().Perm()
	}

	t.Run("existing v1 lock keeps mode", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		dir := t.TempDir()
		path := filepath.Join(dir, ".agents", "config.yaml")
		writeLegacyMarketplaceConfig(t, path, marketplaceYAML)
		lockPath := marketplaceLockPath(t, path)
		v1, err := lockfile.Encode(lockfile.Document{APIVersion: lockfile.APIVersionV1, Scope: lockfile.ScopeRepo})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(lockPath, v1, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := MigrateConfigFilesToV4WithEvidence(context.Background(), MigratePaths{Repo: path}, V4MigrationRepo, false, evidence()); err != nil {
			t.Fatalf("migration failed: %v", err)
		}
		doc, err := lockfile.Decode(mustReadFile(t, lockPath))
		if err != nil {
			t.Fatal(err)
		}
		if doc.APIVersion != lockfile.APIVersionV2 {
			t.Fatalf("lock not upgraded: %q", doc.APIVersion)
		}
		if mode := lockMode(t, lockPath); mode != 0o600 {
			t.Fatalf("existing lock mode = %o, want 0600", mode)
		}
	})

	t.Run("new lock gets 0644", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		dir := t.TempDir()
		path := filepath.Join(dir, ".agents", "config.yaml")
		writeLegacyMarketplaceConfig(t, path, marketplaceYAML)
		lockPath := marketplaceLockPath(t, path)
		if _, err := MigrateConfigFilesToV4WithEvidence(context.Background(), MigratePaths{Repo: path}, V4MigrationRepo, false, evidence()); err != nil {
			t.Fatalf("migration failed: %v", err)
		}
		if mode := lockMode(t, lockPath); mode != 0o644 {
			t.Fatalf("new lock mode = %o, want 0644", mode)
		}
	})
}

func TestMigrateConfigFilesToV4MarketplaceDryRun(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, ".agents", "config.yaml")
	before := writeLegacyMarketplaceConfig(t, path,
		"  - url: https://market.example\n    instance_id: mp\n    resources:\n      - registry_id: src\n        registry_slug: tools\n        kind: skill\n        name: review\n        revision: "+v4sha("a")+"\n        digest: "+v4sha("a")+"\n        direct: true\n")
	lockPath := marketplaceLockPath(t, path)
	evidence := &migrationEvidenceStub{records: map[string]lockfile.MarketplaceResourceLock{
		"skill/review": marketplaceMigrationRecord("tools", "src", "skill", "review", v4sha("a"), v4sha("a"), func(r *lockfile.MarketplaceResourceLock) {
			r.ArchiveDigest = lockfile.Digest(v4sha("b"))
		}),
	}}
	result, err := MigrateConfigFilesToV4WithEvidence(context.Background(), MigratePaths{Repo: path}, V4MigrationRepo, true, evidence)
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if !result.DryRun || !result.RepoMigrated {
		t.Fatalf("result = %#v", result)
	}
	if evidence.calls != 1 {
		t.Fatalf("evidence calls = %d", evidence.calls)
	}
	assertFileUnchanged(t, path, before)
	assertFileMissing(t, lockPath)
}

func TestMigrateConfigFilesToV4MarketplaceCommitFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, ".agents", "config.yaml")
	before := writeLegacyMarketplaceConfig(t, path,
		"  - url: https://market.example\n    instance_id: mp\n    resources:\n      - registry_id: src\n        registry_slug: tools\n        kind: skill\n        name: review\n        revision: "+v4sha("a")+"\n        digest: "+v4sha("a")+"\n        direct: true\n")
	lockPath := marketplaceLockPath(t, path)
	evidence := &migrationEvidenceStub{records: map[string]lockfile.MarketplaceResourceLock{
		"skill/review": marketplaceMigrationRecord("tools", "src", "skill", "review", v4sha("a"), v4sha("a"), func(r *lockfile.MarketplaceResourceLock) {
			r.ArchiveDigest = lockfile.Digest(v4sha("b"))
		}),
	}}
	original := commitV4Migration
	commitV4Migration = func(ctx context.Context, stateDir string, files []dependencytxn.DesiredFile, opts ...dependencytxn.Option) (dependencytxn.Result, error) {
		return dependencytxn.Result{}, errors.New("injected commit failure")
	}
	t.Cleanup(func() { commitV4Migration = original })
	_, err := MigrateConfigFilesToV4WithEvidence(context.Background(), MigratePaths{Repo: path}, V4MigrationRepo, false, evidence)
	if err == nil || !strings.Contains(err.Error(), "injected commit failure") {
		t.Fatalf("expected commit failure, got %v", err)
	}
	assertFileUnchanged(t, path, before)
	assertFileMissing(t, lockPath)
	// No transaction residue alongside the config.
	entries, readErr := os.ReadDir(filepath.Dir(path))
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("transaction residue %q", entry.Name())
		}
	}
}

func TestMigrateConfigFilesToV4MarketplaceAllScopeSingleCommit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	dir := t.TempDir()
	userPath := filepath.Join(dir, "home", ".agents", "config.yaml")
	repoPath := filepath.Join(dir, "repo", ".agents", "config.yaml")
	entry := "  - url: https://market.example\n    instance_id: mp\n    resources:\n      - registry_id: src\n        registry_slug: tools\n        kind: skill\n        name: review\n        revision: " + v4sha("a") + "\n        digest: " + v4sha("a") + "\n        direct: true\n"
	writeLegacyMarketplaceConfig(t, userPath, entry)
	writeLegacyMarketplaceConfig(t, repoPath, entry)
	evidence := &migrationEvidenceStub{records: map[string]lockfile.MarketplaceResourceLock{
		"skill/review": marketplaceMigrationRecord("tools", "src", "skill", "review", v4sha("a"), v4sha("a"), func(r *lockfile.MarketplaceResourceLock) {
			r.ArchiveDigest = lockfile.Digest(v4sha("b"))
		}),
	}}
	original := commitV4Migration
	commits := 0
	commitV4Migration = func(ctx context.Context, stateDir string, files []dependencytxn.DesiredFile, opts ...dependencytxn.Option) (dependencytxn.Result, error) {
		commits++
		if len(files) != 6 {
			return dependencytxn.Result{}, fmt.Errorf("expected 6 desired files, got %d", len(files))
		}
		return original(ctx, stateDir, files, opts...)
	}
	t.Cleanup(func() { commitV4Migration = original })
	result, err := MigrateConfigFilesToV4WithEvidence(context.Background(), MigratePaths{User: userPath, Repo: repoPath}, V4MigrationAll, false, evidence)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	if commits != 1 {
		t.Fatalf("commits = %d", commits)
	}
	if !result.UserMigrated || !result.RepoMigrated {
		t.Fatalf("result = %#v", result)
	}
	for _, p := range []string{userPath, repoPath} {
		if _, err := os.Stat(marketplaceLockPath(t, p)); err != nil {
			t.Fatalf("lock missing for %s: %v", p, err)
		}
	}
	if _, err := LoadConfigFromPath(repoPath); err != nil {
		t.Fatalf("hydrate %s: %v", repoPath, err)
	}
	// The bare filename makes LoadConfigFromPath classify the user file as a
	// repo layer; validate the user lock scope directly instead.
	userDoc, err := lockfile.Decode(mustReadFile(t, marketplaceLockPath(t, userPath)))
	if err != nil {
		t.Fatal(err)
	}
	if userDoc.Scope != lockfile.ScopeUser || len(userDoc.Marketplaces) != 1 {
		t.Fatalf("user lock = %#v", userDoc)
	}
}

func TestMigrateConfigFilesToV4GitRejectsConcurrentConfigWriteWithoutTouchingLock(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	dir := t.TempDir()
	path := filepath.Join(dir, ".agents", "config.yaml")
	writeV2Config(t, path, `apiVersion: agent-marketplace/v2
registries:
  - url: https://example.test/agents.git
    ref: main
    skills: {include: [review]}
`)
	lockPath := marketplaceLockPath(t, path)
	lockBefore := []byte("apiVersion: agent-marketplace-lock/v1\nscope: repo\nregistries: []\n")
	if err := os.WriteFile(lockPath, lockBefore, 0o644); err != nil {
		t.Fatal(err)
	}
	original := commitV4Migration
	concurrent := []byte("apiVersion: agent-marketplace/v2\n# concurrent writer\n")
	commitV4Migration = func(ctx context.Context, stateDir string, files []dependencytxn.DesiredFile, opts ...dependencytxn.Option) (dependencytxn.Result, error) {
		if err := os.WriteFile(path, concurrent, 0o644); err != nil {
			t.Fatal(err)
		}
		return original(ctx, stateDir, files, opts...)
	}
	t.Cleanup(func() { commitV4Migration = original })
	_, err := MigrateConfigFilesToV4(MigratePaths{Repo: path}, V4MigrationRepo, false)
	if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("expected ErrGenerationConflict, got %v", err)
	}
	assertFileUnchanged(t, path, concurrent)
	assertFileUnchanged(t, lockPath, lockBefore)
}

func TestMigrateConfigFilesToV4MarketplaceRejectsConcurrentConfigWrite(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	dir := t.TempDir()
	path := filepath.Join(dir, ".agents", "config.yaml")
	writeLegacyMarketplaceConfig(t, path,
		"  - url: https://market.example\n    instance_id: mp\n    resources:\n      - registry_id: src\n        registry_slug: tools\n        kind: skill\n        name: review\n        revision: "+v4sha("a")+"\n        digest: "+v4sha("a")+"\n        direct: true\n")
	lockPath := marketplaceLockPath(t, path)
	evidence := &migrationEvidenceStub{records: map[string]lockfile.MarketplaceResourceLock{
		"skill/review": marketplaceMigrationRecord("tools", "src", "skill", "review", v4sha("a"), v4sha("a"), func(r *lockfile.MarketplaceResourceLock) {
			r.ArchiveDigest = lockfile.Digest(v4sha("b"))
		}),
	}}
	original := commitV4Migration
	concurrent := []byte("apiVersion: agent-marketplace/v2\n# concurrent writer\n")
	commitV4Migration = func(ctx context.Context, stateDir string, files []dependencytxn.DesiredFile, opts ...dependencytxn.Option) (dependencytxn.Result, error) {
		if err := os.WriteFile(path, concurrent, 0o644); err != nil {
			t.Fatal(err)
		}
		return original(ctx, stateDir, files, opts...)
	}
	t.Cleanup(func() { commitV4Migration = original })
	_, err := MigrateConfigFilesToV4WithEvidence(context.Background(), MigratePaths{Repo: path}, V4MigrationRepo, false, evidence)
	if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("expected ErrGenerationConflict, got %v", err)
	}
	assertFileUnchanged(t, path, concurrent)
	assertFileMissing(t, lockPath)
}

func TestMigrateConfigFilesToV4MarketplaceRejectsConcurrentLockWrite(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	dir := t.TempDir()
	path := filepath.Join(dir, ".agents", "config.yaml")
	before := writeLegacyMarketplaceConfig(t, path,
		"  - url: https://market.example\n    instance_id: mp\n    resources:\n      - registry_id: src\n        registry_slug: tools\n        kind: skill\n        name: review\n        revision: "+v4sha("a")+"\n        digest: "+v4sha("a")+"\n        direct: true\n")
	lockPath := marketplaceLockPath(t, path)
	record := marketplaceMigrationRecord("tools", "src", "skill", "review", v4sha("a"), v4sha("a"), func(r *lockfile.MarketplaceResourceLock) {
		r.ArchiveDigest = lockfile.Digest(v4sha("b"))
	})
	existingDoc, err := lockfile.Normalize(lockfile.Document{
		APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo,
		Marketplaces: []lockfile.MarketplaceLock{{
			URL: "https://market.example", InstanceID: "mp",
			RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "tools", RegistryID: "src"}},
			Resources:        []lockfile.MarketplaceResourceLock{record},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	existingBytes, err := lockfile.Encode(existingDoc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, existingBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	evidence := &migrationEvidenceStub{records: map[string]lockfile.MarketplaceResourceLock{"skill/review": record}}
	original := commitV4Migration
	concurrent := []byte("apiVersion: agent-marketplace-lock/v2\nscope: repo\n# concurrent writer\n")
	commitV4Migration = func(ctx context.Context, stateDir string, files []dependencytxn.DesiredFile, opts ...dependencytxn.Option) (dependencytxn.Result, error) {
		if err := os.WriteFile(lockPath, concurrent, 0o644); err != nil {
			t.Fatal(err)
		}
		return original(ctx, stateDir, files, opts...)
	}
	t.Cleanup(func() { commitV4Migration = original })
	_, err = MigrateConfigFilesToV4WithEvidence(context.Background(), MigratePaths{Repo: path}, V4MigrationRepo, false, evidence)
	if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("expected ErrGenerationConflict, got %v", err)
	}
	assertFileUnchanged(t, lockPath, concurrent)
	assertFileUnchanged(t, path, before)
}

func TestMigrateConfigFilesToV4MarketplaceTwoEndpointsReverseOrder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	dir := t.TempDir()
	path := filepath.Join(dir, ".agents", "config.yaml")
	writeLegacyMarketplaceConfig(t, path,
		"  - url: https://zeta.example\n    instance_id: zeta-id\n    resources:\n      - registry_id: zreg\n        kind: skill\n        name: zed\n        revision: "+v4sha("e")+"\n        digest: "+v4sha("e")+"\n        direct: true\n"+
			"  - url: https://market.example\n    instance_id: mp\n    resources:\n      - registry_id: src\n        registry_slug: tools\n        kind: skill\n        name: review\n        revision: "+v4sha("a")+"\n        digest: "+v4sha("a")+"\n        direct: true\n")
	reviewRecord := marketplaceMigrationRecord("tools", "src", "skill", "review", v4sha("a"), v4sha("a"), func(r *lockfile.MarketplaceResourceLock) {
		r.ArchiveDigest = lockfile.Digest(v4sha("b"))
	})
	zedRecord := marketplaceMigrationRecord("zreg", "zreg", "skill", "zed", v4sha("e"), v4sha("e"), func(r *lockfile.MarketplaceResourceLock) {
		r.ArchiveDigest = lockfile.Digest(v4sha("f"))
	})
	evidence := &migrationEvidenceStub{records: map[string]lockfile.MarketplaceResourceLock{
		"skill/review": reviewRecord,
		"skill/zed":    zedRecord,
	}}
	// Existing lock already stores the canonical sorted endpoint order.
	lockPath := marketplaceLockPath(t, path)
	existingDoc, err := lockfile.Normalize(lockfile.Document{
		APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo,
		Marketplaces: []lockfile.MarketplaceLock{
			{URL: "https://zeta.example", InstanceID: "zeta-id",
				RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "zreg", RegistryID: "zreg"}},
				Resources:        []lockfile.MarketplaceResourceLock{zedRecord}},
			{URL: "https://market.example", InstanceID: "mp",
				RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "tools", RegistryID: "src"}},
				Resources:        []lockfile.MarketplaceResourceLock{reviewRecord}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	existingBytes, err := lockfile.Encode(existingDoc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, existingBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateConfigFilesToV4WithEvidence(context.Background(), MigratePaths{Repo: path}, V4MigrationRepo, false, evidence); err != nil {
		t.Fatalf("reverse-order migration failed: %v", err)
	}
	if _, err := LoadConfigFromPath(path); err != nil {
		t.Fatalf("hydrate migrated pair: %v", err)
	}
}

func TestMigrateConfigFilesToV4MarketplaceDerivedInstalledNameRefused(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, ".agents", "config.yaml")
	before := writeLegacyMarketplaceConfig(t, path,
		"  - url: https://market.example\n    instance_id: mp\n    resources:\n"+
			"      - registry_id: cols\n        registry_slug: collections\n        kind: collection\n        name: onboarding\n        revision: "+v4sha("c")+"\n        digest: "+v4sha("c")+"\n        direct: true\n"+
			"      - registry_id: src\n        registry_slug: tools\n        kind: skill\n        name: review\n        revision: "+v4sha("a")+"\n        digest: "+v4sha("a")+"\n        installed_name: chosen-review\n        selected_by:\n          - registry_id: cols\n            name: onboarding\n            revision: "+v4sha("c")+"\n")
	lockPath := marketplaceLockPath(t, path)
	edge := []lockfile.MarketplaceSelectionLock{{RegistryID: "cols", Name: "onboarding", Revision: lockfile.Digest(v4sha("c"))}}
	evidence := &migrationEvidenceStub{records: map[string]lockfile.MarketplaceResourceLock{
		"collection/onboarding": marketplaceMigrationRecord("collections", "cols", "collection", "onboarding", v4sha("c"), v4sha("c"), func(r *lockfile.MarketplaceResourceLock) {
			r.CollectionManifestDigest = lockfile.Digest(v4sha("c"))
		}),
		"skill/review": marketplaceMigrationRecord("tools", "src", "skill", "review", v4sha("a"), v4sha("a"), func(r *lockfile.MarketplaceResourceLock) {
			r.ArchiveDigest = lockfile.Digest(v4sha("b"))
			r.SelectedBy = edge
		}),
	}}
	_, err := MigrateConfigFilesToV4WithEvidence(context.Background(), MigratePaths{Repo: path}, V4MigrationRepo, false, evidence)
	var v4err *V4ValidationError
	if !errors.As(err, &v4err) || v4err.Code != "v4.migration-marketplace-evidence" {
		t.Fatalf("expected v4.migration-marketplace-evidence, got %v", err)
	}
	assertFileUnchanged(t, path, before)
	assertFileMissing(t, lockPath)
}
