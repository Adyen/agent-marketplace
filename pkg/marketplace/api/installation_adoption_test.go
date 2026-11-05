package api

import (
	"strings"
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

// installationMetricsQuery is the single-expression union of every operand, used to evaluate the whole aggregate in one engine run.
func installationMetricsQuery(labels map[string]string, plan installationQueryPlan) string {
	return strings.Join(installationMetricsQueries(labels, plan), " or ")
}

var adoptionRefreshRequests = int32(len(installationMetricsQueries(map[string]string{"namespace": "ops"}, newInstallationQueryPlan(adoptionTestRegistries(), "mp"))))

func adoptionKey(registry store.RegistryID, kind, name string) installationResourceKey {
	return installationResourceKey{Registry: registry, Kind: kind, Name: name}
}

func aggregateResponseSample(inventoryType string, value float64, labels map[string]string) inventoryMetricSample {
	metric := make(map[string]string, len(labels)+1)
	for key, labelValue := range labels {
		metric[key] = labelValue
	}
	if inventoryType == "count" {
		if _, exists := metric["value"]; !exists {
			metric["value"] = "1"
		}
	}
	metric["inventory_type"] = inventoryType
	return inventoryMetricSample{Metric: metric, Value: value}
}
