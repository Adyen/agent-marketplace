package pipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

func setSkillWithRequires(t *testing.T, f *layerMarketplaceServer, registry store.Registry, name string, requires []any) {
	t.Helper()
	skill := ir.NewSkill(name, name+" description", name+" body", []string{"user"}, map[string]any{
		"metadata": map[string]any{"requires": requires},
	}, ir.SourceRef{})
	artifact, err := hccartifact.FromIR(skill)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	artifact.Manifest.RegistryID = string(registry.ID)
	artifact.Manifest.RegistrySlug = registry.Slug
	artifact.Manifest.Revision = artifact.Manifest.ContentDigest
	raw, archiveDigest, err := hccartifact.Encode(artifact)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	f.resources[registry.Slug+"/skill/"+name] = layerMarketplaceResource{
		kind: "skill", name: name, raw: raw, archiveDigest: archiveDigest,
		revision: layerAssessedRevision(store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: store.MarketplaceID(f.instanceID), Registry: registry.ID, Kind: "skill", Name: name},
			ContentDigest: store.RevisionDigest(artifact.Manifest.ContentDigest),
			ArchiveDigest: store.ArchiveDigest(archiveDigest),
		}),
	}
}

type requiresHeldScope struct {
	name       string
	source     config.Source
	scope      Scope
	configPath func(home, repo string) string
	lockPath   func(home, repo string) string
}

// requiresHeldFixture installs a parent pin through the layer mutation, then
// adds a lock record for "helper" held only by a requires edge from parent.
// It returns the config and lock paths and the fake server.
func requiresHeldFixture(t *testing.T, expand bool, scope requiresHeldScope) (home, repo, configPath, lockPath string, fake *layerMarketplaceServer) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	repo = t.TempDir()
	configPath = scope.configPath(home, repo)
	lockPath = scope.lockPath(home, repo)
	requires := "requires: {expand: false}\n"
	if expand {
		requires = "requires: {expand: true}\n"
	}
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	writeFileMode(t, userConfig, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n", 0o600)
	writeFileMode(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n"+requires, 0o600)
	fake = newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	setSkillWithRequires(t, fake, registry, "parent", []any{"skill:helper"})
	fake.addSkill(t, registry, "helper")
	entry := config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}
	pins := layerPreparePins(t, fake, "skill", "parent")
	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, scope.source, func(m *MarketplaceLayerMutation) error {
		return m.IncludePins(entry, pins)
	}, nil); err != nil {
		t.Fatalf("install parent: %v", err)
	}

	doc := layerMarketplaceLockDoc(t, lockPath)
	parent := marketplaceLockResource(doc, "skill", "parent")
	if parent == nil {
		t.Fatalf("lock missing parent record: %#v", doc.Marketplaces[0].Resources)
	}
	helper := *parent
	helper.Name = "helper"
	helper.SelectedBy = []lockfile.MarketplaceSelectionLock{{
		RegistryID: parent.RegistryID, Kind: "skill", Name: "parent", Revision: parent.ContentDigest,
	}}
	doc.Marketplaces[0].Resources = append(doc.Marketplaces[0].Resources, helper)
	data, err := lockfile.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	writeFileMode(t, lockPath, string(data), 0o644)
	return home, repo, configPath, lockPath, fake
}

var requiresHeldScopes = []requiresHeldScope{
	{name: "user", source: config.UserPersonal, scope: ScopeUser,
		configPath: func(home, repo string) string { return filepath.Join(home, ".agents", "config.yaml") },
		lockPath:   func(home, repo string) string { return filepath.Join(home, ".agents", "lock.yaml") }},
	{name: "repo", source: config.RepoCommitted, scope: ScopeRepo,
		configPath: func(home, repo string) string { return filepath.Join(repo, ".agents", "config.yaml") },
		lockPath:   func(home, repo string) string { return filepath.Join(repo, ".agents", "lock.yaml") }},
}

// spec: resource-removal/enabled-removal-of-a-requires-held-marketplace-prerequisite
func TestRemoveRequiresHeldPinEnabledExpansion(t *testing.T) {
	for _, scope := range requiresHeldScopes {
		t.Run(scope.name, func(t *testing.T) {
			_, repo, configPath, lockPath, _ := requiresHeldFixture(t, true, scope)
			result, err := Remove(context.Background(), RemoveOptions{
				RepoRoot: repo, ResourceName: "helper", Kind: ir.KindSkill, HasKind: true,
				Scope: scope.scope, ScopeSpecified: true,
			})
			if err != nil {
				t.Fatalf("remove: %v", err)
			}
			if result.RemovedName != "helper" {
				t.Fatalf("result = %#v", result)
			}

			loaded, err := config.LoadConfigFromPath(configPath)
			if err != nil {
				t.Fatal(err)
			}
			var exclusion string
			for _, ex := range loaded.MarketplaceExclusions {
				exclusion = ex.Locator
			}
			if exclusion != "source/skill/helper" {
				t.Fatalf("exact exclusion missing in mutated layer: %#v", loaded.MarketplaceExclusions)
			}
			raw, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), "source/skill/parent") {
				t.Fatalf("requiring resource lost its selection:\n%s", raw)
			}

			doc := layerMarketplaceLockDoc(t, lockPath)
			if marketplaceLockResource(doc, "skill", "helper") != nil {
				t.Fatalf("requires-held record survived removal: %#v", doc.Marketplaces[0].Resources)
			}
			parent := marketplaceLockResource(doc, "skill", "parent")
			if parent == nil {
				t.Fatalf("requiring resource lost its lock record: %#v", doc.Marketplaces[0].Resources)
			}
			for _, edge := range parent.SelectedBy {
				if edge.Kind != "" {
					t.Fatalf("removal wrote a requires edge: %#v", edge)
				}
			}

			var unmet *resolver.UnmetRequirementWarning
			found := false
			for _, warning := range result.Warnings {
				if errors.As(warning, &unmet) && unmet.Requirement.Name == "helper" {
					found = true
				}
			}
			if !found {
				t.Fatalf("requiring resource did not report its unmet prerequisite: %v", result.Warnings)
			}
		})
	}
}

// spec: resource-removal/advisory-removal-of-a-requires-held-marketplace-prerequisite
func TestRemoveRequiresHeldPinAdvisoryExpansion(t *testing.T) {
	for _, scope := range requiresHeldScopes {
		t.Run(scope.name, func(t *testing.T) {
			_, repo, configPath, lockPath, _ := requiresHeldFixture(t, false, scope)
			result, err := Remove(context.Background(), RemoveOptions{
				RepoRoot: repo, ResourceName: "helper", Kind: ir.KindSkill, HasKind: true,
				Scope: scope.scope, ScopeSpecified: true,
			})
			if err != nil {
				t.Fatalf("remove: %v", err)
			}
			if result.RemovedName != "helper" {
				t.Fatalf("result = %#v", result)
			}

			raw, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "exclude") {
				t.Fatalf("advisory removal wrote an exclusion:\n%s", raw)
			}
			doc := layerMarketplaceLockDoc(t, lockPath)
			if marketplaceLockResource(doc, "skill", "helper") != nil {
				t.Fatalf("requires-held record survived removal: %#v", doc.Marketplaces[0].Resources)
			}
			if marketplaceLockResource(doc, "skill", "parent") == nil {
				t.Fatalf("requiring resource lost its lock record: %#v", doc.Marketplaces[0].Resources)
			}
		})
	}
}

var _ resolver.MarketplaceRequirementLookup = transientOrganizationMarketplaceResolver{}

// spec: resource-prerequisites/unhinted-marketplace-prerequisite-uses-the-requiring-registry
func TestTransientOrganizationResolverForwardsRequirementLookup(t *testing.T) {
	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "helper")

	wrapped := transientOrganizationMarketplaceResolver{delegate: &marketplace.Resolver{Cache: marketplace.NewCache(t.TempDir())}}
	result, err := wrapped.LookupMarketplaceRequirement(context.Background(), resolver.MarketplaceRequirementQuery{
		Entry:        config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID},
		RegistryID:   string(registry.ID),
		RegistrySlug: registry.Slug,
		Kind:         "skill",
		Name:         "helper",
	})
	if err != nil {
		t.Fatalf("lookup = %v", err)
	}
	want := fake.resources["source/skill/helper"].revision.ContentDigest
	if !result.Found || result.ContentDigest != string(want) {
		t.Fatalf("forwarded lookup = %+v, want found %s", result, want)
	}
}

// spec: resource-prerequisites/lookup-candidate-is-previewed-not-recorded
func TestPreviewOnlyCandidateDoesNotBlockPruning(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	repo := t.TempDir()

	skillArt := func(name string) resolver.ResolvedArtifact {
		return resolver.ResolvedArtifact{
			Entry:       catalog.Entry{Kind: ir.KindSkill, Name: name, Resource: ir.NewSkill(name, name+" desc", name+" body", nil, nil, ir.SourceRef{})},
			Source:      config.RepoCommitted,
			Harnesses:   []hcc.Target{hcc.TargetDevin},
			RegistryURL: "https://example.invalid/registry.git",
			Ref:         "main",
			CatalogKey:  "locked-catalog",
		}
	}
	if _, err := materialize.Run(context.Background(), repo, []resolver.ResolvedArtifact{skillArt("stale"), skillArt("parent")}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	candidate := resolver.ResolvedArtifact{
		Entry:       catalog.Entry{Kind: ir.KindSkill, Name: "candidate"},
		Implied:     true,
		PreviewOnly: true,
		RequiredBy:  []resolver.ResourceRef{{Kind: ir.KindSkill, Name: "parent"}},
		Source:      config.RepoCommitted,
		Harnesses:   []hcc.Target{hcc.TargetDevin},
	}
	rep, err := materialize.Run(context.Background(), repo, []resolver.ResolvedArtifact{skillArt("parent")},
		materialize.WithImpliedPreview(materializableImpliedPreview([]resolver.ResolvedArtifact{candidate})))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, d := range rep.Diags {
		if d.Code == "materialize.resource.nil" {
			t.Fatalf("preview-only candidate reached materialization: %+v", d)
		}
	}
	if _, err := os.Lstat(filepath.Join(repo, ".devin", "skills", "stale")); !os.IsNotExist(err) {
		t.Fatalf("preview-only candidate blocked unrelated stale pruning: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".devin", "skills", "candidate")); !os.IsNotExist(err) {
		t.Fatalf("preview-only candidate preserved or created an output: %v", err)
	}
}
