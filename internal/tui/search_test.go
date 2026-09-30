package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/charmbracelet/x/ansi"
)

func TestFilteredOutSectionsHidden(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
		mkRow("deploy", withProject, withRegistry("infra.git", "infra")),
	}
	m.filter = "audit"
	m.recomputeView()

	paths := sectionPaths(m.flatItems)
	for _, p := range paths {
		if p == "scope:project" || p == "scope:project:infra" {
			t.Fatalf("section %q should be hidden (no match), got paths: %v", p, paths)
		}
	}

	var found bool
	for _, p := range paths {
		if p == "scope:global" {
			found = true
		}
	}
	if !found {
		t.Fatalf("scope:global should remain visible with matches, got %v", paths)
	}
}

func TestActiveFilterOverridesCollapsedFoldState(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{mkRow("audit", withGlobal, withRegistry("sec.git", "security"))}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	// User collapses scope:global.
	m.cursor = 0
	m.toggleFoldAtCursor()
	m.rebuildFlatItems()
	if !m.currentFoldState()["scope:global"] {
		t.Fatalf("precondition: scope:global should be collapsed")
	}

	// Now filter for 'audit': the collapsed fold should be overridden so
	// the match is visible.
	m.filter = "audit"
	m.recomputeView()

	var hit bool
	for _, it := range m.flatItems {
		if it.Kind == flatArtifact && it.Row.Artifact.Name == "audit" {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("active filter must expand collapsed sections to reveal matches")
	}

	// When filter clears, fold state is restored (skill hidden again).
	m.filter = ""
	m.recomputeView()
	for _, it := range m.flatItems {
		if it.Kind == flatArtifact && it.Row.Artifact.Name == "audit" {
			t.Fatalf("after clearing filter, collapsed fold should hide audit again")
		}
	}
}

func TestFilterSnapsCursorToFirstMatch(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
		mkRow("deploy", withProject, withRegistry("infra.git", "infra")),
		mkRow("release", withProject, withRegistry("infra.git", "infra")),
	}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	// Move cursor to last item.
	m.cursor = len(m.flatItems) - 1

	// Filter: only 'audit' matches.
	m.filter = "audit"
	m.recomputeView()

	// Cursor should be positioned at (or before) the first matching skill.
	// Find the first flatArtifact in the result.
	firstSkillIdx := -1
	for i, it := range m.flatItems {
		if it.Kind == flatArtifact {
			firstSkillIdx = i
			break
		}
	}
	if firstSkillIdx < 0 {
		t.Fatalf("expected at least one matching skill, got %+v", m.flatItems)
	}
	if m.cursor != firstSkillIdx {
		t.Fatalf("cursor should snap to first match at %d, got %d", firstSkillIdx, m.cursor)
	}
}

func TestFilterWithNoMatchesResetsCursorToZero(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{mkRow("audit", withGlobal, withRegistry("sec.git", "security"))}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	m.cursor = len(m.flatItems) - 1

	m.filter = "__no_match__"
	m.recomputeView()

	if m.cursor != 0 {
		t.Fatalf("cursor should be 0 with no matches, got %d", m.cursor)
	}
	if len(m.flatItems) != 0 {
		t.Fatalf("no-match filter should yield empty flatItems, got %d", len(m.flatItems))
	}
}

func TestApplyFilterMatchesNameTagAndRegistryConsistently(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = buildBenchRows(50)

	tests := []struct {
		name   string
		filter string
		want   []string
	}{
		{name: "name", filter: "skill-security-0000", want: []string{"skill-security-0000"}},
		{name: "tag", filter: "helm", want: []string{"skill-infra-0001", "skill-infra-0006", "skill-infra-0011", "skill-infra-0016", "skill-infra-0021", "skill-infra-0026", "skill-infra-0031", "skill-infra-0036", "skill-infra-0041", "skill-infra-0046"}},
		{name: "registry", filter: "platform.git", want: []string{"skill-platform-0002", "skill-platform-0007", "skill-platform-0012", "skill-platform-0017", "skill-platform-0022", "skill-platform-0027", "skill-platform-0032", "skill-platform-0037", "skill-platform-0042", "skill-platform-0047"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.filter = tt.filter

			first := m.applyFilter()
			firstNames := rowNames(first)
			// Capture stable snapshot before second call to avoid shared backing storage
			firstNamesCopy := slices.Clone(firstNames)
			second := m.applyFilter()
			secondNames := rowNames(second)

			if !slices.Equal(firstNamesCopy, tt.want) {
				t.Fatalf("first applyFilter() = %v, want %v", firstNamesCopy, tt.want)
			}
			if !slices.Equal(secondNames, tt.want) {
				t.Fatalf("second applyFilter() = %v, want %v", secondNames, tt.want)
			}
		})
	}
}

// --- textinput-driven search tests ---

// searchModel returns a Model ready for search key testing: rows loaded,
// snapshot applied, not loading.
func searchModel() Model {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
		mkRow("deploy", withProject, withRegistry("infra.git", "infra")),
		mkRow("release", withProject, withRegistry("infra.git", "infra")),
	}
	m.loading = false
	m.recomputeView()
	return m
}

func TestSearchInputShowsSpinnerWhileMarketplaceResultsLoad(t *testing.T) {
	m := searchModel()
	m.filterActive = true
	m.discoveryLoading = true
	m.searchInput.SetValue("audit")

	lines := strings.Split(m.renderBaseScreen(), "\n")
	if len(lines) < 3 {
		t.Fatalf("rendered screen has no search line: %q", lines)
	}
	searchLine := ansi.Strip(lines[2])
	spinnerPosition := strings.Index(searchLine, ansi.Strip(m.spinner.View()))
	queryPosition := strings.Index(searchLine, "audit")
	if spinnerPosition < 0 || queryPosition < 0 || spinnerPosition > queryPosition || strings.Contains(searchLine, "/") {
		t.Fatalf("search spinner should replace the slash before the query: %q", searchLine)
	}
	if strings.Contains(ansi.Strip(lines[len(lines)-1]), "Marketplace...") {
		t.Fatalf("footer should not duplicate the search loading indicator: %q", lines[len(lines)-1])
	}

	m.discoveryLoading = false
	lines = strings.Split(m.renderBaseScreen(), "\n")
	if strings.Contains(lines[2], m.spinner.View()) || !strings.Contains(ansi.Strip(lines[2]), "/ audit") {
		t.Fatalf("idle search line should restore the slash prompt: %q", lines[2])
	}
}

func TestSearchClearFromListRestoresBrowseDiscoveryBeforeEmptyQuery(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	m := searchModel()
	endpoint := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}
	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}
	m.authoritativeRows = slices.Clone(m.rows)
	browse := mkRow("browse-result", withAvailable, withRegistry("https://market.example", "market"))
	browse.MarketplaceDiscovery = &marketplaceDiscovery{Endpoint: endpoint}
	remote := mkRow("remote-result", withAvailable, withRegistry("https://market.example", "market"))
	remote.MarketplaceDiscovery = &marketplaceDiscovery{Endpoint: endpoint}
	m.browseDiscoveryRows = []row{browse}
	m.discoveryRows = []row{remote}
	m.filter = "remote"
	m.searchInput.SetValue("remote")
	m.mergeDiscoveryRows()

	m = asModel(t, mustUpdate(m.Update(keyPress('/'))))
	if !m.filterActive || m.filter != "remote" || m.searchInput.Value() != "remote" {
		t.Fatalf("reopened search must resume the query: active=%t filter=%q input=%q", m.filterActive, m.filter, m.searchInput.Value())
	}
	if got := rowNames(m.discoveryRows); !slices.Equal(got, []string{"remote-result"}) {
		t.Fatalf("resuming search must keep the query's discovery rows, got %v", got)
	}
	m = asModel(t, mustUpdate(m.Update(v2Enter())))

	updated, cmd := m.Update(escKey())
	m = updated.(Model)
	if m.filterActive || m.filter != "" || m.searchInput.Value() != "" {
		t.Fatalf("cleared search state: active=%t filter=%q input=%q", m.filterActive, m.filter, m.searchInput.Value())
	}
	if got := rowNames(m.discoveryRows); !slices.Equal(got, []string{"browse-result"}) {
		t.Fatalf("visible discovery rows = %v, want browse results", got)
	}
	if got := rowNames(m.rows); !slices.Equal(got, []string{"audit", "deploy", "release", "browse-result"}) {
		t.Fatalf("merged rows = %v", got)
	}
	if cmd == nil || !m.discoveryLoading {
		t.Fatalf("empty-query discovery not scheduled: cmd=%v loading=%t", cmd, m.discoveryLoading)
	}
}

func TestSearchInputTypingUpdatesFilter(t *testing.T) {
	m := searchModel()

	// Open search with '/'
	mi, _ := m.Update(keyPress('/'))
	m = mi.(Model)
	if !m.filterActive {
		t.Fatalf("expected filterActive after pressing '/'")
	}

	// Type "au"
	mi, _ = m.Update(keyPress('a'))
	m = mi.(Model)
	mi, _ = m.Update(keyPress('u'))
	m = mi.(Model)

	if m.filter != "au" {
		t.Fatalf("expected filter 'au', got %q", m.filter)
	}
	if m.searchInput.Value() != "au" {
		t.Fatalf("expected searchInput value 'au', got %q", m.searchInput.Value())
	}

	// Only "audit" should match
	var matchedNames []string
	for _, it := range m.flatItems {
		if it.Kind == flatArtifact {
			matchedNames = append(matchedNames, it.Row.Artifact.Name)
		}
	}
	if len(matchedNames) != 1 || matchedNames[0] != "audit" {
		t.Fatalf("expected only 'audit' to match, got %v", matchedNames)
	}
}

func rowNames(rows []row) []string {
	names := make([]string, len(rows))
	for i, r := range rows {
		names[i] = r.Artifact.Name
	}
	return names
}

func TestSearchInputEscapeClearsFilter(t *testing.T) {
	m := searchModel()

	// Open search and type something
	mi, _ := m.Update(keyPress('/'))
	m = mi.(Model)
	mi, _ = m.Update(keyPress('a'))
	m = mi.(Model)
	if m.filter != "a" {
		t.Fatalf("expected filter 'a', got %q", m.filter)
	}

	// Escape should clear and close
	mi, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = mi.(Model)
	if m.filterActive {
		t.Fatalf("expected filterActive false after Escape")
	}
	if m.filter != "" {
		t.Fatalf("expected empty filter after Escape, got %q", m.filter)
	}
	if m.searchInput.Value() != "" {
		t.Fatalf("expected searchInput cleared after Escape, got %q", m.searchInput.Value())
	}
}

func TestSearchInputEnterCommitsFilter(t *testing.T) {
	m := searchModel()

	mi, _ := m.Update(keyPress('/'))
	m = mi.(Model)
	mi, _ = m.Update(keyPress('a'))
	m = mi.(Model)

	// Enter should close the input but keep the filter
	mi, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mi.(Model)
	if m.filterActive {
		t.Fatalf("expected filterActive false after Enter")
	}
	if m.filter != "a" {
		t.Fatalf("expected filter 'a' preserved after Enter, got %q", m.filter)
	}
}

func TestSearchInputBackspace(t *testing.T) {
	m := searchModel()

	mi, _ := m.Update(keyPress('/'))
	m = mi.(Model)
	mi, _ = m.Update(keyPress('a'))
	m = mi.(Model)
	mi, _ = m.Update(keyPress('b'))
	m = mi.(Model)
	if m.filter != "ab" {
		t.Fatalf("expected filter 'ab', got %q", m.filter)
	}

	// Backspace deletes the character before the cursor
	mi, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m = mi.(Model)
	if m.filter != "a" {
		t.Fatalf("expected filter 'a' after backspace, got %q", m.filter)
	}
}

func TestSearchInputCursorMovement(t *testing.T) {
	m := searchModel()

	mi, _ := m.Update(keyPress('/'))
	m = mi.(Model)
	mi, _ = m.Update(keyPress('a'))
	m = mi.(Model)
	mi, _ = m.Update(keyPress('b'))
	m = mi.(Model)

	// Cursor should be at the end (position 2)
	if m.searchInput.Position() != 2 {
		t.Fatalf("expected cursor at position 2, got %d", m.searchInput.Position())
	}

	// Move cursor left
	mi, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m = mi.(Model)
	if m.searchInput.Position() != 1 {
		t.Fatalf("expected cursor at position 1 after left, got %d", m.searchInput.Position())
	}

	// Backspace now deletes 'a' (before cursor), not 'b'
	mi, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m = mi.(Model)
	if m.filter != "b" {
		t.Fatalf("expected filter 'b' after backspace at mid-cursor, got %q", m.filter)
	}

	// Move cursor right to end
	mi, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m = mi.(Model)
	if m.searchInput.Position() != 1 {
		t.Fatalf("expected cursor at position 1 after right, got %d", m.searchInput.Position())
	}
}

func TestSearchInputPaste(t *testing.T) {
	m := searchModel()

	mi, _ := m.Update(keyPress('/'))
	m = mi.(Model)

	// Simulate a bracketed paste event: the whole payload arrives as one
	// PasteMsg. The textinput inserts it via insertRunesFromUserInput.
	mi, _ = m.Update(tea.PasteMsg{Content: "audit"})
	m = mi.(Model)

	if m.filter != "audit" {
		t.Fatalf("expected filter 'audit' after paste, got %q", m.filter)
	}
}

func TestSearchInputFocusState(t *testing.T) {
	m := searchModel()

	// Initially not focused
	if m.searchInput.Focused() {
		t.Fatalf("searchInput should not be focused initially")
	}

	// Opening search focuses it
	mi, _ := m.Update(keyPress('/'))
	m = mi.(Model)
	if !m.searchInput.Focused() {
		t.Fatalf("searchInput should be focused after pressing '/'")
	}

	// Escape blurs it
	mi, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = mi.(Model)
	if m.searchInput.Focused() {
		t.Fatalf("searchInput should not be focused after Escape")
	}

	// Open again and Enter blurs it
	mi, _ = m.Update(keyPress('/'))
	m = mi.(Model)
	mi, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mi.(Model)
	if m.searchInput.Focused() {
		t.Fatalf("searchInput should not be focused after Enter")
	}
}
