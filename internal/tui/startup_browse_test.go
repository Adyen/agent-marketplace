package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

func startupBrowseItems(count int) []mpapi.ResourceListItem {
	items := make([]mpapi.ResourceListItem, count)
	for i := range items {
		items[i] = tuiDiscoverySearchItem("registry", "skill", fmt.Sprintf("resource-%02d", i), "startup browse resource")
	}
	return items
}

func drainStartupDiscovery(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	for updates := 0; cmd != nil && updates < 8; updates++ {
		msg := cmd()
		updated, next := m.Update(msg)
		*m = updated.(Model)
		cmd = next
	}
	if cmd != nil {
		t.Fatal("startup discovery produced more than eight snapshots")
	}
}

// spec: tui-startup/cold-startup-browses-beyond-the-first-page
// spec: tui-startup/warm-startup-and-empty-endpoints-remain-usable
func TestStartupBrowseCoalescesEquivalentLoadsAcrossStages(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	items := startupBrowseItems(marketplace.DiscoveryPageLimit + 1)
	var searches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("coalesced-market", "https://example.com", "https://example.com"))
			return
		}
		searches.Add(1)
		offset, limit := 0, marketplace.DiscoveryPageLimit
		_, _ = fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &offset)
		_, _ = fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
		_ = json.NewEncoder(w).Encode(marketplace.SearchResponse{Items: items[offset:min(offset+limit, len(items))], Total: len(items), Offset: offset, Limit: limit})
	}))
	defer server.Close()
	endpoint := config.MarketplaceEntry{URL: server.URL, InstanceID: "coalesced-market", Alias: "market"}
	m := newTestModel()
	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}

	cmd := m.beginMarketplaceDiscovery("", false)
	if cmd == nil {
		t.Fatal("startup did not begin the browse load")
	}
	if duplicate := m.beginMarketplaceDiscovery("", false); duplicate != nil {
		t.Fatal("equivalent startup stage restarted an in-flight full browse load")
	}
	drainStartupDiscovery(t, &m, cmd)
	if got := searches.Load(); got != 2 {
		t.Fatalf("search requests = %d, want one request per catalog page", got)
	}
	if len(m.browseDiscoveryRows) != len(items) {
		t.Fatalf("browse rows = %d, want every resource (%d)", len(m.browseDiscoveryRows), len(items))
	}
	if duplicate := m.beginMarketplaceDiscovery("", false); duplicate != nil {
		t.Fatal("equivalent final snapshot restarted a finished browse load")
	}
	if got := searches.Load(); got != 2 {
		t.Fatalf("finished browse repeated network requests: %d", got)
	}
}

// spec: tui-startup/warm-startup-and-empty-endpoints-remain-usable
func TestUserReloadRestartsFinishedMarketplaceBrowse(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	m := newTestModel()
	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: "https://market.example", InstanceID: "market", Alias: "market"}}}
	key, _, err := marketplaceBrowseRequestKey(m.mergedCfg)
	if err != nil {
		t.Fatal(err)
	}
	m.browseKey = key
	m.browseFinished = true

	updated, reload := m.Update(keyPress('r'))
	m = updated.(Model)
	if reload == nil {
		t.Fatal("reload key did not schedule a snapshot")
	}
	msg := reload()
	snapshot, ok := msg.(reloadMsg)
	if !ok {
		t.Fatalf("reload key produced %T, want an explicit reload", msg)
	}
	snapshot.mergedCfg = m.mergedCfg
	updated, browse := m.Update(snapshot)
	m = updated.(Model)
	if !m.browseLoading || browse == nil {
		t.Fatalf("user reload did not restart the finished Marketplace browse: loading=%v command=%v marketplaces=%d errors=%v", m.browseLoading, browse != nil, len(m.mergedCfg.Marketplaces), snapshot.errs)
	}
	m.cancelMarketplaceLoads()
}

// spec: tui-startup/search-during-loading-and-clear-restore-complete-browsing
func TestSearchDuringStartupBrowseKeepsArrivingRowsAndRestoresBrowseState(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	items := startupBrowseItems(marketplace.DiscoveryPageLimit + 10)
	enteredLaterPage := make(chan struct{}, 1)
	releaseLaterPage := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("held-browse", "https://example.com", "https://example.com"))
			return
		}
		offset, limit := 0, marketplace.DiscoveryPageLimit
		_, _ = fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &offset)
		_, _ = fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
		if offset >= marketplace.DiscoveryPageLimit && r.URL.Query().Get("q") == "" {
			select {
			case enteredLaterPage <- struct{}{}:
			default:
			}
			select {
			case <-releaseLaterPage:
			case <-r.Context().Done():
				return
			}
		}
		_ = json.NewEncoder(w).Encode(marketplace.SearchResponse{Items: items[offset:min(offset+limit, len(items))], Total: len(items), Offset: offset, Limit: limit})
	}))
	defer server.Close()
	defer func() {
		select {
		case <-releaseLaterPage:
		default:
			close(releaseLaterPage)
		}
	}()
	endpoint := config.MarketplaceEntry{URL: server.URL, InstanceID: "held-browse", Alias: "market"}
	local := mkRow("resource-55-local", withGlobal, withRegistry("local", "local"))
	m := newTestModel()
	m.width, m.height = 100, 32
	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}
	m.authoritativeRows = []row{local}
	m.rows = []row{local}
	m.filter = ""
	m.recomputeView()

	cmd := m.beginMarketplaceDiscovery("", false)
	first, ok := cmd().(discoveryLoadedMsg)
	if !ok {
		t.Fatal("browse command did not return a discovery snapshot")
	}
	updated, browseWait := m.Update(first)
	m = updated.(Model)
	if len(m.browseDiscoveryRows) != marketplace.DiscoveryPageLimit {
		t.Fatalf("first page rows = %d, want %d", len(m.browseDiscoveryRows), marketplace.DiscoveryPageLimit)
	}
	select {
	case <-enteredLaterPage:
	case <-time.After(2 * time.Second):
		t.Fatal("later browse page did not start while the first page was visible")
	}
	if !strings.Contains(m.View().Content, "Marketplace: 1 loading") {
		t.Fatalf("partial browse load feedback is missing: %s", m.View().Content)
	}

	m.currentFoldState()["status:available"] = true
	m.pane = paneDetail
	m.detailKey = rowKey(local)
	selected := m.selectionKeyForRebuild()
	m.filter = "resource-55"
	m.recomputeView()
	searchCmd := m.beginMarketplaceDiscovery(m.filter, false)
	drainStartupDiscovery(t, &m, searchCmd)
	assertMatchingRows := func(want int) {
		t.Helper()
		if len(m.filteredRows) != want {
			t.Fatalf("visible rows matching %q = %d, want %d: %#v", m.filter, len(m.filteredRows), want, m.filteredRows)
		}
		for _, filtered := range m.filteredRows {
			if !strings.Contains(filtered.Artifact.Name, "resource-55") {
				t.Fatalf("nonmatching row %q remained visible for %q", filtered.Artifact.Name, m.filter)
			}
		}
	}
	assertMatchingRows(1)
	if m.filter != "resource-55" || m.selectionKeyForRebuild() != selected || m.pane != paneDetail || m.detailKey != rowKey(local) {
		t.Fatalf("search changed browse selection/detail state: filter=%q selection=%q want=%q pane=%v detail=%q", m.filter, m.selectionKeyForRebuild(), selected, m.pane, m.detailKey)
	}

	close(releaseLaterPage)
	for updates := 0; browseWait != nil && updates < 8; updates++ {
		msg := browseWait()
		updated, next := m.Update(msg)
		m = updated.(Model)
		browseWait = next
		if len(m.browseDiscoveryRows) == len(items) {
			assertMatchingRows(2)
		} else {
			assertMatchingRows(1)
		}
	}
	if browseWait != nil {
		t.Fatal("browse did not finish after releasing the later page")
	}
	if m.filter != "resource-55" || m.selectionKeyForRebuild() != selected || m.pane != paneDetail || m.detailKey != rowKey(local) {
		t.Fatalf("arriving page changed active search/detail state: filter=%q selection=%q want=%q pane=%v detail=%q", m.filter, m.selectionKeyForRebuild(), selected, m.pane, m.detailKey)
	}
	if len(m.browseDiscoveryRows) != len(items) || m.browseDiscoveryRows[len(items)-1].Artifact.Name != "resource-59" {
		t.Fatalf("browse inventory after later page = %d rows", len(m.browseDiscoveryRows))
	}
	if !m.foldedSections[m.viewSettings.normalized()]["status:available"] {
		t.Fatal("arriving page changed the saved Available fold")
	}

	updated, _ = m.clearSearch()
	m = updated.(Model)
	if m.filter != "" || len(m.browseDiscoveryRows) != len(items) || len(m.filteredRows) != len(items)+1 {
		t.Fatalf("clear did not restore complete browse inventory: filter=%q rows=%d", m.filter, len(m.browseDiscoveryRows))
	}
	if m.selectionKeyForRebuild() != selected || m.pane != paneDetail || m.detailKey != rowKey(local) || !m.currentFoldState()["status:available"] {
		t.Fatalf("clear changed selection/detail/folds: selection=%q want=%q pane=%v detail=%q folds=%v", m.selectionKeyForRebuild(), selected, m.pane, m.detailKey, m.currentFoldState())
	}
	names := make([]string, 0, len(m.browseDiscoveryRows))
	for _, r := range m.browseDiscoveryRows {
		names = append(names, r.Artifact.Name)
	}
	if !slices.Contains(names, "resource-00") || !slices.Contains(names, "resource-59") {
		t.Fatalf("cleared browse does not contain both ends of the catalog: %q/%q", names[0], names[len(names)-1])
	}
}

// spec: tui-startup/superseded-browse-results-cannot-restore-removed-endpoints
func TestReloadReplacesHeldStartupBrowseEndpoint(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	oldItems := startupBrowseItems(marketplace.DiscoveryPageLimit + 1)
	oldPageCanceled := make(chan struct{})
	oldEntered := make(chan struct{}, 1)
	oldServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("old-market", "https://example.com", "https://example.com"))
			return
		}
		offset, limit := 0, marketplace.DiscoveryPageLimit
		_, _ = fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &offset)
		_, _ = fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
		if offset > 0 {
			select {
			case oldEntered <- struct{}{}:
			default:
			}
			<-r.Context().Done()
			close(oldPageCanceled)
			return
		}
		_ = json.NewEncoder(w).Encode(marketplace.SearchResponse{Items: oldItems[offset:min(offset+limit, len(oldItems))], Total: len(oldItems), Offset: offset, Limit: limit})
	}))
	defer oldServer.Close()
	newServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("new-market", "https://example.com", "https://example.com"))
			return
		}
		item := tuiDiscoverySearchItem("new-registry", "skill", "replacement-resource", "new endpoint")
		_ = json.NewEncoder(w).Encode(marketplace.SearchResponse{Items: []mpapi.ResourceListItem{item}, Total: 1, Offset: 0, Limit: marketplace.DiscoveryPageLimit})
	}))
	defer newServer.Close()
	oldEndpoint := config.MarketplaceEntry{URL: oldServer.URL, InstanceID: "old-market", Alias: "old"}
	newEndpoint := config.MarketplaceEntry{URL: newServer.URL, InstanceID: "new-market", Alias: "new"}
	m := newTestModel()
	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{oldEndpoint}}
	oldCmd := m.beginMarketplaceDiscovery("", false)
	first, ok := oldCmd().(discoveryLoadedMsg)
	if !ok {
		t.Fatal("old browse did not publish its first page")
	}
	updated, oldWait := m.Update(first)
	m = updated.(Model)
	select {
	case <-oldEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("old endpoint later page was not held")
	}
	oldGeneration := m.requests.active(requestBrowse)
	oldSequence := m.browseSequence
	oldCancel := m.browseCancel
	cancelCalled := false
	m.browseCancel = func() {
		cancelCalled = true
		oldCancel()
	}

	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{newEndpoint}}
	newCmd := m.beginMarketplaceDiscovery("", false)
	if newCmd == nil {
		t.Fatal("replacement endpoint did not start a browse load")
	}
	if !cancelCalled || m.requests.active(requestBrowse) == oldGeneration {
		t.Fatal("replacement browse did not synchronously cancel and supersede the old request")
	}
	drainStartupDiscovery(t, &m, newCmd)
	select {
	case <-oldPageCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("reloaded endpoint did not cancel the superseded page")
	}
	if len(m.browseDiscoveryRows) != 1 || m.browseDiscoveryRows[0].Artifact.Name != "replacement-resource" {
		t.Fatalf("replacement browse rows = %#v", m.browseDiscoveryRows)
	}
	if oldWait != nil {
		_ = oldWait()
	}
	late := discoveryLoadedMsg{
		generation: oldGeneration,
		sequence:   oldSequence + 1,
		browse:     true,
		outcomes: []marketplace.DiscoveryOutcome{{
			Endpoint: oldEndpoint,
			Items:    oldItems,
			Total:    len(oldItems),
		}},
	}
	updated, _ = m.Update(late)
	m = updated.(Model)
	if !slices.ContainsFunc(m.browseDiscoveryRows, func(candidate row) bool { return candidate.Artifact.Name == "replacement-resource" }) {
		t.Fatalf("late result erased the replacement inventory: %#v", m.browseDiscoveryRows)
	}
	for _, r := range m.browseDiscoveryRows {
		if r.MarketplaceDiscovery.Endpoint.InstanceID != newEndpoint.InstanceID {
			t.Fatalf("superseded endpoint restored row %q", r.Artifact.Name)
		}
	}
}

// spec: tui-startup/offline-partial-and-denied-discovery-preserve-useful-browsing
func TestQuitCancelsPendingStartupBrowse(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	requestStarted := make(chan struct{})
	requestCanceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("quit-market", "https://example.com", "https://example.com"))
			return
		}
		close(requestStarted)
		<-r.Context().Done()
		close(requestCanceled)
	}))
	defer server.Close()
	endpoint := config.MarketplaceEntry{URL: server.URL, InstanceID: "quit-market"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := newTestModel()
	m.width, m.height = 80, 24
	m.ctx = ctx
	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}
	cmd := m.beginMarketplaceDiscovery("", false)
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	select {
	case <-requestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("startup browse request did not begin")
	}
	updated, quit := m.Update(keyPress('q'))
	m = updated.(Model)
	if quit == nil {
		t.Fatal("quit did not return the quit command")
	}
	if _, ok := quit().(tea.QuitMsg); !ok {
		t.Fatal("quit command did not produce tea.QuitMsg")
	}
	select {
	case <-requestCanceled:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("quit left the pending browse HTTP request active")
	}
	select {
	case <-result:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled browse command did not return")
	}
}

// spec: tui-startup/superseded-browse-results-cannot-restore-removed-endpoints
func TestSearchDropsRowsFromRemovedMarketplaceImmediately(t *testing.T) {
	m := newTestModel()
	endpoint := config.MarketplaceEntry{URL: "https://old.example", InstanceID: "old-market", Alias: "old"}
	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}
	m.filter = "needle"
	outcome := marketplace.DiscoveryOutcome{
		Endpoint: endpoint,
		Items:    []mpapi.ResourceListItem{tuiDiscoverySearchItem("old-registry", "skill", "needle-resource", "removed endpoint")},
	}
	m.applyDiscovery(m.filter, []marketplace.DiscoveryOutcome{outcome})
	if !slices.ContainsFunc(m.rows, func(candidate row) bool { return candidate.Artifact.Name == "needle-resource" }) {
		t.Fatal("test setup did not display the configured Marketplace row")
	}

	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: "https://new.example", InstanceID: "new-market", Alias: "new"}}}
	m.beginMarketplaceDiscovery(m.filter, true)
	m.cancelMarketplaceDiscovery()
	if slices.ContainsFunc(m.rows, func(candidate row) bool { return candidate.Artifact.Name == "needle-resource" }) {
		t.Fatal("removed Marketplace row remained visible after the search began")
	}
}

// spec: tui-startup/offline-partial-and-denied-discovery-preserve-useful-browsing
func TestStartupBrowseFailureAppearsInErrorsOverlay(t *testing.T) {
	m := newTestModel()
	endpoint := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "offline-market", Alias: "offline"}
	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}
	m.applyBrowseDiscovery([]marketplace.DiscoveryOutcome{{
		Endpoint: endpoint,
		Items:    startupBrowseItems(1),
		Err:      fmt.Errorf("later page unavailable"),
	}})

	if !m.hasDiagnostics() {
		t.Fatal("browse failure was not counted as a diagnostic")
	}
	overlay := renderErrors(m)
	if !strings.Contains(overlay, "Marketplace discovery (1)") || !strings.Contains(overlay, "later page unavailable") {
		t.Fatalf("errors overlay omitted the browse failure: %s", overlay)
	}
}
