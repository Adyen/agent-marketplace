package pipeline

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: resource-removal/requires-held-removal-is-not-reported-as-a-collection-member
func TestRequiresHeldPinResolvesAsRequiresHeldNotCollectionOnly(t *testing.T) {
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{
		URL:        "https://market.example",
		InstanceID: "mp",
		Resources: []config.MarketplaceResourcePin{{
			RegistryID: "registry-id",
			Kind:       "skill",
			Name:       "helper",
			Digest:     "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			RequiresEdges: config.MarketplaceRequirementEdgesFrom([]config.MarketplaceRequirementEdge{{
				RegistryID: "registry-id", Kind: "skill", Name: "parent",
				Revision: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			}}),
		}},
	}}}

	pin, entry, ok := marketplaceRequiresHeldPinByName(cfg, "helper", ir.KindSkill, true)
	if !ok || pin.Name != "helper" || entry.InstanceID != "mp" {
		t.Fatalf("requires-held pin must resolve through the requires-held path, got ok=%v pin=%#v", ok, pin)
	}
	if _, ok := marketplaceCollectionOnlyPinByName(cfg, "helper", ir.KindSkill, true); ok {
		t.Fatal("requires-held pin must not be reported as held only through a collection")
	}
}
