package tui

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	tea "github.com/charmbracelet/bubbletea"
	ansi "github.com/charmbracelet/x/ansi"
)

func TestDefaultTUIProgramOptionsLeaveMouseSelectionToTerminal(t *testing.T) {
	var output bytes.Buffer
	options := append(defaultTUIProgramOptions(), tea.WithInput(nil), tea.WithOutput(&output))
	if _, err := tea.NewProgram(guardProbeModel{}, options...).Run(); err != nil {
		t.Fatal(err)
	}

	got := output.String()
	if !strings.Contains(got, "\x1b[?1049h") {
		t.Fatalf("TUI did not enter the alternate screen: %q", got)
	}
	for _, sequence := range []string{"\x1b[?1000h", "\x1b[?1002h", "\x1b[?1003h", "\x1b[?1006h"} {
		if strings.Contains(got, sequence) {
			t.Fatalf("TUI enabled mouse tracking with %q, preventing native text selection", sequence)
		}
	}
}

func TestRowGlyphEnabled(t *testing.T) {
	r := row{IsAvailable: false}
	if got := r.Glyph(); got != "[x]" {
		t.Fatalf("expected [x] for enabled skill, got %s", got)
	}
}

func TestRowGlyphRequired(t *testing.T) {
	r := row{
		Artifact: resolver.ResolvedArtifact{Required: true, Entry: catalog.Entry{Name: "audit-logging"}},
	}
	if got := r.Glyph(); got != "[R]" {
		t.Fatalf("expected [R] for required skill, got %s", got)
	}
}

func TestRowGlyphAvailable(t *testing.T) {
	r := row{IsAvailable: true}
	if got := r.Glyph(); got != "[ ]" {
		t.Fatalf("expected [ ] for available skill, got %s", got)
	}
}

func TestRowGlyphMalformed(t *testing.T) {
	r := row{IsMalformed: true, MalformedMsg: "bad yaml"}
	if got := r.Glyph(); got != "[!]" {
		t.Fatalf("expected [!] for malformed skill, got %s", got)
	}
}

func TestRowGlyphPendingSync(t *testing.T) {
	r := row{DriftItem: &materialize.DriftItem{Reason: materialize.DriftMissing}}
	if got := r.Glyph(); got != "[*]" {
		t.Fatalf("expected [*] for pending sync, got %s", got)
	}
}

func TestRowGlyphOrphaned(t *testing.T) {
	r := row{DriftItem: &materialize.DriftItem{Reason: materialize.DriftOrphaned}}
	if got := r.Glyph(); got != "[-]" {
		t.Fatalf("expected [-] for orphaned, got %s", got)
	}
}

func TestRowStatusText(t *testing.T) {
	tests := []struct {
		name string
		row  row
		want string
	}{
		{"enabled", row{}, "enabled"},
		{"available", row{IsAvailable: true}, "available"},
		{"excluded", row{IsAvailable: true, IsExcluded: true}, "excluded"},
		{"required", row{Artifact: resolver.ResolvedArtifact{Required: true}}, "required (org)"},
		{"malformed", row{IsMalformed: true, MalformedMsg: "bad"}, "malformed: bad"},
		{"pending", row{DriftItem: &materialize.DriftItem{Reason: materialize.DriftMissing}}, "enabled (pending sync)"},
		{"orphaned", row{DriftItem: &materialize.DriftItem{Reason: materialize.DriftOrphaned}}, "orphaned"},
		{"broken", row{DriftItem: &materialize.DriftItem{Reason: materialize.DriftBroken}}, "broken"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.row.StatusText(); got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestApplyFilter(t *testing.T) {
	rows := []row{
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "vm-lifecycle", Tags: []string{"infra"}}}},
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "k8s-deploy", Tags: []string{"k8s"}}}},
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "firewall-rules", Tags: []string{"security"}}}},
	}

	m := Model{rows: rows}
	filtered := m.applyFilter()
	if len(filtered) != 3 {
		t.Fatalf("expected 3 rows with no filter, got %d", len(filtered))
	}

	m.filter = "k8s"
	filtered = m.applyFilter()
	if len(filtered) != 1 {
		t.Fatalf("expected 1 row matching 'k8s', got %d", len(filtered))
	}

	m.filter = "infra"
	filtered = m.applyFilter()
	if len(filtered) != 1 {
		t.Fatalf("expected 1 row matching 'infra', got %d", len(filtered))
	}
}

func TestStaleRefreshLoadedMsgIsIgnoredAfterReloadStarts(t *testing.T) {
	m := Model{
		rows:         []row{{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "new-state"}}}},
		filteredRows: []row{{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "new-state"}}}},
		refreshing:   true,
		requests:     requestState{next: 1, snapshot: 1},
		width:        80,
		height:       24,
		keys:         defaultKeyMap,
		help:         newHelpModel(),
	}

	updatedModel, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd == nil {
		t.Fatalf("expected reload key to return a command")
	}

	gotModel := updatedModel.(Model)
	if got := gotModel.requests.active(requestSnapshot); got != 2 {
		t.Fatalf("active snapshot generation = %d, want 2", got)
	}

	stale := refreshLoadedMsg{snapshotLoadedMsg: snapshotLoadedMsg{
		generation: 1,
		rows:       []row{{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "stale-state"}}}},
	}}

	nextModel, _ := gotModel.Update(stale)
	finalModel := nextModel.(Model)

	if finalModel.rows[0].Artifact.Name != "new-state" {
		t.Fatalf("expected stale refresh to be ignored, got %q", finalModel.rows[0].Artifact.Name)
	}
}

func TestViewFitsTerminalWithLongContent(t *testing.T) {
	r := mkRow("selected-resource-with-a-long-name", withProject, withRegistry("https://example.com/a/very/long/registry/location.git", "registry-with-a-long-name"))
	r.Artifact.Path = "/a/very/long/cache/location/that/must-wrap/SKILL.md"
	r.Artifact.Description = strings.Repeat("long description content ", 12)
	m := modelWithSelectedRow(t, r)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 12})
	out := ansi.Strip(updated.(Model).View())
	lines := strings.Split(out, "\n")
	if len(lines) > 12 {
		t.Fatalf("View() produced %d lines for a 12-line terminal:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[0], "agent-marketplace") {
		t.Fatalf("header is not on the first line: %q", lines[0])
	}
	for _, line := range lines {
		if width := ansi.StringWidth(line); width > 50 {
			t.Fatalf("View() produced a %d-column line for a 50-column terminal: %q", width, line)
		}
	}
}

func TestDownKeepsSelectedResourceVisibleWithWrappedRows(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	m.loading = false
	m.width = 50
	m.height = 12
	m.flatItems = []flatItem{
		{Kind: flatSection, SectionTitle: strings.Repeat("long-section-title-", 5), SectionCount: 2},
		{Kind: flatArtifact, Row: mkRow("first", withGlobal)},
		{Kind: flatArtifact, Row: mkRow("selected", withGlobal)},
	}
	m.cursor = 1

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	out := updated.(Model).View()
	if !strings.Contains(visibleListPane(out, 20), "selected") {
		t.Fatalf("selected resource is outside the visible list pane:\n%s", ansi.Strip(out))
	}
}

func TestNavigationKeepsSelectionVisibleFromLoadedSnapshot(t *testing.T) {
	rows := []row{
		mkRow("alpha", withGlobal),
		mkRow("bravo", withGlobal),
		mkRow("charlie", withGlobal),
		mkRow("delta", withGlobal),
		mkRow("echo", withGlobal),
		mkRow("foxtrot", withGlobal),
		mkRow("golf", withGlobal),
		mkRow("omega", withGlobal),
	}
	m := NewModel(t.TempDir(), t.TempDir())
	resized, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 12})
	loaded, _ := resized.(Model).Update(snapshotLoadedMsg{generation: 1, rows: rows})
	m = loaded.(Model)
	if out := visibleListPane(m.View(), 20); !strings.Contains(out, "alpha") || strings.Contains(out, "omega") {
		t.Fatalf("initial view did not start at the top of the list:\n%s", ansi.Strip(m.View()))
	}

	bottom, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = bottom.(Model)
	if out := visibleListPane(m.View(), 20); !strings.Contains(out, "omega") {
		t.Fatalf("bottom navigation moved the selection outside the list pane:\n%s", ansi.Strip(m.View()))
	}

	top, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = top.(Model)
	if out := visibleListPane(m.View(), 20); !strings.Contains(out, "alpha") || strings.Contains(out, "omega") {
		t.Fatalf("top navigation did not restore the first list rows:\n%s", ansi.Strip(m.View()))
	}
}

func TestResizeKeepsSelectedResourceVisibleWithWrappedRows(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	m.loading = false
	m.width = 100
	m.height = 20
	m.flatItems = []flatItem{
		{Kind: flatSection, SectionTitle: strings.Repeat("long-section-title-", 4), SectionCount: 2},
		{Kind: flatArtifact, Row: mkRow("first", withGlobal)},
		{Kind: flatArtifact, Row: mkRow("selected", withGlobal)},
	}
	m.cursor = 2
	m.clampListOffset()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 12})
	out := updated.(Model).View()
	if !strings.Contains(visibleListPane(out, 20), "selected") {
		t.Fatalf("selected resource is outside the visible list pane after resize:\n%s", ansi.Strip(out))
	}
}

func TestHalfPageNavigationMovesListSelection(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	m.loading = false
	m.width = 80
	m.height = 24
	for i := 0; i < 40; i++ {
		m.flatItems = append(m.flatItems, flatItem{Kind: flatArtifact, Row: mkRow(fmt.Sprintf("resource-%d", i), withGlobal)})
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m = updated.(Model)
	if m.cursor != 10 {
		t.Fatalf("ctrl+d moved cursor to %d, want 10", m.cursor)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = updated.(Model)
	if m.cursor != 31 {
		t.Fatalf("f moved cursor to %d, want 31", m.cursor)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	m = updated.(Model)
	if m.cursor != 10 {
		t.Fatalf("ctrl+b moved cursor to %d, want 10", m.cursor)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = updated.(Model)
	if m.cursor != 39 {
		t.Fatalf("end moved cursor to %d, want 39", m.cursor)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = updated.(Model)
	if m.cursor != 0 {
		t.Fatalf("home moved cursor to %d, want 0", m.cursor)
	}
}

func TestViewFitsTerminalChrome(t *testing.T) {
	for _, tc := range []struct {
		name         string
		width        int
		height       int
		filterActive bool
	}{
		{name: "no pane rows", width: 80, height: 3},
		{name: "header and footer", width: 80, height: 2},
		{name: "active search", width: 80, height: 4, filterActive: true},
		{name: "active search without tabs", width: 80, height: 3, filterActive: true},
		{name: "single cell", width: 1, height: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewModel(t.TempDir(), t.TempDir())
			m.loading = false
			m.filterActive = tc.filterActive
			if tc.filterActive {
				m.searchInput.SetValue("skill")
			}
			updated, _ := m.Update(tea.WindowSizeMsg{Width: tc.width, Height: tc.height})
			out := ansi.Strip(updated.(Model).View())
			if lines := strings.Split(out, "\n"); len(lines) > tc.height {
				t.Fatalf("View() produced %d lines for a %d-line terminal: %q", len(lines), tc.height, out)
			}
			for _, line := range strings.Split(out, "\n") {
				if width := ansi.StringWidth(line); width > tc.width {
					t.Fatalf("View() produced a %d-column line for a %d-column terminal: %q", width, tc.width, line)
				}
			}
			if tc.height >= 2 && !strings.Contains(out, "q quit") {
				t.Fatalf("view omitted quit guidance from a %d-line terminal: %q", tc.height, out)
			}
		})
	}
}

func TestInitialModalFitsTerminal(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	m.loading = false
	m.pane = paneInitModal

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 30, Height: 8})
	out := ansi.Strip(updated.(Model).View())
	lines := strings.Split(out, "\n")
	if len(lines) > 8 {
		t.Fatalf("View() produced %d lines for an 8-line terminal: %q", len(lines), out)
	}
	for _, line := range lines {
		if width := ansi.StringWidth(line); width > 30 {
			t.Fatalf("View() produced a %d-column modal line for a 30-column terminal: %q", width, line)
		}
	}
}

func TestViewWrapsUnicodeDetailWithoutDataLoss(t *testing.T) {
	const source = "/缓存/技能/非常长的资源位置/另一个目录/SKILL.md"
	r := mkRow("unicode", withGlobal)
	r.Artifact.Path = source
	m := modelWithSelectedRow(t, r)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	out := ansi.Strip(updated.(Model).View())
	if !utf8.ValidString(out) {
		t.Fatalf("View() split a Unicode code point: %q", out)
	}
	compact := strings.NewReplacer("\n", "", " ", "").Replace(out)
	if !strings.Contains(compact, source) {
		t.Fatalf("View() lost part of the wrapped source path:\n%s", out)
	}
}

func visibleListPane(output string, paneWidth int) string {
	var b strings.Builder
	for _, line := range strings.Split(ansi.Strip(output), "\n") {
		b.WriteString(ansi.Truncate(line, paneWidth, ""))
		b.WriteByte('\n')
	}
	return b.String()
}
