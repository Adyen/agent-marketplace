package resolver

// harness_test.go covers harness inheritance and Resource attachment
// introduced in Phase 2.
//
// Rules under test:
//   - ResolvedArtifact.Harnesses inherits from top-level config by default
//   - ResolvedArtifact.Harnesses is overridden by per-entry harnesses
//   - ResolvedArtifact.Resource carries the *ir.Skill from the catalog
//   - Harnesses are nil when neither top-level nor per-entry is set
//   - Harnesses survive cross-layer exclude (only the skill is removed,
//     not its harness config)
//   - Local-enabled skills inherit harnesses from their source entry

import (
	"reflect"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
)

// helper: convert string harness names to hcc.Target slice
func targets(names ...string) []hcc.Target {
	out := make([]hcc.Target, len(names))
	for i, n := range names {
		out[i] = hcc.Target(n)
	}
	return out
}

func ckHarness(url, ref string) string {
	return cache.CatalogKeyForEntry(url, ref, "")
}

func TestHarnessInheritsFromTopLevel(t *testing.T) {
	// Use config.Merge to properly propagate top-level harnesses into
	// entry fields, matching production flow.
	repo := &config.Config{
		Harnesses: []string{"factory", "cursor"},
		Registries: []config.RegistryEntry{{
			URL:    "git@example.com:infra.git",
			Ref:    "main",
			Skills: ks("vm-lifecycle"),
		}},
	}
	cfg := config.Merge(nil, repo)
	setCatalogKeys(cfg)

	catalogs := map[string][]catalog.Entry{
		ckHarness("git@example.com:infra.git", "main"): {
			{Name: "vm-lifecycle", Path: "/cache/vm-lifecycle"},
		},
	}

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(got))
	}

	wantHarnesses := targets("factory", "cursor")
	if !reflect.DeepEqual(got[0].Harnesses, wantHarnesses) {
		t.Errorf("expected Harnesses %v, got %v", wantHarnesses, got[0].Harnesses)
	}
}

func TestHarnessPerEntryOverridesTopLevel(t *testing.T) {
	// Use config.Merge to properly resolve inheritance.
	repo := &config.Config{
		Harnesses: []string{"factory", "cursor"},
		Registries: []config.RegistryEntry{
			{
				URL:    "git@example.com:default.git",
				Ref:    "main",
				Skills: ks("default-skill"),
				// no per-entry override → inherits top-level
			},
			{
				URL:       "git@example.com:override.git",
				Ref:       "main",
				Skills:    ks("override-skill"),
				Harnesses: []string{"claude"}, // overrides top-level
			},
		},
	}
	cfg := config.Merge(nil, repo)
	setCatalogKeys(cfg)

	catalogs := map[string][]catalog.Entry{
		ckHarness("git@example.com:default.git", "main"): {
			{Name: "default-skill", Path: "/cache/default-skill"},
		},
		ckHarness("git@example.com:override.git", "main"): {
			{Name: "override-skill", Path: "/cache/override-skill"},
		},
	}

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(got))
	}

	byName := make(map[string]ResolvedArtifact)
	for _, r := range got {
		byName[r.Name] = r
	}

	defaultSkill := byName["default-skill"]
	overrideSkill := byName["override-skill"]

	if !reflect.DeepEqual(defaultSkill.Harnesses, targets("factory", "cursor")) {
		t.Errorf("default-skill should inherit top-level harnesses, got %v", defaultSkill.Harnesses)
	}
	if !reflect.DeepEqual(overrideSkill.Harnesses, targets("claude")) {
		t.Errorf("override-skill should use per-entry harnesses, got %v", overrideSkill.Harnesses)
	}
}

func TestHarnessNilWhenNeitherTopLevelNorPerEntry(t *testing.T) {
	// v1-style config with no harnesses at all
	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:    "git@example.com:legacy.git",
			Ref:    "main",
			Skills: ks("old-skill"),
		}},
	}
	setCatalogKeys(cfg)

	catalogs := map[string][]catalog.Entry{
		ckHarness("git@example.com:legacy.git", "main"): {
			{Name: "old-skill", Path: "/cache/old-skill"},
		},
	}

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(got))
	}

	// When neither top-level nor per-entry harnesses are set,
	// Harnesses should be nil (not an empty slice).
	if got[0].Harnesses != nil {
		t.Errorf("expected nil Harnesses for v1 config without harnesses, got %v", got[0].Harnesses)
	}
}

func TestHarnessEmptySliceWhenPerEntryExplicitlyEmpty(t *testing.T) {
	// Per-entry harnesses set to empty list should explicitly opt out of
	// harness compilation for this entry, even when top-level harnesses exist.
	repo := &config.Config{
		Harnesses: []string{"factory"},
		Registries: []config.RegistryEntry{{
			URL:       "git@example.com:skip.git",
			Ref:       "main",
			Skills:    ks("skip-skill"),
			Harnesses: []string{}, // explicit empty override
		}},
	}
	cfg := config.Merge(nil, repo)
	setCatalogKeys(cfg)

	catalogs := map[string][]catalog.Entry{
		ckHarness("git@example.com:skip.git", "main"): {
			{Name: "skip-skill", Path: "/cache/skip-skill"},
		},
	}

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(got))
	}

	if len(got[0].Harnesses) != 0 {
		t.Fatalf("expected per-entry empty harness override to disable harness targets, got %v", got[0].Harnesses)
	}
}

func TestResourceAttachedFromSkillResources(t *testing.T) {
	repo := &config.Config{
		Harnesses: []string{"factory"},
		Registries: []config.RegistryEntry{{
			URL:    "git@example.com:infra.git",
			Ref:    "main",
			Skills: ks("vm-lifecycle"),
		}},
	}
	cfg := config.Merge(nil, repo)
	setCatalogKeys(cfg)

	// Build catalog with the IR resource attached.
	irSkill := ir.NewSkill("vm-lifecycle", "VM lifecycle skill", "body", nil, nil, ir.SourceRef{})
	catalogs := map[string][]catalog.Entry{
		ckHarness("git@example.com:infra.git", "main"): {
			{Name: "vm-lifecycle", Path: "/cache/vm-lifecycle", Resource: irSkill},
		},
	}

	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(got))
	}

	if got[0].Resource == nil {
		t.Fatalf("expected Resource to be attached, got nil")
	}
	if got[0].Resource.Name() != "vm-lifecycle" {
		t.Errorf("expected Resource.Name() = %q, got %q", "vm-lifecycle", got[0].Resource.Name())
	}
}

func TestResourceNilWhenNoSkillResourcesProvided(t *testing.T) {
	repo := &config.Config{
		Harnesses: []string{"factory"},
		Registries: []config.RegistryEntry{{
			URL:    "git@example.com:infra.git",
			Ref:    "main",
			Skills: ks("vm-lifecycle"),
		}},
	}
	cfg := config.Merge(nil, repo)
	setCatalogKeys(cfg)

	catalogs := map[string][]catalog.Entry{
		ckHarness("git@example.com:infra.git", "main"): {
			{Name: "vm-lifecycle", Path: "/cache/vm-lifecycle"},
		},
	}

	// No WithSkillResources option
	got, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(got))
	}

	if got[0].Resource != nil {
		t.Errorf("expected Resource to be nil when no SkillResources provided, got non-nil")
	}
}

func TestHarnessInheritedByLocalEnabledSkill(t *testing.T) {
	repo := &config.Config{
		Harnesses: []string{"factory", "cursor"},
		Registries: []config.RegistryEntry{{
			URL:    "git@example.com:infra.git",
			Ref:    "main",
			Skills: ks("vm-lifecycle"),
		}},
	}
	cfg := config.Merge(nil, repo)
	setCatalogKeys(cfg)

	catalogs := map[string][]catalog.Entry{
		ckHarness("git@example.com:infra.git", "main"): {
			{Name: "vm-lifecycle", Path: "/cache/vm-lifecycle"},
			{Name: "extra-skill", Path: "/cache/extra-skill"},
		},
	}

	localCfg := &config.LocalConfig{Skills: ks("extra-skill")}
	got, errs := Resolve(cfg, catalogs, localCfg)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	byName := make(map[string]ResolvedArtifact)
	for _, r := range got {
		byName[r.Name] = r
	}

	extra := byName["extra-skill"]
	if extra.Name != "extra-skill" {
		t.Fatalf("expected extra-skill to be resolved")
	}

	// Locally-enabled skills inherit the harnesses from their source entry
	wantHarnesses := targets("factory", "cursor")
	if !reflect.DeepEqual(extra.Harnesses, wantHarnesses) {
		t.Errorf("local-enabled skill should inherit harnesses from source entry, got %v", extra.Harnesses)
	}
}

func TestHarnessPreservedAfterCrossLayerExclude(t *testing.T) {
	// Cross-layer exclude removes a skill but the remaining skills
	// should still carry their harnesses correctly.
	// Both layers set Harnesses so entries inherit them.
	global := &config.Config{
		Harnesses: []string{"factory"},
		Registries: []config.RegistryEntry{{
			URL:    "git@example.com:global.git",
			Ref:    "main",
			Skills: ks("alpha", "bravo"),
		}},
	}
	repo := &config.Config{
		Harnesses: []string{"factory"},
		Registries: []config.RegistryEntry{{
			URL:    "git@example.com:repo.git",
			Ref:    "main",
			Skills: kse([]string{"charlie"}, []string{"alpha"}),
		}},
	}

	merged := config.Merge(global, repo)
	for i := range merged.Registries {
		switch merged.Registries[i].URL {
		case "git@example.com:global.git":
			merged.Registries[i].CatalogKey = "global-key"
		case "git@example.com:repo.git":
			merged.Registries[i].CatalogKey = "repo-key"
		}
	}

	catalogs := map[string][]catalog.Entry{
		"global-key": {{Name: "alpha", Path: "/a"}, {Name: "bravo", Path: "/b"}},
		"repo-key":   {{Name: "charlie", Path: "/c"}},
	}

	got, errs := Resolve(merged, catalogs, nil)
	if len(errs) != 1 {
		t.Fatalf("expected warning for ineffective repo exclude, got %v", errs)
	}

	for _, r := range got {
		if r.Name == "alpha" || r.Name == "bravo" || r.Name == "charlie" {
			wantHarnesses := targets("factory")
			if !reflect.DeepEqual(r.Harnesses, wantHarnesses) {
				t.Errorf("%s: expected harnesses %v, got %v", r.Name, wantHarnesses, r.Harnesses)
			}
		}
	}
}

func TestHarnessPerEntryOverrideInMergedConfig(t *testing.T) {
	// Global has harnesses: [factory]; repo entry overrides to [claude, cursor].
	// Skills from the repo entry should get the per-entry override.
	global := &config.Config{
		Harnesses: []string{"factory"},
		Registries: []config.RegistryEntry{{
			URL:    "git@example.com:global.git",
			Ref:    "main",
			Skills: ks("alpha"),
		}},
	}
	repo := &config.Config{
		Harnesses: []string{"factory"}, // repo also sets top-level
		Registries: []config.RegistryEntry{{
			URL:       "git@example.com:repo.git",
			Ref:       "main",
			Skills:    ks("bravo"),
			Harnesses: []string{"claude", "cursor"}, // per-entry override
		}},
	}

	merged := config.Merge(global, repo)
	for i := range merged.Registries {
		switch merged.Registries[i].URL {
		case "git@example.com:global.git":
			merged.Registries[i].CatalogKey = "global-key"
		case "git@example.com:repo.git":
			merged.Registries[i].CatalogKey = "repo-key"
		}
	}

	catalogs := map[string][]catalog.Entry{
		"global-key": {{Name: "alpha", Path: "/a"}},
		"repo-key":   {{Name: "bravo", Path: "/b"}},
	}

	got, errs := Resolve(merged, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	byName := make(map[string]ResolvedArtifact)
	for _, r := range got {
		byName[r.Name] = r
	}

	// alpha comes from global entry (no per-entry override → inherits top-level)
	if !reflect.DeepEqual(byName["alpha"].Harnesses, targets("factory")) {
		t.Errorf("alpha should inherit top-level harnesses, got %v", byName["alpha"].Harnesses)
	}

	// bravo comes from repo entry (per-entry override)
	if !reflect.DeepEqual(byName["bravo"].Harnesses, targets("claude", "cursor")) {
		t.Errorf("bravo should use per-entry harnesses, got %v", byName["bravo"].Harnesses)
	}
}
