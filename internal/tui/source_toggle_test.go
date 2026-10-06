package tui

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func runSourceToggleAt(repoRoot string, toggle sourceToggle, mapper pipeline.ConfigPathMapper) (bool, error) {
	return runSourceToggleInWorkspace(context.Background(), repoRoot, repoRoot, toggle, mapper)
}

// sourceToggleAtCursor resolves what [e] in the whole-source menu opened at
// the cursor would do.
func (m Model) sourceToggleAtCursor() (sourceToggle, string) {
	target, refusal, ok := m.sourceTargetAtCursor()
	if !ok {
		return sourceToggle{}, refusal
	}
	return m.sourceToggleFor(target)
}

func TestSourceExcludedByHonorsIdentityAndAuthority(t *testing.T) {
	excludeAll := config.RegistryEntry{URL: "/repo/plugins/tools", Source: config.RepoPersonal, ExcludeAll: true}
	perKind := config.RegistryEntry{URL: "git@example.com:mono.git", Ref: "main", Path: "plugins/a", Source: config.RepoPersonal}
	for _, kind := range config.AllKinds {
		perKind.SelectorForKindPtr(kind).ExcludeAll = true
	}
	partial := config.RegistryEntry{URL: "/repo/plugins/partial", Source: config.RepoPersonal}
	partial.Skills.ExcludeAll = true
	project := config.RegistryEntry{URL: "/repo/plugins/project", Source: config.RepoCommitted, ExcludeAll: true}
	required := config.RegistryEntry{URL: "/repo/plugins/required", Source: config.RepoPersonal, Required: true, ExcludeAll: true}
	cfg := &config.Config{Registries: []config.RegistryEntry{excludeAll, perKind, partial, project, required}}

	entries := localExcludeAllEntries(cfg)
	var urls []string
	for _, entry := range entries {
		urls = append(urls, entry.URL)
	}
	if !slices.Equal(urls, []string{excludeAll.URL, perKind.URL}) {
		t.Fatalf("local exclude-all entries = %v", urls)
	}

	artifact := func(url, ref, path string, source config.Source) resolver.ResolvedArtifact {
		return resolver.ResolvedArtifact{RegistryURL: url, Ref: ref, RegistryPath: path, Source: source}
	}
	tests := []struct {
		name     string
		artifact resolver.ResolvedArtifact
		want     bool
	}{
		{"directory source", artifact("/repo/plugins/tools", "", "", config.RepoCommitted), true},
		{"sub-path of excluded path", artifact("git@example.com:mono.git", "main", "plugins/a/nested", config.RepoCommitted), true},
		{"sibling path", artifact("git@example.com:mono.git", "main", "plugins/b", config.RepoCommitted), false},
		{"other ref", artifact("git@example.com:mono.git", "dev", "plugins/a", config.RepoCommitted), false},
		{"user source", artifact("/repo/plugins/tools", "", "", config.UserPersonal), false},
		{"partial kind exclusion", artifact("/repo/plugins/partial", "", "", config.RepoCommitted), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sourceExcludedBy(tt.artifact, entries); got != tt.want {
				t.Fatalf("sourceExcludedBy = %v, want %v", got, tt.want)
			}
		})
	}
	requiredArtifact := artifact("/repo/plugins/tools", "", "", config.RepoCommitted)
	requiredArtifact.Required = true
	if sourceExcludedBy(requiredArtifact, entries) {
		t.Fatal("required resource marked as excluded")
	}
}

func sourceToggleTestModel(rows []row) Model {
	m := NewModel("/repo", "/home")
	m.loading = false
	m.rows = rows
	m.filteredRows = rows
	m.rebuildFlatItems()
	return m
}

func cursorOnRow(t testing.TB, m *Model, name string) {
	t.Helper()
	for i, item := range m.flatItems {
		if item.Kind == flatArtifact && item.Row.Artifact.Name == name {
			m.cursor = i
			return
		}
	}
	t.Fatalf("row %q not in flat items", name)
}

func cursorOnSection(t testing.TB, m *Model, title string) {
	t.Helper()
	for i, item := range m.flatItems {
		if item.Kind == flatSection && item.SectionTitle == title {
			m.cursor = i
			return
		}
	}
	t.Fatalf("section %q not in flat items", title)
}

func TestSourceToggleAtCursorChoosesEdits(t *testing.T) {
	tools := withRegistry("/repo/plugins/tools", "tools")
	sourceExcluded := func(r *row) { r.IsAvailable = true; r.SourceExcludedLocally = true }
	userSource := func(r *row) { r.Artifact.Source = config.UserPersonal }
	tests := []struct {
		name    string
		rows    []row
		exclude bool
		edits   []config.SourceExclusionEdit
		refusal string
	}{
		{
			name:    "enabled source is excluded",
			rows:    []row{mkRow("alpha", withProject, tools), mkRow("beta", withProject, withExcluded, tools)},
			exclude: true,
			edits:   []config.SourceExclusionEdit{config.SourceExcludeAll},
		},
		{
			name:  "exclude_all is cleared",
			rows:  []row{mkRow("alpha", sourceExcluded, tools), mkRow("beta", sourceExcluded, withExcluded, tools)},
			edits: []config.SourceExclusionEdit{config.SourceClearExcludeAll},
		},
		{
			name:  "individually excluded resources are cleared",
			rows:  []row{mkRow("alpha", withAvailable, withExcluded, tools), mkRow("beta", withAvailable, withExcluded, tools), mkRow("req", withRequired, tools)},
			edits: []config.SourceExclusionEdit{config.SourceClearResourceExclusions},
		},
		{
			name:  "both exclusions are cleared",
			rows:  []row{mkRow("alpha", sourceExcluded, withExcluded, tools)},
			edits: []config.SourceExclusionEdit{config.SourceClearExcludeAll, config.SourceClearResourceExclusions},
		},
		{
			name:    "user source is refused",
			rows:    []row{mkRow("alpha", withGlobal, userSource, tools)},
			refusal: "local config cannot exclude a source from global or org config",
		},
		{
			name:    "required source is refused",
			rows:    []row{mkRow("alpha", withRequired, tools)},
			refusal: "required sources cannot be excluded",
		},
		{
			name:    "non-overridable Marketplace source is refused",
			rows:    []row{mkRow("alpha", withProject, tools, func(r *row) { r.Artifact.IsMarketplace = true })},
			refusal: "only organization Marketplace resources can be excluded as a whole; use the toggle menu for the others",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := sourceToggleTestModel(tt.rows)
			cursorOnRow(t, &m, "alpha")
			toggle, refusal := m.sourceToggleAtCursor()
			if refusal != tt.refusal {
				t.Fatalf("refusal = %q, want %q", refusal, tt.refusal)
			}
			if toggle.exclude != tt.exclude || !slices.Equal(toggle.edits, tt.edits) {
				t.Fatalf("toggle = exclude %v edits %v, want %v %v", toggle.exclude, toggle.edits, tt.exclude, tt.edits)
			}
			if tt.refusal == "" && (toggle.entry.URL != "/repo/plugins/tools" || toggle.label != "tools") {
				t.Fatalf("toggle source = %q label %q", toggle.entry.URL, toggle.label)
			}
		})
	}
}

func sourceToggleMarketplaceRow(name, url string, source config.Source, excluded bool) row {
	r := mkRow(name, withGlobal, withKind(ir.KindSkill), withRegistry(url, "market"), withRegistryMeta("v1", "skills"))
	r.Artifact.IsMarketplace = true
	r.Artifact.Source = source
	r.MarketplaceSelections = map[config.Source]marketplaceSelection{
		source: {
			Entry: config.MarketplaceEntry{URL: url, Source: source},
			Pin:   config.MarketplaceResourcePin{RegistrySlug: "tools", Kind: "skill", Name: name},
		},
	}
	r.ExcludedByUser = excluded
	return r
}

func TestSourceToggleAtCursorMarketplaceExcludesWholeOrganizationSource(t *testing.T) {
	primary := sourceToggleMarketplaceRow("alpha", "https://market.example", config.Organization, false)
	rows := []row{
		primary,
		sourceToggleMarketplaceRow("beta", "https://market.example", config.Organization, false),
		sourceToggleMarketplaceRow("required", "https://market.example", config.Organization, false),
		sourceToggleMarketplaceRow("user-installed", "https://market.example", config.UserPersonal, false),
		sourceToggleMarketplaceRow("other-source", "https://other.example", config.Organization, false),
	}
	rows[2].Artifact.Required = true
	discovery := sourceToggleMarketplaceRow("discovery", "https://market.example", config.Organization, false)
	discovery.MarketplaceDiscovery = &marketplaceDiscovery{}
	rows = append(rows, discovery)

	m := sourceToggleTestModel(rows)
	cursorOnRow(t, &m, "alpha")
	toggle, refusal := m.sourceToggleAtCursor()
	if refusal != "" {
		t.Fatalf("Marketplace source refusal = %q", refusal)
	}
	if !toggle.exclude || toggle.resources != 2 || len(toggle.marketplace) != 2 {
		t.Fatalf("Marketplace source toggle = %+v, want exclusion of 2 resources", toggle)
	}
	for i, want := range []string{"alpha", "beta"} {
		act := toggle.marketplace[i]
		if act.skillName != want || !act.enable || act.scope != scopeUserExclude || !act.allowMarketplaceExclude {
			t.Fatalf("Marketplace action %d = %+v", i, act)
		}
	}
}

// pressSourceExclusion presses x to open the whole-source menu, then e.
func pressSourceExclusion(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(keyPress('x'))
	if m = updated.(Model); cmd != nil || m.pane != paneGroupToggleModal {
		t.Fatalf("x did not open the whole-source menu: pane %v", m.pane)
	}
	updated, cmd = m.Update(keyPress('e'))
	return updated.(Model), cmd
}

// TestSourceToggleKeyStartsMarketplaceToggle guards the key handlers: a
// Marketplace toggle carries user overrides instead of local-config edits.
func TestSourceToggleKeyStartsMarketplaceToggle(t *testing.T) {
	m := sourceToggleTestModel([]row{
		sourceToggleMarketplaceRow("alpha", "https://market.example", config.Organization, false),
		sourceToggleMarketplaceRow("beta", "https://market.example", config.Organization, false),
	})
	cursorOnRow(t, &m, "alpha")
	m, cmd := pressSourceExclusion(t, m)
	if cmd == nil || m.operations.active == nil || m.pane != paneList {
		t.Fatalf("x, e did not start the Marketplace source toggle; toast %q", m.toast)
	}
}

func TestSourceToggleAtCursorMarketplaceRestoresWholeOrganizationSource(t *testing.T) {
	m := sourceToggleTestModel([]row{
		sourceToggleMarketplaceRow("alpha", "https://market.example", config.Organization, true),
		sourceToggleMarketplaceRow("beta", "https://market.example", config.Organization, true),
	})
	cursorOnRow(t, &m, "alpha")

	toggle, refusal := m.sourceToggleAtCursor()
	if refusal != "" {
		t.Fatalf("Marketplace restore refusal = %q", refusal)
	}
	if toggle.exclude || toggle.resources != 2 || len(toggle.marketplace) != 2 {
		t.Fatalf("Marketplace restore toggle = %+v, want removal of 2 resources", toggle)
	}
	for _, act := range toggle.marketplace {
		if act.enable {
			t.Fatalf("Marketplace restore action enables resource: %+v", act)
		}
	}
}

func TestSourceToggleAtCursorRefusesNonOverridableMarketplaceAndDiscovery(t *testing.T) {
	user := sourceToggleMarketplaceRow("user-installed", "https://market.example", config.UserPersonal, false)
	m := sourceToggleTestModel([]row{user})
	cursorOnRow(t, &m, "user-installed")
	if _, refusal := m.sourceToggleAtCursor(); refusal != "only organization Marketplace resources can be excluded as a whole; use the toggle menu for the others" {
		t.Fatalf("non-overridable Marketplace refusal = %q", refusal)
	}

	discovery := sourceToggleMarketplaceRow("search-result", "https://market.example", config.Organization, false)
	discovery.MarketplaceDiscovery = &marketplaceDiscovery{}
	m = sourceToggleTestModel([]row{discovery})
	cursorOnRow(t, &m, "search-result")
	if _, refusal := m.sourceToggleAtCursor(); refusal != "Marketplace search results are not installed; install them from the toggle menu" {
		t.Fatalf("Marketplace discovery refusal = %q", refusal)
	}
}

// spec: tui-source-toggle/organization-marketplace-source-uses-user-overrides
func TestRunSourceToggleAtMarketplaceRoundTripUserOverrides(t *testing.T) {
	home, repoRoot := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	userPath, err := config.GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(userPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	rows := []row{
		sourceToggleMarketplaceRow("alpha", "https://market.example", config.Organization, false),
		sourceToggleMarketplaceRow("beta", "https://market.example", config.Organization, false),
	}
	m := sourceToggleTestModel(rows)
	cursorOnRow(t, &m, "alpha")
	toggle, refusal := m.sourceToggleAtCursor()
	if refusal != "" {
		t.Fatalf("Marketplace write refusal = %q", refusal)
	}
	changed, err := runSourceToggleAt(repoRoot, toggle, nil)
	if err != nil || !changed {
		t.Fatalf("Marketplace exclusion write changed=%v err=%v", changed, err)
	}
	user, err := config.LoadConfigFromPath(userPath)
	if err != nil {
		t.Fatal(err)
	}
	locators := make(map[string]bool)
	for _, exclusion := range user.MarketplaceExclusions {
		locators[exclusion.Locator] = true
	}
	for _, name := range []string{"alpha", "beta"} {
		if !locators["tools/skill/"+name] {
			t.Fatalf("user exclusions = %#v, missing %s", user.MarketplaceExclusions, name)
		}
	}

	for i := range m.rows {
		m.rows[i].ExcludedByUser = true
	}
	toggle, refusal = m.marketplaceSourceToggle(m.rows[0])
	if refusal != "" || toggle.exclude {
		t.Fatalf("Marketplace restore toggle = %+v, refusal %q", toggle, refusal)
	}
	changed, err = runSourceToggleAt(repoRoot, toggle, nil)
	if err != nil || !changed {
		t.Fatalf("Marketplace restore write changed=%v err=%v", changed, err)
	}
	user, err = config.LoadConfigFromPath(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(user.MarketplaceExclusions) != 0 {
		t.Fatalf("user exclusions after restore = %#v", user.MarketplaceExclusions)
	}
}

func TestSourceToggleAtCursorOnSections(t *testing.T) {
	rows := []row{
		mkRow("alpha", withProject, withRegistry("/repo/plugins/tools", "tools")),
		mkRow("beta", withProject, withKind(ir.KindCommand), withRegistry("/repo/plugins/tools", "tools")),
		mkRow("gamma", withProject, withRegistry("/elsewhere/tools", "tools")),
		mkRow("delta", withProject, withRegistry("/repo/plugins/other", "other"), withRegistryMeta("main", "")),
	}
	m := sourceToggleTestModel(rows)

	cursorOnSection(t, &m, "Enabled")
	if _, refusal := m.sourceToggleAtCursor(); refusal != "this group holds several sources; select a resource to toggle its source" {
		t.Fatalf("mixed group refusal = %q", refusal)
	}

	cursorOnRow(t, &m, "alpha")
	if toggle, refusal := m.sourceToggleAtCursor(); refusal != "" || toggle.resources != 2 {
		t.Fatalf("row toggle should cover every kind of its source: %+v, refusal %q", toggle, refusal)
	}
}

func TestSourceToggleKeyRefusesWithToast(t *testing.T) {
	m := sourceToggleTestModel([]row{mkRow("alpha", withRequired, withRegistry("/repo/plugins/tools", "tools"))})
	cursorOnRow(t, &m, "alpha")
	updated, cmd := m.Update(keyPress('x'))
	m = updated.(Model)
	if cmd == nil || m.operations.active != nil {
		t.Fatalf("refused toggle started an operation or showed nothing: %#v", m.operations.active)
	}
	if msg, ok := cmd().(toastMsg); !ok || msg.text != "required sources cannot be toggled" {
		t.Fatalf("refusal message = %#v", msg)
	}
}

func TestSectionHeaderMarksExcludedSource(t *testing.T) {
	excluded := flatItem{Kind: flatSection, SectionTitle: "Excluded", SectionCount: 2, SectionExcluded: true}
	if line := formatFlatItem(excluded, 60, false); !strings.HasSuffix(line, "Excluded  (2 excluded)") {
		t.Fatalf("excluded section header = %q", line)
	}
	enabled := flatItem{Kind: flatSection, SectionTitle: "Project", SectionCount: 1}
	if line := formatFlatItem(enabled, 60, false); !strings.HasSuffix(line, "Project  (1)") {
		t.Fatalf("enabled section header = %q", line)
	}
	if got := mkRow("alpha", withAvailable, func(r *row) { r.SourceExcludedLocally = true }).StatusText(); got != "excluded (whole source, local)" {
		t.Fatalf("status text = %q", got)
	}
}

// sourceToggleFixture is a repository whose v4 project config selects every
// resource of two directory sources, tools (alpha, beta) and other (delta).
type sourceToggleFixture struct {
	t         *testing.T
	repo      string
	home      string
	localPath string
	toolsURL  string
}

func newSourceToggleFixture(t *testing.T) *sourceToggleFixture {
	t.Helper()
	f := &sourceToggleFixture{t: t, home: t.TempDir(), repo: t.TempDir()}
	t.Setenv("HOME", f.home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(f.home, ".state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(f.home, ".cache"))
	t.Setenv("TMPDIR", t.TempDir())
	for _, resource := range []string{"tools/skills/alpha/SKILL.md", "tools/skills/beta/SKILL.md", "other/skills/delta/SKILL.md"} {
		f.write(filepath.Join(f.repo, "plugins", resource), "# "+filepath.Base(filepath.Dir(resource))+"\n")
	}
	f.write(config.ProjectConfigPath(f.repo), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  tools:\n    directory: ./plugins/tools\n  other:\n    directory: ./plugins/other\nresources:\n  tools:\n    all: true\n  other:\n    all: true\n")
	f.localPath = config.LocalConfigPath(f.repo)

	originalOrg, originalGlobal := loadOrgConfig, loadGlobalConfig
	originalRepo, originalLocal := loadRepoConfig, loadLocalConfig
	t.Cleanup(func() {
		loadOrgConfig, loadGlobalConfig = originalOrg, originalGlobal
		loadRepoConfig, loadLocalConfig = originalRepo, originalLocal
	})
	loadOrgConfig = func() (*config.Config, error) { return nil, nil }
	loadGlobalConfig = func() (*config.Config, error) { return nil, nil }
	loadRepoConfig = func() (*config.Config, error) { return config.LoadConfigFromPath(config.ProjectConfigPath(f.repo)) }
	loadLocalConfig = func(string) (*config.LocalConfig, error) { return config.LoadLocalConfigFromPath(f.localPath) }
	f.toolsURL = findV4ToggleRow(t, f.snapshot().rows, "alpha").RegistryURL
	return f
}

func (f *sourceToggleFixture) write(path, content string) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *sourceToggleFixture) snapshot() snapshotLoadedMsg {
	f.t.Helper()
	loaded := readSnapshot(context.Background(), f.repo, f.home, catalogLoadCached)
	if len(loaded.errs) > 0 {
		f.t.Fatalf("snapshot errors: %v", loaded.errs)
	}
	return loaded
}

// press presses x on alpha through the staged dependency mutation, applies
// the result to the model, and returns the message with the next snapshot.
func (f *sourceToggleFixture) press(loaded snapshotLoadedMsg) (configWrittenMsg, snapshotLoadedMsg) {
	t := f.t
	t.Helper()
	reconcile := func(_ context.Context, _ string, _ string, _ bool, edits []pipeline.StagedConfigEdit) (pipeline.DependencyLockView, error) {
		for _, edit := range edits {
			if err := os.WriteFile(edit.Path, edit.Bytes, edit.Mode); err != nil {
				return pipeline.DependencyLockView{}, err
			}
		}
		return pipeline.DependencyLockView{}, nil
	}
	mutate := func(ctx context.Context, ws pipeline.Workspace, mutation func(pipeline.ConfigPathMapper) error) error {
		return pipeline.MutateConfigWithDependencies(ctx, ws.RepoRoot(), ws.LocalConfigDir(), mutation, reconcile)
	}
	m := NewModel(f.repo, f.home, WithDependencyLifecycle(DependencyLifecycle{Mutate: mutate}))
	m.loading = false
	m.rows, m.filteredRows, m.mergedCfg = loaded.rows, loaded.rows, loaded.mergedCfg
	m.rebuildFlatItems()
	cursorOnRow(t, &m, "alpha")
	m, cmd := pressSourceExclusion(t, m)
	if cmd == nil || m.operations.active == nil {
		t.Fatalf("x, e did not start a source toggle; toast %q", m.toast)
	}
	msg, ok := cmd().(configWrittenMsg)
	if !ok || msg.err != nil {
		t.Fatalf("source toggle message = %#v", msg)
	}
	updated, _ := m.Update(msg)
	if m = updated.(Model); m.toast != msg.summary {
		t.Fatalf("toast = %q, want summary %q", m.toast, msg.summary)
	}
	assertNoConfigStaging(t)
	return msg, f.snapshot()
}

func (f *sourceToggleFixture) assertTools(loaded snapshotLoadedMsg, available, sourceExcluded bool) {
	t := f.t
	t.Helper()
	for _, name := range []string{"alpha", "beta"} {
		r := findV4ToggleRow(t, loaded.rows, name)
		if r.IsAvailable != available || r.SourceExcludedLocally != sourceExcluded {
			t.Fatalf("%s: available %v source excluded %v, want %v %v", name, r.IsAvailable, r.SourceExcludedLocally, available, sourceExcluded)
		}
	}
	if delta := findV4ToggleRow(t, loaded.rows, "delta"); delta.IsAvailable || delta.SourceExcludedLocally {
		t.Fatalf("other source changed: %+v", delta)
	}
	if !strings.Contains(string(mustReadFile(t, config.ProjectConfigPath(f.repo))), "all: true") {
		t.Fatal("project config changed")
	}
}

// TestSourceToggleRoundTripOnLegacyLocalConfig starts from a legacy local
// config that excludes every resource of one source individually.
func TestSourceToggleRoundTripOnLegacyLocalConfig(t *testing.T) {
	f := newSourceToggleFixture(t)
	f.write(f.localPath, "registries:\n  - url: "+f.toolsURL+"\n    skills:\n      exclude:\n        - alpha\n        - beta\n")
	excluded := f.snapshot()
	if alpha := findV4ToggleRow(t, excluded.rows, "alpha"); !alpha.IsAvailable || !alpha.ExcludedLocally {
		t.Fatalf("fixture did not exclude alpha individually: %+v", alpha)
	}

	msg, restored := f.press(excluded)
	if msg.summary != "Removed local exclusions of source tools" {
		t.Fatalf("restore summary = %q", msg.summary)
	}
	if local := string(mustReadFile(t, f.localPath)); strings.Contains(local, "alpha") || strings.Contains(local, f.toolsURL) {
		t.Fatalf("individual exclusions remain:\n%s", local)
	}
	f.assertTools(restored, false, false)

	msg, sourceOff := f.press(restored)
	if msg.summary != "Excluded source tools locally (2 resources); press x, e to undo" {
		t.Fatalf("exclude summary = %q", msg.summary)
	}
	if local := string(mustReadFile(t, f.localPath)); !strings.Contains(local, "exclude_all: true") {
		t.Fatalf("local config lacks exclude_all:\n%s", local)
	}
	f.assertTools(sourceOff, true, true)

	_, sourceOn := f.press(sourceOff)
	f.assertTools(sourceOn, false, false)
}

// TestSourceToggleRoundTripOnV4LocalConfig writes the "*" exclusion into a
// v4 local root and removes it again together with the source it declared.
// spec: tui-source-toggle/local-whole-source-exclusion-restores-selections
func TestSourceToggleRoundTripOnV4LocalConfig(t *testing.T) {
	f := newSourceToggleFixture(t)
	const original = "apiVersion: agent-marketplace/v4\n"
	f.write(f.localPath, original)

	msg, sourceOff := f.press(f.snapshot())
	if !strings.HasPrefix(msg.summary, "Excluded source tools locally") {
		t.Fatalf("exclude summary = %q", msg.summary)
	}
	local := string(mustReadFile(t, f.localPath))
	if !strings.Contains(local, "sources:") || !strings.Contains(local, "- '*'") && !strings.Contains(local, `- "*"`) {
		t.Fatalf("v4 local config lacks the source exclusion:\n%s", local)
	}
	f.assertTools(sourceOff, true, true)

	msg, sourceOn := f.press(sourceOff)
	if msg.summary != "Removed local exclusions of source tools" {
		t.Fatalf("restore summary = %q", msg.summary)
	}
	if local := string(mustReadFile(t, f.localPath)); strings.Contains(local, "sources:") || strings.Contains(local, "*") {
		t.Fatalf("v4 local config kept the exclusion or its source:\n%s", local)
	}
	f.assertTools(sourceOn, false, false)
}
