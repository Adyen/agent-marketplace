package ownership

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/backup"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// Safety backups are MANDATORY: "Safety backups are mandatory even when
// configured backups are disabled."
//
// The mechanism is structural, not conditional: internal/backup's opt-out lives
// in backup.Config, which governs only the timestamped tar.gz archives that
// package writes under ~/.agent-marketplace/backups. The ownership store has no
// Config parameter and no Option that can switch backups off, so there is no
// code path in which a mutation is applied without one. This test proves it by
// running with the opt-out fully engaged.
func TestSafetyBackupIsMandatoryEvenWhenConfiguredBackupsAreDisabled(t *testing.T) {
	disabled := backup.Config{Enabled: false, MaxDays: 0, MaxPerDay: 0}
	if disabled.Enabled {
		t.Fatal("the test did not actually disable configured backups")
	}
	// Engage the opt-out exactly as the sync pipeline would, then assert it wrote
	// no archive and that the ownership store still backs the file up.
	if err := backup.Rotate(disabled); err != nil {
		t.Fatalf("backup.Rotate: %v", err)
	}
	archives, err := backup.ListBackups()
	if err != nil {
		t.Fatalf("backup.ListBackups: %v", err)
	}
	if len(archives) != 0 {
		t.Fatalf("configured backups produced %d archive sets while disabled", len(archives))
	}

	f := newFixture(t)
	f.writeAggregate(priorContent)
	st := f.store()
	res := mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent),
		Entries: []Entry{mcpEntry("generated", "generated-server", `{"command":"new"}`)}, Reason: ReasonSync})

	if res.BackupPath == "" {
		t.Fatal("Apply reported no safety backup path")
	}
	raw, err := os.ReadFile(res.BackupPath)
	if err != nil {
		t.Fatalf("read safety backup: %v", err)
	}
	// RAW BYTES: the exact original bytes, comments and all, not a
	// re-serialization.
	if string(raw) != priorContent {
		t.Fatalf("safety backup = %q, want the exact pre-mutation bytes %q", raw, priorContent)
	}
	if res.PriorDigest != Digest([]byte(priorContent)) {
		t.Fatalf("prior digest = %q, want the digest of the pre-mutation bytes", res.PriorDigest)
	}
	// It lives under the XDG state root, not under the repository or the cache.
	if !strings.HasPrefix(res.BackupPath, filepath.Join(st.StateRoot(), safetyDirName)) {
		t.Fatalf("safety backup %q is not under the XDG state safety-backups root", res.BackupPath)
	}
}

// "Retain the five newest backups per canonical output file."
func TestSafetyBackupRetainsExactlyFiveVersionsPerFile(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{\n  \"mcpServers\": {}\n}\n")
	st := f.store()

	var wantNewest []string
	for i := 1; i <= 8; i++ {
		content := fmt.Sprintf("{\n  \"mcpServers\": {\"s%d\": {}}\n}\n", i)
		mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(content),
			Entries: []Entry{mcpEntry(fmt.Sprintf("s%d", i), "generated-server", "{}")}, Reason: ReasonSync})
		wantNewest = append(wantNewest, content)
	}

	backups, err := st.SafetyBackups(f.rel)
	if err != nil {
		t.Fatalf("SafetyBackups: %v", err)
	}
	if len(backups) != SafetyBackupRetention {
		t.Fatalf("retained %d safety backups, want %d", len(backups), SafetyBackupRetention)
	}
	// The retained set is the NEWEST five, oldest first. Generation N's backup
	// holds generation N-1's bytes, so generations 4..8 retain contents 3..7.
	for i, b := range backups {
		wantContent := wantNewest[len(wantNewest)-SafetyBackupRetention+i-1]
		raw, err := b.RawBytes()
		if err != nil {
			t.Fatalf("RawBytes: %v", err)
		}
		if string(raw) != wantContent {
			t.Fatalf("retained backup[%d] = %q, want %q", i, raw, wantContent)
		}
		if b.Manifest.Generation != uint64(4+i) {
			t.Fatalf("retained backup[%d] generation = %d, want %d", i, b.Manifest.Generation, 4+i)
		}
	}

	// Bytes and manifests are pruned as a pair: an orphaned manifest would be
	// evidence with no bytes behind it.
	dir := st.SafetyBackupDir(f.rel)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	baks, metas := 0, 0
	for _, e := range entries {
		switch {
		case strings.HasSuffix(e.Name(), metaSuffix):
			metas++
		case strings.HasSuffix(e.Name(), backupSuffix):
			baks++
		default:
			t.Fatalf("unexpected file in the safety backup directory: %s", e.Name())
		}
	}
	if baks != SafetyBackupRetention || metas != SafetyBackupRetention {
		t.Fatalf("backup/manifest counts = %d/%d, want %d/%d", baks, metas, SafetyBackupRetention, SafetyBackupRetention)
	}
}

// Retention is per canonical output file, not per root.
func TestSafetyBackupRetentionIsPerCanonicalOutputFile(t *testing.T) {
	f := newFixture(t)
	st := f.store()
	other := "hooks.v1.json"
	otherAbs := filepath.Join(f.targetRoot, other)

	for i := 0; i < 7; i++ {
		mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile,
			NextBytes: []byte(fmt.Sprintf(`{"mcpServers":{"s%d":{}}}`, i)),
			Entries:   []Entry{mcpEntry(fmt.Sprintf("s%d", i), "r", "{}")}, Reason: ReasonSync})
	}
	mustApply(t, st, Mutation{Rel: other, AbsFile: otherAbs, NextBytes: []byte(`{"PreToolUse":[]}`),
		Entries: []Entry{hookEntry("PreToolUse", "guard", "{}")}, Reason: ReasonSync})

	mcpBackups, err := st.SafetyBackups(f.rel)
	if err != nil {
		t.Fatal(err)
	}
	hookBackups, err := st.SafetyBackups(other)
	if err != nil {
		t.Fatal(err)
	}
	if len(mcpBackups) != SafetyBackupRetention {
		t.Fatalf("mcp backups = %d, want %d", len(mcpBackups), SafetyBackupRetention)
	}
	if len(hookBackups) != 1 {
		t.Fatalf("hook backups = %d, want 1: retention must not be shared between files", len(hookBackups))
	}
	if st.SafetyBackupDir(f.rel) == st.SafetyBackupDir(other) {
		t.Fatal("two canonical output files share one backup directory")
	}
}

// A failed mandatory backup REFUSES the mutation rather than proceeding.
func TestApplyRefusesWhenTheMandatoryBackupCannotBeWritten(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	f := newFixture(t)
	f.writeAggregate(priorContent)
	st := f.store()

	// Make the safety-backups root unwritable.
	safetyRoot := filepath.Join(st.StateRoot(), safetyDirName)
	if err := os.MkdirAll(safetyRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(safetyRoot, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(safetyRoot, 0o700) })

	_, err := st.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent),
		Entries: []Entry{mcpEntry("generated", "g", `{"command":"new"}`)}, Reason: ReasonSync})
	coded, ok := diag.AsCodedError(err)
	if !ok {
		t.Fatalf("Apply error %T is not a *diag.CodedError: %v", err, err)
	}
	if coded.Code != diag.CodeOwnershipSafetyBackup {
		t.Fatalf("code = %q, want %q", coded.Code, diag.CodeOwnershipSafetyBackup)
	}
	if coded.Severity != diag.SeverityError {
		t.Fatalf("severity = %v, want error", coded.Severity)
	}
	if coded.Source.File != f.rel {
		t.Fatalf("source file = %q, want %q", coded.Source.File, f.rel)
	}
	if got, _ := f.readAggregate(); got != priorContent {
		t.Fatalf("aggregate = %q, want it untouched when the mandatory backup failed", got)
	}
	assertGeneration(t, st, 0)
	assertJournalPhase(t, st, "")
}

// A malformed aggregate is backed up as-is: "Malformed JSON is safety-backed up
// before parsing, left unchanged". The store never parses the prior bytes, so
// this is true by construction; the test pins it.
func TestSafetyBackupPreservesMalformedBytesVerbatim(t *testing.T) {
	f := newFixture(t)
	const malformed = "{\n  \"mcpServers\": { // truncated on purpose\n"
	f.writeAggregate(malformed)
	st := f.store()
	res := mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(`{"mcpServers":{}}`),
		Entries: nil, Reason: ReasonSync})
	raw, err := os.ReadFile(res.BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != malformed {
		t.Fatalf("backup = %q, want the malformed bytes verbatim %q", raw, malformed)
	}
}

// A manifest whose bytes are gone, or whose provenance is unreadable, is not
// evidence and must be ignored rather than trusted.
func TestSafetyBackupsSkipsRecordsWithoutUsableProvenance(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	st := f.store()
	res := mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent),
		Entries: []Entry{mcpEntry("generated", "g", `{"command":"new"}`)}, Reason: ReasonSync})

	if err := os.Remove(metaPathFor(res.BackupPath)); err != nil {
		t.Fatal(err)
	}
	backups, err := st.SafetyBackups(f.rel)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 0 {
		t.Fatalf("SafetyBackups returned %d records with no manifest, want 0", len(backups))
	}
}

func TestSafetyBackupsForAnUntouchedFileIsEmpty(t *testing.T) {
	f := newFixture(t)
	backups, err := f.store().SafetyBackups("never-written.json")
	if err != nil {
		t.Fatalf("SafetyBackups: %v", err)
	}
	if len(backups) != 0 {
		t.Fatalf("backups = %d, want 0", len(backups))
	}
}

// Orphan removal needs BOTH explicit confirmation and a fresh backup.
func TestOrphanRemovalRequiresConfirmationAndTakesAFreshBackup(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	st := f.store()

	_, err := st.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonOrphanRemoval})
	if !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("unconfirmed orphan removal error = %v, want ErrConfirmationRequired", err)
	}
	if got, _ := f.readAggregate(); got != priorContent {
		t.Fatalf("aggregate = %q, want it untouched by a refused removal", got)
	}
	if backups, _ := st.SafetyBackups(f.rel); len(backups) != 0 {
		t.Fatalf("a refused removal wrote %d backups, want 0", len(backups))
	}

	res := mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent),
		Reason: ReasonOrphanRemoval, Confirmed: true})
	backups, err := st.SafetyBackups(f.rel)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("confirmed removal produced %d backups, want 1 fresh backup", len(backups))
	}
	if backups[0].Manifest.Reason != ReasonOrphanRemoval {
		t.Fatalf("manifest reason = %q, want %q", backups[0].Manifest.Reason, ReasonOrphanRemoval)
	}
	raw, err := backups[0].RawBytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != priorContent {
		t.Fatalf("fresh backup = %q, want the bytes present before the removal", raw)
	}
	if res.Generation != 1 {
		t.Fatalf("generation = %d, want 1", res.Generation)
	}
}
