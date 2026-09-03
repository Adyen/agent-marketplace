package collection

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// lookupSynth returns the synthetic entry whose URL (and optionally path) matches.
func lookupSynth(t *testing.T, entries []config.RegistryEntry, url string, path ...string) config.RegistryEntry {
	t.Helper()
	wantPath := ""
	if len(path) > 0 {
		wantPath = path[0]
	}
	for _, e := range entries {
		if e.URL == url && e.Path == wantPath {
			return e
		}
	}
	t.Fatalf("no synthetic entry for %q path=%q (entries=%#v)", url, wantPath, entries)
	return config.RegistryEntry{}
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func TestExpandNoRefsReturnsEmptyResult(t *testing.T) {
	got, errs := Expand(nil, nil, nil)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if got == nil {
		t.Fatalf("expected non-nil result")
	}
	if len(got.SyntheticEntries) != 0 {
		t.Fatalf("expected no synthetic entries, got %#v", got.SyntheticEntries)
	}
	if got.ViaMap == nil {
		t.Fatalf("expected ViaMap initialized, got nil")
	}
}

func TestExpandMissingCollectionProducesError(t *testing.T) {
	refs := []string{"does-not-exist"}
	got, errs := Expand(refs, nil, nil)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %v", errs)
	}
	if !strings.Contains(errs[0].Error(), "not found") {
		t.Fatalf("expected 'not found' error, got %v", errs[0])
	}
	if len(got.SyntheticEntries) != 0 {
		t.Fatalf("expected no synthetic entries, got %#v", got.SyntheticEntries)
	}
}

func TestExpandSingleCollectionUsesSourceRegistry(t *testing.T) {
	col := Collection{
		Name:              "core",
		Version:           "1.0",
		Skills:            []CollectionSkill{{Name: "skill-a"}, {Name: "skill-b"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
		SourceCatalogKey:  "/cache/core",
	}
	existing := []config.RegistryEntry{{
		URL:        "git@example.com:core.git",
		Ref:        "main",
		Source:     config.UserPersonal,
		CatalogKey: "/cache/core",
		Skills:     config.KindSelector{Exclude: []string{"legacy-skill"}},
	}}

	res, errs := Expand(
		[]string{"core"},
		[]Collection{col},
		existing,
	)

	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(res.SyntheticEntries) != 1 {
		t.Fatalf("expected 1 synthetic entry, got %#v", res.SyntheticEntries)
	}
	s := res.SyntheticEntries[0]
	if s.URL != "git@example.com:core.git" || s.Ref != "main" {
		t.Fatalf("wrong registry on synthetic entry: %#v", s)
	}
	if !reflect.DeepEqual(sortedCopy(s.Skills.Include), []string{"skill-a", "skill-b"}) {
		t.Fatalf("expected skills [a b], got %#v", s.Skills.Include)
	}
	if s.Source != config.UserPersonal {
		t.Fatalf("expected source propagated from existing entry, got %v", s.Source)
	}
	if s.CatalogKey != "/cache/core" {
		t.Fatalf("expected CatalogKey propagated, got %q", s.CatalogKey)
	}
	if !reflect.DeepEqual(s.Skills.Exclude, []string{"legacy-skill"}) {
		t.Fatalf("expected Skills.Exclude propagated, got %#v", s.Skills.Exclude)
	}

	// Via map: both skills should be annotated since neither is in existing.Skills.
	want := map[string]string{
		"skill:skill-a": "collection:core@1.0",
		"skill:skill-b": "collection:core@1.0",
	}
	if !reflect.DeepEqual(res.ViaMap, want) {
		t.Fatalf("ViaMap = %#v, want %#v", res.ViaMap, want)
	}
}

func TestExpandDirectlyConfiguredSkillsAreNotAnnotated(t *testing.T) {
	col := Collection{
		Name:              "core",
		Skills:            []CollectionSkill{{Name: "skill-a"}, {Name: "skill-b"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{{
		URL:    "git@example.com:core.git",
		Ref:    "main",
		Skills: config.KindSelector{Include: []string{"skill-a"}}, // directly configured
		Source: config.RepoCommitted,
	}}

	res, errs := Expand(
		[]string{"core"},
		[]Collection{col},
		existing,
	)

	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	// skill-a should NOT be annotated (direct config wins); skill-b should be.
	if _, ok := res.ViaMap["skill:skill-a"]; ok {
		t.Fatalf("expected skill-a NOT in ViaMap (directly configured), got %#v", res.ViaMap)
	}
	if res.ViaMap["skill:skill-b"] != "collection:core" {
		t.Fatalf("expected skill-b annotated, got %q", res.ViaMap["skill:skill-b"])
	}
}

func TestExpandSkillReferencingOtherRegistryWithoutRefUsesConfiguredRef(t *testing.T) {
	col := Collection{
		Name: "mixed",
		Skills: []CollectionSkill{
			{Name: "native-skill"},
			{Name: "other-skill", Registry: "git@example.com:other.git"},
		},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Source: config.RepoCommitted, CatalogKey: "/cache/core"},
		{URL: "git@example.com:other.git", Ref: "stable", Source: config.UserPersonal, CatalogKey: "/cache/other"},
	}

	res, errs := Expand(
		[]string{"mixed"},
		[]Collection{col},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(res.SyntheticEntries) != 2 {
		t.Fatalf("expected 2 synthetic entries (one per registry), got %#v", res.SyntheticEntries)
	}

	coreEntry := lookupSynth(t, res.SyntheticEntries, "git@example.com:core.git")
	if !reflect.DeepEqual(coreEntry.Skills.Include, []string{"native-skill"}) {
		t.Fatalf("core entry skills = %#v", coreEntry.Skills.Include)
	}
	if coreEntry.Source != config.RepoCommitted {
		t.Fatalf("core entry source = %v, want RepoCommitted", coreEntry.Source)
	}

	otherEntry := lookupSynth(t, res.SyntheticEntries, "git@example.com:other.git")
	if otherEntry.Ref != "stable" {
		t.Fatalf("other entry should use configured ref 'stable', got %q", otherEntry.Ref)
	}
	if !reflect.DeepEqual(otherEntry.Skills.Include, []string{"other-skill"}) {
		t.Fatalf("other entry skills = %#v", otherEntry.Skills.Include)
	}
	// other.git is UserPersonal, but the collection comes from core.git which
	// is RepoCommitted. The collection's scope acts as a floor: synthetic
	// entries are promoted to RepoCommitted so they land in the repo's
	// harness directory rather than machine-scope dirs (which would cause an
	// install→prune cycle when the user changes context to ~/.).
	if otherEntry.Source != config.RepoCommitted {
		t.Fatalf("other entry source = %v, want RepoCommitted (promoted by collection floor)", otherEntry.Source)
	}
	if otherEntry.CatalogKey != "/cache/other" {
		t.Fatalf("other entry CatalogKey = %q, want /cache/other", otherEntry.CatalogKey)
	}
}

func TestExpandSkillReferencingUnknownRegistryErrors(t *testing.T) {
	col := Collection{
		Name: "bad",
		Skills: []CollectionSkill{
			{Name: "ghost-skill", Registry: "git@example.com:ghost.git"},
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
	if !strings.Contains(errs[0].Error(), "no configured ref") {
		t.Fatalf("expected 'no configured ref' error, got %v", errs[0])
	}
}

func TestExpandSkillWithExplicitRegistryAndRefRequiresConfigured(t *testing.T) {
	col := Collection{
		Name: "bad",
		Skills: []CollectionSkill{
			{Name: "ghost-skill", Registry: "git@example.com:ghost.git", Ref: "v1"},
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
		t.Fatalf("expected 'not present in any config layer' error, got %v", errs[0])
	}
}

func TestExpandNormalizesGitURLsForRegistryMatching(t *testing.T) {
	// Skill references registry without .git suffix; existing entry has it.
	// NormalizeGitURL should treat them as the same registry.
	col := Collection{
		Name: "core",
		Skills: []CollectionSkill{
			{Name: "skill-a", Registry: "git@example.com:core"},
		},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Source: config.RepoCommitted},
	}

	res, errs := Expand(
		[]string{"core"},
		[]Collection{col},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("expected no errors due to URL normalization, got %v", errs)
	}
	if len(res.SyntheticEntries) != 1 {
		t.Fatalf("expected 1 synthetic entry, got %#v", res.SyntheticEntries)
	}
}

func TestExpandFollowsIncludesRecursively(t *testing.T) {
	child := Collection{
		Name:              "child",
		Skills:            []CollectionSkill{{Name: "inner-skill"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	parent := Collection{
		Name:              "parent",
		Skills:            []CollectionSkill{{Name: "outer-skill"}},
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
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(res.SyntheticEntries) != 1 {
		t.Fatalf("expected 1 synthetic entry, got %#v", res.SyntheticEntries)
	}
	got := sortedCopy(res.SyntheticEntries[0].Skills.Include)
	if !reflect.DeepEqual(got, []string{"inner-skill", "outer-skill"}) {
		t.Fatalf("expected both skills merged, got %#v", got)
	}
}

func TestExpandMissingIncludeProducesError(t *testing.T) {
	parent := Collection{
		Name:              "parent",
		Skills:            []CollectionSkill{{Name: "outer-skill"}},
		Includes:          []CollectionInclude{{Name: "ghost-collection"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main"},
	}

	_, errs := Expand(
		[]string{"parent"},
		[]Collection{parent},
		existing,
	)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %v", errs)
	}
	if !strings.Contains(errs[0].Error(), "ghost-collection") {
		t.Fatalf("expected error mentioning ghost-collection, got %v", errs[0])
	}
}

func TestExpandDetectsCycles(t *testing.T) {
	a := Collection{
		Name:              "a",
		Includes:          []CollectionInclude{{Name: "b"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	b := Collection{
		Name:              "b",
		Includes:          []CollectionInclude{{Name: "a"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main"},
	}

	_, errs := Expand(
		[]string{"a"},
		[]Collection{a, b},
		existing,
	)
	if len(errs) == 0 {
		t.Fatalf("expected cycle error")
	}
	foundCycle := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "cycle") {
			foundCycle = true
			break
		}
	}
	if !foundCycle {
		t.Fatalf("expected a 'cycle' error in %v", errs)
	}
}

func TestExpandSharedDAGIncludeIsNotReportedAsCycle(t *testing.T) {
	shared := Collection{
		Name:              "shared",
		Skills:            []CollectionSkill{{Name: "shared-skill"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	left := Collection{
		Name:              "left",
		Includes:          []CollectionInclude{{Name: "shared"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	right := Collection{
		Name:              "right",
		Includes:          []CollectionInclude{{Name: "shared"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	root := Collection{
		Name:              "root",
		Includes:          []CollectionInclude{{Name: "left"}, {Name: "right"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{{URL: "git@example.com:core.git", Ref: "main"}}

	res, errs := Expand([]string{"root"}, []Collection{root, left, right, shared}, existing)
	if len(errs) != 0 {
		t.Fatalf("expected shared DAG to expand without errors, got %v", errs)
	}
	if len(res.SyntheticEntries) != 1 {
		t.Fatalf("expected one synthetic registry entry, got %#v", res.SyntheticEntries)
	}
	if got := res.SyntheticEntries[0].Skills.Include; !reflect.DeepEqual(got, []string{"shared-skill"}) {
		t.Fatalf("expected shared skill from both branches to deduplicate, got %#v", got)
	}
}

func TestExpandMultipleCollectionsMergeOntoSingleSynthEntry(t *testing.T) {
	a := Collection{
		Name:              "a",
		Skills:            []CollectionSkill{{Name: "skill-a"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	b := Collection{
		Name:              "b",
		Skills:            []CollectionSkill{{Name: "skill-b"}, {Name: "skill-a"}}, // duplicate skill-a
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Source: config.RepoCommitted},
	}

	res, errs := Expand(
		[]string{"a", "b"},
		[]Collection{a, b},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(res.SyntheticEntries) != 1 {
		t.Fatalf("expected dedup to single entry, got %#v", res.SyntheticEntries)
	}
	got := sortedCopy(res.SyntheticEntries[0].Skills.Include)
	if !reflect.DeepEqual(got, []string{"skill-a", "skill-b"}) {
		t.Fatalf("expected deduplicated [a b], got %#v", got)
	}
}

func TestExpandEmptySkillNamesSkipped(t *testing.T) {
	col := Collection{
		Name: "core",
		Skills: []CollectionSkill{
			{Name: "real"},
			{Name: "   "}, // whitespace only → skipped
			{Name: ""},
		},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main"},
	}

	res, errs := Expand(
		[]string{"core"},
		[]Collection{col},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(res.SyntheticEntries) != 1 {
		t.Fatalf("expected 1 synthetic entry, got %#v", res.SyntheticEntries)
	}
	if !reflect.DeepEqual(res.SyntheticEntries[0].Skills.Include, []string{"real"}) {
		t.Fatalf("expected only 'real' skill, got %#v", res.SyntheticEntries[0].Skills.Include)
	}
}

func TestAppendUniqueDeduplicatesPreservingOrder(t *testing.T) {
	got := appendUnique([]string{"a", "b"}, []string{"b", "c", "a", "d"})
	want := []string{"a", "b", "c", "d"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("appendUnique = %#v, want %#v", got, want)
	}
}

func TestAppendUniqueReturnsNewSliceWhenExistingIsNil(t *testing.T) {
	got := appendUnique(nil, []string{"a", "a", "b"})
	want := []string{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("appendUnique = %#v, want %#v", got, want)
	}
}

func TestExcludeForRegistryNormalizesURL(t *testing.T) {
	entries := []config.RegistryEntry{{
		URL:    "git@example.com:core.git",
		Skills: config.KindSelector{Exclude: []string{"legacy"}},
	}}
	got := excludeForRegistry("git@example.com:core", entries)
	if !reflect.DeepEqual(got, []string{"legacy"}) {
		t.Fatalf("excludeForRegistry = %#v", got)
	}
}

func TestExcludeForRegistryReturnsNilWhenNotFound(t *testing.T) {
	if got := excludeForRegistry("git@example.com:ghost", nil); got != nil {
		t.Fatalf("expected nil, got %#v", got)
	}
}

func TestFindConfiguredRefReturnsEmptyWhenMissing(t *testing.T) {
	if got := findConfiguredRef("git@example.com:ghost", nil); got != "" {
		t.Fatalf("expected empty ref, got %q", got)
	}
}

func TestFindConfiguredRefMatchesNormalizedURL(t *testing.T) {
	entries := []config.RegistryEntry{{URL: "git@example.com:core.git", Ref: "v2"}}
	if got := findConfiguredRef("git@example.com:core", entries); got != "v2" {
		t.Fatalf("expected v2, got %q", got)
	}
}

func TestSourceForRegistryUsesExistingEntryWhenMatched(t *testing.T) {
	entries := []config.RegistryEntry{{
		URL:    "git@example.com:core.git",
		Ref:    "main",
		Source: config.Organization,
	}}
	got := sourceForRegistry("git@example.com:core.git", "main", "", entries, "", "", "")
	if got != config.Organization {
		t.Fatalf("sourceForRegistry = %v, want Organization", got)
	}
}

func TestSourceForRegistryFallsBackToFallbackSourceRegistry(t *testing.T) {
	// rk does not match any existing entry directly, but fallback URL+ref match
	// an entry by URL alone — source should be taken from that entry.
	entries := []config.RegistryEntry{{
		URL:    "git@example.com:fallback.git",
		Ref:    "main",
		Source: config.UserPersonal,
	}}
	got := sourceForRegistry(
		"git@example.com:fallback.git", "main", "",
		entries,
		"git@example.com:fallback.git", "main", "",
	)
	if got != config.UserPersonal {
		t.Fatalf("sourceForRegistry = %v, want UserPersonal", got)
	}
}

func TestCatalogKeyForRegistryReturnsConfiguredKey(t *testing.T) {
	entries := []config.RegistryEntry{{
		URL:        "git@example.com:core.git",
		Ref:        "main",
		CatalogKey: "/cache/core",
	}}
	got := catalogKeyForRegistry("git@example.com:core.git", "main", "", entries, "/fallback")
	if got != "/cache/core" {
		t.Fatalf("catalogKeyForRegistry = %q, want /cache/core", got)
	}
}

func TestCatalogKeyForRegistryFallsBackWhenKeyEmpty(t *testing.T) {
	entries := []config.RegistryEntry{{
		URL: "git@example.com:core.git",
		Ref: "main",
		// CatalogKey empty
	}}
	got := catalogKeyForRegistry("git@example.com:core.git", "main", "", entries, "/fallback")
	if got != "/fallback" {
		t.Fatalf("catalogKeyForRegistry = %q, want /fallback", got)
	}
}

func TestCatalogKeyForRegistryMatchesOnUrlRefAndPath(t *testing.T) {
	// Two entries for the same URL+ref but different paths — must match
	// the one with the correct path, not the first one found.
	entries := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Path: ".factory/skills", CatalogKey: "/cache/core:factory"},
		{URL: "git@example.com:core.git", Ref: "main", Path: "skills", CatalogKey: "/cache/core:claude"},
	}
	got := catalogKeyForRegistry("git@example.com:core.git", "main", "skills", entries, "/fallback")
	if got != "/cache/core:claude" {
		t.Fatalf("catalogKeyForRegistry = %q, want /cache/core:claude", got)
	}
}

func TestCatalogKeyForRegistryFallsBackToUrlRefWhenPathEmpty(t *testing.T) {
	// When path is empty and no exact match, fall back to url+ref match.
	entries := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Path: ".factory/skills", CatalogKey: "/cache/core:factory"},
	}
	got := catalogKeyForRegistry("git@example.com:core.git", "main", "", entries, "/fallback")
	if got != "/cache/core:factory" {
		t.Fatalf("catalogKeyForRegistry = %q, want /cache/core:factory (fallback)", got)
	}
}

func TestSourceForRegistryUsesHighestPrecedenceWhenMultipleLayersConfigureSameRegistry(t *testing.T) {
	// Global config adds awesome-ai as UserPersonal; repo config adds it as
	// RepoCommitted. A collection synthetic entry for awesome-ai should get
	// RepoCommitted (higher precedence), not UserPersonal (first match).
	entries := []config.RegistryEntry{
		{URL: "git@example.com:awesome-ai.git", Ref: "main", Source: config.UserPersonal},
		{URL: "git@example.com:awesome-ai.git", Ref: "main", Source: config.RepoCommitted},
	}
	got := sourceForRegistry("git@example.com:awesome-ai.git", "main", "", entries, ".", "main", "")
	if got != config.RepoCommitted {
		t.Fatalf("expected RepoCommitted (highest precedence), got %v", got)
	}
}

func TestSourceForRegistryFloorDoesNotPromoteForDifferentRef(t *testing.T) {
	// The collection source registry exists at ref "v2" (RepoCommitted), but
	// fallbackRef is "main". An entry for the same URL at a different ref must
	// not act as a floor — only an exact URL+ref match should count.
	entries := []config.RegistryEntry{
		{URL: "git@example.com:awesome-ai.git", Ref: "main", Source: config.UserPersonal},
		{URL: ".", Ref: "v2", Source: config.RepoCommitted}, // different ref from fallbackRef "main"
	}
	got := sourceForRegistry("git@example.com:awesome-ai.git", "main", "", entries, ".", "main", "")
	// Floor entry ref "v2" != fallbackRef "main" → floor must NOT apply.
	// Highest precedence from the target entries alone is UserPersonal.
	if got != config.UserPersonal {
		t.Fatalf("expected UserPersonal (floor ref mismatch), got %v", got)
	}
}

func TestSourceForRegistryUsesCollectionSourceAsFloorWhenTargetRegistryIsLowerPrecedence(t *testing.T) {
	// awesome-ai is only UserPersonal, but the collection was discovered from
	// a RepoCommitted local registry ("."). The synthetic entry's source must
	// be at least RepoCommitted so the skill goes to the repo's harness dir
	// rather than the machine-scope dir (which would cause an install→prune
	// cycle every time the user changes context).
	entries := []config.RegistryEntry{
		{URL: "git@example.com:awesome-ai.git", Ref: "main", Source: config.UserPersonal},
		{URL: ".", Ref: "main", Source: config.RepoCommitted},
	}
	got := sourceForRegistry("git@example.com:awesome-ai.git", "main", "", entries, ".", "main", "")
	if got != config.RepoCommitted {
		t.Fatalf("expected RepoCommitted (collection source floor), got %v", got)
	}
}

func TestExpandCollectionFromRepoLayerProducesRepoScopedSyntheticEntries(t *testing.T) {
	// The agent-marketplace-tools scenario: the collection is in the repo
	// config (RepoCommitted url:.), and its skills reference an external
	// registry that is ALSO in the global config (UserPersonal). The
	// synthetic entries must be RepoCommitted so the skills land in the
	// repo's harness directory and are not pruned when am sync runs from ~/
	// without the repo config.
	col := Collection{
		Name:              "tools",
		Version:           "1.0",
		Skills:            []CollectionSkill{{Name: "efficient-go", Registry: "git@example.com:awesome-ai.git"}},
		SourceRegistryURL: ".",
		SourceRef:         "main",
		SourceCatalogKey:  "/workspace/repo",
	}
	existing := []config.RegistryEntry{
		// UserPersonal: global config has awesome-ai with explicit skills.
		{URL: "git@example.com:awesome-ai.git", Ref: "main", Source: config.UserPersonal, CatalogKey: "/cache/awesome-ai"},
		// RepoCommitted: repo config also has the local dot registry.
		{URL: ".", Ref: "main", Source: config.RepoCommitted, CatalogKey: "/workspace/repo"},
		// RepoCommitted: repo config adds awesome-ai too.
		{URL: "git@example.com:awesome-ai.git", Ref: "main", Source: config.RepoCommitted, CatalogKey: "/cache/awesome-ai"},
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
		t.Fatalf("expected 1 synthetic entry, got %d: %#v", len(res.SyntheticEntries), res.SyntheticEntries)
	}
	s := res.SyntheticEntries[0]
	if s.Source != config.RepoCommitted {
		t.Fatalf("expected RepoCommitted (repo-scoped collection), got %v — skills would be pruned on context change", s.Source)
	}
}

func TestExpandCollectionFromUserPersonalLayerOnlyProducesMachineScopedEntries(t *testing.T) {
	// When a collection is in the user's global config only (UserPersonal),
	// and the target registry is also only UserPersonal, the synthetic entry
	// should remain UserPersonal (machine-scoped).
	col := Collection{
		Name:              "tools",
		Skills:            []CollectionSkill{{Name: "efficient-go", Registry: "git@example.com:awesome-ai.git"}},
		SourceRegistryURL: "git@example.com:myrepo.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:awesome-ai.git", Ref: "main", Source: config.UserPersonal, CatalogKey: "/cache/awesome-ai"},
		{URL: "git@example.com:myrepo.git", Ref: "main", Source: config.UserPersonal, CatalogKey: "/cache/myrepo"},
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
		t.Fatalf("expected 1 synthetic entry, got %d: %#v", len(res.SyntheticEntries), res.SyntheticEntries)
	}
	s := res.SyntheticEntries[0]
	if s.Source != config.UserPersonal {
		t.Fatalf("expected UserPersonal (global-only collection), got %v", s.Source)
	}
}

func TestExpandCollectionSkillWithExplicitPathProducesSeparateSyntheticEntry(t *testing.T) {
	// When a collection references a skill from a different
	// subdirectory of the same registry, the path on the collection skill
	// must produce a distinct synthetic entry with the correct CatalogKey
	// and Path. Without path in registryKey, both paths collapsed into one
	// synthetic entry pointing to the wrong catalog.
	col := Collection{
		Name:              "tools",
		Skills:            []CollectionSkill{{Name: "efficient-go", Path: "skills"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
		SourcePath:        ".factory/skills",
	}
	existing := []config.RegistryEntry{
		// Entry for the .factory/skills subdirectory (same URL+ref, different path).
		{URL: "git@example.com:core.git", Ref: "main", Path: ".factory/skills", Source: config.RepoCommitted, CatalogKey: "/cache/core:factory"},
		// Entry for the skills/ subdirectory.
		{URL: "git@example.com:core.git", Ref: "main", Path: "skills", Source: config.RepoCommitted, CatalogKey: "/cache/core:claude"},
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
		t.Fatalf("expected 1 synthetic entry, got %d: %#v", len(res.SyntheticEntries), res.SyntheticEntries)
	}
	s := res.SyntheticEntries[0]
	// The synthetic entry should target the skills/ path, not .factory/skills.
	if s.Path != "skills" {
		t.Fatalf("expected Path 'skills' on synthetic entry, got %q", s.Path)
	}
	if s.CatalogKey != "/cache/core:claude" {
		t.Fatalf("expected CatalogKey for skills/ subdirectory, got %q", s.CatalogKey)
	}
}

func TestExpandCollectionSkillInheritsSourcePathWhenNoExplicitPath(t *testing.T) {
	// When a collection skill does not specify a path, it should inherit
	// the SourcePath from the collection (the subdirectory the collection
	// was discovered from).
	col := Collection{
		Name:              "tools",
		Skills:            []CollectionSkill{{Name: "openspec-apply-change"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
		SourcePath:        ".factory/skills",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Path: ".factory/skills", Source: config.RepoCommitted, CatalogKey: "/cache/core:factory"},
		{URL: "git@example.com:core.git", Ref: "main", Path: "skills", Source: config.UserPersonal, CatalogKey: "/cache/core:claude"},
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
	// Should inherit SourcePath (.factory/skills), not fall back to the first entry.
	if s.Path != ".factory/skills" {
		t.Fatalf("expected inherited Path '.factory/skills', got %q", s.Path)
	}
	if s.CatalogKey != "/cache/core:factory" {
		t.Fatalf("expected CatalogKey for .factory/skills, got %q", s.CatalogKey)
	}
}

func TestExpandSameRegistryDifferentPathsProducesDistinctSyntheticEntries(t *testing.T) {
	// A collection that references skills from two different subdirectories
	// of the same registry should produce two separate synthetic entries,
	// each with its own Path and CatalogKey.
	col := Collection{
		Name:              "mixed",
		Skills:            []CollectionSkill{{Name: "factory-skill"}, {Name: "claude-skill", Path: "skills"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
		SourcePath:        ".factory/skills",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Path: ".factory/skills", Source: config.RepoCommitted, CatalogKey: "/cache/core:factory"},
		{URL: "git@example.com:core.git", Ref: "main", Path: "skills", Source: config.UserPersonal, CatalogKey: "/cache/core:claude"},
	}

	res, errs := Expand(
		[]string{"mixed"},
		[]Collection{col},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(res.SyntheticEntries) != 2 {
		t.Fatalf("expected 2 synthetic entries (one per path), got %d: %#v", len(res.SyntheticEntries), res.SyntheticEntries)
	}

	factoryEntry := lookupSynth(t, res.SyntheticEntries, "git@example.com:core.git", ".factory/skills")
	if factoryEntry.CatalogKey != "/cache/core:factory" {
		t.Fatalf("factory entry CatalogKey = %q, want /cache/core:factory", factoryEntry.CatalogKey)
	}
	if !reflect.DeepEqual(factoryEntry.Skills.Include, []string{"factory-skill"}) {
		t.Fatalf("factory entry skills = %#v", factoryEntry.Skills.Include)
	}

	claudeEntry := lookupSynth(t, res.SyntheticEntries, "git@example.com:core.git", "skills")
	if claudeEntry.CatalogKey != "/cache/core:claude" {
		t.Fatalf("claude entry CatalogKey = %q, want /cache/core:claude", claudeEntry.CatalogKey)
	}
	if !reflect.DeepEqual(claudeEntry.Skills.Include, []string{"claude-skill"}) {
		t.Fatalf("claude entry skills = %#v", claudeEntry.Skills.Include)
	}
}

func TestExpandCollectionSkillReferencingLocalRegistryWithoutRef(t *testing.T) {
	// Local filesystem registries have no ref. A collection skill that
	// references a local registry without specifying a ref should not
	// produce a "no configured ref" error — the empty ref is valid for
	// local registries.
	col := Collection{
		Name:              "local-tools",
		Skills:            []CollectionSkill{{Name: "local-skill", Registry: "/path/to/local"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{
		{URL: "/path/to/local", Ref: "", Source: config.RepoCommitted, CatalogKey: "/local/path"},
	}

	res, errs := Expand(
		[]string{"local-tools"},
		[]Collection{col},
		existing,
	)
	if len(errs) != 0 {
		t.Fatalf("expected no errors for local registry, got %v", errs)
	}
	if len(res.SyntheticEntries) != 1 {
		t.Fatalf("expected 1 synthetic entry, got %d", len(res.SyntheticEntries))
	}
	s := res.SyntheticEntries[0]
	if s.URL != "/path/to/local" {
		t.Fatalf("expected URL /path/to/local, got %q", s.URL)
	}
	if s.Ref != "" {
		t.Fatalf("expected empty ref for local registry, got %q", s.Ref)
	}
}

func TestIsLocalRegistry(t *testing.T) {
	entries := []config.RegistryEntry{
		{URL: "/local/path", Ref: ""},
		{URL: "git@example.com:remote.git", Ref: "main"},
	}
	if !isLocalRegistry("/local/path", entries) {
		t.Fatalf("expected /local/path to be local")
	}
	if isLocalRegistry("git@example.com:remote.git", entries) {
		t.Fatalf("expected git@ URL to not be local")
	}
	if isLocalRegistry("/nonexistent", entries) {
		t.Fatalf("expected unconfigured path to not match")
	}
}

func TestExpandResourcesKeyPopulatesMultipleKinds(t *testing.T) {
	col := Collection{
		Name:    "security",
		Version: "1.0",
		Resources: []CollectionEntry{
			{Name: "threat-model"}, // default kind = skill
			{Name: "security-scan", Kind: "command"},
			{Name: "pre-commit-lint", Kind: "hook"},
			{Name: "sentry", Kind: "mcp"},
		},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
		SourceCatalogKey:  "/cache/core",
	}
	existing := []config.RegistryEntry{{
		URL:        "git@example.com:core.git",
		Ref:        "main",
		Source:     config.UserPersonal,
		CatalogKey: "/cache/core",
		Skills:     config.KindSelector{Exclude: []string{"legacy-skill"}},
	}}

	res, errs := Expand(
		[]string{"security"},
		[]Collection{col},
		existing,
	)

	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(res.SyntheticEntries) != 1 {
		t.Fatalf("expected 1 synthetic entry, got %#v", res.SyntheticEntries)
	}
	s := res.SyntheticEntries[0]

	// Skills selector should have "threat-model"
	if !reflect.DeepEqual(s.Skills.Include, []string{"threat-model"}) {
		t.Fatalf("Skills.Include = %#v, want [threat-model]", s.Skills.Include)
	}
	// Commands selector should have "security-scan"
	if !reflect.DeepEqual(s.Commands.Include, []string{"security-scan"}) {
		t.Fatalf("Commands.Include = %#v, want [security-scan]", s.Commands.Include)
	}
	// Hooks selector should have "pre-commit-lint"
	if !reflect.DeepEqual(s.Hooks.Include, []string{"pre-commit-lint"}) {
		t.Fatalf("Hooks.Include = %#v, want [pre-commit-lint]", s.Hooks.Include)
	}
	// MCP selector should have "sentry"
	if !reflect.DeepEqual(s.MCP.Include, []string{"sentry"}) {
		t.Fatalf("MCP.Include = %#v, want [sentry]", s.MCP.Include)
	}
	// Subagents should be empty (no subagent entries in collection)
	if len(s.Subagents.Include) != 0 {
		t.Fatalf("Subagents.Include = %#v, want empty", s.Subagents.Include)
	}

	// Exclude list should be propagated to all kinds
	if !reflect.DeepEqual(s.Skills.Exclude, []string{"legacy-skill"}) {
		t.Fatalf("Skills.Exclude = %#v, want [legacy-skill]", s.Skills.Exclude)
	}
	if !reflect.DeepEqual(s.Commands.Exclude, []string{"legacy-skill"}) {
		t.Fatalf("Commands.Exclude = %#v, want [legacy-skill]", s.Commands.Exclude)
	}

	// ViaMap should annotate all four resources with kind-aware keys
	want := map[string]string{
		"skill:threat-model":    "collection:security@1.0",
		"command:security-scan": "collection:security@1.0",
		"hook:pre-commit-lint":  "collection:security@1.0",
		"mcpserver:sentry":      "collection:security@1.0",
	}
	if !reflect.DeepEqual(res.ViaMap, want) {
		t.Fatalf("ViaMap = %#v, want %#v", res.ViaMap, want)
	}
}

func TestExpandResourcesAndSkillsKeysCoexist(t *testing.T) {
	// Both the legacy skills: key and the resources: key can be used together.
	// They should merge skills into the same KindSelector.
	col := Collection{
		Name: "mixed",
		Skills: []CollectionSkill{
			{Name: "legacy-skill"},
		},
		Resources: []CollectionEntry{
			{Name: "new-skill"}, // skill kind (default)
			{Name: "build", Kind: "command"},
		},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{{
		URL:    "git@example.com:core.git",
		Ref:    "main",
		Source: config.RepoCommitted,
	}}

	res, errs := Expand(
		[]string{"mixed"},
		[]Collection{col},
		existing,
	)

	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(res.SyntheticEntries) != 1 {
		t.Fatalf("expected 1 synthetic entry, got %d", len(res.SyntheticEntries))
	}
	s := res.SyntheticEntries[0]

	got := sortedCopy(s.Skills.Include)
	if !reflect.DeepEqual(got, []string{"legacy-skill", "new-skill"}) {
		t.Fatalf("Skills.Include = %#v, want [legacy-skill new-skill]", got)
	}
	if !reflect.DeepEqual(s.Commands.Include, []string{"build"}) {
		t.Fatalf("Commands.Include = %#v, want [build]", s.Commands.Include)
	}
}

func TestExpandResourcesSubagentKind(t *testing.T) {
	col := Collection{
		Name: "agents",
		Resources: []CollectionEntry{
			{Name: "reviewer", Kind: "subagent"},
		},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{{
		URL:    "git@example.com:core.git",
		Ref:    "main",
		Source: config.UserPersonal,
	}}

	res, errs := Expand(
		[]string{"agents"},
		[]Collection{col},
		existing,
	)

	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(res.SyntheticEntries) != 1 {
		t.Fatalf("expected 1 synthetic entry, got %d", len(res.SyntheticEntries))
	}
	s := res.SyntheticEntries[0]

	if !reflect.DeepEqual(s.Subagents.Include, []string{"reviewer"}) {
		t.Fatalf("Subagents.Include = %#v, want [reviewer]", s.Subagents.Include)
	}
	// Other kinds should be empty
	if len(s.Skills.Include) != 0 || len(s.Commands.Include) != 0 || len(s.Hooks.Include) != 0 || len(s.MCP.Include) != 0 {
		t.Fatalf("expected other kinds empty, got skills=%v commands=%v hooks=%v mcp=%v",
			s.Skills.Include, s.Commands.Include, s.Hooks.Include, s.MCP.Include)
	}
}

func TestExpandResourcesFromDifferentRegistry(t *testing.T) {
	col := Collection{
		Name: "cross",
		Resources: []CollectionEntry{
			{Name: "threat-model"}, // same registry
			{Name: "pre-commit-check", Kind: "hook", Registry: "git@example.com:hooks.git"},
		},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Source: config.RepoCommitted, CatalogKey: "/cache/core"},
		{URL: "git@example.com:hooks.git", Ref: "stable", Source: config.UserPersonal, CatalogKey: "/cache/hooks"},
	}

	res, errs := Expand(
		[]string{"cross"},
		[]Collection{col},
		existing,
	)

	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(res.SyntheticEntries) != 2 {
		t.Fatalf("expected 2 synthetic entries, got %d", len(res.SyntheticEntries))
	}

	coreEntry := lookupSynth(t, res.SyntheticEntries, "git@example.com:core.git")
	if !reflect.DeepEqual(coreEntry.Skills.Include, []string{"threat-model"}) {
		t.Fatalf("core entry Skills.Include = %#v", coreEntry.Skills.Include)
	}

	hooksEntry := lookupSynth(t, res.SyntheticEntries, "git@example.com:hooks.git")
	if hooksEntry.Ref != "stable" {
		t.Fatalf("hooks entry Ref = %q, want stable", hooksEntry.Ref)
	}
	if !reflect.DeepEqual(hooksEntry.Hooks.Include, []string{"pre-commit-check"}) {
		t.Fatalf("hooks entry Hooks.Include = %#v", hooksEntry.Hooks.Include)
	}
}

func TestExpandResourcesKindMcpServerAlias(t *testing.T) {
	// Both "mcp" and "mcpserver" should resolve to KindMcpServer.
	col := Collection{
		Name: "mcp-alias",
		Resources: []CollectionEntry{
			{Name: "server-a", Kind: "mcp"},
			{Name: "server-b", Kind: "mcpserver"},
		},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{{
		URL:    "git@example.com:core.git",
		Ref:    "main",
		Source: config.UserPersonal,
	}}

	res, errs := Expand(
		[]string{"mcp-alias"},
		[]Collection{col},
		existing,
	)

	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(res.SyntheticEntries) != 1 {
		t.Fatalf("expected 1 synthetic entry, got %d", len(res.SyntheticEntries))
	}
	s := res.SyntheticEntries[0]

	got := sortedCopy(s.MCP.Include)
	if !reflect.DeepEqual(got, []string{"server-a", "server-b"}) {
		t.Fatalf("MCP.Include = %#v, want [server-a server-b]", got)
	}
}

func TestExpandIncludesWithMultiKindResources(t *testing.T) {
	child := Collection{
		Name: "child",
		Resources: []CollectionEntry{
			{Name: "inner-hook", Kind: "hook"},
		},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	parent := Collection{
		Name: "parent",
		Resources: []CollectionEntry{
			{Name: "outer-skill"},
			{Name: "outer-command", Kind: "command"},
		},
		Includes:          []CollectionInclude{{Name: "child"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{{
		URL:    "git@example.com:core.git",
		Ref:    "main",
		Source: config.RepoCommitted,
	}}

	res, errs := Expand(
		[]string{"parent"},
		[]Collection{parent, child},
		existing,
	)

	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(res.SyntheticEntries) != 1 {
		t.Fatalf("expected 1 synthetic entry, got %d", len(res.SyntheticEntries))
	}
	s := res.SyntheticEntries[0]

	if !reflect.DeepEqual(s.Skills.Include, []string{"outer-skill"}) {
		t.Fatalf("Skills.Include = %#v, want [outer-skill]", s.Skills.Include)
	}
	if !reflect.DeepEqual(s.Commands.Include, []string{"outer-command"}) {
		t.Fatalf("Commands.Include = %#v, want [outer-command]", s.Commands.Include)
	}
	if !reflect.DeepEqual(s.Hooks.Include, []string{"inner-hook"}) {
		t.Fatalf("Hooks.Include = %#v, want [inner-hook]", s.Hooks.Include)
	}
}

func TestExpandDirectlyConfiguredCommandNotAnnotated(t *testing.T) {
	col := Collection{
		Name: "tools",
		Resources: []CollectionEntry{
			{Name: "build", Kind: "command"},
			{Name: "test", Kind: "command"},
		},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{{
		URL:      "git@example.com:core.git",
		Ref:      "main",
		Commands: config.KindSelector{Include: []string{"build"}}, // directly configured
		Source:   config.RepoCommitted,
	}}

	res, errs := Expand(
		[]string{"tools"},
		[]Collection{col},
		existing,
	)

	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	// "build" should NOT be annotated (direct config wins); "test" should be.
	if _, ok := res.ViaMap["command:build"]; ok {
		t.Fatalf("expected build NOT in ViaMap (directly configured), got %#v", res.ViaMap)
	}
	if res.ViaMap["command:test"] != "collection:tools" {
		t.Fatalf("expected test annotated, got %q", res.ViaMap["command:test"])
	}
}

func TestExpandEmptyCollectionNotAnError(t *testing.T) {
	// A collection with no skills and no resources is valid.
	col := Collection{
		Name:              "empty",
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{{
		URL:    "git@example.com:core.git",
		Ref:    "main",
		Source: config.UserPersonal,
	}}

	res, errs := Expand(
		[]string{"empty"},
		[]Collection{col},
		existing,
	)

	if len(errs) != 0 {
		t.Fatalf("expected no errors for empty collection, got %v", errs)
	}
	// No synthetic entries because there are no resources to install.
	if len(res.SyntheticEntries) != 0 {
		t.Fatalf("expected 0 synthetic entries for empty collection, got %d", len(res.SyntheticEntries))
	}
}

func TestParseKind(t *testing.T) {
	tests := []struct {
		input   string
		want    ir.ResourceKind
		wantErr bool
	}{
		{"", ir.KindSkill, false},
		{"skill", ir.KindSkill, false},
		{"Skill", ir.KindSkill, false},
		{"  skill  ", ir.KindSkill, false},
		{"command", ir.KindCommand, false},
		{"COMMAND", ir.KindCommand, false},
		{"subagent", ir.KindSubagent, false},
		{"hook", ir.KindHook, false},
		{"mcp", ir.KindMcpServer, false},
		{"mcpserver", ir.KindMcpServer, false},
		{"MCP", ir.KindMcpServer, false},
		{"unknown", ir.KindSkill, true}, // unknown kinds now return an error
		{"commnad", ir.KindSkill, true}, // typo returns an error
	}
	for _, tt := range tests {
		got, err := parseKind(tt.input)
		if tt.wantErr && err == nil {
			t.Errorf("parseKind(%q) = %v, want error", tt.input, got)
		}
		if !tt.wantErr && err != nil {
			t.Errorf("parseKind(%q) unexpected error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Errorf("parseKind(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestExpandUnknownKindProducesError(t *testing.T) {
	// A typo like "commnad" should surface as an error, not silently
	// become a skill.
	col := Collection{
		Name: "typo",
		Resources: []CollectionEntry{
			{Name: "build", Kind: "commnad"},
		},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{{
		URL:    "git@example.com:core.git",
		Ref:    "main",
		Source: config.UserPersonal,
	}}

	_, errs := Expand(
		[]string{"typo"},
		[]Collection{col},
		existing,
	)
	if len(errs) == 0 {
		t.Fatalf("expected error for unknown kind, got none")
	}
	if !strings.Contains(errs[0].Error(), "unsupported resource kind") {
		t.Fatalf("expected 'unsupported resource kind' error, got %v", errs[0])
	}
	if !strings.Contains(errs[0].Error(), "commnad") {
		t.Fatalf("expected error to mention the invalid kind string, got %v", errs[0])
	}
}

func TestExpandViaMapCrossKindNoCollision(t *testing.T) {
	// A skill "build" and a command "build" should each get separate
	// ViaMap entries (kind-aware keys) without overwriting each other.
	col := Collection{
		Name: "dev",
		Resources: []CollectionEntry{
			{Name: "build"},                  // skill (default kind)
			{Name: "build", Kind: "command"}, // command with same name
		},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{{
		URL:    "git@example.com:core.git",
		Ref:    "main",
		Source: config.UserPersonal,
	}}

	res, errs := Expand(
		[]string{"dev"},
		[]Collection{col},
		existing,
	)

	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	// Both entries should be present with kind-aware keys
	if res.ViaMap["skill:build"] != "collection:dev" {
		t.Fatalf("ViaMap[skill:build] = %q, want collection:dev", res.ViaMap["skill:build"])
	}
	if res.ViaMap["command:build"] != "collection:dev" {
		t.Fatalf("ViaMap[command:build] = %q, want collection:dev", res.ViaMap["command:build"])
	}

	// Both kinds should have "build" in their respective selectors
	s := res.SyntheticEntries[0]
	if !reflect.DeepEqual(s.Skills.Include, []string{"build"}) {
		t.Fatalf("Skills.Include = %#v, want [build]", s.Skills.Include)
	}
	if !reflect.DeepEqual(s.Commands.Include, []string{"build"}) {
		t.Fatalf("Commands.Include = %#v, want [build]", s.Commands.Include)
	}
}

func TestExpandDirectlyConfiguredSkillDoesNotSuppressCommandVia(t *testing.T) {
	// A directly-configured skill "build" should NOT suppress the Via
	// annotation for a collected command "build" — they are different
	// resource kinds.
	col := Collection{
		Name: "dev",
		Resources: []CollectionEntry{
			{Name: "build"},                  // skill (default kind)
			{Name: "build", Kind: "command"}, // command with same name
		},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{{
		URL:    "git@example.com:core.git",
		Ref:    "main",
		Source: config.RepoCommitted,
		Skills: config.KindSelector{Include: []string{"build"}}, // skill directly configured
	}}

	res, errs := Expand(
		[]string{"dev"},
		[]Collection{col},
		existing,
	)

	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	// Skill "build" is directly configured → not annotated
	if _, ok := res.ViaMap["skill:build"]; ok {
		t.Fatalf("expected skill:build NOT in ViaMap (directly configured)")
	}
	// Command "build" is only from collection → should be annotated
	if res.ViaMap["command:build"] != "collection:dev" {
		t.Fatalf("ViaMap[command:build] = %q, want collection:dev", res.ViaMap["command:build"])
	}
}
