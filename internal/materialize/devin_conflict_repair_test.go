package materialize

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// conflictSecret is a recognizable non-credential stand-in for the kind of
// content an MCP entry carries. Every no-leak assertion below searches for it.
const conflictSecret = "NOT-A-REAL-SECRET-c0ffee"

// writeHandwrittenConflict reproduces the beta tester's starting state: a
// hand-written mcp_config.json holding an entry whose KEY equals a selected
// marketplace MCP server's key, but whose VALUE differs — and no ownership state
// for the root, because nothing ever wrote one.
func writeHandwrittenConflict(t *testing.T, repoRoot, key string) (mcpPath string, selection []resolver.ResolvedArtifact) {
	t.Helper()
	root := devinProjectRoot(repoRoot)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	mcpPath = filepath.Join(root, harness.DevinMcpConfigFile)
	body := "{\n  // hand-written by the user, months before agent-marketplace was installed\n  \"mcpServers\": {\n    \"" +
		key + "\": { \"command\": \"/usr/local/bin/atlassian-mcp\", \"env\": { \"TOKEN\": \"" + conflictSecret + "\" } },\n" +
		"    \"untouched\": { \"command\": \"leave-me-alone\" }\n  }\n}\n"
	if err := os.WriteFile(mcpPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return mcpPath, []resolver.ResolvedArtifact{
		devinResolved(key, ir.NewMcpServer(key, "generated-binary", nil, nil, nil, ir.SourceRef{}), false),
	}
}

// TestDevinUnmanagedValueConflictIsOfferedAsAThirdRepairClass reproduces the
// closed loop end to end and pins that it is now escapable.
//
// Before this class existed, this exact fixture produced adoptions=0,
// orphans=0, Actionable()==false: sync refused to write because it could not
// prove ownership, and `doctor --repair` said "nothing to repair".
// spec: owned-entry-reconciliation/differing-values-are-offered-for-replacement
func TestDevinUnmanagedValueConflictIsOfferedAsAThirdRepairClass(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	mcpPath, selection := writeHandwrittenConflict(t, repoRoot, "genai-atlassian")

	// Step 1: sync collides. The existing entry is preserved and the generated
	// entry fails, forever.
	report, err := Run(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatalf("sync run: %v", err)
	}
	if _, ok := findDiag(report.Diags, diag.CodeOwnershipUnmanagedKey); !ok {
		t.Fatalf("expected a %s collision, got %v", diag.CodeOwnershipUnmanagedKey, report.Diags)
	}
	collided := readFileString(t, mcpPath)
	if strings.Contains(collided, "generated-binary") {
		t.Fatalf("the generated entry won a collision it must lose:\n%s", collided)
	}

	// Step 2: the repair preview describes it — neither an adoption (the values
	// differ) nor an orphan (no retained evidence, and none can ever exist).
	preview, err := DevinRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if got := readFileString(t, mcpPath); got != collided {
		t.Fatalf("the preview mutated the aggregate:\n%s", got)
	}
	file := repairFileFor(t, preview, harness.DevinMcpConfigFile)
	if len(file.Adoptions) != 0 || len(file.Orphans) != 0 {
		t.Fatalf("adoptions=%d orphans=%d, want the conflict class to be the only nomination",
			len(file.Adoptions), len(file.Orphans))
	}
	if len(file.Conflicts) != 1 {
		t.Fatalf("conflicts = %d, want exactly the same-key different-value entry", len(file.Conflicts))
	}
	if !preview.Actionable() {
		t.Fatal("a conflict candidate must make the preview actionable")
	}
	conflict := file.Conflicts[0]
	if conflict.EntryKind() != string(ownership.KindMcpServer) || conflict.Identity() != "genai-atlassian" {
		t.Fatalf("unexpected conflict candidate %s %q", conflict.EntryKind(), conflict.Identity())
	}
	if conflict.Resource != "genai-atlassian" {
		t.Fatalf("conflict resource = %q", conflict.Resource)
	}
	if !conflict.Differs() {
		t.Fatal("the candidate must state that the existing and desired values differ")
	}
	if conflict.ExistingValueDigest() == conflict.DesiredValueDigest() {
		t.Fatal("a conflict candidate cannot carry two equal digests")
	}
	// The untouched hand-written entry matches nothing desired, so it is not
	// nominated for anything.
	for _, other := range file.Conflicts {
		if other.Identity() == "untouched" {
			t.Fatal("an entry that matches no desired key was nominated as a conflict")
		}
	}

	// Step 3: an unconfirmed replacement is refused, and the file is byte-identical.
	if _, err := file.ReplaceConflict(0, false); !errors.Is(err, ownership.ErrConfirmationRequired) {
		t.Fatalf("unconfirmed replace error = %v, want ownership.ErrConfirmationRequired", err)
	}
	if got := readFileString(t, mcpPath); got != collided {
		t.Fatalf("an unconfirmed replace changed the file:\n%s", got)
	}

	// Step 4: a confirmed replacement takes a fresh backup holding the user's
	// exact prior bytes, then replaces exactly that one entry.
	result, err := file.ReplaceConflict(0, true)
	if err != nil {
		t.Fatalf("confirmed replace: %v", err)
	}
	if result.BackupPath == "" {
		t.Fatal("a confirmed replace must take a fresh safety backup")
	}
	backup, err := os.ReadFile(result.BackupPath)
	if err != nil {
		t.Fatalf("read safety backup: %v", err)
	}
	if string(backup) != collided {
		t.Fatalf("the safety backup does not hold the pre-replacement bytes:\n%s", backup)
	}
	replaced := readFileString(t, mcpPath)
	if !strings.Contains(replaced, "generated-binary") {
		t.Fatalf("the generated entry was not installed:\n%s", replaced)
	}
	if strings.Contains(replaced, conflictSecret) {
		t.Fatalf("the replaced entry's value survived:\n%s", replaced)
	}
	if !strings.Contains(replaced, "leave-me-alone") {
		t.Fatalf("an unrelated hand-written entry was destroyed:\n%s", replaced)
	}
	if !strings.Contains(replaced, "hand-written by the user") {
		t.Fatalf("the hand-written comment was lost:\n%s", replaced)
	}

	// Step 5: the loop is open. A re-sync is clean — no collision, nothing to
	// repair — because the replaced entry is now proven owned.
	after, err := Run(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatalf("re-sync: %v", err)
	}
	if _, ok := findDiag(after.Diags, diag.CodeOwnershipUnmanagedKey); ok {
		t.Fatalf("the collision survived the replacement: %v", after.Diags)
	}
	requireNoErrorDiags(t, after.Diags)
	if got := readFileString(t, mcpPath); got != replaced {
		t.Fatalf("the clean re-sync rewrote the aggregate:\nbefore:\n%s\nafter:\n%s", replaced, got)
	}
	clean, err := DevinRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatalf("post-repair preview: %v", err)
	}
	if clean.Actionable() {
		t.Fatal("the replaced entry is still nominated; the replacement did not record ownership")
	}
}

// TestDevinConflictCandidateNeverExposesValues pins the reporting rule: the
// candidate may say the values DIFFER and carry digests, but nothing reachable
// from it may carry an env value, a header, a command string, or any other part
// of either value.
func TestDevinConflictCandidateNeverExposesValues(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	_, selection := writeHandwrittenConflict(t, repoRoot, "genai-atlassian")
	if _, err := Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("sync run: %v", err)
	}

	preview, err := DevinRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	file := repairFileFor(t, preview, harness.DevinMcpConfigFile)
	if len(file.Conflicts) != 1 {
		t.Fatalf("conflicts = %d, want 1", len(file.Conflicts))
	}
	conflict := file.Conflicts[0]

	reported := strings.Join([]string{
		conflict.EntryKind(), conflict.Identity(), conflict.Resource,
		conflict.ExistingValueDigest(), conflict.DesiredValueDigest(),
		conflict.Difference(), file.AbsFile(), file.Rel,
	}, "\n")
	for _, forbidden := range []string{
		conflictSecret,             // an MCP env value
		"/usr/local/bin/atlassian", // an existing command string
		"generated-binary",         // the generated command string
		"TOKEN",                    // an env field VALUE-side name from the user's file
		"leave-me-alone",
	} {
		if strings.Contains(reported, forbidden) {
			t.Fatalf("the conflict report leaked %q:\n%s", forbidden, reported)
		}
	}
	for _, required := range []string{
		string(ownership.KindMcpServer), "genai-atlassian", harness.DevinMcpConfigFile, "sha256:",
	} {
		if !strings.Contains(reported, required) {
			t.Fatalf("the conflict report does not carry %q, so a user cannot act on it:\n%s", required, reported)
		}
	}
	// Every preview diagnostic obeys the same rule.
	for _, d := range preview.Diags {
		if strings.Contains(d.Message+d.Hint, conflictSecret) {
			t.Fatalf("diagnostic %s leaked a value: %s", d.Code, d.Message)
		}
	}
}

// TestDevinConflictReplaceLogsNoValue closes the half of the no-leak rule the
// accessor test above cannot see: structured logs.
//
// The destructive path is the one that has both values in hand — the bytes it
// deletes and the bytes it writes — and it is also the one that logs most, at
// Info level, on a run the user asked for. An attribute added there would ship a
// user's MCP env value or OAuth secret to whatever sink the logging config
// points at, which no assertion on ConflictCandidate's accessors would catch.
func TestDevinConflictReplaceLogsNoValue(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	_, selection := writeHandwrittenConflict(t, repoRoot, "genai-atlassian")

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	if _, err := Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("sync run: %v", err)
	}
	preview, err := DevinRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	file := repairFileFor(t, preview, harness.DevinMcpConfigFile)
	if _, err := file.ReplaceConflict(0, false); err == nil {
		t.Fatal("an unconfirmed replace must be refused")
	}
	if _, err := file.ReplaceConflict(0, true); err != nil {
		t.Fatalf("confirmed replace: %v", err)
	}

	for _, forbidden := range []string{
		conflictSecret,             // the user's MCP env value
		"/usr/local/bin/atlassian", // the command string being deleted
		"generated-binary",         // the command string being written
		"leave-me-alone",           // an untouched sibling entry's value
	} {
		if strings.Contains(logs.String(), forbidden) {
			t.Fatalf("a log attribute leaked %q:\n%s", forbidden, logs.String())
		}
	}
	// The logs still have to be useful: the replacement must be traceable.
	for _, required := range []string{"devin repair replaced a conflicting unmanaged entry", "genai-atlassian", "sha256:"} {
		if !strings.Contains(logs.String(), required) {
			t.Fatalf("the replacement was not traceable in the logs (missing %q):\n%s", required, logs.String())
		}
	}
}

// modifyingCrasher simulates an external tool rewriting the aggregate while a
// ReplaceConflict is mid-Apply. It fires at CrashAfterStagedWrite, after the
// replacement bytes are staged but before the compare-and-swap.
type modifyingCrasher struct {
	path string
	from string
	to   string
}

func (c *modifyingCrasher) Crash(point ownership.CrashPoint) error {
	if point != ownership.CrashAfterStagedWrite {
		return nil
	}
	raw, err := os.ReadFile(c.path)
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, []byte(strings.ReplaceAll(string(raw), c.from, c.to)), 0o644)
}

// TestDevinConflictReplaceRefusesAGenerationConflictNotAStaleCandidate covers
// the case where the candidate itself is still valid but a different key in the
// aggregate changed after the repair read it. The failure is a compare-and-swap
// conflict (the aggregate changed underneath the mutation), not a stale
// candidate (the candidate itself did not change).
func TestDevinConflictReplaceRefusesAGenerationConflictNotAStaleCandidate(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	mcpPath, selection := writeHandwrittenConflict(t, repoRoot, "genai-atlassian")
	if _, err := Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("sync run: %v", err)
	}
	preview, err := DevinRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	file := repairFileFor(t, preview, harness.DevinMcpConfigFile)
	if len(file.Conflicts) != 1 {
		t.Fatalf("conflicts = %d, want 1", len(file.Conflicts))
	}

	// Replace the RepairFile's store with one that simulates a foreign writer
	// changing an unrelated key while the replacement is staged.
	crasher := &modifyingCrasher{
		path: mcpPath,
		from: "leave-me-alone",
		to:   "leave-me-alone-externally",
	}
	store, err := ownership.Open(file.OutDir, ownership.WithCrasher(crasher))
	if err != nil {
		t.Fatalf("open ownership store with crasher: %v", err)
	}
	file.store = store

	_, err = file.ReplaceConflict(0, true)
	if !errors.Is(err, ownership.ErrGenerationConflict) {
		t.Fatalf("replace error = %v, want ownership.ErrGenerationConflict", err)
	}
	if errors.Is(err, ErrRepairCandidateStale) {
		t.Fatalf("replace returned ErrRepairCandidateStale for an aggregate CAS failure")
	}

	got := readFileString(t, mcpPath)
	if !strings.Contains(got, "leave-me-alone-externally") {
		t.Fatalf("the external modification was overwritten:\n%s", got)
	}
	if !strings.Contains(got, "/usr/local/bin/atlassian-mcp") {
		t.Fatalf("the conflict candidate's existing value was changed:\n%s", got)
	}
	if strings.Contains(got, "generated-binary") {
		t.Fatalf("the replacement happened despite the CAS failure:\n%s", got)
	}
}

// TestDevinConflictReplaceRefusesStaleCandidates pins that an approval covers
// the entry the user was SHOWN. If either side of the difference changed, the
// replacement is refused and the file is left exactly as found.
func TestDevinConflictReplaceRefusesStaleCandidates(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	mcpPath, selection := writeHandwrittenConflict(t, repoRoot, "genai-atlassian")
	if _, err := Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("sync run: %v", err)
	}
	preview, err := DevinRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	file := repairFileFor(t, preview, harness.DevinMcpConfigFile)

	edited := strings.ReplaceAll(readFileString(t, mcpPath), conflictSecret, "SOMETHING-ELSE")
	if err := os.WriteFile(mcpPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := file.ReplaceConflict(0, true); !errors.Is(err, ErrRepairCandidateStale) {
		t.Fatalf("stale replace error = %v, want ErrRepairCandidateStale", err)
	}
	if got := readFileString(t, mcpPath); got != edited {
		t.Fatalf("a refused replace changed the file:\n%s", got)
	}
}
