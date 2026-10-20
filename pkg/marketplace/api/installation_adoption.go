package api

import (
	"errors"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Adyen/agent-marketplace/internal/registryid"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

const (
	installationInventoryWindow      = 30 * 24 * time.Hour
	maxHostedInventoryAttributeBytes = 512
	maxExactMetricValue              = 1 << 53
)

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

type installationQueryPlan struct {
	Hosted     []store.RegistryID
	Identities []struct {
		Identity string
		Registry store.RegistryID
	}
	KnownRegistries map[store.RegistryID]bool
	MarketplaceID   store.MarketplaceID
}

func newInstallationQueryPlan(registries map[store.RegistryID]store.Registry, marketplaceID store.MarketplaceID) installationQueryPlan {
	plan := installationQueryPlan{
		Hosted:          make([]store.RegistryID, 0, len(registries)),
		KnownRegistries: make(map[store.RegistryID]bool, len(registries)),
		MarketplaceID:   marketplaceID,
	}
	owners := make(map[string]map[store.RegistryID]struct{})
	for id, registry := range registries {
		plan.Hosted = append(plan.Hosted, id)
		plan.KnownRegistries[id] = false
		for _, origin := range registry.Origins {
			identity, _, local, ok := registryid.SanitizeRegistryTelemetry(origin)
			if !ok || local {
				continue
			}
			if owners[identity] == nil {
				owners[identity] = make(map[store.RegistryID]struct{})
			}
			owners[identity][id] = struct{}{}
			plan.KnownRegistries[id] = true
		}
	}
	sort.Slice(plan.Hosted, func(i, j int) bool { return plan.Hosted[i] < plan.Hosted[j] })
	for identity, ids := range owners {
		if len(ids) > 1 {
			for id := range ids {
				plan.KnownRegistries[id] = false
			}
			continue
		}
		for id := range ids {
			plan.Identities = append(plan.Identities, struct {
				Identity string
				Registry store.RegistryID
			}{Identity: identity, Registry: id})
		}
	}
	sort.Slice(plan.Identities, func(i, j int) bool {
		if plan.Identities[i].Identity != plan.Identities[j].Identity {
			return plan.Identities[i].Identity < plan.Identities[j].Identity
		}
		return plan.Identities[i].Registry < plan.Identities[j].Registry
	})
	return plan
}

func installationMetricsQuery(labels map[string]string, plan installationQueryPlan) string {
	selectors := make([]string, 0, len(labels))
	for key := range labels {
		selectors = append(selectors, key)
	}
	sort.Strings(selectors)
	for i, key := range selectors {
		selectors[i] = key + "=" + strconv.Quote(labels[key])
	}
	baseSelector := strings.Join(selectors, ",")
	snapshotSelector := "{" + baseSelector + "}"
	snapshot := `max by (installation_id, config_fingerprint) (last_over_time(agent_marketplace_config_snapshot` + snapshotSelector + `[30d]))`
	latest := `(` + snapshot + ` == on (installation_id) group_left max by (installation_id) (` + snapshot + `))`
	fresh := `((` + latest + ` unless on (installation_id) (count by (installation_id) (` + latest + `) > 1)) >= (time() - 2592000) * 1e6 <= time() * 1e6)`

	resourceGroups := "installation_id, config_fingerprint, source_kind, marketplace_id, registry_id, resource_kind, resource_name"
	resource := func(extraMatchers ...string) string {
		matchers := append(append([]string{}, selectors...), extraMatchers...)
		selector := "{" + strings.Join(matchers, ",") + "}"
		return `max by (` + resourceGroups + `) (last_over_time(agent_marketplace_config_resource` + selector + `[30d]))`
	}

	var hosted string
	if len(plan.Hosted) > 0 {
		registryIDs := make([]string, len(plan.Hosted))
		for i, id := range plan.Hosted {
			registryIDs[i] = regexp.QuoteMeta(string(id))
		}
		hostedMatchers := append(append([]string{}, selectors...),
			`resource_kind!="rule"`,
			`source_kind="marketplace"`,
			`marketplace_id=`+strconv.Quote(string(plan.MarketplaceID)),
			`registry_id=~`+strconv.Quote(strings.Join(registryIDs, "|")),
		)
		hosted = `label_replace(max by (installation_id, config_fingerprint, registry_id, resource_kind, resource_name) (last_over_time(agent_marketplace_config_resource{` +
			strings.Join(hostedMatchers, ",") + `}[30d])), "am_registry", "$1", "registry_id", "(.*)")`
	}
	var git string
	if len(plan.Identities) > 0 {
		identities := make([]string, len(plan.Identities))
		for i, identity := range plan.Identities {
			identities[i] = regexp.QuoteMeta(identity.Identity)
		}
		gitMatchers := append(append([]string{}, selectors...),
			`resource_kind!="rule"`,
			`source_kind=""`,
			`marketplace_id=""`,
			`registry_id=~`+strconv.Quote(strings.Join(identities, "|")),
		)
		git = `max by (` + resourceGroups + `) (last_over_time(agent_marketplace_config_resource{` + strings.Join(gitMatchers, ",") + `}[30d]))`
		for _, identity := range plan.Identities {
			git = `label_replace(` + git + `, "am_registry", ` + strconv.Quote(string(identity.Registry)) +
				`, "registry_id", ` + strconv.Quote(regexp.QuoteMeta(identity.Identity)) + `)`
		}
	}
	var contributing string
	switch {
	case hosted != "" && git != "":
		contributing = `(` + hosted + ` or ` + git + `)`
	case hosted != "":
		contributing = hosted
	case git != "":
		contributing = git
	}

	var output []string
	if contributing != "" {
		matched := `(` + contributing + ` and on (installation_id, config_fingerprint) ` + fresh + `)`
		counts := `count by (am_registry, resource_kind, resource_name, value) (count_values by (installation_id, am_registry, resource_kind, resource_name) ("value", ` + matched + `))`
		output = append(output, `label_replace(`+counts+`, "inventory_type", "count", "", "")`)
	}
	output = append(output,
		`label_replace(count(`+fresh+`), "inventory_type", "fresh", "", "")`,
		`label_replace(min(`+fresh+`), "inventory_type", "valid_until", "", "")`,
	)

	invalidRows := []string{
		`(` + snapshot + ` != ` + snapshot + `)`,
		`((clamp(` + latest + `, 1, 9007199254740992) - floor(` + latest + `)) != 0)`,
	}
	resourceInvalidMatchers := []string{
		`source_kind!~"|marketplace"`,
		`source_kind="",marketplace_id!=""`,
		`resource_kind!~"skill|command|subagent|hook|mcpserver|collection|rule"`,
		`registry_id=""`,
		`resource_name=""`,
	}
	resourceInvalidRollups := make([]string, len(resourceInvalidMatchers))
	for i, matcher := range resourceInvalidMatchers {
		resourceInvalidRollups[i] = resource(matcher)
	}
	invalidResourceRows := `max by (` + resourceGroups + `) (` + strings.Join(resourceInvalidRollups, ` or `) + `)`
	invalidRows = append(invalidRows, `(`+invalidResourceRows+` and on (installation_id, config_fingerprint) `+latest+`)`)
	for _, name := range []string{"installation_id", "config_fingerprint"} {
		identityMatchers := append(append([]string{}, selectors...), name+`=""`)
		identitySelector := strings.Join(identityMatchers, ",")
		invalidRows = append(invalidRows,
			`last_over_time(agent_marketplace_config_snapshot{`+identitySelector+`}[30d])`,
			`last_over_time(agent_marketplace_config_resource{`+identitySelector+`}[30d])`,
		)
	}
	invalid := `label_replace(count(` + strings.Join(invalidRows, ` or `) + `), "inventory_type", "invalid", "", "")`
	output = append(output, invalid)

	const suspiciousPattern = `|.*[^!-~].*|.{513,}|\.|\.\.|.*[/\\].*`
	suspiciousMatchers := []string{
		`source_kind="marketplace"`,
		`resource_kind!="rule"`,
	}
	suspiciousRows := make([]string, 0, 3)
	for _, name := range []string{"marketplace_id", "registry_id", "resource_name"} {
		matchers := append(append([]string{}, suspiciousMatchers...), name+"=~"+strconv.Quote(suspiciousPattern))
		suspiciousRows = append(suspiciousRows, resource(matchers...))
	}
	suspiciousLatest := `((` + strings.Join(suspiciousRows, ` or `) + `) and on (installation_id, config_fingerprint) ` + latest + `)`
	suspicious := `label_replace(max by (source_kind, marketplace_id, registry_id, resource_kind, resource_name) (` +
		suspiciousLatest + `), "inventory_type", "suspicious", "", "")`
	output = append(output, suspicious)
	return strings.Join(output, ` or `)
}

func aggregateInstallationMetrics(samples []inventoryMetricSample, plan installationQueryPlan, now time.Time) (installationCounts, error) {
	counts := installationCounts{
		Counts:          make(map[installationResourceKey]int64),
		KnownRegistries: make(map[store.RegistryID]bool, len(plan.KnownRegistries)),
	}
	knownIDs := make(map[store.RegistryID]struct{}, len(plan.Hosted))
	for _, id := range plan.Hosted {
		knownIDs[id] = struct{}{}
	}
	for id, known := range plan.KnownRegistries {
		counts.KnownRegistries[id] = known
	}

	cutoff := now.Add(-installationInventoryWindow).UnixMicro()
	freshSeen, validUntilSeen := false, false
	var validUntil int64
	for _, sample := range samples {
		if math.IsNaN(sample.Value) || math.IsInf(sample.Value, 0) {
			return installationCounts{}, errInvalidInstallationMetrics
		}
		switch sample.Metric["inventory_type"] {
		case "count":
			registry := store.RegistryID(sample.Metric["am_registry"])
			kind, name := sample.Metric["resource_kind"], sample.Metric["resource_name"]
			if registry == "" || kind == "" || name == "" || sample.Metric["value"] != "1" || !isExactPositiveMetricValue(sample.Value) {
				return installationCounts{}, errInvalidInstallationMetrics
			}
			if _, known := knownIDs[registry]; !known {
				continue
			}
			key := installationResourceKey{Registry: registry, Kind: kind, Name: name}
			counts.Counts[key] = int64(sample.Value)
			counts.KnownRegistries[registry] = true
		case "fresh":
			if freshSeen || !isExactPositiveMetricValue(sample.Value) {
				return installationCounts{}, errInvalidInstallationMetrics
			}
			freshSeen = true
		case "valid_until":
			if validUntilSeen || math.Trunc(sample.Value) != sample.Value || sample.Value < float64(cutoff) || sample.Value > float64(now.UnixMicro()) {
				return installationCounts{}, errInvalidInstallationMetrics
			}
			validUntilSeen = true
			validUntil = int64(sample.Value)
		case "invalid":
			return installationCounts{}, errInvalidInstallationMetrics
		case "suspicious":
			if !validHostedInventoryIdentity(
				sample.Metric["marketplace_id"],
				sample.Metric["registry_id"],
				sample.Metric["resource_name"],
			) {
				return installationCounts{}, errInvalidInstallationMetrics
			}
		default:
			return installationCounts{}, errInvalidInstallationMetrics
		}
	}
	if !freshSeen {
		return installationCounts{}, errInstallationMetricsUnavailable
	}
	if !validUntilSeen {
		return installationCounts{}, errInvalidInstallationMetrics
	}
	counts.ValidUntil = time.UnixMicro(validUntil).Add(installationInventoryWindow)
	return counts, nil
}

func isExactPositiveMetricValue(value float64) bool {
	return value > 0 && value <= maxExactMetricValue && math.Trunc(value) == value
}

func validHostedInventoryIdentity(marketplaceID, registryID, resourceName string) bool {
	for _, value := range []string{marketplaceID, registryID, resourceName} {
		if value == "" || !utf8.ValidString(value) || len(value) > maxHostedInventoryAttributeBytes || nameutil.ValidateSegment(value) != nil {
			return false
		}
	}
	return true
}
