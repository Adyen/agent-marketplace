package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func TestCatalogProjectionGateAndCapabilities(t *testing.T) {
	meta := sqlite.New(":memory:")
	if err := meta.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = meta.Close() })

	disabled := NewServer(config.Config{Instance: config.Instance{ID: "mp"}}, meta, fsblob.New(t.TempDir())).PublicHandler()
	response := httptest.NewRecorder()
	disabled.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/catalog", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("disabled catalog status = %d, want 404", response.Code)
	}
	for _, route := range []string{"/api/v1/capabilities", "/.well-known/agent-marketplace.json"} {
		response = httptest.NewRecorder()
		disabled.ServeHTTP(response, httptest.NewRequest(http.MethodGet, route, nil))
		var caps Capabilities
		if err := json.Unmarshal(response.Body.Bytes(), &caps); err != nil {
			t.Fatalf("decode disabled %s: %v", route, err)
		}
		if slices.Contains(caps.Features, CatalogProjectionV1Alpha1Feature) {
			t.Fatalf("disabled %s advertised catalog feature: %v", route, caps.Features)
		}
	}

	enabled := NewServer(config.Config{Instance: config.Instance{ID: "mp"}, CatalogProjection: config.CatalogProjection{Enabled: true}}, meta, fsblob.New(t.TempDir())).PublicHandler()
	for _, route := range []string{"/api/v1/capabilities", "/.well-known/agent-marketplace.json"} {
		response = httptest.NewRecorder()
		enabled.ServeHTTP(response, httptest.NewRequest(http.MethodGet, route, nil))
		var caps Capabilities
		if err := json.Unmarshal(response.Body.Bytes(), &caps); err != nil {
			t.Fatalf("decode enabled %s: %v", route, err)
		}
		if !slices.Contains(caps.Features, CatalogProjectionV1Alpha1Feature) {
			t.Fatalf("enabled %s did not advertise catalog feature: %v", route, caps.Features)
		}
	}
}

// TestCatalogETagIsOrderIndependent pins the ETag to the revocation
// declaration set rather than its slice order. A source backed by a map could
// otherwise hand back a different order per instance, so replicas of the same
// deployment would derive different ETags for identical state and never agree
// on 304 responses.
func TestCatalogETagIsOrderIndependent(t *testing.T) {
	declarations := []store.Revocation{
		{Kind: store.RevocationRegistry, TargetID: "src"},
		{Kind: store.RevocationCoordinate, TargetID: "mp/src/command/command"},
		{Kind: store.RevocationRevision, TargetID: "mp/src/skill/skill@sha256:" + strings.Repeat("b", 64)},
	}
	baseline := catalogETag(1, declarations)
	if baseline == "" || !strings.HasPrefix(string(baseline), `"catalog-`) || !strings.HasSuffix(string(baseline), `"`) {
		t.Fatalf("baseline ETag = %q, want strong quoted validator", baseline)
	}
	for i := range declarations {
		rotated := append(append([]store.Revocation(nil), declarations[i:]...), declarations[:i]...)
		if got := catalogETag(1, rotated); got != baseline {
			t.Fatalf("rotation %d ETag = %s, want %s (order must not affect the hash)", i, got, baseline)
		}
	}
	populated := append(append([]store.Revocation(nil), declarations...), store.Revocation{Kind: store.RevocationRegistry, TargetID: "other"})
	if got := catalogETag(1, populated); got == baseline {
		t.Fatal("different declaration set produced identical ETag")
	}
	if got := catalogETag(2, declarations); got == baseline {
		t.Fatal("different catalog version produced identical ETag")
	}
}

func TestCatalogProjectionHTTPContract(t *testing.T) {
	ctx := context.Background()
	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	seedCatalog(t, ctx, meta, []string{KindSkill, KindCommand, KindSubagent, KindHook, KindMcpServer, KindCollection})
	handler := NewServer(config.Config{
		Instance:          config.Instance{ID: "mp"},
		CatalogProjection: config.CatalogProjection{Enabled: true},
	}, meta, fsblob.New(t.TempDir())).PublicHandler()

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/v1/catalog?limit=2", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first page = %d: %s", first.Code, first.Body.String())
	}
	assertCatalogHeaders(t, first)
	var page CatalogPage
	if err := json.Unmarshal(first.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.APIVersion != CatalogAPIVersionV1Alpha1 || page.MarketplaceID != "mp" || page.CatalogVersion == "" || page.Complete || page.NextCursor == "" || len(page.Resources) != 2 {
		t.Fatalf("first catalog page = %+v", page)
	}
	if page.Resources[0].Kind != KindCollection || page.Resources[1].Kind != KindCommand {
		t.Fatalf("first page order = %s/%s, want collection/command", page.Resources[0].Kind, page.Resources[1].Kind)
	}
	if page.Resources[0].Collection == nil || page.Resources[0].Collection.Resources == nil || page.Resources[0].Collection.Includes == nil || page.Resources[0].Collection.UnresolvedReferences == nil {
		t.Fatalf("collection direct relation arrays = %+v", page.Resources[0].Collection)
	}
	if page.Resources[1].Source == nil || page.Resources[1].Source.RepositoryURL != "https://git.example/internal/resources.git" ||
		page.Resources[1].Source.Path != "resources/command.yaml" || page.Resources[1].Source.Commit != strings.Repeat("a", 40) ||
		strings.Contains(first.Body.String(), "token=redact") {
		t.Fatalf("catalog provenance was not safely mapped: %+v", page.Resources[1])
	}
	if len(page.Resources[0].Collection.Resources) != 2 || page.Resources[0].Collection.Resources[0].Kind != KindSkill || page.Resources[0].Collection.Resources[1].Kind != KindCollection ||
		len(page.Resources[0].Collection.Includes) != 1 || page.Resources[0].Collection.Includes[0].Kind != KindCollection ||
		len(page.Resources[0].Collection.UnresolvedReferences) != 2 ||
		page.Resources[0].Collection.UnresolvedReferences[0].ReferenceType != "resource" ||
		page.Resources[0].Collection.UnresolvedReferences[1].ReferenceType != "include" {
		t.Fatalf("catalog collection relations = %+v", page.Resources[0].Collection)
	}

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/api/v1/catalog?limit=500&after="+string(page.NextCursor), nil))
	if second.Code != http.StatusOK {
		t.Fatalf("continuation = %d: %s", second.Code, second.Body.String())
	}
	assertCatalogHeaders(t, second)
	var remainder CatalogPage
	if err := json.Unmarshal(second.Body.Bytes(), &remainder); err != nil {
		t.Fatal(err)
	}
	if !remainder.Complete || remainder.NextCursor != "" || len(remainder.Resources) != 4 {
		t.Fatalf("final catalog page = %+v", remainder)
	}
	if got := catalogNames(append(page.Resources, remainder.Resources...)); strings.Join(got, ",") != "collection,command,hook,mcpserver,skill,subagent" {
		t.Fatalf("catalog order = %v", got)
	}

	etag := first.Header().Get("ETag")
	notModified := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/catalog", nil)
	request.Header.Set("If-None-Match", `"other", `+etag)
	handler.ServeHTTP(notModified, request)
	if notModified.Code != http.StatusNotModified || notModified.Body.Len() != 0 {
		t.Fatalf("multiple validator response = %d/%q, want 304 empty", notModified.Code, notModified.Body.String())
	}
	assertCatalogHeaders(t, notModified)
	wildcard := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/v1/catalog", nil)
	request.Header.Set("If-None-Match", "*")
	handler.ServeHTTP(wildcard, request)
	if wildcard.Code != http.StatusNotModified || wildcard.Body.Len() != 0 {
		t.Fatalf("wildcard response = %d/%q, want 304 empty", wildcard.Code, wildcard.Body.String())
	}
	continuationConditional := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/v1/catalog?after="+string(page.NextCursor), nil)
	request.Header.Set("If-None-Match", etag)
	handler.ServeHTTP(continuationConditional, request)
	if continuationConditional.Code != http.StatusBadRequest {
		t.Fatalf("continuation If-None-Match = %d, want 400", continuationConditional.Code)
	}
	head := httptest.NewRecorder()
	handler.ServeHTTP(head, httptest.NewRequest(http.MethodHead, "/api/v1/catalog", nil))
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD = %d/%q, want 200 empty", head.Code, head.Body.String())
	}
	assertCatalogHeaders(t, head)
}

func TestCatalogProjectionRejectsInvalidInputAndAppliesLiveRevocation(t *testing.T) {
	ctx := context.Background()
	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	seedCatalog(t, ctx, meta, []string{KindSkill, KindCommand})
	blocked := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: KindCommand, Name: KindCommand},
		ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("b", 64)),
	}
	assessment := testRevisionAssessment(t, blocked, store.AssessmentDecisionReject)
	if err := meta.PutResourceAssessment(ctx, assessment); err != nil {
		t.Fatal(err)
	}
	if err := meta.SetCurrentResourceAssessment(ctx, assessment); err != nil {
		t.Fatal(err)
	}
	handler := NewServer(config.Config{
		Instance:          config.Instance{ID: "mp"},
		CatalogProjection: config.CatalogProjection{Enabled: true},
		Revocations:       config.Revocations{Coordinates: []string{"mp/src/skill/skill"}},
	}, meta, fsblob.New(t.TempDir())).PublicHandler()
	for _, query := range []string{"?limit=0", "?limit=501", "?limit=1&limit=2", "?limit=-1", "?unknown=1", "?after=invalid"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/catalog"+query, nil))
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s status = %d, want 400", query, response.Code)
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/catalog", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("catalog = %d: %s", response.Code, response.Body.String())
	}
	var page CatalogPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Resources) != 2 || catalogAvailability(page.Resources, KindSkill) != AvailabilityRevoked ||
		catalogAvailability(page.Resources, KindCommand) != AvailabilityBlocked {
		t.Fatalf("live revocation/current blocked page = %+v", page)
	}
}

func TestCatalogProjectionMovingScanUsesOnlyStatelessCursor(t *testing.T) {
	ctx := context.Background()
	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	seedCatalog(t, ctx, meta, []string{KindCommand, KindSkill, KindSubagent})
	handler := NewServer(config.Config{
		Instance:          config.Instance{ID: "mp"},
		CatalogProjection: config.CatalogProjection{Enabled: true},
	}, meta, fsblob.New(t.TempDir())).PublicHandler()

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/v1/catalog?limit=1", nil))
	var initial CatalogPage
	if first.Code != http.StatusOK || json.Unmarshal(first.Body.Bytes(), &initial) != nil || len(initial.Resources) != 1 || initial.Resources[0].Name != KindCommand {
		t.Fatalf("first moving page = %d/%s", first.Code, first.Body.String())
	}

	const nextCommit = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	manifest := store.RevisionDigest("sha256:" + strings.Repeat("f", 64))
	if err := meta.PutRegistryOccurrence(ctx, store.RegistryOccurrence{
		RegistryID: "src", Ref: "main", Commit: nextCommit, RegistryRevisionManifestDigest: manifest,
		Acquisition: store.AcquisitionMirror, SourceOrigin: "https://git.example/internal/resources.git", OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	// The new registry head removes command and subagent. At that same head,
	// hook is added and skill receives a new revision; hook is reassessed as
	// blocked. None of these state changes depend on retained HTTP scan state.
	for _, change := range []struct {
		kind     string
		name     string
		digest   string
		decision store.AssessmentDecision
	}{
		{kind: KindHook, name: KindHook, digest: "d", decision: store.AssessmentDecisionReject},
		{kind: KindSkill, name: KindSkill, digest: "f", decision: store.AssessmentDecisionAllow},
	} {
		revision := store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: change.kind, Name: change.name},
			ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat(change.digest, 64)),
		}
		if err := meta.PutResourceRevision(ctx, revision); err != nil {
			t.Fatal(err)
		}
		if err := meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{Coordinate: revision.Coordinate, ContentDigest: revision.ContentDigest, Commit: nextCommit, ManifestDigest: manifest}); err != nil {
			t.Fatal(err)
		}
		assessment := testRevisionAssessment(t, revision, change.decision)
		if err := meta.PutResourceAssessment(ctx, assessment); err != nil {
			t.Fatal(err)
		}
		if err := meta.SetCurrentResourceAssessment(ctx, assessment); err != nil {
			t.Fatal(err)
		}
	}
	if err := meta.SetRegistryHead(ctx, "src", nextCommit, strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}

	continuation := httptest.NewRecorder()
	handler.ServeHTTP(continuation, httptest.NewRequest(http.MethodGet, "/api/v1/catalog?after="+string(initial.NextCursor), nil))
	var moved CatalogPage
	if continuation.Code != http.StatusOK || json.Unmarshal(continuation.Body.Bytes(), &moved) != nil {
		t.Fatalf("moving continuation = %d/%s", continuation.Code, continuation.Body.String())
	}
	if moved.CatalogVersion == initial.CatalogVersion || !moved.Complete || strings.Join(catalogNames(moved.Resources), ",") != "hook,skill" ||
		catalogAvailability(moved.Resources, KindHook) != AvailabilityBlocked ||
		catalogRevision(moved.Resources, KindSkill) != "sha256:"+strings.Repeat("f", 64) {
		t.Fatalf("moving continuation = %+v", moved)
	}
	fresh := httptest.NewRecorder()
	handler.ServeHTTP(fresh, httptest.NewRequest(http.MethodGet, "/api/v1/catalog", nil))
	var current CatalogPage
	if fresh.Code != http.StatusOK || json.Unmarshal(fresh.Body.Bytes(), &current) != nil || strings.Join(catalogNames(current.Resources), ",") != "hook,skill" {
		t.Fatalf("fresh scan after removal = %d/%s", fresh.Code, fresh.Body.String())
	}
}

func TestCatalogProjectionPassesCancellationToProjectionStore(t *testing.T) {
	meta := &catalogOnlyStore{}
	handler := NewServer(config.Config{
		Instance:          config.Instance{ID: "mp"},
		CatalogProjection: config.CatalogProjection{Enabled: true},
	}, meta, nil).PublicHandler()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/catalog", nil).WithContext(ctx))
	if response.Code != http.StatusInternalServerError || !meta.sawCancelledContext {
		t.Fatalf("cancelled catalog request = %d, projection store saw cancellation = %v", response.Code, meta.sawCancelledContext)
	}
}

type catalogOnlyStore struct {
	store.MetadataStore
	sawCancelledContext bool
}

func (s *catalogOnlyStore) ReadCatalogProjectionPage(ctx context.Context, _ store.CatalogProjectionPageRequest) (store.CatalogProjectionPage, error) {
	s.sawCancelledContext = ctx.Err() != nil
	return store.CatalogProjectionPage{}, ctx.Err()
}

func seedCatalog(t *testing.T, ctx context.Context, meta store.MetadataStore, kinds []string) {
	t.Helper()
	const (
		marketplace = store.MarketplaceID("mp")
		registry    = store.RegistryID("src")
		commit      = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: marketplace, Name: "Marketplace", CanonicalURL: "https://marketplace.test"}); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutRegistry(ctx, store.Registry{ID: registry, Slug: "source", Ref: "main", Listed: true, Acquisition: store.AcquisitionMirror}); err != nil {
		t.Fatal(err)
	}
	manifest := store.RevisionDigest("sha256:" + strings.Repeat("f", 64))
	if err := meta.PutRegistryOccurrence(ctx, store.RegistryOccurrence{RegistryID: registry, Ref: "main", Commit: commit, RegistryRevisionManifestDigest: manifest, Acquisition: store.AcquisitionMirror, SourceOrigin: "https://git.example/internal/resources.git?token=redact", OccurredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	revisions := make(map[string]store.ResourceRevision, len(kinds))
	for index, kind := range kinds {
		name := kind
		revision := store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: marketplace, Registry: registry, Kind: kind, Name: name},
			ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat(string(rune('a'+index)), 64)),
			Description:   kind + " description",
			License:       "MIT",
		}
		if err := meta.PutResourceRevision(ctx, revision); err != nil {
			t.Fatal(err)
		}
		if err := meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{
			Coordinate: revision.Coordinate, ContentDigest: revision.ContentDigest, Commit: commit, ManifestDigest: manifest,
			SourcePath: "resources/" + kind + ".yaml",
		}); err != nil {
			t.Fatal(err)
		}
		assessment := testRevisionAssessment(t, revision, store.AssessmentDecisionAllow)
		if err := meta.PutResourceAssessment(ctx, assessment); err != nil {
			t.Fatal(err)
		}
		if err := meta.SetCurrentResourceAssessment(ctx, assessment); err != nil {
			t.Fatal(err)
		}
		revisions[kind] = revision
	}
	if collection, ok := revisions[KindCollection]; ok {
		skill, hasSkill := revisions[KindSkill]
		if !hasSkill {
			t.Fatal("catalog collection fixture requires a skill")
		}
		missing := store.ResourceCoordinate{Marketplace: marketplace, Registry: registry, Kind: KindCollection, Name: "missing-member"}
		missingInclude := store.ResourceCoordinate{Marketplace: marketplace, Registry: registry, Kind: KindCollection, Name: "missing-include"}
		if err := meta.PutCollectionRevision(ctx, store.CollectionRevision{
			ID: collection.ContentDigest.String(), RegistryID: registry, ManifestDigest: collection.ContentDigest,
			Members:  []store.ResourceCoordinate{skill.Coordinate, collection.Coordinate, missing},
			Includes: []store.ResourceCoordinate{collection.Coordinate, missingInclude},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := meta.SetRegistryHead(ctx, registry, commit, ""); err != nil {
		t.Fatal(err)
	}
}

func assertCatalogHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Header().Get("Cache-Control") != "no-cache" || !strings.HasPrefix(response.Header().Get("ETag"), `"catalog-`) || !strings.HasSuffix(response.Header().Get("ETag"), `"`) {
		t.Fatalf("catalog headers = %v", response.Header())
	}
	if response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("catalog content type = %q", response.Header().Get("Content-Type"))
	}
}

func catalogNames(resources []CatalogResource) []string {
	names := make([]string, 0, len(resources))
	for _, resource := range resources {
		names = append(names, resource.Name)
	}
	return names
}

func catalogAvailability(resources []CatalogResource, name string) Availability {
	for _, resource := range resources {
		if resource.Name == name {
			return resource.Availability
		}
	}
	return ""
}

func catalogRevision(resources []CatalogResource, name string) string {
	for _, resource := range resources {
		if resource.Name == name {
			return resource.Revision
		}
	}
	return ""
}
