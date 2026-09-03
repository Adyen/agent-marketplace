package resolver

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/skill"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
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

func TestNestedCollectionExclusionDoesNotBypassCollectionPolicy(t *testing.T) {
	outer := ir.NewCollection("outer", "1.0.0", "outer", nil, []string{"inner"}, nil, ir.SourceRef{})
	inner := ir.NewCollection("inner", "1.0.0", "inner", []ir.CollectionMember{{Name: "nested-skill", Kind: ir.KindSkill}}, nil, nil, ir.SourceRef{})

	cfg := &config.Config{Registries: []config.RegistryEntry{
		{URL: collectionsHost, Ref: "main", Collections: ks("outer")},
		{URL: monorepoURL, Ref: "main", Path: "bundles/inner", Collections: config.KindSelector{ExcludeAll: true}},
	}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {{Kind: ir.KindCollection, Name: "outer", Resource: outer}},
		cfg.Registries[1].CatalogKey: {
			{Kind: ir.KindCollection, Name: "inner", Resource: inner},
			{Kind: ir.KindSkill, Name: "nested-skill"},
		},
	}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if got := findResolved(resolved, "nested-skill"); got != nil {
		t.Fatalf("excluded nested collection bypassed its collection exclusion: %#v", got)
	}
	for _, err := range errs {
		if IsSelectedNotFound(err) {
			t.Fatalf("collection exclusion should be a no-op, not a missing-resource error: %v", err)
		}
	}
}

func TestRequiredNestedCollectionIgnoresCollectionExclusion(t *testing.T) {
	outer := ir.NewCollection("outer", "1.0.0", "outer", nil, []string{"inner"}, nil, ir.SourceRef{})
	inner := ir.NewCollection("inner", "1.0.0", "inner", []ir.CollectionMember{{Name: "nested-skill", Kind: ir.KindSkill}}, nil, nil, ir.SourceRef{})

	cfg := &config.Config{Registries: []config.RegistryEntry{
		{URL: collectionsHost, Ref: "main", Collections: ks("outer")},
		{URL: monorepoURL, Ref: "main", Path: "bundles/inner", Required: true, Skills: ks("required-direct"), Collections: config.KindSelector{ExcludeAll: true}},
	}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {{Kind: ir.KindCollection, Name: "outer", Resource: outer}},
		cfg.Registries[1].CatalogKey: {
			{Kind: ir.KindCollection, Name: "inner", Resource: inner},
			{Kind: ir.KindSkill, Name: "nested-skill"},
			{Kind: ir.KindSkill, Name: "required-direct"},
		},
	}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if got := findResolved(resolved, "nested-skill"); got == nil || !got.Required {
		t.Fatalf("required nested collection must survive collection exclusion, got %#v (errs=%v)", got, errs)
	}
}

func TestLocalExcludesDoNotSuppressMachineScopedCollectionMembers(t *testing.T) {
	for _, source := range []config.Source{config.Organization, config.UserPersonal} {
		t.Run(source.String(), func(t *testing.T) {
			const machineURL = "git@example.com:machine.git"
			nested := ir.NewCollection("nested", "1.0.0", "nested", []ir.CollectionMember{{Name: "machine-nested", Kind: ir.KindSkill}}, nil, nil, ir.SourceRef{})
			root := ir.NewCollection("root", "1.0.0", "root", []ir.CollectionMember{{Name: "machine-direct", Kind: ir.KindSkill, Registry: machineURL}}, []string{"nested"}, nil, ir.SourceRef{})

			cfg := &config.Config{Registries: []config.RegistryEntry{
				{URL: collectionsHost, Ref: "main", Source: config.RepoCommitted, Collections: ks("root")},
				{URL: machineURL, Ref: "main", Source: source},
			}}
			setCatalogKeys(cfg)
			catalogs := map[string][]catalog.Entry{
				cfg.Registries[0].CatalogKey: {{Kind: ir.KindCollection, Name: "root", Resource: root}},
				cfg.Registries[1].CatalogKey: {
					{Kind: ir.KindCollection, Name: "nested", Resource: nested},
					{Kind: ir.KindSkill, Name: "machine-direct"},
					{Kind: ir.KindSkill, Name: "machine-nested"},
				},
			}

			resolved, errs := Resolve(cfg, catalogs, &config.LocalConfig{
				Skills:      kse(nil, []string{"machine-direct", "machine-nested"}),
				Collections: kse(nil, []string{"nested"}),
			})
			if len(errs) != 0 {
				t.Fatalf("expected no errors, got %v", errs)
			}
			for _, name := range []string{"machine-direct", "machine-nested"} {
				member := findResolved(resolved, name)
				if member == nil {
					t.Fatalf("local excludes suppressed machine-scope collection member %q: %#v", name, resolved)
				}
				if member.Source != source {
					t.Fatalf("member %q source = %v, want %v", name, member.Source, source)
				}
			}
		})
	}
}

func TestEquivalentRegistryPathsDeduplicateAsOnePhysicalSource(t *testing.T) {
	const registryURL = "git@example.com:shared.git"
	cfg := &config.Config{Registries: []config.RegistryEntry{
		{URL: registryURL, Ref: "main", Path: "foo", Skills: ks("shared")},
		{URL: registryURL, Ref: "main", Path: "./foo/", Skills: ks("shared")},
	}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {{Kind: ir.KindSkill, Name: "shared"}},
	}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(resolved) != 1 {
		t.Fatalf("equivalent registry paths should produce one artifact, got %#v", resolved)
	}
	if resolved[0].RegistryPath != "foo" {
		t.Fatalf("resolved path identity = %q, want canonical foo", resolved[0].RegistryPath)
	}
	for _, err := range errs {
		if IsNameCollision(err) {
			t.Fatalf("equivalent registry paths must not produce a collision warning: %v", err)
		}
	}
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

func TestExplicitPathMemberWithoutRefReportsRefAmbiguity(t *testing.T) {
	col := goToolsCollection(ir.CollectionMember{
		Name:     "efficient-go",
		Kind:     ir.KindSkill,
		Registry: monorepoURL,
		Path:     "teams/development/plugins/dev-language-guides",
	})
	cfg, catalogs := buildMemberCase(t, col, []memberFixture{
		{url: monorepoURL, path: "teams/development/plugins/dev-language-guides", ref: "main", names: []string{"efficient-go"}},
		{url: monorepoURL, path: "teams/development/plugins/dev-language-guides", ref: "next", names: []string{"efficient-go"}},
	})

	resolved, errs := Resolve(cfg, catalogs, nil)
	var warning *AmbiguousCollectionMemberWarning
	for _, err := range errs {
		if errors.As(err, &warning) {
			break
		}
	}
	if warning == nil {
		t.Fatalf("expected ambiguity warning for an unqualified ref, got: %v", errs)
	}
	if warning.Path != "teams/development/plugins/dev-language-guides" {
		t.Fatalf("warning path = %q, want explicit member path", warning.Path)
	}
	for _, want := range []string{"teams/development/plugins/dev-language-guides@main", "teams/development/plugins/dev-language-guides@next"} {
		if !slices.Contains(warning.Candidates, want) {
			t.Fatalf("candidates = %#v, want %q", warning.Candidates, want)
		}
	}
	if !strings.Contains(warning.Error(), "without a ref") {
		t.Fatalf("warning should identify missing ref: %s", warning)
	}
	if got := findResolved(resolved, "efficient-go"); got == nil {
		t.Fatal("ambiguous member was not deterministically resolved")
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

func TestNestedCollectionDefinitionUsesConfiguredOwnerAndAuthority(t *testing.T) {
	tests := []struct {
		name         string
		memberName   string
		ownerA       config.Source
		ownerB       config.Source
		wantURL      string
		wantPath     string
		wantRef      string
		wantSource   config.Source
		wantRequired bool
	}{
		{
			name:         "parent owner wins equal authority",
			memberName:   "member-a",
			wantURL:      "git@example.com:collections-a",
			wantPath:     "bundles/a",
			wantRef:      "main",
			wantRequired: false,
		},
		{
			name:         "higher config authority wins",
			memberName:   "member-b",
			ownerA:       config.UserPersonal,
			ownerB:       config.RepoCommitted,
			wantURL:      "git@example.com:collections-b",
			wantPath:     "bundles/b",
			wantRef:      "next",
			wantSource:   config.RepoCommitted,
			wantRequired: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			innerA := ir.NewCollection("inner", "1.0.0", "A", []ir.CollectionMember{{Name: tc.memberName, Kind: ir.KindSkill}}, nil, nil, ir.SourceRef{})
			innerB := ir.NewCollection("inner", "2.0.0", "B", []ir.CollectionMember{{Name: tc.memberName, Kind: ir.KindSkill}}, nil, nil, ir.SourceRef{})
			outer := ir.NewCollection("outer", "1.0.0", "Outer", nil, []string{"inner"}, nil, ir.SourceRef{})
			cfg := &config.Config{Registries: []config.RegistryEntry{
				{URL: "git@example.com:collections-a", Ref: "main", Path: "bundles/a", Source: tc.ownerA, Collections: ks("outer")},
				{URL: "git@example.com:collections-b", Ref: "next", Path: "bundles/b", Source: tc.ownerB},
			}}
			setCatalogKeys(cfg)
			catalogs := map[string][]catalog.Entry{
				cfg.Registries[0].CatalogKey: {
					{Kind: ir.KindCollection, Name: "outer", Resource: outer},
					{Kind: ir.KindCollection, Name: "inner", Resource: innerA},
					{Kind: ir.KindSkill, Name: tc.memberName},
				},
				cfg.Registries[1].CatalogKey: {
					{Kind: ir.KindCollection, Name: "inner", Resource: innerB},
					{Kind: ir.KindSkill, Name: tc.memberName},
				},
			}

			resolved, errs := Resolve(cfg, catalogs, nil)
			if len(errs) != 0 {
				t.Fatalf("Resolve errors = %v", errs)
			}
			got := findResolved(resolved, tc.memberName)
			if got == nil {
				t.Fatalf("member %q was not expanded: %#v", tc.memberName, resolved)
			}
			if got.RegistryURL != tc.wantURL || got.RegistryPath != tc.wantPath || got.Ref != tc.wantRef || got.Source != tc.wantSource {
				t.Fatalf("member metadata = %s/%s@%s source=%s, want %s/%s@%s source=%s", got.RegistryURL, got.RegistryPath, got.Ref, got.Source, tc.wantURL, tc.wantPath, tc.wantRef, tc.wantSource)
			}
			if got.Required != tc.wantRequired {
				t.Fatalf("member Required = %v, want %v", got.Required, tc.wantRequired)
			}
			if got.Via == "" {
				t.Fatalf("member %q was directly resolved instead of expanded from the selected nested collection", tc.memberName)
			}
		})
	}
}

// TestChooseNestedCollectionRequiredCandidateWins isolates required-policy
// precedence from Resolve's direct-selection behavior. A required duplicate
// definition must win even when the non-required definition appears first.
func TestChooseNestedCollectionRequiredCandidateWins(t *testing.T) {
	nonRequiredDefinition := ir.NewCollection("duplicate", "1.0.0", "non-required", nil, nil, nil, ir.SourceRef{})
	requiredDefinition := ir.NewCollection("duplicate", "2.0.0", "required", nil, nil, nil, ir.SourceRef{})

	candidates := []collectionCandidate{
		{
			entry:      catalog.Entry{Kind: ir.KindCollection, Name: "duplicate", Resource: nonRequiredDefinition},
			catalogKey: "catalog-a",
			owner: config.RegistryEntry{
				URL:  "git@example.com:collections-a",
				Ref:  "main",
				Path: "bundles/a",
			},
			ownerFound: true,
		},
		{
			entry:      catalog.Entry{Kind: ir.KindCollection, Name: "duplicate", Resource: requiredDefinition},
			catalogKey: "catalog-b",
			owner: config.RegistryEntry{
				URL:      "git@example.com:collections-b",
				Ref:      "next",
				Path:     "bundles/b",
				Required: true,
			},
			ownerFound: true,
		},
	}

	chosen, ok := chooseNestedCollection(candidates, ResolvedArtifact{RegistryURL: "git@example.com:collections-a", Ref: "main", RegistryPath: "bundles/a"})
	if !ok {
		t.Fatal("chooseNestedCollection() found no duplicate definition")
	}
	if chosen.entry.Resource != requiredDefinition {
		t.Fatalf("chosen definition = %v, want required duplicate", chosen.entry.Resource)
	}
	if chosen.owner.URL != "git@example.com:collections-b" || chosen.owner.Path != "bundles/b" || chosen.owner.Ref != "next" {
		t.Fatalf("chosen owner = %s/%s@%s, want git@example.com:collections-b/bundles/b@next", chosen.owner.URL, chosen.owner.Path, chosen.owner.Ref)
	}
	if !chosen.owner.Required {
		t.Fatal("chosen duplicate owner is not marked required")
	}
}

func TestNestedCollectionExcludedWinnerFallsBackToEligibleDuplicate(t *testing.T) {
	outer := ir.NewCollection("outer", "1.0.0", "outer", nil, []string{"inner"}, nil, ir.SourceRef{})
	highInner := ir.NewCollection("inner", "2.0.0", "higher authority but excluded", []ir.CollectionMember{{Name: "fallback-skill", Kind: ir.KindSkill}}, nil, nil, ir.SourceRef{})
	lowInner := ir.NewCollection("inner", "1.0.0", "lower eligible", []ir.CollectionMember{{Name: "fallback-skill", Kind: ir.KindSkill}}, nil, nil, ir.SourceRef{})

	cfg := &config.Config{Registries: []config.RegistryEntry{
		{URL: collectionsHost, Ref: "main", Source: config.RepoCommitted, Collections: ks("outer")},
		{URL: "git@example.com:high", Ref: "main", Path: "bundles/high", Source: config.RepoPersonal, Collections: config.KindSelector{Exclude: []string{"inner"}}},
		{URL: "git@example.com:low", Ref: "main", Path: "bundles/low", Source: config.RepoCommitted},
	}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {{Kind: ir.KindCollection, Name: "outer", Resource: outer}},
		cfg.Registries[1].CatalogKey: {{Kind: ir.KindCollection, Name: "inner", Resource: highInner}},
		cfg.Registries[2].CatalogKey: {
			{Kind: ir.KindCollection, Name: "inner", Resource: lowInner},
			{Kind: ir.KindSkill, Name: "fallback-skill"},
		},
	}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("Resolve errors = %v", errs)
	}
	got := findResolved(resolved, "fallback-skill")
	if got == nil {
		t.Fatalf("lower eligible duplicate did not expand: %#v", resolved)
	}
	if got.RegistryURL != "git@example.com:low" || got.RegistryPath != "bundles/low" {
		t.Fatalf("expanded duplicate metadata = %s/%s, want lower eligible definition", got.RegistryURL, got.RegistryPath)
	}
}

func TestNestedCollectionHarnessesCannotWidenParent(t *testing.T) {
	outer := ir.NewCollection("outer", "1.0.0", "outer", nil, []string{"inner"}, nil, ir.SourceRef{})
	inner := ir.NewCollection("inner", "1.0.0", "inner", []ir.CollectionMember{{Name: "nested-skill", Kind: ir.KindSkill}}, nil, nil, ir.SourceRef{})
	cfg := &config.Config{Registries: []config.RegistryEntry{
		{URL: collectionsHost, Ref: "main", Collections: ks("outer"), Harnesses: []string{"claude", "factory"}},
		{URL: "git@example.com:nested.git", Ref: "main", Collections: ks(), Harnesses: []string{"factory", "gemini"}},
	}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {{Kind: ir.KindCollection, Name: "outer", Resource: outer}},
		cfg.Registries[1].CatalogKey: {
			{Kind: ir.KindCollection, Name: "inner", Resource: inner},
			{Kind: ir.KindSkill, Name: "nested-skill"},
		},
	}

	resolved, errs := Resolve(cfg, catalogs, nil, WithTargetHarnesses([]hcc.Target{hcc.TargetClaude, hcc.TargetFactory, hcc.TargetGemini}))
	if len(errs) != 0 {
		t.Fatalf("Resolve errors = %v", errs)
	}
	nested := findResolved(resolved, "nested-skill")
	if nested == nil {
		t.Fatalf("nested skill was not expanded: %#v", resolved)
	}
	want := []hcc.Target{hcc.TargetFactory}
	if !slices.Equal(nested.Harnesses, want) {
		t.Fatalf("nested harnesses = %v, want %v", nested.Harnesses, want)
	}
}

func TestNestedCollectionUsesWithResourcesFallback(t *testing.T) {
	outer := ir.NewCollection("outer", "1.0.0", "outer", nil, []string{"inner"}, nil, ir.SourceRef{})
	inner := ir.NewCollection("inner", "1.0.0", "inner", []ir.CollectionMember{{Name: "nested-skill", Kind: ir.KindSkill}}, nil, nil, ir.SourceRef{})
	cfg := &config.Config{Registries: []config.RegistryEntry{
		{URL: collectionsHost, Ref: "main", Collections: ks("outer")},
		{URL: "git@example.com:nested.git", Ref: "main"},
	}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {{Kind: ir.KindCollection, Name: "outer", Resource: outer}},
		cfg.Registries[1].CatalogKey: {
			{Kind: ir.KindCollection, Name: "inner"},
			{Kind: ir.KindSkill, Name: "nested-skill"},
		},
	}
	resources := map[string]map[string]ir.Resource{
		cfg.Registries[1].CatalogKey: {
			skill.ResourceKey(ir.KindCollection, "inner"): inner,
		},
	}

	resolved, errs := Resolve(cfg, catalogs, nil, WithResources(resources))
	if len(errs) != 0 {
		t.Fatalf("Resolve errors = %v", errs)
	}
	if nested := findResolved(resolved, "nested-skill"); nested == nil {
		t.Fatalf("nested skill was not expanded through resource fallback: %#v", resolved)
	}
}

func TestExplicitPathMemberFiltersLocalExcludesAndHarnessesBeforePreference(t *testing.T) {
	const memberURL = "git@example.com:member.git"
	for _, tc := range []struct {
		name      string
		localExcl bool
		userHarn  []string
		targets   []hcc.Target
	}{
		{name: "local exclusion", localExcl: true, userHarn: []string{"factory"}},
		{name: "incompatible repo owner", userHarn: []string{"factory"}, targets: []hcc.Target{hcc.TargetFactory}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			col := ir.NewCollection("root", "1.0.0", "root", []ir.CollectionMember{{Name: "bound", Kind: ir.KindSkill, Registry: memberURL, Path: "plugins"}}, nil, nil, ir.SourceRef{})
			cfg := &config.Config{Registries: []config.RegistryEntry{
				{URL: collectionsHost, Ref: "main", Collections: ks("root")},
				{URL: memberURL, Ref: "main", Path: "plugins", Source: config.RepoCommitted, FilePrecedence: 2, Harnesses: []string{"cursor"}},
				{URL: memberURL, Ref: "main", Path: "plugins", Source: config.UserPersonal, Harnesses: tc.userHarn},
			}}
			setCatalogKeys(cfg)
			catalogs := map[string][]catalog.Entry{
				cfg.Registries[0].CatalogKey: {{Kind: ir.KindCollection, Name: "root", Resource: col}},
				cfg.Registries[1].CatalogKey: {{Kind: ir.KindSkill, Name: "bound"}},
			}
			var local *config.LocalConfig
			if tc.localExcl {
				local = &config.LocalConfig{Skills: kse(nil, []string{"bound"})}
			}
			resolved, errs := Resolve(cfg, catalogs, local, WithTargetHarnesses(tc.targets))
			if len(errs) != 0 {
				t.Fatalf("Resolve errors = %v", errs)
			}
			got := findResolved(resolved, "bound")
			if got == nil || got.Source != config.UserPersonal {
				t.Fatalf("bound member = %#v, want compatible machine owner", got)
			}
		})
	}
}

func TestSharedNestedCollectionIsNotReportedAsCycle(t *testing.T) {
	root := ir.NewCollection("root", "1.0.0", "root", nil, []string{"left", "right"}, nil, ir.SourceRef{})
	left := ir.NewCollection("left", "1.0.0", "left", nil, []string{"shared"}, nil, ir.SourceRef{})
	right := ir.NewCollection("right", "1.0.0", "right", nil, []string{"shared"}, nil, ir.SourceRef{})
	shared := ir.NewCollection("shared", "1.0.0", "shared", []ir.CollectionMember{{Name: "shared-skill", Kind: ir.KindSkill}}, nil, nil, ir.SourceRef{})
	cfg := &config.Config{Registries: []config.RegistryEntry{{URL: collectionsHost, Ref: "main", Collections: ks("root")}}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {
		{Kind: ir.KindCollection, Name: "root", Resource: root},
		{Kind: ir.KindCollection, Name: "left", Resource: left},
		{Kind: ir.KindCollection, Name: "right", Resource: right},
		{Kind: ir.KindCollection, Name: "shared", Resource: shared},
		{Kind: ir.KindSkill, Name: "shared-skill"},
	}}

	resolved, errs := Resolve(cfg, catalogs, nil)
	for _, err := range errs {
		if strings.Contains(err.Error(), "cycle detected") {
			t.Fatalf("shared DAG node was reported as a cycle: %v", err)
		}
	}
	if nested := findResolved(resolved, "shared-skill"); nested == nil {
		t.Fatalf("shared nested skill was not expanded: %#v", resolved)
	}
}
