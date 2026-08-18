package harnesscompile

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// --- Materialize L4 Tests ---

// TestMaterialize_WritesPartialResultAndReportsItAsUnclean pins the partial
// contract. Compilation already dropped every artifact of the failing resource,
// so what remains is complete output for the resources that compiled cleanly
// and must reach disk. The caller must not be able to read that as a clean run:
// the error wraps ErrPartialResult and the report is flagged Partial.
func TestMaterialize_WritesPartialResultAndReportsItAsUnclean(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "out")
	result := &Result{
		Artifacts: []Artifact{{Path: "kept.txt", Bytes: []byte("valid resource output")}},
		Diagnostics: []Diagnostic{{
			Code:     diag.CodeSubagentSecurityRestrictionUnsupported,
			Severity: diag.SeverityError,
			Resource: "restricted-agent",
			Message:  "security restriction",
		}},
		Skipped: []SkippedResource{{
			Kind:  ir.KindSubagent,
			Name:  "restricted-agent",
			Codes: []string{diag.CodeSubagentSecurityRestrictionUnsupported},
		}},
	}

	report, err := Materialize(result, outDir, WithWriteMode(WriteModeOverwrite))
	if !errors.Is(err, ErrPartialResult) {
		t.Fatalf("Materialize error = %v, want ErrPartialResult", err)
	}
	if report == nil {
		t.Fatal("Materialize must return the report describing what reached disk")
	}
	if !report.Partial {
		t.Error("report.Partial = false, want true")
	}
	if len(report.SkippedResources) != 1 || report.SkippedResources[0].Name != "restricted-agent" {
		t.Errorf("report.SkippedResources = %+v, want the skipped subagent", report.SkippedResources)
	}
	if len(report.Written) != 1 || report.Written[0] != "kept.txt" {
		t.Errorf("report.Written = %v, want [kept.txt]", report.Written)
	}
	got, readErr := os.ReadFile(filepath.Join(outDir, "kept.txt"))
	if readErr != nil {
		t.Fatalf("read kept.txt: %v", readErr)
	}
	if string(got) != "valid resource output" {
		t.Errorf("kept.txt = %q, want %q", got, "valid resource output")
	}
}

// TestMaterialize_PartialResultDoesNotPrune guards the destructive edge of the
// partial contract: a resource that was skipped this run is indistinguishable
// from one that was removed, so pruning must not delete the output the previous
// clean run wrote.
func TestMaterialize_PartialResultDoesNotPrune(t *testing.T) {
	outDir := t.TempDir()
	stale := filepath.Join(outDir, "from-previous-run.txt")
	if err := os.WriteFile(stale, []byte("previous output"), 0o644); err != nil {
		t.Fatalf("seed stale file: %v", err)
	}

	result := &Result{
		Artifacts: []Artifact{{Path: "kept.txt", Bytes: []byte("valid")}},
		Diagnostics: []Diagnostic{{
			Code:     diag.CodeSubagentSecurityRestrictionUnsupported,
			Severity: diag.SeverityError,
			Message:  "security restriction",
		}},
		Skipped: []SkippedResource{{
			Kind:  ir.KindSubagent,
			Name:  "restricted-agent",
			Codes: []string{diag.CodeSubagentSecurityRestrictionUnsupported},
		}},
	}

	report, err := Materialize(result, outDir, WithWriteMode(WriteModeSync), WithPruneStale(true))
	if !errors.Is(err, ErrPartialResult) {
		t.Fatalf("Materialize error = %v, want ErrPartialResult", err)
	}
	if !report.PruneSuppressed {
		t.Error("report.PruneSuppressed = false, want true")
	}
	if len(report.Removed) != 0 {
		t.Errorf("report.Removed = %v, want none", report.Removed)
	}
	if _, statErr := os.Stat(stale); statErr != nil {
		t.Errorf("stale file stat = %v, want it preserved by a partial run", statErr)
	}
}

// TestMaterialize_CleanResultPrunesAndReportsClean is the control case: without
// error diagnostics nothing changes — pruning happens and no error is returned.
func TestMaterialize_CleanResultPrunesAndReportsClean(t *testing.T) {
	outDir := t.TempDir()
	stale := filepath.Join(outDir, "stale.txt")
	if err := os.WriteFile(stale, []byte("stale"), 0o644); err != nil {
		t.Fatalf("seed stale file: %v", err)
	}

	result := &Result{Artifacts: []Artifact{{Path: "kept.txt", Bytes: []byte("valid")}}}
	report, err := Materialize(result, outDir, WithWriteMode(WriteModeSync), WithPruneStale(true))
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if report.Partial || report.PruneSuppressed {
		t.Errorf("report.Partial = %v, PruneSuppressed = %v, want false/false", report.Partial, report.PruneSuppressed)
	}
	if len(report.Removed) != 1 || report.Removed[0] != "stale.txt" {
		t.Errorf("report.Removed = %v, want [stale.txt]", report.Removed)
	}
	if _, statErr := os.Stat(stale); !os.IsNotExist(statErr) {
		t.Errorf("stale file stat = %v, want it pruned", statErr)
	}
}

func TestMaterialize_BasicWrite(t *testing.T) {
	outDir := t.TempDir()
	result := &Result{
		Artifacts: []Artifact{
			{Path: "hello.txt", Bytes: []byte("hello world")},
			{Path: "sub/dir/nested.txt", Bytes: []byte("nested content")},
		},
	}

	report, err := Materialize(result, outDir, WithWriteMode(WriteModeOverwrite))
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	if len(report.Written) != 2 {
		t.Fatalf("Written = %d, want 2; got %v", len(report.Written), report.Written)
	}
	if len(report.Skipped) != 0 {
		t.Fatalf("Skipped = %d, want 0", len(report.Skipped))
	}
	if len(report.Removed) != 0 {
		t.Fatalf("Removed = %d, want 0", len(report.Removed))
	}

	// Verify files exist with correct content
	got, err := os.ReadFile(filepath.Join(outDir, "hello.txt"))
	if err != nil {
		t.Fatalf("read hello.txt: %v", err)
	}
	if string(got) != "hello world" {
		t.Fatalf("hello.txt = %q, want %q", got, "hello world")
	}

	got, err = os.ReadFile(filepath.Join(outDir, "sub/dir/nested.txt"))
	if err != nil {
		t.Fatalf("read nested.txt: %v", err)
	}
	if string(got) != "nested content" {
		t.Fatalf("nested.txt = %q, want %q", got, "nested content")
	}
}

func TestMaterialize_RefusesExternalReconciliationArtifact(t *testing.T) {
	out := t.TempDir()
	path := filepath.Join(out, "config.toml")
	original := []byte("handwritten = true\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	result := &Result{Artifacts: []Artifact{{Path: "config.toml", Bytes: []byte("generated = true\n"), ExternalReconciliationRequired: true}}}
	if _, err := Materialize(result, out); !errors.Is(err, ErrExternalReconciliationRequired) {
		t.Fatalf("Materialize error = %v, want %v", err, ErrExternalReconciliationRequired)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("externally reconciled file changed:\n%s", got)
	}
}

func TestMaterialize_DeduplicatesIdenticalArtifacts(t *testing.T) {
	outDir := t.TempDir()
	result := &Result{
		Artifacts: []Artifact{
			{Path: "support.js", Bytes: []byte("console.log('ok')\n"), Mode: 0o755},
			{Path: "support.js", Bytes: []byte("console.log('ok')\n"), Mode: 0o755},
		},
	}

	report, err := Materialize(result, outDir, WithWriteMode(WriteModeOverwrite))
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if len(report.Written) != 1 {
		t.Fatalf("Written = %d, want 1; got %v", len(report.Written), report.Written)
	}
	got, err := os.ReadFile(filepath.Join(outDir, "support.js"))
	if err != nil {
		t.Fatalf("read support.js: %v", err)
	}
	if string(got) != "console.log('ok')\n" {
		t.Fatalf("support.js = %q", got)
	}
}

func TestMaterialize_RejectsConflictingDuplicateArtifacts(t *testing.T) {
	tests := []struct {
		name      string
		artifacts []Artifact
	}{
		{
			name: "different bytes",
			artifacts: []Artifact{
				{Path: "support.js", Bytes: []byte("one")},
				{Path: "support.js", Bytes: []byte("two")},
			},
		},
		{
			name: "different metadata",
			artifacts: []Artifact{
				{
					Path:           "support.js",
					Bytes:          []byte("console.log('ok')\n"),
					Mode:           0o644,
					ManagedByField: "_managed_by",
					ManagedByValue: "agent-marketplace",
				},
				{
					Path:           "support.js",
					Bytes:          []byte("console.log('ok')\n"),
					Mode:           0o755,
					ManagedByField: "_managed_by",
					ManagedByValue: "other",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outDir := t.TempDir()
			result := &Result{Artifacts: tt.artifacts}

			_, err := Materialize(result, outDir, WithWriteMode(WriteModeOverwrite))
			if err == nil {
				t.Fatal("Materialize should reject conflicting duplicate artifact paths")
			}
			if !strings.Contains(err.Error(), "harnesscompile.Materialize: duplicate artifact path with different contents: support.js") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestMaterialize_FailIfExists(t *testing.T) {
	outDir := t.TempDir()

	// Pre-create a file
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "existing.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := &Result{
		Artifacts: []Artifact{
			{Path: "existing.txt", Bytes: []byte("new content")},
		},
	}

	_, err := Materialize(result, outDir) // default is FailIfExists
	if err == nil {
		t.Fatal("Materialize should fail when file already exists with FailIfExists mode")
	}
}

func TestMaterialize_Overwrite(t *testing.T) {
	outDir := t.TempDir()

	// Pre-create a file
	if err := os.WriteFile(filepath.Join(outDir, "existing.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := &Result{
		Artifacts: []Artifact{
			{Path: "existing.txt", Bytes: []byte("new content")},
		},
	}

	report, err := Materialize(result, outDir, WithWriteMode(WriteModeOverwrite))
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if len(report.Written) != 1 {
		t.Fatalf("Written = %d, want 1", len(report.Written))
	}

	got, err := os.ReadFile(filepath.Join(outDir, "existing.txt"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "new content" {
		t.Fatalf("existing.txt = %q, want %q", got, "new content")
	}
}

func TestMaterialize_IdentityDetection(t *testing.T) {
	outDir := t.TempDir()

	content := []byte("identical content")
	hash := sha256.Sum256(content)
	_ = hex.EncodeToString(hash[:]) // just to ensure we compute it

	// Write the same content to disk
	if err := os.WriteFile(filepath.Join(outDir, "same.txt"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	result := &Result{
		Artifacts: []Artifact{
			{Path: "same.txt", Bytes: content},
		},
	}

	report, err := Materialize(result, outDir, WithWriteMode(WriteModeOverwrite))
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if len(report.Written) != 0 {
		t.Fatalf("Written = %d, want 0 (should be skipped due to sha256 match)", len(report.Written))
	}
	if len(report.Skipped) != 1 {
		t.Fatalf("Skipped = %d, want 1", len(report.Skipped))
	}
	if report.Skipped[0] != "same.txt" {
		t.Fatalf("Skipped[0] = %q, want %q", report.Skipped[0], "same.txt")
	}
}

func TestMaterialize_Idempotency(t *testing.T) {
	outDir := t.TempDir()

	result := &Result{
		Artifacts: []Artifact{
			{Path: "file1.txt", Bytes: []byte("content1")},
			{Path: "file2.txt", Bytes: []byte("content2")},
		},
	}

	// First materialize
	report1, err := Materialize(result, outDir, WithWriteMode(WriteModeOverwrite))
	if err != nil {
		t.Fatalf("first Materialize: %v", err)
	}
	if len(report1.Written) != 2 {
		t.Fatalf("first: Written = %d, want 2", len(report1.Written))
	}

	// Second materialize with same input → all Skipped, zero Written
	report2, err := Materialize(result, outDir, WithWriteMode(WriteModeOverwrite))
	if err != nil {
		t.Fatalf("second Materialize: %v", err)
	}
	if len(report2.Written) != 0 {
		t.Fatalf("second: Written = %d, want 0 (idempotent)", len(report2.Written))
	}
	if len(report2.Skipped) != 2 {
		t.Fatalf("second: Skipped = %d, want 2", len(report2.Skipped))
	}
}

func TestMaterialize_PruneStale(t *testing.T) {
	outDir := t.TempDir()

	// Pre-create stale files
	if err := os.WriteFile(filepath.Join(outDir, "stale1.txt"), []byte("old1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "stale2.txt"), []byte("old2"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := &Result{
		Artifacts: []Artifact{
			{Path: "fresh.txt", Bytes: []byte("new content")},
		},
	}

	report, err := Materialize(result, outDir, WithWriteMode(WriteModeSync), WithPruneStale(true))
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	if len(report.Written) != 1 {
		t.Fatalf("Written = %d, want 1", len(report.Written))
	}
	if len(report.Removed) != 2 {
		t.Fatalf("Removed = %d, want 2; got %v", len(report.Removed), report.Removed)
	}

	// Verify stale files are gone
	if _, err := os.Stat(filepath.Join(outDir, "stale1.txt")); !os.IsNotExist(err) {
		t.Fatal("stale1.txt should be removed")
	}
	if _, err := os.Stat(filepath.Join(outDir, "stale2.txt")); !os.IsNotExist(err) {
		t.Fatal("stale2.txt should be removed")
	}

	// Verify fresh file exists
	got, err := os.ReadFile(filepath.Join(outDir, "fresh.txt"))
	if err != nil {
		t.Fatalf("read fresh.txt: %v", err)
	}
	if string(got) != "new content" {
		t.Fatalf("fresh.txt = %q, want %q", got, "new content")
	}
}

func TestMaterialize_PruneStaleSubdirectory(t *testing.T) {
	outDir := t.TempDir()

	// Pre-create stale files in a subdirectory
	subDir := filepath.Join(outDir, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "stale.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := &Result{
		Artifacts: []Artifact{
			{Path: "fresh.txt", Bytes: []byte("new")},
		},
	}

	report, err := Materialize(result, outDir, WithWriteMode(WriteModeSync), WithPruneStale(true))
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	if len(report.Removed) != 1 {
		t.Fatalf("Removed = %d, want 1; got %v", len(report.Removed), report.Removed)
	}
	if report.Removed[0] != "sub/stale.txt" {
		t.Fatalf("Removed[0] = %q, want %q", report.Removed[0], "sub/stale.txt")
	}
}

func TestMaterialize_DryRun(t *testing.T) {
	outDir := t.TempDir()

	result := &Result{
		Artifacts: []Artifact{
			{Path: "file1.txt", Bytes: []byte("content1")},
			{Path: "file2.txt", Bytes: []byte("content2")},
		},
	}

	report, err := Materialize(result, outDir, WithWriteMode(WriteModeOverwrite), WithDryRun())
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	if len(report.Written) != 2 {
		t.Fatalf("dry-run Written = %d, want 2", len(report.Written))
	}
	if report.OutDir != outDir {
		t.Fatalf("OutDir = %q, want %q", report.OutDir, outDir)
	}

	// Verify NO files actually exist on disk
	if _, err := os.Stat(filepath.Join(outDir, "file1.txt")); !os.IsNotExist(err) {
		t.Fatal("file1.txt should NOT exist after dry run")
	}
	if _, err := os.Stat(filepath.Join(outDir, "file2.txt")); !os.IsNotExist(err) {
		t.Fatal("file2.txt should NOT exist after dry run")
	}
}

func TestMaterialize_RejectsSymlinkedArtifactParent(t *testing.T) {
	outDir := t.TempDir()
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "outside.txt"), []byte("untouched"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(outDir, "linked")
	if err := os.Symlink(external, link); err != nil {
		t.Fatal(err)
	}

	_, err := Materialize(&Result{Artifacts: []Artifact{{Path: "linked/generated.txt", Bytes: []byte("must not write")}}}, outDir, WithWriteMode(WriteModeOverwrite))
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Materialize error = %v, want symlink-parent rejection", err)
	}
	if _, err := os.Stat(filepath.Join(external, "generated.txt")); !os.IsNotExist(err) {
		t.Fatal("materialization must not write through symlinked parent")
	}
}

func TestMaterialize_PathTraversal(t *testing.T) {
	outDir := t.TempDir()

	tests := []struct {
		name string
		path string
	}{
		{"parent escape", "../escape.txt"},
		{"double parent", "sub/../../escape.txt"},
		{"absolute path", "/tmp/evil.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &Result{
				Artifacts: []Artifact{
					{Path: tt.path, Bytes: []byte("evil")},
				},
			}
			_, err := Materialize(result, outDir, WithWriteMode(WriteModeOverwrite))
			if err == nil {
				t.Fatalf("Materialize should reject path %q (path traversal)", tt.path)
			}
		})
	}
}

func TestMaterialize_FragmentMerge(t *testing.T) {
	outDir := t.TempDir()

	// Two JSON fragments for the same path that should be merged
	result := &Result{
		Artifacts: []Artifact{
			{
				Path:        "merged.json",
				Bytes:       []byte(`{"key1": "val1"}`),
				Fragment:    true,
				FragmentKey: "merged",
			},
			{
				Path:        "merged.json",
				Bytes:       []byte(`{"key2": "val2"}`),
				Fragment:    true,
				FragmentKey: "merged",
			},
		},
	}

	report, err := Materialize(result, outDir, WithWriteMode(WriteModeOverwrite))
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if len(report.Written) != 1 {
		t.Fatalf("Written = %d, want 1 (merged fragment)", len(report.Written))
	}

	got, err := os.ReadFile(filepath.Join(outDir, "merged.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// The merged JSON should contain both keys
	if !strings.Contains(string(got), `"key1"`) || !strings.Contains(string(got), `"key2"`) {
		t.Fatalf("merged.json = %q, want both key1 and key2", got)
	}
}

func TestMaterialize_MergeWithExistingPreservesUserEntriesAndPrunesManaged(t *testing.T) {
	outDir := t.TempDir()
	path := filepath.Join(outDir, "mcp.json")
	if err := os.WriteFile(path, []byte(`{
  "mcpServers": {
    "user-server": {"command": "user"},
    "stale-server": {"command": "stale", "_managed_by": "agent-marketplace"}
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	result := &Result{
		Artifacts: []Artifact{{
			Path:              "mcp.json",
			Bytes:             []byte(`{"mcpServers":{"managed-server":{"command":"managed"}}}`),
			MergeWithExisting: true,
			ManagedByField:    "_managed_by",
			ManagedByValue:    "agent-marketplace",
		}},
	}

	report, err := Materialize(result, outDir, WithWriteMode(WriteModeOverwrite))
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if len(report.Written) != 1 || report.Written[0] != "mcp.json" {
		t.Fatalf("Written = %v, want [mcp.json]", report.Written)
	}
	if len(report.MergedFiles) != 1 {
		t.Fatalf("MergedFiles = %d, want 1", len(report.MergedFiles))
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read mcp.json: %v", err)
	}
	gotStr := string(got)
	if !strings.Contains(gotStr, `"user-server"`) {
		t.Fatalf("expected user-server to be preserved, got %s", gotStr)
	}
	if strings.Contains(gotStr, `"stale-server"`) {
		t.Fatalf("expected stale-server to be pruned, got %s", gotStr)
	}
	if !strings.Contains(gotStr, `"_managed_by": "agent-marketplace"`) {
		t.Fatalf("expected managed marker, got %s", gotStr)
	}
}

func TestMaterialize_EmptyResult(t *testing.T) {
	outDir := t.TempDir()

	result := &Result{}

	report, err := Materialize(result, outDir, WithWriteMode(WriteModeOverwrite))
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if len(report.Written) != 0 {
		t.Fatalf("Written = %d, want 0", len(report.Written))
	}
}

func TestMaterialize_NilResult(t *testing.T) {
	outDir := t.TempDir()

	_, err := Materialize(nil, outDir, WithWriteMode(WriteModeOverwrite))
	if err == nil {
		t.Fatal("Materialize(nil) should error")
	}
}

func TestMaterialize_NoStagingDirRemains(t *testing.T) {
	outDir := t.TempDir()

	result := &Result{
		Artifacts: []Artifact{
			{Path: "file.txt", Bytes: []byte("content")},
		},
	}

	_, err := Materialize(result, outDir, WithWriteMode(WriteModeOverwrite))
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	// No .tmp- directories should remain
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Fatalf("staging directory %q should not remain after Materialize", e.Name())
		}
	}
}

// --- Load→Compile(Claude)→Materialize round-trip test ---

func TestMaterialize_RoundTrip_ClaudeSkill(t *testing.T) {
	outDir := t.TempDir()

	// Create IR skill directly
	skill := ir.NewSkill("my-skill", "A test skill", "Do the thing", []string{"click"}, nil, ir.SourceRef{File: "skills/my-skill/SKILL.md", Line: 1})

	result, err := Compile(skill, TargetClaude)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	report, err := Materialize(result, outDir, WithWriteMode(WriteModeOverwrite))
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	if len(report.Written) == 0 {
		t.Fatal("Written should not be empty for round-trip")
	}

	// Verify the skill body file was written and includes frontmatter
	got, err := os.ReadFile(filepath.Join(outDir, "skills/my-skill/SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	if !strings.Contains(string(got), "Do the thing") {
		t.Fatalf("SKILL.md = %q, want content containing 'Do the thing'", got)
	}
	if !strings.HasPrefix(string(got), "---\n") {
		t.Fatalf("SKILL.md should start with frontmatter, got: %q", string(got)[:min(len(string(got)), 80)])
	}
	if !strings.Contains(string(got), "name: my-skill") {
		t.Fatalf("SKILL.md frontmatter should contain 'name: my-skill', got: %q", got)
	}
}

func TestMaterialize_SyncModePruneAndWrite(t *testing.T) {
	outDir := t.TempDir()

	// Pre-create files: one that will be overwritten, one that will be pruned
	if err := os.MkdirAll(filepath.Join(outDir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "keep.txt"), []byte("old-keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "sub", "prune.txt"), []byte("old-prune"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := &Result{
		Artifacts: []Artifact{
			{Path: "keep.txt", Bytes: []byte("new-keep")},
			{Path: "fresh.txt", Bytes: []byte("new-fresh")},
		},
	}

	report, err := Materialize(result, outDir, WithWriteMode(WriteModeSync), WithPruneStale(true))
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	// keep.txt should be written (content differs)
	foundKeep := false
	for _, w := range report.Written {
		if w == "keep.txt" {
			foundKeep = true
		}
	}
	if !foundKeep {
		t.Fatal("keep.txt should be in Written list (content changed)")
	}

	// sub/prune.txt should be removed
	if len(report.Removed) != 1 || report.Removed[0] != "sub/prune.txt" {
		t.Fatalf("Removed = %v, want [sub/prune.txt]", report.Removed)
	}

	// fresh.txt should exist
	got, err := os.ReadFile(filepath.Join(outDir, "fresh.txt"))
	if err != nil {
		t.Fatalf("read fresh.txt: %v", err)
	}
	if string(got) != "new-fresh" {
		t.Fatalf("fresh.txt = %q, want %q", got, "new-fresh")
	}
}

func TestMaterialize_DryRunWithPruneStale(t *testing.T) {
	outDir := t.TempDir()

	// Pre-create stale file
	if err := os.WriteFile(filepath.Join(outDir, "stale.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := &Result{
		Artifacts: []Artifact{
			{Path: "fresh.txt", Bytes: []byte("new")},
		},
	}

	report, err := Materialize(result, outDir, WithWriteMode(WriteModeSync), WithPruneStale(true), WithDryRun())
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	// Plan should show removal
	if len(report.Removed) != 1 {
		t.Fatalf("dry-run Removed = %d, want 1", len(report.Removed))
	}
	if report.Removed[0] != "stale.txt" {
		t.Fatalf("dry-run Removed[0] = %q, want %q", report.Removed[0], "stale.txt")
	}

	// But stale.txt should still exist on disk (dry run)
	if _, err := os.Stat(filepath.Join(outDir, "stale.txt")); os.IsNotExist(err) {
		t.Fatal("stale.txt should still exist after dry run")
	}
}

// Ensure diag package is referenced for compilation
var _ diag.Diagnostic
var _ = sha256.Sum256
var _ = hex.EncodeToString
