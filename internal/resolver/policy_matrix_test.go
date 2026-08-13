package resolver

// policy_matrix_test.go covers the layer × policy matrix introduced in Phase 2.
//
// Matrix axes:
//   Source:  UserPersonal | RepoCommitted (two representative machine vs repo layers)
//   Policy:  required:true | default:true | neither (explicit selection only)
//   Action:  personal-exclude | cross-layer-exclude
//
// Rules under test:
//   required → immune to ALL excludes
//   default  → personal exclude applies; cross-layer exclude applies
//   neither  → personal exclude applies; cross-layer exclude applies
//
// Phase 2 extension: harness inheritance across policy actions.

import (
	"reflect"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
)

var policyMatrixCatalog = []catalog.Entry{
	{Name: "alpha", Path: "/cache/alpha"},
	{Name: "bravo", Path: "/cache/bravo"},
	{Name: "charlie", Path: "/cache/charlie"},
}

// buildMergedCfg constructs a merged config with:
//   - global entry: all three skills, policy as specified
//   - repo entry: explicit selection of "alpha"
func buildMergedWithGlobalPolicy(required, defaultPolicy bool) *config.Config {
	global := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:      "git@example.com:global.git",
			Ref:      "main",
			Required: required,
			Default:  defaultPolicy,
			Skills:   config.KindSelector{}, // auto-select all when required/default
		}},
	}
	if !required && !defaultPolicy {
		global.Registries[0].Skills = ks("alpha", "bravo", "charlie")
	}
	return config.MergeLayers([]config.Layer{
		{Source: config.UserPersonal, Config: global},
	})
}

func TestPolicyMatrix_RequiredImmuneToPersonalExclude(t *testing.T) {
	merged := buildMergedWithGlobalPolicy(true, false)
	// manually set CatalogKey since we're not hitting real cache
	for i := range merged.Registries {
		merged.Registries[i].CatalogKey = "global-key"
	}
	catalogs := map[string][]catalog.Entry{"global-key": policyMatrixCatalog}

	got, errs := Resolve(merged, catalogs, &config.LocalConfig{Skills: kse(nil, []string{"alpha", "bravo"})})
	if len(errs) != 2 {
		t.Fatalf("expected warnings for ineffective local excludes, got: %v", errs)
	}
	names := skillNames(got)
	if len(names) != 3 {
		t.Errorf("required skills must be immune to personal exclude: want 3, got %v", names)
	}
}

func TestPolicyMatrix_DefaultSubjectToPersonalExclude(t *testing.T) {
	merged := buildMergedWithGlobalPolicy(false, true)
	for i := range merged.Registries {
		merged.Registries[i].CatalogKey = "global-key"
	}
	catalogs := map[string][]catalog.Entry{"global-key": policyMatrixCatalog}

	got, errs := Resolve(merged, catalogs, &config.LocalConfig{Skills: kse(nil, []string{"alpha"})})
	if len(errs) != 1 {
		t.Fatalf("expected warning for ineffective local exclude, got: %v", errs)
	}
	names := skillNames(got)
	if len(names) != 3 {
		t.Errorf("expected all skills to remain for machine-scope default registry, got %v", names)
	}
}

func TestPolicyMatrix_ExplicitSubjectToPersonalExclude(t *testing.T) {
	merged := buildMergedWithGlobalPolicy(false, false)
	for i := range merged.Registries {
		merged.Registries[i].CatalogKey = "global-key"
	}
	catalogs := map[string][]catalog.Entry{"global-key": policyMatrixCatalog}

	got, errs := Resolve(merged, catalogs, &config.LocalConfig{Skills: kse(nil, []string{"bravo"})})
	if len(errs) != 1 {
		t.Fatalf("expected warning for ineffective local exclude, got: %v", errs)
	}
	names := skillNames(got)
	if len(names) != 3 {
		t.Errorf("expected all skills to remain for machine-scope explicit registry, got %v", names)
	}
}

func TestPolicyMatrix_RequiredImmuneToDefaultSkillAutoSelect(t *testing.T) {
	// Verify that default:true auto-selects all but required:true is unaffected.
	merged := buildMergedWithGlobalPolicy(true, false)
	for i := range merged.Registries {
		merged.Registries[i].CatalogKey = "global-key"
	}
	catalogs := map[string][]catalog.Entry{"global-key": policyMatrixCatalog}

	got, errs := Resolve(merged, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(got) != 3 {
		t.Errorf("required registry must auto-select all %d skills, got %d", 3, len(got))
	}
}

func TestPolicyMatrix_DefaultAutoSelectsAll(t *testing.T) {
	merged := buildMergedWithGlobalPolicy(false, true)
	for i := range merged.Registries {
		merged.Registries[i].CatalogKey = "global-key"
	}
	catalogs := map[string][]catalog.Entry{"global-key": policyMatrixCatalog}

	got, errs := Resolve(merged, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(got) != 3 {
		t.Errorf("default registry must auto-select all %d skills, got %d", 3, len(got))
	}
}

func TestPolicyMatrix_CrossLayerExcludeBlocksLowerLayerSkill(t *testing.T) {
	// User layer has skill "alpha"; repo layer excludes it via its registry
	// entry exclude list. This is ineffective for machine-scope skills.
	global := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:    "git@example.com:global.git",
			Ref:    "main",
			Skills: ks("alpha"),
		}},
	}
	repo := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:    "git@example.com:repo.git",
			Ref:    "main",
			Skills: kse([]string{"bravo"}, []string{"alpha"}),
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
		"global-key": policyMatrixCatalog,
		"repo-key":   policyMatrixCatalog,
	}

	got, errs := Resolve(merged, catalogs, nil)
	if len(errs) != 1 {
		t.Fatalf("expected warning for ineffective repo exclude, got %v", errs)
	}
	names := skillNames(got)
	foundAlpha := false
	for _, n := range names {
		if n == "alpha" {
			foundAlpha = true
		}
	}
	if !foundAlpha {
		t.Errorf("alpha from user layer should remain when repo exclude is ineffective")
	}
}

func TestPolicyMatrix_CrossLayerExcludeCannotRemoveRequiredSkill(t *testing.T) {
	// User layer has required skill "alpha"; repo layer tries to exclude it.
	// Required must win.
	global := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:      "git@example.com:global.git",
			Ref:      "main",
			Required: true,
			Skills:   ks("alpha"),
		}},
	}
	repo := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:    "git@example.com:repo.git",
			Ref:    "main",
			Skills: kse([]string{"bravo"}, []string{"alpha"}),
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
		"global-key": policyMatrixCatalog,
		"repo-key":   policyMatrixCatalog,
	}

	got, errs := Resolve(merged, catalogs, nil)
	if len(errs) != 1 {
		t.Fatalf("expected warning for ineffective repo exclude, got %v", errs)
	}
	names := skillNames(got)
	found := false
	for _, n := range names {
		if n == "alpha" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("required skill 'alpha' must survive cross-layer exclude attempt")
	}
}

// --- Phase 2: Harness inheritance in the policy matrix ---
//
// These tests extend the policy matrix with harness inheritance:
//   - Required skills that survive excludes carry their harnesses
//   - Skills from entries with per-entry harness overrides keep those overrides
//   - Cross-layer exclude doesn't affect harness assignment on surviving skills

func TestPolicyMatrix_RequiredSkillCarriesHarnessesAfterPersonalExclude(t *testing.T) {
	global := &config.Config{
		Harnesses: []string{"factory"},
		Registries: []config.RegistryEntry{{
			URL:      "git@example.com:global.git",
			Ref:      "main",
			Required: true,
		}},
	}
	merged := config.MergeLayers([]config.Layer{
		{Source: config.UserPersonal, Config: global},
	})
	for i := range merged.Registries {
		merged.Registries[i].CatalogKey = "global-key"
	}
	catalogs := map[string][]catalog.Entry{"global-key": policyMatrixCatalog}

	got, errs := Resolve(merged, catalogs, &config.LocalConfig{Skills: kse(nil, []string{"alpha"})})
	if len(errs) != 1 {
		t.Fatalf("expected warning for ineffective local exclude, got %v", errs)
	}

	// Required skill alpha survives; it should carry the inherited harnesses
	wantHarnesses := []hcc.Target{hcc.Target("factory")}
	for _, r := range got {
		if r.Name == "alpha" {
			if !reflect.DeepEqual(r.Harnesses, wantHarnesses) {
				t.Errorf("required skill alpha after personal-exclude should carry harnesses %v, got %v", wantHarnesses, r.Harnesses)
			}
		}
	}
}

func TestPolicyMatrix_DefaultSkillCarriesHarnessesAfterPersonalExclude(t *testing.T) {
	global := &config.Config{
		Harnesses: []string{"factory", "cursor"},
		Registries: []config.RegistryEntry{{
			URL:     "git@example.com:global.git",
			Ref:     "main",
			Default: true,
		}},
	}
	merged := config.MergeLayers([]config.Layer{
		{Source: config.UserPersonal, Config: global},
	})
	for i := range merged.Registries {
		merged.Registries[i].CatalogKey = "global-key"
	}
	catalogs := map[string][]catalog.Entry{"global-key": policyMatrixCatalog}

	got, errs := Resolve(merged, catalogs, &config.LocalConfig{Skills: kse(nil, []string{"alpha"})})
	if len(errs) != 1 {
		t.Fatalf("expected warning for ineffective local exclude, got %v", errs)
	}

	wantHarnesses := []hcc.Target{hcc.Target("factory"), hcc.Target("cursor")}
	for _, r := range got {
		if !reflect.DeepEqual(r.Harnesses, wantHarnesses) {
			t.Errorf("surviving default skill %s should carry harnesses %v, got %v", r.Name, wantHarnesses, r.Harnesses)
		}
	}
}

func TestPolicyMatrix_PerEntryHarnessOverrideOnRequiredEntry(t *testing.T) {
	global := &config.Config{
		Harnesses: []string{"factory"},
		Registries: []config.RegistryEntry{{
			URL:       "git@example.com:global.git",
			Ref:       "main",
			Required:  true,
			Harnesses: []string{"claude", "cursor"}, // per-entry override
		}},
	}
	merged := config.MergeLayers([]config.Layer{
		{Source: config.UserPersonal, Config: global},
	})
	for i := range merged.Registries {
		merged.Registries[i].CatalogKey = "global-key"
	}
	catalogs := map[string][]catalog.Entry{"global-key": policyMatrixCatalog}

	got, errs := Resolve(merged, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	wantHarnesses := []hcc.Target{hcc.Target("claude"), hcc.Target("cursor")}
	for _, r := range got {
		if !reflect.DeepEqual(r.Harnesses, wantHarnesses) {
			t.Errorf("required skill %s from per-entry-override entry should carry harnesses %v, got %v", r.Name, wantHarnesses, r.Harnesses)
		}
	}
}

func TestPolicyMatrix_CrossLayerExcludeSurvivingSkillKeepsHarnesses(t *testing.T) {
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
		"global-key": policyMatrixCatalog,
		"repo-key":   policyMatrixCatalog,
	}

	got, errs := Resolve(merged, catalogs, nil)
	if len(errs) != 1 {
		t.Fatalf("expected warning for ineffective repo exclude, got %v", errs)
	}

	wantHarnesses := []hcc.Target{hcc.Target("factory")}
	for _, r := range got {
		if !reflect.DeepEqual(r.Harnesses, wantHarnesses) {
			t.Errorf("surviving skill %s should carry harnesses %v, got %v", r.Name, wantHarnesses, r.Harnesses)
		}
	}
}
