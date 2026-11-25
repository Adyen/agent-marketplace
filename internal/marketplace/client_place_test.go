package marketplace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

const placeAcquireProfileYAML = `apiVersion: agent-marketplace.io/target-profile/v1
id: invented-app
label: Invented App
platforms: [macos, linux]
roots: {user: {override: INVENTED_APP_HOME, default: {macos: {base: home, path: .config/invented-app}, linux: {base: xdg-config, path: invented-app}}, allowed_bases: [home, xdg-config]}}
host: {executable: invented-app-bin, version_probe: {argv: [--version], pattern: '(\d+\.\d+\.\d+)'}, verified: [{range: ">=1.0.0 <2.0.0", date: 2026-11-12, evidence: fixture}]}
checks: {initial-load: {scope: eager-package-load, argv: [check, "{subject}"], subject: {under: mods, component: package-name}}}
kinds: {mod: {deliver: place, into: [mods/], verify: initial-load}}
`

// spec: marketplace-api/place-manifests-and-assessments-agree-on-the-exact-binding
func TestAcquirePlacePackageRequiresExactProfileBeforeArchive(t *testing.T) {
	profileBytes := []byte(placeAcquireProfileYAML)
	profileIdentity := placeAcquireProfileIdentity(profileBytes)
	profile, err := targetprofile.Parse(profileBytes, profileIdentity)
	if err != nil {
		t.Fatalf("parse profile fixture: %v", err)
	}
	coordinate := store.ResourceCoordinate{Marketplace: "mp", Registry: "reg", Kind: "invented-app/mod", Name: "sample"}
	files := []targetprofile.PackageFile{{
		SourcePath: "payload/instructions.md", DestinationPath: "mods/sample/instructions.md",
		Mode: 0o644, Bytes: []byte("place package bytes\n"),
	}}
	artifact, err := hccartifact.NewPlaceArtifact(coordinate.Kind, coordinate.Name, profileIdentity, files)
	if err != nil {
		t.Fatalf("build place artifact: %v", err)
	}
	_, placeholder, archiveDigest, err := hccartifact.EncodeArchives(artifact)
	if err != nil {
		t.Fatalf("encode place archives: %v", err)
	}
	revision := placeAcquireRevision(t, coordinate, artifact, archiveDigest)
	var responseRevision atomic.Pointer[store.AssessedResourceRevision]
	responseRevision.Store(&revision)
	revisionPath := "/api/v1/resources/reg/invented-app%2Fmod/sample/revisions/" + string(revision.ContentDigest)
	archivePath := "/api/v1/archives/sha256/" + strings.TrimPrefix(string(revision.ArchiveDigest), "sha256:")
	var revisionRequests atomic.Int32
	var archiveRequests atomic.Int32
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case revisionPath:
			revisionRequests.Add(1)
			values := r.Header.Values("X-Agent-Marketplace-Features")
			if len(values) != 1 || values[0] != "target-profile-place-v1" {
				t.Errorf("resource feature header = %q", values)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(struct {
				store.AssessedResourceRevision
				CurrentAvailability struct {
					Availability string `json:"availability"`
				} `json:"current_availability"`
			}{AssessedResourceRevision: *responseRevision.Load(), CurrentAvailability: struct {
				Availability string `json:"availability"`
			}{Availability: "available"}})
		case archivePath:
			archiveRequests.Add(1)
			_, _ = w.Write(placeholder)
		default:
			t.Errorf("unexpected request path %q", r.URL.EscapedPath())
			http.NotFound(w, r)
		}
	}))
	defer peer.Close()
	client, err := NewClient(peer.URL)
	if err != nil {
		t.Fatal(err)
	}

	gotRevision, gotFiles, err := client.AcquirePlacePackage(context.Background(), coordinate, revision.ContentDigest, profileIdentity, profileBytes)
	if err != nil {
		t.Fatalf("AcquirePlacePackage: %v", err)
	}
	if gotRevision.Coordinate != coordinate || gotRevision.ArchiveDigest != revision.ArchiveDigest ||
		gotRevision.Assessment.HostPackageIdentity == nil ||
		*gotRevision.Assessment.HostPackageIdentity != *revision.Assessment.HostPackageIdentity {
		t.Fatalf("acquired revision = %+v, want exact coordinate, archive and binding", gotRevision)
	}
	if err := gotRevision.Assessment.Validate(); err != nil {
		t.Fatalf("acquired assessment validation: %v", err)
	}
	if len(gotFiles) != 1 || gotFiles[0].SourcePath != files[0].SourcePath ||
		gotFiles[0].DestinationPath != files[0].DestinationPath || gotFiles[0].Mode != files[0].Mode ||
		string(gotFiles[0].Bytes) != string(files[0].Bytes) {
		t.Fatalf("acquired files = %+v, want exact package file", gotFiles)
	}
	if err := targetprofile.ValidatePackageDestinations(profile, coordinate.Kind, gotFiles); err != nil {
		t.Fatalf("acquired destination validation: %v", err)
	}
	if revisionRequests.Load() != 1 || archiveRequests.Load() != 1 {
		t.Fatalf("valid acquisition requests: revision=%d archive=%d, want 1 each", revisionRequests.Load(), archiveRequests.Load())
	}

	beforeRevisionRequests := revisionRequests.Load()
	beforeArchiveRequests := archiveRequests.Load()
	staleProfile := []byte(strings.Replace(placeAcquireProfileYAML, "label: Invented App", "label: Stale App", 1))
	failedRevision, failedFiles, err := client.AcquirePlacePackage(context.Background(), coordinate, revision.ContentDigest, profileIdentity, staleProfile)
	assertPlaceAcquisitionFailure(t, failedRevision, failedFiles, err, "")
	if revisionRequests.Load() != beforeRevisionRequests || archiveRequests.Load() != beforeArchiveRequests {
		t.Fatalf("stale profile issued requests: revision=%d archive=%d", revisionRequests.Load()-beforeRevisionRequests, archiveRequests.Load()-beforeArchiveRequests)
	}

	changedProfile := []byte(strings.Replace(placeAcquireProfileYAML, "evidence: fixture", "evidence: changed fixture", 1))
	changedIdentity := placeAcquireProfileIdentity(changedProfile)
	beforeRevisionRequests = revisionRequests.Load()
	beforeArchiveRequests = archiveRequests.Load()
	failedRevision, failedFiles, err = client.AcquirePlacePackage(context.Background(), coordinate, revision.ContentDigest, changedIdentity, changedProfile)
	assertPlaceAcquisitionFailure(t, failedRevision, failedFiles, err, "marketplace.profile.revision-mismatch")
	if revisionRequests.Load() != beforeRevisionRequests+1 || archiveRequests.Load() != beforeArchiveRequests {
		t.Fatalf("profile mismatch requests: revision=%d archive=%d, want 1/0", revisionRequests.Load()-beforeRevisionRequests, archiveRequests.Load()-beforeArchiveRequests)
	}

	missingBinding := revision
	missingBindingAssessment := missingBinding.Assessment
	missingBindingAssessment.HostPackageIdentity = nil
	missingBinding.Assessment = missingBindingAssessment
	responseRevision.Store(&missingBinding)
	beforeRevisionRequests = revisionRequests.Load()
	beforeArchiveRequests = archiveRequests.Load()
	failedRevision, failedFiles, err = client.AcquirePlacePackage(context.Background(), coordinate, revision.ContentDigest, profileIdentity, profileBytes)
	assertPlaceAcquisitionFailure(t, failedRevision, failedFiles, err, "host package identity is required for scoped resource kind")
	if revisionRequests.Load() != beforeRevisionRequests+1 || archiveRequests.Load() != beforeArchiveRequests {
		t.Fatalf("missing binding requests: revision=%d archive=%d, want 1/0", revisionRequests.Load()-beforeRevisionRequests, archiveRequests.Load()-beforeArchiveRequests)
	}

	mismatchedMapping := revision
	mismatchedAssessment := mismatchedMapping.Assessment
	mismatchedBinding := *mismatchedAssessment.HostPackageIdentity
	mismatchedBinding.DeliveryMappingDigest = "sha256:" + strings.Repeat("c", 64)
	mismatchedAssessment.HostPackageIdentity = &mismatchedBinding
	mismatchedAssessment.AssessmentDigest = ""
	mismatchedMapping.Assessment, err = store.FinalizeResourceAssessment(mismatchedAssessment)
	if err != nil {
		t.Fatalf("finalize changed mapping assessment: %v", err)
	}
	if mismatchedMapping.Assessment.AssessmentDigest == revision.Assessment.AssessmentDigest {
		t.Fatal("mapping-only change retained the prior canonical assessment digest")
	}
	responseRevision.Store(&mismatchedMapping)
	beforeArchiveRequests = archiveRequests.Load()
	failedRevision, failedFiles, err = client.AcquirePlacePackage(context.Background(), coordinate, revision.ContentDigest, profileIdentity, profileBytes)
	assertPlaceAcquisitionFailure(t, failedRevision, failedFiles, err, "place package archive binding does not match assessed revision")
	if archiveRequests.Load() != beforeArchiveRequests+1 {
		t.Fatalf("mapping mismatch archive requests = %d, want 1", archiveRequests.Load()-beforeArchiveRequests)
	}
}

func placeAcquireProfileIdentity(profileBytes []byte) targetprofile.Identity {
	digest := sha256.Sum256(profileBytes)
	return targetprofile.Identity{
		TargetID: "invented-app", SourceID: "builtin:agent-marketplace",
		RevisionDigest: "sha256:" + hex.EncodeToString(digest[:]),
	}
}

func placeAcquireRevision(t *testing.T, coordinate store.ResourceCoordinate, artifact hccartifact.CanonicalArtifact, archiveDigest string) store.AssessedResourceRevision {
	t.Helper()
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: coordinate, ContentDigest: store.RevisionDigest(artifact.Manifest.ContentDigest),
		AnalyzerVersion: "test/v1", PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("a", 64)),
		Decision: store.AssessmentDecisionAllow, InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("b", 64)),
		AssessedAt: time.Unix(1, 0).UTC(), HostPackageIdentity: artifact.Manifest.HostPackageIdentity,
	})
	if err != nil {
		t.Fatalf("finalize place assessment: %v", err)
	}
	return store.AssessedResourceRevision{
		ResourceRevision: store.ResourceRevision{
			Coordinate: coordinate, ContentDigest: store.RevisionDigest(artifact.Manifest.ContentDigest),
			ArchiveDigest: store.ArchiveDigest(archiveDigest),
		},
		Assessment: assessment,
		Occurrence: store.ResourceOccurrence{
			Coordinate: coordinate, ContentDigest: store.RevisionDigest(artifact.Manifest.ContentDigest),
			Commit: strings.Repeat("e", 40), ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("d", 64)),
		},
	}
}

func assertPlaceAcquisitionFailure(t *testing.T, rev store.AssessedResourceRevision, files []targetprofile.PackageFile, err error, want string) {
	t.Helper()
	if err == nil || !reflect.DeepEqual(rev, store.AssessedResourceRevision{}) || files != nil {
		t.Fatalf("failure result = (%+v, %+v, %v), want zero revision and nil files", rev, files, err)
	}
	if want != "" && !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want %q", err, want)
	}
}
