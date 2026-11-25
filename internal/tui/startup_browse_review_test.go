package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/charmbracelet/x/ansi"
)

// spec: tui-startup/warm-startup-and-empty-endpoints-remain-usable
func TestManualReloadPreservesSnapshotRefreshAndSyncGuard(t *testing.T) {
	m := newTestModel()
	endpoint := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market", Alias: "market"}
	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}
	generation := m.beginRequest(requestSnapshot)
	m.syncing = true

	updated, cmd := m.Update(reloadMsg(snapshotLoadedMsg{
		generation:       generation,
		mergedCfg:        m.mergedCfg,
		refreshAfterLoad: true,
	}))
	m = updated.(Model)
	if !m.syncing {
		t.Fatal("manual reload cleared the active sync guard")
	}
	if !m.refreshing || !m.browseLoading || cmd == nil {
		t.Fatalf("manual reload lost ordinary snapshot work: refreshing=%t browseLoading=%t cmd=%t", m.refreshing, m.browseLoading, cmd != nil)
	}

	for _, key := range []rune{'s', 'u'} {
		updated, next := m.Update(keyPress(key))
		m = updated.(Model)
		if next != nil || !m.syncing {
			t.Fatalf("%q started another operation while sync was running: cmd=%t syncing=%t", key, next != nil, m.syncing)
		}
	}
	m.cancelMarketplaceLoads()
}

// spec: tui-startup/superseded-browse-results-cannot-restore-removed-endpoints
func TestOlderManualReloadCannotSupersedeNewerSnapshot(t *testing.T) {
	m := newTestModel()
	older := m.beginRequest(requestSnapshot)
	newer := m.beginRequest(requestSnapshot)

	updated, cmd := m.Update(reloadMsg(snapshotLoadedMsg{generation: older}))
	m = updated.(Model)
	if got := m.requests.Active(requestSnapshot); got != newer {
		t.Fatalf("stale reload replaced generation %d with %d", newer, got)
	}
	if cmd != nil || m.browseLoading || m.discoveryLoading {
		t.Fatalf("stale reload started Marketplace work: cmd=%t browse=%t search=%t", cmd != nil, m.browseLoading, m.discoveryLoading)
	}
}

// spec: tui-startup/offline-partial-and-denied-discovery-preserve-useful-browsing
func TestSyncAndQuitDoesNotRestartCanceledMarketplaceRequests(t *testing.T) {
	m := newTestModel()
	endpoint := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}
	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}
	m.filter = "needle"
	generation := m.beginRequest(requestSnapshot)
	m.syncing = true
	m.syncReload = generation
	m.quitAfterSync = true
	browseCtx, cancelBrowse := context.WithCancel(context.Background())
	discoveryCtx, cancelDiscovery := context.WithCancel(context.Background())
	m.browseCancel = func() { cancelBrowse() }
	m.discoveryCancel = func() { cancelDiscovery() }
	m.browseLoading = true
	m.discoveryLoading = true

	updated, cmd := m.Update(snapshotLoadedMsg{generation: generation, mergedCfg: m.mergedCfg})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("accepted sync-and-quit snapshot did not return its quit command")
	}
	select {
	case <-browseCtx.Done():
	default:
		t.Fatal("sync-and-quit did not cancel the browse request")
	}
	select {
	case <-discoveryCtx.Done():
	default:
		t.Fatal("sync-and-quit did not cancel the search request")
	}
	if m.browseLoading || m.discoveryLoading {
		t.Fatalf("sync-and-quit restarted a canceled request: browse=%t search=%t", m.browseLoading, m.discoveryLoading)
	}
}

// spec: tui-startup/search-during-loading-and-clear-restore-complete-browsing
func TestClearingSearchRebuildsAnEmptyBrowseInventory(t *testing.T) {
	endpoint := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}
	m := newTestModel()
	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}
	m.filter = "needle"
	m.applyDiscovery("needle", []marketplace.DiscoveryOutcome{{
		Endpoint: endpoint,
		Items:    []mpapi.ResourceListItem{tuiDiscoverySearchItem("registry", "skill", "needle-resource", "search result")},
		Total:    1,
	}})
	if !slices.ContainsFunc(m.rows, func(candidate row) bool { return candidate.Artifact.Name == "needle-resource" }) {
		t.Fatal("search result was not shown in the setup")
	}
	key, _, err := marketplaceBrowseRequestKey(m.mergedCfg)
	if err != nil {
		t.Fatal(err)
	}
	m.browseKey = key
	m.browseFinished = true

	updated, _ := m.clearSearch()
	m = updated.(Model)
	if slices.ContainsFunc(m.rows, func(candidate row) bool { return candidate.Artifact.Name == "needle-resource" }) {
		t.Fatal("clearing an empty browse inventory left stale search rows on screen")
	}
}

// spec: tui-startup/search-during-loading-and-clear-restore-complete-browsing
func TestClearingSearchRestoresBrowseInventoryBeforeLocalRowsLoad(t *testing.T) {
	endpoint := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}
	m := NewModel(t.TempDir(), t.TempDir())
	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}
	m.applyBrowseDiscovery([]marketplace.DiscoveryOutcome{{
		Endpoint: endpoint,
		Items:    []mpapi.ResourceListItem{tuiDiscoverySearchItem("registry", "skill", "browse-resource", "browse result")},
		Total:    1,
	}})
	if !slices.ContainsFunc(m.filteredRows, func(candidate row) bool { return candidate.Artifact.Name == "browse-resource" }) {
		t.Fatal("browse result was not shown before local rows loaded")
	}

	m.filter = "needle"
	m.applyDiscovery("needle", []marketplace.DiscoveryOutcome{{
		Endpoint: endpoint,
		Items:    []mpapi.ResourceListItem{tuiDiscoverySearchItem("registry", "skill", "needle-resource", "search result")},
		Total:    1,
	}})
	if !slices.ContainsFunc(m.filteredRows, func(candidate row) bool { return candidate.Artifact.Name == "needle-resource" }) {
		t.Fatal("search result was not shown")
	}
	key, _, err := marketplaceBrowseRequestKey(m.mergedCfg)
	if err != nil {
		t.Fatal(err)
	}
	m.browseKey = key
	m.browseFinished = true

	updated, _ := m.clearSearch()
	m = updated.(Model)
	if !slices.ContainsFunc(m.filteredRows, func(candidate row) bool { return candidate.Artifact.Name == "browse-resource" }) {
		t.Fatal("clearing search did not restore the browse result before local rows loaded")
	}
}

// spec: tui-startup/warm-startup-and-empty-endpoints-remain-usable
func TestWhitespaceSearchReusesTheExistingBrowse(t *testing.T) {
	m := newTestModel()
	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{{
		URL: "https://market.example", InstanceID: "market",
	}}}
	key, _, err := marketplaceBrowseRequestKey(m.mergedCfg)
	if err != nil {
		t.Fatal(err)
	}
	m.browseKey = key
	m.browseFinished = true
	m.filter = " "

	cmd := m.beginMarketplaceRefresh(false)
	if cmd != nil || m.discoveryLoading || m.discoverySequence != 0 || m.browseLoading {
		t.Fatalf("whitespace search started another discovery: cmd=%t search=%t sequence=%d browse=%t", cmd != nil, m.discoveryLoading, m.discoverySequence, m.browseLoading)
	}
}

// spec: tui-startup/search-during-loading-and-clear-restore-complete-browsing
func TestWhitespaceSearchCancelsInFlightDiscovery(t *testing.T) {
	endpoint := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}
	m := NewModel(t.TempDir(), t.TempDir())
	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}
	key, _, err := marketplaceBrowseRequestKey(m.mergedCfg)
	if err != nil {
		t.Fatal(err)
	}
	m.browseKey = key
	m.browseFinished = true

	if cmd := m.beginMarketplaceDiscovery("needle", false); cmd == nil {
		t.Fatal("search did not start discovery")
	}
	defer m.cancelMarketplaceLoads()
	generation := m.requests.Active(requestDiscovery)
	m.beginMarketplaceDiscovery(" ", false)
	updated, _ := m.Update(discoveryLoadedMsg{
		generation: generation,
		sequence:   1,
		query:      "needle",
		outcomes: []marketplace.DiscoveryOutcome{{
			Endpoint: endpoint,
			Items:    []mpapi.ResourceListItem{tuiDiscoverySearchItem("registry", "skill", "late-result", "late search result")},
			Total:    1,
		}},
	})
	m = updated.(Model)
	if len(m.discoveryRows) != 0 {
		t.Fatal("whitespace search accepted the superseded query result")
	}
}

// spec: tui-startup/search-during-loading-and-clear-restore-complete-browsing
func TestWhitespaceSearchRestoresBrowseRows(t *testing.T) {
	endpoint := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}
	m := newTestModel()
	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}
	m.filter = "needle"
	m.applyBrowseDiscovery([]marketplace.DiscoveryOutcome{{
		Endpoint: endpoint,
		Items:    []mpapi.ResourceListItem{tuiDiscoverySearchItem("registry", "skill", "browse-resource", "browse result")},
		Total:    1,
	}})
	m.applyDiscovery("needle", []marketplace.DiscoveryOutcome{{
		Endpoint: endpoint,
		Items:    []mpapi.ResourceListItem{tuiDiscoverySearchItem("registry", "skill", "needle-resource", "search result")},
		Total:    1,
	}})
	key, _, err := marketplaceBrowseRequestKey(m.mergedCfg)
	if err != nil {
		t.Fatal(err)
	}
	m.browseKey = key
	m.browseFinished = true
	m.searchInput.SetValue("")
	m.searchInput.Focus()

	updated, _ := m.applySearchInput(keyPress(' '))
	m = updated.(Model)
	if m.filter != "" || m.searchInput.Value() != "" {
		t.Fatalf("whitespace search was not normalized to empty: filter=%q input=%q", m.filter, m.searchInput.Value())
	}
	if slices.ContainsFunc(m.rows, func(candidate row) bool { return candidate.Artifact.Name == "needle-resource" }) {
		t.Fatal("search-only row remained visible after whitespace search")
	}
	if !slices.ContainsFunc(m.rows, func(candidate row) bool { return candidate.Artifact.Name == "browse-resource" }) {
		t.Fatal("whitespace search did not restore browse rows")
	}
}

// spec: tui-startup/superseded-browse-results-cannot-restore-removed-endpoints
func TestBrowseReloadDropsNewlyDeniedEndpointRowsAndReportsPolicy(t *testing.T) {
	allowedEndpoint := config.MarketplaceEntry{URL: "https://allowed.example", InstanceID: "allowed", Alias: "allowed"}
	deniedEndpoint := config.MarketplaceEntry{URL: "https://denied.example", InstanceID: "denied", Alias: "denied"}
	allowed := []string{allowedEndpoint.URL}
	m := newTestModel()
	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{allowedEndpoint, deniedEndpoint}}
	m.applyBrowseDiscovery([]marketplace.DiscoveryOutcome{
		{Endpoint: allowedEndpoint, Items: []mpapi.ResourceListItem{tuiDiscoverySearchItem("registry-a", "skill", "allowed-row", "allowed")}, Total: 1},
		{Endpoint: deniedEndpoint, Items: []mpapi.ResourceListItem{tuiDiscoverySearchItem("registry-b", "skill", "denied-row", "denied")}, Total: 1},
	})
	oldKey, _, err := marketplaceBrowseRequestKey(m.mergedCfg)
	if err != nil {
		t.Fatal(err)
	}
	m.browseKey = oldKey
	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{allowedEndpoint, deniedEndpoint}, AllowedMarketplaces: &allowed}

	cmd := m.beginMarketplaceBrowse(false)
	defer m.cancelMarketplaceLoads()
	if cmd == nil {
		t.Fatal("allowed Marketplace did not start its replacement browse")
	}
	if !slices.ContainsFunc(m.browseDiscoveryRows, func(candidate row) bool { return candidate.Artifact.Name == "allowed-row" }) {
		t.Fatal("allowlisted Marketplace row disappeared during reload")
	}
	if slices.ContainsFunc(m.browseDiscoveryRows, func(candidate row) bool { return candidate.Artifact.Name == "denied-row" }) {
		t.Fatal("newly denied Marketplace row remained visible during reload")
	}
	if !slices.ContainsFunc(m.browseErrs, func(err error) bool { return strings.Contains(err.Error(), "denied") }) {
		t.Fatalf("newly denied Marketplace policy error is missing: %v", m.browseErrs)
	}
	if combined := m.marketplaceDiscoveryErrors(); len(combined) != 1 || !strings.Contains(combined[0].Error(), "denied") {
		t.Fatalf("combined Marketplace errors = %v, want the configured policy denial exactly once", combined)
	}
}

// spec: tui-startup/cold-startup-browses-beyond-the-first-page
func TestBrowsePendingMatchesAcquiredEndpointIdentity(t *testing.T) {
	for _, tc := range []struct {
		name       string
		configured config.MarketplaceEntry
		outcome    config.MarketplaceEntry
		want       int
	}{
		{
			name:       "unacquired URL accepts its acquired identity",
			configured: config.MarketplaceEntry{URL: "HTTPS://market.example/"},
			outcome:    config.MarketplaceEntry{URL: "https://market.example", InstanceID: "acquired"},
			want:       0,
		},
		{
			name:       "explicit identity stays bound",
			configured: config.MarketplaceEntry{URL: "https://market.example", InstanceID: "configured"},
			outcome:    config.MarketplaceEntry{URL: "https://market.example", InstanceID: "acquired"},
			want:       1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel()
			m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{tc.configured}}
			m.browsePending = []config.MarketplaceEntry{tc.configured}
			m.updateBrowsePending([]marketplace.DiscoveryOutcome{{Endpoint: tc.outcome, Total: 1}})
			if len(m.browsePending) != tc.want {
				t.Fatalf("pending endpoints = %#v, want %d", m.browsePending, tc.want)
			}
		})
	}
}

// spec: tui-startup/warm-startup-and-empty-endpoints-remain-usable
func TestFooterShowsFirstPagePendingAndKeepsSearchLimitWarning(t *testing.T) {
	requestStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("pending-market", "https://example.com", "https://example.com"))
			return
		}
		close(requestStarted)
		<-r.Context().Done()
	}))
	defer server.Close()

	endpoint := config.MarketplaceEntry{URL: server.URL, InstanceID: "pending-market"}
	m := newTestModel()
	m.width, m.height = 160, 24
	m.loading = false
	m.driftAssessment = driftReady
	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}
	cmd := m.beginMarketplaceBrowse(false)
	defer m.cancelMarketplaceBrowse()
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	select {
	case <-requestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("Marketplace did not begin its first browse request")
	}
	_, parts := m.footerStatus()
	footer := ansi.Strip(strings.Join(parts, " "))
	if !strings.Contains(footer, "Marketplace: 1 loading") {
		t.Fatalf("footer omitted the endpoint waiting for its first page: %q", footer)
	}

	m.discoveryOutcomes = []marketplace.DiscoveryOutcome{{Endpoint: endpoint, Truncated: true}}
	m.browseOutcomes = []marketplace.DiscoveryOutcome{{Endpoint: endpoint, Truncated: true}}
	_, parts = m.footerStatus()
	footer = ansi.Strip(strings.Join(parts, " "))
	if !strings.Contains(footer, "Marketplace: 1 loading") || !strings.Contains(footer, "Marketplace: 1 limited") {
		t.Fatalf("footer hid the search-limit warning during browse loading: %q", footer)
	}
	m.cancelMarketplaceBrowse()
	select {
	case <-result:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled first-page browse did not return")
	}
}

// spec: tui-startup/offline-partial-and-denied-discovery-preserve-useful-browsing
func TestFooterCountsFailedEndpointsAcrossSearchAndBrowseOnce(t *testing.T) {
	endpoint := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "same-market", Alias: "same"}
	m := newTestModel()
	m.width, m.height = 160, 24
	m.loading = false
	m.driftAssessment = driftReady
	m.applyDiscovery("needle", []marketplace.DiscoveryOutcome{{
		Endpoint: endpoint, Err: fmt.Errorf("search failed"),
	}})
	m.applyBrowseDiscovery([]marketplace.DiscoveryOutcome{{
		Endpoint: endpoint, Err: fmt.Errorf("browse failed"),
	}})

	_, parts := m.footerStatus()
	footer := ansi.Strip(strings.Join(parts, " "))
	if !strings.Contains(footer, "Marketplace: 1 failed") || strings.Contains(footer, "Marketplace: 2 failed") {
		t.Fatalf("footer failed endpoint count = %q, want one distinct endpoint", footer)
	}
	overlay := ansi.Strip(renderErrors(m))
	if !strings.Contains(overlay, "search failed") || !strings.Contains(overlay, "browse failed") {
		t.Fatalf("errors overlay omitted one Marketplace discovery failure: %q", overlay)
	}
}
