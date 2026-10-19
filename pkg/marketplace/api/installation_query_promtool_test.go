package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"gopkg.in/yaml.v3"
)

const (
	promtoolEvalTime = "40d"
	promtoolNow      = "3455999000000"
)

type promtoolSeries struct {
	Series string `yaml:"series"`
	Values string `yaml:"values"`
}

type promtoolExpectedSample struct {
	Labels string  `yaml:"labels"`
	Value  float64 `yaml:"value"`
}

type promtoolExpressionTest struct {
	Expr       string                   `yaml:"expr"`
	EvalTime   string                   `yaml:"eval_time"`
	ExpSamples []promtoolExpectedSample `yaml:"exp_samples"`
}

type promtoolTestCase struct {
	Interval       string                   `yaml:"interval"`
	InputSeries    []promtoolSeries         `yaml:"input_series"`
	PromQLExprTest []promtoolExpressionTest `yaml:"promql_expr_test"`
}

type promtoolTestFile struct {
	RuleFiles []string           `yaml:"rule_files"`
	Tests     []promtoolTestCase `yaml:"tests"`
}

func snapshotSeries(installation, fingerprint, value string) promtoolSeries {
	return promtoolSeries{
		Series: fmt.Sprintf(`agent_marketplace_config_snapshot{namespace="ops",installation_id=%q,config_fingerprint=%q}`, installation, fingerprint),
		Values: value + "+0x40",
	}
}

func snapshotSeriesWithPod(installation, fingerprint, value, pod string) promtoolSeries {
	return promtoolSeries{
		Series: fmt.Sprintf(`agent_marketplace_config_snapshot{namespace="ops",installation_id=%q,config_fingerprint=%q,pod=%q}`, installation, fingerprint, pod),
		Values: value + "+0x40",
	}
}

func resourceSeries(installation, fingerprint, registry, kind, name string, labels map[string]string, value string) promtoolSeries {
	seriesLabels := map[string]string{
		"namespace":          "ops",
		"installation_id":    installation,
		"config_fingerprint": fingerprint,
		"registry_id":        registry,
		"resource_kind":      kind,
		"resource_name":      name,
	}
	for key, labelValue := range labels {
		seriesLabels[key] = labelValue
	}
	keys := make([]string, 0, len(seriesLabels))
	for key := range seriesLabels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+strconv.Quote(seriesLabels[key]))
	}
	return promtoolSeries{
		Series: "agent_marketplace_config_resource{" + strings.Join(parts, ",") + "}",
		Values: value + "+0x40",
	}
}

func promtoolFreshSamples(count int, oldest string) []promtoolExpectedSample {
	return []promtoolExpectedSample{
		{Labels: `{inventory_type="fresh"}`, Value: float64(count)},
		{Labels: `{inventory_type="valid_until"}`, Value: mustPromtoolFloat(oldest)},
	}
}

func mustPromtoolFloat(value string) float64 {
	result, err := strconv.ParseFloat(value, 64)
	if err != nil {
		panic(err)
	}
	return result
}

func invalidMarkerSamples() []promtoolExpectedSample {
	samples := []promtoolExpectedSample{
		{Labels: `{inventory_type="invalid"}`, Value: 1},
	}
	return samples
}

func malformedObservationCase(name, fingerprint, sourceKind, marketplaceID, registry, kind, resource, value string) promtoolTestCase {
	labels := map[string]string{}
	if sourceKind != "" {
		labels["source_kind"] = sourceKind
	}
	if marketplaceID != "" {
		labels["marketplace_id"] = marketplaceID
	}
	series := []promtoolSeries{
		snapshotSeries(name, fingerprint, promtoolNow),
		resourceSeries(name, fingerprint, registry, kind, resource, labels, value),
	}
	expected := append(promtoolFreshSamples(1, promtoolNow), invalidMarkerSamples()...)
	if sourceKind == "" && marketplaceID == "" && registry == telemetryAlpha {
		countLabels := `{am_registry="rA",inventory_type="count",resource_kind="` + kind + `"`
		if resource != "" {
			countLabels += `,resource_name="` + resource + `"`
		}
		countLabels += `,value="` + value + `"}`
		countSample := promtoolExpectedSample{Labels: countLabels, Value: 1}
		if value == "1" {
			expected = append([]promtoolExpectedSample{countSample}, expected...)
		} else {
			expected = append(promtoolFreshSamples(1, promtoolNow), countSample)
		}
	}
	return promtoolTestCase{
		Interval:    "1d",
		InputSeries: series,
		PromQLExprTest: []promtoolExpressionTest{{
			EvalTime:   promtoolEvalTime,
			ExpSamples: expected,
		}},
	}
}

func malformedSnapshotCase(value string, fresh bool) promtoolTestCase {
	expected := invalidMarkerSamples()
	if fresh {
		expected = append(promtoolFreshSamples(1, value), expected...)
	}
	return promtoolTestCase{
		Interval:    "1d",
		InputSeries: []promtoolSeries{snapshotSeries("bad-snapshot", "g", value)},
		PromQLExprTest: []promtoolExpressionTest{{
			EvalTime:   promtoolEvalTime,
			ExpSamples: expected,
		}},
	}
}

func missingIdentityCase(series, value string, freshCount int) promtoolTestCase {
	return promtoolTestCase{
		Interval: "1d",
		InputSeries: []promtoolSeries{
			snapshotSeries("control", "g", promtoolNow),
			{Series: series, Values: value + "+0x40"},
		},
		PromQLExprTest: []promtoolExpressionTest{{
			EvalTime:   promtoolEvalTime,
			ExpSamples: append(promtoolFreshSamples(freshCount, promtoolNow), invalidMarkerSamples()...),
		}},
	}
}

func suspiciousHostedIdentityCase() promtoolTestCase {
	return promtoolTestCase{
		Interval: "1d",
		InputSeries: []promtoolSeries{
			snapshotSeries("bad-hosted-identity", "g", promtoolNow),
			resourceSeries("bad-hosted-identity", "g", "rA", "skill", "../bad", map[string]string{"source_kind": "marketplace", "marketplace_id": "mp"}, "1"),
		},
		PromQLExprTest: []promtoolExpressionTest{{
			EvalTime: promtoolEvalTime,
			ExpSamples: append(promtoolFreshSamples(1, promtoolNow),
				promtoolExpectedSample{Labels: `{am_registry="rA",inventory_type="count",resource_kind="skill",resource_name="../bad",value="1"}`, Value: 1},
				promtoolExpectedSample{Labels: `{inventory_type="suspicious",marketplace_id="mp",registry_id="rA",resource_kind="skill",resource_name="../bad",source_kind="marketplace"}`, Value: 1},
			),
		}},
	}
}

func supersededHostedIdentityCase() promtoolTestCase {
	return promtoolTestCase{
		Interval: "1d",
		InputSeries: []promtoolSeries{
			snapshotSeries("superseded-hosted", "old", "3455991800000"),
			resourceSeries("superseded-hosted", "old", "rA", "skill", "../old", map[string]string{"source_kind": "marketplace", "marketplace_id": "mp"}, "1"),
			resourceSeries("superseded-hosted", "old", "../old-registry", "skill", "old-registry", map[string]string{"source_kind": "marketplace", "marketplace_id": "mp"}, "1"),
			resourceSeries("superseded-hosted", "old", "rA", "skill", "old-marketplace", map[string]string{"source_kind": "marketplace", "marketplace_id": "../old-marketplace"}, "1"),
			snapshotSeries("superseded-hosted", "new", promtoolNow),
			resourceSeries("superseded-hosted", "new", "rA", "skill", "current", map[string]string{"source_kind": "marketplace", "marketplace_id": "mp"}, "1"),
		},
		PromQLExprTest: []promtoolExpressionTest{{
			EvalTime: promtoolEvalTime,
			ExpSamples: append(promtoolFreshSamples(1, promtoolNow),
				promtoolExpectedSample{Labels: `{am_registry="rA",inventory_type="count",resource_kind="skill",resource_name="current",value="1"}`, Value: 1},
			),
		}},
	}
}

// spec: marketplace-api/latest-inventory-excludes-removed-and-stale-resources
// spec: marketplace-api/registry-identity-matching-does-not-conflate-resources
// spec: marketplace-api/rule-observations-preserve-selectable-resource-adoption
// spec: marketplace-api/superseded-inventory-history-does-not-exhaust-the-response-budget
// spec: marketplace-api/hosted-adoption-is-isolated-and-deduplicated
// spec: marketplace-api/adoption-growth-does-not-exhaust-the-response-budget
// spec: marketplace-api/malformed-observations-retain-the-fallback-under-aggregation
func TestInstallationMetricsQueryPromtool(t *testing.T) {
	image := os.Getenv("AM_PROMTOOL_IMAGE")
	if image == "" {
		t.Skip("AM_PROMTOOL_IMAGE is not set")
	}

	labels := map[string]string{"namespace": "ops"}
	plan := promtoolQueryPlan()
	query := installationMetricsQuery(labels, plan)
	cases := []promtoolTestCase{
		{
			Interval: "1d",
			InputSeries: []promtoolSeries{
				snapshotSeries("i1", "old", "3455991800000"),
				snapshotSeriesWithPod("i1", "new", "3455995400000", "one"),
				snapshotSeriesWithPod("i1", "new", "3455995400000", "two"),
				resourceSeries("i1", "old", telemetryAlpha, "skill", "removed", nil, "1"),
				resourceSeries("i1", "new", telemetryAlpha, "skill", "shared", nil, "1"),
				resourceSeries("i1", "new", telemetryOldAlpha, "skill", "shared", nil, "1"),
				snapshotSeries("i2", "hosted", "3455991800000"),
				resourceSeries("i2", "hosted", telemetryAlpha, "skill", "shared", nil, "1"),
				resourceSeries("i2", "hosted", "rA", "skill", "shared", map[string]string{"source_kind": "marketplace", "marketplace_id": "mp"}, "1"),
				resourceSeries("i2", "hosted", "rB", "skill", "shared", map[string]string{"source_kind": "marketplace", "marketplace_id": "mp"}, "1"),
				snapshotSeries("i3", "empty", "3455988200000"),
				snapshotSeries("boundary", "edge", "864000000000"),
				resourceSeries("boundary", "edge", telemetryAlpha, "skill", "boundary", nil, "1"),
				snapshotSeries("tied", "a", "3455999000000"),
				snapshotSeries("tied", "b", "3455999000000"),
				resourceSeries("tied", "a", telemetryAlpha, "skill", "ambiguous-a", nil, "1"),
				resourceSeries("tied", "b", telemetryAlpha, "skill", "ambiguous-b", nil, "1"),
				snapshotSeries("stale", "old", "814600000000"),
				resourceSeries("stale", "old", telemetryAlpha, "skill", "stale", nil, "1"),
				snapshotSeries("future", "ahead", "3456001000000"),
				resourceSeries("future", "ahead", telemetryAlpha, "skill", "future", nil, "1"),
				snapshotSeries("noncontributing-value", "bad-value", promtoolNow),
				resourceSeries("noncontributing-value", "bad-value", "rA", "skill", "foreign-value", map[string]string{"source_kind": "marketplace", "marketplace_id": "other"}, "2"),
			},
			PromQLExprTest: []promtoolExpressionTest{{
				EvalTime: promtoolEvalTime,
				ExpSamples: append(promtoolFreshSamples(5, "864000000000"),
					promtoolExpectedSample{Labels: `{am_registry="rA",inventory_type="count",resource_kind="skill",resource_name="shared",value="1"}`, Value: 2},
					promtoolExpectedSample{Labels: `{am_registry="rB",inventory_type="count",resource_kind="skill",resource_name="shared",value="1"}`, Value: 1},
					promtoolExpectedSample{Labels: `{am_registry="rA",inventory_type="count",resource_kind="skill",resource_name="boundary",value="1"}`, Value: 1},
				),
			}},
		},
		{
			Interval: "1d",
			InputSeries: []promtoolSeries{
				snapshotSeries("ambiguous", "g", promtoolNow),
				resourceSeries("ambiguous", "g", telemetryShared, "skill", "ambiguous", nil, "1"),
				snapshotSeries("local", "l", promtoolNow),
				resourceSeries("local", "l", telemetryLocal, "skill", "local", nil, "1"),
				snapshotSeries("unknown", "u", promtoolNow),
				resourceSeries("unknown", "u", "unknown.test/team/pkg", "skill", "unknown", nil, "1"),
				snapshotSeries("foreign", "f", promtoolNow),
				resourceSeries("foreign", "f", "rA", "skill", "foreign", map[string]string{"source_kind": "marketplace", "marketplace_id": "other"}, "1"),
				snapshotSeries("hosted-unknown", "hu", promtoolNow),
				resourceSeries("hosted-unknown", "hu", "rX", "skill", "hosted-unknown", map[string]string{"source_kind": "marketplace", "marketplace_id": "mp"}, "1"),
				snapshotSeries("hosted-known", "hk", promtoolNow),
				resourceSeries("hosted-known", "hk", "rA", "skill", "hosted-known", map[string]string{"source_kind": "marketplace", "marketplace_id": "mp"}, "1"),
				snapshotSeries("rule", "r", promtoolNow),
				resourceSeries("rule", "r", telemetryAlpha, "rule", "policy", nil, "1"),
			},
			PromQLExprTest: []promtoolExpressionTest{{
				EvalTime:   promtoolEvalTime,
				ExpSamples: append(promtoolFreshSamples(7, promtoolNow), promtoolExpectedSample{Labels: `{am_registry="rA",inventory_type="count",resource_kind="skill",resource_name="hosted-known",value="1"}`, Value: 1}),
			}},
		},
		{
			Interval: "1d",
			InputSeries: func() []promtoolSeries {
				var series []promtoolSeries
				for i := 0; i < 50; i++ {
					installation := fmt.Sprintf("growth-%02d", i)
					fingerprint := fmt.Sprintf("f-%02d", i)
					series = append(series,
						snapshotSeries(installation, fingerprint, promtoolNow),
						resourceSeries(installation, fingerprint, telemetryAlpha, "skill", "growth", nil, "1"),
					)
				}
				return series
			}(),
			PromQLExprTest: []promtoolExpressionTest{{
				EvalTime: promtoolEvalTime,
				ExpSamples: append(promtoolFreshSamples(50, promtoolNow),
					promtoolExpectedSample{Labels: `{am_registry="rA",inventory_type="count",resource_kind="skill",resource_name="growth",value="1"}`, Value: 50},
				),
			}},
		},
		{
			Interval: "1d",
			InputSeries: []promtoolSeries{
				snapshotSeries("stale-only", "s", "814600000000"),
				resourceSeries("stale-only", "s", telemetryAlpha, "skill", "stale", nil, "1"),
				snapshotSeries("future-only", "f", "3456001000000"),
				resourceSeries("future-only", "f", telemetryAlpha, "skill", "future", nil, "1"),
			},
			PromQLExprTest: []promtoolExpressionTest{{EvalTime: promtoolEvalTime}},
		},
		malformedObservationCase("bad-source", "g", "future", "", telemetryAlpha, "skill", "bad-source", "1"),
		malformedObservationCase("bad-hosted-pair", "g", "", "mp", "rA", "skill", "bad-hosted-pair", "1"),
		suspiciousHostedIdentityCase(),
		supersededHostedIdentityCase(),
		malformedObservationCase("bad-kind", "g", "", "", telemetryAlpha, "future-kind", "bad-kind", "1"),
		malformedObservationCase("empty-registry", "g", "", "", "", "skill", "empty-registry", "1"),
		malformedObservationCase("empty-resource", "g", "", "", telemetryAlpha, "skill", "", "1"),
		malformedObservationCase("bad-gauge", "g", "", "", telemetryAlpha, "skill", "bad-gauge", "2"),
		malformedSnapshotCase("0", false),
		malformedSnapshotCase("-1", false),
		malformedSnapshotCase("3455999000000.5", true),
		malformedSnapshotCase("NaN", false),
		malformedSnapshotCase("9007199254740994", false),
		missingIdentityCase(`agent_marketplace_config_snapshot{namespace="ops",config_fingerprint="no-installation"}`, promtoolNow, 2),
		missingIdentityCase(`agent_marketplace_config_snapshot{namespace="ops",installation_id="no-fingerprint"}`, promtoolNow, 2),
		missingIdentityCase(`agent_marketplace_config_resource{namespace="ops",config_fingerprint="g",registry_id="example.test/team/alpha",resource_kind="skill",resource_name="no-installation"}`, "1", 1),
		missingIdentityCase(`agent_marketplace_config_resource{namespace="ops",installation_id="control",registry_id="example.test/team/alpha",resource_kind="skill",resource_name="no-fingerprint"}`, "1", 1),
		{
			Interval: "1d",
			InputSeries: []promtoolSeries{
				snapshotSeries("unicode", "g", promtoolNow),
				resourceSeries("unicode", "g", "rA", "skill", "café", map[string]string{"source_kind": "marketplace", "marketplace_id": "mp"}, "1"),
			},
			PromQLExprTest: []promtoolExpressionTest{{
				EvalTime: promtoolEvalTime,
				ExpSamples: append(promtoolFreshSamples(1, promtoolNow),
					promtoolExpectedSample{Labels: `{am_registry="rA",inventory_type="count",resource_kind="skill",resource_name="café",value="1"}`, Value: 1},
					promtoolExpectedSample{Labels: `{inventory_type="suspicious",marketplace_id="mp",registry_id="rA",resource_kind="skill",resource_name="café",source_kind="marketplace"}`, Value: 1},
				),
			}},
		},
	}
	for i := range cases {
		for j := range cases[i].PromQLExprTest {
			cases[i].PromQLExprTest[j].Expr = query
		}
	}
	fixture, err := yaml.Marshal(promtoolTestFile{RuleFiles: []string{}, Tests: cases})
	if err != nil {
		t.Fatalf("marshal promtool fixture: %v", err)
	}
	dir := t.TempDir()
	fixturePath := filepath.Join(dir, "installation_query_test.yaml")
	if err := os.WriteFile(fixturePath, fixture, 0o600); err != nil {
		t.Fatalf("write promtool fixture: %v", err)
	}
	command := exec.Command("docker", "run", "--rm", "-v", dir+":/work", "--entrypoint", "promtool", image, "test", "rules", "/work/installation_query_test.yaml")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("promtool query semantics failed: %v\n%s", err, output)
	}
}

func promtoolQueryPlan() installationQueryPlan {
	return newInstallationQueryPlan(map[store.RegistryID]store.Registry{
		"rA": {ID: "rA", Origins: []string{"https://example.test/team/alpha.git", "https://example.test/team/old-alpha.git"}},
		"rB": {ID: "rB", Origins: []string{"https://example.test/team/beta.git"}},
		"rC": {ID: "rC", Origins: []string{"https://example.test/team/shared.git"}},
		"rD": {ID: "rD", Origins: []string{"https://example.test/team/shared.git"}},
	}, "mp")
}

func TestInstallationQueryPlanAndDeterministicText(t *testing.T) {
	registries := map[store.RegistryID]store.Registry{
		"rA":    {ID: "rA", Origins: []string{"https://example.test/team/alpha.git", "git@example.test:team/alpha.git", "https://example.test/team/old-alpha.git"}},
		"rB":    {ID: "rB", Origins: []string{"https://example.test/team/beta.git"}},
		"rC":    {ID: "rC", Origins: []string{"https://example.test/team/shared.git"}},
		"rD":    {ID: "rD", Origins: []string{"https://example.test/team/shared.git"}},
		"local": {ID: "local", Origins: []string{"local"}},
	}
	plan := newInstallationQueryPlan(registries, "mp")
	if got, want := plan.Hosted, []store.RegistryID{"local", "rA", "rB", "rC", "rD"}; !slices.Equal(got, want) {
		t.Fatalf("hosted registries = %v, want %v", got, want)
	}
	if got, want := plan.Identities, []struct {
		Identity string
		Registry store.RegistryID
	}{
		{Identity: telemetryAlpha, Registry: "rA"},
		{Identity: telemetryBeta, Registry: "rB"},
		{Identity: telemetryOldAlpha, Registry: "rA"},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("registry identities = %#v, want %#v", got, want)
	}
	if plan.KnownRegistries["rA"] != true || plan.KnownRegistries["rB"] != true || plan.KnownRegistries["rC"] || plan.KnownRegistries["rD"] || plan.KnownRegistries["local"] {
		t.Fatalf("known registries = %#v", plan.KnownRegistries)
	}
	reordered := map[store.RegistryID]store.Registry{}
	for _, key := range []store.RegistryID{"local", "rD", "rC", "rB", "rA"} {
		reordered[key] = registries[key]
	}
	query := installationMetricsQuery(map[string]string{"namespace": "ops"}, plan)
	if other := installationMetricsQuery(map[string]string{"namespace": "ops"}, newInstallationQueryPlan(reordered, "mp")); query != other {
		t.Fatalf("query depends on registry map order:\n%s\n%s", query, other)
	}
	for _, expected := range []string{
		`agent_marketplace_config_snapshot{namespace="ops"}[30d]`,
		`"inventory_type", "count"`,
		`"inventory_type", "fresh"`,
		`"inventory_type", "valid_until"`,
		`"inventory_type", "invalid"`,
		`"inventory_type", "suspicious"`,
		`count by (am_registry, resource_kind, resource_name, value) (count_values by (installation_id, am_registry, resource_kind, resource_name)`,
		`"am_registry", "rA"`,
	} {
		if !strings.Contains(query, expected) {
			t.Errorf("query missing %q: %s", expected, query)
		}
	}
	if got := strings.Count(query, `registry_id=~"example`); got != 1 {
		t.Fatalf("Git identity rollup appears %d times in query, want one to preserve the production latency bound", got)
	}
}

// spec: marketplace-api/adoption-growth-does-not-exhaust-the-response-budget
func TestAggregateInstallationMetricsParsesAggregateRows(t *testing.T) {
	now := adoptionFixedNow
	plan := newInstallationQueryPlan(adoptionTestRegistries(), "mp")
	samples := []inventoryMetricSample{
		{Metric: map[string]string{"inventory_type": "count", "am_registry": "rA", "resource_kind": "skill", "resource_name": "shared", "value": "1"}, Value: 2},
		{Metric: map[string]string{"inventory_type": "count", "am_registry": "unknown", "resource_kind": "skill", "resource_name": "ignored", "value": "1"}, Value: 7},
		{Metric: map[string]string{"inventory_type": "fresh"}, Value: 3},
		{Metric: map[string]string{"inventory_type": "valid_until"}, Value: float64(now.Add(-time.Hour).UnixMicro())},
	}
	counts, err := aggregateInstallationMetrics(samples, plan, now)
	if err != nil {
		t.Fatalf("aggregateInstallationMetrics() error = %v, want nil", err)
	}
	if got, want := counts.Counts[installationResourceKey{Registry: "rA", Kind: "skill", Name: "shared"}], int64(2); got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	if _, exists := counts.Counts[installationResourceKey{Registry: "unknown", Kind: "skill", Name: "ignored"}]; exists {
		t.Fatalf("unknown registry count was retained: %#v", counts.Counts)
	}
	if !counts.KnownRegistries["rA"] {
		t.Fatalf("counted registry was not marked known: %#v", counts.KnownRegistries)
	}
	if want := now.Add(installationInventoryWindow - time.Hour); !counts.ValidUntil.Equal(want) {
		t.Fatalf("valid until = %v, want %v", counts.ValidUntil, want)
	}
}

// spec: marketplace-api/malformed-observations-retain-the-fallback-under-aggregation
func TestAggregateInstallationMetricsRejectsMalformedAggregateRows(t *testing.T) {
	now := adoptionFixedNow
	plan := newInstallationQueryPlan(adoptionTestRegistries(), "mp")
	valid := []inventoryMetricSample{
		{Metric: map[string]string{"inventory_type": "fresh"}, Value: 1},
		{Metric: map[string]string{"inventory_type": "valid_until"}, Value: float64(now.UnixMicro())},
	}
	for _, test := range []struct {
		name    string
		entries []inventoryMetricSample
		want    error
	}{
		{name: "invalid marker", entries: []inventoryMetricSample{{Metric: map[string]string{"inventory_type": "invalid"}, Value: 1}}, want: errInvalidInstallationMetrics},
		{name: "unknown type", entries: []inventoryMetricSample{{Metric: map[string]string{"inventory_type": "snapshot"}, Value: 1}}, want: errInvalidInstallationMetrics},
		{name: "empty coordinate", entries: []inventoryMetricSample{{Metric: map[string]string{"inventory_type": "count", "am_registry": "rA", "resource_kind": "skill", "value": "1"}, Value: 1}}, want: errInvalidInstallationMetrics},
		{name: "zero count", entries: []inventoryMetricSample{{Metric: map[string]string{"inventory_type": "count", "am_registry": "rA", "resource_kind": "skill", "resource_name": "x", "value": "1"}, Value: 0}}, want: errInvalidInstallationMetrics},
		{name: "fractional count", entries: []inventoryMetricSample{{Metric: map[string]string{"inventory_type": "count", "am_registry": "rA", "resource_kind": "skill", "resource_name": "x", "value": "1"}, Value: 1.5}}, want: errInvalidInstallationMetrics},
		{name: "oversized count", entries: []inventoryMetricSample{{Metric: map[string]string{"inventory_type": "count", "am_registry": "rA", "resource_kind": "skill", "resource_name": "x", "value": "1"}, Value: 1<<53 + 2}}, want: errInvalidInstallationMetrics},
		{name: "missing value label", entries: []inventoryMetricSample{{Metric: map[string]string{"inventory_type": "count", "am_registry": "rA", "resource_kind": "skill", "resource_name": "x"}, Value: 1}}, want: errInvalidInstallationMetrics},
		{name: "non-one value label", entries: []inventoryMetricSample{{Metric: map[string]string{"inventory_type": "count", "am_registry": "rA", "resource_kind": "skill", "resource_name": "x", "value": "2"}, Value: 1}}, want: errInvalidInstallationMetrics},
		{name: "no fresh", entries: []inventoryMetricSample{{Metric: map[string]string{"inventory_type": "count", "am_registry": "rA", "resource_kind": "skill", "resource_name": "x", "value": "1"}, Value: 1}}, want: errInstallationMetricsUnavailable},
		{name: "duplicate fresh", entries: []inventoryMetricSample{{Metric: map[string]string{"inventory_type": "fresh"}, Value: 1}}, want: errInvalidInstallationMetrics},
		{name: "missing valid until", entries: []inventoryMetricSample{{Metric: map[string]string{"inventory_type": "fresh"}, Value: 1}}, want: errInvalidInstallationMetrics},
		{name: "duplicate valid until", entries: []inventoryMetricSample{{Metric: map[string]string{"inventory_type": "valid_until"}, Value: float64(now.UnixMicro())}}, want: errInvalidInstallationMetrics},
		{name: "future valid until", entries: []inventoryMetricSample{{Metric: map[string]string{"inventory_type": "valid_until"}, Value: float64(now.Add(time.Microsecond).UnixMicro())}}, want: errInvalidInstallationMetrics},
		{name: "nonpositive fresh", entries: []inventoryMetricSample{{Metric: map[string]string{"inventory_type": "fresh"}, Value: 0}}, want: errInvalidInstallationMetrics},
	} {
		t.Run(test.name, func(t *testing.T) {
			entries := append([]inventoryMetricSample{}, valid...)
			switch test.name {
			case "no fresh":
				entries = entries[1:]
			case "duplicate fresh":
				entries = append(entries, test.entries...)
			case "missing valid until":
				entries = entries[:1]
			case "duplicate valid until":
				entries = append(entries, test.entries...)
			case "future valid until":
				entries[1] = test.entries[0]
			case "nonpositive fresh":
				entries[0] = test.entries[0]
			default:
				entries = append(entries, test.entries...)
			}
			if _, err := aggregateInstallationMetrics(entries, plan, now); !errors.Is(err, test.want) {
				t.Fatalf("aggregateInstallationMetrics() error = %v, want %v", err, test.want)
			}
		})
	}
}

// spec: marketplace-api/malformed-observations-retain-the-fallback-under-aggregation
func TestAggregateInstallationMetricsValidatesSuspiciousRows(t *testing.T) {
	now := adoptionFixedNow
	plan := newInstallationQueryPlan(adoptionTestRegistries(), "mp")
	samples := []inventoryMetricSample{
		{Metric: map[string]string{"inventory_type": "fresh"}, Value: 1},
		{Metric: map[string]string{"inventory_type": "valid_until"}, Value: float64(now.UnixMicro())},
		{Metric: map[string]string{"inventory_type": "suspicious", "source_kind": "marketplace", "marketplace_id": "mp", "registry_id": "rA", "resource_kind": "skill", "resource_name": "café"}, Value: 1},
	}
	if _, err := aggregateInstallationMetrics(samples, plan, now); err != nil {
		t.Fatalf("well-formed suspicious superset row error = %v, want nil", err)
	}
	samples[2].Metric["resource_name"] = "../bad"
	if _, err := aggregateInstallationMetrics(samples, plan, now); !errors.Is(err, errInvalidInstallationMetrics) {
		t.Fatalf("invalid suspicious row error = %v, want %v", err, errInvalidInstallationMetrics)
	}
}

// spec: marketplace-api/registry-identity-and-inventory-expiry-prevent-unsafe-counts
func TestInstallationMetricsFetchAcceptsWholeSecondFreshnessCutoff(t *testing.T) {
	evalTime := adoptionFixedNow.Add(500 * time.Millisecond)
	evalSecond := time.Unix(evalTime.Unix(), 0)
	cutoff := evalSecond.Add(-installationInventoryWindow)
	samples := []inventoryMetricSample{
		aggregateResponseSample("fresh", 1, nil),
		aggregateResponseSample("valid_until", float64(cutoff.UnixMicro()), nil),
	}
	fixture := newMetricsFixture(t, func() string { return prometheusVector(samples, evalTime) })
	reader := newTestReader(t, fixture)

	counts, err := reader.fetch(context.Background(), adoptionTestRegistries(), evalTime)
	if err != nil {
		t.Fatalf("fetch() at the whole-second freshness cutoff error = %v, want nil", err)
	}
	if !counts.ValidUntil.Equal(evalSecond) {
		t.Fatalf("valid until = %v, want %v", counts.ValidUntil, evalSecond)
	}
	if request := <-fixture.requests; request.Form.Get("time") != fmt.Sprintf("%d", evalTime.Unix()) {
		t.Fatalf("evaluation time = %q, want %d", request.Form.Get("time"), evalTime.Unix())
	}
}
