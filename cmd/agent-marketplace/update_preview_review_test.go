package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

// writeAllKindsRegistry seeds one Git registry with every reviewable resource
// kind: a skill with a supporting file, a command, a subagent, a hook with an
// adjacent support script, an MCP server, and a collection of two skills.
func writeAllKindsRegistry(t *testing.T, working string) {
	t.Helper()
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v1", []string{"infra"})
	writeSupportFile(t, working, "vm-lifecycle", "runbook.txt", "v1 runbook contents")
	writeSkill(t, working, "log-triage", "Log triage v1", []string{"observability"})
	writeCommand(t, working, "deploy", "Deploy v1", "echo deploy-v1")
	writeSubagent(t, working, "reviewer", "Reviewer v1", []string{"Read"})
	writeFile(t, filepath.Join(working, "hooks", "hooks.json"), `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./deploy-check.sh"}]}]}`)
	writeFile(t, filepath.Join(working, "hooks", "deploy-check.sh"), "#!/bin/sh\necho hook-support-v1\n")
	writeFile(t, filepath.Join(working, ".mcp.json"), `{"mcpServers":{"ci-mcp":{"command":"echo","args":["v1"]}}}`)
	writeCollection(t, working, "onboarding", `
apiVersion: agent-marketplace/v1
kind: Collection
name: onboarding
version: 1.0.0
description: Onboarding collection v1
skills:
  - name: vm-lifecycle
  - name: log-triage
`)
}

// advanceAllKindsRegistry changes every resource definition and the skill's
// preserved extension field so each kind has a real, distinctive change.
func advanceAllKindsRegistry(t *testing.T, working string) {
	t.Helper()
	writeFile(t, filepath.Join(working, "skills", "vm-lifecycle", "SKILL.md"), `---
name: vm-lifecycle
description: VM lifecycle v2 distinctive
metadata:
  tags:
    - infra
x-review-note: distinctive-extension-v2
---

# vm-lifecycle
`)
	writeSupportFile(t, working, "vm-lifecycle", "runbook.txt", "v2 runbook contents")
	writeSkill(t, working, "log-triage", "Log triage v2 distinctive", []string{"observability"})
	writeCommand(t, working, "deploy", "Deploy v2 distinctive", "echo deploy-v2-distinctive")
	writeSubagent(t, working, "reviewer", "Reviewer v2 distinctive", []string{"Read", "Grep"})
	writeFile(t, filepath.Join(working, "hooks", "hooks.json"), `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./run-v2.sh"}]}]}`)
	if err := os.Remove(filepath.Join(working, "hooks", "deploy-check.sh")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(working, "hooks", "run-v2.sh"), "#!/bin/sh\necho hook-support-v2-distinctive\n")
	writeFile(t, filepath.Join(working, ".mcp.json"), `{"mcpServers":{"ci-mcp":{"command":"echo","args":["v2-distinctive-mcp"]}}}`)
	writeCollection(t, working, "onboarding", `
apiVersion: agent-marketplace/v1
kind: Collection
name: onboarding
version: 2.0.0
description: Onboarding collection v2 distinctive
skills:
  - name: vm-lifecycle
`)
}

// spec: update-output/all-supported-resource-definitions-are-reviewable
func TestUpdatePreviewAllResourceKindsReviewable(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	origin, working := createBareRepoWithWorkingClone(t, "all-kinds-review.git")
	writeAllKindsRegistry(t, working)
	commitAndPushAll(t, working, "seed all kinds")

	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources:
  all:
    git: file://`+origin+`
    ref: main
resources:
  all:
    all: true
`)
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}

	advanceAllKindsRegistry(t, working)
	commitAndPushAll(t, working, "advance all kinds")

	output, err := updateDryRun(t, "--include-diff")
	if err != nil {
		t.Fatalf("all-kinds dry-run failed: %v\n%s", err, output)
	}
	for _, want := range []string{
		// Every resource kind is identified with scope, kind, and name.
		`skill "vm-lifecycle"`,
		`skill "log-triage"`,
		`command "deploy"`,
		`subagent "reviewer"`,
		`hook "PreToolUse-command"`,
		`mcpserver "ci-mcp"`,
		`collection "onboarding"`,
		// Bodies, executable declarations, and changed semantic metadata are
		// visible in the normalized definitions.
		`-  "description": "VM lifecycle v1"`,
		`+  "description": "VM lifecycle v2 distinctive"`,
		`+    "command": "echo deploy-v2-distinctive"`,
		`+    "Grep"`,
		`+  "command": "./run-v2.sh"`,
		`+    "v2-distinctive-mcp"`,
		`+  "description": "Onboarding collection v2 distinctive"`,
		// A changed preserved extension field does not silently disappear.
		"x-review-note",
		"distinctive-extension-v2",
		// Supporting-file changes stay visible next to the definition.
		"+v2 runbook contents",
		"+echo hook-support-v2-distinctive",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("all-kinds review output missing %q:\n%s", want, output)
		}
	}
	for _, forbidden := range []string{"Log triage v1", "deploy-v1", "hook-support-v1"} {
		if strings.Contains(output, `+`+forbidden) {
			t.Fatalf("review presents stale v1 content as added: %s", forbidden)
		}
	}
}

// spec: update-output/post-publication-failure-is-not-reported-as-full-application
func TestUpdateIncludeDiffPostPublicationSyncFailureReportsPhaseBoundary(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	origin, working := createBareRepoWithWorkingClone(t, "committed-then-sync-failure.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v1", []string{"openstack"})
	writeFile(t, filepath.Join(working, ".mcp.json"), `{"mcpServers":{"ci-mcp":{"command":"echo","args":["ok"]}}}`)
	commitAndPushAll(t, working, "seed v1")

	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
harnesses: [devin]
sources:
  infra:
    git: file://`+origin+`
    ref: main
resources:
  infra:
    all: true
`)
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}

	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v2 applied body", []string{"openstack"})
	// The candidate MCP entry switches to a transport Devin does not document,
	// so dependency publication succeeds and harness synchronization fails.
	writeUndocumentedTransportMCP(t, working, "ci-mcp")
	commitAndPushAll(t, working, "advance v2")
	newCommit := gitHeadRevision(t, working)

	output, err := executeCommand("update", "--include-diff")
	var committed *pipeline.DependencyCommittedSyncError
	if !errors.As(err, &committed) {
		t.Fatalf("update --include-diff error = %v, want DependencyCommittedSyncError\n%s", err, output)
	}
	for _, want := range []string{
		"Dependency updates were published, but harness synchronization did not complete",
		"mcp.devin.transport-unsupported",
		"websocket",
		`+  "description": "VM lifecycle v2 applied body"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("post-publication report missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "Dry run") {
		t.Fatalf("post-publication failure is reported as a dry run:\n%s", output)
	}
	// Published dependency state advanced even though materialization failed.
	doc := readLockDocument(t, filepath.Join(repo, ".agents", "lock.yaml"))
	if len(doc.Registries) != 1 || !strings.HasSuffix(doc.Registries[0].Commit.String(), newCommit) {
		t.Fatalf("published lock does not carry the prepared candidate: %+v", doc.Registries)
	}
	// The failing MCP entry was skipped, so materialization is incomplete:
	// the report must not label all harness changes as completed.
	if mcpBody := readRequiredFile(t, filepath.Join(repo, ".devin", "mcp_config.json")); strings.Contains(string(mcpBody), "websocket") {
		t.Fatalf("report claimed completed materialization; the failing MCP entry is the incomplete part:\n%s", mcpBody)
	}
}

// failingResultWriter fails every write so the requested diff result cannot be
// written after a successful publication.
type failingResultWriter struct {
	err error
}

func (w *failingResultWriter) Write([]byte) (int, error) { return 0, w.err }

// spec: update-output/result-output-failure-does-not-imply-rollback
func TestUpdateResultOutputFailureDoesNotImplyRollback(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, working, repoRoot, _ := previewTestSetup(t)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v2 applied body", []string{"openstack"})
	commitAndPushAll(t, working, "advance v2")
	newCommit := gitHeadRevision(t, working)

	writeErr := errors.New("result output write failed sentinel")
	// The publication path is the real runUpdate flow; only the result
	// writer fails, after dependency publication has already succeeded.
	err := runUpdate(context.Background(), &failingResultWriter{err: writeErr}, false, false, false, true, nil, nil)
	if !errors.Is(err, writeErr) {
		t.Fatalf("update error = %v, want the result-output failure", err)
	}
	// The publication was real and stays applied: the failure is not a
	// pre-publication refusal and no rollback is claimed or performed.
	doc := readLockDocument(t, filepath.Join(repoRoot, ".agents", "lock.yaml"))
	if len(doc.Registries) != 1 || !strings.HasSuffix(doc.Registries[0].Commit.String(), newCommit) {
		t.Fatalf("result-output failure rolled back or prevented publication: %+v", doc.Registries)
	}
}

// spec: update-output/review-does-not-broaden-symlink-or-special-file-admission
func TestUpdateDiffRefusesSymlinkedSupportFileWithoutReadingTarget(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, working, repoRoot, _ := previewTestSetup(t)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v2 upstream", []string{"openstack"})
	// The candidate support file is a symlink pointing outside the skill
	// directory; the review must not read that foreign target.
	if err := os.Remove(filepath.Join(working, "skills", "vm-lifecycle", "runbook.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(working, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(working, "docs", "foreign-target.txt"), "FOREIGN-SYMLINK-TARGET-V2\n")
	if err := os.Symlink(filepath.Join("..", "..", "docs", "foreign-target.txt"), filepath.Join(working, "skills", "vm-lifecycle", "runbook.txt")); err != nil {
		t.Fatal(err)
	}
	commitAndPushAll(t, working, "advance v2 with symlinked support")
	before := snapshotConsumerTree(t, repoRoot)
	lockBefore := readRequiredFile(t, filepath.Join(repoRoot, ".agents", "lock.yaml"))

	for _, invocation := range []struct {
		label string
		args  []string
	}{
		{label: "dry-run diff", args: []string{"update", "--dry-run", "--include-diff"}},
		{label: "applying diff", args: []string{"update", "--include-diff"}},
	} {
		output, err := executeCommand(invocation.args...)
		if err == nil {
			t.Fatalf("%s accepted a symlinked supporting file:\n%s", invocation.label, output)
		}
		// The existing source-containment contract refuses the candidate
		// before any comparison: the symlink target must not be admitted.
		if !strings.Contains(err.Error(), "escapes snapshot") && !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("%s refusal does not retain the source-containment contract:\n%s\nerror: %v", invocation.label, output, err)
		}
		if strings.Contains(output, "FOREIGN-SYMLINK-TARGET-V2") {
			t.Fatalf("%s read or disclosed the foreign symlink target:\n%s", invocation.label, output)
		}
	}
	assertTreesIdentical(t, "consumer", before, snapshotConsumerTree(t, repoRoot))
	if string(readRequiredFile(t, filepath.Join(repoRoot, ".agents", "lock.yaml"))) != string(lockBefore) {
		t.Fatalf("refused diff invocations changed the acquisition lock")
	}
}

// spec: update-output/candidate-movement-does-not-change-an-applying-invocation-s-comparison
func TestUpdateIncludeDiffRejectsConcurrentConfigEdit(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, working, repoRoot, configPath := previewTestSetup(t)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v2 applied body", []string{"openstack"})
	commitAndPushAll(t, working, "advance v2")

	// The prepared comparison is consumed through the existing dependency
	// transaction: a concurrent config edit between preparation and commit
	// retains the existing generation-conflict refusal instead of publishing
	// the compared candidates over changed input.
	originalCommit := commitDependencyTransaction
	commitDependencyTransaction = func(ctx context.Context, stateDir string, desired []dependencytxn.DesiredFile, opts ...dependencytxn.Option) (dependencytxn.Result, error) {
		writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nsources: {}\n")
		return dependencytxn.Commit(ctx, stateDir, desired, opts...)
	}
	t.Cleanup(func() { commitDependencyTransaction = originalCommit })

	output, err := executeCommand("update", "--include-diff")
	if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("update --include-diff error = %v\n%s; want generation conflict", err, output)
	}
	// Nothing was published over the concurrently edited input.
	if !strings.Contains(string(readRequiredFile(t, configPath)), "sources: {}") {
		t.Fatalf("generation-conflicted update overwrote the concurrent config edit")
	}
	installed := filepath.Join(repoRoot, ".factory", "skills", "vm-lifecycle", "SKILL.md")
	if body := readRequiredFile(t, installed); strings.Contains(string(body), "VM lifecycle v2 applied body") {
		t.Fatalf("generation-conflicted update materialized the prepared candidate:\n%s", body)
	}
}

// spec: update-output/candidate-movement-does-not-change-an-applying-invocation-s-comparison
func TestUpdateIncludeDiffAppliesComparedCandidateWhenUpstreamAdvances(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, working, repoRoot, _ := previewTestSetup(t)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	writeSkill(t, working, "vm-lifecycle", "Compared candidate v2", []string{"openstack"})
	commitAndPushAll(t, working, "candidate v2")
	comparedRevision := gitHeadRevision(t, working)

	// Publication happens after comparison. Advance the real upstream at
	// that boundary, then let the real transaction publish its prepared set.
	originalCommit := commitDependencyTransaction
	var upstreamRevision string
	commitCalls := 0
	commitDependencyTransaction = func(ctx context.Context, stateDir string, desired []dependencytxn.DesiredFile, opts ...dependencytxn.Option) (dependencytxn.Result, error) {
		commitCalls++
		if commitCalls == 1 {
			writeSkill(t, working, "vm-lifecycle", "Later upstream v3", []string{"openstack"})
			commitAndPushAll(t, working, "advance upstream during update")
			upstreamRevision = gitHeadRevision(t, working)
		}
		return dependencytxn.Commit(ctx, stateDir, desired, opts...)
	}
	t.Cleanup(func() { commitDependencyTransaction = originalCommit })

	output, err := executeCommand("update", "--include-diff")
	commitDependencyTransaction = originalCommit
	if err != nil {
		t.Fatalf("update with advancing upstream: %v\n%s", err, output)
	}
	if commitCalls != 1 || upstreamRevision == "" || upstreamRevision == comparedRevision {
		t.Fatalf("upstream did not advance once during publication: calls=%d compared=%q upstream=%q", commitCalls, comparedRevision, upstreamRevision)
	}
	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	if got := readLockDocument(t, lockPath).Registries[0].Commit.String(); got != "sha1:"+comparedRevision {
		t.Errorf("published commit = %q, want compared commit %q", got, comparedRevision)
	}
	installed := filepath.Join(repoRoot, ".factory", "skills", "vm-lifecycle", "SKILL.md")
	if body := string(readRequiredFile(t, installed)); !strings.Contains(body, "Compared candidate v2") || strings.Contains(body, "Later upstream v3") {
		t.Errorf("installed content does not match compared candidate:\n%s", body)
	}
	if !strings.Contains(output, "Compared candidate v2") || !strings.Contains(output, comparedRevision) || strings.Contains(output, "Later upstream v3") || strings.Contains(output, upstreamRevision) {
		t.Errorf("reported content or revision does not match compared candidate:\n%s", output)
	}

	// A separate invocation must see the new upstream, rather than treating
	// the earlier comparison as a reservation for subsequent updates.
	if nextOutput, nextErr := executeCommand("update"); nextErr != nil {
		t.Fatalf("subsequent update: %v\n%s", nextErr, nextOutput)
	}
	if got := readLockDocument(t, lockPath).Registries[0].Commit.String(); got != "sha1:"+upstreamRevision {
		t.Errorf("subsequent commit = %q, want advanced upstream %q", got, upstreamRevision)
	}
	if body := string(readRequiredFile(t, installed)); !strings.Contains(body, "Later upstream v3") {
		t.Errorf("subsequent invocation did not install the advanced upstream:\n%s", body)
	}
}

// spec: update-output/unavailable-recorded-baseline-refuses-before-new-updates
func TestUpdateDiffUnavailableRecordedBaselineRefusesBeforeNewUpdates(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	origin, working, repoRoot, _ := previewTestSetup(t)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v2 upstream", []string{"openstack"})
	commitAndPushAll(t, working, "advance v2")

	// Replace the origin with an unrelated root commit so the previously
	// locked revision is physically absent from the acquisition source, while
	// the candidate content stays fetchable under the same configured URL.
	runGit(t, working, "checkout", "--orphan", "rewrite-main")
	runGit(t, working, "add", ".")
	runGit(t, working, "commit", "-m", "rewritten root history")
	if err := os.RemoveAll(origin); err != nil {
		t.Fatal(err)
	}
	runGit(t, "", "init", "--bare", "--initial-branch=main", origin)
	disableBareRemoteMaintenance(t, origin)
	runGit(t, working, "push", "--force", "origin", "rewrite-main:main")

	// The disposable acquisition cache lives under HOME/.cache regardless of
	// XDG_CACHE_HOME (cache.CacheDir resolves the home directory).
	cacheHome := os.Getenv("HOME")
	if cacheHome == "" {
		t.Fatal("sandboxed child has no HOME")
	}
	if err := os.RemoveAll(filepath.Join(cacheHome, ".cache", "agent-marketplace")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}

	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	before := snapshotConsumerTree(t, repoRoot)
	lockBefore := readRequiredFile(t, lockPath)
	configBefore := readRequiredFile(t, configPath)
	if _, statErr := os.Stat(filepath.Join(cacheHome, ".cache", "agent-marketplace")); statErr == nil {
		t.Fatal("cache removal did not remove the acquisition cache")
	}
	oldCommit := readLockDocument(t, lockPath).Registries[0].Commit.String()
	probe := exec.Command("git", "-C", origin, "cat-file", "-t", strings.TrimPrefix(oldCommit, "sha1:"))
	if probeOut, probeErr := probe.CombinedOutput(); probeErr == nil {
		t.Fatalf("origin still contains the previously locked commit %s: %s", oldCommit, probeOut)
	}

	for _, invocation := range []struct {
		label string
		args  []string
	}{
		{label: "dry-run diff", args: []string{"update", "--dry-run", "--include-diff"}},
		{label: "applying diff", args: []string{"update", "--include-diff"}},
	} {
		output, err := executeCommand(invocation.args...)
		if err == nil {
			t.Fatalf("%s succeeded despite an unavailable recorded baseline:\n%s", invocation.label, output)
		}
		if !strings.Contains(output, "load previously locked revision") || !strings.Contains(output, lockPath) {
			t.Fatalf("%s does not identify the failed baseline comparison:\n%s\nerror: %v", invocation.label, output, err)
		}
		if strings.Contains(output, "Proposed selected-resource changes") || strings.Contains(output, "Dry run: no updates were applied") {
			t.Fatalf("%s displayed a complete-success report despite the refusal:\n%s", invocation.label, output)
		}
		assertTreesIdentical(t, "consumer ("+invocation.label+")", before, snapshotConsumerTree(t, repoRoot))
		if string(readRequiredFile(t, lockPath)) != string(lockBefore) {
			t.Fatalf("%s published a new acquisition lock", invocation.label)
		}
		if string(readRequiredFile(t, configPath)) != string(configBefore) {
			t.Fatalf("%s published new configuration state", invocation.label)
		}
	}
}

// spec: update-output/changed-source-and-complementary-occurrences-remain-visible
func TestUpdateIncludeDiffReportsChangedCollisionWinner(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	originA, workA := createBareRepoWithWorkingClone(t, "example-preview-collision-a.git")
	writeSkill(t, workA, "vm-lifecycle", "VM lifecycle from registry A", []string{"openstack"})
	// An unselected sibling keeps the registry's harness layout recognizable
	// after the colliding skill disappears upstream.
	writeSkill(t, workA, "registry-a-only", "Registry A only", []string{"openstack"})
	commitAndPushAll(t, workA, "seed A")
	originB, workB := createBareRepoWithWorkingClone(t, "example-preview-collision-b.git")
	writeSkill(t, workB, "vm-lifecycle", "VM lifecycle from registry B", []string{"openstack"})
	commitAndPushAll(t, workB, "seed B")

	repo := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	writeConfig(t, configPath, `
apiVersion: agent-marketplace/v4
sources:
  first:
    git: file://`+originA+`
    ref: main
  second:
    git: file://`+originB+`
    ref: main
resources:
  first:
    all: true
  second:
    all: true
`)
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	skillPath := filepath.Join(repo, ".factory", "skills", "vm-lifecycle", "SKILL.md")
	if string(readRequiredFile(t, skillPath)) == "" || !strings.Contains(string(readRequiredFile(t, skillPath)), "registry A") {
		t.Fatalf("expected the deterministically winning first source to install its copy: %s", string(readRequiredFile(t, skillPath)))
	}
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	lockBefore := readRequiredFile(t, lockPath)
	configBefore := readRequiredFile(t, configPath)

	// The winning registry drops the colliding skill upstream: the update's
	// candidate resolution must report the collision winner moving to the
	// second source with both old and new sources named, not a plain
	// modification.
	if err := os.RemoveAll(filepath.Join(workA, "skills", "vm-lifecycle")); err != nil {
		t.Fatalf("remove registry A skill: %v", err)
	}
	commitAndPushAll(t, workA, "drop the colliding skill from registry A")

	output, err := updateDryRun(t, "--include-diff")
	if err != nil {
		t.Fatalf("dry-run diff with a changed collision winner failed: %v\n%s", err, output)
	}
	for _, want := range []string{
		": source changed (previous",
		"example-preview-collision-a.git",
		"example-preview-collision-b.git",
		"VM lifecycle from registry B",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("collision-winner diff missing %q:\n%s", want, output)
		}
	}
	if got := readRequiredFile(t, skillPath); !strings.Contains(string(got), "registry A") {
		t.Fatalf("dry run replaced the installed collision winner:\n%s", string(got))
	}
	if string(readRequiredFile(t, lockPath)) != string(lockBefore) {
		t.Fatal("dry run published a new acquisition lock")
	}
	if string(readRequiredFile(t, configPath)) != string(configBefore) {
		t.Fatal("dry run published new configuration state")
	}
}

// spec: update-preview/newly-missing-selection-blocks-the-preview
func TestUpdateRefusesUnresolvableIncludeInNeverLockedSource(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	origin, working := createBareRepoWithWorkingClone(t, "example-preview-neverlocked.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v1", []string{"openstack"})
	commitAndPushAll(t, working, "seed v1")

	repo := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	writeConfig(t, configPath, `
apiVersion: agent-marketplace/v4
sources:
  infra:
    git: file://`+origin+`
    ref: main
resources:
  infra:
    include: [skill/vm-lifecycle, skill/absent-skill]
`)
	withWorkingDir(t, repo)

	// No lock exists and the registry was never synced: the newly-missing
	// selection guard must keep its conservative refusal for the
	// never-locked source instead of letting the preview's initial-
	// acquisition fallback make the miss observable and silently accepted.
	for _, invocation := range []struct {
		label string
		args  []string
	}{
		{label: "applying", args: []string{"update"}},
		{label: "dry-run", args: []string{"update", "--dry-run", "--include-diff"}},
	} {
		output, err := executeCommand(invocation.args...)
		if err == nil {
			t.Fatalf("%s update accepted an unresolvable include in a never-locked source:\n%s", invocation.label, output)
		}
		for _, want := range []string{`skill "absent-skill" was selected but not found`, "update left the lock and harness outputs unchanged"} {
			if !strings.Contains(output, want) {
				t.Fatalf("%s update refusal missing %q:\n%s", invocation.label, want, output)
			}
		}
		if _, statErr := os.Stat(filepath.Join(repo, ".agents", "lock.yaml")); !os.IsNotExist(statErr) {
			t.Fatalf("%s update created a lock despite refusing: %v", invocation.label, statErr)
		}
	}
	if _, err := os.Stat(filepath.Join(repo, ".factory")); !os.IsNotExist(err) {
		t.Fatalf("refused update materialized outputs: %v", err)
	}
}
