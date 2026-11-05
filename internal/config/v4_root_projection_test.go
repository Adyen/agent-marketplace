package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

// spec: consumer-config-format/v4-fragments-preserve-include-authority
func TestV4RootProjectionUsesIncludedBindingsWithoutChildSelections(t *testing.T) {
	for _, source := range []Source{UserPersonal, RepoCommitted, RepoPersonal} {
		t.Run(source.String(), func(t *testing.T) {
			fixture := newV4OnlyFixture(t)
			root := fixture.paths[source]
			fragment := filepath.Join(filepath.Dir(root), "fragments", "sources.yaml")
			registry := filepath.Join(filepath.Dir(fragment), "registry")
			mustMkdirAll(t, registry)
			fragmentBefore := v4OnlyWriteFixture(t, fragment, `sources:
  shared: {directory: ./registry}
  child-only: {git: https://example.test/child.git, ref: main}
resources:
  shared: {include: [skill/child], all: true, required: true, tags: [child-tag], harnesses: [factory]}
  child-only: [command/child]
`)
			rootBefore := v4OnlyWriteFixture(t, root, "apiVersion: agent-marketplace/v4\ninclude: fragments/sources.yaml\nresources: {shared: [skill/root]}\n")
			cfg, err := LoadRootConfigFromPath(root)
			if err != nil {
				t.Fatalf("root projection rejected included source binding: %v", err)
			}
			canonicalRegistry, err := filepath.EvalSymlinks(registry)
			if err != nil {
				t.Fatal(err)
			}
			canonicalRoot, err := filepath.EvalSymlinks(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(cfg.Registries) != 1 {
				t.Fatalf("child-only source leaked: %#v", cfg.Registries)
			}
			entry := cfg.Registries[0]
			if entry.URL != canonicalRegistry || !reflect.DeepEqual(entry.Skills.Include, []string{"root"}) || entry.Default || entry.Required || entry.Tags != nil || entry.Harnesses != nil || len(entry.V4Decisions) != 1 {
				t.Fatalf("root projection adopted child policy/selections or lost directory base: %#v", entry)
			}
			if entry.V4Decisions[0].Provenance.File != canonicalRoot || entry.V4Decisions[0].Request == nil || entry.V4Decisions[0].Request.Provenance[0].File != canonicalRoot {
				t.Fatalf("root decision provenance = %#v", entry.V4Decisions)
			}
			full, err := LoadConfigFromPath(root)
			if err != nil || len(full.Registries) != 2 || !full.Registries[1].Required || !full.Registries[1].V4All {
				t.Fatalf("full tree control = %#v, %v", full, err)
			}
			v4OnlyAssertUnchanged(t, root, rootBefore)
			v4OnlyAssertUnchanged(t, fragment, fragmentBefore)
		})
	}
}

func TestV4RootProjectionValidatesIncludedSourceBindings(t *testing.T) {
	for _, tc := range []struct{ name, fragment, rootFields, code string }{
		{"conflicting source", "sources: {shared: {git: https://example.test/child.git, ref: main}}\n", "sources: {shared: {git: https://example.test/root.git, ref: main}}\nresources: {shared: [skill/root]}\n", "v4.source-conflict"},
		{"undefined child source", "resources: {missing: [skill/child]}\n", "sources: {root: {git: https://example.test/root.git, ref: main}}\nresources: {root: [skill/root]}\n", "v4.undefined-source"},
		{"unsupported fragment", "apiVersion: agent-marketplace/v3\n", "sources: {root: {git: https://example.test/root.git, ref: main}}\n", "v4.api-version"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newV4OnlyFixture(t)
			root := fixture.paths[RepoCommitted]
			fragment := filepath.Join(filepath.Dir(root), "fragment.yaml")
			before := v4OnlyWriteFixture(t, fragment, tc.fragment)
			rootBefore := v4OnlyWriteFixture(t, root, "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\n"+tc.rootFields)
			_, err := LoadRootConfigFromPath(root)
			var validation *V4ValidationError
			if !errors.As(err, &validation) || validation.Code != tc.code {
				t.Fatalf("invalid full tree escaped root projection: %v, want %s", err, tc.code)
			}
			v4OnlyAssertUnchanged(t, root, rootBefore)
			v4OnlyAssertUnchanged(t, fragment, before)
		})
	}
}

func TestV4SourceOnlyMarketplaceNeedsNoPinEvidence(t *testing.T) {
	for _, source := range []Source{UserPersonal, RepoCommitted, RepoPersonal} {
		t.Run(source.String(), func(t *testing.T) {
			fixture := newV4OnlyFixture(t)
			root := fixture.paths[source]
			before := v4OnlyWriteFixture(t, root, "apiVersion: agent-marketplace/v4\nsources: {catalog: {marketplace: https://MARKET.example/}}\n")
			for _, load := range []func(string) (*Config, error){LoadRootConfigFromPath, LoadConfigFromPath} {
				cfg, err := load(root)
				if err != nil {
					t.Fatalf("authored source-only registration requires premature acquisition: %v", err)
				}
				if len(cfg.Marketplaces) != 1 || cfg.Marketplaces[0].URL != "https://market.example" || cfg.Marketplaces[0].Alias != "catalog" || cfg.Marketplaces[0].Source != source || cfg.Marketplaces[0].InstanceID != "" || len(cfg.Marketplaces[0].Resources) != 0 {
					t.Fatalf("unacquired registration fabricated or omitted evidence: %#v", cfg.Marketplaces)
				}
			}
			intent := ReadIntent(t.Context(), LoadEffectiveOptions{RepoRoot: fixture.repo})
			bound := Bind(intent, Evidence{})
			if len(bound.Errors) != 0 || len(bound.Merged.Marketplaces) != 1 {
				t.Fatalf("pre-acquisition install consumer cannot bind authored registration: %v", errors.Join(bound.Errors...))
			}
			v4OnlyAssertUnchanged(t, root, before)
			lockPath := filepath.Join(filepath.Dir(root), "lock.yaml")
			if source == RepoPersonal {
				lockPath = filepath.Join(filepath.Dir(root), "lock.local.yaml")
			}
			assertFileMissing(t, lockPath)
			v4OnlyWriteFixture(t, root, "apiVersion: agent-marketplace/v4\nsources: {catalog: {marketplace: https://market.example}}\nresources: {catalog: [tools/skill/review]}\n")
			for _, load := range []func(string) (*Config, error){LoadRootConfigFromPath, LoadConfigFromPath} {
				_, err := load(root)
				if !MissingMarketplaceEvidence(err) {
					t.Fatalf("selected Marketplace resource bypassed required evidence: %v", err)
				}
			}
		})
	}
}

func TestV4RootProjectionMarketplaceEvidenceSubset(t *testing.T) {
	for _, ownCollection := range []bool{false, true} {
		t.Run(fmt.Sprintf("collection=%t", ownCollection), func(t *testing.T) {
			fixture := newV4OnlyFixture(t)
			root := fixture.paths[RepoCommitted]
			fragment := filepath.Join(filepath.Dir(root), "fragment.yaml")
			rootRequest, childRequest := "tools/skill/review: {as: root-review, revision: "+v4sha("a")+"}", "main/collection/onboarding"
			if ownCollection {
				rootRequest, childRequest = "main/collection/onboarding", "tools/skill/review: {as: child-review}"
			}
			fragmentBefore := v4OnlyWriteFixture(t, fragment, "sources:\n  shared: {marketplace: https://market.example}\n  child-only: {marketplace: https://other.example}\nresources:\n  shared:\n    include: ["+childRequest+"]\n  child-only: [tools/skill/other]\n")
			rootBefore := v4OnlyWriteFixture(t, root, "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nresources:\n  shared:\n    include: ["+rootRequest+"]\n")
			doc := v4LockFixture(t, lockfile.ScopeRepo)
			other := v4LockResource("tools", "other-id", "skill", "other", v4sha("1"), v4sha("1"))
			other.ArchiveDigest = lockfile.Digest(v4sha("2"))
			doc.Marketplaces = append(doc.Marketplaces, lockfile.MarketplaceLock{URL: "https://other.example", InstanceID: "other-instance", RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "tools", RegistryID: "other-id"}}, Resources: []lockfile.MarketplaceResourceLock{other}})
			encoded, err := lockfile.Encode(doc)
			if err != nil {
				t.Fatal(err)
			}
			lockPath := filepath.Join(filepath.Dir(root), "lock.yaml")
			lockBefore := v4OnlyWriteFixture(t, lockPath, string(encoded))
			cfg, err := LoadRootConfigFromPath(root)
			if err != nil {
				t.Fatalf("root projection cannot use full owning-lock evidence: %v", err)
			}
			if len(cfg.Marketplaces) != 1 || cfg.Marketplaces[0].Alias != "shared" || cfg.Marketplaces[0].InstanceID != "market-id" {
				t.Fatalf("child-only endpoint adopted: %#v", cfg.Marketplaces)
			}
			pins := cfg.Marketplaces[0].Resources
			if !ownCollection {
				if len(pins) != 1 || pins[0].Name != "review" || !pins[0].Direct || pins[0].InstalledName != "root-review" || len(pins[0].SelectedBy.List()) != 0 {
					t.Fatalf("root leaf adopted child collection reachability/options: %#v", pins)
				}
			} else {
				if len(pins) != 4 {
					t.Fatalf("root collection lost its acquired closure: %#v", pins)
				}
				for _, pin := range pins {
					if pin.Direct != (pin.Kind == "collection") || pin.InstalledName != "" {
						t.Fatalf("child direct request became root-owned: %#v", pin)
					}
				}
			}
			full, err := LoadConfigFromPath(root)
			if err != nil || len(full.Marketplaces) != 2 {
				t.Fatalf("full owning-lock control failed: %#v %v", full, err)
			}
			v4OnlyAssertUnchanged(t, root, rootBefore)
			v4OnlyAssertUnchanged(t, fragment, fragmentBefore)
			v4OnlyAssertUnchanged(t, lockPath, lockBefore)
			unexpected := v4LockResource("tools", "tools-id", "skill", "unrequested", v4sha("3"), v4sha("3"))
			unexpected.ArchiveDigest = lockfile.Digest(v4sha("4"))
			doc.Marketplaces[0].Resources = append(doc.Marketplaces[0].Resources, unexpected)
			encoded, err = lockfile.Encode(doc)
			if err != nil {
				t.Fatal(err)
			}
			badBefore := v4OnlyWriteFixture(t, lockPath, string(encoded))
			_, err = LoadRootConfigFromPath(root)
			var validation *V4ValidationError
			if !errors.As(err, &validation) || validation.Code != "v4.marketplace-lock-mismatch" {
				t.Fatalf("projection hid an invalid full owning lock: %v", err)
			}
			v4OnlyAssertUnchanged(t, lockPath, badBefore)
		})
	}
}

func TestV4UnacquiredRegistrationAliasBoundaries(t *testing.T) {
	fixture := newV4OnlyFixture(t)
	for _, source := range []Source{UserPersonal, RepoCommitted} {
		v4OnlyWriteFixture(t, fixture.paths[source], "apiVersion: agent-marketplace/v4\nsources: {catalog: {marketplace: https://market.example}}\n")
	}
	intent := ReadIntent(t.Context(), LoadEffectiveOptions{RepoRoot: fixture.repo})
	bound := Bind(intent, Evidence{})
	if len(bound.Errors) != 0 || len(bound.Merged.Marketplaces) != 2 {
		t.Fatalf("same endpoint registrations failed: %v", errors.Join(bound.Errors...))
	}
	v4OnlyWriteFixture(t, fixture.paths[RepoCommitted], "apiVersion: agent-marketplace/v4\nsources: {catalog: {marketplace: https://other.example}}\n")
	intent = ReadIntent(t.Context(), LoadEffectiveOptions{RepoRoot: fixture.repo})
	bound = Bind(intent, Evidence{})
	if len(bound.Errors) == 0 || !strings.Contains(errors.Join(bound.Errors...).Error(), "duplicates") {
		t.Fatalf("unacquired alias conflicts escaped validation: %v", errors.Join(bound.Errors...))
	}
}

// spec: consumer-config-format/v4-fragments-preserve-include-authority
func TestV4RootProjectionUserLinkKeepsLogicalLockAndDeclaringBases(t *testing.T) {
	fixture := newV4OnlyFixture(t)
	root := fixture.paths[UserPersonal]
	contentDir := filepath.Join(filepath.Dir(filepath.Dir(root)), "dotfiles")
	target := filepath.Join(contentDir, "consumer.yaml")
	fragment := filepath.Join(contentDir, "fragments", "bindings.yaml")
	registry := filepath.Join(filepath.Dir(fragment), "registry")
	mustMkdirAll(t, registry)
	fragmentBefore := v4OnlyWriteFixture(t, fragment, "sources:\n  local: {directory: ./registry}\n  shared: {marketplace: https://market.example}\nresources:\n  local: [skill/child]\n  shared: [main/collection/onboarding]\n")
	targetBefore := v4OnlyWriteFixture(t, target, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\ninclude: fragments/bindings.yaml\nresources:\n  local: [skill/root]\n  shared: [tools/skill/review]\n")
	mustMkdirAll(t, filepath.Dir(root))
	if err := os.Symlink(target, root); err != nil {
		t.Fatal(err)
	}
	encoded, err := lockfile.Encode(v4LockFixture(t, lockfile.ScopeUser))
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(filepath.Dir(root), "lock.yaml")
	lockBefore := v4OnlyWriteFixture(t, lockPath, string(encoded))
	cfg, err := LoadRootConfigFromPath(root)
	if err != nil || len(cfg.Registries) != 1 || len(cfg.Marketplaces) != 1 || len(cfg.Marketplaces[0].Resources) != 1 {
		t.Fatalf("linked root projection = %#v, %v", cfg, err)
	}
	canonicalRegistry, err := filepath.EvalSymlinks(registry)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Registries[0].URL != canonicalRegistry || !reflect.DeepEqual(cfg.Registries[0].Skills.Include, []string{"root"}) {
		t.Fatalf("declaring fragment base lost: %#v", cfg.Registries[0])
	}
	canonical, data, err := readEffectiveLayer(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	layer, err := readRootLayerIntent(root, data, userLayer, canonical)
	if err != nil {
		t.Fatal(err)
	}
	if len(layer.Reads) != 2 || layer.Reads[1].Path != canonical || layer.Reads[1].RequestedPath != root {
		t.Fatalf("captured root/include identities = %#v", layer.Reads)
	}
	paths, err := ResolveConfigLockPaths(root)
	if err != nil || paths.RequestedLock != lockPath {
		t.Fatalf("logical lock ownership = %#v, %v", paths, err)
	}
	if text, err := os.Readlink(root); err != nil || text != target {
		t.Fatalf("content link changed: %q, %v", text, err)
	}
	v4OnlyAssertUnchanged(t, target, targetBefore)
	v4OnlyAssertUnchanged(t, fragment, fragmentBefore)
	v4OnlyAssertUnchanged(t, lockPath, lockBefore)
	assertFileMissing(t, filepath.Join(contentDir, "lock.yaml"))
}

func TestV4SourceOnlyRegistrationCanPrecedeMarketplaceLockReconciliation(t *testing.T) {
	fixture := newV4OnlyFixture(t)
	root := fixture.paths[RepoCommitted]
	v4OnlyWriteFixture(t, root, "apiVersion: agent-marketplace/v4\nsources: {catalog: {marketplace: https://market.example}}\n")
	doc := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo}
	encoded, err := lockfile.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(filepath.Dir(root), "lock.yaml")
	before := v4OnlyWriteFixture(t, lockPath, string(encoded))
	cfg, err := LoadConfigFromPath(root)
	if err != nil || len(cfg.Marketplaces) != 1 || cfg.Marketplaces[0].InstanceID != "" || len(cfg.Marketplaces[0].Resources) != 0 {
		t.Fatalf("authored registration cannot precede lock reconciliation: %#v, %v", cfg, err)
	}
	strict, err := LoadV4ConfigIfPresent(root, RepoCommitted)
	if err != nil {
		t.Fatal(err)
	}
	_, err = strict.ToLegacyConfigWithLock(repoLayer, &doc)
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "v4.marketplace-lock-mismatch" {
		t.Fatalf("exact runtime binding accepted missing endpoint evidence: %v", err)
	}
	v4OnlyAssertUnchanged(t, lockPath, before)
}

func TestV4SourceOnlyRegistrationCannotHideMalformedDeployedIdentity(t *testing.T) {
	fixture := newV4OnlyFixture(t)
	root := fixture.paths[RepoCommitted]
	v4OnlyWriteFixture(t, root, "apiVersion: agent-marketplace/v4\nsources: {catalog: {marketplace: https://market.example}}\n")
	intent := ReadIntent(t.Context(), LoadEffectiveOptions{RepoRoot: fixture.repo})
	var evidence Evidence
	evidence.Locks[RepoCommitted] = &lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo, Marketplaces: []lockfile.MarketplaceLock{{URL: "https://market.example"}}}
	bound := Bind(intent, evidence)
	var validation *V4ValidationError
	if !errors.As(errors.Join(bound.Errors...), &validation) || validation.Code != "v4.marketplace-lock-mismatch" {
		t.Fatalf("malformed present identity became an unacquired registration: %v", errors.Join(bound.Errors...))
	}
}

func TestV4RootProjectionRejectsForeignMarketplaceLockScope(t *testing.T) {
	fixture := newV4OnlyFixture(t)
	root := fixture.paths[RepoCommitted]
	v4OnlyWriteFixture(t, root, v4MarketplaceConfigYAML)
	doc := v4LockFixture(t, lockfile.ScopeUser)
	encoded, err := lockfile.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(filepath.Dir(root), "lock.yaml")
	before := v4OnlyWriteFixture(t, lockPath, string(encoded))
	_, err = LoadRootConfigFromPath(root)
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "v4.marketplace-lock-mismatch" {
		t.Fatalf("foreign lock scope accepted: %v", err)
	}
	v4OnlyAssertUnchanged(t, lockPath, before)
	entries, err := os.ReadDir(filepath.Dir(root))
	if err != nil || len(entries) != 2 {
		t.Fatalf("projection read left residue: %v %v", entries, err)
	}
}
