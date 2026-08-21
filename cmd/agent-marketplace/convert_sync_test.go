package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolatedConvertState points the durable convert-ownership store at a private
// directory so one test can never read or prune another test's record.
func isolatedConvertState(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
}

func convertSyncFixture(t *testing.T, skills ...string) (srcDir, outDir string) {
	t.Helper()
	srcDir = t.TempDir()
	for _, name := range skills {
		writeClaudePluginSkill(t, srcDir, name)
	}
	return srcDir, filepath.Join(t.TempDir(), "factory-out")
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(raw), want) {
		t.Fatalf("expected %s to contain %q, got:\n%s", path, want, raw)
	}
}

func assertFileAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be absent, stat error = %v", path, err)
	}
}

// Given an output directory holding a handwritten top-level file, when convert
// --sync runs, then the unrelated file survives and is reported as preserved.
func TestConvertSyncPreservesUnrelatedTopLevelFile(t *testing.T) {
	isolatedConvertState(t)
	srcDir, outDir := convertSyncFixture(t, "demo-skill")
	unrelated := filepath.Join(outDir, "README.md")
	writeFile(t, unrelated, "handwritten notes\n")

	output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--out", outDir, srcDir)
	if err != nil {
		t.Fatalf("expected convert --sync to succeed: %v\n%s", err, output)
	}

	assertFileContains(t, unrelated, "handwritten notes")
	assertFileExists(t, filepath.Join(outDir, "skills", "demo-skill", "SKILL.md"))
	if !strings.Contains(output, "preserved") || !strings.Contains(output, "README.md") {
		t.Fatalf("expected preserved report naming README.md, got:\n%s", output)
	}
	if !strings.Contains(output, "Removed 0") {
		t.Fatalf("expected no removals, got:\n%s", output)
	}
}

// Given unrelated files nested below the output root, when convert --sync runs,
// then every nested unrelated file survives, including one inside a directory
// the conversion itself writes into.
func TestConvertSyncPreservesUnrelatedNestedFiles(t *testing.T) {
	isolatedConvertState(t)
	srcDir, outDir := convertSyncFixture(t, "demo-skill")
	nested := map[string]string{
		filepath.Join(outDir, "skills", "handwritten-skill", "SKILL.md"): "hand-authored skill\n",
		filepath.Join(outDir, "skills", "demo-skill", "notes.md"):        "notes inside a generated skill\n",
		filepath.Join(outDir, "docs", "deep", "nested", "design.md"):     "deep design doc\n",
	}
	for path, body := range nested {
		writeFile(t, path, body)
	}

	output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--out", outDir, srcDir)
	if err != nil {
		t.Fatalf("expected convert --sync to succeed: %v\n%s", err, output)
	}

	for path, body := range nested {
		assertFileContains(t, path, strings.TrimSpace(body))
	}
	assertFileExists(t, filepath.Join(outDir, "skills", "demo-skill", "SKILL.md"))
	for _, want := range []string{"skills/handwritten-skill/SKILL.md", "skills/demo-skill/notes.md", "docs/deep/nested/design.md"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected preserved report to name %q, got:\n%s", want, output)
		}
	}
}

// Given a previous convert --sync run recorded its own output, when a resource
// disappears from the source, then only the recorded output is pruned.
func TestConvertSyncPrunesOnlyItsOwnRecordedOutput(t *testing.T) {
	isolatedConvertState(t)
	srcDir, outDir := convertSyncFixture(t, "demo-skill", "going-away")
	unrelated := filepath.Join(outDir, "keep-me.md")

	if output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--out", outDir, srcDir); err != nil {
		t.Fatalf("expected first convert --sync to succeed: %v\n%s", err, output)
	}
	writeFile(t, unrelated, "handwritten\n")
	if err := os.RemoveAll(filepath.Join(srcDir, "skills", "going-away")); err != nil {
		t.Fatal(err)
	}

	output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--out", outDir, srcDir)
	if err != nil {
		t.Fatalf("expected second convert --sync to succeed: %v\n%s", err, output)
	}

	assertFileAbsent(t, filepath.Join(outDir, "skills", "going-away", "SKILL.md"))
	assertFileExists(t, filepath.Join(outDir, "skills", "demo-skill", "SKILL.md"))
	assertFileContains(t, unrelated, "handwritten")
	if !strings.Contains(output, "Removed 1") {
		t.Fatalf("expected exactly one removal, got:\n%s", output)
	}
}

// Given a recorded output file the user then edited, when it becomes stale,
// then the edit is preserved instead of pruned.
func TestConvertSyncPreservesEditedOwnedOutput(t *testing.T) {
	isolatedConvertState(t)
	srcDir, outDir := convertSyncFixture(t, "demo-skill", "going-away")

	if output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--out", outDir, srcDir); err != nil {
		t.Fatalf("expected first convert --sync to succeed: %v\n%s", err, output)
	}
	edited := filepath.Join(outDir, "skills", "going-away", "SKILL.md")
	writeFile(t, edited, "locally edited after generation\n")
	if err := os.RemoveAll(filepath.Join(srcDir, "skills", "going-away")); err != nil {
		t.Fatal(err)
	}

	output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--out", outDir, srcDir)
	if err != nil {
		t.Fatalf("expected second convert --sync to succeed: %v\n%s", err, output)
	}

	assertFileContains(t, edited, "locally edited after generation")
	if !strings.Contains(output, "Removed 0") {
		t.Fatalf("expected no removals for a drifted file, got:\n%s", output)
	}
}

// Given a cache-independent store, when the durable record is destroyed, then
// convert --sync falls back to owning nothing and prunes nothing.
func TestConvertSyncFailsClosedWhenRecordIsCorrupt(t *testing.T) {
	isolatedConvertState(t)
	srcDir, outDir := convertSyncFixture(t, "demo-skill", "going-away")

	if output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--out", outDir, srcDir); err != nil {
		t.Fatalf("expected first convert --sync to succeed: %v\n%s", err, output)
	}
	stateDir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "agent-marketplace", "convert")
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatalf("read state dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one convert record, got %d", len(entries))
	}
	if err := os.WriteFile(filepath.Join(stateDir, entries[0].Name()), []byte("{corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(srcDir, "skills", "going-away")); err != nil {
		t.Fatal(err)
	}

	output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--out", outDir, srcDir)
	if err != nil {
		t.Fatalf("expected convert --sync to succeed with corrupt state: %v\n%s", err, output)
	}

	assertFileExists(t, filepath.Join(outDir, "skills", "going-away", "SKILL.md"))
	if !strings.Contains(output, "Removed 0") {
		t.Fatalf("expected no removals with corrupt ownership state, got:\n%s", output)
	}
}

// Given an unowned output file at a path the conversion would rewrite, when
// convert --sync runs without authorization, then it refuses before writing.
func TestConvertSyncRefusesToReplaceUnownedCollidingFile(t *testing.T) {
	isolatedConvertState(t)
	srcDir, outDir := convertSyncFixture(t, "demo-skill")
	collision := filepath.Join(outDir, "skills", "demo-skill", "SKILL.md")
	writeFile(t, collision, "handwritten skill that must survive\n")

	output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--out", outDir, srcDir)
	if err == nil {
		t.Fatalf("expected convert --sync to refuse an unowned collision, got:\n%s", output)
	}
	if !strings.Contains(output, "convert.sync.unmanaged-collision") {
		t.Fatalf("expected the refusal code in the output, got:\n%s", output)
	}
	if !strings.Contains(output, "--replace-unmanaged") {
		t.Fatalf("expected the refusal to name the authorizing flag, got:\n%s", output)
	}
	assertFileContains(t, collision, "handwritten skill that must survive")
}

// Given explicit authorization, when convert --sync runs, then it replaces the
// colliding file and prunes unrelated output.
func TestConvertSyncReplaceUnmanagedAuthorizesDestructiveReplacement(t *testing.T) {
	isolatedConvertState(t)
	srcDir, outDir := convertSyncFixture(t, "demo-skill")
	collision := filepath.Join(outDir, "skills", "demo-skill", "SKILL.md")
	writeFile(t, collision, "handwritten skill\n")
	unrelated := filepath.Join(outDir, "docs", "unrelated.md")
	writeFile(t, unrelated, "unrelated\n")

	output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--replace-unmanaged", "--out", outDir, srcDir)
	if err != nil {
		t.Fatalf("expected authorized convert --sync to succeed: %v\n%s", err, output)
	}

	assertFileContains(t, collision, "Demo skill")
	assertFileAbsent(t, unrelated)
	if !strings.Contains(output, "Removed 1") {
		t.Fatalf("expected the unrelated file to be pruned, got:\n%s", output)
	}
}

// Given a dry run, when convert --sync would prune recorded output, then
// nothing changes on disk and the record is left intact.
func TestConvertSyncDryRunChangesNothing(t *testing.T) {
	isolatedConvertState(t)
	srcDir, outDir := convertSyncFixture(t, "demo-skill", "going-away")

	if output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--out", outDir, srcDir); err != nil {
		t.Fatalf("expected first convert --sync to succeed: %v\n%s", err, output)
	}
	if err := os.RemoveAll(filepath.Join(srcDir, "skills", "going-away")); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(outDir, "keep-me.md")
	writeFile(t, unrelated, "handwritten\n")

	output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--dry-run", "--out", outDir, srcDir)
	if err != nil {
		t.Fatalf("expected convert --sync --dry-run to succeed: %v\n%s", err, output)
	}

	assertFileExists(t, filepath.Join(outDir, "skills", "going-away", "SKILL.md"))
	assertFileContains(t, unrelated, "handwritten")
	if !strings.Contains(output, "Removed 1") {
		t.Fatalf("expected the dry run to plan one removal, got:\n%s", output)
	}
	if !strings.Contains(output, "keep-me.md") {
		t.Fatalf("expected the dry run to report the preserved file, got:\n%s", output)
	}

	// The record must still describe the previous run, so a later real sync can
	// still prune what this dry run only planned.
	output, err = executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--out", outDir, srcDir)
	if err != nil {
		t.Fatalf("expected follow-up convert --sync to succeed: %v\n%s", err, output)
	}
	assertFileAbsent(t, filepath.Join(outDir, "skills", "going-away", "SKILL.md"))
	assertFileContains(t, unrelated, "handwritten")
}

// Given a symlink inside the output tree, when convert --sync runs, then the
// link is preserved rather than pruned, since nothing proves it was generated.
func TestConvertSyncPreservesSymlinkedOutputEntry(t *testing.T) {
	isolatedConvertState(t)
	srcDir, outDir := convertSyncFixture(t, "demo-skill")
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "secret.md")
	writeFile(t, outside, "outside the output tree\n")
	link := filepath.Join(outDir, "skills", "linked.md")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--out", outDir, srcDir)
	if err != nil {
		t.Fatalf("expected convert --sync to succeed: %v\n%s", err, output)
	}

	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("expected the symlink to survive: %v", err)
	}
	assertFileContains(t, outside, "outside the output tree")
	if !strings.Contains(output, "Removed 0") {
		t.Fatalf("expected no removals, got:\n%s", output)
	}
}

// Given a run that could not prune because the conversion was partial, when a
// later clean run reconciles the same output, then the ownership the partial run
// inherited is still there and the stale file is pruned.
//
// The partial run writes a record too. If that record described only what the
// partial run itself materialized, the previous run's output would become
// unmanaged permanently: pruning it would never again be authorized, and the
// conversion's own file would start reading as an unmanaged collision.
func TestConvertSyncPartialRunKeepsOwnershipItCouldNotPrune(t *testing.T) {
	isolatedConvertState(t)
	srcDir, outDir := convertSyncFixture(t, "demo-skill", "going-away")
	stale := filepath.Join(outDir, "skills", "going-away", "SKILL.md")

	if output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--out", outDir, srcDir); err != nil {
		t.Fatalf("expected first convert --sync to succeed: %v\n%s", err, output)
	}

	// A subagent carrying a restriction Factory cannot represent is an error
	// diagnostic, which makes the result partial and suppresses pruning.
	restricted := filepath.Join(srcDir, "agents", "restricted.md")
	writeFile(t, restricted, "---\nname: restricted\ndescription: Restricted agent\nmodel: sonnet\nisolation: worktree\n---\n\nBody\n")
	if err := os.RemoveAll(filepath.Join(srcDir, "skills", "going-away")); err != nil {
		t.Fatal(err)
	}

	output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--out", outDir, srcDir)
	if err == nil {
		t.Fatalf("expected a partial conversion to fail the run, got:\n%s", output)
	}
	if !strings.Contains(output, "pruning was skipped because the conversion was partial") {
		t.Fatalf("expected the partial run to report suppressed pruning, got:\n%s", output)
	}
	assertFileExists(t, stale)

	if err := os.Remove(restricted); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(outDir, "keep-me.md")
	writeFile(t, unrelated, "handwritten\n")

	output, err = executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--out", outDir, srcDir)
	if err != nil {
		t.Fatalf("expected the clean follow-up convert --sync to succeed: %v\n%s", err, output)
	}

	assertFileAbsent(t, stale)
	assertFileContains(t, unrelated, "handwritten")
	if !strings.Contains(output, "Removed 1") {
		t.Fatalf("expected the clean run to prune the file the partial run could not, got:\n%s", output)
	}
}

// Given an output file whose bytes cannot be read, when convert --sync runs,
// then the file is unmanaged and preserved, and --replace-unmanaged can still
// take it over instead of the run failing outright.
func TestConvertSyncTreatsUnreadableOutputAsUnmanaged(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: file permissions do not deny reads")
	}
	isolatedConvertState(t)
	srcDir, outDir := convertSyncFixture(t, "demo-skill")

	if output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--out", outDir, srcDir); err != nil {
		t.Fatalf("expected first convert --sync to succeed: %v\n%s", err, output)
	}
	opaque := filepath.Join(outDir, "docs", "opaque.md")
	writeFile(t, opaque, "unreadable content\n")
	if err := os.Chmod(opaque, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(opaque, 0o600) })
	if _, err := os.ReadFile(opaque); err == nil {
		t.Skip("the filesystem does not deny reads on a 0000 file")
	}

	output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--out", outDir, srcDir)
	if err != nil {
		t.Fatalf("expected convert --sync to succeed with an unreadable output file: %v\n%s", err, output)
	}
	if !strings.Contains(output, "docs/opaque.md") || !strings.Contains(output, "convert.sync.preserved-unmanaged") {
		t.Fatalf("expected the unreadable file to be reported as preserved, got:\n%s", output)
	}
	if !strings.Contains(output, "Removed 0") {
		t.Fatalf("expected no removals, got:\n%s", output)
	}
	if _, err := os.Lstat(opaque); err != nil {
		t.Fatalf("expected the unreadable file to survive: %v", err)
	}

	output, err = executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--replace-unmanaged", "--out", outDir, srcDir)
	if err != nil {
		t.Fatalf("expected --replace-unmanaged to proceed over an unreadable file: %v\n%s", err, output)
	}
	assertFileAbsent(t, opaque)
	if !strings.Contains(output, "Removed 1") {
		t.Fatalf("expected the authorized run to prune the unreadable file, got:\n%s", output)
	}
}

// Given output recorded for one target harness, when convert --sync runs for a
// different target into the same directory, then the record does not authorize
// anything: the other target's output is unmanaged and preserved.
func TestConvertSyncDoesNotPruneAnotherTargetsRecordedOutput(t *testing.T) {
	isolatedConvertState(t)
	srcDir, outDir := convertSyncFixture(t, "demo-skill", "going-away")

	if output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--out", outDir, srcDir); err != nil {
		t.Fatalf("expected the factory convert --sync to succeed: %v\n%s", err, output)
	}
	if err := os.RemoveAll(filepath.Join(srcDir, "skills", "going-away")); err != nil {
		t.Fatal(err)
	}

	output, err := executeCommand("convert", "--from", "claude", "--to", "gemini", "--sync", "--out", outDir, srcDir)
	if err != nil {
		t.Fatalf("expected the gemini convert --sync to succeed: %v\n%s", err, output)
	}

	for _, rel := range []string{"skills/demo-skill/SKILL.md", "skills/going-away/SKILL.md"} {
		assertFileExists(t, filepath.Join(outDir, filepath.FromSlash(rel)))
		if !strings.Contains(output, rel) {
			t.Fatalf("expected %q to be reported as preserved, got:\n%s", rel, output)
		}
	}
	if !strings.Contains(output, "Removed 0") {
		t.Fatalf("expected a record for another target to authorize no pruning, got:\n%s", output)
	}
}

// Given two targets converted into the same output directory in turn, when each
// syncs again, then each still owns and prunes its own output and leaves the
// other target's output alone.
//
// A record keyed only by output root makes whichever target syncs last erase the
// other's record, so alternating targets permanently lose the ability to prune
// their own stale output.
func TestConvertSyncAlternatingTargetsKeepTheirOwnOwnership(t *testing.T) {
	isolatedConvertState(t)
	srcDir, outDir := convertSyncFixture(t, "demo-skill", "going-away")
	factoryStale := filepath.Join(outDir, "skills", "going-away", "SKILL.md")
	geminiOutput := []string{
		filepath.Join(outDir, ".gemini", "skills", "demo-skill", "SKILL.md"),
		filepath.Join(outDir, ".gemini", "skills", "going-away", "SKILL.md"),
	}

	if output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--out", outDir, srcDir); err != nil {
		t.Fatalf("expected the first factory convert --sync to succeed: %v\n%s", err, output)
	}
	if output, err := executeCommand("convert", "--from", "claude", "--to", "gemini", "--sync", "--out", outDir, srcDir); err != nil {
		t.Fatalf("expected the gemini convert --sync to succeed: %v\n%s", err, output)
	}
	for _, path := range geminiOutput {
		assertFileExists(t, path)
	}
	assertFileExists(t, factoryStale)

	if err := os.RemoveAll(filepath.Join(srcDir, "skills", "going-away")); err != nil {
		t.Fatal(err)
	}
	output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--out", outDir, srcDir)
	if err != nil {
		t.Fatalf("expected the second factory convert --sync to succeed: %v\n%s", err, output)
	}

	assertFileAbsent(t, factoryStale)
	assertFileExists(t, filepath.Join(outDir, "skills", "demo-skill", "SKILL.md"))
	if !strings.Contains(output, "Removed 1") {
		t.Fatalf("expected factory to still prune its own stale output after a gemini sync, got:\n%s", output)
	}
	for _, path := range geminiOutput {
		assertFileExists(t, path)
	}

	// Both records coexist, one per target, for the same output root.
	stateDir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "agent-marketplace", "convert")
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatalf("read state dir: %v", err)
	}
	if len(entries) != 2 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("convert records = %v, want one per target harness", names)
	}
}

// Given both an unowned colliding destination and unowned stale files, when
// convert --sync refuses, then it describes the complete bounded scope of what
// --replace-unmanaged would destroy before the user reaches for that flag.
func TestConvertSyncRefusalDescribesTheFullOverrideScope(t *testing.T) {
	isolatedConvertState(t)
	srcDir, outDir := convertSyncFixture(t, "demo-skill")
	collision := filepath.Join(outDir, "skills", "demo-skill", "SKILL.md")
	writeFile(t, collision, "handwritten skill that must survive\n")
	const unrelatedFiles = convertSyncPathListLimit + 2
	for i := 0; i < unrelatedFiles; i++ {
		writeFile(t, filepath.Join(outDir, "docs", fmt.Sprintf("note-%02d.md", i)), "handwritten\n")
	}

	output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--out", outDir, srcDir)
	if err == nil {
		t.Fatalf("expected convert --sync to refuse an unowned collision, got:\n%s", output)
	}

	wants := []string{
		convertSyncCollisionCode,
		fmt.Sprintf("would destroy %d output file(s)", unrelatedFiles+1),
		"replace 1 unowned file(s)",
		"replace skills/demo-skill/SKILL.md",
		fmt.Sprintf("delete %d unowned file(s)", unrelatedFiles),
		fmt.Sprintf("delete %d more file(s)", unrelatedFiles-convertSyncPathListLimit),
		fmt.Sprintf("authorize destroying all %d unowned file(s)", unrelatedFiles+1),
	}
	for _, want := range wants {
		if !strings.Contains(output, want) {
			t.Fatalf("expected the refusal to state %q, got:\n%s", want, output)
		}
	}

	// Describing the scope must not perform any of it.
	assertFileContains(t, collision, "handwritten skill that must survive")
	for i := 0; i < unrelatedFiles; i++ {
		assertFileContains(t, filepath.Join(outDir, "docs", fmt.Sprintf("note-%02d.md", i)), "handwritten")
	}
}

// Given explicit authorization, when convert --sync destroys unowned output,
// then it states the same scope before doing any of it.
func TestConvertSyncAuthorizedRunStatesItsScopeBeforeDestroying(t *testing.T) {
	isolatedConvertState(t)
	srcDir, outDir := convertSyncFixture(t, "demo-skill")
	collision := filepath.Join(outDir, "skills", "demo-skill", "SKILL.md")
	writeFile(t, collision, "handwritten skill\n")
	unrelated := filepath.Join(outDir, "docs", "unrelated.md")
	writeFile(t, unrelated, "unrelated\n")

	output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--replace-unmanaged", "--out", outDir, srcDir)
	if err != nil {
		t.Fatalf("expected authorized convert --sync to succeed: %v\n%s", err, output)
	}

	for _, want := range []string{
		convertSyncAuthorizedCode,
		"destroying 2 output file(s)",
		"replace skills/demo-skill/SKILL.md",
		"delete docs/unrelated.md",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected the authorized run to state %q, got:\n%s", want, output)
		}
	}
	scope := strings.Index(output, convertSyncAuthorizedCode)
	summary := strings.Index(output, "Converted ")
	if scope < 0 || summary < 0 || scope > summary {
		t.Fatalf("expected the destructive scope before the conversion summary, got:\n%s", output)
	}
	assertFileContains(t, collision, "Demo skill")
	assertFileAbsent(t, unrelated)
}

// Given an unreadable file at a destination the conversion must write, when
// convert --sync runs, then the run is refused with the documented collision
// rather than failing inside the write, and --replace-unmanaged replaces it.
//
// Ownership is proved by comparing bytes, and so is the compilation library's
// decision to write, so a destination it cannot read fails the whole
// materialization — planning included — unless this command settles it first.
func TestConvertSyncSettlesAnUnreadableDestination(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: file permissions do not deny reads")
	}
	isolatedConvertState(t)
	srcDir, outDir := convertSyncFixture(t, "demo-skill")
	destination := filepath.Join(outDir, "skills", "demo-skill", "SKILL.md")
	writeFile(t, destination, "handwritten and unreadable\n")
	if err := os.Chmod(destination, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(destination, 0o600) })
	if _, err := os.ReadFile(destination); err == nil {
		t.Skip("the filesystem does not deny reads on a 0000 file")
	}

	output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--out", outDir, srcDir)
	if err == nil {
		t.Fatalf("expected convert --sync to refuse an unreadable destination, got:\n%s", output)
	}
	for _, want := range []string{convertSyncCollisionCode, "cannot be read", "replace skills/demo-skill/SKILL.md", "--replace-unmanaged"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected the refusal to state %q, got:\n%s", want, output)
		}
	}
	if strings.Contains(output, "plan sync") {
		t.Fatalf("expected a scoped refusal rather than a failure inside planning, got:\n%s", output)
	}

	// A dry run cannot clear the destination, and no plan can be produced while
	// it exists, so it reports instead of previewing a reconciliation it cannot
	// compute — and still changes nothing.
	output, err = executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--replace-unmanaged", "--dry-run", "--out", outDir, srcDir)
	if err == nil {
		t.Fatalf("expected --dry-run over an unreadable destination to report it cannot preview, got:\n%s", output)
	}
	if !strings.Contains(output, convertSyncUnreplaceableCode) {
		t.Fatalf("expected the dry run to name the blocking condition, got:\n%s", output)
	}
	if _, err := os.Lstat(destination); err != nil {
		t.Fatalf("expected the dry run to leave the unreadable destination alone: %v", err)
	}

	output, err = executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--replace-unmanaged", "--out", outDir, srcDir)
	if err != nil {
		t.Fatalf("expected authorized convert --sync to replace an unreadable destination: %v\n%s", err, output)
	}
	if !strings.Contains(output, convertSyncAuthorizedCode) {
		t.Fatalf("expected the authorized run to state its scope, got:\n%s", output)
	}
	assertFileContains(t, destination, "Demo skill")
}

// Given more preserved files than a message should list, when convert --sync
// reports them, then the listing is bounded and the remainder is summarized.
func TestConvertSyncBoundsThePreservedFileListing(t *testing.T) {
	isolatedConvertState(t)
	srcDir, outDir := convertSyncFixture(t, "demo-skill")
	const unrelatedFiles = convertSyncPathListLimit + 3
	for i := 0; i < unrelatedFiles; i++ {
		writeFile(t, filepath.Join(outDir, "docs", fmt.Sprintf("note-%02d.md", i)), "handwritten\n")
	}

	output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--sync", "--out", outDir, srcDir)
	if err != nil {
		t.Fatalf("expected convert --sync to succeed: %v\n%s", err, output)
	}

	if got := strings.Count(output, "preserved "); got != convertSyncPathListLimit+1 {
		t.Fatalf("preserved report lines = %d, want %d (%d paths plus one summary), got:\n%s",
			got, convertSyncPathListLimit+1, convertSyncPathListLimit, output)
	}
	if want := fmt.Sprintf("preserved %d more file(s)", unrelatedFiles-convertSyncPathListLimit); !strings.Contains(output, want) {
		t.Fatalf("expected %q in the report, got:\n%s", want, output)
	}
	for i := 0; i < unrelatedFiles; i++ {
		assertFileContains(t, filepath.Join(outDir, "docs", fmt.Sprintf("note-%02d.md", i)), "handwritten")
	}
}

// Given --replace-unmanaged without --sync, when convert runs, then it refuses
// rather than implying an authorization that has nothing to authorize.
func TestConvertReplaceUnmanagedRequiresSync(t *testing.T) {
	isolatedConvertState(t)
	srcDir, outDir := convertSyncFixture(t, "demo-skill")

	output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--replace-unmanaged", "--out", outDir, srcDir)
	if err == nil {
		t.Fatalf("expected --replace-unmanaged without --sync to fail, got:\n%s", output)
	}
	if !strings.Contains(output, "--replace-unmanaged requires --sync") {
		t.Fatalf("expected an explanatory error, got:\n%s", output)
	}
}

// Given no --sync flag, when convert runs into a directory with unrelated
// files, then the legacy overwrite behavior is unchanged and nothing is pruned.
func TestConvertWithoutSyncNeitherPrunesNorRefuses(t *testing.T) {
	isolatedConvertState(t)
	srcDir, outDir := convertSyncFixture(t, "demo-skill")
	unrelated := filepath.Join(outDir, "README.md")
	writeFile(t, unrelated, "handwritten\n")
	collision := filepath.Join(outDir, "skills", "demo-skill", "SKILL.md")
	writeFile(t, collision, "stale generated body\n")

	output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--out", outDir, srcDir)
	if err != nil {
		t.Fatalf("expected convert without --sync to succeed: %v\n%s", err, output)
	}

	assertFileContains(t, unrelated, "handwritten")
	assertFileContains(t, collision, "Demo skill")
}
