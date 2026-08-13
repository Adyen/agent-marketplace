package resolver

// TransportNeutralityTests verifies that skill selection, tag filtering, and
// exclude logic produce identical results regardless of whether a registry is
// accessed via a git remote or a local filesystem path. This is a regression
// guard for the invariant stated in the implementation plan:
//
//	"No resolver, sync, precedence, exclude, or override logic may branch on
//	 transport (git vs local). Only fetch/update paths may distinguish them."
//
// If any of these tests fail after a code change, it means selection logic has
// accidentally started treating local and git registries differently.

import (
	"reflect"
	"sort"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
)

// skillNames extracts sorted skill names from a resolved slice.
func skillNames(skills []ResolvedArtifact) []string {
	names := make([]string, 0, len(skills))
	for _, s := range skills {
		names = append(names, s.Name)
	}
	sort.Strings(names)
	return names
}

// skillMeta extracts (Name, Tags, Description) from a resolved slice, keyed by
// name. Used to compare content without comparing transport-specific fields
// (RegistryURL, Ref, CatalogKey, Path).
type skillMeta struct {
	Name        string
	Description string
	Tags        []string
}

func skillMetaMap(skills []ResolvedArtifact) map[string]skillMeta {
	m := make(map[string]skillMeta, len(skills))
	for _, s := range skills {
		tags := append([]string(nil), s.Tags...)
		sort.Strings(tags)
		m[s.Name] = skillMeta{Name: s.Name, Description: s.Description, Tags: tags}
	}
	return m
}

// buildCatalog returns a catalog keyed by catalogKey containing the provided skills.
func buildCatalog(catalogKey string, skills []catalog.Entry) map[string][]catalog.Entry {
	return map[string][]catalog.Entry{catalogKey: skills}
}

// testSkills is a shared catalog used across transport-neutrality tests.
var testSkills = []catalog.Entry{
	{Name: "alpha", Tags: []string{"compute"}, Description: "Alpha skill", Path: "/base/alpha"},
	{Name: "bravo", Tags: []string{"network", "compute"}, Description: "Bravo skill", Path: "/base/bravo"},
	{Name: "charlie", Tags: []string{"network"}, Description: "Charlie skill", Path: "/base/charlie"},
	{Name: "delta", Tags: []string{"security"}, Description: "Delta skill", Path: "/base/delta"},
}

const (
	gitURL       = "git@example.com:org/skills.git"
	gitRef       = "main"
	gitCatalog   = "/fake-cache/git-registry"
	localURL     = "/opt/local-skills"
	localCatalog = "/opt/local-skills"
)

// makeGitEntry builds a git-backed registry entry with the given selection fields.
func makeGitEntry(skills, tags, exclude []string, required bool) config.RegistryEntry {
	return config.RegistryEntry{
		URL:        gitURL,
		Ref:        gitRef,
		CatalogKey: gitCatalog,
		Skills:     config.KindSelector{Include: skills, Exclude: exclude},
		Tags:       tags,
		Required:   required,
	}
}

// makeLocalEntry builds a local-filesystem registry entry with the same selection fields.
func makeLocalEntry(skills, tags, exclude []string, required bool) config.RegistryEntry {
	return config.RegistryEntry{
		URL:        localURL,
		CatalogKey: localCatalog,
		Skills:     config.KindSelector{Include: skills, Exclude: exclude},
		Tags:       tags,
		Required:   required,
	}
}

func resolve(entry config.RegistryEntry, catalogKey string) ([]ResolvedArtifact, []error) {
	cfg := &config.Config{Registries: []config.RegistryEntry{entry}}
	catalogs := buildCatalog(catalogKey, testSkills)
	return Resolve(cfg, catalogs, nil)
}

// TestTransportNeutral_ExplicitSkills verifies that selecting skills by name works
// identically for git and local registries.
func TestTransportNeutral_ExplicitSkills(t *testing.T) {
	gitEntry := makeGitEntry([]string{"alpha", "charlie"}, nil, nil, false)
	localEntry := makeLocalEntry([]string{"alpha", "charlie"}, nil, nil, false)

	gitResolved, gitErrs := resolve(gitEntry, gitCatalog)
	localResolved, localErrs := resolve(localEntry, localCatalog)

	if len(gitErrs) != 0 || len(localErrs) != 0 {
		t.Fatalf("unexpected errors: git=%v local=%v", gitErrs, localErrs)
	}

	gitNames := skillNames(gitResolved)
	localNames := skillNames(localResolved)
	if !reflect.DeepEqual(gitNames, localNames) {
		t.Errorf("explicit-skills selection differs by transport:\n  git:   %v\n  local: %v", gitNames, localNames)
	}

	if !reflect.DeepEqual(skillMetaMap(gitResolved), skillMetaMap(localResolved)) {
		t.Errorf("skill metadata differs by transport:\n  git:   %v\n  local: %v", skillMetaMap(gitResolved), skillMetaMap(localResolved))
	}
}

// TestTransportNeutral_TagSelection verifies that tag-based selection works
// identically for git and local registries.
func TestTransportNeutral_TagSelection(t *testing.T) {
	gitEntry := makeGitEntry(nil, []string{"compute"}, nil, false)
	localEntry := makeLocalEntry(nil, []string{"compute"}, nil, false)

	gitResolved, gitErrs := resolve(gitEntry, gitCatalog)
	localResolved, localErrs := resolve(localEntry, localCatalog)

	if len(gitErrs) != 0 || len(localErrs) != 0 {
		t.Fatalf("unexpected errors: git=%v local=%v", gitErrs, localErrs)
	}

	gitNames := skillNames(gitResolved)
	localNames := skillNames(localResolved)
	if !reflect.DeepEqual(gitNames, localNames) {
		t.Errorf("tag selection differs by transport:\n  git:   %v\n  local: %v", gitNames, localNames)
	}
}

// TestTransportNeutral_ExcludeFilter verifies that exclude lists filter skills
// identically for git and local registries.
func TestTransportNeutral_ExcludeFilter(t *testing.T) {
	gitEntry := makeGitEntry(nil, []string{"network"}, []string{"charlie"}, false)
	localEntry := makeLocalEntry(nil, []string{"network"}, []string{"charlie"}, false)

	gitResolved, gitErrs := resolve(gitEntry, gitCatalog)
	localResolved, localErrs := resolve(localEntry, localCatalog)

	if len(gitErrs) != 0 || len(localErrs) != 0 {
		t.Fatalf("unexpected errors: git=%v local=%v", gitErrs, localErrs)
	}

	gitNames := skillNames(gitResolved)
	localNames := skillNames(localResolved)
	if !reflect.DeepEqual(gitNames, localNames) {
		t.Errorf("exclude filter differs by transport:\n  git:   %v\n  local: %v", gitNames, localNames)
	}

	for _, name := range gitNames {
		if name == "charlie" {
			t.Errorf("excluded skill 'charlie' still present in git result")
		}
	}
}

// TestTransportNeutral_RequiredIncludesAll verifies that required:true includes
// all catalog skills identically for git and local registries.
func TestTransportNeutral_RequiredIncludesAll(t *testing.T) {
	gitEntry := makeGitEntry(nil, nil, nil, true)
	localEntry := makeLocalEntry(nil, nil, nil, true)

	gitResolved, gitErrs := resolve(gitEntry, gitCatalog)
	localResolved, localErrs := resolve(localEntry, localCatalog)

	if len(gitErrs) != 0 || len(localErrs) != 0 {
		t.Fatalf("unexpected errors: git=%v local=%v", gitErrs, localErrs)
	}

	gitNames := skillNames(gitResolved)
	localNames := skillNames(localResolved)
	if !reflect.DeepEqual(gitNames, localNames) {
		t.Errorf("required selection differs by transport:\n  git:   %v\n  local: %v", gitNames, localNames)
	}

	if len(gitNames) != len(testSkills) {
		t.Errorf("required:true should select all %d skills, got %d", len(testSkills), len(gitNames))
	}
}

// TestTransportNeutral_RequiredIgnoresExclude verifies that required:true ignores
// exclude lists identically for git and local registries.
func TestTransportNeutral_RequiredIgnoresExclude(t *testing.T) {
	// Required:true entries with explicit skills AND an exclude should
	// ignore the exclude (required skills are immune to exclusion).
	allSkills := []string{"alpha", "bravo", "charlie", "delta"}
	gitEntry := makeGitEntry(allSkills, nil, []string{"alpha"}, true)
	localEntry := makeLocalEntry(allSkills, nil, []string{"alpha"}, true)

	gitResolved, gitErrs := resolve(gitEntry, gitCatalog)
	localResolved, localErrs := resolve(localEntry, localCatalog)

	if len(gitErrs) != 0 || len(localErrs) != 0 {
		t.Fatalf("unexpected errors: git=%v local=%v", gitErrs, localErrs)
	}

	gitNames := skillNames(gitResolved)
	localNames := skillNames(localResolved)
	if !reflect.DeepEqual(gitNames, localNames) {
		t.Errorf("required+exclude behavior differs by transport:\n  git:   %v\n  local: %v", gitNames, localNames)
	}

	if len(gitNames) != len(testSkills) {
		t.Errorf("required:true should include all %d skills even with exclude, got %d", len(testSkills), len(gitNames))
	}
}
