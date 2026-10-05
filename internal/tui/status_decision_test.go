package tui

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

// TestRowStatusDecision verifies the single authoritative status decision:
// one precedence chain derives the ASCII glyph, the color-profile symbol, and
// the status label so they can never diverge.
// spec: tui-rendering/colorless-and-narrow-rendering
func TestRowStatusDecision(t *testing.T) {
	marketplaceRow := func(stale bool) row {
		r := mkRow("s", withAvailable)
		r.MarketplaceDiscovery = &marketplaceDiscovery{
			Item: mpapi.ResourceListItem{Availability: mpapi.AvailabilityAvailable},
		}
		if stale {
			r.MarketplaceDiscovery.CacheState = marketplace.DiscoveryCacheStale
		}
		return r
	}
	requiredOrg := func(r *row) {
		r.Artifact.Required = true
		r.Artifact.Source = config.Organization
	}
	pending := func(r *row) { r.PendingVerification = true }
	unavailable := func(r *row) { r.UnavailableReason = "catalog fetch failed" }

	cases := []struct {
		name   string
		row    row
		glyph  string
		symbol string
		label  string
	}{
		{"pending enabled", mkRow("s", pending), "[x]", "●", "enabled in config; verification pending"},
		{"pending available", mkRow("s", withAvailable, pending), "[ ]", "○", "available; verification pending"},
		{"pending excluded", mkRow("s", withAvailable, withExcluded, pending), "[ ]", "○", "excluded; verification pending"},
		{"malformed", mkRow("s", withMalformed("bad frontmatter")), "[!]", "✗", "malformed: bad frontmatter"},
		{"unavailable", mkRow("s", unavailable), "[!]", "✗", "unavailable: catalog fetch failed"},
		{"available", mkRow("s", withAvailable), "[ ]", "○", "available"},
		{"excluded", mkRow("s", withAvailable, withExcluded), "[ ]", "○", "excluded"},
		{"marketplace fresh", marketplaceRow(false), "[ ]", "○", "available via Marketplace"},
		{"marketplace stale", marketplaceRow(true), "[ ]", "○", "available, stale via Marketplace"},
		{"required", mkRow("s", withRequired), "[R]", "■", "required"},
		{"required org", mkRow("s", requiredOrg), "[R]", "■", "required (org)"},
		{"drift pending", mkRow("s", withDrift(materialize.DriftMissing)), "[*]", "◐", "enabled (pending sync)"},
		{"orphaned", mkRow("s", withDrift(materialize.DriftOrphaned)), "[-]", "◌", "orphaned"},
		{"broken", mkRow("s", withDrift(materialize.DriftBroken)), "[!]", "✗", "broken"},
		{"enabled", mkRow("s"), "[x]", "●", "enabled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := tc.row.Status()
			if st.Glyph != tc.glyph || st.Symbol != tc.symbol || st.Label != tc.label {
				t.Fatalf("status() = {%q %q %q}, want {%q %q %q}",
					st.Glyph, st.Symbol, st.Label, tc.glyph, tc.symbol, tc.label)
			}
			if got := tc.row.Glyph(); got != tc.glyph {
				t.Fatalf("Glyph() = %q, want %q", got, tc.glyph)
			}
			if got := tc.row.StatusText(); got != tc.label {
				t.Fatalf("StatusText() = %q, want %q", got, tc.label)
			}
			stubColorCapable(t, true)
			if got := statusSymbol(tc.row); got != tc.symbol {
				t.Fatalf("statusSymbol = %q, want %q", got, tc.symbol)
			}
			stubColorCapable(t, false)
			if got := statusSymbol(tc.row); got != tc.glyph {
				t.Fatalf("no-color statusSymbol = %q, want ASCII %q", got, tc.glyph)
			}
		})
	}
}
