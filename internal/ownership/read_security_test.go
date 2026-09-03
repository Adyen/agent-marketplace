package ownership

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

func TestOversizedStateRefusesMutation(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	st := f.store()
	mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent),
		Entries: []Entry{mcpEntry("generated", "generated-server", `{"command":"new"}`)}, Reason: ReasonSync})
	before, _ := f.readAggregate()
	if err := os.Truncate(st.StateFile(), maxOwnershipMetadataSize+1); err != nil {
		t.Fatal(err)
	}

	_, err := st.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(priorContent),
		Entries: nil, Reason: ReasonSync})
	if !errors.Is(err, ErrStateCorrupt) {
		t.Fatalf("Apply error = %v, want ErrStateCorrupt", err)
	}
	coded, ok := diag.AsCodedError(err)
	if !ok || coded.Code != diag.CodeOwnershipStateCorrupt {
		t.Fatalf("error = %v, want code %q", err, diag.CodeOwnershipStateCorrupt)
	}
	if got, _ := f.readAggregate(); got != before {
		t.Fatalf("aggregate changed after oversized state refusal: %q", got)
	}
}

func TestStateSymlinkIsNotFollowedDuringMutation(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	st := f.store()
	mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent),
		Entries: []Entry{mcpEntry("generated", "generated-server", `{"command":"new"}`)}, Reason: ReasonSync})
	stateBytes, err := os.ReadFile(st.StateFile())
	if err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(t.TempDir(), "state-victim.json")
	if err := os.WriteFile(victim, stateBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(st.StateFile()); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, st.StateFile()); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	before, _ := f.readAggregate()

	_, err = st.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(priorContent),
		Entries: nil, Reason: ReasonSync})
	if !errors.Is(err, ErrStateCorrupt) {
		t.Fatalf("Apply error = %v, want ErrStateCorrupt", err)
	}
	if got, _ := f.readAggregate(); got != before {
		t.Fatalf("aggregate changed after symlinked state refusal: %q", got)
	}
	if got, readErr := os.ReadFile(victim); readErr != nil || string(got) != string(stateBytes) {
		t.Fatalf("state symlink victim changed: %q, %v", got, readErr)
	}
}

func TestOversizedJournalRefusesRecoveryWithoutMutation(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	st := f.store(WithCrasher(CrashAt(CrashAfterJournalRenamed)))
	if _, err := st.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent),
		Entries: []Entry{mcpEntry("generated", "generated-server", `{"command":"new"}`)}, Reason: ReasonSync,
		ExpectPriorDigest: Digest([]byte(priorContent))}); !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("Apply error = %v, want injected crash", err)
	}
	before, _ := f.readAggregate()
	if err := os.Truncate(st.JournalFile(), maxOwnershipMetadataSize+1); err != nil {
		t.Fatal(err)
	}
	journalInfo, err := os.Stat(st.JournalFile())
	if err != nil {
		t.Fatal(err)
	}

	_, err = f.store().Recover()
	if !errors.Is(err, ErrStateCorrupt) {
		t.Fatalf("Recover error = %v, want ErrStateCorrupt", err)
	}
	if got, _ := f.readAggregate(); got != before {
		t.Fatalf("aggregate changed after oversized journal refusal: %q", got)
	}
	if info, statErr := os.Stat(st.JournalFile()); statErr != nil || info.Size() != journalInfo.Size() {
		t.Fatalf("oversized journal changed or disappeared: %v", statErr)
	}
}

func TestOversizedSafetyManifestIsNotEvidence(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	st := f.store()
	res := mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent),
		Entries: []Entry{mcpEntry("generated", "generated-server", `{"command":"new"}`)}, Reason: ReasonSync})
	before, _ := f.readAggregate()
	manifestPath := metaPathFor(res.BackupPath)
	if err := os.Truncate(manifestPath, maxOwnershipMetadataSize+1); err != nil {
		t.Fatal(err)
	}

	backups, err := st.SafetyBackups(f.rel)
	if err != nil {
		t.Fatalf("SafetyBackups: %v", err)
	}
	if len(backups) != 0 {
		t.Fatalf("oversized manifest yielded %d evidence records", len(backups))
	}
	if got, _ := f.readAggregate(); got != before {
		t.Fatalf("aggregate changed while reading oversized manifest: %q", got)
	}
}
