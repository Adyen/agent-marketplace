package e2e_test

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
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
				{RegistryID: "src", RegistrySlug: "source", Kind: "skill", Name: "skill-one", Revision: marketplaceTestDigest('a'), ContentDigest: marketplaceTestDigest('a')},
				{RegistryID: "src", RegistrySlug: "source", Kind: "command", Name: "command-one", Revision: marketplaceTestDigest('b'), ContentDigest: marketplaceTestDigest('b')},
				{RegistryID: "src", RegistrySlug: "source", Kind: "subagent", Name: "agent-one", Revision: marketplaceTestDigest('c'), ContentDigest: marketplaceTestDigest('c')},
				{RegistryID: "src", RegistrySlug: "source", Kind: "hook", Name: "hook-one", Revision: marketplaceTestDigest('d'), ContentDigest: marketplaceTestDigest('d')},
				{RegistryID: "src", RegistrySlug: "source", Kind: "mcpserver", Name: "server-one", Revision: marketplaceTestDigest('e'), ContentDigest: marketplaceTestDigest('e')},
				{RegistryID: "src", RegistrySlug: "source", Kind: "collection", Name: "bundle-one", Revision: marketplaceTestDigest('f'), ContentDigest: marketplaceTestDigest('f')},
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
			slices.SortFunc(items, func(a, b mpapi.ResourceListItem) int {
				if order := cmp.Compare(a.Kind, b.Kind); order != 0 {
					return order
				}
				return cmp.Compare(a.Name, b.Name)
			})
			total := len(items)
			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			if limit <= 0 {
				limit = 100
			}
			start := min(max(offset, 0), len(items))
			items = items[start:min(start+limit, len(items))]
			_ = json.NewEncoder(w).Encode(struct {
				Items  []mpapi.ResourceListItem `json:"items"`
				Total  int                      `json:"total"`
				Offset int                      `json:"offset"`
				Limit  int                      `json:"limit"`
			}{Items: items, Total: total, Offset: offset, Limit: limit})
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

func TestMarketplaceSearchLimitOffsetAndInvalidBounds(t *testing.T) {
	fixture := newMarketplaceSearchFixture(t, "marketplace-id", http.StatusOK)
	env := newEnv(t)
	env.WriteGlobalConfig(fmt.Sprintf("marketplaces:\n  - url: %s\n    instance_id: marketplace-id\n    alias: public", fixture.server.URL))
	configBefore := env.ReadFile("/root/.agents/config.yaml")
	readNames := func(args ...string) []string {
		t.Helper()
		result := env.RunAM(args...)
		if result.ExitCode != 0 {
			t.Fatalf("search %v failed (exit %d): %s", args, result.ExitCode, result.Combined)
		}
		var payload struct {
			Results []struct {
				Name string `json:"name"`
			} `json:"results"`
		}
		if err := json.Unmarshal([]byte(result.Stdout), &payload); err != nil {
			t.Fatalf("decode search %v: %v\n%s", args, err, result.Stdout)
		}
		names := make([]string, len(payload.Results))
		for i, resource := range payload.Results {
			names[i] = resource.Name
		}
		return names
	}
	all := readNames("search", "--marketplace", "public", "--output", "json")
	wantAll := []string{"bundle-one", "command-one", "hook-one", "server-one", "skill-one", "agent-one"}
	if !slices.Equal(all, wantAll) {
		t.Fatalf("unfiltered search = %v, want %v", all, wantAll)
	}
	page := readNames("search", "--marketplace", "public", "--output", "json", "--limit", "2", "--offset", "1")
	wantPage := []string{"command-one", "hook-one"}
	if !slices.Equal(page, wantPage) {
		t.Fatalf("search pagination = %v, want %v", page, wantPage)
	}
	beforeInvalid := fixture.requests.Load()
	for _, args := range [][]string{{"search", "--limit", "-1"}, {"search", "--offset", "-1"}} {
		result := env.RunAM(args...)
		if result.ExitCode == 0 {
			t.Fatalf("invalid bounds %v succeeded: %s", args, result.Combined)
		}
	}
	if got := fixture.requests.Load(); got != beforeInvalid {
		t.Fatalf("invalid search bounds made %d Marketplace requests, want %d", got, beforeInvalid)
	}
	if got := env.ReadFile("/root/.agents/config.yaml"); got != configBefore {
		t.Fatalf("search rewrote user config: %q", got)
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

// Users search through the Marketplace the deployed organization config
// declares under an alias: v4, a collection selected, no organization lock.
// Their user config is the v4 one init creates.
func TestMarketplaceSearchThroughDeployedOrganizationSource(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("Behavior body", true))
	consumer := newEnv(t)
	consumer.InitGitRepo()
	consumer.MustRunAM("init", "--harnesses", "factory")
	consumer.WriteOrgConfigWithMode(fmt.Sprintf(`apiVersion: agent-marketplace/v4
sources:
  org:
    marketplace: %s
allowed_marketplaces:
  - '^http://127\.0\.0\.1:[0-9]+$'
resources:
  org:
    include:
      - behavior-source/collection/behavior-bundle
`, fixture.server.URL), 0o644)

	result := consumer.RunAM("search", "skill-one", "--output", "json")
	if result.ExitCode != 0 || !strings.Contains(result.Stdout, `"name": "skill-one"`) {
		t.Fatalf("search through the organization source failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	result = consumer.RunAM("search", "--marketplace", "org", "--kind", "command", "--output", "json")
	if result.ExitCode != 0 || !strings.Contains(result.Stdout, `"name": "command-one"`) || strings.Contains(result.Stdout, `"name": "skill-one"`) {
		t.Fatalf("search --marketplace org --kind command returned the wrong resources (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	consumer.AssertFileAbsent("/etc/agent-marketplace/lock.yaml")
}
