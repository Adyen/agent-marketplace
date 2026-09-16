package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	pkgcatalog "github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// ks is a shorthand for constructing KindSelector with includes only.
func ks(v ...string) config.KindSelector {
	if len(v) == 0 {
		return config.KindSelector{}
	}
	return config.KindSelector{Include: v}
}

func TestLoadCatalogsForTUIUsesExistingCacheWithoutRefresh(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "https://example.com/skills.git", Ref: "main", Path: "skills"},
			{URL: "https://example.com/skills.git", Ref: "main", Path: "skills"},
		},
	}

	cachePath := cache.RegistryCachePath(cfg.Registries[0].URL, cfg.Registries[0].Ref)
	if err := os.MkdirAll(filepath.Join(cachePath, "skills"), 0o755); err != nil {
		t.Fatalf("mkdir cache skills dir: %v", err)
	}

	cloneCalls := 0
	discoverCalls := 0

	restoreClone := swapCloneOrPull(func(url, ref string) (string, error) {
		cloneCalls++
		return cachePath, nil
	})
	defer restoreClone()

	restoreDiscover := swapDiscoverSkills(func(dir string) ([]pkgcatalog.Entry, []skill.MalformedSkill, error) {
		discoverCalls++
		return []pkgcatalog.Entry{{Name: "cached-skill"}}, nil, nil
	})
	defer restoreDiscover()

	catalogs, refreshAfterLoad, err := loadCatalogsForTUI(cfg, t.TempDir(), catalogLoadCached)
	if err != nil {
		t.Fatalf("load catalogs: %v", err)
	}

	if refreshAfterLoad != true {
		t.Fatalf("expected warm cache startup to request async refresh")
	}

	if cloneCalls != 0 {
		t.Fatalf("expected cached startup to avoid clone/pull, got %d calls", cloneCalls)
	}

	// discoverAll scans skillDir (cachePath/skills) once; both registry
	// entries are identical and deduplicated by skillDir.
	if discoverCalls != 1 {
		t.Fatalf("expected 1 discover call (skillDir only), got %d calls", discoverCalls)
	}

	if len(catalogs) != 1 {
		t.Fatalf("expected 1 deduplicated catalog, got %d", len(catalogs))
	}
}

func TestLoadCatalogsForTUIRefreshDeduplicatesGitRegistries(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "https://example.com/skills.git", Ref: "main", Path: "skills"},
			{URL: "https://example.com/skills.git", Ref: "main", Path: "skills"},
		},
	}

	cachePath := t.TempDir()
	cloneCalls := 0

	restoreClone := swapCloneOrPull(func(url, ref string) (string, error) {
		cloneCalls++
		return cachePath, nil
	})
	defer restoreClone()

	restoreDiscover := swapDiscoverSkills(func(dir string) ([]pkgcatalog.Entry, []skill.MalformedSkill, error) {
		return []pkgcatalog.Entry{{Name: "cached-skill"}}, nil, nil
	})
	defer restoreDiscover()

	_, refreshAfterLoad, err := loadCatalogsForTUI(cfg, t.TempDir(), catalogLoadRefresh)
	if err != nil {
		// Overlap warnings are acceptable for duplicate entries.
		if !pkgcatalog.IsOverlapWarning(err) {
			t.Fatalf("load catalogs: %v", err)
		}
	}

	if refreshAfterLoad {
		t.Fatalf("expected explicit refresh loads to return false for refreshAfterLoad")
	}

	if cloneCalls != 1 {
		t.Fatalf("expected duplicate registries to refresh once, got %d calls", cloneCalls)
	}
}

func TestLoadCatalogsForTUIRepairsPartialCacheBeforeUsingIt(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "https://example.com/skills.git", Ref: "main", Path: "skills"},
		},
	}

	cachePath := cache.RegistryCachePath(cfg.Registries[0].URL, cfg.Registries[0].Ref)
	if err := os.MkdirAll(cachePath, 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}

	cloneCalls := 0

	restoreClone := swapCloneOrPull(func(url, ref string) (string, error) {
		cloneCalls++
		if err := os.MkdirAll(filepath.Join(cachePath, "skills"), 0o755); err != nil {
			t.Fatalf("repair cache dir: %v", err)
		}
		return cachePath, nil
	})
	defer restoreClone()

	restoreDiscover := swapDiscoverSkills(func(dir string) ([]pkgcatalog.Entry, []skill.MalformedSkill, error) {
		return []pkgcatalog.Entry{{Name: "repaired-skill"}}, nil, nil
	})
	defer restoreDiscover()

	_, refreshAfterLoad, err := loadCatalogsForTUI(cfg, t.TempDir(), catalogLoadCached)
	if err != nil {
		t.Fatalf("load catalogs: %v", err)
	}

	if refreshAfterLoad {
		t.Fatalf("expected repaired cold cache startup to skip follow-up refresh")
	}

	if cloneCalls != 1 {
		t.Fatalf("expected partial cache to be repaired via clone/pull once, got %d calls", cloneCalls)
	}
}

func TestLoadCatalogsForTUISeparatesSameURLDifferentPaths(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "https://example.com/skills.git", Ref: "main", Path: "skills"},
			{URL: "https://example.com/skills.git", Ref: "main", Path: ".factory/skills"},
		},
	}

	cachePath := t.TempDir()
	cloneCalls := 0

	restoreClone := swapCloneOrPull(func(url, ref string) (string, error) {
		cloneCalls++
		return cachePath, nil
	})
	defer restoreClone()

	discoverCalls := make(map[string]int)
	restoreDiscover := swapDiscoverSkills(func(dir string) ([]pkgcatalog.Entry, []skill.MalformedSkill, error) {
		discoverCalls[dir]++
		if strings.Contains(dir, ".factory") {
			return []pkgcatalog.Entry{{Name: "openspec-apply-change"}}, nil, nil
		}
		return []pkgcatalog.Entry{{Name: "writing-golang-code"}, {Name: "efficient-go"}}, nil, nil
	})
	defer restoreDiscover()

	catalogs, _, err := loadCatalogsForTUI(cfg, t.TempDir(), catalogLoadRefresh)
	if err != nil {
		t.Fatalf("load catalogs: %v", err)
	}

	if cloneCalls != 1 {
		t.Fatalf("expected same URL+ref to clone once, got %d calls", cloneCalls)
	}

	// discoverAll scans each unique skillDir once: cachePath/skills and
	// cachePath/.factory/skills. No separate basePath scan.
	if got := len(discoverCalls); got != 2 {
		t.Fatalf("expected 2 distinct dirs to be scanned (2 skillDirs), got %d (calls=%v)", got, discoverCalls)
	}

	// With path-aware catalog keys, entries with the same URL+ref but
	// different paths produce separate catalogs (one per path).
	if len(catalogs) != 2 {
		t.Fatalf("expected 2 catalogs (one per path), got %d", len(catalogs))
	}

	// Verify each catalog contains the expected skills from its path.
	allNames := make(map[string]struct{})
	for _, v := range catalogs {
		for _, s := range v.Entries {
			allNames[s.Name] = struct{}{}
		}
	}
	for _, want := range []string{"writing-golang-code", "efficient-go", "openspec-apply-change"} {
		if _, ok := allNames[want]; !ok {
			t.Fatalf("combined catalogs missing %q; have %v", want, allNames)
		}
	}
}

func TestRegistryDisplayNameStripsSuffixAndPath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"git@example.com:org/security-skills.git", "security-skills"},
		{"https://example.com/org/infra-skills.git", "infra-skills"},
		{"/local/path/team-skills", "team-skills"},
		{"team-skills", "team-skills"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := registryDisplayName(tc.in); got != tc.want {
			t.Fatalf("registryDisplayName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBuildRowsPopulatesPerScopeEnablement(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:        "git@example.com:org/security.git",
				Ref:        "main",
				Skills:     ks("audit-logging"),
				Source:     config.UserPersonal,
				CatalogKey: "sec-key",
			},
			{
				URL:        "git@example.com:org/team.git",
				Ref:        "main",
				Skills:     ks("code-review"),
				Source:     config.RepoCommitted,
				CatalogKey: "team-key",
			},
		},
	}
	catalogs := map[string]catalogInfo{
		"sec-key": {Entries: []pkgcatalog.Entry{
			{Name: "audit-logging", Path: "/cache/sec/audit-logging"},
			{Name: "extra-security", Path: "/cache/sec/extra-security"},
		}},
		"team-key": {Entries: []pkgcatalog.Entry{
			{Name: "code-review", Path: "/cache/team/code-review"},
			{Name: "deploy-checklist", Path: "/cache/team/deploy-checklist"},
		}},
	}

	// Simulate resolver output (ignore real resolver for test focus).
	resolved := []resolver.ResolvedArtifact{
		{Entry: pkgcatalog.Entry{Name: "audit-logging"}, RegistryURL: "git@example.com:org/security.git", Source: config.UserPersonal, CatalogKey: "sec-key"},
		{Entry: pkgcatalog.Entry{Name: "code-review"}, RegistryURL: "git@example.com:org/team.git", Source: config.RepoCommitted, CatalogKey: "team-key"},
		{Entry: pkgcatalog.Entry{Name: "my-personal", Path: "/cache/team/my-personal"}, RegistryURL: "git@example.com:org/team.git", Source: config.RepoCommitted, CatalogKey: "team-key"},
	}

	// my-personal is locally enabled; deploy-checklist is locally excluded.
	localCfg := &config.LocalConfig{
		Skills: config.KindSelector{Include: []string{"my-personal", "code-review"}, Exclude: []string{"deploy-checklist"}},
	}

	rows := buildRows(cfg, catalogs, resolved, localCfg, materialize.DriftReport{})

	byName := make(map[string]row, len(rows))
	for _, r := range rows {
		byName[r.Artifact.Name] = r
	}

	// audit-logging: global-only enablement.
	a := byName["audit-logging"]
	if !a.EnabledGlobal || a.EnabledProject || a.EnabledLocal {
		t.Fatalf("audit-logging: expected EnabledGlobal only, got %+v", a)
	}
	if a.RegistryName != "security" {
		t.Fatalf("audit-logging: expected RegistryName=security, got %q", a.RegistryName)
	}

	// code-review: project + local.
	cr := byName["code-review"]
	if cr.EnabledGlobal || !cr.EnabledProject || !cr.EnabledLocal {
		t.Fatalf("code-review: expected project+local, got %+v", cr)
	}

	// my-personal: local-only (local Skills field, not in any registry's Skills list).
	mp := byName["my-personal"]
	if mp.EnabledGlobal || mp.EnabledProject || !mp.EnabledLocal {
		t.Fatalf("my-personal: expected EnabledLocal only, got %+v", mp)
	}

	// deploy-checklist: available but locally excluded.
	dc, ok := byName["deploy-checklist"]
	if !ok {
		t.Fatalf("deploy-checklist not present in rows")
	}
	if !dc.IsAvailable {
		t.Fatalf("deploy-checklist should be available, got %+v", dc)
	}
	if !dc.ExcludedLocally {
		t.Fatalf("deploy-checklist: expected ExcludedLocally=true, got %+v", dc)
	}
	if dc.RegistryName != "team" {
		t.Fatalf("deploy-checklist: expected RegistryName=team, got %q", dc.RegistryName)
	}

	// extra-security: available, no scope flags set.
	es := byName["extra-security"]
	if !es.IsAvailable || es.EnabledGlobal || es.EnabledProject || es.EnabledLocal || es.ExcludedLocally {
		t.Fatalf("extra-security: expected available-only, got %+v", es)
	}
	if es.RegistryName != "security" {
		t.Fatalf("extra-security: expected RegistryName=security, got %q", es.RegistryName)
	}
}

// TestBuildRowsRegistryScopedLocalSelections is a regression guard: install
// --scope local and the TUI now write registry-scoped local entries
// (RepoPersonal source) instead of top-level selectors. Such includes must be
// recognized as EnabledLocal (not mis-attributed as EnabledProject), and
// registry-scoped excludes must set ExcludedLocally. Because buildRows can
// return multiple rows for the same artifact name from different registries,
// local state must be keyed by registry identity so a local include in one
// registry does not badge a same-named resource from another registry.
func TestBuildRowsRegistryScopedLocalSelections(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			// Local (RepoPersonal) registry whose catalog provides keep-skill
			// and a same-named "shared" skill. Crucially it carries NO
			// includes here, so the EnabledLocal outcome must come from the
			// localCfg.Registries include handling under test — not from the
			// merged RepoPersonal entry's own selection.
			{
				URL:        "./plugins/local",
				Source:     config.RepoPersonal,
				CatalogKey: "local-key",
			},
			// Project registry enables its skills via default; the local layer
			// excludes drop-skill from it (registry-scoped, cross-layer). It
			// also provides a same-named "shared" skill that must NOT inherit
			// the local badge from the other registry's local include.
			{
				URL:        "./plugins/proj",
				Default:    true,
				Source:     config.RepoCommitted,
				CatalogKey: "proj-key",
			},
		},
	}
	catalogs := map[string]catalogInfo{
		"local-key": {Entries: []pkgcatalog.Entry{{Name: "keep-skill"}, {Name: "shared"}}},
		"proj-key":  {Entries: []pkgcatalog.Entry{{Name: "drop-skill"}, {Name: "shared"}}},
	}
	resolved := []resolver.ResolvedArtifact{
		{Entry: pkgcatalog.Entry{Name: "keep-skill"}, RegistryURL: "./plugins/local", Source: config.RepoPersonal, CatalogKey: "local-key"},
		// "shared" resolves from BOTH registries; buildRows keeps a row per
		// catalog key.
		{Entry: pkgcatalog.Entry{Name: "shared"}, RegistryURL: "./plugins/local", Source: config.RepoPersonal, CatalogKey: "local-key"},
		{Entry: pkgcatalog.Entry{Name: "shared"}, RegistryURL: "./plugins/proj", Source: config.RepoCommitted, CatalogKey: "proj-key"},
	}
	localCfg := &config.LocalConfig{
		Registries: []config.RegistryEntry{
			{URL: "./plugins/local", Skills: config.KindSelector{Include: []string{"keep-skill", "shared"}}},
			{URL: "./plugins/proj", Skills: config.KindSelector{Exclude: []string{"drop-skill"}}},
		},
	}

	rows := buildRows(cfg, catalogs, resolved, localCfg, materialize.DriftReport{})
	// buildRows can return multiple rows for the same artifact name from
	// different registries, so key by (catalog key, name), not name alone.
	byKey := make(map[string]row, len(rows))
	for _, r := range rows {
		byKey[r.Artifact.CatalogKey+"\x01"+r.Artifact.Name] = r
	}

	// Registry-scoped local include → EnabledLocal, NOT mis-attributed as project.
	keep := byKey["local-key\x01keep-skill"]
	if !keep.EnabledLocal {
		t.Fatalf("keep-skill: expected EnabledLocal=true (registry-scoped local include), got %+v", keep)
	}
	if keep.EnabledProject {
		t.Fatalf("keep-skill: expected EnabledProject=false (must not be mis-attributed), got %+v", keep)
	}

	// Registry-scoped local exclude must set ExcludedLocally.
	drop := byKey["proj-key\x01drop-skill"]
	if !drop.ExcludedLocally {
		t.Fatalf("drop-skill: expected ExcludedLocally=true (registry-scoped local exclude), got %+v", drop)
	}

	// Same-name cross-registry: the local include applies ONLY to the local
	// registry's "shared" row; the project registry's same-named "shared" must
	// not get the local badge.
	sharedLocal := byKey["local-key\x01shared"]
	if !sharedLocal.EnabledLocal {
		t.Fatalf("shared (local registry): expected EnabledLocal=true, got %+v", sharedLocal)
	}
	sharedProj := byKey["proj-key\x01shared"]
	if sharedProj.EnabledLocal {
		t.Fatalf("shared (project registry): expected EnabledLocal=false (local include must not bleed across registries), got %+v", sharedProj)
	}
}

// TestBuildRowsLocalCollectionMembersEnabled guards that resources contributed
// by a locally-selected collection render as EnabledLocal. Local-scope
// collection selections are registry-scoped, so a member's enablement must be
// inherited from the local collection state (not only from global/project).
func TestBuildRowsLocalCollectionMembersEnabled(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:         "./plugins/local",
				Collections: config.KindSelector{Include: []string{"mycoll"}},
				Source:      config.RepoPersonal,
				CatalogKey:  "local-key",
			},
		},
	}
	catalogs := map[string]catalogInfo{
		"local-key": {Entries: []pkgcatalog.Entry{
			{Kind: ir.KindCollection, Name: "mycoll"},
			{Kind: ir.KindSkill, Name: "member-skill"},
		}},
	}
	resolved := []resolver.ResolvedArtifact{
		// Collection member contributed via the locally-selected collection.
		{Entry: pkgcatalog.Entry{Kind: ir.KindSkill, Name: "member-skill"}, RegistryURL: "./plugins/local", Source: config.RepoPersonal, CatalogKey: "local-key", Via: "collection:mycoll"},
	}
	localCfg := &config.LocalConfig{
		Registries: []config.RegistryEntry{
			{URL: "./plugins/local", Collections: config.KindSelector{Include: []string{"mycoll"}}},
		},
	}

	rows := buildRows(cfg, catalogs, resolved, localCfg, materialize.DriftReport{})
	var member *row
	for i := range rows {
		if rows[i].Artifact.Name == "member-skill" && rows[i].Artifact.Via != "" {
			member = &rows[i]
			break
		}
	}
	if member == nil {
		t.Fatal("member-skill (collection member) row not found")
	}
	if !member.EnabledLocal {
		t.Fatalf("member-skill: expected EnabledLocal=true (member of locally-selected collection), got %+v", *member)
	}
}

func TestBuildRowsUsesResolverCatalogWinners(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL: "git@example.com:awesome-ai.git", Ref: "main", Path: "teams", Source: config.Organization, CatalogKey: "parent",
				MCP: ks("genai-atlassian"),
			},
			{
				URL: "git@example.com:awesome-ai", Ref: "main", Path: "teams/common/genai", Source: config.UserPersonal, CatalogKey: "child",
				MCP: ks("genai-atlassian"),
			},
		},
	}
	entries := []pkgcatalog.Entry{
		{Kind: ir.KindMcpServer, Name: "genai-airflow"},
		{Kind: ir.KindMcpServer, Name: "genai-atlassian"},
	}
	catalogs := map[string]catalogInfo{
		"parent": {Entries: entries},
		"child":  {Entries: entries},
	}
	catalogEntries := entriesFromCatalogs(catalogs)
	resolved, errs := resolver.Resolve(cfg, catalogEntries, nil)
	if len(errs) != 0 {
		t.Fatalf("Resolve() errors = %v, want none", errs)
	}

	rows := buildRows(cfg, catalogs, resolved, nil, materialize.DriftReport{})
	if len(rows) != 2 {
		t.Fatalf("buildRows() returned %d rows, want 2: %#v", len(rows), rows)
	}
	byName := make(map[string]row, len(rows))
	for _, candidate := range rows {
		byName[candidate.Artifact.Name] = candidate
	}
	if !byName["genai-airflow"].IsAvailable {
		t.Fatalf("genai-airflow row = %#v, want available", byName["genai-airflow"])
	}
	if byName["genai-atlassian"].IsAvailable {
		t.Fatalf("genai-atlassian row = %#v, want enabled", byName["genai-atlassian"])
	}
	for _, name := range []string{"genai-airflow", "genai-atlassian"} {
		if got := byName[name].RegistryPath; got != "teams/common/genai" {
			t.Fatalf("%s registry path = %q, want child path", name, got)
		}
	}
}

func TestBuildRowsIncludesAllResourceKinds(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:        "git@example.com:org/resources.git",
				Ref:        "main",
				Skills:     ks("audit-skill"),
				Commands:   ks("continue"),
				Subagents:  ks("worker-generic"),
				Hooks:      ks("pre-tool-use"),
				MCP:        ks("memory"),
				Source:     config.UserPersonal,
				CatalogKey: "resources-key",
			},
		},
	}
	catalogs := map[string]catalogInfo{
		"resources-key": {Entries: []pkgcatalog.Entry{
			{Kind: ir.KindSkill, Name: "audit-skill"},
			{Kind: ir.KindCommand, Name: "continue"},
			{Kind: ir.KindSubagent, Name: "worker-generic"},
			{Kind: ir.KindHook, Name: "pre-tool-use"},
			{Kind: ir.KindMcpServer, Name: "memory"},
		}},
	}
	resolved := []resolver.ResolvedArtifact{
		{Entry: pkgcatalog.Entry{Kind: ir.KindSkill, Name: "audit-skill"}, RegistryURL: "git@example.com:org/resources.git", Source: config.UserPersonal, CatalogKey: "resources-key"},
		{Entry: pkgcatalog.Entry{Kind: ir.KindCommand, Name: "continue"}, RegistryURL: "git@example.com:org/resources.git", Source: config.UserPersonal, CatalogKey: "resources-key"},
		{Entry: pkgcatalog.Entry{Kind: ir.KindSubagent, Name: "worker-generic"}, RegistryURL: "git@example.com:org/resources.git", Source: config.UserPersonal, CatalogKey: "resources-key"},
		{Entry: pkgcatalog.Entry{Kind: ir.KindHook, Name: "pre-tool-use"}, RegistryURL: "git@example.com:org/resources.git", Source: config.UserPersonal, CatalogKey: "resources-key"},
		{Entry: pkgcatalog.Entry{Kind: ir.KindMcpServer, Name: "memory"}, RegistryURL: "git@example.com:org/resources.git", Source: config.UserPersonal, CatalogKey: "resources-key"},
	}

	rows := buildRows(cfg, catalogs, resolved, nil, materialize.DriftReport{})

	byResource := make(map[string]row, len(rows))
	for _, r := range rows {
		byResource[skill.ResourceKey(r.Artifact.Kind, r.Artifact.Name)] = r
	}
	for _, want := range []pkgcatalog.Entry{
		{Kind: ir.KindSkill, Name: "audit-skill"},
		{Kind: ir.KindCommand, Name: "continue"},
		{Kind: ir.KindSubagent, Name: "worker-generic"},
		{Kind: ir.KindHook, Name: "pre-tool-use"},
		{Kind: ir.KindMcpServer, Name: "memory"},
	} {
		r, ok := byResource[skill.ResourceKey(want.Kind, want.Name)]
		if !ok {
			t.Fatalf("missing %s %q row; have %v", skill.KindLabel(want.Kind), want.Name, byResource)
		}
		if !r.EnabledGlobal || r.EnabledProject || r.EnabledLocal || r.IsAvailable {
			t.Fatalf("%s %q: expected enabled global row, got %+v", skill.KindLabel(want.Kind), want.Name, r)
		}
	}
}

func TestRenderListShowsLoadingStateBeforeSnapshotHydrates(t *testing.T) {
	m := Model{loading: true}

	if got := renderList(m, 24); got != "Loading resources..." {
		t.Fatalf("expected loading state, got %q", got)
	}
}

func swapCloneOrPull(fn func(url, ref string) (string, error)) func() {
	prev := cloneOrPull
	cloneOrPull = fn
	return func() {
		cloneOrPull = prev
	}
}

func swapDiscoverSkills(fn func(dir string) ([]pkgcatalog.Entry, []skill.MalformedSkill, error)) func() {
	prev := discoverCatalog
	discoverCatalog = func(dir, sourceHarness string) (catalogInfo, error) {
		entries, malformed, err := fn(dir)
		if err != nil {
			return catalogInfo{}, err
		}
		return catalogInfo{
			Entries:   entries,
			Malformed: malformed,
			Resources: pkgcatalog.EntriesResources(entries),
		}, nil
	}
	return func() {
		discoverCatalog = prev
	}
}

func TestReadSnapshotResourcesMaterializeThroughTUISync(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	registryRoot := t.TempDir()
	t.Setenv("HOME", homeDir)

	if err := os.MkdirAll(filepath.Join(repoRoot, ".agents"), 0o755); err != nil {
		t.Fatalf("mkdir repo agents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".agents", "config.yaml"), []byte(repoV2Header), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	skillDir := filepath.Join(registryRoot, "skills", "audit")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir registry skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: audit\ndescription: Audit workflows\n---\n# Audit\n"), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	globalConfig := v2Header + `registries:
  - url: ` + registryRoot + `
    skills:
      - audit
`
	if err := os.MkdirAll(filepath.Join(homeDir, ".agents"), 0o755); err != nil {
		t.Fatalf("mkdir home agents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".agents", "config.yaml"), []byte(globalConfig), 0o644); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) {
		return nil, nil
	})
	defer restoreOrg()

	restoreGlobal := swapLoadGlobalConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(homeDir, ".agents", "config.yaml"))
	})
	defer restoreGlobal()
	restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(repoRoot, ".agents", "config.yaml"))
	})
	defer restoreRepo()
	restoreLocal := swapLoadLocalConfig(func() (*config.LocalConfig, error) {
		return config.LoadLocalConfigFromPath(filepath.Join(repoRoot, ".agents", "config.local.yaml"))
	})
	defer restoreLocal()

	snap := readSnapshot(context.Background(), repoRoot, homeDir, catalogLoadCached)
	if len(snap.errs) > 0 {
		t.Fatalf("snapshot errors: %v", snap.errs)
	}
	if len(snap.resolved) != 1 {
		t.Fatalf("resolved %d skills, want 1", len(snap.resolved))
	}
	if snap.resolved[0].Resource == nil {
		t.Fatalf("resolved skill Resource is nil; TUI sync would prune without writing")
	}

	msg := runSyncCmd(context.Background(), repoRoot)()
	done, ok := msg.(syncDoneMsg)
	if !ok {
		t.Fatalf("runSyncCmd returned %T, want syncDoneMsg", msg)
	}
	if done.err != nil {
		t.Fatalf("sync error: %v", done.err)
	}
	if _, err := os.Lstat(filepath.Join(homeDir, ".factory", "skills", "audit")); err != nil {
		t.Fatalf("expected TUI sync to materialize audit skill in ~/.factory: %v", err)
	}
}

func TestReadSnapshotRowsIncludeAllFactoryResourceKinds(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	registryRoot := t.TempDir()
	t.Setenv("HOME", homeDir)

	if err := os.MkdirAll(filepath.Join(repoRoot, ".agents"), 0o755); err != nil {
		t.Fatalf("mkdir repo agents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".agents", "config.yaml"), []byte(repoV2Header), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	for path, content := range map[string]string{
		".agents/skills/audit/SKILL.md": "---\nname: audit\ndescription: Audit workflows\n---\n# Audit\n",
		".agents/commands/continue.md":  "# Continue\n",
		".agents/droids/worker.md":      "---\nname: worker\ndescription: Do work\n---\nWork\n",
		".agents/hooks/hooks.json":      `{"hooks":{"PreToolUse":[{"type":"command","command":"${DROID_PLUGIN_ROOT}/check.sh","matcher":"Read"}]}}`,
		".agents/mcp.json":              `{"mcpServers":{"memory":{"command":"memory-server"}}}`,
	} {
		fullPath := filepath.Join(registryRoot, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(fullPath), err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", fullPath, err)
		}
	}

	globalConfig := v2Header + `registries:
  - url: ` + registryRoot + `
    path: .agents
    source_harness: factory
    skills:
      - audit
    commands:
      - continue
    subagents:
      - worker
    hooks:
      - PreToolUse-command
    mcp:
      - memory
`
	if err := os.MkdirAll(filepath.Join(homeDir, ".agents"), 0o755); err != nil {
		t.Fatalf("mkdir home agents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".agents", "config.yaml"), []byte(globalConfig), 0o644); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) {
		return nil, nil
	})
	defer restoreOrg()

	restoreGlobal := swapLoadGlobalConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(homeDir, ".agents", "config.yaml"))
	})
	defer restoreGlobal()
	restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(repoRoot, ".agents", "config.yaml"))
	})
	defer restoreRepo()
	restoreLocal := swapLoadLocalConfig(func() (*config.LocalConfig, error) {
		return config.LoadLocalConfigFromPath(filepath.Join(repoRoot, ".agents", "config.local.yaml"))
	})
	defer restoreLocal()

	snap := readSnapshot(context.Background(), repoRoot, homeDir, catalogLoadCached)
	if len(snap.errs) > 0 {
		t.Fatalf("snapshot errors: %v", snap.errs)
	}

	byResource := make(map[string]row, len(snap.rows))
	for _, r := range snap.rows {
		byResource[skill.ResourceKey(r.Artifact.Kind, r.Artifact.Name)] = r
	}
	for _, want := range []pkgcatalog.Entry{
		{Kind: ir.KindSkill, Name: "audit"},
		{Kind: ir.KindCommand, Name: "continue"},
		{Kind: ir.KindSubagent, Name: "worker"},
		{Kind: ir.KindHook, Name: "PreToolUse-command"},
		{Kind: ir.KindMcpServer, Name: "memory"},
	} {
		r, ok := byResource[skill.ResourceKey(want.Kind, want.Name)]
		if !ok {
			t.Fatalf("missing %s %q row; have %v", skill.KindLabel(want.Kind), want.Name, byResource)
		}
		if !r.EnabledGlobal || r.IsAvailable {
			t.Fatalf("%s %q: expected enabled global row, got %+v", skill.KindLabel(want.Kind), want.Name, r)
		}
	}
}

func TestRunUpdateCmdRefreshesAndMaterializes(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	registryRoot := t.TempDir()
	t.Setenv("HOME", homeDir)

	if err := os.MkdirAll(filepath.Join(repoRoot, ".agents"), 0o755); err != nil {
		t.Fatalf("mkdir repo agents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".agents", "config.yaml"), []byte(repoV2Header), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	skillDir := filepath.Join(registryRoot, "skills", "refresh-me")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir registry skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: refresh-me\ndescription: Refresh workflows\n---\n# Refresh\n"), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	globalConfig := v2Header + `registries:
  - url: ` + registryRoot + `
    skills:
      - refresh-me
`
	if err := os.MkdirAll(filepath.Join(homeDir, ".agents"), 0o755); err != nil {
		t.Fatalf("mkdir home agents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".agents", "config.yaml"), []byte(globalConfig), 0o644); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) {
		return nil, nil
	})
	defer restoreOrg()

	restoreGlobal := swapLoadGlobalConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(homeDir, ".agents", "config.yaml"))
	})
	defer restoreGlobal()
	restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(repoRoot, ".agents", "config.yaml"))
	})
	defer restoreRepo()
	restoreLocal := swapLoadLocalConfig(func() (*config.LocalConfig, error) {
		return config.LoadLocalConfigFromPath(filepath.Join(repoRoot, ".agents", "config.local.yaml"))
	})
	defer restoreLocal()

	msg := runUpdateCmd(context.Background(), repoRoot, homeDir, 1)()
	done, ok := msg.(updateDoneMsg)
	if !ok {
		t.Fatalf("runUpdateCmd returned %T, want updateDoneMsg", msg)
	}
	if done.err != nil {
		t.Fatalf("runUpdateCmd failed: %v", done.err)
	}
	if done.snapshot.generation != 1 {
		t.Fatalf("snapshot generation = %d, want 1", done.snapshot.generation)
	}
	if _, err := os.Lstat(filepath.Join(homeDir, ".factory", "skills", "refresh-me")); err != nil {
		t.Fatalf("expected TUI update to materialize refresh-me in ~/.factory: %v", err)
	}
}

func TestBuildRowsPopulatesHarnessesForResolvedAndAvailableSkills(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:        "git@example.com:org/security.git",
				Ref:        "main",
				Skills:     ks("audit-logging"),
				Harnesses:  []string{"factory", "cursor"},
				Source:     config.RepoCommitted,
				CatalogKey: "sec-key",
			},
		},
	}
	catalogs := map[string]catalogInfo{
		"sec-key": {Entries: []pkgcatalog.Entry{
			{Name: "audit-logging", Path: "/cache/sec/audit-logging"},
			{Name: "extra-security", Path: "/cache/sec/extra-security"},
		}},
	}
	resolved := []resolver.ResolvedArtifact{
		{Entry: pkgcatalog.Entry{Name: "audit-logging"}, RegistryURL: "git@example.com:org/security.git", CatalogKey: "sec-key", Harnesses: testTargets("factory", "cursor")},
	}

	rows := buildRows(cfg, catalogs, resolved, nil, materialize.DriftReport{})

	byName := make(map[string]row, len(rows))
	for _, r := range rows {
		byName[r.Artifact.Name] = r
	}
	if got := byName["audit-logging"].Harnesses; len(got) != 2 || got[0] != "factory" || got[1] != "cursor" {
		t.Fatalf("resolved skill harnesses = %#v, want [factory cursor]", got)
	}
	if got := byName["extra-security"].Harnesses; len(got) != 2 || got[0] != "factory" || got[1] != "cursor" {
		t.Fatalf("available skill inherited harnesses = %#v, want [factory cursor]", got)
	}
}

func TestBuildRowsLabelsInheritedAndRegistryOverrideHarnesses(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:        "git@example.com:org/default.git",
				Ref:        "main",
				Skills:     ks("default-skill"),
				Harnesses:  []string{"factory"},
				Source:     config.RepoCommitted,
				CatalogKey: "default-key",
			},
			{
				URL:        "git@example.com:org/override.git",
				Ref:        "main",
				Skills:     ks("override-skill"),
				Harnesses:  []string{"cursor"},
				Source:     config.RepoCommitted,
				CatalogKey: "override-key",
			},
		},
	}
	catalogs := map[string]catalogInfo{
		"default-key":  {Entries: []pkgcatalog.Entry{{Name: "default-skill"}}},
		"override-key": {Entries: []pkgcatalog.Entry{{Name: "override-skill"}}},
	}
	resolved := []resolver.ResolvedArtifact{
		{Entry: pkgcatalog.Entry{Name: "default-skill"}, RegistryURL: "git@example.com:org/default.git", CatalogKey: "default-key", Harnesses: testTargets("factory")},
		{Entry: pkgcatalog.Entry{Name: "override-skill"}, RegistryURL: "git@example.com:org/override.git", CatalogKey: "override-key", Harnesses: testTargets("cursor")},
	}
	defaults := map[config.Source][]string{config.RepoCommitted: []string{"factory"}}

	rows := buildRows(cfg, catalogs, resolved, nil, materialize.DriftReport{}, defaults)

	byName := make(map[string]row, len(rows))
	for _, r := range rows {
		byName[r.Artifact.Name] = r
	}
	if got := byName["default-skill"].HarnessSource; got != "project default" {
		t.Fatalf("default-skill HarnessSource = %q, want project default", got)
	}
	if got := byName["override-skill"].HarnessSource; got != "registry override" {
		t.Fatalf("override-skill HarnessSource = %q, want registry override", got)
	}
}

func TestBuildRowsAttributesMarketplacePinsByScope(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	pin := config.MarketplaceResourcePin{
		RegistryID: "source-id", Kind: "skill", Name: "review", Revision: "v1", Digest: digest,
		Acquisition: config.MarketplaceAcquisitionMirror, InstalledName: "team-review", Direct: true,
	}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{
		{URL: "https://market.example", InstanceID: "market-id", Alias: "public", Resources: []config.MarketplaceResourcePin{pin}, Source: config.UserPersonal},
		{URL: "https://market.example", InstanceID: "market-id", Alias: "public", Resources: []config.MarketplaceResourcePin{pin}, Source: config.RepoCommitted},
		{URL: "https://market.example", InstanceID: "market-id", Alias: "public", Resources: []config.MarketplaceResourcePin{pin}, Source: config.RepoPersonal},
	}}
	resolved := []resolver.ResolvedArtifact{{
		Entry:       pkgcatalog.Entry{Kind: ir.KindSkill, Name: "team-review"},
		RegistryURL: "https://market.example", Ref: "v1", Source: config.RepoCommitted, CatalogKey: marketplaceCatalogKey(cfg.Marketplaces[0], pin),
		IsMarketplace: true, MarketplaceURL: "https://market.example",
		MarketplaceInstanceID: "market-id", MarketplaceRegistryID: "source-id", InstalledName: "team-review",
	}}

	rows := buildRows(cfg, nil, resolved, nil, materialize.DriftReport{})

	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if !rows[0].EnabledGlobal || !rows[0].EnabledProject || !rows[0].EnabledLocal {
		t.Fatalf("Marketplace scope flags = global:%v project:%v local:%v", rows[0].EnabledGlobal, rows[0].EnabledProject, rows[0].EnabledLocal)
	}
	if len(rows[0].MarketplaceSelections) != 3 {
		t.Fatalf("Marketplace selections = %#v", rows[0].MarketplaceSelections)
	}
}

func TestBuildRowsAttributesIndexOnlyPinAcrossEquivalentMarketplaceURLs(t *testing.T) {
	digest := "sha256:" + strings.Repeat("d", 64)
	commit := strings.Repeat("e", 40)
	pin := config.MarketplaceResourcePin{
		RegistryID: "source-id", Kind: "skill", Name: "review", Revision: "revision-id", Digest: digest,
		Acquisition: config.MarketplaceAcquisitionIndexOnly, RegistryCommit: commit, Direct: true,
	}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{
		URL: "HTTPS://MARKET.example/", InstanceID: "market-id", Resources: []config.MarketplaceResourcePin{pin}, Source: config.RepoCommitted,
	}}}
	resolved := []resolver.ResolvedArtifact{{
		Entry: pkgcatalog.Entry{Kind: ir.KindSkill, Name: "review"}, RegistryURL: "https://market.example", Ref: commit, CatalogKey: marketplaceCatalogKey(cfg.Marketplaces[0], pin),
		IsMarketplace: true, MarketplaceURL: "https://market.example", MarketplaceInstanceID: "market-id", MarketplaceRegistryID: "source-id",
	}}

	rows := buildRows(cfg, nil, resolved, nil, materialize.DriftReport{})

	if len(rows) != 1 || !rows[0].EnabledProject || len(rows[0].MarketplaceSelections) != 1 {
		t.Fatalf("index-only Marketplace row = %#v", rows)
	}
}

func TestBuildRowsKeepsMarketplaceRevisionsScopedToExactArtifact(t *testing.T) {
	userPin := config.MarketplaceResourcePin{
		RegistryID: "source-id", Kind: "skill", Name: "review", Revision: "v1",
		Digest: "sha256:" + strings.Repeat("1", 64), Acquisition: config.MarketplaceAcquisitionMirror, Direct: true,
	}
	projectPin := userPin
	projectPin.Revision = "v2"
	projectPin.Digest = "sha256:" + strings.Repeat("2", 64)
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{
		{URL: "https://market.example", InstanceID: "market-id", Resources: []config.MarketplaceResourcePin{userPin}, Source: config.UserPersonal},
		{URL: "https://market.example", InstanceID: "market-id", Resources: []config.MarketplaceResourcePin{projectPin}, Source: config.RepoCommitted},
	}}
	resolved := []resolver.ResolvedArtifact{{
		Entry: pkgcatalog.Entry{Kind: ir.KindSkill, Name: "review"}, CatalogKey: marketplaceCatalogKey(cfg.Marketplaces[1], projectPin),
		IsMarketplace: true, MarketplaceURL: "https://market.example", MarketplaceInstanceID: "market-id", MarketplaceRegistryID: "source-id",
	}}

	rows := buildRows(cfg, nil, resolved, nil, materialize.DriftReport{})

	if len(rows) != 1 || rows[0].EnabledGlobal || !rows[0].EnabledProject || len(rows[0].MarketplaceSelections) != 1 {
		t.Fatalf("exact-revision Marketplace row = %#v", rows)
	}
	if got := rows[0].MarketplaceSelections[config.RepoCommitted].Pin.Revision; got != "v2" {
		t.Fatalf("selected Marketplace revision = %q, want v2", got)
	}
}

func TestBuildRowsKeepsMarketplaceKindsSeparate(t *testing.T) {
	digest := "sha256:" + strings.Repeat("3", 64)
	skillPin := config.MarketplaceResourcePin{
		RegistryID: "source-id", Kind: "skill", Name: "skill-source-name", InstalledName: "shared", Revision: "v1",
		Digest: digest, Acquisition: config.MarketplaceAcquisitionMirror, Direct: true,
	}
	commandPin := skillPin
	commandPin.Kind = "command"
	commandPin.Name = "command-source-name"
	entry := config.MarketplaceEntry{
		URL: "https://market.example", InstanceID: "market-id", Resources: []config.MarketplaceResourcePin{skillPin, commandPin}, Source: config.RepoCommitted,
	}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}
	catalogKey := marketplaceCatalogKey(entry, skillPin)
	resolved := []resolver.ResolvedArtifact{
		{Entry: pkgcatalog.Entry{Kind: ir.KindSkill, Name: "shared"}, CatalogKey: catalogKey, IsMarketplace: true},
		{Entry: pkgcatalog.Entry{Kind: ir.KindCommand, Name: "shared"}, CatalogKey: catalogKey, IsMarketplace: true},
	}

	rows := buildRows(cfg, nil, resolved, nil, materialize.DriftReport{})

	if len(rows) != 2 {
		t.Fatalf("Marketplace rows = %#v", rows)
	}
	for _, row := range rows {
		selection := row.MarketplaceSelections[config.RepoCommitted]
		if selection.Pin.Kind != marketplaceProtocolKind(row.Artifact.Kind) {
			t.Fatalf("%s row attributed to %s pin", row.Artifact.Kind, selection.Pin.Kind)
		}
	}
}

func TestBuildRowsMarksCollectionOnlyMarketplaceLeaf(t *testing.T) {
	digest := "sha256:" + strings.Repeat("c", 64)
	pin := config.MarketplaceResourcePin{
		RegistryID: "source-id", Kind: "skill", Name: "review", Revision: "v1", Digest: digest,
		Acquisition: config.MarketplaceAcquisitionMirror,
		SelectedBy:  config.MarketplaceSelectionsFrom([]config.MarketplaceSelection{{RegistryID: "collections", Name: "bundle", Revision: "v1"}}),
	}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{
		URL: "https://market.example", InstanceID: "market-id", Resources: []config.MarketplaceResourcePin{pin}, Source: config.RepoCommitted,
	}}}
	resolved := []resolver.ResolvedArtifact{{
		Entry: pkgcatalog.Entry{Kind: ir.KindSkill, Name: "review"}, RegistryURL: "https://market.example", Ref: "v1", CatalogKey: marketplaceCatalogKey(cfg.Marketplaces[0], pin),
		IsMarketplace: true, MarketplaceURL: "https://market.example", MarketplaceInstanceID: "market-id", MarketplaceRegistryID: "source-id",
	}}

	rows := buildRows(cfg, nil, resolved, nil, materialize.DriftReport{})

	if len(rows) != 1 || !rows[0].EnabledProject || !rows[0].MarketplaceCollectionOnly {
		t.Fatalf("collection-only Marketplace row = %#v", rows)
	}
}

func testTargets(names ...string) []hcc.Target {
	targets := make([]hcc.Target, 0, len(names))
	for _, name := range names {
		targets = append(targets, hcc.Target(name))
	}
	return targets
}

func TestBuildRowsUsesSelectedResolverWinnerAcrossRegistries(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:        "git@example.com:org/primary.git",
				Ref:        "main",
				Skills:     ks("smith"),
				Source:     config.UserPersonal,
				CatalogKey: "primary-key",
			},
			{
				URL:        "/tmp/smith",
				Path:       "skills",
				Source:     config.RepoCommitted,
				CatalogKey: "smith-local-key",
			},
		},
	}
	catalogs := map[string]catalogInfo{
		"primary-key": {Entries: []pkgcatalog.Entry{
			{Name: "smith", Path: "/cache/primary/smith"},
		}},
		"smith-local-key": {Entries: []pkgcatalog.Entry{
			{Name: "smith", Path: "/tmp/smith/skills/smith"},
		}},
	}

	// Only the primary registry version is resolved (enabled).
	resolved := []resolver.ResolvedArtifact{
		{Entry: pkgcatalog.Entry{Name: "smith"}, RegistryURL: "git@example.com:org/primary.git", Source: config.UserPersonal, CatalogKey: "primary-key"},
	}

	rows := buildRows(cfg, catalogs, resolved, nil, materialize.DriftReport{})

	// The selected resolver winner should suppress the available runner-up.
	smithRows := make([]row, 0, 1)
	for _, r := range rows {
		if r.Artifact.Name == "smith" {
			smithRows = append(smithRows, r)
		}
	}
	if len(smithRows) != 1 {
		t.Fatalf("expected 1 smith resolver winner, got %d: %+v", len(smithRows), smithRows)
	}
	if smithRows[0].RegistryName != "primary" {
		t.Fatalf("smith registry = %q, want primary", smithRows[0].RegistryName)
	}
	if smithRows[0].IsAvailable {
		t.Fatal("primary smith should be enabled, got available")
	}
}

func TestBuildRowsEnablementFlagsScopedByRegistry(t *testing.T) {
	// Two registries at different scopes, both providing "smith".
	// The global one selects smith; the project one does not.
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:        "git@example.com:org/global.git",
				Ref:        "main",
				Skills:     ks("smith"),
				Source:     config.UserPersonal,
				CatalogKey: "global-key",
			},
			{
				URL:        "git@example.com:org/project.git",
				Ref:        "main",
				Source:     config.RepoCommitted,
				CatalogKey: "project-key",
			},
		},
	}
	catalogs := map[string]catalogInfo{
		"global-key": {Entries: []pkgcatalog.Entry{
			{Name: "smith", Path: "/cache/global/smith"},
		}},
		"project-key": {Entries: []pkgcatalog.Entry{
			{Name: "smith", Path: "/cache/project/smith"},
		}},
	}

	// Only the global registry version is resolved.
	resolved := []resolver.ResolvedArtifact{
		{Entry: pkgcatalog.Entry{Name: "smith"}, RegistryURL: "git@example.com:org/global.git", Source: config.UserPersonal, CatalogKey: "global-key"},
	}

	rows := buildRows(cfg, catalogs, resolved, nil, materialize.DriftReport{})

	smithRows := make([]row, 0, 1)
	for _, r := range rows {
		if r.Artifact.Name == "smith" {
			smithRows = append(smithRows, r)
		}
	}
	if len(smithRows) != 1 {
		t.Fatalf("expected 1 smith resolver winner, got %d", len(smithRows))
	}

	winner := smithRows[0]
	if winner.RegistryURL != "git@example.com:org/global.git" {
		t.Fatalf("smith registry = %q, want selected global registry", winner.RegistryURL)
	}
	// Global smith: EnabledGlobal=true, EnabledProject=false
	if !winner.EnabledGlobal {
		t.Fatal("global smith should be EnabledGlobal=true")
	}
	if winner.EnabledProject {
		t.Fatal("global smith should be EnabledProject=false (cross-contamination)")
	}
}
