package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// originBadge returns the short letter badge that summarizes which
// configuration file(s) enable this skill. The alphabet is:
//
//	R     Required (org-mandated; scopes don't apply).
//	G     Enabled in global config only.
//	P     Enabled in project config only.
//	L     Enabled in local config only.
//	GP    Enabled in global + project.
//	GL    Enabled in global + local.
//	PL    Enabled in project + local.
//	GPL   Enabled in all three.
//	P-    Enabled in project but also in local Exclude (ineffective).
//	""    Available, not enabled anywhere; or malformed.
//
// G- is not reachable: global-scope registries ignore local excludes per
// resolver policy, so a globally-enabled skill cannot be locally excluded.
func originBadge(r row) string {
	if r.IsMalformed {
		return ""
	}
	if r.Artifact.Required {
		return "R"
	}
	if r.EnabledProject && r.ExcludedLocally {
		return "P-"
	}
	var b strings.Builder
	if r.EnabledGlobal {
		b.WriteByte('G')
	}
	if r.EnabledProject {
		b.WriteByte('P')
	}
	if r.EnabledLocal {
		b.WriteByte('L')
	}
	return b.String()
}

// scopeChips returns the semantic scope labels rendered as chips in the
// non-compact layout, in stable global → project → local order.
func scopeChips(r row) []string {
	switch badge := originBadge(r); badge {
	case "":
		return nil
	case "R":
		return []string{"required"}
	case "P-":
		return []string{"project", "local-excluded"}
	default:
		chips := make([]string, 0, len(badge))
		for _, letter := range badge {
			switch letter {
			case 'G':
				chips = append(chips, "global")
			case 'P':
				chips = append(chips, "project")
			case 'L':
				chips = append(chips, "local")
			}
		}
		return chips
	}
}

// scopeBadgeText renders scope chips at normal widths and falls back to the
// letter badge when the layout is compact.
func scopeBadgeText(r row, compact bool) string {
	if compact {
		return originBadge(r)
	}
	return strings.Join(scopeChips(r), " ")
}

// scopeBadgeSuffix returns the styled-able badge suffix for a list row.
// Chips are used only when they fit inside remaining cells; otherwise the
// compact letter badge keeps the row on one line.
func scopeBadgeSuffix(r row, compact bool, remaining int) string {
	badge := scopeBadgeText(r, compact)
	if badge == "" {
		return ""
	}
	if !compact && 1+ansi.StringWidth(badge) > remaining {
		if letters := originBadge(r); letters != "" && 1+ansi.StringWidth(letters) <= remaining {
			badge = letters
		} else {
			return ""
		}
	}
	return " " + badge
}

// badgeStyle returns the style used to render a badge or chip. Single-scope
// marks get a dim accent color; multi-scope, the ineffective-project
// variant, and local-excluded get a brighter color to draw attention.
func badgeStyle(badge string) lipgloss.Style {
	switch badge {
	case "":
		return lipgloss.NewStyle()
	case "R":
		return styleGlyphRequired
	case "P-":
		return lipgloss.NewStyle().Foreground(paletteWarn)
	}
	if len(badge) > 1 {
		// Multi-scope: use a brighter accent.
		return lipgloss.NewStyle().
			Bold(true).
			Foreground(paletteAccent)
	}
	// Single-scope badge (G/P/L): dim.
	return lipgloss.NewStyle().
		Foreground(paletteMuted)
}

func harnessBadge(r row) string {
	if r.IsMalformed || len(r.Harnesses) == 0 || r.HarnessSource != "registry override" {
		return ""
	}
	return "{" + harnessListLabel(r.Harnesses, ",") + "}"
}

func harnessBadgeStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(paletteMuted)
}
