package ownership

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/testutil"
)

// This file is the ADVERSARIAL suite for B2 (agent-marketplace-mfez): the per-
// root advisory lock wired into Apply, Recover, and BackupOnly. Its job is not to
// confirm that the migration works — lock_migrate_test.go does that — but to
// attack the four things a serialization change can still get wrong once the
// happy path is green:
//
//  1. state that is corrupted THROUGH the lock, because the caller's read is
//     still outside the section (the residual B3 gap, measured here rather than
//     assumed);
//  2. the exactly-once contract of Store.withWriteLock on paths no production
//     caller reaches (a panicking body, a reentrant body, a second Store);
//  3. the degrade branch used as a privilege path — anything that can force
//     ErrLockUnsupported on a normally working system silently disables the P0
//     fix, and fd exhaustion is the realistic version of that;
//  4. a read path that blocks, mutates, or acquires.
//
// Every test here is deterministic. Where a rate is genuinely statistical it is
// LOGGED, never asserted, and the invariant asserted alongside it is one that
// holds for every interleaving.

// ---------------------------------------------------------------------------
// 1. Corrupting state THROUGH the lock: the residual B3 gap
// ---------------------------------------------------------------------------

// applyContent commits an exact aggregate content map, WITHOUT re-reading the
// file first. It is applyAddingKey split at the read, which is the only way to
// construct the B3-shaped interleaving deterministically: two writers that each
// read the aggregate BEFORE either of them entered its critical section.
func applyContent(st *Store, rel, abs string, content map[string]string) (Result, error) {
	next, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		return Result{}, err
	}
	next = append(next, '\n')
	proof, _ := st.Proof(rel)
	gen, priorDigest := proof.Generation(), ""
	if raw, readErr := os.ReadFile(abs); readErr == nil {
		priorDigest = Digest(raw)
	}
	return st.Apply(Mutation{
		Rel:                   rel,
		AbsFile:               abs,
		NextBytes:             next,
		Entries:               aggregateEntries(content),
		Reason:                ReasonSync,
		ExpectPriorGeneration: gen,
		ExpectPriorDigest:     priorDigest,
	})
}

// TestAdversarialB2StillLosesAnUpdateWhenTheCallersReadIsOutsideTheSection was
// B2's honest statement of what B2 did NOT fix. B3 (agent-marketplace-wrp8) fixed
// it, so THE ASSERTION IS NOW INVERTED, exactly as the previous version of this
// comment instructed: no key may be lost.
//
// The name is deliberately unchanged so the inversion is visible in `git log -p`
// rather than looking like a deleted test and an unrelated new one.
//
// What changed is the CALLER's shape, which is the whole of B3. Before, the
// caller read the aggregate outside any section and only then called Apply
// (internal/materialize/devin.go read at :469 and applied at :563), so two
// processes could both read, then take the lock one after the other, and the
// second one's patch — computed from bytes the first had already replaced —
// DROPPED the first one's entry. Now reconcileDevinAggregates wraps its whole
// read-modify-commit in one [Store.WithWriteSessionContext], which is modelled
// here by the session around each writer: Apply's own acquisition reenters it, and
// the second writer's READ happens after the first writer's commit is published.
//
// Both writers are in one process here, so a process-global mutex keyed by root
// would satisfy this test too. That is why it is not the acceptance bar on its
// own: [TestSessionsFromTwoProcessesNeitherLoseAnUpdate] is the same property
// across two real exec'd processes, and it is the one that cannot be faked.
func TestAdversarialB2StillLosesAnUpdateWhenTheCallersReadIsOutsideTheSection(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{}\n")

	first := f.store()
	second := f.store()

	// Writer one: read, patch, commit — all inside ONE session, which is the
	// shape internal/materialize now uses.
	var firstGen uint64
	if err := first.WithWriteSession("reconcile", func() error {
		view, _, err := aggregateFromDisk(f.absFile)
		if err != nil {
			return err
		}
		view["alpha"] = aggregateValue("alpha")
		res, err := applyContent(first, f.rel, f.absFile, view)
		firstGen = res.Generation
		return err
	}); err != nil {
		t.Fatalf("first session: %v", err)
	}
	if firstGen != 1 {
		t.Fatalf("first generation = %d, want 1", firstGen)
	}

	// Writer two, same shape. Its read is INSIDE its own section, so it cannot
	// observe a pre-alpha aggregate: the first session had to finish first.
	var secondGen uint64
	if err := second.WithWriteSession("reconcile", func() error {
		view, _, err := aggregateFromDisk(f.absFile)
		if err != nil {
			return err
		}
		view["beta"] = aggregateValue("beta")
		res, err := applyContent(second, f.rel, f.absFile, view)
		secondGen = res.Generation
		return err
	}); err != nil {
		t.Fatalf("second session: %v", err)
	}
	if secondGen != 2 {
		t.Fatalf("second generation = %d, want 2: the two sessions were not serialized", secondGen)
	}

	// THE DEFECT THIS BEAD CLOSES: neither writer's entry may be lost.
	content, _, err := aggregateFromDisk(f.absFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"alpha", "beta"} {
		if _, ok := content[key]; !ok {
			t.Fatalf("entry %q was lost: %v.\n"+
				"A writer's read escaped the critical section, so its patch was computed from bytes "+
				"another writer had already replaced. This is the lost update bead "+
				"agent-marketplace-wrp8 exists to remove.", key, content)
		}
	}

	// And B2's guarantee still holds on top of it: the record describes the live
	// file, so the next sync sees owned-clean rather than drift, and nothing is
	// left in flight.
	if err := recordDescribesFile(f.store(), f.rel, f.absFile); err != nil {
		t.Fatalf("the record stopped describing the file: %v", err)
	}
	// The safety backups still bracket both generations, so a user's bytes remain
	// recoverable whatever the outcome: one per generation, the newest holding
	// the content the second generation replaced.
	backups, err := f.store().SafetyBackups(f.rel)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 2 {
		t.Fatalf("retained backups = %d, want one per generation", len(backups))
	}
	raw, err := backups[len(backups)-1].RawBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "alpha") {
		t.Fatalf("the newest safety backup does not hold the bytes the second generation replaced: %q", raw)
	}
}

// TestAdversarialMeasureResidualLostUpdateRate raced the caller shape — read the
// aggregate, then Apply — from two Stores over many trials and LOGGED how often an
// update was lost. With B2 alone the answer was 60/60. B3 makes it 0, and the
// measurement is therefore now an ASSERTION: a single lost entry fails the test.
//
// The name is unchanged so the inversion is auditable in the history, and the rate
// is still logged so the number in bead agent-marketplace-wrp8's report is
// reproducible rather than remembered.
//
// Two things make asserting the rate legitimate now, where before it would have
// been the sampling-based test lock_migrate_test.go's header forbids. It is no
// longer a claim about a narrowed WINDOW that happened not to be hit: with the
// read inside the section there is no window, so the assertion holds for every
// interleaving the scheduler can produce, and a tree that reopened the window
// fails here at a rate the old measurement puts at 100%. And the invariant B2
// contributed — the record still describes the live file — is asserted per trial
// alongside it, so neither guarantee can be sampled away by the other.
func TestAdversarialMeasureResidualLostUpdateRate(t *testing.T) {
	// 200 trials in a full run, which is the count bead agent-marketplace-wrp8's
	// acceptance criterion names, and 20 under -short: the rate this measures was
	// 100% per trial before the fix, so twenty trials is just as deterministic and
	// the other 180 are repetition (see testutil.Trials). Each trial is two
	// goroutines with their OWN Store, so they hold two open file descriptions and
	// genuinely contend in the kernel.
	trials := testutil.Trials(testutil.FullTrials, testing.Short())
	lost := 0
	for trial := 0; trial < trials; trial++ {
		t.Run(fmt.Sprintf("trial-%d", trial), func(t *testing.T) {
			f := newFixture(t)
			f.writeAggregate("{}\n")

			start := make(chan struct{})
			var wg sync.WaitGroup
			errs := make(chan error, 2)
			for _, key := range []string{"alpha", "beta"} {
				wg.Add(1)
				go func(key string) {
					defer wg.Done()
					st := f.store(WithClock(time.Now), WithLockTimeout(60*time.Second))
					<-start
					// The READ is inside the session, which is what
					// reconcileDevinAggregates now does around its own read of the
					// aggregate and of the ownership record.
					errs <- st.WithWriteSession("reconcile", func() error {
						_, err := applyAddingKey(st, f.rel, f.absFile, key)
						return err
					})
				}(key)
			}
			close(start)
			wg.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatalf("trial %d: session: %v", trial, err)
				}
			}

			if err := recordDescribesFile(f.store(), f.rel, f.absFile); err != nil {
				t.Fatalf("trial %d: the record stopped describing the file: %v", trial, err)
			}
			content, _, err := aggregateFromDisk(f.absFile)
			if err != nil {
				t.Fatal(err)
			}
			if len(content) != 2 {
				lost++
				t.Errorf("trial %d lost an entry: %v.\n"+
					"With B2 alone this happened in 60/60 trials because the caller's read was outside the "+
					"section; with the widened session it must never happen.", trial, content)
			}
		})
	}
	t.Logf("LOST-UPDATE RATE with the widened session: %d/%d trials lost an entry "+
		"(the record described the file in every trial). B2 alone measured 60/60 lost.",
		lost, trials)
}

// ---------------------------------------------------------------------------
// 2. The exactly-once guarantee, on paths no production caller reaches
// ---------------------------------------------------------------------------

// panicCrasher panics at one crash point instead of returning an error, which is
// the only way to make a withWriteLock body unwind without returning.
type panicCrasher struct {
	point CrashPoint
	runs  *int
}

func (c *panicCrasher) Crash(point CrashPoint) error {
	if point != c.point {
		return nil
	}
	*c.runs++
	panic("adversarial: a mutation body panicked inside the critical section")
}

// TestAdversarialAPanickingBodyRunsOnceAndReleasesTheLock attacks the `started`
// flag from the one direction it cannot observe: a body that never returns at
// all.
//
// Two things must hold. The body must not be rerun by the degraded branch (a
// panic is not ErrLockUnsupported, but neither is it a return, so a naive
// recover-and-retry would double-mutate). And the lock must be released, because
// a panic that leaves a held descriptor anchored in heldDescriptors would wedge
// every future writer on this root for the life of the process — the anchoring
// in lock.go trades a silent release for a loud leak, and that trade is only
// acceptable if the ordinary unwind path still releases.
//
// A process-global mutex would also pass this test; it is a test of the
// exactly-once contract, not of the mechanism.
func TestAdversarialAPanickingBodyRunsOnceAndReleasesTheLock(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{}\n")

	runs := 0
	st := f.store(WithCrasher(&panicCrasher{point: CrashBeforeBackup, runs: &runs}))

	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Fatal("the panic did not propagate out of Apply")
			}
		}()
		_, _ = applyAddingKey(st, f.rel, f.absFile, "alpha")
	}()

	if runs != 1 {
		t.Fatalf("the mutation body ran %d times, want exactly 1", runs)
	}
	if st.HoldsLock() {
		t.Fatalf("a panicking body left the lock held at depth %d", st.LockDepth())
	}
	// The kernel lock is genuinely gone: a DIFFERENT Store, i.e. a different open
	// file description, acquires it fail-fast.
	release, err := f.store(WithLockTimeout(0)).Lock()
	if err != nil {
		t.Fatalf("a panicking body wedged the root for every future writer: %v", err)
	}
	release()
	// And nothing was written: the panic fired before the mandatory backup.
	if content, _ := f.readAggregate(); content != "{}\n" {
		t.Fatalf("a panicking body mutated the aggregate: %q", content)
	}
	if fileExists(t, st.StateFile()) || fileExists(t, st.JournalFile()) {
		t.Fatal("a panicking body committed ownership state")
	}
}

// TestAdversarialLockNeverReturnsAReleaserWithAnError extends
// TestAdversarialFailedLockReturnsANilReleaser, which pins the CONTENDED failure,
// to the two failures withWriteLock's own comment reasons about: an unsupported
// filesystem and an unopenable lock path.
//
// The contract is that Store.Lock returns a NIL releaser on every failure, so
// `release, err := s.Lock(); defer release()` above the error check is a panic
// rather than a caller silently believing it holds a lock. If any error path ever
// started returning a no-op releaser, WithLock's `if err != nil { return err }`
// would still be correct while every OTHER caller of Lock became able to run an
// unserialized section believing otherwise — the exact silent de-serialization
// this bead exists to remove.
func TestAdversarialLockNeverReturnsAReleaserWithAnError(t *testing.T) {
	t.Run("unsupported", func(t *testing.T) {
		f := newFixture(t)
		st := f.store()
		st.tryLock = func(*os.File) error { return ErrLockUnsupported }
		release, err := st.Lock()
		if !errors.Is(err, ErrLockUnsupported) {
			t.Fatalf("err = %v, want ErrLockUnsupported", err)
		}
		if release != nil {
			t.Fatal("Lock returned a releaser together with ErrLockUnsupported")
		}
	})

	t.Run("open fails", func(t *testing.T) {
		f := newFixture(t)
		st := f.store()
		// A directory where the lock FILE belongs: O_RDWR on a directory fails,
		// and it is the cheapest deterministic stand-in for every open failure
		// (EACCES, EMFILE, ENOSPC) that must not be mistaken for "this
		// filesystem cannot lock".
		if err := os.MkdirAll(st.LockPath(), 0o755); err != nil {
			t.Fatal(err)
		}
		release, err := st.Lock()
		if err == nil {
			t.Fatal("Lock succeeded with a directory in place of the lock file")
		}
		if errors.Is(err, ErrLockUnsupported) {
			t.Fatalf("an unopenable lock path degraded to unserialized instead of failing: %v", err)
		}
		if release != nil {
			t.Fatal("Lock returned a releaser together with an open failure")
		}
	})
}

// ---------------------------------------------------------------------------
// 3. The degrade branch as a privilege path
// ---------------------------------------------------------------------------

// TestAdversarialAnUnopenableLockPathRefusesEveryWriterRatherThanDegrading is
// the filesystem-level version of the highest-value attack in this review: can
// anything an attacker or an unlucky filesystem can arrange push a normally
// working system onto the ErrLockUnsupported branch, which proceeds with NO
// serialization at all?
//
// The answer must be no for every failure that is not literally "this filesystem
// has no advisory locking". Here the lock PATH cannot be opened — the concrete
// stand-in is a directory at locks/<rootKey>.lock, which is what an attacker who
// can write into locks/ would plant, and which produces the same class of open
// error as EACCES, EMFILE, and ENOSPC. Every one of the three write entry points
// must refuse and write nothing.
func TestAdversarialAnUnopenableLockPathRefusesEveryWriterRatherThanDegrading(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(st *Store, f *fixture) error
	}{
		{"Apply", func(st *Store, f *fixture) error {
			_, err := applyAddingKey(st, f.rel, f.absFile, "alpha")
			return err
		}},
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
			logs := captureLogs(t)
			st := f.store()
			if err := os.MkdirAll(st.LockPath(), 0o755); err != nil {
				t.Fatal(err)
			}

			err := tc.call(st, f)
			if err == nil {
				t.Fatal("the writer proceeded with an unopenable lock path, i.e. completely unserialized")
			}
			if errors.Is(err, ErrLockUnsupported) {
				t.Fatalf("an unopenable lock path was classified as 'this filesystem cannot lock': %v", err)
			}
			if body := logs(); strings.Contains(body, "NOT serialized") {
				t.Fatalf("the degrade branch fired for an ordinary open failure:\n%s", body)
			}
			// Nothing was written, so a failing lock path can never become a way
			// to make a mutation happen without serialization.
			if content, _ := f.readAggregate(); content != "{}\n" {
				t.Fatalf("aggregate changed: %q", content)
			}
			if fileExists(t, st.StateFile()) || fileExists(t, st.JournalFile()) {
				t.Fatal("ownership state was written")
			}
			if st.EverRecorded() {
				t.Fatal("a refused writer left durable evidence that this root was managed")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 4. Reads never block, never mutate, never acquire — including on bad state
// ---------------------------------------------------------------------------

// TestAdversarialReadPathsOnUnusableStateTakeNoLockAndCreateNothing extends the
// happy-path read test to the states a read path is most likely to have a
// side effect in: a CORRUPT record, a corrupt journal, and a Proof taken after a
// failed Load. All three are checked while another process holds the lock, with
// the default 30s timeout, so a read that acquired would be unmissable rather
// than flaky.
//
// This is the fail-closed invariant restated as a locking property: unusable
// state must still mean "everything unmanaged, preserve all, prune nothing", and
// it must mean that WITHOUT the reader waiting on, or creating, anything.
func TestAdversarialReadPathsOnUnusableStateTakeNoLockAndCreateNothing(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{\"handwritten\":\"keep me\"}\n")

	// Seed a real generation, then corrupt both durable artefacts.
	if _, err := applyAddingKey(f.store(), f.rel, f.absFile, "alpha"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	seed := f.store()
	if err := writeFile(seed.StateFile(), "{ not json"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(seed.JournalFile(), "{ not json either"); err != nil {
		t.Fatal(err)
	}

	holder := f.store()
	releaseHolder, err := holder.Lock()
	if err != nil {
		t.Fatalf("holder Lock: %v", err)
	}
	defer releaseHolder()

	before := stateRootSnapshot(t, f.stateRoot)
	reader := f.store() // default 30s timeout
	deadline := time.Now().Add(10 * time.Second)

	if _, err := reader.Load(); !errors.Is(err, ErrStateCorrupt) {
		t.Fatalf("Load on a corrupt record = %v, want ErrStateCorrupt", err)
	}
	proof, err := reader.Proof(f.rel)
	if !errors.Is(err, ErrStateCorrupt) {
		t.Fatalf("Proof on a corrupt record = %v, want ErrStateCorrupt", err)
	}
	if errors.Is(err, ErrLockTimeout) || errors.Is(err, ErrLockUnsupported) {
		t.Fatalf("a read path reported a locking failure: %v", err)
	}
	// Fail closed: corrupt state means unmanaged, so sync preserves and prunes
	// nothing.
	id := mcpEntry("alpha", "resource-alpha", aggregateValue("alpha")).ID()
	if status := proof.Classify(id, Digest([]byte(aggregateValue("alpha")))); status != StatusUnmanaged {
		t.Fatalf("Classify on a corrupt record = %s, want unmanaged", status)
	}
	if _, err := reader.ReadJournal(); !errors.Is(err, ErrStateCorrupt) {
		t.Fatalf("ReadJournal on a corrupt journal = %v, want ErrStateCorrupt", err)
	}
	// The evidence-bearing reads: neither may create a directory, and neither may
	// nominate an orphan from a corrupt record.
	if _, err := reader.SafetyBackups(f.rel); err != nil {
		t.Fatalf("SafetyBackups: %v", err)
	}
	existing := []EntryValue{{ID: id, ValueDigest: Digest([]byte(aggregateValue("alpha")))}}
	candidates, err := reader.OrphanCandidates(proof, f.rel, existing, nil)
	if err != nil {
		t.Fatalf("OrphanCandidates: %v", err)
	}
	// A corrupt record does NOT suppress nomination, and that is correct rather
	// than a fail-closed hole: nomination is driven by RETAINED evidence (a
	// safety-backup manifest, here from the generation seeded above), which is
	// exactly the source the plan names, and it is a READ that prunes nothing by
	// itself — removal still needs Mutation.Confirmed, which
	// TestApplyAcquiresBeforeValidatingAndARefusalWritesNothing pins. So what is
	// asserted here is that the nomination is evidence-backed and takes no lock,
	// not that it is absent.
	if len(candidates) != 1 {
		t.Fatalf("evidence-backed nomination from a retained manifest = %d candidates, want 1", len(candidates))
	}
	if got := candidates[0].Evidence().Kind(); got != EvidenceSafetyBackup {
		t.Fatalf("evidence kind = %v, want the retained safety backup", got)
	}
	// An entry with NO retained evidence is never nominated, corrupt record or
	// not: that is the half of the rule the corruption could have weakened.
	// "never-managed" appears in no generation and therefore in no retained
	// manifest, unlike "handwritten", which the seeded generation claimed
	// because it was already in the file.
	unmanaged := mcpEntry("never-managed", "resource-never-managed", "mine").ID()
	unbacked, err := reader.OrphanCandidates(proof, f.rel,
		append(existing, EntryValue{ID: unmanaged, ValueDigest: Digest([]byte("mine"))}), nil)
	if err != nil {
		t.Fatalf("OrphanCandidates: %v", err)
	}
	for _, c := range unbacked {
		if c.EntryID().mapKey() == unmanaged.mapKey() {
			t.Fatal("an entry with no retained evidence was nominated for removal")
		}
	}
	// SafetyBackups for a file that has no backup directory at all must not
	// create one.
	if _, err := reader.SafetyBackups("hooks.v1.json"); err != nil {
		t.Fatalf("SafetyBackups on an unbacked file: %v", err)
	}

	if time.Now().After(deadline) {
		t.Fatal("the read paths blocked; one of them is waiting on the per-root lock")
	}
	if reader.HoldsLock() || reader.LockDepth() != 0 {
		t.Fatalf("a read path acquired the lock: depth %d", reader.LockDepth())
	}
	if after := stateRootSnapshot(t, f.stateRoot); after != before {
		t.Fatalf("read paths changed the state root:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// ---------------------------------------------------------------------------
// 6. Crash interaction: a killed holder and a waiter in the backoff loop
// ---------------------------------------------------------------------------

// TestAdversarialAWaiterInBackoffResolvesTheJournalAfterTheHolderIsSIGKILLed is
// the crash/lock interaction the design's table claims is safe, constructed
// rather than asserted.
//
// The setup is the exact row of that table: a live journal in phase "prepared"
// with its staged file on disk, and a process holding the lock that then dies by
// SIGKILL. The waiter is provably inside the backoff loop before the kill (it
// reported LockContended through the observer), so this measures the real
// transition and not a lucky first attempt.
//
// What must happen: the kernel drops the lock at death, the waiter acquires
// PROMPTLY (one backoff interval, not the 30s timeout), and it resolves the
// journal to a CONSISTENT state — a roll-back here, because the aggregate is
// still the pre-mutation content, which the journal recorded as PriorDigest. A
// half-published generation would show up as a surviving journal or a record
// that no longer describes the file.
//
// A process-global mutex would NOT pass this test: the holder is a real OS
// process.
func TestAdversarialAWaiterInBackoffResolvesTheJournalAfterTheHolderIsSIGKILLed(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{}\n")

	// A genuinely in-flight generation, made with the production crash seam.
	crashed := f.store(WithCrasher(CrashAt(CrashAfterStagedWrite)))
	if _, err := applyAddingKey(crashed, f.rel, f.absFile, "alpha"); !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("seeding an in-flight generation: want ErrInjectedCrash, got %v", err)
	}
	staged, err := filepath.Glob(f.absFile + ".am-staged-*")
	if err != nil || len(staged) != 1 {
		t.Fatalf("staged files = %v (err %v), want exactly one", staged, err)
	}

	holder, _ := startHolder(t, f.targetRoot, f.stateRoot)

	contended := make(chan struct{})
	var once sync.Once
	waiter := f.store(WithLockTimeout(30*time.Second), WithLockObserver(func(ev LockEvent) {
		if ev.Kind == LockContended {
			once.Do(func() { close(contended) })
		}
	}))

	type result struct {
		outcomes []RecoveryOutcome
		err      error
		waited   time.Duration
	}
	done := make(chan result, 1)
	go func() {
		start := time.Now()
		outcomes, err := waiter.Recover()
		done <- result{outcomes: outcomes, err: err, waited: time.Since(start)}
	}()

	select {
	case <-contended:
	case <-time.After(10 * time.Second):
		t.Fatal("the waiter never reported contention, so the kill below would prove nothing")
	}

	if err := holder.Process.Kill(); err != nil {
		t.Fatalf("kill holder: %v", err)
	}

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("the waiter's Recover failed after the holder was killed: %v", got.err)
		}
		if got.waited >= 25*time.Second {
			t.Fatalf("the waiter took %s, i.e. it waited out the timeout instead of noticing the kernel had dropped the lock", got.waited)
		}
		if len(got.outcomes) != 1 || got.outcomes[0].Action != ActionRolledBack {
			t.Fatalf("recovery outcomes = %+v, want one rolled-back", got.outcomes)
		}
	case <-time.After(40 * time.Second):
		t.Fatal("the waiter never acquired the lock a dead process was holding")
	}

	// Consistent state, not a half-published generation.
	if content, _ := f.readAggregate(); content != "{}\n" {
		t.Fatalf("the rollback touched the live file: %q", content)
	}
	if fileExists(t, staged[0]) {
		t.Fatal("the staged file survived the rollback")
	}
	if fileExists(t, f.store().JournalFile()) {
		t.Fatal("the journal survived the rollback")
	}
	// And the root is immediately usable by the next writer.
	if _, err := applyAddingKey(f.store(), f.rel, f.absFile, "beta"); err != nil {
		t.Fatalf("Apply after the killed holder was cleaned up: %v", err)
	}
	if err := recordDescribesFile(f.store(), f.rel, f.absFile); err != nil {
		t.Fatalf("after recovering from a killed holder: %v", err)
	}
}

// ---------------------------------------------------------------------------
// The caller-visible session seam B3 needs
// ---------------------------------------------------------------------------

// TestAdversarialAWriteSessionDegradesLikeEveryOtherWriter is the trap B3 would
// otherwise walk into.
//
// Store.withWriteLock's documented policy for ErrLockUnsupported is PROCEED
// UNSERIALIZED AND WARN, because refusing would make the tool unusable on the
// filesystems where advisory locking is unavailable — and unavailable is the
// ONLY behaviour on GOOS=windows. But B3 does not call withWriteLock: it widens
// the section over the caller's read, which needs a caller-visible entry point.
// The obvious candidate, Store.WithLock, does the OPPOSITE — it returns the
// acquisition error and never runs the body — so a section widened with WithLock
// would turn "not serialized here" into "this root is never reconciled at all",
// on exactly the platforms the design promises to degrade on.
//
// Store.WithWriteSession is therefore the seam B3 must use, and it must share
// withWriteLock's policy exactly: exactly-once body, degrade-and-warn on
// ErrLockUnsupported, refuse on everything else.
func TestAdversarialAWriteSessionDegradesLikeEveryOtherWriter(t *testing.T) {
	t.Run("degrades and warns when locking is unavailable", func(t *testing.T) {
		f := newFixture(t)
		f.writeAggregate("{}\n")
		logs := captureLogs(t)
		st := f.store()
		st.tryLock = func(*os.File) error { return ErrLockUnsupported }

		runs := 0
		err := st.WithWriteSession("session", func() error {
			runs++
			// The whole read-modify-commit, as B3 will do it.
			_, applyErr := applyAddingKey(st, f.rel, f.absFile, "alpha")
			return applyErr
		})
		if err != nil {
			t.Fatalf("a write session on a filesystem with no advisory locking: %v; the documented decision is to PROCEED with a warning, not to skip the body", err)
		}
		if runs != 1 {
			t.Fatalf("the session body ran %d times, want exactly 1", runs)
		}
		if err := recordDescribesFile(st, f.rel, f.absFile); err != nil {
			t.Fatalf("degraded session: %v", err)
		}
		if body := logs(); !strings.Contains(body, "NOT serialized") {
			t.Fatalf("the degradation was silent:\n%s", body)
		}
	})

	t.Run("refuses on contention and runs nothing", func(t *testing.T) {
		f := newFixture(t)
		f.writeAggregate("{}\n")
		_, letGo := startHolder(t, f.targetRoot, f.stateRoot)
		defer letGo()

		st := f.store(WithLockTimeout(0))
		runs := 0
		err := st.WithWriteSession("session", func() error {
			runs++
			return nil
		})
		if !errors.Is(err, ErrLockTimeout) {
			t.Fatalf("a contended write session = %v, want ErrLockTimeout", err)
		}
		if runs != 0 {
			t.Fatalf("the session body ran %d times while another process held the lock", runs)
		}
	})

	t.Run("Apply inside a session reenters instead of self-contending", func(t *testing.T) {
		f := newFixture(t)
		f.writeAggregate("{}\n")
		var depths []int
		st := f.store(WithLockObserver(func(ev LockEvent) {
			if ev.Kind == LockReentered {
				depths = append(depths, ev.Depth)
			}
		}))
		err := st.WithWriteSession("session", func() error {
			if _, err := st.Recover(); err != nil {
				return err
			}
			_, err := applyAddingKey(st, f.rel, f.absFile, "alpha")
			return err
		})
		if err != nil {
			t.Fatalf("session: %v", err)
		}
		// Recover and Apply each reentered at depth 2 rather than opening a
		// second descriptor, which is what makes widening the section possible.
		if len(depths) != 2 {
			t.Fatalf("reentries = %v, want one for Recover and one for Apply", depths)
		}
		for _, d := range depths {
			if d != 2 {
				t.Fatalf("reentry depth = %d, want 2", d)
			}
		}
		if st.HoldsLock() {
			t.Fatal("the session did not release")
		}
	})
}
