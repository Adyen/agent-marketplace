package tui

import "testing"

func artifactNames(items []flatItem) []string {
	var names []string
	for _, it := range items {
		if it.Kind == flatArtifact {
			names = append(names, it.Row.Artifact.Name)
		}
	}
	return names
}

func namesEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestFuzzySearchRanksBestMatchFirstWithinLeaf(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("alpha-deploy", withGlobal, withRegistry("sec.git", "security")),
		mkRow("deploy", withGlobal, withRegistry("sec.git", "security")),
	}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	if got := artifactNames(m.flatItems); !namesEqual(got, []string{"alpha-deploy", "deploy"}) {
		t.Fatalf("precondition: unfiltered order should be alphabetical, got %v", got)
	}

	m.filter = "dep"
	m.recomputeView()
	if got := artifactNames(m.flatItems); !namesEqual(got, []string{"deploy", "alpha-deploy"}) {
		t.Fatalf("fuzzy filter should rank best match first within the leaf section, got %v", got)
	}
}

func TestFuzzySearchMatchesNonContiguousQuery(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("deploy", withGlobal, withRegistry("sec.git", "security")),
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
	}
	m.filter = "dpl"
	m.recomputeView()
	if got := artifactNames(m.flatItems); !namesEqual(got, []string{"deploy"}) {
		t.Fatalf("fuzzy subsequence query 'dpl' should match only deploy, got %v", got)
	}
}

func TestFuzzySearchClearRestoresAlphabeticalOrderAndFolds(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("alpha-deploy", withGlobal, withRegistry("sec.git", "security")),
		mkRow("deploy", withGlobal, withRegistry("sec.git", "security")),
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
	}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	// Collapse the leaf section, then filter and clear: the fold must
	// survive and alphabetical order must return.
	m.cursor = 0
	for i, it := range m.flatItems {
		if it.Kind == flatSection && it.SectionDepth == 2 {
			m.cursor = i
			break
		}
	}
	foldPath, ok := m.selectedSectionPath()
	if !ok {
		t.Fatalf("no leaf section found in flatItems")
	}
	m.toggleFoldAtCursor()
	if !m.currentFoldState()[foldPath] {
		t.Fatalf("precondition: leaf section should be folded")
	}

	m.filter = "dep"
	m.recomputeView()
	if len(artifactNames(m.flatItems)) == 0 {
		t.Fatalf("active filter should expand folded sections to reveal matches")
	}

	m.filter = ""
	m.recomputeView()
	if !m.currentFoldState()[foldPath] {
		t.Fatalf("clearing the filter should restore the saved fold state")
	}
	if len(artifactNames(m.flatItems)) != 0 {
		t.Fatalf("folded leaf should hide rows after filter clears")
	}

	m.currentFoldState()[foldPath] = false
	m.rebuildFlatItems()
	if got := artifactNames(m.flatItems); !namesEqual(got, []string{"alpha-deploy", "audit", "deploy"}) {
		t.Fatalf("clearing the filter should restore alphabetical order, got %v", got)
	}
}
