package session

import (
	"cmp"
	"slices"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
)

const DiscoverySearchDebounce = 250 * time.Millisecond

func EffectiveMarketplaceConfig(cfg *config.Config) config.EffectiveConfig {
	ec := config.EffectiveConfig{Merged: cfg}
	if cfg != nil && cfg.AllowedMarketplaces != nil {
		ec.AllowedMarketplaces = slices.Clone(*cfg.AllowedMarketplaces)
	}
	return ec
}

func DiscoveryRows(outcomes []marketplace.DiscoveryOutcome) []Row {
	var rows []Row
	for _, outcome := range outcomes {
		for _, item := range outcome.Items {
			kind, ok := skill.ParseKind(item.Kind)
			if !ok {
				continue
			}
			tags := make([]string, 0)
			if value := item.Metadata["tags"]; value != "" {
				tags = append(tags, value)
			}
			discovery := &MarketplaceDiscovery{Endpoint: outcome.Endpoint, Item: item, CacheState: outcome.CacheState, ObservedAt: outcome.ObservedAt}
			rows = append(rows, Row{
				Artifact: resolver.ResolvedArtifact{
					Entry:                   catalog.Entry{Kind: kind, Name: item.Name, Description: item.Description, Tags: tags},
					CatalogKey:              "marketplace-discovery:" + outcome.Endpoint.InstanceID + ":" + item.RegistryID + ":" + item.Revision + ":" + item.ContentDigest,
					IsMarketplace:           true,
					MarketplaceURL:          outcome.Endpoint.URL,
					MarketplaceInstanceID:   outcome.Endpoint.InstanceID,
					MarketplaceAlias:        outcome.Endpoint.Alias,
					MarketplaceRegistryID:   item.RegistryID,
					MarketplaceResourceName: item.Name,
				},
				IsAvailable:          true,
				RegistryURL:          outcome.Endpoint.URL,
				RegistryName:         MarketplaceEndpointDisplay(outcome.Endpoint) + "/" + item.RegistrySlug,
				MarketplaceDiscovery: discovery,
			})
		}
	}
	slices.SortFunc(rows, func(a, b Row) int {
		da, db := a.MarketplaceDiscovery, b.MarketplaceDiscovery
		if da.Item.Score != db.Item.Score {
			if da.Item.Score > db.Item.Score {
				return -1
			}
			return 1
		}
		for _, c := range []int{
			cmp.Compare(MarketplaceEndpointDisplay(da.Endpoint), MarketplaceEndpointDisplay(db.Endpoint)),
			cmp.Compare(da.Endpoint.URL, db.Endpoint.URL),
			cmp.Compare(da.Endpoint.InstanceID, db.Endpoint.InstanceID),
			cmp.Compare(da.Item.RegistrySlug, db.Item.RegistrySlug),
			cmp.Compare(da.Item.Kind, db.Item.Kind),
			cmp.Compare(da.Item.Name, db.Item.Name),
			cmp.Compare(da.Item.Revision, db.Item.Revision),
			cmp.Compare(da.Item.ContentDigest, db.Item.ContentDigest),
		} {
			if c != 0 {
				return c
			}
		}
		return 0
	})
	return rows
}

func MergeDiscoveryRows(authoritative, provisional, discovery []Row) []Row {
	rows := make([]Row, 0, len(authoritative)+len(provisional)+len(discovery))
	rows = append(rows, authoritative...)
	pending := make(map[string]struct{}, len(provisional))
	if authoritative == nil {
		for _, r := range provisional {
			if r.Artifact.IsMarketplace {
				pending[provisionalCoordinate(r)] = struct{}{}
			}
		}
		rows = append(rows, provisional...)
	}
	enabled := make(map[string]struct{})
	for _, r := range authoritative {
		if r.IsAvailable || !r.Artifact.IsMarketplace {
			continue
		}
		selectionCoordinates := 0
		for _, selection := range r.MarketplaceSelections {
			pin := selection.Pin
			if selection.Entry.InstanceID == "" || pin.RegistryID == "" || pin.Kind == "" || pin.Name == "" {
				continue
			}
			enabled[discoveryCoordinate(selection.Entry.InstanceID, pin.RegistryID, pin.Kind, pin.Name)] = struct{}{}
			selectionCoordinates++
		}
		if selectionCoordinates == 0 {
			enabled[discoveryCoordinate(r.Artifact.MarketplaceInstanceID, r.Artifact.MarketplaceRegistryID, r.Artifact.Kind.String(), r.Artifact.Name)] = struct{}{}
		}
	}
	for _, r := range discovery {
		d := r.MarketplaceDiscovery
		if d == nil {
			continue
		}
		if _, ok := enabled[discoveryCoordinate(d.Endpoint.InstanceID, d.Item.RegistryID, d.Item.Kind, d.Item.Name)]; ok {
			continue
		}
		url := d.Endpoint.URL
		if normalized, err := marketplace.NormalizeMarketplaceURL(url); err == nil {
			url = normalized
		}
		if _, ok := pending[url+"\x00"+d.Item.RegistryID+"\x00"+d.Item.Kind+"\x00"+d.Item.Name]; ok {
			continue
		}
		rows = append(rows, r)
	}
	return rows
}

func discoveryCoordinate(instanceID, registryID, kind, name string) string {
	return instanceID + "\x00" + registryID + "\x00" + kind + "\x00" + name
}

func provisionalCoordinate(r Row) string {
	url := r.Artifact.MarketplaceURL
	if url == "" {
		url = r.RegistryURL
	}
	if normalized, err := marketplace.NormalizeMarketplaceURL(url); err == nil {
		url = normalized
	}
	return url + "\x00" + r.Artifact.MarketplaceRegistryID + "\x00" + r.Artifact.Kind.String() + "\x00" + r.Artifact.MarketplaceResourceName
}
