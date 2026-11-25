package session

import (
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

func TestEffectiveMarketplaceConfigClonesAllowedMarketplacePolicy(t *testing.T) {
	allowed := []string{"^https://market.example$"}
	cfg := &config.Config{AllowedMarketplaces: &allowed}

	got := EffectiveMarketplaceConfig(cfg)
	if got.Merged != cfg {
		t.Fatal("effective config did not retain the accepted merged config")
	}
	if len(got.AllowedMarketplaces) != 1 || got.AllowedMarketplaces[0] != allowed[0] {
		t.Fatalf("allowed marketplaces = %q, want %q", got.AllowedMarketplaces, allowed)
	}
	got.AllowedMarketplaces[0] = "changed"
	if allowed[0] != "^https://market.example$" {
		t.Fatalf("effective config shares the source allowlist: %q", allowed)
	}
}

func TestDiscoveryRowsUsesDeterministicSDKOrdering(t *testing.T) {
	endpointA := config.MarketplaceEntry{URL: "https://market-a.example", InstanceID: "market-a", Alias: "alpha"}
	endpointB := config.MarketplaceEntry{URL: "https://market-b.example", InstanceID: "market-b", Alias: "beta"}
	outcomes := []marketplace.DiscoveryOutcome{
		{
			Endpoint: endpointB,
			Items: []mpapi.ResourceListItem{
				discoveryTestItem("registry-b", "tools", "skill", "alpha", "revision-a", "digest-a", 5, nil),
				discoveryTestItem("registry-b", "tools", "skill", "zeta", "revision-z", "digest-z", 5, nil),
			},
			CacheState: marketplace.DiscoveryCacheFresh,
			ObservedAt: time.Unix(1, 0),
		},
		{
			Endpoint: endpointA,
			Items: []mpapi.ResourceListItem{
				discoveryTestItem("registry-a", "tools", "skill", "lower", "revision-lower", "digest-lower", 4, nil),
				discoveryTestItem("registry-a", "tools", "skill", "shared", "revision-two", "digest-two", 5, nil),
				discoveryTestItem("registry-a", "tools", "skill", "shared", "revision-one", "digest-one", 5, nil),
				discoveryTestItem("registry-a", "tools", "skill", "highest", "revision-highest", "digest-highest", 8, map[string]string{"tags": "remote, marketplace"}),
				discoveryTestItem("registry-a", "tools", "unknown", "unsupported", "revision-unsupported", "digest-unsupported", 99, nil),
			},
			CacheState: marketplace.DiscoveryCacheStale,
			ObservedAt: time.Unix(2, 0),
		},
	}

	rows := DiscoveryRows(outcomes)
	var got []string
	for _, row := range rows {
		discovery := row.MarketplaceDiscovery
		if discovery == nil {
			t.Fatalf("row %q is missing its Marketplace discovery identity", row.Artifact.Name)
		}
		got = append(got, strings.Join([]string{
			discovery.Endpoint.Alias,
			discovery.Endpoint.InstanceID,
			discovery.Item.RegistryID,
			discovery.Item.Kind,
			discovery.Item.Name,
			discovery.Item.Revision,
			discovery.Item.ContentDigest,
		}, "\x00"))
	}
	want := []string{
		"alpha\x00market-a\x00registry-a\x00skill\x00highest\x00revision-highest\x00digest-highest",
		"alpha\x00market-a\x00registry-a\x00skill\x00shared\x00revision-one\x00digest-one",
		"alpha\x00market-a\x00registry-a\x00skill\x00shared\x00revision-two\x00digest-two",
		"beta\x00market-b\x00registry-b\x00skill\x00alpha\x00revision-a\x00digest-a",
		"beta\x00market-b\x00registry-b\x00skill\x00zeta\x00revision-z\x00digest-z",
		"alpha\x00market-a\x00registry-a\x00skill\x00lower\x00revision-lower\x00digest-lower",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("discovery row order = %q, want %q", got, want)
	}
	if len(rows[0].Artifact.Tags) != 1 || rows[0].Artifact.Tags[0] != "remote, marketplace" {
		t.Fatalf("discovery metadata tags = %q, want one source tag", rows[0].Artifact.Tags)
	}
	if rows[0].MarketplaceDiscovery.CacheState != marketplace.DiscoveryCacheStale {
		t.Fatalf("discovery cache state = %q, want stale", rows[0].MarketplaceDiscovery.CacheState)
	}
}

func TestMergeDiscoveryRowsDeduplicatesVerifiedSelectionsWithoutCollapsingResources(t *testing.T) {
	selected := selectedMarketplaceTestRow("market-instance", "registry-one", "shared")
	remote := []Row{
		discoveryTestRow(config.MarketplaceEntry{URL: "https://alias-endpoint.example", InstanceID: "market-instance", Alias: "new-alias"}, "registry-one", "skill", "shared", "revision-one", "digest-one", 1, nil),
		discoveryTestRow(config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market-instance", Alias: "public"}, "registry-two", "skill", "shared", "revision-two", "digest-two", 1, nil),
		discoveryTestRow(config.MarketplaceEntry{URL: "https://market.example", InstanceID: "replacement-instance", Alias: "replacement"}, "registry-one", "skill", "shared", "revision-three", "digest-three", 1, nil),
		discoveryTestRow(config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market-instance", Alias: "public"}, "registry-one", "skill", "other", "revision-four", "digest-four", 1, nil),
	}

	got := MergeDiscoveryRows([]Row{selected}, nil, remote)
	if len(got) != 4 || got[0].Artifact.Name != "shared" {
		t.Fatalf("merged rows = %#v, want selected row and three distinct discovery resources", got)
	}
	want := []string{"registry-two/shared", "registry-one/shared", "registry-one/other"}
	for i, key := range want {
		row := got[i+1]
		actual := row.MarketplaceDiscovery.Item.RegistryID + "/" + row.Artifact.Name
		if actual != key {
			t.Fatalf("merged discovery row %d = %q, want %q", i, actual, key)
		}
	}
}

func TestMergeDiscoveryRowsUsesProvisionalPinsOnlyBeforeAuthoritativeInventory(t *testing.T) {
	pending := provisionalMarketplaceTestRow("https://market.example/", "registry-one", "shared")
	remote := []Row{
		discoveryTestRow(config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market-instance"}, "registry-one", "skill", "shared", "revision-one", "digest-one", 1, nil),
		discoveryTestRow(config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market-instance"}, "registry-two", "skill", "shared", "revision-two", "digest-two", 1, nil),
	}

	beforeInventory := MergeDiscoveryRows(nil, []Row{pending}, remote)
	if len(beforeInventory) != 2 || !beforeInventory[0].PendingVerification || beforeInventory[1].MarketplaceDiscovery.Item.RegistryID != "registry-two" {
		t.Fatalf("pre-inventory merge = %#v, want pending selection plus unmatched registry", beforeInventory)
	}
	afterInventory := MergeDiscoveryRows([]Row{}, []Row{pending}, remote)
	if len(afterInventory) != 2 || afterInventory[0].PendingVerification || afterInventory[0].MarketplaceDiscovery.Item.RegistryID != "registry-one" {
		t.Fatalf("post-inventory merge = %#v, want both discovery rows without stale provisional selection", afterInventory)
	}
}

func discoveryTestItem(registryID, registrySlug, kind, name, revision, digest string, score int, metadata map[string]string) mpapi.ResourceListItem {
	return mpapi.ResourceListItem{
		RegistryID: registryID, RegistrySlug: registrySlug, Kind: kind, Name: name,
		Revision: revision, ContentDigest: digest, Score: float64(score), Availability: mpapi.AvailabilityAvailable, Metadata: metadata,
	}
}

func discoveryTestRow(endpoint config.MarketplaceEntry, registryID, kind, name, revision, digest string, score int, metadata map[string]string) Row {
	kindValue, ok := skill.ParseKind(kind)
	if !ok {
		panic("invalid discovery test kind")
	}
	item := discoveryTestItem(registryID, "tools", kind, name, revision, digest, score, metadata)
	return Row{
		Artifact: resolver.ResolvedArtifact{
			Entry:                   catalog.Entry{Kind: kindValue, Name: name},
			CatalogKey:              "marketplace-discovery:" + endpoint.InstanceID + ":" + registryID + ":" + revision + ":" + digest,
			IsMarketplace:           true,
			MarketplaceURL:          endpoint.URL,
			MarketplaceInstanceID:   endpoint.InstanceID,
			MarketplaceRegistryID:   registryID,
			MarketplaceResourceName: name,
		},
		IsAvailable:          true,
		RegistryURL:          endpoint.URL,
		RegistryName:         MarketplaceEndpointDisplay(endpoint) + "/tools",
		MarketplaceDiscovery: &MarketplaceDiscovery{Endpoint: endpoint, Item: item, CacheState: marketplace.DiscoveryCacheFresh, ObservedAt: time.Unix(3, 0)},
	}
}

func selectedMarketplaceTestRow(instanceID, registryID, name string) Row {
	entry := config.MarketplaceEntry{URL: "https://market.example", InstanceID: instanceID, Alias: "configured"}
	return Row{
		Artifact: resolver.ResolvedArtifact{
			Entry:                   catalog.Entry{Kind: ir.KindSkill, Name: name},
			IsMarketplace:           true,
			MarketplaceURL:          entry.URL,
			MarketplaceInstanceID:   instanceID,
			MarketplaceRegistryID:   registryID,
			MarketplaceResourceName: name,
		},
		EnabledGlobal: true,
		MarketplaceSelections: map[config.Source]MarketplaceSelection{
			config.UserPersonal: {Entry: entry, Pin: config.MarketplaceResourcePin{RegistryID: registryID, Kind: "skill", Name: name}},
		},
	}
}

func provisionalMarketplaceTestRow(endpointURL, registryID, name string) Row {
	return Row{
		Artifact: resolver.ResolvedArtifact{
			Entry:                   catalog.Entry{Kind: ir.KindSkill, Name: name},
			IsMarketplace:           true,
			MarketplaceURL:          endpointURL,
			MarketplaceInstanceID:   "market-instance",
			MarketplaceRegistryID:   registryID,
			MarketplaceResourceName: name,
		},
		PendingVerification: true,
	}
}
