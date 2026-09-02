package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
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

func TestBlockedMarketplaceDiscoveryRowCannotOpenInstallScope(t *testing.T) {
	m := newTestModel()
	m.loading = false
	m.applyDiscovery("", []marketplace.DiscoveryOutcome{{Endpoint: config.MarketplaceEntry{InstanceID: "market", Alias: "public"}, Items: []mpapi.ResourceListItem{{RegistryID: "registry", RegistrySlug: "tools", Kind: "skill", Name: "blocked", Availability: mpapi.AvailabilityBlocked}}}})
	for i, item := range m.flatItems {
		if item.Kind == flatArtifact {
			m.cursor = i
			break
		}
	}
	updated, cmd := m.Update(keyPress(' '))
	m = updated.(Model)
	if m.pane == paneToggleModal {
		t.Fatal("blocked discovery row opened install scope modal")
	}
	if cmd == nil {
		t.Fatal("blocked discovery row did not explain refusal")
	}
}
