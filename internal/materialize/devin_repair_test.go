package materialize

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// dropOwnershipState deletes the durable record for a target root, which is how
// a real user reaches the repair surface: "Missing or corrupt ownership state
// makes all existing aggregate entries unmanaged". Everything sync wrote is then
// present on disk but unproven, which is exactly the state `doctor --repair`
// exists to recover from.
func dropOwnershipState(t *testing.T, outDir string) {
	t.Helper()
	store, err := ownership.Open(outDir)
	if err != nil {
		t.Fatalf("open ownership store: %v", err)
	}
	if err := os.Remove(store.StateFile()); err != nil {
		t.Fatalf("remove ownership state: %v", err)
	}
}

func devinMcpSelection(names ...string) []resolver.ResolvedArtifact {
	resolved := make([]resolver.ResolvedArtifact, 0, len(names))
	for _, name := range names {
		resolved = append(resolved, devinResolvedResource(name, ir.NewMcpServer(name, name+"-binary", nil, nil, nil, ir.SourceRef{}), false))
	}
	return resolved
}

func repairFileFor(t *testing.T, preview *RepairPreview, rel string) *RepairFile {
	t.Helper()
	for _, file := range preview.Files {
		if file.Rel == rel {
			return file
		}
	}
	t.Fatalf("preview has no repair file for %q (%d files)", rel, len(preview.Files))
	return nil
}

func TestRepairPreviewsEnumeratesEveryValidatedAdapter(t *testing.T) {
	repoRoot := t.TempDir()
	previews, err := RepairPreviews(context.Background(), repoRoot, nil, nil)
	if err != nil {
		t.Fatalf("repair previews: %v", err)
	}
	if len(previews) != 2 {
		t.Fatalf("repair previews = %d, want one preview per migrated adapter", len(previews))
	}
	if previews[0].Target != hcc.TargetCodex || previews[1].Target != hcc.TargetDevin {
		t.Fatalf("repair targets = %#v, want [codex devin]", []hcc.Target{previews[0].Target, previews[1].Target})
	}
}

// TestDevinRepairPreviewIsReadOnlyAndNominatesExactMatchesOnly pins the two
// halves of the preview contract: "`doctor --repair` previews exact adoption
// matches and evidence-backed orphan candidates" and "off-TTY, `doctor --repair`
// is preview-only".
func TestDevinRepairPreviewIsReadOnlyAndNominatesExactMatchesOnly(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	root := devinProjectRoot(repoRoot)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	mcpPath := filepath.Join(root, harness.DevinMcpConfigFile)
	if err := os.WriteFile(mcpPath, []byte("{\n  \"mcpServers\": {\n    \"handwritten\": { \"command\": \"user-binary\" }\n  }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	selection := devinMcpSelection("managed")
	report, err := Run(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatalf("sync run: %v", err)
	}
	requireNoErrorDiags(t, report.Diags)
	dropOwnershipState(t, root)

	before := readFileString(t, mcpPath)
	preview, err := DevinRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if got := readFileString(t, mcpPath); got != before {
		t.Fatalf("preview mutated the aggregate; it must be read-only:\nbefore:\n%s\nafter:\n%s", before, got)
	}

	file := repairFileFor(t, preview, harness.DevinMcpConfigFile)
	if len(file.Adoptions) != 1 {
		t.Fatalf("expected exactly one adoption candidate, got %d", len(file.Adoptions))
	}
	candidate := file.Adoptions[0]
	if candidate.EntryKind() != string(ownership.KindMcpServer) || candidate.Identity() != "managed" {
		t.Fatalf("unexpected adoption candidate %s %q", candidate.EntryKind(), candidate.Identity())
	}
	if candidate.Resource != "managed" {
		t.Fatalf("adoption candidate resource = %q, want %q", candidate.Resource, "managed")
	}
	// The handwritten entry is NOT nominated: it matches no desired entry, so
	// it is neither an exact adoption match nor evidence-backed.
	for _, other := range file.Adoptions {
		if other.Identity() == "handwritten" {
			t.Fatal("a handwritten entry that matches nothing desired was nominated for adoption")
		}
	}
	if len(file.Orphans) != 0 {
		t.Fatalf("a still-desired entry must never be an orphan candidate, got %d", len(file.Orphans))
	}
}

// TestDevinRepairAdoptionRecordsOwnershipWithoutChangingBytes pins "`doctor
// --repair --yes` adopts exact current desired matches": adoption is a statement
// about the RECORD, so the user's file must come out byte-for-byte identical.
// spec: owned-entry-reconciliation/identical-values-are-offered-for-adoption
func TestDevinRepairAdoptionRecordsOwnershipWithoutChangingBytes(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	root := devinProjectRoot(repoRoot)
	selection := devinMcpSelection("managed")
	if _, err := Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("sync run: %v", err)
	}
	mcpPath := filepath.Join(root, harness.DevinMcpConfigFile)
	dropOwnershipState(t, root)
	before := readFileString(t, mcpPath)

	preview, err := DevinRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	file := repairFileFor(t, preview, harness.DevinMcpConfigFile)
	result, err := file.Adopt([]int{0})
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if result.Generation == 0 {
		t.Fatal("adoption did not commit an ownership generation")
	}
	if result.BackupPath == "" {
		t.Fatal("adoption did not take a safety backup")
	}
	if got := readFileString(t, mcpPath); got != before {
		t.Fatalf("adoption changed the aggregate bytes:\nbefore:\n%s\nafter:\n%s", before, got)
	}

	after, err := DevinRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatalf("second preview: %v", err)
	}
	if after.Actionable() {
		t.Fatal("the adopted entry is still nominated; adoption did not record ownership")
	}

	// A proven owned entry is now prunable, which is the whole point of
	// adopting it: deselecting the resource removes it.
	if _, err := Run(context.Background(), repoRoot, nil); err != nil {
		t.Fatalf("prune run: %v", err)
	}
	if got := readFileString(t, mcpPath); strings.Contains(got, "managed-binary") {
		t.Fatalf("adopted entry did not become prunable:\n%s", got)
	}
}

// TestDevinRepairOrphanRemovalRequiresConfirmationAndEvidence pins the two
// gates on deletion: "orphan-removal candidates require non-heuristic retained
// journal or safety-backup evidence" and "explicit per-entry confirmation and a
// fresh backup".
func TestDevinRepairOrphanRemovalRequiresConfirmationAndEvidence(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	root := devinProjectRoot(repoRoot)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	mcpPath := filepath.Join(root, harness.DevinMcpConfigFile)
	if err := os.WriteFile(mcpPath, []byte("{\n  \"mcpServers\": {\n    \"handwritten\": { \"command\": \"user-binary\" }\n  }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), repoRoot, devinMcpSelection("managed")); err != nil {
		t.Fatalf("sync run: %v", err)
	}
	dropOwnershipState(t, root)

	// Nothing is selected any more, so the entry sync wrote is undesired and
	// unproven — but a retained safety-backup manifest records that a prior
	// managed generation claimed exactly it.
	preview, err := DevinRepairPreview(context.Background(), repoRoot, nil)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	file := repairFileFor(t, preview, harness.DevinMcpConfigFile)
	if len(file.Orphans) != 1 {
		t.Fatalf("expected exactly one evidence-backed orphan candidate, got %d", len(file.Orphans))
	}
	orphan := file.Orphans[0]
	if orphan.Identity() != "managed" {
		t.Fatalf("unexpected orphan candidate %q", orphan.Identity())
	}
	if !strings.Contains(orphan.Evidence(), string(ownership.EvidenceSafetyBackup)) {
		t.Fatalf("orphan evidence does not name its retained source: %q", orphan.Evidence())
	}
	// The handwritten entry is undesired and unproven too, but no retained
	// record ever claimed it, so it is never nominated: "entries without such
	// evidence are never nominated; users must edit them manually".
	for _, candidate := range file.Orphans {
		if candidate.Identity() == "handwritten" {
			t.Fatal("a handwritten entry with no retained evidence was nominated for removal")
		}
	}

	before := readFileString(t, mcpPath)
	if _, err := file.RemoveOrphan(0, false); !errors.Is(err, ownership.ErrConfirmationRequired) {
		t.Fatalf("unconfirmed removal error = %v, want ownership.ErrConfirmationRequired", err)
	}
	if got := readFileString(t, mcpPath); got != before {
		t.Fatalf("an unconfirmed removal changed the file:\n%s", got)
	}

	result, err := file.RemoveOrphan(0, true)
	if err != nil {
		t.Fatalf("confirmed removal: %v", err)
	}
	if result.BackupPath == "" {
		t.Fatal("confirmed orphan removal did not take a fresh backup")
	}
	got := readFileString(t, mcpPath)
	if strings.Contains(got, "managed-binary") {
		t.Fatalf("confirmed orphan was not removed:\n%s", got)
	}
	if !strings.Contains(got, "user-binary") {
		t.Fatalf("the unrelated handwritten entry was removed:\n%s", got)
	}
}

// TestDevinRepairRefusesStaleCandidates pins that an approval is authorization
// for the entry the user was SHOWN, not for whatever occupies that identity by
// the time the action runs.
func TestDevinRepairRefusesStaleCandidates(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	root := devinProjectRoot(repoRoot)
	selection := devinMcpSelection("managed")
	if _, err := Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("sync run: %v", err)
	}
	mcpPath := filepath.Join(root, harness.DevinMcpConfigFile)
	dropOwnershipState(t, root)

	preview, err := DevinRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	file := repairFileFor(t, preview, harness.DevinMcpConfigFile)
	if len(file.Adoptions) != 1 {
		t.Fatalf("the fixture produced %d adoption candidates, want 1", len(file.Adoptions))
	}

	// Somebody edits the entry between preview and confirmation.
	edited := strings.ReplaceAll(readFileString(t, mcpPath), "managed-binary", "someone-elses-binary")
	if err := os.WriteFile(mcpPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Adopt([]int{0}); !errors.Is(err, ErrRepairCandidateStale) {
		t.Fatalf("stale adoption error = %v, want ErrRepairCandidateStale", err)
	}
	if got := readFileString(t, mcpPath); got != edited {
		t.Fatalf("a refused adoption changed the file:\n%s", got)
	}
}

// TestDevinRepairPreviewReportsCorruptAggregateWithoutTouchingIt pins the
// "corrupt state/file" input to the exit contract: the file is reported and
// left byte-for-byte as found.
func TestDevinRepairPreviewReportsCorruptAggregateWithoutTouchingIt(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	root := devinProjectRoot(repoRoot)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	broken := "{ \"mcpServers\": { \"a\": }\n"
	mcpPath := filepath.Join(root, harness.DevinMcpConfigFile)
	if err := os.WriteFile(mcpPath, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	preview, err := DevinRepairPreview(context.Background(), repoRoot, devinMcpSelection("managed"))
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if !preview.HasErrors() {
		t.Fatalf("expected an error diagnostic for a malformed aggregate, got %#v", preview.Diags)
	}
	if preview.Actionable() {
		t.Fatal("a malformed aggregate must offer no repair actions")
	}
	if got := readFileString(t, mcpPath); got != broken {
		t.Fatalf("preview modified a malformed aggregate:\n%s", got)
	}
}
