package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func withDescription(desc string) func(*row) {
	return func(r *row) { r.Artifact.Description = desc }
}

func typeQuery(t *testing.T, m Model, query string) Model {
	t.Helper()
	for _, r := range query {
		m = asModel(t, mustUpdate(m.Update(v2KeyPress(r))))
	}
	return m
}

func escKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEscape} }

// spec: tui-navigation/scattered-metadata-letters
func TestSearchIgnoresScatteredMetadataLetters(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("sam-capture", withGlobal, withRegistry("sec.git", "security"),
			withDescription("Use when the user shares a thought, idea, remind me, or anything they will later recall")),
		mkRow("grill-me", withGlobal, withRegistry("sec.git", "security")),
	}
	m.filter = "grill"
	m.recomputeView()
	if got := artifactNames(m.flatItems); !namesEqual(got, []string{"grill-me"}) {
		t.Fatalf("query letters scattered through a description must not match, got %v", got)
	}
}

func TestSearchMatchesMetadataWordPrefixes(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("plain", withGlobal, withRegistry("sec.git", "security"), withDescription("deep audit tooling")),
		mkRow("other", withGlobal, withRegistry("sec.git", "security"), withDescription("unrelated helper")),
	}
	for _, query := range []string{"tool", "DEEP", "deep tool", "aud deep"} {
		m.filter = query
		m.recomputeView()
		if got := artifactNames(m.flatItems); !namesEqual(got, []string{"plain"}) {
			t.Fatalf("query %q should match description word prefixes only on plain, got %v", query, got)
		}
	}
	m.filter = "ooling"
	m.recomputeView()
	if got := artifactNames(m.flatItems); len(got) != 0 {
		t.Fatalf("a mid-word metadata fragment must not match, got %v", got)
	}
}

func TestSearchMultiWordQueryFuzzyMatchesName(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{mkRow("grill-with-docs", withGlobal, withRegistry("sec.git", "security"))}
	m.filter = "grill docs"
	m.recomputeView()
	if got := artifactNames(m.flatItems); !namesEqual(got, []string{"grill-with-docs"}) {
		t.Fatalf("a spaced query should still fuzzily match the name, got %v", got)
	}
}

// spec: tui-navigation/name-match-outranks-metadata-match
func TestSearchNameMatchOutranksMetadataAndIsSelected(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("audit", withGlobal, withRegistry("sec.git", "security"), withDescription("deploy helpers")),
		mkRow("deployer", withGlobal, withRegistry("sec.git", "security")),
		mkRow("runbook", withGlobal, withRegistry("sec.git", "security"), withDescription("deploy runbooks")),
		mkRow("deploy", withProject, withRegistry("sec.git", "security")),
	}
	m.filter = "deploy"
	m.recomputeView()
	if got := artifactNames(m.flatItems); !namesEqual(got, []string{"deployer", "audit", "runbook", "deploy"}) {
		t.Fatalf("name matches must rank above metadata-only matches within a section, got %v", got)
	}
	sel, ok := m.selectedRow()
	if !ok || sel.Artifact.Name != "deploy" {
		t.Fatalf("the best-ranked name match across sections should be selected, got %+v", sel.Artifact.Name)
	}
}

func TestSearchHidesSectionsWithoutMatches(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
		mkRow("deploy", withProject, withRegistry("sec.git", "security")),
	}
	m.filter = "deploy"
	m.recomputeView()
	for _, p := range sectionPaths(m.flatItems) {
		if strings.HasPrefix(p, "scope:global") {
			t.Fatalf("a section without matches must be hidden, got %v", sectionPaths(m.flatItems))
		}
	}
}

// spec: tui-navigation/confirmed-filter
func TestConfirmedFilterStaysVisibleUntilEscInList(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.loading = false
	m.width, m.height = 100, 24
	m.rows = []row{
		mkRow("deploy", withGlobal, withRegistry("sec.git", "security")),
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
	}
	m.recomputeView()

	m = asModel(t, mustUpdate(m.Update(v2KeyPress('/'))))
	m = typeQuery(t, m, "dep")
	m = asModel(t, mustUpdate(m.Update(v2Enter())))
	if m.filterActive {
		t.Fatalf("Enter should leave search input focus")
	}
	view := ansi.Strip(m.viewContent())
	if !strings.Contains(view, "dep") || !strings.Contains(view, "1 match") {
		t.Fatalf("confirmed filter and match count must stay visible:\n%s", view)
	}

	m = asModel(t, mustUpdate(m.Update(escKey())))
	if m.filter != "" {
		t.Fatalf("Esc in the list must clear the applied filter, got %q", m.filter)
	}
	if got := artifactNames(m.flatItems); !namesEqual(got, []string{"audit", "deploy"}) {
		t.Fatalf("clearing must restore the unfiltered list, got %v", got)
	}
	if strings.Contains(ansi.Strip(m.viewContent()), "1 match") {
		t.Fatalf("the filter chip must disappear once cleared")
	}
}

// spec: tui-navigation/refine-a-filter
func TestStartingSearchResumesAppliedQuery(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.loading = false
	m.width, m.height = 100, 24
	m.rows = []row{
		mkRow("deploy", withGlobal, withRegistry("sec.git", "security")),
		mkRow("deployer", withGlobal, withRegistry("sec.git", "security")),
	}
	m.recomputeView()
	m = asModel(t, mustUpdate(m.Update(v2KeyPress('/'))))
	m = typeQuery(t, m, "dep")
	m = asModel(t, mustUpdate(m.Update(v2Enter())))

	m = asModel(t, mustUpdate(m.Update(v2KeyPress('/'))))
	if !m.filterActive || m.searchInput.Value() != "dep" || m.filter != "dep" {
		t.Fatalf("starting search must resume the current query; active=%v input=%q filter=%q", m.filterActive, m.searchInput.Value(), m.filter)
	}
	m = typeQuery(t, m, "loyer")
	if got := artifactNames(m.flatItems); !namesEqual(got, []string{"deployer"}) {
		t.Fatalf("refined query should narrow the list, got %v", got)
	}
}
