package tui

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/charmbracelet/x/ansi"
)

// A row whose toggle is refused offers neither the detail pane's space hint
// nor the footer's space hint; an eligible row keeps both, and so does a
// selected section header, whose space toggles the whole section.
// spec: tui-feedback/toggle-hints-follow-row-eligibility
func TestToggleHintsFollowRowEligibility(t *testing.T) {
	collectionOnly := mkRow("coll", withKind(ir.KindSkill))
	collectionOnly.Artifact.IsMarketplace = true
	collectionOnly.Artifact.Source = config.RepoCommitted
	collectionOnly.MarketplaceCollectionOnly = true
	pendingVerification := mkRow("pend", withProject)
	pendingVerification.PendingVerification = true
	cases := []struct {
		name       string
		r          row
		wantToggle bool
	}{
		{"eligible", mkRow("plain", withProject), true},
		{"required", mkRow("org-review", withGlobal, withRequired), false},
		{"collection only", collectionOnly, false},
		{"pending verification", pendingVerification, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := footerFixture(120)
			m.rows = []row{tc.r}
			m.recomputeView()
			if _, onSection := m.selectedSectionPath(); !onSection {
				t.Fatal("fixture must start on the section header")
			}
			if footer := ansi.Strip(renderFooter(m)); !strings.Contains(footer, "space toggle") {
				t.Fatalf("a selected section header lost the space hint: %q", footer)
			}
			m.cursor = 1
			if r, ok := m.selectedRow(); !ok || r.Artifact.Name != tc.r.Artifact.Name {
				t.Fatalf("cursor 1 selects %+v, want %s", r, tc.r.Artifact.Name)
			}
			detail := ansi.Strip(renderDetail(m))
			footer := ansi.Strip(renderFooter(m))
			if got := strings.Contains(detail, "space = toggle"); got != tc.wantToggle {
				t.Fatalf("detail offers space = toggle: %v, want %v:\n%s", got, tc.wantToggle, detail)
			}
			if !strings.Contains(detail, "enter = full view") {
				t.Fatalf("detail lost the full-view hint:\n%s", detail)
			}
			if got := strings.Contains(footer, "space toggle"); got != tc.wantToggle {
				t.Fatalf("footer offers space toggle: %v, want %v: %q", got, tc.wantToggle, footer)
			}
		})
	}
}
