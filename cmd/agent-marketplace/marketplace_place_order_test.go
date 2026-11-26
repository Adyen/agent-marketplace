package main

import (
	"bytes"
	"context"
	"encoding/json"
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

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
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

	routes := make(map[string]http.HandlerFunc)
	var requests, archiveRequests atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if route, ok := routes[r.URL.EscapedPath()]; ok {
			route(w, r)
			return
		}
		t.Errorf("unexpected hosted route %q", r.URL.EscapedPath())
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	endpoint := "http://" + server.Listener.Addr().String()
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
	buildPackage := func(kind, name string, file targetprofile.PackageFile, requires string) (store.AssessedResourceRevision, []byte) {
		t.Helper()
		artifact, err := hccartifact.NewPlaceArtifact(kind, name, fixture.identity, []targetprofile.PackageFile{file})
		if err != nil {
			t.Fatalf("build hosted %s/%s artifact: %v", kind, name, err)
		}
		_, placeholder, archiveDigest, err := hccartifact.EncodeArchives(artifact)
		if err != nil {
			t.Fatalf("encode hosted %s/%s artifact: %v", kind, name, err)
		}
		if artifact.Manifest.HostPackageIdentity == nil {
			t.Fatalf("hosted %s/%s artifact has no package binding", kind, name)
		}
		coordinate := store.ResourceCoordinate{
			Marketplace: marketplaceID, Registry: registryID, Kind: kind, Name: name,
		}
		revision := hostedLifecycleRevision(
			t,
			store.ResourceRevision{
				Coordinate: coordinate, ContentDigest: store.RevisionDigest(artifact.Manifest.ContentDigest),
				ArchiveDigest: store.ArchiveDigest(archiveDigest), Requires: requires,
			},
			artifact.Manifest.HostPackageIdentity,
			inspection.Digest(placeholder),
		)
		return revision, placeholder
	}
	modRevision, modPlaceholder := buildPackage("invented-app/mod", modName, modFile, "")
	layoutRequires := fmt.Sprintf(`[{"kind":"invented-app/mod","name":%q}]`, modName)
	layoutRevision, layoutPlaceholder := buildPackage("invented-app/layout", layoutName, layoutFile, layoutRequires)
	modDigest := string(modRevision.ContentDigest)
	layoutDigest := string(layoutRevision.ContentDigest)
	profileRevisionPath := "/api/v1/resources/tools-id/target-profile/" + fixture.identity.TargetID +
		"/revisions/" + fixture.identity.RevisionDigest
	modRevisionPath := "/api/v1/resources/tools-id/" + url.PathEscape("invented-app/mod") +
		"/" + modName + "/revisions/" + modDigest
	layoutRevisionPath := "/api/v1/resources/tools-id/" + url.PathEscape("invented-app/layout") +
		"/" + layoutName + "/revisions/" + layoutDigest
	modArchivePath := "/api/v1/archives/sha256/" + strings.TrimPrefix(string(modRevision.ArchiveDigest), "sha256:")
	layoutArchivePath := "/api/v1/archives/sha256/" + strings.TrimPrefix(string(layoutRevision.ArchiveDigest), "sha256:")

	routes["/.well-known/agent-marketplace.json"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown(string(marketplaceID), endpoint))
	}
	routes["/api/v1/registries/tools"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mpapi.RegistryDTO{
			ID: registryID, Slug: "tools", Origins: []string{"https://git.example.com/tools"},
			Ref: "main", Roots: []store.RegistryRoot{{Path: "profiles"}},
			Acquisition: store.AcquisitionMirror, Listed: true,
		})
	}
	routes[profileRevisionPath] = func(w http.ResponseWriter, r *http.Request) {
		assertHostedProfileFeature(t, r)
		writeHostedLifecycleRevision(w, profileRevision)
	}
	profileInspectionPath := profileRevisionPath + "/inspection"
	routes[profileInspectionPath] = func(w http.ResponseWriter, r *http.Request) {
		assertHostedProfileFeature(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(profileProjection.Payload)
	}
	profileContentPath := profileInspectionPath + "/content"
	routes[profileContentPath] = func(w http.ResponseWriter, r *http.Request) {
		assertHostedProfileFeature(t, r)
		w.Header().Set("Content-Type", inspection.MediaYAML)
		_, _ = w.Write(fixture.profileBytes)
	}
	routes[modRevisionPath] = func(w http.ResponseWriter, r *http.Request) {
		assertHostedProfileFeature(t, r)
		writeHostedLifecycleRevision(w, modRevision)
	}
	routes[layoutRevisionPath] = func(w http.ResponseWriter, r *http.Request) {
		assertHostedProfileFeature(t, r)
		writeHostedLifecycleRevision(w, layoutRevision)
	}
	routes[modArchivePath] = func(w http.ResponseWriter, _ *http.Request) {
		archiveRequests.Add(1)
		_, _ = w.Write(modPlaceholder)
	}
	routes[layoutArchivePath] = func(w http.ResponseWriter, _ *http.Request) {
		archiveRequests.Add(1)
		_, _ = w.Write(layoutPlaceholder)
	}
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
		t.Fatalf("pipeline sync offline=%t errors: %v", offline, result.Errors)
	}
	return result
}

func assertHostedPlaceChangedOrder(t *testing.T, result pipeline.SyncResult, targetID string, want []string, phase string) {
	t.Helper()
	committed, ok := result.PlaceSync.Committed[targetID]
	if !ok {
		t.Fatalf("%s has no committed place result for %s: %+v", phase, targetID, result.PlaceSync)
	}
	var got []string
	for _, changed := range committed.Changed {
		if slices.Contains(want, changed) {
			got = append(got, changed)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("%s changed paths = %v; want package file order %v (all changes %v)", phase, got, want, committed.Changed)
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
		t.Fatalf("open hosted place ownership for %s: %v", root, err)
	}
	snapshot, err := owner.SnapshotState()
	if err != nil {
		t.Fatalf("snapshot hosted place ownership for %s: %v", root, err)
	}
	projection, err := owner.PlaceFromSnapshot(snapshot)
	if err != nil {
		t.Fatalf("read hosted place projection for %s: %v", root, err)
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
		t.Fatalf("hosted place projection at %s = %#v; want exact layout -> mod dependency", root, projection)
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
			t.Fatalf("hosted place file %s = %q, %v; want %q", file.path, data, err, file.want)
		}
	}
}

func assertHostedPlaceEmpty(t *testing.T, root, stateRoot string) {
	t.Helper()
	owner, err := ownership.Open(root, ownership.WithStateRoot(stateRoot))
	if err != nil {
		t.Fatalf("open deselected place ownership for %s: %v", root, err)
	}
	snapshot, err := owner.SnapshotState()
	if err != nil {
		t.Fatalf("snapshot deselected place ownership for %s: %v", root, err)
	}
	projection, err := owner.PlaceFromSnapshot(snapshot)
	if err != nil || len(projection.Packages) != 0 {
		t.Fatalf("deselected place projection at %s = %#v, %v; want no packages", root, projection, err)
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
