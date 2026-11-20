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
	Checked    bool   `json:"checked"`
	Message    string `json:"message"`
}

type discoveryEventPayload struct {
	RequestID         uint64               `json:"requestId"`
	Query             string               `json:"query"`
	DiscoverySequence uint64               `json:"discoverySequence"`
	Rows              []rowDTO             `json:"rows"`
	Health            []discoveryHealthDTO `json:"health"`
	BrowseHealth      []discoveryHealthDTO `json:"browseHealth"`
	Loading           bool                 `json:"loading"`
	BrowseLoading     bool                 `json:"browseLoading"`
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
	nextGeneration     uint64
	browse             discoverySlot
}

type discoverySlot struct {
	outcomes           []marketplace.DiscoveryOutcome
	rows               []session.Row
	health             []discoveryHealthDTO
	cancel             context.CancelFunc
	generation         uint64
	snapshotGeneration session.RequestGeneration
	configKey          string
	loading            bool
}

type discoveryLaunch struct {
	ctx        context.Context
	cancel     context.CancelFunc
	effective  config.EffectiveConfig
	config     *config.Config
	configKey  string
	query      string
	requestID  uint64
	generation uint64
	slot       string
	debounce   bool
	refresh    bool
	next       *discoveryLaunch
}

type discoveryRequest struct {
	query     string
	requestID uint64
}

func (a *API) acceptSnapshotStateLocked(snapshot session.Snapshot, refreshBrowse bool) *discoveryLaunch {
	a.snapshotRowsKnown = true
	switch snapshot.Stage {
	case session.SnapshotStageConfig:
		if snapshot.ProvisionalCfg != nil {
			a.provisionalRows = cloneSessionRows(snapshot.ProvisionalRows)
			return nil
		}
		if snapshot.MergedCfg != nil {
			return a.reconcileDiscoveryConfigLocked(snapshot.MergedCfg, snapshot.Generation, refreshBrowse)
		}
	case session.SnapshotStageInventory:
		a.authoritativeRows = cloneSessionRowsNonNil(snapshot.Rows)
		a.provisionalRows = nil
		if snapshot.MergedCfg != nil {
			return a.reconcileDiscoveryConfigLocked(snapshot.MergedCfg, snapshot.Generation, refreshBrowse)
		}
		a.acceptedMergedCfg = nil
		a.acceptedConfigKey = ""
		a.disableDiscoveryLocked(snapshot.Generation)
	default:
		a.authoritativeRows = cloneSessionRowsNonNil(snapshot.Rows)
		a.provisionalRows = nil
		if snapshot.MergedCfg != nil {
			return a.reconcileDiscoveryConfigLocked(snapshot.MergedCfg, snapshot.Generation, refreshBrowse)
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
	previousQuery := a.discovery.query
	a.cancelSearchDiscoveryLocked()
	a.discovery.query = request.query
	a.discovery.requestID = request.requestID
	var launch *discoveryLaunch
	if request.query != "" {
		if request.query != previousQuery {
			a.discovery.health = []discoveryHealthDTO{}
			if previousQuery != "" {
				a.discovery.outcomes = nil
				a.discovery.rows = nil
			}
		}
		launch = a.beginDiscoveryLocked(ctx, cancel, session.EffectiveMarketplaceConfig(a.acceptedMergedCfg), a.acceptedConfigKey, request.query, request.requestID, true)
	} else {
		a.discovery.outcomes = nil
		a.discovery.rows = nil
		a.syncVisibleBrowseLocked()
	}
	a.publishDiscoveryLocked()
	endpointCount := len(a.acceptedMergedCfg.Marketplaces)
	a.mu.Unlock()

	logDiscoveryRequest(request.query, request.requestID, endpointCount)
	if launch != nil {
		a.runDiscovery(launch)
	}
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

func (a *API) reconcileDiscoveryConfigLocked(cfg *config.Config, snapshotGeneration session.RequestGeneration, refreshBrowse bool) *discoveryLaunch {
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
	changed := !a.discovery.started || previousKey != configKey
	if a.closed {
		a.discovery.configKey = configKey
		return nil
	}
	if !changed && !refreshBrowse {
		a.discovery.browse.snapshotGeneration = snapshotGeneration
		return nil
	}
	if changed {
		a.cancelDiscoveryLocked()
		a.retainConfiguredDiscoveryLocked(cfg)
	} else {
		a.cancelBrowseDiscoveryLocked()
	}
	a.discovery.configKey = configKey
	launch := a.startDiscoveryWorkerLocked(effective, configKey, snapshotGeneration, refreshBrowse)
	if changed && a.discovery.query != "" && launch != nil {
		launch.next = a.startSearchDiscoveryWorkerLocked(
			effective, configKey, snapshotGeneration, a.discovery.query, a.discovery.requestID,
		)
	}
	return launch
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
	a.discovery.browse = discoverySlot{health: []discoveryHealthDTO{}}
	a.discovery.health = []discoveryHealthDTO{}
}

func (a *API) cancelDiscoveryLocked() {
	a.cancelSearchDiscoveryLocked()
	a.cancelBrowseDiscoveryLocked()
}

func (a *API) cancelSearchDiscoveryLocked() {
	if a.discovery.cancel != nil {
		a.discovery.cancel()
		a.discovery.cancel = nil
	}
	a.requests.Cancel(session.RequestDiscovery)
	a.discovery.generation = 0
}

func (a *API) cancelBrowseDiscoveryLocked() {
	if a.discovery.browse.cancel != nil {
		a.discovery.browse.cancel()
		a.discovery.browse.cancel = nil
	}
	a.discovery.browse.generation = 0
	a.discovery.browse.loading = false
}

func (a *API) startDiscoveryWorkerLocked(effective config.EffectiveConfig, configKey string, snapshotGeneration session.RequestGeneration, refresh bool) *discoveryLaunch {
	if a.closed {
		return nil
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.discovery.nextGeneration++
	a.discovery.browse.cancel = cancel
	a.discovery.browse.generation = a.discovery.nextGeneration
	a.discovery.browse.configKey = configKey
	a.discovery.browse.snapshotGeneration = snapshotGeneration
	a.discovery.browse.loading = true
	a.discovery.snapshotGeneration = snapshotGeneration
	a.discovery.started = true
	a.discovery.browse.health = discoveryHealthForConfig(a.acceptedMergedCfg, a.discovery.browse.outcomes, "")
	a.syncVisibleBrowseLocked()
	a.wg.Add(1)
	return &discoveryLaunch{
		ctx: ctx, cancel: cancel, effective: effective, config: a.acceptedMergedCfg,
		configKey: configKey, query: "", requestID: a.discovery.requestID,
		generation: a.discovery.nextGeneration, slot: discoveryBrowseSlot, refresh: refresh,
	}
}

func (a *API) startSearchDiscoveryWorkerLocked(effective config.EffectiveConfig, configKey string, snapshotGeneration session.RequestGeneration, query string, requestID uint64) *discoveryLaunch {
	ctx, cancel := context.WithCancel(a.ctx)
	launch := a.beginDiscoveryLocked(ctx, cancel, effective, configKey, query, requestID, false)
	a.discovery.snapshotGeneration = snapshotGeneration
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
	return &discoveryLaunch{
		ctx: ctx, cancel: cancel, effective: effective, config: a.acceptedMergedCfg,
		configKey: configKey, query: query, requestID: requestID, generation: uint64(generation),
		slot: discoverySearchSlot, debounce: debounce,
	}
}

func (a *API) retainConfiguredDiscoveryLocked(cfg *config.Config) {
	endpoints := make(map[string]struct{}, len(cfg.Marketplaces))
	for _, endpoint := range cfg.Marketplaces {
		if key, ok := discoveryEndpointKey(endpoint); ok {
			endpoints[key] = struct{}{}
		}
	}
	filter := func(slot *discoverySlot) {
		outcomes := make([]marketplace.DiscoveryOutcome, 0, len(slot.outcomes))
		for _, outcome := range slot.outcomes {
			if key, ok := discoveryEndpointKey(outcome.Endpoint); ok {
				if _, retained := endpoints[key]; retained {
					outcomes = append(outcomes, outcome)
				}
			}
		}
		rows := make([]session.Row, 0, len(slot.rows))
		for _, row := range slot.rows {
			if row.MarketplaceDiscovery == nil {
				continue
			}
			if key, ok := discoveryEndpointKey(row.MarketplaceDiscovery.Endpoint); ok {
				if _, retained := endpoints[key]; retained {
					rows = append(rows, row)
				}
			}
		}
		slot.outcomes, slot.rows = outcomes, rows
	}
	browse := discoverySlot{outcomes: a.discovery.browse.outcomes, rows: a.discovery.browse.rows}
	search := discoverySlot{outcomes: a.discovery.outcomes, rows: a.discovery.rows}
	filter(&browse)
	filter(&search)
	a.discovery.browse.outcomes, a.discovery.browse.rows = browse.outcomes, browse.rows
	a.discovery.outcomes, a.discovery.rows = search.outcomes, search.rows
	a.discovery.browse.health = discoveryHealthForConfig(cfg, a.discovery.browse.outcomes, "")
	a.discovery.health = discoveryHealthForConfig(cfg, a.discovery.outcomes, "")
	a.syncVisibleBrowseLocked()
}

const (
	discoveryBrowseSlot = "browse"
	discoverySearchSlot = "search"
)

func (a *API) syncVisibleBrowseLocked() {
	if a.discovery.query == "" {
		a.discovery.outcomes = slices.Clone(a.discovery.browse.outcomes)
		a.discovery.rows = slices.Clone(a.discovery.browse.rows)
		a.discovery.loading = a.discovery.browse.loading
		a.discovery.health = append([]discoveryHealthDTO{}, a.discovery.browse.health...)
	}
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
			item.Checked = outcome.Err == nil
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
	next := launch.next
	launch.next = nil
	go func() {
		defer a.wg.Done()
		a.runDiscovery(launch)
	}()
	a.launchDiscovery(next)
}

func (a *API) runDiscovery(launch *discoveryLaunch) {
	defer launch.cancel()
	options := marketplace.DiscoveryOptions{
		Limit: marketplace.DiscoveryPageLimit, Timeout: marketplace.DefaultSearchTimeout,
		Refresh: launch.refresh,
	}
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
	if a.closed || !a.discovery.enabled || a.currentG == 0 || a.acceptedConfigKey != launch.configKey {
		return false
	}
	if launch.slot == discoveryBrowseSlot {
		slot := &a.discovery.browse
		return slot.generation == launch.generation && slot.configKey == launch.configKey &&
			slot.snapshotGeneration == a.currentG
	}
	return a.requests.Active(session.RequestDiscovery) == session.RequestGeneration(launch.generation) &&
		a.discovery.generation == session.RequestGeneration(launch.generation) &&
		a.discovery.snapshotGeneration == a.currentG && a.discovery.configKey == launch.configKey &&
		a.discovery.query == launch.query && a.discovery.requestID == launch.requestID
}

func (a *API) updateDiscovery(launch *discoveryLaunch, outcomes []marketplace.DiscoveryOutcome, loading bool, requestFailure string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.discoveryLaunchCurrentLocked(launch) {
		return
	}
	if launch.slot == discoveryBrowseSlot {
		a.discovery.browse.outcomes = slices.Clone(outcomes)
		a.discovery.browse.rows = session.DiscoveryRows(outcomes)
		a.discovery.browse.loading = loading
		a.discovery.browse.health = discoveryHealthForConfig(a.acceptedMergedCfg, outcomes, requestFailure)
		a.syncVisibleBrowseLocked()
	} else {
		a.discovery.outcomes = slices.Clone(outcomes)
		a.discovery.rows = session.DiscoveryRows(outcomes)
		a.discovery.health = discoveryHealthForConfig(a.acceptedMergedCfg, outcomes, requestFailure)
		a.discovery.loading = loading
	}
	a.publishDiscoveryLocked()
}

func (a *API) completeDiscovery(launch *discoveryLaunch, outcomes []marketplace.DiscoveryOutcome, requestFailure string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.discoveryLaunchCurrentLocked(launch) {
		return
	}
	if launch.slot == discoveryBrowseSlot {
		a.discovery.browse.outcomes = slices.Clone(outcomes)
		a.discovery.browse.rows = session.DiscoveryRows(outcomes)
		a.discovery.browse.loading = false
		a.discovery.browse.cancel = nil
		a.discovery.browse.health = discoveryHealthForConfig(a.acceptedMergedCfg, outcomes, requestFailure)
		a.syncVisibleBrowseLocked()
	} else {
		a.discovery.outcomes = slices.Clone(outcomes)
		a.discovery.rows = session.DiscoveryRows(outcomes)
		a.discovery.health = discoveryHealthForConfig(a.acceptedMergedCfg, outcomes, requestFailure)
		a.discovery.loading = false
		a.discovery.cancel = nil
		a.requests.Cancel(session.RequestDiscovery)
	}
	a.publishDiscoveryLocked()
}

func (a *API) finishCancelledDiscovery(launch *discoveryLaunch) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.discoveryLaunchCurrentLocked(launch) {
		return
	}
	if launch.slot == discoveryBrowseSlot {
		a.discovery.browse.loading = false
		a.discovery.browse.cancel = nil
		a.syncVisibleBrowseLocked()
	} else {
		a.discovery.loading = false
		a.discovery.cancel = nil
		a.requests.Cancel(session.RequestDiscovery)
	}
	a.publishDiscoveryLocked()
}

func (a *API) publishDiscoveryLocked() {
	a.syncVisibleBrowseLocked()
	rows := session.MergeDiscoveryRows(a.authoritativeRows, a.provisionalRows, a.discovery.rows)
	projected := make([]rowDTO, 0, len(rows))
	for _, row := range rows {
		projected = append(projected, a.resourceRowPresentation(row))
	}
	health := append([]discoveryHealthDTO{}, a.discovery.health...)
	browseHealth := append([]discoveryHealthDTO{}, a.discovery.browse.health...)
	payload := discoveryEventPayload{
		RequestID: a.discovery.requestID, Query: a.discovery.query, DiscoverySequence: a.discovery.sequence + 1,
		Rows: projected, Health: health, BrowseHealth: browseHealth, Loading: a.discovery.loading,
		BrowseLoading: a.discovery.browse.loading,
	}
	a.discovery.sequence = payload.DiscoverySequence
	if a.current != nil {
		current := *a.current
		current.Rows = projected
		current.DiscoveryEnabled = a.discovery.enabled
		current.DiscoveryHealth = append([]discoveryHealthDTO{}, health...)
		current.DiscoveryBrowseHealth = append([]discoveryHealthDTO{}, browseHealth...)
		current.DiscoveryQuery = a.discovery.query
		current.DiscoveryRequestID = a.discovery.requestID
		current.DiscoverySequence = a.discovery.sequence
		current.DiscoveryLoading = a.discovery.loading
		current.DiscoveryBrowseLoading = a.discovery.browse.loading
		a.current = &current
	}
	a.emitLocked("discovery", a.currentG, nil, &payload)
}

func logDiscoveryRequest(query string, requestID uint64, endpointCount int) {
	slog.Info("localapi Marketplace discovery accepted", "request_id", requestID, "query_empty", query == "", "endpoints", endpointCount)
}
