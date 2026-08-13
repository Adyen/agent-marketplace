package ownership

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// This file is the ADVERSARIAL suite for bead agent-marketplace-wrp8 (B3) at the
// ownership level. B3 widened the sync critical section, and it did so by adding
// two things to this package that the earlier beads did not have: a
// CONTEXT-carrying session ([Store.WithWriteSessionContext]) and an ENV VAR that
// overrides the lock timeout on every command.
//
// The env var is the higher-risk of the two, which is why most of this file is
// about it. A lock timeout is the one knob that can turn serialization off, and
// an environment variable is the easiest thing in the world for a CI harness, a
// Dockerfile, or a shell profile to set carelessly and then forget. So the bar is
// not "the documented values behave as documented"; it is "NO value, however
// malformed, hostile, or extreme, can make a mutation proceed WITHOUT
// serialization while still writing".

// ---------------------------------------------------------------------------
// 1. The env var cannot de-serialize a writer
// ---------------------------------------------------------------------------

// TestAdversarialNoLockTimeoutEnvValueCanDisableSerialization is the highest-value
// configuration attack in this review.
//
// For every value a user could plausibly set, exactly one of three things must
// happen, and the third is the one that must never happen:
//
//  1. Open REJECTS it, so the misconfiguration is loud on the first command; or
//  2. the value is accepted and a CONTENDED writer is REFUSED (it writes
//     nothing, and reports ErrLockTimeout); or
//  3. the writer proceeds and mutates while another process holds the lock —
//     which is silent de-serialization, i.e. the P0 reinstated by an env var.
//
// The holder is a second Store, which is a second open file description and
// therefore contends in the kernel exactly as another process would. Every case
// asserts the aggregate is byte-identical afterwards, because "refused" and
// "wrote something anyway" are the whole difference here.
func TestAdversarialNoLockTimeoutEnvValueCanDisableSerialization(t *testing.T) {
	const original = "{\"handwritten\":\"keep me\"}\n"
	for _, tc := range []struct {
		name string
		// value is what the user exported. unset is spelled separately because
		// "" is itself one of the interesting values.
		value string
		// rejected means Open must fail rather than accept the value.
		rejected bool
	}{
		{name: "zero fails fast", value: "0"},
		{name: "zero with a unit fails fast", value: "0s"},
		{name: "zero with a sign fails fast", value: "+0"},
		{name: "negative zero is still zero, not negative", value: "-0"},
		{name: "empty is not configured, so the default applies", value: ""},
		{name: "whitespace is not configured, so the default applies", value: "   "},
		{name: "a tab is not configured either", value: "\t"},
		{name: "surrounding whitespace is trimmed, not rejected", value: "  10ms  "},
		{name: "a bare number has no unit", value: "30", rejected: true},
		{name: "a negative duration is rejected", value: "-1s", rejected: true},
		{name: "a huge negative duration is rejected", value: "-2562047h", rejected: true},
		{name: "prose is rejected", value: "forever", rejected: true},
		{name: "an empty-looking unit is rejected", value: "s", rejected: true},
		{name: "an overflowing duration is rejected by ParseDuration", value: "9999999999h", rejected: true},
		{name: "a case variant of a unit is rejected", value: "10S", rejected: true},
		// The largest duration time.ParseDuration will return. It is ACCEPTED,
		// which means it is an effectively unbounded wait — about 292 years —
		// and time.Time.Add and time.Time.Sub both saturate rather than wrapping
		// to a negative remaining time, so it cannot degenerate into a fail-fast
		// by accident either. It is refused here only because the caller's
		// context is cancelled, which is exactly the point: a user who raises the
		// knob to absurdity gets a wait they can Ctrl-C out of, never an
		// unserialized write.
		//
		// A NUL-padded value is deliberately NOT in this table: the operating
		// system refuses to put a NUL in an environment value at all (setenv
		// returns EINVAL), so the case is unreachable rather than untested.
		{name: "the largest representable duration is accepted", value: "2562047h47m16s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.writeAggregate(original)
			t.Setenv(LockTimeoutEnvVar, tc.value)

			// A tiny in-process configuration, so that a value which is
			// SILENTLY IGNORED would show up as "the writer succeeded"
			// rather than as a 30s hang: the option asks to fail fast, and
			// only an accepted env value may override it.
			st, err := Open(f.targetRoot, WithStateRoot(f.stateRoot), WithClock(f.nextClock), WithLockTimeout(0))
			if tc.rejected {
				if !errors.Is(err, ErrLockTimeoutConfig) {
					t.Fatalf("Open with %s=%q = %v, want ErrLockTimeoutConfig: a value that does not "+
						"parse must fail loudly, never fall back to a default the user did not ask for",
						LockTimeoutEnvVar, tc.value, err)
				}
				if st != nil {
					t.Fatal("Open returned a Store together with a configuration error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Open with %s=%q: %v", LockTimeoutEnvVar, tc.value, err)
			}

			// A second open file description holds the lock, i.e. the
			// other terminal.
			holder := f.store()
			release, err := holder.Lock()
			if err != nil {
				t.Fatalf("holder Lock: %v", err)
			}
			defer release()

			// Every accepted value in this table is either 0 or a few
			// milliseconds or enormous. The enormous ones would block for
			// their whole timeout, so the writer gets a context that is
			// cancelled: a cancelled wait and an expired wait are both
			// REFUSALS, and refusal is the property under test.
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			applyErr := st.WithWriteSessionContext(ctx, "adversarial", func() error {
				_, err := applyAddingKey(st, f.rel, f.absFile, "alpha")
				return err
			})
			if applyErr == nil {
				t.Fatalf("with %s=%q a writer MUTATED a root another open file description held.\n"+
					"That is silent de-serialization configured by an environment variable: the P0 this "+
					"bead closes, reinstated by anyone who exports this knob carelessly.",
					LockTimeoutEnvVar, tc.value)
			}
			if got, _ := f.readAggregate(); got != original {
				t.Fatalf("a refused writer changed the aggregate with %s=%q: %q",
					LockTimeoutEnvVar, tc.value, got)
			}
			if fileExists(t, st.StateFile()) || fileExists(t, st.JournalFile()) {
				t.Fatalf("a refused writer committed ownership state with %s=%q", LockTimeoutEnvVar, tc.value)
			}
			if st.EverRecorded() {
				t.Fatalf("a refused writer left durable evidence that this root was managed with %s=%q",
					LockTimeoutEnvVar, tc.value)
			}
		})
	}
}

// TestAdversarialZeroLockTimeoutIsNotConfusedWithUnset pins the one arithmetic
// trap in the timeout plumbing: clampedLockTimeout returns s.lockTimeout as-is,
// so if the zero value of the field meant "use the default", then
// AGENT_MARKETPLACE_OWNERSHIP_LOCK_TIMEOUT=0 would silently mean "wait 30
// seconds" — the exact opposite of the documented fail-fast — and every
// fail-fast test seam in the repository would become a 30s hang.
//
// Asserted by observation rather than by reading the field: with 0 the
// acquisition must make exactly ONE attempt.
func TestAdversarialZeroLockTimeoutIsNotConfusedWithUnset(t *testing.T) {
	f := newFixture(t)
	t.Setenv(LockTimeoutEnvVar, "0")

	holder := f.store()
	release, err := holder.Lock()
	if err != nil {
		t.Fatalf("holder Lock: %v", err)
	}
	defer release()

	var timedOut *LockEvent
	st, err := Open(f.targetRoot, WithStateRoot(f.stateRoot), WithClock(f.nextClock),
		// A 60s option, so a value silently treated as "unset" would be
		// unmissable rather than merely slow.
		WithLockTimeout(60*time.Second),
		WithLockObserver(func(ev LockEvent) {
			if ev.Kind == LockTimedOut {
				copied := ev
				timedOut = &copied
			}
		}))
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, err := st.Lock(); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("Lock with a zero env timeout = %v, want ErrLockTimeout", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("a zero env timeout waited %s; zero must mean exactly one non-blocking attempt", elapsed)
	}
	if timedOut == nil {
		t.Fatal("no LockTimedOut event was observed")
	}
	if timedOut.Attempts != 1 {
		t.Fatalf("a zero timeout made %d attempts, want exactly 1", timedOut.Attempts)
	}
}

// TestAdversarialAMalformedLockTimeoutBreaksReadOnlyCommandsToo is a REPORTED
// CONSEQUENCE rather than a defect claim, and it is deliberately written as an
// assertion so that the decision is pinned rather than discovered later by a
// user.
//
// The env var is resolved inside [Open], and Open is on every read-only path:
// status, list, sync --check, the TUI refresh, and doctor's preview all open
// stores and none of them ever takes the lock. So a mistyped value fails commands
// that could not possibly have been affected by a lock timeout.
//
// That is the documented choice ("the failure is loud, immediate, and identical
// on every command"), and the alternative — ignoring a malformed value on read
// paths — would mean a user whose writer is misconfigured gets a clean `status`,
// which is worse. This test exists so that anyone who changes the decision has to
// change a test that says why.
func TestAdversarialAMalformedLockTimeoutBreaksReadOnlyCommandsToo(t *testing.T) {
	f := newFixture(t)
	t.Setenv(LockTimeoutEnvVar, "banana")
	st, err := Open(f.targetRoot, WithStateRoot(f.stateRoot))
	if !errors.Is(err, ErrLockTimeoutConfig) {
		t.Fatalf("Open = %v, want ErrLockTimeoutConfig", err)
	}
	if st != nil {
		t.Fatal("Open returned a Store together with a configuration error")
	}
	if !strings.Contains(err.Error(), LockTimeoutEnvVar) {
		t.Fatalf("the error does not name the knob a user has to fix: %v", err)
	}
	// And it created nothing: a rejected configuration is still a side-effect-free
	// Open.
	if _, statErr := os.Lstat(f.stateRoot); !os.IsNotExist(statErr) {
		t.Fatalf("a rejected Open created the state root: %v", statErr)
	}
}

// ---------------------------------------------------------------------------
// 2. Escaping the widened session
// ---------------------------------------------------------------------------

// TestAdversarialAPanickingSessionBodyReleasesTheLock attacks the widened
// section from the one direction its caller cannot handle: a body that never
// returns.
//
// The existing panic test covers a panic inside Apply. This one covers a panic in
// the CALLER's part of the section — the jsonc patch, a nil map, anything in the
// hundred-odd lines internal/materialize now runs while holding the lock — which
// is new surface that B3 created. A panic that left the descriptor anchored in
// heldDescriptors would wedge every future writer on that root for the life of
// the process, and since the section now spans a whole root's reconciliation
// rather than one Apply, the amount of code that can panic inside it grew by two
// orders of magnitude.
func TestAdversarialAPanickingSessionBodyReleasesTheLock(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{}\n")
	st := f.store()

	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Fatal("the panic did not propagate out of WithWriteSessionContext")
			}
		}()
		_ = st.WithWriteSessionContext(context.Background(), "adversarial", func() error {
			panic("adversarial: the caller's half of the widened section panicked")
		})
	}()

	if st.HoldsLock() {
		t.Fatalf("a panicking session body left the lock held at depth %d", st.LockDepth())
	}
	// The kernel lock is genuinely gone: a DIFFERENT open file description takes
	// it fail-fast.
	release, err := f.store(WithLockTimeout(0)).Lock()
	if err != nil {
		t.Fatalf("a panicking session body wedged the root for every future writer: %v", err)
	}
	release()
}

// TestAdversarialASessionThatReturnsAnErrorStillReleasesTheLock is the ordinary
// half of the same concern, and it also pins that the session's error is the
// BODY's error rather than being swallowed. internal/materialize relies on the
// converse — it returns nil from the body unconditionally and treats any non-nil
// return as an ACQUISITION failure that means the root was skipped without a byte
// written — so a body error that leaked through as a session error would be
// reported as "this target root was skipped and its aggregate files were left
// exactly as found" about a root that had in fact been written.
func TestAdversarialASessionThatReturnsAnErrorStillReleasesTheLock(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{}\n")
	st := f.store()

	sentinel := errors.New("adversarial: the body failed")
	err := st.WithWriteSessionContext(context.Background(), "adversarial", func() error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("session error = %v, want the body's own error", err)
	}
	if st.HoldsLock() {
		t.Fatalf("a failing session body left the lock held at depth %d", st.LockDepth())
	}
	// The distinction internal/materialize's devinLockSessionDiag depends on: a
	// body error must NOT look like an acquisition failure.
	if errors.Is(err, ErrLockTimeout) || errors.Is(err, ErrLockUnsupported) {
		t.Fatalf("a body error was reported as an acquisition failure: %v", err)
	}
	release, err := f.store(WithLockTimeout(0)).Lock()
	if err != nil {
		t.Fatalf("a failing session body wedged the root: %v", err)
	}
	release()
}

// TestAdversarialSequentialMutationsInOneSessionAreNeverRefusedAsNested is the
// reentrancy item: ErrNestedMutation is placed INSIDE withWriteLock precisely so
// that a widened session containing Recover plus N Applies is legal, and the
// value of that placement is only as good as a test of it.
//
// The shape asserted is exactly internal/materialize's: one session, one Recover,
// then several Applies against DIFFERENT files of the same root, all sequential.
// None may be refused, and the generations must be consecutive with no gap and no
// reuse — a nested-mutation refusal in the middle of a real sync would leave the
// root half reconciled with a diagnostic nobody could act on.
func TestAdversarialSequentialMutationsInOneSessionAreNeverRefusedAsNested(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{}\n")
	st := f.store()

	var gens []uint64
	if err := st.WithWriteSessionContext(context.Background(), "reconcile", func() error {
		if _, err := st.Recover(); err != nil {
			return err
		}
		if _, err := st.BackupOnly(f.rel, f.absFile, ReasonSync); err != nil {
			return err
		}
		for _, key := range []string{"alpha", "beta", "gamma"} {
			res, err := applyAddingKey(st, f.rel, f.absFile, key)
			if err != nil {
				return err
			}
			gens = append(gens, res.Generation)
		}
		return nil
	}); err != nil {
		if errors.Is(err, ErrNestedMutation) {
			t.Fatalf("a widened session containing SEQUENTIAL mutations was refused as nested: %v.\n"+
				"That guard belongs inside withWriteLock for exactly this reason; if it moved outside, "+
				"every real sync of a root with two aggregate files would fail.", err)
		}
		t.Fatalf("session: %v", err)
	}
	for i, gen := range gens {
		if want := uint64(i + 1); gen != want {
			t.Fatalf("generation %d = %d, want %d: generations must be consecutive with no gap and no reuse", i, gen, want)
		}
	}
	if err := recordDescribesFile(f.store(), f.rel, f.absFile); err != nil {
		t.Fatalf("the record stopped describing the file: %v", err)
	}
	if st.HoldsLock() {
		t.Fatalf("the session was not released: depth %d", st.LockDepth())
	}
}

// TestAdversarialANestedMutationInsideASessionIsStillRefused is the inverse: the
// guard must not have been weakened into uselessness by the widening. A mutation
// that starts while another one on the same Store is half written is still
// refused, session or no session — because inside a session the lock is a depth
// bump and cannot exclude it, which is the whole reason the guard exists.
func TestAdversarialANestedMutationInsideASessionIsStillRefused(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{}\n")
	st := f.store()

	var inner error
	nested := &nestingCrasher{run: func() { _, inner = applyAddingKey(st, f.rel, f.absFile, "beta") }}
	st.crasher = nested

	if err := st.WithWriteSessionContext(context.Background(), "reconcile", func() error {
		_, err := applyAddingKey(st, f.rel, f.absFile, "alpha")
		return err
	}); err != nil {
		t.Fatalf("outer session: %v", err)
	}
	if !nested.fired {
		t.Fatal("the nested mutation never ran, so this test proves nothing")
	}
	if !errors.Is(inner, ErrNestedMutation) {
		t.Fatalf("a mutation started while another was half written returned %v, want ErrNestedMutation.\n"+
			"Widening the section must not weaken the one guard the reentrant lock structurally cannot "+
			"provide.", inner)
	}
	if err := recordDescribesFile(f.store(), f.rel, f.absFile); err != nil {
		t.Fatalf("the record stopped describing the file: %v", err)
	}
}

// nestingCrasher runs an arbitrary function at one crash point without failing
// the mutation, which is how a second mutation is made to start while the first
// one is genuinely half written.
type nestingCrasher struct {
	run   func()
	fired bool
}

func (c *nestingCrasher) Crash(point CrashPoint) error {
	if c.fired || point != CrashAfterStagedWrite {
		return nil
	}
	c.fired = true
	c.run()
	return nil
}
