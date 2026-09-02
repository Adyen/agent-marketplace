package tui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	tea "github.com/charmbracelet/bubbletea"
)

func TestMarketplaceDiscoveryCommandPublishesFastEndpointBeforeSlowEndpointCompletes(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	blocked := make(chan struct{})
	release := make(chan struct{}, 1)
	server := func(instanceID, name string, wait <-chan struct{}) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/.well-known/agent-marketplace.json":
				_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown(instanceID, "https://example.com", "https://example.com"))
			case "/api/v1/search":
				if wait != nil {
					close(blocked)
					<-wait
				}
				item := mpapi.ResourceListItem{RegistryID: "registry-" + name, RegistrySlug: name, Kind: "skill", Name: name, Revision: "sha256:" + strings.Repeat("a", 64), ContentDigest: "sha256:" + strings.Repeat("a", 64), Availability: mpapi.AvailabilityAvailable}
				_ = json.NewEncoder(w).Encode(marketplace.SearchResponse{Items: []mpapi.ResourceListItem{item}, Total: 1, Limit: marketplace.DiscoveryPageLimit})
			default:
				http.NotFound(w, r)
			}
		}))
	}
	fast := server("fast-id", "fast", nil)
	defer fast.Close()
	slow := server("slow-id", "slow", release)
	defer slow.Close()
	defer func() {
		select {
		case release <- struct{}{}:
		default:
		}
	}()
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{
		{URL: fast.URL, InstanceID: "fast-id", Alias: "fast"},
		{URL: slow.URL, InstanceID: "slow-id", Alias: "slow"},
	}}

	m := newTestModel()
	generation := m.beginRequest(requestDiscovery)
	m.discoveryLoading = true
	messages := make(chan tea.Msg, 1)
	go func() { messages <- loadMarketplaceDiscovery(context.Background(), cfg, "", generation)() }()
	select {
	case <-blocked:
	case <-time.After(time.Second):
		t.Fatal("slow endpoint did not block")
	}
	var next tea.Cmd
	select {
	case raw := <-messages:
		msg, ok := raw.(discoveryLoadedMsg)
		if !ok {
			t.Fatalf("message = %T, want discoveryLoadedMsg", raw)
		}
		updated, cmd := m.Update(msg)
		m = updated.(Model)
		if cmd == nil {
			t.Fatal("partial discovery message did not schedule the next endpoint update")
		}
		next = cmd
		if len(m.discoveryRows) != 1 || m.discoveryRows[0].Artifact.Name != "fast" {
			t.Fatalf("visible discovery rows = %#v, want fast endpoint only", m.discoveryRows)
		}
		if !m.discoveryLoading {
			t.Fatal("discovery stopped loading while the slow endpoint was blocked")
		}
	case <-time.After(time.Second):
		t.Fatal("fast endpoint was not published before slow endpoint completed")
	}

	release <- struct{}{}
	updated, finalCmd := m.Update(next().(discoveryLoadedMsg))
	m = updated.(Model)
	if len(m.discoveryRows) != 2 || m.discoveryRows[0].Artifact.Name != "fast" || m.discoveryRows[1].Artifact.Name != "slow" {
		t.Fatalf("completed discovery rows = %#v, want deterministic fast, slow order", m.discoveryRows)
	}
	if finalCmd == nil {
		t.Fatal("last endpoint update did not schedule stream completion")
	}
	updated, _ = m.Update(finalCmd().(discoveryLoadedMsg))
	m = updated.(Model)
	if m.discoveryLoading || m.requests.active(requestDiscovery) != 0 {
		t.Fatalf("completed discovery remained active: loading=%t generation=%d", m.discoveryLoading, m.requests.active(requestDiscovery))
	}
}

func TestMarketplaceDiscoveryCompletionPreservesAuthoritativeRowsAndSelection(t *testing.T) {
	authoritative := row{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "enabled"}}}
	m := newTestModel()
	m.authoritativeRows = []row{authoritative}
	m.rows = []row{authoritative}
	m.recomputeView()
	m.cursor = 1 // first resource after the section header
	selected := m.selectionKey()
	generation := m.beginRequest(requestDiscovery)
	item := mpapi.ResourceListItem{RegistryID: "registry-id", RegistrySlug: "tools", Kind: "skill", Name: "remote", Revision: "sha256:" + strings.Repeat("a", 64), ContentDigest: "sha256:" + strings.Repeat("a", 64), Availability: mpapi.AvailabilityAvailable}
	outcome := marketplace.DiscoveryOutcome{Endpoint: config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market-id", Alias: "public"}, Items: []mpapi.ResourceListItem{item}, Total: 1, ObservedAt: time.Now()}

	updated, _ := m.Update(discoveryLoadedMsg{generation: generation, outcomes: []marketplace.DiscoveryOutcome{outcome}, done: true})
	m = updated.(Model)
	if len(m.rows) != 2 || m.rows[0].Artifact.Name != "enabled" || m.rows[1].Artifact.Name != "remote" {
		t.Fatalf("rows = %#v, want authoritative plus discovery", m.rows)
	}
	if got := m.selectionKey(); got != selected {
		t.Fatalf("selection changed from %q to %q", selected, got)
	}
	for i, entry := range m.flatItems {
		if entry.Kind == flatArtifact && entry.Row.Artifact.Name == "remote" {
			m.cursor = i
			break
		}
	}
	detail := renderDetail(m)
	for _, want := range []string{"remote", "public", "tools", "discovery only"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail missing %q:\n%s", want, detail)
		}
	}
}

func TestMarketplaceDiscoveryRejectsSupersededAndDuplicateCompletions(t *testing.T) {
	m := newTestModel()
	old := m.beginRequest(requestDiscovery)
	current := m.beginRequest(requestDiscovery)
	outcome := marketplace.DiscoveryOutcome{Endpoint: config.MarketplaceEntry{InstanceID: "market", Alias: "public"}, Items: []mpapi.ResourceListItem{{RegistryID: "registry", Kind: "skill", Name: "stale", Availability: mpapi.AvailabilityAvailable}}}

	updated, _ := m.Update(discoveryLoadedMsg{generation: old, outcomes: []marketplace.DiscoveryOutcome{outcome}, done: true})
	m = updated.(Model)
	if len(m.discoveryRows) != 0 {
		t.Fatalf("superseded completion changed rows: %#v", m.discoveryRows)
	}
	updated, _ = m.Update(discoveryLoadedMsg{generation: current, outcomes: []marketplace.DiscoveryOutcome{outcome}, done: true})
	m = updated.(Model)
	if len(m.discoveryRows) != 1 {
		t.Fatalf("current completion produced %d rows", len(m.discoveryRows))
	}
	m.discoveryRows[0].Artifact.Name = "accepted"
	updated, _ = m.Update(discoveryLoadedMsg{generation: current, outcomes: []marketplace.DiscoveryOutcome{outcome}, done: true})
	m = updated.(Model)
	if m.discoveryRows[0].Artifact.Name != "accepted" {
		t.Fatal("duplicate completion replayed")
	}
}

func TestMarketplaceDiscoveryRejectsOlderPartialReplay(t *testing.T) {
	m := newTestModel()
	generation := m.beginRequest(requestDiscovery)
	newer := marketplace.DiscoveryOutcome{Endpoint: config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}, Items: []mpapi.ResourceListItem{{RegistryID: "registry", Kind: "skill", Name: "newer", Availability: mpapi.AvailabilityAvailable}}}
	older := marketplace.DiscoveryOutcome{Endpoint: newer.Endpoint, Items: []mpapi.ResourceListItem{{RegistryID: "registry", Kind: "skill", Name: "older", Availability: mpapi.AvailabilityAvailable}}}

	updated, _ := m.Update(discoveryLoadedMsg{generation: generation, sequence: 2, outcomes: []marketplace.DiscoveryOutcome{newer}})
	m = updated.(Model)
	updated, _ = m.Update(discoveryLoadedMsg{generation: generation, sequence: 1, outcomes: []marketplace.DiscoveryOutcome{older}})
	m = updated.(Model)
	if len(m.discoveryRows) != 1 || m.discoveryRows[0].Artifact.Name != "newer" {
		t.Fatalf("older partial replay changed rows: %#v", m.discoveryRows)
	}
}

func TestMarketplaceDiscoveryRemovalCancelsAndRejectsLateCompletion(t *testing.T) {
	m := newTestModel()
	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: "https://market.example", InstanceID: "market"}}}
	generation := m.beginRequest(requestDiscovery)
	cancelled := false
	m.discoveryCancel = func() { cancelled = true }
	m.discoveryLoading = true
	m.discoverySequence = 7
	m.discoveryRows = []row{{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "old"}}}}
	m.browseDiscoveryRows = append([]row(nil), m.discoveryRows...)
	m.discoveryOutcomes = []marketplace.DiscoveryOutcome{{Endpoint: m.mergedCfg.Marketplaces[0]}}
	m.discoveryErrs = []error{errors.New("old failure")}
	m.mergedCfg = &config.Config{}

	if cmd := m.beginMarketplaceDiscovery("", false); cmd != nil {
		t.Fatal("zero Marketplace config scheduled discovery")
	}
	if !cancelled || m.discoveryCancel != nil || m.requests.active(requestDiscovery) != 0 || m.discoverySequence != 0 {
		t.Fatalf("discovery was not invalidated: cancelled=%t cancel=%v generation=%d sequence=%d", cancelled, m.discoveryCancel, m.requests.active(requestDiscovery), m.discoverySequence)
	}
	if m.discoveryLoading || len(m.discoveryRows) != 0 || len(m.browseDiscoveryRows) != 0 || len(m.discoveryOutcomes) != 0 || len(m.discoveryErrs) != 0 {
		t.Fatalf("removed Marketplace state retained: loading=%t rows=%#v browse=%#v outcomes=%#v errs=%#v", m.discoveryLoading, m.discoveryRows, m.browseDiscoveryRows, m.discoveryOutcomes, m.discoveryErrs)
	}
	late := marketplace.DiscoveryOutcome{Endpoint: config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}, Items: []mpapi.ResourceListItem{{RegistryID: "registry", Kind: "skill", Name: "late", Availability: mpapi.AvailabilityAvailable}}}
	updated, _ := m.Update(discoveryLoadedMsg{generation: generation, sequence: 8, outcomes: []marketplace.DiscoveryOutcome{late}})
	m = updated.(Model)
	if len(m.discoveryRows) != 0 || len(m.rows) != 0 {
		t.Fatalf("late completion repopulated removed Marketplace: rows=%#v all=%#v", m.discoveryRows, m.rows)
	}
}

func TestMarketplaceDiscoveryDedupesInstalledAliasByPinnedCoordinate(t *testing.T) {
	endpoint := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market", Alias: "public", Source: config.RepoCommitted}
	m := newTestModel()
	m.authoritativeRows = []row{{
		Artifact:              resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "installed-alias"}, IsMarketplace: true, MarketplaceInstanceID: endpoint.InstanceID, MarketplaceRegistryID: "registry"},
		MarketplaceSelections: map[config.Source]marketplaceSelection{config.RepoCommitted: {Entry: endpoint, Pin: config.MarketplaceResourcePin{RegistryID: "registry", Kind: "skill", Name: "original-name"}}},
	}}
	m.applyDiscovery("", []marketplace.DiscoveryOutcome{{Endpoint: endpoint, Items: []mpapi.ResourceListItem{{RegistryID: "registry", Kind: "skill", Name: "original-name", Availability: mpapi.AvailabilityAvailable}}}})
	if len(m.rows) != 1 || m.rows[0].Artifact.Name != "installed-alias" {
		t.Fatalf("discovery duplicated pinned resource under original name: %#v", m.rows)
	}
}

func TestMarketplaceDiscoveryDropsSameInstanceAtReplacedEndpoint(t *testing.T) {
	m := newTestModel()
	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: "https://old.example", InstanceID: "market", Alias: "old"}}}
	m.applyDiscovery("", []marketplace.DiscoveryOutcome{{Endpoint: m.mergedCfg.Marketplaces[0], Items: []mpapi.ResourceListItem{{RegistryID: "registry", Kind: "skill", Name: "old", Availability: mpapi.AvailabilityAvailable}}}})
	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: "https://new.example", InstanceID: "market", Alias: "new"}}}

	m.beginMarketplaceDiscovery("", true)
	if len(m.discoveryRows) != 0 || len(m.browseDiscoveryRows) != 0 {
		t.Fatalf("superseded endpoint rows retained: discovery=%#v browse=%#v", m.discoveryRows, m.browseDiscoveryRows)
	}
}

func TestMarketplaceConsentRetainsAndDelegatesPreparedInstall(t *testing.T) {
	m := newTestModel()
	operation, ok := m.beginOperation(operationMutation)
	if !ok {
		t.Fatal("failed to start install operation")
	}
	prepared := &pipeline.PreparedMarketplaceInstall{}
	consent := &pipeline.MarketplaceConsentError{Preview: "verified", Prepared: prepared, Err: errors.New("consent required")}
	updated, _ := m.Update(discoveryInstallMsg{operation: operation, options: pipeline.InstallOptions{RepoRoot: t.TempDir()}, err: consent})
	m = updated.(Model)
	if m.marketplacePreparedInstall != prepared || m.pane != paneMarketplaceConsent {
		t.Fatal("consent did not retain the exact prepared install")
	}
	updated, cmd := m.handleMarketplaceConsentKey(keyPress('y'))
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("consent did not delegate prepared commit")
	}
	result := cmd().(discoveryInstallMsg)
	if result.err == nil || !strings.Contains(result.err.Error(), "not prepared") {
		t.Fatalf("consent reran install instead of committing captured plan: %v", result.err)
	}
}

func TestMarketplaceInstallOperationRemainsSerializedThroughReload(t *testing.T) {
	m := newTestModel()
	operation, ok := m.beginOperation(operationMutation)
	if !ok {
		t.Fatal("failed to start install operation")
	}
	m.marketplaceInstallOperation = operation
	updated, cmd := m.Update(discoveryInstallMsg{operation: operation, result: &pipeline.InstallResult{Installed: []string{"review"}}})
	m = updated.(Model)
	if cmd == nil || m.marketplaceInstallReload == 0 {
		t.Fatal("successful install did not schedule authoritative reload")
	}
	if _, ok := m.beginOperation(operationMutation); ok {
		t.Fatal("second mutation started before authoritative reload")
	}
	generation := m.marketplaceInstallReload
	updated, _ = m.Update(snapshotLoadedMsg{generation: generation, mergedCfg: &config.Config{}})
	m = updated.(Model)
	if m.operations.active != nil || m.marketplaceInstallReload != 0 {
		t.Fatalf("install operation remained active after reload: %#v", m.operations.active)
	}
}

func TestMarketplaceInstallReloadFinishesOnceFromSupersedingUpdateSnapshot(t *testing.T) {
	m := newTestModel()
	operation, ok := m.beginOperation(operationMutation)
	if !ok {
		t.Fatal("failed to start install operation")
	}
	m.marketplaceInstallOperation = operation
	updated, _ := m.Update(discoveryInstallMsg{operation: operation, result: &pipeline.InstallResult{Installed: []string{"review"}}})
	m = updated.(Model)
	installGeneration := m.marketplaceInstallReload
	updateGeneration := m.beginRequest(requestSnapshot)

	updated, _ = m.Update(snapshotLoadedMsg{generation: installGeneration, mergedCfg: &config.Config{}})
	m = updated.(Model)
	if m.operations.active == nil {
		t.Fatal("stale install snapshot finished the operation")
	}
	updated, _ = m.Update(updateDoneMsg{generation: updateGeneration, snapshot: snapshotLoadedMsg{generation: updateGeneration, mergedCfg: &config.Config{}}})
	m = updated.(Model)
	if m.operations.active != nil || m.marketplaceInstallReload != 0 {
		t.Fatalf("superseding update snapshot did not finish install: active=%#v reload=%d", m.operations.active, m.marketplaceInstallReload)
	}
	if m.toast != "Marketplace resource installed" {
		t.Fatalf("install outcome = %q", m.toast)
	}
	later, ok := m.beginOperation(operationMutation)
	if !ok {
		t.Fatal("later mutation remained blocked")
	}
	updated, _ = m.Update(snapshotLoadedMsg{generation: installGeneration, mergedCfg: &config.Config{}})
	m = updated.(Model)
	if !m.operations.accepts(later) {
		t.Fatal("stale original snapshot finished the later operation")
	}
}

func TestAliaslessMarketplaceEndpointDisplayIsCanonicalAndStable(t *testing.T) {
	m := newTestModel()
	m.loading = false
	endpoint := config.MarketplaceEntry{URL: "HTTPS://Z.EXAMPLE/", InstanceID: "z"}
	item := mpapi.ResourceListItem{RegistryID: "registry", RegistrySlug: "tools", Kind: "skill", Name: "review", Availability: mpapi.AvailabilityAvailable}
	m.applyDiscovery("", []marketplace.DiscoveryOutcome{
		{Endpoint: endpoint, Items: []mpapi.ResourceListItem{item}, Err: errors.New("unavailable")},
		{Endpoint: config.MarketplaceEntry{URL: "https://a.example", InstanceID: "a"}, Items: []mpapi.ResourceListItem{item}},
	})
	if got := m.discoveryRows[1].RegistryName; got != "https://z.example/tools" {
		t.Fatalf("alias-less row registry = %q", got)
	}
	if got := m.discoveryRows[0].MarketplaceDiscovery.Endpoint.InstanceID; got != "a" {
		t.Fatalf("alias-less ordering first instance = %q, want a", got)
	}
	if len(m.discoveryErrs) != 1 || !strings.HasPrefix(m.discoveryErrs[0].Error(), "https://z.example: ") {
		t.Fatalf("alias-less error prefix = %v", m.discoveryErrs)
	}
	m.rows = m.discoveryRows
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	for i, flat := range m.flatItems {
		if flat.Kind == flatArtifact && flat.Row.MarketplaceDiscovery.Endpoint.InstanceID == "z" {
			m.cursor = i
			break
		}
	}
	if detail := renderDetail(m); !strings.Contains(detail, "https://z.example") {
		t.Fatalf("alias-less detail omitted canonical endpoint: %q", detail)
	}
}

func TestHostileMarketplaceDiscoveryRenderingUsesTerminalSanitizer(t *testing.T) {
	m := newTestModel()
	m.loading = false
	m.width, m.height = 80, 20
	hostile := "remote\x1b]52;c;c2VjcmV0\a\u202e\nname"
	m.applyDiscovery("", []marketplace.DiscoveryOutcome{{Endpoint: config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market", Alias: hostile}, Items: []mpapi.ResourceListItem{{RegistryID: "registry", RegistrySlug: hostile, Kind: "skill", Name: hostile, Description: hostile, Availability: mpapi.AvailabilityAvailable}}}})
	output := m.View()
	if strings.Contains(output, "\x1b]52;") || strings.ContainsAny(output, "\a\u202e") || strings.Contains(output, "\nname") {
		t.Fatalf("hostile discovery data reached terminal output: %q", output)
	}
	if !strings.Contains(output, `\x1B`) {
		t.Fatalf("sanitized evidence missing from output: %q", output)
	}
}

func TestMarketplaceConsentFitsSupportedMinimum(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 50, 12
	m.pane = paneMarketplaceConsent
	m.marketplaceInstallPreview = strings.Repeat("verified capability and finding ", 20)
	assertTerminalBounds(t, m.View(), 50, 12)
}

func TestMarketplaceDiscoveryInstallCapturesExactLocatorAndScope(t *testing.T) {
	digest := "sha256:" + strings.Repeat("b", 64)
	revision := "registry-revision-7"
	r := row{MarketplaceDiscovery: &marketplaceDiscovery{Endpoint: config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market-id", Alias: "public"}, Item: mpapi.ResourceListItem{RegistryID: "registry-id", RegistrySlug: "tools", Kind: "skill", Name: "review", Revision: revision, ContentDigest: digest, Availability: mpapi.AvailabilityAvailable}}}
	opts, err := discoveryInstallOptions("/repo", r, scopeLocal)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Scope != "local" || !opts.ScopeSpecified {
		t.Fatalf("scope = %q specified=%t", opts.Scope, opts.ScopeSpecified)
	}
	want := "public:registry-id/skill/review@" + digest
	if len(opts.Args) != 1 || opts.Args[0] != want || opts.Marketplace != "public" {
		t.Fatalf("options = %#v, want exact locator %q", opts, want)
	}
	if opts.ExactMarketplaceLocator == nil || opts.ExactMarketplaceLocator.Revision != revision || opts.ExactMarketplaceLocator.Digest != digest || opts.ExactMarketplaceLocator.Registry != "registry-id" {
		t.Fatalf("typed exact locator = %#v", opts.ExactMarketplaceLocator)
	}
}

func TestUnavailableMarketplaceDiscoveryRowsCannotOpenInstallScope(t *testing.T) {
	for _, availability := range []mpapi.Availability{mpapi.AvailabilityBlocked, mpapi.AvailabilityRevoked, "unknown"} {
		t.Run(string(availability), func(t *testing.T) {
			m := newTestModel()
			m.loading = false
			m.applyDiscovery("", []marketplace.DiscoveryOutcome{{Endpoint: config.MarketplaceEntry{InstanceID: "market", Alias: "public"}, Items: []mpapi.ResourceListItem{{RegistryID: "registry", RegistrySlug: "tools", Kind: "skill", Name: "unavailable", Availability: availability}}}})
			for i, item := range m.flatItems {
				if item.Kind == flatArtifact {
					m.cursor = i
					break
				}
			}
			updated, cmd := m.Update(keyPress(' '))
			m = updated.(Model)
			if m.pane == paneToggleModal {
				t.Fatal("unavailable discovery row opened install scope modal")
			}
			if cmd == nil {
				t.Fatal("unavailable discovery row did not explain refusal")
			}
		})
	}
}
