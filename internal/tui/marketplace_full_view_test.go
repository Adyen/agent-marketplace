package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func marketplaceBundleModel(t *testing.T, dir string, width int) Model {
	t.Helper()
	m := modelWithSelectedRow(t, installedMarketplaceRow(ir.KindSkill, "grill", dir))
	m.width = width
	m.height = 16
	m, cmd := enterDetail(t, m)
	if m.bundle == nil {
		t.Fatalf("bundle did not open; viewport:\n%s", ansi.Strip(m.detailViewport.View()))
	}
	if cmd == nil {
		t.Fatal("enter did not schedule a primary preview")
	}
	updated, _ := m.Update(cmd().(detailContentMsg))
	return updated.(Model)
}

// spec: tui-mouse-navigation/long-bundle-labels-do-not-displace-selectable-preview-text
func TestMarketplaceFullViewLongRootKeepsPreviewSelectable(t *testing.T) {
	primary := "alpha 日本語 beta"
	for _, width := range []int{50, 80} {
		for _, rootLen := range []int{70, 8} {
			t.Run(fmt.Sprintf("width%d-root%d", width, rootLen), func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), strings.Repeat("r", rootLen))
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Grill\n\n"+primary), 0o644); err != nil {
					t.Fatal(err)
				}
				m := marketplaceBundleModel(t, dir, width)
				listWidth, _ := paneWidths(width)
				tree := renderBundleTree(m, listWidth, max(1, m.height-1))
				for i, line := range strings.Split(tree, "\n") {
					if got := ansi.StringWidth(line); got > listWidth {
						t.Fatalf("tree row %d is %d columns wide, exceeds allocated %d: %q", i, got, listWidth, line)
					}
				}
				frame := ansi.Strip(m.View().Content)
				x0, y0 := cellOf(t, frame, "alpha")
				if x0 != listWidth+2 {
					t.Fatalf("preview text begins at column %d, want %d (allocated %d + margin); frame:\n%s", x0, listWidth+2, listWidth, frame)
				}

				model := newFramePacedModel(m)
				model.View()
				var cmd tea.Cmd
				for _, ev := range []tea.MouseMsg{selClick(x0, y0), selMotion(x0+11, y0), selRelease(x0+11, y0)} {
					model, cmd = updateFramePaced(t, model, ev)
				}
				if got := clipboardText(t, cmd); got != "alpha 日本語" {
					t.Fatalf("copied %q, want %q (tree text must not leak)", got, "alpha 日本語")
				}
				inner := innerListModel(t, model)
				if !inner.previewSel.copied {
					t.Fatal("release did not leave a copied selection")
				}
				lo, hi := highlightCols(t, model.View().Content, y0)
				if lo != x0 || hi != x0+12 {
					t.Fatalf("highlight covers [%d,%d), want [%d,%d)", lo, hi, x0, x0+12)
				}
			})
		}
	}

	m := selectionDetailModel(t, "plain preview alpha")
	snap := selSnap(m)
	_, cmd := selDrag(t, m, snap, 33, 0, 37, 0)
	if got := clipboardText(t, cmd); got != "lain" {
		t.Fatalf("baseline selection copied %q, want %q", got, "lain")
	}
}

// spec: tui-mouse-navigation/marketplace-selection-and-scrolling-preserve-clipboard-boundaries
func TestMarketplaceFullViewSelectionBoundaries(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Grill\n\n"+strings.Repeat("preview line\n", 40)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("wheel target note\n\n"+strings.Repeat("note line\n\n", 40)), 0o644); err != nil {
		t.Fatal(err)
	}
	newModel := func(t *testing.T) Model { return marketplaceBundleModel(t, dir, 80) }

	t.Run("preview drag copies and wheel keeps routing", func(t *testing.T) {
		m := newModel(t)
		listWidth, _ := paneWidths(m.width)
		snap := selSnap(m)
		x0, y0 := cellOf(t, ansi.Strip(m.View().Content), "preview line")
		m, cmd := selDrag(t, m, snap, x0, y0, x0+6, y0)
		if got := clipboardText(t, cmd); got != "preview" {
			t.Fatalf("preview drag copied %q, want %q", got, "preview")
		}
		if !m.previewSel.copied {
			t.Fatal("drag left no copied selection")
		}
		offset := m.detailViewport.YOffset()
		next, wheelCmd := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 1, Y: 1})
		m = next.(Model)
		if m.detailSelectedPath != "notes.md" {
			t.Fatalf("tree wheel selected %q, want notes.md", m.detailSelectedPath)
		}
		if m.detailViewport.YOffset() != offset {
			t.Fatalf("tree wheel moved preview offset to %d, want %d", m.detailViewport.YOffset(), offset)
		}
		if wheelCmd == nil {
			t.Fatal("tree wheel did not schedule the next file preview")
		}
		updated, _ := m.Update(wheelCmd().(detailContentMsg))
		m = updated.(Model)
		if !contains(ansi.Strip(m.detailViewport.View()), "wheel target note") {
			t.Fatalf("tree wheel preview missing notes body:\n%s", ansi.Strip(m.detailViewport.View()))
		}
		next, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: listWidth + 4, Y: 3})
		m = next.(Model)
		if m.previewSel.active || m.detailViewport.YOffset() != 1 {
			t.Fatalf("preview wheel: active=%v offset=%d", m.previewSel.active, m.detailViewport.YOffset())
		}
	})

	for _, tc := range []struct{ name string }{{name: "tree drag"}, {name: "footer drag"}} {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel(t)
			snap := selSnap(m)
			x, y := 40, m.height-1
			if tc.name == "tree drag" {
				x, y = 2, 1
			}
			var cmd tea.Cmd
			for _, ev := range []tea.MouseMsg{selClick(x, y), selMotion(x+4, y), selRelease(x+4, y)} {
				m, cmd = selSend(t, m, snap, ev)
				if cmd != nil {
					t.Fatalf("%s scheduled %v", tc.name, cmd)
				}
			}
			if m.previewSel.active || m.previewSel.copied {
				t.Fatalf("%s produced a selection %+v", tc.name, m.previewSel)
			}
		})
	}

	t.Run("selection clears on scroll, resize and content change", func(t *testing.T) {
		m := newModel(t)
		snap := selSnap(m)
		x0, y0 := cellOf(t, ansi.Strip(m.View().Content), "preview line")
		m, cmd := selDrag(t, m, snap, x0, y0, x0+6, y0)
		if clipboardText(t, cmd) == "" {
			t.Fatal("setup drag did not copy")
		}
		next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
		m = next.(Model)
		if m.previewSel.active {
			t.Fatal("resize kept the selection")
		}
		snap = selSnap(m)
		x0, y0 = cellOf(t, ansi.Strip(m.View().Content), "preview line")
		m, cmd = selDrag(t, m, snap, x0, y0, x0+6, y0)
		if clipboardText(t, cmd) == "" {
			t.Fatal("re-selection after resize did not copy")
		}
		generation := m.beginRequest(requestDetail)
		next, _ = m.Update(detailContentMsg{generation: generation, path: m.detailSelectedPath, content: "replaced content", width: m.previewWidth()})
		m = next.(Model)
		if m.previewSel.active {
			t.Fatal("accepted content replacement kept the selection")
		}
	})
}
