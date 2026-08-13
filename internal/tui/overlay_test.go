package tui

import (
	"strings"
	"testing"
)

func TestOverlayAtPlacesTopOverBase(t *testing.T) {
	base := "aaaaaaaaaa\nbbbbbbbbbb\ncccccccccc\ndddddddddd"
	top := "XY\nZW"

	got := overlayAt(base, top, 2, 1)
	wantLines := []string{
		"aaaaaaaaaa",
		"bbXYbbbbbb",
		"ccZWcccccc",
		"dddddddddd",
	}
	want := strings.Join(wantLines, "\n")
	if got != want {
		t.Fatalf("overlayAt result:\n%s\n\nwant:\n%s", got, want)
	}
}

func TestOverlayAtPadsShortBaseLines(t *testing.T) {
	base := "a\nb"
	top := "XY"

	got := overlayAt(base, top, 3, 0)
	// base line 0 ("a") is shorter than col 3, so it should be padded
	// with two spaces before "XY".
	wantLine0 := "a  XY"
	if lines := strings.Split(got, "\n"); lines[0] != wantLine0 {
		t.Fatalf("short base line not padded: got %q, want %q", lines[0], wantLine0)
	}
}

func TestOverlayAtClipsTopLinesBelowBase(t *testing.T) {
	base := "aaaa"
	top := "X\nY\nZ"

	got := overlayAt(base, top, 1, 0)
	lines := strings.Split(got, "\n")
	// Only 1 line of base, so 'Y' and 'Z' should be silently dropped.
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "X") {
		t.Fatalf("expected X in base line, got %q", lines[0])
	}
	if strings.Contains(got, "Y") || strings.Contains(got, "Z") {
		t.Fatalf("top lines Y/Z should have been clipped, got %q", got)
	}
}

func TestCenterOverlayCentersWithinBox(t *testing.T) {
	// 6-wide x 3-tall base
	base := "aaaaaa\nbbbbbb\ncccccc"
	top := "XX"

	got := centerOverlay(base, top, 6, 3)
	// top is 2 cells wide, box is 6 → col = (6-2)/2 = 2
	// top is 1 line tall, box is 3 → row = (3-1)/2 = 1
	wantLines := []string{
		"aaaaaa",
		"bbXXbb",
		"cccccc",
	}
	if got != strings.Join(wantLines, "\n") {
		t.Fatalf("centerOverlay:\n%s\nwant:\n%s", got, strings.Join(wantLines, "\n"))
	}
}

func TestModalViewCompositesOnTopOfBaseLayout(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.loading = false
	m.width = 80
	m.height = 24
	m.rows = []row{mkRow("audit", withGlobal, withRegistry("sec.git", "security"))}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	m.pane = paneToggleModal
	m.modalSkill = m.rows[0]

	out := m.View()
	// Base layout still visible: tab bar labels.
	if !contains(out, "Scope") {
		t.Fatalf("modal view should still show the tab bar (Scope label), got:\n%s", out)
	}
	// Modal content visible on top.
	if !contains(out, "audit") {
		t.Fatalf("modal view should show the skill name, got:\n%s", out)
	}
	if !contains(out, "[g]") {
		t.Fatalf("modal view should show the action hints, got:\n%s", out)
	}
}
