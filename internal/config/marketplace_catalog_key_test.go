package config

import (
	"strings"
	"testing"
)

func TestMarketplaceCatalogKeyDimensionsAndAliases(t *testing.T) {
	base := MarketplaceEntry{URL: "https://EXAMPLE.invalid/market/", InstanceID: "instance"}
	pin := MarketplaceResourcePin{RegistryID: "registry:one", Kind: "mcpserver", Name: "probe", Digest: "sha256:" + strings.Repeat("a", 64), Revision: "main:one"}
	baseKey := MarketplaceCatalogKey(base, pin)
	if baseKey != MarketplaceCatalogKey(MarketplaceEntry{URL: "https://example.invalid/market", InstanceID: "instance"}, MarketplaceResourcePin{RegistryID: pin.RegistryID, Kind: pin.Kind, Name: pin.Name, Digest: pin.Digest, Revision: pin.Revision, Acquisition: MarketplaceAcquisitionMirror}) {
		t.Fatal("omitted and explicit mirror acquisition or normalized endpoint changed identity")
	}
	variants := []MarketplaceResourcePin{
		{RegistryID: "registry:two", Kind: pin.Kind, Name: pin.Name, Digest: pin.Digest, Revision: pin.Revision},
		{RegistryID: pin.RegistryID, Kind: "skill", Name: pin.Name, Digest: pin.Digest, Revision: pin.Revision},
		{RegistryID: pin.RegistryID, Kind: pin.Kind, Name: "other", Digest: pin.Digest, Revision: pin.Revision},
		{RegistryID: pin.RegistryID, Kind: pin.Kind, Name: pin.Name, Digest: "sha256:" + strings.Repeat("b", 64), Revision: pin.Revision},
		{RegistryID: pin.RegistryID, Kind: pin.Kind, Name: pin.Name, Digest: pin.Digest, Revision: "other"},
		{RegistryID: pin.RegistryID, Kind: pin.Kind, Name: pin.Name, Digest: pin.Digest, Revision: pin.Revision, Acquisition: MarketplaceAcquisitionIndexOnly, RegistryCommit: strings.Repeat("c", 40), RegistryManifestDigest: "sha256:" + strings.Repeat("d", 64)},
	}
	for i, variant := range variants {
		if key := MarketplaceCatalogKey(base, variant); key == baseKey {
			t.Fatalf("variant %d reused catalog identity", i)
		}
	}
	if MarketplaceCatalogKey(base, MarketplaceResourcePin{RegistryID: pin.RegistryID, Kind: pin.Kind, Name: pin.Name, Digest: pin.Digest, Revision: pin.Revision, InstalledName: "local-review"}) != baseKey {
		t.Fatal("installed name changed catalog identity")
	}
	if MarketplaceCatalogKey(MarketplaceEntry{URL: base.URL, InstanceID: base.InstanceID, Source: UserPersonal}, pin) != baseKey {
		t.Fatal("source changed catalog identity")
	}
}
