package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

func TestToggleSnapshotReloadPreservesNonEmptyMarketplaceQuery(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	queries := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("market-id", "https://example.com", "https://example.com"))
		case "/api/v1/search":
			queries <- r.URL.Query().Get("q")
			_ = json.NewEncoder(w).Encode(marketplace.SearchResponse{Limit: marketplace.DiscoveryPageLimit})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	m := newTestModel()
	m.filter = "remote query"
	generation := m.requests.active(requestSnapshot)
	updated, cmd := m.Update(snapshotLoadedMsg{
		generation:      generation,
		mergedCfg:       &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: server.URL, InstanceID: "market-id"}}},
		driftAssessment: driftReady,
	})
	m = updated.(Model)
	if m.filter != "remote query" {
		t.Fatalf("filter = %q, want remote query", m.filter)
	}
	commands := batchCmds(cmd)
	for _, candidate := range commands {
		_ = candidate()
	}
	select {
	case got := <-queries:
		if got != "remote query" {
			t.Fatalf("remote query = %q, want %q", got, "remote query")
		}
	case <-time.After(time.Second):
		t.Fatal("snapshot reload did not issue Marketplace search")
	}
}

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

func TestMarketplaceDiscoveryAllowlistTighteningClearsPriorStateAndRejectsLateCompletions(t *testing.T) {
	endpoint := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market", Alias: "public"}
	authoritative := row{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "installed"}}}
	m := newTestModel()
	m.authoritativeRows = []row{authoritative}
	m.rows = []row{authoritative}
	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}
	m.applyDiscovery("", []marketplace.DiscoveryOutcome{{
		Endpoint:  endpoint,
		Items:     []mpapi.ResourceListItem{{RegistryID: "registry", Kind: "skill", Name: "now-blocked", Availability: mpapi.AvailabilityAvailable}},
		Total:     75,
		Truncated: true,
		Err:       errors.New("old endpoint failure"),
	}})
	generation := m.beginRequest(requestDiscovery)
	m.discoverySequence = 4
	m.discoveryLoading = true
	cancelled := false
	m.discoveryCancel = func() { cancelled = true }
	allowed := []string{`^allowed\.example$`}
	m.mergedCfg.AllowedMarketplaces = &allowed

	if cmd := m.beginMarketplaceDiscovery("", false); cmd != nil {
		t.Fatal("invalid Marketplace config scheduled discovery")
	}
	if !cancelled || m.discoveryCancel != nil || m.requests.active(requestDiscovery) != 0 || m.discoverySequence != 0 || m.discoveryLoading {
		t.Fatalf("prior discovery remained active: cancelled=%t cancel=%v generation=%d sequence=%d loading=%t", cancelled, m.discoveryCancel, m.requests.active(requestDiscovery), m.discoverySequence, m.discoveryLoading)
	}
	if len(m.discoveryRows) != 0 || len(m.browseDiscoveryRows) != 0 || len(m.discoveryOutcomes) != 0 {
		t.Fatalf("blocked endpoint metadata remained: rows=%#v browse=%#v outcomes=%#v", m.discoveryRows, m.browseDiscoveryRows, m.discoveryOutcomes)
	}
	if len(m.rows) != 1 || m.rows[0].Artifact.Name != "installed" {
		t.Fatalf("authoritative rows were not rebuilt: %#v", m.rows)
	}
	if len(m.discoveryErrs) != 1 {
		t.Fatalf("discovery errors = %v, want only current validation error", m.discoveryErrs)
	}
	var blocked *config.MarketplaceURLBlockedError
	if !errors.As(m.discoveryErrs[0], &blocked) {
		t.Fatalf("discovery error = %v, want MarketplaceURLBlockedError", m.discoveryErrs[0])
	}
	late := marketplace.DiscoveryOutcome{Endpoint: endpoint, Items: []mpapi.ResourceListItem{{RegistryID: "registry", Kind: "skill", Name: "late", Availability: mpapi.AvailabilityAvailable}}}
	updated, _ := m.Update(discoveryLoadedMsg{generation: generation, sequence: 5, outcomes: []marketplace.DiscoveryOutcome{late}})
	m = updated.(Model)
	updated, _ = m.Update(discoveryLoadedMsg{generation: generation, outcomes: []marketplace.DiscoveryOutcome{late}, done: true})
	m = updated.(Model)
	if len(m.discoveryRows) != 0 || len(m.discoveryOutcomes) != 0 || len(m.rows) != 1 || m.rows[0].Artifact.Name != "installed" {
		t.Fatalf("late prior completion changed cleared state: discovery=%#v outcomes=%#v rows=%#v", m.discoveryRows, m.discoveryOutcomes, m.rows)
	}
}

func TestSnapshotPolicyFailureClearsDiscoveryAndRejectsLateCompletions(t *testing.T) {
	for _, test := range []struct {
		name     string
		snapshot snapshotLoadedMsg
		wantPane pane
	}{
		{
			name: "config error",
			snapshot: snapshotLoadedMsg{
				driftAssessment: driftFailed,
				errs:            []error{errors.New("invalid effective config")},
			},
			wantPane: paneList,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			endpoint := config.MarketplaceEntry{URL: "https://old.example", InstanceID: "old", Alias: "old"}
			authoritative := row{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "installed"}}}
			m := newTestModel()
			m.authoritativeRows = []row{authoritative}
			m.rows = []row{authoritative}
			m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}
			m.applyDiscovery("", []marketplace.DiscoveryOutcome{{Endpoint: endpoint, Items: []mpapi.ResourceListItem{{RegistryID: "registry", Kind: "skill", Name: "old", Availability: mpapi.AvailabilityAvailable}}, Err: errors.New("old endpoint failure")}})
			discoveryGeneration := m.beginRequest(requestDiscovery)
			m.discoverySequence = 3
			m.discoveryLoading = true
			cancelled := false
			m.discoveryCancel = func() { cancelled = true }
			test.snapshot.generation = m.requests.active(requestSnapshot)

			updated, _ := m.Update(test.snapshot)
			m = updated.(Model)
			if !cancelled || m.requests.active(requestDiscovery) != 0 || m.discoverySequence != 0 || m.discoveryLoading {
				t.Fatalf("active discovery was not invalidated: cancelled=%t generation=%d sequence=%d loading=%t", cancelled, m.requests.active(requestDiscovery), m.discoverySequence, m.discoveryLoading)
			}
			if len(m.discoveryRows) != 0 || len(m.browseDiscoveryRows) != 0 || len(m.discoveryOutcomes) != 0 || len(m.discoveryErrs) != 0 {
				t.Fatalf("prior discovery state remained: rows=%#v browse=%#v outcomes=%#v errs=%v", m.discoveryRows, m.browseDiscoveryRows, m.discoveryOutcomes, m.discoveryErrs)
			}
			if len(m.rows) != 1 || m.rows[0].Artifact.Name != "installed" {
				t.Fatalf("authoritative rows changed: %#v", m.rows)
			}
			if m.pane != test.wantPane {
				t.Fatalf("pane = %v, want %v", m.pane, test.wantPane)
			}
			if test.name == "config error" && (len(m.errs) != 1 || m.errs[0].Error() != "invalid effective config") {
				t.Fatalf("snapshot error behavior changed: %v", m.errs)
			}

			late := marketplace.DiscoveryOutcome{Endpoint: endpoint, Items: []mpapi.ResourceListItem{{RegistryID: "registry", Kind: "skill", Name: "late", Availability: mpapi.AvailabilityAvailable}}}
			updated, _ = m.Update(discoveryLoadedMsg{generation: discoveryGeneration, sequence: 4, outcomes: []marketplace.DiscoveryOutcome{late}})
			m = updated.(Model)
			updated, _ = m.Update(discoveryLoadedMsg{generation: discoveryGeneration, outcomes: []marketplace.DiscoveryOutcome{late}, done: true})
			m = updated.(Model)
			if len(m.discoveryRows) != 0 {
				t.Fatalf("late discovery changed discovery state: %#v", m.discoveryRows)
			}
			if len(m.rows) != 1 || m.rows[0].Artifact.Name != "installed" {
				t.Fatalf("late discovery changed authoritative state: %#v", m.rows)
			}
		})
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
	old := config.MarketplaceEntry{URL: "https://old.example", InstanceID: "market", Alias: "old"}
	remaining := config.MarketplaceEntry{URL: "https://remaining.example", InstanceID: "remaining", Alias: "remaining"}
	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{old, remaining}}
	m.applyDiscovery("", []marketplace.DiscoveryOutcome{
		{Endpoint: old, Total: 75, Truncated: true, Err: errors.New("old failure")},
		{Endpoint: remaining, Total: 1, Err: errors.New("remaining failure")},
	})
	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{
		{URL: "https://new.example", InstanceID: "market", Alias: "new"},
		remaining,
	}}

	m.beginMarketplaceDiscovery("", true)
	if len(m.discoveryRows) != 0 || len(m.browseDiscoveryRows) != 0 {
		t.Fatalf("superseded endpoint rows retained: discovery=%#v browse=%#v", m.discoveryRows, m.browseDiscoveryRows)
	}
	if len(m.discoveryOutcomes) != 1 || m.discoveryOutcomes[0].Endpoint.InstanceID != "remaining" {
		t.Fatalf("superseded endpoint outcome retained: %#v", m.discoveryOutcomes)
	}
	if len(m.discoveryErrs) != 1 || !strings.HasPrefix(m.discoveryErrs[0].Error(), "remaining: ") {
		t.Fatalf("discovery errors were not rebuilt from retained outcomes: %v", m.discoveryErrs)
	}
	if !m.discoveryLoading {
		t.Fatal("remaining endpoint discovery did not proceed")
	}
}

func TestMarketplaceInstallReloadHardDiagnosticFailsWithUsableConfig(t *testing.T) {
	m := newTestModel()
	operation, ok := m.beginOperation(operationMutation)
	if !ok {
		t.Fatal("failed to start install operation")
	}
	m.marketplaceInstallOperation = operation
	m.marketplaceInstallReload = m.beginRequest(requestSnapshot)
	failure := errors.New("catalog unavailable")

	updated, _ := m.Update(snapshotLoadedMsg{
		generation:      m.marketplaceInstallReload,
		mergedCfg:       &config.Config{},
		driftAssessment: driftFailed,
		diagnostics:     []tuiDiagnostic{snapshotDiagnostic(diagnosticError, diagnosticCatalog, failure)},
	})
	m = updated.(Model)
	if m.operations.active != nil || m.marketplaceInstallReload != 0 {
		t.Fatalf("failed reload did not unblock operation: active=%#v reload=%d", m.operations.active, m.marketplaceInstallReload)
	}
	if !strings.HasPrefix(m.toast, "Marketplace install reload failed:") || strings.Contains(m.toast, "resource installed") {
		t.Fatalf("hard snapshot diagnostic reported success: %q", m.toast)
	}
}

func TestLowRiskMarketplaceDiscoveryCommitsPreparedPlanWithoutConsent(t *testing.T) {
	repo := t.TempDir()
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("apiVersion: v2\nharnesses: [claude]\n")
	if err := os.WriteFile(configPath, original, 0o644); err != nil {
		t.Fatal(err)
	}
	prepared := &pipeline.PreparedMarketplaceInstall{}
	oldPrepare, oldCommit := prepareDiscoveryMarketplaceInstall, commitPreparedMarketplaceInstall
	t.Cleanup(func() {
		prepareDiscoveryMarketplaceInstall = oldPrepare
		commitPreparedMarketplaceInstall = oldCommit
	})
	prepareDiscoveryMarketplaceInstall = func(context.Context, pipeline.InstallOptions) (*pipeline.PreparedMarketplaceInstall, error) {
		return prepared, nil // low-risk plan: no findings or capabilities requiring CLI consent
	}
	var committed *pipeline.PreparedMarketplaceInstall
	commitPreparedMarketplaceInstall = func(_ context.Context, got *pipeline.PreparedMarketplaceInstall) (*pipeline.InstallResult, error) {
		committed = got
		return &pipeline.InstallResult{Installed: []string{"review"}}, nil
	}
	row := row{MarketplaceDiscovery: &marketplaceDiscovery{
		Endpoint: config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market", Alias: "public"},
		Item:     mpapi.ResourceListItem{RegistryID: "registry", Kind: "skill", Name: "review", Revision: "revision", ContentDigest: "sha256:" + strings.Repeat("a", 64), Availability: mpapi.AvailabilityAvailable},
	}}

	m := newTestModel()
	m.repoRoot = repo
	updated, prepareCmd := m.startDiscoveryInstall(row, scopeProject)
	m = updated.(Model)
	if prepareCmd == nil {
		t.Fatal("install did not start asynchronous preparation")
	}
	// The prepare batch includes the observation pump; drive commands like
	// the tea runtime until the prepare result arrives.
	var prepareMsg tea.Msg
	for _, c := range batchCmds(prepareCmd) {
		if msg, ok := c().(discoveryInstallMsg); ok {
			prepareMsg = msg
		}
	}
	if prepareMsg == nil {
		t.Fatal("prepare batch did not produce an install result")
	}
	updated, commitCmd := m.Update(prepareMsg)
	m = updated.(Model)
	if m.pane == paneMarketplaceConsent {
		t.Fatal("no-findings preparation opened a consent prompt")
	}
	if commitCmd == nil {
		t.Fatal("no-findings preparation did not schedule commit")
	}
	if got, err := os.ReadFile(configPath); err != nil || string(got) != string(original) {
		t.Fatalf("configuration changed before commit: %q err=%v", got, err)
	}
	updated, reloadCmd := m.Update(commitCmd().(discoveryInstallMsg))
	m = updated.(Model)
	if committed != prepared {
		t.Fatalf("commit got plan %p, want exact prepared plan %p", committed, prepared)
	}
	if reloadCmd == nil || m.marketplaceInstallReload == 0 {
		t.Fatal("successful direct commit did not schedule authoritative reload")
	}
}

// spec: tui-interactions/marketplace-install-keeps-locked-sources-at-their-locked-revision
func TestMarketplaceDiscoveryInstallSyncsThroughTheDependencyReconciler(t *testing.T) {
	oldPrepare := prepareDiscoveryMarketplaceInstall
	t.Cleanup(func() { prepareDiscoveryMarketplaceInstall = oldPrepare })
	var prepared []pipeline.InstallOptions
	prepareDiscoveryMarketplaceInstall = func(_ context.Context, options pipeline.InstallOptions) (*pipeline.PreparedMarketplaceInstall, error) {
		prepared = append(prepared, options)
		return &pipeline.PreparedMarketplaceInstall{}, nil
	}
	reconciled := 0
	m := newTestModel()
	m.repoRoot = t.TempDir()
	m.dependencyLifecycle.InstallReconcile = func(context.Context, string, string, bool, []pipeline.StagedConfigEdit) (pipeline.DependencyLockView, error) {
		reconciled++
		return pipeline.DependencyLockView{}, nil
	}
	r := row{MarketplaceDiscovery: &marketplaceDiscovery{
		Endpoint: config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market", Alias: "public"},
		Item:     mpapi.ResourceListItem{RegistryID: "registry", Kind: "skill", Name: "review", Revision: "revision", ContentDigest: "sha256:" + strings.Repeat("a", 64), Availability: mpapi.AvailabilityAvailable},
	}}

	_, cmd := m.startDiscoveryInstall(r, scopeProject)
	for _, c := range batchCmds(cmd) {
		if _, ok := c().(discoveryInstallMsg); ok {
			break
		}
	}
	if len(prepared) != 1 {
		t.Fatalf("prepare ran %d times, want once", len(prepared))
	}
	reconcile := prepared[0].DependencyReconcile
	if reconcile == nil {
		t.Fatal("discovery install prepared without a dependency reconciler, so its sync would bypass exact acquisition locks")
	}
	if _, err := reconcile(context.Background(), m.repoRoot, "", false, nil); err != nil || reconciled != 1 {
		t.Fatalf("install reconciler is not the lifecycle reconciler: err=%v calls=%d", err, reconciled)
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
	updated, _ = m.Update(snapshotLoadedMsg{generation: generation, mergedCfg: &config.Config{}, driftAssessment: driftReady})
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

	updated, _ = m.Update(snapshotLoadedMsg{generation: installGeneration, mergedCfg: &config.Config{}, driftAssessment: driftReady})
	m = updated.(Model)
	if m.operations.active == nil {
		t.Fatal("stale install snapshot finished the operation")
	}
	updated, _ = m.Update(updateDoneMsg{generation: updateGeneration, snapshot: snapshotLoadedMsg{generation: updateGeneration, mergedCfg: &config.Config{}, driftAssessment: driftReady}})
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
	updated, _ = m.Update(snapshotLoadedMsg{generation: installGeneration, mergedCfg: &config.Config{}, driftAssessment: driftReady})
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
	output := m.View().Content
	if strings.Contains(output, "\x1b]52;") || strings.ContainsAny(output, "\a\u202e") || strings.Contains(output, "\nname") {
		t.Fatalf("hostile discovery data reached terminal output: %q", output)
	}
	if !strings.Contains(output, `\x1B`) {
		t.Fatalf("sanitized evidence missing from output: %q", output)
	}
}

func TestMarketplaceConsentPreviewScrollsAtSupportedMinimum(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 50, 12
	operation, ok := m.beginOperation(operationMutation)
	if !ok {
		t.Fatal("failed to start install operation")
	}
	preview := strings.Join([]string{
		"Marketplace install preview:",
		"marketplace: public (https://market.example)",
		"instance_id: market",
		"registry: tools (registry-id)",
		"kind: skill",
		"name: review",
		"revision: sha256:" + strings.Repeat("a", 64),
		"content_digest: sha256:" + strings.Repeat("b", 64),
		"archive_digest: sha256:" + strings.Repeat("c", 64),
		"security: high-risk capability requires review",
		"provenance: registry_url=https://market.example path=skills/review generator=scanner\x1b]52;c;bad\a",
	}, "\n")
	prepared := &pipeline.PreparedMarketplaceInstall{}
	consent := &pipeline.MarketplaceConsentError{Preview: preview, Prepared: prepared, Err: errors.New("risk acceptance required")}
	updated, _ := m.Update(discoveryInstallMsg{operation: operation, prepared: prepared, preview: preview, err: consent})
	m = updated.(Model)

	initial := m.View().Content
	assertTerminalBounds(t, initial, 50, 12)
	if strings.Contains(initial, "security:") || strings.Contains(initial, "provenance:") {
		t.Fatalf("long preview did not start at the top:\n%s", initial)
	}
	for _, required := range []string{"Verified Marketplace install", "Install this exact verified revision?", "↑/↓/page", "y install", "n/esc cancel", "^C quit"} {
		if !strings.Contains(initial, required) {
			t.Fatalf("initial consent omitted %q:\n%s", required, initial)
		}
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	m = updated.(Model)
	if m.marketplaceConsentViewport.YOffset() == 0 {
		t.Fatal("page down did not scroll the consent preview")
	}
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 60, Height: 14})
	m = updated.(Model)
	if m.marketplaceConsentViewport.YOffset() == 0 {
		t.Fatal("resize reset the consent scroll position")
	}
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 50, Height: 12})
	m = updated.(Model)
	if m.marketplaceConsentViewport.YOffset() == 0 {
		t.Fatal("returning to minimum size reset the consent scroll position")
	}
	for i := 0; i < 10 && !strings.Contains(m.View().Content, "provenance:"); i++ {
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
		m = updated.(Model)
	}
	scrolled := m.View().Content
	assertTerminalBounds(t, scrolled, 50, 12)
	for _, required := range []string{"security:", "provenance:", "y install", "n/esc cancel"} {
		if !strings.Contains(scrolled, required) {
			t.Fatalf("scrolled consent omitted %q:\n%s", required, scrolled)
		}
	}
	if strings.Contains(scrolled, "\x1b]52;") || strings.ContainsRune(scrolled, '\a') {
		t.Fatalf("unsanitized preview reached viewport: %q", scrolled)
	}
	offset := m.marketplaceConsentViewport.YOffset()
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = updated.(Model)
	if m.marketplaceConsentViewport.YOffset() >= offset {
		t.Fatal("up did not preserve and adjust consent scroll position")
	}
	updated, cmd := m.Update(keyPress('y'))
	m = updated.(Model)
	if cmd == nil || m.pane != paneList {
		t.Fatalf("confirmation became unavailable after scrolling: pane=%v cmd=%v", m.pane, cmd)
	}
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

// spec: tui-navigation/every-kind-query-discovers-all-marketplace-matches
// spec: tui-navigation/other-kinds-retain-additive-text-matching
func TestMarketplaceKindQueriesIncludeHostedAndTextMatches(t *testing.T) {
	cases := []struct {
		query string
		kind  string
	}{
		{query: ""},
		{query: "skill", kind: "skill"},
		{query: "Command", kind: "command"},
		{query: "subagent", kind: "subagent"},
		{query: "HOOK", kind: "hook"},
		{query: "MCP", kind: "mcpserver"},
		{query: "collection", kind: "collection"},
		{query: "mcpserver", kind: "mcpserver"},
		{query: "agents", kind: "subagent"},
		{query: "all"},
		{query: "plugin"},
		{query: "mcp server"},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			var requestedKinds []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/.well-known/agent-marketplace.json":
					_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("kind-query-market", "https://example.com", "https://example.com"))
				case "/api/v1/search":
					query, kind := r.URL.Query().Get("q"), r.URL.Query().Get("kind")
					requestedKinds = append(requestedKinds, kind)
					offset, limit := 0, marketplace.DiscoveryPageLimit
					_, _ = fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &offset)
					_, _ = fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
					var items []mpapi.ResourceListItem
					total := 0
					if tc.kind != "" && query == "" && kind == tc.kind {
						items = []mpapi.ResourceListItem{tuiDiscoverySearchItem("kind-source", tc.kind, "atlas", "opaque details")}
						total = len(items)
					} else if query != "" && kind == "" {
						term := strings.ToLower(strings.TrimSpace(query))
						items = []mpapi.ResourceListItem{
							tuiDiscoverySearchItem("text-source", "skill", "name-"+term, "ordinary text result"),
							tuiDiscoverySearchItem("metadata-source", "skill", "aaa-metadata", term+" metadata result"),
						}
						total = len(items)
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(marketplace.SearchResponse{Items: items, Total: total, Offset: offset, Limit: limit})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			endpoint := config.MarketplaceEntry{URL: server.URL, InstanceID: "kind-query-market", Alias: "kind-query"}
			cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}
			msg, ok := loadMarketplaceDiscovery(context.Background(), cfg, tc.query, 1)().(discoveryLoadedMsg)
			if !ok || msg.err != nil {
				t.Fatalf("discovery message = %#v", msg)
			}
			if tc.kind != "" {
				if !hasDiscoveryKind(requestedKinds, tc.kind) {
					t.Fatalf("kind filters = %v, want %q", requestedKinds, tc.kind)
				}
			} else {
				for _, requestedKind := range requestedKinds {
					if requestedKind != "" {
						t.Fatalf("non-kind query unexpectedly requested kind %q: %v", requestedKind, requestedKinds)
					}
				}
			}

			local := mkRow("local-"+strings.ToLower(strings.ReplaceAll(tc.query, " ", "-")), withAvailable, withRegistry("local", "local"))
			m := newTestModel()
			m.mergedCfg = cfg
			m.authoritativeRows = []row{local}
			m.rows = []row{local}
			m.filter = tc.query
			m.applyDiscovery(tc.query, msg.outcomes)
			if tc.kind == "" {
				return
			}
			names := artifactNames(m.flatItems)
			nameIndex, metadataIndex, kindIndex := -1, -1, -1
			for i, name := range names {
				switch name {
				case "name-" + strings.ToLower(tc.query):
					nameIndex = i
				case "aaa-metadata":
					metadataIndex = i
				case "atlas":
					kindIndex = i
				}
			}
			if nameIndex < 0 || metadataIndex < 0 || kindIndex < 0 {
				t.Fatalf("filtered names = %v; want text, metadata, and kind-only matches", names)
			}
			if nameIndex >= metadataIndex || nameIndex >= kindIndex {
				t.Fatalf("name match must rank above metadata and kind-only matches: %v", names)
			}
			for _, r := range m.filteredRows {
				if r.Artifact.Name == "atlas" && (r.searchNameMatch || len(r.searchMatchRunes) != 0) {
					t.Fatalf("kind-only result %q highlighted name characters: %+v", r.Artifact.Name, r)
				}
			}
		})
	}
}

// spec: tui-navigation/superseded-kind-search-cannot-replace-current-results
func TestSupersededKindDiscoveryCannotReplaceSearchOrBrowseInventory(t *testing.T) {
	for _, mode := range []string{"changed", "cleared"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			entered := make(chan struct{}, 1)
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/.well-known/agent-marketplace.json":
					_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("held-kind-market", "https://example.com", "https://example.com"))
				case "/api/v1/search":
					if r.URL.Query().Get("kind") == "mcpserver" {
						entered <- struct{}{}
						<-release
						_ = json.NewEncoder(w).Encode(marketplace.SearchResponse{Items: []mpapi.ResourceListItem{tuiDiscoverySearchItem("registry", "mcpserver", "late-opaque", "held result")}, Total: 1, Offset: 0, Limit: marketplace.DiscoveryPageLimit})
						return
					}
					_ = json.NewEncoder(w).Encode(marketplace.SearchResponse{Items: []mpapi.ResourceListItem{}, Total: 0, Offset: 0, Limit: marketplace.DiscoveryPageLimit})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			endpoint := config.MarketplaceEntry{URL: server.URL, InstanceID: "held-kind-market", Alias: "held"}
			cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}
			browseItem := tuiDiscoverySearchItem("registry", "mcpserver", "browse-opaque", "browse inventory")
			local := mkRow("local-mcp", withAvailable, withRegistry("local", "local"))
			m := newTestModel()
			m.mergedCfg = cfg
			m.authoritativeRows = []row{local}
			m.rows = []row{local}
			m.applyDiscovery("", []marketplace.DiscoveryOutcome{{Endpoint: endpoint, Items: []mpapi.ResourceListItem{browseItem}, Total: 1}})
			m.currentFoldState()["status:available"] = true
			m.filter = "mcp"
			m.recomputeView()
			oldGeneration := m.beginRequest(requestDiscovery)
			m.discoveryLoading = true
			messages := make(chan discoveryLoadedMsg, 1)
			go func() {
				messages <- loadMarketplaceDiscovery(context.Background(), cfg, "mcp", oldGeneration)().(discoveryLoadedMsg)
			}()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("Marketplace kind page was not held")
			}
			if got := artifactNames(m.flatItems); !namesEqual(got, []string{"local-mcp", "browse-opaque"}) {
				t.Fatalf("local filtering blocked or changed while kind page was held: %v", got)
			}
			browseNames := []string{m.browseDiscoveryRows[0].Artifact.Name}

			var expectedFilter string
			if mode == "changed" {
				expectedFilter = "command"
				m.filter = expectedFilter
				m.recomputeView()
				m.beginMarketplaceDiscovery(expectedFilter, true)
			} else {
				m.filter = "no-such-resource"
				m.recomputeView()
				if len(artifactNames(m.flatItems)) != 0 {
					t.Fatalf("no-match query left visible resources: %v", artifactNames(m.flatItems))
				}
				updated, _ := m.clearSearch()
				m = updated.(Model)
				if !m.currentFoldState()["status:available"] {
					t.Fatal("clearing search did not restore the saved browse fold")
				}
			}
			selected := m.selectionKey()
			close(release)
			msg := <-messages
			updated, _ := m.Update(msg)
			m = updated.(Model)
			if m.filter != expectedFilter || m.selectionKey() != selected {
				t.Fatalf("late completion changed query/selection: filter=%q selection=%q, want %q/%q", m.filter, m.selectionKey(), expectedFilter, selected)
			}
			if len(m.browseDiscoveryRows) != 1 || m.browseDiscoveryRows[0].Artifact.Name != browseNames[0] {
				t.Fatalf("late completion changed browse inventory: %#v", m.browseDiscoveryRows)
			}
			if len(m.discoveryRows) != 1 || m.discoveryRows[0].Artifact.Name != browseNames[0] {
				t.Fatalf("late completion replaced current discovery: %#v", m.discoveryRows)
			}
			if m.cursor < 0 || (len(m.flatItems) > 0 && m.cursor >= len(m.flatItems)) {
				t.Fatalf("search left unsafe cursor %d for %d items", m.cursor, len(m.flatItems))
			}
			if mode == "cleared" && m.filter != "" {
				t.Fatalf("clear restored filter %q, want empty", m.filter)
			}
		})
	}
}

// spec: tui-navigation/failed-kind-discovery-preserves-usable-local-results
func TestFailedAndDeniedKindDiscoveryPreservesUsableRows(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("failing-market", "https://example.com", "https://example.com"))
			return
		}
		http.Error(w, "endpoint unavailable", http.StatusServiceUnavailable)
	}))
	defer failed.Close()
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("healthy-market", "https://example.com", "https://example.com"))
		case "/api/v1/search":
			items := []mpapi.ResourceListItem{}
			if r.URL.Query().Get("kind") == "mcpserver" {
				items = append(items, tuiDiscoverySearchItem("registry", "mcpserver", "healthy-opaque", "healthy kind result"))
			}
			_ = json.NewEncoder(w).Encode(marketplace.SearchResponse{Items: items, Total: len(items), Offset: 0, Limit: marketplace.DiscoveryPageLimit})
		default:
			http.NotFound(w, r)
		}
	}))
	defer healthy.Close()
	endpoints := []config.MarketplaceEntry{
		{URL: failed.URL, InstanceID: "failing-market", Alias: "failing"},
		{URL: healthy.URL, InstanceID: "healthy-market", Alias: "healthy"},
	}
	cfg := &config.Config{Marketplaces: endpoints}
	local := mkRow("local-mcp", withAvailable, withRegistry("local", "local"))
	m := newTestModel()
	m.mergedCfg = cfg
	m.authoritativeRows = []row{local}
	m.rows = []row{local}
	m.filter = "mcp"
	generation := m.beginRequest(requestDiscovery)
	m.discoveryLoading = true
	raw := loadMarketplaceDiscovery(context.Background(), cfg, "mcp", generation)()
	for {
		msg := raw.(discoveryLoadedMsg)
		updated, next := m.Update(msg)
		m = updated.(Model)
		if next == nil {
			break
		}
		raw = next()
	}
	if len(m.discoveryErrs) != 1 || !strings.Contains(m.discoveryErrs[0].Error(), "failing") || !strings.Contains(m.View().Content, "Marketplace: 1 failed") {
		t.Fatalf("failed endpoint diagnostic is not visible: errors=%v view=%s", m.discoveryErrs, m.View().Content)
	}
	if len(m.discoveryRows) != 1 || m.discoveryRows[0].Artifact.Name != "healthy-opaque" {
		t.Fatalf("healthy kind result was lost: %#v", m.discoveryRows)
	}
	if got := artifactNames(m.flatItems); !namesEqual(got, []string{"local-mcp", "healthy-opaque"}) {
		t.Fatalf("failed endpoint removed usable local or healthy rows: %v", got)
	}

	var contacted atomic.Int32
	deniedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contacted.Add(1)
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("denied-market", "https://example.com", "https://example.com"))
			return
		}
		http.NotFound(w, r)
	}))
	defer deniedServer.Close()
	denied := []string{}
	deniedCfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: deniedServer.URL, InstanceID: "denied-market", Alias: "denied"}}, AllowedMarketplaces: &denied}
	deniedModel := newTestModel()
	deniedModel.mergedCfg = deniedCfg
	deniedModel.authoritativeRows = []row{local}
	deniedModel.rows = []row{local}
	deniedModel.filter = "mcp"
	if cmd := deniedModel.beginMarketplaceDiscovery("mcp", false); cmd != nil {
		t.Fatal("denied endpoint scheduled discovery")
	}
	if contacted.Load() != 0 {
		t.Fatalf("denied endpoint received %d requests", contacted.Load())
	}
	if len(deniedModel.discoveryErrs) != 1 || !strings.Contains(deniedModel.View().Content, "Marketplace: 1 failed") {
		t.Fatalf("denied configuration diagnostic is not visible: errors=%v view=%s", deniedModel.discoveryErrs, deniedModel.View().Content)
	}
	if got := artifactNames(deniedModel.flatItems); !namesEqual(got, []string{"local-mcp"}) {
		t.Fatalf("denied discovery removed local results: %v", got)
	}
}

func tuiDiscoverySearchItem(registryID, kind, name, description string) mpapi.ResourceListItem {
	digest := "sha256:" + strings.Repeat("a", 64)
	return mpapi.ResourceListItem{RegistryID: registryID, RegistrySlug: registryID, Kind: kind, Name: name, Revision: digest, ContentDigest: digest, Description: description, Availability: mpapi.AvailabilityAvailable}
}

func hasDiscoveryKind(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
