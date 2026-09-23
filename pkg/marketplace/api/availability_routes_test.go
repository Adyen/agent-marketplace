package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/admin"
	marketplaceartifact "github.com/Adyen/agent-marketplace/pkg/marketplace/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/revocation"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
)

type availabilityRouteStore struct {
	*availabilityMetadata
	rev        store.AssessedResourceRevision
	owners     []store.AssessedResourceRevision
	registry   store.Registry
	occurrence store.RegistryOccurrence
}

func (s *availabilityRouteStore) GetRegistryOccurrence(context.Context, store.RegistryID, string) (store.RegistryOccurrence, error) {
	return s.occurrence, nil
}
func (s *availabilityRouteStore) GetRegistryBySlug(context.Context, string) (store.Registry, error) {
	return s.registry, nil
}
func (s *availabilityRouteStore) GetRegistry(context.Context, store.RegistryID) (store.Registry, error) {
	return s.registry, nil
}
func (s *availabilityRouteStore) ListRegistries(context.Context) ([]store.Registry, error) {
	return []store.Registry{s.registry}, nil
}
func (s *availabilityRouteStore) GetLatestResourceRevision(context.Context, store.ResourceCoordinate) (store.AssessedResourceRevision, error) {
	return s.rev, nil
}
func (s *availabilityRouteStore) GetResourceRevision(context.Context, store.ResourceCoordinate, store.RevisionDigest) (store.AssessedResourceRevision, error) {
	return s.rev, nil
}
func (s *availabilityRouteStore) ListResourceRevisions(context.Context, store.ResourceCoordinate) ([]store.AssessedResourceRevision, error) {
	return []store.AssessedResourceRevision{s.rev}, nil
}
func (s *availabilityRouteStore) SearchResourceRevisions(context.Context, store.ResourceSearchFilter) ([]store.AssessedResourceRevision, error) {
	return []store.AssessedResourceRevision{s.rev}, nil
}
func (s *availabilityRouteStore) ListResourceRevisionsByArchiveDigest(context.Context, store.MarketplaceID, store.ArchiveDigest) ([]store.AssessedResourceRevision, error) {
	return s.owners, nil
}
func (s *availabilityRouteStore) ListResourceRevisionsByContentDigest(context.Context, store.MarketplaceID, store.RevisionDigest) ([]store.AssessedResourceRevision, error) {
	return s.owners, nil
}
func (s *availabilityRouteStore) ListBlobOwners(context.Context, store.MarketplaceID, string, store.ArchiveDigest) ([]store.AssessedResourceRevision, []store.RegistryID, error) {
	return s.owners, []store.RegistryID{s.registry.ID}, nil
}
func (s *availabilityRouteStore) GetResourceAssessment(context.Context, store.AssessmentDigest) (store.ResourceAssessment, error) {
	return s.rev.Assessment, nil
}
func (s *availabilityRouteStore) ReadCatalogProjectionPage(context.Context, store.CatalogProjectionPageRequest) (store.CatalogProjectionPage, error) {
	return store.CatalogProjectionPage{CatalogVersion: 1, Resources: []store.CatalogProjectionResource{{AssessedResourceRevision: s.rev, RegistrySlug: s.registry.Slug, Availability: store.CatalogAvailabilityAvailable}}}, nil
}

func managedRouteFixture(t *testing.T) (*Server, *availabilityRouteStore, admin.Snapshot, http.Handler, []string) {
	t.Helper()
	server, meta, snapshot, _ := availabilityFixture(t)
	artifact, err := hccartifact.FromIR(ir.NewMcpServer("example", "npx", []string{"example"}, nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatal(err)
	}
	artifactData, proofData, archive, err := hccartifact.EncodeWithProof(artifact)
	if err != nil {
		t.Fatal(err)
	}
	intrinsic := store.ResourceRevision{Coordinate: store.ResourceCoordinate{Marketplace: "market", Registry: "reg", Kind: "mcpserver", Name: "example"}, ContentDigest: store.RevisionDigest(artifact.Manifest.ContentDigest), ArchiveDigest: store.ArchiveDigest(archive), License: "MIT"}
	projection, err := inspection.Build(intrinsic.Coordinate, string(intrinsic.ContentDigest), []byte(`{"name":"safe"}`), nil, 10, 1024, 4096)
	if err != nil {
		t.Fatal(err)
	}
	rev := testAssessedRevision(t, intrinsic, store.AssessmentDecisionAllow)
	rev.Assessment.InspectionManifestDigest = store.RevisionDigest(projection.Manifest.ManifestDigest)
	rev.Assessment.AssessmentDigest = ""
	rev.Assessment, err = store.FinalizeResourceAssessment(rev.Assessment)
	if err != nil {
		t.Fatal(err)
	}
	manifest, manifestDigest, err := marketplaceartifact.CanonicalRegistryRevisionPayload(marketplaceartifact.RegistryRevisionManifest{SchemaVersion: "v1", Generator: marketplaceartifact.RegistryRevisionGeneratorV1, Resources: []marketplaceartifact.RevisionResource{{Kind: "mcpserver", Name: "example", ContentDigest: string(intrinsic.ContentDigest)}}})
	if err != nil {
		t.Fatal(err)
	}
	blobs := fsblob.New(t.TempDir())
	uploads := map[string][]byte{
		archive: artifactData, string(store.ArtifactProofBlobKey(store.ArchiveDigest(archive))): proofData,
		projection.Manifest.ManifestDigest: projection.Payload, manifestDigest: manifest,
	}
	for digest, body := range projection.Bodies {
		uploads[digest] = body
	}
	for digest, body := range uploads {
		if err := blobs.Put(context.Background(), store.ArchiveDigest(digest), body); err != nil {
			t.Fatal(err)
		}
	}
	adapter := &availabilityRouteStore{availabilityMetadata: meta, rev: rev, owners: []store.AssessedResourceRevision{rev}, registry: store.Registry{ID: "reg", Slug: "source", Ref: "main", Acquisition: store.AcquisitionMirror}}
	adapter.registry.Origins = []string{"https://example.com/repo"}
	adapter.occurrence = store.RegistryOccurrence{OccurredAt: time.Unix(1, 0).UTC(), RegistryID: "reg", Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Commit: strings.Repeat("e", 40), RegistryRevisionManifestDigest: store.RevisionDigest(manifestDigest), Acquisition: store.AcquisitionMirror}
	server.meta = adapter
	server.blobs = blobs
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/resources/", server.handleResources)
	mux.HandleFunc("/api/v1/registries/", server.handleRegistries)
	mux.HandleFunc("/api/v1/artifacts/", server.handleArtifacts)
	mux.HandleFunc("/api/v1/artifact-proofs/", server.handleArtifactProofs)
	mux.HandleFunc("/api/v1/inspection/", server.handleInspectionDigest)
	mux.HandleFunc("/api/v1/content/", server.handleContentDigest)
	mux.HandleFunc("/api/v1/manifests/", server.handleManifests)
	mux.HandleFunc("/api/v1/assessments/", server.handleAssessmentDigest)
	mux.HandleFunc("/api/v1/catalog", server.handleCatalogProjection)
	mux.HandleFunc("/api/v1/search", server.handleSearch)
	resource := "/api/v1/resources/source/mcpserver/example"
	exact := resource + "/revisions/" + string(intrinsic.ContentDigest)
	bodyDigest := projection.Manifest.Files[0].ContentDigest
	paths := []string{resource, resource + "/revisions", exact, exact + "/inspection", exact + "/inspection/content", exact + "/inspection/content/" + bodyDigest, "/api/v1/artifacts/" + strings.Replace(archive, ":", "/", 1), "/api/v1/artifact-proofs/" + strings.Replace(archive, ":", "/", 1), "/api/v1/inspection/" + strings.Replace(projection.Manifest.ManifestDigest, ":", "/", 1), "/api/v1/content/" + strings.Replace(bodyDigest, ":", "/", 1), "/api/v1/manifests/" + manifestDigest}
	paths = append(paths, "/api/v1/registries/source/occurrences/"+adapter.occurrence.Commit)
	return server, adapter, snapshot, server.availabilityHandler(mux), paths
}

func TestManagedPublicRouteAvailability(t *testing.T) {
	server, meta, snapshot, handler, paths := managedRouteFixture(t)
	blobs := &countingBlobStore{BlobStore: server.blobs}
	server.blobs = blobs
	for _, state := range []struct {
		name   string
		edit   func()
		status int
	}{
		{"allowed", func() {}, 200},
		{"paused", func() { snapshot.Registries[0].Paused = true; snapshot.Registries[0].Lifecycle = "paused" }, 200},
		{"withdrawn", func() { snapshot.Registries[0].Withdrawn = true }, 410},
		{"restored paused", func() { snapshot.Registries[0].Withdrawn = false }, 200},
		{"policy tightened", func() { snapshot.LicensePolicy.Allowlist = []string{"Apache-2.0"} }, 403},
		{"policy relaxed", func() { snapshot.LicensePolicy.Allowlist = nil }, 200},
		{"retired", func() { snapshot.Registries[0].Retired = true }, 410},
	} {
		t.Run(state.name, func(t *testing.T) {
			state.edit()
			meta.change.Revision++
			meta.change.Snapshot, _ = json.Marshal(snapshot)
			search := httptest.NewRecorder()
			handler.ServeHTTP(search, httptest.NewRequest("GET", "/api/v1/search", nil))
			wantAvailability := "available"
			if state.status == 403 {
				wantAvailability = "policy-blocked"
			}
			if state.status == 410 {
				wantAvailability = "revoked"
			}
			if search.Code != 200 || !strings.Contains(search.Body.String(), `"availability":"`+wantAvailability+`"`) {
				t.Fatalf("search = %d %s", search.Code, search.Body.String())
			}
			readsBefore := blobs.gets
			for _, path := range paths {
				for _, method := range []string{"GET", "HEAD"} {
					request := httptest.NewRequest(method, path, nil)
					request.Header.Set("If-None-Match", "*")
					request.Header.Set("Range", "bytes=0-3")
					response := httptest.NewRecorder()
					handler.ServeHTTP(response, request)
					if response.Code != state.status {
						t.Fatalf("%s %s = %d: %s", method, path, response.Code, response.Body.String())
					}
					if response.Header().Get("Cache-Control") != "private, no-cache, max-age=0, must-revalidate" {
						t.Fatalf("cache %s", response.Header().Get("Cache-Control"))
					}
				}
			}
			if state.status != 200 && blobs.gets != readsBefore {
				t.Fatal("denied request read blob storage")
			}
			if state.status == 200 && blobs.gets == readsBefore {
				t.Fatal("positive content requests did not read blobs")
			}
		})
	}
}

func TestManagedOperatorExposedRoutesRemainInstallable(t *testing.T) {
	server, meta, snapshot, handler, paths := managedRouteFixture(t)
	blobs := &countingBlobStore{BlobStore: server.blobs}
	server.blobs = blobs
	meta.rev.Assessment.Decision = store.AssessmentDecisionReject
	meta.rev.Assessment.AssessmentDigest = ""
	var err error
	meta.rev.Assessment, err = store.FinalizeResourceAssessment(meta.rev.Assessment)
	if err != nil {
		t.Fatal(err)
	}
	meta.owners[0] = meta.rev
	snapshot.DistributionPolicy.RejectedResources = config.RejectedResourcesExpose
	meta.change.Snapshot, _ = json.Marshal(snapshot)
	search := httptest.NewRecorder()
	handler.ServeHTTP(search, httptest.NewRequest("GET", "/api/v1/search", nil))
	if search.Code != http.StatusOK || !strings.Contains(search.Body.String(), `"availability":"operator-exposed"`) {
		t.Fatalf("search = %d %s", search.Code, search.Body.String())
	}
	for _, path := range paths {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", path, response.Code, response.Body.String())
		}
	}
	if blobs.gets == 0 {
		t.Fatal("operator-exposed content did not read blob storage")
	}
}

func TestManagedSharedDigestOwnersConservative(t *testing.T) {
	_, meta, snapshot, handler, paths := managedRouteFixture(t)
	other := meta.rev
	other.Coordinate.Registry = "other"
	meta.owners = append(meta.owners, other)
	snapshot.Registries = append(snapshot.Registries, admin.Registry{ID: "other", Generation: 1, Lifecycle: "withdrawn", Withdrawn: true, Acquisition: store.AcquisitionMirror})
	meta.change.Snapshot, _ = json.Marshal(snapshot)
	for _, path := range paths[6:] {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != 410 {
			t.Fatalf("shared %s = %d", path, response.Code)
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", paths[4], nil))
	if response.Code != 200 {
		t.Fatalf("coordinate-scoped inspection should remain available: %d", response.Code)
	}
}

func TestManagedCatalogRevalidatesAfterPolicyChange(t *testing.T) {
	_, meta, snapshot, handler, _ := managedRouteFixture(t)
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest("GET", "/api/v1/catalog", nil))
	if first.Code != 200 || !strings.Contains(first.Body.String(), `"availability":"available"`) {
		t.Fatalf("first=%d %s", first.Code, first.Body.String())
	}
	oldETag := first.Header().Get("ETag")
	if oldETag == "" {
		t.Fatal("no ETag")
	}
	snapshot.LicensePolicy.Allowlist = []string{"Apache-2.0"}
	meta.change.Revision++
	meta.change.Snapshot, _ = json.Marshal(snapshot)
	request := httptest.NewRequest("GET", "/api/v1/catalog", nil)
	request.Header.Set("If-None-Match", oldETag)
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, request)
	if second.Code != 200 || !strings.Contains(second.Body.String(), `"availability":"blocked"`) || second.Header().Get("ETag") == oldETag {
		t.Fatalf("second=%d %s %s", second.Code, second.Body.String(), second.Header().Get("ETag"))
	}
	snapshot.Registries[0].Retired = true
	meta.change.Revision++
	meta.change.Snapshot, _ = json.Marshal(snapshot)
	third := httptest.NewRecorder()
	handler.ServeHTTP(third, request)
	if third.Code != 200 || !strings.Contains(third.Body.String(), `"availability":"revoked"`) {
		t.Fatalf("retired anchor=%d %s", third.Code, third.Body.String())
	}
	assessment := httptest.NewRecorder()
	handler.ServeHTTP(assessment, httptest.NewRequest("GET", "/api/v1/assessments/"+strings.Replace(string(meta.rev.Assessment.AssessmentDigest), ":", "/", 1), nil))
	if assessment.Code != 200 || assessment.Header().Get("Cache-Control") != "private, no-cache, max-age=0, must-revalidate" {
		t.Fatalf("diagnostic assessment=%d %s", assessment.Code, assessment.Body.String())
	}
	meta.err = store.ErrManagedStateCorrupt
	failed := httptest.NewRecorder()
	handler.ServeHTTP(failed, request)
	if failed.Code != 503 {
		t.Fatalf("corrupt snapshot conditional=%d", failed.Code)
	}
}

func TestManagedCollectionManifestRestrictions(t *testing.T) {
	server, meta, snapshot, handler, _ := managedRouteFixture(t)
	payload, digest, err := hccartifact.EncodeCollectionManifest(hccartifact.CollectionManifest{SchemaVersion: hccartifact.CollectionSchemaV1, Name: "bundle", Members: []hccartifact.CollectionMemberEntry{{Kind: "mcpserver", Name: "example", RegistryID: "reg", Revision: string(meta.rev.ContentDigest)}}, SupportedSchemas: []string{hccartifact.CollectionSchemaV1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.blobs.Put(context.Background(), store.ArchiveDigest(digest), payload); err != nil {
		t.Fatal(err)
	}
	collection := meta.rev
	collection.Coordinate.Kind = "collection"
	collection.Coordinate.Name = "bundle"
	collection.ContentDigest = store.RevisionDigest(digest)
	collection.ArchiveDigest = ""
	meta.owners = []store.AssessedResourceRevision{collection}
	path := "/api/v1/manifests/" + digest
	allowed := httptest.NewRecorder()
	handler.ServeHTTP(allowed, httptest.NewRequest("GET", path, nil))
	if allowed.Code != 200 {
		t.Fatalf("collection positive=%d %s", allowed.Code, allowed.Body.String())
	}
	snapshot.LicensePolicy.Allowlist = []string{"Apache-2.0"}
	meta.change.Snapshot, _ = json.Marshal(snapshot)
	blocked := httptest.NewRecorder()
	handler.ServeHTTP(blocked, httptest.NewRequest("GET", path, nil))
	if blocked.Code != 403 {
		t.Fatalf("collection restrictive policy=%d", blocked.Code)
	}
	snapshot.LicensePolicy.Allowlist = nil
	meta.change.Snapshot, _ = json.Marshal(snapshot)
	meta.owners[0].Assessment.Decision = store.AssessmentDecisionReject
	rejected := httptest.NewRecorder()
	handler.ServeHTTP(rejected, httptest.NewRequest("GET", path, nil))
	if rejected.Code != 403 {
		t.Fatalf("collection assessment restriction=%d", rejected.Code)
	}
}

func TestManagedMetadataSeparatesPolicyAndAssessment(t *testing.T) {
	_, meta, snapshot, handler, paths := managedRouteFixture(t)
	meta.rev.License = ""
	snapshot.LicensePolicy.Undeclared = "warn"
	meta.change.Snapshot, _ = json.Marshal(snapshot)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", paths[0], nil))
	var body struct {
		Assessment          store.ResourceAssessment `json:"assessment"`
		CurrentAvailability availabilityDecision     `json:"current_availability"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || body.Assessment.AssessmentDigest != meta.rev.Assessment.AssessmentDigest || body.CurrentAvailability.LicenseOutcome != "warn" || body.CurrentAvailability.AssessmentDecision != store.AssessmentDecisionAllow {
		t.Fatalf("warning diagnostic=%d %+v", response.Code, body)
	}
	snapshot.LicensePolicy.Undeclared = "reject"
	meta.change.Revision++
	meta.change.Snapshot, _ = json.Marshal(snapshot)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", paths[0], nil))
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response.Code != 403 || body.CurrentAvailability.LicenseOutcome != "reject" || body.CurrentAvailability.AssessmentDecision != store.AssessmentDecisionAllow || body.CurrentAvailability.ManagedRevision != meta.change.Revision {
		t.Fatalf("restriction diagnostic=%d %+v", response.Code, body)
	}
}

func TestManagedDigestOwnershipCapabilityRequired(t *testing.T) {
	server, meta, _, _ := availabilityFixture(t)
	handler := server.availabilityHandler(http.HandlerFunc(server.handleContentDigest))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/content/sha256/"+strings.Repeat("a", 64), nil))
	if response.Code != 503 || meta.reads != 1 {
		t.Fatalf("missing ownership capability=%d reads=%d", response.Code, meta.reads)
	}
}

func TestManagedRestoreDoesNotCancelDeploymentRevocation(t *testing.T) {
	for _, declarations := range []config.Revocations{
		{RegistryIDs: []string{"reg"}},
		{Coordinates: []string{"market/reg/skill/example"}},
		{Revisions: []string{"market/reg/skill/example@sha256:" + strings.Repeat("a", 64)}},
	} {
		server, meta, snapshot, rev := availabilityFixture(t)
		var err error
		server.revocations, err = revocation.New(config.Config{Revocations: declarations})
		if err != nil {
			t.Fatal(err)
		}
		snapshot.Registries[0].Withdrawn = true
		meta.change.Snapshot, _ = json.Marshal(snapshot)
		view, err := server.loadAvailability(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		both := server.evaluateAvailability(view, rev)
		if !both.AdminWithdrawal || !both.DeploymentRevocation {
			t.Fatalf("source union=%+v", both)
		}
		snapshot.Registries[0].Withdrawn = false
		snapshot.Registries[0].Paused = true
		meta.change.Snapshot, _ = json.Marshal(snapshot)
		view, err = server.loadAvailability(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		restored := server.evaluateAvailability(view, rev)
		if restored.AdminWithdrawal || !restored.DeploymentRevocation || restored.Availability != AvailabilityRevoked {
			t.Fatalf("restore=%+v", restored)
		}
	}
}
