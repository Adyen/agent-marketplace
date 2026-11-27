package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	marketplaceartifact "github.com/Adyen/agent-marketplace/pkg/marketplace/artifact"
	mpconfig "github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

// spec: target-profile-place/dependent-packages-publish-in-order
func TestHostedPlacePrerequisitesOrderPublicationAndDeselection(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Run("live endpoint receives no offline requests", func(t *testing.T) {
		testHostedPlaceOrder(t, true)
	})
	t.Run("cache works with server stopped", func(t *testing.T) {
		testHostedPlaceOrder(t, false)
	})
}

func testHostedPlaceOrder(t *testing.T, keepServerLive bool) {
	t.Helper()
	profileBytes := []byte(strings.Replace(
		targetApproveProfileYAML,
		"kinds: {mod: {deliver: place, into: [mods/]}}",
		"kinds: {mod: {deliver: place, into: [mods/]}, layout: {deliver: place, into: [layouts/]}}",
		1,
	))
	if bytes.Equal(profileBytes, []byte(targetApproveProfileYAML)) {
		t.Fatal("could not extend hosted fixture profile with layout delivery")
	}
	fixture := newTargetApprovalFixtureWithProfile(t, profileBytes)
	const marketplaceID = store.MarketplaceID("market-id")
	const registryID = store.RegistryID("tools-id")

	var requests, archiveRequests atomic.Int32
	server := httptest.NewUnstartedServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	endpoint := "http://" + server.Listener.Addr().String()
	meta := sqlite.New(filepath.Join(t.TempDir(), "marketplace.sqlite"))
	if err := meta.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	blobs := fsblob.New(t.TempDir())
	if err := meta.PutMarketplace(context.Background(), store.Marketplace{
		ID: marketplaceID, Name: "Test", CanonicalURL: endpoint,
	}); err != nil {
		t.Fatal(err)
	}
	registry := store.Registry{
		ID: registryID, Slug: "tools", Ref: "main", Origins: []string{"https://git.example.com/tools"},
		Roots:       []store.RegistryRoot{{Path: "profiles", SourceHarness: "factory"}},
		Acquisition: store.AcquisitionMirror, Listed: true, License: "MIT",
		CreatedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(1, 0).UTC(),
	}
	if err := meta.PutRegistry(context.Background(), registry); err != nil {
		t.Fatal(err)
	}

	packageID := func(kind, name string) string {
		return dependencytxn.Digest([]byte(strings.Join([]string{
			"agent-marketplace/place-package/v1", endpoint, string(marketplaceID), string(registryID), kind, name,
		}, "\x00")))
	}
	layoutName, layoutID := "", ""
	modName, modID := "", ""
	for i := 0; i < 32; i++ {
		layoutCandidate := fmt.Sprintf("sample-layout-%d", i)
		layoutCandidateID := packageID("invented-app/layout", layoutCandidate)
		if layoutName == "" || layoutCandidateID < layoutID {
			layoutName, layoutID = layoutCandidate, layoutCandidateID
		}
		modCandidate := fmt.Sprintf("sample-mod-%d", i)
		modCandidateID := packageID("invented-app/mod", modCandidate)
		if modName == "" || modCandidateID > modID {
			modName, modID = modCandidate, modCandidateID
		}
	}
	if layoutID >= modID {
		t.Fatalf("fixture package IDs are not ordered to expose dependency ordering: layout=%s mod=%s", layoutID, modID)
	}

	profileCoordinate := store.ResourceCoordinate{
		Marketplace: marketplaceID, Registry: registryID, Kind: "target-profile", Name: fixture.identity.TargetID,
	}
	profileProjection, err := inspection.BuildTargetProfile(
		profileCoordinate, fixture.identity, fixture.profileBytes, 1,
		targetprofile.MaxProfileBytes, targetprofile.MaxProfileBytes,
	)
	if err != nil {
		t.Fatalf("build hosted profile inspection: %v", err)
	}
	profileRevision := hostedLifecycleRevision(
		t,
		store.ResourceRevision{
			Coordinate: profileCoordinate, ContentDigest: store.RevisionDigest(fixture.identity.RevisionDigest),
		},
		nil,
		inspection.Digest(profileProjection.Payload),
	)

	modBytes := []byte("base mod payload\n")
	modFile := targetprofile.PackageFile{
		SourcePath: "payload/base.md", DestinationPath: filepath.ToSlash(filepath.Join("mods", modName, "guide.md")),
		Mode: 0o644, Bytes: modBytes,
	}
	layoutBytes := []byte("dependent layout payload\n")
	layoutFile := targetprofile.PackageFile{
		SourcePath: "payload/layout.yaml", DestinationPath: filepath.ToSlash(filepath.Join("layouts", layoutName+".yaml")),
		Mode: 0o644, Bytes: layoutBytes,
	}
	buildPackage := func(kind, name string, file targetprofile.PackageFile, requires string) (store.AssessedResourceRevision, []byte, inspection.Projection) {
		t.Helper()
		artifact, err := hccartifact.NewPlaceArtifact(kind, name, fixture.identity, []targetprofile.PackageFile{file})
		if err != nil {
			t.Fatalf("build hosted %s/%s artifact: %v", kind, name, err)
		}
		finalArchive, _, archiveDigest, err := hccartifact.EncodeArchives(artifact)
		if err != nil {
			t.Fatalf("encode hosted %s/%s artifact: %v", kind, name, err)
		}
		if artifact.Manifest.HostPackageIdentity == nil {
			t.Fatalf("hosted %s/%s artifact has no package binding", kind, name)
		}
		coordinate := store.ResourceCoordinate{
			Marketplace: marketplaceID, Registry: registryID, Kind: kind, Name: name,
		}
		projection, err := inspection.Build(coordinate, artifact.Manifest.ContentDigest, []byte(`{}`), nil, 8, 1024*1024, 2*1024*1024)
		if err != nil {
			t.Fatalf("build hosted %s/%s inspection: %v", kind, name, err)
		}
		revision := hostedLifecycleRevision(
			t,
			store.ResourceRevision{
				Coordinate: coordinate, ContentDigest: store.RevisionDigest(artifact.Manifest.ContentDigest),
				ArchiveDigest: store.ArchiveDigest(archiveDigest), Requires: requires,
			},
			artifact.Manifest.HostPackageIdentity,
			inspection.Digest(projection.Payload),
		)
		return revision, finalArchive, projection
	}
	modRevision, modArchive, modInspection := buildPackage("invented-app/mod", modName, modFile, "[]")
	layoutRequires := fmt.Sprintf(`[{"kind":"invented-app/mod","name":%q}]`, modName)
	layoutRevision, layoutArchive, layoutInspection := buildPackage("invented-app/layout", layoutName, layoutFile, layoutRequires)
	modDigest := string(modRevision.ContentDigest)
	layoutDigest := string(layoutRevision.ContentDigest)
	publishHostedPlaceFixture(
		t, meta, blobs, registry, profileRevision, modRevision, layoutRevision,
		profileProjection.Payload, fixture.profileBytes, modArchive, layoutArchive,
		modInspection, layoutInspection,
	)
	persisted, err := meta.GetResourceRevision(context.Background(), layoutRevision.Coordinate, layoutRevision.ContentDigest)
	if err != nil || persisted.Requires != layoutRequires {
		t.Fatalf("persisted layout prerequisites = %q, %v; want exact %q", persisted.Requires, err, layoutRequires)
	}
	apiServer := mpapi.NewServer(mpconfig.Config{
		Instance: mpconfig.Instance{ID: string(marketplaceID), Name: "Test", CanonicalURL: endpoint},
	}, meta, blobs)
	publicHandler := apiServer.PublicHandler()
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if strings.HasPrefix(r.URL.Path, "/api/v1/archives/") {
			archiveRequests.Add(1)
		}
		publicHandler.ServeHTTP(w, r)
	})
	server.Start()
	if server.URL != endpoint {
		t.Fatalf("hosted fixture endpoint = %q, listener-derived endpoint = %q", server.URL, endpoint)
	}

	userConfig := fmt.Sprintf(`apiVersion: agent-marketplace/v4
targets: [invented-app]
sources:
  hosted: {marketplace: %q}
resources:
  hosted:
    include:
      - tools/target-profile/invented-app: {revision: %s}
      - tools/%s/%s: {revision: %s}
      - tools/%s/%s: {revision: %s}
`, endpoint, fixture.identity.RevisionDigest, url.PathEscape("invented-app/layout"), layoutName, layoutDigest,
		url.PathEscape("invented-app/mod"), modName, modDigest)
	if err := os.WriteFile(fixture.userConfig, []byte(userConfig), 0o600); err != nil {
		t.Fatalf("write hosted place configuration: %v", err)
	}
	if err := os.Remove(fixture.userLock); err != nil {
		t.Fatalf("remove fixture's initial user lock: %v", err)
	}
	output, err := executeCommand("sync")
	if err != nil || !strings.Contains(output, "target.approval.required") {
		t.Fatalf("initial hosted sync = %v\n%s; want pending approval", err, output)
	}
	if archiveRequests.Load() != 0 {
		t.Fatalf("pending hosted sync fetched %d package archives, want none", archiveRequests.Load())
	}
	for _, destination := range []string{modFile.DestinationPath, layoutFile.DestinationPath} {
		if _, err := os.Lstat(filepath.Join(fixture.targetRoot, filepath.FromSlash(destination))); !os.IsNotExist(err) {
			t.Fatalf("pending sync wrote %s: %v", destination, err)
		}
	}

	ctx := context.Background()
	workspace := mustTargetWorkspace(t, fixture)
	binding, err := pipeline.PrepareDeployedPlaceApproval(ctx, workspace, fixture.identity.TargetID)
	if err != nil {
		t.Fatalf("prepare hosted place binding: %v", err)
	}
	if binding.CanonicalRoot != fixture.targetRoot {
		t.Fatalf("initial target root = %q, want %q", binding.CanonicalRoot, fixture.targetRoot)
	}
	if err := targetprofile.RecordApproval(binding); err != nil {
		t.Fatalf("record initial place approval: %v", err)
	}
	archivesBefore := archiveRequests.Load()
	online := runHostedPlacePipelineSync(t, fixture, false)
	assertHostedPlaceChangedOrder(t, online, fixture.identity.TargetID,
		[]string{modFile.DestinationPath, layoutFile.DestinationPath}, "online publication")
	if got := archiveRequests.Load() - archivesBefore; got != 2 {
		t.Fatalf("online place sync fetched %d package archives, want mod and layout", got)
	}
	assertHostedPlacePackages(
		t, binding.CanonicalRoot, binding.CanonicalStateRoot, modName, layoutName, modBytes, layoutBytes, modID, layoutID,
	)

	if !keepServerLive {
		server.Close()
	}
	requestsBeforeOffline := requests.Load()

	secondRoot := filepath.Join(fixture.home, ".config", "invented-app-second")
	if err := os.MkdirAll(secondRoot, 0o700); err != nil {
		t.Fatalf("create second fixture target root: %v", err)
	}
	secondRoot, err = filepath.EvalSymlinks(secondRoot)
	if err != nil {
		t.Fatalf("canonicalize second fixture target root: %v", err)
	}
	t.Setenv("INVENTED_APP_HOME", secondRoot)
	secondBinding, err := pipeline.PrepareDeployedPlaceApproval(ctx, workspace, fixture.identity.TargetID)
	if err != nil {
		t.Fatalf("prepare cached hosted place binding: %v", err)
	}
	if secondBinding.CanonicalRoot != secondRoot || secondBinding.CanonicalStateRoot != binding.CanonicalStateRoot {
		t.Fatalf("cached binding roots = %q / %q; want %q / %q",
			secondBinding.CanonicalRoot, secondBinding.CanonicalStateRoot, secondRoot, binding.CanonicalStateRoot)
	}
	if err := targetprofile.RecordApproval(secondBinding); err != nil {
		t.Fatalf("record cached place approval: %v", err)
	}
	cached := runHostedPlacePipelineSync(t, fixture, true)
	assertHostedPlaceChangedOrder(t, cached, fixture.identity.TargetID,
		[]string{modFile.DestinationPath, layoutFile.DestinationPath}, "offline cached publication")
	assertHostedPlacePackages(
		t, secondBinding.CanonicalRoot, secondBinding.CanonicalStateRoot,
		modName, layoutName, modBytes, layoutBytes, modID, layoutID,
	)
	if got := requests.Load(); keepServerLive && got != requestsBeforeOffline {
		t.Fatalf("offline cached publication made hosted requests: before=%d after=%d", requestsBeforeOffline, got)
	}

	profileOnlyConfig := fmt.Sprintf(`apiVersion: agent-marketplace/v4
targets: [invented-app]
sources:
  hosted: {marketplace: %q}
resources:
  hosted:
    include:
      - tools/target-profile/invented-app: {revision: %s}
`, endpoint, fixture.identity.RevisionDigest)
	if err := os.WriteFile(fixture.userConfig, []byte(profileOnlyConfig), 0o600); err != nil {
		t.Fatalf("write profile-only hosted configuration: %v", err)
	}
	removeHostedScopedPackageLocks(t, fixture.userLock)
	deselected := runHostedPlacePipelineSync(t, fixture, true)
	assertHostedPlaceChangedOrder(t, deselected, fixture.identity.TargetID,
		[]string{layoutFile.DestinationPath, modFile.DestinationPath}, "offline deselection")
	assertHostedPlaceEmpty(t, secondBinding.CanonicalRoot, secondBinding.CanonicalStateRoot)
	for _, destination := range []string{modFile.DestinationPath, layoutFile.DestinationPath} {
		if _, err := os.Lstat(filepath.Join(secondBinding.CanonicalRoot, filepath.FromSlash(destination))); !os.IsNotExist(err) {
			t.Fatalf("deselected package path %s remains: %v", destination, err)
		}
	}
	assertHostedPlacePackages(
		t, binding.CanonicalRoot, binding.CanonicalStateRoot, modName, layoutName, modBytes, layoutBytes, modID, layoutID,
	)
	if got := requests.Load(); keepServerLive && got != requestsBeforeOffline {
		t.Fatalf("offline deselection made hosted requests: before=%d after=%d", requestsBeforeOffline, got)
	}
}

func runHostedPlacePipelineSync(t *testing.T, fixture targetApprovalFixture, offline bool) pipeline.SyncResult {
	t.Helper()
	ctx := context.Background()
	workspace := mustTargetWorkspace(t, fixture)
	mode := lockMode{}
	if offline {
		mode = lockMode{offline: true, locked: true}
	}
	exactOption, _, lease, err := prepareRuntimeLocks(ctx, workspace.RepoRoot(), workspace.LocalConfigDir(), mode)
	if err != nil {
		t.Fatalf("prepare exact locks for offline=%t: %v", offline, err)
	}
	result := pipeline.Sync(ctx, pipeline.ResolveOptions{
		Workspace: workspace, EnablePlaceSync: true, NoBackup: true, Offline: offline,
		CatalogOption: []catalog.LoadOption{exactOption}, ExactLockLease: lease,
	})
	if len(result.Errors) != 0 {
		t.Errorf("pipeline sync offline=%t errors: %v", offline, result.Errors)
	}
	return result
}

func assertHostedPlaceChangedOrder(t *testing.T, result pipeline.SyncResult, targetID string, want []string, phase string) {
	t.Helper()
	committed, ok := result.PlaceSync.Committed[targetID]
	if !ok {
		t.Errorf("%s has no committed place result for %s: %+v", phase, targetID, result.PlaceSync)
		return
	}
	var got []string
	for _, changed := range committed.Changed {
		if slices.Contains(want, changed) {
			got = append(got, changed)
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("%s changed paths = %v; want package file order %v (all changes %v)", phase, got, want, committed.Changed)
	}
}

func assertHostedPlacePackages(
	t *testing.T,
	root, stateRoot, modName, layoutName string,
	modBytes, layoutBytes []byte,
	modID, layoutID string,
) {
	t.Helper()
	owner, err := ownership.Open(root, ownership.WithStateRoot(stateRoot))
	if err != nil {
		t.Errorf("open hosted place ownership for %s: %v", root, err)
		return
	}
	snapshot, err := owner.SnapshotState()
	if err != nil {
		t.Errorf("snapshot hosted place ownership for %s: %v", root, err)
		return
	}
	projection, err := owner.PlaceFromSnapshot(snapshot)
	if err != nil {
		t.Errorf("read hosted place projection for %s: %v", root, err)
		return
	}
	var mod, layout *ownership.PlacePackage
	for i := range projection.Packages {
		switch projection.Packages[i].Kind {
		case "invented-app/mod":
			mod = &projection.Packages[i]
		case "invented-app/layout":
			layout = &projection.Packages[i]
		}
	}
	if len(projection.Packages) != 2 || mod == nil || layout == nil ||
		mod.Name != modName || mod.ID != modID || layout.Name != layoutName || layout.ID != layoutID ||
		len(layout.Requires) != 1 || layout.Requires[0] != mod.ID {
		t.Errorf("hosted place projection at %s = %#v; want exact layout -> mod dependency", root, projection)
	}
	for _, file := range []struct {
		path string
		want []byte
	}{
		{path: filepath.Join(root, "mods", modName, "guide.md"), want: modBytes},
		{path: filepath.Join(root, "layouts", layoutName+".yaml"), want: layoutBytes},
	} {
		data, err := os.ReadFile(file.path)
		if err != nil || !bytes.Equal(data, file.want) {
			t.Errorf("hosted place file %s = %q, %v; want %q", file.path, data, err, file.want)
		}
	}
}

func assertHostedPlaceEmpty(t *testing.T, root, stateRoot string) {
	t.Helper()
	owner, err := ownership.Open(root, ownership.WithStateRoot(stateRoot))
	if err != nil {
		t.Errorf("open deselected place ownership for %s: %v", root, err)
		return
	}
	snapshot, err := owner.SnapshotState()
	if err != nil {
		t.Errorf("snapshot deselected place ownership for %s: %v", root, err)
		return
	}
	projection, err := owner.PlaceFromSnapshot(snapshot)
	if err != nil || len(projection.Packages) != 0 {
		t.Errorf("deselected place projection at %s = %#v, %v; want no packages", root, projection, err)
	}
}

func publishHostedPlaceFixture(
	t *testing.T,
	meta *sqlite.Store,
	blobs store.BlobStore,
	registry store.Registry,
	profile, mod, layout store.AssessedResourceRevision,
	profileInspection []byte,
	profileBytes, modArchive, layoutArchive []byte,
	modProjection, layoutProjection inspection.Projection,
) {
	t.Helper()
	const commit = "2222222222222222222222222222222222222222"
	resources := []store.ResourceRevision{profile.ResourceRevision, mod.ResourceRevision, layout.ResourceRevision}
	revisionManifest := marketplaceartifact.RegistryRevisionManifest{
		SchemaVersion: marketplaceartifact.RegistryRevisionSchemaV1,
		Generator:     "hosted Place prerequisite order fixture",
	}
	occurrences := make([]store.ResourceOccurrence, 0, len(resources))
	assessments := []store.ResourceAssessment{profile.Assessment, mod.Assessment, layout.Assessment}
	for _, revision := range resources {
		revisionManifest.Resources = append(revisionManifest.Resources, marketplaceartifact.RevisionResource{
			Kind: revision.Coordinate.Kind, Name: revision.Coordinate.Name,
			ContentDigest: string(revision.ContentDigest), ArchiveDigest: string(revision.ArchiveDigest),
		})
		occurrences = append(occurrences, store.ResourceOccurrence{
			Coordinate: revision.Coordinate, ContentDigest: revision.ContentDigest,
			Commit: commit, SourcePath: "profiles/" + revision.Coordinate.Name,
		})
	}
	payload, digest, err := marketplaceartifact.CanonicalRegistryRevisionPayload(revisionManifest)
	if err != nil {
		t.Fatalf("encode hosted registry revision manifest: %v", err)
	}
	manifestDigest := store.RevisionDigest(digest)
	for i := range occurrences {
		occurrences[i].ManifestDigest = manifestDigest
	}
	occurrence := store.RegistryOccurrence{
		RegistryID: registry.ID, Ref: registry.Ref, Roots: registry.Roots, Commit: commit,
		RegistryRevisionManifestDigest: manifestDigest, Acquisition: registry.Acquisition,
	}
	inspectionBlobs := []store.BlobUpload{
		{Digest: store.ArchiveDigest(profile.Assessment.InspectionManifestDigest), Data: profileInspection},
		{Digest: store.ArchiveDigest(inspection.Digest(profileBytes)), Data: profileBytes},
	}
	addProjection := func(revision store.AssessedResourceRevision, projection inspection.Projection) {
		inspectionBlobs = append(inspectionBlobs,
			store.BlobUpload{Digest: store.ArchiveDigest(revision.Assessment.InspectionManifestDigest), Data: projection.Payload})
		for _, body := range projection.Bodies {
			inspectionBlobs = append(inspectionBlobs, store.BlobUpload{
				Digest: store.ArchiveDigest(inspection.Digest(body)), Data: body,
			})
		}
	}
	addProjection(mod, modProjection)
	addProjection(layout, layoutProjection)
	if err := store.PublishRegistryRevision(context.Background(), meta, blobs, store.RegistryRevisionPublication{
		Occurrence: occurrence, Resources: resources, ResourceOccurrences: occurrences,
		Assessments: assessments, ManifestPayload: payload,
		ExtraBlobs: []store.BlobUpload{
			{Digest: mod.ArchiveDigest, Data: modArchive},
			{Digest: layout.ArchiveDigest, Data: layoutArchive},
			{Digest: store.ArchiveDigest(profile.ContentDigest), Data: profileBytes},
		},
		InspectionBlobs: inspectionBlobs,
	}); err != nil {
		t.Fatalf("publish hosted Place registry revision: %v", err)
	}
}

func removeHostedScopedPackageLocks(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read hosted lock before deselection: %v", err)
	}
	document, err := lockfile.Decode(raw)
	if err != nil {
		t.Fatalf("decode hosted lock before deselection: %v", err)
	}
	removed := 0
	for i := range document.Marketplaces {
		resources := document.Marketplaces[i].Resources[:0]
		for _, resource := range document.Marketplaces[i].Resources {
			if resource.Kind == "invented-app/mod" || resource.Kind == "invented-app/layout" {
				removed++
				continue
			}
			resources = append(resources, resource)
		}
		document.Marketplaces[i].Resources = resources
	}
	if removed != 2 {
		t.Fatalf("removed %d scoped lock resources, want mod and layout", removed)
	}
	encoded, err := lockfile.Encode(document)
	if err != nil {
		t.Fatalf("encode profile-only hosted lock: %v", err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("write profile-only hosted lock: %v", err)
	}
}
