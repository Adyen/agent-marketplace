package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"
)

// spec: tui-navigation/single-status-list-at-minimum-size
// spec: session-navigation/status-grouped-resource-order-and-filtering
func TestSingleStatusGroupingWithoutTabBar(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("req-skill", withRequired, withProject, withRegistry("sec.git", "security")),
		mkRow("enabled-skill", withProject, withRegistry("team.git", "team")),
		mkRow("available-skill", withAvailable, withRegistry("team.git", "team")),
		mkRow("bad-skill", withMalformed("yaml parse error")),
	}
	m.width, m.height = 120, 24
	m.recomputeView()

	var sections []string
	for _, it := range m.flatItems {
		if it.Kind != flatSection {
			continue
		}
		sections = append(sections, it.SectionPath)
		if !strings.HasPrefix(it.SectionPath, "status:") {
			t.Fatalf("only status:* sections allowed, got %q in %v", it.SectionPath, sections)
		}
	}
	wantOrder := []string{"status:required", "status:enabled", "status:available", "status:malformed"}
	if len(sections) != len(wantOrder) {
		t.Fatalf("sections = %v, want %v", sections, wantOrder)
	}
	for i, want := range wantOrder {
		if sections[i] != want {
			t.Fatalf("sections = %v, want %v", sections, wantOrder)
		}
	}

	screen := ansi.Strip(m.viewContent())
	for _, tabLabel := range []string{"Scope", "Registry", "Status"} {
		if strings.Contains(screen, tabLabel) {
			t.Fatalf("tab-bar label %q must not render; screen:\n%s", tabLabel, screen)
		}
	}
}

// spec: tui-navigation/single-status-list-at-minimum-size
func TestSingleStatusReclaimsTabRowAtMinimumSize(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.width, m.height = 50, 12
	m.rows = []row{mkRow("a", withProject, withRegistry("t.git", "team"))}
	m.recomputeView()

	if got := m.listVisibleHeight(); got != 10 {
		t.Fatalf("listVisibleHeight at 50x12 = %d, want 10 (tab row reclaimed)", got)
	}
	screen := ansi.Strip(m.viewContent())
	if lines := strings.Count(screen, "\n") + 1; lines > 12 {
		t.Fatalf("50x12 screen overflowed: %d lines:\n%s", lines, screen)
	}
}

// spec: tui-rendering/aligned-safe-labels-at-normal-and-narrow-sizes
func TestSingleStatusScopeColumnVariants(t *testing.T) {
	cases := []struct {
		name string
		row  row
		want string
	}{
		{"global", mkRow("audit", withGlobal, withRegistry("sec.git", "security")), "  [x] G   skill security/audit"},
		{"project", mkRow("review", withProject, withRegistry("team.git", "team")), "  [x] P   skill team/review"},
		{"local", mkRow("mine", withLocal, withRegistry("team.git", "team")), "  [x] L   skill team/mine"},
		{"global-project", mkRow("gp", withGlobal, withProject, withRegistry("team.git", "team")), "  [x] GP  skill team/gp"},
		{"global-local", mkRow("gl", withGlobal, withLocal, withRegistry("team.git", "team")), "  [x] GL  skill team/gl"},
		{"project-local", mkRow("pl", withProject, withLocal, withRegistry("team.git", "team")), "  [x] PL  skill team/pl"},
		{"all-scopes", mkRow("gpl", withGlobal, withProject, withLocal, withRegistry("team.git", "team")), "  [x] GPL skill team/gpl"},
		{"project-local-excluded", mkRow("ineff", withProject, withExcluded, withRegistry("team.git", "team")), "  [x] P-  skill team/ineff"},
		{"required", mkRow("mand", withRequired, withProject, withRegistry("sec.git", "security")), "  [R] R   skill security/mand"},
		{"available-excluded-org-pin", mkRow("orgpin", withAvailable, withGlobal, withRegistry("tools.git", "tools")), "  [ ]     skill tools/orgpin"},
		{"available-blank", mkRow("avail", withAvailable, withRegistry("team.git", "team")), "  [ ]     skill team/avail"},
		{"malformed-blank", mkRow("bad", withMalformed("yaml parse error")), "  [!]     skill (local)/bad"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			it := flatItem{Kind: flatArtifact, Row: tc.row, Depth: 1}
			for _, width := range []int{60, 24} {
				if got := formatFlatItem(it, width, false); got != tc.want {
					t.Fatalf("formatFlatItem width %d = %q, want %q", width, got, tc.want)
				}
			}
			plain := ansi.Strip(stylizeFlatItem(it, 60, false))
			if plain != tc.want {
				t.Fatalf("stylizeFlatItem = %q, want %q", plain, tc.want)
			}
		})
	}
}

// spec: tui-rendering/aligned-safe-labels-at-normal-and-narrow-sizes
func TestSingleStatusRegistryFallbackAndSubpathOmitted(t *testing.T) {
	urlOnly := mkRow("dep", withProject)
	urlOnly.RegistryURL = "https://git.example.com/org/platform.git"
	it := flatItem{Kind: flatArtifact, Row: urlOnly, Depth: 1}
	if got, want := formatFlatItem(it, 60, false), "  [x] P   skill platform/dep"; got != want {
		t.Fatalf("URL-basename fallback = %q, want %q", got, want)
	}

	sub := mkRow("pm", withProject, withRegistry("https://git.example.com/org/awesome.git", "awesome"), withRegistryMeta("main", "teams/pm"))
	it = flatItem{Kind: flatArtifact, Row: sub, Depth: 1}
	if got, want := formatFlatItem(it, 60, false), "  [x] P   skill awesome/pm"; got != want {
		t.Fatalf("subpath must be omitted = %q, want %q", got, want)
	}

	local := mkRow("solo", withProject)
	it = flatItem{Kind: flatArtifact, Row: local, Depth: 1}
	if got, want := formatFlatItem(it, 60, false), "  [x] P   skill (local)/solo"; got != want {
		t.Fatalf("(local) fallback = %q, want %q", got, want)
	}
}

// spec: tui-rendering/aligned-safe-labels-at-normal-and-narrow-sizes
func TestSingleStatusHostileAndUnicodeLabels(t *testing.T) {
	hostile := mkRow("uni-\u00e9cho", withProject)
	hostile.RegistryName = "evil\x1b[31m\x07name"
	hostile.RegistryURL = "https://git.example.com/org/evil.git"
	it := flatItem{Kind: flatArtifact, Row: hostile, Depth: 1}
	got := formatFlatItem(it, 60, false)
	want := "  [x] P   skill evil\\x1B[31m\\x07name/uni-écho"
	if got != want {
		t.Fatalf("hostile registry label = %q, want %q", got, want)
	}

	stubColorCapable(t, true)
	m := NewModel("/repo", "/home")
	m.rows = []row{mkRow("déploy", withGlobal, withRegistry("sec.git", "security"))}
	m.filter = "dépl"
	m.recomputeView()
	var target flatItem
	for _, it := range m.flatItems {
		if it.Kind == flatArtifact {
			target = it
		}
	}
	if target.Kind != flatArtifact {
		t.Fatalf("expected a matching artifact row")
	}
	wantLabel := "skill security/" + styleSearchMatch.Render("dépl") + "oy"
	if got := highlightNameMatches(target.Row, "skill security/déploy"); got != wantLabel {
		t.Fatalf("highlighted label = %q, want %q", got, wantLabel)
	}
	line := stylizeFlatItem(target, 60, false)
	if !strings.Contains(line, wantLabel) {
		t.Fatalf("rendered line = %q, want highlighted label %q", ansi.Strip(line), wantLabel)
	}
	if strings.Contains(line, styleSearchMatch.Render("skill security/")) {
		t.Fatalf("prefix must stay unstyled: %q", line)
	}

	hostileMatch := mkRow("déploy", withGlobal)
	hostileMatch.RegistryName = "evil\x1b[31m\x07name"
	hostileMatch.SearchMatchRunes = []int{0, 1, 2, 3}
	wantEsc := "skill evil\\x1B[31m\\x07name/" + styleSearchMatch.Render("dépl") + "oy"
	if got := highlightNameMatches(hostileMatch, "skill evil\\x1B[31m\\x07name/déploy"); got != wantEsc {
		t.Fatalf("escaped-prefix highlighted label = %q, want %q", got, wantEsc)
	}
}

// spec: tui-navigation/former-tab-keys-leave-list-state-unchanged
func TestSingleStatusFormerTabKeysInert(t *testing.T) {
	build := func() Model {
		m := NewModel("/repo", "/home")
		m.rows = []row{
			mkRow("a", withGlobal, withRegistry("sec.git", "security")),
			mkRow("b", withProject, withRegistry("team.git", "team")),
			mkRow("c", withAvailable, withRegistry("team.git", "team")),
		}
		m.width, m.height = 80, 24
		m.recomputeView()
		return m
	}
	for _, key := range []tea.KeyPressMsg{
		keyPress('1'), keyPress('2'), keyPress('3'),
		keyNamed("tab"), keyNamed("shift+tab"),
	} {
		m := build()
		before := m.flatItems
		cursor, offset, filter := m.cursor, m.listOffset, m.filter
		mi, _ := m.Update(key)
		m = mi.(Model)
		if len(m.flatItems) != len(before) {
			t.Fatalf("key %v changed the list: %d -> %d items", key.Code, len(before), len(m.flatItems))
		}
		for i := range before {
			if flatItemKey(m.flatItems[i]) != flatItemKey(before[i]) {
				t.Fatalf("key %v reordered the list at %d", key.Code, i)
			}
		}
		if m.cursor != cursor || m.listOffset != offset || m.filter != filter {
			t.Fatalf("key %v changed navigation state: cursor %d->%d offset %d->%d filter %q->%q",
				key.Code, cursor, m.cursor, offset, m.listOffset, filter, m.filter)
		}
		if m.pane != paneList {
			t.Fatalf("key %v left the list pane: %v", key.Code, m.pane)
		}
	}
}

// spec: tui-navigation/former-tab-keys-leave-list-state-unchanged
func TestSingleStatusHelpPaletteFooterHaveNoTabSwitches(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{mkRow("a", withProject, withRegistry("t.git", "team"))}
	m.width, m.height = 120, 24
	m.recomputeView()

	for _, action := range paletteActions() {
		if strings.Contains(strings.ToLower(action.label), "tab") {
			t.Fatalf("palette exposes tab-switch action %q", action.label)
		}
	}
	ids := map[paletteActionID]bool{}
	for _, action := range paletteActions() {
		ids[action.id] = true
	}
	for _, want := range []paletteActionID{actionSearch, actionDetail, actionToggle, actionSync, actionQuit} {
		if !ids[want] {
			t.Fatalf("palette lost supported action %q", want)
		}
	}

	for _, group := range m.keys.FullHelp() {
		for _, b := range group {
			if strings.Contains(strings.ToLower(b.Help().Desc), "tab") {
				t.Fatalf("help advertises tab binding %q", b.Help().Desc)
			}
		}
	}

	footer := ansi.Strip(renderFooter(m))
	if strings.Contains(footer, "tab") {
		t.Fatalf("footer advertises tab switching: %q", footer)
	}
}

// spec: tui-navigation/former-tab-keys-leave-list-state-unchanged
func TestSingleStatusModalTabKeepsNativeFieldFocus(t *testing.T) {
	m := newTestModel()
	m.loading = false
	m.width, m.height = 80, 24
	for i, it := range m.flatItems {
		if it.Kind == flatArtifact {
			m.cursor = i
			break
		}
	}
	mi, _ := m.Update(keyNamed("space"))
	m = mi.(Model)
	if m.pane != paneToggleModal {
		t.Fatalf("space did not open the toggle modal, pane=%v", m.pane)
	}
	before := m.flatItems
	for _, key := range []tea.KeyPressMsg{keyNamed("tab"), keyNamed("shift+tab"), keyNamed("tab")} {
		mi, _ = m.Update(key)
		m = mi.(Model)
		if m.pane != paneToggleModal {
			t.Fatalf("key %v left the modal pane: %v", key.Code, m.pane)
		}
		if m.toggleForm == nil || m.toggleForm.State != huh.StateNormal {
			t.Fatalf("key %v must stay inside the running native form", key.Code)
		}
		if m.operations.Active != nil {
			t.Fatalf("key %v dispatched a mutation without an explicit submit", key.Code)
		}
	}
	if len(m.flatItems) != len(before) {
		t.Fatalf("modal Tab navigation changed the resource list")
	}
}

// spec: tui-feedback/detail-browse-span-has-no-tab-attribute
func TestSingleStatusDetailBrowseSpanHasNoTabAttr(t *testing.T) {
	e := newTestSpanExporter(t)
	ctx, tel := setupTuiTelemetry(t, e)
	defer func() { _ = tel.Shutdown(context.Background()) }()

	m := NewModel("/repo", "/home")
	m.ctx = ctx
	m.rows = []row{mkRow("audit", withProject, withRegistry("t.git", "team"))}
	m.width, m.height = 80, 24
	m.recomputeView()
	for i, it := range m.flatItems {
		if it.Kind == flatArtifact {
			m.cursor = i
			break
		}
	}
	mi, _ := m.Update(keyNamed("enter"))
	m = mi.(Model)

	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	for _, s := range e.spansNamed("agent_marketplace.tui.action") {
		if attrValue(s.Attributes, "action") != "browse" || attrValue(s.Attributes, "view") != "detail" {
			continue
		}
		if attrValue(s.Attributes, "resource.kind") != "skill" {
			t.Fatalf("browse detail span lost resource.kind: %v", s.Attributes)
		}
		for _, a := range s.Attributes {
			if a.Key == "tab" {
				t.Fatalf("browse detail span still carries obsolete tab attribute %q", a.Value.GetStringValue())
			}
		}
		return
	}
	t.Fatalf("no browse detail span emitted")
}

// spec: tui-navigation/browse-inspect-search-fold-and-scroll-in-one-view
// spec: session-navigation/status-grouped-resource-order-and-filtering
// spec: session-navigation/local-search-applies-immediately
func TestSingleStatusBrowseSearchFoldScroll(t *testing.T) {
	m := NewModel("/repo", "/home")
	var rows []row
	for _, name := range []string{"a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9", "aa", "ab", "ac"} {
		rows = append(rows, mkRow(name, withProject, withRegistry("t.git", "team")))
	}
	rows = append(rows, mkRow("needle", withAvailable, withRegistry("t.git", "team")))
	m.rows = rows
	m.width, m.height = 50, 12
	m.recomputeView()

	mi, _ := m.Update(keyPress('G'))
	m = mi.(Model)
	last := m.flatItems[len(m.flatItems)-1]
	if last.Kind != flatArtifact || last.Row.Artifact.Name != "needle" {
		t.Fatalf("bottom row = %v, want needle row", last)
	}
	if m.cursor != len(m.flatItems)-1 {
		t.Fatalf("G did not reach the last row: cursor %d", m.cursor)
	}

	mi, _ = m.Update(keyPress('/'))
	m = mi.(Model)
	for _, r := range "needle" {
		mi, _ = m.Update(keyPress(r))
		m = mi.(Model)
	}
	mi, _ = m.Update(keyNamed("enter"))
	m = mi.(Model)
	if got := artifactNames(m.flatItems); len(got) != 1 || got[0] != "needle" {
		t.Fatalf("search results = %v, want [needle]", got)
	}

	mi, _ = m.Update(keyNamed("esc"))
	m = mi.(Model)
	m.cursor = 0
	mi, _ = m.Update(keyPress('z'))
	m = mi.(Model)
	mi, _ = m.Update(keyPress('a'))
	m = mi.(Model)
	first := m.flatItems[0]
	if first.Kind != flatSection || first.SectionPath != "status:enabled" || !first.Collapsed {
		t.Fatalf("za should fold status:enabled in place, got %+v", first)
	}
	if artifactNames(m.flatItems)[0] != "needle" {
		t.Fatalf("folding must not disturb other sections: %v", artifactNames(m.flatItems))
	}
}

// spec: session-startup/provisional-rows-retain-single-view-position-and-guarded-actions
func TestSingleStatusReloadAnchorsSelection(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("keep", withProject, withRegistry("t.git", "team")),
		mkRow("target", withProject, withRegistry("t.git", "team")),
	}
	m.width, m.height = 80, 24
	m.recomputeView()
	for i, it := range m.flatItems {
		if it.Kind == flatArtifact && it.Row.Artifact.Name == "target" {
			m.cursor = i
		}
	}
	anchor := m.captureRebuildAnchor()
	m.rows = append([]row{mkRow("new-first", withProject, withRegistry("t.git", "team"))}, m.rows...)
	m.recomputeView()
	m.restoreSelectionAfterRebuild(anchor)
	it := m.flatItems[m.cursor]
	if it.Kind != flatArtifact || it.Row.Artifact.Name != "target" {
		t.Fatalf("reload anchoring lost selection: cursor on %+v", it)
	}
}
