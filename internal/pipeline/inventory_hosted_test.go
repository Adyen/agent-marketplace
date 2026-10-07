package pipeline

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: marketplace-api/mixed-hosted-and-git-inventory-remains-complete
func TestBuildInventorySnapshotMixedHostedAndGitRemainsComplete(t *testing.T) {
	tests := []struct {
		name           string
		marketplaceURL string
		registryURL    string
	}{
		{name: "root mirror endpoint", marketplaceURL: "https://marketplace.example", registryURL: "https://marketplace.example"},
		{name: "pathful mirror endpoint", marketplaceURL: "https://marketplace.example/gateway/v1", registryURL: "https://marketplace.example/gateway/v1"},
		{name: "index-only cached endpoint", marketplaceURL: "https://marketplace.example/gateway/v1", registryURL: "https://marketplace.example"},
	}
	fingerprints := make([]string, 0, len(tests))
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := inventoryResolveResult()
			result.Resolved = append(result.Resolved, hostedInventoryArtifact(
				"adyen", "awesome-ai", "canonical-review", "local-alias", test.marketplaceURL,
				test.registryURL, "awesome-ai", "sha256:revision", config.RepoCommitted, false,
			))
			original := append([]resolver.ResolvedArtifact(nil), result.Resolved...)

			snapshot, omission, ok := BuildInventorySnapshot(result)
			if !ok || omission != "" {
				t.Fatalf("mixed inventory omitted: ok=%v reason=%q", ok, omission)
			}
			if !reflect.DeepEqual(result.Resolved, original) {
				t.Fatal("inventory construction mutated resolved artifacts")
			}
			if len(snapshot.Registries) != len(result.MergedCfg.Registries)+1 {
				t.Fatalf("registry count = %d, want configured Git registries plus one hosted registry", len(snapshot.Registries))
			}

			var hostedResource *telemetry.InventoryResource
			for i := range snapshot.Resources {
				resource := &snapshot.Resources[i]
				if resource.RegistryLocation == "marketplace" {
					hostedResource = resource
					break
				}
			}
			if hostedResource == nil {
				t.Fatalf("hosted resource missing from snapshot: %#v", snapshot.Resources)
			}
			if hostedResource.RegistryID != "awesome-ai" || hostedResource.RegistryPath != "" ||
				hostedResource.RegistryRef != "sha256:revision" || hostedResource.Kind != "skill" ||
				hostedResource.Name != "canonical-review" || hostedResource.ConfigScope != "repo" || hostedResource.Required {
				t.Fatalf("hosted resource identity = %#v", *hostedResource)
			}

			var hostedRegistry *telemetry.InventoryRegistry
			for i := range snapshot.Registries {
				registry := &snapshot.Registries[i]
				if registry.ID == "awesome-ai" && registry.Location == "marketplace" {
					hostedRegistry = registry
					break
				}
			}
			if hostedRegistry == nil || hostedRegistry.Path != "" || hostedRegistry.Ref != "" ||
				hostedRegistry.ConfigScope != "repo" || hostedRegistry.Required || hostedRegistry.Default {
				t.Fatalf("hosted registry row = %#v", hostedRegistry)
			}

			var wire struct {
				Registries []struct {
					ID            string
					Location      string
					SourceKind    string
					MarketplaceID string
				} `json:"registries"`
				Resources []struct {
					RegistryID    string
					Name          string
					SourceKind    string
					MarketplaceID string
				} `json:"resources"`
			}
			encoded, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatalf("marshal inventory: %v", err)
			}
			if err := json.Unmarshal(encoded, &wire); err != nil {
				t.Fatalf("decode inventory identity fields: %v", err)
			}
			registryIdentityFound, resourceIdentityFound := false, false
			for _, registry := range wire.Registries {
				if registry.ID == "awesome-ai" && registry.Location == "marketplace" {
					registryIdentityFound = registry.SourceKind == "marketplace" && registry.MarketplaceID == "adyen"
				}
			}
			for _, resource := range wire.Resources {
				if resource.RegistryID == "awesome-ai" && resource.Name == "canonical-review" {
					resourceIdentityFound = resource.SourceKind == "marketplace" && resource.MarketplaceID == "adyen"
				}
			}
			if !registryIdentityFound || !resourceIdentityFound {
				t.Fatalf("hosted wire identity missing: registries=%#v resources=%#v", wire.Registries, wire.Resources)
			}
			if strings.Contains(string(encoded), test.marketplaceURL) || strings.Contains(string(encoded), test.registryURL) {
				t.Fatalf("Marketplace endpoint leaked into inventory: %s", encoded)
			}
			fingerprints = append(fingerprints, snapshot.Fingerprint)
		})
	}
	if len(fingerprints) == len(tests) && (fingerprints[0] != fingerprints[1] || fingerprints[1] != fingerprints[2]) {
		t.Fatalf("Marketplace endpoint spelling changed fingerprint: %v", fingerprints)
	}
}

func TestBuildInventorySnapshotHostedRegistryRowsUseIdentityTuple(t *testing.T) {
	result := inventoryResolveResult()
	first := hostedInventoryArtifact("adyen", "awesome-ai", "one", "one", "https://marketplace.example", "https://github.com/Adyen/cache.git", "awesome-ai", "v1", config.RepoCommitted, false)
	second := first
	second.MarketplaceResourceName = "two"
	second.Name = "two"
	otherScope := first
	otherScope.MarketplaceResourceName = "three"
	otherScope.Name = "three"
	otherScope.Source = config.UserPersonal
	otherRequired := first
	otherRequired.MarketplaceResourceName = "four"
	otherRequired.Name = "four"
	otherRequired.Required = true
	result.Resolved = append(result.Resolved, first, second, otherScope, otherRequired)

	snapshot, omission, ok := BuildInventorySnapshot(result)
	if !ok || omission != "" {
		t.Fatalf("hosted inventory omitted: ok=%v reason=%q", ok, omission)
	}
	hostedRows := 0
	for _, registry := range snapshot.Registries {
		if registry.Location == "marketplace" {
			hostedRows++
		}
	}
	if hostedRows != 3 {
		t.Fatalf("hosted registry rows = %d, want one per instance/registry/scope/required tuple", hostedRows)
	}
}

func TestBuildInventorySnapshotHostedInstancesFingerprintSeparatelyAndSortDeterministically(t *testing.T) {
	base := inventoryResolveResult()
	first := hostedInventoryArtifact("adyen", "awesome-ai", "canonical-review", "local-alias", "https://marketplace.example", "https://github.com/Adyen/cache.git", "awesome-ai", "sha256:revision", config.RepoCommitted, false)
	otherInstance := first
	otherInstance.MarketplaceInstanceID = "other-marketplace"

	firstResult := base
	firstResult.Resolved = append(append([]resolver.ResolvedArtifact(nil), base.Resolved...), first)
	otherResult := base
	otherResult.Resolved = append(append([]resolver.ResolvedArtifact(nil), base.Resolved...), otherInstance)
	firstSnapshot, firstOmission, firstOK := BuildInventorySnapshot(firstResult)
	otherSnapshot, otherOmission, otherOK := BuildInventorySnapshot(otherResult)
	if !firstOK || firstOmission != "" || !otherOK || otherOmission != "" {
		t.Fatalf("hosted identity snapshots omitted: first=%q other=%q", firstOmission, otherOmission)
	}
	if firstSnapshot.Fingerprint == otherSnapshot.Fingerprint {
		t.Fatalf("Marketplace instance did not affect fingerprint: %q", firstSnapshot.Fingerprint)
	}

	both := base
	both.Resolved = append(append([]resolver.ResolvedArtifact(nil), base.Resolved...), first, otherInstance)
	reversed := base
	reversed.Resolved = append(append([]resolver.ResolvedArtifact(nil), base.Resolved...), otherInstance, first)
	forwardSnapshot, forwardOmission, forwardOK := BuildInventorySnapshot(both)
	reverseSnapshot, reverseOmission, reverseOK := BuildInventorySnapshot(reversed)
	if !forwardOK || forwardOmission != "" || !reverseOK || reverseOmission != "" {
		t.Fatalf("multiple hosted inventory omitted: forward=%q reverse=%q", forwardOmission, reverseOmission)
	}
	if forwardSnapshot.Fingerprint != reverseSnapshot.Fingerprint {
		t.Fatalf("resolved ordering changed hosted fingerprint: %q != %q", forwardSnapshot.Fingerprint, reverseSnapshot.Fingerprint)
	}
}

// spec: marketplace-api/invalid-hosted-identity-preserves-safe-omission
func TestBuildInventorySnapshotRejectsInvalidHostedIdentityWithoutPartialData(t *testing.T) {
	tests := []struct {
		name       string
		instanceID string
		registryID string
		nameValue  string
		want       telemetry.InventoryOmissionReason
	}{
		{name: "missing instance", instanceID: "", registryID: "awesome-ai", nameValue: "canonical-review", want: telemetry.InventoryOmissionInvalid},
		{name: "missing registry", instanceID: "adyen", registryID: "", nameValue: "canonical-review", want: telemetry.InventoryOmissionInvalid},
		{name: "missing canonical name", instanceID: "adyen", registryID: "awesome-ai", nameValue: "", want: telemetry.InventoryOmissionInvalid},
		{name: "dot instance", instanceID: ".", registryID: "awesome-ai", nameValue: "canonical-review", want: telemetry.InventoryOmissionInvalid},
		{name: "separator registry", instanceID: "adyen", registryID: "../private", nameValue: "canonical-review", want: telemetry.InventoryOmissionInvalid},
		{name: "control canonical name", instanceID: "adyen", registryID: "awesome-ai", nameValue: "canonical\nreview", want: telemetry.InventoryOmissionInvalid},
		{name: "invalid UTF-8 canonical name", instanceID: "adyen", registryID: "awesome-ai", nameValue: string([]byte{0xff}), want: telemetry.InventoryOmissionInvalid},
		{name: "oversized instance", instanceID: strings.Repeat("i", telemetry.MaxInventoryAttributeBytes+1), registryID: "awesome-ai", nameValue: "canonical-review", want: telemetry.InventoryOmissionTooLarge},
		{name: "oversized registry", instanceID: "adyen", registryID: strings.Repeat("r", telemetry.MaxInventoryAttributeBytes+1), nameValue: "canonical-review", want: telemetry.InventoryOmissionTooLarge},
		{name: "oversized canonical name", instanceID: "adyen", registryID: "awesome-ai", nameValue: strings.Repeat("n", telemetry.MaxInventoryAttributeBytes+1), want: telemetry.InventoryOmissionTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := inventoryResolveResult()
			result.Resolved = append(result.Resolved, hostedInventoryArtifact(
				test.instanceID, test.registryID, test.nameValue, "local-alias", "https://marketplace.example",
				"https://github.com/Adyen/cache.git", "awesome-ai", "sha256:revision", config.RepoCommitted, false,
			))
			snapshot, omission, ok := BuildInventorySnapshot(result)
			if ok || omission != test.want || !reflect.DeepEqual(snapshot, telemetry.InventorySnapshot{}) {
				t.Fatalf("got ok=%v omission=%q snapshot=%#v; want whole-inventory omission %q", ok, omission, snapshot, test.want)
			}
		})
	}
}

func TestBuildInventorySnapshotHostedRegistriesCountTowardTotalBound(t *testing.T) {
	result := inventoryResolveResult()
	for len(result.MergedCfg.Registries) < telemetry.MaxInventoryRegistries {
		index := len(result.MergedCfg.Registries)
		result.MergedCfg.Registries = append(result.MergedCfg.Registries, config.RegistryEntry{
			URL: fmt.Sprintf("https://github.com/Adyen/hosted-bound-%04d.git", index),
		})
	}
	result.Resolved = append(result.Resolved, hostedInventoryArtifact(
		"adyen", "awesome-ai", "canonical-review", "local-alias", "https://marketplace.example",
		"https://github.com/Adyen/cache.git", "awesome-ai", "sha256:revision", config.RepoCommitted, false,
	))

	snapshot, omission, ok := BuildInventorySnapshot(result)
	if ok || omission != telemetry.InventoryOmissionTooLarge || !reflect.DeepEqual(snapshot, telemetry.InventorySnapshot{}) {
		t.Fatalf("hosted row beyond registry bound = ok:%v omission:%q snapshot:%#v", ok, omission, snapshot)
	}
}

func TestBuildInventorySnapshotLegacyFingerprintUnchanged(t *testing.T) {
	snapshot, omission, ok := BuildInventorySnapshot(inventoryResolveResult())
	if !ok || omission != "" {
		t.Fatalf("legacy inventory omitted: ok=%v reason=%q", ok, omission)
	}
	const want = "b813e4aaf3823c2e8a202880530ed74612227f5a776f95d9c25c8228a9e27565"
	if snapshot.Fingerprint != want {
		t.Fatalf("legacy fingerprint = %q, want unchanged %q", snapshot.Fingerprint, want)
	}
}

func hostedInventoryArtifact(instanceID, registryID, resourceName, installedName, marketplaceURL, registryURL, registryPath, ref string, source config.Source, required bool) resolver.ResolvedArtifact {
	return resolver.ResolvedArtifact{
		Entry: catalog.Entry{Kind: ir.KindSkill, Name: installedName}, RegistryURL: registryURL, RegistryPath: registryPath,
		Ref: ref, Source: source, Required: required, IsMarketplace: true, MarketplaceURL: marketplaceURL,
		MarketplaceInstanceID: instanceID, MarketplaceRegistryID: registryID, MarketplaceResourceName: resourceName,
	}
}
