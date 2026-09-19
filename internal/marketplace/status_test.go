package marketplace

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
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestInspectPreservesPinStates(t *testing.T) {
	contentDigest := "sha256:" + strings.Repeat("a", 64)
	archiveDigest := "sha256:" + strings.Repeat("b", 64)
	coordinate := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "revoked"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			writeTestWellKnown(w, "mp")
			return
		}
		if strings.Contains(r.URL.Path, "/missing/") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if strings.Contains(r.URL.Path, "/unavailable/") {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		rev := revokedRevision(store.ResourceRevision{
			Coordinate:    coordinate,
			ContentDigest: store.RevisionDigest(contentDigest),
			ArchiveDigest: store.ArchiveDigest(archiveDigest),
		}, "revoked")
		_ = json.NewEncoder(w).Encode(rev)
	}))
	defer server.Close()

	cache := NewCache(t.TempDir())
	pins := []config.MarketplaceResourcePin{
		{RegistryID: "src", RegistrySlug: "source-slug", Kind: "skill", Name: "missing", Revision: "v1", Digest: contentDigest},
		{RegistryID: "src", RegistrySlug: "source-slug", Kind: "skill", Name: "revoked", Revision: "v1", Digest: contentDigest},
		{RegistryID: "src", RegistrySlug: "source-slug", Kind: "skill", Name: "unavailable", Revision: "v1", Digest: contentDigest},
	}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{
		URL: server.URL, InstanceID: "mp", Alias: "hostile\talias", Source: config.Organization, Resources: pins,
	}}}

	inspection := (&Resolver{Cache: cache}).Inspect(context.Background(), cfg, nil, resolver.MarketplaceResolveModeInspect)
	if len(inspection.Pins) != len(pins) {
		t.Fatalf("inspected %d pins, want %d", len(inspection.Pins), len(pins))
	}
	states := make(map[string]PinStatus, len(inspection.Pins))
	for _, pin := range inspection.Pins {
		states[pin.Name] = pin
	}
	if got := states["missing"].State; got != PinStateMissing {
		t.Fatalf("missing state = %q, want %q", got, PinStateMissing)
	}
	if got := states["revoked"].State; got != PinStateRevoked {
		t.Fatalf("revoked state = %q, want %q", got, PinStateRevoked)
	}
	if got := states["unavailable"].State; got != PinStateUnavailable {
		t.Fatalf("unavailable state = %q, want %q", got, PinStateUnavailable)
	}
	if !states["revoked"].Required || states["revoked"].Source != config.Organization {
		t.Fatalf("organization pin provenance = required %t, source %s; want required true, ORG", states["revoked"].Required, states["revoked"].Source)
	}
	if states["revoked"].RegistrySlug != "source-slug" || states["revoked"].ContentDigest != contentDigest {
		t.Fatalf("revoked identity = slug %q digest %q", states["revoked"].RegistrySlug, states["revoked"].ContentDigest)
	}
	resolved, errs := inspection.Resolve(context.Background(), cfg, nil, resolver.MarketplaceResolveModeInspect)
	if len(resolved) != 0 || len(errs) != len(pins) {
		t.Fatalf("inspection resolver output = %d artifacts, %d errors; want no artifacts and required health errors", len(resolved), len(pins))
	}
	for _, pin := range inspection.Pins {
		if pin.UpdateAvailable || (pin.Name != "revoked" && pin.LatestQueried) {
			t.Fatalf("pin %q claimed incorrect latest information: %#v", pin.Name, pin)
		}
	}
}

func TestInspectPreservesMaterializableArtifactForHealthyPin(t *testing.T) {
	resource := ir.NewSkill("review", "Review", "body", nil, nil, ir.SourceRef{})
	canonical, err := hccartifact.FromIR(resource)
	if err != nil {
		t.Fatal(err)
	}
	contentDigest := canonical.Manifest.ContentDigest
	canonical.Manifest.RegistryID = "src"
	canonical.Manifest.Revision = contentDigest
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatal(err)
	}
	revision := assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(contentDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest),
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			writeTestWellKnown(w, "mp")
			return
		}
		_ = json.NewEncoder(w).Encode(revision)
	}))
	defer server.Close()

	cache := NewCache(t.TempDir())
	entry := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{{
		RegistryID: "src", RegistrySlug: "source-slug", Kind: "skill", Name: "review", Revision: "v1", Digest: contentDigest,
	}}}
	if _, err := cache.WriteResourceRevision(server.URL, "src", "skill", "review", contentDigest, revision); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	if _, err := cache.WriteArtifact(archiveDigest, raw); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
		t.Fatalf("mark artifact verified: %v", err)
	}

	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}
	inspection := (&Resolver{Cache: cache}).Inspect(context.Background(), cfg, nil, resolver.MarketplaceResolveModeInspect)
	if len(inspection.Pins) != 1 || inspection.Pins[0].State != PinStateCurrent {
		t.Fatalf("pins = %#v, want one current pin", inspection.Pins)
	}
	// A healthy installed pin must carry the same resolved resource
	// information sync --check resolves so status can report installed output
	// and materialization drift identically.
	resolved, errs := inspection.Resolve(context.Background(), cfg, nil, resolver.MarketplaceResolveModeInspect)
	if len(errs) != 0 {
		t.Fatalf("inspection resolver errors = %v, want none", errs)
	}
	if len(resolved) != 1 {
		t.Fatalf("resolved artifacts = %#v, want the installed pin", resolved)
	}
	artifact := resolved[0]
	if artifact.Unresolved || artifact.Resource == nil || artifact.Name != "review" || artifact.Kind != ir.KindSkill {
		t.Fatalf("resolved artifact = %#v, want materializable review skill", artifact)
	}
	if !artifact.IsMarketplace || artifact.MarketplaceURL != server.URL || artifact.MarketplaceInstanceID != "mp" {
		t.Fatalf("resolved artifact provenance = %#v, want Marketplace identity", artifact)
	}
}

func TestInspectKeepsUnresolvedStubForUnverifiablePin(t *testing.T) {
	contentDigest := "sha256:" + strings.Repeat("a", 64)
	archiveDigest := "sha256:" + strings.Repeat("b", 64)
	revision := assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(contentDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest),
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			writeTestWellKnown(w, "mp")
			return
		}
		_ = json.NewEncoder(w).Encode(revision)
	}))
	defer server.Close()

	// The endpoint reports the pin healthy and revision metadata is cached,
	// but no verified artifact is: the inspection must not fabricate a
	// materializable artifact, and it must not let status conclude the pin's
	// managed output is orphaned while sync --check would refresh and report
	// in-sync.
	entry := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{{
		RegistryID: "src", Kind: "skill", Name: "review", Revision: "v1", Digest: contentDigest,
	}}}
	cache := NewCache(t.TempDir())
	if _, err := cache.WriteResourceRevision(server.URL, "src", "skill", "review", contentDigest, revision); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}
	inspection := (&Resolver{Cache: cache}).Inspect(context.Background(), cfg, nil, resolver.MarketplaceResolveModeInspect)
	if len(inspection.Pins) != 1 || inspection.Pins[0].Detail == "" {
		t.Fatalf("unverifiable pin status = %#v, want a degraded detail", inspection.Pins)
	}
	resolved, errs := inspection.Resolve(context.Background(), cfg, nil, resolver.MarketplaceResolveModeInspect)
	if len(resolved) != 0 {
		t.Fatalf("unverifiable pin resolved = %#v, want no materializable artifacts", resolved)
	}
	if len(errs) != 1 {
		t.Fatalf("unverifiable pin resolver errors = %v, want one drift-suppressing error", errs)
	}
}

func TestInspectCachedModeReportsVerifiedArtifact(t *testing.T) {
	contentDigest := "sha256:" + strings.Repeat("c", 64)
	archiveDigest := "sha256:" + strings.Repeat("d", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			writeTestWellKnown(w, "mp")
			return
		}
		_ = json.NewEncoder(w).Encode(assessedRevision(store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "cached"},
			ContentDigest: store.RevisionDigest(contentDigest),
			ArchiveDigest: store.ArchiveDigest(archiveDigest),
		}))
	}))
	defer server.Close()

	cache := NewCache(t.TempDir())
	entry := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{{
		RegistryID: "src", Kind: "skill", Name: "cached", Revision: "v1", Digest: contentDigest,
	}}}
	if _, err := cache.WriteResourceRevision(entry.URL, "src", "skill", "cached", contentDigest, assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "cached"},
		ContentDigest: store.RevisionDigest(contentDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest),
	})); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	if _, err := cache.WriteArtifact(archiveDigest, []byte("artifact")); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
		t.Fatalf("mark artifact verified: %v", err)
	}

	inspection := (&Resolver{Cache: cache}).Inspect(context.Background(), &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}, nil, resolver.MarketplaceResolveModeCached)
	if len(inspection.Pins) != 1 {
		t.Fatalf("inspected %d pins, want 1", len(inspection.Pins))
	}
	pin := inspection.Pins[0]
	if pin.State != PinStateCached || pin.Cache != CacheStateArtifactVerified || !pin.ArtifactVerified {
		t.Fatalf("cached pin = %#v, want cached verified state", pin)
	}
	if pin.EndpointQueried {
		t.Fatal("cached mode queried the exact revision")
	}
}

func TestInspectCachedNoFetchUsesOnlyCachedState(t *testing.T) {
	cachedResource := ir.NewSkill("cached", "Cached", "body", nil, nil, ir.SourceRef{})
	cachedCanonical, err := hccartifact.FromIR(cachedResource)
	if err != nil {
		t.Fatal(err)
	}
	contentDigest := cachedCanonical.Manifest.ContentDigest
	cachedCanonical.Manifest.RegistryID = "src"
	cachedCanonical.Manifest.Revision = contentDigest
	cachedRaw, archiveDigest, err := hccartifact.Encode(cachedCanonical)
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "unexpected network request", http.StatusInternalServerError)
	}))
	defer server.Close()

	cache := NewCache(t.TempDir())
	entry := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp", Source: config.Organization, Resources: []config.MarketplaceResourcePin{
		{RegistryID: "src", Kind: "skill", Name: "cached", Revision: "v1", Digest: contentDigest},
		{RegistryID: "src", Kind: "skill", Name: "missing", Revision: "v1", Digest: "sha256:" + strings.Repeat("a", 64)},
	}}
	if _, err := cache.WriteResourceRevision(server.URL, "src", "skill", "cached", contentDigest, assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "cached"},
		ContentDigest: store.RevisionDigest(contentDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest),
	})); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	if _, err := cache.WriteArtifact(archiveDigest, cachedRaw); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
		t.Fatalf("mark artifact verified: %v", err)
	}

	inspection := (&Resolver{Cache: cache}).Inspect(context.Background(), &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}, nil, resolver.MarketplaceResolveModeCachedNoFetch)
	if requests != 0 {
		t.Fatalf("cached no-fetch inspection made %d HTTP requests, want 0", requests)
	}
	if len(inspection.Pins) != 2 {
		t.Fatalf("inspected %d pins, want 2", len(inspection.Pins))
	}
	states := make(map[string]PinStatus, len(inspection.Pins))
	for _, pin := range inspection.Pins {
		states[pin.Name] = pin
		if pin.EndpointQueried || pin.LatestQueried {
			t.Fatalf("cached no-fetch pin queried endpoint: %#v", pin)
		}
	}
	if pin := states["cached"]; pin.State != PinStateCached || pin.Cache != CacheStateArtifactVerified {
		t.Fatalf("cached pin = %#v, want cached verified state", pin)
	}
	if pin := states["missing"]; pin.State != PinStateMissing {
		t.Fatalf("missing pin = %#v, want missing state", pin)
	}
	_, errs := inspection.Resolve(context.Background(), nil, nil, resolver.MarketplaceResolveModeCachedNoFetch)
	if len(errs) != 1 {
		t.Fatalf("required health errors = %d, want 1: %v", len(errs), errs)
	}
	var statusErr *PinStatusError
	if !errors.As(errs[0], &statusErr) || statusErr.Pin.Name != "missing" {
		t.Fatalf("required health error = %v, want missing PinStatusError", errs[0])
	}
}

func TestInspectionResolveFiltersMetadataAndPromotesRequiredHealth(t *testing.T) {
	materializable := resolver.ResolvedArtifact{
		Entry: catalog.Entry{Kind: ir.KindSkill, Name: "ready", Resource: ir.NewSkill("ready", "", "body", nil, nil, ir.SourceRef{})},
	}
	inspection := Inspection{
		Artifacts: []resolver.ResolvedArtifact{
			{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "metadata"}, Unresolved: true},
			{Entry: catalog.Entry{Kind: ir.KindCollection, Name: "bundle"}, Unresolved: true},
			materializable,
		},
		Pins: []PinStatus{
			{RegistryID: "src", Kind: "skill", Name: "missing", Required: true, State: PinStateMissing},
			{RegistryID: "src", Kind: "skill", Name: "cache", Required: true, Cache: CacheStateError, State: PinStateCurrent, Detail: "decode metadata: malformed"},
			{RegistryID: "src", Kind: "skill", Name: "optional", State: PinStateUnavailable},
		},
	}

	artifacts, errs := inspection.Resolve(context.Background(), nil, nil, resolver.MarketplaceResolveModeInspect)
	if len(artifacts) != 1 || artifacts[0].Name != "ready" {
		t.Fatalf("materializable artifacts = %#v, want only ready", artifacts)
	}
	if len(errs) != 2 {
		t.Fatalf("required health errors = %d, want 2: %v", len(errs), errs)
	}
	for _, err := range errs {
		var statusErr *PinStatusError
		if !errors.As(err, &statusErr) {
			t.Fatalf("error %T = %v, want PinStatusError", err, err)
		}
	}
}

func TestInspectMalformedMetadataRemainsCacheError(t *testing.T) {
	contentDigest := "sha256:" + strings.Repeat("e", 64)
	archiveDigest := "sha256:" + strings.Repeat("f", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			writeTestWellKnown(w, "mp")
			return
		}
		_ = json.NewEncoder(w).Encode(assessedRevision(store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "broken"},
			ContentDigest: store.RevisionDigest(contentDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest),
		}))
	}))
	defer server.Close()

	cache := NewCache(t.TempDir())
	metadataPath := cache.MetadataPath(server.URL, "src", "skill", "broken", contentDigest)
	if err := os.MkdirAll(filepath.Dir(metadataPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metadataPath, []byte("{malformed"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{{
		RegistryID: "src", Kind: "skill", Name: "broken", Revision: "v1", Digest: contentDigest,
	}}}
	inspection := (&Resolver{Cache: cache}).Inspect(context.Background(), &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}, nil, resolver.MarketplaceResolveModeInspect)
	if len(inspection.Pins) != 1 {
		t.Fatalf("pins = %d, want 1", len(inspection.Pins))
	}
	pin := inspection.Pins[0]
	if pin.Cache != CacheStateError || pin.State != PinStateUnavailable || !strings.Contains(pin.Detail, "decode metadata") {
		t.Fatalf("malformed metadata pin = %#v, want unavailable cache error with decode detail", pin)
	}
}

func TestInspectQueriesLatestAndPreservesExactStateOnLatestFailure(t *testing.T) {
	oldDigest := "sha256:" + strings.Repeat("1", 64)
	newDigest := "sha256:" + strings.Repeat("2", 64)
	archiveDigest := "sha256:" + strings.Repeat("3", 64)
	latestFails := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			writeTestWellKnown(w, "mp")
		case "/api/v1/resources/source-slug/skill/review/revisions/" + oldDigest:
			_ = json.NewEncoder(w).Encode(assessedRevision(store.ResourceRevision{Coordinate: store.ResourceCoordinate{Marketplace: "mp", Registry: "source-id", Kind: "skill", Name: "review"}, ContentDigest: store.RevisionDigest(oldDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest)}))
		case "/api/v1/resources/source-slug/skill/review":
			if latestFails {
				http.Error(w, "latest unavailable", http.StatusServiceUnavailable)
				return
			}
			_ = json.NewEncoder(w).Encode(assessedRevision(store.ResourceRevision{Coordinate: store.ResourceCoordinate{Marketplace: "mp", Registry: "source-id", Kind: "skill", Name: "review"}, ContentDigest: store.RevisionDigest(newDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest)}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	entry := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{{RegistryID: "source-id", RegistrySlug: "source-slug", Kind: "skill", Name: "review", Revision: "refs/heads/main", Digest: oldDigest}}}

	inspection := (&Resolver{Cache: NewCache(t.TempDir())}).Inspect(context.Background(), &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}, nil, resolver.MarketplaceResolveModeInspect)
	pin := inspection.Pins[0]
	if !pin.LatestQueried || !pin.UpdateAvailable || pin.State != PinStateUpdateAvailable {
		t.Fatalf("latest update pin = %#v, want checked/update-available", pin)
	}

	latestFails = true
	inspection = (&Resolver{Cache: NewCache(t.TempDir())}).Inspect(context.Background(), &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}, nil, resolver.MarketplaceResolveModeInspect)
	pin = inspection.Pins[0]
	if pin.LatestQueried || pin.UpdateAvailable || pin.State != PinStateCurrent {
		t.Fatalf("latest failure pin = %#v, want exact current state without latest claim", pin)
	}
}

func TestInspectReportsIndexOnlyOccurrenceUpdateWithUnchangedContent(t *testing.T) {
	contentDigest := "sha256:" + strings.Repeat("5", 64)
	manifestDigest := "sha256:" + strings.Repeat("6", 64)
	commit := strings.Repeat("a", 40)
	cases := []struct {
		name             string
		latestOccurrence store.ResourceOccurrence
	}{
		{name: "commit", latestOccurrence: store.ResourceOccurrence{Commit: strings.Repeat("b", 40), ManifestDigest: store.RevisionDigest(manifestDigest)}},
		{name: "manifest", latestOccurrence: store.ResourceOccurrence{Commit: commit, ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("7", 64))}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pin := config.MarketplaceResourcePin{
				RegistryID: "src", RegistrySlug: "source-slug", Kind: "skill", Name: "review",
				Revision: commit, Digest: contentDigest, Acquisition: config.MarketplaceAcquisitionIndexOnly,
				RegistryCommit: commit, RegistryManifestDigest: manifestDigest,
			}
			pinned := assessedRevisionOccurrence(store.ResourceRevision{
				Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
				ContentDigest: store.RevisionDigest(contentDigest),
			}, commit, manifestDigest)
			latest := pinned
			latest.Occurrence.Commit = tc.latestOccurrence.Commit
			latest.Occurrence.ManifestDigest = tc.latestOccurrence.ManifestDigest
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/.well-known/agent-marketplace.json":
					writeTestWellKnown(w, "mp")
				case strings.HasSuffix(r.URL.Path, "/revisions/"+contentDigest):
					_ = json.NewEncoder(w).Encode(pinned)
				case strings.HasSuffix(r.URL.Path, "/occurrences/"+commit):
					_ = json.NewEncoder(w).Encode(mpapi.ExactRegistryOccurrence{
						RegistryID: "src", Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Commit: commit, RegistryRevisionManifestDigest: store.RevisionDigest(manifestDigest),
						Acquisition: store.AcquisitionIndexOnly, AcquisitionOrigin: "https://git.example/repo.git", OccurredAt: time.Now().UTC(),
					})
				case r.URL.Path == "/api/v1/resources/source-slug/skill/review":
					_ = json.NewEncoder(w).Encode(latest)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			entry := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{pin}}
			inspection := (&Resolver{Cache: NewCache(t.TempDir())}).Inspect(context.Background(), &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}, nil, resolver.MarketplaceResolveModeInspect)
			if len(inspection.Pins) != 1 {
				t.Fatalf("pins = %d, want 1", len(inspection.Pins))
			}
			got := inspection.Pins[0]
			if !got.LatestQueried || !got.UpdateAvailable || got.State != PinStateUpdateAvailable {
				t.Fatalf("index-only unchanged-content pin = %#v, want queried/update-available", got)
			}
		})
	}
}

func TestInspectDoesNotReportRevokedLatestAsUpdate(t *testing.T) {
	pinnedDigest := "sha256:" + strings.Repeat("5", 64)
	latestDigest := "sha256:" + strings.Repeat("6", 64)
	archiveDigest := "sha256:" + strings.Repeat("7", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			writeTestWellKnown(w, "mp")
		case "/api/v1/resources/source-slug/skill/review/revisions/" + pinnedDigest:
			_ = json.NewEncoder(w).Encode(assessedRevision(store.ResourceRevision{
				Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "source-id", Kind: "skill", Name: "review"},
				ContentDigest: store.RevisionDigest(pinnedDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest),
			}))
		case "/api/v1/resources/source-slug/skill/review":
			_ = json.NewEncoder(w).Encode(revokedRevision(store.ResourceRevision{
				Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "source-id", Kind: "skill", Name: "review"},
				ContentDigest: store.RevisionDigest(latestDigest),
			}, latestDigest))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	entry := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{{
		RegistryID: "source-id", RegistrySlug: "source-slug", Kind: "skill", Name: "review", Revision: "refs/heads/main", Digest: pinnedDigest,
	}}}
	inspection := (&Resolver{Cache: NewCache(t.TempDir())}).Inspect(context.Background(), &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}, nil, resolver.MarketplaceResolveModeInspect)
	if len(inspection.Pins) != 1 {
		t.Fatalf("pins = %d, want 1", len(inspection.Pins))
	}
	pin := inspection.Pins[0]
	if !pin.LatestQueried || pin.UpdateAvailable || pin.State != PinStateCurrent {
		t.Fatalf("revoked latest pin = %#v, want queried current pin without update", pin)
	}
	if !strings.Contains(pin.Detail, "latest revision is revoked") {
		t.Fatalf("revoked latest detail = %q, want revoked-latest detail", pin.Detail)
	}
}

func TestApplyCachedStateTreatsBroadCachedRevocationAsRevoked(t *testing.T) {
	status := PinStatus{Cache: CacheStateArtifactVerified, MetadataCached: true, ArtifactVerified: true}
	cached := revokedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "source-id", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("a", 64)),
	}, "source-id")
	applyCachedState(&status, cached, true, nil)
	if status.State != PinStateRevoked {
		t.Fatalf("cached broad revocation state = %q, want revoked", status.State)
	}
}

func TestPinStatusErrorQuotesDynamicFields(t *testing.T) {
	statusErr := (&PinStatusError{Pin: PinStatus{
		RegistryID: "source\x00\x1b[31m\n\u202e", Kind: "skill\t", Name: "name\r", State: PinState("state\x07"), Detail: "detail\u2028",
	}}).Error()
	if strings.ContainsAny(statusErr, "\x00\x07\x1b\n\r\t") {
		t.Fatalf("status error contains raw terminal controls: %q", statusErr)
	}
	for _, want := range []string{`\x00`, `\x1b`, `\n`, `\u202e`, `\t`, `\r`, `\a`, `\u2028`} {
		if !strings.Contains(statusErr, want) {
			t.Errorf("status error = %q, want escaped field %q", statusErr, want)
		}
	}
}

func TestInspectRendersCollectionAsVirtualPin(t *testing.T) {
	digest := "sha256:" + strings.Repeat("4", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			writeTestWellKnown(w, "mp")
			return
		}
		_ = json.NewEncoder(w).Encode(assessedRevision(store.ResourceRevision{Coordinate: store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "collection", Name: "bundle"}, ContentDigest: store.RevisionDigest(digest)}))
	}))
	defer server.Close()
	entry := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{{RegistryID: "src", Kind: "collection", Name: "bundle", Revision: "v1", Digest: digest}}}
	inspection := (&Resolver{Cache: NewCache(t.TempDir())}).Inspect(context.Background(), &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}, nil, resolver.MarketplaceResolveModeInspect)
	if len(inspection.Pins) != 1 || inspection.Pins[0].Kind != "collection" || inspection.Pins[0].State != PinStateCurrent {
		t.Fatalf("collection pins = %#v, want one current virtual collection row", inspection.Pins)
	}
	if len(inspection.Artifacts) != 1 || inspection.Artifacts[0].Kind != ir.KindCollection {
		t.Fatalf("collection artifacts = %#v, want one filtered virtual artifact", inspection.Artifacts)
	}
	resolved, errs := inspection.Resolve(context.Background(), nil, nil, resolver.MarketplaceResolveModeInspect)
	if len(resolved) != 0 || len(errs) != 0 {
		t.Fatalf("collection resolver output = %v, %v; want no materialization and no observational error", resolved, errs)
	}
}

func TestInspectionResolvePropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	inspection := Inspection{Pins: []PinStatus{{Name: "cancelled", State: PinStateUnavailable}}}
	_, errs := inspection.Resolve(ctx, nil, nil, resolver.MarketplaceResolveModeInspect)
	if len(errs) != 1 || !errors.Is(errs[0], context.Canceled) {
		t.Fatalf("cancellation errors = %v, want context.Canceled", errs)
	}
}

func TestInspectCachedIndexOnlyRequiresOccurrenceScopedStaging(t *testing.T) {
	resource := ir.NewSkill("review", "Review", "body", nil, nil, ir.SourceRef{})
	canonical, err := hccartifact.FromIR(resource)
	if err != nil {
		t.Fatal(err)
	}
	contentDigest := canonical.Manifest.ContentDigest
	canonical.Manifest.RegistryID = "src"
	canonical.Manifest.Revision = contentDigest
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatal(err)
	}
	commit := strings.Repeat("a", 40)
	manifestDigest := "sha256:" + strings.Repeat("b", 64)
	pin := config.MarketplaceResourcePin{
		RegistryID: "src", Kind: "skill", Name: "review", Revision: contentDigest, Digest: contentDigest,
		Acquisition: config.MarketplaceAcquisitionIndexOnly, RegistryCommit: commit, RegistryManifestDigest: manifestDigest,
	}
	identity := IndexOnlyIdentity{Marketplace: "mp", RegistryID: "src", Commit: commit, ManifestDigest: manifestDigest}

	for _, tc := range []struct {
		name      string
		wantError bool
		stage     func(t *testing.T, cache *Cache, path string)
	}{
		{name: "valid", stage: func(t *testing.T, _ *Cache, path string) {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte("cached"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "missing", wantError: true},
		{name: "corrupt", wantError: true, stage: func(t *testing.T, _ *Cache, path string) {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("not a directory"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "symlink", wantError: true, stage: func(t *testing.T, _ *Cache, path string) {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(t.TempDir(), path); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := NewCache(t.TempDir())
			if _, err := cache.WriteIndexOnlyResourceRevision(identity, pin.RegistryID, pin.Kind, pin.Name, pin.Digest, assessedRevisionOccurrence(store.ResourceRevision{
				Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
				ContentDigest: store.RevisionDigest(contentDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest),
			}, commit, manifestDigest)); err != nil {
				t.Fatal(err)
			}
			if _, err := cache.WriteIndexOnlyArtifact(identity, pin.Digest, raw); err != nil {
				t.Fatal(err)
			}
			if err := cache.MarkIndexOnlyArtifactVerified(identity, pin.Digest); err != nil {
				t.Fatal(err)
			}
			stagedPath := cachedReconstructedResourcePath(cache, identity, pin.Kind, pin.Name)
			if tc.stage != nil {
				tc.stage(t, cache, stagedPath)
			}

			inspection := (&Resolver{Cache: cache}).Inspect(context.Background(), &config.Config{Marketplaces: []config.MarketplaceEntry{{
				URL: "https://market.example", InstanceID: "mp", Resources: []config.MarketplaceResourcePin{pin},
			}}}, nil, resolver.MarketplaceResolveModeCachedNoFetch)
			if len(inspection.Pins) != 1 {
				t.Fatalf("pins = %d, want 1", len(inspection.Pins))
			}
			got := inspection.Pins[0]
			if !tc.wantError {
				if got.Cache != CacheStateArtifactVerified || got.State != PinStateCached || !got.ArtifactVerified {
					t.Fatalf("valid staging pin = %#v, want cached/verified", got)
				}
				return
			}
			if got.Cache != CacheStateError || got.State != PinStateUnavailable {
				t.Fatalf("%s staging pin = %#v, want cache error/unavailable", tc.name, got)
			}
			if !strings.Contains(got.Detail, "occurrence-scoped reconstructed source") {
				t.Fatalf("%s staging detail = %q, want occurrence-scoped validation detail", tc.name, got.Detail)
			}
			marker := filepath.Join(filepath.Dir(cache.IndexOnlyArtifactPath(identity, pin.Digest)), "verified.marker")
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("%s occurrence marker = %v, want preserved by read-only inspection", tc.name, err)
			}
		})
	}
}

// TestInspectBlocksExpandedCollectionLeafForRevokedGraph covers the inspect
// seam behind `agent-marketplace status`: an expanded-collection leaf must not
// resolve to a materializable artifact while its collection graph is blocked,
// matching the revocation propagation sync --check applies.
func TestInspectBlocksExpandedCollectionLeafForRevokedGraph(t *testing.T) {
	nestedDigest := "sha256:" + strings.Repeat("b", 64)
	leafResource := ir.NewSkill("review", "Review", "body", nil, nil, ir.SourceRef{})
	leafCanonical, err := hccartifact.FromIR(leafResource)
	if err != nil {
		t.Fatal(err)
	}
	leafDigest := leafCanonical.Manifest.ContentDigest
	leafCanonical.Manifest.RegistryID = "leaf-source"
	leafCanonical.Manifest.Revision = leafDigest
	leafRaw, leafArchiveDigest, err := hccartifact.Encode(leafCanonical)
	if err != nil {
		t.Fatal(err)
	}
	rootPayload, rootDigest, err := hccartifact.EncodeCollectionManifest(hccartifact.CollectionManifest{
		SchemaVersion:    hccartifact.CollectionSchemaV1,
		Name:             "bundle",
		Members:          []hccartifact.CollectionMemberEntry{{Kind: "collection", Name: "inner", RegistryID: "nested-source", Revision: nestedDigest}},
		Leaves:           []hccartifact.CollectionMemberEntry{{Kind: "skill", Name: "review", RegistryID: "leaf-source", Revision: leafDigest, InclusionPaths: [][]string{{"bundle", "inner"}}}},
		SupportedSchemas: []string{hccartifact.CollectionSchemaV1},
	})
	if err != nil {
		t.Fatal(err)
	}
	leafRevision := assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "leaf-source", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(leafDigest), ArchiveDigest: store.ArchiveDigest(leafArchiveDigest),
	})
	rootRevision := assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "root-source", Kind: "collection", Name: "bundle"},
		ContentDigest: store.RevisionDigest(rootDigest),
	})
	nestedRevision := revokedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "nested-source", Kind: "collection", Name: "inner"},
		ContentDigest: store.RevisionDigest(nestedDigest),
	}, "mp/nested-source/collection/inner@"+nestedDigest)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			writeTestWellKnown(w, "mp")
		case "/api/v1/resources/root-source/collection/bundle/revisions/" + rootDigest:
			_ = json.NewEncoder(w).Encode(rootRevision)
		case "/api/v1/resources/leaf-source/skill/review/revisions/" + leafDigest:
			_ = json.NewEncoder(w).Encode(leafRevision)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cache := NewCache(t.TempDir())
	if _, err := cache.WriteResourceRevision(server.URL, "root-source", "collection", "bundle", rootDigest, rootRevision); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.WriteResourceRevision(server.URL, "nested-source", "collection", "inner", nestedDigest, nestedRevision); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.WriteResourceRevision(server.URL, "leaf-source", "skill", "review", leafDigest, leafRevision); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.WriteCollectionManifest(rootDigest, rootPayload); err != nil {
		t.Fatal(err)
	}
	if err := cache.MarkCollectionVerified(rootDigest); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.WriteArtifact(leafArchiveDigest, leafRaw); err != nil {
		t.Fatal(err)
	}
	if err := cache.MarkArtifactVerified(leafArchiveDigest); err != nil {
		t.Fatal(err)
	}

	entry := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{
		{RegistryID: "root-source", Kind: "collection", Name: "bundle", Revision: rootDigest, Digest: rootDigest},
		{RegistryID: "leaf-source", Kind: "skill", Name: "review", Revision: leafDigest, Digest: leafDigest,
			SelectedBy: config.MarketplaceSelectionsFrom([]config.MarketplaceSelection{{RegistryID: "root-source", Name: "bundle", Revision: rootDigest}})},
	}}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}

	inspection := (&Resolver{HTTPClient: server.Client(), Cache: cache}).Inspect(context.Background(), cfg, nil, resolver.MarketplaceResolveModeInspect)
	var leaf PinStatus
	found := false
	for _, pin := range inspection.Pins {
		if pin.Kind == "skill" && pin.Name == "review" {
			leaf = pin
			found = true
		}
	}
	if !found || leaf.Detail == "" {
		t.Fatalf("blocked leaf pin = %#v, want a degraded detail", leaf)
	}
	resolved, errs := inspection.Resolve(context.Background(), cfg, nil, resolver.MarketplaceResolveModeInspect)
	for _, artifact := range resolved {
		if artifact.Kind == ir.KindSkill && artifact.Name == "review" {
			t.Fatalf("blocked collection leaf materialized an artifact: %#v", artifact)
		}
	}
	if len(errs) == 0 {
		t.Fatal("inspection resolver reported no error for a blocked collection graph")
	}
}

// TestInspectPreservesMirrorVerificationMarker covers the no-write inspect
// contract: a cached artifact that fails verification during cache-only
// resolution must degrade the pin and surface an error, but must not remove
// the verification marker — only a mutating sync may do that.
func TestInspectPreservesMirrorVerificationMarker(t *testing.T) {
	// The cached bytes decode and self-verify, but their manifest identity does
	// not match the pin, so resolution must reject them. A mutating sync would
	// drop the marker; read-only inspection must leave it untouched while still
	// failing closed.
	canonical, err := hccartifact.FromIR(ir.NewSkill("other", "Other", "body", nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatal(err)
	}
	contentDigest := canonical.Manifest.ContentDigest
	canonical.Manifest.RegistryID = "src"
	canonical.Manifest.Revision = contentDigest
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatal(err)
	}
	revision := assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "cached"},
		ContentDigest: store.RevisionDigest(contentDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest),
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			writeTestWellKnown(w, "mp")
			return
		}
		_ = json.NewEncoder(w).Encode(revision)
	}))
	defer server.Close()

	cache := NewCache(t.TempDir())
	entry := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{{
		RegistryID: "src", Kind: "skill", Name: "cached", Revision: "v1", Digest: contentDigest,
	}}}
	if _, err := cache.WriteResourceRevision(entry.URL, "src", "skill", "cached", contentDigest, revision); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.WriteArtifact(archiveDigest, raw); err != nil {
		t.Fatal(err)
	}
	if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(filepath.Dir(cache.ArtifactPath(archiveDigest)), "verified.marker")

	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}
	inspection := (&Resolver{Cache: cache}).Inspect(context.Background(), cfg, nil, resolver.MarketplaceResolveModeInspect)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("verified marker = %v, want preserved by read-only inspection", err)
	}
	resolved, errs := inspection.Resolve(context.Background(), cfg, nil, resolver.MarketplaceResolveModeCachedNoFetch)
	if len(resolved) != 0 {
		t.Fatalf("corrupt artifact resolved = %#v, want fail-closed filtering", resolved)
	}
	if len(errs) != 1 {
		t.Fatalf("corrupt artifact resolver errors = %v, want one drift-suppressing error", errs)
	}
}

// TestInspectCacheWipeWithManagedOutputYieldsDriftIncomplete pins the
// disposable-cache contract: a full cache wipe removes every acquisition fact
// while managed output persists on disk. The healthy endpoint still classifies
// the pin current, but the failed materialization proof must leave drift
// incomplete — suppressing the status drift dry-run that would report the
// installed output as orphaned — without a hard status error, because the
// wiped cache cannot prove the pin was ever acquired.
func TestInspectCacheWipeWithManagedOutputYieldsDriftIncomplete(t *testing.T) {
	contentDigest := "sha256:" + strings.Repeat("a", 64)
	archiveDigest := "sha256:" + strings.Repeat("b", 64)
	revision := assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(contentDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest),
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			writeTestWellKnown(w, "mp")
			return
		}
		_ = json.NewEncoder(w).Encode(revision)
	}))
	defer server.Close()

	// The cache was fully wiped after the pin's managed output was installed:
	// no metadata, no artifact, no verification marker remain.
	cache := NewCache(t.TempDir())
	entry := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{{
		RegistryID: "src", Kind: "skill", Name: "review", Revision: "v1", Digest: contentDigest,
	}}}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}
	inspection := (&Resolver{Cache: cache}).Inspect(context.Background(), cfg, nil, resolver.MarketplaceResolveModeInspect)
	if len(inspection.Pins) != 1 {
		t.Fatalf("pins = %#v, want one pin", inspection.Pins)
	}
	pin := inspection.Pins[0]
	if pin.State != PinStateCurrent || pin.MetadataCached || pin.ArtifactPresent {
		t.Fatalf("wiped-cache pin = %#v, want current endpoint state without cached facts", pin)
	}
	resolved, errs := inspection.Resolve(context.Background(), cfg, nil, resolver.MarketplaceResolveModeInspect)
	if len(resolved) != 0 {
		t.Fatalf("wiped-cache pin resolved = %#v, want no materializable artifacts", resolved)
	}
	if len(errs) != 0 {
		t.Fatalf("wiped-cache pin resolver errors = %v, want soft drift-incomplete suppression, not a hard error", errs)
	}
	if len(inspection.DriftIncomplete) != 1 {
		t.Fatalf("wiped-cache pin drift-incomplete = %v, want one suppressed materialization proof", inspection.DriftIncomplete)
	}
}

// TestInspectPartialCacheLossYieldsHardMissingDiagnostic pins the partial
// cache-loss contract: cached metadata proves the pin was acquired, but its
// artifact is gone, so the inspection classifies the pin missing. That failed
// materialization proof must become a hard resolve error — status can never
// let the drift dry-run read the pin's managed output as orphaned — while a
// never-acquired missing pin beside it stays drift-incomplete.
func TestInspectPartialCacheLossYieldsHardMissingDiagnostic(t *testing.T) {
	contentDigest := "sha256:" + strings.Repeat("c", 64)
	archiveDigest := "sha256:" + strings.Repeat("d", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			writeTestWellKnown(w, "mp")
			return
		}
		_ = json.NewEncoder(w).Encode(assessedRevision(store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
			ContentDigest: store.RevisionDigest(contentDigest),
			ArchiveDigest: store.ArchiveDigest(archiveDigest),
		}))
	}))
	defer server.Close()

	cache := NewCache(t.TempDir())
	entry := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{
		{RegistryID: "src", Kind: "skill", Name: "review", Revision: "v1", Digest: contentDigest},
		{RegistryID: "src", Kind: "skill", Name: "never-acquired", Revision: "v1", Digest: "sha256:" + strings.Repeat("e", 64)},
	}}
	// Metadata survives, the artifact does not: partial cache loss.
	if _, err := cache.WriteResourceRevision(entry.URL, "src", "skill", "review", contentDigest, assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(contentDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest),
	})); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	inspection := (&Resolver{Cache: cache}).Inspect(context.Background(), &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}, nil, resolver.MarketplaceResolveModeCached)
	if len(inspection.Pins) != 2 {
		t.Fatalf("inspected %d pins, want 2", len(inspection.Pins))
	}
	states := make(map[string]PinStatus, len(inspection.Pins))
	for _, pin := range inspection.Pins {
		states[pin.Name] = pin
	}
	if pin := states["review"]; pin.State != PinStateMissing || !pin.MetadataCached {
		t.Fatalf("partial-loss pin = %#v, want missing state with cached metadata", pin)
	}
	if pin := states["never-acquired"]; pin.State != PinStateMissing || pin.MetadataCached {
		t.Fatalf("never-acquired pin = %#v, want missing state without cached facts", pin)
	}
	if len(inspection.ResolveErrors) != 1 {
		t.Fatalf("partial-loss pin resolve errors = %v, want one hard diagnostic", inspection.ResolveErrors)
	}
	if len(inspection.DriftIncomplete) != 1 || inspection.DriftIncomplete[0].Name != "never-acquired" {
		t.Fatalf("drift-incomplete = %v, want only the never-acquired pin", inspection.DriftIncomplete)
	}
}

// TestInspectUnavailableNonRequiredPinWithCacheYieldsResolveError covers the
// unavailable-endpoint path of the same contract: cached facts prove the pin
// was acquired and managed output may exist, so even a non-required pin must
// surface a resolve error that suppresses an unsafe orphaned-output drift
// report while retaining its unavailable status detail.
func TestInspectUnavailableNonRequiredPinWithCacheYieldsResolveError(t *testing.T) {
	contentDigest := "sha256:" + strings.Repeat("c", 64)
	archiveDigest := "sha256:" + strings.Repeat("d", 64)
	revision := assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(contentDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest),
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "endpoint unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cache := NewCache(t.TempDir())
	entry := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{{
		RegistryID: "src", Kind: "skill", Name: "review", Revision: "v1", Digest: contentDigest,
	}}}
	if _, err := cache.WriteResourceRevision(entry.URL, "src", "skill", "review", contentDigest, revision); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.WriteArtifact(archiveDigest, []byte("artifact")); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}
	inspection := (&Resolver{Cache: cache}).Inspect(context.Background(), cfg, nil, resolver.MarketplaceResolveModeInspect)
	if len(inspection.Pins) != 1 {
		t.Fatalf("pins = %#v, want one pin", inspection.Pins)
	}
	pin := inspection.Pins[0]
	if pin.State != PinStateUnavailable || pin.Required || !pin.MetadataCached || !pin.ArtifactPresent || pin.Detail == "" {
		t.Fatalf("unavailable pin = %#v, want non-required unavailable state with cached acquisition facts", pin)
	}
	resolved, errs := inspection.Resolve(context.Background(), cfg, nil, resolver.MarketplaceResolveModeInspect)
	if len(resolved) != 0 {
		t.Fatalf("unavailable pin resolved = %#v, want no materializable artifacts", resolved)
	}
	if len(errs) != 1 {
		t.Fatalf("unavailable pin resolver errors = %v, want one drift-suppressing error", errs)
	}
}
