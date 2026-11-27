package tui

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	ansi "github.com/charmbracelet/x/ansi"
)

func TestOriginBadgeSingleScopeLetters(t *testing.T) {
	cases := []struct {
		name string
		r    row
		want string
	}{
		{"global only", mkRow("s", withGlobal), "G"},
		{"project only", mkRow("s", withProject), "P"},
		{"local only", mkRow("s", withLocal), "L"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := originBadge(tc.r); got != tc.want {
				t.Fatalf("originBadge(%s) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// spec: tui-rendering/aligned-safe-labels-at-normal-and-narrow-sizes
func TestOriginBadgeOptionalOrganizationUsesOWithinThreeCells(t *testing.T) {
	organization := mkRow("s", withGlobal)
	organization.Artifact.Source = config.Organization
	organization.EnabledOrganization = true
	multipleLayers := mkRow("s", withGlobal, withProject, withLocal)
	multipleLayers.Artifact.Source = config.Organization
	multipleLayers.EnabledOrganization = true
	user := mkRow("s", withGlobal)
	user.Artifact.Source = config.UserPersonal
	user.EnabledUser = true
	marketplace := mkRow("s", withGlobal)
	marketplace.Artifact.Source = config.Organization
	marketplace.Artifact.IsMarketplace = true
	marketplace.EnabledOrganization = true
	userFromOrganizationRegistry := mkRow("s", withGlobal)
	userFromOrganizationRegistry.Artifact.Source = config.Organization
	userFromOrganizationRegistry.EnabledUser = true

	for _, tc := range []struct {
		name string
		row  row
		want string
	}{
		{name: "organization default", row: organization, want: "O"},
		{name: "organization Marketplace default", row: marketplace, want: "O"},
		{name: "user global", row: user, want: "G"},
		{name: "user selection from organization registry", row: userFromOrganizationRegistry, want: "G"},
		{name: "organization with every other scope", row: multipleLayers, want: "OPL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := originBadge(tc.row)
			if got != tc.want {
				t.Fatalf("originBadge = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOriginBadgeMultiScopeLettersPreserveOrder(t *testing.T) {
	cases := []struct {
		name string
		r    row
		want string
	}{
		{"global+project", mkRow("s", withGlobal, withProject), "GP"},
		{"global+local", mkRow("s", withGlobal, withLocal), "GL"},
		{"project+local", mkRow("s", withProject, withLocal), "PL"},
		{"all three", mkRow("s", withGlobal, withProject, withLocal), "GPL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := originBadge(tc.r); got != tc.want {
				t.Fatalf("originBadge(%s) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestRenderListShowsOptionalOrganizationBadgeAtSupportedWidths(t *testing.T) {
	r := mkRow("alpha\x1b[2J", withGlobal, withRegistry("https://example.invalid/org.git", "org"))
	r.Artifact.Source = config.Organization
	r.EnabledOrganization = true
	m := NewModel("/repo", "/home")
	m.rows = []row{r}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	for _, width := range []int{80, 50} {
		out := ansi.Strip(renderList(m, width))
		if !contains(out, "[x] O   skill") || !contains(out, `alpha\x1B[2J`) {
			t.Fatalf("renderList(%d) should align the O badge and visibly escape the name, got:\n%s", width, out)
		}
		if contains(out, "\x1b") {
			t.Fatalf("renderList(%d) emitted a terminal control byte, got:\n%s", width, out)
		}
	}
}

func TestOriginBadgeRequiredWinsOverScopes(t *testing.T) {
	r := mkRow("s", withRequired, withProject, withLocal)
	if got := originBadge(r); got != "R" {
		t.Fatalf("required skill should yield R, got %q", got)
	}
}

func TestOriginBadgeLocallyExcludedProject(t *testing.T) {
	// Project-enabled + locally-excluded → "P-" (ineffective enablement).
	r := mkRow("s", withProject, withExcluded)
	if got := originBadge(r); got != "P-" {
		t.Fatalf("project+excluded should yield P-, got %q", got)
	}
}

func TestOriginBadgeAvailableReturnsEmpty(t *testing.T) {
	r := mkRow("s", withAvailable)
	if got := originBadge(r); got != "" {
		t.Fatalf("available skill should return empty badge, got %q", got)
	}
}

func TestOriginBadgeMalformedReturnsEmpty(t *testing.T) {
	r := row{IsMalformed: true, MalformedMsg: "boom"}
	if got := originBadge(r); got != "" {
		t.Fatalf("malformed skill should return empty badge, got %q", got)
	}
}

func TestOriginBadgeLocalOnlyExcludedIsLocal(t *testing.T) {
	// Locally excluded but no project enablement: the skill is only present
	// because of the local exclude. Badge should still indicate the
	// ineffective-project state only if project was set; here it's just "".
	r := mkRow("s", withExcluded)
	if got := originBadge(r); got != "" {
		t.Fatalf("exclude-only available skill should return empty badge, got %q", got)
	}
}

func TestRenderListShowsOriginBadgeOnSkillRows(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.loading = false
	m.rows = []row{mkRow("code-review", withProject, withLocal, withRegistry("t.git", "t"))}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	out := renderList(m, 24)
	if !contains(out, "PL") {
		t.Fatalf("renderList should include PL badge for project+local skill, got:\n%s", out)
	}
}

func TestRenderListSkipsBadgeForAvailableSkill(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.loading = false
	m.rows = []row{mkRow("deploy", withAvailable, withRegistry("t.git", "t"))}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	out := renderList(m, 24)
	// Available row contains the glyph and name; no single-letter badge suffix.
	for _, bad := range []string{" G\n", " P\n", " L\n", " R\n"} {
		if contains(out, bad) {
			t.Fatalf("available skill should not render a scope badge, output: %q", out)
		}
	}
}
