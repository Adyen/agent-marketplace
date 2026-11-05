package config

import (
	"strings"
	"testing"
)

func TestMarketplaceCatalogKeyDimensionsAndAliases(t *testing.T) {
	base := MarketplaceEntry{URL: "https://EXAMPLE.invalid/market/", InstanceID: "instance"}
	pin := MarketplaceResourcePin{RegistryID: "registry:one", Kind: "mcpserver", Name: "probe", Digest: "sha256:" + strings.Repeat("a", 64), Revision: "main:one"}
	baseKey := MarketplaceCatalogKey(base, pin)
	wantKey := "marketplace:https://example.invalid/market\x00instance\x00registry:one\x00mcpserver\x00probe\x00sha256:" + strings.Repeat("a", 64) + "\x00main:one\x00mirror\x00\x00"
	if baseKey != wantKey {
		t.Fatalf("catalog key = %q, want independently authored key %q", baseKey, wantKey)
	}
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
	if MarketplaceCatalogKey(MarketplaceEntry{URL: base.URL, InstanceID: base.InstanceID, Alias: "public", Source: UserPersonal}, pin) != baseKey {
		t.Fatal("source or authored source alias changed catalog identity")
	}
	for _, entry := range []MarketplaceEntry{{URL: "https://other.invalid/market", InstanceID: "instance"}, {URL: base.URL, InstanceID: "other-instance"}} {
		if MarketplaceCatalogKey(entry, pin) == baseKey {
			t.Fatalf("endpoint/instance variant reused catalog key: %#v", entry)
		}
	}
	indexPin := pin
	indexPin.Acquisition, indexPin.RegistryCommit, indexPin.RegistryManifestDigest = "index-only", strings.Repeat("c", 40), "sha256:"+strings.Repeat("d", 64)
	indexKey := MarketplaceCatalogKey(base, indexPin)
	changedCommit := indexPin
	changedCommit.RegistryCommit = strings.Repeat("e", 40)
	changedManifest := indexPin
	changedManifest.RegistryManifestDigest = "sha256:" + strings.Repeat("f", 64)
	if MarketplaceCatalogKey(base, changedCommit) == indexKey || MarketplaceCatalogKey(base, changedManifest) == indexKey {
		t.Fatal("exact index-only occurrence fields did not affect catalog identity independently")
	}
	metadata := pin
	metadata.RegistrySlug, metadata.InstalledName, metadata.Direct = "renamed-registry", "chosen-probe", true
	metadata.SelectedBy = MarketplaceSelectionsFrom([]MarketplaceSelection{{RegistryID: "collections", Name: "bundle", Revision: "sha256:" + strings.Repeat("b", 64)}})
	if MarketplaceCatalogKey(base, metadata) != baseKey {
		t.Fatal("selector graph or display metadata changed exact catalog identity")
	}
}
