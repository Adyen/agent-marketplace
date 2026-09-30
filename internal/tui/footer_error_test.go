package tui

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/charmbracelet/x/ansi"
)

func TestFooterErrorPriorityKeepsRecoveryActions(t *testing.T) {
	for _, width := range []int{50, 59, 60, 80, 100, 160} {
		for _, height := range []int{12, 24} {
			m := NewModel(t.TempDir(), t.TempDir())
			m.width, m.height = width, height
			m.loading = false
			m.driftAssessment = driftReady
			m.discoveryOutcomes = []marketplace.DiscoveryOutcome{{Truncated: true}}
			m.errs = []error{errors.New("config mutation failed: " + strings.Repeat("unsafe/path ", 12) + " COMPLETE-DIAGNOSTIC")}
			m.warnings = []error{errors.New("warning one"), errors.New("warning two"), errors.New("warning three")}
			m.toast = "active transient toast"
			m.toastExp = time.Now().Add(time.Minute)
			footer := ansi.Strip(renderFooter(m))
			if !strings.Contains(footer, "! 1 errors") {
				t.Fatalf("width %d height %d omitted error count: %q", width, height, footer)
			}
			for _, action := range []string{"e:errors", "r:reload", "q:quit"} {
				if !strings.Contains(footer, action) {
					t.Fatalf("width %d height %d omitted %q: %q", width, height, action, footer)
				}
			}
			if strings.Contains(footer, "active transient toast") {
				t.Fatalf("active toast replaced persistent error: %q", footer)
			}
			if ansi.StringWidth(footer) > width {
				t.Fatalf("width %d exceeded: %d %q", width, ansi.StringWidth(footer), footer)
			}
			output := m.View().Content
			assertTerminalBounds(t, output, width, height)
			mi, _ := m.Update(tea.KeyPressMsg{Code: 'e', Text: string('e')})
			updated, ok := mi.(Model)
			if !ok {
				t.Fatalf("error key update type %T", mi)
			}
			if updated.pane != paneErrors {
				t.Fatalf("error key opened pane %v", updated.pane)
			}
			overlay := ansi.Strip(renderErrors(updated))
			if !strings.Contains(overlay, "config mutation failed") || !strings.Contains(overlay, "COMPLETE-DIAGNOSTIC") {
				t.Fatalf("error overlay lost complete diagnostic at width %d: %q", width, overlay)
			}
		}
	}
}

func TestFooterErrorHandlesNilValues(t *testing.T) {
	var typedNil *os.PathError
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, "! 1 errors"},
		{"typed-nil", typedNil, "<nil>"},
		{"ordinary", errors.New("selection failed"), "selection failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewModel(t.TempDir(), t.TempDir())
			m.width, m.height = 80, 24
			m.loading = false
			m.errs = []error{tc.err}
			footer := ansi.Strip(renderFooter(m))
			for _, want := range []string{"! 1 errors", tc.want, "e:errors", "r:reload", "q:quit"} {
				if !strings.Contains(footer, want) {
					t.Fatalf("footer %q missing %q", footer, want)
				}
			}
			if ansi.StringWidth(footer) > m.width {
				t.Fatalf("footer exceeds width: %q", footer)
			}
		})
	}
}

func TestFooterErrorFilterActions(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	m.width, m.height = 80, 24
	m.filterActive = true
	m.errs = []error{errors.New("unsafe \x1b[2J mutation")}
	footer := ansi.Strip(renderFooter(m))
	if !strings.Contains(footer, "! 1 errors") || !strings.Contains(footer, "enter apply · esc cancel · q quit") {
		t.Fatalf("filter footer = %q", footer)
	}
	if strings.Contains(footer, "\x1b") {
		t.Fatalf("unsafe error escaped footer: %q", footer)
	}
}
