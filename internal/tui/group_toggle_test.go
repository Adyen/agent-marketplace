package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

func runGroupToggleAt(repoRoot string, toggle groupToggle, mapper pipeline.ConfigPathMapper) (bool, error) {
	return runGroupToggleInWorkspace(context.Background(), repoRoot, repoRoot, toggle, mapper)
}

// spec: tui-source-toggle/resource-or-single-source-section-opens-the-menu
func TestSourceMenuKeyOpensGroupToggleModal(t *testing.T) {
	for _, onRow := range []bool{false, true} {
		m := sourceToggleTestModel([]row{
			mkRow("alpha", withProject, withRegistry("https://example.test/tools.git", "tools")),
			mkRow("beta", withProject, withRegistry("https://example.test/tools.git", "tools")),
		})
		if onRow {
			cursorOnRow(t, &m, "beta")
		} else {
			cursorOnSection(t, &m, "Enabled")
		}

		updated, cmd := m.Update(keyPress('x'))
		m = updated.(Model)
		if cmd != nil || m.pane != paneGroupToggleModal {
			t.Fatalf("x (on row %v) = pane %v cmd %v, want group modal and no cmd", onRow, m.pane, cmd)
		}
		if m.modalSource.RegistryURL != "https://example.test/tools.git" {
			t.Fatalf("modal source = %#v", m.modalSource)
		}
	}
}

func TestSpaceOnSectionHeadersFolds(t *testing.T) {
	m := sourceToggleTestModel([]row{
		mkRow("alpha", withProject, withRegistry("https://example.test/tools.git", "tools")),
	})
	cursorOnSection(t, &m, "Enabled")
	path := m.flatItems[m.cursor].SectionPath
	updated, cmd := m.Update(keyNamed("space"))
	m = updated.(Model)
	if cmd != nil || m.pane != paneList {
		t.Fatalf("space on status section = pane %v cmd %v", m.pane, cmd)
	}
	if !m.currentFoldState()[path] {
		t.Fatal("space did not fold status section")
	}
}

// spec: tui-source-toggle/mixed-or-protected-sources-are-refused
func TestSourceMenuKeyRefusals(t *testing.T) {
	t.Run("several sources", func(t *testing.T) {
		m := sourceToggleTestModel([]row{
			mkRow("alpha", withProject, withRegistry("https://example.test/one.git", "tools")),
			mkRow("beta", withProject, withRegistry("https://example.test/two.git", "tools")),
		})
		cursorOnSection(t, &m, "Enabled")
		updated, cmd := m.Update(keyPress('x'))
		m = updated.(Model)
		if m.pane != paneList || cmd == nil {
			t.Fatalf("mixed-source group = pane %v cmd %v", m.pane, cmd)
		}
		if got := cmd().(toastMsg).text; got != "this group holds several sources; select a resource to toggle its source" {
			t.Fatalf("mixed-source refusal = %q", got)
		}
	})

	t.Run("all required", func(t *testing.T) {
		m := sourceToggleTestModel([]row{
			mkRow("alpha", withRequired, withGlobal, withRegistry("https://example.test/tools.git", "tools")),
			mkRow("beta", withRequired, withGlobal, withRegistry("https://example.test/tools.git", "tools")),
		})
		cursorOnSection(t, &m, "Required")
		updated, cmd := m.Update(keyPress('x'))
		m = updated.(Model)
		if m.pane != paneList || cmd == nil {
			t.Fatalf("required group = pane %v cmd %v", m.pane, cmd)
		}
		if got := cmd().(toastMsg).text; got != "required sources cannot be toggled" {
			t.Fatalf("required refusal = %q", got)
		}
	})

	t.Run("stale rows", func(t *testing.T) {
		m := sourceToggleTestModel([]row{
			mkRow("alpha", withProject, withRegistry("https://example.test/tools.git", "tools")),
		})
		m.rowsStale = true
		cursorOnSection(t, &m, "Enabled")
		updated, cmd := m.Update(keyPress('x'))
		m = updated.(Model)
		if m.pane != paneGroupToggleModal || cmd != nil {
			t.Fatalf("stale group = pane %v cmd %v", m.pane, cmd)
		}
	})

	t.Run("syncing", func(t *testing.T) {
		m := sourceToggleTestModel([]row{
			mkRow("alpha", withProject, withRegistry("https://example.test/tools.git", "tools")),
		})
		m.syncing = true
		cursorOnSection(t, &m, "Enabled")
		updated, cmd := m.Update(keyPress('x'))
		m = updated.(Model)
		if cmd != nil || m.pane != paneList {
			t.Fatalf("syncing group = pane %v cmd %v", m.pane, cmd)
		}
	})
}

func TestGroupToggleForScopesRequiredRowsAndIdentity(t *testing.T) {
	target := mkRow("alpha", withProject, withRegistry("https://example.test/tools.git", "tools"))
	target.RegistryRef = ""
	target.RegistryPath = "skills"
	required := mkRow("required", withRequired, withGlobal, withRegistry("https://example.test/tools.git", "tools"))
	required.RegistryRef = "main"
	required.RegistryPath = "skills"
	other := mkRow("beta", withProject, withRegistry("https://example.test/tools.git", "tools"))
	other.RegistryRef = "main"
	other.RegistryPath = "skills"
	m := sourceToggleTestModel([]row{target, required, other})

	toggle, refusal := m.groupToggleFor(target, scopeGlobal)
	if refusal != "" {
		t.Fatalf("global group toggle refusal = %q", refusal)
	}
	if !toggle.enable || toggle.resources != 2 || toggle.entry.URL != target.RegistryURL ||
		toggle.entry.Ref != "main" || toggle.entry.Path != "skills" {
		t.Fatalf("global group toggle = %+v", toggle)
	}

	toggle, refusal = m.groupToggleFor(target, scopeProject)
	if refusal != "" || toggle.enable {
		t.Fatalf("project group toggle = %+v, refusal %q; all eligible rows should disable", toggle, refusal)
	}

	relative := target
	relative.RegistryURL = "./plugins/tools"
	relative.RegistryRef = ""
	relative.RegistryPath = ""
	m = sourceToggleTestModel([]row{relative})
	_, refusal = m.groupToggleFor(relative, scopeGlobal)
	if refusal != `cannot write repo-relative registry "./plugins/tools" to the global config; use an absolute path or a git remote` {
		t.Fatalf("relative global refusal = %q", refusal)
	}
}

func marketplaceGroupRow(name string, source config.Source, enabled bool) row {
	r := mkRow(name, withRegistry("https://market.example", "market"))
	r.Artifact.IsMarketplace = true
	r.Artifact.Source = source
	r.EnabledProject = enabled
	r.MarketplaceSelections = map[config.Source]marketplaceSelection{
		source: {
			Entry: config.MarketplaceEntry{URL: "https://market.example", Source: source},
			Pin: config.MarketplaceResourcePin{
				RegistrySlug: "tools",
				Kind:         "skill",
				Name:         name,
				Direct:       true,
			},
		},
	}
	return r
}

func TestMarketplaceGroupToggleRefusalsAndActions(t *testing.T) {
	t.Run("organization overrides", func(t *testing.T) {
		m := sourceToggleTestModel([]row{
			sourceToggleMarketplaceRow("alpha", "https://market.example", config.Organization, false),
			sourceToggleMarketplaceRow("beta", "https://market.example", config.Organization, false),
		})
		toggle, refusal := m.marketplaceGroupToggle(m.rows[0], scopeProject)
		if len(toggle.marketplace) != 0 || !strings.Contains(refusal, "[e] user override") {
			t.Fatalf("organization group = %+v, refusal %q", toggle, refusal)
		}
	})

	t.Run("collection only", func(t *testing.T) {
		collectionRow := marketplaceGroupRow("collection", config.RepoCommitted, false)
		collectionRow.MarketplaceCollectionOnly = true
		m := sourceToggleTestModel([]row{collectionRow})
		_, refusal := m.marketplaceGroupToggle(collectionRow, scopeProject)
		if refusal != "collection-selected Marketplace items must be toggled through their collection" {
			t.Fatalf("collection refusal = %q", refusal)
		}
	})

	t.Run("direct pins differ", func(t *testing.T) {
		rows := []row{
			marketplaceGroupRow("user", config.UserPersonal, false),
			marketplaceGroupRow("project", config.RepoCommitted, true),
			marketplaceGroupRow("local", config.RepoPersonal, false),
		}
		m := sourceToggleTestModel(rows)
		toggle, refusal := m.marketplaceGroupToggle(rows[0], scopeProject)
		if refusal != "" || !toggle.enable || len(toggle.marketplace) != 2 {
			t.Fatalf("direct group = %+v, refusal %q", toggle, refusal)
		}
		for _, act := range toggle.marketplace {
			if !act.enable || (act.skillName != "user" && act.skillName != "local") {
				t.Fatalf("direct action = %+v", act)
			}
			if !act.hasMarketplaceSelection {
				t.Fatalf("direct action lacks selection: %+v", act)
			}
		}
	})
}

func TestHandleGroupToggleModalKey(t *testing.T) {
	gitRows := []row{
		mkRow("alpha", withProject, withRegistry("https://example.test/tools.git", "tools")),
		mkRow("beta", withProject, withRegistry("https://example.test/tools.git", "tools")),
	}
	for _, key := range []rune{'g', 'p', 'l'} {
		t.Run(string(key), func(t *testing.T) {
			m := sourceToggleTestModel(gitRows)
			m.pane = paneGroupToggleModal
			m.modalSource = gitRows[0]
			updated, cmd := m.Update(keyPress(key))
			m = updated.(Model)
			if cmd == nil || m.pane != paneList {
				t.Fatalf("key %q = pane %v cmd %v", key, m.pane, cmd)
			}
		})
	}

	t.Run("esc", func(t *testing.T) {
		m := sourceToggleTestModel(gitRows)
		m.pane = paneGroupToggleModal
		m.modalSource = gitRows[0]
		updated, cmd := m.Update(keyNamed("esc"))
		m = updated.(Model)
		if cmd != nil || m.pane != paneList {
			t.Fatalf("esc = pane %v cmd %v", m.pane, cmd)
		}
	})

	t.Run("e delegates Git source", func(t *testing.T) {
		m := sourceToggleTestModel(gitRows)
		m.pane = paneGroupToggleModal
		m.modalSource = gitRows[0]
		updated, cmd := m.Update(keyPress('e'))
		m = updated.(Model)
		if cmd == nil || m.pane != paneList || m.operations.active == nil {
			t.Fatalf("Git e = pane %v cmd %v operation %v", m.pane, cmd, m.operations.active)
		}
	})

	t.Run("e refuses machine source", func(t *testing.T) {
		machineRow := mkRow("alpha", withGlobal, withRegistry("https://example.test/tools.git", "tools"))
		machineRow.Artifact.Source = config.Organization
		m := sourceToggleTestModel([]row{machineRow})
		m.pane = paneGroupToggleModal
		m.modalSource = machineRow
		updated, cmd := m.Update(keyPress('e'))
		m = updated.(Model)
		if cmd == nil || m.pane != paneGroupToggleModal {
			t.Fatalf("machine e = pane %v cmd %v", m.pane, cmd)
		}
		if got := cmd().(toastMsg).text; got != "local config cannot exclude a source from global or org config" {
			t.Fatalf("machine refusal = %q", got)
		}
	})

	t.Run("refusal keeps toast command", func(t *testing.T) {
		requiredRow := mkRow("required", withRequired, withGlobal, withRegistry("https://example.test/tools.git", "tools"))
		m := sourceToggleTestModel([]row{requiredRow})
		m.pane = paneGroupToggleModal
		m.modalSource = requiredRow
		updated, cmd := m.Update(keyPress('g'))
		m = updated.(Model)
		if cmd == nil || m.pane != paneGroupToggleModal {
			t.Fatalf("required refusal = pane %v cmd %v", m.pane, cmd)
		}
		if got := cmd().(toastMsg).text; got != "required sources cannot be toggled" {
			t.Fatalf("required group refusal = %q", got)
		}
	})
}

func TestRunGroupToggleAtLegacyLocalRoundTrip(t *testing.T) {
	repo := t.TempDir()
	source := filepath.Join(repo, "plugins", "tools")
	path := config.LocalConfigPath(repo)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("registries:\n  - url: "+source+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	toggle := groupToggle{
		entry:     config.RegistryEntry{URL: source},
		label:     "tools",
		scope:     scopeLocal,
		enable:    true,
		resources: 2,
	}
	changed, err := runGroupToggleAt(repo, toggle, nil)
	if err != nil || !changed {
		t.Fatalf("legacy enable changed=%v err=%v", changed, err)
	}
	data := string(mustReadFile(t, path))
	if !strings.Contains(data, "default: true") {
		t.Fatalf("legacy enable config =\n%s", data)
	}

	toggle.enable = false
	changed, err = runGroupToggleAt(repo, toggle, nil)
	if err != nil || !changed {
		t.Fatalf("legacy disable changed=%v err=%v", changed, err)
	}
	data = string(mustReadFile(t, path))
	if strings.Contains(data, "default: true") {
		t.Fatalf("legacy disable config =\n%s", data)
	}
}

// spec: tui-source-toggle/whole-source-scope-selection-round-trips
func TestRunGroupToggleAtV4ProjectRoundTripAndSummary(t *testing.T) {
	repo := t.TempDir()
	path := config.ProjectConfigPath(repo)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	const source = "https://example.test/tools.git"
	original := "apiVersion: agent-marketplace/v4\nsources:\n  tools:\n    git: " + source + "\n    ref: main\nresources:\n  tools:\n    include:\n      - skill/alpha\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	toggle := groupToggle{
		entry:     config.RegistryEntry{URL: source, Ref: "main", V4Kind: config.V4SourceGit},
		label:     "tools",
		scope:     scopeProject,
		enable:    true,
		resources: 2,
	}
	changed, err := runGroupToggleAt(repo, toggle, nil)
	if err != nil || !changed {
		t.Fatalf("v4 enable changed=%v err=%v", changed, err)
	}
	if data := string(mustReadFile(t, path)); !strings.Contains(data, "all: true") {
		t.Fatalf("v4 enable config =\n%s", data)
	}

	toggle.enable = false
	changed, err = runGroupToggleAt(repo, toggle, nil)
	if err != nil || !changed {
		t.Fatalf("v4 disable changed=%v err=%v", changed, err)
	}
	if data := string(mustReadFile(t, path)); strings.Contains(data, "all: true") {
		t.Fatalf("v4 disable config =\n%s", data)
	}

	noSelection := toggle
	noSelection.enable = false
	noSelection.scope = scopeLocal
	none := t.TempDir()
	noneWorkspace, err := pipeline.NewWorkspace(none, none)
	if err != nil {
		t.Fatal(err)
	}
	msg, ok := runDependencyGroupToggle(context.Background(), noneWorkspace, noSelection, nil)().(configWrittenMsg)
	if !ok || msg.err != nil || msg.summary != "No local selections of tools to change" {
		t.Fatalf("no-selection summary = %#v", msg)
	}
}

func TestRunDependencyGroupToggleSummaries(t *testing.T) {
	repo := t.TempDir()
	source := filepath.Join(repo, "plugins", "tools")
	path := config.LocalConfigPath(repo)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("registries:\n  - url: "+source+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	toggle := groupToggle{entry: config.RegistryEntry{URL: source}, label: "tools", scope: scopeLocal, enable: true, resources: 2}
	workspace, err := pipeline.NewWorkspace(repo, repo)
	if err != nil {
		t.Fatal(err)
	}
	msg := runDependencyGroupToggle(context.Background(), workspace, toggle, nil)().(configWrittenMsg)
	if msg.err != nil || msg.summary != "Enabled source tools in local config (2 resources)" {
		t.Fatalf("enable summary = %#v", msg)
	}
	toggle.enable = false
	msg = runDependencyGroupToggle(context.Background(), workspace, toggle, nil)().(configWrittenMsg)
	if msg.err != nil || msg.summary != "Disabled source tools in local config (2 resources)" {
		t.Fatalf("disable summary = %#v", msg)
	}
}

func TestRenderGroupToggleModal(t *testing.T) {
	target := mkRow("alpha", withProject, withRegistry("https://example.test/tools.git", "tools"))
	second := mkRow("beta", withLocal, withRegistry("https://example.test/tools.git", "tools"))
	m := sourceToggleTestModel([]row{target, second})
	m.modalSource = target
	out := ansi.Strip(renderGroupToggleModal(m))
	for _, want := range []string{`Source "tools"`, "1/2", "[g]", "[p]", "[l]", "[e]", "esc"} {
		if !strings.Contains(out, want) {
			t.Fatalf("group modal missing %q:\n%s", want, out)
		}
	}

	org := sourceToggleMarketplaceRow("alpha", "https://market.example", config.Organization, false)
	org2 := sourceToggleMarketplaceRow("beta", "https://market.example", config.Organization, false)
	m = sourceToggleTestModel([]row{org, org2})
	m.modalSource = org
	out = ansi.Strip(renderGroupToggleModal(m))
	for _, hidden := range []string{"[g]", "[p]", "[l]"} {
		if strings.Contains(out, hidden) {
			t.Fatalf("organization-only modal contains %s:\n%s", hidden, out)
		}
	}
	if !strings.Contains(out, "[e]") || !strings.Contains(out, "esc") {
		t.Fatalf("organization-only modal lost escape actions:\n%s", out)
	}

	m = sourceToggleTestModel([]row{target, second})
	m.modalSource = target
	m.width, m.height = 50, 12
	out = ansi.Strip(renderGroupToggleModal(m))
	if !strings.Contains(out, "[e]") || !strings.Contains(out, "esc") {
		t.Fatalf("compact group modal lost actions:\n%s", out)
	}
}
