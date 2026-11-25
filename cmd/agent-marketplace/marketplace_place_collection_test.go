package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// spec: target-profile-place/collections-keep-and-report-skipped-members
func TestMarketplacePlaceCollectionSkipsUnavailableMember(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	fixture := newTargetApprovalFixture(t)
	profileCoordinate := store.ResourceCoordinate{
		Marketplace: "market-id", Registry: "tools-id", Kind: "target-profile", Name: fixture.identity.TargetID,
	}
	profileProjection, err := inspection.BuildTargetProfile(
		profileCoordinate, fixture.identity, fixture.profileBytes, 1,
		targetprofile.MaxProfileBytes, targetprofile.MaxProfileBytes,
	)
	if err != nil {
		t.Fatal(err)
	}
	profileRevision := hostedLifecycleRevision(t, store.ResourceRevision{
		Coordinate: profileCoordinate, ContentDigest: store.RevisionDigest(fixture.identity.RevisionDigest),
	}, nil, inspection.Digest(profileProjection.Payload))

	files := []targetprofile.PackageFile{{
		SourcePath: "payload/mod.md", DestinationPath: "mods/sample.md", Mode: 0o644, Bytes: []byte("scoped leaf\n"),
	}}
	artifact, err := hccartifact.NewPlaceArtifact("invented-app/mod", "sample", fixture.identity, files)
	if err != nil {
		t.Fatal(err)
	}
	_, placeholderArchive, archiveDigest, err := hccartifact.EncodeArchives(artifact)
	if err != nil {
		t.Fatal(err)
	}
	binding := artifact.Manifest.HostPackageIdentity
	packageRevision := hostedLifecycleRevision(t, store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "market-id", Registry: "tools-id", Kind: "invented-app/mod", Name: "sample"},
		ContentDigest: store.RevisionDigest(artifact.Manifest.ContentDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest),
	}, binding, "sha256:"+strings.Repeat("d", 64))
	member := hccartifact.CollectionMemberEntry{
		Kind: "invented-app/mod", Name: "sample", RegistryID: "tools-id",
		Revision: string(packageRevision.ContentDigest),
	}
	leaf := member
	leaf.InclusionPaths = [][]string{{"bundle"}}
	manifestPayload, manifestDigest, err := hccartifact.EncodeCollectionManifest(hccartifact.CollectionManifest{
		SchemaVersion: hccartifact.CollectionSchemaV1, Name: "bundle",
		Members: []hccartifact.CollectionMemberEntry{member}, Leaves: []hccartifact.CollectionMemberEntry{leaf},
	})
	if err != nil {
		t.Fatal(err)
	}
	collectionRevision := hostedLifecycleRevision(t, store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "market-id", Registry: "tools-id", Kind: "collection", Name: "bundle"},
		ContentDigest: store.RevisionDigest(manifestDigest),
	}, nil, "sha256:"+strings.Repeat("e", 64))

	var manifestRequests, scopedRevisionRequests, archiveRequests atomic.Int32
	archivePath := "/api/v1/archives/sha256/" + strings.TrimPrefix(archiveDigest, "sha256:")
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("market-id", server.URL))
		case "/api/v1/registries/tools", "/api/v1/registries/tools-id":
			_ = json.NewEncoder(w).Encode(mpapi.RegistryDTO{
				ID: "tools-id", Slug: "tools", Origins: []string{"https://git.example/tools"},
				Ref: "main", Roots: []store.RegistryRoot{{Path: "profiles"}},
				Acquisition: store.AcquisitionMirror, Listed: true,
			})
		case "/api/v1/resources/tools-id/target-profile/invented-app/revisions/" + fixture.identity.RevisionDigest:
			assertHostedProfileFeature(t, r)
			writeHostedLifecycleRevision(w, profileRevision)
		case "/api/v1/resources/tools-id/target-profile/invented-app/revisions/" + fixture.identity.RevisionDigest + "/inspection":
			assertHostedProfileFeature(t, r)
			_, _ = w.Write(profileProjection.Payload)
		case "/api/v1/resources/tools-id/target-profile/invented-app/revisions/" + fixture.identity.RevisionDigest + "/inspection/content":
			assertHostedProfileFeature(t, r)
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write(fixture.profileBytes)
		case "/api/v1/resources/tools/collection/bundle/revisions/" + manifestDigest,
			"/api/v1/resources/tools-id/collection/bundle/revisions/" + manifestDigest:
			writeHostedLifecycleRevision(w, collectionRevision)
		case "/api/v1/manifests/" + manifestDigest:
			manifestRequests.Add(1)
			assertHostedProfileFeature(t, r)
			_, _ = w.Write(manifestPayload)
		case "/api/v1/resources/tools-id/" + url.PathEscape("invented-app/mod") + "/sample/revisions/" + string(packageRevision.ContentDigest):
			scopedRevisionRequests.Add(1)
			assertHostedProfileFeature(t, r)
			writeHostedLifecycleRevision(w, packageRevision)
		case archivePath:
			archiveRequests.Add(1)
			_, _ = w.Write(placeholderArchive)
		default:
			if strings.HasPrefix(r.URL.EscapedPath(), "/api/v1/archives/") {
				archiveRequests.Add(1)
			}
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configBytes := fmt.Sprintf(`apiVersion: agent-marketplace/v4
targets: [factory, invented-app]
sources:
  profile: {git: %q, ref: main, path: profiles, source_harness: factory}
  hosted: {marketplace: %q}
resources:
  hosted:
    include:
      - tools/target-profile/invented-app: {revision: %s}
      - tools/collection/bundle: {revision: %s}
`, fixture.rawGitURL, server.URL, fixture.identity.RevisionDigest, manifestDigest)
	if err := os.WriteFile(fixture.userConfig, []byte(configBytes), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(fixture.userLock); err != nil {
		t.Fatal(err)
	}
	workspace := mustTargetWorkspace(t, fixture)
	_, _, _, err = prepareRuntimeLocks(context.Background(), workspace.RepoRoot(), workspace.LocalConfigDir(), lockMode{})
	if err != nil {
		t.Fatalf("hosted collection lock bootstrap: %v", err)
	}
	if manifestRequests.Load() != 1 || scopedRevisionRequests.Load() != 1 {
		t.Fatalf("hosted metadata requests: manifest=%d scoped revision=%d, want one each", manifestRequests.Load(), scopedRevisionRequests.Load())
	}
	if archiveRequests.Load() != 0 {
		t.Fatalf("metadata-only collection bootstrap fetched %d archives", archiveRequests.Load())
	}
	rawLock, err := os.ReadFile(fixture.userLock)
	if err != nil {
		t.Fatal(err)
	}
	document, err := lockfile.Decode(rawLock)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Marketplaces) != 1 {
		t.Fatalf("marketplace locks = %#v", document.Marketplaces)
	}
	var collection, profile, scoped *lockfile.MarketplaceResourceLock
	for i := range document.Marketplaces[0].Resources {
		record := &document.Marketplaces[0].Resources[i]
		switch record.Kind {
		case "collection":
			collection = record
		case "target-profile":
			profile = record
		case "invented-app/mod":
			scoped = record
		}
	}
	if collection == nil || collection.CollectionManifestDigest != lockfile.Digest(manifestDigest) ||
		profile == nil || profile.ProfileIdentity == nil || *profile.ProfileIdentity != fixture.identity {
		t.Fatalf("collection/profile lock records = collection:%+v profile:%+v", collection, profile)
	}
	if scoped == nil || scoped.Revision != string(packageRevision.ContentDigest) ||
		scoped.ContentDigest != lockfile.Digest(packageRevision.ContentDigest) ||
		scoped.AssessmentDigest != lockfile.Digest(packageRevision.Assessment.AssessmentDigest) ||
		scoped.ArchiveDigest != lockfile.Digest(archiveDigest) || scoped.HostPackageIdentity == nil ||
		*scoped.HostPackageIdentity != *binding ||
		len(scoped.SelectedBy) != 1 || scoped.SelectedBy[0] != (lockfile.MarketplaceSelectionLock{
		RegistryID: "tools-id", Name: "bundle", Revision: lockfile.Digest(manifestDigest),
	}) {
		t.Fatalf("scoped collection lock record = %+v", scoped)
	}

	lockedConfig, err := os.ReadFile(fixture.userConfig)
	if err != nil {
		t.Fatal(err)
	}
	lockedBytes, err := os.ReadFile(fixture.userLock)
	if err != nil {
		t.Fatal(err)
	}
	targetBefore := snapshotTargetStateTree(t, fixture.targetRoot)
	stateBefore := snapshotTargetStateTree(t, fixture.stateRoot)
	missingRoot := filepath.Join(fixture.targetRoot, "missing")
	t.Setenv("INVENTED_APP_HOME", missingRoot)
	output, err := executeCommand("sync")
	if err != nil || !strings.Contains(output, "target.place.collection-member-skipped") ||
		!strings.Contains(output, "invented-app/mod/sample") || !strings.Contains(output, "bundle") ||
		!strings.Contains(output, "target.approval.root-unavailable") {
		t.Fatalf("sync with unavailable collection target = %v\n%s; want collection skip and target.approval.root-unavailable", err, output)
	}
	assertTargetStateTreeUnchanged(t, targetBefore, fixture.targetRoot)
	assertTargetStateTreeUnchanged(t, stateBefore, fixture.stateRoot)
	if _, err := os.Stat(missingRoot); !os.IsNotExist(err) {
		t.Fatalf("unavailable target root stat error = %v, want absent", err)
	}
	if _, err := os.Stat(filepath.Join(missingRoot, "mods", "sample.md")); !os.IsNotExist(err) {
		t.Fatalf("unavailable collection destination stat error = %v, want absent", err)
	}
	assertCollectionLockConfigUnchanged(t, fixture, lockedConfig, lockedBytes)
	if archiveRequests.Load() != 0 || scopedRevisionRequests.Load() != 1 {
		t.Fatalf("unavailable target acquired package data: archives=%d scoped revisions=%d", archiveRequests.Load(), scopedRevisionRequests.Load())
	}

	output, err = executeCommand("sync", "--check", "--offline")
	if err != nil || !strings.Contains(output, "target.place.collection-member-skipped") ||
		!strings.Contains(output, "target.approval.root-unavailable") {
		t.Fatalf("offline sync check with unavailable collection target = %v\n%s; want the same collection skip diagnostic", err, output)
	}
	assertTargetStateTreeUnchanged(t, targetBefore, fixture.targetRoot)
	assertTargetStateTreeUnchanged(t, stateBefore, fixture.stateRoot)
	assertCollectionLockConfigUnchanged(t, fixture, lockedConfig, lockedBytes)
	if archiveRequests.Load() != 0 || scopedRevisionRequests.Load() != 1 {
		t.Fatalf("offline check acquired package data: archives=%d scoped revisions=%d", archiveRequests.Load(), scopedRevisionRequests.Load())
	}

	t.Setenv("INVENTED_APP_HOME", fixture.targetRoot)
	approvalBinding, err := pipeline.PrepareDeployedPlaceApproval(context.Background(), mustTargetWorkspace(t, fixture), fixture.identity.TargetID)
	if err != nil {
		t.Fatalf("prepare approval for positive collection control: %v", err)
	}
	if err := targetprofile.RecordApproval(approvalBinding); err != nil {
		t.Fatalf("record approval for positive collection control: %v", err)
	}
	output, err = executeCommand("sync")
	if err != nil {
		t.Fatalf("sync approved collection member: %v\n%s", err, output)
	}
	if archiveRequests.Load() != 1 {
		t.Fatalf("approved collection fetched %d archives, want exactly one", archiveRequests.Load())
	}
	installed, err := os.ReadFile(filepath.Join(fixture.targetRoot, "mods", "sample.md"))
	if err != nil || string(installed) != string(files[0].Bytes) {
		t.Fatalf("approved collection installed bytes = %q, err=%v; want %q", installed, err, files[0].Bytes)
	}
	assertCollectionLockConfigUnchanged(t, fixture, lockedConfig, lockedBytes)

	installedTarget := snapshotTargetStateTree(t, fixture.targetRoot)
	installedState := snapshotTargetStateTree(t, fixture.stateRoot)
	scopedBeforeOffline := scopedRevisionRequests.Load()
	archivesBeforeOffline := archiveRequests.Load()
	missingRoot = filepath.Join(fixture.targetRoot, "missing")
	t.Setenv("INVENTED_APP_HOME", missingRoot)
	output, err = executeCommand("sync", "--offline")
	if err != nil || !strings.Contains(output, "target.place.collection-member-skipped") ||
		!strings.Contains(output, "target.approval.root-unavailable") {
		t.Fatalf("offline sync with unavailable collection target = %v\n%s; want collection skip and target.approval.root-unavailable", err, output)
	}
	assertTargetStateTreeUnchanged(t, installedTarget, fixture.targetRoot)
	assertTargetStateTreeUnchanged(t, installedState, fixture.stateRoot)
	assertCollectionLockConfigUnchanged(t, fixture, lockedConfig, lockedBytes)
	if archiveRequests.Load() != archivesBeforeOffline || scopedRevisionRequests.Load() != scopedBeforeOffline {
		t.Fatalf("offline sync acquired package data: archives=%d (before %d), scoped revisions=%d (before %d)",
			archiveRequests.Load(), archivesBeforeOffline, scopedRevisionRequests.Load(), scopedBeforeOffline)
	}
	if _, err := os.Stat(missingRoot); !os.IsNotExist(err) {
		t.Fatalf("offline sync target root stat error = %v, want absent", err)
	}
}

func assertCollectionLockConfigUnchanged(t *testing.T, fixture targetApprovalFixture, configBytes, lockBytes []byte) {
	t.Helper()
	gotConfig, err := os.ReadFile(fixture.userConfig)
	if err != nil || string(gotConfig) != string(configBytes) {
		t.Fatalf("collection user config changed: err=%v", err)
	}
	gotLock, err := os.ReadFile(fixture.userLock)
	if err != nil || string(gotLock) != string(lockBytes) {
		t.Fatalf("collection user lock changed: err=%v", err)
	}
}
