package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestCompositeModalPreservesBaseAndTopContent(t *testing.T) {
	base := strings.Join([]string{"abcdefghij", "klmnopqrst", "uvwxyz0123"}, "\n")
	top := "TT"
	got := ansi.Strip(compositeModal(base, top, 10, 3))
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("composite line count = %d:\n%s", len(lines), got)
	}
	if !strings.Contains(lines[1], "TT") {
		t.Fatalf("top layer not composited into middle row:\n%s", got)
	}
	if !strings.HasPrefix(lines[0], "abcdefghij") || !strings.HasPrefix(lines[2], "uvwxyz0123") {
		t.Fatalf("base rows not preserved:\n%s", got)
	}
}

func TestCompositeModalClipsOversizedTop(t *testing.T) {
	base := "aaaaaaaaaaaaaaaaaaaa"
	top := strings.Join([]string{"TTTTTTTTTTTTTTTTTTTT", "TTTTTTTTTTTTTTTTTTTT", "TTTTTTTTTTTTTTTTTTTT"}, "\n")
	got := ansi.Strip(compositeModal(base, top, 10, 1))
	for _, line := range strings.Split(got, "\n") {
		if w := ansi.StringWidth(line); w > 10 {
			t.Fatalf("composite exceeded bound: %d %q", w, line)
		}
	}
}
