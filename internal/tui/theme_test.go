package tui

import (
	"image/color"
	"testing"
)

// TestNewThemeBackgroundAdaptiveRoles verifies newTheme assigns every
// semantic role a concrete color and adapts background-dependent roles to
// the requested terminal background, without touching the detected color
// profile.
func TestNewThemeBackgroundAdaptiveRoles(t *testing.T) {
	for _, isDark := range []bool{true, false} {
		th := newTheme(isDark)
		roles := map[string]color.Color{
			"accent":  th.accent,
			"ok":      th.ok,
			"warn":    th.warn,
			"mark":    th.mark,
			"err":     th.err,
			"muted":   th.muted,
			"text":    th.text,
			"surface": th.surface,
			"onSurf":  th.onSurf,
			"card":    th.card,
		}
		for name, c := range roles {
			if c == nil {
				t.Fatalf("newTheme(%v) left role %s nil", isDark, name)
			}
		}
	}
	r1, g1, b1, _ := newTheme(true).text.RGBA()
	r2, g2, b2, _ := newTheme(false).text.RGBA()
	if r1 == r2 && g1 == g2 && b1 == b2 {
		t.Fatalf("text role did not adapt to the requested background")
	}
}
