package collection

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// ============================================================
// DEEP NESTING
// ============================================================

func TestExpandThreeLevelNesting(t *testing.T) {
	// grandchild → child → root. All skills should be merged into one
	// synthetic entry since they all reference the same registry.
	grandchild := Collection{
		Name:              "grandchild",
		Skills:            []CollectionSkill{{Name: "deep-skill"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	child := Collection{
		Name:              "child",
		Skills:            []CollectionSkill{{Name: "child-skill"}},
		Includes:          []CollectionInclude{{Name: "grandchild"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	root := Collection{
		Name:              "root",
		Skills:            []CollectionSkill{{Name: "root-skill"}},
		Includes:          []CollectionInclude{{Name: "child"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Source: config.RepoCommitted},
	}

	res, errs := Expand(
		[]string{"root"},
		[]Collection{root, child, grandchild},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(res.SyntheticEntries) != 1 {
		t.Fatalf("expected 1 synthetic entry, got %d", len(res.SyntheticEntries))
	}
	got := sortedCopy(res.SyntheticEntries[0].Skills.Include)
	want := []string{"child-skill", "deep-skill", "root-skill"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("skills = %#v, want %#v", got, want)
	}
}

func TestExpandThreeLevelCycleDetection(t *testing.T) {
	a := Collection{
		Name:              "a",
		Includes:          []CollectionInclude{{Name: "b"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	b := Collection{
		Name:              "b",
		Includes:          []CollectionInclude{{Name: "c"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	c := Collection{
		Name:              "c",
		Includes:          []CollectionInclude{{Name: "a"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main"},
	}

	_, errs := Expand(
		[]string{"a"},
		[]Collection{a, b, c},
		existing,
	)
	foundCycle := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "cycle") {
			foundCycle = true
			break
		}
	}
	if !foundCycle {
		t.Fatalf("expected cycle error in 3-node cycle, got %v", errs)
	}
}

// ============================================================
// CROSS-REGISTRY NESTED COLLECTIONS
// ============================================================

func TestExpandNestedCollectionFromDifferentRegistry(t *testing.T) {
	// Parent collection is from core.git, child collection is from
	// other.git. Each produces its own synthetic entry.
	child := Collection{
		Name:              "child",
		Skills:            []CollectionSkill{{Name: "other-skill"}},
		SourceRegistryURL: "git@example.com:other.git",
		SourceRef:         "stable",
	}
	parent := Collection{
		Name:              "parent",
		Skills:            []CollectionSkill{{Name: "core-skill"}},
		Includes:          []CollectionInclude{{Name: "child"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Source: config.RepoCommitted, CatalogKey: "/cache/core"},
		{URL: "git@example.com:other.git", Ref: "stable", Source: config.UserPersonal, CatalogKey: "/cache/other"},
	}

	res, errs := Expand(
		[]string{"parent"},
		[]Collection{parent, child},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(res.SyntheticEntries) != 2 {
		t.Fatalf("expected 2 synthetic entries (one per registry), got %d", len(res.SyntheticEntries))
	}

	coreEntry := lookupSynth(t, res.SyntheticEntries, "git@example.com:core.git")
	if !reflect.DeepEqual(coreEntry.Skills.Include, []string{"core-skill"}) {
		t.Fatalf("core entry skills = %#v", coreEntry.Skills.Include)
	}

	otherEntry := lookupSynth(t, res.SyntheticEntries, "git@example.com:other.git")
	if !reflect.DeepEqual(otherEntry.Skills.Include, []string{"other-skill"}) {
		t.Fatalf("other entry skills = %#v", otherEntry.Skills.Include)
	}
	// Source should be promoted to RepoCommitted (floor from parent's source registry).
	if otherEntry.Source != config.RepoCommitted {
		t.Fatalf("other entry source = %v, want RepoCommitted", otherEntry.Source)
	}
}

// ============================================================
// REGISTRY REUSE ACROSS COLLECTIONS
// ============================================================

func TestExpandMultipleCollectionsReuseSameExternalRegistry(t *testing.T) {
	// Two independent collections both reference the same external registry.
	// Skills should be merged into a single synthetic entry for that registry.
	col1 := Collection{
		Name:              "frontend",
		Skills:            []CollectionSkill{{Name: "react-skill", Registry: "git@example.com:shared.git"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	col2 := Collection{
		Name:              "backend",
		Skills:            []CollectionSkill{{Name: "go-skill", Registry: "git@example.com:shared.git"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Source: config.RepoCommitted},
		{URL: "git@example.com:shared.git", Ref: "stable", Source: config.UserPersonal, CatalogKey: "/cache/shared"},
	}

	res, errs := Expand(
		[]string{"frontend", "backend"},
		[]Collection{col1, col2},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(res.SyntheticEntries) != 1 {
		t.Fatalf("expected 1 synthetic entry (merged on shared.git), got %d", len(res.SyntheticEntries))
	}
	got := sortedCopy(res.SyntheticEntries[0].Skills.Include)
	if !reflect.DeepEqual(got, []string{"go-skill", "react-skill"}) {
		t.Fatalf("skills = %#v", got)
	}
}

// ============================================================
// SKILL RE-CREATION / SAME SKILL IN DIFFERENT COLLECTIONS
// ============================================================

func TestExpandSameSkillInMultipleCollectionsDeduplicates(t *testing.T) {
	// Two collections contribute the same skill name from the same registry.
	// The skill should appear only once in the synthetic entry.
	col1 := Collection{
		Name:              "base",
		Skills:            []CollectionSkill{{Name: "common-skill"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	col2 := Collection{
		Name:              "extra",
		Skills:            []CollectionSkill{{Name: "common-skill"}, {Name: "unique-skill"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Source: config.RepoCommitted},
	}

	res, errs := Expand(
		[]string{"base", "extra"},
		[]Collection{col1, col2},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(res.SyntheticEntries) != 1 {
		t.Fatalf("expected 1 synthetic entry, got %d", len(res.SyntheticEntries))
	}
	got := sortedCopy(res.SyntheticEntries[0].Skills.Include)
	if !reflect.DeepEqual(got, []string{"common-skill", "unique-skill"}) {
		t.Fatalf("expected deduped [common-skill unique-skill], got %#v", got)
	}
	// ViaMap: common-skill is overwritten by the last collection that
	// contributes it (extra in expansion order). This is by design —
	// the expansion processes collections sequentially and later writes
	// to the same key overwrite earlier ones.
	if res.ViaMap["skill:common-skill"] != "collection:extra" {
		t.Fatalf("ViaMap[skill:common-skill] = %q, want collection:extra", res.ViaMap["skill:common-skill"])
	}
	if res.ViaMap["skill:unique-skill"] != "collection:extra" {
		t.Fatalf("ViaMap[skill:unique-skill] = %q, want collection:extra", res.ViaMap["skill:unique-skill"])
	}
}

func TestExpandSameSkillInParentAndChildCollectionDeduplicates(t *testing.T) {
	// Parent includes child; both define the same skill. Should deduplicate.
	child := Collection{
		Name:              "child",
		Skills:            []CollectionSkill{{Name: "shared-skill"}, {Name: "child-only"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	parent := Collection{
		Name:              "parent",
		Skills:            []CollectionSkill{{Name: "shared-skill"}, {Name: "parent-only"}},
		Includes:          []CollectionInclude{{Name: "child"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Source: config.RepoCommitted},
	}

	res, errs := Expand(
		[]string{"parent"},
		[]Collection{parent, child},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(res.SyntheticEntries) != 1 {
		t.Fatalf("expected 1 synthetic entry, got %d", len(res.SyntheticEntries))
	}
	got := sortedCopy(res.SyntheticEntries[0].Skills.Include)
	if !reflect.DeepEqual(got, []string{"child-only", "parent-only", "shared-skill"}) {
		t.Fatalf("skills = %#v", got)
	}
}

// ============================================================
// SAME SKILL FROM TWO DIFFERENT REGISTRIES (CROSS-REGISTRY COLLISION)
// ============================================================

func TestExpandSameSkillFromDifferentRegistriesProducesTwoSyntheticEntries(t *testing.T) {
	// A skill named "review" exists in both core.git and other.git.
	// Each registry gets its own synthetic entry; name collision is the
	// caller's responsibility (resolver handles it).
	col := Collection{
		Name: "multi-source",
		Skills: []CollectionSkill{
			{Name: "review"}, // from source registry
			{Name: "review", Registry: "git@example.com:other.git", Ref: "stable"}, // from other registry
		},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Source: config.RepoCommitted, CatalogKey: "/cache/core"},
		{URL: "git@example.com:other.git", Ref: "stable", Source: config.UserPersonal, CatalogKey: "/cache/other"},
	}

	res, errs := Expand(
		[]string{"multi-source"},
		[]Collection{col},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(res.SyntheticEntries) != 2 {
		t.Fatalf("expected 2 synthetic entries (one per registry), got %d", len(res.SyntheticEntries))
	}

	coreEntry := lookupSynth(t, res.SyntheticEntries, "git@example.com:core.git")
	if !reflect.DeepEqual(coreEntry.Skills.Include, []string{"review"}) {
		t.Fatalf("core entry skills = %#v", coreEntry.Skills.Include)
	}

	otherEntry := lookupSynth(t, res.SyntheticEntries, "git@example.com:other.git")
	if !reflect.DeepEqual(otherEntry.Skills.Include, []string{"review"}) {
		t.Fatalf("other entry skills = %#v", otherEntry.Skills.Include)
	}
}

// ============================================================
// PATH-RELATED SCENARIOS
// ============================================================

func TestExpandCollectionSkillWithRegistryAndPathFromDifferentRegistry(t *testing.T) {
	// A collection skill that specifies registry, ref, AND path — all
	// explicit. Must produce a synthetic entry with the correct URL+ref+path.
	col := Collection{
		Name: "tools",
		Skills: []CollectionSkill{
			{Name: "custom-skill", Registry: "git@example.com:extra.git", Ref: "v2", Path: "plugins"},
		},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Source: config.RepoCommitted},
		{URL: "git@example.com:extra.git", Ref: "v2", Path: "plugins", Source: config.UserPersonal, CatalogKey: "/cache/extra:plugins"},
	}

	res, errs := Expand(
		[]string{"tools"},
		[]Collection{col},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(res.SyntheticEntries) != 1 {
		t.Fatalf("expected 1 synthetic entry, got %d", len(res.SyntheticEntries))
	}
	s := res.SyntheticEntries[0]
	if s.URL != "git@example.com:extra.git" {
		t.Fatalf("URL = %q, want extra.git", s.URL)
	}
	if s.Ref != "v2" {
		t.Fatalf("Ref = %q, want v2", s.Ref)
	}
	if s.Path != "plugins" {
		t.Fatalf("Path = %q, want plugins", s.Path)
	}
	if s.CatalogKey != "/cache/extra:plugins" {
		t.Fatalf("CatalogKey = %q, want /cache/extra:plugins", s.CatalogKey)
	}
}

func TestExpandMultipleCollectionsSameRegistryDifferentPathsProducesDistinctEntries(t *testing.T) {
	// Two collections reference the same registry but different paths.
	// Each must produce its own synthetic entry with the correct path.
	col1 := Collection{
		Name:              "factory-tools",
		Skills:            []CollectionSkill{{Name: "f-skill"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
		SourcePath:        ".factory/skills",
	}
	col2 := Collection{
		Name:              "claude-tools",
		Skills:            []CollectionSkill{{Name: "c-skill"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
		SourcePath:        "skills",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Path: ".factory/skills", Source: config.RepoCommitted, CatalogKey: "/cache/core:factory"},
		{URL: "git@example.com:core.git", Ref: "main", Path: "skills", Source: config.UserPersonal, CatalogKey: "/cache/core:claude"},
	}

	res, errs := Expand(
		[]string{"factory-tools", "claude-tools"},
		[]Collection{col1, col2},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(res.SyntheticEntries) != 2 {
		t.Fatalf("expected 2 synthetic entries, got %d", len(res.SyntheticEntries))
	}

	fEntry := lookupSynth(t, res.SyntheticEntries, "git@example.com:core.git", ".factory/skills")
	if fEntry.CatalogKey != "/cache/core:factory" {
		t.Fatalf("factory CatalogKey = %q", fEntry.CatalogKey)
	}
	if !reflect.DeepEqual(fEntry.Skills.Include, []string{"f-skill"}) {
		t.Fatalf("factory skills = %#v", fEntry.Skills.Include)
	}

	cEntry := lookupSynth(t, res.SyntheticEntries, "git@example.com:core.git", "skills")
	if cEntry.CatalogKey != "/cache/core:claude" {
		t.Fatalf("claude CatalogKey = %q", cEntry.CatalogKey)
	}
	if !reflect.DeepEqual(cEntry.Skills.Include, []string{"c-skill"}) {
		t.Fatalf("claude skills = %#v", cEntry.Skills.Include)
	}
}

func TestExpandCollectionSkillExplicitPathOverridesSourcePath(t *testing.T) {
	// When a collection skill specifies an explicit path, it must override
	// the collection's SourcePath (not inherit it).
	col := Collection{
		Name:              "tools",
		Skills:            []CollectionSkill{{Name: "override-skill", Path: "overrides"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
		SourcePath:        ".factory/skills",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Path: ".factory/skills", Source: config.RepoCommitted, CatalogKey: "/cache/core:factory"},
		{URL: "git@example.com:core.git", Ref: "main", Path: "overrides", Source: config.UserPersonal, CatalogKey: "/cache/core:overrides"},
	}

	res, errs := Expand(
		[]string{"tools"},
		[]Collection{col},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(res.SyntheticEntries) != 1 {
		t.Fatalf("expected 1 synthetic entry, got %d", len(res.SyntheticEntries))
	}
	s := res.SyntheticEntries[0]
	if s.Path != "overrides" {
		t.Fatalf("expected Path 'overrides' (explicit), got %q", s.Path)
	}
	if s.CatalogKey != "/cache/core:overrides" {
		t.Fatalf("expected CatalogKey for overrides path, got %q", s.CatalogKey)
	}
}

func TestExpandNestedCollectionDifferentPathsFromParent(t *testing.T) {
	// Parent collection is at .factory/skills, child collection is at
	// skills (different path). Both contribute skills; each path gets
	// its own synthetic entry.
	child := Collection{
		Name:              "child",
		Skills:            []CollectionSkill{{Name: "child-skill"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
		SourcePath:        "skills",
	}
	parent := Collection{
		Name:              "parent",
		Skills:            []CollectionSkill{{Name: "parent-skill"}},
		Includes:          []CollectionInclude{{Name: "child"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
		SourcePath:        ".factory/skills",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Path: ".factory/skills", Source: config.RepoCommitted, CatalogKey: "/cache/core:factory"},
		{URL: "git@example.com:core.git", Ref: "main", Path: "skills", Source: config.UserPersonal, CatalogKey: "/cache/core:claude"},
	}

	res, errs := Expand(
		[]string{"parent"},
		[]Collection{parent, child},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(res.SyntheticEntries) != 2 {
		t.Fatalf("expected 2 synthetic entries (different paths), got %d", len(res.SyntheticEntries))
	}

	fEntry := lookupSynth(t, res.SyntheticEntries, "git@example.com:core.git", ".factory/skills")
	if !reflect.DeepEqual(fEntry.Skills.Include, []string{"parent-skill"}) {
		t.Fatalf("factory skills = %#v", fEntry.Skills.Include)
	}

	cEntry := lookupSynth(t, res.SyntheticEntries, "git@example.com:core.git", "skills")
	if !reflect.DeepEqual(cEntry.Skills.Include, []string{"child-skill"}) {
		t.Fatalf("claude skills = %#v", cEntry.Skills.Include)
	}
}

func TestExpandCollectionWithMixedPathAndNoPathSkills(t *testing.T) {
	// One skill inherits SourcePath, another has explicit path.
	// Must produce two synthetic entries with correct paths.
	col := Collection{
		Name: "mixed-paths",
		Skills: []CollectionSkill{
			{Name: "inherited"},             // inherits SourcePath
			{Name: "explicit", Path: "cli"}, // explicit path
		},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
		SourcePath:        "skills",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Path: "skills", Source: config.RepoCommitted, CatalogKey: "/cache/core:skills"},
		{URL: "git@example.com:core.git", Ref: "main", Path: "cli", Source: config.UserPersonal, CatalogKey: "/cache/core:cli"},
	}

	res, errs := Expand(
		[]string{"mixed-paths"},
		[]Collection{col},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(res.SyntheticEntries) != 2 {
		t.Fatalf("expected 2 synthetic entries, got %d", len(res.SyntheticEntries))
	}

	skillsEntry := lookupSynth(t, res.SyntheticEntries, "git@example.com:core.git", "skills")
	if !reflect.DeepEqual(skillsEntry.Skills.Include, []string{"inherited"}) {
		t.Fatalf("skills entry = %#v", skillsEntry.Skills.Include)
	}

	cliEntry := lookupSynth(t, res.SyntheticEntries, "git@example.com:core.git", "cli")
	if !reflect.DeepEqual(cliEntry.Skills.Include, []string{"explicit"}) {
		t.Fatalf("cli entry = %#v", cliEntry.Skills.Include)
	}
}

// ============================================================
// HARNESS PROPAGATION
// ============================================================

func TestExpandSyntheticEntryInheritsHarnesses(t *testing.T) {
	// Per-entry harnesses should propagate to the synthetic entry.
	col := Collection{
		Name:              "tools",
		Skills:            []CollectionSkill{{Name: "review"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Source: config.RepoCommitted, Harnesses: []string{"factory", "claude"}},
	}

	res, errs := Expand(
		[]string{"tools"},
		[]Collection{col},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(res.SyntheticEntries) != 1 {
		t.Fatalf("expected 1 synthetic entry, got %d", len(res.SyntheticEntries))
	}
	s := res.SyntheticEntries[0]
	if !reflect.DeepEqual(s.Harnesses, []string{"factory", "claude"}) {
		t.Fatalf("Harnesses = %#v, want [factory claude]", s.Harnesses)
	}
}

func TestExpandSyntheticEntryInheritsHarnessesForPathSpecificEntry(t *testing.T) {
	// Per-entry harnesses should propagate for path-specific entries too.
	col := Collection{
		Name:              "tools",
		Skills:            []CollectionSkill{{Name: "review", Path: "skills"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
		SourcePath:        ".factory/skills",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Path: "skills", Source: config.RepoCommitted, Harnesses: []string{"claude"}},
		{URL: "git@example.com:core.git", Ref: "main", Path: ".factory/skills", Source: config.UserPersonal, Harnesses: []string{"factory"}},
	}

	res, errs := Expand(
		[]string{"tools"},
		[]Collection{col},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	s := lookupSynth(t, res.SyntheticEntries, "git@example.com:core.git", "skills")
	if !reflect.DeepEqual(s.Harnesses, []string{"claude"}) {
		t.Fatalf("Harnesses = %#v, want [claude]", s.Harnesses)
	}
}

func TestExpandSyntheticEntryInheritsSourceHarness(t *testing.T) {
	// Per-entry source_harness should propagate to the synthetic entry.
	col := Collection{
		Name:              "tools",
		Skills:            []CollectionSkill{{Name: "review"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Source: config.RepoCommitted, SourceHarness: "factory-plugin"},
	}

	res, errs := Expand(
		[]string{"tools"},
		[]Collection{col},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	s := res.SyntheticEntries[0]
	if s.SourceHarness != "factory-plugin" {
		t.Fatalf("SourceHarness = %q, want factory-plugin", s.SourceHarness)
	}
}

func TestExpandSyntheticEntryInheritsSourceHarnessForPathSpecificEntry(t *testing.T) {
	// source_harness should propagate for path-specific entries.
	col := Collection{
		Name:              "tools",
		Skills:            []CollectionSkill{{Name: "review", Path: "skills"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
		SourcePath:        ".factory/skills",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Path: "skills", Source: config.UserPersonal, SourceHarness: "claude-plugin"},
		{URL: "git@example.com:core.git", Ref: "main", Path: ".factory/skills", Source: config.RepoCommitted, SourceHarness: "factory-plugin"},
	}

	res, errs := Expand(
		[]string{"tools"},
		[]Collection{col},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	s := lookupSynth(t, res.SyntheticEntries, "git@example.com:core.git", "skills")
	if s.SourceHarness != "claude-plugin" {
		t.Fatalf("SourceHarness = %q, want claude-plugin", s.SourceHarness)
	}
}

// ============================================================
// EXCLUDE PROPAGATION
// ============================================================

func TestExpandSyntheticEntryExcludeIsPathAware(t *testing.T) {
	// Exclude list should be propagated from the matching path-specific
	// entry, not from a different path on the same URL.
	col := Collection{
		Name:              "tools",
		Skills:            []CollectionSkill{{Name: "review", Path: "skills"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
		SourcePath:        ".factory/skills",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Path: ".factory/skills", Source: config.RepoCommitted,
			Skills: config.KindSelector{Exclude: []string{"factory-legacy"}}},
		{URL: "git@example.com:core.git", Ref: "main", Path: "skills", Source: config.UserPersonal,
			Skills: config.KindSelector{Exclude: []string{"claude-legacy"}}},
	}

	res, errs := Expand(
		[]string{"tools"},
		[]Collection{col},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	s := lookupSynth(t, res.SyntheticEntries, "git@example.com:core.git", "skills")
	if !reflect.DeepEqual(s.Skills.Exclude, []string{"claude-legacy"}) {
		t.Fatalf("Exclude = %#v, want [claude-legacy] (path-aware match)", s.Skills.Exclude)
	}
}

// ============================================================
// LOCAL REGISTRY SCENARIOS
// ============================================================

func TestExpandLocalRegistryWithPathProducesPathSpecificEntry(t *testing.T) {
	// Local filesystem registry with a path suffix should produce a
	// path-specific synthetic entry.
	col := Collection{
		Name:              "local-tools",
		Skills:            []CollectionSkill{{Name: "local-skill", Registry: "/path/to/local", Path: "skills"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{
		{URL: "/path/to/local", Ref: "", Path: "skills", Source: config.RepoCommitted, CatalogKey: "/local/path:skills"},
	}

	res, errs := Expand(
		[]string{"local-tools"},
		[]Collection{col},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(res.SyntheticEntries) != 1 {
		t.Fatalf("expected 1 synthetic entry, got %d", len(res.SyntheticEntries))
	}
	s := res.SyntheticEntries[0]
	if s.Path != "skills" {
		t.Fatalf("expected Path 'skills', got %q", s.Path)
	}
	if s.CatalogKey != "/local/path:skills" {
		t.Fatalf("expected CatalogKey /local/path:skills, got %q", s.CatalogKey)
	}
}

func TestExpandCollectionFromLocalRegistryWithSkillFromRemoteRegistry(t *testing.T) {
	// Collection discovered from a local registry (SourceRegistryURL is a
	// local path) references a skill from a remote registry.
	col := Collection{
		Name:              "local-col",
		Skills:            []CollectionSkill{{Name: "remote-skill", Registry: "git@example.com:remote.git", Ref: "main"}},
		SourceRegistryURL: "/local/repo",
		SourceRef:         "",
	}
	existing := []config.RegistryEntry{
		{URL: "/local/repo", Ref: "", Source: config.RepoCommitted, CatalogKey: "/local/repo"},
		{URL: "git@example.com:remote.git", Ref: "main", Source: config.UserPersonal, CatalogKey: "/cache/remote"},
	}

	res, errs := Expand(
		[]string{"local-col"},
		[]Collection{col},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(res.SyntheticEntries) != 1 {
		t.Fatalf("expected 1 synthetic entry, got %d", len(res.SyntheticEntries))
	}
	s := res.SyntheticEntries[0]
	if s.URL != "git@example.com:remote.git" {
		t.Fatalf("expected URL remote.git, got %q", s.URL)
	}
	// Source should be promoted to RepoCommitted (floor from local repo).
	if s.Source != config.RepoCommitted {
		t.Fatalf("expected RepoCommitted (floor from local collection), got %v", s.Source)
	}
}

// ============================================================
// DETERMINISTIC ORDERING
// ============================================================

func TestExpandSyntheticEntriesAreSortedDeterministically(t *testing.T) {
	// Three registries: z.git, a.git, m.git — synthetic entries must be
	// sorted by URL, then ref, then path.
	col := Collection{
		Name: "multi",
		Skills: []CollectionSkill{
			{Name: "z-skill", Registry: "git@example.com:z.git"},
			{Name: "a-skill", Registry: "git@example.com:a.git"},
			{Name: "m-skill", Registry: "git@example.com:m.git"},
		},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Source: config.RepoCommitted},
		{URL: "git@example.com:z.git", Ref: "main", Source: config.UserPersonal},
		{URL: "git@example.com:a.git", Ref: "main", Source: config.UserPersonal},
		{URL: "git@example.com:m.git", Ref: "main", Source: config.UserPersonal},
	}

	res, errs := Expand(
		[]string{"multi"},
		[]Collection{col},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(res.SyntheticEntries) != 3 {
		t.Fatalf("expected 3 synthetic entries, got %d", len(res.SyntheticEntries))
	}

	// Verify sorted order: a < m < z
	urls := make([]string, len(res.SyntheticEntries))
	for i, s := range res.SyntheticEntries {
		urls[i] = s.URL
	}
	if !sort.StringsAreSorted(urls) {
		t.Fatalf("synthetic entries not sorted by URL: %v", urls)
	}
	if urls[0] != "git@example.com:a.git" || urls[1] != "git@example.com:m.git" || urls[2] != "git@example.com:z.git" {
		t.Fatalf("expected sorted [a m z], got %v", urls)
	}
}

func TestExpandSameURLDifferentPathsSortedByPath(t *testing.T) {
	// Same URL with different paths should be sorted by path within the URL.
	col := Collection{
		Name: "multi-path",
		Skills: []CollectionSkill{
			{Name: "z-skill", Path: "z-path"},
			{Name: "a-skill", Path: "a-path"},
		},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Path: "z-path", Source: config.UserPersonal, CatalogKey: "/cache/core:z"},
		{URL: "git@example.com:core.git", Ref: "main", Path: "a-path", Source: config.UserPersonal, CatalogKey: "/cache/core:a"},
	}

	res, errs := Expand(
		[]string{"multi-path"},
		[]Collection{col},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(res.SyntheticEntries) != 2 {
		t.Fatalf("expected 2 synthetic entries, got %d", len(res.SyntheticEntries))
	}

	paths := []string{res.SyntheticEntries[0].Path, res.SyntheticEntries[1].Path}
	if paths[0] != "a-path" || paths[1] != "z-path" {
		t.Fatalf("expected sorted paths [a-path z-path], got %v", paths)
	}
}

// ============================================================
// VIAMAP WITH NESTED COLLECTIONS
// ============================================================

func TestExpandViaMapWithNestedCollections(t *testing.T) {
	// Skills from parent and child should be annotated with their
	// respective collection labels in ViaMap.
	child := Collection{
		Name:              "child",
		Version:           "2.0",
		Skills:            []CollectionSkill{{Name: "child-skill"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	parent := Collection{
		Name:              "parent",
		Version:           "1.0",
		Skills:            []CollectionSkill{{Name: "parent-skill"}},
		Includes:          []CollectionInclude{{Name: "child"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Source: config.RepoCommitted},
	}

	res, errs := Expand(
		[]string{"parent"},
		[]Collection{parent, child},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if res.ViaMap["skill:parent-skill"] != "collection:parent@1.0" {
		t.Fatalf("ViaMap[skill:parent-skill] = %q, want collection:parent@1.0", res.ViaMap["skill:parent-skill"])
	}
	if res.ViaMap["skill:child-skill"] != "collection:parent@1.0" {
		// When parent includes child, all skills are attributed to the
		// top-level collection ref ("parent@1.0") because the expansion
		// was triggered by a CollectionRef to "parent".
		t.Fatalf("ViaMap[skill:child-skill] = %q, want collection:parent@1.0", res.ViaMap["skill:child-skill"])
	}
}

func TestExpandViaMapOnlyAnnotatesCollectionOnlySkills(t *testing.T) {
	// A skill that is directly configured should NOT appear in ViaMap,
	// even if it's also contributed by a collection.
	col := Collection{
		Name:              "overlap",
		Skills:            []CollectionSkill{{Name: "shared"}, {Name: "exclusive"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Source: config.RepoCommitted,
			Skills: config.KindSelector{Include: []string{"shared"}}},
	}

	res, errs := Expand(
		[]string{"overlap"},
		[]Collection{col},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if _, ok := res.ViaMap["skill:shared"]; ok {
		t.Fatalf("expected 'shared' NOT in ViaMap (directly configured)")
	}
	if res.ViaMap["skill:exclusive"] != "collection:overlap" {
		t.Fatalf("ViaMap[skill:exclusive] = %q, want collection:overlap", res.ViaMap["skill:exclusive"])
	}
}

// ============================================================
// EDGE CASES: SOURCE PATH WITH PATH-AWARE FLOOR LOGIC
// ============================================================

func TestSourceForRegistryPathAwareExactMatch(t *testing.T) {
	// When url+ref+path all match an existing entry, that entry's source
	// should be returned (path-aware lookup).
	entries := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Path: ".factory/skills", Source: config.RepoPersonal},
		{URL: "git@example.com:core.git", Ref: "main", Path: "skills", Source: config.UserPersonal},
	}
	got := sourceForRegistry("git@example.com:core.git", "main", "skills", entries, "", "", "")
	if got != config.UserPersonal {
		t.Fatalf("expected UserPersonal (path=skills), got %v", got)
	}
}

func TestSourceForRegistryFloorWithPathAwareFallback(t *testing.T) {
	// Floor logic must match on (url, ref, path) for the collection's
	// source registry. When the collection source has a path, only
	// entries with that exact path should be considered for the floor.
	entries := []config.RegistryEntry{
		{URL: "git@example.com:other.git", Ref: "main", Source: config.UserPersonal},
		{URL: "git@example.com:core.git", Ref: "main", Path: ".factory/skills", Source: config.RepoCommitted},
		{URL: "git@example.com:core.git", Ref: "main", Path: "skills", Source: config.UserPersonal},
	}
	// Target is other.git (UserPersonal). The collection source has
	// path=".factory/skills". Floor entry is RepoCommitted (from .factory/skills).
	got := sourceForRegistry("git@example.com:other.git", "main", "", entries,
		"git@example.com:core.git", "main", ".factory/skills")
	if got != config.RepoCommitted {
		t.Fatalf("expected RepoCommitted (path-aware floor), got %v", got)
	}
}

func TestSourceForRegistryFloorIgnoresEntriesWithDifferentPath(t *testing.T) {
	// When the fallbackPath is ".factory/skills", entries with path "skills"
	// should NOT be considered as floor candidates.
	entries := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Path: "skills", Source: config.Organization},
	}
	got := sourceForRegistry("git@example.com:other.git", "main", "", entries,
		"git@example.com:core.git", "main", ".factory/skills")
	// No matching path for fallback → floor doesn't apply.
	if got != config.SourceUnset {
		t.Fatalf("expected SourceUnset (no matching floor path), got %v", got)
	}
}

func TestSourceForRegistryFallbackPathEmptyMatchesOnUrlRefOnly(t *testing.T) {
	// When fallbackPath is empty, the floor logic should fall back to
	// matching on (url, ref) only, matching entries with any path.
	entries := []config.RegistryEntry{
		{URL: "git@example.com:other.git", Ref: "main", Source: config.UserPersonal},
		{URL: "git@example.com:core.git", Ref: "main", Path: "skills", Source: config.RepoCommitted},
	}
	got := sourceForRegistry("git@example.com:other.git", "main", "", entries,
		"git@example.com:core.git", "main", "")
	// fallbackPath="" → falls back to url+ref match → finds RepoCommitted
	if got != config.RepoCommitted {
		t.Fatalf("expected RepoCommitted (url+ref floor fallback), got %v", got)
	}
}

// ============================================================
// CATALOG KEY HELPERS
// ============================================================

func TestCatalogKeyForRegistryWithNoMatchAndNoFallback(t *testing.T) {
	// No matching entry and no fallback → empty string.
	got := catalogKeyForRegistry("git@example.com:ghost.git", "main", "", nil, "")
	if got != "" {
		t.Fatalf("expected empty catalog key, got %q", got)
	}
}

// ============================================================
// HARNESS AND SOURCE_HARNESS HELPERS
// ============================================================

func TestHarnessesForRegistryReturnsNilWhenNoMatch(t *testing.T) {
	got := harnessesForRegistry("git@example.com:ghost.git", "main", "", nil)
	if got != nil {
		t.Fatalf("expected nil, got %#v", got)
	}
}

func TestHarnessesForRegistryMatchesOnPath(t *testing.T) {
	entries := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Path: ".factory/skills", Harnesses: []string{"factory"}},
		{URL: "git@example.com:core.git", Ref: "main", Path: "skills", Harnesses: []string{"claude"}},
	}
	got := harnessesForRegistry("git@example.com:core.git", "main", "skills", entries)
	if !reflect.DeepEqual(got, []string{"claude"}) {
		t.Fatalf("expected [claude], got %#v", got)
	}
}

func TestHarnessesForRegistryFallsBackWhenPathEmpty(t *testing.T) {
	entries := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Path: "skills", Harnesses: []string{"claude"}},
	}
	got := harnessesForRegistry("git@example.com:core.git", "main", "", entries)
	if !reflect.DeepEqual(got, []string{"claude"}) {
		t.Fatalf("expected [claude] (url+ref fallback), got %#v", got)
	}
}

func TestSourceHarnessForRegistryMatchesOnPath(t *testing.T) {
	entries := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Path: ".factory/skills", SourceHarness: "factory-plugin"},
		{URL: "git@example.com:core.git", Ref: "main", Path: "skills", SourceHarness: "claude-plugin"},
	}
	got := sourceHarnessForRegistry("git@example.com:core.git", "main", "skills", entries)
	if got != "claude-plugin" {
		t.Fatalf("expected claude-plugin, got %q", got)
	}
}

func TestSourceHarnessForRegistryFallsBackWhenPathEmpty(t *testing.T) {
	entries := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Path: "skills", SourceHarness: "claude-plugin"},
	}
	got := sourceHarnessForRegistry("git@example.com:core.git", "main", "", entries)
	if got != "claude-plugin" {
		t.Fatalf("expected claude-plugin (url+ref fallback), got %q", got)
	}
}

// ============================================================
// COMPLEX INTEGRATION SCENARIO
// ============================================================

func TestExpandComplexMultiLayerScenario(t *testing.T) {
	// Real-world scenario: repo config has a local dot-registry at
	// .factory/skills with a "dev-tools" collection. The collection
	// references skills from:
	// 1. Same registry, same path (inherited) → repo-committed
	// 2. Same registry, different path (explicit "skills") → repo-committed
	// 3. External registry (awesome-ai.git) → promoted to repo-committed (floor)
	// 4. Local filesystem registry → repo-committed
	//
	// Expected: 4 synthetic entries with correct paths, sources, catalog keys.
	col := Collection{
		Name:    "dev-tools",
		Version: "1.0",
		Skills: []CollectionSkill{
			{Name: "openspec-apply-change"},                                    // inherits source
			{Name: "openspec-explore", Path: "skills"},                         // explicit path
			{Name: "efficient-go", Registry: "git@example.com:awesome-ai.git"}, // external
			{Name: "custom-hook", Registry: "/local/custom-hooks"},             // local
		},
		SourceRegistryURL: ".",
		SourceRef:         "main",
		SourcePath:        ".factory/skills",
		SourceCatalogKey:  "/workspace/repo",
	}
	existing := []config.RegistryEntry{
		// Dot registry at .factory/skills
		{URL: ".", Ref: "main", Path: ".factory/skills", Source: config.RepoCommitted, CatalogKey: "/workspace/repo"},
		// Dot registry at skills
		{URL: ".", Ref: "main", Path: "skills", Source: config.RepoCommitted, CatalogKey: "/workspace/repo:skills"},
		// External registry
		{URL: "git@example.com:awesome-ai.git", Ref: "main", Source: config.UserPersonal, CatalogKey: "/cache/awesome-ai"},
		// Local filesystem registry
		{URL: "/local/custom-hooks", Ref: "", Source: config.RepoCommitted, CatalogKey: "/local/custom-hooks"},
	}

	res, errs := Expand(
		[]string{"dev-tools"},
		[]Collection{col},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(res.SyntheticEntries) != 4 {
		t.Fatalf("expected 4 synthetic entries, got %d: %#v", len(res.SyntheticEntries), res.SyntheticEntries)
	}

	// Entry 1: dot registry at .factory/skills
	factoryEntry := lookupSynth(t, res.SyntheticEntries, ".", ".factory/skills")
	if factoryEntry.Source != config.RepoCommitted {
		t.Fatalf("factory entry source = %v, want RepoCommitted", factoryEntry.Source)
	}
	if factoryEntry.CatalogKey != "/workspace/repo" {
		t.Fatalf("factory CatalogKey = %q, want /workspace/repo", factoryEntry.CatalogKey)
	}
	if !reflect.DeepEqual(factoryEntry.Skills.Include, []string{"openspec-apply-change"}) {
		t.Fatalf("factory skills = %#v", factoryEntry.Skills.Include)
	}

	// Entry 2: dot registry at skills
	skillsEntry := lookupSynth(t, res.SyntheticEntries, ".", "skills")
	if skillsEntry.Source != config.RepoCommitted {
		t.Fatalf("skills entry source = %v, want RepoCommitted", skillsEntry.Source)
	}
	if skillsEntry.CatalogKey != "/workspace/repo:skills" {
		t.Fatalf("skills CatalogKey = %q, want /workspace/repo:skills", skillsEntry.CatalogKey)
	}
	if !reflect.DeepEqual(skillsEntry.Skills.Include, []string{"openspec-explore"}) {
		t.Fatalf("skills entry = %#v", skillsEntry.Skills.Include)
	}

	// Entry 3: external registry
	awesomeEntry := lookupSynth(t, res.SyntheticEntries, "git@example.com:awesome-ai.git")
	if awesomeEntry.Source != config.RepoCommitted {
		t.Fatalf("awesome-ai entry source = %v, want RepoCommitted (promoted by floor)", awesomeEntry.Source)
	}
	if awesomeEntry.CatalogKey != "/cache/awesome-ai" {
		t.Fatalf("awesome-ai CatalogKey = %q, want /cache/awesome-ai", awesomeEntry.CatalogKey)
	}
	if !reflect.DeepEqual(awesomeEntry.Skills.Include, []string{"efficient-go"}) {
		t.Fatalf("awesome-ai skills = %#v", awesomeEntry.Skills.Include)
	}

	// Entry 4: local registry is merged into a fourth entry
	localEntry := lookupSynth(t, res.SyntheticEntries, "/local/custom-hooks")
	if localEntry.Source != config.RepoCommitted {
		t.Fatalf("local entry source = %v, want RepoCommitted", localEntry.Source)
	}
	if !reflect.DeepEqual(localEntry.Skills.Include, []string{"custom-hook"}) {
		t.Fatalf("local skills = %#v", localEntry.Skills.Include)
	}

	// Verify ViaMap
	if res.ViaMap["skill:openspec-apply-change"] != "collection:dev-tools@1.0" {
		t.Fatalf("ViaMap[skill:openspec-apply-change] = %q", res.ViaMap["skill:openspec-apply-change"])
	}
	if res.ViaMap["skill:efficient-go"] != "collection:dev-tools@1.0" {
		t.Fatalf("ViaMap[skill:efficient-go] = %q", res.ViaMap["skill:efficient-go"])
	}
}

// ============================================================
// COLLECTION DISCOVERY WITH PATH
// ============================================================

func TestDiscoverCollectionsWithPathProvenance(t *testing.T) {
	// Verify that DiscoverCollections stamps SourcePath on each
	// discovered collection, and that this propagates correctly.
	skillDir := t.TempDir()
	writeCollection(t, skillDir, "with-path", `name: with-path
skills:
  - name: skill-a
  - name: skill-b
    path: other/skills
`)

	got, err := DiscoverCollections(skillDir, "cache-key", "git@example.com:source.git", "v1", ".factory/skills")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 collection, got %d", len(got))
	}
	c := got[0]
	if c.SourcePath != ".factory/skills" {
		t.Fatalf("expected SourcePath .factory/skills, got %q", c.SourcePath)
	}
	if c.SourceRegistryURL != "git@example.com:source.git" {
		t.Fatalf("expected SourceRegistryURL, got %q", c.SourceRegistryURL)
	}
	if c.SourceRef != "v1" {
		t.Fatalf("expected SourceRef v1, got %q", c.SourceRef)
	}
	if c.SourceCatalogKey != "cache-key" {
		t.Fatalf("expected SourceCatalogKey cache-key, got %q", c.SourceCatalogKey)
	}
	// Collection skill's own path should still be parsed from YAML.
	if c.Skills[1].Path != "other/skills" {
		t.Fatalf("expected skill-b Path 'other/skills', got %q", c.Skills[1].Path)
	}
}

func TestDiscoverCollectionsMultipleCollectionsWithDifferentSourcePaths(t *testing.T) {
	// Two separate DiscoverCollections calls with different paths should
	// produce collections with different SourcePaths.
	skillDir := t.TempDir()
	writeCollection(t, skillDir, "tools", "name: tools\nskills:\n  - name: s1\n")

	got1, err1 := DiscoverCollections(skillDir, "key1", "git@example.com:core.git", "main", ".factory/skills")
	if err1 != nil {
		t.Fatalf("first DiscoverCollections: %v", err1)
	}
	if len(got1) == 0 {
		t.Fatalf("first DiscoverCollections returned no collections")
	}
	got2, err2 := DiscoverCollections(skillDir, "key2", "git@example.com:core.git", "main", "skills")
	if err2 != nil {
		t.Fatalf("second DiscoverCollections: %v", err2)
	}
	if len(got2) == 0 {
		t.Fatalf("second DiscoverCollections returned no collections")
	}

	if got1[0].SourcePath != ".factory/skills" {
		t.Fatalf("first call SourcePath = %q, want .factory/skills", got1[0].SourcePath)
	}
	if got2[0].SourcePath != "skills" {
		t.Fatalf("second call SourcePath = %q, want skills", got2[0].SourcePath)
	}
}

// ============================================================
// ERROR ACCUMULATION
// ============================================================

func TestExpandAccumulatesMultipleErrors(t *testing.T) {
	// Two missing collections + one missing include should produce
	// three errors total.
	col := Collection{
		Name:              "parent",
		Skills:            []CollectionSkill{{Name: "s"}},
		Includes:          []CollectionInclude{{Name: "missing-child"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main"},
	}

	_, errs := Expand(
		[]string{"missing1", "missing2", "parent"},
		[]Collection{col},
		existing,
	)
	if len(errs) != 3 {
		t.Fatalf("expected 3 errors, got %d: %v", len(errs), errs)
	}
}

func TestExpandSkillReferencingUnconfiguredRegistryWithExplicitRefErrors(t *testing.T) {
	// A collection skill with an explicit registry+ref where that
	// registry is not in any config layer should produce an error.
	col := Collection{
		Name: "bad",
		Skills: []CollectionSkill{
			{Name: "s1", Registry: "git@example.com:unconfigured.git", Ref: "v3"},
		},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main"},
	}

	_, errs := Expand(
		[]string{"bad"},
		[]Collection{col},
		existing,
	)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %v", errs)
	}
	if !strings.Contains(errs[0].Error(), "not present in any config layer") {
		t.Fatalf("expected 'not present in any config layer', got %v", errs[0])
	}
}

// ============================================================
// URL NORMALIZATION WITH PATH
// ============================================================

func TestExpandNormalizesGitURLsWithPathMatching(t *testing.T) {
	// A collection skill references registry without .git suffix,
	// and the existing entry has .git suffix with a path. URL
	// normalization should still match them correctly.
	col := Collection{
		Name:              "tools",
		Skills:            []CollectionSkill{{Name: "review", Path: "skills"}},
		SourceRegistryURL: "git@example.com:core",
		SourceRef:         "main",
		SourcePath:        ".factory/skills",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Path: "skills", Source: config.RepoCommitted, CatalogKey: "/cache/core:skills"},
		{URL: "git@example.com:core.git", Ref: "main", Path: ".factory/skills", Source: config.UserPersonal, CatalogKey: "/cache/core:factory"},
	}

	res, errs := Expand(
		[]string{"tools"},
		[]Collection{col},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("expected no errors due to URL normalization, got %v", errs)
	}
	s := lookupSynth(t, res.SyntheticEntries, "git@example.com:core", "skills")
	if s.CatalogKey != "/cache/core:skills" {
		t.Fatalf("expected path-aware CatalogKey, got %q", s.CatalogKey)
	}
}
