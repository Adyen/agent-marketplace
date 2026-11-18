package tui

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/charmbracelet/x/ansi"
)

const marketplaceSearchDebounce = 250 * time.Millisecond

type discoveryLoadedMsg struct {
	generation requestGeneration
	sequence   uint64
	query      string
	browse     bool
	outcomes   []marketplace.DiscoveryOutcome
	updates    <-chan []marketplace.DiscoveryOutcome
	done       bool
	err        error
}

type discoveryDebounceMsg struct {
	generation requestGeneration
	query      string
}

type discoveryInstallMsg struct {
	operation operationID
	options   pipeline.InstallOptions
	prepared  *pipeline.PreparedMarketplaceInstall
	preview   string
	result    *pipeline.InstallResult
	err       error
}

func pipelineScope(s scope) pipeline.Scope {
	switch s {
	case scopeGlobal:
		return pipeline.ScopeUser
	case scopeLocal:
		return pipeline.ScopeLocal
	default:
		return pipeline.ScopeRepo
	}
}

func marketplaceDiscoveryInstallable(r row) bool {
	d := r.MarketplaceDiscovery
	return d != nil && (d.Item.Availability == mpapi.AvailabilityAvailable || d.Item.Availability == mpapi.AvailabilityOperatorExposed)
}

func discoveryInstallOptions(repoRoot string, r row, s scope) (pipeline.InstallOptions, error) {
	d := r.MarketplaceDiscovery
	if !marketplaceDiscoveryInstallable(r) {
		return pipeline.InstallOptions{}, errors.New("marketplace resource is not installable")
	}
	if d.Item.Revision == "" || d.Item.ContentDigest == "" {
		return pipeline.InstallOptions{}, errors.New("marketplace discovery did not provide an exact revision and digest")
	}
	registry := d.Item.RegistryID
	if registry == "" {
		registry = d.Item.RegistrySlug
	}
	locator := d.Endpoint.Alias + ":" + registry + "/" + d.Item.Kind + "/" + d.Item.Name + "@" + d.Item.ContentDigest
	selector := d.Endpoint.Alias
	if d.Endpoint.Alias == "" {
		locator = strings.TrimSuffix(d.Endpoint.URL, "/") + "/r/" + registry + "/" + d.Item.Kind + "/" + d.Item.Name + "/revisions/" + d.Item.ContentDigest
		selector = d.Endpoint.URL
	}
	kind, ok := skill.ParseKind(d.Item.Kind)
	if !ok {
		return pipeline.InstallOptions{}, fmt.Errorf("unsupported Marketplace resource kind %q", d.Item.Kind)
	}
	exact := &marketplace.MarketplaceLocator{Marketplace: selector, Alias: d.Endpoint.Alias, URL: d.Endpoint.URL, Registry: registry, Kind: d.Item.Kind, Name: d.Item.Name, Revision: d.Item.Revision, Digest: d.Item.ContentDigest}
	return pipeline.InstallOptions{RepoRoot: repoRoot, Args: []string{locator}, Kind: kind, HasKind: true, Scope: pipelineScope(s), ScopeSpecified: true, Marketplace: selector, ExactMarketplaceLocator: exact}, nil
}

var (
	prepareDiscoveryMarketplaceInstall = pipeline.PrepareMarketplaceInstall
	commitPreparedMarketplaceInstall   = pipeline.CommitPreparedMarketplaceInstall
)

func prepareDiscoveryInstall(ctx context.Context, operation operationID, options pipeline.InstallOptions) tea.Cmd {
	return func() tea.Msg {
		prepared, err := prepareDiscoveryMarketplaceInstall(ctx, options)
		preview := ""
		if prepared != nil {
			preview = prepared.Preview()
		}
		return discoveryInstallMsg{operation: operation, options: options, prepared: prepared, preview: preview, err: err}
	}
}

func commitDiscoveryInstall(ctx context.Context, operation operationID, options pipeline.InstallOptions, prepared *pipeline.PreparedMarketplaceInstall) tea.Cmd {
	return func() tea.Msg {
		result, err := commitPreparedMarketplaceInstall(ctx, prepared)
		return discoveryInstallMsg{operation: operation, options: options, result: result, err: err}
	}
}

func (m Model) startDiscoveryInstall(r row, s scope) (tea.Model, tea.Cmd) {
	options, err := discoveryInstallOptions(m.repoRoot, r, s)
	if err != nil {
		return m, func() tea.Msg { return toastMsg{text: err.Error(), duration: 3 * time.Second} }
	}
	options.DependencyReconcile = m.dependencyLifecycle.InstallReconcile
	operation, ok := m.beginOperation(operationMutation)
	if !ok {
		return m, operationBlockedCmd()
	}
	m.pane = paneList
	m.marketplaceInstallIsSource = false
	m.mutationPendingWarnings = nil
	m.mutationPendingReport = nil
	m.marketplaceInstallOptions = options
	m.marketplaceInstallOperation = operation
	m.beginOperationProgressFor(operation, "install", m.requests.active(requestSnapshot))
	return m, tea.Batch(
		prepareDiscoveryInstall(m.observedContext(ctxOrBackground(m.ctx)), operation, options),
		m.operationProgressCmd(),
	)
}

func (m *Model) finishMarketplaceInstallReload(generation requestGeneration, err error) tea.Cmd {
	if m.marketplaceInstallReload == 0 || generation < m.marketplaceInstallReload {
		return nil
	}
	m.clearOperationProgress()
	m.finishOperation(m.marketplaceInstallOperation)
	m.marketplaceInstallOperation = 0
	m.marketplaceInstallReload = 0
	if err != nil {
		m.recordOperationFailure(operationMutation, diagnosticMutation, err)
		message := "Marketplace install reload failed: " + err.Error()
		if m.marketplaceInstallIsSource {
			message = "Source install reload failed: " + err.Error()
		}
		m.marketplaceInstallIsSource = false
		m.finishMutationReview()
		m.pushToast(message, 5*time.Second)
		return toastExpiryCmd(5 * time.Second)
	}
	m.recordOperationSuccess(operationMutation)
	m.finishMutationReview()
	message := "Marketplace resource installed"
	if m.marketplaceInstallIsSource {
		count := len(m.marketplaceInstallOptions.Locators)
		message = fmt.Sprintf("Installed %d from %s %s", count, registryDisplayName(m.marketplaceInstallOptions.FromURL), pipeline.InstallScopeLabel(m.marketplaceInstallOptions.Scope))
		m.cancelInstallSourcePreview()
		m.installSource = installSourceModal{}
	}
	m.marketplaceInstallIsSource = false
	m.pushToast(message, 2*time.Second)
	return toastExpiryCmd(2 * time.Second)
}

func (m *Model) resizeMarketplaceConsentViewport(reset bool) {
	m.marketplaceConsentViewport.SetWidth(max(1, m.width))
	m.marketplaceConsentViewport.SetHeight(max(1, m.height-3))
	m.marketplaceConsentViewport.SetContent(ansi.Hardwrap(m.marketplaceInstallPreview, m.marketplaceConsentViewport.Width(), true))
	if reset {
		m.marketplaceConsentViewport.GotoTop()
	}
}

func (m *Model) setMarketplaceConsentPreview(preview string) {
	m.marketplaceInstallPreview = sanitizeTerminalData(preview, terminalMarkdownBody)
	m.resizeMarketplaceConsentViewport(true)
}

func (m *Model) clearMarketplaceConsent() {
	m.clearOperationProgress()
	m.finishOperation(m.marketplaceInstallOperation)
	m.marketplaceInstallOperation = 0
	m.marketplaceInstallIsSource = false
	m.marketplacePreparedInstall = nil
	m.marketplaceInstallPreview = ""
	m.marketplaceConsentViewport.SetContent("")
	m.marketplaceConsentViewport.GotoTop()
}

func renderMarketplaceConsent(m Model) string {
	return styleConfirmTitle.Render("Verified Marketplace install") + "\nInstall this exact verified revision?\n" + m.marketplaceConsentViewport.View()
}

func (m Model) handleMarketplaceConsentKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.pane = paneList
		return m, commitDiscoveryInstall(m.observedContext(ctxOrBackground(m.ctx)), m.marketplaceInstallOperation, m.marketplaceInstallOptions, m.marketplacePreparedInstall)
	case "n", "N", "esc":
		m.pane = paneList
		m.clearMarketplaceConsent()
		return m, nil
	}
	if key.Matches(msg, m.keys.Top) {
		m.marketplaceConsentViewport.GotoTop()
		return m, nil
	}
	if key.Matches(msg, m.keys.Bottom) {
		m.marketplaceConsentViewport.GotoBottom()
		return m, nil
	}
	var cmd tea.Cmd
	m.marketplaceConsentViewport, cmd = m.marketplaceConsentViewport.Update(msg)
	return m, cmd
}

func marketplaceEndpointDisplay(endpoint config.MarketplaceEntry) string {
	if endpoint.Alias != "" {
		return endpoint.Alias
	}
	if normalized, err := marketplace.NormalizeMarketplaceURL(endpoint.URL); err == nil {
		return normalized
	}
	return endpoint.URL
}

func effectiveMarketplaceConfig(cfg *config.Config) config.EffectiveConfig {
	ec := config.EffectiveConfig{Merged: cfg}
	if cfg != nil && cfg.AllowedMarketplaces != nil {
		ec.AllowedMarketplaces = slices.Clone(*cfg.AllowedMarketplaces)
	}
	return ec
}

func marketplaceKindQuery(query string) (ir.ResourceKind, string, bool) {
	normalized := strings.TrimSpace(query)
	if normalized == "" || strings.IndexFunc(normalized, unicode.IsSpace) >= 0 || strings.EqualFold(normalized, "all") {
		return 0, "", false
	}
	kind, ok := skill.ParseKind(normalized)
	if !ok || kind == ir.KindPlugin {
		return 0, "", false
	}
	apiKind := marketplaceProtocolKind(kind)
	if !mpapi.IsMarketplaceKind(apiKind) {
		return 0, "", false
	}
	return kind, apiKind, true
}

func loadMarketplaceDiscovery(ctx context.Context, cfg *config.Config, query string, generation requestGeneration) tea.Cmd {
	return loadMarketplaceDiscoveryRequest(ctx, cfg, query, generation, false)
}

func loadMarketplaceBrowse(ctx context.Context, cfg *config.Config, generation requestGeneration) tea.Cmd {
	return loadMarketplaceDiscoveryRequest(ctx, cfg, "", generation, true)
}

func loadMarketplaceDiscoveryRequest(ctx context.Context, cfg *config.Config, query string, generation requestGeneration, browse bool) tea.Cmd {
	return func() tea.Msg {
		_, includeKind, _ := marketplaceKindQuery(query)
		updates, err := marketplace.DiscoverUpdates(ctx, effectiveMarketplaceConfig(cfg), query, marketplace.DiscoveryOptions{Limit: marketplace.DiscoveryPageLimit, IncludeKind: includeKind})
		if err != nil {
			return discoveryLoadedMsg{generation: generation, query: query, browse: browse, done: true, err: err}
		}
		return waitMarketplaceUpdate(generation, 1, query, browse, updates)()
	}
}

func waitMarketplaceDiscoveryUpdate(generation requestGeneration, sequence uint64, query string, updates <-chan []marketplace.DiscoveryOutcome) tea.Cmd {
	return waitMarketplaceUpdate(generation, sequence, query, false, updates)
}

func waitMarketplaceBrowseUpdate(generation requestGeneration, sequence uint64, updates <-chan []marketplace.DiscoveryOutcome) tea.Cmd {
	return waitMarketplaceUpdate(generation, sequence, "", true, updates)
}

func waitMarketplaceUpdate(generation requestGeneration, sequence uint64, query string, browse bool, updates <-chan []marketplace.DiscoveryOutcome) tea.Cmd {
	return func() tea.Msg {
		outcomes, ok := <-updates
		return discoveryLoadedMsg{generation: generation, sequence: sequence, query: query, browse: browse, outcomes: outcomes, updates: updates, done: !ok}
	}
}

func (m *Model) cancelMarketplaceDiscovery() {
	if m.discoveryCancel != nil {
		m.discoveryCancel()
		m.discoveryCancel = nil
	}
	m.cancelRequest(requestDiscovery)
	m.discoverySequence = 0
	m.discoveryLoading = false
}

func (m *Model) cancelMarketplaceBrowse() {
	if m.browseCancel != nil {
		m.browseCancel()
		m.browseCancel = nil
	}
	m.cancelRequest(requestBrowse)
	m.browseSequence = 0
	m.browseLoading = false
}

func (m *Model) cancelMarketplaceLoads() {
	m.cancelMarketplaceDiscovery()
	m.cancelMarketplaceBrowse()
}

func (m *Model) clearMarketplaceBrowse() {
	m.cancelMarketplaceBrowse()
	m.browseKey = ""
	m.browseFinished = false
	m.browseDiscoveryRows = nil
	m.browseOutcomes = nil
	m.browseErrs = nil
	m.browsePending = nil
}

func (m *Model) clearMarketplaceDiscovery(rebuildAuthoritative bool) {
	m.cancelMarketplaceDiscovery()
	m.clearMarketplaceBrowse()
	m.discoveryRows = nil
	m.discoveryOutcomes = nil
	m.discoveryErrs = nil
	if rebuildAuthoritative {
		m.mergeDiscoveryRows()
	}
}

// discoveryEndpointMatcher never treats a missing pinned identity as a wildcard.
// Only an exactly empty, resource-free declaration can match a live-acquired ID,
// and even then the canonical configured transport URL must remain the same.
func discoveryEndpointMatcher(entries []config.MarketplaceEntry, allowed []string) func(config.MarketplaceEntry) bool {
	configured := make(map[string]struct{}, len(entries))
	unacquired := make(map[string]struct{})
	for _, endpoint := range entries {
		url, err := marketplace.NormalizeMarketplaceURL(endpoint.URL)
		if err != nil {
			continue
		}
		if endpoint.InstanceID == "" && len(endpoint.Resources) == 0 {
			unacquired[url] = struct{}{}
		} else if strings.TrimSpace(endpoint.InstanceID) != "" {
			configured[url+"\x00"+endpoint.InstanceID] = struct{}{}
		}
	}
	return func(endpoint config.MarketplaceEntry) bool {
		if config.ValidateMarketplaceURLs([]config.MarketplaceEntry{endpoint}, allowed) != nil {
			return false
		}
		url, err := marketplace.NormalizeMarketplaceURL(endpoint.URL)
		if err != nil || (endpoint.InstanceID != "" && strings.TrimSpace(endpoint.InstanceID) == "") {
			return false
		}
		if _, ok := configured[url+"\x00"+endpoint.InstanceID]; ok {
			return true
		}
		_, ok := unacquired[url]
		return ok
	}
}

func (m *Model) beginMarketplaceDiscovery(query string, debounce bool) tea.Cmd {
	if strings.TrimSpace(query) == "" {
		m.cancelMarketplaceDiscovery()
		return m.beginMarketplaceBrowse(false)
	}
	if m.mergedCfg == nil || len(m.mergedCfg.Marketplaces) == 0 {
		rebuildAuthoritative := len(m.discoveryRows) > 0 || len(m.browseDiscoveryRows) > 0 || m.authoritativeRows != nil
		m.clearMarketplaceDiscovery(rebuildAuthoritative)
		return nil
	}
	isConfigured := discoveryEndpointMatcher(m.mergedCfg.Marketplaces, effectiveMarketplaceConfig(m.mergedCfg).AllowedMarketplaces)
	before := len(m.discoveryRows)
	m.discoveryRows = filterMarketplaceRows(m.discoveryRows, isConfigured)
	if len(m.discoveryRows) != before {
		m.mergeDiscoveryRows()
	}
	retainedOutcomes := m.discoveryOutcomes[:0]
	for _, outcome := range m.discoveryOutcomes {
		if isConfigured(outcome.Endpoint) {
			retainedOutcomes = append(retainedOutcomes, outcome)
		}
	}
	m.discoveryOutcomes = retainedOutcomes
	m.rebuildDiscoveryErrors()
	m.cancelMarketplaceDiscovery()
	if !debounce {
		allowedMarketplaces := effectiveMarketplaceConfig(m.mergedCfg).AllowedMarketplaces
		if err := config.ValidateMarketplaceURLs(m.mergedCfg.Marketplaces, allowedMarketplaces); err != nil {
			var blocked *config.MarketplaceURLBlockedError
			if errors.As(err, &blocked) {
				for _, endpoint := range m.mergedCfg.Marketplaces {
					if config.ValidateMarketplaceURLs([]config.MarketplaceEntry{endpoint}, allowedMarketplaces) == nil {
						err = nil
						break
					}
				}
			}
			if err != nil {
				m.cancelMarketplaceDiscovery()
				m.discoveryRows = nil
				m.discoveryOutcomes = nil
				m.mergeDiscoveryRows()
				m.discoveryErrs = []error{err}
				return nil
			}
		}
	}
	generation := m.beginRequest(requestDiscovery)
	m.discoveryLoading = true
	if debounce {
		return tea.Tick(marketplaceSearchDebounce, func(time.Time) tea.Msg {
			return discoveryDebounceMsg{generation: generation, query: query}
		})
	}
	ctx, cancel := context.WithCancel(ctxOrBackground(m.ctx))
	m.discoveryCancel = cancel
	return loadMarketplaceDiscovery(ctx, m.mergedCfg, query, generation)
}

func filterMarketplaceRows(rows []row, matches func(config.MarketplaceEntry) bool) []row {
	kept := rows[:0]
	for _, candidate := range rows {
		if candidate.MarketplaceDiscovery != nil && matches(candidate.MarketplaceDiscovery.Endpoint) {
			kept = append(kept, candidate)
		}
	}
	return kept
}

func filterConfiguredOutcomes(outcomes []marketplace.DiscoveryOutcome, matches func(config.MarketplaceEntry) bool) []marketplace.DiscoveryOutcome {
	return slices.DeleteFunc(outcomes, func(outcome marketplace.DiscoveryOutcome) bool {
		return !matches(outcome.Endpoint)
	})
}

func marketplaceBrowseRequestKey(cfg *config.Config) (string, int, error) {
	effective := effectiveMarketplaceConfig(cfg)
	endpoints := make([]string, 0, len(cfg.Marketplaces))
	allowedCount := 0
	for _, endpoint := range cfg.Marketplaces {
		normalized, err := marketplace.NormalizeMarketplaceURL(endpoint.URL)
		if err != nil {
			return "", 0, err
		}
		endpoints = append(endpoints, normalized+"\x00"+endpoint.InstanceID+"\x00"+endpoint.Alias)
		err = config.ValidateMarketplaceURLs([]config.MarketplaceEntry{endpoint}, effective.AllowedMarketplaces)
		if err == nil {
			allowedCount++
			continue
		}
		var blocked *config.MarketplaceURLBlockedError
		if !errors.As(err, &blocked) {
			return "", 0, err
		}
	}
	slices.Sort(endpoints)
	allowed := slices.Clone(effective.AllowedMarketplaces)
	slices.Sort(allowed)
	return strings.Join(endpoints, "\x00") + "\x01" + strings.Join(allowed, "\x00"), allowedCount, nil
}

func (m *Model) beginMarketplaceBrowse(force bool) tea.Cmd {
	if m.mergedCfg == nil || len(m.mergedCfg.Marketplaces) == 0 {
		rebuildAuthoritative := len(m.discoveryRows) > 0 || len(m.browseDiscoveryRows) > 0 || m.authoritativeRows != nil
		m.clearMarketplaceDiscovery(rebuildAuthoritative)
		return nil
	}
	key, allowedCount, err := marketplaceBrowseRequestKey(m.mergedCfg)
	if err != nil {
		m.clearMarketplaceDiscovery(true)
		m.browseErrs = []error{err}
		return nil
	}
	if allowedCount == 0 {
		validationErr := config.ValidateMarketplaceURLs(m.mergedCfg.Marketplaces, effectiveMarketplaceConfig(m.mergedCfg).AllowedMarketplaces)
		m.clearMarketplaceDiscovery(true)
		if validationErr != nil {
			m.browseErrs = []error{validationErr}
		}
		return nil
	}
	if !force && key == m.browseKey && (m.browseLoading || m.browseFinished) {
		return nil
	}
	keyChanged := m.browseKey != "" && key != m.browseKey
	if keyChanged {
		m.cancelMarketplaceBrowse()
		m.cancelMarketplaceDiscovery()
		isConfigured := discoveryEndpointMatcher(m.mergedCfg.Marketplaces, effectiveMarketplaceConfig(m.mergedCfg).AllowedMarketplaces)
		m.discoveryRows = filterMarketplaceRows(m.discoveryRows, isConfigured)
		m.browseDiscoveryRows = filterMarketplaceRows(m.browseDiscoveryRows, isConfigured)
		m.discoveryOutcomes = filterConfiguredOutcomes(m.discoveryOutcomes, isConfigured)
		m.browseOutcomes = filterConfiguredOutcomes(m.browseOutcomes, isConfigured)
		m.rebuildDiscoveryErrors()
		m.rebuildBrowseErrors()
		m.mergeDiscoveryRows()
	} else if m.browseLoading {
		m.cancelMarketplaceBrowse()
	}
	m.browseKey = key
	m.browseFinished = false
	m.browseLoading = true
	m.browsePending = m.browsePending[:0]
	allowed := effectiveMarketplaceConfig(m.mergedCfg).AllowedMarketplaces
	for _, endpoint := range m.mergedCfg.Marketplaces {
		if config.ValidateMarketplaceURLs([]config.MarketplaceEntry{endpoint}, allowed) == nil {
			m.browsePending = append(m.browsePending, endpoint)
		}
	}
	generation := m.beginRequest(requestBrowse)
	ctx, cancel := context.WithCancel(ctxOrBackground(m.ctx))
	m.browseCancel = cancel
	return loadMarketplaceBrowse(ctx, m.mergedCfg, generation)
}

func (m *Model) beginMarketplaceRefresh(forceBrowse bool) tea.Cmd {
	browse := m.beginMarketplaceBrowse(forceBrowse)
	if strings.TrimSpace(m.filter) == "" {
		return browse
	}
	return tea.Batch(browse, m.beginMarketplaceDiscovery(m.filter, false))
}

type marketplaceEndpointError struct {
	Endpoint config.MarketplaceEntry
	Err      error
}

func (e *marketplaceEndpointError) Error() string {
	return fmt.Sprintf("%s: %v", marketplaceEndpointDisplay(e.Endpoint), e.Err)
}

func (e *marketplaceEndpointError) Unwrap() error { return e.Err }

func marketplaceEndpointIdentity(endpoint config.MarketplaceEntry) string {
	url := endpoint.URL
	if normalized, err := marketplace.NormalizeMarketplaceURL(url); err == nil {
		url = normalized
	}
	return url + "\x00" + endpoint.InstanceID
}

func discoveryCoordinate(instanceID, registryID, kind, name string) string {
	return instanceID + "\x00" + registryID + "\x00" + kind + "\x00" + name
}

func (m *Model) setProvisionalRows(rows []row) {
	if m.authoritativeRows != nil {
		m.provisionalRows = nil
		return
	}
	m.provisionalRows = rows
	m.mergeDiscoveryRows()
}

func provisionalCoordinate(r row) string {
	url := r.Artifact.MarketplaceURL
	if url == "" {
		url = r.RegistryURL
	}
	if normalized, err := marketplace.NormalizeMarketplaceURL(url); err == nil {
		url = normalized
	}
	return url + "\x00" + r.Artifact.MarketplaceRegistryID + "\x00" + r.Artifact.Kind.String() + "\x00" + r.Artifact.MarketplaceResourceName
}

func (m *Model) mergeDiscoveryRows() {
	selection := m.captureRebuildAnchor()
	discoveryRows := m.displayDiscoveryRows()
	rows := make([]row, 0, len(m.authoritativeRows)+len(m.provisionalRows)+len(discoveryRows))
	rows = append(rows, m.authoritativeRows...)
	pending := make(map[string]struct{}, len(m.provisionalRows))
	if m.authoritativeRows == nil {
		for _, r := range m.provisionalRows {
			if r.Artifact.IsMarketplace {
				pending[provisionalCoordinate(r)] = struct{}{}
			}
		}
		rows = append(rows, m.provisionalRows...)
	}
	enabled := make(map[string]struct{})
	for _, r := range m.authoritativeRows {
		if r.IsAvailable || !r.Artifact.IsMarketplace {
			continue
		}
		selectionCoordinates := 0
		for _, selection := range r.MarketplaceSelections {
			pin := selection.Pin
			if selection.Entry.InstanceID == "" || pin.RegistryID == "" || pin.Kind == "" || pin.Name == "" {
				continue
			}
			enabled[discoveryCoordinate(selection.Entry.InstanceID, pin.RegistryID, pin.Kind, pin.Name)] = struct{}{}
			selectionCoordinates++
		}
		if selectionCoordinates == 0 {
			enabled[discoveryCoordinate(r.Artifact.MarketplaceInstanceID, r.Artifact.MarketplaceRegistryID, r.Artifact.Kind.String(), r.Artifact.Name)] = struct{}{}
		}
	}
	for _, r := range discoveryRows {
		d := r.MarketplaceDiscovery
		if d == nil {
			continue
		}
		if _, ok := enabled[discoveryCoordinate(d.Endpoint.InstanceID, d.Item.RegistryID, d.Item.Kind, d.Item.Name)]; ok {
			continue
		}
		url := d.Endpoint.URL
		if normalized, err := marketplace.NormalizeMarketplaceURL(url); err == nil {
			url = normalized
		}
		if _, ok := pending[url+"\x00"+d.Item.RegistryID+"\x00"+d.Item.Kind+"\x00"+d.Item.Name]; ok {
			continue
		}
		rows = append(rows, r)
	}
	m.rows = rows
	m.recomputeView()
	m.restoreSelectionAfterRebuild(selection)
}

func marketplaceRowCoordinate(r row) string {
	d := r.MarketplaceDiscovery
	if d == nil {
		return ""
	}
	url := d.Endpoint.URL
	if normalized, err := marketplace.NormalizeMarketplaceURL(url); err == nil {
		url = normalized
	}
	return d.Endpoint.InstanceID + "\x00" + url + "\x00" + d.Item.RegistryID + "\x00" + d.Item.Kind + "\x00" + d.Item.Name
}

func (m *Model) displayDiscoveryRows() []row {
	if m.filter == "" {
		return m.browseDiscoveryRows
	}
	rows := slices.Clone(m.discoveryRows)
	seen := make(map[string]struct{}, len(rows)+len(m.browseDiscoveryRows))
	for _, r := range rows {
		seen[marketplaceRowCoordinate(r)] = struct{}{}
	}
	for _, r := range m.browseDiscoveryRows {
		coordinate := marketplaceRowCoordinate(r)
		if _, ok := seen[coordinate]; ok {
			continue
		}
		seen[coordinate] = struct{}{}
		rows = append(rows, r)
	}
	return rows
}

func (m *Model) rebuildDiscoveryErrors() {
	m.discoveryErrs = m.discoveryErrs[:0]
	seen := make(map[string]struct{})
	for _, outcome := range m.discoveryOutcomes {
		if outcome.Err != nil {
			m.discoveryErrs = append(m.discoveryErrs, &marketplaceEndpointError{Endpoint: outcome.Endpoint, Err: outcome.Err})
			seen[marketplaceEndpointIdentity(outcome.Endpoint)] = struct{}{}
		}
	}
	if m.mergedCfg != nil {
		browseErrors := make(map[string]struct{}, len(m.browseErrs))
		for _, err := range m.browseErrs {
			var endpointErr *marketplaceEndpointError
			if errors.As(err, &endpointErr) {
				browseErrors[marketplaceEndpointIdentity(endpointErr.Endpoint)] = struct{}{}
			}
		}
		for _, err := range configuredMarketplaceErrors(m.mergedCfg) {
			var endpointErr *marketplaceEndpointError
			if !errors.As(err, &endpointErr) {
				continue
			}
			key := marketplaceEndpointIdentity(endpointErr.Endpoint)
			if _, exists := seen[key]; exists {
				continue
			}
			if _, exists := browseErrors[key]; exists {
				continue
			}
			m.discoveryErrs = append(m.discoveryErrs, err)
			seen[key] = struct{}{}
		}
	}
}

func (m *Model) rebuildBrowseErrors() {
	m.browseErrs = m.browseErrs[:0]
	seen := make(map[string]struct{})
	for _, outcome := range m.browseOutcomes {
		if outcome.Err != nil {
			m.browseErrs = append(m.browseErrs, &marketplaceEndpointError{Endpoint: outcome.Endpoint, Err: outcome.Err})
			seen[marketplaceEndpointIdentity(outcome.Endpoint)] = struct{}{}
		}
	}
	for _, err := range configuredMarketplaceErrors(m.mergedCfg) {
		var endpointErr *marketplaceEndpointError
		if !errors.As(err, &endpointErr) {
			continue
		}
		key := marketplaceEndpointIdentity(endpointErr.Endpoint)
		if _, exists := seen[key]; exists {
			continue
		}
		m.browseErrs = append(m.browseErrs, err)
		seen[key] = struct{}{}
	}
}

func configuredMarketplaceErrors(cfg *config.Config) []error {
	if cfg == nil {
		return nil
	}
	allowed := effectiveMarketplaceConfig(cfg).AllowedMarketplaces
	var errs []error
	for _, endpoint := range cfg.Marketplaces {
		if err := config.ValidateMarketplaceURLs([]config.MarketplaceEntry{endpoint}, allowed); err != nil {
			errs = append(errs, &marketplaceEndpointError{Endpoint: endpoint, Err: err})
		}
	}
	return errs
}

func (m Model) marketplaceDiscoveryErrors() []error {
	return append(slices.Clone(m.discoveryErrs), m.browseErrs...)
}

func (m *Model) applyDiscovery(query string, outcomes []marketplace.DiscoveryOutcome) {
	if query == "" {
		m.applyBrowseDiscovery(outcomes)
		return
	}
	// The request generation/sequence is checked by Update before applying a
	// snapshot. Also match the current endpoint declarations: acquiring a new ID
	// must not attach results from a replaced URL or an existing exact identity.
	if m.mergedCfg != nil {
		isConfigured := discoveryEndpointMatcher(m.mergedCfg.Marketplaces, effectiveMarketplaceConfig(m.mergedCfg).AllowedMarketplaces)
		outcomes = filterConfiguredOutcomes(slices.Clone(outcomes), isConfigured)
	}
	m.discoveryOutcomes = outcomes
	m.rebuildDiscoveryErrors()
	m.discoveryRows = m.discoveryRowsFromOutcomes(outcomes)
	m.mergeDiscoveryRows()
}

func (m *Model) applyBrowseDiscovery(outcomes []marketplace.DiscoveryOutcome) {
	if m.mergedCfg != nil {
		isConfigured := discoveryEndpointMatcher(m.mergedCfg.Marketplaces, effectiveMarketplaceConfig(m.mergedCfg).AllowedMarketplaces)
		outcomes = filterConfiguredOutcomes(slices.Clone(outcomes), isConfigured)
	}
	m.browseOutcomes = outcomes
	m.rebuildBrowseErrors()
	m.updateBrowsePending(outcomes)
	m.browseDiscoveryRows = m.discoveryRowsFromOutcomes(outcomes)
	m.mergeDiscoveryRows()
}

func (m *Model) updateBrowsePending(outcomes []marketplace.DiscoveryOutcome) {
	if len(m.browsePending) == 0 {
		return
	}
	remaining := m.browsePending[:0]
	for _, endpoint := range m.browsePending {
		pending := true
		for _, outcome := range outcomes {
			if marketplaceEndpointIdentity(outcome.Endpoint) != marketplaceEndpointIdentity(endpoint) {
				continue
			}
			pending = outcome.Err == nil && (outcome.Truncated || outcome.CacheState == marketplace.DiscoveryCacheStale)
			break
		}
		if pending {
			remaining = append(remaining, endpoint)
		}
	}
	m.browsePending = remaining
}

func (m Model) marketplaceDiscoveryFailureCount() int {
	seen := make(map[string]struct{})
	for _, err := range m.marketplaceDiscoveryErrors() {
		var endpointErr *marketplaceEndpointError
		key := ""
		if errors.As(err, &endpointErr) {
			key = "endpoint:" + marketplaceEndpointIdentity(endpointErr.Endpoint)
		} else {
			key = "error:" + err.Error()
		}
		seen[key] = struct{}{}
	}
	return len(seen)
}

func (m *Model) discoveryRowsFromOutcomes(outcomes []marketplace.DiscoveryOutcome) []row {
	var rows []row
	for _, outcome := range outcomes {
		for _, item := range outcome.Items {
			kind, ok := skill.ParseKind(item.Kind)
			if !ok {
				continue
			}
			tags := make([]string, 0)
			if value := item.Metadata["tags"]; value != "" {
				tags = append(tags, value)
			}
			discovery := &marketplaceDiscovery{Endpoint: outcome.Endpoint, Item: item, CacheState: outcome.CacheState, ObservedAt: outcome.ObservedAt}
			rows = append(rows, row{
				Artifact: resolver.ResolvedArtifact{
					Entry:                   catalog.Entry{Kind: kind, Name: item.Name, Description: item.Description, Tags: tags},
					CatalogKey:              "marketplace-discovery:" + outcome.Endpoint.InstanceID + ":" + item.RegistryID + ":" + item.Revision + ":" + item.ContentDigest,
					IsMarketplace:           true,
					MarketplaceURL:          outcome.Endpoint.URL,
					MarketplaceInstanceID:   outcome.Endpoint.InstanceID,
					MarketplaceAlias:        outcome.Endpoint.Alias,
					MarketplaceRegistryID:   item.RegistryID,
					MarketplaceResourceName: item.Name,
				},
				IsAvailable:          true,
				RegistryURL:          outcome.Endpoint.URL,
				RegistryName:         marketplaceEndpointDisplay(outcome.Endpoint) + "/" + item.RegistrySlug,
				MarketplaceDiscovery: discovery,
			})
		}
	}
	slices.SortFunc(rows, func(a, b row) int {
		da, db := a.MarketplaceDiscovery, b.MarketplaceDiscovery
		if da.Item.Score != db.Item.Score {
			if da.Item.Score > db.Item.Score {
				return -1
			}
			return 1
		}
		for _, c := range []int{
			cmp.Compare(marketplaceEndpointDisplay(da.Endpoint), marketplaceEndpointDisplay(db.Endpoint)),
			cmp.Compare(da.Endpoint.URL, db.Endpoint.URL),
			cmp.Compare(da.Endpoint.InstanceID, db.Endpoint.InstanceID),
			cmp.Compare(da.Item.RegistrySlug, db.Item.RegistrySlug),
			cmp.Compare(da.Item.Kind, db.Item.Kind),
			cmp.Compare(da.Item.Name, db.Item.Name),
			cmp.Compare(da.Item.Revision, db.Item.Revision),
			cmp.Compare(da.Item.ContentDigest, db.Item.ContentDigest),
		} {
			if c != 0 {
				return c
			}
		}
		return 0
	})
	return rows
}
