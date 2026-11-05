package tui

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// clickAt sends a left press and a release in the same cell through the
// frame-paced wrapper, the path real terminal input takes.
func clickAt(t *testing.T, model *framePacedModel, x, y int) (*framePacedModel, tea.Cmd) {
	t.Helper()
	model, _ = updateFramePaced(t, model, tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y})
	return updateFramePaced(t, model, tea.MouseReleaseMsg{Button: tea.MouseLeft, X: x, Y: y})
}

func wrapClickModel(m Model) *framePacedModel {
	model := newFramePacedModel(m)
	model.View()
	return model
}

func assertNoClipboardWrite(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	clipboard := fmt.Sprintf("%T", tea.SetClipboard("")())
	if msg := cmd(); msg != nil && fmt.Sprintf("%T", msg) == clipboard {
		t.Fatalf("click wrote to the clipboard: %v", msg)
	}
}

// spec: tui-mouse-navigation/clicking-a-resource-row-selects-it
func TestMouseClickSelectsResourceRow(t *testing.T) {
	t.Run("short rows", func(t *testing.T) {
		model, _ := newFramePacedListModel(t, []string{"alpha", "beta", "gamma", "delta"})
		model.View()
		// The list starts on row 1, below the header.
		model, cmd := clickAt(t, model, 2, 3)
		inner := innerListModel(t, model)
		if inner.cursor != 2 || inner.pane != paneList {
			t.Fatalf("cursor=%d pane=%d, want cursor 2 in the list", inner.cursor, inner.pane)
		}
		if !strings.Contains(model.View().Content, "selected detail gamma") {
			t.Fatal("right-hand pane does not show the clicked resource's details")
		}
		assertNoClipboardWrite(t, cmd)
	})

	t.Run("wrapped rows after a scrolled offset", func(t *testing.T) {
		names := make([]string, 30)
		for i := range names {
			names[i] = fmt.Sprintf("resource-%02d", i)
		}
		names[5] = "fifth visible resource with several words and filler before FIFTH05-END"
		targetName := "target-six resource label with several words TARGET06-END"
		names[6] = targetName

		model, _ := newFramePacedListModel(t, names)
		inner := innerListModel(t, model)
		inner.listOffset = 5
		inner.cursor = 8
		model = wrapClickModel(inner)

		lines := strings.Split(ansi.Strip(model.View().Content), "\n")
		leftRows := make([]string, len(lines))
		for i, line := range lines {
			leftRows[i] = ansi.Truncate(line, 32, "")
		}
		previousStart, previousEnd := -1, -1
		targetStart, targetEnd, targetEndCount := -1, -1, 0
		for y, line := range leftRows {
			if strings.Contains(line, "fifth") {
				previousStart = y
			}
			if strings.Contains(line, "FIFTH05-END") {
				previousEnd = y
			}
			if strings.Contains(line, "target-six") {
				targetStart = y
			}
			if strings.Contains(line, "TARGET06-END") {
				targetEnd = y
				targetEndCount++
			}
		}
		if previousStart < 0 || previousEnd <= previousStart {
			t.Fatalf("index 5 did not wrap in the rendered left pane:\n%v", leftRows)
		}
		if targetStart < 0 || targetEndCount != 1 || targetEnd <= targetStart {
			t.Fatalf("target row did not wrap to one unique ending token after its start: start=%d end=%d count=%d rows=%v", targetStart, targetEnd, targetEndCount, leftRows)
		}
		// The last screen line of the second visible item.
		model, cmd := clickAt(t, model, 1, targetEnd)
		assertNoClipboardWrite(t, cmd)
		inner = innerListModel(t, model)
		if inner.cursor != 6 || inner.flatItems[inner.cursor].Row.Artifact.Name != targetName || inner.pane != paneList {
			t.Fatalf("clicked continuation selected cursor=%d resource=%q pane=%d, want cursor 6 resource %q in the list", inner.cursor, inner.flatItems[inner.cursor].Row.Artifact.Name, inner.pane, targetName)
		}
		rightRows := make([]string, 0)
		for _, line := range strings.Split(ansi.Strip(model.View().Content), "\n") {
			rightRows = append(rightRows, ansi.Cut(line, 32, 80))
		}
		details := strings.Join(strings.Fields(strings.Join(rightRows, " ")), " ")
		if !strings.Contains(details, "selected detail "+targetName) {
			t.Fatalf("right-hand pane does not show details for %q:\n%s", targetName, details)
		}
	})
}

// spec: tui-mouse-navigation/clicking-the-selected-resource-opens-its-details
func TestMouseClickOnSelectedResourceOpensDetails(t *testing.T) {
	model, _ := newFramePacedListModel(t, []string{"alpha", "beta", "gamma"})
	model.View()
	model, _ = clickAt(t, model, 2, 2)
	if inner := innerListModel(t, model); inner.cursor != 1 || inner.pane != paneList {
		t.Fatalf("first click: cursor=%d pane=%d, want cursor 1 in the list", inner.cursor, inner.pane)
	}
	model, _ = clickAt(t, model, 2, 2)
	inner := innerListModel(t, model)
	if inner.pane != paneDetail {
		t.Fatalf("second click left pane=%d, want detail", inner.pane)
	}
	if r, ok := inner.selectedRow(); !ok || r.Artifact.Name != "beta" || inner.detailKey != rowKey(r) {
		t.Fatalf("detail view opened for %q, want beta", inner.detailKey)
	}
}

// spec: tui-mouse-navigation/clicking-a-section-header-toggles-its-fold
func TestMouseClickTogglesSectionFold(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	m.width, m.height, m.loading = 80, 24, false
	m.rows = []row{
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
		mkRow("deploy", withAvailable, withRegistry("infra.git", "infra")),
	}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	header := -1
	for i, it := range m.flatItems {
		if it.Kind == flatSection && it.SectionPath == "status:available" {
			header = i
		}
	}
	if header < 0 {
		t.Fatal("fixture has no status:available section")
	}
	m.clampListOffset()
	model := wrapClickModel(m)

	model, _ = clickAt(t, model, 1, 1+header)
	inner := innerListModel(t, model)
	if !inner.currentFoldState()["status:available"] {
		t.Fatal("clicking an expanded section header did not fold it")
	}
	if it := inner.flatItems[inner.cursor]; it.Kind != flatSection || it.SectionPath != "status:available" {
		t.Fatalf("cursor is on %+v, want the clicked header", it)
	}

	model, _ = clickAt(t, model, 1, 1+header)
	inner = innerListModel(t, model)
	if inner.currentFoldState()["status:available"] {
		t.Fatal("clicking a folded section header did not unfold it")
	}
}

func bundleClickModel(t *testing.T) Model {
	return bundleClickModelWithRefs(t, true)
}

func bundleClickModelWithRefs(t *testing.T, withRefs bool) Model {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"a.md", "b.md"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if withRefs {
		if err := os.MkdirAll(filepath.Join(root, "refs"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "refs", "c.md"), []byte("c"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := NewModel(t.TempDir(), t.TempDir())
	m.width, m.height = 80, 12
	m.pane = paneDetail
	r := mkRow("bundle", withGlobal)
	r.Artifact.Path = root
	m.rows = []row{r}
	m.detailKey = rowKey(r)
	m.bundle = newBundleTree(root, "")
	m.detailSelectedPath = m.bundle.selectedPath()
	m.detailViewport = newTUIViewport(47, 3)
	m.detailViewport.SetContent(strings.Repeat("preview line\n", 20))
	return m
}

func bundleRowOf(t *testing.T, m Model, relPath string) int {
	t.Helper()
	for i, e := range m.bundle.entries() {
		if e.RelPath == relPath {
			return 1 + i - m.bundleOffset
		}
	}
	t.Fatalf("bundle has no visible entry %q", relPath)
	return 0
}

// A bundle root label wider than the tree pane stays on one row, so a click
// selects the entry drawn under the pointer rather than the one below it.
// spec: tui-mouse-navigation/clicking-a-bundle-tree-entry-selects-and-previews-it
func TestMouseClickBundleTreeEntryUnderALongRootLabel(t *testing.T) {
	m := bundleClickModelWithRefs(t, false)
	root := filepath.Join(t.TempDir(), strings.Repeat("long-bundle-name-", 4))
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.md", "b.md"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m.bundle = newBundleTree(root, "")
	m.detailSelectedPath = m.bundle.selectedPath()
	row := -1
	for y, line := range strings.Split(ansi.Strip(m.viewContent()), "\n") {
		if strings.Contains(line, "b.md") {
			row = y
			break
		}
	}
	if row < 0 {
		t.Fatalf("b.md is not drawn:\n%s", ansi.Strip(m.viewContent()))
	}
	model, cmd := clickAt(t, wrapClickModel(m), 2, row)
	if got := innerListModel(t, model).detailSelectedPath; got != "b.md" || cmd == nil {
		t.Fatalf("clicking the drawn b.md row %d selected %q (preview requested %t), want b.md", row, got, cmd != nil)
	}
}

// spec: tui-mouse-navigation/clicking-a-bundle-tree-entry-selects-and-previews-it
func TestMouseClickSelectsBundleTreeEntry(t *testing.T) {
	m := bundleClickModel(t)
	if m.detailSelectedPath != "a.md" {
		t.Fatalf("initial selection = %q, want a.md", m.detailSelectedPath)
	}
	model := wrapClickModel(m)

	model, cmd := clickAt(t, model, 2, bundleRowOf(t, m, "b.md"))
	inner := innerListModel(t, model)
	if inner.detailSelectedPath != "b.md" || cmd == nil {
		t.Fatalf("selected %q (preview requested %t), want b.md with a preview request", inner.detailSelectedPath, cmd != nil)
	}

	model, cmd = clickAt(t, model, 2, bundleRowOf(t, inner, "b.md"))
	if inner = innerListModel(t, model); inner.detailSelectedPath != "b.md" || cmd != nil {
		t.Fatalf("re-clicking the previewed file selected %q (preview requested %t), want b.md without a new request", inner.detailSelectedPath, cmd != nil)
	}

	refs := bundleRowOf(t, inner, "refs")
	model, _ = clickAt(t, model, 2, refs)
	inner = innerListModel(t, model)
	if e, ok := inner.bundle.selectedEntry(); !ok || e.RelPath != "refs" {
		t.Fatalf("selected entry %+v, want refs", e)
	}
	if bundleRowOf(t, inner, "refs/c.md") != refs+1 {
		t.Fatal("clicking a collapsed directory did not expand it")
	}
	if inner.detailSelectedPath != "b.md" {
		t.Fatalf("directory click changed the preview to %q", inner.detailSelectedPath)
	}

	model, _ = clickAt(t, model, 2, refs)
	for _, e := range innerListModel(t, model).bundle.entries() {
		if e.RelPath == "refs/c.md" {
			t.Fatal("clicking an expanded directory did not collapse it")
		}
	}

	t.Run("keyboard-selected files request their preview when clicked", func(t *testing.T) {
		for _, tt := range []struct {
			name           string
			key            tea.KeyPressMsg
			filesOnly      bool
			initialPreview string
			target         string
		}{
			{name: "g", key: keyPress('g'), filesOnly: true, initialPreview: "b.md", target: "a.md"},
			{name: "Home", key: tea.KeyPressMsg{Code: tea.KeyHome}, filesOnly: true, initialPreview: "b.md", target: "a.md"},
			{name: "G", key: keyPress('G'), initialPreview: "a.md", target: "b.md"},
			{name: "End", key: tea.KeyPressMsg{Code: tea.KeyEnd}, initialPreview: "a.md", target: "b.md"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				m := bundleClickModelWithRefs(t, !tt.filesOnly)
				if !m.bundle.restoreSelection(tt.initialPreview) {
					t.Fatalf("could not initialize tree selection to %q", tt.initialPreview)
				}
				initialEntry, ok := m.bundle.selectedEntry()
				if !ok || initialEntry.IsDir || initialEntry.RelPath != tt.initialPreview || m.bundle.selectedPath() != tt.initialPreview {
					t.Fatalf("initial tree entry=%+v path=%q, want file %q", initialEntry, m.bundle.selectedPath(), tt.initialPreview)
				}
				m.detailSelectedPath = tt.initialPreview
				m.detailViewport.SetContent("OLD PREVIEW: " + tt.initialPreview)
				if m.detailSelectedPath != tt.initialPreview || !strings.Contains(m.detailViewport.View(), "OLD PREVIEW: "+tt.initialPreview) {
					t.Fatalf("initial preview path=%q content=%q, want %q", m.detailSelectedPath, m.detailViewport.View(), tt.initialPreview)
				}
				model := wrapClickModel(m)
				var cmd tea.Cmd
				model, cmd = updateFramePaced(t, model, tt.key)
				if cmd != nil {
					t.Fatalf("key %q requested a preview before a click", tt.name)
				}
				inner := innerListModel(t, model)
				entry, ok := inner.bundle.selectedEntry()
				if !ok || entry.IsDir || entry.RelPath != tt.target || inner.bundle.selectedPath() != tt.target {
					t.Fatalf("key selected entry %+v and path %q, want file %q", entry, inner.bundle.selectedPath(), tt.target)
				}
				if inner.detailSelectedPath != tt.initialPreview || !strings.Contains(inner.detailViewport.View(), "OLD PREVIEW: "+tt.initialPreview) || inner.detailSelectedPath == inner.bundle.selectedPath() {
					t.Fatalf("post-key preview=%q content=%q tree selection=%q, want unchanged preview and divergence from %q", inner.detailSelectedPath, inner.detailViewport.View(), inner.bundle.selectedPath(), tt.target)
				}

				model, cmd = clickAt(t, model, 2, bundleRowOf(t, inner, tt.target))
				inner = innerListModel(t, model)
				if inner.detailSelectedPath != tt.target || cmd == nil {
					t.Fatalf("click selected preview %q (request=%t), want %q with a request", inner.detailSelectedPath, cmd != nil, tt.target)
				}
				generation := inner.requests.active(requestDetail)
				loaded, ok := cmd().(detailContentMsg)
				if !ok {
					t.Fatalf("preview command returned %T, want detailContentMsg", loaded)
				}
				if loaded.path != tt.target || loaded.generation != generation {
					t.Fatalf("preview message path=%q generation=%v, want path=%q generation=%v", loaded.path, loaded.generation, tt.target, generation)
				}

				model, cmd = clickAt(t, model, 2, bundleRowOf(t, innerListModel(t, model), tt.target))
				assertNoClipboardWrite(t, cmd)
				if cmd != nil {
					t.Fatal("re-clicking while the preview request was outstanding requested another preview")
				}

				model, _ = updateFramePaced(t, model, loaded)
				inner = innerListModel(t, model)
				content := ansi.Strip(inner.detailViewport.View())
				if !strings.Contains(content, tt.target) || strings.Contains(content, "OLD PREVIEW:") {
					t.Fatalf("preview content = %q, want %q without old content", content, tt.target)
				}
				_, cmd = clickAt(t, model, 2, bundleRowOf(t, inner, tt.target))
				assertNoClipboardWrite(t, cmd)
				if cmd != nil {
					t.Fatal("re-clicking the completed preview requested another preview")
				}
			})
		}
	})

	t.Run("directory toggle does not preview a divergent tree file", func(t *testing.T) {
		m := bundleClickModel(t)
		m.bundle.cursor = 0
		m.bundle.toggle()
		if !m.bundle.restoreSelection("refs/c.md") {
			t.Fatal("could not select refs/c.md in the expanded tree")
		}
		m.detailSelectedPath = "b.md"
		m.detailViewport.SetContent("OLD PREVIEW: b.md")
		if m.bundle.selectedPath() == m.detailSelectedPath {
			t.Fatal("fixture did not diverge the tree selection and preview")
		}
		model := wrapClickModel(m)

		refs := bundleRowOf(t, innerListModel(t, model), "refs")
		model, cmd := clickAt(t, model, 2, refs)
		if cmd != nil {
			t.Fatal("collapsing a directory requested a file preview")
		}
		assertNoClipboardWrite(t, cmd)
		inner := innerListModel(t, model)
		if inner.detailSelectedPath != "b.md" || !strings.Contains(inner.detailViewport.View(), "OLD PREVIEW: b.md") {
			t.Fatalf("directory click changed preview path=%q content=%q", inner.detailSelectedPath, inner.detailViewport.View())
		}
		entry, ok := inner.bundle.selectedEntry()
		if !ok || entry.RelPath != "refs" || !entry.IsDir || entry.Expanded {
			t.Fatalf("collapsed directory selection = %+v, want collapsed refs", entry)
		}
	})
}

// spec: tui-mouse-navigation/clicks-that-do-not-target-a-navigable-row-are-ignored
func TestMouseClicksOutsideNavigableRowsAreIgnored(t *testing.T) {
	type gesture []tea.Msg
	click := func(x, y int) gesture {
		return gesture{tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y}, tea.MouseReleaseMsg{Button: tea.MouseLeft, X: x, Y: y}}
	}
	listCases := []struct {
		name            string
		setup           func(*Model)
		gesture         gesture
		horizontalWheel bool
	}{
		{name: "drag to another cell", gesture: gesture{
			tea.MouseClickMsg{Button: tea.MouseLeft, X: 2, Y: 3},
			tea.MouseMotionMsg{Button: tea.MouseLeft, X: 8, Y: 3},
			tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 8, Y: 3},
		}},
		{name: "drag that returns to the press cell", gesture: gesture{
			tea.MouseClickMsg{Button: tea.MouseLeft, X: 2, Y: 3},
			tea.MouseMotionMsg{Button: tea.MouseLeft, X: 2, Y: 4},
			tea.MouseMotionMsg{Button: tea.MouseLeft, X: 2, Y: 3},
			tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 2, Y: 3},
		}},
		{name: "shift click", gesture: gesture{
			tea.MouseClickMsg{Button: tea.MouseLeft, Mod: tea.ModShift, X: 2, Y: 3},
			tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 2, Y: 3},
		}},
		{name: "right click", gesture: gesture{
			tea.MouseClickMsg{Button: tea.MouseRight, X: 2, Y: 3},
			tea.MouseReleaseMsg{Button: tea.MouseRight, X: 2, Y: 3},
		}},
		{name: "release without press", gesture: gesture{tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 2, Y: 3}}},
		{name: "pane round trip before release", gesture: gesture{
			tea.MouseClickMsg{Button: tea.MouseLeft, X: 2, Y: 3},
			keyPress('?'),
			tea.KeyPressMsg{Code: tea.KeyEscape},
			tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 2, Y: 3},
		}},
		{name: "wheel before release", gesture: gesture{
			tea.MouseClickMsg{Button: tea.MouseLeft, X: 2, Y: 3},
			tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 2, Y: 3},
			tea.MouseWheelMsg{Button: tea.MouseWheelUp, X: 2, Y: 3},
			tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 2, Y: 3},
		}},
		{name: "horizontal wheel left before release", horizontalWheel: true, gesture: gesture{
			tea.MouseClickMsg{Button: tea.MouseLeft, X: 2, Y: 3},
			tea.MouseWheelMsg{Button: tea.MouseWheelLeft, X: 2, Y: 3},
			tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 2, Y: 3},
		}},
		{name: "horizontal wheel right before release", horizontalWheel: true, gesture: gesture{
			tea.MouseClickMsg{Button: tea.MouseLeft, X: 2, Y: 3},
			tea.MouseWheelMsg{Button: tea.MouseWheelRight, X: 2, Y: 3},
			tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 2, Y: 3},
		}},
		{name: "header", gesture: click(2, 0)},
		{name: "footer", gesture: click(2, 23)},
		{name: "empty list row", gesture: click(2, 10)},
		{name: "right-hand pane", gesture: click(60, 3)},
		{name: "search row", setup: func(m *Model) { m.filter = "a" }, gesture: click(2, 1)},
		{name: "typing a search", setup: func(m *Model) { m.filterActive = true }, gesture: click(2, 4)},
		{name: "command palette", setup: func(m *Model) { m.palette.open = true }, gesture: click(2, 3)},
		{name: "help overlay", setup: func(m *Model) { m.pane = paneHelp }, gesture: click(2, 3)},
		{name: "dialog", setup: func(m *Model) { m.pane = paneToggleModal }, gesture: click(2, 3)},
		{name: "below minimum size", setup: func(m *Model) { m.width = 40 }, gesture: click(2, 3)},
	}
	for _, tt := range listCases {
		t.Run(tt.name, func(t *testing.T) {
			model, _ := newFramePacedListModel(t, []string{"alpha", "beta", "gamma", "delta"})
			inner := innerListModel(t, model)
			if tt.setup != nil {
				tt.setup(&inner)
			}
			wantPane, wantFilter := inner.pane, inner.filterActive
			wantFolds := maps.Clone(inner.currentFoldState())
			model = wrapClickModel(inner)
			var cmd tea.Cmd
			for _, msg := range tt.gesture {
				model, cmd = updateFramePaced(t, model, msg)
				if wheel, ok := msg.(tea.MouseWheelMsg); ok && tt.horizontalWheel {
					if cmd != nil || model.framePending {
						t.Fatalf("horizontal wheel scheduled command %v or paced frame %t", cmd != nil, model.framePending)
					}
					current := innerListModel(t, model)
					if current.cursor != 0 || current.pane != wantPane || !maps.Equal(current.currentFoldState(), wantFolds) {
						t.Fatalf("horizontal wheel %v changed cursor=%d pane=%d folds=%v", wheel.Button, current.cursor, current.pane, current.currentFoldState())
					}
				} else {
					assertNoClipboardWrite(t, cmd)
				}
			}
			got := innerListModel(t, model)
			if got.cursor != 0 || got.pane != wantPane || got.filterActive != wantFilter {
				t.Fatalf("cursor=%d pane=%d filterActive=%t, want cursor 0 pane=%d filterActive=%t",
					got.cursor, got.pane, got.filterActive, wantPane, wantFilter)
			}
			if !maps.Equal(got.currentFoldState(), wantFolds) {
				t.Fatalf("fold state changed to %v", got.currentFoldState())
			}
		})
	}

	t.Run("bundle preview pane", func(t *testing.T) {
		m := bundleClickModel(t)
		wantCursor := m.bundle.cursor
		model := wrapClickModel(m)
		model, cmd := clickAt(t, model, 60, 2)
		assertNoClipboardWrite(t, cmd)
		if got := innerListModel(t, model); got.detailSelectedPath != "a.md" || got.bundle.cursor != wantCursor {
			t.Fatalf("preview click changed the tree selection to %q", got.detailSelectedPath)
		}
	})
}
