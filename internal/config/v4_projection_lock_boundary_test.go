package config

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func projectionBoundaryLock(t *testing.T, scope lockfile.Scope) lockfile.Document {
	t.Helper()
	resource := v4LockResource("tools", "tools-id", "skill", "review", v4sha("a"), v4sha("a"))
	resource.ArchiveDigest = lockfile.Digest(v4sha("b"))
	doc, err := lockfile.Normalize(lockfile.Document{
		APIVersion: lockfile.APIVersionV2,
		Scope:      scope,
		Marketplaces: []lockfile.MarketplaceLock{{
			URL: "https://market.example", InstanceID: "market-id",
			RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "tools", RegistryID: "tools-id"}},
			Resources:        []lockfile.MarketplaceResourceLock{resource},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// spec: consumer-config-format/v4-fragments-preserve-include-authority
func TestV4ProjectedRootRequiresFullyIncludedMarketplaceEvidence(t *testing.T) {
	for _, source := range []Source{UserPersonal, RepoCommitted, RepoPersonal} {
		for _, state := range []string{"absent", "corrupt", "wrong scope", "missing endpoint", "unrequested resource", "valid"} {
			t.Run(source.String()+"/"+state, func(t *testing.T) {
				fixture := newV4OnlyFixture(t)
				root := fixture.paths[source]
				fragment := filepath.Join(filepath.Dir(root), "fragment.yaml")
				registry := filepath.Join(fixture.repo, "registry")
				mustMkdirAll(t, registry)
				fragmentBefore := v4OnlyWriteFixture(t, fragment, "sources: {catalog: {marketplace: https://market.example}}\nresources: {catalog: [tools/skill/review]}\n")
				rootBefore := v4OnlyWriteFixture(t, root, "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nsources: {root: {directory: "+registry+"}}\nresources: {root: [command/owned]}\n")
				lockPath := source.LockPath(root)
				doc := projectionBoundaryLock(t, lockfile.Scope(source.LockScope()))
				var lockBefore v4OnlyFileState
				if state != "absent" {
					var encoded []byte
					var err error
					switch state {
					case "corrupt":
						encoded = []byte("marketplaces: [\n")
					case "wrong scope":
						doc.Scope = lockfile.ScopeRepo
						if source == RepoCommitted {
							doc.Scope = lockfile.ScopeUser
						}
					case "missing endpoint":
						doc.Marketplaces = nil
					case "unrequested resource":
						unexpected := v4LockResource("tools", "tools-id", "skill", "unrequested", v4sha("c"), v4sha("c"))
						unexpected.ArchiveDigest = lockfile.Digest(v4sha("d"))
						doc.Marketplaces[0].Resources = append(doc.Marketplaces[0].Resources, unexpected)
					}
					if encoded == nil {
						encoded, err = lockfile.Encode(doc)
						if err != nil {
							t.Fatal(err)
						}
					}
					lockBefore = v4OnlyWriteFixture(t, lockPath, string(encoded))
				}
				intent := ReadIntent(t.Context(), LoadEffectiveOptions{RepoRoot: fixture.repo})
				if len(intent.Errors) != 0 || !intent.Layers[source].NeedsMarketplaceLock() {
					t.Fatalf("authored include intent depends on acquisition evidence: %v", errors.Join(intent.Errors...))
				}
				cfg, err := LoadRootConfigFromPath(root)
				switch state {
				case "valid":
					if err != nil || cfg == nil || len(cfg.Marketplaces) != 0 || len(cfg.Registries) != 1 || !reflect.DeepEqual(cfg.Registries[0].Commands.Include, []string{"owned"}) {
						t.Fatalf("valid full-tree evidence adopted a child selection or lost the root: %#v, %v", cfg, err)
					}
				case "corrupt":
					var coded *lockfile.Error
					if !errors.As(err, &coded) || coded.Code != lockfile.CodeMalformedYAML {
						t.Fatalf("projection hid corrupt owning evidence: %v", err)
					}
				default:
					var coded *V4ValidationError
					want := "v4.marketplace-lock-mismatch"
					if state == "absent" {
						want = "v4.marketplace-lock-required"
					}
					if !errors.As(err, &coded) || coded.Code != want {
						t.Fatalf("projection hid %s owning evidence: %v, want %s", state, err, want)
					}
				}
				v4OnlyAssertUnchanged(t, root, rootBefore)
				v4OnlyAssertUnchanged(t, fragment, fragmentBefore)
				if state == "absent" {
					assertFileMissing(t, lockPath)
				} else {
					v4OnlyAssertUnchanged(t, lockPath, lockBefore)
				}
				assertNoConfigTempResidue(t, filepath.Dir(root))
			})
		}
	}
}

func TestV4ProjectedResourceFreeRegistrationKeepsReadAndWriterRolesSeparate(t *testing.T) {
	for _, source := range []Source{UserPersonal, RepoCommitted, RepoPersonal} {
		t.Run(source.String(), func(t *testing.T) {
			fixture := newV4OnlyFixture(t)
			root := fixture.paths[source]
			fragment := filepath.Join(filepath.Dir(root), "fragment.yaml")
			fragmentBefore := v4OnlyWriteFixture(t, fragment, "sources: {catalog: {marketplace: https://market.example}}\n")
			rootBefore := v4OnlyWriteFixture(t, root, "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\n")
			intent := ReadIntent(t.Context(), LoadEffectiveOptions{RepoRoot: fixture.repo})
			layer := intent.Layers[source]
			if len(intent.Errors) != 0 || !layer.NeedsMarketplaceLock() || !layer.NeedsLock() {
				t.Fatalf("endpoint registration omitted from explicit lock requirements: %#v, %v", layer, errors.Join(intent.Errors...))
			}
			registrations := layer.MarketplaceSources()
			if len(registrations) != 1 || registrations[0].Name != "catalog" || registrations[0].URL != "https://market.example" || len(registrations[0].Requests) != 0 {
				t.Fatalf("endpoint-only lock producer inputs = %#v", registrations)
			}
			bound := Bind(intent, Evidence{})
			if len(bound.Errors) != 0 || len(bound.Merged.Marketplaces) != 1 || bound.Merged.Marketplaces[0].InstanceID != "" || len(bound.Merged.Marketplaces[0].Resources) != 0 {
				t.Fatalf("authored read acquired or fabricated endpoint identity: %#v, %v", bound.Merged, errors.Join(bound.Errors...))
			}
			projection, err := LoadRootConfigFromPath(root)
			if err != nil || len(projection.Marketplaces) != 0 {
				t.Fatalf("resource-free projection adopted included registration: %#v, %v", projection, err)
			}
			v4OnlyAssertUnchanged(t, root, rootBefore)
			v4OnlyAssertUnchanged(t, fragment, fragmentBefore)
			assertFileMissing(t, source.LockPath(root))
		})
	}
}

func TestV4EmptyMarketplaceExclusionIsARegistrationNotAnOverride(t *testing.T) {
	for _, source := range []Source{UserPersonal, RepoCommitted, RepoPersonal} {
		t.Run(source.String(), func(t *testing.T) {
			fixture := newV4OnlyFixture(t)
			root := fixture.paths[source]
			before := v4OnlyWriteFixture(t, root, "apiVersion: agent-marketplace/v4\nsources: {catalog: {marketplace: https://market.example}}\nresources: {catalog: {exclude: []}}\n")
			intent := ReadIntent(t.Context(), LoadEffectiveOptions{RepoRoot: fixture.repo})
			layer := intent.Layers[source]
			if len(intent.Errors) != 0 || !layer.NeedsMarketplaceLock() || !layer.NeedsLock() || len(layer.MarketplaceSources()) != 1 {
				t.Fatalf("empty exclusion erased an endpoint registration: %#v, %v", layer, errors.Join(intent.Errors...))
			}
			for _, load := range []func(string) (*Config, error){LoadConfigFromPath, LoadRootConfigFromPath} {
				cfg, err := load(root)
				if err != nil || len(cfg.Marketplaces) != 1 || cfg.Marketplaces[0].Alias != "catalog" || cfg.Marketplaces[0].InstanceID != "" || len(cfg.Marketplaces[0].Resources) != 0 || len(cfg.MarketplaceExclusions) != 0 {
					t.Fatalf("empty exclusion did not remain an unacquired registration: %#v, %v", cfg, err)
				}
			}
			v4OnlyAssertUnchanged(t, root, before)
			assertFileMissing(t, source.LockPath(root))
		})
	}
	fixture := newV4OnlyFixture(t)
	root := fixture.paths[UserPersonal]
	before := v4OnlyWriteFixture(t, root, "apiVersion: agent-marketplace/v4\nsources: {catalog: {marketplace: https://market.example}}\nresources: {catalog: {exclude: [tools/skill/review]}}\n")
	intent := ReadIntent(t.Context(), LoadEffectiveOptions{RepoRoot: fixture.repo})
	layer := intent.Layers[UserPersonal]
	if len(intent.Errors) != 0 || layer.NeedsMarketplaceLock() || layer.NeedsLock() || len(layer.MarketplaceSources()) != 0 {
		t.Fatalf("non-empty override became an acquisition owner: %#v, %v", layer, errors.Join(intent.Errors...))
	}
	cfg, err := LoadConfigFromPath(root)
	if err != nil || len(cfg.Marketplaces) != 0 || len(cfg.MarketplaceExclusions) != 1 || cfg.MarketplaceExclusions[0].Locator != "tools/skill/review" {
		t.Fatalf("non-empty exclusion control = %#v, %v", cfg, err)
	}
	v4OnlyAssertUnchanged(t, root, before)
	assertFileMissing(t, UserPersonal.LockPath(root))
}

func TestV4EmptyExclusionRegistrationRetainsStaleEndpointChecks(t *testing.T) {
	fixture := newV4OnlyFixture(t)
	root := fixture.paths[RepoCommitted]
	v4OnlyWriteFixture(t, root, "apiVersion: agent-marketplace/v4\nsources: {catalog: {marketplace: https://market.example}}\nresources: {catalog: {exclude: []}}\n")
	doc := projectionBoundaryLock(t, lockfile.ScopeRepo)
	doc.Marketplaces[0].Resources = nil
	unexpected := lockfile.MarketplaceLock{URL: "https://unexpected.example", InstanceID: "unexpected-id"}
	doc.Marketplaces = append(doc.Marketplaces, unexpected)
	encoded, err := lockfile.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	before := v4OnlyWriteFixture(t, RepoCommitted.LockPath(root), string(encoded))
	_, err = LoadConfigFromPath(root)
	var coded *V4ValidationError
	if !errors.As(err, &coded) || coded.Code != "v4.marketplace-lock-mismatch" {
		t.Fatalf("empty exclusion hid a stale endpoint record: %v", err)
	}
	v4OnlyAssertUnchanged(t, RepoCommitted.LockPath(root), before)
}
