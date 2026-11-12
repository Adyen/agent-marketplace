package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

func tuiDiscoveryRegistration(t *testing.T, url string) (*config.Config, string, []byte) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")
	original := []byte(fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  public: {marketplace: %q}\n", url))
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	intent := config.ReadIntent(context.Background(), config.LoadEffectiveOptions{RepoRoot: root, RepoPath: path, GlobalPath: filepath.Join(root, "absent-user.yaml"), OrganizationPath: filepath.Join(root, "absent-org.yaml")})
	ec := config.Bind(intent, config.Evidence{})
	if len(ec.Errors) != 0 || ec.Merged == nil || len(ec.Merged.Marketplaces) != 1 || ec.Merged.Marketplaces[0].InstanceID != "" || len(ec.Merged.Marketplaces[0].Resources) != 0 {
		t.Fatalf("v4 resource-free registration = %#v, errors = %v", ec.Merged, ec.Errors)
	}
	return ec.Merged, path, original
}

func registrationDiscoveryOutcome(endpoint config.MarketplaceEntry, name string) marketplace.DiscoveryOutcome {
	return marketplace.DiscoveryOutcome{Endpoint: endpoint, Items: []mpapi.ResourceListItem{{RegistryID: "registry-id", RegistrySlug: "tools", Kind: "skill", Name: name, Description: "Review code", Revision: "sha256:" + strings.Repeat("a", 64), ContentDigest: "sha256:" + strings.Repeat("a", 64), Availability: mpapi.AvailabilityAvailable}}, Total: 1}
}

func TestDiscoveryRegistrationTUILiveAndRefreshRetainRealIdentity(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var metadata, searches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/catalog/.well-known/agent-marketplace.json":
			metadata.Add(1)
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("real-tui-instance", "https://public.example/catalog"))
		case "/catalog/api/v1/search":
			searches.Add(1)
			if r.URL.Query().Get("limit") != "50" {
				t.Errorf("limit = %q, want 50", r.URL.Query().Get("limit"))
			}
			out := registrationDiscoveryOutcome(config.MarketplaceEntry{}, "review")
			out.Items[0].Description = "Review \x1b]52;c;secret\a code"
			_ = json.NewEncoder(w).Encode(marketplace.SearchResponse{Items: out.Items, Total: 1, Limit: 50})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	canonical := server.URL + "/catalog"
	cfg, path, original := tuiDiscoveryRegistration(t, "HTTP"+strings.TrimPrefix(canonical, "http")+"/")
	m := newTestModel()
	m.mergedCfg = cfg
	cmd := m.beginMarketplaceDiscovery("", false)
	if cmd == nil {
		t.Fatal("source-only discovery was not scheduled")
	}
	for cmd != nil {
		msg, ok := cmd().(discoveryLoadedMsg)
		if !ok {
			t.Fatal("discovery did not produce its asynchronous result")
		}
		updated, next := m.Update(msg)
		m, cmd = updated.(Model), next
	}
	if metadata.Load() != 1 || searches.Load() != 1 || len(m.discoveryErrs) != 0 || len(m.discoveryRows) != 1 || len(m.browseDiscoveryRows) != 1 || len(m.discoveryOutcomes) != 1 {
		t.Fatalf("live TUI discovery = rows %#v, errors %v, requests %d/%d", m.discoveryRows, m.discoveryErrs, metadata.Load(), searches.Load())
	}
	r := m.discoveryRows[0]
	if r.MarketplaceDiscovery.Endpoint.InstanceID != "real-tui-instance" || r.MarketplaceDiscovery.Endpoint.URL != canonical || r.MarketplaceDiscovery.Endpoint.Alias != "public" || r.Artifact.MarketplaceInstanceID != "real-tui-instance" || r.RegistryName != "public/tools" {
		t.Fatalf("TUI lost acquired identity or attached metadata to the wrong endpoint: %#v", r)
	}
	for i, item := range m.flatItems {
		if item.Kind == flatArtifact && item.Row.Artifact.Name == "review" {
			m.cursor = i
			break
		}
	}
	detail := renderDetail(m)
	if !strings.Contains(detail, "Review") || !strings.Contains(detail, `\x1B`) || strings.Contains(detail, "\x1b]52;") || strings.Contains(detail, "\a") {
		t.Fatalf("served description missing or terminal metadata unsanitized: %q", detail)
	}
	options, err := discoveryInstallOptions("/repo", r, scopeProject)
	if err != nil || options.Marketplace != "public" || options.ExactMarketplaceLocator.URL != canonical || options.ExactMarketplaceLocator.Digest != "sha256:"+strings.Repeat("a", 64) {
		t.Fatalf("real identity did not reach exact install options: %#v, %v", options, err)
	}
	// Beginning another request must retain this same-URL verified page even
	// though its authored declaration still deliberately has no instance ID.
	m.beginMarketplaceDiscovery("review", true)
	if len(m.discoveryRows) != 1 || len(m.browseDiscoveryRows) != 1 || len(m.discoveryOutcomes) != 1 || m.discoveryRows[0].Artifact.MarketplaceInstanceID != "real-tui-instance" {
		t.Fatalf("source-only refresh discarded verified discovery: rows %#v, outcomes %#v", m.discoveryRows, m.discoveryOutcomes)
	}
	if cfg.Marketplaces[0].InstanceID != "" || len(cfg.Marketplaces[0].Resources) != 0 {
		t.Fatalf("TUI discovery mutated authored registration: %#v", cfg.Marketplaces)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(original) {
		t.Fatalf("TUI discovery wrote config identity: %q, %v", got, err)
	}
}

func TestDiscoveryRegistrationTUIEndpointMatchIsNarrow(t *testing.T) {
	for _, tc := range []struct {
		name       string
		configured config.MarketplaceEntry
		returned   config.MarketplaceEntry
		keep       bool
	}{
		{name: "unacquired canonical endpoint", configured: config.MarketplaceEntry{URL: "HTTPS://market.example/", Alias: "public"}, returned: config.MarketplaceEntry{URL: "https://market.example", Alias: "public", InstanceID: "real-instance"}, keep: true},
		{name: "different URL with same alias", configured: config.MarketplaceEntry{URL: "https://new.example", Alias: "public"}, returned: config.MarketplaceEntry{URL: "https://old.example", Alias: "public", InstanceID: "real-instance"}},
		{name: "same URL different explicit identity", configured: config.MarketplaceEntry{URL: "https://market.example", InstanceID: "pinned-instance"}, returned: config.MarketplaceEntry{URL: "https://market.example", InstanceID: "real-instance"}},
		{name: "matching exact identity", configured: config.MarketplaceEntry{URL: "https://market.example/", InstanceID: "real-instance"}, returned: config.MarketplaceEntry{URL: "https://market.example", InstanceID: "real-instance"}, keep: true},
		{name: "missing identity with pins", configured: config.MarketplaceEntry{URL: "https://market.example", Resources: []config.MarketplaceResourcePin{{RegistryID: "registry-id", Kind: "skill", Name: "review"}}}, returned: config.MarketplaceEntry{URL: "https://market.example", InstanceID: "real-instance"}},
		{name: "whitespace declaration is not unacquired", configured: config.MarketplaceEntry{URL: "https://market.example", InstanceID: " "}, returned: config.MarketplaceEntry{URL: "https://market.example", InstanceID: "real-instance"}},
		{name: "whitespace result is not real identity", configured: config.MarketplaceEntry{URL: "https://market.example"}, returned: config.MarketplaceEntry{URL: "https://market.example", InstanceID: " "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel()
			m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{tc.configured}}
			out := registrationDiscoveryOutcome(tc.returned, "review")
			// Seed a previous verified page, then exercise the real refresh filter.
			m.discoveryOutcomes = []marketplace.DiscoveryOutcome{out}
			m.discoveryRows = []row{{MarketplaceDiscovery: &marketplaceDiscovery{Endpoint: tc.returned, Item: out.Items[0]}}}
			m.browseDiscoveryRows = append([]row(nil), m.discoveryRows...)
			m.beginMarketplaceDiscovery("review", true)
			want := 0
			if tc.keep {
				want = 1
			}
			if len(m.discoveryRows) != want || len(m.browseDiscoveryRows) != want || len(m.discoveryOutcomes) != want {
				t.Fatalf("endpoint match retained wrong metadata: rows %#v, browse %#v, outcomes %#v", m.discoveryRows, m.browseDiscoveryRows, m.discoveryOutcomes)
			}
		})
	}
}

func TestDiscoveryRegistrationTUIRejectsLateGenerationsAndEndpointChanges(t *testing.T) {
	for _, change := range []string{"different URL", "exact identity acquired"} {
		t.Run(change, func(t *testing.T) {
			m := newTestModel()
			declaration := config.MarketplaceEntry{URL: "https://old.example", Alias: "public"}
			m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{declaration}}
			m.beginMarketplaceDiscovery("", true)
			oldGeneration := m.requests.Active(requestDiscovery)
			returned := declaration
			returned.InstanceID = "old-instance"
			late := registrationDiscoveryOutcome(returned, "late")
			current := declaration
			if change == "different URL" {
				current.URL = "https://new.example"
			} else {
				current.InstanceID = "new-instance"
			}
			m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{current}}
			m.beginMarketplaceDiscovery("", true)
			generation := m.requests.Active(requestDiscovery)
			for _, msg := range []discoveryLoadedMsg{
				{generation: oldGeneration, sequence: 1, outcomes: []marketplace.DiscoveryOutcome{late}},
				{generation: oldGeneration, outcomes: []marketplace.DiscoveryOutcome{late}, done: true},
			} {
				updated, _ := m.Update(msg)
				m = updated.(Model)
				if len(m.discoveryRows) != 0 || len(m.discoveryOutcomes) != 0 || m.requests.Active(requestDiscovery) != generation {
					t.Fatalf("late source-only generation attached metadata: %#v", m.discoveryOutcomes)
				}
			}
			// Endpoint matching is independent of the generation guard: a current
			// completion cannot attach a previous URL or a replaced exact identity.
			verified := current
			verified.InstanceID = "new-instance"
			control := registrationDiscoveryOutcome(verified, "current")
			updated, _ := m.Update(discoveryLoadedMsg{generation: generation, sequence: 2, outcomes: []marketplace.DiscoveryOutcome{late, control}})
			m = updated.(Model)
			if len(m.discoveryRows) != 1 || m.discoveryRows[0].Artifact.Name != "current" || m.discoveryRows[0].Artifact.MarketplaceURL != current.URL || m.discoveryRows[0].Artifact.MarketplaceInstanceID != "new-instance" || len(m.discoveryOutcomes) != 1 || len(m.browseDiscoveryRows) != 1 {
				t.Fatalf("current completion crossed endpoint/identity boundaries: rows %#v, outcomes %#v", m.discoveryRows, m.discoveryOutcomes)
			}
			updated, _ = m.Update(discoveryLoadedMsg{generation: generation, sequence: 1, outcomes: []marketplace.DiscoveryOutcome{late}})
			m = updated.(Model)
			if len(m.discoveryRows) != 1 || m.discoveryRows[0].Artifact.Name != "current" || m.discoverySequence != 2 {
				t.Fatalf("older partial completion replaced verified metadata: %#v", m.discoveryRows)
			}
			updated, _ = m.Update(discoveryLoadedMsg{generation: generation, outcomes: []marketplace.DiscoveryOutcome{late, control}, done: true})
			m = updated.(Model)
			if len(m.discoveryRows) != 1 || m.discoveryRows[0].Artifact.Name != "current" {
				t.Fatalf("final completion crossed endpoint/identity boundaries: %#v", m.discoveryRows)
			}
			updated, _ = m.Update(discoveryLoadedMsg{generation: generation, outcomes: []marketplace.DiscoveryOutcome{late}, done: true})
			m = updated.(Model)
			if len(m.discoveryRows) != 1 || m.discoveryRows[0].Artifact.Name != "current" {
				t.Fatalf("duplicate completion replaced verified metadata: %#v", m.discoveryRows)
			}
		})
	}
}
