package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/charmbracelet/x/ansi"

	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

// spec: session-navigation/ranked-search-within-sections
func TestFuzzySearchRanksWithinLeafInSingleView(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("alpha-deploy", withGlobal, withRegistry("sec.git", "security")),
		mkRow("deploy", withGlobal, withRegistry("sec.git", "security")),
	}
	m.filter = "dep"
	m.recomputeView()
	if got := artifactNames(m.flatItems); !namesEqual(got, []string{"deploy", "alpha-deploy"}) {
		t.Fatalf("fuzzy order = %v, want %v", got, []string{"deploy", "alpha-deploy"})
	}
}

func TestFuzzySearchDoesNotReorderSections(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("zeta-dep", withGlobal, withRegistry("sec.git", "security")),
		mkRow("dep", withRequired, withProject, withRegistry("sec.git", "security")),
	}
	m.filter = "dep"
	m.recomputeView()
	paths := sectionPaths(m.flatItems)
	enabledIdx, requiredIdx := -1, -1
	for i, p := range paths {
		if p == "status:enabled" {
			enabledIdx = i
		}
		if p == "status:required" {
			requiredIdx = i
		}
	}
	if enabledIdx < 0 || requiredIdx < 0 || requiredIdx > enabledIdx {
		t.Fatalf("fuzzy ranking must not reorder sections: %v", paths)
	}
}

// spec: session-navigation/ranked-search-within-sections
func TestFuzzySearchMetadataUsesSortedKeyOrder(t *testing.T) {
	build := func() string {
		r := mkRow("item", withAvailable)
		r.MarketplaceDiscovery = &marketplaceDiscovery{
			Item: mpapi.ResourceListItem{
				Metadata: map[string]string{
					"zz-last":  "v1",
					"aa-first": "v2",
					"mm-mid":   "v3",
				},
			},
		}
		r.EnsureSearchable()
		return strings.Join(r.SearchWords, " ")
	}
	first, second := build(), build()
	if first != second {
		t.Fatalf("searchable words must be deterministic across builds:\n%q\n%q", first, second)
	}
	for _, word := range []string{"aa", "first", "mm", "mid", "zz", "last", "v1", "v2", "v3"} {
		if !strings.Contains(" "+first+" ", " "+word+" ") {
			t.Fatalf("metadata word %q missing from searchable words: %q", word, first)
		}
	}
}

func TestFuzzySearchHighlightsOnlyMatchedNameRunes(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("deploy", withGlobal, withRegistry("sec.git", "security")),
	}
	m.filter = "dep"
	m.recomputeView()
	var target flatItem
	for _, it := range m.flatItems {
		if it.Kind == flatArtifact {
			target = it
		}
	}
	if target.Kind != flatArtifact {
		t.Fatalf("no artifact row rendered")
	}
	line := stylizeFlatItem(target, 40, false)
	label := resourceLabel(target.Row)
	if strings.Contains(line, label) {
		t.Fatalf("matched name runes should be styled, got plain line %q", line)
	}
	prefix := len([]rune(label)) - len([]rune(sanitizeTerminalData(target.Row.Artifact.Name, terminalSingleLine)))
	idx := []int{prefix, prefix + 1, prefix + 2}
	want := lipgloss.StyleRunes(label, idx, styleSearchMatch, lipgloss.NewStyle())
	if !strings.Contains(line, want) {
		t.Fatalf("line %q should contain highlighted name %q", line, want)
	}
	plain := ansi.Strip(line)
	if !strings.Contains(plain, label) {
		t.Fatalf("sanitized name should survive styling, got %q", plain)
	}
}

// spec: session-navigation/metadata-only-match
func TestFuzzySearchMetadataOnlyMatchDoesNotHighlightName(t *testing.T) {
	m := NewModel("/repo", "/home")
	r := mkRow("plain", withAvailable, withRegistry("sec.git", "security"))
	r.Artifact.Description = "deep audit tooling"
	m.rows = []row{r}
	m.filter = "deep"
	m.recomputeView()
	var target flatItem
	for _, it := range m.flatItems {
		if it.Kind == flatArtifact {
			target = it
		}
	}
	if target.Kind != flatArtifact {
		t.Fatalf("metadata-only match should still render the row")
	}
	line := stylizeFlatItem(target, 40, false)
	if !strings.Contains(line, resourceLabel(target.Row)) {
		t.Fatalf("metadata-only match must not highlight name runes, got %q", line)
	}
}

// spec: session-navigation/clear-and-refresh
func TestFuzzySearchRemoteArrivalKeepsSelectionAnchor(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("alpha-deploy", withGlobal, withRegistry("sec.git", "security")),
		mkRow("deploy", withGlobal, withRegistry("sec.git", "security")),
	}
	m.authoritativeRows = append([]row(nil), m.rows...)
	m.filter = "dep"
	m.recomputeView()
	for i, it := range m.flatItems {
		if it.Kind == flatArtifact && it.Row.Artifact.Name == "deploy" {
			m.cursor = i
		}
	}

	endpoint := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}
	m.applyDiscovery("dep", []marketplace.DiscoveryOutcome{{
		Endpoint: endpoint,
		Items: []mpapi.ResourceListItem{{
			RegistryID:   "registry",
			Kind:         "skill",
			Name:         "dep-remote",
			Availability: mpapi.AvailabilityAvailable,
		}},
	}})

	sel, ok := m.selectedRow()
	if !ok || sel.Artifact.Name != "deploy" {
		name := ""
		if ok {
			name = sel.Artifact.Name
		}
		t.Fatalf("remote result arrival must keep the selection anchored to deploy, got %q", name)
	}
	if got := artifactNames(m.flatItems); len(got) == 0 || got[0] == "alpha-deploy" {
		t.Fatalf("ranked order should place scored matches ahead of weaker ones, got %v", got)
	}
}
