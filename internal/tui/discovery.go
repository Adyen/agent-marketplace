package tui

import (
	"context"
	"fmt"
	"slices"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/charmbracelet/x/ansi"
)

const marketplaceSearchDebounce = session.DiscoverySearchDebounce

type discoveryLoadedMsg struct {
	generation requestGeneration
	sequence   uint64
	query      string
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

func prepareDiscoveryInstall(ctx context.Context, operation operationID, options pipeline.InstallOptions) tea.Cmd {
	return func() tea.Msg {
		result := session.PrepareDiscoveryInstall(ctx, options)
		return discoveryInstallMsg{operation: operation, options: result.Options, prepared: result.Prepared, preview: result.Preview, err: result.Err}
	}
}

func commitDiscoveryInstall(ctx context.Context, operation operationID, options pipeline.InstallOptions, prepared *pipeline.PreparedMarketplaceInstall) tea.Cmd {
	return func() tea.Msg {
		result := session.CommitDiscoveryInstall(ctx, options, prepared)
		return discoveryInstallMsg{operation: operation, options: result.Options, result: result.Result, err: result.Err}
	}
}

func (m Model) startDiscoveryInstall(r row, s scope) (tea.Model, tea.Cmd) {
	options, err := discoveryInstallOptions(m.repoRoot, r, s)
	if err != nil {
		return m, func() tea.Msg { return toastMsg{text: err.Error(), duration: 3 * time.Second} }
	}
	operation, ok := m.beginOperation(operationMutation)
	if !ok {
		return m, operationBlockedCmd()
	}
	m.pane = paneList
	m.marketplaceInstallOptions = options
	m.marketplaceInstallOperation = operation
	m.beginOperationProgressFor(operation, "install", m.requests.Active(requestSnapshot))
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
		m.pushToast("Marketplace install reload failed: "+err.Error(), 5*time.Second)
		return toastExpiryCmd(5 * time.Second)
	}
	m.recordOperationSuccess(operationMutation)
	m.pushToast("Marketplace resource installed", 2*time.Second)
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

func effectiveMarketplaceConfig(cfg *config.Config) config.EffectiveConfig {
	return session.EffectiveMarketplaceConfig(cfg)
}

func loadMarketplaceDiscovery(ctx context.Context, cfg *config.Config, query string, generation requestGeneration) tea.Cmd {
	return func() tea.Msg {
		updates, err := marketplace.DiscoverUpdates(ctx, effectiveMarketplaceConfig(cfg), query, marketplace.DiscoveryOptions{Limit: marketplace.DiscoveryPageLimit})
		if err != nil {
			return discoveryLoadedMsg{generation: generation, query: query, done: true, err: err}
		}
		return waitMarketplaceDiscoveryUpdate(generation, 1, query, updates)()
	}
}

func waitMarketplaceDiscoveryUpdate(generation requestGeneration, sequence uint64, query string, updates <-chan []marketplace.DiscoveryOutcome) tea.Cmd {
	return func() tea.Msg {
		outcomes, ok := <-updates
		return discoveryLoadedMsg{generation: generation, sequence: sequence, query: query, outcomes: outcomes, updates: updates, done: !ok}
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

func (m *Model) clearMarketplaceDiscovery(rebuildAuthoritative bool) {
	m.cancelMarketplaceDiscovery()
	m.discoveryRows = nil
	m.browseDiscoveryRows = nil
	m.discoveryOutcomes = nil
	m.discoveryErrs = nil
	if rebuildAuthoritative {
		m.mergeDiscoveryRows()
	}
}

func (m *Model) beginMarketplaceDiscovery(query string, debounce bool) tea.Cmd {
	if m.mergedCfg == nil || len(m.mergedCfg.Marketplaces) == 0 {
		rebuildAuthoritative := len(m.discoveryRows) > 0 || len(m.browseDiscoveryRows) > 0 || m.authoritativeRows != nil
		m.clearMarketplaceDiscovery(rebuildAuthoritative)
		return nil
	}
	configured := make(map[string]struct{}, len(m.mergedCfg.Marketplaces))
	for _, endpoint := range m.mergedCfg.Marketplaces {
		url, err := marketplace.NormalizeMarketplaceURL(endpoint.URL)
		if err == nil {
			configured[url+"\x00"+endpoint.InstanceID] = struct{}{}
		}
	}
	isConfigured := func(endpoint config.MarketplaceEntry) bool {
		url, err := marketplace.NormalizeMarketplaceURL(endpoint.URL)
		if err != nil {
			return false
		}
		_, ok := configured[url+"\x00"+endpoint.InstanceID]
		return ok
	}
	filterConfigured := func(rows []row) []row {
		kept := rows[:0]
		for _, candidate := range rows {
			if candidate.MarketplaceDiscovery != nil && isConfigured(candidate.MarketplaceDiscovery.Endpoint) {
				kept = append(kept, candidate)
			}
		}
		return kept
	}
	before := len(m.discoveryRows)
	m.discoveryRows = filterConfigured(m.discoveryRows)
	m.browseDiscoveryRows = filterConfigured(m.browseDiscoveryRows)
	retainedOutcomes := m.discoveryOutcomes[:0]
	for _, outcome := range m.discoveryOutcomes {
		if isConfigured(outcome.Endpoint) {
			retainedOutcomes = append(retainedOutcomes, outcome)
		}
	}
	m.discoveryOutcomes = retainedOutcomes
	m.rebuildDiscoveryErrors()
	if len(m.discoveryRows) != before {
		m.mergeDiscoveryRows()
	}
	m.cancelMarketplaceDiscovery()
	if !debounce {
		if err := config.ValidateMarketplaceURLs(m.mergedCfg.Marketplaces, effectiveMarketplaceConfig(m.mergedCfg).AllowedMarketplaces); err != nil {
			m.clearMarketplaceDiscovery(true)
			m.discoveryErrs = []error{err}
			return nil
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

func (m *Model) setProvisionalRows(rows []row) {
	if m.authoritativeRows != nil {
		m.provisionalRows = nil
		return
	}
	m.provisionalRows = rows
	m.mergeDiscoveryRows()
}

func (m *Model) mergeDiscoveryRows() {
	selection := m.captureRebuildAnchor()
	m.rows = session.MergeDiscoveryRows(m.authoritativeRows, m.provisionalRows, m.discoveryRows)
	m.recomputeView()
	m.restoreSelectionAfterRebuild(selection)
}

func (m *Model) rebuildDiscoveryErrors() {
	m.discoveryErrs = m.discoveryErrs[:0]
	for _, outcome := range m.discoveryOutcomes {
		if outcome.Err != nil {
			m.discoveryErrs = append(m.discoveryErrs, fmt.Errorf("%s: %w", marketplaceEndpointDisplay(outcome.Endpoint), outcome.Err))
		}
	}
}

func (m *Model) applyDiscovery(query string, outcomes []marketplace.DiscoveryOutcome) {
	m.discoveryOutcomes = outcomes
	m.rebuildDiscoveryErrors()
	m.discoveryRows = session.DiscoveryRows(outcomes)
	if query == "" {
		m.browseDiscoveryRows = slices.Clone(m.discoveryRows)
	}
	m.mergeDiscoveryRows()
}
