package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestResolvePlacePackagesUsesExactOfflineEvidence(t *testing.T) {
	profileBytes := []byte(placeAcquireProfileYAML)
	identity := placeAcquireProfileIdentity(profileBytes)
	files := []targetprofile.PackageFile{{SourcePath: "payload/instructions.md", DestinationPath: "mods/sample/instructions.md", Mode: 0o644, Bytes: []byte("resolver place\n")}}
	coordinate := store.ResourceCoordinate{Marketplace: "mp", Registry: "tools-id", Kind: "invented-app/mod", Name: "sample"}
	artifact, err := hccartifact.NewPlaceArtifact(coordinate.Kind, coordinate.Name, identity, files)
	if err != nil {
		t.Fatal(err)
	}
	_, placeholder, archiveDigest, err := hccartifact.EncodeArchives(artifact)
	if err != nil {
		t.Fatal(err)
	}
	revision := placeAcquireRevision(t, coordinate, artifact, archiveDigest)
	locked := lockedPlaceResource(revision)
	pin := config.MarketplaceResourcePin{
		RegistryID: "tools-id", RegistrySlug: "tools", Kind: locked.Kind, Name: locked.Name,
		Revision: locked.Revision, Digest: string(locked.ContentDigest), Acquisition: config.MarketplaceAcquisitionMirror,
		Direct: true, LockArchiveDigest: string(locked.ArchiveDigest), LockAssessmentDigest: string(locked.AssessmentDigest),
		LockHostPackageIdentity: *locked.HostPackageIdentity,
	}
	profilePin := config.MarketplaceResourcePin{
		RegistryID: "tools-id", RegistrySlug: "tools", Kind: "target-profile", Name: identity.TargetID,
		Revision: identity.RevisionDigest, Digest: identity.RevisionDigest, Acquisition: config.MarketplaceAcquisitionMirror,
		Direct: true, LockProfileIdentity: identity,
	}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{
		URL: "https://market.example", InstanceID: "mp", Source: config.UserPersonal,
		Resources: []config.MarketplaceResourcePin{profilePin, pin},
	}}}
	cache := NewCache(t.TempDir())
	writeLockedPlaceCache(t, cache, cfg.Marketplaces[0].URL, revision, placeholder)
	transport := &placeCountingTransport{}
	r := &Resolver{Cache: cache, HTTPClient: &http.Client{Transport: transport}}
	approved := map[targetprofile.Identity][]byte{identity: profileBytes}

	resolved, err := r.ResolvePlacePackages(context.Background(), cfg, approved, resolver.MarketplaceResolveModeCachedNoFetch)
	if err != nil {
		t.Fatalf("offline resolve: %v", err)
	}
	if len(resolved) != 1 || resolved[0].Pin.Name != pin.Name ||
		resolved[0].Revision.Assessment.AssessmentDigest != revision.Assessment.AssessmentDigest ||
		len(resolved[0].Files) != 1 || string(resolved[0].Files[0].Bytes) != string(files[0].Bytes) {
		t.Fatalf("resolved place packages = %+v", resolved)
	}
	if transport.requests.Load() != 0 {
		t.Fatalf("CachedNoFetch made %d HTTP requests", transport.requests.Load())
	}
	if out, err := r.ResolvePlacePackages(context.Background(), cfg, approved, resolver.MarketplaceResolveModeCached); err == nil || out != nil {
		t.Fatalf("unsupported mode result = %+v, %v", out, err)
	}
	if transport.requests.Load() != 0 {
		t.Fatal("unsupported mode issued HTTP")
	}
	before := transport.requests.Load()
	withoutApproval, err := r.ResolvePlacePackages(context.Background(), cfg, nil, resolver.MarketplaceResolveModeRefresh)
	if err != nil || len(withoutApproval) != 0 {
		t.Fatalf("missing approved profile = %+v, %v; want no acquisition", withoutApproval, err)
	}
	if transport.requests.Load() != before {
		t.Fatal("missing approved profile issued HTTP")
	}

	badCfg := *cfg
	badEntry := cfg.Marketplaces[0]
	badEntry.Resources = append([]config.MarketplaceResourcePin(nil), badEntry.Resources...)
	for i := range badEntry.Resources {
		if badEntry.Resources[i].Kind == pin.Kind {
			binding := badEntry.Resources[i].LockHostPackageIdentity
			binding.ProfileIdentity.RevisionDigest = "sha256:" + strings.Repeat("e", 64)
			binding.ProfileDigest = binding.ProfileIdentity.RevisionDigest
			badEntry.Resources[i].LockHostPackageIdentity = binding
		}
	}
	badCfg.Marketplaces = []config.MarketplaceEntry{badEntry}
	before = transport.requests.Load()
	if out, err := r.ResolvePlacePackages(context.Background(), &badCfg, approved, resolver.MarketplaceResolveModeRefresh); err == nil || out != nil || !strings.Contains(err.Error(), "marketplace.profile.revision-mismatch") {
		t.Fatalf("stale package binding result = %+v, %v", out, err)
	}
	if transport.requests.Load() != before {
		t.Fatal("stale package binding issued HTTP")
	}
	if out, err := r.ResolvePlacePackages(context.Background(), cfg, map[targetprofile.Identity][]byte{identity: []byte("invalid profile")}, resolver.MarketplaceResolveModeRefresh); err == nil || out != nil {
		t.Fatalf("invalid approved profile result = %+v, %v", out, err)
	}
	if transport.requests.Load() != before {
		t.Fatal("invalid approved profile issued HTTP")
	}

	var wellKnownRequests, packageRequests atomic.Int32
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/.well-known/agent-marketplace.json" {
			wellKnownRequests.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("different-instance", "https://market.example"))
			return
		}
		packageRequests.Add(1)
		http.NotFound(w, req)
	}))
	defer peer.Close()
	identityCfg := *cfg
	identityEntry := cfg.Marketplaces[0]
	identityEntry.URL = peer.URL
	identityCfg.Marketplaces = []config.MarketplaceEntry{identityEntry}
	identityCache := NewCache(t.TempDir())
	identityResolver := &Resolver{Cache: identityCache}
	if out, err := identityResolver.ResolvePlacePackages(context.Background(), &identityCfg, approved, resolver.MarketplaceResolveModeRefresh); err == nil ||
		out != nil || !strings.Contains(err.Error(), "instance ID mismatch") {
		t.Fatalf("well-known identity mismatch result = %+v, %v", out, err)
	}
	if wellKnownRequests.Load() != 1 || packageRequests.Load() != 0 {
		t.Fatalf("identity mismatch requests: well-known=%d package=%d", wellKnownRequests.Load(), packageRequests.Load())
	}
	if entries, err := os.ReadDir(identityCache.root); err != nil || len(entries) != 0 {
		t.Fatalf("identity mismatch wrote cache files: entries=%v err=%v", entries, err)
	}

	emptyCacheResolver := &Resolver{Cache: NewCache(t.TempDir()), HTTPClient: &http.Client{Transport: transport}}
	if out, err := emptyCacheResolver.ResolvePlacePackages(context.Background(), cfg, approved, resolver.MarketplaceResolveModeCachedNoFetch); err == nil || out != nil {
		t.Fatalf("missing offline cache result = %+v, %v", out, err)
	}
	if transport.requests.Load() != before {
		t.Fatal("missing offline cache issued HTTP")
	}
}

type placeCountingTransport struct{ requests atomic.Int32 }

func (t *placeCountingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.requests.Add(1)
	return nil, errors.New("unexpected HTTP request")
}
