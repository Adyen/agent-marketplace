package main

import (
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resourcediff"
)

// previewFileState records one path's observable filesystem state.
type previewFileState struct {
	size    int64
	mode    os.FileMode
	modTime time.Time
	link    string
	digest  [sha256.Size]byte
}

// snapshotConsumerTree records every path under root with its size, mode,
// modification time, content digest, and symlink target so a preview test can prove the
// command changed nothing it was not allowed to change.
func snapshotConsumerTree(t *testing.T, root string) map[string]previewFileState {
	t.Helper()
	snapshot := map[string]previewFileState{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		state := previewFileState{size: info.Size(), mode: info.Mode(), modTime: info.ModTime()}
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			state.digest = sha256.Sum256(data)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err == nil {
				state.link = target
			}
		}
		snapshot[rel] = state
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return snapshot
}

func assertTreesIdentical(t *testing.T, label string, before, after map[string]previewFileState) {
	t.Helper()
	missing := make([]string, 0)
	extra := make([]string, 0)
	changed := make([]string, 0)
	for path, want := range before {
		got, ok := after[path]
		if !ok {
			missing = append(missing, path)
			continue
		}
		if got.size != want.size || got.mode != want.mode || got.link != want.link || got.digest != want.digest || !got.modTime.Equal(want.modTime) {
			changed = append(changed, path)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			extra = append(extra, path)
		}
	}
	if len(missing) == 0 && len(extra) == 0 && len(changed) == 0 {
		return
	}
	sort.Strings(missing)
	sort.Strings(extra)
	sort.Strings(changed)
	t.Fatalf("%s state changed: missing=%v extra=%v changed=%v", label, missing, extra, changed)
}

func gitHeadRevision(t *testing.T, repoDir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD in %s: %v", repoDir, err)
	}
	return strings.TrimSpace(string(output))
}

func previewTestSetup(t *testing.T) (origin, working, repoRoot, configPath string) {
	t.Helper()
	originDir, workingDir := createBareRepoWithWorkingClone(t, "example-preview-skills.git")
	writeSkill(t, workingDir, "vm-lifecycle", "VM lifecycle v1", []string{"openstack"})
	writeSupportFile(t, workingDir, "vm-lifecycle", "runbook.txt", "v1 runbook contents")
	commitAndPushAll(t, workingDir, "seed v1")

	repo := filepath.Join(t.TempDir(), "repo")
	cfg := filepath.Join(repo, ".agents", "config.yaml")
	writeConfig(t, cfg, `
apiVersion: agent-marketplace/v4
sources:
  infra:
    git: file://`+originDir+`
    ref: main
resources:
  infra:
    include: [skill/vm-lifecycle]
`)
	withWorkingDir(t, repo)
	return originDir, workingDir, repo, cfg
}

func writeSupportFile(t *testing.T, repoDir, skill, name, content string) {
	t.Helper()
	dir := filepath.Join(repoDir, "skills", skill)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write support file: %v", err)
	}
}

func readRequiredFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func updateDryRun(t *testing.T, args ...string) (string, error) {
	t.Helper()
	if len(args) == 0 {
		return executeCommand("update", "--dry-run")
	}
	full := append([]string{"update", "--dry-run"}, args...)
	return executeCommand(full...)
}

// spec: update-preview/preview-summarizes-a-real-update-without-content-hunks
func TestUpdateDryRunSummarizesRealUpdateWithoutApplying(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	origin, working, repoRoot, configPath := previewTestSetup(t)

	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}

	beforeConfig := readRequiredFile(t, configPath)
	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	beforeLock := readRequiredFile(t, lockPath)
	beforeLockDoc := readLockDocument(t, lockPath)
	if len(beforeLockDoc.Registries) != 1 || beforeLockDoc.Registries[0].Commit.IsZero() {
		t.Fatalf("expected one locked registry with a commit, got %+v", beforeLockDoc.Registries)
	}
	oldCommit := beforeLockDoc.Registries[0].Commit.String()
	installedSkill := filepath.Join(repoRoot, ".factory", "skills", "vm-lifecycle")
	installedTarget, err := filepath.EvalSymlinks(installedSkill)
	if err != nil {
		t.Fatalf("resolve installed skill: %v", err)
	}
	beforeState := snapshotConsumerTree(t, repoRoot)
	// Disposable cache writes are allowed, but the exact installed target
	// behind the symlink must retain its bytes and metadata.
	beforeCache := snapshotConsumerTree(t, installedTarget)
	if _, ok := beforeCache["SKILL.md"]; !ok {
		t.Fatal("cache-preservation fixture does not observe the installed target")
	}

	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v2", []string{"openstack"})
	writeSupportFile(t, working, "vm-lifecycle", "runbook.txt", "v2 runbook contents with detail")
	commitAndPushAll(t, working, "advance v2")
	newCommit := gitHeadRevision(t, working)

	output, err := updateDryRun(t)
	if err != nil {
		t.Fatalf("update --dry-run failed: %v\n%s", err, output)
	}

	for _, want := range []string{
		"Dry run: no updates were applied.",
		"repo skill \"vm-lifecycle\"",
		origin,
		oldCommit,
		newCommit,
		"modified",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("dry-run output missing %q:\n%s", want, output)
		}
	}
	for _, forbidden := range []string{"\n@@", "+++", "--- a/"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("dry-run summary without --include-diff contains diff marker %q:\n%s", forbidden, output)
		}
	}

	afterConfig := readRequiredFile(t, configPath)
	if string(afterConfig) != string(beforeConfig) {
		t.Fatalf("dry-run changed config bytes")
	}
	afterLock := readRequiredFile(t, lockPath)
	if string(afterLock) != string(beforeLock) {
		t.Fatalf("dry-run changed lock bytes")
	}
	afterState := snapshotConsumerTree(t, repoRoot)
	assertTreesIdentical(t, "consumer", beforeState, afterState)
	assertTreesIdentical(t, "installed target", beforeCache, snapshotConsumerTree(t, installedTarget))

	// The preview is a mutation control, not a no-op: the plain command still
	// applies the update the preview described, proving the fixture detects a
	// real mutation when one occurs.
	if output, err := executeCommand("update"); err != nil {
		t.Fatalf("plain update after dry-run failed: %v\n%s", err, output)
	}
	afterApplyDoc := readLockDocument(t, lockPath)
	if len(afterApplyDoc.Registries) != 1 || !strings.HasSuffix(afterApplyDoc.Registries[0].Commit.String(), newCommit) {
		t.Fatalf("plain update did not advance lock to %s: %+v", newCommit, afterApplyDoc.Registries)
	}
	updatedBody := readRequiredFile(t, filepath.Join(repoRoot, ".factory", "skills", "vm-lifecycle", "SKILL.md"))
	if !strings.Contains(string(updatedBody), "VM lifecycle v2") {
		t.Fatalf("installed body did not advance after plain update:\n%s", updatedBody)
	}
}

// spec: update-preview/preview-with-a-diff-uses-the-same-candidates
func TestUpdateDryRunWithAndWithoutIncludeDiffUseSameCandidates(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	origin, working, repoRoot, configPath := previewTestSetup(t)

	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}

	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	beforeLockDoc := readLockDocument(t, lockPath)
	oldCommit := beforeLockDoc.Registries[0].Commit.String()
	beforeConfig := readRequiredFile(t, configPath)
	beforeLock := readRequiredFile(t, lockPath)
	beforeState := snapshotConsumerTree(t, repoRoot)

	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v2 with distinctive body change", []string{"openstack"})
	writeSupportFile(t, working, "vm-lifecycle", "runbook.txt", "v2 runbook contents with detail")
	commitAndPushAll(t, working, "advance v2")
	newCommit := gitHeadRevision(t, working)

	summary, err := updateDryRun(t)
	if err != nil {
		t.Fatalf("update --dry-run failed: %v\n%s", err, summary)
	}
	withDiff, err := updateDryRun(t, "--include-diff")
	if err != nil {
		t.Fatalf("update --dry-run --include-diff failed: %v\n%s", err, withDiff)
	}

	for _, candidate := range []string{summary, withDiff} {
		for _, want := range []string{origin, oldCommit, newCommit, "repo skill \"vm-lifecycle\"", "modified", "Dry run: no updates were applied."} {
			if !strings.Contains(candidate, want) {
				t.Fatalf("preview output missing %q:\n%s", want, candidate)
			}
		}
	}
	if !strings.Contains(withDiff, "@@") || !strings.Contains(withDiff, `+  "description": "VM lifecycle v2 with distinctive body change"`) || !strings.Contains(withDiff, `-  "description": "VM lifecycle v1"`) {
		t.Fatalf("include-diff output lacks definition hunks:\n%s", withDiff)
	}
	if !strings.Contains(withDiff, "runbook.txt") || !strings.Contains(withDiff, "+v2 runbook contents with detail") {
		t.Fatalf("include-diff output lacks supporting-file hunks:\n%s", withDiff)
	}
	if strings.Contains(summary, "@@") {
		t.Fatalf("summary without --include-diff contains hunks:\n%s", summary)
	}

	afterConfig := readRequiredFile(t, configPath)
	if string(afterConfig) != string(beforeConfig) {
		t.Fatalf("dry-run with diff changed config bytes")
	}
	afterLock := readRequiredFile(t, lockPath)
	if string(afterLock) != string(beforeLock) {
		t.Fatalf("dry-run with diff changed lock bytes")
	}
	assertTreesIdentical(t, "consumer", beforeState, snapshotConsumerTree(t, repoRoot))
}

// spec: update-preview/no-update-is-a-successful-preview
func TestUpdateDryRunWithNoChangesExitsZero(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, _, repoRoot, configPath := previewTestSetup(t)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	beforeLock := readRequiredFile(t, filepath.Join(repoRoot, ".agents", "lock.yaml"))
	beforeConfig := readRequiredFile(t, configPath)

	for _, args := range [][]string{{}, {"--include-diff"}} {
		output, err := updateDryRun(t, args...)
		if err != nil {
			t.Fatalf("update --dry-run %v with no changes failed: %v\n%s", args, err, output)
		}
		for _, want := range []string{"No versioned updates are available.", "Dry run: no updates were applied."} {
			if !strings.Contains(output, want) {
				t.Fatalf("no-change preview missing %q:\n%s", want, output)
			}
		}
		if strings.Contains(output, "@@") {
			t.Fatalf("no-change preview printed hunks:\n%s", output)
		}
	}

	if string(readRequiredFile(t, configPath)) != string(beforeConfig) {
		t.Fatalf("no-change dry-run changed config bytes")
	}
	if string(readRequiredFile(t, filepath.Join(repoRoot, ".agents", "lock.yaml"))) != string(beforeLock) {
		t.Fatalf("no-change dry-run changed lock bytes")
	}
}

// spec: update-preview/initial-acquisition-is-previewed-without-creating-writable-roots
func TestUpdateDryRunInitialAcquisitionDoesNotCreateWritableRoots(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, working, repoRoot, _ := previewTestSetup(t)
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v2 acquired content", []string{"openstack"})
	writeSupportFile(t, working, "vm-lifecycle", "runbook.txt", "v2 runbook contents")
	commitAndPushAll(t, working, "advance v2")
	newCommit := gitHeadRevision(t, working)

	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("expected no lock before dry-run: %v", err)
	}

	output, err := updateDryRun(t, "--include-diff")
	if err != nil {
		t.Fatalf("initial-acquisition dry-run failed: %v\n%s", err, output)
	}
	for _, want := range []string{"initial acquisition", "Dry run: no updates were applied.", newCommit, "VM lifecycle v2 acquired content"} {
		if !strings.Contains(output, want) {
			t.Fatalf("initial-acquisition preview missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "previously installed") {
		t.Fatalf("initial acquisition was described as a comparison against previous content:\n%s", output)
	}

	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run created the missing lock: %v", err)
	}
	agentsDir := filepath.Join(repoRoot, ".agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		t.Fatalf("read .agents: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.yaml" {
		t.Fatalf("dry-run created files under .agents: %v", entries)
	}

	// Control: the plain command creates the lock the preview described.
	if output, err := executeCommand("update"); err != nil {
		t.Fatalf("plain update after dry-run failed: %v\n%s", err, output)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("plain update did not create lock: %v", err)
	}
	doc := readLockDocument(t, lockPath)
	if len(doc.Registries) != 1 || !strings.HasSuffix(doc.Registries[0].Commit.String(), newCommit) {
		t.Fatalf("plain update locked wrong commit: %+v", doc.Registries)
	}
}

// spec: update-preview/cancellation-preserves-state-and-removes-private-preparation
func TestUpdateDryRunPreparationFailurePreservesState(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	origin, _, repoRoot, configPath := previewTestSetup(t)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	beforeLock := readRequiredFile(t, filepath.Join(repoRoot, ".agents", "lock.yaml"))
	beforeConfig := readRequiredFile(t, configPath)
	beforeState := snapshotConsumerTree(t, repoRoot)

	// Make candidate acquisition impossible while keeping prior state intact.
	if err := os.RemoveAll(origin); err != nil {
		t.Fatalf("remove origin: %v", err)
	}

	output, err := updateDryRun(t, "--include-diff")
	if err == nil {
		t.Fatalf("dry-run succeeded with a missing upstream:\n%s", output)
	}
	if string(readRequiredFile(t, configPath)) != string(beforeConfig) {
		t.Fatalf("failed dry-run changed config bytes")
	}
	if string(readRequiredFile(t, filepath.Join(repoRoot, ".agents", "lock.yaml"))) != string(beforeLock) {
		t.Fatalf("failed dry-run changed lock bytes")
	}
	assertTreesIdentical(t, "consumer", beforeState, snapshotConsumerTree(t, repoRoot))

	// Private attempt-owned preparation leaves no residue: neither the
	// disposable cache nor the private staging area keeps attempt-owned files.
	for _, pattern := range []string{
		filepath.Join(cache.CacheDir(), "*staging*"),
		filepath.Join(os.TempDir(), ".agent-marketplace-config-*"),
	} {
		residue, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("glob %s: %v", pattern, err)
		}
		if len(residue) > 0 {
			t.Fatalf("preparation residue after failed dry-run: %v", residue)
		}
	}
}

// spec: update-preview/newly-missing-selection-blocks-the-preview
func TestUpdateDryRunRetainsNewlyMissingSelectionRefusal(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	origin, working, repoRoot, _ := previewTestSetup(t)
	writeSkill(t, working, "aux-skill", "Aux skill v1", []string{"aux"})
	commitAndPushAll(t, working, "seed both skills")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
apiVersion: agent-marketplace/v4
sources:
  infra:
    git: file://`+origin+`
    ref: main
resources:
  infra:
    include: [skill/vm-lifecycle, skill/aux-skill]
`)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	beforeLock := readRequiredFile(t, lockPath)

	// Remove a directly selected resource upstream and advance the ref.
	if err := os.RemoveAll(filepath.Join(working, "skills", "aux-skill")); err != nil {
		t.Fatalf("remove aux-skill: %v", err)
	}
	commitAndPushAll(t, working, "drop aux-skill")

	for _, args := range [][]string{{}, {"--include-diff"}} {
		output, err := updateDryRun(t, args...)
		if err == nil {
			t.Fatalf("dry-run %v accepted a newly missing direct selection:\n%s", args, output)
		}
		if !strings.Contains(output, "aux-skill") || !strings.Contains(output, "selection") {
			t.Fatalf("dry-run refusal does not name the missing selection:\n%s\nerror: %v", output, err)
		}
		if strings.Contains(output, "removed") || strings.Contains(output, "removal") {
			t.Fatalf("dry-run presented the missing selection as a removal:\n%s", output)
		}
		if strings.Contains(output, "Dry run: no updates were applied.") {
			// The refusal message must not present the blocked update as a
			// successful preview summary.
			t.Fatalf("blocked preview printed the success summary:\n%s", output)
		}
	}
	if string(readRequiredFile(t, lockPath)) != string(beforeLock) {
		t.Fatalf("refused dry-run changed lock bytes")
	}
}

// spec: update-preview/live-sources-are-not-mistaken-for-unchanged-versioned-resources
func TestUpdateDryRunLabelsLiveDirectorySourceWithoutHistoricalComparison(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	origin, working, repoRoot, _ := previewTestSetup(t)
	liveDir := filepath.Join(t.TempDir(), "live-registry")
	writeSkill(t, liveDir, "live-skill", "Live skill body", []string{"live"})
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
apiVersion: agent-marketplace/v4
sources:
  infra:
    git: file://`+origin+`
    ref: main
  local:
    directory: `+liveDir+`
resources:
  infra:
    include: [skill/vm-lifecycle]
  local:
    include: [skill/live-skill]
`)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v2", []string{"openstack"})
	commitAndPushAll(t, working, "advance v2")

	output, err := updateDryRun(t)
	if err != nil {
		t.Fatalf("dry-run with live source failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "no historical locked comparison") {
		t.Fatalf("dry-run does not label the live source:\n%s", output)
	}
	if !strings.Contains(output, "live-registry") {
		t.Fatalf("dry-run does not identify the live source path:\n%s", output)
	}
	if !strings.Contains(output, "repo skill \"vm-lifecycle\"") {
		t.Fatalf("dry-run omits the comparable versioned change:\n%s", output)
	}
	if !strings.Contains(output, "does not enumerate every subsequent materialization change") {
		t.Fatalf("dry-run does not state the materialization limit:\n%s", output)
	}
}

// spec: update-preview/installed-file-drift-does-not-enter-the-upstream-diff
func TestUpdateDryRunDiffComparesLockedRevisionNotInstalledDrift(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	origin, working, repoRoot, configPath := previewTestSetup(t)
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
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	installedSkill := filepath.Join(repoRoot, ".factory", "skills", "vm-lifecycle", "SKILL.md")

	// Drift the installed copy only; the locked snapshot stays authoritative.
	drifted := strings.Replace(string(readRequiredFile(t, installedSkill)), "VM lifecycle v1", "LOCAL DRIFT EDITED COPY", 1)
	if err := os.WriteFile(installedSkill, []byte(drifted), 0o644); err != nil {
		t.Fatalf("drift installed copy: %v", err)
	}
	beforeState := snapshotConsumerTree(t, repoRoot)

	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v2 upstream change", []string{"openstack"})
	commitAndPushAll(t, working, "advance v2")

	output, err := updateDryRun(t, "--include-diff")
	if err != nil {
		t.Fatalf("dry-run with drifted installation failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, `-  "description": "VM lifecycle v1"`) || !strings.Contains(output, `+  "description": "VM lifecycle v2 upstream change"`) {
		t.Fatalf("diff does not compare the locked revision to the candidate:\n%s", output)
	}
	if strings.Contains(output, "LOCAL DRIFT EDITED COPY") {
		t.Fatalf("diff includes local installed-file drift:\n%s", output)
	}
	if strings.Contains(output, "ownership checks") {
		t.Fatalf("preview claims completed ownership checks:\n%s", output)
	}
	assertTreesIdentical(t, "consumer", beforeState, snapshotConsumerTree(t, repoRoot))
}

// spec: update-preview/separate-invocations-may-discover-different-revisions
func TestUpdatePreviewDoesNotReserveCandidateRevisions(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, working, repoRoot, _ := previewTestSetup(t)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}

	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v2", []string{"openstack"})
	commitAndPushAll(t, working, "advance v2")
	previewedCommit := gitHeadRevision(t, working)

	if output, err := updateDryRun(t); err != nil {
		t.Fatalf("first dry-run failed: %v\n%s", err, output)
	}

	// Upstream advances after the preview.
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v3", []string{"openstack"})
	commitAndPushAll(t, working, "advance v3")
	finalCommit := gitHeadRevision(t, working)

	if output, err := executeCommand("update"); err != nil {
		t.Fatalf("later plain update failed: %v\n%s", err, output)
	}
	doc := readLockDocument(t, filepath.Join(repoRoot, ".agents", "lock.yaml"))
	if len(doc.Registries) != 1 {
		t.Fatalf("expected one registry lock: %+v", doc.Registries)
	}
	locked := doc.Registries[0].Commit.String()
	if strings.HasSuffix(locked, previewedCommit) || !strings.HasSuffix(locked, finalCommit) {
		t.Fatalf("later update locked %q; expected the fresh candidate %q, not the previewed %q", locked, finalCommit, previewedCommit)
	}
}

// spec: update-output/applying-update-reports-the-reviewed-content
// spec: update-output/candidate-movement-does-not-change-an-applying-invocation-s-comparison
func TestUpdateIncludeDiffAppliesPreparedCandidatesAndReportsDiffs(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, working, repoRoot, _ := previewTestSetup(t)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")

	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v2 applied body", []string{"openstack"})
	writeSupportFile(t, working, "vm-lifecycle", "runbook.txt", "v2 runbook contents applied")
	commitAndPushAll(t, working, "advance v2")
	newCommit := gitHeadRevision(t, working)

	output, err := executeCommand("update", "--include-diff")
	if err != nil {
		t.Fatalf("update --include-diff failed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"repo skill \"vm-lifecycle\"",
		newCommit,
		"@",
		`+  "description": "VM lifecycle v2 applied body"`,
		`-  "description": "VM lifecycle v1"`,
		"+v2 runbook contents applied",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("apply-with-diff output missing %q:\n%s", want, output)
		}
	}
	doc := readLockDocument(t, lockPath)
	if len(doc.Registries) != 1 || !strings.HasSuffix(doc.Registries[0].Commit.String(), newCommit) {
		t.Fatalf("applied lock commit does not match the reported candidate: %+v", doc.Registries)
	}
	installed, err := filepath.EvalSymlinks(filepath.Join(repoRoot, ".factory", "skills", "vm-lifecycle"))
	if err != nil {
		t.Fatalf("resolve installed skill: %v", err)
	}
	if body := readRequiredFile(t, filepath.Join(installed, "SKILL.md")); !strings.Contains(string(body), "VM lifecycle v2 applied body") {
		t.Fatalf("installed body was not applied:\n%s", body)
	}
}

// spec: update-output/ordinary-update-does-not-opt-into-content-disclosure
func TestPlainUpdateOmitsContentDisclosure(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, working, _, _ := previewTestSetup(t)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	writeSkill(t, working, "vm-lifecycle", "DISTINCTIVE-INSTRUCTION-BODY-V2", []string{"openstack"})
	commitAndPushAll(t, working, "advance v2")

	output, err := executeCommand("update")
	if err != nil {
		t.Fatalf("plain update failed: %v\n%s", err, output)
	}
	if strings.Contains(output, "DISTINCTIVE-INSTRUCTION-BODY-V2") {
		t.Fatalf("plain update disclosed changed content:\n%s", output)
	}
	if strings.Contains(output, "\n@@") {
		t.Fatalf("plain update printed content hunks:\n%s", output)
	}
}

func TestPreviewSnapshotsDetectSameSizeContentChanges(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := t.TempDir()
	path := filepath.Join(root, "protected.txt")
	writeFile(t, path, "before")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	before := snapshotConsumerTree(t, root)
	writeFile(t, path, "after!")
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(before, snapshotConsumerTree(t, root)) {
		t.Error("snapshot missed changed bytes with unchanged size, mode, and mtime")
	}
}

func TestPreviewPriorRegistriesPreservesDistinctRefBaselines(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	const url = "https://example.com/registry"
	doc := lockfile.Document{Registries: []lockfile.GitRegistry{
		{URL: url, Ref: "v1"},
		{URL: url, Ref: "stable"},
		{URL: url, Ref: "v2"},
		{URL: "https://example.com/other", Ref: "unrelated"},
	}}
	for _, tt := range []struct {
		name  string
		ref   string
		bumps []pipeline.RefBump
		want  []string
	}{
		{"exact ref wins", "stable", []pipeline.RefBump{{URL: url, OldRef: "v1", NewRef: "stable"}}, []string{"stable"}},
		{"bumped ref follows edit", "v3", []pipeline.RefBump{{URL: url + ".git", OldRef: "v2", NewRef: "v3"}}, []string{"v2"}},
		{"converging refs retain both baselines", "v3", []pipeline.RefBump{{URL: url, OldRef: "v1", NewRef: "v3"}, {URL: url, OldRef: "v2", NewRef: "v3"}}, []string{"v1", "v2"}},
		{"unrelated ref is not a baseline", "v3", nil, nil},
		{"other origin is not a baseline", "v3", []pipeline.RefBump{{URL: "https://example.com/other", OldRef: "unrelated", NewRef: "v3"}}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, registry := range previewPriorRegistries(doc, url, tt.ref, tt.bumps) {
				got = append(got, registry.Ref)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("prior refs = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPreviewSourceChangeIncludesPriorIdentity(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tt := range []struct {
		name  string
		prior resourcediff.Identity
		want  string
	}{
		{"git ref and path", resourcediff.Identity{SourceURL: "https://example.com/registry", Ref: "v1", Path: "old-root"}, `previous "https://example.com/registry" path "old-root" (ref "v1")`},
		{"marketplace instance and coordinate", resourcediff.Identity{SourceURL: "https://market.example", InstanceID: "old-instance", Coordinate: "old-registry"}, `previous "https://market.example" instance "old-instance" registry "old-registry"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			line, err := previewResourceChangeLine(resourcediff.ResourceChange{
				Kind:     resourcediff.ChangeSource,
				Identity: resourcediff.Identity{Scope: "repo", Kind: "skill", Name: "review", SourceURL: tt.prior.SourceURL},
				Prior:    &resourcediff.Side{Identity: tt.prior},
			})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(line, tt.want) {
				t.Errorf("source change missing %q:\n%s", tt.want, line)
			}
		})
	}
}
