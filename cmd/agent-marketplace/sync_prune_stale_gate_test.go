package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// compoundMaterializeErrorMCP is the UNRELATED materialization failure the
// compound tests below need: an MCP server whose explicit remote transport is
// outside Devin's documented vocabulary, so the entry is skipped and the run
// exits non-zero (mcp.devin.transport-unsupported, SeverityError in
// pkg/harness-compilation/diag/codes.go).
//
// It is deliberately NOT the scoped allowed-tools skill fixture. That one
// relies on skill.devin.tool-unknown staying an error, and bead
// agent-marketplace-pu6b downgrades exactly that code to a warning that omits
// the key instead of suppressing the artifact. Once pu6b merges, that fixture
// stops producing any error and these tests stop testing the compound
// scenario. The entry-skipping Devin aggregate codes are the durable choice:
// pu6b does not touch them, and their whole contract (pinned end to end by
// TestSyncExitCodeForSkippedDevinAggregateEntries) is "entry skipped, sync
// exits non-zero".
const compoundMaterializeErrorMCP = `{
  "mcpServers": {
    "good": {"command": "npx"},
    "bad": {"url": "https://example.invalid/mcp", "transport": "websocket"}
  }
}`

// stalePruneCompoundRepo builds the beta tester's exact compound failure: a
// registry that loads cleanly, one unrelated resource that fails to
// materialize for Devin, and `stale` selections that are genuinely absent
// upstream. It returns the repo root and its config path.
func stalePruneCompoundRepo(t *testing.T, stale ...string) (string, string) {
	t.Helper()

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "compound-failure.git")
	writeSkill(t, working, "keeper", "Survives the materialize failure", nil)
	writeFile(t, filepath.Join(working, ".mcp.json"), compoundMaterializeErrorMCP)
	commitAndPushAll(t, working, "Add compound fixture")

	selections := "      - keeper\n"
	for _, name := range stale {
		selections += "      - " + name + "\n"
	}

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
apiVersion: agent-marketplace/v2
harnesses:
  - devin
registries:
  - url: `+origin+`
    ref: main
    mcp:
      - good
      - bad
    skills:
`+selections)

	withWorkingDir(t, repoRoot)
	return repoRoot, configPath
}

// TestSyncPruneStalePrunesDespiteUnrelatedMaterializeError is the regression
// test for bead agent-marketplace-tbjf. Unrelated Devin materialize errors made
// len(errs) != 0, so --prune-stale silently did nothing and the genuinely stale
// selections stayed in the config forever. A materialize failure for an
// unrelated resource is not evidence that the catalog lied, so pruning must
// proceed — while the run still exits non-zero for that failure.
func scenarioSyncPruneStalePrunesDespiteUnrelatedMaterializeError(t *testing.T) {
	repoRoot, configPath := stalePruneCompoundRepo(t, "beads", "mayor", "openspec-to-beads")

	output, err := executeCommand("sync", "--prune-stale")
	if err == nil {
		t.Fatalf("sync must still exit non-zero for the unrelated materialize failure; output:\n%s", output)
	}
	if !strings.Contains(err.Error(), "mcp.devin.transport-unsupported") {
		t.Errorf("sync error = %v, want it to still name the unrelated materialize failure", err)
	}

	for _, name := range []string{"beads", "mayor", "openspec-to-beads"} {
		if !strings.Contains(output, `pruned stale skill "`+name+`"`) {
			t.Errorf("stale selection %q was not pruned; output:\n%s", name, output)
		}
	}
	if !strings.Contains(output, "Pruned 3 stale selection(s); re-syncing...") {
		t.Errorf("expected a prune summary before the re-sync; output:\n%s", output)
	}

	configData, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("read config: %v", readErr)
	}
	for _, name := range []string{"beads", "mayor", "openspec-to-beads"} {
		for _, line := range strings.Split(string(configData), "\n") {
			if strings.TrimSpace(line) == "- "+name {
				t.Errorf("stale selection %q survived in the config:\n%s", name, configData)
			}
		}
	}

	// The healthy sibling still materialized: pruning is not a rollback.
	if _, statErr := os.Lstat(filepath.Join(repoRoot, ".devin", "skills", "keeper")); statErr != nil {
		t.Errorf("valid skill was lost: %v", statErr)
	}
}

// TestSyncPruneStaleReSyncTerminatesWhenErrorPersists pins the recursion-safety
// risk that comes with pruning while errors are outstanding: runSync re-enters
// itself after a successful prune, and the materialize failure is still there on
// the re-run. The recursive call passes pruneStale=false, so exactly one prune
// round can ever happen. Asserted by counting rounds, and bounded by a
// deadline so a regression fails instead of hanging the suite.
func scenarioSyncPruneStaleReSyncTerminatesWhenErrorPersists(t *testing.T) {
	_, configPath := stalePruneCompoundRepo(t, "beads", "mayor")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	type run struct {
		output string
		err    error
	}
	done := make(chan run, 1)
	go func() {
		output, err := executeCommandContext(ctx, "sync", "--prune-stale")
		done <- run{output: output, err: err}
	}()

	var result run
	select {
	case result = <-done:
	case <-ctx.Done():
		t.Fatalf("sync --prune-stale did not terminate: %v", ctx.Err())
	}

	if result.err == nil {
		t.Fatalf("expected the persistent materialize failure to fail the run; output:\n%s", result.output)
	}
	if got := strings.Count(result.output, "re-syncing..."); got != 1 {
		t.Errorf("prune re-sync happened %d times, want exactly 1; output:\n%s", got, result.output)
	}
	if got := strings.Count(result.output, `pruned stale skill "beads"`); got != 1 {
		t.Errorf("stale skill %q pruned %d times, want exactly 1; output:\n%s", "beads", got, result.output)
	}
	if strings.Contains(result.output, "prune-stale skipped") {
		t.Errorf("the re-run must not report a skipped prune; output:\n%s", result.output)
	}

	// A follow-up run has nothing left to prune and still fails for the
	// materialize error: the loop cannot restart from a persisted state either.
	output2, err2 := executeCommand("sync", "--prune-stale")
	if err2 == nil {
		t.Errorf("second run should still fail for the materialize error; output:\n%s", output2)
	}
	if strings.Contains(output2, "pruned stale") {
		t.Errorf("second run pruned again; output:\n%s", output2)
	}
	configData, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("read config: %v", readErr)
	}
	if !strings.Contains(string(configData), "keeper") {
		t.Errorf("pruning removed more than the stale selections:\n%s", configData)
	}
}

// TestSyncPruneStaleStrictModePrunesTheSelectionsItPromoted pins a deliberate
// consequence of gating on catalog trust: --strict turns the very stale
// selections that --prune-stale exists to remove into hard errors, and those
// promoted errors are NOT evidence of an unreadable catalog. Asking for both
// flags therefore prunes and then re-syncs clean, instead of the old behavior
// where --strict's own promotion suppressed the fix it was complaining about.
func scenarioSyncPruneStaleStrictModePrunesTheSelectionsItPromoted(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "strict-prune.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", nil)
	commitAndPushAll(t, working, "Add skill")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - vm-lifecycle
      - deleted-skill
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync", "--strict", "--prune-stale")
	if !strings.Contains(output, `pruned stale skill "deleted-skill"`) {
		t.Fatalf("--strict must not suppress the prune it asked for; output:\n%s", output)
	}
	if err != nil {
		t.Errorf("re-sync after the prune has nothing left to promote: %v\noutput:\n%s", err, output)
	}
	configData, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("read config: %v", readErr)
	}
	for _, line := range strings.Split(string(configData), "\n") {
		if strings.TrimSpace(line) == "- deleted-skill" {
			t.Errorf("stale selection survived:\n%s", configData)
		}
	}
}

// TestSyncPruneStaleStrictSurvivesTheReSync pins the other half of the --strict
// change. Now that --strict no longer blocks the prune, the run's exit code is
// the RE-SYNC's exit code, so the re-sync has to stay strict: a stale selection
// the prune cannot remove — here a collection member, which is deliberately out
// of the prune set — must still fail the run. Dropping strict on the recursive
// call would exit 0 with the stale selection still in the config, silently
// discarding the promotion the user asked for.
func scenarioSyncPruneStaleStrictSurvivesTheReSync(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "strict-resync.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", nil)
	writeCollection(t, working, "infra-bundle", `
apiVersion: agent-marketplace/v1
kind: Collection
name: infra-bundle
version: 1.0.0
skills:
  - name: vm-lifecycle
  - name: collection-only-stale
`)
	commitAndPushAll(t, working, "Add skill and collection")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - vm-lifecycle
      - deleted-skill
collections:
  include:
    - infra-bundle
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync", "--strict", "--prune-stale")
	if !strings.Contains(output, `pruned stale skill "deleted-skill"`) {
		t.Fatalf("the directly selected stale skill must still be pruned; output:\n%s", output)
	}
	if strings.Contains(output, `pruned stale skill "collection-only-stale"`) {
		t.Fatalf("a collection-sourced selection must never be pruned; output:\n%s", output)
	}
	if err == nil {
		t.Fatalf("--strict must still fail the run for the stale selection the prune could not remove; output:\n%s", output)
	}
	configData, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("read config: %v", readErr)
	}
	if !strings.Contains(string(configData), "infra-bundle") {
		t.Errorf("the collection reference was removed from the config:\n%s", configData)
	}
}

// TestSyncPruneStaleSkipExplainsUntrustedCatalog covers the other half of the
// bead: when the catalog itself could not be trusted, pruning is still refused
// entirely — a resource absent from a registry that failed to load is not
// proof that it is gone. A silently skipped --prune-stale is indistinguishable
// from one that found nothing, so the skip is now stated with its reason.
func scenarioSyncPruneStaleSkipExplainsUntrustedCatalog(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "healthy-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", nil)
	commitAndPushAll(t, working, "Add skill")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - vm-lifecycle
      - deleted-skill
  - url: `+filepath.Join(t.TempDir(), "unreachable.git")+`
    ref: main
    skills:
      - vm-lifecycle
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync", "--prune-stale")
	if err == nil {
		t.Fatalf("expected the unreachable registry to fail the run; output:\n%s", output)
	}
	if strings.Contains(output, "pruned stale") {
		t.Fatalf("a catalog-load failure must block pruning entirely; output:\n%s", output)
	}
	if !strings.Contains(output, "prune-stale skipped") {
		t.Fatalf("a skipped prune must say so; output:\n%s", output)
	}
	if !strings.Contains(output, `skill "deleted-skill"`) {
		t.Errorf("the skip message must name what was kept; output:\n%s", output)
	}
	if !strings.Contains(output, "could not be trusted") {
		t.Errorf("the skip message must say why it was skipped; output:\n%s", output)
	}

	configData, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("read config: %v", readErr)
	}
	if !strings.Contains(string(configData), "deleted-skill") {
		t.Errorf("selection was pruned despite an untrusted catalog:\n%s", configData)
	}
}
