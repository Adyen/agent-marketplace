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
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// spec: target-profile-place/assessment-binds-the-exact-profile-online-and-offline
func TestHostedTargetProfileBootstrapApprovalAndOfflineSync(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	fixture := newTargetApprovalFixture(t)
	files := []targetprofile.PackageFile{{
		SourcePath: "payload/guide.md", DestinationPath: "mods/sample/guide.md",
		Mode: 0o644, Bytes: []byte("owned place package\n"),
	}}
	artifact, err := hccartifact.NewPlaceArtifact("invented-app/mod", "sample", fixture.identity, files)
	if err != nil {
		t.Fatalf("build scoped package artifact: %v", err)
	}
	_, placeholder, archiveDigest, err := hccartifact.EncodeArchives(artifact)
	if err != nil {
		t.Fatalf("encode scoped package archive: %v", err)
	}
	binding := artifact.Manifest.HostPackageIdentity
	if binding == nil {
		t.Fatal("scoped package artifact has no host package identity")
	}

	profileCoordinate := store.ResourceCoordinate{
		Marketplace: "market-id", Registry: "tools-id", Kind: "target-profile", Name: fixture.identity.TargetID,
	}
	profileProjection, err := inspection.BuildTargetProfile(
		profileCoordinate, fixture.identity, fixture.profileBytes, 1,
		targetprofile.MaxProfileBytes, targetprofile.MaxProfileBytes,
	)
	if err != nil {
		t.Fatalf("project target profile inspection: %v", err)
	}
	profileRevision := hostedLifecycleRevision(
		t,
		store.ResourceRevision{
			Coordinate: profileCoordinate, ContentDigest: store.RevisionDigest(fixture.identity.RevisionDigest),
		},
		nil,
		inspection.Digest(profileProjection.Payload),
	)
	packageCoordinate := store.ResourceCoordinate{
		Marketplace: "market-id", Registry: "tools-id", Kind: "invented-app/mod", Name: "sample",
	}
	packageDigest := artifact.Manifest.ContentDigest
	packageRevision := hostedLifecycleRevision(
		t,
		store.ResourceRevision{
			Coordinate:    packageCoordinate,
			ContentDigest: store.RevisionDigest(packageDigest),
			ArchiveDigest: store.ArchiveDigest(archiveDigest),
		},
		binding,
		inspection.Digest(placeholder),
	)

	profileRevisionPath := "/api/v1/resources/tools-id/target-profile/" + fixture.identity.TargetID +
		"/revisions/" + fixture.identity.RevisionDigest
	profileInspectionPath := profileRevisionPath + "/inspection"
	profileContentPath := profileInspectionPath + "/content"
	packageRevisionPath := "/api/v1/resources/tools-id/" + url.PathEscape("invented-app/mod") +
		"/sample/revisions/" + packageDigest
	archivePath := "/api/v1/archives/sha256/" + strings.TrimPrefix(archiveDigest, "sha256:")
	var requests hostedLifecycleRequestCounters
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/.well-known/agent-marketplace.json":
			requests.wellKnown.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("market-id", server.URL))
		case "/api/v1/registries/tools":
			requests.registries.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(mpapi.RegistryDTO{
				ID: "tools-id", Slug: "tools", Origins: []string{"https://git.example.com/tools"},
				Ref: "main", Roots: []store.RegistryRoot{{Path: "profiles"}},
				Acquisition: store.AcquisitionMirror, Listed: true,
			})
		case profileRevisionPath:
			requests.profileRevision.Add(1)
			assertHostedProfileFeature(t, r)
			writeHostedLifecycleRevision(w, profileRevision)
		case profileInspectionPath:
			requests.profileInspection.Add(1)
			assertHostedProfileFeature(t, r)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(profileProjection.Payload)
		case profileContentPath:
			requests.profileContent.Add(1)
			assertHostedProfileFeature(t, r)
			w.Header().Set("Content-Type", inspection.MediaYAML)
			_, _ = w.Write(fixture.profileBytes)
		case packageRevisionPath:
			requests.packageRevision.Add(1)
			assertHostedProfileFeature(t, r)
			writeHostedLifecycleRevision(w, packageRevision)
		case archivePath:
			requests.archive.Add(1)
			_, _ = w.Write(placeholder)
		default:
			if strings.HasPrefix(r.URL.EscapedPath(), "/api/v1/archives/") {
				requests.archive.Add(1)
			} else {
				requests.recordUnexpected(r.URL.EscapedPath())
			}
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	userConfig := fmt.Sprintf(`apiVersion: agent-marketplace/v4
targets: [invented-app]
sources:
  hosted: {marketplace: %s}
resources:
  hosted:
    include:
      - tools/target-profile/invented-app: {revision: %s}
      - tools/invented-app%%2Fmod/sample: {revision: %s}
`, server.URL, fixture.identity.RevisionDigest, packageDigest)
	if err := os.WriteFile(fixture.userConfig, []byte(userConfig), 0o600); err != nil {
		t.Fatalf("write hosted-only v4 config: %v", err)
	}
	if err := os.Remove(fixture.userLock); err != nil {
		t.Fatalf("remove fixture's initial user lock: %v", err)
	}

	targetPath := filepath.Join(fixture.targetRoot, "mods", "sample", "guide.md")
	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("first sync/bootstrap: %v\n%s", err, output)
	}
	if !strings.Contains(output, "target.approval.required") {
		t.Fatalf("first sync output = %q, want pending approval warning", output)
	}
	assertHostedLifecycleLock(t, fixture, server.URL, fixture.identity, *binding, packageDigest, archiveDigest, string(packageRevision.Assessment.AssessmentDigest))
	if data, err := os.ReadFile(filepath.Join(fixture.targetRoot, "keep.txt")); err != nil || !bytes.Equal(data, fixture.rootBytes) {
		t.Fatalf("target root sentinel after bootstrap = %q, %v", data, err)
	}
	if _, err := os.Lstat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("unapproved package target stat = %v, want not-exist", err)
	}
	bootstrapRequests := requests.snapshot()
	if bootstrapRequests.profileRevision != 1 || bootstrapRequests.profileInspection != 1 ||
		bootstrapRequests.profileContent != 1 || bootstrapRequests.packageRevision != 1 ||
		bootstrapRequests.archive != 0 || bootstrapRequests.unknown != 0 ||
		bootstrapRequests.wellKnown == 0 || bootstrapRequests.registries == 0 {
		t.Fatalf("bootstrap Marketplace requests = %+v; want exact profile/scoped evidence, no archive or unknown route", bootstrapRequests)
	}
	if paths := requests.unexpectedPaths(); len(paths) > 0 {
		t.Fatalf("unexpected Marketplace paths during bootstrap: %v", paths)
	}
	if receipts, err := filepath.Glob(filepath.Join(fixture.stateRoot, "target-approval-*.json")); err != nil || len(receipts) != 0 {
		t.Fatalf("pre-approval receipts = %v, %v; want none", receipts, err)
	}

	_, stderr, err := executeTargetApprovalAtTerminal(t, "yes\n", nil, "target", "approve", fixture.identity.TargetID)
	if err != nil {
		t.Fatalf("approve hosted target: %v\n%s", err, stderr)
	}
	receipts, err := filepath.Glob(filepath.Join(fixture.stateRoot, "target-approval-*.json"))
	if err != nil || len(receipts) != 1 {
		t.Fatalf("approval receipts = %v, %v; want one", receipts, err)
	}
	approvalBinding, err := pipeline.PrepareDeployedPlaceApproval(context.Background(), mustTargetWorkspace(t, fixture), fixture.identity.TargetID)
	if err != nil {
		t.Fatalf("prepare deployed approval binding: %v", err)
	}
	if err := targetprofile.RequireApproval(approvalBinding); err != nil {
		t.Fatalf("require newly recorded approval: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(fixture.targetRoot, "keep.txt")); err != nil || !bytes.Equal(data, fixture.rootBytes) {
		t.Fatalf("target root sentinel after approval = %q, %v", data, err)
	}
	if _, err := os.Lstat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("approval wrote target package = %v, want not-exist", err)
	}
	if got := requests.archive.Load(); got != 0 {
		t.Fatalf("archive requests after approval = %d, want 0", got)
	}

	beforeApprovedArchives := requests.archive.Load()
	output, err = executeCommand("sync")
	if err != nil {
		t.Fatalf("approved online sync: %v\n%s", err, output)
	}
	if got := requests.archive.Load() - beforeApprovedArchives; got != 1 {
		t.Fatalf("approved sync archive requests = %d, want exactly 1", got)
	}
	if snapshot := requests.snapshot(); snapshot.unknown != 0 {
		t.Fatalf("unexpected Marketplace routes during approved sync: %v", requests.unexpectedPaths())
	}
	approvalBinding, err = pipeline.PrepareDeployedPlaceApproval(context.Background(), mustTargetWorkspace(t, fixture), fixture.identity.TargetID)
	if err != nil {
		t.Fatalf("read deployed profile after install: %v", err)
	}
	assertOwnedTargetPackage(t, fixture, approvalBinding, files[0].Bytes)

	targetTree := snapshotTargetStateTree(t, fixture.targetRoot)
	stateTree := snapshotTargetStateTree(t, fixture.stateRoot)
	onlineConfig, err := os.ReadFile(fixture.userConfig)
	if err != nil {
		t.Fatal(err)
	}
	onlineLock, err := os.ReadFile(fixture.userLock)
	if err != nil {
		t.Fatal(err)
	}
	onlineRequests := requests.snapshot()
	if onlineRequests.unknown != 0 {
		t.Fatalf("unexpected Marketplace routes after online sync: %v", requests.unexpectedPaths())
	}
	assertHostedSyncRefusesProfileMismatch(t, fixture, onlineConfig, onlineLock, &requests)
	assertTargetStateTreeUnchanged(t, targetTree, fixture.targetRoot)
	assertTargetStateTreeUnchanged(t, stateTree, fixture.stateRoot)
	if got, err := os.ReadFile(fixture.userConfig); err != nil || !bytes.Equal(got, onlineConfig) {
		t.Fatalf("user config not restored after mismatch checks: %v", err)
	}
	if got, err := os.ReadFile(fixture.userLock); err != nil || !bytes.Equal(got, onlineLock) {
		t.Fatalf("deployed lock not restored after mismatch checks: %v", err)
	}
	offlineConfig := snapshotTargetStateTree(t, fixture.userConfig)
	offlineLock := snapshotTargetStateTree(t, fixture.userLock)
	onlineRequests = requests.snapshot()

	server.Close()
	output, err = executeCommand("sync", "--offline")
	if err != nil {
		t.Fatalf("offline sync after Marketplace shutdown: %v\n%s", err, output)
	}
	assertTargetStateTreeUnchanged(t, targetTree, fixture.targetRoot)
	assertTargetStateTreeUnchanged(t, stateTree, fixture.stateRoot)
	assertTargetStateTreeUnchanged(t, offlineConfig, fixture.userConfig)
	assertTargetStateTreeUnchanged(t, offlineLock, fixture.userLock)
	if offlineRequests := requests.snapshot(); offlineRequests != onlineRequests {
		t.Fatalf("offline sync changed Marketplace counters: before=%+v after=%+v", onlineRequests, offlineRequests)
	}
}

func assertHostedSyncRefusesProfileMismatch(
	t *testing.T,
	fixture targetApprovalFixture,
	onlineConfig, onlineLock []byte,
	requests *hostedLifecycleRequestCounters,
) {
	t.Helper()
	alternateBytes := bytes.Replace(fixture.profileBytes, []byte("label: Invented App"), []byte("label: Altered App"), 1)
	if bytes.Equal(alternateBytes, fixture.profileBytes) {
		t.Fatal("could not alter target-profile fixture label")
	}
	alternate := fixture.identity
	alternate.RevisionDigest = inspection.Digest(alternateBytes)
	if err := alternate.Validate(); err != nil {
		t.Fatalf("validate alternate profile identity: %v", err)
	}
	if _, err := targetprofile.Parse(alternateBytes, alternate); err != nil {
		t.Fatalf("parse alternate profile: %v", err)
	}

	document, err := lockfile.Decode(onlineLock)
	if err != nil {
		t.Fatalf("decode online lock for profile mismatch: %v", err)
	}
	if len(document.Marketplaces) != 1 {
		t.Fatalf("online lock has %d Marketplace endpoints, want one", len(document.Marketplaces))
	}
	if _, ok := marketplaceLockResource(document, "sample"); !ok {
		t.Fatal("online lock lacks the scoped sample package")
	}
	var profileRecord *lockfile.MarketplaceResourceLock
	for i := range document.Marketplaces[0].Resources {
		record := &document.Marketplaces[0].Resources[i]
		if record.Kind == "target-profile" && record.Name == fixture.identity.TargetID {
			profileRecord = record
			break
		}
	}
	if profileRecord == nil {
		t.Fatalf("online lock lacks selected profile: %+v", document.Marketplaces[0].Resources)
	}
	profileRecord.Revision = alternate.RevisionDigest
	profileRecord.ContentDigest = lockfile.Digest(alternate.RevisionDigest)
	profileRecord.ProfileIdentity = &alternate
	alteredLock, err := lockfile.Encode(document)
	if err != nil {
		t.Fatalf("encode mismatched hosted profile lock: %v", err)
	}

	if count := bytes.Count(onlineConfig, []byte(fixture.identity.RevisionDigest)); count != 1 {
		t.Fatalf("authored profile revision appears %d times in user config, want one", count)
	}
	alteredConfig := bytes.Replace(onlineConfig, []byte(fixture.identity.RevisionDigest), []byte(alternate.RevisionDigest), 1)
	if bytes.Contains(alteredConfig, []byte(fixture.identity.RevisionDigest)) ||
		bytes.Count(alteredConfig, []byte(alternate.RevisionDigest)) != 1 {
		t.Fatal("failed to replace only the authored profile revision pin")
	}

	configBefore := snapshotTargetStateTree(t, fixture.userConfig)
	lockBefore := snapshotTargetStateTree(t, fixture.userLock)
	if !bytes.Equal(configBefore["."].bytes, onlineConfig) || !bytes.Equal(lockBefore["."].bytes, onlineLock) {
		t.Fatal("online config or lock changed before mismatch check")
	}
	defer func() {
		if err := os.WriteFile(fixture.userConfig, onlineConfig, configBefore["."].mode.Perm()); err != nil {
			t.Errorf("restore online config after mismatch check: %v", err)
		}
		if err := os.WriteFile(fixture.userLock, onlineLock, lockBefore["."].mode.Perm()); err != nil {
			t.Errorf("restore online lock after mismatch check: %v", err)
		}
	}()
	if err := os.WriteFile(fixture.userConfig, alteredConfig, configBefore["."].mode.Perm()); err != nil {
		t.Fatalf("stage mismatched profile config: %v", err)
	}
	if err := os.WriteFile(fixture.userLock, alteredLock, lockBefore["."].mode.Perm()); err != nil {
		t.Fatalf("stage mismatched profile lock: %v", err)
	}

	stagedConfig := snapshotTargetStateTree(t, fixture.userConfig)
	stagedLock := snapshotTargetStateTree(t, fixture.userLock)
	if !bytes.Equal(stagedConfig["."].bytes, alteredConfig) || !bytes.Equal(stagedLock["."].bytes, alteredLock) {
		t.Fatal("mismatched config or lock candidate was not staged exactly")
	}
	targetTree := snapshotTargetStateTree(t, fixture.targetRoot)
	stateTree := snapshotTargetStateTree(t, fixture.stateRoot)
	requestSnapshot := requests.snapshot()
	for _, args := range [][]string{{"sync"}, {"sync", "--offline"}} {
		output, commandErr := executeCommand(args...)
		message := output
		if commandErr != nil {
			message += commandErr.Error()
		}
		if commandErr == nil || !strings.Contains(message, "marketplace.profile.revision-mismatch") ||
			!strings.Contains(message, "sample") || !strings.Contains(message, "invented-app") {
			t.Fatalf("%v with mismatched profile candidate error = %v\n%s", args, commandErr, output)
		}
		if current := requests.snapshot(); current != requestSnapshot {
			t.Fatalf("%v with mismatched profile made Marketplace requests: before=%+v after=%+v", args, requestSnapshot, current)
		}
		assertTargetStateTreeUnchanged(t, targetTree, fixture.targetRoot)
		assertTargetStateTreeUnchanged(t, stateTree, fixture.stateRoot)
		assertTargetStateTreeUnchanged(t, stagedConfig, fixture.userConfig)
		assertTargetStateTreeUnchanged(t, stagedLock, fixture.userLock)
	}
}

type hostedLifecycleRequestCounters struct {
	wellKnown         atomic.Int32
	registries        atomic.Int32
	profileRevision   atomic.Int32
	profileInspection atomic.Int32
	profileContent    atomic.Int32
	packageRevision   atomic.Int32
	archive           atomic.Int32
	unknown           atomic.Int32
	unknownMu         sync.Mutex
	unknownPaths      []string
}

type hostedLifecycleRequestSnapshot struct {
	wellKnown         int32
	registries        int32
	profileRevision   int32
	profileInspection int32
	profileContent    int32
	packageRevision   int32
	archive           int32
	unknown           int32
}

func (c *hostedLifecycleRequestCounters) snapshot() hostedLifecycleRequestSnapshot {
	return hostedLifecycleRequestSnapshot{
		wellKnown: c.wellKnown.Load(), registries: c.registries.Load(),
		profileRevision: c.profileRevision.Load(), profileInspection: c.profileInspection.Load(),
		profileContent: c.profileContent.Load(), packageRevision: c.packageRevision.Load(),
		archive: c.archive.Load(), unknown: c.unknown.Load(),
	}
}

func (c *hostedLifecycleRequestCounters) recordUnexpected(path string) {
	c.unknown.Add(1)
	c.unknownMu.Lock()
	c.unknownPaths = append(c.unknownPaths, path)
	c.unknownMu.Unlock()
}

func (c *hostedLifecycleRequestCounters) unexpectedPaths() []string {
	c.unknownMu.Lock()
	defer c.unknownMu.Unlock()
	return append([]string(nil), c.unknownPaths...)
}

func hostedLifecycleRevision(
	t *testing.T,
	revision store.ResourceRevision,
	binding *targetprofile.Binding,
	inspectionDigest string,
) store.AssessedResourceRevision {
	t.Helper()
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: revision.Coordinate, ContentDigest: revision.ContentDigest,
		AnalyzerVersion: "hosted-lifecycle-fixture", PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("a", 64)),
		Decision:                 store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest(inspectionDigest),
		AssessedAt:               time.Unix(1, 0).UTC(), HostPackageIdentity: binding,
	})
	if err != nil {
		t.Fatalf("finalize hosted revision assessment: %v", err)
	}
	return store.AssessedResourceRevision{
		ResourceRevision: revision,
		Assessment:       assessment,
		Occurrence: store.ResourceOccurrence{
			Coordinate: revision.Coordinate, ContentDigest: revision.ContentDigest,
			Commit: strings.Repeat("1", 40), ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("f", 64)),
		},
	}
}

func writeHostedLifecycleRevision(w http.ResponseWriter, revision store.AssessedResourceRevision) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		store.AssessedResourceRevision
		CurrentAvailability struct {
			Availability string `json:"availability"`
		} `json:"current_availability"`
	}{
		AssessedResourceRevision: revision,
		CurrentAvailability: struct {
			Availability string `json:"availability"`
		}{Availability: "available"},
	})
}

func assertHostedProfileFeature(t *testing.T, r *http.Request) {
	t.Helper()
	if values := r.Header.Values("X-Agent-Marketplace-Features"); len(values) != 1 ||
		values[0] != mpapi.TargetProfilePlaceFeature {
		t.Errorf("place metadata feature header = %q, want exactly %q", values, mpapi.TargetProfilePlaceFeature)
	}
}

func assertHostedLifecycleLock(
	t *testing.T,
	fixture targetApprovalFixture,
	endpoint string,
	identity targetprofile.Identity,
	binding targetprofile.Binding,
	packageDigest, archiveDigest, assessmentDigest string,
) {
	t.Helper()
	raw, err := os.ReadFile(fixture.userLock)
	if err != nil {
		t.Fatalf("read bootstrapped user lock: %v", err)
	}
	doc, err := lockfile.Decode(raw)
	if err != nil {
		t.Fatalf("decode bootstrapped user lock: %v", err)
	}
	if len(doc.Marketplaces) != 1 {
		t.Fatalf("bootstrapped Marketplace entries = %#v, want one", doc.Marketplaces)
	}
	market := doc.Marketplaces[0]
	if market.URL != endpoint || market.InstanceID != "market-id" ||
		!reflect.DeepEqual(market.RegistryBindings, []lockfile.MarketplaceRegistryBinding{{Name: "tools", RegistryID: "tools-id"}}) ||
		len(market.Resources) != 2 {
		t.Fatalf("bootstrapped Marketplace lock = %+v", market)
	}
	profileWant := lockfile.MarketplaceResourceLock{
		RegistryName: "tools", RegistryID: "tools-id", Kind: "target-profile", Name: identity.TargetID,
		Revision: identity.RevisionDigest, ContentDigest: lockfile.Digest(identity.RevisionDigest),
		ProfileIdentity: &identity, Acquisition: lockfile.MarketplaceAcquisitionMirror,
	}
	packageWant := lockfile.MarketplaceResourceLock{
		RegistryName: "tools", RegistryID: "tools-id", Kind: "invented-app/mod", Name: "sample",
		Revision: packageDigest, ContentDigest: lockfile.Digest(packageDigest),
		ArchiveDigest: lockfile.Digest(archiveDigest), AssessmentDigest: lockfile.Digest(assessmentDigest),
		HostPackageIdentity: &binding, Acquisition: lockfile.MarketplaceAcquisitionMirror,
	}
	var profileLock, packageLock *lockfile.MarketplaceResourceLock
	for i := range market.Resources {
		switch market.Resources[i].Kind {
		case "target-profile":
			profileLock = &market.Resources[i]
		case "invented-app/mod":
			packageLock = &market.Resources[i]
		}
	}
	if profileLock == nil || !reflect.DeepEqual(*profileLock, profileWant) {
		t.Fatalf("bootstrapped profile lock = %+v, want %+v", profileLock, profileWant)
	}
	if packageLock == nil || !reflect.DeepEqual(*packageLock, packageWant) {
		t.Fatalf("bootstrapped scoped package lock = %+v, want %+v", packageLock, packageWant)
	}
}
