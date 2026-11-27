package tui

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func footerFixture(width int) Model {
	m := NewModel("/repo", "/home")
	m.loading = false
	m.width, m.height = width, 24
	m.rows = []row{mkRow("audit", withGlobal, withRegistry("sec.git", "security"))}
	m.recomputeView()
	return m
}

func assertFooterFits(t *testing.T, footer string, width int, want ...string) {
	t.Helper()
	plain := ansi.Strip(footer)
	if w := ansi.StringWidth(plain); w > width {
		t.Fatalf("footer is %d columns wide, terminal is %d: %q", w, width, plain)
	}
	if strings.Contains(plain, "…") {
		t.Fatalf("footer hints must drop whole, never cut mid-word: %q", plain)
	}
	for _, s := range want {
		if !strings.Contains(plain, s) {
			t.Fatalf("footer at %d columns is missing %q: %q", width, s, plain)
		}
	}
}

func assertFooterHintStyle(t *testing.T, footer, key, desc string, colored bool) {
	t.Helper()
	want := key + " " + desc
	if colored {
		want = lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(14)).Render(key) +
			" " + lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(8)).Render(desc)
	} else {
		footer = ansi.Strip(footer)
	}
	if !strings.Contains(footer, want) {
		t.Fatalf("footer hint %q does not use the expected key and description styles: %q", key+" "+desc, footer)
	}
}

// spec: tui-feedback/primary-and-floor-footer-hints-share-key-emphasis
func TestFooterShortcutKeyEmphasis(t *testing.T) {
	m := footerFixture(160)
	hints := [][2]string{
		{"/", "search"},
		{"space", "toggle"},
		{"s", "sync"},
		{"H", "harnesses"},
		{"E", "config"},
		{"v", "view"},
		{"i", "install from source"},
		{":", "commands"},
		{"?", "help"},
		{"q", "quit"},
	}
	for _, profile := range []struct {
		name    string
		profile colorprofile.Profile
		colored bool
	}{
		{name: "color", profile: colorprofile.ANSI256, colored: true},
		{name: "colorless", profile: colorprofile.Ascii},
	} {
		t.Run(profile.name, func(t *testing.T) {
			var output bytes.Buffer
			writer := colorprofile.NewWriter(&output, nil)
			writer.Profile = profile.profile
			if _, err := writer.Write([]byte(renderFooter(m))); err != nil {
				t.Fatal(err)
			}
			footer := output.String()
			for _, hint := range hints {
				assertFooterHintStyle(t, footer, hint[0], hint[1], profile.colored)
			}
			if !profile.colored && strings.Contains(footer, "\x1b[38;") {
				t.Fatalf("colorless footer contains ANSI color: %q", footer)
			}
		})
	}
}

// spec: tui-feedback/narrow-widths-retain-floor-priority-order
func TestFooterNarrowRetainsFloorPriority(t *testing.T) {
	wide := renderFooter(footerFixture(120))
	assertFooterFits(t, wide, 120, "v view")
	m := footerFixture(50)
	m.height = 12
	footer := renderFooter(m)
	assertFooterFits(t, footer, 50, ": commands", "? help", "q quit")
	if strings.Contains(ansi.Strip(footer), "v view") {
		t.Fatalf("narrow footer kept the lowest-priority action hint: %q", ansi.Strip(footer))
	}
}

// spec: tui-feedback/narrow-footer-with-status
func TestFooterAlwaysShowsPaletteHelpAndQuit(t *testing.T) {
	states := map[string]func(*Model){
		"idle": func(*Model) {},
		"warnings and Marketplace health": func(m *Model) {
			m.warnings = []error{errors.New("w1"), errors.New("w2")}
			m.discoveryOutcomes = []marketplace.DiscoveryOutcome{{CacheState: "stale"}, {Truncated: true}}
			m.discoveryErrs = []error{errors.New("down")}
		},
		"active sync": func(m *Model) {
			m.syncing = true
			m.warnings = []error{errors.New("w1")}
		},
	}
	for _, width := range []int{50, 80, 120} {
		for name, apply := range states {
			t.Run(fmt.Sprintf("%s/%d", name, width), func(t *testing.T) {
				m := footerFixture(width)
				apply(&m)
				assertFooterFits(t, renderFooter(m), width, ": commands", "? help", "q quit")
			})
		}
	}
}

func TestFooterShowsActionHintsWhenWide(t *testing.T) {
	m := footerFixture(120)
	assertFooterFits(t, renderFooter(m), 120, "~ drift unavailable", "/ search", "s sync", ": commands", "v view")
}

func TestFooterShowsInstallSourceHintWhenWide(t *testing.T) {
	m := footerFixture(160)
	assertFooterFits(t, renderFooter(m), 160, "i install from source")
}

func TestFooterErrorStateKeepsDiagnosticsAndFloor(t *testing.T) {
	for _, width := range []int{50, 120} {
		m := footerFixture(width)
		m.errs = []error{errors.New("boom")}
		assertFooterFits(t, renderFooter(m), width, "! 1 errors", "e errors", "r reload", ": c", "q quit")
	}
}

// The errors hint appears only when the errors view has something to show:
// plain pending drift keeps sync as its recovery hint.
// spec: tui-feedback/errors-hint-only-with-diagnostics
func TestFooterOffersErrorsOnlyWithDiagnostics(t *testing.T) {
	pending := func(m *Model) {
		m.driftAssessment = driftReady
		m.driftReport = materialize.DriftReport{Missing: []materialize.DriftItem{{Name: "audit"}}}
	}
	m := footerFixture(120)
	pending(&m)
	footer := ansi.Strip(renderFooter(m))
	assertFooterFits(t, footer, 120, "~ 1 out of sync", "s sync", "r reload")
	if strings.Contains(footer, "e errors") {
		t.Fatalf("plain drift offered the errors view, which has nothing to show: %q", footer)
	}
	if m.actionEligible(actionDiagnostics) {
		t.Fatal("the palette offered the errors view for plain drift")
	}

	m.warnings = []error{errors.New("w1")}
	assertFooterFits(t, renderFooter(m), 120, "⚠ 1 warnings", "e errors")
	if !m.actionEligible(actionDiagnostics) {
		t.Fatal("the palette hid the errors view while a warning is present")
	}
}

// spec: tui-feedback/sync-recovery-shortcut-remains-visible
func TestMissingSnapshotFooterPrioritizesSync(t *testing.T) {
	for _, width := range []int{50, 80, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := footerFixture(width)
			m.driftAssessment = driftFailed
			m.replaceSnapshotDiagnostics([]tuiDiagnostic{snapshotDiagnostic(diagnosticWarning, session.DiagnosticCatalog, cache.ErrExactGitSnapshotUnavailable)}, nil, nil)
			footer := renderFooter(m)
			assertFooterFits(t, footer, width, "s sync", ": commands", "? help", "q quit")
			if strings.Count(ansi.Strip(footer), "s sync") != 1 {
				t.Fatalf("sync hint was duplicated: %q", ansi.Strip(footer))
			}
		})
	}
}

func TestFooterSearchModeShowsApplyAndCancel(t *testing.T) {
	for _, width := range []int{50, 120} {
		m := footerFixture(width)
		m.filterActive = true
		footer := renderFooter(m)
		assertFooterFits(t, footer, width, "enter apply", "esc cancel")
		if strings.Contains(ansi.Strip(footer), ": commands") {
			t.Fatalf("typed characters are query text while searching; the palette hint must not show: %q", ansi.Strip(footer))
		}
	}
}
