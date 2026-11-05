package marketplace

import (
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

// PinArtifact returns the identity-only artifact for a configured Marketplace
// pin, the shape display code uses before or without cache verification. It
// reports false when the pin kind has no IR equivalent. Collection pins map to
// the virtual collection kind, which mpapi.ToIR deliberately omits.
func PinArtifact(entry config.MarketplaceEntry, pin config.MarketplaceResourcePin) (resolver.ResolvedArtifact, bool) {
	kind, ok := mpapi.ToIR(pin.Kind)
	if pin.Kind == mpapi.KindCollection {
		kind, ok = ir.KindCollection, true
	}
	if !ok {
		return resolver.ResolvedArtifact{}, false
	}
	name := pin.Name
	if pin.InstalledName != "" {
		name = pin.InstalledName
	}
	return resolver.ResolvedArtifact{
		Entry:                   catalog.Entry{Kind: kind, Name: name},
		RegistryURL:             entry.URL,
		RegistryPath:            pin.RegistryID,
		Ref:                     pin.Revision,
		Source:                  entry.Source,
		Required:                entry.Required,
		CatalogKey:              marketplaceCatalogKey(entry, pin),
		IsMarketplace:           true,
		MarketplaceURL:          entry.URL,
		MarketplaceInstanceID:   entry.InstanceID,
		MarketplaceAlias:        entry.Alias,
		MarketplaceRegistryID:   pin.RegistryID,
		MarketplaceResourceName: pin.Name,
	}, true
}
