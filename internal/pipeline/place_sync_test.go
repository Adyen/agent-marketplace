package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestSyncPlaceUpsertsOnlineThenOffline(t *testing.T) {
	fixture := newPlaceAdmissionFixture(t)
	setPlacePublishEnvironment(t, fixture)
	distinctRoot := filepath.Join(fixture.env.Home, ".config", "distinct-app")
	if err := os.MkdirAll(distinctRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		left, right string
		want        bool
	}{
		{left: fixture.root, right: fixture.root, want: true},
		{left: fixture.env.Home, right: fixture.root, want: true},
		{left: fixture.root, right: distinctRoot, want: false},
	} {
		overlap, err := placeSyncRootsOverlap(test.left, test.right)
		if err != nil || overlap != test.want {
			t.Fatalf("placeSyncRootsOverlap(%q, %q) = %t, %v; want %t", test.left, test.right, overlap, err, test.want)
		}
	}
	pkg := resolvedPlacePackageForPublish(t, fixture)
	artifact, err := hccartifact.NewPlaceArtifact(pkg.Pin.Kind, pkg.Pin.Name, fixture.identity, pkg.Files)
	if err != nil {
		t.Fatal(err)
	}
	_, placeholder, archiveDigest, err := hccartifact.EncodeArchives(artifact)
	if err != nil {
		t.Fatal(err)
	}
	pkg.Pin.LockArchiveDigest = string(archiveDigest)
	pkg.Revision.ArchiveDigest = store.ArchiveDigest(archiveDigest)
	pkg.Revision.Occurrence = store.ResourceOccurrence{
		Coordinate: pkg.Revision.Coordinate, ContentDigest: pkg.Revision.ContentDigest,
		Commit: strings.Repeat("1", 40), ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("f", 64)),
	}

	var wellKnownRequests, revisionRequests, archiveRequests atomic.Int32
	var peer *httptest.Server
	revisionPath := "/api/v1/resources/" + url.PathEscape(string(pkg.Pin.RegistryID)) + "/" + url.PathEscape(pkg.Pin.Kind) + "/" + url.PathEscape(pkg.Pin.Name) +
		"/revisions/" + pkg.Pin.Revision
	archivePath := "/api/v1/archives/sha256/" + strings.TrimPrefix(string(archiveDigest), "sha256:")
	peer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.EscapedPath() {
		case "/.well-known/agent-marketplace.json":
			wellKnownRequests.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("market-id", peer.URL))
		case revisionPath:
			revisionRequests.Add(1)
			values := req.Header.Values("X-Agent-Marketplace-Features")
			if len(values) != 1 || values[0] != mpapi.TargetProfilePlaceFeature {
				t.Errorf("resource feature header = %q", values)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(struct {
				store.AssessedResourceRevision
				CurrentAvailability struct {
					Availability string `json:"availability"`
				} `json:"current_availability"`
				Requires []store.ResourceRequirement `json:"requires"`
			}{
				AssessedResourceRevision: pkg.Revision,
				CurrentAvailability: struct {
					Availability string `json:"availability"`
				}{Availability: "available"},
				Requires: []store.ResourceRequirement{},
			})
		case archivePath:
			archiveRequests.Add(1)
			_, _ = w.Write(placeholder)
		default:
			t.Errorf("unexpected request path %q", req.URL.EscapedPath())
			http.NotFound(w, req)
		}
	}))
	defer peer.Close()

	entry := fixture.config.Marketplaces[0]
	entry.URL = peer.URL
	entry.Resources = append([]config.MarketplaceResourcePin(nil), entry.Resources...)
	var packagePinSet bool
	for i := range entry.Resources {
		if entry.Resources[i].Kind == pkg.Pin.Kind && entry.Resources[i].Name == pkg.Pin.Name {
			entry.Resources[i] = pkg.Pin
			packagePinSet = true
		}
	}
	if !packagePinSet {
		t.Fatal("fixture package pin was not present in config")
	}
	fixture.config.Marketplaces = []config.MarketplaceEntry{entry}
	fixture.resolver.HTTPClient = peer.Client()
	cacheRoot := t.TempDir()
	fixture.resolver.Cache = marketplace.NewCache(cacheRoot)

	reader := func(_ context.Context, selected targetprofile.Identity, mode resolver.MarketplaceResolveMode) ([]byte, error) {
		if selected != fixture.identity ||
			(mode != resolver.MarketplaceResolveModeRefresh && mode != resolver.MarketplaceResolveModeCachedNoFetch) {
			t.Fatalf("profile reader identity=%#v mode=%v", selected, mode)
		}
		return append([]byte(nil), fixture.raw...), nil
	}
	ctx := context.Background()
	pending, err := SyncPlaceUpserts(ctx, fixture.config, []string{fixture.identity.TargetID}, fixture.env, reader, fixture.resolver, resolver.MarketplaceResolveModeRefresh)
	if err != nil || len(pending.Pending) != 1 || pending.Pending[0] != fixture.identity || len(pending.Committed) != 0 {
		t.Fatalf("SyncPlaceUpserts without receipt = %#v, %v", pending, err)
	}
	if wellKnownRequests.Load() != 0 || revisionRequests.Load() != 0 || archiveRequests.Load() != 0 {
		t.Fatalf("pending admission issued requests: well-known=%d revision=%d archive=%d",
			wellKnownRequests.Load(), revisionRequests.Load(), archiveRequests.Load())
	}
	if entries, err := os.ReadDir(fixture.root); err != nil || len(entries) != 0 {
		t.Fatalf("pending admission changed target root: %v, %v", entries, err)
	}
	assertPlacePublishPathAbsent(t, filepath.Join(fixture.state, "packages"))
	assertPlacePublishPathAbsent(t, filepath.Join(fixture.state, "ownership"))
	assertPlacePublishPathAbsent(t, filepath.Join(fixture.state, "dependencies"))
	if entries, err := os.ReadDir(cacheRoot); err != nil || len(entries) != 0 {
		t.Fatalf("pending admission changed cache: %v, %v", entries, err)
	}

	if err := targetprofile.RecordApproval(fixture.binding); err != nil {
		t.Fatal(err)
	}
	online, err := SyncPlaceUpserts(ctx, fixture.config, []string{fixture.identity.TargetID}, fixture.env, reader, fixture.resolver, resolver.MarketplaceResolveModeRefresh)
	if err != nil || len(online.Pending) != 0 || len(online.Committed) != 1 || len(online.Committed[fixture.identity.TargetID].Changed) == 0 {
		t.Fatalf("online SyncPlaceUpserts = %#v, %v", online, err)
	}
	if wellKnownRequests.Load() != 1 || revisionRequests.Load() != 1 || archiveRequests.Load() != 1 {
		t.Fatalf("online requests: well-known=%d revision=%d archive=%d, want 1 each",
			wellKnownRequests.Load(), revisionRequests.Load(), archiveRequests.Load())
	}
	targetPath := filepath.Join(fixture.root, pkg.Files[0].DestinationPath)
	assertPlacePublishFile(t, targetPath, pkg.Files[0].Bytes, pkg.Files[0].Mode)
	ownerStore, snapshot := openPlacePublishOwnership(t, fixture)
	statePath := ownerStore.StateFile()
	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	targetInfo, err := os.Stat(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	stateInfo, err := os.Stat(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ownerStore.PlaceFromSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}

	peer.Close()
	offline, err := SyncPlaceUpserts(ctx, fixture.config, []string{fixture.identity.TargetID}, fixture.env, reader, fixture.resolver, resolver.MarketplaceResolveModeCachedNoFetch)
	if err != nil || len(offline.Pending) != 0 || len(offline.Committed) != 1 || len(offline.Committed[fixture.identity.TargetID].Changed) != 0 {
		t.Fatalf("offline SyncPlaceUpserts = %#v, %v", offline, err)
	}
	targetAfter, err := os.Stat(targetPath)
	stateAfter, readErr := os.ReadFile(statePath)
	stateAfterInfo, statErr := os.Stat(statePath)
	if err != nil || readErr != nil || statErr != nil ||
		!targetInfo.ModTime().Equal(targetAfter.ModTime()) ||
		!stateInfo.ModTime().Equal(stateAfterInfo.ModTime()) ||
		!bytes.Equal(stateBefore, stateAfter) {
		t.Fatalf("offline admission changed output/state: target=%v state=%v read=%v stat=%v", targetAfter, stateAfterInfo, readErr, statErr)
	}
	if wellKnownRequests.Load() != 1 || revisionRequests.Load() != 1 || archiveRequests.Load() != 1 {
		t.Fatalf("offline mode issued HTTP: well-known=%d revision=%d archive=%d",
			wellKnownRequests.Load(), revisionRequests.Load(), archiveRequests.Load())
	}
}
