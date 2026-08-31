package receiver

import (
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"

	mpb "go.opentelemetry.io/proto/otlp/metrics/v1"
)

var (
	installationIDPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	commandNamePattern    = regexp.MustCompile(`^(?:agent-marketplace(?: [a-z][a-z0-9-]*)+|[a-z][a-z0-9-]*)$`)
	versionPattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)
	platformPattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9_]{0,15}$`)
	knownHarnesses        = map[string]struct{}{"factory": {}, "claude": {}, "cursor": {}, "windsurf": {}, "gemini": {}, "devin": {}, "codex": {}}
	resourceAttributes    = map[string]struct{}{
		string(tmcfg.ResourceAttrSchemaVersion): {}, string(tmcfg.ResourceAttrInstallationID): {}, string(tmcfg.ResourceAttrCLIVersion): {},
		string(tmcfg.ResourceAttrOSName): {}, string(tmcfg.ResourceAttrArch): {},
	}
	metricAttributes = map[string]struct{}{
		string(tmcfg.ResourceAttrCommandName): {}, string(tmcfg.ResourceAttrHarnesses): {},
		string(tmcfg.ResourceAttrManaged): {}, string(tmcfg.ResourceAttrOutcomeCode): {},
	}
)

var activeInstallationWindows = []struct {
	name     string
	duration time.Duration
}{
	{name: "1d", duration: 24 * time.Hour},
	{name: "7d", duration: 7 * 24 * time.Hour},
	{name: "30d", duration: 30 * 24 * time.Hour},
}

type resourceLabels struct {
	cliVersion string
	osName     string
	arch       string
}

type invocationLabels struct {
	arch        string
	cliVersion  string
	commandName string
	managed     string
	osName      string
	outcomeCode string
}

type durationLabels struct {
	commandName string
	outcomeCode string
}

type requestLabels struct {
	reason string
	result string
}

type durationPoint struct {
	labels  durationLabels
	buckets []uint64
	count   uint64
	sum     float64
}

type aggregatedHistogram struct {
	buckets []uint64
	count   uint64
	sum     float64
}

func validateResourceLabels(labels resourceLabels) error {
	if !versionPattern.MatchString(labels.cliVersion) {
		return fmt.Errorf("invalid cli.version")
	}
	if !platformPattern.MatchString(labels.osName) {
		return fmt.Errorf("invalid os.name")
	}
	if !platformPattern.MatchString(labels.arch) {
		return fmt.Errorf("invalid arch")
	}
	return nil
}

func validateMetricAttributes(attrs map[string]string) error {
	commandName := attrs[string(tmcfg.ResourceAttrCommandName)]
	if len(commandName) > 128 || !commandNamePattern.MatchString(commandName) {
		return fmt.Errorf("invalid command.name")
	}
	if managed := attrs[string(tmcfg.ResourceAttrManaged)]; managed != "true" && managed != "false" {
		return fmt.Errorf("invalid managed attribute")
	}
	if outcome := attrs[string(tmcfg.ResourceAttrOutcomeCode)]; outcome != "success" && outcome != "error" {
		return fmt.Errorf("invalid outcome.code")
	}
	harnesses := make(map[string]struct{})
	for _, harness := range strings.Split(attrs[string(tmcfg.ResourceAttrHarnesses)], ",") {
		harness = strings.TrimSpace(harness)
		if harness == "" {
			continue
		}
		if _, ok := knownHarnesses[harness]; !ok {
			return fmt.Errorf("invalid harness %q", harness)
		}
		if _, duplicate := harnesses[harness]; duplicate {
			return fmt.Errorf("duplicate harness %q", harness)
		}
		harnesses[harness] = struct{}{}
	}
	return nil
}

func newInvocationLabels(resource resourceLabels, attrs map[string]string) invocationLabels {
	return invocationLabels{
		arch:        resource.arch,
		cliVersion:  resource.cliVersion,
		commandName: attrs[string(tmcfg.ResourceAttrCommandName)],
		managed:     attrs[string(tmcfg.ResourceAttrManaged)],
		osName:      resource.osName,
		outcomeCode: attrs[string(tmcfg.ResourceAttrOutcomeCode)],
	}
}

func newDurationPoint(dp *mpb.HistogramDataPoint) (durationPoint, error) {
	if dp == nil {
		return durationPoint{}, fmt.Errorf("duration histogram data point is nil")
	}
	if !equalBounds(dp.GetExplicitBounds(), tmcfg.CommandDurationBucketBoundaries) {
		return durationPoint{}, fmt.Errorf("duration histogram has unexpected bucket boundaries")
	}
	if len(dp.GetBucketCounts()) != len(tmcfg.CommandDurationBucketBoundaries)+1 {
		return durationPoint{}, fmt.Errorf("duration histogram has %d bucket counts, expected %d", len(dp.GetBucketCounts()), len(tmcfg.CommandDurationBucketBoundaries)+1)
	}
	var bucketCount uint64
	for _, count := range dp.GetBucketCounts() {
		if math.MaxUint64-bucketCount < count {
			return durationPoint{}, fmt.Errorf("duration histogram bucket count overflow")
		}
		bucketCount += count
	}
	if bucketCount != dp.GetCount() {
		return durationPoint{}, fmt.Errorf("duration histogram bucket count %d does not match count %d", bucketCount, dp.GetCount())
	}
	if dp.Sum == nil || math.IsNaN(dp.GetSum()) || math.IsInf(dp.GetSum(), 0) || dp.GetSum() < 0 {
		return durationPoint{}, fmt.Errorf("duration histogram has invalid sum")
	}
	attrs, err := attributeMap(dp.GetAttributes(), metricAttributes)
	if err != nil {
		return durationPoint{}, err
	}
	if err := validateMetricAttributes(attrs); err != nil {
		return durationPoint{}, err
	}
	return durationPoint{
		labels: durationLabels{
			commandName: attrs[string(tmcfg.ResourceAttrCommandName)],
			outcomeCode: attrs[string(tmcfg.ResourceAttrOutcomeCode)],
		},
		buckets: append([]uint64(nil), dp.GetBucketCounts()...),
		count:   dp.GetCount(),
		sum:     dp.GetSum(),
	}, nil
}

func equalBounds(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func mergeHistogram(current *aggregatedHistogram, point durationPoint) (*aggregatedHistogram, error) {
	if current == nil {
		current = &aggregatedHistogram{buckets: make([]uint64, len(point.buckets))}
	}
	for i, count := range point.buckets {
		if math.MaxUint64-current.buckets[i] < count {
			return nil, fmt.Errorf("duration histogram bucket overflow")
		}
	}
	if math.MaxUint64-current.count < point.count || math.MaxFloat64-current.sum < point.sum {
		return nil, fmt.Errorf("duration histogram aggregation overflow")
	}
	for i, count := range point.buckets {
		current.buckets[i] += count
	}
	current.count += point.count
	current.sum += point.sum
	return current, nil
}

func mergeAggregatedHistogram(current, delta *aggregatedHistogram) *aggregatedHistogram {
	if current == nil {
		return &aggregatedHistogram{buckets: append([]uint64(nil), delta.buckets...), count: delta.count, sum: delta.sum}
	}
	for i, count := range delta.buckets {
		current.buckets[i] += count
	}
	current.count += delta.count
	current.sum += delta.sum
	return current
}

func addInt64[K comparable](values map[K]int64, key K, delta int64) error {
	if delta < 0 || values[key] > math.MaxInt64-delta {
		return fmt.Errorf("metric aggregation overflow")
	}
	values[key] += delta
	return nil
}

func (s *Server) wouldExceedSeriesBudget(parsed *parsedRequest) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	used := s.prometheusSeriesUsedLocked()
	for labels := range parsed.invocations {
		if _, exists := s.invocationCounts[labels]; !exists {
			used++
		}
	}
	for harness := range parsed.harnesses {
		if _, exists := s.processHarnesses[harness]; !exists {
			used++
		}
	}
	for labels := range parsed.durations {
		if _, exists := s.durationHistograms[labels]; !exists {
			used += durationHistogramSeriesCount()
		}
	}
	return used > s.cfg.MaxSeries
}

func (s *Server) wouldOverflow(parsed *parsedRequest) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !canAddInt64(s.totalMetrics, parsed.metricCount) || !canAddInt64(s.processMetrics, parsed.metricCount) {
		return true
	}
	for labels, delta := range parsed.invocations {
		if !canAddInt64(s.invocationCounts[labels], delta) {
			return true
		}
	}
	for command, delta := range parsed.commands {
		if !canAddInt64(s.commandCounts[command], delta) {
			return true
		}
	}
	for harness, delta := range parsed.harnesses {
		if !canAddInt64(s.harnessCounts[harness], delta) || !canAddInt64(s.processHarnesses[harness], delta) {
			return true
		}
	}
	for labels, delta := range parsed.durations {
		current := s.durationHistograms[labels]
		if current == nil {
			continue
		}
		for i, count := range delta.buckets {
			if math.MaxUint64-current.buckets[i] < count {
				return true
			}
		}
		if math.MaxUint64-current.count < delta.count || math.MaxFloat64-current.sum < delta.sum {
			return true
		}
	}
	return false
}

func canAddInt64(current, delta int64) bool {
	return delta >= 0 && current <= math.MaxInt64-delta
}

func (s *Server) prometheusSeriesUsedLocked() int {
	return len(s.invocationCounts) + len(s.processHarnesses) + len(s.durationHistograms)*durationHistogramSeriesCount()
}

func durationHistogramSeriesCount() int {
	return len(tmcfg.CommandDurationBucketBoundaries) + 3
}

func (s *Server) recordRequest(result, reason string) {
	s.mu.Lock()
	s.requestCounts[requestLabels{reason: reason, result: result}]++
	s.mu.Unlock()
}

func (s *Server) rejectMetrics(w http.ResponseWriter, status int, message, reason string) {
	s.recordRequest("rejected", reason)
	http.Error(w, message, status)
}

func (s *Server) handlePrometheusMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.RLock()
	invocations := make(map[invocationLabels]int64, len(s.invocationCounts))
	for labels, count := range s.invocationCounts {
		invocations[labels] = count
	}
	harnesses := make(map[string]int64, len(s.processHarnesses))
	for harness, count := range s.processHarnesses {
		harnesses[harness] = count
	}
	durations := make(map[durationLabels]aggregatedHistogram, len(s.durationHistograms))
	for labels, histogram := range s.durationHistograms {
		durations[labels] = aggregatedHistogram{
			buckets: append([]uint64(nil), histogram.buckets...),
			count:   histogram.count,
			sum:     histogram.sum,
		}
	}
	requests := make(map[requestLabels]int64, len(s.requestCounts))
	for labels, count := range s.requestCounts {
		requests[labels] = count
	}
	activeInstallations := make(map[string]int64, len(activeInstallationWindows))
	now := time.Now().UTC()
	for _, seenAt := range s.installationSeen {
		for _, window := range activeInstallationWindows {
			if !seenAt.Before(now.Add(-window.duration)) {
				activeInstallations[window.name]++
			}
		}
	}
	seriesUsed := s.prometheusSeriesUsedLocked()
	processMetrics := s.processMetrics
	lastReceive := s.lastReceive
	s.mu.RUnlock()

	storageBytes, _ := s.storage.size()
	lastReceiveSeconds := float64(0)
	if !lastReceive.IsZero() {
		lastReceiveSeconds = float64(lastReceive.Unix())
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	writePrometheusMetrics(w, prometheusSnapshot{
		invocations:         invocations,
		harnesses:           harnesses,
		durations:           durations,
		requests:            requests,
		activeInstallations: activeInstallations,
		metricCount:         processMetrics,
		storageBytes:        storageBytes,
		lastReceiveSeconds:  lastReceiveSeconds,
		budgetUsed:          int64(s.budget.Count()),
		budgetLimit:         int64(s.cfg.MaxInstallations),
		seriesUsed:          int64(seriesUsed),
		seriesLimit:         int64(s.cfg.MaxSeries),
	})
}

type prometheusSnapshot struct {
	invocations         map[invocationLabels]int64
	harnesses           map[string]int64
	durations           map[durationLabels]aggregatedHistogram
	requests            map[requestLabels]int64
	activeInstallations map[string]int64
	metricCount         int64
	storageBytes        int64
	lastReceiveSeconds  float64
	budgetUsed          int64
	budgetLimit         int64
	seriesUsed          int64
	seriesLimit         int64
}

func writePrometheusMetrics(w http.ResponseWriter, snapshot prometheusSnapshot) {
	writeMetricHeader(w, "agent_marketplace_receiver_invocations_total", "Accepted command invocations", "counter")
	invocationKeys := sortedInvocationLabels(snapshot.invocations)
	for _, labels := range invocationKeys {
		writeMetric(w, "agent_marketplace_receiver_invocations_total", invocationLabelSet(labels), strconv.FormatInt(snapshot.invocations[labels], 10))
	}

	writeMetricHeader(w, "agent_marketplace_receiver_harness_invocations_total", "Accepted command invocations by harness", "counter")
	harnesses := make([]string, 0, len(snapshot.harnesses))
	for harness := range snapshot.harnesses {
		harnesses = append(harnesses, harness)
	}
	sort.Strings(harnesses)
	for _, harness := range harnesses {
		writeMetric(w, "agent_marketplace_receiver_harness_invocations_total", labelSet([2]string{"harness", harness}), strconv.FormatInt(snapshot.harnesses[harness], 10))
	}

	writeMetricHeader(w, "agent_marketplace_receiver_command_duration_milliseconds", "Accepted command duration in milliseconds", "histogram")
	durationKeys := sortedDurationLabels(snapshot.durations)
	for _, labels := range durationKeys {
		histogram := snapshot.durations[labels]
		cumulative := uint64(0)
		for i, count := range histogram.buckets {
			cumulative += count
			upperBound := "+Inf"
			if i < len(tmcfg.CommandDurationBucketBoundaries) {
				upperBound = strconv.FormatFloat(tmcfg.CommandDurationBucketBoundaries[i], 'g', -1, 64)
			}
			writeMetric(w, "agent_marketplace_receiver_command_duration_milliseconds_bucket", durationLabelSet(labels, upperBound), strconv.FormatUint(cumulative, 10))
		}
		writeMetric(w, "agent_marketplace_receiver_command_duration_milliseconds_sum", durationLabelSet(labels, ""), strconv.FormatFloat(histogram.sum, 'g', -1, 64))
		writeMetric(w, "agent_marketplace_receiver_command_duration_milliseconds_count", durationLabelSet(labels, ""), strconv.FormatUint(histogram.count, 10))
	}

	writeMetricHeader(w, "agent_marketplace_receiver_requests_total", "Telemetry requests by result and reason", "counter")
	requestKeys := make([]requestLabels, 0, len(snapshot.requests))
	for labels := range snapshot.requests {
		requestKeys = append(requestKeys, labels)
	}
	sort.Slice(requestKeys, func(i, j int) bool {
		return requestLabelSet(requestKeys[i]) < requestLabelSet(requestKeys[j])
	})
	for _, labels := range requestKeys {
		writeMetric(w, "agent_marketplace_receiver_requests_total", requestLabelSet(labels), strconv.FormatInt(snapshot.requests[labels], 10))
	}

	writeMetricHeader(w, "agent_marketplace_receiver_datapoints_total", "Accepted metric data points", "counter")
	writeMetric(w, "agent_marketplace_receiver_datapoints_total", "", strconv.FormatInt(snapshot.metricCount, 10))
	writeMetricHeader(w, "agent_marketplace_receiver_storage_bytes", "Receiver storage size in bytes", "gauge")
	writeMetric(w, "agent_marketplace_receiver_storage_bytes", "", strconv.FormatInt(snapshot.storageBytes, 10))
	writeMetricHeader(w, "agent_marketplace_receiver_last_receive_timestamp_seconds", "Unix timestamp of the last accepted telemetry request", "gauge")
	writeMetric(w, "agent_marketplace_receiver_last_receive_timestamp_seconds", "", strconv.FormatFloat(snapshot.lastReceiveSeconds, 'g', -1, 64))
	writeMetricHeader(w, "agent_marketplace_receiver_active_installations", "Installations active within a rolling window", "gauge")
	for _, window := range activeInstallationWindows {
		writeMetric(w, "agent_marketplace_receiver_active_installations", labelSet([2]string{"window", window.name}), strconv.FormatInt(snapshot.activeInstallations[window.name], 10))
	}
	writeMetricHeader(w, "agent_marketplace_receiver_installation_budget_used", "Installation cardinality budget currently used", "gauge")
	writeMetric(w, "agent_marketplace_receiver_installation_budget_used", "", strconv.FormatInt(snapshot.budgetUsed, 10))
	writeMetricHeader(w, "agent_marketplace_receiver_installation_budget_limit", "Installation cardinality budget limit", "gauge")
	writeMetric(w, "agent_marketplace_receiver_installation_budget_limit", "", strconv.FormatInt(snapshot.budgetLimit, 10))
	writeMetricHeader(w, "agent_marketplace_receiver_series_budget_used", "Prometheus series budget currently used", "gauge")
	writeMetric(w, "agent_marketplace_receiver_series_budget_used", "", strconv.FormatInt(snapshot.seriesUsed, 10))
	writeMetricHeader(w, "agent_marketplace_receiver_series_budget_limit", "Prometheus series budget limit", "gauge")
	writeMetric(w, "agent_marketplace_receiver_series_budget_limit", "", strconv.FormatInt(snapshot.seriesLimit, 10))
}

func sortedInvocationLabels(counts map[invocationLabels]int64) []invocationLabels {
	keys := make([]invocationLabels, 0, len(counts))
	for labels := range counts {
		keys = append(keys, labels)
	}
	sort.Slice(keys, func(i, j int) bool { return invocationLabelSet(keys[i]) < invocationLabelSet(keys[j]) })
	return keys
}

func sortedDurationLabels(histograms map[durationLabels]aggregatedHistogram) []durationLabels {
	keys := make([]durationLabels, 0, len(histograms))
	for labels := range histograms {
		keys = append(keys, labels)
	}
	sort.Slice(keys, func(i, j int) bool { return durationLabelSet(keys[i], "") < durationLabelSet(keys[j], "") })
	return keys
}

func invocationLabelSet(labels invocationLabels) string {
	return labelSet(
		[2]string{"arch", labels.arch},
		[2]string{"cli_version", labels.cliVersion},
		[2]string{"command_name", labels.commandName},
		[2]string{"managed", labels.managed},
		[2]string{"os_name", labels.osName},
		[2]string{"outcome_code", labels.outcomeCode},
	)
}

func requestLabelSet(labels requestLabels) string {
	return labelSet(
		[2]string{"reason", labels.reason},
		[2]string{"result", labels.result},
	)
}

func durationLabelSet(labels durationLabels, upperBound string) string {
	pairs := [][2]string{
		{"command_name", labels.commandName},
		{"outcome_code", labels.outcomeCode},
	}
	if upperBound != "" {
		pairs = append(pairs, [2]string{"le", upperBound})
	}
	return labelSet(pairs...)
}

func labelSet(labels ...[2]string) string {
	parts := make([]string, len(labels))
	for i, label := range labels {
		parts[i] = label[0] + `="` + escapeLabelValue(label[1]) + `"`
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func escapeLabelValue(value string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`).Replace(value)
}

func writeMetricHeader(w http.ResponseWriter, name, help, metricType string) {
	_, _ = fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, metricType)
}

func writeMetric(w http.ResponseWriter, name, labels, value string) {
	_, _ = fmt.Fprintf(w, "%s%s %s\n", name, labels, value)
}
