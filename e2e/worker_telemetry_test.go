package e2e_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/acquire"
	mpconfig "github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

const workerMetricWait = 12 * time.Second

var workerMetricFamilies = []string{
	"agent_marketplace_source_observations_total",
	"agent_marketplace_source_observation_duration_seconds",
	"agent_marketplace_registry_refreshes_total",
	"agent_marketplace_registry_refresh_duration_seconds",
	"agent_marketplace_host_budget_delays_total",
	"agent_marketplace_config_candidates_total",
	"agent_marketplace_config_restart_required",
	"agent_marketplace_registry_last_successful_observation_timestamp_seconds",
	"agent_marketplace_registry_last_successful_index_timestamp_seconds",
	"agent_marketplace_registry_consecutive_probe_failures",
}

type serveTestProcess struct {
	cmd     *exec.Cmd
	env     *Env
	output  bytes.Buffer
	done    chan error
	cancel  context.CancelFunc
	stopped bool
	waitErr error
}

func newServeMetricsEnv(t *testing.T) (*Env, string, string, string, string) {
	t.Helper()
	env, registryURL := newServeReindexEnvWithURL(t)
	listen := publicGateEphemeralAddress(t)
	operations := publicGateEphemeralAddress(t)
	config := env.ReadFile("/workspace/marketplace.yaml")
	config = strings.Replace(config,
		"  canonical_url: https://example.com\n",
		"  canonical_url: https://example.com\nlisten: "+listen+"\noperations_listen: "+operations+"\n", 1)
	config = strings.Replace(config, "    dsn: marketplace.db\n",
		"    dsn: "+env.mustResolvePath("/workspace/marketplace.db")+"\n", 1)
	config = strings.Replace(config, "  interval: 1h\n", "  interval: 200ms\n", 1)
	config = strings.Replace(config, "    ref: main\n", "    ref: main\n    credentials:\n      token:\n        env: MARKETPLACE_PRIVATE_TOKEN\n", 1)
	env.WriteFile("/workspace/marketplace.yaml", config)
	return env, registryURL, config, listen, operations
}

func startServeTestProcess(t *testing.T, env *Env, configPath, role string, overrides map[string]string) *serveTestProcess {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, env.binary, "serve", "--config", configPath, "--role", role)
	cmd.Dir = env.workspace
	cmd.Env = subprocessEnvWithOverrides(env.subprocessEnv(), overrides)
	process := &serveTestProcess{cmd: cmd, env: env, done: make(chan error, 1), cancel: cancel}
	cmd.Stdout = &process.output
	cmd.Stderr = &process.output
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start serve --role %s: %v", role, err)
	}
	go func() { process.done <- cmd.Wait() }()
	t.Cleanup(func() { process.stop(t) })
	return process
}

func (p *serveTestProcess) stop(t *testing.T) string {
	t.Helper()
	if !p.stopped {
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
		select {
		case p.waitErr = <-p.done:
		case <-time.After(15 * time.Second):
			p.cancel()
			t.Errorf("serve process did not stop within 15 seconds")
			p.waitErr = <-p.done
		}
		p.cancel()
		p.stopped = true
		if p.waitErr != nil {
			t.Errorf("serve process exited with %v", p.waitErr)
		}
	}
	return p.logs()
}

func (p *serveTestProcess) logs() string {
	logs := p.output.String()
	logPath := filepath.Join(p.env.home, ".agent-marketplace", "logs", "agent-marketplace.log")
	if data, err := os.ReadFile(logPath); err == nil {
		logs += string(data)
	}
	return logs
}

func workerMetricsURL(operations string) string {
	return "http://" + operations + "/metrics"
}

func waitWorkerMetrics(t *testing.T, operations string) string {
	t.Helper()
	client := &http.Client{Timeout: 300 * time.Millisecond}
	deadline := time.Now().Add(workerMetricWait)
	for time.Now().Before(deadline) {
		response, err := client.Get(workerMetricsURL(operations))
		if err == nil {
			body, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if readErr == nil && response.StatusCode == http.StatusOK {
				return string(body)
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("operations /metrics at %s did not return 200", operations)
	return ""
}

func waitForMetric(t *testing.T, operations, name string, labels map[string]string, minimum float64, timeout time.Duration) (string, float64, bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	body := ""
	for time.Now().Before(deadline) {
		body = waitWorkerMetrics(t, operations)
		if value, ok := workerMetricValue(body, name, labels); ok && value >= minimum {
			return body, value, true
		}
		time.Sleep(25 * time.Millisecond)
	}
	value, ok := workerMetricValue(body, name, labels)
	return body, value, ok && value >= minimum
}

func workerMetricValue(body, name string, labels map[string]string) (float64, bool) {
	for _, line := range strings.Split(body, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		sample, rawValue, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		sampleName := sample
		labelText := ""
		if open := strings.IndexByte(sample, '{'); open >= 0 {
			sampleName = sample[:open]
			labelText = strings.TrimSuffix(sample[open+1:], "}")
		}
		if sampleName != name {
			continue
		}
		matches := true
		for key, value := range labels {
			if !strings.Contains(labelText, key+`="`+value+`"`) {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		value, err := strconv.ParseFloat(strings.Fields(rawValue)[0], 64)
		return value, err == nil
	}
	return 0, false
}

func workerMetricFamilyCount(body, name string) int {
	return strings.Count(body, "# HELP "+name+" ")
}

func workerTelemetryLines(body string) string {
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "agent_marketplace_source_") ||
			strings.HasPrefix(line, "agent_marketplace_registry_refresh") ||
			strings.HasPrefix(line, "agent_marketplace_host_budget") ||
			strings.HasPrefix(line, "agent_marketplace_config_candidate") ||
			strings.HasPrefix(line, "agent_marketplace_config_restart") ||
			strings.HasPrefix(line, "agent_marketplace_registry_last_successful") ||
			strings.HasPrefix(line, "agent_marketplace_registry_consecutive_probe_failures") {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func openWorkerMetadata(t *testing.T, configPath string) store.MetadataStore {
	t.Helper()
	cfg, err := mpconfig.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("load Marketplace config for metadata inspection: %v", err)
	}
	meta, _, err := mpconfig.OpenStores(t.Context(), cfg)
	if err != nil {
		t.Fatalf("open Marketplace metadata store: %v", err)
	}
	t.Cleanup(func() {
		if err := meta.Close(); err != nil {
			t.Errorf("close Marketplace metadata store: %v", err)
		}
	})
	return meta
}

func waitForServeCondition(t *testing.T, condition func() bool, description string) {
	t.Helper()
	deadline := time.Now().Add(workerMetricWait)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", description)
}

func appendMetricsRegistry(config, id, slug, origin string, duplicateRoots bool) string {
	root := "      - path: .agents\n        source_harness: factory\n"
	roots := root
	if duplicateRoots {
		roots += root
	}
	return config + fmt.Sprintf(`  - id: %s
    slug: %s
    origin: %s
    ref: main
    roots:
%s`, id, slug, origin, roots)
}

func replaceServeConfig(t *testing.T, env *Env, contents string) {
	t.Helper()
	path := env.mustResolvePath("/workspace/marketplace.yaml")
	temporary := path + ".replacement"
	if err := os.WriteFile(temporary, []byte(contents), 0o644); err != nil {
		t.Fatalf("write replacement config: %v", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		t.Fatalf("atomically replace config: %v", err)
	}
}

func workerCounterTotal(body, name string, labelName string, values ...string) float64 {
	var total float64
	for _, value := range values {
		metricValue, _ := workerMetricValue(body, name, map[string]string{labelName: value})
		total += metricValue
	}
	return total
}

func assertWorkerMetricLabels(t *testing.T, body string, registries map[string]bool) {
	t.Helper()
	allowed := map[string]map[string]bool{
		"outcome": {
			"changed": true, "unchanged": true, "failed": true,
			"succeeded": true, "superseded": true, "applied": true,
			"rejected": true, "apply_failed": true,
		},
		"trigger": {
			"source-change": true, "config-change": true, "reconciliation": true,
			"manual": true, "unknown": true,
		},
		"operation": {"observation": true, "refresh": true},
	}
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "agent_marketplace_source_observations_") &&
			!strings.HasPrefix(line, "agent_marketplace_source_observation_duration_") &&
			!strings.HasPrefix(line, "agent_marketplace_registry_refreshes_") &&
			!strings.HasPrefix(line, "agent_marketplace_registry_refresh_duration_") &&
			!strings.HasPrefix(line, "agent_marketplace_host_budget_delays_") &&
			!strings.HasPrefix(line, "agent_marketplace_config_candidates_") &&
			!strings.HasPrefix(line, "agent_marketplace_config_restart_required") &&
			!strings.HasPrefix(line, "agent_marketplace_registry_last_successful_") &&
			!strings.HasPrefix(line, "agent_marketplace_registry_consecutive_probe_failures") {
			continue
		}
		sample, _, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		open := strings.IndexByte(sample, '{')
		if open < 0 {
			continue
		}
		for _, label := range strings.Split(strings.TrimSuffix(sample[open+1:], "}"), ",") {
			key, value, ok := strings.Cut(label, "=")
			if !ok {
				t.Errorf("invalid worker label %q", label)
				continue
			}
			value = strings.Trim(value, `"`)
			switch key {
			case "registry":
				if !registries[value] {
					t.Errorf("unconfigured registry label %q", value)
				}
			case "le":
			default:
				if !allowed[key][value] {
					t.Errorf("unbounded worker label %s=%q", key, value)
				}
			}
		}
	}
}

func holdHostBudget(t *testing.T, meta store.MetadataStore, host string, count int, acquisition bool) []store.HostBudgetLease {
	t.Helper()
	limit := store.HostBudgetLimit{Operations: 2, Acquisitions: 1}
	deadline := time.Now().Add(workerMetricWait)
	for time.Now().Before(deadline) {
		leases := make([]store.HostBudgetLease, 0, count)
		var acquireErr error
		for i := 0; i < count; i++ {
			lease, err := meta.AcquireHostBudget(t.Context(), host, fmt.Sprintf("telemetry-test-%d", i), acquisition, limit, time.Minute)
			if err != nil {
				acquireErr = err
				break
			}
			leases = append(leases, lease)
		}
		if acquireErr == nil {
			return leases
		}
		for _, lease := range leases {
			_ = meta.ReleaseHostBudget(t.Context(), lease)
		}
		if !errors.Is(acquireErr, store.ErrConflict) {
			t.Fatalf("acquire host budget for telemetry test: %v", acquireErr)
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("could not acquire %d host budget lease(s) for %s", count, host)
	return nil
}

func releaseHostBudget(t *testing.T, meta store.MetadataStore, leases []store.HostBudgetLease) {
	t.Helper()
	for _, lease := range leases {
		if err := meta.ReleaseHostBudget(t.Context(), lease); err != nil {
			t.Errorf("release test host budget: %v", err)
		}
	}
}

// spec: marketplace-worker-telemetry/a-worker-only-process-serves-worker-metrics
func TestServeWorkerOnlyMetricsOnOperationsListener(t *testing.T) {
	env, _, _, listen, operations := newServeMetricsEnv(t)
	process := startServeTestProcess(t, env, env.mustResolvePath("/workspace/marketplace.yaml"), "worker", map[string]string{
		"MARKETPLACE_PRIVATE_TOKEN": "worker-telemetry-secret",
	})
	body, _, ready := waitForMetric(t, operations, "agent_marketplace_source_observations_total",
		map[string]string{"outcome": "changed"}, 1, workerMetricWait)
	if !ready {
		t.Fatal("worker did not report its initial source observation")
	}
	body, _, ready = waitForMetric(t, operations, "agent_marketplace_registry_last_successful_index_timestamp_seconds",
		map[string]string{"registry": "src-serve"}, 1, workerMetricWait)
	if !ready {
		t.Fatal("worker did not report its successful index")
	}
	for _, family := range workerMetricFamilies {
		if workerMetricFamilyCount(body, family) != 1 {
			t.Errorf("worker metrics response has %d HELP records for %s, want one", workerMetricFamilyCount(body, family), family)
		}
	}
	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + operations + "/readyz")
	if err != nil {
		t.Fatalf("GET worker /readyz: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("worker /readyz status = %d, want 404", response.StatusCode)
	}
	connection, err := net.DialTimeout("tcp", listen, 100*time.Millisecond)
	if err == nil {
		_ = connection.Close()
		t.Errorf("worker process unexpectedly opened public listener %s", listen)
	}
	_ = process
}

func TestServeWorkerFailsWhenOperationsListenerIsOccupied(t *testing.T) {
	env, _, _, _, operations := newServeMetricsEnv(t)
	occupied, err := net.Listen("tcp", operations)
	if err != nil {
		t.Fatalf("occupy operations listener %s: %v", operations, err)
	}
	t.Cleanup(func() { _ = occupied.Close() })
	process := startServeTestProcess(t, env, env.mustResolvePath("/workspace/marketplace.yaml"), "worker", map[string]string{
		"MARKETPLACE_PRIVATE_TOKEN": "worker-telemetry-secret",
	})
	var exitErr error
	select {
	case exitErr = <-process.done:
	case <-time.After(10 * time.Second):
		t.Fatal("worker with an occupied operations listener did not exit within 10 seconds")
	}
	process.waitErr = exitErr
	process.stopped = true
	if exitErr == nil {
		t.Fatal("worker with an occupied operations listener exited successfully")
	}
	if logs := process.logs(); !strings.Contains(logs, "listen worker operations") {
		t.Errorf("worker failure does not name the operations listener:\n%s", logs)
	}
}

// spec: marketplace-worker-telemetry/an-all-role-process-exposes-each-worker-series-once
func TestServeAllRoleExposesWorkerMetricsOnce(t *testing.T) {
	env, _, _, listen, operations := newServeMetricsEnv(t)
	startServeTestProcess(t, env, env.mustResolvePath("/workspace/marketplace.yaml"), "all", nil)
	waitForServeCondition(t, func() bool {
		client := &http.Client{Timeout: 300 * time.Millisecond}
		response, err := client.Get("http://" + listen + "/healthz")
		if err != nil {
			return false
		}
		_ = response.Body.Close()
		return response.StatusCode == http.StatusOK
	}, "public health listener")
	body, _, ready := waitForMetric(t, operations, "agent_marketplace_source_observations_total",
		map[string]string{"outcome": "changed"}, 1, workerMetricWait)
	if !ready {
		t.Fatal("all-role worker did not report its initial source observation")
	}
	for _, family := range workerMetricFamilies {
		if workerMetricFamilyCount(body, family) != 1 {
			t.Errorf("all-role metrics response has %d HELP records for %s, want one", workerMetricFamilyCount(body, family), family)
		}
	}
	if workerMetricFamilyCount(body, "agent_marketplace_requests_total") != 1 {
		t.Error("all-role metrics response omitted API request metrics")
	}
	response, err := http.Get("http://" + listen + "/metrics")
	if err != nil {
		t.Fatalf("GET public /metrics: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("public /metrics status = %d, want 404", response.StatusCode)
	}
}

// spec: marketplace-worker-telemetry/an-unchanged-observation-is-reported-as-healthy
// spec: marketplace-worker-telemetry/a-source-failure-is-distinct-from-a-refresh-failure
// spec: marketplace-worker-telemetry/a-host-budget-delay-is-counted-separately
func TestServeWorkerOperationalSignals(t *testing.T) {
	env, registryURL, config, _, operations := newServeMetricsEnv(t)
	config = appendMetricsRegistry(config, "src-refresh-fail", "refresh-fail", registryURL, true)
	env.WriteFile("/workspace/marketplace.yaml", config)
	configPath := env.mustResolvePath("/workspace/marketplace.yaml")
	meta := openWorkerMetadata(t, configPath)
	host, err := acquire.SourceHost(registryURL)
	if err != nil {
		t.Fatal(err)
	}
	refreshBudget := holdHostBudget(t, meta, host, 1, true)
	process := startServeTestProcess(t, env, configPath, "all", map[string]string{
		"MARKETPLACE_PRIVATE_TOKEN": "worker-telemetry-secret",
	})
	body, _, ok := waitForMetric(t, operations, "agent_marketplace_host_budget_delays_total",
		map[string]string{"operation": "refresh"}, 1, workerMetricWait)
	if !ok {
		t.Fatalf("refresh host-budget delay was not reported: %s", workerTelemetryLines(body))
	}
	if failed, _ := workerMetricValue(body, "agent_marketplace_source_observations_total", map[string]string{"outcome": "failed"}); failed != 0 {
		t.Fatalf("refresh host-budget delay increased failed observations to %v", failed)
	}
	releaseHostBudget(t, meta, refreshBudget)
	body, _, ok = waitForMetric(t, operations, "agent_marketplace_registry_refreshes_total",
		map[string]string{"trigger": "source-change", "outcome": "succeeded"}, 1, workerMetricWait)
	if !ok {
		t.Fatal("successful refresh was not reported after host budget release")
	}
	body, _, ok = waitForMetric(t, operations, "agent_marketplace_registry_refreshes_total",
		map[string]string{"trigger": "source-change", "outcome": "failed"}, 1, workerMetricWait)
	if !ok {
		t.Fatal("failed refresh from duplicate roots was not reported")
	}
	body, _, ok = waitForMetric(t, operations, "agent_marketplace_source_observations_total",
		map[string]string{"outcome": "unchanged"}, 1, workerMetricWait)
	if !ok {
		t.Fatal("unchanged observation was not reported")
	}
	lastIndex, found := workerMetricValue(body, "agent_marketplace_registry_last_successful_index_timestamp_seconds",
		map[string]string{"registry": "src-serve"})
	if !found || lastIndex <= 0 {
		t.Fatalf("successful index timestamp = %v (present %t), want a positive timestamp", lastIndex, found)
	}
	refreshFailures, _ := workerMetricValue(body, "agent_marketplace_registry_refreshes_total",
		map[string]string{"trigger": "source-change", "outcome": "failed"})
	observationsFailed, _ := workerMetricValue(body, "agent_marketplace_source_observations_total",
		map[string]string{"outcome": "failed"})
	refreshFailureLease := holdRegistryLease(t, meta, "src-refresh-fail")
	body = waitWorkerMetrics(t, operations)
	refreshFailures, _ = workerMetricValue(body, "agent_marketplace_registry_refreshes_total",
		map[string]string{"trigger": "source-change", "outcome": "failed"})
	result := env.Exec("git", "--git-dir", filepath.Join(env.repos, "serve-reg.git"), "update-ref", "-d", "refs/heads/main")
	if result.ExitCode != 0 {
		t.Fatalf("remove local source branch: %s", result.Combined)
	}
	sourceLease := holdRegistryLease(t, meta, "src-serve")
	body = waitWorkerMetrics(t, operations)
	lastObservation, found := workerMetricValue(body, "agent_marketplace_registry_last_successful_observation_timestamp_seconds",
		map[string]string{"registry": "src-serve"})
	if !found || lastObservation <= 0 {
		t.Fatalf("successful observation timestamp = %v (present %t), want a positive timestamp", lastObservation, found)
	}
	observationsFailed, _ = workerMetricValue(body, "agent_marketplace_source_observations_total",
		map[string]string{"outcome": "failed"})
	if err := forceServeObservationDue(t, meta, "src-serve"); err != nil {
		t.Fatal(err)
	}
	if err := meta.ReleaseRegistryLease(t.Context(), "src-serve", sourceLease); err != nil {
		t.Fatalf("release source registry lease: %v", err)
	}
	body, _, ok = waitForMetric(t, operations, "agent_marketplace_source_observations_total",
		map[string]string{"outcome": "failed"}, observationsFailed+1, workerMetricWait)
	if !ok {
		t.Fatal("source ref failure was not reported")
	}
	if err := forceServeObservationDue(t, meta, "src-serve"); err != nil {
		t.Fatal(err)
	}
	body, _, ok = waitForMetric(t, operations, "agent_marketplace_source_observations_total",
		map[string]string{"outcome": "failed"}, observationsFailed+2, workerMetricWait)
	if !ok {
		t.Fatal("repeated source ref failure was not reported")
	}
	if current, present := workerMetricValue(body, "agent_marketplace_registry_last_successful_observation_timestamp_seconds",
		map[string]string{"registry": "src-serve"}); !present || current != lastObservation {
		t.Errorf("source failure changed last successful observation timestamp from %v to %v", lastObservation, current)
	}
	if current, present := workerMetricValue(body, "agent_marketplace_registry_last_successful_index_timestamp_seconds",
		map[string]string{"registry": "src-serve"}); !present || current != lastIndex {
		t.Errorf("source failure changed last successful index timestamp from %v to %v", lastIndex, current)
	}
	if current, _ := workerMetricValue(body, "agent_marketplace_registry_refreshes_total",
		map[string]string{"trigger": "source-change", "outcome": "failed"}); current != refreshFailures {
		t.Errorf("source failure changed failed refresh count from %v to %v", refreshFailures, current)
	}
	currentSourceFailures, _ := workerMetricValue(body, "agent_marketplace_source_observations_total",
		map[string]string{"outcome": "failed"})
	if currentSourceFailures <= observationsFailed {
		t.Errorf("source failure count did not advance: %v to %v", observationsFailed, currentSourceFailures)
	}
	currentProbeFailures, present := workerMetricValue(body, "agent_marketplace_registry_consecutive_probe_failures",
		map[string]string{"registry": "src-serve"})
	if !present || currentProbeFailures < 1 {
		t.Errorf("consecutive probe failure gauge = %v (present %t), want at least one", currentProbeFailures, present)
	}

	sourceProbeLease := holdRegistryLease(t, meta, "src-serve")
	body = waitWorkerMetrics(t, operations)
	currentSourceFailures, _ = workerMetricValue(body, "agent_marketplace_source_observations_total",
		map[string]string{"outcome": "failed"})
	observationDelaysBefore, _ := workerMetricValue(body, "agent_marketplace_host_budget_delays_total",
		map[string]string{"operation": "observation"})
	observationBudget := holdHostBudget(t, meta, host, 2, false)
	if err := forceServeObservationDue(t, meta, "src-serve"); err != nil {
		t.Fatal(err)
	}
	if err := meta.ReleaseRegistryLease(t.Context(), "src-serve", sourceProbeLease); err != nil {
		t.Fatalf("release source registry observation lease: %v", err)
	}
	body, _, ok = waitForMetric(t, operations, "agent_marketplace_host_budget_delays_total",
		map[string]string{"operation": "observation"}, observationDelaysBefore+1, workerMetricWait)
	if !ok {
		t.Fatal("observation host-budget delay was not reported")
	}
	time.Sleep(700 * time.Millisecond)
	body = waitWorkerMetrics(t, operations)
	if current, _ := workerMetricValue(body, "agent_marketplace_host_budget_delays_total",
		map[string]string{"operation": "observation"}); current > observationDelaysBefore+2 {
		t.Errorf("two configured registries increased the host-budget delay counter by more than two delayed observations: %v to %v",
			observationDelaysBefore, current)
	}
	releaseHostBudget(t, meta, observationBudget)
	if err := meta.ReleaseRegistryLease(t.Context(), "src-refresh-fail", refreshFailureLease); err != nil {
		t.Fatalf("release failed-refresh registry lease: %v", err)
	}
	if current, _ := workerMetricValue(body, "agent_marketplace_source_observations_total",
		map[string]string{"outcome": "failed"}); current != currentSourceFailures {
		t.Errorf("host-budget delay changed failed observation count from %v to %v", currentSourceFailures, current)
	}

	logs := process.stop(t)
	if !strings.Contains(logs, "refresh finished") || !strings.Contains(logs, "registry") ||
		!strings.Contains(logs, "trigger") || !strings.Contains(logs, "outcome") || !strings.Contains(logs, "duration") {
		t.Errorf("refresh completion log is missing registry, trigger, outcome, or duration fields:\n%s", logs)
	}
	if strings.Contains(logs, "worker-telemetry-secret") || strings.Contains(body, "worker-telemetry-secret") {
		t.Error("worker metrics or logs exposed the configured token")
	}
	assertWorkerMetricLabels(t, body, map[string]bool{"src-serve": true, "src-refresh-fail": true})
}

func forceServeObservationDue(t *testing.T, meta store.MetadataStore, id store.RegistryID) error {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		observation, err := meta.GetRegistryObservation(t.Context(), id)
		if err != nil {
			return err
		}
		observation.NextProbeAt = time.Now().UTC().Add(-time.Second)
		if _, err := meta.UpdateRegistryObservation(t.Context(), observation.Version, observation); err == nil {
			return nil
		} else if !errors.Is(err, store.ErrConflict) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("observation for registry %q stayed contended", id)
}

// spec: marketplace-runtime-configuration/a-running-serve-process-applies-a-replaced-configuration
func TestServeRuntimeConfigAppliedReplacement(t *testing.T) {
	env, registryURL, config, _, operations := newServeMetricsEnv(t)
	configPath := env.mustResolvePath("/workspace/marketplace.yaml")
	process := startServeTestProcess(t, env, configPath, "all", map[string]string{
		"MARKETPLACE_PRIVATE_TOKEN": "worker-telemetry-secret",
	})
	meta := openWorkerMetadata(t, configPath)
	waitForServeCondition(t, func() bool {
		_, err := meta.LatestRegistryOccurrence(t.Context(), "src-serve")
		return err == nil
	}, "initial registry index")

	candidate := appendMetricsRegistry(config, "src-added", "added-source", registryURL, false)
	replaceServeConfig(t, env, candidate)
	waitForServeCondition(t, func() bool {
		_, err := meta.LatestRegistryOccurrence(t.Context(), "src-added")
		return err == nil
	}, "index of the added registry")
	body, applied, ok := waitForMetric(t, operations, "agent_marketplace_config_candidates_total",
		map[string]string{"outcome": "applied"}, 1, workerMetricWait)
	if !ok {
		t.Fatalf("applied configuration candidate count = %v", applied)
	}
	if restartRequired, _ := workerMetricValue(body, "agent_marketplace_config_restart_required", nil); restartRequired != 0 {
		t.Errorf("restart-required gauge after worker-safe addition = %v, want zero", restartRequired)
	}
	logs := process.stop(t)
	if !strings.Contains(logs, "config candidate applied") || strings.Contains(logs, "worker-telemetry-secret") {
		t.Errorf("applied replacement log missing or exposed a credential:\n%s", logs)
	}
}

// spec: marketplace-runtime-configuration/a-running-serve-process-rejects-an-invalid-replacement
// spec: marketplace-worker-telemetry/a-configuration-rejection-is-distinct-from-restart-required-drift
// spec: marketplace-worker-telemetry/worker-metrics-contain-no-credentials-or-unbounded-values
// spec: marketplace-worker-telemetry/a-removed-registry-s-series-are-dropped
func TestServeRuntimeConfigRejectionRestartDriftAndSeriesRemoval(t *testing.T) {
	env, registryURL, startupConfig, listen, operations := newServeMetricsEnv(t)
	startupConfig = appendMetricsRegistry(startupConfig, "src-removed", "removed-source", registryURL, false)
	env.WriteFile("/workspace/marketplace.yaml", startupConfig)
	configPath := env.mustResolvePath("/workspace/marketplace.yaml")
	process := startServeTestProcess(t, env, configPath, "all", map[string]string{
		"MARKETPLACE_PRIVATE_TOKEN": "worker-telemetry-secret",
	})
	meta := openWorkerMetadata(t, configPath)
	for _, id := range []store.RegistryID{"src-serve", "src-removed"} {
		registryID := id
		waitForServeCondition(t, func() bool {
			_, err := meta.LatestRegistryOccurrence(t.Context(), registryID)
			return err == nil
		}, "initial index for "+string(id))
	}

	before, err := meta.GetRegistryObservation(t.Context(), "src-serve")
	if err != nil {
		t.Fatal(err)
	}
	replaceServeConfig(t, env, "registries: [\n")
	if _, _, rejected := waitForMetric(t, operations, "agent_marketplace_config_candidates_total",
		map[string]string{"outcome": "rejected"}, 1, workerMetricWait); !rejected {
		t.Fatal("invalid configuration replacement was not reported as rejected")
	}
	waitForServeCondition(t, func() bool {
		current, err := meta.GetRegistryObservation(t.Context(), "src-serve")
		return err == nil && current.ObservedAt.After(before.ObservedAt)
	}, "observation continuing under the last known-good configuration after rejection")
	if _, err := http.Get(workerMetricsURL(operations)); err != nil {
		t.Fatalf("serve stopped after invalid configuration: %v", err)
	}

	restartCandidate := strings.Replace(startupConfig, "listen: "+listen+"\n", "listen: 127.0.0.1:1\n", 1)
	replaceServeConfig(t, env, restartCandidate)
	body, restartRequired, ok := waitForMetric(t, operations, "agent_marketplace_config_restart_required", nil, 1, workerMetricWait)
	if !ok {
		t.Fatalf("restart-required gauge = %v", restartRequired)
	}
	if rejected, _ := workerMetricValue(body, "agent_marketplace_config_candidates_total",
		map[string]string{"outcome": "rejected"}); rejected != 1 {
		t.Errorf("rejected candidate count after restart-only drift = %v, want one", rejected)
	}

	removedCandidate := strings.Replace(restartCandidate,
		appendMetricsRegistry("", "src-removed", "removed-source", registryURL, false), "", 1)
	replaceServeConfig(t, env, removedCandidate)
	waitForServeCondition(t, func() bool {
		registry, err := meta.GetRegistry(t.Context(), "src-removed")
		return err == nil && !registry.Listed
	}, "runtime removal of the second registry")
	body = waitWorkerMetrics(t, operations)
	for _, family := range []string{
		"agent_marketplace_registry_last_successful_observation_timestamp_seconds",
		"agent_marketplace_registry_last_successful_index_timestamp_seconds",
		"agent_marketplace_registry_consecutive_probe_failures",
	} {
		if _, exists := workerMetricValue(body, family, map[string]string{"registry": "src-removed"}); exists {
			t.Errorf("removed registry still has a %s series", family)
		}
	}
	assertWorkerMetricLabels(t, body, map[string]bool{"src-serve": true, "src-removed": true})
	logs := process.stop(t)
	if strings.Count(logs, "config candidate rejected") != 1 ||
		!strings.Contains(logs, "restart-required config drift detected") ||
		!strings.Contains(logs, "config candidate applied") {
		t.Errorf("configuration outcome logs did not distinguish rejection, application, and restart drift:\n%s", logs)
	}
	if strings.Contains(logs, "worker-telemetry-secret") || strings.Contains(body, "worker-telemetry-secret") {
		t.Error("worker metrics or logs exposed the configured token")
	}
}

// spec: marketplace-worker-telemetry/a-superseded-refresh-is-not-a-refresh-failure
func TestServeSupersededRefreshHasDistinctOutcome(t *testing.T) {
	env, registryURL, startupConfig, _, operations := newServeMetricsEnv(t)
	gatedURL, cloneStarted, releaseClone := newGatedGitHTTPServer(t, env.repos)
	defer releaseClone()
	config := strings.Replace(startupConfig, registryURL, gatedURL+"/serve-reg.git", 1)
	env.WriteFile("/workspace/marketplace.yaml", config)
	configPath := env.mustResolvePath("/workspace/marketplace.yaml")
	process := startServeTestProcess(t, env, configPath, "all", map[string]string{
		"MARKETPLACE_PRIVATE_TOKEN": "worker-telemetry-secret",
	})
	select {
	case <-cloneStarted:
	case <-time.After(workerMetricWait):
		t.Fatal("worker did not start the local Git refresh")
	}

	candidate := strings.Replace(config, "  hidden_content: reject\n", "  hidden_content: warn\n", 1)
	replaceServeConfig(t, env, candidate)
	_, _, applied := waitForMetric(t, operations, "agent_marketplace_config_candidates_total",
		map[string]string{"outcome": "applied"}, 1, 5*time.Second)
	releaseClone()
	if !applied {
		t.Error("runtime configuration change did not report an applied candidate")
	}

	body, _, superseded := waitForMetric(t, operations, "agent_marketplace_registry_refreshes_total",
		map[string]string{"trigger": "source-change", "outcome": "superseded"}, 1, workerMetricWait)
	if !superseded {
		t.Error("superseded refresh was not counted")
	}
	if failures, _ := workerMetricValue(body, "agent_marketplace_registry_refreshes_total",
		map[string]string{"trigger": "source-change", "outcome": "failed"}); failures != 0 {
		t.Errorf("superseded refresh increased failed count to %v", failures)
	}
	meta := openWorkerMetadata(t, configPath)
	waitForServeCondition(t, func() bool {
		history, err := meta.RegistryHistory(t.Context(), "src-serve")
		return err == nil && len(history) == 1
	}, "publication under the current configuration")
	logs := process.stop(t)
	if !strings.Contains(logs, "refresh finished") || !strings.Contains(logs, "superseded") {
		t.Errorf("superseded refresh completion was not logged:\n%s", logs)
	}
}

func newGatedGitHTTPServer(t *testing.T, projectRoot string) (string, <-chan struct{}, func()) {
	t.Helper()
	backend := newGitHTTPServer(t, projectRoot)
	target, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var releaseOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git-upload-pack") {
			once.Do(func() { close(started) })
			<-release
		}
		proxy.ServeHTTP(w, r)
	}))
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(func() {
		unblock()
		server.Close()
		backend.Close()
	})
	return server.URL, started, unblock
}

// spec: marketplace-source-observation/two-worker-processes-probe-and-refresh-a-changed-registry-once
func TestTwoServeWorkersProbeAndRefreshChangedRegistryOnce(t *testing.T) {
	env, _, config, _, firstOperations := newServeMetricsEnv(t)
	secondOperations := publicGateEphemeralAddress(t)
	config = strings.Replace(config,
		"  interval: 200ms\n", "  interval: 1s\n", 1)
	if dsn := os.Getenv("POSTGRES_DSN"); dsn != "" {
		config = strings.Replace(config,
			"    driver: sqlite\n    dsn: "+env.mustResolvePath("/workspace/marketplace.db")+"\n",
			"    driver: postgres\n    dsn_env: POSTGRES_DSN\n", 1)
	}
	env.WriteFile("/workspace/marketplace.yaml", config)
	configPath := env.mustResolvePath("/workspace/marketplace.yaml")
	first := startServeTestProcess(t, env, configPath, "worker", map[string]string{
		"MARKETPLACE_PRIVATE_TOKEN": "worker-telemetry-secret",
	})
	meta := openWorkerMetadata(t, configPath)
	waitWorkerMetrics(t, firstOperations)
	waitForServeCondition(t, func() bool {
		history, err := meta.RegistryHistory(t.Context(), "src-serve")
		return err == nil && len(history) == 1
	}, "one initial registry occurrence")
	firstBody, _, firstReady := waitForMetric(t, firstOperations, "agent_marketplace_registry_refreshes_total",
		map[string]string{"trigger": "source-change", "outcome": "succeeded"}, 1, workerMetricWait)
	if !firstReady {
		t.Fatalf("initial source-change refresh was not observed: %s", workerTelemetryLines(firstBody))
	}
	secondConfig := strings.Replace(config, "operations_listen: "+firstOperations+"\n",
		"operations_listen: "+secondOperations+"\n", 1)
	secondConfigPath := env.mustResolvePath("/workspace/worker-two.yaml")
	if err := os.WriteFile(secondConfigPath, []byte(secondConfig), 0o644); err != nil {
		t.Fatalf("write second worker configuration: %v", err)
	}
	second := startServeTestProcess(t, env, secondConfigPath, "worker", map[string]string{
		"MARKETPLACE_PRIVATE_TOKEN": "worker-telemetry-secret",
	})
	waitWorkerMetrics(t, secondOperations)
	lease := holdRegistryLease(t, meta, "src-serve")
	firstBefore := waitWorkerMetrics(t, firstOperations)
	secondBefore := waitWorkerMetrics(t, secondOperations)
	observationsBefore := workerObservationsTotal(firstBefore) + workerObservationsTotal(secondBefore)
	if err := forceServeObservationDue(t, meta, "src-serve"); err != nil {
		t.Fatal(err)
	}
	if err := meta.ReleaseRegistryLease(t.Context(), "src-serve", lease); err != nil {
		t.Fatalf("release initial observation lease: %v", err)
	}
	waitForServeCondition(t, func() bool {
		return workerObservationsTotal(waitWorkerMetrics(t, firstOperations))+
			workerObservationsTotal(waitWorkerMetrics(t, secondOperations)) > observationsBefore
	}, "an observation with both workers running")

	lease = holdRegistryLease(t, meta, "src-serve")
	firstBefore = waitWorkerMetrics(t, firstOperations)
	secondBefore = waitWorkerMetrics(t, secondOperations)
	observationsBefore = workerObservationsTotal(firstBefore) + workerObservationsTotal(secondBefore)
	refreshesBefore := workerMetricValueOrZero(firstBefore, "agent_marketplace_registry_refreshes_total",
		map[string]string{"trigger": "source-change", "outcome": "succeeded"}) +
		workerMetricValueOrZero(secondBefore, "agent_marketplace_registry_refreshes_total",
			map[string]string{"trigger": "source-change", "outcome": "succeeded"})
	commit := pushMetricsRegistryCommit(t, env)
	if err := forceServeObservationDue(t, meta, "src-serve"); err != nil {
		t.Fatal(err)
	}
	if err := meta.ReleaseRegistryLease(t.Context(), "src-serve", lease); err != nil {
		t.Fatalf("release test registry lease: %v", err)
	}
	waitForServeCondition(t, func() bool {
		history, err := meta.RegistryHistory(t.Context(), "src-serve")
		if err != nil || len(history) != 2 {
			return false
		}
		return history[0].Commit == commit || history[1].Commit == commit
	}, "publication of the new commit")
	waitForServeCondition(t, func() bool {
		firstMetrics := waitWorkerMetrics(t, firstOperations)
		secondMetrics := waitWorkerMetrics(t, secondOperations)
		return workerMetricValueOrZero(firstMetrics, "agent_marketplace_registry_refreshes_total",
			map[string]string{"trigger": "source-change", "outcome": "succeeded"})+
			workerMetricValueOrZero(secondMetrics, "agent_marketplace_registry_refreshes_total",
				map[string]string{"trigger": "source-change", "outcome": "succeeded"}) > refreshesBefore
	}, "completion of the changed-source refresh")
	firstAfter := waitWorkerMetrics(t, firstOperations)
	secondAfter := waitWorkerMetrics(t, secondOperations)
	observationsAfter := workerObservationsTotal(firstAfter) + workerObservationsTotal(secondAfter)
	refreshesAfter := workerMetricValueOrZero(firstAfter, "agent_marketplace_registry_refreshes_total",
		map[string]string{"trigger": "source-change", "outcome": "succeeded"}) +
		workerMetricValueOrZero(secondAfter, "agent_marketplace_registry_refreshes_total",
			map[string]string{"trigger": "source-change", "outcome": "succeeded"})
	logs := first.stop(t) + second.stop(t)
	if observationsAfter-observationsBefore > 1 {
		t.Errorf("two workers performed %v observations for one due slot, want at most one",
			observationsAfter-observationsBefore)
	}
	if refreshesAfter-refreshesBefore != 1 {
		t.Errorf("source-change refreshes increased by %v, want exactly one", refreshesAfter-refreshesBefore)
	}
	history, err := meta.RegistryHistory(t.Context(), "src-serve")
	if err != nil {
		t.Fatal(err)
	}
	occurrences := 0
	for _, item := range history {
		if item.Commit == commit {
			occurrences++
		}
	}
	if occurrences != 1 {
		t.Errorf("new commit appears in registry history %d times, want once", occurrences)
	}
	if !strings.Contains(logs, "refresh finished") || strings.Contains(logs, "worker-telemetry-secret") {
		t.Errorf("worker logs omitted refresh completion or exposed a secret:\n%s", logs)
	}
}

func workerMetricValueOrZero(body, name string, labels map[string]string) float64 {
	value, _ := workerMetricValue(body, name, labels)
	return value
}

func workerObservationsTotal(body string) float64 {
	return workerCounterTotal(body, "agent_marketplace_source_observations_total", "outcome",
		"changed", "unchanged", "failed")
}

func holdRegistryLease(t *testing.T, meta store.MetadataStore, id store.RegistryID) store.RegistryLease {
	t.Helper()
	deadline := time.Now().Add(workerMetricWait)
	for time.Now().Before(deadline) {
		lease, err := meta.AcquireRegistryLease(t.Context(), id, "telemetry-test", 2*time.Minute)
		if err == nil {
			return lease
		}
		if !errors.Is(err, store.ErrConflict) {
			t.Fatalf("acquire test registry lease: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("registry %q stayed leased", id)
	return store.RegistryLease{}
}

func pushMetricsRegistryCommit(t *testing.T, env *Env) string {
	t.Helper()
	work := filepath.Join(env.repos, "work-metrics-update")
	bare := filepath.Join(env.repos, "serve-reg.git")
	if result := env.Exec("git", "clone", "--quiet", bare, work); result.ExitCode != 0 {
		t.Fatalf("clone local registry for update: %s", result.Combined)
	}
	marker := filepath.Join(work, ".agents", "skills", "review", "SKILL.md")
	if err := os.WriteFile(marker, []byte("---\nname: review\ndescription: Updated review workflow\nlicense: MIT\n---\n\n# Updated review\n"), 0o644); err != nil {
		t.Fatalf("update local registry content: %v", err)
	}
	for _, args := range [][]string{
		{"-C", work, "config", "user.name", "E2E Test"},
		{"-C", work, "config", "user.email", "e2e@test.local"},
		{"-C", work, "add", ".agents/skills/review/SKILL.md"},
		{"-C", work, "commit", "-q", "-m", "update source"},
	} {
		result := env.Exec(append([]string{"git"}, args...)...)
		if result.ExitCode != 0 {
			t.Fatalf("update local registry with %v: %s", args, result.Combined)
		}
	}
	result := env.Exec("git", "-C", work, "rev-parse", "HEAD")
	if result.ExitCode != 0 {
		t.Fatalf("resolve updated commit: %s", result.Combined)
	}
	commit := strings.TrimSpace(result.Stdout)
	if result := env.Exec("git", "-C", work, "push", "-q", "origin", "main"); result.ExitCode != 0 {
		t.Fatalf("push updated local registry: %s", result.Combined)
	}
	return commit
}
