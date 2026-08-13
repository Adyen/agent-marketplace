package ownership

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// The nested-mutation guard: the one hazard the per-root advisory lock
// STRUCTURALLY cannot cover.
//
// Acquisition is reentrant per Store, which is what lets a caller widen one
// section over Recover plus N x Apply. The cost of that reentrancy is that a
// second mutation started on the SAME Store while the first one's generation is
// half written gets a depth bump instead of exclusion — and the journal is one
// slot per canonical root while Record.Generation is one counter for all of the
// root's files, so the interleaving corrupts state THROUGH the lock.
//
// Measured before the guard existed, with a nested Apply on a second aggregate
// file of one root: the inner generation published its file and committed its
// record, then the outer commitRecord — built from a Load that predated the inner
// commit — overwrote it, leaving the inner file on disk with NO ownership record
// at all. That file then classifies unmanaged forever, which is a file
// agent-marketplace wrote and can no longer prune or update.
//
// Nothing in this repository nests mutations, so this is a latent hazard rather
// than a live defect; the guard makes it a diagnosable error instead of silent
// corruption if a future caller ever does.
//
// The neighbouring case — a nested SECOND Store on the same root — is the
// opposite failure and is tested here too: two Stores are two open file
// descriptions and therefore genuinely contend, so that one self-deadlocks. It
// must stay BOUNDED (a timeout and a refusal) rather than becoming a hang.

// reentrantCrasher calls back into the Store from inside its own critical
// section, exactly once.
type reentrantCrasher struct {
	point CrashPoint
	fired bool
	inner func() error
	err   error
}

func (c *reentrantCrasher) Crash(point CrashPoint) error {
	if c.fired || point != c.point {
		return nil
	}
	c.fired = true
	c.err = c.inner()
	return nil
}

// TestAdversarialASecondStoreInsideAHeldSectionIsBoundedNotAWedge is the
// deadlock attack the design's reentrancy note invites.
//
// Reentrancy is per STORE. A body that opens a SECOND Store on the same root and
// writes therefore contends with the section it is running inside — flock lives
// on the open file description, so two Stores in one process are two
// descriptions and genuinely exclude each other. The question is whether that
// self-contention is BOUNDED (a timeout, a refusal, and a completed outer
// generation) or a hang.
//
// It must be bounded, and the outer mutation must still complete correctly, so a
// future caller that accidentally nests two Stores gets a diagnosable
// ownership.lock-timeout rather than a sync that never returns.
//
// No production path does this today: internal/materialize opens exactly one
// Store per canonical root (devin.go:429, devin_repair.go:283, memoized in a
// local) and internal/ownership never opens a Store from inside itself. This test
// is the guard for that staying true.
func TestAdversarialASecondStoreInsideAHeldSectionIsBoundedNotAWedge(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{}\n")

	crasher := &reentrantCrasher{point: CrashAfterJournalPrepared}
	outer := f.store(WithCrasher(crasher))
	crasher.inner = func() error {
		// A second Store on the same root, with a fail-fast timeout so the test
		// cannot hang even if the lock were broken.
		nested := f.store(WithLockTimeout(0))
		_, err := applyAddingKey(nested, "hooks.v1.json", filepath.Join(f.targetRoot, "hooks.v1.json"), "nested")
		return err
	}

	done := make(chan error, 1)
	go func() {
		_, err := applyAddingKey(outer, f.rel, f.absFile, "alpha")
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the outer generation did not complete: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("a nested second Store wedged the outer mutation: this is a deadlock, not a bounded refusal")
	}

	if !errors.Is(crasher.err, ErrLockTimeout) {
		t.Fatalf("a second Store inside a held section got %v, want a bounded ErrLockTimeout", crasher.err)
	}
	// The nested attempt wrote nothing, and the outer generation is intact.
	if fileExists(t, filepath.Join(f.targetRoot, "hooks.v1.json")) {
		t.Fatal("the refused nested mutation created its aggregate")
	}
	if err := recordDescribesFile(f.store(), f.rel, f.absFile); err != nil {
		t.Fatalf("after a refused nested mutation: %v", err)
	}
}

// TestAdversarialANestedApplyOnOneStoreIsRefusedRatherThanReentered is the
// reentrancy attack, and it is the one place where "the lock is held" is NOT
// enough on its own.
//
// Reentrancy is per Store, so a body that calls Apply from INSIDE Apply on the
// same Store gets a depth bump, not exclusion — and unlike Recover-then-Apply,
// which is sequential, a nested Apply runs while the outer generation is
// half-written. The journal is one slot per root and Record.Generation is one
// counter, so the interleaving corrupts state THROUGH the lock: the inner
// generation writes and clears the journal the outer one is relying on, then the
// outer's commitRecord — computed from a Load that predates the inner commit —
// overwrites the inner generation's ownership entirely.
//
// No caller does this today, and the lock cannot stop it, so the refusal is a
// separate guard: Apply refuses to nest on one Store. That makes the failure a
// diagnosable error instead of a silently dropped file ownership, and it costs
// one boolean.
func TestAdversarialANestedApplyOnOneStoreIsRefusedRatherThanReentered(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{}\n")

	crasher := &reentrantCrasher{point: CrashAfterJournalPrepared}
	st := f.store(WithCrasher(crasher))
	nestedRel := "hooks.v1.json"
	nestedAbs := filepath.Join(f.targetRoot, nestedRel)
	crasher.inner = func() error {
		// The SAME Store, so the acquisition reenters and provides no exclusion.
		_, err := applyAddingKey(st, nestedRel, nestedAbs, "nested")
		return err
	}

	if _, err := applyAddingKey(st, f.rel, f.absFile, "alpha"); err != nil {
		t.Fatalf("outer Apply: %v", err)
	}
	if !errors.Is(crasher.err, ErrNestedMutation) {
		t.Fatalf("a nested Apply on one Store returned %v, want ErrNestedMutation.\n"+
			"Reentrancy makes the lock a no-op here, so without this guard the inner generation writes and clears "+
			"the journal the outer one depends on and the outer commit drops the inner file's ownership.", crasher.err)
	}
	// The refused nested mutation wrote nothing, and the outer generation is
	// whole: one file, one generation, a record that describes it.
	if fileExists(t, nestedAbs) {
		t.Fatal("the refused nested mutation created its aggregate")
	}
	if err := recordDescribesFile(f.store(), f.rel, f.absFile); err != nil {
		t.Fatalf("after a refused nested mutation: %v", err)
	}
	rec, err := f.store().Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Files) != 1 {
		t.Fatalf("record covers %d files, want only the outer one: %+v", len(rec.Files), rec.Files)
	}
	if st.HoldsLock() {
		t.Fatal("the outer mutation did not release")
	}
}

// TestAdversarialNestedRecoverAndBackupOnlyAreAlsoRefused covers the other two
// writers reachable from inside a mutation. Recover resolves the very journal the
// outer generation is using, and BackupOnly prunes the retention window the outer
// generation just wrote into, so both are the same hazard as a nested Apply.
func TestAdversarialNestedRecoverAndBackupOnlyAreAlsoRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(st *Store, f *fixture) error
	}{
		{"Recover", func(st *Store, f *fixture) error {
			_, err := st.Recover()
			return err
		}},
		{"BackupOnly", func(st *Store, f *fixture) error {
			_, err := st.BackupOnly(f.rel, f.absFile, ReasonSync)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.writeAggregate("{}\n")
			crasher := &reentrantCrasher{point: CrashAfterJournalPrepared}
			st := f.store(WithCrasher(crasher))
			crasher.inner = func() error { return tc.call(st, f) }

			if _, err := applyAddingKey(st, f.rel, f.absFile, "alpha"); err != nil {
				t.Fatalf("outer Apply: %v", err)
			}
			if !errors.Is(crasher.err, ErrNestedMutation) {
				t.Fatalf("a nested %s returned %v, want ErrNestedMutation", tc.name, crasher.err)
			}
			if err := recordDescribesFile(f.store(), f.rel, f.absFile); err != nil {
				t.Fatalf("after a refused nested %s: %v", tc.name, err)
			}
		})
	}
}
