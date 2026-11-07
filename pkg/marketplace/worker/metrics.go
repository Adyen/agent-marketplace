package worker

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

var (
	observationOutcomes = []string{"changed", "unchanged", "failed"}
	refreshTriggers     = []string{"source-change", "config-change", "reconciliation", "manual", "unknown"}
	refreshOutcomes     = []string{"succeeded", "failed", "superseded"}
	budgetOperations    = []string{"observation", "refresh"}
	candidateOutcomes   = []string{"applied", "rejected", "apply_failed"}
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
	}
}

func (m *Metrics) RecordObservation(id store.RegistryID, outcome string, duration time.Duration, lastSuccessfulAt time.Time, failures int) {
	if m == nil {
		return
	}
	if containsValue(observationOutcomes, outcome) {
		m.sourceObservations.WithLabelValues(outcome).Inc()
	}
	m.sourceObservationDuration.Observe(duration.Seconds())
	m.lastSuccessfulIndex.WithLabelValues(string(id))
	if outcome != "failed" && !lastSuccessfulAt.IsZero() {
		m.lastSuccessfulObservation.WithLabelValues(string(id)).Set(float64(lastSuccessfulAt.UnixNano()) / float64(time.Second))
	}
	m.consecutiveProbeFailures.WithLabelValues(string(id)).Set(float64(failures))
}

func (m *Metrics) RecordSuccessfulIndex(id store.RegistryID, indexedAt time.Time) {
	if m == nil || indexedAt.IsZero() {
		return
	}
	m.lastSuccessfulIndex.WithLabelValues(string(id)).Set(float64(indexedAt.UnixNano()) / float64(time.Second))
}

func (m *Metrics) RecordRefresh(trigger, outcome string, duration time.Duration) {
	if m == nil {
		return
	}
	trigger = knownRefreshTrigger(trigger)
	if !containsValue(refreshOutcomes, outcome) {
		return
	}
	m.registryRefreshes.WithLabelValues(trigger, outcome).Inc()
	m.registryRefreshDuration.WithLabelValues(outcome).Observe(duration.Seconds())
}

func (m *Metrics) RecordHostBudgetDelay(operation string) {
	if m == nil || !containsValue(budgetOperations, operation) {
		return
	}
	m.hostBudgetDelays.WithLabelValues(operation).Inc()
}

func (m *Metrics) RecordCandidateOutcome(outcome string) {
	if m == nil || !containsValue(candidateOutcomes, outcome) {
		return
	}
	m.configCandidates.WithLabelValues(outcome).Inc()
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
	if containsValue(refreshTriggers, trigger) {
		return trigger
	}
	return "unknown"
}

func containsValue(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
