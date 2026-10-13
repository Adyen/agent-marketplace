package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Adyen/agent-marketplace/internal/config"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/charmbracelet/x/ansi"
)

// readySizedPaletteModel returns a loaded model at a supported size.
func readySizedPaletteModel(width, height int) Model {
	m := readyPaletteModel()
	m.width = width
	m.height = height
	return m
}

// modelWithRows returns a ready model whose list contains the given rows.
func modelWithRows(rows ...row) Model {
	m := readyPaletteModel()
	m.rows = rows
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	return m
}

// AC5: the palette toggle action mirrors the keyboard Space guards, so it is
// only offered when dispatch would actually reach the toggle modal.
func TestPaletteToggleFollowsKeyboardEligibility(t *testing.T) {
	mutable := mkRow("tool", withGlobal, withRegistry("sec.git", "security"))
	pending := mkRow("pending", withGlobal, withRegistry("sec.git", "security"))
	pending.PendingVerification = true
	required := mkRow("required", withGlobal, withRequired, withRegistry("sec.git", "security"))
	malformed := mkRow("broken", withMalformed("parse yaml: bad frontmatter"), withRegistry("sec.git", "security"))

	t.Run("mutable row offers toggle and detail", func(t *testing.T) {
		m := modelWithRows(mutable)
		m.cursor = cursorOnArtifact(m)
		m = openPaletteFor(t, m)
		if !paletteHasAction(m, actionToggle) {
			t.Fatal("toggle should be offered on a mutable row")
		}
		if !paletteHasAction(m, actionDetail) {
			t.Fatal("detail should be offered on a mutable row")
		}
	})

	t.Run("section header hides toggle but keeps detail", func(t *testing.T) {
		m := modelWithRows(mutable)
		m.cursor = 0 // scope section header
		m = openPaletteFor(t, m)
		if paletteHasAction(m, actionToggle) {
			t.Fatal("toggle must not be offered on a section header")
		}
		if !paletteHasAction(m, actionDetail) {
			t.Fatal("section headers remain fold-inspectable through detail")
		}
	})

	blocked := []struct {
		name string
		row  row
	}{
		{"required", required},
		{"pending verification", pending},
		{"malformed", malformed},
	}
	for _, tc := range blocked {
		t.Run(tc.name+" row hides toggle but keeps detail", func(t *testing.T) {
			m := modelWithRows(tc.row)
			m.cursor = cursorOnArtifact(m)
			if m.cursor < 0 {
				t.Fatal("fixture must contain an artifact row")
			}
			m = openPaletteFor(t, m)
			if paletteHasAction(m, actionToggle) {
				t.Fatalf("toggle must not be offered on a %s row", tc.name)
			}
			if !paletteHasAction(m, actionDetail) {
				t.Fatalf("a %s row remains detail-inspectable", tc.name)
			}
		})
	}

	t.Run("invalid config hides toggle", func(t *testing.T) {
		m := modelWithRows(mutable)
		m.cursor = cursorOnArtifact(m)
		m.configInvalid = true
		m = openPaletteFor(t, m)
		if paletteHasAction(m, actionToggle) {
			t.Fatal("toggle must not be offered while the config is invalid")
		}
	})

	t.Run("active operation hides toggle", func(t *testing.T) {
		m := modelWithRows(mutable)
		m.cursor = cursorOnArtifact(m)
		m.syncing = true
		m = openPaletteFor(t, m)
		if paletteHasAction(m, actionToggle) {
			t.Fatal("toggle must not be offered while an operation runs")
		}
	})
}

// AC5: eligibility is re-evaluated on submit against the row/config/operation
// state at that moment; a stale selection neither opens the toggle modal nor
// writes config.
// spec: tui-feedback/state-changes-after-opening
func TestPaletteToggleSubmitReevaluatesCurrentState(t *testing.T) {
	openWithToggle := func(t *testing.T) Model {
		m := modelWithRows(mkRow("tool", withGlobal, withRegistry("sec.git", "security")))
		m.cursor = cursorOnArtifact(m)
		m = openPaletteFor(t, m)
		m = typePaletteQuery(t, m, "toggle res")
		if !paletteHasAction(m, actionToggle) {
			t.Fatal("toggle should be listed while the row is mutable")
		}
		return m
	}

	stale := []struct {
		name   string
		mutate func(m *Model)
	}{
		{"row became required", func(m *Model) { m.flatItems[m.cursor].Row.Artifact.Required = true }},
		{"config became invalid", func(m *Model) { m.configInvalid = true }},
		{"operation started", func(m *Model) { m.syncing = true }},
		{"selection moved to section", func(m *Model) { m.cursor = 0 }},
	}
	for _, tc := range stale {
		t.Run(tc.name, func(t *testing.T) {
			m := openWithToggle(t)
			tc.mutate(&m)
			m, _ = submitPalette(t, m)
			if !m.palette.open {
				t.Fatal("rejecting a stale selection should keep the palette open")
			}
			if m.pane == paneToggleModal {
				t.Fatal("a stale selection must not open the toggle modal or write config")
			}
			if paletteHasAction(m, actionToggle) {
				t.Fatal("the now-ineligible toggle must drop out of the matches")
			}
		})
	}
}

// AC5: the keyboard Space dispatch and the palette toggle action consult one
// shared eligibility decision. For every policy case the palette exposes the
// toggle exactly when the keyboard path would open the toggle modal, and the
// keyboard path keeps its coded no-op/toast outcome otherwise.
func TestToggleEligibilityParityMatrix(t *testing.T) {
	collectionOnly := mkRow("org-item", withGlobal)
	collectionOnly.Artifact.IsMarketplace = true
	collectionOnly.Artifact.Source = config.Organization
	collectionOnly.MarketplaceCollectionOnly = true

	restricted := mkRow("user-item", withGlobal)
	restricted.Artifact.IsMarketplace = true
	restricted.MarketplaceCollectionOnly = true

	cases := []struct {
		name      string
		row       row
		mutate    func(m *Model)
		wantModal bool
		wantToast string
		wantNoCmd bool
	}{
		{name: "normal mutable row", row: mkRow("tool", withGlobal, withRegistry("sec.git", "security")), wantModal: true},
		{name: "required", row: mkRow("required", withGlobal, withRequired, withRegistry("sec.git", "security")), wantToast: "required items cannot be toggled"},
		{name: "malformed", row: mkRow("broken", withMalformed("parse yaml"), withRegistry("sec.git", "security")), wantNoCmd: true},
		{name: "pending verification", row: func() row {
			r := mkRow("pending", withGlobal, withRegistry("sec.git", "security"))
			r.PendingVerification = true
			return r
		}(), wantToast: "selection is still being verified"},
		{name: "blocked marketplace discovery", row: marketplaceDiscoveryRow("skill", "review", mpapi.AvailabilityBlocked), wantToast: "not installable"},
		{name: "collection-only restricted", row: restricted, wantToast: "collection"},
		{name: "collection-only overridable", row: collectionOnly, wantModal: true},
		{name: "loading", row: mkRow("tool", withGlobal), mutate: func(m *Model) { m.loading = true }, wantNoCmd: true},
		{name: "syncing", row: mkRow("tool", withGlobal), mutate: func(m *Model) { m.syncing = true }, wantNoCmd: true},
		{name: "config invalid", row: mkRow("tool", withGlobal), mutate: func(m *Model) { m.configInvalid = true }, wantToast: "invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := modelWithRows(tc.row)
			m.cursor = cursorOnArtifact(m)
			if m.cursor < 0 {
				t.Fatal("fixture must contain an artifact row")
			}
			m.pane = paneList
			if tc.mutate != nil {
				tc.mutate(&m)
			}
			if got := m.actionEligible(actionToggle); got != tc.wantModal {
				t.Fatalf("palette toggle eligibility = %v, want %v", got, tc.wantModal)
			}

			mi, cmd := m.handleKey(tea.KeyPressMsg{Code: tea.KeySpace})
			m = mi.(Model)
			if tc.wantModal {
				if m.pane != paneToggleModal {
					t.Fatalf("keyboard Space did not open the toggle modal: pane %v", m.pane)
				}
				return
			}
			if m.pane == paneToggleModal {
				t.Fatal("keyboard Space opened the toggle modal on an ineligible row")
			}
			if tc.wantNoCmd && cmd != nil {
				t.Fatal("keyboard Space emitted a command for a silent denial")
			}
			if tc.wantToast != "" {
				if cmd == nil {
					t.Fatal("keyboard Space returned no denial toast")
				}
				msg, ok := cmd().(toastMsg)
				if !ok || !strings.Contains(msg.text, tc.wantToast) {
					t.Fatalf("keyboard Space toast = %#v, want text containing %q", cmd(), tc.wantToast)
				}
			}
		})
	}

	t.Run("section header opens bulk modal and swaps the palette toggle", func(t *testing.T) {
		m := modelWithRows(mkRow("tool", withGlobal))
		m.cursor = 0 // section header
		m.pane = paneList
		if m.actionEligible(actionToggle) {
			t.Fatal("palette must hide the single-row toggle on a section header")
		}
		if !m.actionEligible(actionToggleSection) {
			t.Fatal("palette must offer the section bulk toggle on a section header")
		}
		path, ok := m.selectedSectionPath()
		if !ok {
			t.Fatal("fixture cursor must sit on a section header")
		}
		mi, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeySpace})
		m = mi.(Model)
		if m.pane != paneBulkToggle {
			t.Fatalf("keyboard Space on a section header must open the bulk toggle modal, got pane %v", m.pane)
		}
		if m.currentFoldState()[path] {
			t.Fatal("keyboard Space on a section header must not fold it")
		}
	})
}

// AC5: the palette bounds its rendered actions and labels to the available
// dimensions so the footer quit/hard-exit controls stay visible at the
// minimum supported size; every action stays keyboard reachable by scrolling.
func TestPaletteFitsMinimumTerminalAndKeepsFooter(t *testing.T) {
	m := readySizedPaletteModel(minimumTerminalWidth, minimumTerminalHeight)
	m = openPaletteFor(t, m)

	// Scroll to the last action to exercise the bounded window.
	for range m.palette.matches {
		mi, _ := m.Update(keyNamed("down"))
		m = mi.(Model)
	}

	view := m.viewContent()
	lines := strings.Split(view, "\n")
	if len(lines) > minimumTerminalHeight {
		t.Fatalf("palette render exceeds %d rows: %d", minimumTerminalHeight, len(lines))
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w > minimumTerminalWidth {
			t.Fatalf("line %d exceeds %d columns (%d): %q", i, minimumTerminalWidth, w, line)
		}
	}
	last := lines[len(lines)-1]
	if !strings.Contains(last, "quit") {
		t.Fatalf("footer quit controls must stay visible under the palette, last line: %q\nview:\n%s", last, view)
	}
	selected := m.palette.matches[m.palette.cursor].action
	if !strings.Contains(view, selected.label) {
		t.Fatalf("the selected action %q must remain visible after scrolling\nview:\n%s", selected.label, view)
	}
}

// AC5: equal fuzzy scores order by the stable action identity, not by
// declaration order.
func TestPaletteMatchOrderingTiesByActionID(t *testing.T) {
	high := paletteMatch{action: paletteAction{id: actionUpdate}, score: 10}
	low := paletteMatch{action: paletteAction{id: actionDetail}, score: 5}
	if !paletteMatchLess(high, low) {
		t.Fatal("a higher score must sort first")
	}
	// fold-all sorts before sync at equal scores even though sync is
	// declared earlier in paletteActions().
	a := paletteMatch{action: paletteAction{id: actionSync}, score: 5}
	b := paletteMatch{action: paletteAction{id: actionFoldAll}, score: 5}
	if !paletteMatchLess(b, a) || paletteMatchLess(a, b) {
		t.Fatal("equal scores must break ties by ascending action identity")
	}
}

func TestPaletteEqualScoresOrderByIdentity(t *testing.T) {
	m := openPaletteFor(t, readyPaletteModel())
	m = typePaletteQuery(t, m, "e")
	if len(m.palette.matches) < 2 {
		t.Fatal("query should match several actions")
	}
	for i := 1; i < len(m.palette.matches); i++ {
		prev, cur := m.palette.matches[i-1], m.palette.matches[i]
		if prev.score < cur.score {
			t.Fatalf("matches must sort by descending score: %v", m.palette.matches)
		}
		if prev.score == cur.score && prev.action.id >= cur.action.id {
			t.Fatalf("equal scores must order by action identity: %q before %q", prev.action.id, cur.action.id)
		}
	}
}

// AC5: an async transition that moves the pane away from the list closes the
// palette so it cannot resurrect or dispatch stale input on return.
func TestPaletteClosesOnAsyncPaneTransition(t *testing.T) {
	m := readySizedPaletteModel(80, 24)
	m = openPaletteFor(t, m)
	generation := m.beginRequest(requestSnapshot)
	mi, _ := m.Update(snapshotLoadedMsg{
		generation:       generation,
		pendingMigration: migrationPrompt{configs: []migrationConfig{{label: "Project", path: "/repo/.agents/config.yaml"}}},
	})
	m = mi.(Model)
	if m.pane != paneMigrationModal {
		t.Fatalf("expected the migration modal after the async snapshot, got %v", m.pane)
	}
	if m.palette.open {
		t.Fatal("the palette must close when the pane leaves the list")
	}

	mi, _ = m.Update(migrationDoneMsg{err: errors.New("declined")})
	m = mi.(Model)
	if m.pane != paneList {
		t.Fatalf("expected the list pane after the modal resolves, got %v", m.pane)
	}
	if m.palette.open {
		t.Fatal("the palette must not resurrect after returning to the list")
	}
	if strings.Contains(m.viewContent(), "Command palette") {
		t.Fatal("no stale palette dispatch or render may remain after the transition")
	}
}
