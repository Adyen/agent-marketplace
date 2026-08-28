package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// assertValidConfigYAML fails the test if the post-prune config is not
// parseable YAML. Every prune path edits the user's committed file, so an
// unparseable result is the worst outcome short of deleting a live selection.
func assertValidConfigYAML(t *testing.T, data []byte) {
	t.Helper()
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		t.Fatalf("ATTACK SUCCEEDED (config corruption): config is no longer valid YAML after the prune: %v\nconfig:\n%s", err, data)
	}
}

// ATTACK A1: a registry whose `path:` subdirectory does not exist in the
// cloned repo. The clone succeeds, so nothing is reported as a catalog error,
// but the catalog is EMPTY — every selection from that registry looks
// "selected but not found". If CatalogTrusted() is true here, --prune-stale
// deletes selections for resources that are perfectly fine upstream.
func TestAttackPruneMissingRegistrySubdirectory(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "typo-path.git")
	// The resources really do exist, under "bundle/".
	writeSkill(t, filepath.Join(working, "bundle"), "vm-lifecycle", "VM lifecycle", nil)
	writeSkill(t, filepath.Join(working, "bundle"), "deploy", "Deploy", nil)
	commitAndPushAll(t, working, "Add skills under bundle/")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	// The user typo'd the path: "bundel" instead of "bundle".
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: main
    path: bundel
    skills:
      - vm-lifecycle
      - deploy
`)
	withWorkingDir(t, repoRoot)

	output, _ := executeCommand("sync", "--prune-stale")
	t.Logf("output:\n%s", output)

	configData, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("read config: %v", readErr)
	}
	for _, name := range []string{"vm-lifecycle", "deploy"} {
		if !strings.Contains(string(configData), name) {
			t.Errorf("ATTACK SUCCEEDED: selection %q was deleted from the config because the registry path did not exist; the resource is fine upstream.\nconfig:\n%s\noutput:\n%s", name, configData, output)
		}
	}
}

// ATTACK A2: a registry that clones fine but whose bundle directory is EMPTY
// (all resources deleted in one bad upstream commit, or the wrong ref). Zero
// entries, zero catalog errors -> catalog "trusted" -> every selection pruned.
func TestAttackPruneEmptyRegistry(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, _ := createBareRepoWithWorkingClone(t, "empty-registry.git")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - vm-lifecycle
      - deploy
`)
	withWorkingDir(t, repoRoot)

	output, _ := executeCommand("sync", "--prune-stale")
	t.Logf("output:\n%s", output)

	configData, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("read config: %v", readErr)
	}
	for _, name := range []string{"vm-lifecycle", "deploy"} {
		if !strings.Contains(string(configData), name) {
			t.Errorf("ATTACK SUCCEEDED: selection %q deleted because the registry returned ZERO entries.\nconfig:\n%s\noutput:\n%s", name, configData, output)
		}
	}
}

// ATTACK A6: a malformed resource. The skill exists upstream but its SKILL.md
// is broken, so it lands in CatalogLoad.Malformed rather than Entries. It is
// "selected but not found" while the catalog reports no error at all.
// Deleting the selection is wrong: the fix is to repair the resource.
func TestAttackPruneMalformedResource(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "malformed-registry.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", nil)
	// A skill directory whose SKILL.md has no frontmatter at all.
	writeFile(t, filepath.Join(working, "skills", "deploy", "SKILL.md"), "just prose, no frontmatter\n")
	commitAndPushAll(t, working, "Add one good and one malformed skill")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - vm-lifecycle
      - deploy
`)
	withWorkingDir(t, repoRoot)

	output, _ := executeCommand("sync", "--prune-stale")
	t.Logf("output:\n%s", output)

	configData, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("read config: %v", readErr)
	}
	if !strings.Contains(string(configData), "deploy") {
		t.Errorf("ATTACK SUCCEEDED: a MALFORMED (but present) resource had its selection deleted.\nconfig:\n%s\noutput:\n%s", configData, output)
	}
}

// ATTACK A4: several registries, one unreachable, and the stale selection
// belongs to a HEALTHY registry. This is the case the trust gate is supposed
// to cover; assert the gate holds and the skip message is truthful.
func TestAttackSkipMessageCountsAgreeWithPrintedErrors(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "healthy.git")
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
  - url: `+filepath.Join(t.TempDir(), "gone-a.git")+`
    ref: main
    skills:
      - vm-lifecycle
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync", "--prune-stale")
	t.Logf("output:\n%s\nerr: %v", output, err)

	if strings.Contains(output, "pruned stale") {
		t.Errorf("pruned despite an untrusted catalog; output:\n%s", output)
	}
	// The skip message claims N catalog/resolve errors. That count must match
	// the number of error lines the run actually prints, or the user is told
	// about errors that were never named.
	if err == nil {
		t.Fatalf("expected non-zero exit")
	}
	nErrLines := len(strings.Split(err.Error(), "\n"))
	if nErrLines != 1 {
		t.Errorf("expected 1 error line, got %d; err:\n%s", nErrLines, err)
	}
	want := "(" + strconv.Itoa(nErrLines) + " catalog/resolve error(s))"
	if !strings.Contains(output, want) {
		t.Errorf("skip message does not report %s; the run printed %d error line(s); output:\n%s\nerr:\n%s", want, nErrLines, output, err)
	}
}

// TestSyncPruneStaleNamesTheErrorsItCounted pins reporting truthfulness on the
// re-sync path. A successful prune ends in `return runSync(...)`, so the CLI
// framework only ever renders the SECOND pass's error — while the first pass
// has already printed "Errors N" at the top of the output. Every one of those
// N must be named before the re-sync is announced, or the run reports a count
// of errors it never states.
//
// Attack found during the agent-marketplace-tbjf review; fixed by
// printSupersededErrors.
func TestSyncPruneStaleNamesTheErrorsItCounted(t *testing.T) {
	stalePruneCompoundRepo(t, "beads")

	output, err := executeCommand("sync", "--prune-stale")
	t.Logf("output:\n%s\nerr: %v", output, err)

	first := strings.SplitN(output, "\n", 2)[0]
	if !strings.HasPrefix(first, "✓ Installed ") {
		t.Fatalf("unexpected first line %q", first)
	}
	if strings.HasSuffix(first, "Errors 0") {
		t.Fatalf("fixture must produce a first-pass error; got %q", first)
	}
	idx := strings.Index(output, "Pruned ")
	if idx < 0 {
		t.Fatalf("expected a prune; output:\n%s", output)
	}
	pass1 := output[:idx]
	named := strings.Count(pass1, "error (superseded by re-sync):")
	if named == 0 {
		t.Errorf("misleading output: pass 1 reported %q but never named any error before recursing.\npass1:\n%s", first, pass1)
	}
	if !strings.Contains(pass1, "mcp.devin.transport-unsupported") {
		t.Errorf("the superseded error must be named in full, not just counted.\npass1:\n%s", pass1)
	}
	// The count printed in the summary and the number of named lines agree.
	if want := "Errors " + strconv.Itoa(named); !strings.HasSuffix(first, want) {
		t.Errorf("summary %q disagrees with %d named error line(s).\npass1:\n%s", first, named, pass1)
	}
}

// TestSyncPruneStaleStrictCleanReSyncNamesThePromotedErrors is the --strict
// half of the same defect, and the one that produced a ZERO exit code: under
// --strict the stale selections ARE the errors, so pass 1 printed "Errors 1",
// pruned, re-synced clean and exited 0 having named nothing at all.
func TestSyncPruneStaleStrictCleanReSyncNamesThePromotedErrors(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "strict-unnamed.git")
	writeSkill(t, working, "keeper", "Keeper", nil)
	commitAndPushAll(t, working, "Add skill")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - keeper
      - gone
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync", "--strict", "--prune-stale")
	t.Logf("output:\n%s\nerr: %v", output, err)
	if err != nil {
		t.Fatalf("the re-sync has nothing left to promote, so the run must exit 0: %v", err)
	}
	first := strings.SplitN(output, "\n", 2)[0]
	if strings.HasSuffix(first, "Errors 0") {
		return // nothing was counted, so nothing needs naming
	}
	if !strings.Contains(output, "error (superseded by re-sync):") {
		t.Errorf("misleading output: the run printed %q, exited 0, and never named a single error.\noutput:\n%s", first, output)
	}
	if !strings.Contains(output, `skill "gone" was selected but not found`) {
		t.Errorf("the promoted stale-selection error must be named.\noutput:\n%s", output)
	}
}

// ATTACK 6a: pruning the LAST remaining selection of a list. Does the YAML
// stay valid, and does the surrounding structure survive?
func TestAttackPruneLastSelectionKeepsValidYAML(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "last-entry.git")
	writeSkill(t, working, "keeper", "Keeper", nil)
	commitAndPushAll(t, working, "Add skill")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
# top-of-file comment that must survive
registries:
  # a comment about the registry
  - url: `+origin+`
    ref: main
    # a comment inside the entry
    skills:
      # a comment about the selection
      - only-stale   # trailing comment
    # a comment about required
    required: false
`)
	withWorkingDir(t, repoRoot)

	output, _ := executeCommand("sync", "--prune-stale")
	t.Logf("output:\n%s", output)

	configData, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("read config: %v", readErr)
	}
	t.Logf("config after prune:\n%s", configData)
	assertValidConfigYAML(t, configData)

	// Comments attached to the node that was removed (the `skills:` key and
	// the `- only-stale` item) legitimately go with it. Everything ELSE must
	// be byte-preserved, including the comment on the key that follows the
	// removed one — the classic yaml.Node re-encode casualty.
	for _, want := range []string{
		"top-of-file comment that must survive",
		"a comment about the registry",
		"a comment about required",
		"required: false",
	} {
		if !strings.Contains(string(configData), want) {
			t.Errorf("ATTACK SUCCEEDED (config corruption): unrelated content %q was destroyed by the prune.\nconfig:\n%s", want, configData)
		}
	}
}

// ATTACK 6b: the same selection listed TWICE in the same list.
func TestAttackPruneDuplicateSelectionEntries(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "dupe-entry.git")
	writeSkill(t, working, "keeper", "Keeper", nil)
	commitAndPushAll(t, working, "Add skill")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - keeper
      - gone
      - gone
`)
	withWorkingDir(t, repoRoot)

	output, _ := executeCommand("sync", "--prune-stale")
	t.Logf("output:\n%s", output)

	configData, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("read config: %v", readErr)
	}
	t.Logf("config after prune:\n%s", configData)
	assertValidConfigYAML(t, configData)
	for _, line := range strings.Split(string(configData), "\n") {
		if strings.TrimSpace(line) == "- gone" {
			t.Errorf("ATTACK SUCCEEDED: a duplicated stale selection survived the prune (only one copy removed).\nconfig:\n%s\noutput:\n%s", configData, output)
		}
	}
	if !strings.Contains(string(configData), "keeper") {
		t.Errorf("prune removed the healthy selection:\n%s", configData)
	}
}

// ATTACK 6c: YAML anchors / aliases in the selection list.
func TestAttackPruneWithYAMLAnchors(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "anchors.git")
	writeSkill(t, working, "keeper", "Keeper", nil)
	commitAndPushAll(t, working, "Add skill")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
apiVersion: agent-marketplace/v2
harnesses:
  - factory
common: &common
  ref: main
registries:
  - url: `+origin+`
    <<: *common
    skills:
      - keeper
      - gone
`)
	withWorkingDir(t, repoRoot)

	output, _ := executeCommand("sync", "--prune-stale")
	t.Logf("output:\n%s", output)

	configData, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("read config: %v", readErr)
	}
	t.Logf("config after prune:\n%s", configData)
	assertValidConfigYAML(t, configData)
	if strings.Contains(output, "pruned stale") && !strings.Contains(string(configData), "&common") {
		t.Errorf("ATTACK SUCCEEDED (config corruption): the anchor definition was destroyed.\nconfig:\n%s", configData)
	}
}

// ATTACK 6d: CRLF line endings.
func TestAttackPruneCRLFConfig(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "crlf.git")
	writeSkill(t, working, "keeper", "Keeper", nil)
	commitAndPushAll(t, working, "Add skill")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	body := `apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: ` + origin + `
    ref: main
    skills:
      - keeper
      - gone
`
	writeFile(t, configPath, strings.ReplaceAll(body, "\n", "\r\n"))
	withWorkingDir(t, repoRoot)

	output, _ := executeCommand("sync", "--prune-stale")
	t.Logf("output:\n%s", output)

	configData, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("read config: %v", readErr)
	}
	t.Logf("config after prune: %q", string(configData))
	assertValidConfigYAML(t, configData)
	if !strings.Contains(string(configData), "keeper") {
		t.Errorf("healthy selection lost on a CRLF config:\n%s", configData)
	}
}

// ATTACK 7: idempotency. Two consecutive prune runs must converge.
func TestAttackPruneIdempotent(t *testing.T) {
	_, configPath := stalePruneCompoundRepo(t, "beads", "mayor")

	out1, _ := executeCommand("sync", "--prune-stale")
	after1, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	out2, _ := executeCommand("sync", "--prune-stale")
	after2, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(after1) != string(after2) {
		t.Errorf("ATTACK SUCCEEDED: prune is not idempotent.\nafter run 1:\n%s\nafter run 2:\n%s\nout1:\n%s\nout2:\n%s", after1, after2, out1, out2)
	}
	if strings.Contains(out2, "pruned stale") {
		t.Errorf("second run pruned again:\n%s", out2)
	}
}

// ATTACK 4b: --strict --prune-stale where the config edit FAILS (read-only
// config file). No selection can be removed, so no re-sync happens and the
// original errs path runs. --strict must still fail the run.
func TestAttackStrictPruneWithUnwritableConfig(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory write permissions")
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "readonly-config.git")
	writeSkill(t, working, "keeper", "Keeper", nil)
	commitAndPushAll(t, working, "Add skill")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - keeper
      - gone
`)
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	// Make the containing directory read-only so an atomic rename cannot land.
	if err := os.Chmod(filepath.Dir(configPath), 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(configPath), 0o755) })
	withWorkingDir(t, repoRoot)

	output, syncErr := executeCommand("sync", "--strict", "--prune-stale")
	t.Logf("output:\n%s\nerr: %v", output, syncErr)

	if syncErr == nil {
		t.Errorf("ATTACK SUCCEEDED (wrong exit code): --strict exited 0 while the stale selection is still in the config because the prune edit failed.\noutput:\n%s", output)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("ATTACK SUCCEEDED (config corruption): a failed prune still modified the config.\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// ATTACK 5b: the skip message must never appear on a run that also pruned.
func TestAttackSkipMessageNeverCoexistsWithPrune(t *testing.T) {
	_, _ = stalePruneCompoundRepo(t, "beads")
	output, _ := executeCommand("sync", "--prune-stale")
	if strings.Contains(output, "prune-stale skipped") && strings.Contains(output, "pruned stale") {
		t.Errorf("ATTACK SUCCEEDED (misleading output): both the skip message and a prune appear.\noutput:\n%s", output)
	}
}
