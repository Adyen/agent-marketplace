package resolver

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// Adversarial coverage for agent-marketplace-qbfm. Containment is now the
// selector for a path-less collection member, so these tests attack the
// selector itself: can it bind to a resource the user did not mean, can it
// change the severity of a policy-mandated miss, and is it deterministic?

// findSNF returns the SelectedNotFoundError for name, if any.
func findSNF(errs []error, name string) *SelectedNotFoundError {
	for _, err := range errs {
		var snf *SelectedNotFoundError
		if errors.As(err, &snf) && snf.Name == name {
			return snf
		}
	}
	return nil
}

func findAmbiguity(errs []error, name string) *AmbiguousCollectionMemberWarning {
	for _, err := range errs {
		var amb *AmbiguousCollectionMemberWarning
		if errors.As(err, &amb) && amb.Name == name {
			return amb
		}
	}
	return nil
}

// TestPathLessMemberPrefersRequiredContainingEntry attacks org policy: when a
// required (org-mandated) entry and a non-required entry both contain the
// member, binding by config order can strip Required from the materialized
// artifact, which makes it excludable by a personal config. `required: true`
// immunity must survive collection binding.
func TestPathLessMemberPrefersRequiredContainingEntry(t *testing.T) {
	col := goToolsCollection(ir.CollectionMember{Name: "efficient-go", Kind: ir.KindSkill, Registry: monorepoURL})

	// The non-required entry is first in config order; the required (org)
	// entry is second. Both contain the member. The required entry carries an
	// explicit selection so it does not auto-select every resource, which is
	// the realistic org-profile shape.
	cfg, catalogs := buildMemberCase(t, col, []memberFixture{
		{url: monorepoURL, path: "personal/scratch", names: []string{"efficient-go"}},
		{url: monorepoURL, path: "teams/security/plugins/devsecops", required: true, names: []string{"efficient-go", "devsecops"}},
	})
	cfg.Registries[2].Skills = ks("devsecops")
	setCatalogKeys(cfg)

	resolved, _ := Resolve(cfg, catalogs, nil)
	got := findResolved(resolved, "efficient-go")
	if got == nil {
		t.Fatalf("expected efficient-go to resolve, got %+v", resolved)
	}
	if !got.Required {
		t.Fatalf("a required entry contains this member; binding must not strip required-ness (bound to %q, Required=%v)", got.RegistryPath, got.Required)
	}
	if got.RegistryPath != "teams/security/plugins/devsecops" {
		t.Fatalf("expected the org-mandated location to win over config order, got %q", got.RegistryPath)
	}
}

// TestPathLessMemberSkipsLocallyExcludedRepoOwner verifies that owner filtering
// happens before precedence selection: a repo-local exclusion cannot hide a
// compatible machine-scope owner of the same physical repository.
func TestPathLessMemberSkipsLocallyExcludedRepoOwner(t *testing.T) {
	member := ir.CollectionMember{Name: "efficient-go", Kind: ir.KindSkill, Registry: monorepoURL}
	cfg, catalogs := buildMemberCase(t, goToolsCollection(member), []memberFixture{
		{url: monorepoURL, path: "repo/tools", names: []string{"efficient-go"}},
		{url: monorepoURL, path: "machine/tools", names: []string{"efficient-go"}},
	})
	cfg.Registries[1].Source = config.RepoPersonal
	cfg.Registries[2].Source = config.UserPersonal
	setCatalogKeys(cfg)

	resolved, errs := Resolve(cfg, catalogs, &config.LocalConfig{
		Skills: kse(nil, []string{"efficient-go"}),
	})
	if len(errs) != 0 {
		t.Fatalf("expected local exclusion to be a no-op for the machine owner, got %v", errs)
	}
	got := findResolved(resolved, "efficient-go")
	if got == nil {
		t.Fatalf("expected efficient-go from the machine owner, got %+v", resolved)
	}
	if got.Source != config.UserPersonal || got.RegistryPath != "machine/tools" {
		t.Fatalf("expected compatible machine owner, got source=%v path=%q", got.Source, got.RegistryPath)
	}
}

// TestPathLessMemberChoosesHarnessCompatibleOwner verifies harness filtering is
// applied before authority selection, while an incompatible required org owner
// still emits its hard policy diagnostic.
func TestPathLessMemberChoosesHarnessCompatibleOwner(t *testing.T) {
	member := ir.CollectionMember{Name: "efficient-go", Kind: ir.KindSkill, Registry: monorepoURL}
	cfg, catalogs := buildMemberCase(t, goToolsCollection(member), []memberFixture{
		{url: monorepoURL, path: "org/tools", required: true, names: []string{"efficient-go"}},
		{url: monorepoURL, path: "machine/tools", names: []string{"efficient-go"}},
	})
	cfg.Registries[1].Source = config.Organization
	cfg.Registries[1].Harnesses = []string{"claude"}
	cfg.Registries[2].Source = config.UserPersonal
	cfg.Registries[2].Harnesses = []string{"factory"}
	setCatalogKeys(cfg)

	resolved, errs := Resolve(cfg, catalogs, nil, WithTargetHarnesses([]hcc.Target{hcc.Target("factory")}))
	got := findResolved(resolved, "efficient-go")
	if got == nil || got.RegistryPath != "machine/tools" {
		t.Fatalf("expected harness-compatible machine owner, got %+v (errs=%v)", got, errs)
	}
	var incompatible *IncompatibleOrganizationRequirement
	for _, err := range errs {
		if errors.As(err, &incompatible) {
			break
		}
	}
	if incompatible == nil {
		t.Fatalf("expected incompatible required organization diagnostic, got %v", errs)
	}
}

// TestPathLessMemberBindsToRequiredOnlyProvider is the inverse check: when the
// only containing entry is required, the member must keep its required-ness
// (no silent downgrade of an org-mandated resource).
func TestPathLessMemberBindsToRequiredOnlyProvider(t *testing.T) {
	col := goToolsCollection(ir.CollectionMember{Name: "efficient-go", Kind: ir.KindSkill, Registry: monorepoURL})

	cfg, catalogs := buildMemberCase(t, col, []memberFixture{
		{url: monorepoURL, path: "teams/development/plugins/dev-openspec", names: []string{"openspec-propose"}},
		{url: monorepoURL, path: "teams/security/plugins/devsecops", required: true, names: []string{"efficient-go", "devsecops"}},
	})
	cfg.Registries[2].Skills = ks("devsecops")
	setCatalogKeys(cfg)

	resolved, _ := Resolve(cfg, catalogs, nil)
	got := findResolved(resolved, "efficient-go")
	if got == nil {
		t.Fatalf("expected efficient-go to resolve from the required entry, got %+v", resolved)
	}
	if !got.Required {
		t.Fatalf("the only provider is required: %+v", got)
	}
}

// TestAllRequiredIsNotDowngradableByADuplicateSpellingOfTheSameLocation closes
// the most dangerous direction of the severity rule: re-declaring the org's
// own entry (same physical location, only the ".git" spelling differs) without
// `required: true` must not turn a hard, org-mandated miss into a warning.
// Since agent-marketplace-0236 no added entry can downgrade it at all, but the
// per-location fold is still what makes the two spellings one location.
func TestAllRequiredIsNotDowngradableByADuplicateSpellingOfTheSameLocation(t *testing.T) {
	member := ir.CollectionMember{Name: "efficient-go", Kind: ir.KindSkill, Registry: monorepoURL}

	// Baseline: the org entry alone keeps the miss hard.
	cfg, catalogs := buildMemberCase(t, goToolsCollection(member), []memberFixture{
		{url: monorepoURL, path: "teams/security/plugins/devsecops", required: true, names: []string{"devsecops"}},
	})
	_, errs := Resolve(cfg, catalogs, nil)
	if snf := findSNF(errs, "efficient-go"); snf == nil || !snf.Required {
		t.Fatalf("baseline: expected a hard miss, got %v", errs)
	}

	// Attack: a personal duplicate of the very same location, spelled with
	// ".git" and not marked required.
	cfg, catalogs = buildMemberCase(t, goToolsCollection(member), []memberFixture{
		{url: monorepoURL, path: "teams/security/plugins/devsecops", required: true, names: []string{"devsecops"}},
		{url: monorepoDotGit, path: "teams/security/plugins/devsecops", names: []string{"devsecops"}},
	})
	_, errs = Resolve(cfg, catalogs, nil)
	snf := findSNF(errs, "efficient-go")
	if snf == nil {
		t.Fatalf("expected a miss, got %v", errs)
	}
	if !snf.Required {
		t.Fatalf("a non-required duplicate of the SAME location downgraded the org-mandated miss to a warning: %v", snf)
	}
}

// TestPathLessMemberSkipsExcludeAllScopedEntry attacks the "deliberately scoped
// away" case: a user who ran `remove --from <url> --path <p>` gets an
// `exclude_all` on that entry. Containment must not bind a collection member to
// a directory the user scoped away when another configured location provides it.
func TestPathLessMemberSkipsExcludeAllScopedEntry(t *testing.T) {
	col := goToolsCollection(ir.CollectionMember{Name: "efficient-go", Kind: ir.KindSkill, Registry: monorepoURL})

	cfg, catalogs := buildMemberCase(t, col, []memberFixture{
		{url: monorepoURL, path: "teams/legacy/plugins/old-guides", names: []string{"efficient-go"}},
		{url: monorepoURL, path: "teams/development/plugins/dev-language-guides", names: []string{"efficient-go"}},
	})
	// Scope away the first location entirely.
	cfg.Registries[1].Skills.ExcludeAll = true
	setCatalogKeys(cfg)

	resolved, _ := Resolve(cfg, catalogs, nil)
	got := findResolved(resolved, "efficient-go")
	if got == nil {
		t.Fatalf("expected efficient-go to resolve from the location that is not scoped away, got %+v", resolved)
	}
	if got.RegistryPath == "teams/legacy/plugins/old-guides" {
		t.Fatal("bound to a location the user scoped away with exclude_all while another location provides the member")
	}
}

// TestPathLessMemberExcludeAllOnlyCandidateIsNotBound is the single-candidate
// variant: the only containing location is scoped away, so nothing may
// silently materialize from it.
func TestPathLessMemberExcludeAllOnlyCandidateIsNotBound(t *testing.T) {
	col := goToolsCollection(ir.CollectionMember{Name: "efficient-go", Kind: ir.KindSkill, Registry: monorepoURL})

	cfg, catalogs := buildMemberCase(t, col, []memberFixture{
		{url: monorepoURL, path: "teams/legacy/plugins/old-guides", names: []string{"efficient-go"}},
	})
	cfg.Registries[1].Skills.ExcludeAll = true
	setCatalogKeys(cfg)

	resolved, _ := Resolve(cfg, catalogs, nil)
	if got := findResolved(resolved, "efficient-go"); got != nil {
		t.Fatalf("exclude_all scoped this location away; the member must not materialize from it, got %+v", got)
	}
}

// TestPathLessMemberDoesNotSilentlyBindDifferentRef attacks the ref fallback:
// findRegistryCandidates falls back to every entry for the URL when no entry
// matches the requested ref, so a member pinned to `ref: v2` can bind to
// content from `main` with no diagnostic at all.
func TestPathLessMemberDoesNotSilentlyBindDifferentRef(t *testing.T) {
	col := goToolsCollection(ir.CollectionMember{
		Name:     "efficient-go",
		Kind:     ir.KindSkill,
		Registry: monorepoURL,
		Ref:      "v2-frozen",
	})

	cfg, catalogs := buildMemberCase(t, col, []memberFixture{
		{url: monorepoURL, path: "teams/development/plugins/dev-language-guides", ref: "main", names: []string{"efficient-go"}},
	})

	resolved, errs := Resolve(cfg, catalogs, nil)
	got := findResolved(resolved, "efficient-go")
	var reported bool
	for _, err := range errs {
		if strings.Contains(err.Error(), "v2-frozen") {
			reported = true
		}
	}
	if got == nil && !reported {
		t.Fatalf("member pinned to ref %q neither bound nor produced a diagnostic (errs=%v)", "v2-frozen", errs)
	}
	if got != nil && got.Ref != "v2-frozen" {
		// Binding to another ref may be tolerable, but it must be reported.
		if !reported {
			t.Fatalf("member asked for ref %q but silently bound to ref %q with no diagnostic (errs=%v)", "v2-frozen", got.Ref, errs)
		}
	}
}

// TestAnyLocationRequiredOverEmptyCandidateSetIsFalse pins the vacuous-truth question
// directly at the unit level. It survives the switch from an all-quantifier to
// an any-quantifier (agent-marketplace-0236): an empty set has no required
// location either way, so the miss stays soft.
func TestAnyLocationRequiredOverEmptyCandidateSetIsFalse(t *testing.T) {
	if anyLocationRequired(nil) {
		t.Fatal("severity over an empty candidate set must not be vacuously required")
	}
	if anyLocationRequired([]memberEntryCandidate{}) {
		t.Fatal("severity over an empty candidate set must not be vacuously required")
	}
}

// TestPathLessMissIsSuppressedWhenMemberResolvesElsewhere attacks the
// cross-registry suppression contract: the member is absent from its declared
// repository but the very same (kind, name) is already resolved from another
// registry, so the miss is a false positive.
func TestPathLessMissIsSuppressedWhenMemberResolvesElsewhere(t *testing.T) {
	col := goToolsCollection(ir.CollectionMember{Name: "efficient-go", Kind: ir.KindSkill, Registry: monorepoURL})

	cfg, catalogs := buildMemberCase(t, col, []memberFixture{
		{url: monorepoURL, path: "teams/development/plugins/dev-openspec", names: []string{"openspec-propose"}},
	})
	// A completely different registry provides efficient-go and selects it.
	other := config.RegistryEntry{
		URL:    "git@example.com:go-guides.git",
		Ref:    "main",
		Skills: ks("efficient-go"),
	}
	cfg.Registries = append(cfg.Registries, other)
	setCatalogKeys(cfg)
	key := cfg.Registries[len(cfg.Registries)-1].CatalogKey
	catalogs[key] = []catalog.Entry{{Kind: ir.KindSkill, Name: "efficient-go", Path: "/cache/other/efficient-go"}}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if findResolved(resolved, "efficient-go") == nil {
		t.Fatalf("expected efficient-go from the other registry, got %+v", resolved)
	}
	if snf := findSNF(errs, "efficient-go"); snf != nil {
		t.Fatalf("efficient-go is resolved from another registry; the collection-member miss is a false positive: %v", snf)
	}
}

// TestPathLessMissRequiredIsSuppressedWhenMemberResolvesElsewhere is the hard
// variant of the previous attack: when every configured entry for the declared
// repository is required, the false-positive miss becomes a HARD sync failure
// for a resource that is present and resolved.
func TestPathLessMissRequiredIsSuppressedWhenMemberResolvesElsewhere(t *testing.T) {
	col := goToolsCollection(ir.CollectionMember{Name: "efficient-go", Kind: ir.KindSkill, Registry: monorepoURL})

	cfg, catalogs := buildMemberCase(t, col, []memberFixture{
		{url: monorepoURL, path: "teams/security/plugins/devsecops", required: true, names: []string{"devsecops"}},
	})
	other := config.RegistryEntry{
		URL:    "git@example.com:go-guides.git",
		Ref:    "main",
		Skills: ks("efficient-go"),
	}
	cfg.Registries = append(cfg.Registries, other)
	setCatalogKeys(cfg)
	key := cfg.Registries[len(cfg.Registries)-1].CatalogKey
	catalogs[key] = []catalog.Entry{{Kind: ir.KindSkill, Name: "efficient-go", Path: "/cache/other/efficient-go"}}

	_, errs := Resolve(cfg, catalogs, nil)
	for _, err := range errs {
		if IsWarning(err) || IsSoftSelectedNotFound(err) {
			continue
		}
		t.Fatalf("the member is resolved from another registry; nothing may fail the sync, got hard error: %v", err)
	}
}

// TestPathLessMissMessageNamesRefsWhenCandidatesSpanRefs attacks message
// truthfulness: the message renders one ref, so when the searched candidates
// live at different refs it claims to have searched paths at a ref it did not
// search.
func TestPathLessMissMessageNamesRefsWhenCandidatesSpanRefs(t *testing.T) {
	col := goToolsCollection(ir.CollectionMember{Name: "efficient-go", Kind: ir.KindSkill, Registry: monorepoURL})

	cfg, catalogs := buildMemberCase(t, col, []memberFixture{
		{url: monorepoURL, path: "teams/a", ref: "main", names: []string{"other-a"}},
		{url: monorepoURL, path: "teams/b", ref: "next", names: []string{"other-b"}},
	})

	_, errs := Resolve(cfg, catalogs, nil)
	snf := findSNF(errs, "efficient-go")
	if snf == nil {
		t.Fatalf("expected a miss, got %v", errs)
	}
	msg := snf.Error()
	if !strings.Contains(msg, "next") {
		t.Fatalf("the searched candidates span refs main and next; the message must not present them all as one ref: %s", msg)
	}
}

// TestPathLessMissMessageListsExactlyWhatWasSearched checks that the reported
// candidate list has no omissions and no phantoms: entries for other
// repositories must not appear, and every entry for this repository must.
func TestPathLessMissMessageListsExactlyWhatWasSearched(t *testing.T) {
	col := goToolsCollection(ir.CollectionMember{Name: "efficient-go", Kind: ir.KindSkill, Registry: monorepoURL})

	cfg, catalogs := buildMemberCase(t, col, []memberFixture{
		{url: monorepoURL, path: "teams/a", names: []string{"other-a"}},
		{url: monorepoDotGit, path: "", names: []string{"root-thing"}},
		{url: "git@example.com:unrelated.git", path: "teams/z", names: []string{"efficient-go"}},
	})

	_, errs := Resolve(cfg, catalogs, nil)
	snf := findSNF(errs, "efficient-go")
	if snf == nil {
		t.Fatalf("expected a miss, got %v", errs)
	}
	if len(snf.CandidatePaths) != 2 {
		t.Fatalf("expected exactly the two entries of the declared repository, got %#v", snf.CandidatePaths)
	}
	for _, label := range snf.CandidatePaths {
		if strings.Contains(label, "teams/z") {
			t.Fatalf("an unrelated repository's path is a phantom candidate: %#v", snf.CandidatePaths)
		}
	}
	if !strings.Contains(snf.Error(), "<repository root>") {
		t.Fatalf("the repository-root entry was searched and must be named: %s", snf.Error())
	}
}

// TestFindRegistryCandidatesIgnoresSurroundingWhitespace attacks URL
// normalization: NormalizeGitURL does not trim whitespace, so an entry whose
// URL carries a stray trailing space is invisible to the new binding code and
// the member is reported as belonging to no configured repository at all.
func TestFindRegistryCandidatesIgnoresSurroundingWhitespace(t *testing.T) {
	col := goToolsCollection(ir.CollectionMember{Name: "efficient-go", Kind: ir.KindSkill, Registry: monorepoURL})

	cfg, catalogs := buildMemberCase(t, col, []memberFixture{
		{url: monorepoURL + " ", path: "teams/development/plugins/dev-language-guides", names: []string{"efficient-go"}},
	})

	resolved, errs := Resolve(cfg, catalogs, nil)
	for _, err := range errs {
		if strings.Contains(err.Error(), "not present in any config layer") {
			t.Fatalf("the repository IS configured (its URL just has trailing whitespace): %v", err)
		}
	}
	if findResolved(resolved, "efficient-go") == nil {
		t.Fatalf("expected the member to bind across a whitespace-only URL difference, got %+v", resolved)
	}
}

// TestPathLessMemberBindingIsDeterministic runs the same resolution many times
// in one process and requires byte-identical bindings and diagnostics.
func TestPathLessMemberBindingIsDeterministic(t *testing.T) {
	member := ir.CollectionMember{Name: "efficient-go", Kind: ir.KindSkill, Registry: monorepoURL}

	var first string
	for i := 0; i < 300; i++ {
		cfg, catalogs := buildMemberCase(t, goToolsCollection(member), []memberFixture{
			{url: monorepoURL, path: "teams/a", names: []string{"efficient-go"}},
			{url: monorepoDotGit, path: "teams/b", names: []string{"efficient-go"}},
			{url: monorepoURL, path: "teams/c", names: []string{"efficient-go"}},
			{url: monorepoURL, path: "teams/d", names: []string{"efficient-go"}},
			{url: monorepoURL, path: "teams/e", names: []string{"efficient-go"}},
		})
		resolved, errs := Resolve(cfg, catalogs, nil)
		got := findResolved(resolved, "efficient-go")
		if got == nil {
			t.Fatalf("iteration %d: member did not resolve", i)
		}
		amb := findAmbiguity(errs, "efficient-go")
		if amb == nil {
			t.Fatalf("iteration %d: expected an ambiguity warning, got %v", i, errs)
		}
		sig := fmt.Sprintf("%s|%s|%s", got.RegistryPath, amb.Chosen, strings.Join(amb.Candidates, ","))
		if i == 0 {
			first = sig
			continue
		}
		if sig != first {
			t.Fatalf("iteration %d: non-deterministic binding/diagnostic\n first: %s\n  got: %s", i, first, sig)
		}
	}
}

// TestPathLessMissDiagnosticIsDeterministic does the same for the miss path,
// where the reported candidate list is built from the config slice.
func TestPathLessMissDiagnosticIsDeterministic(t *testing.T) {
	member := ir.CollectionMember{Name: "efficient-go", Kind: ir.KindSkill, Registry: monorepoURL}

	var first string
	for i := 0; i < 300; i++ {
		cfg, catalogs := buildMemberCase(t, goToolsCollection(member), []memberFixture{
			{url: monorepoURL, path: "teams/a", names: []string{"a"}},
			{url: monorepoURL, path: "teams/b", names: []string{"b"}},
			{url: monorepoURL, path: "teams/c", names: []string{"c"}},
			{url: monorepoURL, path: "teams/d", names: []string{"d"}},
		})
		_, errs := Resolve(cfg, catalogs, nil)
		snf := findSNF(errs, "efficient-go")
		if snf == nil {
			t.Fatalf("iteration %d: expected a miss, got %v", i, errs)
		}
		sig := snf.Error() + "||" + snf.Remediation()
		if i == 0 {
			first = sig
			continue
		}
		if sig != first {
			t.Fatalf("iteration %d: non-deterministic diagnostic\n first: %s\n  got: %s", i, first, sig)
		}
	}
}

// TestPathLessMemberDotGitSlashSpelling covers the ".git/" spelling, which
// NormalizeGitURL only handles because it trims "/" before ".git".
func TestPathLessMemberDotGitSlashSpelling(t *testing.T) {
	col := goToolsCollection(ir.CollectionMember{Name: "efficient-go", Kind: ir.KindSkill, Registry: monorepoURL})

	cfg, catalogs := buildMemberCase(t, col, []memberFixture{
		{url: monorepoURL + ".git/", path: "teams/development/plugins/dev-language-guides", names: []string{"efficient-go"}},
	})

	resolved, errs := Resolve(cfg, catalogs, nil)
	for _, err := range errs {
		if IsSelectedNotFound(err) {
			t.Fatalf("a %q spelling must match: %v", ".git/", err)
		}
	}
	if findResolved(resolved, "efficient-go") == nil {
		t.Fatalf("expected the member to bind across a .git/ spelling, got %+v", resolved)
	}
}

// TestUnrelatedRequiredEntryEscalatesAMissIsTheAcceptedCost records the blast
// radius the owner accepted in agent-marketplace-0236: with an any-candidate
// rule, one `required: true` entry anywhere in a monorepo makes a miss for a
// member of that repository hard, even when the mandated subdirectory has
// nothing to do with the member. This is the documented, deliberate cost of
// making `required: true` unsoftenable — not an accident. It is bounded to
// members that bind nowhere: a member that binds is unaffected.
func TestUnrelatedRequiredEntryEscalatesAMissIsTheAcceptedCost(t *testing.T) {
	// One required entry, for a totally unrelated subdirectory of the
	// monorepo, plus an ordinary entry that also does not provide the member.
	fixtures := []memberFixture{
		{url: monorepoURL, path: "teams/security/plugins/devsecops", required: true, names: []string{"devsecops"}},
		{url: monorepoURL, path: "teams/development/plugins/dev-openspec", names: []string{"openspec-propose"}},
	}

	t.Run("pinned path", func(t *testing.T) {
		col := goToolsCollection(ir.CollectionMember{
			Name:     "efficient-go",
			Kind:     ir.KindSkill,
			Registry: monorepoURL,
			Path:     "teams/development/plugins/dev-language-guides",
		})
		cfg, catalogs := buildMemberCase(t, col, fixtures)
		_, errs := Resolve(cfg, catalogs, nil)
		snf := findSNF(errs, "efficient-go")
		if snf == nil {
			t.Fatalf("expected a pinned-path miss, got %v", errs)
		}
		if !snf.Required {
			t.Fatalf("a required location in the repository must keep the miss hard: %v", snf)
		}
	})

	t.Run("path-less", func(t *testing.T) {
		col := goToolsCollection(ir.CollectionMember{Name: "efficient-go", Kind: ir.KindSkill, Registry: monorepoURL})
		cfg, catalogs := buildMemberCase(t, col, fixtures)
		_, errs := Resolve(cfg, catalogs, nil)
		snf := findSNF(errs, "efficient-go")
		if snf == nil {
			t.Fatalf("expected a path-less miss, got %v", errs)
		}
		if !snf.Required {
			t.Fatalf("a required location in the repository must keep the miss hard: %v", snf)
		}
	})

	t.Run("a member that binds is unaffected", func(t *testing.T) {
		col := goToolsCollection(ir.CollectionMember{Name: "openspec-propose", Kind: ir.KindSkill, Registry: monorepoURL})
		cfg, catalogs := buildMemberCase(t, col, fixtures)
		resolved, errs := Resolve(cfg, catalogs, nil)
		if findResolved(resolved, "openspec-propose") == nil {
			t.Fatalf("expected the member to bind to the non-required location that provides it, got %+v", resolved)
		}
		if snf := findSNF(errs, "openspec-propose"); snf != nil {
			t.Fatalf("escalation must not touch a member that binds: %v", snf)
		}
	})
}

// TestPinnedMemberPathNormalizes covers collection-authored path spellings that
// differ only by a "./" prefix, a trailing slash, or OS path separators. The
// configured entry path is canonical, so the comparison must normalize both
// sides before deciding the member is unresolvable.
func TestPinnedMemberPathNormalizes(t *testing.T) {
	fixture := []memberFixture{
		{url: monorepoURL, path: "teams/development/plugins/dev-language-guides", names: []string{"efficient-go"}},
	}
	cases := []struct {
		name string
		path string
	}{
		{"trailing slash", "teams/development/plugins/dev-language-guides/"},
		{"leading dot slash", "./teams/development/plugins/dev-language-guides"},
		{"backslash separators", "teams\\development\\plugins\\dev-language-guides"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			col := goToolsCollection(ir.CollectionMember{
				Name:     "efficient-go",
				Kind:     ir.KindSkill,
				Registry: monorepoURL,
				Path:     tc.path,
			})
			cfg, catalogs := buildMemberCase(t, col, fixture)
			resolved, errs := Resolve(cfg, catalogs, nil)
			for _, err := range errs {
				if IsSelectedNotFound(err) {
					t.Fatalf("normalized path %q must bind: %v", tc.path, err)
				}
			}
			if findResolved(resolved, "efficient-go") == nil {
				t.Fatalf("normalized path %q did not bind: %+v", tc.path, resolved)
			}
		})
	}
}

// TestExplicitRootPathPinHonorsRootEntry protects the explicit root pin.
// A collection member whose path is "." or "./" must select the repository
// root entry, not fall back to path-less containment matching that could bind
// to a non-root entry or report an ambiguity.
func TestExplicitRootPathPinHonorsRootEntry(t *testing.T) {
	fixture := []memberFixture{
		{url: monorepoURL, path: "", names: []string{"efficient-go"}},
		{url: monorepoURL, path: "teams/development/plugins/dev-language-guides", names: []string{"efficient-go"}},
	}

	for _, path := range []string{".", "./"} {
		t.Run("path "+path, func(t *testing.T) {
			col := goToolsCollection(ir.CollectionMember{
				Name:     "efficient-go",
				Kind:     ir.KindSkill,
				Registry: monorepoURL,
				Path:     path,
			})
			cfg, catalogs := buildMemberCase(t, col, fixture)
			resolved, errs := Resolve(cfg, catalogs, nil)
			for _, err := range errs {
				if IsSelectedNotFound(err) || IsCollectionMemberAmbiguity(err) {
					t.Fatalf("explicit root path %q must bind to root unambiguously: %v", path, err)
				}
			}
			got := findResolved(resolved, "efficient-go")
			if got == nil {
				t.Fatalf("explicit root path %q did not resolve: %+v", path, resolved)
			}
			if got.RegistryPath != "" {
				t.Fatalf("explicit root path %q must bind to root, got %q", path, got.RegistryPath)
			}
		})
	}
}
