package localapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/session"
)

const (
	discoveryEndpointFailureMessage = "Marketplace search failed; cached results were retained when available."
	discoveryRequestFailureMessage  = "Marketplace discovery failed; check the endpoint and retry."
)

type discoveryHealthDTO struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Total      int    `json:"total"`
	Truncated  bool   `json:"truncated"`
	CacheState string `json:"cacheState"`
	Message    string `json:"message"`
}

type discoveryEventPayload struct {
	RequestID         uint64               `json:"requestId"`
	Query             string               `json:"query"`
	DiscoverySequence uint64               `json:"discoverySequence"`
	Rows              []rowDTO             `json:"rows"`
	Health            []discoveryHealthDTO `json:"health"`
	Loading           bool                 `json:"loading"`
}

type discoveryState struct {
	enabled            bool
	started            bool
	query              string
	requestID          uint64
	sequence           uint64
	loading            bool
	health             []discoveryHealthDTO
	outcomes           []marketplace.DiscoveryOutcome
	rows               []session.Row
	cancel             context.CancelFunc
	generation         session.RequestGeneration
	snapshotGeneration session.RequestGeneration
	configKey          string
}

type discoveryLaunch struct {
	ctx        context.Context
	cancel     context.CancelFunc
	effective  config.EffectiveConfig
	config     *config.Config
	configKey  string
	query      string
	requestID  uint64
	generation session.RequestGeneration
	debounce   bool
}

type discoveryRequest struct {
	query     string
	requestID uint64
}

func (a *API) acceptSnapshotStateLocked(snapshot session.Snapshot) *discoveryLaunch {
	a.snapshotRowsKnown = true
	switch snapshot.Stage {
	case session.SnapshotStageConfig:
		if snapshot.ProvisionalCfg != nil {
			a.provisionalRows = cloneSessionRows(snapshot.ProvisionalRows)
			return nil
		}
		if snapshot.MergedCfg != nil {
			return a.reconcileDiscoveryConfigLocked(snapshot.MergedCfg, snapshot.Generation)
		}
	case session.SnapshotStageInventory:
		a.authoritativeRows = cloneSessionRowsNonNil(snapshot.Rows)
		a.provisionalRows = nil
		if snapshot.MergedCfg != nil {
			return a.reconcileDiscoveryConfigLocked(snapshot.MergedCfg, snapshot.Generation)
		}
		a.acceptedMergedCfg = nil
		a.acceptedConfigKey = ""
		a.disableDiscoveryLocked(snapshot.Generation)
	default:
		a.authoritativeRows = cloneSessionRowsNonNil(snapshot.Rows)
		a.provisionalRows = nil
		if snapshot.MergedCfg != nil {
			return a.reconcileDiscoveryConfigLocked(snapshot.MergedCfg, snapshot.Generation)
		}
		if a.acceptedMergedCfg == nil {
			a.disableDiscoveryLocked(snapshot.Generation)
		} else {
			a.discovery.snapshotGeneration = snapshot.Generation
		}
	}
	return nil
}

func parseDiscoveryRequest(r *http.Request) (discoveryRequest, bool) {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return discoveryRequest{}, false
	}
	var request discoveryRequest
	querySeen, requestIDSeen := false, false
	if !decodeStrictObject(r, func(decoder *json.Decoder, key string) bool {
		switch key {
		case "query":
			if querySeen {
				return false
			}
			querySeen = true
			return decodeStrictString(decoder, &request.query)
		case "requestId":
			if requestIDSeen {
				return false
			}
			requestIDSeen = true
			return decoder.Decode(&request.requestID) == nil && request.requestID > 0
		default:
			return false
		}
	}) {
		return discoveryRequest{}, false
	}
	return request, querySeen && requestIDSeen
}

func (a *API) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	if !a.userOnly {
		writeError(w, http.StatusBadRequest, codeNotSupported)
		return
	}
	request, ok := parseDiscoveryRequest(r)
	if !ok {
		writeError(w, http.StatusBadRequest, codeInvalidRequest)
		return
	}
	if r.Context().Err() != nil {
		return
	}
	ctx, cancel := context.WithCancel(a.ctx)
	stop := context.AfterFunc(r.Context(), cancel)
	defer func() {
		stop()
		cancel()
	}()

	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		writeError(w, http.StatusServiceUnavailable, codeClosed)
		return
	}
	if request.requestID < a.discovery.requestID {
		a.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if request.requestID == a.discovery.requestID {
		if request.query == a.discovery.query {
			a.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		a.mu.Unlock()
		writeError(w, http.StatusBadRequest, codeInvalidRequest)
		return
	}
	if !a.discovery.enabled || a.acceptedMergedCfg == nil || a.current == nil || a.currentG == 0 {
		a.mu.Unlock()
		writeError(w, http.StatusBadRequest, codeNotReady)
		return
	}
	if ctx.Err() != nil {
		a.mu.Unlock()
		return
	}
	a.cancelDiscoveryLocked()
	a.discovery.query = request.query
	a.discovery.requestID = request.requestID
	launch := a.beginDiscoveryLocked(ctx, cancel, session.EffectiveMarketplaceConfig(a.acceptedMergedCfg), a.acceptedConfigKey, request.query, request.requestID, true)
	a.publishDiscoveryLocked()
	a.mu.Unlock()

	logDiscoveryRequest(request.query, request.requestID, len(launch.config.Marketplaces))
	a.runDiscovery(launch)
	w.WriteHeader(http.StatusNoContent)
}

func cloneSessionRows(rows []session.Row) []session.Row {
	return slices.Clone(rows)
}

func cloneSessionRowsNonNil(rows []session.Row) []session.Row {
	cloned := slices.Clone(rows)
	if cloned == nil {
		return []session.Row{}
	}
	return cloned
}

func discoveryConfiguration(cfg *config.Config, userOnly bool) (config.EffectiveConfig, string, bool) {
	if !userOnly || cfg == nil || len(cfg.Marketplaces) == 0 || len(config.ValidateMarketplaceEntries(cfg.Marketplaces)) > 0 {
		return config.EffectiveConfig{}, "", false
	}
	effective := session.EffectiveMarketplaceConfig(cfg)
	if err := config.ValidateMarketplaceURLs(cfg.Marketplaces, effective.AllowedMarketplaces); err != nil {
		return config.EffectiveConfig{}, "", false
	}
	identities := make([]string, 0, len(cfg.Marketplaces))
	for _, endpoint := range cfg.Marketplaces {
		key, ok := discoveryEndpointKey(endpoint)
		if !ok {
			return config.EffectiveConfig{}, "", false
		}
		identities = append(identities, key+"\x00"+endpoint.Alias)
	}
	slices.Sort(identities)
	allowed := slices.Clone(effective.AllowedMarketplaces)
	slices.Sort(allowed)
	allowState := "nil"
	if cfg.AllowedMarketplaces != nil {
		allowState = "set"
	}
	return effective, strings.Join(identities, "\x01") + "\x02" + allowState + "\x00" + strings.Join(allowed, "\x01"), true
}

func discoveryEndpointKey(endpoint config.MarketplaceEntry) (string, bool) {
	url, err := marketplace.NormalizeMarketplaceURL(endpoint.URL)
	if err != nil || endpoint.InstanceID == "" {
		return "", false
	}
	return url + "\x00" + endpoint.InstanceID, true
}

func (a *API) reconcileDiscoveryConfigLocked(cfg *config.Config, snapshotGeneration session.RequestGeneration) *discoveryLaunch {
	effective, configKey, valid := discoveryConfiguration(cfg, a.userOnly)
	if !valid {
		a.acceptedMergedCfg = nil
		a.acceptedConfigKey = ""
		a.disableDiscoveryLocked(snapshotGeneration)
		return nil
	}

	previousKey := a.discovery.configKey
	a.acceptedMergedCfg = cfg
	a.acceptedConfigKey = configKey
	a.discovery.enabled = true
	a.discovery.snapshotGeneration = snapshotGeneration
	a.retainConfiguredDiscoveryLocked(cfg)
	if a.closed || (a.discovery.started && previousKey == configKey) {
		a.discovery.configKey = configKey
		return nil
	}
	a.cancelDiscoveryLocked()
	a.discovery.configKey = configKey
	return a.startDiscoveryWorkerLocked(effective, configKey, snapshotGeneration, a.discovery.query, a.discovery.requestID)
}

func (a *API) disableDiscoveryLocked(snapshotGeneration session.RequestGeneration) {
	a.cancelDiscoveryLocked()
	a.discovery.enabled = false
	a.discovery.started = false
	a.discovery.configKey = ""
	a.discovery.snapshotGeneration = snapshotGeneration
	a.discovery.loading = false
	a.discovery.outcomes = nil
	a.discovery.rows = nil
	a.discovery.health = []discoveryHealthDTO{}
}

func (a *API) cancelDiscoveryLocked() {
	if a.discovery.cancel != nil {
		a.discovery.cancel()
		a.discovery.cancel = nil
	}
	a.requests.Cancel(session.RequestDiscovery)
}

func (a *API) startDiscoveryWorkerLocked(effective config.EffectiveConfig, configKey string, snapshotGeneration session.RequestGeneration, query string, requestID uint64) *discoveryLaunch {
	if a.closed {
		return nil
	}
	ctx, cancel := context.WithCancel(a.ctx)
	launch := a.beginDiscoveryLocked(ctx, cancel, effective, configKey, query, requestID, false)
	a.discovery.snapshotGeneration = snapshotGeneration
	a.discovery.health = discoveryHealthForConfig(a.acceptedMergedCfg, a.discovery.outcomes, "")
	a.wg.Add(1)
	return launch
}

func (a *API) beginDiscoveryLocked(ctx context.Context, cancel context.CancelFunc, effective config.EffectiveConfig, configKey, query string, requestID uint64, debounce bool) *discoveryLaunch {
	generation := a.requests.Begin(session.RequestDiscovery)
	a.discovery.cancel = cancel
	a.discovery.configKey = configKey
	a.discovery.generation = generation
	a.discovery.snapshotGeneration = a.currentG
	a.discovery.query = query
	a.discovery.requestID = requestID
	a.discovery.loading = true
	a.discovery.started = true
	return &discoveryLaunch{
		ctx: ctx, cancel: cancel, effective: effective, config: a.acceptedMergedCfg,
		configKey: configKey, query: query, requestID: requestID, generation: generation, debounce: debounce,
	}
}

func (a *API) retainConfiguredDiscoveryLocked(cfg *config.Config) {
	endpoints := make(map[string]struct{}, len(cfg.Marketplaces))
	for _, endpoint := range cfg.Marketplaces {
		if key, ok := discoveryEndpointKey(endpoint); ok {
			endpoints[key] = struct{}{}
		}
	}
	outcomes := make([]marketplace.DiscoveryOutcome, 0, len(a.discovery.outcomes))
	for _, outcome := range a.discovery.outcomes {
		if key, ok := discoveryEndpointKey(outcome.Endpoint); ok {
			if _, retained := endpoints[key]; retained {
				outcomes = append(outcomes, outcome)
			}
		}
	}
	rows := make([]session.Row, 0, len(a.discovery.rows))
	for _, row := range a.discovery.rows {
		if row.MarketplaceDiscovery == nil {
			continue
		}
		if key, ok := discoveryEndpointKey(row.MarketplaceDiscovery.Endpoint); ok {
			if _, retained := endpoints[key]; retained {
				rows = append(rows, row)
			}
		}
	}
	a.discovery.outcomes = outcomes
	a.discovery.rows = rows
	a.discovery.health = discoveryHealthForConfig(cfg, outcomes, "")
}

func discoveryHealthForConfig(cfg *config.Config, outcomes []marketplace.DiscoveryOutcome, requestFailure string) []discoveryHealthDTO {
	health := make([]discoveryHealthDTO, 0)
	if cfg == nil {
		return health
	}
	outcomeByEndpoint := make(map[string]marketplace.DiscoveryOutcome, len(outcomes))
	for _, outcome := range outcomes {
		if key, ok := discoveryEndpointKey(outcome.Endpoint); ok {
			outcomeByEndpoint[key] = outcome
		}
	}
	seen := make(map[string]struct{}, len(cfg.Marketplaces))
	for _, endpoint := range cfg.Marketplaces {
		key, ok := discoveryEndpointKey(endpoint)
		if !ok {
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		endpoint.URL = strings.Split(key, "\x00")[0]
		identity := sha256.Sum256([]byte(key))
		item := discoveryHealthDTO{
			ID:         hex.EncodeToString(identity[:]),
			Label:      session.SanitizeTerminalData(session.MarketplaceEndpointDisplay(endpoint), session.TerminalSingleLine),
			CacheState: string(marketplace.DiscoveryCacheMiss),
		}
		if outcome, found := outcomeByEndpoint[key]; found {
			item.Total = outcome.Total
			item.Truncated = outcome.Truncated
			if outcome.CacheState != "" {
				item.CacheState = string(outcome.CacheState)
			}
			if outcome.Err != nil {
				item.Message = discoveryEndpointFailureMessage
			}
		}
		if requestFailure != "" {
			item.Message = requestFailure
		}
		item.Message = session.SanitizeTerminalData(item.Message, session.TerminalSingleLine)
		health = append(health, item)
	}
	return health
}

func (a *API) launchDiscovery(launch *discoveryLaunch) {
	if launch == nil {
		return
	}
	go func() {
		defer a.wg.Done()
		a.runDiscovery(launch)
	}()
}

func (a *API) runDiscovery(launch *discoveryLaunch) {
	defer launch.cancel()
	options := marketplace.DiscoveryOptions{Limit: marketplace.DiscoveryPageLimit, Timeout: marketplace.DefaultSearchTimeout}
	outcomes, needsNetwork, err := marketplace.CachedDiscovery(launch.effective, launch.query, options)
	if launch.ctx.Err() != nil {
		a.finishCancelledDiscovery(launch)
		return
	}
	if err != nil {
		a.completeDiscovery(launch, nil, discoveryRequestFailureMessage)
		return
	}
	if !needsNetwork {
		a.completeDiscovery(launch, outcomes, "")
		return
	}
	a.updateDiscovery(launch, outcomes, true, "")
	if launch.debounce {
		timer := time.NewTimer(session.DiscoverySearchDebounce)
		defer timer.Stop()
		select {
		case <-launch.ctx.Done():
			a.finishCancelledDiscovery(launch)
			return
		case <-timer.C:
		}
	}
	updates, err := marketplace.DiscoverUpdates(launch.ctx, launch.effective, launch.query, options)
	if err != nil {
		a.completeDiscovery(launch, outcomes, discoveryRequestFailureMessage)
		return
	}
	latest := outcomes
	for update := range updates {
		latest = update
		if launch.ctx.Err() != nil {
			break
		}
		a.updateDiscovery(launch, update, true, "")
	}
	if launch.ctx.Err() != nil {
		a.finishCancelledDiscovery(launch)
		return
	}
	a.completeDiscovery(launch, latest, "")
}

func (a *API) discoveryLaunchCurrentLocked(launch *discoveryLaunch) bool {
	return !a.closed && a.discovery.enabled && a.currentG != 0 &&
		a.requests.Active(session.RequestDiscovery) == launch.generation &&
		a.discovery.generation == launch.generation &&
		a.discovery.snapshotGeneration == a.currentG &&
		a.discovery.configKey == launch.configKey &&
		a.discovery.query == launch.query && a.discovery.requestID == launch.requestID
}

func (a *API) updateDiscovery(launch *discoveryLaunch, outcomes []marketplace.DiscoveryOutcome, loading bool, requestFailure string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.discoveryLaunchCurrentLocked(launch) {
		return
	}
	a.discovery.outcomes = slices.Clone(outcomes)
	a.discovery.rows = session.DiscoveryRows(outcomes)
	a.discovery.health = discoveryHealthForConfig(a.acceptedMergedCfg, outcomes, requestFailure)
	a.discovery.loading = loading
	a.publishDiscoveryLocked()
}

func (a *API) completeDiscovery(launch *discoveryLaunch, outcomes []marketplace.DiscoveryOutcome, requestFailure string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.discoveryLaunchCurrentLocked(launch) {
		return
	}
	a.discovery.outcomes = slices.Clone(outcomes)
	a.discovery.rows = session.DiscoveryRows(outcomes)
	a.discovery.health = discoveryHealthForConfig(a.acceptedMergedCfg, outcomes, requestFailure)
	a.discovery.loading = false
	a.discovery.cancel = nil
	a.requests.Cancel(session.RequestDiscovery)
	a.publishDiscoveryLocked()
}

func (a *API) finishCancelledDiscovery(launch *discoveryLaunch) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.discoveryLaunchCurrentLocked(launch) {
		return
	}
	a.discovery.loading = false
	a.discovery.cancel = nil
	a.requests.Cancel(session.RequestDiscovery)
	a.publishDiscoveryLocked()
}

func (a *API) publishDiscoveryLocked() {
	rows := session.MergeDiscoveryRows(a.authoritativeRows, a.provisionalRows, a.discovery.rows)
	projected := make([]rowDTO, 0, len(rows))
	for _, row := range rows {
		projected = append(projected, rowPresentation(row))
	}
	health := append([]discoveryHealthDTO{}, a.discovery.health...)
	payload := discoveryEventPayload{
		RequestID: a.discovery.requestID, Query: a.discovery.query, DiscoverySequence: a.discovery.sequence + 1,
		Rows: projected, Health: health, Loading: a.discovery.loading,
	}
	a.discovery.sequence = payload.DiscoverySequence
	if a.current != nil {
		current := *a.current
		current.Rows = projected
		current.DiscoveryEnabled = a.discovery.enabled
		current.DiscoveryHealth = health
		current.DiscoveryQuery = a.discovery.query
		current.DiscoveryRequestID = a.discovery.requestID
		current.DiscoverySequence = a.discovery.sequence
		current.DiscoveryLoading = a.discovery.loading
		a.current = &current
	}
	a.emitLocked("discovery", a.currentG, nil, &payload)
}

func logDiscoveryRequest(query string, requestID uint64, endpointCount int) {
	slog.Info("localapi Marketplace discovery accepted", "request_id", requestID, "query_empty", query == "", "endpoints", endpointCount)
}
