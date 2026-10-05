package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
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
		return styleBadgeWarn
	}
	if len(badge) > 1 {
		// Multi-scope: use a brighter accent.
		return styleBadgeMulti
	}
	// Single-scope badge (G/P/L): dim.
	return styleDim
}

func harnessBadge(r row) string {
	if r.IsMalformed || len(r.Harnesses) == 0 || r.HarnessSource != "registry override" {
		return ""
	}
	return "{" + harnessListLabel(r.Harnesses, ",") + "}"
}

func harnessBadgeStyle() lipgloss.Style {
	return styleDim
}
