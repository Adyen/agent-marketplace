package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// assertCompleteFrame checks that a rendered modal is one rounded frame no
// wider than width, with every row closed by the border.
func assertCompleteFrame(t *testing.T, name, modal string, width int) []string {
	t.Helper()
	lines := strings.Split(ansi.Strip(modal), "\n")
	if len(lines) < 3 {
		t.Fatalf("%s: modal has no frame:\n%s", name, strings.Join(lines, "\n"))
	}
	top, bottom := lines[0], lines[len(lines)-1]
	if !strings.HasPrefix(top, "╭") || !strings.HasSuffix(top, "╮") || !strings.HasPrefix(bottom, "╰") || !strings.HasSuffix(bottom, "╯") {
		t.Fatalf("%s: incomplete border:\n%s", name, strings.Join(lines, "\n"))
	}
	for _, l := range lines {
		if w := ansi.StringWidth(l); w > width {
			t.Fatalf("%s: row is %d columns in a %d-column terminal: %q", name, w, width, l)
		}
		if ansi.StringWidth(l) != ansi.StringWidth(top) {
			t.Fatalf("%s: ragged frame row %q", name, l)
		}
	}
	for _, l := range lines[1 : len(lines)-1] {
		if !strings.HasPrefix(l, "│") || !strings.HasSuffix(l, "│") {
			t.Fatalf("%s: row escapes the border: %q", name, l)
		}
	}
	return lines
}

// spec: tui-rendering/modal-at-common-sizes
func TestEveryModalRendersOneBoundedFrame(t *testing.T) {
	repo := "/Users/someone/src/worktrees/agent-marketplace/a-very-long-feature-branch-worktree-name"
	modals := map[string]func(Model) string{
		"toggle": func(m Model) string {
			m.modalSkill = mkRow("resource", withProject)
			return renderToggleModal(m)
		},
		"harness": func(m Model) string {
			m.globalHarnesses = []string{"claude", "devin"}
			m.openHarnessModal()
			return renderHarnessModal(m)
		},
		"init": func(m Model) string {
			m.initModal = initModalState{selected: map[string]bool{"factory": true}, detected: true}
			return renderInitModal(m)
		},
		"edit config": renderEditConfigModal,
		"migration": func(m Model) string {
			m.migrationPrompt = migrationPrompt{Configs: []migrationConfig{{Label: "Project", Path: repo + "/.agents/config.yaml"}}}
			return renderMigrationModal(m)
		},
	}
	for _, size := range [][2]int{{50, 12}, {80, 24}, {120, 34}} {
		for name, render := range modals {
			label := fmt.Sprintf("%s %dx%d", name, size[0], size[1])
			m := NewModel(repo, "/Users/someone")
			m.width, m.height = size[0], size[1]
			out := render(m)
			lines := assertCompleteFrame(t, label, out, size[0])
			if len(lines) > size[1] {
				t.Fatalf("%s: modal is %d rows in a %d-row terminal", label, len(lines), size[1])
			}
			plain := strings.Join(lines, "\n")
			if strings.Contains(plain, "┃") {
				t.Fatalf("%s: a second focus bar duplicates the option cursor:\n%s", label, plain)
			}
			if n := strings.Count(plain, "> "); n > 1 {
				t.Fatalf("%s: %d focus indicators, want at most one:\n%s", label, n, plain)
			}
			cancel := "esc cancel"
			if name == "migration" {
				cancel = "l/esc leave unchanged"
			}
			if !strings.Contains(plain, cancel) {
				t.Fatalf("%s: the cancel path %q must stay visible:\n%s", label, cancel, plain)
			}
			if name == "edit config" && size[1] >= 20 && !strings.Contains(plain, "config.local.yaml") {
				t.Fatalf("%s: a shortened path must keep its file name:\n%s", label, plain)
			}
		}
	}
}

func TestShortenPathKeepsFileName(t *testing.T) {
	got := shortenPath("/Users/someone/src/long/nested/path/.agents/config.local.yaml", "/Users/someone", 30)
	if ansi.StringWidth(got) > 30 || !strings.HasSuffix(got, "config.local.yaml") || !strings.HasPrefix(got, "~/") {
		t.Fatalf("shortenPath = %q", got)
	}
	if got := shortenPath("/etc/agent-marketplace/config.yaml", "/Users/someone", 60); got != "/etc/agent-marketplace/config.yaml" {
		t.Fatalf("a path that fits must stay unchanged, got %q", got)
	}
}

func TestDetailLabelsNeverTouchTheirValues(t *testing.T) {
	out := ansi.Strip(keyValueTable(newTheme(true), [][2]string{{"Kind:", "skill"}, {"Registry:", "https://example.test"}}))
	if !strings.Contains(out, "Registry: https://example.test") || strings.Contains(out, "Registry:https") {
		t.Fatalf("the longest label must be separated from its value:\n%s", out)
	}
}
