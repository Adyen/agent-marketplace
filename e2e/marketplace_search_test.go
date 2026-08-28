package e2e_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

type marketplaceSearchFixture struct {
	server   *httptest.Server
	requests atomic.Int32
	kinds    chan string
}

func newMarketplaceSearchFixture(t *testing.T, instanceID string, status int) *marketplaceSearchFixture {
	t.Helper()
	fixture := &marketplaceSearchFixture{kinds: make(chan string, 16)}
	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown(instanceID, "https://example.invalid"))
		case "/api/v1/search":
			fixture.requests.Add(1)
			fixture.kinds <- r.URL.Query().Get("kind")
			if status != http.StatusOK {
				http.Error(w, `{"error":"unavailable"}`, status)
				return
			}
			items := []mpapi.ResourceListItem{
				{SourceID: "src", SourceSlug: "source", Kind: "skill", Name: "skill-one", Revision: marketplaceTestDigest('a'), ContentDigest: marketplaceTestDigest('a')},
				{SourceID: "src", SourceSlug: "source", Kind: "command", Name: "command-one", Revision: marketplaceTestDigest('b'), ContentDigest: marketplaceTestDigest('b')},
				{SourceID: "src", SourceSlug: "source", Kind: "subagent", Name: "agent-one", Revision: marketplaceTestDigest('c'), ContentDigest: marketplaceTestDigest('c')},
				{SourceID: "src", SourceSlug: "source", Kind: "hook", Name: "hook-one", Revision: marketplaceTestDigest('d'), ContentDigest: marketplaceTestDigest('d')},
				{SourceID: "src", SourceSlug: "source", Kind: "mcpserver", Name: "server-one", Revision: marketplaceTestDigest('e'), ContentDigest: marketplaceTestDigest('e')},
				{SourceID: "src", SourceSlug: "source", Kind: "collection", Name: "bundle-one", Revision: marketplaceTestDigest('f'), ContentDigest: marketplaceTestDigest('f')},
			}
			if kind := r.URL.Query().Get("kind"); kind != "" {
				filtered := items[:0]
				for _, item := range items {
					if item.Kind == kind {
						filtered = append(filtered, item)
					}
				}
				items = filtered
			}
			_ = json.NewEncoder(w).Encode(struct {
				Items  []mpapi.ResourceListItem `json:"items"`
				Total  int                      `json:"total"`
				Offset int                      `json:"offset"`
				Limit  int                      `json:"limit"`
			}{Items: items, Total: len(items), Limit: 100})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fixture.server.Close)
	return fixture
}

func marketplaceTestDigest(char byte) string {
	return "sha256:" + strings.Repeat(string(char), 64)
}

func TestMarketplaceSearchAllKindsAndDeduplicatesLayeredEndpoint(t *testing.T) {
	fixture := newMarketplaceSearchFixture(t, "marketplace-id", http.StatusOK)
	env := newEnv(t)
	env.WriteGlobalConfig(fmt.Sprintf("marketplaces:\n  - url: %s\n    instance_id: marketplace-id\n    alias: public", fixture.server.URL))
	env.WriteRepoConfig(fmt.Sprintf("marketplaces:\n  - url: %s/\n    instance_id: marketplace-id", fixture.server.URL))

	result := env.RunAM("search", "--output", "json")
	if result.ExitCode != 0 {
		t.Fatalf("search all kinds failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	for _, name := range []string{"skill-one", "command-one", "agent-one", "hook-one", "server-one", "bundle-one"} {
		if strings.Count(result.Stdout, `"name": "`+name+`"`) != 1 {
			t.Fatalf("search output did not contain exactly one %q:\n%s", name, result.Stdout)
		}
	}
	if got := fixture.requests.Load(); got != 1 {
		t.Fatalf("search requests = %d, want 1 for a repeated Marketplace identity", got)
	}
	if kind := <-fixture.kinds; kind != "" {
		t.Fatalf("unfiltered search sent kind %q", kind)
	}

	result = env.RunAM("search", "--marketplace", "public", "--kind", "mcp", "--output", "json")
	if result.ExitCode != 0 {
		t.Fatalf("MCP search failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	if !strings.Contains(result.Stdout, `"name": "server-one"`) || strings.Contains(result.Stdout, `"name": "skill-one"`) {
		t.Fatalf("MCP search returned the wrong resources:\n%s", result.Stdout)
	}
	if kind := <-fixture.kinds; kind != "mcpserver" {
		t.Fatalf("MCP search sent kind %q, want mcpserver", kind)
	}
}

func TestMarketplaceSearchPartialAndTotalFailure(t *testing.T) {
	good := newMarketplaceSearchFixture(t, "good-id", http.StatusOK)
	bad := newMarketplaceSearchFixture(t, "bad-id", http.StatusServiceUnavailable)
	env := newEnv(t)
	env.WriteGlobalConfig(fmt.Sprintf("marketplaces:\n  - url: %s\n    instance_id: good-id\n    alias: good\n  - url: %s\n    instance_id: bad-id\n    alias: bad", good.server.URL, bad.server.URL))

	result := env.RunAM("search", "--output", "json", "skill")
	if result.ExitCode != 0 || !strings.Contains(result.Stdout, `"name": "skill-one"`) || !strings.Contains(result.Stdout, `"errors"`) || !strings.Contains(result.Stderr, "warning: bad:") {
		t.Fatalf("partial search result = exit %d\nstdout:\n%s\nstderr:\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}

	result = env.RunAM("search", "--marketplace", "bad", "skill")
	if result.ExitCode == 0 || !strings.Contains(result.Combined, "marketplace error 503") {
		t.Fatalf("explicit failing Marketplace result = exit %d:\n%s", result.ExitCode, result.Combined)
	}

	allBad := newEnv(t)
	allBad.WriteGlobalConfig(fmt.Sprintf("marketplaces:\n  - url: %s\n    instance_id: bad-id\n    alias: bad", bad.server.URL))
	result = allBad.RunAM("search", "--output", "json", "skill")
	if result.ExitCode == 0 || !strings.Contains(result.Stdout, `"errors"`) || !strings.Contains(result.Combined, "marketplace error 503") {
		t.Fatalf("all-failing federated search result = exit %d:\n%s", result.ExitCode, result.Combined)
	}
}
