package startup

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type Fixture struct {
	Server           *httptest.Server
	Revisions        map[string]string
	archiveDigests   map[string]string
	collectionDigest string
}

func New(t *testing.T, cache *marketplace.Cache) *Fixture {
	t.Helper()
	type resource struct {
		name     string
		raw      []byte
		revision store.AssessedResourceRevision
		content  string
		archive  string
	}
	resources := make([]resource, 0, 3)
	for _, name := range []string{"audit", "review", "gamma"} {
		canonical, err := hccartifact.FromIR(ir.NewSkill(name, "Startup fixture", "# "+name+"\n", nil, nil, ir.SourceRef{}))
		if err != nil {
			t.Fatal("create canonical startup skill")
		}
		canonical.Manifest.RegistryID = "tools"
		canonical.Manifest.RegistrySlug = "tools"
		canonical.Manifest.Revision = canonical.Manifest.ContentDigest
		canonical.Manifest.Provenance = hccartifact.CanonicalProvenance{
			RegistryURL: "https://git.example/repo.git", Ref: "main", Path: ".agents", Generator: "test",
		}
		canonical.Manifest.SupportedSchemas = []string{hccartifact.CanonicalArtifactSchemaV1}
		raw, archiveDigest, err := hccartifact.Encode(canonical)
		if err != nil {
			t.Fatal("encode canonical startup skill")
		}
		contentDigest := canonical.Manifest.ContentDigest
		coordinate := store.ResourceCoordinate{Marketplace: "mp", Registry: "tools", Kind: "skill", Name: name}
		content := store.RevisionDigest(contentDigest)
		archive := store.ArchiveDigest(archiveDigest)
		assessment, err := store.FinalizeResourceAssessment(startupFixtureAssessment(coordinate, content))
		if err != nil {
			t.Fatal("finalize startup skill assessment")
		}
		revision := store.AssessedResourceRevision{
			ResourceRevision: store.ResourceRevision{Coordinate: coordinate, ContentDigest: content, ArchiveDigest: archive},
			Assessment:       assessment,
			Occurrence: store.ResourceOccurrence{Coordinate: coordinate, ContentDigest: content, Commit: strings.Repeat("a", 40),
				ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("b", 64))},
		}
		resources = append(resources, resource{name: name, raw: raw, revision: revision, content: contentDigest, archive: archiveDigest})
	}
	revisionPaths := make(map[string]store.AssessedResourceRevision, len(resources))
	artifactPaths := make(map[string][]byte, len(resources))
	fixture := &Fixture{Revisions: make(map[string]string, len(resources)), archiveDigests: make(map[string]string, len(resources))}
	fixture.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", fixture.Server.URL))
		case "/api/v1/search":
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(marketplace.SearchResponse{Items: []mpapi.ResourceListItem{}, Total: 0, Offset: offset, Limit: limit})
		default:
			if revision, ok := revisionPaths[r.URL.Path]; ok {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(revision)
				return
			}
			if raw, ok := artifactPaths[r.URL.Path]; ok {
				w.Header().Set("Content-Type", "application/octet-stream")
				_, _ = w.Write(raw)
				return
			}
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fixture.Server.Close)
	for _, resource := range resources {
		if _, err := cache.WriteArtifact(resource.archive, resource.raw); err != nil {
			t.Fatal("seed startup artifact cache")
		}
		if err := cache.MarkArtifactVerified(resource.archive); err != nil {
			t.Fatal("mark startup artifact verified")
		}
		if _, err := cache.WriteResourceRevision(fixture.Server.URL, "tools", "skill", resource.name, resource.content, resource.revision); err != nil {
			t.Fatal("seed startup revision metadata")
		}
		fixture.Revisions[resource.name] = resource.content
		fixture.archiveDigests[resource.name] = resource.archive
		revisionPaths["/api/v1/resources/tools/skill/"+resource.name+"/revisions/"+resource.content] = resource.revision
		artifactPaths["/api/v1/artifacts/sha256/"+strings.TrimPrefix(resource.archive, "sha256:")] = resource.raw
	}
	digest, revision, payload, _, _ := SeedCollection(t, cache, fixture.Server.URL, "bundle", []hccartifact.CollectionMemberEntry{
		{Kind: "skill", Name: "gamma", RegistryID: "tools", Revision: fixture.Revisions["gamma"]},
	}, true)
	fixture.collectionDigest = digest
	revisionPaths["/api/v1/resources/tools/collection/bundle/revisions/"+digest] = revision
	artifactPaths["/api/v1/manifests/"+digest] = payload
	return fixture
}

func startupFixtureAssessment(coordinate store.ResourceCoordinate, content store.RevisionDigest) store.ResourceAssessment {
	return store.ResourceAssessment{
		Coordinate: coordinate, ContentDigest: content, AnalyzerVersion: "gui-startup-fixture",
		PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("c", 64)), Decision: store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("d", 64)), AssessedAt: time.Unix(1, 0).UTC(),
	}
}

// SeedCollection writes a canonical collection manifest and its revision
// metadata into cache for baseURL, marking the manifest verified when requested.
func SeedCollection(t *testing.T, cache *marketplace.Cache, baseURL, name string, members []hccartifact.CollectionMemberEntry, verified bool) (digest string, revision store.AssessedResourceRevision, payload []byte, metadataPath, manifestPath string) {
	t.Helper()
	payload, digest, err := hccartifact.EncodeCollectionManifest(hccartifact.CollectionManifest{
		SchemaVersion: hccartifact.CollectionSchemaV1, Name: name, SupportedSchemas: []string{hccartifact.CollectionSchemaV1}, Members: members,
	})
	if err != nil {
		t.Fatal("encode startup collection manifest")
	}
	coordinate := store.ResourceCoordinate{Marketplace: "mp", Registry: "tools", Kind: "collection", Name: name}
	content := store.RevisionDigest(digest)
	assessment, err := store.FinalizeResourceAssessment(startupFixtureAssessment(coordinate, content))
	if err != nil {
		t.Fatal("finalize startup collection assessment")
	}
	revision = store.AssessedResourceRevision{
		ResourceRevision: store.ResourceRevision{Coordinate: coordinate, ContentDigest: content},
		Assessment:       assessment,
		Occurrence: store.ResourceOccurrence{Coordinate: coordinate, ContentDigest: content, Commit: strings.Repeat("a", 40),
			ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("b", 64))},
	}
	metadataPath, err = cache.WriteResourceRevision(baseURL, "tools", "collection", name, digest, revision)
	if err != nil {
		t.Fatal("seed startup collection revision metadata")
	}
	manifestPath, err = cache.WriteCollectionManifest(digest, payload)
	if err != nil {
		t.Fatal("seed startup collection manifest")
	}
	if verified {
		if err := cache.MarkCollectionVerified(digest); err != nil {
			t.Fatal("mark startup collection manifest verified")
		}
	}
	return digest, revision, payload, metadataPath, manifestPath
}

func (f *Fixture) OrganizationConfig(required bool) []byte {
	requiredLine := ""
	if required {
		requiredLine = "    required: true\n"
	}
	return []byte(fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [devin]\ntelemetry:\n  level: off\nsources:\n  mp: {marketplace: %q}\nresources:\n  mp:\n    include:\n      - tools/skill/audit: {revision: %q}\n      - tools/skill/review: {revision: %q}\n%s", f.Server.URL, f.Revisions["audit"], f.Revisions["review"], requiredLine))
}

func (f *Fixture) CollectionDigest() string {
	return f.collectionDigest
}

func (f *Fixture) OrganizationLock(t *testing.T) []byte {
	t.Helper()
	resources := make([]lockfile.MarketplaceResourceLock, 0, 2)
	for _, name := range []string{"audit", "review"} {
		revision, revisionOK := f.Revisions[name]
		archive, archiveOK := f.archiveDigests[name]
		if !revisionOK || !archiveOK {
			t.Fatal("startup fixture lock resource was not seeded")
		}
		resources = append(resources, lockfile.MarketplaceResourceLock{
			RegistryName: "tools", RegistryID: "tools", Kind: "skill", Name: name,
			Revision: revision, ContentDigest: lockfile.Digest(revision), ArchiveDigest: lockfile.Digest(archive),
			Acquisition: lockfile.MarketplaceAcquisitionMirror,
		})
	}
	data, err := lockfile.Encode(lockfile.Document{
		APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeOrganization,
		Marketplaces: []lockfile.MarketplaceLock{{
			URL: f.Server.URL, InstanceID: "mp",
			RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "tools", RegistryID: "tools"}},
			Resources:        resources,
		}},
	})
	if err != nil {
		t.Fatal("encode seeded organization lock")
	}
	return data
}

func (f *Fixture) UserCollectionLock(t *testing.T) []byte {
	t.Helper()
	gammaRevision, archive := f.Revisions["gamma"], f.archiveDigests["gamma"]
	if gammaRevision == "" || archive == "" || f.collectionDigest == "" {
		t.Fatal("startup fixture collection lock was not seeded")
	}
	data, err := lockfile.Encode(lockfile.Document{
		APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeUser,
		Marketplaces: []lockfile.MarketplaceLock{{
			URL: f.Server.URL, InstanceID: "mp",
			RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "tools", RegistryID: "tools"}},
			Resources: []lockfile.MarketplaceResourceLock{
				{
					RegistryName: "tools", RegistryID: "tools", Kind: "collection", Name: "bundle",
					Revision: f.collectionDigest, ContentDigest: lockfile.Digest(f.collectionDigest),
					CollectionManifestDigest: lockfile.Digest(f.collectionDigest), Acquisition: lockfile.MarketplaceAcquisitionMirror,
				},
				{
					RegistryName: "tools", RegistryID: "tools", Kind: "skill", Name: "gamma",
					Revision: gammaRevision, ContentDigest: lockfile.Digest(gammaRevision), ArchiveDigest: lockfile.Digest(archive),
					Acquisition: lockfile.MarketplaceAcquisitionMirror,
					SelectedBy:  []lockfile.MarketplaceSelectionLock{{RegistryID: "tools", Name: "bundle", Revision: lockfile.Digest(f.collectionDigest)}},
				},
			},
		}},
	})
	if err != nil {
		t.Fatal("encode seeded user collection lock")
	}
	return data
}

func (f *Fixture) UserConfig(excludeReview bool) []byte {
	if excludeReview {
		return []byte(fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [devin]\ntelemetry:\n  level: off\nsources:\n  mp: {marketplace: %q}\nresources:\n  mp:\n    exclude: [tools/skill/review]\n", f.Server.URL))
	}
	return []byte("apiVersion: agent-marketplace/v4\nharnesses: [devin]\ntelemetry:\n  level: off\nsources: {}\nresources: {}\n")
}
