package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestHelpEveryBindingReachableByScrollingAtMinimumSize(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 12})
	m = updated.(Model)
	m.pane = paneHelp
	m.helpViewport.SetContent(renderHelp(m))

	var seen strings.Builder
	for {
		seen.WriteString(ansi.Strip(m.helpViewport.View()))
		seen.WriteByte('\n')
		if m.helpViewport.AtBottom() {
			break
		}
		m.helpViewport.ScrollDown(1)
	}
	visible := seen.String()

	for _, sec := range m.helpSections() {
		if !strings.Contains(visible, sec.title) {
			t.Fatalf("help section %q never reachable at 50x12", sec.title)
		}
		for _, kb := range sec.bindings {
			if !kb.Enabled() {
				continue
			}
			h := kb.Help()
			if !strings.Contains(visible, h.Key) || !strings.Contains(visible, h.Desc) {
				t.Fatalf("binding %q (%q) not reachable by scrolling help at 50x12", h.Key, h.Desc)
			}
		}
	}
}

func TestHelpCloseAndQuitHintsStayVisibleWhileScrolling(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 12})
	m = updated.(Model)
	m.pane = paneHelp
	m.helpViewport.SetContent(renderHelp(m))

	for i := 0; i < 4; i++ {
		out := m.View().Content
		if !strings.Contains(out, "esc/? close") || !strings.Contains(out, "Ctrl+C quit") {
			t.Fatalf("close/quit hints must stay visible at offset %d:\n%s", m.helpViewport.YOffset(), out)
		}
		m.helpViewport.ScrollDown(m.helpViewport.Height() / 2)
	}
}

func TestHelpOffsetPreservedAcrossReopen(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 12})
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	m = updated.(Model)
	offset := m.helpViewport.YOffset()
	if offset == 0 {
		t.Fatalf("help did not scroll at 50x12")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	m = updated.(Model)
	if m.helpViewport.YOffset() != offset {
		t.Fatalf("help offset not preserved across reopen: got %d want %d", m.helpViewport.YOffset(), offset)
	}
}
