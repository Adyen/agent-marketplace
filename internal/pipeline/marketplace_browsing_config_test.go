package pipeline

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func gateMarketplaceAcquisition(t *testing.T, fake *layerMarketplaceServer) func() {
	t.Helper()
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	inner := fake.Config.Handler
	fake.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/") {
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}
		inner.ServeHTTP(w, r)
	})
	t.Cleanup(unblock)
	return unblock
}

func writeOrganizationMarketplaceConfig(t *testing.T, path, marketplaceURL string) {
	t.Helper()
	writeFileMode(t, path, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  mp: {marketplace: "+marketplaceURL+"}\nresources:\n  mp: {include: [source/skill/review]}\n", 0o600)
}

func missingEffectiveOpts(dir, orgPath string) config.LoadEffectiveOptions {
	return config.LoadEffectiveOptions{
		OrganizationPath: orgPath,
		GlobalPath:       filepath.Join(dir, "missing-user.yaml"),
		RepoPath:         filepath.Join(dir, "missing-repo.yaml"),
		LocalPath:        filepath.Join(dir, "missing-local.yaml"),
	}
}

func TestBrowsingConfigReadyPublishesVerifiedEndpointBeforeAcquisition(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	dir := t.TempDir()

	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "review")
	release := gateMarketplaceAcquisition(t, fake)

	orgPath := filepath.Join(dir, "config.yaml")
	writeOrganizationMarketplaceConfig(t, orgPath, fake.URL)

	browsingSeen := make(chan *config.Config, 1)
	authoritativeSeen := make(chan *config.Config, 1)
	resultCh := make(chan ResolveResult, 1)
	workspace := mustWorkspace(t, repo)

	go func() {
		resultCh <- Resolve(context.Background(), ResolveOptions{
			Workspace:     workspace,
			CatalogMode:   catalog.LoadModeInspect,
			EffectiveOpts: missingEffectiveOpts(dir, orgPath),
			BrowsingConfigReady: func(merged *config.Config) {
				browsingSeen <- merged
			},
			ConfigReady: func(merged *config.Config) {
				authoritativeSeen <- merged
			},
		})
	}()

	var published *config.Config
	select {
	case published = <-browsingSeen:
	case <-time.After(30 * time.Second):
		t.Fatal("no browse-safe config published while organization Marketplace acquisition was blocked")
	}
	select {
	case <-resultCh:
		t.Fatal("resolve returned before transient acquisition was released")
	default:
	}

	if len(published.Marketplaces) != 1 {
		t.Fatalf("browsing config marketplaces = %#v", published.Marketplaces)
	}
	entry := published.Marketplaces[0]
	if entry.InstanceID != fake.instanceID {
		t.Fatalf("browsing endpoint instance_id = %q, want well-known verified %q", entry.InstanceID, fake.instanceID)
	}
	if len(entry.Resources) != 0 {
		t.Fatalf("browsing endpoint carried %d provisional pins that must never be emitted", len(entry.Resources))
	}

	release()
	var result ResolveResult
	select {
	case result = <-resultCh:
	case <-time.After(60 * time.Second):
		t.Fatal("resolve did not finish after acquisition was released")
	}
	if len(result.Errors) != 0 {
		t.Fatalf("resolve errors = %v", result.Errors)
	}
	var authoritative *config.Config
	select {
	case authoritative = <-authoritativeSeen:
	case <-time.After(5 * time.Second):
		t.Fatal("authoritative ConfigReady never fired")
	}
	if len(authoritative.Marketplaces) != 1 || len(authoritative.Marketplaces[0].Resources) == 0 {
		t.Fatalf("authoritative config missing verified pins: %#v", authoritative.Marketplaces)
	}
	if authoritative.Marketplaces[0].InstanceID != fake.instanceID {
		t.Fatalf("authoritative endpoint instance_id = %q", authoritative.Marketplaces[0].InstanceID)
	}
}

func TestProvisionalConfigReadyPublishesPinsBeforeAcquisition(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	dir := t.TempDir()

	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "review")
	release := gateMarketplaceAcquisition(t, fake)

	orgPath := filepath.Join(dir, "config.yaml")
	writeOrganizationMarketplaceConfig(t, orgPath, fake.URL)

	provisionalSeen := make(chan *config.Config, 1)
	browsingSeen := make(chan *config.Config, 1)
	resultCh := make(chan ResolveResult, 1)
	workspace := mustWorkspace(t, repo)

	go func() {
		resultCh <- Resolve(context.Background(), ResolveOptions{
			Workspace:     workspace,
			CatalogMode:   catalog.LoadModeInspect,
			EffectiveOpts: missingEffectiveOpts(dir, orgPath),
			ProvisionalConfigReady: func(merged *config.Config, _ *config.LocalConfig) {
				provisionalSeen <- merged
			},
			BrowsingConfigReady: func(merged *config.Config) {
				browsingSeen <- merged
			},
		})
	}()

	var published *config.Config
	select {
	case published = <-provisionalSeen:
	case <-time.After(30 * time.Second):
		t.Fatal("no provisional config published while organization Marketplace acquisition was blocked")
	}
	select {
	case <-resultCh:
		t.Fatal("resolve returned before transient acquisition was released")
	default:
	}

	if len(published.Marketplaces) != 1 {
		t.Fatalf("provisional config marketplaces = %#v", published.Marketplaces)
	}
	entry := published.Marketplaces[0]
	if len(entry.Resources) != 1 {
		t.Fatalf("provisional endpoint carried %d pins, want the 1 configured selection", len(entry.Resources))
	}
	pin := entry.Resources[0]
	if pin.RegistryID != "source" || pin.Kind != "skill" || pin.Name != "review" {
		t.Fatalf("provisional pin = %#v, want source/skill/review", pin)
	}

	release()
	var result ResolveResult
	select {
	case result = <-resultCh:
	case <-time.After(60 * time.Second):
		t.Fatal("resolve did not finish after acquisition was released")
	}
	if len(result.Errors) != 0 {
		t.Fatalf("resolve errors = %v", result.Errors)
	}
}

func TestBrowsingConfigReadyNeverPublishesUnsafeConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "review")
	dead := newLayerMarketplaceServer(t)
	deadURL := dead.URL
	dead.Close()

	zero := 0
	cases := []struct {
		name      string
		org       string
		lock      string
		published *int
		wantErrs  bool
	}{
		{name: "no transient acquisition", org: "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n", published: nil},
		{name: "denied by allowed_marketplaces", org: "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nallowed_marketplaces: [\"^https://blocked\\\\.example$\"]\nsources:\n  mp: {marketplace: " + fake.URL + "}\nresources:\n  mp: {include: [source/skill/review]}\n", published: nil, wantErrs: true},
		{name: "malformed deployed lock", org: "marketplace", lock: "not: [valid\n", published: nil, wantErrs: true},
		{name: "unbindable endpoint", org: deadURL, published: &zero, wantErrs: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := t.TempDir()
			dir := t.TempDir()
			orgPath := filepath.Join(dir, "config.yaml")
			switch tc.org {
			case "marketplace":
				writeOrganizationMarketplaceConfig(t, orgPath, fake.URL)
			case deadURL:
				writeOrganizationMarketplaceConfig(t, orgPath, deadURL)
			default:
				writeFileMode(t, orgPath, tc.org, 0o600)
			}
			if tc.lock != "" {
				lockPath, err := lockfile.PathForConfig(orgPath)
				if err != nil {
					t.Fatalf("lock path: %v", err)
				}
				writeFileMode(t, lockPath, tc.lock, 0o600)
			}

			var published *config.Config
			result := Resolve(context.Background(), ResolveOptions{
				Workspace:     mustWorkspace(t, repo),
				CatalogMode:   catalog.LoadModeInspect,
				EffectiveOpts: missingEffectiveOpts(dir, orgPath),
				BrowsingConfigReady: func(merged *config.Config) {
					published = merged
				},
			})
			if tc.wantErrs && len(result.Errors) == 0 {
				t.Fatal("resolve unexpectedly succeeded")
			}
			if !tc.wantErrs && len(result.Errors) != 0 {
				t.Fatalf("resolve errors = %v", result.Errors)
			}
			if tc.published == nil {
				if published != nil {
					t.Fatalf("browsing callback fired for unsafe config: %#v", published.Marketplaces)
				}
				return
			}
			if published == nil {
				t.Fatal("browsing callback never fired")
			}
			if len(published.Marketplaces) != *tc.published {
				t.Fatalf("published marketplaces = %#v", published.Marketplaces)
			}
			for _, entry := range published.Marketplaces {
				if len(entry.Resources) != 0 {
					t.Fatalf("published endpoint carried %d provisional pins", len(entry.Resources))
				}
			}
		})
	}
}
