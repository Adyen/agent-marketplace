package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
)

// spec: update-preview/live-sources-are-not-mistaken-for-unchanged-versioned-resources
func TestUpdatePreviewLeavesOrganizationMarketplaceCollectionUnchecked(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, cacheState := range []string{"cold", "warm"} {
		t.Run(cacheState, func(t *testing.T) {
			_, working, repoRoot, configPath := previewTestSetup(t)
			if output, err := executeCommand("sync"); err != nil {
				t.Fatalf("initial repository sync: %v\n%s", err, output)
			}
			server := newV4MarketplaceCollectionServer(t, "org-"+cacheState, "org-bundle", "org-review")
			t.Cleanup(server.Close)
			authority := t.TempDir()
			orgPath := filepath.Join(authority, "config.yaml")
			writeConfig(t, orgPath, "apiVersion: agent-marketplace/v4\nsources:\n  hosted: {marketplace: "+server.URL+"}\nresources:\n  hosted: {include: [tools/collection/org-bundle]}\n")
			config.SetTestOrganizationConfigPath(t, orgPath)
			if cacheState == "warm" {
				if output, err := executeCommand("sync"); err != nil {
					t.Fatalf("warm organization collection: %v\n%s", err, output)
				}
			}
			writeSkill(t, working, "vm-lifecycle", "Repository preview candidate v2", []string{"openstack"})
			commitAndPushAll(t, working, "advance repository candidate")
			lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
			beforeLock := string(readRequiredFile(t, lockPath))
			beforeConfig := string(readRequiredFile(t, configPath))
			beforeRepo := snapshotConsumerTree(t, repoRoot)
			beforeOrg := snapshotConsumerTree(t, authority)
			machineRoot := filepath.Join(os.Getenv("HOME"), ".factory")
			beforeMachine := snapshotConsumerTree(t, machineRoot)

			output, err := updateDryRun(t, "--include-diff")
			if err != nil {
				t.Fatalf("preview alongside %s organization collection: %v\n%s", cacheState, err, output)
			}
			for _, want := range []string{"Organization-managed Marketplace pins: 2", "Use `agent-marketplace update --verbose`", "Repository preview candidate v2", "@@", "Dry run: no updates were applied."} {
				if !strings.Contains(output, want) {
					t.Errorf("preview missing %q:\n%s", want, output)
				}
			}
			if strings.Contains(output, `"org-bundle": not checked`) || strings.Contains(output, `"org-review": not checked`) {
				t.Errorf("non-verbose preview listed organization pins:\n%s", output)
			}
			verboseOutput, verboseErr := updateDryRun(t, "--include-diff", "--verbose")
			if verboseErr != nil {
				t.Fatalf("verbose organization preview: %v\n%s", verboseErr, verboseOutput)
			}
			for _, want := range []string{`"collection"/"org-bundle": not checked (organization-managed)`, `"skill"/"org-review": not checked (organization-managed)`, "Organization-managed Marketplace pins: 2"} {
				if !strings.Contains(verboseOutput, want) {
					t.Errorf("verbose preview missing %q:\n%s", want, verboseOutput)
				}
			}
			_, contentDiffs, found := strings.Cut(output, "Resource content diffs")
			if !found {
				t.Fatalf("preview did not print requested content diffs:\n%s", output)
			}
			for _, unwanted := range []string{"org-bundle", "org-review", "Review carefully."} {
				if strings.Contains(contentDiffs, unwanted) {
					t.Errorf("preview compared an organization-managed definition %q:\n%s", unwanted, output)
				}
			}
			if got := string(readRequiredFile(t, lockPath)); got != beforeLock {
				t.Error("preview changed the repository lock")
			}
			if got := string(readRequiredFile(t, configPath)); got != beforeConfig {
				t.Error("preview changed the repository config")
			}
			assertTreesIdentical(t, "repository", beforeRepo, snapshotConsumerTree(t, repoRoot))
			assertTreesIdentical(t, "organization", beforeOrg, snapshotConsumerTree(t, authority))
			assertTreesIdentical(t, "machine installation", beforeMachine, snapshotConsumerTree(t, machineRoot))

			// Applying really installs both the repository candidate and the
			// organization's selected member, without taking over its lock.
			if output, err := executeCommand("update", "--include-diff"); err != nil {
				t.Fatalf("apply alongside organization collection: %v\n%s", err, output)
			}
			if got := string(readRequiredFile(t, lockPath)); got == beforeLock {
				t.Error("applying update did not advance the repository lock")
			}
			repoSkill := filepath.Join(repoRoot, ".factory", "skills", "vm-lifecycle", "SKILL.md")
			if body := string(readRequiredFile(t, repoSkill)); !strings.Contains(body, "Repository preview candidate v2") {
				t.Errorf("applying update missed the repository candidate:\n%s", body)
			}
			orgSkill := filepath.Join(machineRoot, "skills", "org-review", "SKILL.md")
			if body := string(readRequiredFile(t, orgSkill)); !strings.Contains(body, "Review carefully.") {
				t.Errorf("organization collection member was not installed:\n%s", body)
			}
			assertTreesIdentical(t, "organization after apply", beforeOrg, snapshotConsumerTree(t, authority))
		})
	}
}

// spec: update-output/missing-resource-preview-retains-state-and-reports-other-candidates
func TestUpdateDryRunMissingMarketplacePinRetainsStateAndReportsOthers(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server := newV4UpdateServer(t, "stay", "gone")
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  mp: {marketplace: "+server.URL+"}\n")
	repo := t.TempDir()
	withWorkingDir(t, repo)
	for _, name := range []string{"stay", "gone"} {
		if output, err := executeCommand("install", "--scope", "repo", "tools/skill/"+name, "--marketplace", "mp", "--yes"); err != nil {
			t.Fatalf("install %s: %v\n%s", name, err, output)
		}
	}
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	goneBefore := commandLockResource(t, lockPath, "gone")
	stayBefore := commandLockResource(t, lockPath, "stay")
	beforeLock := readRequiredFile(t, lockPath)
	beforeState := snapshotConsumerTree(t, repo)

	server.setSkill(t, "stay", "stay body v2")
	server.resources["gone"].missing.Store(true)

	output, err := updateDryRun(t, "--include-diff")
	if err == nil {
		t.Fatalf("missing-pin preview succeeded, want exit 1:\n%s", output)
	}
	for _, want := range []string{
		"Dry run: no updates were applied.",
		"Marketplace pins proposed for update: 1",
		"update",
		"gone",
		"agent-marketplace remove gone",
		"+stay body v2",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing-pin preview missing %q:\n%s", want, output)
		}
	}
	// The retained missing pin has no proposed content change: its lock entry
	// stays at the pinned revision and no diff is invented for it.
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "gone") && (strings.Contains(line, "modified") || strings.Contains(line, "added") || strings.Contains(line, "removed") || strings.HasPrefix(strings.TrimSpace(line), "+") || strings.HasPrefix(strings.TrimSpace(line), "-")) {
			t.Fatalf("missing pin got an invented content change or hunk: %s", line)
		}
	}

	if string(readRequiredFile(t, lockPath)) != string(beforeLock) {
		t.Fatalf("missing-pin preview changed lock bytes")
	}
	assertTreesIdentical(t, "consumer", beforeState, snapshotConsumerTree(t, repo))
	if after := commandLockResource(t, lockPath, "gone"); after == nil || !reflect.DeepEqual(*after, *goneBefore) {
		t.Fatalf("missing pin lock entry changed: got %#v, want %#v", after, goneBefore)
	}
	if after := commandLockResource(t, lockPath, "stay"); after == nil || !reflect.DeepEqual(*after, *stayBefore) {
		t.Fatalf("eligible pin lock entry changed during preview: got %#v, want %#v", after, stayBefore)
	}
}

// spec: update-output/applied-diffs-retain-partial-update-behavior
func TestUpdateIncludeDiffAppliesEligibleAndReportsRetainedMissing(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server := newV4UpdateServer(t, "stay", "gone")
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  mp: {marketplace: "+server.URL+"}\n")
	repo := t.TempDir()
	withWorkingDir(t, repo)
	for _, name := range []string{"stay", "gone"} {
		if output, err := executeCommand("install", "--scope", "repo", "tools/skill/"+name, "--marketplace", "mp", "--yes"); err != nil {
			t.Fatalf("install %s: %v\n%s", name, err, output)
		}
	}
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	goneBefore := commandLockResource(t, lockPath, "gone")

	server.setSkill(t, "stay", "stay body v2 applied")
	server.resources["gone"].missing.Store(true)

	output, err := executeCommand("update", "--include-diff")
	if err == nil {
		t.Fatalf("partial update succeeded, want exit 1:\n%s", output)
	}
	for _, want := range []string{
		"Marketplace pins updated: 1",
		"+stay body v2 applied",
		"gone",
		"agent-marketplace remove gone",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("applied partial diff report missing %q:\n%s", want, output)
		}
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "\"gone\"") && (strings.Contains(line, "modified") || strings.Contains(line, "added") || strings.Contains(line, "removed")) {
			t.Fatalf("retained missing pin was reported as an invented content change: %s", line)
		}
	}
	if after := commandLockResource(t, lockPath, "gone"); after == nil || !reflect.DeepEqual(*after, *goneBefore) {
		t.Fatalf("missing pin lock entry changed: got %#v, want %#v", after, goneBefore)
	}
	stayAfter := commandLockResource(t, lockPath, "stay")
	if stayAfter == nil || string(stayAfter.ContentDigest) != string(server.resources["stay"].revision.ContentDigest) {
		t.Fatalf("eligible pin was not updated: %+v", stayAfter)
	}
	if output, err := executeCommand("sync", "--check"); err != nil || strings.Contains(output, "out of sync") {
		t.Fatalf("sync --check after partial update = (%v):\n%s", err, output)
	}
}

// spec: update-preview/pending-dependency-transaction-is-not-replayed
func TestUpdateDryRunRefusesPendingJournalWithoutReplay(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, working, repoRoot, configPath := previewTestSetup(t)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v2 newer upstream", []string{"openstack"})
	commitAndPushAll(t, working, "advance v2")

	stateDir, err := dependencyStateDir()
	if err != nil {
		t.Fatal(err)
	}
	configGeneration, err := dependencytxn.ReadGeneration(configPath)
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	lockGeneration, err := dependencytxn.ReadGeneration(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("interrupted transaction")
	_, err = dependencytxn.Commit(context.Background(), stateDir, []dependencytxn.DesiredFile{
		{Path: configPath, Expected: configGeneration, Bytes: []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []\n"), Mode: 0o600},
		{Path: lockPath, Expected: lockGeneration, Bytes: []byte("apiVersion: agent-marketplace-lock/v1\nscope: repo\nregistries: []\n"), Mode: 0o640},
	}, dependencytxn.WithFaultHook(func(point dependencytxn.FaultPoint) error {
		if point.Stage == dependencytxn.FaultAfterJournal {
			return injected
		}
		return nil
	}))
	if !errors.Is(err, injected) {
		t.Fatalf("seed pending transaction: %v", err)
	}
	before := snapshotConsumerTree(t, repoRoot)

	for _, args := range [][]string{{}, {"--include-diff"}} {
		output, err := updateDryRun(t, args...)
		if !strings.Contains(err.Error(), "dependency.recovery-required") {
			t.Fatalf("dry-run %v does not refuse the pending journal: %v\n%s", args, err, output)
		}
	}
	assertTreesIdentical(t, "consumer", before, snapshotConsumerTree(t, repoRoot))
}

// spec: update-preview/unsupported-consumer-input-refuses-before-new-work
func TestUpdateDryRunUnsupportedRootRefusesBeforeNewWork(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	origin, working, repoRoot, _ := previewTestSetup(t)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v2 newer upstream", []string{"openstack"})
	commitAndPushAll(t, working, "advance v2")

	// Replace the required root with a versionless document and remove the
	// existing lock so refusal must happen before any new acquisition or lock
	// creation.
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	if err := os.Remove(filepath.Join(repoRoot, ".agents", "lock.yaml")); err != nil {
		t.Fatal(err)
	}
	// writeFile, not writeConfig: the helper adds the required apiVersion to
	// repo config.yaml, which would hide the versionless-root refusal.
	writeFile(t, configPath, "harnesses: [factory]\nsources:\n  infra:\n    git: file://"+origin+"\n    ref: main\nresources:\n  infra:\n    include: [skill/vm-lifecycle]\n")

	beforeCache := snapshotConsumerTree(t, cache.CacheDir())
	if len(beforeCache) == 0 {
		t.Fatal("refusal fixture has no acquired cache state to preserve")
	}

	for _, args := range [][]string{{}, {"--include-diff"}} {
		output, err := updateDryRun(t, args...)
		if err == nil {
			t.Fatalf("dry-run %v accepted a versionless required root:\n%s", args, output)
		}
		if !strings.Contains(output, "v4.api-version-required") {
			t.Fatalf("refusal does not name the required apiVersion refusal:\n%s\nerror: %v", output, err)
		}
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".agents", "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("refused dry-run created the missing lock: %v", err)
	}
	assertTreesIdentical(t, "cache", beforeCache, snapshotConsumerTree(t, cache.CacheDir()))
}

// spec: update-preview/preview-preserves-an-existing-installation-in-either-materialization-mode
func TestUpdateDryRunPreservesInstallationInBothMaterializationModes(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	origin, working, repoRoot, configPath := previewTestSetup(t)
	// One child runs both modes; command-test isolation matches top-level
	// test names only, so the matrix stays in this single child.
	for _, mode := range []string{"symlink", "file"} {
		if mode == "file" {
			writeConfig(t, configPath, `
apiVersion: agent-marketplace/v4
materialization: {mode: file}
sources:
  infra:
    git: file://`+origin+`
    ref: main
resources:
  infra:
    include: [skill/vm-lifecycle]
`)
		}
		if output, err := executeCommand("sync"); err != nil {
			t.Fatalf("sync (%s mode): %v\n%s", mode, err, output)
		}
		stateHome := os.Getenv("XDG_STATE_HOME")
		if stateHome == "" {
			stateHome = filepath.Join(os.Getenv("HOME"), ".local", "state")
		}
		beforeState := snapshotConsumerTree(t, repoRoot)
		beforeOwnership := snapshotConsumerTree(t, filepath.Join(stateHome, "agent-marketplace"))

		writeSkill(t, working, "vm-lifecycle", "VM lifecycle v2 for "+mode, []string{"openstack"})
		writeSupportFile(t, working, "vm-lifecycle", "runbook.txt", "v2 runbook "+mode)
		commitAndPushAll(t, working, "advance v2 for "+mode)

		output, err := updateDryRun(t, "--include-diff")
		if err != nil {
			t.Fatalf("dry-run (%s mode) failed: %v\n%s", mode, err, output)
		}
		if !strings.Contains(output, "VM lifecycle v2 for "+mode) {
			t.Fatalf("dry-run (%s mode) did not show the proposed update:\n%s", mode, output)
		}
		assertTreesIdentical(t, "consumer ("+mode+" mode)", beforeState, snapshotConsumerTree(t, repoRoot))
		assertTreesIdentical(t, "ownership-state ("+mode+" mode)", beforeOwnership, snapshotConsumerTree(t, filepath.Join(stateHome, "agent-marketplace")))

		// Mutation control: the fixture detects a real mutation, so the
		// no-write assertions above are not vacuous.
		if output, err := executeCommand("update"); err != nil {
			t.Fatalf("plain update (%s mode) failed: %v\n%s", mode, err, output)
		}
		installed := filepath.Join(repoRoot, ".factory", "skills", "vm-lifecycle", "SKILL.md")
		if body := readRequiredFile(t, installed); !strings.Contains(string(body), "VM lifecycle v2 for "+mode) {
			t.Fatalf("plain update (%s mode) did not change the installed body; preservation assertions are vacuous:\n%s", mode, body)
		}
	}
}

// spec: update-preview/preview-preserves-targeting-and-ref-bump-behavior
func TestUpdateDryRunBumpMatchesApplyingApplicability(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	origin, working, repoRoot, configPath := previewTestSetup(t)
	createAndPushTag(t, working, "v1.0.0")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v2", []string{"openstack"})
	writeSupportFile(t, working, "vm-lifecycle", "runbook.txt", "v2 runbook")
	commitAndPushAll(t, working, "advance v2")
	createAndPushTag(t, working, "v1.0.1")
	writeConfig(t, configPath, `
apiVersion: agent-marketplace/v4
sources:
  infra:
    git: file://`+origin+`
    ref: v1.0.0
resources:
  infra:
    include: [skill/vm-lifecycle]
`)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	beforeConfig := readRequiredFile(t, configPath)
	beforeLock := readRequiredFile(t, filepath.Join(repoRoot, ".agents", "lock.yaml"))
	beforeState := snapshotConsumerTree(t, repoRoot)

	output, err := updateDryRun(t, "--bump")
	if err != nil {
		t.Fatalf("dry-run --bump failed: %v\n%s", err, output)
	}
	for _, want := range []string{"refs to bump: 1", "v1.0.1", "Dry run: no updates were applied."} {
		if !strings.Contains(output, want) {
			t.Fatalf("dry-run --bump missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "initial acquisition") {
		t.Errorf("ref bump was reported as initial acquisition:\n%s", output)
	}
	if !strings.Contains(output, `(ref "v1.0.0")`) || !strings.Contains(output, `(ref "v1.0.1")`) {
		t.Errorf("ref bump omitted the old or candidate ref:\n%s", output)
	}
	if string(readRequiredFile(t, configPath)) != string(beforeConfig) {
		t.Fatalf("dry-run --bump changed config bytes")
	}
	if string(readRequiredFile(t, filepath.Join(repoRoot, ".agents", "lock.yaml"))) != string(beforeLock) {
		t.Fatalf("dry-run --bump changed lock bytes")
	}
	assertTreesIdentical(t, "consumer", beforeState, snapshotConsumerTree(t, repoRoot))

	// Control: applying the same flags performs exactly the proposed edit.
	applyOutput, err := executeCommand("update", "--bump")
	if err != nil {
		t.Fatalf("applying update --bump failed: %v\n%s", err, applyOutput)
	}
	afterConfig := readRequiredFile(t, configPath)
	if !strings.Contains(string(afterConfig), "ref: v1.0.1") {
		t.Fatalf("applying bump did not rewrite the ref the preview proposed:\n%s", afterConfig)
	}
}
