package resolver

import (
	"errors"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// Regression coverage for agent-marketplace-qbfm: a collection member that
// names a `registry:` without a `path:` must bind to whichever configured entry
// for that repository actually contains it, and must never blame an arbitrary
// path when nothing contains it.

const (
	monorepoURL     = "git@gitlab.example.com:org/shared-ai"
	monorepoDotGit  = "git@gitlab.example.com:org/shared-ai.git"
	collectionsHost = "git@example.com:agent-marketplace.git"
)

// memberFixture describes one configured registry entry for the member's
// repository together with the skill names its catalog exposes.
type memberFixture struct {
	url      string
	path     string
	ref      string
	required bool
	names    []string
}

// buildMemberCase wires a collection (hosted in its own registry) plus a set of
// configured entries for the member's repository, and returns the merged config
// and catalogs that Resolve expects.
func buildMemberCase(t *testing.T, col *ir.Collection, fixtures []memberFixture) (*config.Config, map[string][]catalog.Entry) {
	t.Helper()

	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:         collectionsHost,
			Ref:         "main",
			Path:        "skills",
			Collections: ks(col.Name()),
		}},
	}
	for _, f := range fixtures {
		ref := f.ref
		if ref == "" {
			ref = "main"
		}
		cfg.Registries = append(cfg.Registries, config.RegistryEntry{
			URL:      f.url,
			Ref:      ref,
			Path:     f.path,
			Required: f.required,
		})
	}
	setCatalogKeys(cfg)

	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {{
			Kind:     ir.KindCollection,
			Name:     col.Name(),
			Resource: col,
			Path:     "/cache/host/collections/" + col.Name(),
		}},
	}
	for i, f := range fixtures {
		key := cfg.Registries[i+1].CatalogKey
		for _, name := range f.names {
			catalogs[key] = append(catalogs[key], catalog.Entry{
				Kind: ir.KindSkill,
				Name: name,
				Path: "/cache/" + f.path + "/" + name,
			})
		}
		if _, ok := catalogs[key]; !ok {
			catalogs[key] = []catalog.Entry{}
		}
	}
	return cfg, catalogs
}

func goToolsCollection(members ...ir.CollectionMember) *ir.Collection {
	return ir.NewCollection("agent-marketplace-tools", "", "", members, nil, nil, ir.SourceRef{})
}

func findResolved(resolved []ResolvedArtifact, name string) *ResolvedArtifact {
	for i := range resolved {
		if resolved[i].Name == name {
			return &resolved[i]
		}
	}
	return nil
}

// TestPathLessMemberBindsToContainingEntry pins the core rule: with several
// entries for the same repository at different paths, the member binds to the
// one whose catalog contains it, not to the first one in config order.
func TestPathLessMemberBindsToContainingEntry(t *testing.T) {
	col := goToolsCollection(ir.CollectionMember{Name: "efficient-go", Kind: ir.KindSkill, Registry: monorepoURL})

	cfg, catalogs := buildMemberCase(t, col, []memberFixture{
		{url: monorepoURL, path: "teams/development/plugins/dev-brainstorming", names: []string{"brainstorming"}},
		{url: monorepoURL, path: "teams/development/plugins/dev-openspec", names: []string{"openspec-propose"}},
		{url: monorepoURL, path: "teams/development/plugins/dev-language-guides", names: []string{"efficient-go", "writing-golang-code"}},
	})

	resolved, errs := Resolve(cfg, catalogs, nil)
	for _, err := range errs {
		if IsSelectedNotFound(err) {
			t.Fatalf("efficient-go exists at dev-language-guides; expected no selected-not-found error, got: %v", err)
		}
	}

	got := findResolved(resolved, "efficient-go")
	if got == nil {
		t.Fatalf("expected efficient-go to resolve from the entry that contains it, resolved: %+v", resolved)
	}
	if got.RegistryPath != "teams/development/plugins/dev-language-guides" {
		t.Fatalf("expected efficient-go to bind to the containing entry path, got %q", got.RegistryPath)
	}
	if got.Via != "collection:agent-marketplace-tools" {
		t.Fatalf("expected collection provenance, got %q", got.Via)
	}
}

// TestPathLessMemberIgnoresGitSuffixDifference proves the containment search is
// not defeated by a ".git" spelling difference between the collection member
// and the configured entry.
func TestPathLessMemberIgnoresGitSuffixDifference(t *testing.T) {
	col := goToolsCollection(ir.CollectionMember{Name: "writing-golang-code", Kind: ir.KindSkill, Registry: monorepoURL})

	cfg, catalogs := buildMemberCase(t, col, []memberFixture{
		{url: monorepoURL, path: "teams/development/plugins/dev-brainstorming", names: []string{"brainstorming"}},
		{url: monorepoDotGit, path: "teams/development/plugins/dev-language-guides", names: []string{"writing-golang-code"}},
	})

	resolved, errs := Resolve(cfg, catalogs, nil)
	for _, err := range errs {
		if IsSelectedNotFound(err) {
			t.Fatalf("writing-golang-code exists in the .git-spelled entry; got: %v", err)
		}
	}
	got := findResolved(resolved, "writing-golang-code")
	if got == nil {
		t.Fatalf("expected writing-golang-code to resolve across the .git suffix difference, resolved: %+v", resolved)
	}
	if got.RegistryPath != "teams/development/plugins/dev-language-guides" {
		t.Fatalf("expected the containing entry path, got %q", got.RegistryPath)
	}
}

// TestPathLessMemberAbsentNamesSearchedPaths covers the genuinely-absent case:
// the warning must enumerate the candidate paths that were searched instead of
// asserting one arbitrary path, and must not blame an upstream removal.
func TestPathLessMemberAbsentNamesSearchedPaths(t *testing.T) {
	col := goToolsCollection(ir.CollectionMember{Name: "efficient-go", Kind: ir.KindSkill, Registry: monorepoURL})

	cfg, catalogs := buildMemberCase(t, col, []memberFixture{
		{url: monorepoDotGit, path: "teams/development/plugins/dev-brainstorming", names: []string{"brainstorming"}},
		{url: monorepoURL, path: "teams/development/plugins/dev-openspec", names: []string{"openspec-propose"}},
	})

	_, errs := Resolve(cfg, catalogs, nil)

	var snf *SelectedNotFoundError
	for _, err := range errs {
		var candidate *SelectedNotFoundError
		if errors.As(err, &candidate) && candidate.Name == "efficient-go" {
			snf = candidate
			break
		}
	}
	if snf == nil {
		t.Fatalf("expected a selected-not-found error for the absent member, got: %v", errs)
	}

	if len(snf.CandidatePaths) != 2 {
		t.Fatalf("expected both searched paths recorded, got %#v", snf.CandidatePaths)
	}
	msg := snf.Error()
	for _, want := range []string{
		"teams/development/plugins/dev-brainstorming",
		"teams/development/plugins/dev-openspec",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected message to name searched path %q, got: %s", want, msg)
		}
	}
	if snf.Path != "" {
		t.Fatalf("a path-less member must not be attributed to a single path, got Path=%q", snf.Path)
	}
	if strings.Contains(msg, monorepoDotGit) {
		t.Fatalf("expected the member's declared registry URL, not an entry's .git spelling, got: %s", msg)
	}

	rem := snf.Remediation()
	if strings.Contains(rem, "removed or renamed upstream") {
		t.Fatalf("remediation must not claim upstream removal for a path-less member, got: %s", rem)
	}
	if !strings.Contains(rem, "path") {
		t.Fatalf("remediation should point at pinning a path, got: %s", rem)
	}
	if snf.Required {
		t.Fatal("no candidate entry is required; the missing member must stay a soft warning")
	}
}

// TestPathLessMemberAmbiguityIsReported covers the >1 case: two distinct
// locations for the same repository both contain the member, which must be
// reported instead of silently picking one.
func TestPathLessMemberAmbiguityIsReported(t *testing.T) {
	col := goToolsCollection(ir.CollectionMember{Name: "efficient-go", Kind: ir.KindSkill, Registry: monorepoURL})

	cfg, catalogs := buildMemberCase(t, col, []memberFixture{
		{url: monorepoURL, path: "teams/development/plugins/dev-language-guides", names: []string{"efficient-go"}},
		{url: monorepoDotGit, path: "teams/common/plugins/go-guides", names: []string{"efficient-go"}},
	})

	resolved, errs := Resolve(cfg, catalogs, nil)

	var ambiguity *AmbiguousCollectionMemberWarning
	for _, err := range errs {
		if errors.As(err, &ambiguity) {
			break
		}
	}
	if ambiguity == nil {
		t.Fatalf("expected an explicit ambiguity report, got: %v", errs)
	}
	if len(ambiguity.Candidates) != 2 {
		t.Fatalf("expected both containing paths reported, got %#v", ambiguity.Candidates)
	}
	if !IsCollectionMemberAmbiguity(ambiguity) || !IsWarning(ambiguity) {
		t.Fatal("ambiguity must be classified as a soft warning, not a hard failure")
	}
	msg := ambiguity.Error()
	for _, want := range []string{"teams/development/plugins/dev-language-guides", "teams/common/plugins/go-guides"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected message to name %q, got: %s", want, msg)
		}
	}

	// The member still materializes exactly once, deterministically.
	count := 0
	for _, artifact := range resolved {
		if artifact.Name == "efficient-go" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected efficient-go to resolve exactly once, got %d", count)
	}
}

// TestPathLessMemberSameLocationTwiceIsNotAmbiguous guards against a false
// ambiguity report when the same location is configured twice, once with a
// ".git" suffix and once without.
func TestPathLessMemberSameLocationTwiceIsNotAmbiguous(t *testing.T) {
	col := goToolsCollection(ir.CollectionMember{Name: "efficient-go", Kind: ir.KindSkill, Registry: monorepoURL})

	cfg, catalogs := buildMemberCase(t, col, []memberFixture{
		{url: monorepoURL, path: "teams/development/plugins/dev-language-guides", names: []string{"efficient-go"}},
		{url: monorepoDotGit, path: "teams/development/plugins/dev-language-guides", names: []string{"efficient-go"}},
	})

	_, errs := Resolve(cfg, catalogs, nil)
	for _, err := range errs {
		if IsCollectionMemberAmbiguity(err) {
			t.Fatalf("two spellings of the same location are one location, not an ambiguity: %v", err)
		}
	}
}

// TestPathLessMemberAmbiguityAcrossRefsNamesRefs covers two configured
// locations that share a subdirectory and differ only by ref: they are two
// locations, so the reported candidate list must name two of them rather than
// collapsing to one path label and claiming "1 configured locations".
func TestPathLessMemberAmbiguityAcrossRefsNamesRefs(t *testing.T) {
	col := goToolsCollection(ir.CollectionMember{Name: "efficient-go", Kind: ir.KindSkill, Registry: monorepoURL})

	cfg, catalogs := buildMemberCase(t, col, []memberFixture{
		{url: monorepoURL, path: "teams/development/plugins/dev-language-guides", ref: "main", names: []string{"efficient-go"}},
		{url: monorepoURL, path: "teams/development/plugins/dev-language-guides", ref: "next", names: []string{"efficient-go"}},
	})

	_, errs := Resolve(cfg, catalogs, nil)

	var ambiguity *AmbiguousCollectionMemberWarning
	for _, err := range errs {
		if errors.As(err, &ambiguity) {
			break
		}
	}
	if ambiguity == nil {
		t.Fatalf("two refs of the same subdirectory are two locations; expected an ambiguity report, got: %v", errs)
	}
	if len(ambiguity.Candidates) != 2 {
		t.Fatalf("expected one label per location, got %#v", ambiguity.Candidates)
	}
	msg := ambiguity.Error()
	for _, want := range []string{"dev-language-guides@main", "dev-language-guides@next", "2 configured locations"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected message to contain %q, got: %s", want, msg)
		}
	}
	if !strings.Contains(msg, "so "+ambiguity.Chosen+" was used") {
		t.Fatalf("the bound location must be named in the same spelling as the candidates, got: %s", msg)
	}
}

// TestExplicitPathMemberBindsToThatPath keeps the explicit-path contract: a
// pinned path selects exactly that entry even when another entry also contains
// the member.
func TestExplicitPathMemberBindsToThatPath(t *testing.T) {
	col := goToolsCollection(ir.CollectionMember{
		Name:     "efficient-go",
		Kind:     ir.KindSkill,
		Registry: monorepoURL,
		Path:     "teams/development/plugins/dev-language-guides",
	})

	cfg, catalogs := buildMemberCase(t, col, []memberFixture{
		{url: monorepoDotGit, path: "teams/common/plugins/go-guides", names: []string{"efficient-go"}},
		{url: monorepoURL, path: "teams/development/plugins/dev-language-guides", names: []string{"efficient-go"}},
	})

	resolved, errs := Resolve(cfg, catalogs, nil)
	for _, err := range errs {
		if IsSelectedNotFound(err) || IsCollectionMemberAmbiguity(err) {
			t.Fatalf("an explicit path is unambiguous, got: %v", err)
		}
	}
	got := findResolved(resolved, "efficient-go")
	if got == nil || got.RegistryPath != "teams/development/plugins/dev-language-guides" {
		t.Fatalf("expected the pinned path to win, got %+v", got)
	}
}

// TestExplicitPathMemberUnknownPathNamesConfiguredPaths keeps a pinned path
// diagnosable: when no entry uses it, the error names the paths that are
// configured rather than claiming the repository is absent entirely.
func TestExplicitPathMemberUnknownPathNamesConfiguredPaths(t *testing.T) {
	col := goToolsCollection(ir.CollectionMember{
		Name:     "efficient-go",
		Kind:     ir.KindSkill,
		Registry: monorepoURL,
		Path:     "teams/development/plugins/dev-language-guides",
	})

	cfg, catalogs := buildMemberCase(t, col, []memberFixture{
		{url: monorepoURL, path: "teams/development/plugins/dev-openspec", names: []string{"openspec-propose"}},
	})

	_, errs := Resolve(cfg, catalogs, nil)
	var found bool
	for _, err := range errs {
		msg := err.Error()
		if strings.Contains(msg, "efficient-go") && strings.Contains(msg, "teams/development/plugins/dev-openspec") {
			found = true
			if strings.Contains(msg, "not present in any config layer") {
				t.Fatalf("the repository IS configured, just not at that path: %s", msg)
			}
		}
	}
	if !found {
		t.Fatalf("expected an error naming the configured paths, got: %v", errs)
	}
}

// TestExplicitPathMemberUnknownPathIsSoft pins the severity of a pinned-path
// miss: the pin lives in a collection the consumer usually cannot edit, so an
// unconfigured path must degrade to a soft warning (sync still exits 0) while
// still naming the exact path the collection asked for.
func TestExplicitPathMemberUnknownPathIsSoft(t *testing.T) {
	col := goToolsCollection(ir.CollectionMember{
		Name:     "efficient-go",
		Kind:     ir.KindSkill,
		Registry: monorepoURL,
		Path:     "teams/development/plugins/dev-language-guides",
	})

	cfg, catalogs := buildMemberCase(t, col, []memberFixture{
		{url: monorepoURL, path: "teams/common/plugins/go-guides", names: []string{"efficient-go"}},
	})

	_, errs := Resolve(cfg, catalogs, nil)

	var snf *SelectedNotFoundError
	for _, err := range errs {
		var candidate *SelectedNotFoundError
		if errors.As(err, &candidate) && candidate.Name == "efficient-go" {
			snf = candidate
			break
		}
	}
	if snf == nil {
		t.Fatalf("expected a selected-not-found error for the pinned-path miss, got: %v", errs)
	}
	if !IsSoftSelectedNotFound(snf) {
		t.Fatalf("a pinned-path miss against non-required entries must be soft, got hard: %v", snf)
	}
	if snf.Required {
		t.Fatal("no candidate entry is required; the pinned-path miss must stay soft")
	}
	if snf.RequestedPath != "teams/development/plugins/dev-language-guides" {
		t.Fatalf("expected the requested path recorded, got %q", snf.RequestedPath)
	}

	msg := snf.Error()
	for _, want := range []string{
		"teams/development/plugins/dev-language-guides", // what the collection asked for
		"teams/common/plugins/go-guides",                // what is actually configured
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected message to name %q, got: %s", want, msg)
		}
	}
	if strings.Contains(msg, "under any configured path") {
		t.Fatalf("a pinned-path miss must not be reported with the path-less wording: %s", msg)
	}

	rem := snf.Remediation()
	if !strings.Contains(rem, "teams/development/plugins/dev-language-guides") {
		t.Fatalf("remediation must name the requested path, got: %s", rem)
	}
	if strings.Contains(rem, "removed or renamed upstream") {
		t.Fatalf("remediation must not claim upstream removal for a pinned-path miss, got: %s", rem)
	}

	// Every error the run produced must be soft, so the sync exits 0.
	for _, err := range errs {
		if !IsWarning(err) && !IsSoftSelectedNotFound(err) {
			t.Fatalf("expected only soft diagnostics so sync can exit 0, got hard error: %v", err)
		}
	}
}

// TestExplicitPathMemberUnknownPathRequiredStaysHard is the mirror case: the
// downgrade must not become a way around `required: true` org policy.
func TestExplicitPathMemberUnknownPathRequiredStaysHard(t *testing.T) {
	col := goToolsCollection(ir.CollectionMember{
		Name:     "efficient-go",
		Kind:     ir.KindSkill,
		Registry: monorepoURL,
		Path:     "teams/development/plugins/dev-language-guides",
	})

	cfg, catalogs := buildMemberCase(t, col, []memberFixture{
		{url: monorepoURL, path: "teams/security/plugins/devsecops", required: true, names: []string{"devsecops"}},
	})

	_, errs := Resolve(cfg, catalogs, nil)

	var snf *SelectedNotFoundError
	for _, err := range errs {
		var candidate *SelectedNotFoundError
		if errors.As(err, &candidate) && candidate.Name == "efficient-go" {
			snf = candidate
			break
		}
	}
	if snf == nil {
		t.Fatalf("expected a selected-not-found error for the pinned-path miss, got: %v", errs)
	}
	if !snf.Required {
		t.Fatal("every configured entry is required; the pinned-path miss must stay hard")
	}
	if IsSoftSelectedNotFound(snf) {
		t.Fatal("a required registry must never be downgraded to a warning by a pinned path")
	}
	if !strings.Contains(snf.Error(), "teams/development/plugins/dev-language-guides") {
		t.Fatalf("expected the requested path named even in the required case, got: %s", snf.Error())
	}
}

// TestPathLessMemberUnknownRepositoryStillErrors keeps the pre-existing
// behavior for a repository that appears in no config layer at all.
func TestPathLessMemberUnknownRepositoryStillErrors(t *testing.T) {
	col := goToolsCollection(ir.CollectionMember{Name: "efficient-go", Kind: ir.KindSkill, Registry: "git@example.com:absent.git"})

	cfg, catalogs := buildMemberCase(t, col, []memberFixture{
		{url: monorepoURL, path: "teams/development/plugins/dev-openspec", names: []string{"openspec-propose"}},
	})

	_, errs := Resolve(cfg, catalogs, nil)
	var found bool
	for _, err := range errs {
		if strings.Contains(err.Error(), "not present in any config layer") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the unknown-registry error, got: %v", errs)
	}
}

// TestPathLessMemberRequiredWhenAnyCandidateIsRequired documents the policy
// call for an unbindable member: it is a hard error as soon as ANY configured
// location for the repository is org-mandated. The previous rule demanded
// every location be required, which a personal, gitignored config layer could
// defeat by adding one non-required entry for the same repository
// (agent-marketplace-0236).
func TestPathLessMemberRequiredWhenAnyCandidateIsRequired(t *testing.T) {
	member := ir.CollectionMember{Name: "efficient-go", Kind: ir.KindSkill, Registry: monorepoURL}

	tests := []struct {
		name         string
		fixtures     []memberFixture
		wantMiss     bool
		wantRequired bool
	}{
		{
			name: "mixed candidates stay hard",
			fixtures: []memberFixture{
				{url: monorepoURL, path: "teams/security/plugins/devsecops", required: true, names: []string{"devsecops"}},
				{url: monorepoURL, path: "teams/development/plugins/dev-openspec", names: []string{"openspec-propose"}},
			},
			wantMiss: true, wantRequired: true,
		},
		{
			name: "no required candidate stays soft",
			fixtures: []memberFixture{
				{url: monorepoURL, path: "teams/development/plugins/dev-openspec", names: []string{"openspec-propose"}},
				{url: monorepoURL, path: "teams/development/plugins/dev-brainstorming", names: []string{"brainstorming"}},
			},
			wantMiss: true, wantRequired: false,
		},
		{
			name: "all required candidates stay hard",
			fixtures: []memberFixture{
				{url: monorepoURL, path: "teams/security/plugins/devsecops", required: true, names: []string{"devsecops"}},
			},
			wantMiss: true, wantRequired: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, catalogs := buildMemberCase(t, goToolsCollection(member), tc.fixtures)
			_, errs := Resolve(cfg, catalogs, nil)
			snf := findSNF(errs, "efficient-go")
			if (snf != nil) != tc.wantMiss {
				t.Fatalf("miss=%v, want %v (errs=%v)", snf != nil, tc.wantMiss, errs)
			}
			if snf != nil && snf.Required != tc.wantRequired {
				t.Fatalf("Required=%v, want %v", snf.Required, tc.wantRequired)
			}
		})
	}
}
