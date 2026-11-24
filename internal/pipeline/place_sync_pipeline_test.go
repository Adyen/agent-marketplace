package pipeline

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
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/sourcerevision"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestSyncUsesDeployedPlaceTargets(t *testing.T) {
	ctx := context.Background()
	gitFixture := newDeployedGitPlaceProfileFixture(t)
	if err := os.WriteFile(filepath.Join(gitFixture.repo, filepath.FromSlash(gitFixture.root)), gitFixture.profile, 0o600); err != nil {
		t.Fatal(err)
	}
	gitSkill := filepath.Join(gitFixture.repo, "profiles", "skills", "machine-control", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(gitSkill), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gitSkill, []byte("---\nname: machine-control\ndescription: git source control\n---\n# Machine Control\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, gitFixture.repo, "add", gitFixture.root, "profiles/skills")
	runGit(t, gitFixture.repo, "commit", "-m", "add a valid source root")
	lockedCommit, err := lockfile.ParseGitOID("sha1:" + gitCommitForTest(t, gitFixture.repo))
	if err != nil {
		t.Fatal(err)
	}
	gitFixture.lockedFirst.Commit = lockedCommit
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	setDeployedPlaceGitHome(t, home)
	configHome := filepath.Join(home, ".config")
	stateHome := filepath.Join(home, ".local", "state")
	stateRoot := filepath.Join(stateHome, "agent-marketplace")
	targetRoot := filepath.Join(configHome, "invented-app")
	for _, root := range []string{configHome, stateHome, stateRoot, targetRoot} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("INVENTED_APP_HOME", targetRoot)

	profileReader := LockedGitPlaceProfileReader(gitFixture.lockedFirst, gitFixture.rawURL)
	profileBytes, err := profileReader(ctx, gitFixture.identity, resolver.MarketplaceResolveModeRefresh)
	if err != nil || !bytes.Equal(profileBytes, gitFixture.profile) {
		t.Fatalf("warm pinned profile cache = %q, %v", profileBytes, err)
	}
	gitRoot := deployedPlaceTestGitRoot(t, ctx, gitFixture)

	files := []targetprofile.PackageFile{{
		SourcePath: "payload/guide.md", DestinationPath: "mods/sample/guide.md",
		Mode: 0o644, Bytes: []byte("deployed place package\n"),
	}}
	hostBinding, err := targetprofile.BuildBinding(gitFixture.identity, files)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := hccartifact.NewPlaceArtifact("invented-app/mod", "sample", gitFixture.identity, files)
	if err != nil {
		t.Fatal(err)
	}
	_, placeholder, archiveDigest, err := hccartifact.EncodeArchives(artifact)
	if err != nil {
		t.Fatal(err)
	}
	coordinate := store.ResourceCoordinate{
		Marketplace: "market-id", Registry: "tools-id", Kind: "invented-app/mod", Name: "sample",
	}
	assessment := store.ResourceAssessment{
		Coordinate: coordinate, ContentDigest: store.RevisionDigest(hostBinding.PackageContentDigest),
		AnalyzerVersion: "fixture-analyzer", PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("d", 64)),
		Decision:                 store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("e", 64)),
		AssessedAt:               time.Unix(1, 0).UTC(),
		HostPackageIdentity:      &hostBinding,
	}
	assessment, err = store.FinalizeResourceAssessment(assessment)
	if err != nil {
		t.Fatal(err)
	}
	revision := store.AssessedResourceRevision{
		ResourceRevision: store.ResourceRevision{
			Coordinate: coordinate, ContentDigest: store.RevisionDigest(hostBinding.PackageContentDigest),
			ArchiveDigest: store.ArchiveDigest(archiveDigest),
		},
		Assessment: assessment,
	}
	revision.Occurrence = store.ResourceOccurrence{
		Coordinate: coordinate, ContentDigest: store.RevisionDigest(hostBinding.PackageContentDigest),
		Commit: strings.Repeat("1", 40), ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("f", 64)),
	}
	var wellKnownRequests, revisionRequests, archiveRequests atomic.Int32
	var peer *httptest.Server
	revisionPath := "/api/v1/resources/" + url.PathEscape("tools-id") + "/" +
		url.PathEscape("invented-app/mod") + "/" + url.PathEscape("sample") +
		"/revisions/" + hostBinding.PackageContentDigest
	archivePath := "/api/v1/archives/sha256/" + strings.TrimPrefix(archiveDigest, "sha256:")
	peer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.EscapedPath() {
		case "/.well-known/agent-marketplace.json":
			wellKnownRequests.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("market-id", peer.URL))
		case revisionPath:
			revisionRequests.Add(1)
			values := req.Header.Values("X-Agent-Marketplace-Features")
			if len(values) != 1 || values[0] != mpapi.TargetProfilePlaceFeature {
				t.Errorf("resource feature header = %q", values)
			}
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
		case archivePath:
			archiveRequests.Add(1)
			_, _ = w.Write(placeholder)
		default:
			t.Errorf("unexpected Marketplace request path %q", req.URL.EscapedPath())
			http.NotFound(w, req)
		}
	}))
	defer peer.Close()

	profileLock := lockfile.MarketplaceResourceLock{
		RegistryName: "tools", RegistryID: "tools-id", Kind: "target-profile", Name: gitFixture.identity.TargetID,
		Revision: gitFixture.identity.RevisionDigest, ContentDigest: lockfile.Digest(gitFixture.identity.RevisionDigest),
		ProfileIdentity: &gitFixture.identity, Acquisition: lockfile.MarketplaceAcquisitionMirror,
	}
	packageLock := lockfile.MarketplaceResourceLock{
		RegistryName: "tools", RegistryID: "tools-id", Kind: "invented-app/mod", Name: "sample",
		Revision: hostBinding.PackageContentDigest, ContentDigest: lockfile.Digest(hostBinding.PackageContentDigest),
		ArchiveDigest: lockfile.Digest(archiveDigest), AssessmentDigest: lockfile.Digest(assessment.AssessmentDigest),
		HostPackageIdentity: &hostBinding, Acquisition: lockfile.MarketplaceAcquisitionMirror,
	}
	lockData, err := lockfile.Encode(lockfile.Document{
		APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeUser,
		Registries: []lockfile.GitRegistry{{
			URL: gitFixture.origin, Ref: gitFixture.ref, Commit: gitFixture.lockedFirst.Commit,
			Roots: []lockfile.GitRoot{gitRoot},
		}},
		Marketplaces: []lockfile.MarketplaceLock{{
			URL: peer.URL, InstanceID: "market-id",
			RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "tools", RegistryID: "tools-id"}},
			Resources:        []lockfile.MarketplaceResourceLock{profileLock, packageLock},
		}},
	})
	if err != nil {
		t.Fatalf("encode deployed user lock: %v", err)
	}

	repo := filepath.Join(home, "workspace")
	userConfigPath := filepath.Join(home, ".agents", "config.yaml")
	repoConfigPath := filepath.Join(repo, ".agents", "config.yaml")
	localConfigPath := filepath.Join(repo, ".agents", "config.local.yaml")
	organizationConfigPath := filepath.Join(home, "organization", "config.yaml")
	registryRoot := filepath.Join(repo, "registry")
	skillPath := filepath.Join(registryRoot, "skills", "review", "SKILL.md")
	for _, dir := range []string{filepath.Dir(userConfigPath), filepath.Dir(repoConfigPath), filepath.Dir(skillPath)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	userConfig := fmt.Sprintf(`apiVersion: agent-marketplace/v4
targets: [invented-app]
sources:
  profile:
    git: %q
    ref: %q
    path: profiles
    source_harness: factory
  hosted:
    marketplace: %q
resources:
  profile: [skill/machine-control]
  hosted:
    include:
      - tools/target-profile/invented-app
      - tools/invented-app%%2Fmod/sample
`, gitFixture.rawURL, gitFixture.ref, peer.URL)
	repoConfig := fmt.Sprintf(`apiVersion: agent-marketplace/v4
targets: [factory]
sources:
  local: {directory: %q, source_harness: factory}
resources:
  local: [skill/review]
`, registryRoot)
	for path, data := range map[string][]byte{
		userConfigPath: []byte(userConfig),
		repoConfigPath: []byte(repoConfig),
		skillPath:      []byte("---\nname: review\ndescription: compiled control\n---\n# Review\n"),
	} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := config.ResolveConfigLockPaths(userConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.CanonicalLock), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.CanonicalLock, lockData, 0o600); err != nil {
		t.Fatal(err)
	}

	effectiveOpts := config.LoadEffectiveOptions{
		RepoRoot: repo, LocalConfigDir: repo, OrganizationPath: organizationConfigPath,
		GlobalPath: userConfigPath, RepoPath: repoConfigPath, LocalPath: localConfigPath,
	}
	effective := LoadEffective(ctx, ConfigLoadStrict, effectiveOpts, nil)
	if len(effective.Errors) != 0 || effective.Merged == nil {
		t.Fatalf("load deployed config and lock: merged=%#v errors=%v", effective.Merged, effective.Errors)
	}
	seedResolver := marketplace.NewResolver()
	seedResolver.HTTPClient = peer.Client()
	if _, err := seedResolver.ResolvePlacePackages(
		ctx, effective.Merged, map[targetprofile.Identity][]byte{gitFixture.identity: gitFixture.profile},
		resolver.MarketplaceResolveModeRefresh,
	); err != nil {
		t.Fatalf("seed verified locked package cache: %v", err)
	}
	warmRequestCounts := []int32{wellKnownRequests.Load(), revisionRequests.Load(), archiveRequests.Load()}
	if warmRequestCounts[0] == 0 || warmRequestCounts[1] == 0 || warmRequestCounts[2] == 0 {
		t.Fatalf("package cache seed requests = %v", warmRequestCounts)
	}
	peer.Close()

	workspace, err := NewWorkspace(repo, repo)
	if err != nil {
		t.Fatal(err)
	}
	opts := ResolveOptions{
		Workspace: workspace, Offline: true, NoBackup: true, NoConfigBackup: true,
		ConfigLoadMode: ConfigLoadStrict, ReadOnlyExactLocks: true, EnablePlaceSync: true,
		EffectiveOpts: effectiveOpts,
	}
	assertPending := func(result SyncResult) {
		t.Helper()
		if len(result.Errors) != 0 {
			t.Fatalf("offline Sync errors = %v", result.Errors)
		}
		foundPending := false
		for _, warning := range result.Warnings {
			if strings.Contains(warning.Error(), "target.approval.required") &&
				strings.Contains(warning.Error(), gitFixture.identity.TargetID) {
				foundPending = true
			}
		}
		if !foundPending {
			t.Fatalf("offline Sync warnings = %v, want pending approval for %s", result.Warnings, gitFixture.identity.TargetID)
		}
		if len(result.PlaceSync.Pending) != 1 || result.PlaceSync.Pending[0] != gitFixture.identity ||
			len(result.PlaceSync.Committed) != 0 {
			t.Fatalf("offline place sync = %#v", result.PlaceSync)
		}
		if len(result.MachineTargetHarnesses) != 0 {
			t.Fatalf("compiled machine targets = %v, want none for place-only user targets", result.MachineTargetHarnesses)
		}
		if entries, err := os.ReadDir(targetRoot); err != nil || len(entries) != 0 {
			t.Fatalf("pending Sync wrote place target: entries=%v err=%v", entries, err)
		}
	}
	pending := Sync(ctx, opts)
	assertPending(pending)
	checkResolved, releaseCheck := ResolveWithExactLockLease(ctx, opts)
	defer releaseCheck()
	if len(checkResolved.Errors) != 0 {
		t.Fatalf("resolve for place check = %v", checkResolved.Errors)
	}
	pendingCheck, err := CheckDeployedPlace(ctx, checkResolved)
	if err != nil || len(pendingCheck.Pending) != 1 || pendingCheck.Pending[0] != gitFixture.identity ||
		len(pendingCheck.Drift) != 0 {
		t.Fatalf("place check before approval = %#v, err=%v", pendingCheck, err)
	}
	if entries, err := os.ReadDir(targetRoot); err != nil || len(entries) != 0 {
		t.Fatalf("pending place check changed target root: %v, %v", entries, err)
	}
	assertPlacePublishPathAbsent(t, filepath.Join(stateRoot, "ownership"))
	assertPlacePublishPathAbsent(t, filepath.Join(stateRoot, "packages"))
	if got := []int32{wellKnownRequests.Load(), revisionRequests.Load(), archiveRequests.Load()}; got[0] != warmRequestCounts[0] || got[1] != warmRequestCounts[1] || got[2] != warmRequestCounts[2] {
		t.Fatalf("pending place check issued Marketplace requests: before=%v after=%v", warmRequestCounts, got)
	}
	if !containsResolvedSource(pending.Resolved, config.RepoCommitted, "review") || pending.Report == nil {
		t.Fatalf("compiled directory source did not materialize independently: resolved=%#v report=%#v", pending.Resolved, pending.Report)
	}
	repoSkill, err := os.ReadFile(filepath.Join(repo, ".factory", "skills", "review", "SKILL.md"))
	if err != nil || !bytes.Contains(repoSkill, []byte("compiled control")) {
		t.Fatalf("repo compiled skill = %q, %v", repoSkill, err)
	}
	for _, artifact := range pending.Resolved {
		if artifact.Source == config.UserPersonal && len(artifact.Harnesses) > 0 {
			t.Fatalf("place-only machine configuration compiled %s for %v", artifact.Name, artifact.Harnesses)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "machine-control", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("place-only machine configuration wrote machine skill: %v", err)
	}
	if got := []int32{wellKnownRequests.Load(), revisionRequests.Load(), archiveRequests.Load()}; got[0] != warmRequestCounts[0] || got[1] != warmRequestCounts[1] || got[2] != warmRequestCounts[2] {
		t.Fatalf("offline pending Sync issued Marketplace requests: before=%v after=%v", warmRequestCounts, got)
	}

	profile, err := targetprofile.Parse(gitFixture.profile, gitFixture.identity)
	if err != nil {
		t.Fatal(err)
	}
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	platform := "linux"
	if runtime.GOOS == "darwin" {
		platform = "macos"
	}
	approvalEnvironment := targetprofile.ApprovalEnvironment{
		Platform: platform, Home: home, XDGConfig: configHome, CanonicalStateRoot: filepath.Dir(stateDir),
	}
	binding, err := resolvePlaceApprovalBinding(profile, gitFixture.identity, approvalEnvironment)
	if err != nil {
		t.Fatalf("resolve actual approval binding: %v", err)
	}
	if err := targetprofile.RecordApproval(binding); err != nil {
		t.Fatalf("record local approval receipt: %v", err)
	}
	approvedCheck, err := CheckDeployedPlace(ctx, checkResolved)
	if err != nil || len(approvedCheck.Pending) != 0 || len(approvedCheck.Drift) != 1 ||
		approvedCheck.Drift[0] != gitFixture.identity.TargetID {
		t.Fatalf("approved pre-install place check = %#v, err=%v", approvedCheck, err)
	}
	if entries, err := os.ReadDir(targetRoot); err != nil || len(entries) != 0 {
		t.Fatalf("approved place check changed target root: %v, %v", entries, err)
	}
	assertPlacePublishPathAbsent(t, filepath.Join(stateRoot, "ownership"))
	assertPlacePublishPathAbsent(t, filepath.Join(stateRoot, "packages"))
	if got := []int32{wellKnownRequests.Load(), revisionRequests.Load(), archiveRequests.Load()}; got[0] != warmRequestCounts[0] || got[1] != warmRequestCounts[1] || got[2] != warmRequestCounts[2] {
		t.Fatalf("approved place check issued Marketplace requests: before=%v after=%v", warmRequestCounts, got)
	}

	approved := Sync(ctx, opts)
	if len(approved.Errors) != 0 {
		t.Fatalf("approved offline Sync errors = %v", approved.Errors)
	}
	committed, ok := approved.PlaceSync.Committed[gitFixture.identity.TargetID]
	if !ok || len(committed.Changed) == 0 {
		t.Fatalf("approved offline place sync = %#v", approved.PlaceSync)
	}
	targetFile := filepath.Join(targetRoot, "mods", "sample", "guide.md")
	assertPlacePublishFile(t, targetFile, files[0].Bytes, files[0].Mode)
	info, err := os.Stat(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	owner, _ := openPlacePublishOwnership(t, placeAdmissionFixture{binding: binding})
	ownershipPath := owner.StateFile()
	ownershipBefore, err := os.ReadFile(ownershipPath)
	if err != nil {
		t.Fatal(err)
	}
	ownershipInfo, err := os.Stat(ownershipPath)
	if err != nil {
		t.Fatal(err)
	}
	packageTreeBefore := snapshotPlaceCheckTree(t, filepath.Join(stateRoot, "packages"))
	unchanged := Sync(ctx, opts)
	if len(unchanged.Errors) != 0 || len(unchanged.PlaceSync.Committed[gitFixture.identity.TargetID].Changed) != 0 {
		t.Fatalf("second offline Sync = %#v", unchanged.PlaceSync)
	}
	after, err := os.Stat(targetFile)
	if err != nil || !info.ModTime().Equal(after.ModTime()) {
		t.Fatalf("repeat offline Sync changed place file timestamp: before=%v after=%v err=%v", info, after, err)
	}
	cleanCheck, err := CheckDeployedPlace(ctx, checkResolved)
	ownershipAfter, readErr := os.ReadFile(ownershipPath)
	ownershipAfterInfo, statErr := os.Stat(ownershipPath)
	targetAfterCheck, targetStatErr := os.Stat(targetFile)
	packageTreeAfter := snapshotPlaceCheckTree(t, filepath.Join(stateRoot, "packages"))
	if err != nil || len(cleanCheck.Pending) != 0 || len(cleanCheck.Drift) != 0 || readErr != nil || statErr != nil ||
		targetStatErr != nil || !bytes.Equal(ownershipBefore, ownershipAfter) ||
		!ownershipInfo.ModTime().Equal(ownershipAfterInfo.ModTime()) ||
		!info.ModTime().Equal(targetAfterCheck.ModTime()) || !reflect.DeepEqual(packageTreeBefore, packageTreeAfter) {
		t.Fatalf("clean place check changed installed state: check=%#v err=%v read=%v stat=%v targetStat=%v", cleanCheck, err, readErr, statErr, targetStatErr)
	}
	assertPlacePublishFile(t, targetFile, files[0].Bytes, files[0].Mode)
	tampered := []byte("operator edit remains untouched\n")
	if err := os.WriteFile(targetFile, tampered, files[0].Mode); err != nil {
		t.Fatal(err)
	}
	tamperedInfo, err := os.Stat(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CheckDeployedPlace(ctx, checkResolved); err == nil {
		t.Fatal("place check accepted a tampered owned file")
	}
	tamperedAfter, err := os.ReadFile(targetFile)
	tamperedAfterInfo, statErr := os.Stat(targetFile)
	stateAfterConflict, readErr := os.ReadFile(ownershipPath)
	if err != nil || statErr != nil || readErr != nil || !bytes.Equal(tamperedAfter, tampered) ||
		!tamperedInfo.ModTime().Equal(tamperedAfterInfo.ModTime()) || !bytes.Equal(ownershipBefore, stateAfterConflict) {
		t.Fatalf("conflict check changed tampered installation: read=%v stat=%v state=%v", err, statErr, readErr)
	}
	if err := os.WriteFile(targetFile, files[0].Bytes, files[0].Mode); err != nil {
		t.Fatal(err)
	}
	if got := []int32{wellKnownRequests.Load(), revisionRequests.Load(), archiveRequests.Load()}; got[0] != warmRequestCounts[0] || got[1] != warmRequestCounts[1] || got[2] != warmRequestCounts[2] {
		t.Fatalf("offline approved Sync issued Marketplace requests: before=%v after=%v", warmRequestCounts, got)
	}
}

type placeCheckTreeEntry struct {
	bytes   []byte
	mode    os.FileMode
	modTime time.Time
}

func snapshotPlaceCheckTree(t *testing.T, root string) map[string]placeCheckTreeEntry {
	t.Helper()
	snapshot := make(map[string]placeCheckTreeEntry)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		var data []byte
		if !entry.IsDir() {
			data, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		snapshot[relative] = placeCheckTreeEntry{bytes: data, mode: info.Mode(), modTime: info.ModTime()}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot place check tree %s: %v", root, err)
	}
	return snapshot
}

func deployedPlaceTestGitRoot(t *testing.T, ctx context.Context, fixture deployedPlaceGitProfileFixture) lockfile.GitRoot {
	t.Helper()
	oid, err := cache.ParseGitOID(fixture.lockedFirst.Commit.String())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, release, err := cache.LeaseExactGitReadOnlyWithRootContext(ctx, fixture.origin, oid, "profiles")
	if err != nil {
		t.Fatalf("open warmed exact Git snapshot: %v", err)
	}
	defer release()
	built, err := sourcerevision.BuildWithContext(ctx, sourcerevision.Input{
		SnapshotRoot: snapshot, RootPath: "profiles", SourceHarness: "factory",
		RevisionSchema: lockfile.CurrentRevisionSchema,
	})
	if err != nil {
		t.Fatalf("build exact Git root revision: %v", err)
	}
	projection := built.Projection()
	root := lockfile.GitRoot{
		Path: "profiles", SourceHarness: projection.SourceHarness, RevisionSchema: projection.RevisionSchema,
		RevisionDigest: lockfile.Digest(projection.RevisionDigest),
	}
	for _, resource := range projection.Resources {
		support := make(map[string]lockfile.Digest, len(resource.SupportFiles))
		for name, digest := range resource.SupportFiles {
			support[name] = lockfile.Digest(digest)
		}
		root.Resources = append(root.Resources, lockfile.GitResource{
			Kind: resource.Kind, Name: resource.Name, ContentDigest: lockfile.Digest(resource.ContentDigest),
			ArchiveDigest: lockfile.Digest(resource.ArchiveDigest), SupportFiles: support,
		})
	}
	return root
}

func containsResolvedSource(artifacts []resolver.ResolvedArtifact, source config.Source, name string) bool {
	for _, artifact := range artifacts {
		if artifact.Source == source && artifact.Name == name {
			return true
		}
	}
	return false
}

func TestMarketplaceConfigViewsForPlaceSync(t *testing.T) {
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{
		{
			Source: config.UserPersonal,
			Resources: []config.MarketplaceResourcePin{
				{Kind: nameutil.TargetProfileKind, Name: "invented-app"},
				{Kind: "invented-app/mod", Name: "sample"},
				{Kind: "skill", Name: "review"},
			},
		},
		{Source: config.UserPersonal},
		{
			Source: config.RepoCommitted,
			Resources: []config.MarketplaceResourcePin{
				{Kind: nameutil.TargetProfileKind, Name: "other-app"},
			},
		},
	}}
	filter := func(source config.Source) bool { return source == config.UserPersonal }
	compiled, place := marketplaceConfigViews(cfg, true, filter)
	if compiled == cfg || place == cfg || len(compiled.Marketplaces) != 2 || len(place.Marketplaces) != 2 {
		t.Fatalf("filtered config views = compiled %#v, place %#v", compiled, place)
	}
	if len(compiled.Marketplaces[0].Resources) != 1 || compiled.Marketplaces[0].Resources[0].Kind != "skill" ||
		len(compiled.Marketplaces[1].Resources) != 0 {
		t.Fatalf("compiled Marketplace entries = %#v", compiled.Marketplaces)
	}
	if len(place.Marketplaces[0].Resources) != 3 || len(place.Marketplaces[1].Resources) != 0 {
		t.Fatalf("place Marketplace entries = %#v", place.Marketplaces)
	}
	legacy, _ := marketplaceConfigViews(cfg, false, filter)
	if len(legacy.Marketplaces) != 2 || len(legacy.Marketplaces[0].Resources) != 3 {
		t.Fatalf("source-filtered legacy view = %#v", legacy.Marketplaces)
	}
}
