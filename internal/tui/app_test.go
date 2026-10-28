package tui

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	ansi "github.com/charmbracelet/x/ansi"
)

// spec: tui-mouse-navigation/wheel-reporting-is-enabled
func TestDefaultTUIProgramOptionsEnableMouseWheelReporting(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	view := m.View()
	if !view.AltScreen || view.MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("TUI view contract = {AltScreen: %t, MouseMode: %v}, want alternate screen with cell-motion mouse", view.AltScreen, view.MouseMode)
	}

	var output bytes.Buffer
	options := append(defaultTUIProgramOptions(), tea.WithInput(nil), tea.WithOutput(&output))
	if _, err := tea.NewProgram(guardProbeModel{}, options...).Run(); err != nil {
		t.Fatal(err)
	}

	got := output.String()
	if !strings.Contains(got, "\x1b[?1049h") {
		t.Fatalf("TUI did not enter the alternate screen: %q", got)
	}
	for _, sequence := range []string{"\x1b[?1002h", "\x1b[?1006h"} {
		if !strings.Contains(got, sequence) {
			t.Fatalf("TUI did not enable mouse wheel reporting with %q: %q", sequence, got)
		}
	}
	for _, sequence := range []string{"\x1b[?1000h", "\x1b[?1003h"} {
		if strings.Contains(got, sequence) {
			t.Fatalf("TUI enabled an unexpected mouse tracking mode with %q", sequence)
		}
	}
}

// spec: tui-mouse-navigation/unrelated-mouse-events-are-ignored
func TestMouseInputOnlyScrollsOnVerticalWheelPress(t *testing.T) {
	tests := []struct {
		name    string
		event   tea.MouseMsg
		delta   int
		startAt func(*viewport.Model)
	}{
		{
			name:  "wheel down",
			event: tea.MouseWheelMsg{Button: tea.MouseWheelDown},
			delta: 1,
		},
		{
			name:  "shift wheel down stays vertical",
			event: tea.MouseWheelMsg{Button: tea.MouseWheelDown, Mod: tea.ModShift},
			delta: 1,
		},
		{
			name:  "wheel up",
			event: tea.MouseWheelMsg{Button: tea.MouseWheelUp},
			delta: -1,
			startAt: func(v *viewport.Model) {
				v.GotoBottom()
			},
		},
		{name: "left click", event: tea.MouseClickMsg{Button: tea.MouseLeft}},
		{name: "left release", event: tea.MouseReleaseMsg{Button: tea.MouseLeft}},
		{name: "drag motion", event: tea.MouseMotionMsg{Button: tea.MouseLeft}},
		{name: "horizontal wheel", event: tea.MouseWheelMsg{Button: tea.MouseWheelRight}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel(t.TempDir(), t.TempDir())
			m.width, m.height = 80, 24
			m.pane = paneDetail
			m.detailViewport = newTUIViewport(80, 3)
			m.detailViewport.SetContent("one\ntwo\nthree\nfour\nfive\nsix\nseven\neight")
			if tt.startAt != nil {
				tt.startAt(&m.detailViewport)
			}
			before := m.detailViewport.YOffset()

			updated, _ := m.Update(tt.event)
			gotModel := updated.(Model)
			got := gotModel.detailViewport.YOffset()
			if want := before + tt.delta; got != want {
				t.Fatalf("viewport offset = %d after event, want %d (started at %d)", got, want, before)
			}
		})
	}
}

// spec: tui-mouse-navigation/scrollable-overlays-move-by-one-line
func TestMouseWheelScrollsHelpOverlay(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	m.width, m.height = 80, 24
	m.pane = paneHelp
	m.helpViewport.SetWidth(m.width)
	m.helpViewport.SetHeight(m.height - 1)
	m.helpViewport.SetContent(renderHelp(m))
	if lines := strings.Count(renderHelp(m), "\n") + 1; lines <= m.helpViewport.Height() {
		t.Fatalf("help fixture has %d lines, want more than viewport height %d", lines, m.helpViewport.Height())
	}

	updated, _ := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	gotModel := updated.(Model)
	got := gotModel.helpViewport.YOffset()
	if got != 1 {
		t.Fatalf("wheel down moved help viewport to line %d, want exactly one line", got)
	}
}

// spec: tui-mouse-navigation/scrollable-overlays-move-by-one-line
func TestMouseWheelScrollsErrorsOverlay(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	m.width, m.height = 80, 12
	m.pane = paneErrors
	m.errorsViewport.SetHeight(m.height - 1)
	for i := 0; i < 20; i++ {
		m.errs = append(m.errs, fmt.Errorf("diagnostic %d", i))
	}
	m.errorsViewport.SetContent(renderErrors(m))
	if m.errorsViewport.AtBottom() {
		t.Fatal("errors fixture does not overflow the viewport")
	}

	updated, _ := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	gotModel := updated.(Model)
	if got := gotModel.errorsViewport.YOffset(); got != 1 {
		t.Fatalf("errors viewport offset = %d, want one line", got)
	}
}

// spec: tui-mouse-navigation/bundle-tree-and-preview-receive-their-own-wheel-input
func TestMouseWheelTargetsDetailBundleTree(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.md", "b.md"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, tt := range []struct {
		name        string
		x           int
		wantPath    string
		wantPreview int
		wantLoad    bool
	}{
		{name: "over tree", x: 1, wantPath: "b.md", wantLoad: true},
		{name: "over preview", x: 33, wantPath: "a.md", wantPreview: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel(t.TempDir(), t.TempDir())
			m.width, m.height = 80, 12
			m.pane = paneDetail
			r := mkRow("bundle", withGlobal)
			r.Artifact.Path = root
			m.rows = []row{r}
			m.detailKey = rowKey(r)
			m.bundle = newBundleTree(root, "")
			m.detailSelectedPath = m.bundle.selectedPath()
			if m.detailSelectedPath != "a.md" {
				t.Fatalf("initial bundle selection = %q, want a.md", m.detailSelectedPath)
			}
			m.detailViewport = newTUIViewport(47, 3)
			m.detailViewport.SetContent(strings.Repeat("preview line\n", 20))

			updated, cmd := m.Update(tea.MouseWheelMsg{
				Button: tea.MouseWheelDown, X: tt.x, Y: 1,
			})
			got := updated.(Model)
			if got.detailSelectedPath != tt.wantPath || got.bundle.selectedPath() != tt.wantPath {
				t.Fatalf("selected path = %q (tree %q), want %q",
					got.detailSelectedPath, got.bundle.selectedPath(), tt.wantPath)
			}
			if got.detailViewport.YOffset() != tt.wantPreview {
				t.Fatalf("preview offset = %d, want %d", got.detailViewport.YOffset(), tt.wantPreview)
			}
			if (cmd != nil) != tt.wantLoad {
				t.Fatalf("preview load scheduled = %t, want %t", cmd != nil, tt.wantLoad)
			}
		})
	}
}

// spec: tui-mouse-navigation/scrollable-overlays-move-by-one-line
func TestMouseWheelScrollsMarketplaceConsent(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	m.width, m.height = 80, 12
	m.pane = paneMarketplaceConsent
	m.setMarketplaceConsentPreview(strings.Repeat("verified resource\n", 30))
	if m.marketplaceConsentViewport.AtBottom() {
		t.Fatal("consent preview does not overflow the viewport")
	}

	updated, _ := m.Update(tea.MouseWheelMsg{
		Button: tea.MouseWheelDown, X: 1, Y: 3,
	})
	gotModel := updated.(Model)
	if got := gotModel.marketplaceConsentViewport.YOffset(); got != 1 {
		t.Fatalf("consent preview offset = %d, want one line", got)
	}
}

// spec: tui-mouse-navigation/list-selection-moves-by-one-entry
func TestMouseWheelMovesSelectionInResourceList(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	m.width, m.height = 80, 24
	m.loading = false
	for i := 0; i < 12; i++ {
		m.flatItems = append(m.flatItems, flatItem{
			Kind: flatArtifact,
			Row:  mkRow(fmt.Sprintf("resource-%02d", i), withGlobal),
		})
	}
	m.cursor = 4

	down, _ := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if got := down.(Model).cursor; got != m.cursor+1 {
		t.Fatalf("wheel down moved list selection from %d to %d, want one row", m.cursor, got)
	}

	up, _ := down.(Model).Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if got := up.(Model).cursor; got != m.cursor {
		t.Fatalf("wheel up did not return selection to its starting row: got %d, want %d", got, m.cursor)
	}
}

// spec: tui-mouse-navigation/reversing-at-the-list-boundary-is-immediate
func TestWheelDirectionReversalAtListBoundaryIsImmediate(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	m.width, m.height = 80, 24
	m.loading = false
	for i := 0; i < 12; i++ {
		m.flatItems = append(m.flatItems, flatItem{
			Kind: flatArtifact,
			Row:  mkRow(fmt.Sprintf("resource-%02d", i), withGlobal),
		})
	}

	up, _ := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if got := up.(Model).cursor; got != 0 {
		t.Fatalf("wheel up at first item moved selection to %d, want 0", got)
	}
	down, _ := up.(Model).Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if got := down.(Model).cursor; got != 1 {
		t.Fatalf("wheel down immediately after boundary input moved selection to %d, want 1", got)
	}
}

// spec: tui-mouse-navigation/wheel-input-below-the-minimum-terminal-size-is-ignored
func TestMouseWheelIgnoredBelowMinimumTerminalSize(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	m.width, m.height = 49, 12
	m.flatItems = []flatItem{
		{Kind: flatArtifact, Row: mkRow("first", withGlobal)},
		{Kind: flatArtifact, Row: mkRow("second", withGlobal)},
	}

	updated, cmd := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if got := updated.(Model).cursor; got != 0 || cmd != nil {
		t.Fatalf("wheel below minimum size moved list cursor to %d or scheduled command %v", got, cmd)
	}
}

// spec: tui-mouse-navigation/detail-refresh-retains-a-wheel-scroll
func TestDetailMouseWheelClearsStickyBottom(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	m.width, m.height = 80, 12
	m.pane = paneDetail
	m.detailViewport = newTUIViewport(80, 11)
	content := strings.Repeat("detail line\n", 20)
	m.detailViewport.SetContent(content)
	m.detailViewport.GotoBottom()
	m.detailStickyBottom = true
	generation := m.beginRequest(requestDetail)

	scrolled, _ := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	m = scrolled.(Model)
	offset := m.detailViewport.YOffset()
	if m.detailStickyBottom {
		t.Fatal("wheel-up left sticky-bottom enabled")
	}

	updated, _ := m.Update(detailContentMsg{
		generation: generation,
		content:    content,
		width:      m.previewWidth(),
		keepScroll: true,
	})
	gotModel := updated.(Model)
	got := gotModel.detailViewport.YOffset()
	if got != offset {
		t.Fatalf("detail refresh reset wheel scroll offset from %d to %d", offset, got)
	}
}

func TestTUIViewportsUseOneLineMouseWheelDelta(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	for _, tt := range []struct {
		name string
		v    viewport.Model
	}{
		{name: "detail", v: m.detailViewport},
		{name: "help", v: m.helpViewport},
		{name: "errors", v: m.errorsViewport},
		{name: "marketplace consent", v: m.marketplaceConsentViewport},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.v.MouseWheelDelta != 1 {
				t.Fatalf("mouse wheel delta = %d, want one line", tt.v.MouseWheelDelta)
			}
		})
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

func TestRowGlyphOrphanedCopyIsPending(t *testing.T) {
	r := row{DriftItem: &materialize.DriftItem{Reason: materialize.DriftOrphaned}}
	if got := r.Glyph(); got != "[*]" {
		t.Fatalf("expected [*] for a stale copy pending sync, got %s", got)
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
		{"required org", row{Artifact: resolver.ResolvedArtifact{Required: true, Source: config.Organization}}, "required (org)"},
		{"required user", row{Artifact: resolver.ResolvedArtifact{Required: true, Source: config.UserPersonal}}, "required"},
		{"malformed", row{IsMalformed: true, MalformedMsg: "bad"}, "malformed: bad"},
		{"pending", row{DriftItem: &materialize.DriftItem{Reason: materialize.DriftMissing}}, "enabled (pending sync)"},
		{"orphaned copy", row{DriftItem: &materialize.DriftItem{Reason: materialize.DriftOrphaned}}, "enabled (pending sync)"},
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
		requests:     requestState{Next: 1, Snapshot: 1},
		width:        80,
		height:       24,
		keys:         defaultKeyMap,
		help:         newHelpModel(newTheme(true)),
	}

	updatedModel, cmd := m.handleKey(tea.KeyPressMsg{Code: 'r', Text: string('r')})
	if cmd == nil {
		t.Fatalf("expected reload key to return a command")
	}

	gotModel := updatedModel.(Model)
	if got := gotModel.requests.Active(requestSnapshot); got != 2 {
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
	out := ansi.Strip(updated.(Model).View().Content)
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

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	out := updated.(Model).View().Content
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
	if out := visibleListPane(m.View().Content, 20); !strings.Contains(out, "alpha") || strings.Contains(out, "omega") {
		t.Fatalf("initial view did not start at the top of the list:\n%s", ansi.Strip(m.View().Content))
	}

	bottom, _ := m.Update(tea.KeyPressMsg{Code: 'G', Text: string('G')})
	m = bottom.(Model)
	if out := visibleListPane(m.View().Content, 20); !strings.Contains(out, "omega") {
		t.Fatalf("bottom navigation moved the selection outside the list pane:\n%s", ansi.Strip(m.View().Content))
	}

	top, _ := m.Update(tea.KeyPressMsg{Code: 'g', Text: string('g')})
	m = top.(Model)
	if out := visibleListPane(m.View().Content, 20); !strings.Contains(out, "alpha") || strings.Contains(out, "omega") {
		t.Fatalf("top navigation did not restore the first list rows:\n%s", ansi.Strip(m.View().Content))
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
	out := updated.(Model).View().Content
	if !strings.Contains(visibleListPane(out, 20), "selected") {
		t.Fatalf("selected resource is outside the visible list pane after resize:\n%s", ansi.Strip(out))
	}
}

func TestHalfPageNavigationMovesListSelection(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	m.loading = false
	m.width = 120
	m.height = 24
	for i := 0; i < 40; i++ {
		m.flatItems = append(m.flatItems, flatItem{Kind: flatArtifact, Row: mkRow(fmt.Sprintf("resource-%d", i), withGlobal)})
	}
	half := max(1, m.listVisibleHeight()/2)
	page := m.listVisibleHeight()

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	m = updated.(Model)
	if m.cursor != half {
		t.Fatalf("ctrl+d moved cursor to %d, want %d", m.cursor, half)
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'f', Text: string('f')})
	m = updated.(Model)
	if want := min(half+page, 39); m.cursor != want {
		t.Fatalf("f moved cursor to %d, want %d", m.cursor, want)
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	m = updated.(Model)
	if m.cursor != half {
		t.Fatalf("ctrl+b moved cursor to %d, want %d", m.cursor, half)
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	m = updated.(Model)
	if m.cursor != 39 {
		t.Fatalf("end moved cursor to %d, want 39", m.cursor)
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyHome})
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
			out := ansi.Strip(updated.(Model).View().Content)
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
	out := ansi.Strip(updated.(Model).View().Content)
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
	out := ansi.Strip(updated.(Model).View().Content)
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
