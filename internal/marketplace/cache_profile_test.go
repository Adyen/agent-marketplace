package marketplace

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestPrepareTargetProfileLockSupportsOfflineRead(t *testing.T) {
	profileBytes := []byte(placeAcquireProfileYAML)
	identity := placeAcquireProfileIdentity(profileBytes)
	identity.SourceID = targetProfileSourceID
	if err := identity.Validate(); err != nil {
		t.Fatalf("validate profile identity: %v", err)
	}
	coordinate := store.ResourceCoordinate{
		Marketplace: "mp", Registry: "src", Kind: "target-profile", Name: identity.TargetID,
	}
	base, err := buildTargetProfileAcquisitionResponse(t, coordinate, identity, profileBytes)
	if err != nil {
		t.Fatalf("build profile response: %v", err)
	}

	var requests atomic.Int32
	revisionPath := "/api/v1/resources/src/target-profile/" + identity.TargetID + "/revisions/" + identity.RevisionDigest
	manifestPath := revisionPath + "/inspection"
	contentPath := manifestPath + "/content"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if strings.HasPrefix(r.URL.Path, revisionPath) {
			if values := r.Header.Values("X-Agent-Marketplace-Features"); len(values) != 1 || values[0] != mpapi.TargetProfilePlaceFeature {
				t.Errorf("target-profile feature headers = %q, want exactly %q", values, mpapi.TargetProfilePlaceFeature)
			}
		}
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			writeTestWellKnown(w, "mp")
		case "/api/v1/registries/tools":
			writeTestRegistry(w, store.Registry{
				ID: "src", Slug: "tools", Origins: []string{"https://git.example.com/tools"},
				Ref: "main", Roots: []store.RegistryRoot{{Path: "profiles"}},
				Acquisition: store.AcquisitionMirror, Listed: true,
			})
		case revisionPath:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(struct {
				store.AssessedResourceRevision
				CurrentAvailability struct {
					Availability string `json:"availability"`
				} `json:"current_availability"`
			}{
				AssessedResourceRevision: base.revision,
				CurrentAvailability: struct {
					Availability string `json:"availability"`
				}{Availability: base.availability},
			})
		case manifestPath:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(base.manifest)
		case contentPath:
			w.Header().Set("Content-Type", inspection.MediaYAML)
			_, _ = w.Write(base.body)
		default:
			t.Errorf("unexpected Marketplace request path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cache := NewCache(t.TempDir())
	resolver := &Resolver{Cache: cache, HTTPClient: server.Client()}
	entry := config.MarketplaceEntry{URL: server.URL, Source: config.UserPersonal}
	locator := MarketplaceLocator{
		Registry: "tools", Kind: "target-profile", Name: identity.TargetID, Revision: identity.RevisionDigest,
	}
	if _, _, err := resolver.PrepareTargetProfileLock(
		context.Background(), entry,
		MarketplaceLocator{Registry: locator.Registry, Kind: locator.Kind, Name: locator.Name},
		nil,
	); err == nil {
		t.Fatal("unpinned target-profile locator was accepted")
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("unpinned target-profile locator made %d HTTP requests", got)
	}

	enriched, locked, err := resolver.PrepareTargetProfileLock(context.Background(), entry, locator, NewPreparationBudget(1))
	if err != nil {
		t.Fatalf("PrepareTargetProfileLock: %v", err)
	}
	if enriched.InstanceID != "mp" {
		t.Fatalf("enriched Marketplace instance ID = %q, want mp", enriched.InstanceID)
	}
	wantLock := lockfile.MarketplaceResourceLock{
		RegistryName: locator.Registry, RegistryID: "src", Kind: locator.Kind, Name: locator.Name,
		Revision: locator.Revision, ContentDigest: lockfile.Digest(identity.RevisionDigest),
		ProfileIdentity: &identity, Acquisition: lockfile.MarketplaceAcquisitionMirror,
	}
	if !reflect.DeepEqual(locked, wantLock) {
		t.Fatalf("target-profile lock = %+v, want %+v", locked, wantLock)
	}
	if got := requests.Load(); got != 5 {
		t.Fatalf("online acquisition made %d requests, want well-known, registry, revision, inspection, and content", got)
	}

	server.Close()
	got, err := cache.ReadLockedTargetProfile(enriched.URL, enriched.InstanceID, locked)
	if err != nil {
		t.Fatalf("offline ReadLockedTargetProfile: %v", err)
	}
	if !bytes.Equal(got, profileBytes) {
		t.Fatalf("offline profile bytes differ: got %q, want %q", got, profileBytes)
	}

	otherSource := identity
	otherSource.SourceID = "git:https%3A%2F%2Fgit.example.com%2Fother@main#profiles%2Finvented-app.yaml"
	if err := otherSource.Validate(); err != nil {
		t.Fatalf("validate alternate source identity: %v", err)
	}
	otherLock := locked
	otherLock.ProfileIdentity = &otherSource
	if _, err := cache.ReadLockedTargetProfile(enriched.URL, enriched.InstanceID, otherLock); err == nil ||
		!strings.Contains(err.Error(), "marketplace.cache.profile-missing") {
		t.Fatalf("same-digest alternate source read = %v, want exact cache miss", err)
	}

	cachePath := cache.targetProfileCachePath(enriched.URL, enriched.InstanceID, locked.RegistryID, identity)
	changed := bytes.Replace(profileBytes, []byte("Invented App"), []byte("Invented Bob"), 1)
	if bytes.Equal(changed, profileBytes) || len(changed) != len(profileBytes) {
		t.Fatal("corruption fixture must change bytes without changing length")
	}
	if err := os.WriteFile(cachePath, changed, 0o644); err != nil {
		t.Fatalf("overwrite cached fixture: %v", err)
	}
	if _, err := cache.ReadLockedTargetProfile(enriched.URL, enriched.InstanceID, locked); err == nil ||
		!strings.Contains(strings.ToLower(err.Error()), "digest") {
		t.Fatalf("same-length changed cached YAML read = %v, want digest refusal", err)
	}
}
