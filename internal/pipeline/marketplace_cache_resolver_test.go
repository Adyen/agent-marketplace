package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// recordingMarketplaceResolver records how it was invoked and returns a fixed
// result, so a test can pin the adapter's own behavior without a Marketplace.
type recordingMarketplaceResolver struct {
	modes     []resolver.MarketplaceResolveMode
	artifacts []resolver.ResolvedArtifact
	errs      []error
}

func (r *recordingMarketplaceResolver) Resolve(ctx context.Context, cfg *config.Config, targets []hcc.Target, mode resolver.MarketplaceResolveMode) ([]resolver.ResolvedArtifact, []error) {
	return r.ResolveWithPolicy(ctx, cfg, targets, mode, nil)
}

func (r *recordingMarketplaceResolver) ResolveWithPolicy(_ context.Context, _ *config.Config, _ []hcc.Target, mode resolver.MarketplaceResolveMode, _ resolver.MarketplaceRevocationPolicy) ([]resolver.ResolvedArtifact, []error) {
	r.modes = append(r.modes, mode)
	return r.artifacts, r.errs
}

func testMarketplacePinError(err error) *marketplace.PinError {
	return &marketplace.PinError{
		Entry: config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"},
		Pin:   config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review"},
		Err:   err,
	}
}

func testUnhealthyPinStatus() marketplace.PinStatus {
	return marketplace.PinStatus{
		URL:        "https://market.example",
		RegistryID: "src",
		Kind:       "skill",
		Name:       "review",
		Required:   true,
		State:      marketplace.PinStateRevoked,
	}
}

func TestMarketplaceCacheArtifactResolverMergesPinHealthErrorsWithoutDuplication(t *testing.T) {
	delegate := &recordingMarketplaceResolver{errs: []error{testMarketplacePinError(errors.New("boom"))}}
	adapter := marketplaceCacheArtifactResolver{
		delegate: delegate,
		pins:     []marketplace.PinStatus{testUnhealthyPinStatus()},
	}

	_, errs := adapter.Resolve(context.Background(), &config.Config{}, nil, resolver.MarketplaceResolveModeInspect)

	if len(errs) != 1 {
		t.Fatalf("merged errors = %v, want exactly one row for the single broken pin", errs)
	}
	var statusErr *marketplace.PinStatusError
	if !errors.As(errs[0], &statusErr) {
		t.Fatalf("merged error = %v, want the inspected pin health error", errs[0])
	}
}

func TestMarketplaceCacheArtifactResolverKeepsUnrelatedPinErrors(t *testing.T) {
	other := &marketplace.PinError{
		Entry: config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"},
		Pin:   config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "other"},
		Err:   errors.New("boom"),
	}
	entryLevel := errors.New("marketplace https://market.example: unreachable")
	delegate := &recordingMarketplaceResolver{errs: []error{other, entryLevel}}
	adapter := marketplaceCacheArtifactResolver{
		delegate: delegate,
		pins:     []marketplace.PinStatus{testUnhealthyPinStatus()},
	}

	_, errs := adapter.Resolve(context.Background(), &config.Config{}, nil, resolver.MarketplaceResolveModeInspect)

	// One health error for "review", plus both delegate errors, which concern
	// a different pin and no pin at all.
	if len(errs) != 3 {
		t.Fatalf("merged errors = %v, want the health error plus both unrelated failures", errs)
	}
}

func TestMarketplaceCacheArtifactResolverPreservesTypedNilPinError(t *testing.T) {
	var pinErr *marketplace.PinError
	wrapped := errors.Join(pinErr)
	delegate := &recordingMarketplaceResolver{errs: []error{wrapped}}
	adapter := marketplaceCacheArtifactResolver{delegate: delegate}

	_, errs := adapter.Resolve(context.Background(), &config.Config{}, nil, resolver.MarketplaceResolveModeInspect)
	if len(errs) != 1 || errs[0] != wrapped {
		t.Fatal("expected the unidentifiable error to pass through unchanged")
	}
}

func TestMarketplacePendingPinsIgnoresTypedNilErrors(t *testing.T) {
	var pinErr *marketplace.PinError
	var statusErr *marketplace.PinStatusError
	for name, err := range map[string]error{
		"pin": errors.Join(pinErr), "status": errors.Join(statusErr),
	} {
		t.Run(name, func(t *testing.T) {
			valid := testMarketplacePinError(errors.New("missing"))
			pending := MarketplacePendingPins([]error{err, valid})
			if len(pending) != 1 {
				t.Fatal("expected only the valid pin to be pending")
			}
			if _, ok := pending["skill\x00review"]; !ok {
				t.Fatal("expected the valid review pin to be pending")
			}
		})
	}
}

func TestSuppressUnmetMarketplaceRequirementsPreservesTypedNilWarning(t *testing.T) {
	var unmet *resolver.UnmetRequirementWarning
	wrapped := errors.Join(unmet)
	valid := &resolver.UnmetRequirementWarning{Requirement: catalog.Requirement{Kind: ir.KindSkill, Name: "review"}}
	warnings := SuppressUnmetMarketplaceRequirements([]error{wrapped, valid}, map[string]struct{}{"skill\x00review": {}})
	if len(warnings) != 1 || warnings[0] != wrapped {
		t.Fatal("expected the unidentifiable warning to pass through unchanged")
	}
}

func TestMarketplaceCacheArtifactResolverForcesCachedNoFetch(t *testing.T) {
	delegate := &recordingMarketplaceResolver{}
	adapter := marketplaceCacheArtifactResolver{delegate: delegate}

	for _, mode := range []resolver.MarketplaceResolveMode{
		resolver.MarketplaceResolveModeInspect,
		resolver.MarketplaceResolveModeRefresh,
		resolver.MarketplaceResolveModeCached,
	} {
		adapter.Resolve(context.Background(), &config.Config{}, nil, mode)
	}

	if len(delegate.modes) != 3 {
		t.Fatalf("delegate invocations = %d, want 3", len(delegate.modes))
	}
	for _, got := range delegate.modes {
		if got != resolver.MarketplaceResolveModeCachedNoFetch {
			t.Fatalf("delegate resolve mode = %v, want cached-no-fetch for every requested mode", got)
		}
	}
}

// TestMarketplaceCacheArtifactResolverClassifiesMissingCacheEvidence drives the
// real Marketplace resolver against a cache that has no endpoint behind it, so
// a reachable server cannot mask a missing-evidence classification.
func TestMarketplaceCacheArtifactResolverClassifiesMissingCacheEvidence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	canonical, err := hccartifact.FromIR(ir.NewSkill("review", "Review", "Review carefully.", nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatal(err)
	}
	canonical.Manifest.RegistryID = "src"
	contentDigest := canonical.Manifest.ContentDigest
	_, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatal(err)
	}
	entry := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp", Resources: []config.MarketplaceResourcePin{{
		RegistryID: "src", Kind: "skill", Name: "review", Revision: contentDigest, Digest: contentDigest,
	}}}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}
	cache := marketplace.DefaultCache()
	adapter := marketplaceCacheArtifactResolver{delegate: &marketplace.Resolver{Cache: cache}}

	// No evidence at all: the pin was selected but never synced.
	_, errs := adapter.Resolve(context.Background(), cfg, nil, resolver.MarketplaceResolveModeInspect)
	if len(errs) != 1 {
		t.Fatalf("errors for a never-acquired pin = %v, want exactly one", errs)
	}
	if !IsMarketplaceCacheMetadataMissingWarning(errs[0]) {
		t.Fatalf("never-acquired pin produced a hard failure: %v", errs[0])
	}
	for _, want := range []string{"src/skill/review", "agent-marketplace sync"} {
		if !strings.Contains(errs[0].Error(), want) {
			t.Fatalf("error %q does not name %q", errs[0], want)
		}
	}

	// Revision metadata present but no verified artifact: the pin is half
	// acquired, which is a hard failure the user must repair with sync.
	revision := testAssessedResourceRevision(t, store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(contentDigest),
		ArchiveDigest: store.ArchiveDigest(archiveDigest),
	}, store.AssessmentDecisionAllow, nil)
	if _, err := cache.WriteResourceRevision(entry.URL, "src", "skill", "review", contentDigest, revision); err != nil {
		t.Fatal(err)
	}

	_, errs = adapter.Resolve(context.Background(), cfg, nil, resolver.MarketplaceResolveModeInspect)
	if len(errs) != 1 {
		t.Fatalf("errors for an unverified artifact = %v, want exactly one", errs)
	}
	if IsMarketplaceCacheMetadataMissingWarning(errs[0]) {
		t.Fatalf("missing verified artifact was downgraded to a warning: %v", errs[0])
	}
	for _, want := range []string{"src/skill/review", "agent-marketplace sync"} {
		if !strings.Contains(errs[0].Error(), want) {
			t.Fatalf("error %q does not name %q", errs[0], want)
		}
	}
}

// TestInspectResolveWrapsCacheResolverForTransientOrganizationLock proves the
// organization half of the fix: when the organization lock is intentionally
// absent and acquired transiently, the cache-backed artifact resolver goes
// through the same wrapper sync uses, so organization selections expand.
func TestInspectResolveWrapsCacheResolverForTransientOrganizationLock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))

	canonical, err := hccartifact.FromIR(ir.NewSkill("review", "Review", "Review carefully.", nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatal(err)
	}
	canonical.Manifest.RegistryID = "src"
	canonical.Manifest.RegistrySlug = "tools"
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatal(err)
	}
	revision := testAssessedResourceRevision(t, store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(canonical.Manifest.ContentDigest),
		ArchiveDigest: store.ArchiveDigest(archiveDigest),
	}, store.AssessmentDecisionAllow, nil)
	registry := mpapi.PublicRegistry{ID: "src", Slug: "tools", Origins: []string{"https://example.com/tools.git"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", "https://example.com"))
		case r.URL.Path == "/api/v1/registries/tools" || r.URL.Path == "/api/v1/registries/src":
			_ = json.NewEncoder(w).Encode(registry)
		case strings.HasPrefix(r.URL.Path, "/api/v1/resources/tools/skill/review") || strings.HasPrefix(r.URL.Path, "/api/v1/resources/src/skill/review"):
			_ = json.NewEncoder(w).Encode(revision)
		case strings.HasPrefix(r.URL.Path, "/api/v1/artifacts/"):
			_, _ = w.Write(raw)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	if err := os.WriteFile(orgPath, []byte("apiVersion: agent-marketplace/v4\nsources:\n  hosted: {marketplace: "+server.URL+"}\nresources:\n  hosted: {include: [tools/skill/review]}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	userPath := filepath.Join(home, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	config.SetTestOrganizationConfigPath(t, orgPath)

	repo := t.TempDir()
	ctx := WithTransientOrganizationMarketplaceLockCache(context.Background())
	result := Resolve(ctx, ResolveOptions{
		Workspace:                     mustWorkspace(t, repo),
		CatalogMode:                   catalog.LoadModeInspect,
		ReadOnlyExactLocks:            true,
		MarketplaceArtifactsFromCache: true,
		EffectiveOpts: config.LoadEffectiveOptions{
			OrganizationPath: orgPath,
			GlobalPath:       userPath,
			RepoPath:         filepath.Join(repo, ".agents", "config.yaml"),
			LocalPath:        filepath.Join(repo, ".agents", "config.local.yaml"),
		},
		Loaders: Loaders{
			Org:    config.LoadOrganizationConfig,
			Global: func() (*config.Config, error) { return config.LoadConfigFromPath(userPath) },
			Repo:   func() (*config.Config, error) { return nil, nil },
			Local:  func() (*config.LocalConfig, error) { return nil, nil },
		},
	})

	adapter, ok := result.MarketplaceArtifactResolver.(marketplaceCacheArtifactResolver)
	if !ok {
		t.Fatalf("MarketplaceArtifactResolver = %T, want the cache-backed adapter", result.MarketplaceArtifactResolver)
	}
	if _, ok := adapter.delegate.(transientOrganizationMarketplaceResolver); !ok {
		t.Fatalf("adapter delegate = %T, want the transient organization resolver", adapter.delegate)
	}
	if err := errors.Join(result.Errors...); err != nil {
		t.Fatalf("inspect resolve of an organization Marketplace selection: %v", err)
	}
	var found bool
	for _, artifact := range result.Resolved {
		if artifact.IsMarketplace && artifact.Name == "review" {
			found = true
		}
	}
	if !found {
		t.Fatalf("organization Marketplace selection missing from the resolved set: %#v", result.Resolved)
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("inspect resolve created an organization lock: %v", err)
	}
}
