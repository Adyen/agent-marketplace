package marketplace

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

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

const registrationInstanceID = "real-deployed-marketplace"

type registrationIdentityFixture struct {
	server       *httptest.Server
	pin          config.MarketplaceResourcePin
	requests     atomic.Int32
	wellKnown    atomic.Int32
	searches     atomic.Int32
	acquisitions atomic.Int32
}

func newRegistrationIdentityFixture(t *testing.T, wellKnown func(http.ResponseWriter)) *registrationIdentityFixture {
	t.Helper()
	artifact, err := hccartifact.FromIR(ir.NewSkill("review", "Review code", "Review carefully.", nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatal(err)
	}
	artifact.Manifest.RegistryID = "registry-id"
	artifact.Manifest.RegistrySlug = "source"
	artifact.Manifest.Revision = "refs/heads/main"
	raw, archiveDigest, err := hccartifact.Encode(artifact)
	if err != nil {
		t.Fatal(err)
	}
	digest := artifact.Manifest.ContentDigest
	revision := assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: registrationInstanceID, Registry: "registry-id", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(digest), ArchiveDigest: store.ArchiveDigest(archiveDigest),
	})
	fixture := &registrationIdentityFixture{pin: config.MarketplaceResourcePin{
		RegistryID: "registry-id", RegistrySlug: "source", Kind: "skill", Name: "review", Revision: digest, Digest: digest,
	}}
	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.requests.Add(1)
		switch {
		case r.URL.Path == "/.well-known/agent-marketplace.json":
			fixture.wellKnown.Add(1)
			if wellKnown != nil {
				wellKnown(w)
			} else {
				writeTestWellKnown(w, registrationInstanceID)
			}
		case r.URL.Path == "/api/v1/search":
			fixture.searches.Add(1)
			_ = json.NewEncoder(w).Encode(SearchResponse{Items: []mpapi.ResourceListItem{{
				RegistryID: "registry-id", RegistrySlug: "source", Kind: "skill", Name: "review", Revision: digest, ContentDigest: digest,
			}}, Total: 1, Limit: 100})
		case strings.HasPrefix(r.URL.Path, "/api/v1/registries/"):
			fixture.acquisitions.Add(1)
			writeTestRegistry(w, store.Registry{ID: "registry-id", Slug: "source", Origins: []string{"https://git.example/repo.git"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true})
		case strings.HasPrefix(r.URL.Path, "/api/v1/resources/"):
			fixture.acquisitions.Add(1)
			_ = json.NewEncoder(w).Encode(revision)
		case strings.HasPrefix(r.URL.Path, "/api/v1/artifacts/"):
			fixture.acquisitions.Add(1)
			_, _ = w.Write(raw)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fixture.server.Close)
	return fixture
}

func sourceOnlyRegistration(t *testing.T, url string) config.EffectiveConfig {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(path, []byte(fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  public: {marketplace: %q}\n", url)), 0o600); err != nil {
		t.Fatal(err)
	}
	intent := config.ReadIntent(context.Background(), config.LoadEffectiveOptions{
		RepoRoot: root, RepoPath: path, GlobalPath: filepath.Join(root, "absent-user.yaml"), OrganizationPath: filepath.Join(root, "absent-org.yaml"),
	})
	ec := config.Bind(intent, config.Evidence{})
	if len(ec.Errors) != 0 || ec.Merged == nil || len(ec.Merged.Marketplaces) != 1 {
		t.Fatalf("source-only v4 configuration = %#v, errors = %v", ec.Merged, ec.Errors)
	}
	entry := ec.Merged.Marketplaces[0]
	if entry.InstanceID != "" || len(entry.Resources) != 0 || entry.URL != url || entry.Alias != "public" {
		t.Fatalf("authored registration fabricated acquisition evidence: %#v", entry)
	}
	return ec
}

// spec: configuration-intent/reading-authored-selections-without-acquisition
func TestRegistrationIdentitySourceOnlyConsumerPaths(t *testing.T) {
	fixture := newRegistrationIdentityFixture(t, nil)
	ec := sourceOnlyRegistration(t, fixture.server.URL)
	if got := fixture.requests.Load(); got != 0 {
		t.Fatalf("config read made %d acquisition requests", got)
	}
	r := &Resolver{Cache: NewCache(t.TempDir())}
	ctx := context.Background()
	t.Run("identity", func(t *testing.T) {
		client, err := r.clientForEntry(ec.Merged.Marketplaces[0])
		if err != nil {
			t.Fatal(err)
		}
		entry, err := acquireMarketplaceIdentity(ctx, client, ec.Merged.Marketplaces[0])
		if err != nil || entry.InstanceID != registrationInstanceID || entry.URL != fixture.server.URL || len(entry.Resources) != 0 {
			t.Fatalf("live registration identity = %#v, error = %v", entry, err)
		}
	})
	t.Run("resolve", func(t *testing.T) {
		before := fixture.wellKnown.Load()
		for _, mode := range []resolver.MarketplaceResolveMode{resolver.MarketplaceResolveModeCached, resolver.MarketplaceResolveModeRefresh} {
			if resolved, errs := r.Resolve(ctx, ec.Merged, nil, mode); len(resolved) != 0 || len(errs) != 0 {
				t.Fatalf("source-only Resolve(%v): artifacts = %v, errors = %v", mode, resolved, errs)
			}
		}
		if got := fixture.wellKnown.Load() - before; got != 2 {
			t.Fatalf("online resolution validated %d well-known documents, want 2", got)
		}
	})
	t.Run("search", func(t *testing.T) {
		for _, selector := range []string{"all", "public", fixture.server.URL} {
			results, endpointErrs, err := FederatedSearch(ctx, ec, "review", SearchOpts{Marketplace: selector})
			if err != nil || len(endpointErrs) != 0 || len(results) != 1 || results[0].Name != "review" || results[0].Marketplace != "public" {
				t.Fatalf("source-only search %q: results = %v, endpoint errors = %v, error = %v", selector, results, endpointErrs, err)
			}
		}
	})
	plan, err := r.PrepareLocator(ctx, ec.Merged.Marketplaces[0], MarketplaceLocator{Registry: "source", Kind: "skill", Name: "review"}, []hcc.Target{hcc.TargetClaude})
	if err != nil {
		t.Fatalf("PrepareLocator: %v", err)
	}
	if plan.Entry.InstanceID != registrationInstanceID || plan.Entry.URL != fixture.server.URL || plan.Pin.RegistryID != "registry-id" || plan.Artifact.Manifest.Name != "review" {
		t.Fatalf("prepared acquisition did not retain real identity: %#v", plan)
	}
	bound := &config.Config{Marketplaces: []config.MarketplaceEntry{plan.Entry}}
	bound.Marketplaces[0].Resources = []config.MarketplaceResourcePin{plan.Pin}
	resolved, errs := r.Resolve(ctx, bound, []hcc.Target{hcc.TargetClaude}, resolver.MarketplaceResolveModeCachedNoFetch)
	if len(errs) != 0 || len(resolved) != 1 || resolved[0].MarketplaceInstanceID != registrationInstanceID || resolved[0].Resource == nil {
		t.Fatalf("acquired offline resolution = %#v, errors = %v", resolved, errs)
	}
	if ec.Merged.Marketplaces[0].InstanceID != "" || len(ec.Merged.Marketplaces[0].Resources) != 0 {
		t.Fatal("acquisition mutated authored registration")
	}
}

func TestRegistrationIdentityExactEntriesFailClosed(t *testing.T) {
	fixture := newRegistrationIdentityFixture(t, nil)
	r := &Resolver{Cache: NewCache(t.TempDir())}
	ctx := context.Background()
	valid := config.MarketplaceEntry{URL: fixture.server.URL, Alias: "public", InstanceID: registrationInstanceID, Resources: []config.MarketplaceResourcePin{fixture.pin}}
	// Prime real verified evidence: the negative cases must not be cache misses.
	resolved, errs := r.Resolve(ctx, &config.Config{Marketplaces: []config.MarketplaceEntry{valid}}, nil, resolver.MarketplaceResolveModeRefresh)
	if len(errs) != 0 || len(resolved) != 1 {
		t.Fatalf("valid exact-pin control = %v, errors = %v", resolved, errs)
	}
	results, endpointErrs, err := FederatedSearch(ctx, config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{valid}}}, "review", SearchOpts{})
	if err != nil || len(endpointErrs) != 0 || len(results) != 1 {
		t.Fatalf("valid search control = %v, %v, %v", results, endpointErrs, err)
	}
	plan, err := r.PrepareLocator(ctx, valid, MarketplaceLocator{Registry: "source", Kind: "skill", Name: "review"}, nil)
	if err != nil || plan.Entry.InstanceID != registrationInstanceID || plan.Pin.RegistryID != "registry-id" {
		t.Fatalf("valid exact-identity prepared install control: error = %v", err)
	}
	for _, tc := range []struct {
		name   string
		id     string
		noPins bool
	}{
		{name: "wrong instance with pin", id: "another-instance"},
		{name: "wrong instance without pins", id: "another-instance", noPins: true},
		{name: "whitespace is not an unacquired registration", id: " ", noPins: true},
		{name: "missing instance with pin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := valid
			entry.InstanceID = tc.id
			if tc.noPins {
				entry.Resources = nil
			}
			cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}
			before := fixture.acquisitions.Load()
			resolved, errs := r.Resolve(ctx, cfg, nil, resolver.MarketplaceResolveModeCached)
			if len(resolved) != 0 || len(errs) != 1 {
				t.Fatalf("invalid exact identity resolved: %v, %v", resolved, errs)
			}
			if fixture.acquisitions.Load() != before {
				t.Fatal("invalid exact identity reached acquisition")
			}
			beforeSearch := fixture.searches.Load()
			results, endpointErrs, err := FederatedSearch(ctx, config.EffectiveConfig{Merged: cfg}, "review", SearchOpts{})
			if len(results) != 0 || err == nil && len(endpointErrs) != 1 {
				t.Fatalf("invalid exact identity searched: %v, %v, %v", results, endpointErrs, err)
			}
			if fixture.searches.Load() != beforeSearch {
				t.Fatal("invalid exact identity reached search")
			}
			before = fixture.acquisitions.Load()
			plan, err := r.PrepareLocator(ctx, entry, MarketplaceLocator{Registry: "source", Kind: "skill", Name: "review"}, nil)
			if plan != nil || err == nil || fixture.acquisitions.Load() != before {
				t.Fatalf("invalid exact identity acquired a prepared install: plan returned = %t, error = %v", plan != nil, err)
			}
			if !tc.noPins {
				before := fixture.requests.Load()
				resolved, errs := r.Resolve(ctx, cfg, nil, resolver.MarketplaceResolveModeCachedNoFetch)
				if len(resolved) != 0 || len(errs) != 1 || fixture.requests.Load() != before {
					t.Fatalf("invalid exact pin used offline evidence or network: %v, %v", resolved, errs)
				}
			}
		})
	}
}

func TestRegistrationIdentityInvalidMetadataAndOffline(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		code int
	}{
		{name: "missing", code: http.StatusNotFound},
		{name: "malformed JSON", body: "{"},
		{name: "missing identity", body: `{"canonical_url":"https://example.com","api_base":"/api/v1"}`},
		{name: "missing capabilities", body: `{"instance_id":"real-deployed-marketplace","canonical_url":"https://example.com","api_base":"/api/v1"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newRegistrationIdentityFixture(t, func(w http.ResponseWriter) {
				if tc.code != 0 {
					w.WriteHeader(tc.code)
				}
				_, _ = w.Write([]byte(tc.body))
			})
			ec := sourceOnlyRegistration(t, fixture.server.URL)
			r := &Resolver{Cache: NewCache(t.TempDir())}
			resolved, errs := r.Resolve(context.Background(), ec.Merged, nil, resolver.MarketplaceResolveModeRefresh)
			if len(resolved) != 0 || len(errs) != 1 || fixture.wellKnown.Load() != 1 {
				t.Fatalf("invalid metadata resolution = %v, %v; well-known requests = %d", resolved, errs, fixture.wellKnown.Load())
			}
			if tc.code != 0 {
				var apiErr *APIError
				if !errors.As(errs[0], &apiErr) || apiErr.StatusCode != tc.code {
					t.Fatalf("missing metadata error = %v, want APIError %d", errs, tc.code)
				}
			}
			results, endpointErrs, err := FederatedSearch(context.Background(), ec, "review", SearchOpts{})
			if len(results) != 0 || err != nil || len(endpointErrs) != 1 || fixture.wellKnown.Load() != 2 {
				t.Fatalf("invalid metadata search = %v, %v, %v; well-known requests = %d", results, endpointErrs, err, fixture.wellKnown.Load())
			}
			if fixture.searches.Load() != 0 || fixture.acquisitions.Load() != 0 {
				t.Fatal("invalid metadata reached search or resource acquisition")
			}
			before := fixture.requests.Load()
			resolved, errs = r.Resolve(context.Background(), ec.Merged, nil, resolver.MarketplaceResolveModeCachedNoFetch)
			if len(resolved) != 0 || len(errs) != 0 || fixture.requests.Load() != before {
				t.Fatalf("offline source-only registration acquired untrusted identity: %v, %v", resolved, errs)
			}
		})
	}
}

func TestRegistrationIdentityDeclarationPolicyPrecedesMetadata(t *testing.T) {
	fixture := newRegistrationIdentityFixture(t, nil)
	ec := sourceOnlyRegistration(t, fixture.server.URL)
	deny := []string{}
	ec.AllowedMarketplaces = deny
	ec.Merged.AllowedMarketplaces = &deny
	r := &Resolver{Cache: NewCache(t.TempDir())}
	resolved, errs := r.Resolve(context.Background(), ec.Merged, nil, resolver.MarketplaceResolveModeRefresh)
	var blocked *config.MarketplaceURLBlockedError
	if len(resolved) != 0 || len(errs) != 1 || !errors.As(errs[0], &blocked) {
		t.Fatalf("source-only allowlist refusal = %v, %v", resolved, errs)
	}
	results, endpointErrs, err := FederatedSearch(context.Background(), ec, "review", SearchOpts{})
	if len(results) != 0 || len(endpointErrs) != 0 || !errors.As(err, &blocked) || fixture.requests.Load() != 0 {
		t.Fatalf("source-only allowlist search = %v, %v, %v; requests = %d", results, endpointErrs, err, fixture.requests.Load())
	}

	entry := ec.Merged.Marketplaces[0]
	for _, tc := range []struct {
		name    string
		entries []config.MarketplaceEntry
	}{
		{name: "reserved alias", entries: []config.MarketplaceEntry{{URL: fixture.server.URL, Alias: "all"}}},
		{name: "unsafe alias", entries: []config.MarketplaceEntry{{URL: fixture.server.URL, Alias: "public\n"}}},
		{name: "unsafe URL", entries: []config.MarketplaceEntry{{URL: fixture.server.URL + "?token=secret", Alias: "public"}}},
		{name: "conflicting alias", entries: []config.MarketplaceEntry{entry, {URL: "https://other.example", Alias: "public", Source: config.UserPersonal}}},
		{name: "unresolved alias cannot hide exact identity", entries: []config.MarketplaceEntry{entry, {URL: fixture.server.URL, Alias: "public", InstanceID: "other-instance", Source: config.UserPersonal}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: tc.entries}}
			results, endpointErrs, err := FederatedSearch(context.Background(), ec, "review", SearchOpts{Marketplace: "public"})
			if len(results) != 0 || len(endpointErrs) != 0 || err == nil || fixture.requests.Load() != 0 {
				t.Fatalf("invalid declaration reached metadata: %v, %v, %v; requests = %d", results, endpointErrs, err, fixture.requests.Load())
			}
		})
	}
	// A valid declaration reaches the same live endpoint after the policy gate.
	ec.AllowedMarketplaces = nil
	ec.Merged.AllowedMarketplaces = nil
	results, endpointErrs, err = FederatedSearch(context.Background(), ec, "review", SearchOpts{})
	if err != nil || len(endpointErrs) != 0 || len(results) != 1 || fixture.searches.Load() != 1 {
		t.Fatalf("valid declaration control = %v, %v, %v", results, endpointErrs, err)
	}
}

func TestRegistrationIdentityOfflineDiscoveryRejectsUntrustedNamespace(t *testing.T) {
	fixture := newRegistrationIdentityFixture(t, nil)
	ec := sourceOnlyRegistration(t, fixture.server.URL)
	cache := NewDiscoveryCache(t.TempDir())
	entry := ec.Merged.Marketplaces[0]
	item := mpapi.ResourceListItem{RegistryID: "registry-id", RegistrySlug: "source", Kind: "skill", Name: "untrusted", Revision: fixture.pin.Digest, ContentDigest: fixture.pin.Digest}
	out := DiscoveryOutcome{Endpoint: entry, Items: []mpapi.ResourceListItem{item}, Total: 1, ObservedAt: time.Now()}
	if err := cache.Store(out, "", "", DiscoveryPageLimit); err != nil {
		t.Fatal(err)
	}
	opts := DiscoveryOptions{Cache: cache}
	if outcomes, _, err := CachedDiscovery(ec, "", opts); err == nil || len(outcomes) != 0 {
		t.Fatalf("source-only registration adopted empty-ID cached metadata: %#v, %v", outcomes, err)
	}
	if fixture.requests.Load() != 0 {
		t.Fatal("offline discovery fetched an identity")
	}
	// A separate exact-ID namespace remains usable without network access.
	entry.InstanceID = registrationInstanceID
	out.Endpoint = entry
	out.Items[0].Name = "trusted-coordinate"
	if err := cache.Store(out, "", "", DiscoveryPageLimit); err != nil {
		t.Fatal(err)
	}
	ec.Merged.Marketplaces = []config.MarketplaceEntry{entry}
	outcomes, needsNetwork, err := CachedDiscovery(ec, "", opts)
	if err != nil || needsNetwork || len(outcomes) != 1 || len(outcomes[0].Items) != 1 || outcomes[0].Items[0].Name != "trusted-coordinate" || outcomes[0].Endpoint.InstanceID != registrationInstanceID {
		t.Fatalf("exact-ID offline discovery control = %#v, needs network = %t, error = %v", outcomes, needsNetwork, err)
	}
	if fixture.requests.Load() != 0 {
		t.Fatal("exact-ID cache control fetched metadata")
	}
}
