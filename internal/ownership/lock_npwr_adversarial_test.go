package ownership

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// Adversarial review of bead agent-marketplace-npwr (B4), degrade-path half.
//
// B4's three repair mutators run inside [Store.WithWriteSession], which on a
// filesystem with no advisory locking (GOOS=windows unconditionally, an
// NFS-mounted state root) PROCEEDS UNSERIALIZED while deleting user-written
// content. The standards reviewer recommended keeping that, arguing the
// destructive path carries protections the lock is not part of: the per-entry
// confirmation, Confirmed being refused before the backup, the mandatory raw-byte
// backup, and the caller's both-digest re-derivation.
//
// This file tests that argument instead of trusting it. Everything below runs
// with tryLock forced to report [ErrLockUnsupported], which is byte-for-byte what
// filelock_other.go does on a non-unix build.

// degradedStore is a store whose acquisition always reports ErrLockUnsupported,
// i.e. a root on a filesystem that refuses advisory locks.
func degradedStore(f *fixture, opts ...Option) *Store {
	st := f.store(opts...)
	st.tryLock = func(*os.File) error { return ErrLockUnsupported }
	return st
}

// TestAdversarialDegradedRootStillRefusesAnUnconfirmedDestructiveMutation is the
// first of the three claims the degrade decision rests on.
//
// If the confirmation gate lived anywhere behind the lock, an unserialized root
// would be a root on which `doctor --repair` deletes without consent. It does
// not: validateMutation runs inside applyLocked and is reached identically on the
// degraded path, and it is reached BEFORE the safety backup, so nothing at all is
// written — not the aggregate, not a backup, not a journal.
func TestAdversarialDegradedRootStillRefusesAnUnconfirmedDestructiveMutation(t *testing.T) {
	for _, reason := range []Reason{ReasonOrphanRemoval, ReasonConflictReplace} {
		t.Run(string(reason), func(t *testing.T) {
			f := newFixture(t)
			f.writeAggregate(`{"mcpServers":{"hand":{"command":"/hand/written"}}}`)
			st := degradedStore(f)

			if _, err := st.Apply(Mutation{
				Rel:       f.rel,
				AbsFile:   f.absFile,
				NextBytes: []byte(`{"mcpServers":{}}`),
				Reason:    reason,
			}); !errors.Is(err, ErrConfirmationRequired) {
				t.Fatalf("an unconfirmed %s on an UNSERIALIZED root: err = %v, want ErrConfirmationRequired.\n"+
					"If the degrade path could bypass the confirmation, proceeding unserialized while "+
					"deleting user content would be indefensible.", reason, err)
			}
			if got, _ := f.readAggregate(); got != `{"mcpServers":{"hand":{"command":"/hand/written"}}}` {
				t.Fatalf("a refused destructive mutation changed the aggregate: %s", got)
			}
			if fileExists(t, st.JournalFile()) {
				t.Fatal("a refused destructive mutation wrote a journal")
			}
			backups, err := st.SafetyBackups(f.rel)
			if err != nil {
				t.Fatal(err)
			}
			if len(backups) != 0 {
				t.Fatalf("a refused destructive mutation took %d safety backups; the gate must sit "+
					"before the backup step", len(backups))
			}
		})
	}
}

// TestAdversarialDegradedRootBackupHoldsThePreMutationBytes is the second claim:
// on the unserialized path the mandatory backup must still be the user's exact
// prior bytes, because it is the ONLY thing that makes an unserialized
// destructive write recoverable.
//
// The bytes are compared exactly, not by digest, because a backup that recorded a
// digest of the right bytes and the content of the wrong ones would satisfy every
// digest assertion and be worthless to the human who has to restore it.
func TestAdversarialDegradedRootBackupHoldsThePreMutationBytes(t *testing.T) {
	f := newFixture(t)
	handWritten := "{\n  // a comment the user wrote\n  \"mcpServers\": {\"hand\": {\"command\": \"" + fakeCommand + "\"}}\n}\n"
	f.writeAggregate(handWritten)
	st := degradedStore(f)

	proof, _ := st.Proof(f.rel)
	res, err := st.Apply(Mutation{
		Rel:                   f.rel,
		AbsFile:               f.absFile,
		NextBytes:             []byte(`{"mcpServers":{}}`),
		Reason:                ReasonConflictReplace,
		Confirmed:             true,
		ExpectPriorGeneration: proof.Generation(),
		ExpectPriorDigest:     Digest([]byte(handWritten)),
	})
	if err != nil {
		t.Fatalf("a confirmed replacement on a degraded root must still work: %v", err)
	}
	if res.BackupPath == "" {
		t.Fatal("a destructive mutation on a degraded root reported no mandatory backup path")
	}
	got, err := readFile(res.BackupPath)
	if err != nil {
		t.Fatalf("read the mandatory backup: %v", err)
	}
	if got != handWritten {
		t.Fatalf("the mandatory backup on an UNSERIALIZED root does not hold the pre-mutation bytes.\n"+
			"That backup is the only thing that makes the degrade decision defensible.\n"+
			"--- want ---\n%s\n--- got ---\n%s", handWritten, got)
	}
	if after, _ := f.readAggregate(); after == handWritten {
		t.Fatal("the confirmed replacement did not happen, so the backup assertion proved nothing")
	}
}

// TestAdversarialDegradedRootReportsTheDegradationAndRunsTheBodyOnce is the third
// property: the degradation must be VISIBLE and the body must run exactly once.
//
// Running it twice would apply a destructive mutation twice — the one outcome the
// degrade path must not add on top of losing serialization.
func TestAdversarialDegradedRootReportsTheDegradationAndRunsTheBodyOnce(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(`{"mcpServers":{}}`)
	var events []LockEvent
	var mu sync.Mutex
	st := degradedStore(f, WithLockObserver(func(ev LockEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, ev)
	}))

	runs := 0
	if err := st.WithWriteSession("repair-remove-orphan", func() error {
		runs++
		return nil
	}); err != nil {
		t.Fatalf("a session on a degraded root must proceed, not refuse: %v", err)
	}
	if runs != 1 {
		t.Fatalf("the repair section body ran %d times on the degraded path, want exactly 1", runs)
	}
	mu.Lock()
	defer mu.Unlock()
	unsupported := 0
	for _, ev := range events {
		if ev.Kind == LockUnsupported {
			unsupported++
		}
		if ev.Kind == LockAcquired {
			t.Fatalf("a degraded root reported LockAcquired; nothing is serialized there")
		}
	}
	if unsupported == 0 {
		t.Fatalf("the degradation was silent: no LockUnsupported event out of %d", len(events))
	}
}

// TestAdversarialTwoDegradedRepairsLeaveTheUserBytesRecoverable is the fourth and
// most important claim: on an unserialized root a concurrent writer must not be
// able to make the outcome UNRECOVERABLE.
//
// Two Stores for one root, both degraded, both committing a confirmed destructive
// mutation over the same aggregate at once. Losing an update is the documented
// cost of the degradation. What must never happen is that neither retained backup
// holds the user's original bytes — because then a lost update is permanent, and
// the degrade decision would be wrong for a destructive path.
func TestAdversarialTwoDegradedRepairsLeaveTheUserBytesRecoverable(t *testing.T) {
	f := newFixture(t)
	handWritten := "{\n  \"mcpServers\": {\"hand\": {\"command\": \"/hand/written\"}}\n}\n"
	f.writeAggregate(handWritten)

	// The real clock, not the fixture's counter: nextClock mutates one shared
	// field and two racing Applies would trip the race detector on the TEST
	// helper rather than on anything under test.
	first := degradedStore(f, WithClock(time.Now))
	second := degradedStore(f, WithClock(time.Now))

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	for _, pair := range []struct {
		st    *Store
		bytes string
	}{{first, `{"mcpServers":{"a":{"command":"/gen/a"}}}`}, {second, `{"mcpServers":{"b":{"command":"/gen/b"}}}`}} {
		go func(st *Store, next string) {
			defer wg.Done()
			<-start
			// An unserialized loser is allowed to fail; it is not allowed to make
			// the user's bytes unrecoverable.
			proof, _ := st.Proof(f.rel)
			gen := proof.Generation()
			priorDigest := ""
			if raw, err := os.ReadFile(f.absFile); err == nil {
				priorDigest = Digest(raw)
			}
			_, _ = st.Apply(Mutation{
				Rel:                   f.rel,
				AbsFile:               f.absFile,
				NextBytes:             []byte(next),
				Reason:                ReasonConflictReplace,
				Confirmed:             true,
				ExpectPriorGeneration: gen,
				ExpectPriorDigest:     priorDigest,
			})
		}(pair.st, pair.bytes)
	}
	close(start)
	wg.Wait()

	backups, err := first.SafetyBackups(f.rel)
	if err != nil {
		t.Fatalf("list the retained safety backups: %v", err)
	}
	for _, b := range backups {
		got, readErr := readFile(b.Path)
		if readErr != nil {
			t.Fatalf("read backup %s: %v", b.Path, readErr)
		}
		if got == handWritten {
			return
		}
	}
	var names []string
	for _, b := range backups {
		names = append(names, b.Path)
	}
	t.Fatalf("two concurrent confirmed replacements on an UNSERIALIZED root left the user's original "+
		"bytes in NO retained safety backup, so the lost update is permanent. That makes the "+
		"degrade-and-proceed decision wrong for a destructive repair.\nretained backups: %s",
		strings.Join(names, ", "))
}
