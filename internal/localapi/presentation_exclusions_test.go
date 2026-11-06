package localapi

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestRowPresentationRetainsExclusionCheckState(t *testing.T) {
	const registryURL = "https://marketplace.example.test"
	base := session.Row{
		Artifact: resolver.ResolvedArtifact{
			Entry:       catalog.Entry{Kind: ir.KindSkill, Name: "review"},
			RegistryURL: registryURL, Source: config.Organization, IsMarketplace: true,
			MarketplaceURL: registryURL, MarketplaceInstanceID: "fixture-market",
			MarketplaceRegistryID: "tools", MarketplaceResourceName: "review",
		},
		RegistryURL: registryURL, EnabledGlobal: true,
	}
	cases := []struct {
		name                                                                         string
		available, excluded, userExcluded, pending, required, enabledGlobal, checked bool
		status                                                                       string
	}{
		{"user-excluded-pending", true, true, true, true, false, true, false, "excluded; verification pending"},
		{"user-excluded-verified", true, true, true, false, false, true, false, "excluded"},
		{"enabled-pending", false, false, false, true, false, true, true, "enabled in config; verification pending"},
		{"required-pending", false, false, false, true, true, false, true, "enabled in config; verification pending"},
		{"inactive-available", true, false, false, false, false, false, false, "available"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := base
			row.IsAvailable, row.IsExcluded, row.ExcludedByUser = tc.available, tc.excluded, tc.userExcluded
			row.PendingVerification, row.Artifact.Required, row.EnabledGlobal = tc.pending, tc.required, tc.enabledGlobal
			got := rowPresentationForUser(row, true)
			if got.Checked != tc.checked {
				t.Fatalf("checked = %t, want %t", got.Checked, tc.checked)
			}
			if got.Scope != "organization" {
				t.Fatalf("scope = %q, want organization", got.Scope)
			}
			if got.Status != tc.status {
				t.Fatalf("status = %q, want %q", got.Status, tc.status)
			}
		})
	}
}
