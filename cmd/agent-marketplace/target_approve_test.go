package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/gitidentity"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/sourcerevision"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
)

const targetApproveProfileYAML = `apiVersion: agent-marketplace.io/target-profile/v1
id: invented-app
label: Invented App
platforms: [macos, linux]
roots: {user: {override: INVENTED_APP_HOME, default: {macos: {base: home, path: .config/invented-app}, linux: {base: home, path: .config/invented-app}}, allowed_bases: [home]}}
kinds: {mod: {deliver: place, into: [mods/]}}
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
	profileBytes := []byte(targetApproveProfileYAML)
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
	writeConfig(t, userConfig, fmt.Sprintf(`apiVersion: agent-marketplace/v4
sources:
  profile: {git: %q, ref: main, path: profiles, source_harness: factory}
  hosted: {marketplace: https://market.example}
resources:
  hosted: [tools/target-profile/invented-app]
`, rawGitURL))
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
	reader := pipeline.LockedGitPlaceProfileReader(fixture.gitLock, fixture.rawGitURL)
	profileBytes, err := reader(context.Background(), fixture.identity, resolver.MarketplaceResolveModeRefresh)
	if err != nil || !bytes.Equal(profileBytes, fixture.profileBytes) {
		t.Fatalf("warm exact local profile cache = %q, %v", profileBytes, err)
	}
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
	if result.Status != "approved" || result.Binding.Profile != fixture.identity || !result.Binding.NoExecution ||
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
