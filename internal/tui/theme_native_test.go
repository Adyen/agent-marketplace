package tui

import (
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/charmbracelet/x/ansi"
)

// TestThemeRolesUseANSI16Colors verifies the semantic theme maps every role
// onto the terminal's ANSI-16 palette (inheriting user colors) rather than
// emitting truecolor escapes.
func TestThemeRolesUseANSI16Colors(t *testing.T) {
	theme := newTheme(true)
	roles := map[string]color.Color{
		"accent":  theme.accent,
		"ok":      theme.ok,
		"warn":    theme.warn,
		"mark":    theme.mark,
		"err":     theme.err,
		"muted":   theme.muted,
		"text":    theme.text,
		"surface": theme.surface,
	}
	for name, c := range roles {
		out := lipgloss.NewStyle().Foreground(c).Render("x")
		if !strings.Contains(out, "\x1b[") {
			t.Fatalf("role %s rendered without ANSI color: %q", name, out)
		}
		if strings.Contains(out, "38;2;") {
			t.Fatalf("role %s emitted a truecolor sequence instead of ANSI-16: %q", name, out)
		}
	}
	// The text role picks a readable ANSI foreground for each background.
	darkText := lipgloss.NewStyle().Foreground(newTheme(true).text).Render("x")
	lightText := lipgloss.NewStyle().Foreground(newTheme(false).text).Render("x")
	if darkText == lightText {
		t.Fatalf("text role did not adapt to background: %q", darkText)
	}
}

func stubColorCapable(t *testing.T, capable bool) {
	t.Helper()
	previous := colorCapable
	colorCapable = func() bool { return capable }
	t.Cleanup(func() { colorCapable = previous })
}

// TestStatusSymbolsColorProfile verifies styled status symbols on
// color-capable terminals and the retained ASCII glyphs on no-color ones.
// spec: tui-rendering/colorless-and-narrow-rendering
func TestStatusSymbolsColorProfile(t *testing.T) {
	cases := []struct {
		name   string
		row    row
		ascii  string
		symbol string
	}{
		{"enabled", mkRow("s"), "[x]", "●"},
		{"available", mkRow("s", withAvailable), "[ ]", "○"},
		{"pending", mkRow("s", withDrift(materialize.DriftMissing)), "[*]", "◐"},
		{"orphaned", mkRow("s", withDrift(materialize.DriftOrphaned)), "[-]", "◌"},
		{"broken", mkRow("s", withDrift(materialize.DriftBroken)), "[!]", "✗"},
		{"required", mkRow("s", withRequired), "[R]", "■"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stubColorCapable(t, true)
			if got := statusSymbol(tc.row); got != tc.symbol {
				t.Fatalf("statusSymbol = %q, want %q", got, tc.symbol)
			}
			stubColorCapable(t, false)
			if got := statusSymbol(tc.row); got != tc.ascii {
				t.Fatalf("no-color statusSymbol = %q, want readable ASCII %q", got, tc.ascii)
			}
		})
	}
}

// TestScopeChips verifies chip labels render at normal widths while the
// compact layout keeps the letter badge form.
func TestScopeChips(t *testing.T) {
	cases := []struct {
		name  string
		row   row
		chips []string
	}{
		{"global only", mkRow("s", withGlobal), []string{"global"}},
		{"project+local", mkRow("s", withProject, withLocal), []string{"project", "local"}},
		{"all scopes", mkRow("s", withGlobal, withProject, withLocal), []string{"global", "project", "local"}},
		{"required", mkRow("s", withRequired, withProject), []string{"required"}},
		{"project excluded locally", mkRow("s", withProject, withExcluded), []string{"project", "local-excluded"}},
		{"available", mkRow("s", withAvailable), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scopeChips(tc.row)
			if len(got) != len(tc.chips) {
				t.Fatalf("scopeChips = %v, want %v", got, tc.chips)
			}
			for i := range got {
				if got[i] != tc.chips[i] {
					t.Fatalf("scopeChips = %v, want %v", got, tc.chips)
				}
			}
			joined := strings.Join(tc.chips, " ")
			if text := scopeBadgeText(tc.row, false); text != joined {
				t.Fatalf("scopeBadgeText non-compact = %q, want %q", text, joined)
			}
			if text := scopeBadgeText(tc.row, true); text != originBadge(tc.row) {
				t.Fatalf("scopeBadgeText compact = %q, want letter badge %q", text, originBadge(tc.row))
			}
		})
	}
}

// TestCompositeModalLayer verifies modal composition: the modal lands
// centered over the base inside the given bounds and preserves styling on
// both layers.
func TestCompositeModalLayer(t *testing.T) {
	base := strings.Join([]string{
		"aaaaaaaaaa",
		"bbbbbbbbbb",
		"cccccccccc",
		"dddddddddd",
	}, "\n")
	modal := lipgloss.NewStyle().Render("XX")
	out := compositeModal(base, modal, 10, 4)
	stripped := ansi.Strip(out)
	lines := strings.Split(stripped, "\n")
	if len(lines) != 4 {
		t.Fatalf("composite changed line count: %q", stripped)
	}
	if !strings.Contains(lines[1], "XX") {
		t.Fatalf("modal not centered on expected row:\n%s", stripped)
	}
	if !strings.HasPrefix(lines[1], "bbbb") || !strings.HasSuffix(lines[1], "bbbb") {
		t.Fatalf("modal did not splice into the middle of the base line: %q", lines[1])
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w > 10 {
			t.Fatalf("line %d exceeds bound: %d %q", i, w, line)
		}
	}
	if got := compositeModal(base, "", 10, 4); got != base {
		t.Fatalf("empty modal should leave the base untouched: %q", got)
	}
}

// TestStatusBarFitsWithinTerminal verifies the status bar keeps whole
// action groups within narrow widths.
func TestStatusBarFitsWithinTerminal(t *testing.T) {
	for _, width := range []int{50, 60, 80} {
		m := NewModel(t.TempDir(), t.TempDir())
		m.width, m.height = width, 12
		m.loading = false
		m.driftAssessment = driftReady
		footer := ansi.Strip(renderFooter(m))
		if w := ansi.StringWidth(footer); w > width {
			t.Fatalf("status bar at width %d = %d cells: %q", width, w, footer)
		}
		if !strings.Contains(footer, "q quit") && !strings.Contains(footer, "quit") {
			t.Fatalf("status bar dropped the quit hint at width %d: %q", width, footer)
		}
	}
	if got := fitStatusGroups("e:errors · r:reload · q:quit", " · ", 20); ansi.StringWidth(ansi.Strip(got)) > 20 {
		t.Fatalf("fitStatusGroups exceeded width: %q", got)
	}
}

// TestRenderBundleTreeNativeBounds verifies the bundle browser renders its
// native tree bounded to the pane, keeps the fold glyphs and selection, and
// sanitizes hostile entry names at the 50x12 minimum.
// spec: tui-rendering/hostile-input-at-minimum-size
func TestRenderBundleTreeNativeBounds(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("# Skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	evil := "evil\x1b[2Jname.md"
	if err := os.WriteFile(filepath.Join(root, evil), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	bundle := newBundleTree(root, "")
	m := NewModel(t.TempDir(), t.TempDir())
	m.width, m.height = 50, 12
	m.bundle = bundle

	out := renderBundleTree(m, 50, 11)
	stripped := ansi.Strip(out)
	lines := strings.Split(stripped, "\n")
	if len(lines) > 11 {
		t.Fatalf("bundle tree exceeded height: %d lines", len(lines))
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w > 50 {
			t.Fatalf("bundle line %d exceeded width: %d %q", i, w, line)
		}
	}
	if !strings.Contains(stripped, "SKILL.md") || !strings.Contains(stripped, "scripts") {
		t.Fatalf("bundle tree missing entries:\n%s", stripped)
	}
	if strings.Contains(stripped, "\x1b[2J") {
		t.Fatalf("untrusted entry name leaked a control sequence: %q", stripped)
	}
	if !strings.Contains(stripped, "▸") && !strings.Contains(stripped, "▾") {
		t.Fatalf("bundle tree lost its directory fold glyph:\n%s", stripped)
	}
}
