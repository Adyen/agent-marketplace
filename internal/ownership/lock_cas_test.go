package ownership

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// blockingCrasher blocks at a named crash point until the test resumes it, then
// returns nil so Apply continues. It is the only deterministic way to pause one
// Apply after it has written its staged file but before its compare-and-swap.
type blockingCrasher struct {
	point   CrashPoint
	reached chan struct{}
	resume  chan struct{}
}

func (c *blockingCrasher) Crash(point CrashPoint) error {
	if point != c.point {
		return nil
	}
	close(c.reached)
	<-c.resume
	return nil
}

func countEvents(events []LockEvent, kind LockEventKind) int {
	n := 0
	for _, ev := range events {
		if ev.Kind == kind {
			n++
		}
	}
	return n
}

// fmtKey returns a distinct test key for racer i.
func fmtKey(i int) string { return "server-" + string(rune('a'+i)) }

// TestCASPreconditionNeverFiresWithLockHeld races several Apply calls against a
// real per-root lock and asserts that the compare-and-swap precondition never
// fires. The lock serializes writers, so the caller's read inside the locked
// section is still valid at rename time.
func TestCASPreconditionNeverFiresWithLockHeld(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{}\n")

	var events []LockEvent
	var mu sync.Mutex
	observer := func(ev LockEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, ev)
	}

	const racers = 6
	start := make(chan struct{})
	errs := make(chan error, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// One Store per goroutine: flock is per open file description, and
			// a shared Store would reenter rather than serialize.
			racer := f.store(WithLockObserver(observer), WithLockTimeout(60*time.Second), WithClock(time.Now))
			<-start
			if _, err := applyAddingKey(racer, f.rel, f.absFile, fmtKey(i)); err != nil {
				errs <- err
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("racer failed: %v", err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if n := countEvents(events, LockCASConflict); n != 0 {
		t.Fatalf("saw %d LockCASConflict events with the real lock held; want 0", n)
	}
}

// TestCASPreconditionFiresOnDegradedConcurrentApply is the lost-update bead's
// backstop: on a filesystem with no advisory locking, two writers can interleave.
// The first pauses after staging, the second commits, and the first's resumed
// Apply fails the compare-and-swap because the record and the aggregate have
// both changed.
func TestCASPreconditionFiresOnDegradedConcurrentApply(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{}\n")

	reached := make(chan struct{})
	resume := make(chan struct{})
	crasher := &blockingCrasher{
		point:   CrashAfterStagedWrite,
		reached: reached,
		resume:  resume,
	}

	var firstEvents []LockEvent
	st1 := f.store(WithCrasher(crasher))
	st1.tryLock = func(*os.File) error { return ErrLockUnsupported }
	st1.lockObserver = func(ev LockEvent) { firstEvents = append(firstEvents, ev) }

	st2 := f.store()
	st2.tryLock = func(*os.File) error { return ErrLockUnsupported }

	var firstErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, firstErr = applyAddingKey(st1, f.rel, f.absFile, "alpha")
	}()

	select {
	case <-reached:
	case <-time.After(10 * time.Second):
		t.Fatal("first Apply did not reach the staging crash point")
	}

	// The second writer commits while the first is paused after staging.
	if _, err := applyAddingKey(st2, f.rel, f.absFile, "beta"); err != nil {
		t.Fatalf("second degraded Apply failed: %v", err)
	}

	close(resume)
	wg.Wait()

	if !errors.Is(firstErr, ErrGenerationConflict) {
		t.Fatalf("first Apply error = %v, want ErrGenerationConflict", firstErr)
	}
	if n := countEvents(firstEvents, LockCASConflict); n != 1 {
		t.Fatalf("first store LockCASConflict events = %d, want 1", n)
	}
	coded, ok := diag.AsCodedError(firstErr)
	if !ok {
		t.Fatalf("error %v is not a coded error; want %q", firstErr, diag.CodeOwnershipGenerationConflict)
	}
	if coded.Code != diag.CodeOwnershipGenerationConflict {
		t.Fatalf("error code = %q, want %q", coded.Code, diag.CodeOwnershipGenerationConflict)
	}

	// The first writer's partial generation left no durable trace.
	assertNoStagedFiles(t, f)
	assertJournalPhase(t, f.store(), "")
	backups, err := f.store().SafetyBackups(f.rel)
	if err != nil {
		t.Fatalf("SafetyBackups: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("retained backups = %d, want 1 (the second writer's)", len(backups))
	}
	// The live aggregate is the second writer's content.
	content, _ := f.readAggregate()
	if !strings.Contains(content, "beta") || strings.Contains(content, "alpha") {
		t.Fatalf("live aggregate = %q, want beta's content", content)
	}
}

// TestCASPreconditionRefusesAnExternalWrite reproduces a foreign tool changing
// the aggregate after the caller read it. Apply must refuse, leave the file as
// found, and clean up the staged file, journal, and safety backup it prepared.
func TestCASPreconditionRefusesAnExternalWrite(t *testing.T) {
	f := newFixture(t)
	prior := "{}\n"
	f.writeAggregate(prior)

	st := f.store()
	proof, _ := st.Proof(f.rel)
	priorDigest := Digest([]byte(prior))

	// A foreign tool writes the aggregate while the caller is building its patch.
	const external = `{"external":"write"}` + "\n"
	if err := os.WriteFile(f.absFile, []byte(external), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := st.Apply(Mutation{
		Rel:                   f.rel,
		AbsFile:               f.absFile,
		NextBytes:             []byte(`{"alpha":"value-alpha"}` + "\n"),
		Entries:               []Entry{mcpEntry("alpha", "alpha", "value-alpha")},
		Reason:                ReasonSync,
		ExpectPriorGeneration: proof.Generation(),
		ExpectPriorDigest:     priorDigest,
	})
	if !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("Apply error = %v, want ErrGenerationConflict", err)
	}

	// The foreign write is preserved, and no partial generation leaked.
	if got, _ := f.readAggregate(); got != external {
		t.Fatalf("aggregate = %q, want the external write preserved", got)
	}
	assertNoStagedFiles(t, f)
	assertJournalPhase(t, st, "")
	backups, err := st.SafetyBackups(f.rel)
	if err != nil {
		t.Fatalf("SafetyBackups: %v", err)
	}
	if len(backups) != 0 {
		t.Fatalf("CAS failure retained %d backup(s); want 0", len(backups))
	}
}

// TestCASFailureCleansBackupStagedAndJournal drives Apply to the compare-and-swap
// with a stale root generation and asserts that cleanupAfterCASFailure leaves no
// durable trace: no staged file, no journal, and no safety backup.
func TestCASFailureCleansBackupStagedAndJournal(t *testing.T) {
	f := newFixture(t)
	prior := "{}\n"
	f.writeAggregate(prior)

	st := f.store()
	_, _ = st.Proof(f.rel) // proof is uninitialized; we deliberately pass a wrong generation.
	priorDigest := Digest([]byte(prior))

	_, err := st.Apply(Mutation{
		Rel:                   f.rel,
		AbsFile:               f.absFile,
		NextBytes:             []byte(`{"alpha":"value-alpha"}` + "\n"),
		Entries:               []Entry{mcpEntry("alpha", "alpha", "value-alpha")},
		Reason:                ReasonSync,
		ExpectPriorGeneration: 1, // stale: there is no ownership record yet.
		ExpectPriorDigest:     priorDigest,
	})
	if !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("Apply error = %v, want ErrGenerationConflict", err)
	}

	if got, _ := f.readAggregate(); got != prior {
		t.Fatalf("aggregate = %q, want original prior bytes", got)
	}
	assertNoStagedFiles(t, f)
	assertJournalPhase(t, st, "")
	backups, err := st.SafetyBackups(f.rel)
	if err != nil {
		t.Fatalf("SafetyBackups: %v", err)
	}
	if len(backups) != 0 {
		t.Fatalf("CAS failure retained %d backup(s); want 0", len(backups))
	}
}
