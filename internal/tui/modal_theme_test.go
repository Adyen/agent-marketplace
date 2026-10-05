package tui

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestModalFormsBindModelTheme verifies modal Huh forms render through the
// model's semantic light/dark theme rather than a fixed base theme: the same
// form built under a dark and a light model theme must render differently
// because the text role adapts to the reported terminal background.
// spec: tui-rendering/colorless-and-narrow-rendering
func TestModalFormsBindModelTheme(t *testing.T) {
	render := func(isDark bool) string {
		m := NewModel(t.TempDir(), t.TempDir())
		m.isDark = isDark
		m.theme = newTheme(isDark)
		m.buildSyncQuitForm()
		return m.syncQuitForm.View()
	}
	if render(true) == render(false) {
		t.Fatal("modal form rendering ignored the model's light/dark semantic theme")
	}
}

// TestInlineColorsStayInTheme enforces the theme ownership rule: non-test
// sources outside theme.go must not construct color styles inline. Layout-only
// styles (Width/Height/Padding without color) remain allowed.
// spec: tui-rendering/colorless-and-narrow-rendering
func TestInlineColorsStayInTheme(t *testing.T) {
	colorDef := regexp.MustCompile(`(^|[^A-Za-z])(Foreground|Background)\(|lipgloss\.(Color|ANSIColor)\(`)
	allowed := regexp.MustCompile(`\.BorderForeground\(|\.BorderBackground\(|context\.Background\(`)
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var violations []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") ||
			strings.HasSuffix(name, "_test.go") || name == "theme.go" {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if colorDef.MatchString(line) && !allowed.MatchString(line) {
				violations = append(violations, name+":"+itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
	}
	if len(violations) > 0 {
		t.Fatalf("inline color definitions outside theme.go:\n%s", strings.Join(violations, "\n"))
	}
}

// TestHuhThemeUsesSemanticRoles verifies the Huh theme factory maps the
// semantic ANSI-16 roles onto form chrome: accent selectors, ok selections,
// err diagnostics, muted descriptions, and background-adaptive text/options.
// spec: tui-rendering/colorless-and-narrow-rendering
func TestHuhThemeUsesSemanticRoles(t *testing.T) {
	for _, isDark := range []bool{true, false} {
		th := newTheme(isDark)
		styles := huhTheme(th)
		pairs := []struct {
			name string
			got  string
			want string
		}{
			{"select selector", styles.Focused.SelectSelector.Render(), accentStyle(th).SetString("> ").Render()},
			{"multi selector", styles.Focused.MultiSelectSelector.Render(), accentStyle(th).SetString("> ").Render()},
			{"option", styles.Focused.Option.Render("x"), textStyle(th).Render("x")},
			{"unselected option", styles.Focused.UnselectedOption.Render("x"), textStyle(th).Render("x")},
			{"selected option", styles.Focused.SelectedOption.Render("x"), okStyle(th).Render("x")},
			{"error message", styles.Focused.ErrorMessage.Render(), errStyle(th).SetString(" *").Render()},
			{"description", styles.Focused.Description.Render("x"), dimTextStyle(th).Render("x")},
			{"unselected prefix", styles.Focused.UnselectedPrefix.Render(), dimTextStyle(th).SetString("[ ] ").Render()},
		}
		for _, p := range pairs {
			if p.got != p.want {
				t.Fatalf("huhTheme(dark=%v) %s = %q, want semantic role %q", isDark, p.name, p.got, p.want)
			}
		}
		for name, out := range map[string]string{
			"selector":  styles.Focused.SelectSelector.Render("x"),
			"option":    styles.Focused.Option.Render("x"),
			"error":     styles.Focused.ErrorMessage.Render("x"),
			"focusedBt": styles.Focused.FocusedButton.Render("x"),
		} {
			if strings.Contains(out, "38;2;") {
				t.Fatalf("huhTheme %s emitted a truecolor sequence instead of ANSI-16: %q", name, out)
			}
		}
	}
	dark := huhTheme(newTheme(true)).Focused.Option.Render("x")
	light := huhTheme(newTheme(false)).Focused.Option.Render("x")
	if dark == light {
		t.Fatal("huhTheme options did not adapt to the model's terminal background")
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [8]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}
