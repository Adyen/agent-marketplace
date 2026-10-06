package api

import (
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/internal/registryid"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

const installationInventoryWindow = 30 * 24 * time.Hour

var (
	errInstallationMetricsUnavailable = errors.New("installation metrics unavailable")
	errInvalidInstallationMetrics     = errors.New("invalid installation metrics")
)

type inventoryMetricSample struct {
	Metric map[string]string
	Value  float64
}

type installationResourceKey struct {
	Registry   store.RegistryID
	Kind, Name string
}

type installationCounts struct {
	Counts          map[installationResourceKey]int64
	KnownRegistries map[store.RegistryID]bool
	ValidUntil      time.Time
}

func installationMetricsQuery(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	selectors := make([]string, 0, len(keys))
	for _, key := range keys {
		selectors = append(selectors, key+"="+strconv.Quote(labels[key]))
	}
	selector := "{" + strings.Join(selectors, ",") + "}[30d]"
	return `label_replace(max by (installation_id, config_fingerprint) (last_over_time(agent_marketplace_config_snapshot` + selector + `)), "inventory_type", "snapshot", "", "") or label_replace(max by (installation_id, config_fingerprint, registry_id, resource_kind, resource_name) (last_over_time(agent_marketplace_config_resource` + selector + `)), "inventory_type", "resource", "", "")`
}

func aggregateInstallationMetrics(samples []inventoryMetricSample, registries map[store.RegistryID]store.Registry, now time.Time) (installationCounts, error) {
	type generation struct {
		fingerprint string
		observedAt  int64
		ambiguous   bool
	}
	latest := make(map[string]generation)
	for _, sample := range samples {
		installation := sample.Metric["installation_id"]
		fingerprint := sample.Metric["config_fingerprint"]
		if installation == "" || fingerprint == "" || math.IsNaN(sample.Value) || math.IsInf(sample.Value, 0) {
			return installationCounts{}, errInvalidInstallationMetrics
		}
		switch sample.Metric["inventory_type"] {
		case "snapshot":
			if sample.Value <= 0 || sample.Value > 1<<53 || math.Trunc(sample.Value) != sample.Value {
				return installationCounts{}, errInvalidInstallationMetrics
			}
			observedAt := int64(sample.Value)
			previous, exists := latest[installation]
			if !exists || observedAt > previous.observedAt {
				latest[installation] = generation{fingerprint: fingerprint, observedAt: observedAt}
			} else if observedAt == previous.observedAt && fingerprint != previous.fingerprint {
				previous.ambiguous = true
				latest[installation] = previous
			}
		case "resource":
			if sample.Value != 1 || sample.Metric["registry_id"] == "" || sample.Metric["resource_name"] == "" || !IsMarketplaceKind(sample.Metric["resource_kind"]) {
				return installationCounts{}, errInvalidInstallationMetrics
			}
		default:
			return installationCounts{}, errInvalidInstallationMetrics
		}
	}

	counts := installationCounts{Counts: make(map[installationResourceKey]int64), KnownRegistries: make(map[store.RegistryID]bool)}
	cutoff := now.Add(-installationInventoryWindow).UnixMicro()
	fresh := 0
	for installation, selected := range latest {
		if selected.ambiguous || selected.observedAt < cutoff || selected.observedAt > now.UnixMicro() {
			delete(latest, installation)
			continue
		}
		fresh++
		expires := time.UnixMicro(selected.observedAt).Add(installationInventoryWindow)
		if counts.ValidUntil.IsZero() || expires.Before(counts.ValidUntil) {
			counts.ValidUntil = expires
		}
	}
	if fresh == 0 {
		return installationCounts{}, errInstallationMetricsUnavailable
	}

	owners := make(map[string]map[store.RegistryID]struct{})
	for id, registry := range registries {
		for _, origin := range registry.Origins {
			identity, _, local, ok := registryid.SanitizeRegistryTelemetry(origin)
			if !ok || local {
				continue
			}
			if owners[identity] == nil {
				owners[identity] = make(map[store.RegistryID]struct{})
			}
			owners[identity][id] = struct{}{}
			counts.KnownRegistries[id] = true
		}
	}
	for _, identities := range owners {
		if len(identities) > 1 {
			for id := range identities {
				counts.KnownRegistries[id] = false
			}
		}
	}
	identityIndex := make(map[string]store.RegistryID)
	for identity, identities := range owners {
		if len(identities) == 1 {
			for id := range identities {
				if counts.KnownRegistries[id] {
					identityIndex[identity] = id
				}
			}
		}
	}

	type observation struct {
		installation string
		resource     installationResourceKey
	}
	seen := make(map[observation]struct{})
	for _, sample := range samples {
		if sample.Metric["inventory_type"] != "resource" {
			continue
		}
		installation := sample.Metric["installation_id"]
		selected, fresh := latest[installation]
		registry, known := identityIndex[sample.Metric["registry_id"]]
		if !fresh || selected.fingerprint != sample.Metric["config_fingerprint"] || !known {
			continue
		}
		key := installationResourceKey{Registry: registry, Kind: sample.Metric["resource_kind"], Name: sample.Metric["resource_name"]}
		observed := observation{installation: installation, resource: key}
		if _, duplicate := seen[observed]; !duplicate {
			seen[observed] = struct{}{}
			counts.Counts[key]++
		}
	}
	return counts, nil
}
