package tui

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const marketplaceSearchDebounce = 250 * time.Millisecond

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

func discoveryInstallOptions(repoRoot string, r row, s scope) (pipeline.InstallOptions, error) {
	d := r.MarketplaceDiscovery
	if d == nil || d.Item.Availability != "available" {
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
	operation, ok := m.beginOperation(operationMutation)
	if !ok {
		return m, operationBlockedCmd()
	}
	m.pane = paneList
	m.marketplaceInstallOptions = options
	m.marketplaceInstallOperation = operation
	return m, prepareDiscoveryInstall(ctxOrBackground(m.ctx), operation, options)
}

func (m *Model) finishMarketplaceInstallReload(generation requestGeneration, err error) tea.Cmd {
	if m.marketplaceInstallReload == 0 || generation < m.marketplaceInstallReload {
		return nil
	}
	m.finishOperation(m.marketplaceInstallOperation)
	m.marketplaceInstallOperation = 0
	m.marketplaceInstallReload = 0
	if err != nil {
		m.recordOperationFailure(operationMutation, diagnosticMutation, err)
		m.toast = "Marketplace install reload failed: " + err.Error()
		m.toastExp = time.Now().Add(5 * time.Second)
		return toastExpiryCmd(5 * time.Second)
	}
	m.recordOperationSuccess(operationMutation)
	m.toast = "Marketplace resource installed"
	m.toastExp = time.Now().Add(2 * time.Second)
	return toastExpiryCmd(2 * time.Second)
}

func (m *Model) resizeMarketplaceConsentViewport(reset bool) {
	m.marketplaceConsentViewport.Width = max(1, m.width)
	m.marketplaceConsentViewport.Height = max(1, m.height-3)
	m.marketplaceConsentViewport.SetContent(ansi.Hardwrap(m.marketplaceInstallPreview, m.marketplaceConsentViewport.Width, true))
	if reset {
		m.marketplaceConsentViewport.GotoTop()
	}
}

func (m *Model) setMarketplaceConsentPreview(preview string) {
	m.marketplaceInstallPreview = sanitizeTerminalData(preview, terminalMarkdownBody)
	m.resizeMarketplaceConsentViewport(true)
}

func renderMarketplaceConsent(m Model) string {
	return styleConfirmTitle.Render("Verified Marketplace install") + "\nInstall this exact verified revision?\n" + m.marketplaceConsentViewport.View()
}

func (m Model) handleMarketplaceConsentKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.pane = paneList
		return m, commitDiscoveryInstall(ctxOrBackground(m.ctx), m.marketplaceInstallOperation, m.marketplaceInstallOptions, m.marketplacePreparedInstall)
	case "n", "N", "esc":
		m.pane = paneList
		m.finishOperation(m.marketplaceInstallOperation)
		m.marketplaceInstallOperation = 0
		m.marketplacePreparedInstall = nil
		m.marketplaceInstallPreview = ""
		m.marketplaceConsentViewport.SetContent("")
		m.marketplaceConsentViewport.GotoTop()
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

func (m *Model) beginMarketplaceDiscovery(query string, debounce bool) tea.Cmd {
	if m.mergedCfg == nil || len(m.mergedCfg.Marketplaces) == 0 {
		hadDiscovery := len(m.discoveryRows) > 0 || len(m.browseDiscoveryRows) > 0
		m.cancelMarketplaceDiscovery()
		m.discoveryRows = nil
		m.browseDiscoveryRows = nil
		m.discoveryOutcomes = nil
		m.discoveryErrs = nil
		if hadDiscovery || m.authoritativeRows != nil {
			m.mergeDiscoveryRows()
		}
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
		cached, needsNetwork, err := marketplace.CachedDiscovery(effectiveMarketplaceConfig(m.mergedCfg), query, marketplace.DiscoveryOptions{Limit: marketplace.DiscoveryPageLimit})
		if err != nil {
			m.discoveryErrs = []error{err}
			return nil
		}
		if len(cached) > 0 {
			m.applyDiscovery(query, cached)
		}
		if !needsNetwork {
			m.discoveryLoading = false
			m.cancelRequest(requestDiscovery)
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

func discoveryCoordinate(instanceID, registryID, kind, name string) string {
	return instanceID + "\x00" + registryID + "\x00" + kind + "\x00" + name
}

func (m *Model) mergeDiscoveryRows() {
	selection := m.selectionKeyForRebuild()
	rows := make([]row, 0, len(m.authoritativeRows)+len(m.discoveryRows))
	rows = append(rows, m.authoritativeRows...)
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
	for _, r := range m.discoveryRows {
		d := r.MarketplaceDiscovery
		if d == nil {
			continue
		}
		if _, ok := enabled[discoveryCoordinate(d.Endpoint.InstanceID, d.Item.RegistryID, d.Item.Kind, d.Item.Name)]; ok {
			continue
		}
		rows = append(rows, r)
	}
	m.rows = rows
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
					Entry:                 catalog.Entry{Kind: kind, Name: item.Name, Description: item.Description, Tags: tags},
					CatalogKey:            "marketplace-discovery:" + outcome.Endpoint.InstanceID + ":" + item.RegistryID + ":" + item.Revision + ":" + item.ContentDigest,
					IsMarketplace:         true,
					MarketplaceURL:        outcome.Endpoint.URL,
					MarketplaceInstanceID: outcome.Endpoint.InstanceID,
					MarketplaceAlias:      outcome.Endpoint.Alias,
					MarketplaceRegistryID: item.RegistryID,
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
	m.discoveryRows = rows
	if query == "" {
		m.browseDiscoveryRows = slices.Clone(rows)
	}
	m.mergeDiscoveryRows()
}
