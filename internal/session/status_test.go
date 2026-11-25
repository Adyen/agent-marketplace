package session

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

func TestStatusGroupsOrderAndLabels(t *testing.T) {
	want := []struct {
		group StatusGroup
		id    string
		title string
	}{
		{GroupRequired, "required", "Required"},
		{GroupEnabled, "enabled", "Enabled"},
		{GroupPending, "pending", "Pending sync"},
		{GroupStale, "stale", "Stale"},
		{GroupBroken, "broken", "Broken"},
		{GroupAvailable, "available", "Available"},
		{GroupMalformed, "malformed", "Malformed"},
	}
	groups := StatusGroups()
	if len(groups) != len(want) {
		t.Fatalf("StatusGroups() length = %d, want %d", len(groups), len(want))
	}
	for i, expected := range want {
		if groups[i] != expected.group {
			t.Fatalf("StatusGroups()[%d] = %v, want %v", i, groups[i], expected.group)
		}
		if got := groups[i].ID(); got != expected.id {
			t.Errorf("group[%d].ID() = %q, want %q", i, got, expected.id)
		}
		if got := groups[i].Title(); got != expected.title {
			t.Errorf("group[%d].Title() = %q, want %q", i, got, expected.title)
		}
	}
	if got := StatusGroup(255).ID(); got != "unknown" {
		t.Errorf("unknown group ID() = %q, want unknown", got)
	}
	if got := StatusGroup(255).Title(); got != "?" {
		t.Errorf("unknown group Title() = %q, want ?", got)
	}
}

func TestRowStatusGroupPreservesTUIClassification(t *testing.T) {
	required := resolver.ResolvedArtifact{Required: true}
	cases := []struct {
		name string
		row  Row
		want StatusGroup
	}{
		{
			name: "malformed precedes other flags",
			row:  Row{IsMalformed: true, UnavailableReason: "catalog unavailable", Artifact: required},
			want: GroupMalformed,
		},
		{
			name: "unavailable precedes required",
			row:  Row{UnavailableReason: "catalog unavailable", Artifact: required},
			want: GroupBroken,
		},
		{
			name: "required precedes drift",
			row:  Row{Artifact: required, DriftItem: &materialize.DriftItem{Reason: materialize.DriftMissing}},
			want: GroupRequired,
		},
		{
			name: "available precedes drift",
			row:  Row{IsAvailable: true, DriftItem: &materialize.DriftItem{Reason: materialize.DriftMissing}},
			want: GroupAvailable,
		},
		{
			name: "missing drift is pending",
			row:  Row{DriftItem: &materialize.DriftItem{Reason: materialize.DriftMissing}},
			want: GroupPending,
		},
		{
			name: "orphaned drift is pending",
			row:  Row{DriftItem: &materialize.DriftItem{Reason: materialize.DriftOrphaned}},
			want: GroupPending,
		},
		{
			name: "broken drift is broken",
			row:  Row{DriftItem: &materialize.DriftItem{Reason: materialize.DriftBroken}},
			want: GroupBroken,
		},
		{
			name: "unknown drift remains enabled",
			row:  Row{DriftItem: &materialize.DriftItem{Reason: materialize.DriftReason(255)}},
			want: GroupEnabled,
		},
		{
			name: "stale marketplace cache remains available",
			row: Row{
				IsAvailable: true,
				MarketplaceDiscovery: &MarketplaceDiscovery{
					CacheState: marketplace.DiscoveryCacheStale,
				},
			},
			want: GroupAvailable,
		},
		{name: "default is enabled", row: Row{}, want: GroupEnabled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.row.StatusGroup(); got != tc.want {
				t.Fatalf("StatusGroup() = %s, want %s", got.ID(), tc.want.ID())
			}
		})
	}
}
