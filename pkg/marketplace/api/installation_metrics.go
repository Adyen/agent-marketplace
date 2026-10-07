package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

const (
	installationMetricsTimeout     = 2 * time.Second
	installationMetricsMaxBody     = 8 << 20
	installationMetricsFreshness   = 5 * time.Minute
	installationMetricsFailureHold = 30 * time.Second
)

type installationMetricsReader struct {
	url           string
	labels        map[string]string
	tokenEnv      string
	marketplaceID store.MarketplaceID
	client        *http.Client
	now           func() time.Time

	mu               sync.Mutex
	cached           installationCounts
	cachedSignature  string
	cachedAt         time.Time
	hasCached        bool
	failureAt        time.Time
	failureSignature string
	hasFailure       bool
}

func newInstallationMetricsReader(metrics config.InstallationMetrics, marketplaceID store.MarketplaceID) *installationMetricsReader {
	return &installationMetricsReader{
		url:           strings.TrimRight(strings.TrimSpace(metrics.URL), "/"),
		labels:        metrics.Labels,
		tokenEnv:      strings.TrimSpace(metrics.BearerTokenEnv),
		marketplaceID: marketplaceID,
		client: &http.Client{
			Timeout: installationMetricsTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		now: time.Now,
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

func (r *installationMetricsReader) counts(ctx context.Context, registries map[store.RegistryID]store.Registry) (installationCounts, error) {
	signature := registryOriginsSignature(registries)

	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	if r.hasCached && r.cachedSignature == signature && now.Before(minTime(r.cachedAt.Add(installationMetricsFreshness), r.cached.ValidUntil)) {
		return r.cached, nil
	}
	if r.hasFailure && r.failureSignature == signature && now.Before(r.failureAt.Add(installationMetricsFailureHold)) {
		return installationCounts{}, errInstallationMetricsUnavailable
	}

	counts, err := r.fetch(ctx, registries, now)
	if err != nil {
		r.hasCached = false
		r.hasFailure = true
		r.failureAt = now
		r.failureSignature = signature
		return installationCounts{}, err
	}
	r.cached = counts
	r.cachedSignature = signature
	r.cachedAt = now
	r.hasCached = true
	r.hasFailure = false
	return counts, nil
}

func minTime(a, b time.Time) time.Time {
	if b.IsZero() || a.Before(b) {
		return a
	}
	return b
}

func (r *installationMetricsReader) fetch(ctx context.Context, registries map[store.RegistryID]store.Registry, evalTime time.Time) (installationCounts, error) {
	form := url.Values{
		"query": {installationMetricsQuery(r.labels)},
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
	return aggregateInstallationMetrics(samples, registries, r.marketplaceID, evalTime)
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
