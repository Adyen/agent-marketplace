package config

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func BenchmarkParseV4(b *testing.B) {
	data := []byte(`apiVersion: agent-marketplace/v4
sources: {m: {marketplace: https://market.example}}
resources:
  m:
    include:
      - tools/skill/review: {as: review}
      - tools/skill/deploy: {as: deploy}
      - tools/skill/audit: {as: audit}
`)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParseV4(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkV4MarketplaceLockHydration(b *testing.B) {
	digest := func(i int) string { return fmt.Sprintf("sha256:%064x", i) }
	collectionDigest := digest(0)
	resources := make([]lockfile.MarketplaceResourceLock, 0, 101)
	resources = append(resources, lockfile.MarketplaceResourceLock{
		RegistryName: "collections", RegistryID: "collections-id", Kind: "collection", Name: "onboarding",
		Revision: collectionDigest, ContentDigest: lockfile.Digest(collectionDigest),
		CollectionManifestDigest: lockfile.Digest(collectionDigest),
		Acquisition:              lockfile.MarketplaceAcquisitionMirror,
	})
	var includes strings.Builder
	for i := 1; i <= 100; i++ {
		d := digest(i)
		resources = append(resources, lockfile.MarketplaceResourceLock{
			RegistryName: "tools", RegistryID: "tools-id", Kind: "skill", Name: fmt.Sprintf("skill-%03d", i),
			Revision: d, ContentDigest: lockfile.Digest(d), ArchiveDigest: lockfile.Digest(digest(i + 1000)),
			Acquisition: lockfile.MarketplaceAcquisitionMirror,
			SelectedBy: []lockfile.MarketplaceSelectionLock{
				{RegistryID: "collections-id", Name: "onboarding", Revision: lockfile.Digest(collectionDigest)},
			},
		})
	}
	includes.WriteString("      - collections/collection/onboarding\n")
	v4, err := ParseV4([]byte("apiVersion: agent-marketplace/v4\nsources: {m: {marketplace: https://market.example}}\nresources:\n  m:\n    include:\n" + includes.String()))
	if err != nil {
		b.Fatal(err)
	}
	doc, err := lockfile.Normalize(lockfile.Document{
		APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo,
		Marketplaces: []lockfile.MarketplaceLock{{
			URL: "https://market.example", InstanceID: "market-id",
			RegistryBindings: []lockfile.MarketplaceRegistryBinding{
				{Name: "collections", RegistryID: "collections-id"},
				{Name: "tools", RegistryID: "tools-id"},
			},
			Resources: resources,
		}},
	})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := v4.toLegacyConfig(repoLayer, &doc); err != nil {
			b.Fatal(err)
		}
	}
}
