package api

import (
	"math"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

var adoptionFixedNow = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

const (
	telemetryAlpha    = "example.test/team/alpha"
	telemetryOldAlpha = "example.test/team/old-alpha"
	telemetryBeta     = "example.test/team/beta"
	telemetryShared   = "example.test/team/shared"
	telemetryLocal    = "local"
)

func adoptionTestRegistries() map[store.RegistryID]store.Registry {
	return map[store.RegistryID]store.Registry{
		"rA": {ID: "rA", Origins: []string{
			"https://example.test/team/alpha.git",
			"git@example.test:team/alpha.git",
			"https://example.test/team/old-alpha.git",
		}},
		"rB": {ID: "rB", Origins: []string{"https://example.test/team/beta.git"}},
		"rC": {ID: "rC", Origins: []string{"https://example.test/team/shared.git"}},
		"rD": {ID: "rD", Origins: []string{"https://example.test/team/shared.git"}},
	}
}

func inventorySnapshotSample(installationID, fingerprint string, eventTime time.Time) inventoryMetricSample {
	return inventoryMetricSample{
		Metric: map[string]string{
			"inventory_type":     "snapshot",
			"installation_id":    installationID,
			"config_fingerprint": fingerprint,
		},
		Value: float64(eventTime.UnixMicro()),
	}
}

func inventoryResourceSample(installationID, fingerprint, registryID, kind, name string) inventoryMetricSample {
	return inventoryMetricSample{
		Metric: map[string]string{
			"inventory_type":     "resource",
			"installation_id":    installationID,
			"config_fingerprint": fingerprint,
			"registry_id":        registryID,
			"resource_kind":      kind,
			"resource_name":      name,
		},
		Value: 1,
	}
}

func adoptionKey(registry store.RegistryID, kind, name string) installationResourceKey {
	return installationResourceKey{Registry: registry, Kind: kind, Name: name}
}

func aggregateAdoptionOrFail(t *testing.T, samples []inventoryMetricSample, registries map[store.RegistryID]store.Registry) installationCounts {
	t.Helper()
	counts, err := aggregateInstallationMetrics(samples, registries, adoptionFixedNow)
	if err != nil {
		t.Fatalf("aggregateInstallationMetrics() error = %v, want nil", err)
	}
	return counts
}

func assertAdoptionCounts(t *testing.T, got installationCounts, want map[installationResourceKey]int64, known map[store.RegistryID]bool) {
	t.Helper()
	if len(got.Counts) != len(want) {
		t.Fatalf("counts size = %d, want %d; got %v", len(got.Counts), len(want), got.Counts)
	}
	for key, wantCount := range want {
		if got.Counts[key] != wantCount {
			t.Errorf("counts[%v] = %d, want %d", key, got.Counts[key], wantCount)
		}
	}
	if known == nil {
		return
	}
	for id, wantKnown := range known {
		if got.KnownRegistries[id] != wantKnown {
			t.Errorf("knownRegistries[%q] = %v, want %v", id, got.KnownRegistries[id], wantKnown)
		}
	}
}

// spec: marketplace-api/latest-inventory-excludes-removed-and-stale-resources
func TestInstallationAdoptionLatestGenerationExcludesRemovedResources(t *testing.T) {
	fresh := adoptionFixedNow.Add(-time.Hour)
	older := adoptionFixedNow.Add(-2 * time.Hour)

	samples := []inventoryMetricSample{
		inventorySnapshotSample("install-1", "gen-old", older),
		inventoryResourceSample("install-1", "gen-old", telemetryAlpha, "skill", "removed"),
		inventoryResourceSample("install-1", "gen-old", telemetryAlpha, "skill", "kept"),
		inventorySnapshotSample("install-1", "gen-new", fresh),
		inventoryResourceSample("install-1", "gen-new", telemetryAlpha, "skill", "kept"),
		inventoryResourceSample("install-1", "gen-new", telemetryAlpha, "skill", "kept"),
	}

	counts := aggregateAdoptionOrFail(t, samples, adoptionTestRegistries())
	assertAdoptionCounts(t, counts, map[installationResourceKey]int64{
		adoptionKey("rA", "skill", "kept"): 1,
	}, map[store.RegistryID]bool{"rA": true})
}

// spec: marketplace-api/latest-inventory-excludes-removed-and-stale-resources
func TestInstallationAdoptionStaleSnapshotExcludedWithFreshControl(t *testing.T) {
	fresh := adoptionFixedNow.Add(-time.Hour)
	stale := adoptionFixedNow.Add(-31 * 24 * time.Hour)

	samples := []inventoryMetricSample{
		inventorySnapshotSample("install-stale", "gen-stale", stale),
		inventoryResourceSample("install-stale", "gen-stale", telemetryAlpha, "skill", "old-resource"),
		inventorySnapshotSample("install-fresh", "gen-fresh", fresh),
		inventoryResourceSample("install-fresh", "gen-fresh", telemetryAlpha, "skill", "current-resource"),
	}

	counts := aggregateAdoptionOrFail(t, samples, adoptionTestRegistries())
	assertAdoptionCounts(t, counts, map[installationResourceKey]int64{
		adoptionKey("rA", "skill", "current-resource"): 1,
	}, map[store.RegistryID]bool{"rA": true})
}

// spec: marketplace-api/latest-inventory-excludes-removed-and-stale-resources
func TestInstallationAdoptionExactThirtyDayBoundaryCounts(t *testing.T) {
	boundary := adoptionFixedNow.Add(-30 * 24 * time.Hour)

	samples := []inventoryMetricSample{
		inventorySnapshotSample("install-1", "gen-1", boundary),
		inventoryResourceSample("install-1", "gen-1", telemetryAlpha, "skill", "edge"),
	}

	counts := aggregateAdoptionOrFail(t, samples, adoptionTestRegistries())
	assertAdoptionCounts(t, counts, map[installationResourceKey]int64{
		adoptionKey("rA", "skill", "edge"): 1,
	}, map[store.RegistryID]bool{"rA": true})
}

// spec: marketplace-api/latest-inventory-excludes-removed-and-stale-resources
func TestInstallationAdoptionTiedGenerationFingerprintsOmitInstallation(t *testing.T) {
	fresh := adoptionFixedNow.Add(-time.Hour)

	samples := []inventoryMetricSample{
		inventorySnapshotSample("install-tied", "gen-a", fresh),
		inventorySnapshotSample("install-tied", "gen-b", fresh),
		inventoryResourceSample("install-tied", "gen-a", telemetryAlpha, "skill", "ambiguous"),
		inventoryResourceSample("install-tied", "gen-b", telemetryAlpha, "skill", "ambiguous"),
		inventorySnapshotSample("install-ok", "gen-ok", fresh),
		inventoryResourceSample("install-ok", "gen-ok", telemetryBeta, "skill", "ambiguous"),
	}

	counts := aggregateAdoptionOrFail(t, samples, adoptionTestRegistries())
	assertAdoptionCounts(t, counts, map[installationResourceKey]int64{
		adoptionKey("rB", "skill", "ambiguous"): 1,
	}, map[store.RegistryID]bool{"rB": true})
}

// spec: marketplace-api/latest-inventory-excludes-removed-and-stale-resources
func TestInstallationAdoptionNoFreshSnapshotsReturnsError(t *testing.T) {
	stale := adoptionFixedNow.Add(-31 * 24 * time.Hour)

	samples := []inventoryMetricSample{
		inventorySnapshotSample("install-stale", "gen-stale", stale),
		inventoryResourceSample("install-stale", "gen-stale", telemetryAlpha, "skill", "old-resource"),
	}

	counts, err := aggregateInstallationMetrics(samples, adoptionTestRegistries(), adoptionFixedNow)
	if err == nil {
		t.Fatalf("aggregateInstallationMetrics() error = nil with counts %v; want an unavailable error, not measured zeros", counts.Counts)
	}
}

// spec: marketplace-api/latest-inventory-excludes-removed-and-stale-resources
func TestInstallationAdoptionFutureSnapshotOmitsInstallation(t *testing.T) {
	fresh := adoptionFixedNow.Add(-time.Hour)

	samples := []inventoryMetricSample{
		inventorySnapshotSample("install-future", "gen-future", adoptionFixedNow.Add(24*time.Hour)),
		inventoryResourceSample("install-future", "gen-future", telemetryAlpha, "skill", "future-resource"),
		inventorySnapshotSample("install-ok", "gen-ok", fresh),
		inventoryResourceSample("install-ok", "gen-ok", telemetryBeta, "skill", "current"),
	}

	counts := aggregateAdoptionOrFail(t, samples, adoptionTestRegistries())
	assertAdoptionCounts(t, counts, map[installationResourceKey]int64{
		adoptionKey("rB", "skill", "current"): 1,
	}, nil)
}

// spec: marketplace-api/latest-inventory-excludes-removed-and-stale-resources
func TestInstallationAdoptionMalformedSnapshotValueReturnsError(t *testing.T) {
	fresh := adoptionFixedNow.Add(-time.Hour)

	samples := []inventoryMetricSample{
		{Metric: map[string]string{
			"inventory_type":     "snapshot",
			"installation_id":    "install-bad",
			"config_fingerprint": "gen-bad",
		}, Value: math.NaN()},
		inventoryResourceSample("install-bad", "gen-bad", telemetryAlpha, "skill", "bad-resource"),
		inventorySnapshotSample("install-ok", "gen-ok", fresh),
		inventoryResourceSample("install-ok", "gen-ok", telemetryBeta, "skill", "current"),
	}

	counts, err := aggregateInstallationMetrics(samples, adoptionTestRegistries(), adoptionFixedNow)
	if err == nil {
		t.Fatalf("aggregateInstallationMetrics() error = nil with counts %v; want an error for a malformed snapshot value", counts.Counts)
	}
}

// spec: marketplace-api/registry-identity-matching-does-not-conflate-resources
func TestInstallationAdoptionSameNameDifferentOriginsStaySeparate(t *testing.T) {
	fresh := adoptionFixedNow.Add(-time.Hour)

	samples := []inventoryMetricSample{
		inventorySnapshotSample("install-1", "gen-1", fresh),
		inventoryResourceSample("install-1", "gen-1", telemetryAlpha, "skill", "shared-name"),
		inventorySnapshotSample("install-2", "gen-2", fresh),
		inventoryResourceSample("install-2", "gen-2", telemetryBeta, "skill", "shared-name"),
	}

	counts := aggregateAdoptionOrFail(t, samples, adoptionTestRegistries())
	assertAdoptionCounts(t, counts, map[installationResourceKey]int64{
		adoptionKey("rA", "skill", "shared-name"): 1,
		adoptionKey("rB", "skill", "shared-name"): 1,
	}, map[store.RegistryID]bool{"rA": true, "rB": true})
}

// spec: marketplace-api/registry-identity-matching-does-not-conflate-resources
func TestInstallationAdoptionOneRegistryAcrossOriginsCountsOnce(t *testing.T) {
	fresh := adoptionFixedNow.Add(-time.Hour)

	samples := []inventoryMetricSample{
		inventorySnapshotSample("install-1", "gen-1", fresh),
		inventoryResourceSample("install-1", "gen-1", telemetryAlpha, "skill", "multi-origin"),
		inventoryResourceSample("install-1", "gen-1", telemetryOldAlpha, "skill", "multi-origin"),
		inventoryResourceSample("install-1", "gen-1", telemetryOldAlpha, "skill", "historical-only"),
		inventorySnapshotSample("install-2", "gen-2", fresh),
		inventoryResourceSample("install-2", "gen-2", telemetryAlpha, "skill", "multi-origin"),
	}

	counts := aggregateAdoptionOrFail(t, samples, adoptionTestRegistries())
	assertAdoptionCounts(t, counts, map[installationResourceKey]int64{
		adoptionKey("rA", "skill", "multi-origin"):    2,
		adoptionKey("rA", "skill", "historical-only"): 1,
	}, map[store.RegistryID]bool{"rA": true})
}

// spec: marketplace-api/registry-identity-matching-does-not-conflate-resources
func TestInstallationAdoptionCanonicalIDMatchesSpellingsAndLocalIsUnknown(t *testing.T) {
	fresh := adoptionFixedNow.Add(-time.Hour)

	registries := adoptionTestRegistries()
	registries["rL"] = store.Registry{ID: "rL", Origins: []string{"file:///home/operator/private-registry"}}

	samples := []inventoryMetricSample{
		inventorySnapshotSample("install-1", "gen-1", fresh),
		inventoryResourceSample("install-1", "gen-1", telemetryAlpha, "skill", "canonical-reported"),
		inventorySnapshotSample("install-local", "gen-2", fresh),
		inventoryResourceSample("install-local", "gen-2", telemetryLocal, "skill", "local-only"),
	}

	counts := aggregateAdoptionOrFail(t, samples, registries)
	assertAdoptionCounts(t, counts, map[installationResourceKey]int64{
		adoptionKey("rA", "skill", "canonical-reported"): 1,
	}, map[store.RegistryID]bool{"rA": true, "rL": false})
	if _, ok := counts.Counts[adoptionKey("rL", "skill", "local-only")]; ok {
		t.Errorf("unknown local identity produced an rL count entry: %v", counts.Counts)
	}
}

// spec: marketplace-api/registry-identity-matching-does-not-conflate-resources
func TestInstallationAdoptionAmbiguousOriginContributesNothing(t *testing.T) {
	fresh := adoptionFixedNow.Add(-time.Hour)

	samples := []inventoryMetricSample{
		inventorySnapshotSample("install-ambig", "gen-1", fresh),
		inventoryResourceSample("install-ambig", "gen-1", telemetryShared, "skill", "shared-origin-resource"),
		inventorySnapshotSample("install-ok", "gen-2", fresh),
		inventoryResourceSample("install-ok", "gen-2", telemetryBeta, "skill", "shared-origin-resource"),
	}

	counts := aggregateAdoptionOrFail(t, samples, adoptionTestRegistries())
	assertAdoptionCounts(t, counts, map[installationResourceKey]int64{
		adoptionKey("rB", "skill", "shared-origin-resource"): 1,
	}, map[store.RegistryID]bool{"rB": true, "rC": false, "rD": false})
	if _, ok := counts.Counts[adoptionKey("rC", "skill", "shared-origin-resource")]; ok {
		t.Errorf("ambiguous origin produced an rC count entry: %v", counts.Counts)
	}
	if _, ok := counts.Counts[adoptionKey("rD", "skill", "shared-origin-resource")]; ok {
		t.Errorf("ambiguous origin produced an rD count entry: %v", counts.Counts)
	}
}

// spec: marketplace-api/registry-identity-matching-does-not-conflate-resources
func TestInstallationAdoptionDuplicateOriginSpellingsAreNotAmbiguous(t *testing.T) {
	fresh := adoptionFixedNow.Add(-time.Hour)

	registries := map[store.RegistryID]store.Registry{
		"rE": {ID: "rE", Origins: []string{
			"https://EXAMPLE.test:443/team/dup.git",
			"git@example.test:team/dup.git",
		}},
	}
	samples := []inventoryMetricSample{
		inventorySnapshotSample("install-1", "gen-1", fresh),
		inventoryResourceSample("install-1", "gen-1", "example.test/team/dup", "skill", "dup"),
	}

	counts := aggregateAdoptionOrFail(t, samples, registries)
	assertAdoptionCounts(t, counts, map[installationResourceKey]int64{
		adoptionKey("rE", "skill", "dup"): 1,
	}, map[store.RegistryID]bool{"rE": true})
}

// spec: marketplace-api/latest-inventory-excludes-removed-and-stale-resources
func TestInstallationAdoptionQuery(t *testing.T) {
	wantSelected := `label_replace(max by (installation_id, config_fingerprint) (last_over_time(agent_marketplace_config_snapshot{endpoint="metrics",namespace="inventory-test"}[30d])), "inventory_type", "snapshot", "", "") or label_replace(max by (installation_id, config_fingerprint, registry_id, resource_kind, resource_name) (last_over_time(agent_marketplace_config_resource{endpoint="metrics",namespace="inventory-test"}[30d])), "inventory_type", "resource", "", "")`
	if got := installationMetricsQuery(map[string]string{"endpoint": "metrics", "namespace": "inventory-test"}); got != wantSelected {
		t.Errorf("installationMetricsQuery() = %q, want %q", got, wantSelected)
	}

	wantEmpty := `label_replace(max by (installation_id, config_fingerprint) (last_over_time(agent_marketplace_config_snapshot{}[30d])), "inventory_type", "snapshot", "", "") or label_replace(max by (installation_id, config_fingerprint, registry_id, resource_kind, resource_name) (last_over_time(agent_marketplace_config_resource{}[30d])), "inventory_type", "resource", "", "")`
	if got := installationMetricsQuery(map[string]string{}); got != wantEmpty {
		t.Errorf("installationMetricsQuery(empty) = %q, want %q", got, wantEmpty)
	}
}
