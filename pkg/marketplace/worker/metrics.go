package worker

import (
	"slices"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

var (
	observationOutcomes    = []string{"changed", "unchanged", "failed"}
	refreshTriggers        = []string{"source-change", "config-change", "reconciliation", "manual", "unknown"}
	refreshOutcomes        = []string{"succeeded", "failed", "superseded"}
	budgetOperations       = []string{"observation", "refresh"}
	candidateOutcomes      = []string{"applied", "rejected", "apply_failed"}
	claudeDeliveryOutcomes = []string{"succeeded", "failed"}
	claudeDeliveryStates   = []string{"published", "retired", "skipped", "conflict", "failed"}
)

type Metrics struct {
	sourceObservations        *prometheus.CounterVec
	sourceObservationDuration prometheus.Histogram
	registryRefreshes         *prometheus.CounterVec
	registryRefreshDuration   *prometheus.HistogramVec
	hostBudgetDelays          *prometheus.CounterVec
	configCandidates          *prometheus.CounterVec
	configRestartRequired     prometheus.Gauge
	lastSuccessfulObservation *prometheus.GaugeVec
	lastSuccessfulIndex       *prometheus.GaugeVec
	consecutiveProbeFailures  *prometheus.GaugeVec
	claudeDeliveryReconciles  *prometheus.CounterVec
	claudeDeliveryPlugins     *prometheus.GaugeVec
	claudePluginsAPIRequests  *prometheus.CounterVec
	claudeDeliveryThrottled   prometheus.Counter
	claudeDeliveryPassTime    prometheus.Histogram
}

func NewMetrics() *Metrics {
	metrics := &Metrics{
		sourceObservations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_marketplace_source_observations_total",
			Help: "Source observations by outcome.",
		}, []string{"outcome"}),
		sourceObservationDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "agent_marketplace_source_observation_duration_seconds",
			Help: "Source observation duration in seconds.",
		}),
		registryRefreshes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_marketplace_registry_refreshes_total",
			Help: "Registry refreshes by trigger and outcome.",
		}, []string{"trigger", "outcome"}),
		registryRefreshDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "agent_marketplace_registry_refresh_duration_seconds",
			Help: "Registry refresh duration in seconds by outcome.",
		}, []string{"outcome"}),
		hostBudgetDelays: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_marketplace_host_budget_delays_total",
			Help: "Worker operations delayed by a host budget.",
		}, []string{"operation"}),
		configCandidates: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_marketplace_config_candidates_total",
			Help: "Runtime configuration candidates by outcome.",
		}, []string{"outcome"}),
		configRestartRequired: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "agent_marketplace_config_restart_required",
			Help: "Whether startup-owned configuration differs from the running process.",
		}),
		lastSuccessfulObservation: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "agent_marketplace_registry_last_successful_observation_timestamp_seconds",
			Help: "Unix timestamp of the last successful source observation.",
		}, []string{"registry"}),
		lastSuccessfulIndex: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "agent_marketplace_registry_last_successful_index_timestamp_seconds",
			Help: "Unix timestamp of the last successful registry index.",
		}, []string{"registry"}),
		consecutiveProbeFailures: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "agent_marketplace_registry_consecutive_probe_failures",
			Help: "Consecutive source probe failures.",
		}, []string{"registry"}),
		claudeDeliveryReconciles: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_marketplace_claude_delivery_reconciles_total",
			Help: "Claude Enterprise delivery reconcile passes by outcome.",
		}, []string{"outcome"}),
		claudeDeliveryPlugins: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "agent_marketplace_claude_delivery_plugins",
			Help: "Claude Enterprise delivery plugin count by state.",
		}, []string{"state"}),
		claudePluginsAPIRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_marketplace_claude_plugins_api_requests_total",
			Help: "Claude Plugins API requests by endpoint and HTTP status.",
		}, []string{"endpoint", "status"}),
		claudeDeliveryThrottled: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "agent_marketplace_claude_plugins_api_throttled_total",
			Help: "Claude Plugins API rate-limited responses.",
		}),
		claudeDeliveryPassTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "agent_marketplace_claude_delivery_pass_duration_seconds",
			Help: "Claude Enterprise delivery reconcile pass duration.",
		}),
	}
	for _, outcome := range observationOutcomes {
		metrics.sourceObservations.WithLabelValues(outcome)
	}
	for _, trigger := range refreshTriggers {
		for _, outcome := range refreshOutcomes {
			metrics.registryRefreshes.WithLabelValues(trigger, outcome)
		}
	}
	for _, outcome := range refreshOutcomes {
		metrics.registryRefreshDuration.WithLabelValues(outcome)
	}
	for _, operation := range budgetOperations {
		metrics.hostBudgetDelays.WithLabelValues(operation)
	}
	for _, outcome := range candidateOutcomes {
		metrics.configCandidates.WithLabelValues(outcome)
	}
	for _, outcome := range claudeDeliveryOutcomes {
		metrics.claudeDeliveryReconciles.WithLabelValues(outcome)
	}
	for _, state := range claudeDeliveryStates {
		metrics.claudeDeliveryPlugins.WithLabelValues(state)
	}
	metrics.configRestartRequired.Set(0)
	return metrics
}

func (m *Metrics) Collectors() []prometheus.Collector {
	return []prometheus.Collector{
		m.sourceObservations,
		m.sourceObservationDuration,
		m.registryRefreshes,
		m.registryRefreshDuration,
		m.hostBudgetDelays,
		m.configCandidates,
		m.configRestartRequired,
		m.lastSuccessfulObservation,
		m.lastSuccessfulIndex,
		m.consecutiveProbeFailures,
		m.claudeDeliveryReconciles,
		m.claudeDeliveryPlugins,
		m.claudePluginsAPIRequests,
		m.claudeDeliveryThrottled,
		m.claudeDeliveryPassTime,
	}
}

func (m *Metrics) RecordObservation(id store.RegistryID, outcome string, duration time.Duration, lastObservedAt, lastIndexedAt time.Time, failures int) {
	if m == nil {
		return
	}
	if slices.Contains(observationOutcomes, outcome) {
		m.sourceObservations.WithLabelValues(outcome).Inc()
	}
	m.sourceObservationDuration.Observe(duration.Seconds())
	m.lastSuccessfulIndex.WithLabelValues(string(id)).Set(unixSeconds(lastIndexedAt))
	if outcome != "failed" && !lastObservedAt.IsZero() {
		m.lastSuccessfulObservation.WithLabelValues(string(id)).Set(unixSeconds(lastObservedAt))
	}
	m.consecutiveProbeFailures.WithLabelValues(string(id)).Set(float64(failures))
}

func (m *Metrics) RecordSuccessfulIndex(id store.RegistryID, indexedAt time.Time) {
	if m == nil || indexedAt.IsZero() {
		return
	}
	m.lastSuccessfulIndex.WithLabelValues(string(id)).Set(unixSeconds(indexedAt))
}

func (m *Metrics) RecordRefresh(trigger, outcome string, duration time.Duration) {
	if m == nil {
		return
	}
	trigger = knownRefreshTrigger(trigger)
	if !slices.Contains(refreshOutcomes, outcome) {
		return
	}
	m.registryRefreshes.WithLabelValues(trigger, outcome).Inc()
	m.registryRefreshDuration.WithLabelValues(outcome).Observe(duration.Seconds())
}

func (m *Metrics) RecordHostBudgetDelay(operation string) {
	if m == nil || !slices.Contains(budgetOperations, operation) {
		return
	}
	m.hostBudgetDelays.WithLabelValues(operation).Inc()
}

func (m *Metrics) RecordCandidateOutcome(outcome string) {
	if m == nil || !slices.Contains(candidateOutcomes, outcome) {
		return
	}
	m.configCandidates.WithLabelValues(outcome).Inc()
}

func (m *Metrics) RecordClaudeDeliveryReconcile(outcome string, duration time.Duration) {
	if m == nil || !slices.Contains(claudeDeliveryOutcomes, outcome) {
		return
	}
	m.claudeDeliveryReconciles.WithLabelValues(outcome).Inc()
	m.claudeDeliveryPassTime.Observe(duration.Seconds())
}

func (m *Metrics) SetClaudeDeliveryPluginStates(counts map[string]int) {
	if m == nil {
		return
	}
	for _, state := range claudeDeliveryStates {
		m.claudeDeliveryPlugins.WithLabelValues(state).Set(float64(counts[state]))
	}
}

func (m *Metrics) RecordClaudeAPIRequest(endpoint string, status int) {
	if m == nil {
		return
	}
	switch endpoint {
	case "plugin_marketplaces", "plugins", "plugin", "plugin_versions", "plugin_version", "installation_settings":
	default:
		endpoint = "other"
	}
	statusCode := "network"
	if status >= 100 && status <= 599 {
		statusCode = strconv.Itoa(status)
	}
	m.claudePluginsAPIRequests.WithLabelValues(endpoint, statusCode).Inc()
}

func (m *Metrics) RecordClaudeDeliveryThrottled() {
	if m != nil {
		m.claudeDeliveryThrottled.Inc()
	}
}

func (m *Metrics) SetRestartRequired(required bool) {
	if m == nil {
		return
	}
	if required {
		m.configRestartRequired.Set(1)
	} else {
		m.configRestartRequired.Set(0)
	}
}

func (m *Metrics) RemoveRegistry(id string) {
	if m == nil {
		return
	}
	m.lastSuccessfulObservation.DeleteLabelValues(id)
	m.lastSuccessfulIndex.DeleteLabelValues(id)
	m.consecutiveProbeFailures.DeleteLabelValues(id)
}

var _ config.RuntimeReporter = (*Metrics)(nil)

func knownRefreshTrigger(trigger string) string {
	if slices.Contains(refreshTriggers, trigger) {
		return trigger
	}
	return "unknown"
}

func unixSeconds(t time.Time) float64 {
	if t.IsZero() {
		return 0
	}
	return float64(t.UnixNano()) / float64(time.Second)
}
