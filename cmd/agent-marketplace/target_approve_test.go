package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/gitidentity"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/sourcerevision"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

const targetApproveProfileYAML = `apiVersion: agent-marketplace.io/target-profile/v1
id: invented-app
label: Invented App
platforms: [macos, linux]
roots: {user: {override: INVENTED_APP_HOME, default: {macos: {base: home, path: .config/invented-app}, linux: {base: home, path: .config/invented-app}}, allowed_bases: [home]}}
kinds: {mod: {deliver: place, into: [mods/]}}
`

const targetVerifyProfileYAML = `apiVersion: agent-marketplace.io/target-profile/v1
id: invented-app
label: Invented App
platforms: [macos, linux]
roots: {user: {override: INVENTED_APP_HOME, default: {macos: {base: home, path: .config/invented-app}, linux: {base: home, path: .config/invented-app}}, allowed_bases: [home]}}
host: {executable: target-verify-fixture}
checks: {initial-load: {scope: eager-package-load, argv: [check, "{subject}"], subject: {under: mods, component: package-name}}}
kinds: {mod: {deliver: place, into: [mods/], verify: initial-load}}
`

type targetApprovalFixture struct {
	home, repo, userConfig, userLock, targetRoot, stateRoot string
	profileBytes                                            []byte
	userConfigBytes, userLockBytes, rootBytes               []byte
	identity                                                targetprofile.Identity
	gitLock                                                 lockfile.GitRegistry
	rawGitURL                                               string
}

func newTargetApprovalFixture(t *testing.T) targetApprovalFixture {
	return newTargetApprovalFixtureWithProfile(t, []byte(targetApproveProfileYAML))
}

func newTargetApprovalFixtureWithProfile(t *testing.T, profileBytes []byte) targetApprovalFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	root := filepath.Join(home, ".config", "invented-app")
	stateRoot := filepath.Join(home, ".local", "state", "agent-marketplace")
	for _, dir := range []string{root, stateRoot} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("INVENTED_APP_HOME", root)
	config.SetTestOrganizationConfigPath(t, filepath.Join(home, "missing-org.yaml"))

	repo := filepath.Join(home, "workspace")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "--object-format=sha1", "--initial-branch=main")
	sourceRepo := filepath.Join(home, "profile-source")
	if err := os.MkdirAll(sourceRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, sourceRepo, "init", "--object-format=sha1", "--initial-branch=main")
	profilePath := filepath.Join(sourceRepo, "profiles", "invented-app.yaml")
	skillPath := filepath.Join(sourceRepo, "profiles", "skills", "machine-control", "SKILL.md")
	for _, path := range []string{filepath.Dir(profilePath), filepath.Dir(skillPath)} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(profilePath, profileBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillPath, []byte("---\nname: machine-control\ndescription: source lock control\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, sourceRepo, "add", "profiles")
	runGit(t, sourceRepo, "commit", "-m", "add target profile")
	commitBytes, err := exec.Command("git", "-C", sourceRepo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("resolve local profile commit: %v", err)
	}
	commit, err := lockfile.ParseGitOID("sha1:" + strings.TrimSpace(string(commitBytes)))
	if err != nil {
		t.Fatal(err)
	}
	canonicalRepo, err := filepath.EvalSymlinks(sourceRepo)
	if err != nil {
		t.Fatal(err)
	}
	rawGitURL := (&url.URL{Scheme: "file", Path: canonicalRepo}).String()
	origin, err := gitidentity.CanonicalV1(rawGitURL)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(profileBytes)
	identity := targetprofile.Identity{
		TargetID:       "invented-app",
		SourceID:       "git:" + url.QueryEscape(origin) + "@main#" + url.QueryEscape("profiles/invented-app.yaml"),
		RevisionDigest: "sha256:" + hex.EncodeToString(sum[:]),
	}
	built, err := sourcerevision.BuildWithContext(context.Background(), sourcerevision.Input{
		SnapshotRoot: sourceRepo, RootPath: "profiles", SourceHarness: "factory",
		RevisionSchema: lockfile.CurrentRevisionSchema,
	})
	if err != nil {
		t.Fatalf("build deployed Git root: %v", err)
	}
	projection := built.Projection()
	gitRoot := lockfile.GitRoot{
		Path: "profiles", SourceHarness: projection.SourceHarness, RevisionSchema: projection.RevisionSchema,
		RevisionDigest: lockfile.Digest(projection.RevisionDigest),
	}
	for _, resource := range projection.Resources {
		gitRoot.Resources = append(gitRoot.Resources, lockfile.GitResource{
			Kind: resource.Kind, Name: resource.Name, ContentDigest: lockfile.Digest(resource.ContentDigest),
			ArchiveDigest: lockfile.Digest(resource.ArchiveDigest),
		})
	}
	gitLock := lockfile.GitRegistry{URL: origin, Ref: "main", Commit: commit, Roots: []lockfile.GitRoot{gitRoot}}
	profileLock := lockfile.MarketplaceResourceLock{
		RegistryName: "tools", RegistryID: "tools-id", Kind: nameutil.TargetProfileKind,
		Name: identity.TargetID, Revision: identity.RevisionDigest,
		ContentDigest: lockfile.Digest(identity.RevisionDigest), ProfileIdentity: &identity,
		Acquisition: lockfile.MarketplaceAcquisitionMirror,
	}
	lockBytes, err := lockfile.Encode(lockfile.Document{
		APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeUser,
		Registries: []lockfile.GitRegistry{gitLock},
		Marketplaces: []lockfile.MarketplaceLock{{
			URL: "https://market.example", InstanceID: "market-id",
			RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "tools", RegistryID: "tools-id"}},
			Resources:        []lockfile.MarketplaceResourceLock{profileLock},
		}},
	})
	if err != nil {
		t.Fatalf("encode deployed locks: %v", err)
	}
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	userConfigContent := fmt.Sprintf(`apiVersion: agent-marketplace/v4
sources:
  profile: {git: %q, ref: main, path: profiles, source_harness: factory}
  hosted: {marketplace: https://market.example}
resources:
  hosted: [tools/target-profile/invented-app]
`, rawGitURL)
	if err := os.MkdirAll(filepath.Dir(userConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userConfig, []byte(userConfigContent), 0o600); err != nil {
		t.Fatal(err)
	}
	lockPaths, err := config.ResolveConfigLockPaths(userConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(lockPaths.CanonicalLock), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPaths.CanonicalLock, lockBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(root, "keep.txt")
	rootBytes := []byte("user root stays untouched\n")
	if err := os.WriteFile(targetFile, rootBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	userConfigBytes, err := os.ReadFile(userConfig)
	if err != nil {
		t.Fatal(err)
	}
	userLockBytes, err := os.ReadFile(lockPaths.CanonicalLock)
	if err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, repo)
	return targetApprovalFixture{
		home: home, repo: repo, userConfig: userConfig, userLock: lockPaths.CanonicalLock,
		targetRoot: root, stateRoot: stateRoot, profileBytes: profileBytes,
		userConfigBytes: userConfigBytes, userLockBytes: userLockBytes, rootBytes: rootBytes,
		identity: identity,
		gitLock:  gitLock, rawGitURL: rawGitURL,
	}
}

func TestTargetApproveRequiresTerminal(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	fixture := newTargetApprovalFixture(t)
	assertUnchanged := func() {
		t.Helper()
		assertTargetApprovalFixtureUnchanged(t, fixture)
		exactGit := filepath.Join(cache.CacheDir(), "exact-git")
		if _, err := os.Stat(exactGit); !os.IsNotExist(err) {
			t.Fatalf("confirmation refusal acquired profile cache: %v", err)
		}
	}
	for _, tc := range []struct {
		name string
		run  func() (string, error)
	}{
		{
			name: "non-terminal",
			run: func() (string, error) {
				return executeCommand("target", "approve", fixture.identity.TargetID)
			},
		},
		{
			name: "yes",
			run: func() (string, error) {
				return executeCommandAtTerminal(t, "yes\n", "target", "approve", fixture.identity.TargetID, "--yes")
			},
		},
		{
			name: "explicit false yes",
			run: func() (string, error) {
				return executeCommandAtTerminal(t, "yes\n", "target", "approve", fixture.identity.TargetID, "--yes=false")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, err := tc.run()
			if err == nil || !strings.Contains(err.Error(), "target.confirmation-required") {
				t.Fatalf("approve error = %v, want target.confirmation-required\n%s", err, output)
			}
			assertUnchanged()
		})
	}
}

func TestTargetApproveRecordsOnlyConfirmedBinding(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	fixture := newTargetApprovalFixture(t)
	warmTargetApprovalProfileCache(t, fixture)
	assertTargetApprovalFixtureUnchanged(t, fixture)

	t.Run("binding changed after display", func(t *testing.T) {
		changedRoot := filepath.Join(fixture.home, ".config", "changed-invented-app")
		changedContent := []byte("changed root stays untouched\n")
		if err := os.MkdirAll(changedRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(changedRoot, "keep.txt"), changedContent, 0o600); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, err := executeTargetApprovalAtTerminal(t, "yes\n", func() {
			t.Setenv("INVENTED_APP_HOME", changedRoot)
		}, "target", "approve", fixture.identity.TargetID)
		if err == nil || !strings.Contains(err.Error(), "target.approval.context-changed") {
			t.Fatalf("changed binding error = %v, want context-changed\nstderr=%s", err, stderr)
		}
		if stdout != "" || !strings.Contains(stderr, fixture.targetRoot) || !strings.Contains(stderr, "Type yes to approve:") {
			t.Fatalf("context confirmation streams: stdout=%q stderr=%q", stdout, stderr)
		}
		assertTargetApprovalFixtureUnchanged(t, fixture)
		if data, err := os.ReadFile(filepath.Join(changedRoot, "keep.txt")); err != nil || !bytes.Equal(data, changedContent) {
			t.Fatalf("changed root was modified: %q, %v", data, err)
		}
		changedEntries, err := os.ReadDir(changedRoot)
		if err != nil || len(changedEntries) != 1 || changedEntries[0].Name() != "keep.txt" {
			t.Fatalf("changed root inventory = %v, %v", changedEntries, err)
		}
		if receipts, err := filepath.Glob(filepath.Join(fixture.stateRoot, "target-approval-*.json")); err != nil || len(receipts) != 0 {
			t.Fatalf("context change wrote approval receipts %v, err=%v", receipts, err)
		}
	})

	output, err := executeCommandAtTerminal(t, "no\n", "target", "approve", fixture.identity.TargetID)
	if err == nil || !strings.Contains(err.Error(), "target.confirmation-required") {
		t.Fatalf("negative confirmation error = %v, want target.confirmation-required\n%s", err, output)
	}
	assertTargetApprovalFixtureUnchanged(t, fixture)
	receipts, err := filepath.Glob(filepath.Join(fixture.stateRoot, "target-approval-*.json"))
	if err != nil || len(receipts) != 0 {
		t.Fatalf("denial wrote approval receipts %v, err=%v", receipts, err)
	}

	stdout, stderr, err := executeTargetApprovalAtTerminal(t, "yes\n", nil, "target", "approve", fixture.identity.TargetID, "--json")
	if err != nil {
		t.Fatalf("confirmed approval: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var result targetApprovalResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode approval stdout %q: %v", stdout, err)
	}
	approvalBinding, err := pipeline.PrepareDeployedPlaceApproval(context.Background(), mustTargetWorkspace(t, fixture), fixture.identity.TargetID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "approved" || result.Binding.Profile != fixture.identity || !result.Binding.NoExecution ||
		result.Binding.InstallationID != pipeline.PlaceInstallationID(approvalBinding) ||
		!strings.Contains(stderr, fixture.targetRoot) || !strings.Contains(stderr, "Type yes to approve:") {
		t.Fatalf("approval result or pre-confirmation context: result=%#v stderr=%q", result, stderr)
	}
	assertTargetApprovalFixtureUnchanged(t, fixture, 1)
	receipts, err = filepath.Glob(filepath.Join(fixture.stateRoot, "target-approval-*.json"))
	if err != nil || len(receipts) != 1 {
		t.Fatalf("approval receipt files = %v, err=%v", receipts, err)
	}
	info, err := os.Stat(receipts[0])
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("receipt mode = %v, err=%v; want 0600", info, err)
	}
	raw, err := os.ReadFile(receipts[0])
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		TargetID        string `json:"target_id"`
		ProfileDigest   string `json:"profile_digest"`
		ProfileSourceID string `json:"profile_source_id"`
		CanonicalRoot   string `json:"canonical_root"`
		Executable      string `json:"executable"`
		StateStore      struct {
			CanonicalRoot string `json:"canonical_root"`
			RootKey       string `json:"root_key"`
		} `json:"state_store"`
	}
	if err := json.Unmarshal(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.TargetID != fixture.identity.TargetID || receipt.ProfileDigest != fixture.identity.RevisionDigest ||
		receipt.ProfileSourceID != fixture.identity.SourceID || receipt.CanonicalRoot != fixture.targetRoot ||
		receipt.Executable != "no-execution" || receipt.StateStore.CanonicalRoot != fixture.stateRoot || receipt.StateStore.RootKey == "" {
		t.Fatalf("receipt binding = %#v", receipt)
	}
	humanOutput, err := executeCommandAtTerminal(t, "yes\n", "target", "approve", fixture.identity.TargetID)
	if err != nil || !strings.Contains(humanOutput, "future installation ID for this approved root") ||
		!strings.Contains(humanOutput, pipeline.PlaceInstallationID(approvalBinding)) {
		t.Fatalf("human approval output = %q, %v", humanOutput, err)
	}
}

// spec: target-profile-place/enable-requires-a-selected-admitted-profile
func TestTargetEnableRequiresSelectedAdmittedProfile(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Run("enables selected cached profile without side effects", func(t *testing.T) {
		fixture := newTargetApprovalFixture(t)
		warmTargetApprovalProfileCache(t, fixture)
		output, err := executeCommand("target", "enable", fixture.identity.TargetID)
		if err != nil {
			t.Fatalf("enable selected target: %v\n%s", err, output)
		}
		data, err := os.ReadFile(fixture.userConfig)
		if err != nil || !bytes.Contains(data, []byte("harnesses:")) || !bytes.Contains(data, []byte(fixture.identity.TargetID)) {
			t.Fatalf("enabled user config = %q, %v", data, err)
		}
		assertTargetApprovalNonConfigUnchanged(t, fixture, 0)
		assertTargetProfileSelected(t, fixture)
	})
	t.Run("unknown profile is refused without writes", func(t *testing.T) {
		fixture := newTargetApprovalFixture(t)
		warmTargetApprovalProfileCache(t, fixture)
		output, err := executeCommand("target", "enable", "unknown-app")
		if err == nil || !strings.Contains(err.Error(), "marketplace.profile.selection-invalid") {
			t.Fatalf("enable unknown target error = %v\n%s", err, output)
		}
		assertTargetApprovalFixtureUnchanged(t, fixture)
	})
	t.Run("blocked profile source is refused without writes", func(t *testing.T) {
		fixture := newTargetApprovalFixture(t)
		warmTargetApprovalProfileCache(t, fixture)
		configBytes := bytes.Replace(fixture.userConfigBytes, []byte("apiVersion: agent-marketplace/v4\n"),
			[]byte("apiVersion: agent-marketplace/v4\nallowed_marketplaces: ['^https://blocked.example$']\n"), 1)
		if err := os.WriteFile(fixture.userConfig, configBytes, 0o600); err != nil {
			t.Fatal(err)
		}
		fixture.userConfigBytes = configBytes
		output, err := executeCommand("target", "enable", fixture.identity.TargetID)
		var blocked *config.MarketplaceURLBlockedError
		if err == nil || !errors.As(err, &blocked) || !slices.Contains(blocked.BlockedURLs, "https://market.example") {
			t.Fatalf("enable blocked target error = %v\n%s", err, output)
		}
		assertTargetApprovalFixtureUnchanged(t, fixture)
	})
}

// spec: config-mutation/removing-the-last-target-without-targets-spelling-is-refused
// spec: target-profile-place/disable-preserves-the-profile-selection-and-installation
func TestTargetDisablePreservesSelectionAndOwnedFiles(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Run("writes an explicit empty targets list and is idempotent", func(t *testing.T) {
		fixture := newTargetApprovalFixture(t)
		warmTargetApprovalProfileCache(t, fixture)
		binding, err := pipeline.PrepareDeployedPlaceApproval(context.Background(), mustTargetWorkspace(t, fixture), fixture.identity.TargetID)
		if err != nil {
			t.Fatal(err)
		}
		if err := targetprofile.RecordApproval(binding); err != nil {
			t.Fatal(err)
		}
		configBytes := bytes.Replace(fixture.userConfigBytes, []byte("apiVersion: agent-marketplace/v4\n"),
			[]byte("apiVersion: agent-marketplace/v4\ntargets: [invented-app]\n"), 1)
		if err := os.WriteFile(fixture.userConfig, configBytes, 0o600); err != nil {
			t.Fatal(err)
		}
		fixture.userConfigBytes = configBytes
		receiptsBefore, err := filepath.Glob(filepath.Join(fixture.stateRoot, "target-approval-*.json"))
		if err != nil || len(receiptsBefore) != 1 {
			t.Fatalf("prepared approval receipts = %v, err=%v", receiptsBefore, err)
		}
		receiptBefore, err := os.ReadFile(receiptsBefore[0])
		if err != nil {
			t.Fatal(err)
		}
		files, pkg := resolvedTargetApprovalPackage(t, fixture)
		if _, err := pipeline.CommitPlaceUpserts(context.Background(), binding, fixture.profileBytes, targetApprovalEnvironment(t, fixture), []marketplace.ResolvedPlacePackage{pkg}); err != nil {
			t.Fatalf("seed real owned place package: %v", err)
		}
		statePath, ownershipBytes, ownershipInfo := assertOwnedTargetPackage(t, fixture, binding, files[0].Bytes)
		targetPath := filepath.Join(fixture.targetRoot, filepath.FromSlash(files[0].DestinationPath))
		ownershipTree := snapshotTargetStateTree(t, filepath.Dir(statePath))
		packageTree := snapshotTargetStateTree(t, filepath.Join(fixture.stateRoot, "packages"))
		targetInfo, err := os.Stat(targetPath)
		if err != nil {
			t.Fatal(err)
		}
		output, err := executeCommand("target", "disable", fixture.identity.TargetID)
		if err != nil {
			t.Fatalf("disable target: %v\n%s", err, output)
		}
		disabled, err := os.ReadFile(fixture.userConfig)
		if err != nil || !bytes.Contains(disabled, []byte("targets: []")) {
			t.Fatalf("disabled user config = %q, %v", disabled, err)
		}
		fixture.userConfigBytes = disabled
		assertOwnedTargetUnchanged(t, fixture, binding, statePath, ownershipBytes, ownershipInfo, targetPath, files[0].Bytes, targetInfo, receiptsBefore[0], receiptBefore, fixture.userLockBytes)
		assertTargetStateTreeUnchanged(t, ownershipTree, filepath.Dir(statePath))
		assertTargetStateTreeUnchanged(t, packageTree, filepath.Join(fixture.stateRoot, "packages"))
		assertTargetProfileSelected(t, fixture)
		info, err := os.Stat(fixture.userConfig)
		if err != nil {
			t.Fatal(err)
		}
		output, err = executeCommand("target", "disable", fixture.identity.TargetID)
		if err != nil {
			t.Fatalf("repeat disable: %v\n%s", err, output)
		}
		after, err := os.Stat(fixture.userConfig)
		if err != nil || !info.ModTime().Equal(after.ModTime()) {
			t.Fatalf("no-op disable changed config mtime: before=%v after=%v err=%v", info, after, err)
		}
		afterBytes, err := os.ReadFile(fixture.userConfig)
		if err != nil || !bytes.Equal(afterBytes, disabled) {
			t.Fatalf("no-op disable changed config bytes: %q, %v", afterBytes, err)
		}
		receiptAfter, err := os.ReadFile(receiptsBefore[0])
		if err != nil || !bytes.Equal(receiptAfter, receiptBefore) {
			t.Fatalf("disable changed approval receipt: %v", err)
		}
		assertOwnedTargetUnchanged(t, fixture, binding, statePath, ownershipBytes, ownershipInfo, targetPath, files[0].Bytes, targetInfo, receiptsBefore[0], receiptBefore, fixture.userLockBytes)
		assertTargetStateTreeUnchanged(t, ownershipTree, filepath.Dir(statePath))
		assertTargetStateTreeUnchanged(t, packageTree, filepath.Join(fixture.stateRoot, "packages"))
	})
	t.Run("last legacy harness target refuses with rename guidance", func(t *testing.T) {
		fixture := newTargetApprovalFixture(t)
		configBytes := bytes.Replace(fixture.userConfigBytes, []byte("apiVersion: agent-marketplace/v4\n"),
			[]byte("apiVersion: agent-marketplace/v4\nharnesses: [invented-app]\n"), 1)
		if err := os.WriteFile(fixture.userConfig, configBytes, 0o600); err != nil {
			t.Fatal(err)
		}
		fixture.userConfigBytes = configBytes
		output, err := executeCommand("target", "disable", fixture.identity.TargetID)
		if err == nil || !strings.Contains(strings.ToLower(err.Error()+output), "rename") {
			t.Fatalf("last legacy target error = %v\n%s", err, output)
		}
		assertTargetApprovalFixtureUnchanged(t, fixture)
	})
}

// spec: target-profile-place/target-verify-runs-only-the-named-check
func TestTargetVerifyRunsOnlyNamedCheck(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	fixture, binding, marker, targetPath, statePath, generation := newInstalledTargetVerificationFixture(t)
	installationID := expectedTargetInstallationID(binding)
	if pipeline.PlaceInstallationID(binding) != installationID {
		t.Fatalf("PlaceInstallationID() = %q, want %q", pipeline.PlaceInstallationID(binding), installationID)
	}
	revisedBinding := binding
	revisedBinding.Profile.RevisionDigest = "sha256:" + strings.Repeat("f", 64)
	if pipeline.PlaceInstallationID(revisedBinding) != installationID {
		t.Fatal("profile revision unexpectedly changed the installation ID")
	}
	configBefore, err := os.ReadFile(fixture.userConfig)
	if err != nil {
		t.Fatal(err)
	}
	lockBefore, err := os.ReadFile(fixture.userLock)
	if err != nil {
		t.Fatal(err)
	}
	targetBefore, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	ownerBefore, err := ownership.Open(binding.CanonicalRoot, ownership.WithStateRoot(binding.CanonicalStateRoot))
	if err != nil {
		t.Fatal(err)
	}
	snapshotBefore, err := ownerBefore.SnapshotState()
	if err != nil {
		t.Fatal(err)
	}
	projectionBefore, err := ownerBefore.PlaceFromSnapshot(snapshotBefore)
	if err != nil || len(projectionBefore.Packages) != 1 {
		t.Fatalf("pre-check installed projection = %#v, %v", projectionBefore, err)
	}
	packageDigest := projectionBefore.Packages[0].Binding.PackageContentDigest
	receipts, err := filepath.Glob(filepath.Join(fixture.stateRoot, "target-approval-*.json"))
	if err != nil || len(receipts) != 1 {
		t.Fatalf("approval receipts = %v, %v", receipts, err)
	}
	receiptBefore, err := os.ReadFile(receipts[0])
	if err != nil {
		t.Fatal(err)
	}
	rootTreeBefore := snapshotTargetStateTree(t, fixture.targetRoot)
	packageTreeBefore := snapshotTargetStateTree(t, filepath.Join(fixture.stateRoot, "packages"))

	output, err := executeCommand(
		"target", "verify", "invented-app%2Fmod/sample", "--installation", installationID,
	)
	if err != nil {
		t.Fatalf("verify installed place package: %v\n%s", err, output)
	}
	if strings.Count(output, "\n") != 1 || strings.Contains(output, "sensitive") ||
		!strings.Contains(output, "initial-load") || !strings.Contains(output, "pass") ||
		!strings.Contains(output, fixture.identity.RevisionDigest) || !strings.Contains(output, generation) ||
		!strings.Contains(output, packageDigest) || !strings.Contains(output, "target-verify") ||
		!strings.Contains(output, "unknown") {
		t.Fatalf("verification output = %q", output)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "invoked" {
		t.Fatalf("check marker = %q, %v", data, err)
	}
	if data, err := os.ReadFile(fixture.userConfig); err != nil || !bytes.Equal(data, configBefore) {
		t.Fatalf("verify changed config: %v", err)
	}
	if data, err := os.ReadFile(fixture.userLock); err != nil || !bytes.Equal(data, lockBefore) {
		t.Fatalf("verify changed deployed lock: %v", err)
	}
	if data, err := os.ReadFile(targetPath); err != nil || !bytes.Equal(data, targetBefore) {
		t.Fatalf("verify changed installed file: %q, %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(fixture.targetRoot, "keep.txt")); err != nil || !bytes.Equal(data, fixture.rootBytes) {
		t.Fatalf("verify changed unrelated target file: %q, %v", data, err)
	}
	if data, err := os.ReadFile(receipts[0]); err != nil || !bytes.Equal(data, receiptBefore) {
		t.Fatalf("verify changed approval receipt: %v", err)
	}
	owner, err := ownership.Open(binding.CanonicalRoot, ownership.WithStateRoot(binding.CanonicalStateRoot))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := owner.SnapshotState()
	if err != nil {
		t.Fatal(err)
	}
	projection, err := owner.PlaceFromSnapshot(snapshot)
	if err != nil || len(projection.Packages) != 1 || projection.Packages[0].Verification == nil ||
		projection.Packages[0].Verification.Outcome != "pass" ||
		projection.Packages[0].Verification.GenerationKey != generation {
		t.Fatalf("recorded verification projection = %#v, %v", projection, err)
	}
	beforePackage, afterPackage := projectionBefore.Packages[0], projection.Packages[0]
	beforePackage.Verification, afterPackage.Verification = nil, nil
	if !reflect.DeepEqual(beforePackage, afterPackage) {
		t.Fatalf("verification changed package projection beyond result: before=%#v after=%#v", beforePackage, afterPackage)
	}
	assertTargetStateTreeUnchanged(t, rootTreeBefore, fixture.targetRoot)
	assertTargetStateTreeUnchanged(t, packageTreeBefore, filepath.Join(fixture.stateRoot, "packages"))
	if _, err := os.Stat(statePath); err != nil {
		t.Fatal(err)
	}
}

// spec: target-profile-place/target-verify-refuses-drifted-files-without-running-checks
func TestTargetVerifyRefusesDrift(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Run("installation mismatch refuses before execution", func(t *testing.T) {
		_, binding, marker, _, statePath, _ := newInstalledTargetVerificationFixture(t)
		stateBefore, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatal(err)
		}
		output, err := executeCommand(
			"target", "verify", "invented-app%2Fmod/sample",
			"--installation", strings.Repeat("0", 64),
		)
		if err == nil || !strings.Contains(err.Error(), "target.place.installation-unavailable") || strings.Contains(output, "Verified") {
			t.Fatalf("installation mismatch = %v, %q", err, output)
		}
		if _, err := os.Lstat(marker); !os.IsNotExist(err) {
			t.Fatalf("installation mismatch ran the check: %v", err)
		}
		stateAfter, err := os.ReadFile(statePath)
		if err != nil || !bytes.Equal(stateBefore, stateAfter) {
			t.Fatalf("installation mismatch changed ownership: %v", err)
		}
		if got := expectedTargetInstallationID(binding); got == strings.Repeat("0", 64) {
			t.Fatal("fixture installation ID unexpectedly equals the mismatch")
		}
	})
	t.Run("edited file refuses before execution or result write", func(t *testing.T) {
		_, binding, marker, targetPath, statePath, _ := newInstalledTargetVerificationFixture(t)
		stateBefore, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatal(err)
		}
		edited := []byte("edited before verify\n")
		if err := os.WriteFile(targetPath, edited, 0o644); err != nil {
			t.Fatal(err)
		}
		output, err := executeCommand(
			"target", "verify", "invented-app%2Fmod/sample",
			"--installation", expectedTargetInstallationID(binding),
		)
		if err == nil || !strings.Contains(err.Error(), "target.place.drift") ||
			!strings.Contains(err.Error(), "mods/sample/guide.md") || strings.Contains(output, "Verified") {
			t.Fatalf("drift refusal = %v, %q", err, output)
		}
		if _, err := os.Lstat(marker); !os.IsNotExist(err) {
			t.Fatalf("drift refusal ran the check: %v", err)
		}
		stateAfter, err := os.ReadFile(statePath)
		if err != nil || !bytes.Equal(stateBefore, stateAfter) {
			t.Fatalf("drift refusal changed ownership: %v", err)
		}
		if data, err := os.ReadFile(targetPath); err != nil || !bytes.Equal(data, edited) {
			t.Fatalf("drift refusal changed edited file: %q, %v", data, err)
		}
	})
}

func newInstalledTargetVerificationFixture(
	t *testing.T,
) (targetApprovalFixture, targetprofile.ApprovalBinding, string, string, string, string) {
	t.Helper()
	fixture := newTargetApprovalFixtureWithProfile(t, []byte(targetVerifyProfileYAML))
	hostDir := filepath.Join(fixture.home, "bin")
	if err := os.Mkdir(hostDir, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(fixture.home, "verify-invoked")
	host := "#!/bin/sh\n" +
		"if [ \"$#\" -ne 2 ] || [ \"$1\" != check ] || [ \"$2\" != " + targetVerifyShellQuote(filepath.Join(fixture.targetRoot, "mods", "sample")) + " ]; then exit 81; fi\n" +
		"if IFS= read -r input; then exit 82; fi\n" +
		"printf invoked > " + targetVerifyShellQuote(marker) + "\nprintf 'sensitive diagnostic\\n'\nprintf 'sensitive stderr\\n' >&2\n"
	hostPath := filepath.Join(hostDir, "target-verify-fixture")
	if err := os.WriteFile(hostPath, []byte(host), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", hostDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	warmTargetApprovalProfileCache(t, fixture)
	binding, err := pipeline.PrepareDeployedPlaceApproval(context.Background(), mustTargetWorkspace(t, fixture), fixture.identity.TargetID)
	if err != nil {
		t.Fatal(err)
	}
	if err := targetprofile.RecordApproval(binding); err != nil {
		t.Fatal(err)
	}
	files, pkg := resolvedTargetApprovalPackage(t, fixture)
	if _, err := pipeline.CommitPlaceUpserts(
		context.Background(), binding, fixture.profileBytes, targetApprovalEnvironment(t, fixture),
		[]marketplace.ResolvedPlacePackage{pkg},
	); err != nil {
		t.Fatalf("install owned verification package: %v", err)
	}
	targetPath := filepath.Join(fixture.targetRoot, filepath.FromSlash(files[0].DestinationPath))
	owner, err := ownership.Open(binding.CanonicalRoot, ownership.WithStateRoot(binding.CanonicalStateRoot))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := owner.SnapshotState()
	if err != nil {
		t.Fatal(err)
	}
	projection, err := owner.PlaceFromSnapshot(snapshot)
	if err != nil || len(projection.Packages) != 1 {
		t.Fatalf("installed verification package = %#v, %v", projection, err)
	}
	return fixture, binding, marker, targetPath, owner.StateFile(), projection.Packages[0].GenerationKey
}

func expectedTargetInstallationID(binding targetprofile.ApprovalBinding) string {
	sum := sha256.Sum256([]byte("agent-marketplace/place-installation/v1\x00" +
		binding.Profile.TargetID + "\x00" + binding.CanonicalRoot + "\x00" + binding.CanonicalStateRoot))
	return hex.EncodeToString(sum[:])
}

func targetVerifyShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func warmTargetApprovalProfileCache(t *testing.T, fixture targetApprovalFixture) {
	t.Helper()
	reader := pipeline.LockedGitPlaceProfileReader(fixture.gitLock, fixture.rawGitURL)
	profileBytes, err := reader(context.Background(), fixture.identity, resolver.MarketplaceResolveModeRefresh)
	if err != nil || !bytes.Equal(profileBytes, fixture.profileBytes) {
		t.Fatalf("warm exact local profile cache = %q, %v", profileBytes, err)
	}
}

func mustTargetWorkspace(t *testing.T, fixture targetApprovalFixture) pipeline.Workspace {
	t.Helper()
	ws, err := pipeline.NewWorkspace(fixture.repo, fixture.repo)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

func assertTargetApprovalNonConfigUnchanged(t *testing.T, fixture targetApprovalFixture, receipts int) {
	t.Helper()
	if lockData, err := os.ReadFile(fixture.userLock); err != nil || !bytes.Equal(lockData, fixture.userLockBytes) {
		t.Fatalf("deployed user lock changed: %v", err)
	}
	rootFile := filepath.Join(fixture.targetRoot, "keep.txt")
	if data, err := os.ReadFile(rootFile); err != nil || !bytes.Equal(data, fixture.rootBytes) {
		t.Fatalf("target root changed: %q, %v", data, err)
	}
	rootEntries, err := os.ReadDir(fixture.targetRoot)
	if err != nil || len(rootEntries) != 1 || rootEntries[0].Name() != "keep.txt" {
		t.Fatalf("target root inventory = %v, %v", rootEntries, err)
	}
	stateEntries, err := os.ReadDir(fixture.stateRoot)
	if err != nil {
		t.Fatalf("read state root inventory: %v", err)
	}
	gotReceipts := 0
	for _, entry := range stateEntries {
		switch {
		case entry.Name() == "dependencies" && entry.IsDir():
			children, readErr := os.ReadDir(filepath.Join(fixture.stateRoot, entry.Name()))
			if readErr != nil || len(children) != 1 || children[0].Name() != "locks" || !children[0].IsDir() {
				t.Fatalf("dependency transaction state = %v, %v", children, readErr)
			}
			lockFiles, lockErr := os.ReadDir(filepath.Join(fixture.stateRoot, entry.Name(), "locks"))
			if lockErr != nil {
				t.Fatalf("read dependency lock inventory: %v", lockErr)
			}
			for _, lock := range lockFiles {
				info, statErr := lock.Info()
				if statErr != nil || !strings.HasSuffix(lock.Name(), ".lock") || !info.Mode().IsRegular() || info.Size() != 0 {
					t.Fatalf("unexpected dependency lock artifact %q: %v, %v", lock.Name(), info, statErr)
				}
			}
		case strings.HasPrefix(entry.Name(), "target-approval-") && strings.HasSuffix(entry.Name(), ".json"):
			gotReceipts++
		default:
			t.Fatalf("unexpected state entry %q", entry.Name())
		}
	}
	if gotReceipts != receipts {
		t.Fatalf("state root inventory = %v; want %d approval receipt(s)", stateEntries, receipts)
	}
}

func assertTargetProfileSelected(t *testing.T, fixture targetApprovalFixture) {
	t.Helper()
	intent := config.ReadIntent(context.Background(), config.LoadEffectiveOptions{RepoRoot: fixture.repo})
	evidence, err := pipeline.ReadDeployedLocks(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	effective := config.Bind(intent, evidence)
	selected, err := effective.Merged.SelectedMarketplaceProfiles()
	if len(effective.Errors) != 0 || err != nil || selected[fixture.identity.TargetID] != fixture.identity {
		t.Fatalf("selected profile after target edit = %#v, errors=%v, err=%v", selected, effective.Errors, err)
	}
}

func resolvedTargetApprovalPackage(t *testing.T, fixture targetApprovalFixture) ([]targetprofile.PackageFile, marketplace.ResolvedPlacePackage) {
	t.Helper()
	files := []targetprofile.PackageFile{{
		SourcePath: "payload/guide.md", DestinationPath: "mods/sample/guide.md",
		Mode: 0o644, Bytes: []byte("owned place package\n"),
	}}
	hostBinding, err := targetprofile.BuildBinding(fixture.identity, files)
	if err != nil {
		t.Fatal(err)
	}
	coordinate := store.ResourceCoordinate{
		Marketplace: store.MarketplaceID("market-id"), Registry: store.RegistryID("tools-id"),
		Kind: "invented-app/mod", Name: "sample",
	}
	archiveDigest := store.ArchiveDigest("sha256:" + strings.Repeat("c", 64))
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: coordinate, ContentDigest: store.RevisionDigest(hostBinding.PackageContentDigest),
		AnalyzerVersion: "target-toggle-test", PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("d", 64)),
		Decision:                 store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("e", 64)),
		AssessedAt:               time.Unix(1, 0).UTC(), HostPackageIdentity: &hostBinding,
	})
	if err != nil {
		t.Fatal(err)
	}
	pin := config.MarketplaceResourcePin{
		RegistryID: "tools-id", RegistrySlug: "tools", Kind: coordinate.Kind, Name: coordinate.Name,
		Revision: hostBinding.PackageContentDigest, Digest: hostBinding.PackageContentDigest,
		Acquisition: config.MarketplaceAcquisitionMirror, Direct: true,
		LockArchiveDigest: string(archiveDigest), LockAssessmentDigest: string(assessment.AssessmentDigest),
		LockHostPackageIdentity: hostBinding,
	}
	entry := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market-id", Source: config.UserPersonal}
	return files, marketplace.ResolvedPlacePackage{
		Entry: entry, Pin: pin, Files: files,
		Revision: store.AssessedResourceRevision{
			ResourceRevision: store.ResourceRevision{
				Coordinate: coordinate, ContentDigest: store.RevisionDigest(hostBinding.PackageContentDigest),
				ArchiveDigest: archiveDigest,
			},
			Assessment: assessment, EffectiveAvailability: "available",
		},
	}
}

func targetApprovalEnvironment(t *testing.T, fixture targetApprovalFixture) targetprofile.ApprovalEnvironment {
	t.Helper()
	platform := "linux"
	if runtime.GOOS == "darwin" {
		platform = "macos"
	}
	return targetprofile.ApprovalEnvironment{
		Platform: platform, Home: fixture.home, XDGConfig: filepath.Join(fixture.home, ".config"),
		CanonicalStateRoot: fixture.stateRoot,
	}
}

func assertOwnedTargetPackage(t *testing.T, fixture targetApprovalFixture, binding targetprofile.ApprovalBinding, fileBytes []byte) (string, []byte, os.FileInfo) {
	t.Helper()
	owner, err := ownership.Open(binding.CanonicalRoot, ownership.WithStateRoot(binding.CanonicalStateRoot))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := owner.SnapshotState()
	if err != nil {
		t.Fatal(err)
	}
	projection, err := owner.PlaceFromSnapshot(snapshot)
	if err != nil || len(projection.Packages) != 1 || projection.Packages[0].Binding.ProfileIdentity != binding.Profile {
		t.Fatalf("owned place projection = %#v, %v", projection, err)
	}
	targetPath := filepath.Join(fixture.targetRoot, "mods", "sample", "guide.md")
	if data, err := os.ReadFile(targetPath); err != nil || !bytes.Equal(data, fileBytes) {
		t.Fatalf("owned target file = %q, %v", data, err)
	}
	statePath := owner.StateFile()
	stateBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(stateBytes) == 0 {
		t.Fatal("ownership state file is empty")
	}
	stateInfo, err := os.Stat(statePath)
	if err != nil {
		t.Fatal(err)
	}
	return statePath, stateBytes, stateInfo
}

type targetStateFileSnapshot struct {
	bytes   []byte
	mode    os.FileMode
	modTime time.Time
}

func snapshotTargetStateTree(t *testing.T, root string) map[string]targetStateFileSnapshot {
	t.Helper()
	files := make(map[string]targetStateFileSnapshot)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
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
		info, err := entry.Info()
		if err != nil {
			return err
		}
		files[relative] = targetStateFileSnapshot{
			bytes: data, mode: info.Mode(), modTime: info.ModTime(),
		}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot target state tree %s: %v", root, err)
	}
	return files
}

func assertTargetStateTreeUnchanged(t *testing.T, before map[string]targetStateFileSnapshot, root string) {
	t.Helper()
	after := snapshotTargetStateTree(t, root)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("target state tree %s changed: before=%v after=%v", root, before, after)
	}
}

func assertOwnedTargetUnchanged(
	t *testing.T,
	fixture targetApprovalFixture,
	binding targetprofile.ApprovalBinding,
	statePath string,
	stateBytes []byte,
	stateInfo os.FileInfo,
	targetPath string,
	targetBytes []byte,
	targetInfo os.FileInfo,
	receiptPath string,
	receiptBytes []byte,
	lockBytes []byte,
) {
	t.Helper()
	if data, err := os.ReadFile(targetPath); err != nil || !bytes.Equal(data, targetBytes) {
		t.Fatalf("owned target changed: %q, %v", data, err)
	}
	targetAfter, err := os.Stat(targetPath)
	if err != nil || !targetInfo.ModTime().Equal(targetAfter.ModTime()) {
		t.Fatalf("owned target mtime changed: before=%v after=%v err=%v", targetInfo, targetAfter, err)
	}
	stateAfter, err := os.ReadFile(statePath)
	stateAfterInfo, statErr := os.Stat(statePath)
	if err != nil || statErr != nil || !bytes.Equal(stateAfter, stateBytes) || !stateInfo.ModTime().Equal(stateAfterInfo.ModTime()) {
		t.Fatalf("ownership state changed: read=%v stat=%v", err, statErr)
	}
	if _, _, info := assertOwnedTargetPackage(t, fixture, binding, targetBytes); !stateInfo.ModTime().Equal(info.ModTime()) {
		t.Fatalf("ownership projection timestamp changed: before=%v after=%v", stateInfo, info)
	}
	if data, err := os.ReadFile(receiptPath); err != nil || !bytes.Equal(data, receiptBytes) {
		t.Fatalf("approval receipt changed: %v", err)
	}
	if data, err := os.ReadFile(fixture.userLock); err != nil || !bytes.Equal(data, lockBytes) {
		t.Fatalf("deployed lock changed: %v", err)
	}
}

func assertTargetApprovalFixtureUnchanged(t *testing.T, fixture targetApprovalFixture, expectedReceipts ...int) {
	t.Helper()
	wantReceipts := 0
	if len(expectedReceipts) > 0 {
		wantReceipts = expectedReceipts[0]
	}
	if data, err := os.ReadFile(fixture.userConfig); err != nil || !bytes.Equal(data, fixture.userConfigBytes) {
		t.Fatalf("user config changed: %q, %v", data, err)
	}
	lockData, err := os.ReadFile(fixture.userLock)
	if err != nil {
		t.Fatalf("read user lock: %v", err)
	}
	if !bytes.Equal(lockData, fixture.userLockBytes) {
		t.Fatal("deployed user lock changed")
	}
	rootFile := filepath.Join(fixture.targetRoot, "keep.txt")
	if data, err := os.ReadFile(rootFile); err != nil || !bytes.Equal(data, fixture.rootBytes) {
		t.Fatalf("target root changed: %q, %v", data, err)
	}
	rootEntries, err := os.ReadDir(fixture.targetRoot)
	if err != nil || len(rootEntries) != 1 || rootEntries[0].Name() != "keep.txt" {
		t.Fatalf("target root inventory changed: %v, %v", rootEntries, err)
	}
	stateEntries, err := os.ReadDir(fixture.stateRoot)
	if err != nil || len(stateEntries) != wantReceipts {
		t.Fatalf("state root inventory = %v, %v; want %d receipt(s)", stateEntries, err, wantReceipts)
	}
}

type targetApprovalPromptWriter struct {
	output *bytes.Buffer
	prompt func()
}

func (w targetApprovalPromptWriter) Write(p []byte) (int, error) {
	n, err := w.output.Write(p)
	if w.prompt != nil && strings.Contains(string(p), "Type yes to approve:") {
		w.prompt()
	}
	return n, err
}

func executeTargetApprovalAtTerminal(t *testing.T, input string, prompt func(), args ...string) (string, string, error) {
	t.Helper()
	previous := isTerminal
	isTerminal = func(uintptr) bool { return true }
	t.Cleanup(func() { isTerminal = previous })
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	go func() {
		_, _ = writer.WriteString(input)
		_ = writer.Close()
	}()
	cmd := newRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(targetApprovalPromptWriter{output: &stderr, prompt: prompt})
	cmd.SetIn(reader)
	cmd.SetArgs(args)
	cmd.SetContext(context.Background())
	execCmd, execErr := cmd.ExecuteC()
	finishTelemetry(execCmd, execErr)
	finishRootLogging(execErr)
	return stdout.String(), stderr.String(), execErr
}
