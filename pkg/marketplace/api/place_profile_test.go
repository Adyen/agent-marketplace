package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestExactTargetProfileRoutesRequireOptIn(t *testing.T) {
	server, meta, snapshot, handler, _ := managedRouteFixture(t)
	raw := []byte(`apiVersion: agent-marketplace.io/target-profile/v1
id: invented-app
label: Invented App
platforms: [macos, linux]
roots: {user: {override: INVENTED_APP_HOME, default: {macos: {base: home, path: .config/invented-app}, linux: {base: xdg-config, path: invented-app}}, allowed_bases: [home, xdg-config]}}
host: {executable: invented-app-bin, version_probe: {argv: [--version], pattern: '(\d+\.\d+\.\d+)'}, verified: [{range: ">=1.0.0 <2.0.0", date: 2026-11-12, evidence: fixture}]}
checks: {initial-load: {scope: eager-package-load, argv: [check, "{subject}"], subject: {under: mods, component: package-name}}}
kinds: {mod: {deliver: place, into: [mods/], verify: initial-load}}
`)
	identity := targetprofile.Identity{
		TargetID: "invented-app", SourceID: "git:https%3A%2F%2Fgit.example.com%2Ftools@main#profiles%2Finvented-app.yaml",
		RevisionDigest: inspection.Digest(raw),
	}
	coordinate := meta.rev.Coordinate
	coordinate.Kind = nameutil.TargetProfileKind
	coordinate.Name = identity.TargetID
	contentDigest := store.RevisionDigest(inspection.Digest(raw))
	projection, err := inspection.BuildTargetProfile(coordinate, identity, raw, 10, int64(len(raw)), int64(len(raw)))
	if err != nil {
		t.Fatalf("build profile inspection projection: %v", err)
	}
	revision := store.ResourceRevision{Coordinate: coordinate, ContentDigest: contentDigest, License: "MIT"}
	assessed := testAssessedRevision(t, revision, store.AssessmentDecisionAllow)
	assessed.Assessment.InspectionManifestDigest = store.RevisionDigest(projection.Manifest.ManifestDigest)
	assessed.Assessment.AssessmentDigest = ""
	assessed.Assessment, err = store.FinalizeResourceAssessment(assessed.Assessment)
	if err != nil {
		t.Fatalf("finalize profile assessment: %v", err)
	}
	assessed.Occurrence.SourcePath = "profiles/invented-app.yaml"
	meta.rev = assessed
	meta.owners = []store.AssessedResourceRevision{assessed}
	profileSource, err := targetprofile.ParseSourceID(identity.SourceID)
	if err != nil {
		t.Fatalf("parse profile source identity: %v", err)
	}
	if len(meta.registry.Origins) != 1 || meta.registry.Origins[0] == profileSource.Origin {
		t.Fatalf("registry origins = %q, want a public source different from the pinned profile identity", meta.registry.Origins)
	}
	uploads := map[string][]byte{projection.Manifest.ManifestDigest: projection.Payload}
	for digest, body := range projection.Bodies {
		uploads[digest] = body
	}
	for digest, body := range uploads {
		if err := server.blobs.Put(context.Background(), store.ArchiveDigest(digest), body); err != nil {
			t.Fatalf("store profile inspection blob %q: %v", digest, err)
		}
	}

	base := "/api/v1/resources/source/target-profile/invented-app/revisions/" + string(contentDigest)
	request := func(path string, optedIn bool) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if optedIn {
			req.Header.Set(placeFeaturesHeader, TargetProfilePlaceFeature)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	assertVary := func(response *httptest.ResponseRecorder) {
		t.Helper()
		if !strings.Contains(strings.Join(response.Header().Values("Vary"), ","), placeFeaturesHeader) {
			t.Fatalf("Vary = %q, want %s", response.Header().Values("Vary"), placeFeaturesHeader)
		}
	}

	for _, path := range []string{base, base + "/inspection"} {
		response := request(path, false)
		if response.Code == http.StatusOK {
			t.Fatalf("legacy target profile route %q returned 200: %s", path, response.Body.String())
		}
		assertVary(response)
	}

	response := request(base, true)
	if response.Code != http.StatusOK {
		t.Fatalf("opted-in exact profile revision status = %d: %s", response.Code, response.Body.String())
	}
	assertVary(response)
	var got resourceRevisionDTO
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode profile revision: %v", err)
	}
	if got.Coordinate != coordinate || got.ArchiveDigest != "" {
		t.Fatalf("profile revision = %+v, want coordinate %q and no archive", got, coordinate)
	}

	response = request(base+"/inspection", true)
	if response.Code != http.StatusOK {
		t.Fatalf("opted-in profile inspection status = %d: %s", response.Code, response.Body.String())
	}
	assertVary(response)
	var manifest inspection.Manifest
	if err := json.Unmarshal(response.Body.Bytes(), &manifest); err != nil {
		t.Fatalf("decode profile inspection: %v", err)
	}
	if manifest.Coordinate != coordinate || manifest.ProfileIdentity == nil || *manifest.ProfileIdentity != identity {
		t.Fatalf("profile inspection = %+v, want original identity %+v", manifest, identity)
	}

	response = request(base+"/inspection/content", true)
	if response.Code != http.StatusOK || response.Body.String() != string(raw) {
		t.Fatalf("opted-in profile content = %d/%q, want exact profile bytes", response.Code, response.Body.String())
	}
	assertVary(response)

	inspectionDigestPath := "/api/v1/inspection/" + strings.Replace(projection.Manifest.ManifestDigest, ":", "/", 1)
	response = request(inspectionDigestPath, false)
	if response.Code != http.StatusNotFound {
		t.Fatalf("legacy profile inspection digest status = %d, want 404", response.Code)
	}
	assertVary(response)
	response = request(inspectionDigestPath, true)
	if response.Code != http.StatusOK || response.Body.String() != string(projection.Payload) {
		t.Fatalf("opted-in profile inspection digest = %d/%q, want exact manifest bytes", response.Code, response.Body.String())
	}
	assertVary(response)

	otherRaw := bytes.Replace(raw, []byte("label: Invented App"), []byte("label: Other App"), 1)
	otherIdentity := identity
	otherIdentity.RevisionDigest = inspection.Digest(otherRaw)
	otherDigest := store.RevisionDigest(otherIdentity.RevisionDigest)
	otherProjection, err := inspection.BuildTargetProfile(coordinate, otherIdentity, otherRaw, 10, int64(len(otherRaw)), int64(len(otherRaw)))
	if err != nil {
		t.Fatalf("build alternate profile inspection projection: %v", err)
	}
	otherRevision := store.ResourceRevision{Coordinate: coordinate, ContentDigest: otherDigest, License: "MIT"}
	otherAssessed := testAssessedRevision(t, otherRevision, store.AssessmentDecisionAllow)
	otherAssessed.Assessment.InspectionManifestDigest = store.RevisionDigest(otherProjection.Manifest.ManifestDigest)
	otherAssessed.Assessment.AssessmentDigest = ""
	otherAssessed.Assessment, err = store.FinalizeResourceAssessment(otherAssessed.Assessment)
	if err != nil {
		t.Fatalf("finalize alternate profile assessment: %v", err)
	}
	otherAssessed.Occurrence.SourcePath = "profiles/invented-app.yaml"
	for digest, body := range map[string][]byte{
		otherProjection.Manifest.ManifestDigest: otherProjection.Payload,
		inspection.Digest(otherRaw):             otherRaw,
	} {
		if err := server.blobs.Put(context.Background(), store.ArchiveDigest(digest), body); err != nil {
			t.Fatalf("store alternate profile inspection blob %q: %v", digest, err)
		}
	}
	meta.rev = otherAssessed
	meta.owners = []store.AssessedResourceRevision{otherAssessed}
	response = request(base+"/inspection/content", true)
	if response.Code == http.StatusOK || bytes.Contains(response.Body.Bytes(), raw) || bytes.Contains(response.Body.Bytes(), otherRaw) {
		t.Fatalf("mismatched returned revision served profile bytes: %d/%s", response.Code, response.Body.String())
	}

	meta.rev = assessed
	meta.owners = []store.AssessedResourceRevision{assessed}
	snapshot.Registries[0].Withdrawn = true
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	meta.change.Snapshot = encoded
	response = request(base+"/inspection/content", true)
	if response.Code != http.StatusGone {
		t.Fatalf("withdrawn profile inspection content status = %d, want 410: %s", response.Code, response.Body.String())
	}
}
