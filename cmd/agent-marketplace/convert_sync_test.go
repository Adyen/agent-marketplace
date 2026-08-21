package main

import (
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
