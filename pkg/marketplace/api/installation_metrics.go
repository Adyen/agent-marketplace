package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

const installationMetricsMaxBody = 8 << 20

type installationSnapshot struct {
	counts      installationCounts
	signature   string
	evaluatedAt time.Time
	expiresAt   time.Time
}

type installationMetricsReader struct {
	url           string
	labels        map[string]string
	tokenEnv      string
	marketplaceID store.MarketplaceID
	client        *http.Client
	now           func() time.Time
	timing        config.InstallationMetricsTiming
	metrics       *serverMetrics

	snapshot atomic.Pointer[installationSnapshot]
	hints    chan struct{}

	backoffUntil time.Time
	nextWake     time.Time
}

func newInstallationMetricsReader(metrics config.InstallationMetrics, marketplaceID store.MarketplaceID) *installationMetricsReader {
	timing := config.ResolveInstallationMetricsTiming(metrics)
	return &installationMetricsReader{
		url:           strings.TrimRight(strings.TrimSpace(metrics.URL), "/"),
		labels:        metrics.Labels,
		tokenEnv:      strings.TrimSpace(metrics.BearerTokenEnv),
		marketplaceID: marketplaceID,
		client: &http.Client{
			Timeout: timing.Timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		now:    time.Now,
		timing: timing,
		hints:  make(chan struct{}, 1),
	}
}

func registryOriginsSignature(registries map[store.RegistryID]store.Registry) string {
	rows := make([]string, 0, len(registries))
	for id, registry := range registries {
		rows = append(rows, string(id)+"\x00"+strings.Join(registry.Origins, "\x00"))
	}
	sort.Strings(rows)
	return strings.Join(rows, "\x00\x00")
}

func (r *installationMetricsReader) lookup(registries map[store.RegistryID]store.Registry) (*installationCounts, bool) {
	snapshot := r.snapshot.Load()
	if snapshot == nil || snapshot.signature != registryOriginsSignature(registries) || !r.now().Before(snapshot.expiresAt) {
		return nil, false
	}
	return &snapshot.counts, true
}

func (r *installationMetricsReader) hint() {
	select {
	case r.hints <- struct{}{}:
	default:
	}
}

func (r *installationMetricsReader) refresh(ctx context.Context, registries map[store.RegistryID]store.Registry) (installationCounts, error) {
	evaluatedAt := r.now()
	counts, err := r.fetch(ctx, registries, evaluatedAt)
	if err != nil {
		return installationCounts{}, err
	}
	expiresAt := minTime(evaluatedAt.Add(r.timing.MaxAge), counts.ValidUntil)
	if !r.now().Before(expiresAt) {
		return installationCounts{}, errInstallationMetricsUnavailable
	}
	r.snapshot.Store(&installationSnapshot{
		counts:      counts,
		signature:   registryOriginsSignature(registries),
		evaluatedAt: evaluatedAt,
		expiresAt:   expiresAt,
	})
	return counts, nil
}

func (r *installationMetricsReader) run(ctx context.Context, index func(context.Context) (map[store.RegistryID]store.Registry, error)) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			timer.Reset(r.attempt(ctx, index, false))
		case <-r.hints:
			timer.Reset(r.attempt(ctx, index, true))
		}
	}
}

func (r *installationMetricsReader) attempt(ctx context.Context, index func(context.Context) (map[store.RegistryID]store.Registry, error), fromHint bool) time.Duration {
	started := r.now()
	if fromHint && started.Before(r.backoffUntil) {
		return r.backoffUntil.Sub(started)
	}
	attemptCtx, cancel := context.WithTimeout(ctx, r.timing.Timeout)
	defer cancel()
	registries, err := index(attemptCtx)
	if err != nil {
		return r.failedAttempt(started, "unavailable")
	}
	signature := registryOriginsSignature(registries)
	if fromHint {
		if snapshot := r.snapshot.Load(); snapshot != nil && snapshot.signature == signature && r.now().Before(snapshot.expiresAt) {
			if r.nextWake.IsZero() {
				r.nextWake = minTime(snapshot.evaluatedAt.Add(r.timing.Interval+r.jitter()), snapshot.expiresAt)
			}
			return r.remaining(r.now())
		}
	}
	if _, err := r.refresh(attemptCtx, registries); err != nil {
		return r.failedAttempt(started, installationCountsErrorLogValue(err))
	}
	completed := r.now()
	published := r.snapshot.Load()
	r.backoffUntil = time.Time{}
	if r.metrics != nil {
		r.metrics.observeInstallationRefresh("success", started, completed, published.evaluatedAt)
	}
	r.nextWake = minTime(published.evaluatedAt.Add(r.timing.Interval+r.jitter()), published.expiresAt)
	return r.remaining(completed)
}

func (r *installationMetricsReader) remaining(now time.Time) time.Duration {
	if delay := r.nextWake.Sub(now); delay > 0 {
		return delay
	}
	return 0
}

func (r *installationMetricsReader) jitter() time.Duration {
	if r.timing.Jitter <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(r.timing.Jitter)))
}

func (r *installationMetricsReader) failedAttempt(started time.Time, outcome string) time.Duration {
	completed := r.now()
	r.backoffUntil = completed.Add(r.timing.RetryInterval)
	r.nextWake = r.backoffUntil
	if r.metrics != nil {
		r.metrics.observeInstallationRefresh(outcome, started, completed, time.Time{})
	}
	slog.Warn("installation metrics refresh failed", "reason", outcome)
	return r.timing.RetryInterval
}

func minTime(a, b time.Time) time.Time {
	if b.IsZero() || a.Before(b) {
		return a
	}
	return b
}

func (r *installationMetricsReader) fetch(ctx context.Context, registries map[store.RegistryID]store.Registry, evalTime time.Time) (installationCounts, error) {
	evalTime = time.Unix(evalTime.Unix(), 0)
	plan := newInstallationQueryPlan(registries, r.marketplaceID)
	form := url.Values{
		"query": {installationMetricsQuery(r.labels, plan)},
		"time":  {strconv.FormatInt(evalTime.Unix(), 10)},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url+"/api/v1/query", strings.NewReader(form.Encode()))
	if err != nil {
		return installationCounts{}, errInstallationMetricsUnavailable
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if r.tokenEnv != "" {
		token, ok := os.LookupEnv(r.tokenEnv)
		if !ok || token == "" || strings.ContainsAny(token, "\r\n") {
			return installationCounts{}, errInstallationMetricsUnavailable
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return installationCounts{}, errInstallationMetricsUnavailable
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return installationCounts{}, errInstallationMetricsUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, installationMetricsMaxBody+1))
	if err != nil || len(body) > installationMetricsMaxBody {
		return installationCounts{}, errInstallationMetricsUnavailable
	}
	samples, err := parsePrometheusVector(body)
	if err != nil {
		return installationCounts{}, err
	}
	return aggregateInstallationMetrics(samples, plan, evalTime)
}

type prometheusVectorResponse struct {
	Status   string   `json:"status"`
	Warnings []string `json:"warnings"`
	Data     struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []any             `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

func parsePrometheusVector(body []byte) ([]inventoryMetricSample, error) {
	if !json.Valid(body) {
		return nil, errInvalidInstallationMetrics
	}
	if _, err := jsonc.Canonicalize("metrics.json", body); err != nil {
		return nil, errInvalidInstallationMetrics
	}
	root, err := prometheusObject(body, "status", "warnings", "data")
	if err != nil {
		return nil, err
	}
	data, err := prometheusObject(root["data"], "resultType", "result")
	if err != nil {
		return nil, err
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(data["result"], &rows); err != nil {
		return nil, errInvalidInstallationMetrics
	}
	for _, row := range rows {
		object, err := prometheusObject(row, "metric", "value")
		if err != nil {
			return nil, err
		}
		var pair []json.RawMessage
		if err := json.Unmarshal(object["value"], &pair); err != nil || len(pair) != 2 {
			return nil, errInvalidInstallationMetrics
		}
		if !finiteMetricNumber(pair[0]) {
			return nil, errInvalidInstallationMetrics
		}
		var sample string
		if err := json.Unmarshal(pair[1], &sample); err != nil {
			return nil, errInvalidInstallationMetrics
		}
	}

	var decoded prometheusVectorResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, errInvalidInstallationMetrics
	}
	if decoded.Status != "success" || len(decoded.Warnings) != 0 || decoded.Data.ResultType != "vector" {
		return nil, errInvalidInstallationMetrics
	}
	samples := make([]inventoryMetricSample, 0, len(decoded.Data.Result))
	for _, entry := range decoded.Data.Result {
		if len(entry.Value) != 2 || entry.Metric == nil {
			return nil, errInvalidInstallationMetrics
		}
		raw, ok := entry.Value[1].(string)
		if !ok {
			return nil, errInvalidInstallationMetrics
		}
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, errInvalidInstallationMetrics
		}
		samples = append(samples, inventoryMetricSample{Metric: entry.Metric, Value: value})
	}
	return samples, nil
}

func prometheusObject(raw json.RawMessage, modeled ...string) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if raw == nil || json.Unmarshal(raw, &object) != nil || object == nil {
		return nil, errInvalidInstallationMetrics
	}
	for key := range object {
		for _, name := range modeled {
			if key != name && strings.EqualFold(key, name) {
				return nil, errInvalidInstallationMetrics
			}
		}
	}
	return object, nil
}

func finiteMetricNumber(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || (trimmed[0] != '-' && (trimmed[0] < '0' || trimmed[0] > '9')) {
		return false
	}
	var value float64
	return json.Unmarshal(trimmed, &value) == nil && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func installationCountsErrorLogValue(err error) string {
	if errors.Is(err, errInvalidInstallationMetrics) {
		return "invalid-response"
	}
	return "unavailable"
}
