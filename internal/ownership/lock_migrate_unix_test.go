//go:build unix

package ownership

import (
	"errors"
	"log/slog"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// TestWritersProceedUnserializedWhenLockingIsUnavailable pins the one policy
// decision B2 had to make explicitly, on the branch that is unreachable on a
// normal darwin or linux filesystem and is the ONLY behaviour on GOOS=windows.
//
// ErrLockUnsupported means NO protection at all. The choice, taken from the
// design and written into Store.withWriteLock rather than left to a reader of the
// code to infer, is that a WRITER PROCEEDS and warns:
//
//   - refusing would make every mutation fail exactly where flock is
//     unavailable, i.e. make the tool unusable rather than merely unserialized;
//   - proceeding is byte-for-byte today's behaviour on those filesystems, so it
//     is a documented degradation and not a new weakening;
//   - the portable backstop for this path is the compare-and-swap precondition
//     on Apply (B5), which needs no kernel support.
//
// A test is the only thing that can keep that from silently becoming "refuse"
// (which would break the fleet's NFS homes) or "proceed silently" (which would
// hide the degradation). The errno set comes from classifyFlockError rather than
// from a literal here, so this test cannot drift from filelock_unix.go.
func TestWritersProceedUnserializedWhenLockingIsUnavailable(t *testing.T) {
	unsupported := func(*os.File) error { return classifyFlockError(syscall.ENOTSUP) }

	t.Run("Apply", func(t *testing.T) {
		f := newFixture(t)
		f.writeAggregate("{}\n")
		logs := captureLogs(t)

		var kinds []LockEventKind
		st := f.store(WithLockObserver(func(ev LockEvent) { kinds = append(kinds, ev.Kind) }))
		st.tryLock = unsupported

		res, err := applyAddingKey(st, f.rel, f.absFile, "alpha")
		if err != nil {
			t.Fatalf("Apply on a filesystem with no advisory locking: %v; the documented decision is to PROCEED with a warning, not to refuse", err)
		}
		// Exactly one generation: the degraded path must run the body once, not
		// once per attempt.
		if res.Generation != 1 {
			t.Fatalf("degraded Apply committed generation %d, want 1; the body ran more than once", res.Generation)
		}
		backups, err := st.SafetyBackups(f.rel)
		if err != nil {
			t.Fatal(err)
		}
		if len(backups) != 1 {
			t.Fatalf("degraded Apply retained %d backups, want 1; the body ran more than once", len(backups))
		}
		if err := recordDescribesFile(st, f.rel, f.absFile); err != nil {
			t.Fatalf("degraded Apply left the record and the file disagreeing: %v", err)
		}
		for _, k := range kinds {
			if k == LockAcquired {
				t.Fatal("the observer saw an acquisition on a filesystem that cannot lock")
			}
		}
		if st.HoldsLock() {
			t.Fatal("the store believes it holds a lock on a filesystem that cannot lock")
		}
		// The degradation is reported, not silent.
		if body := logs(); !strings.Contains(body, diag.CodeOwnershipLockUnavailable) ||
			!strings.Contains(body, "NOT serialized") {
			t.Fatalf("no %s warning was logged for a degraded mutation:\n%s", diag.CodeOwnershipLockUnavailable, body)
		}
	})

	t.Run("Recover", func(t *testing.T) {
		f := newFixture(t)
		f.writeAggregate("{}\n")
		// Seed an in-flight generation with the production crash seam.
		crashed := f.store(WithCrasher(CrashAt(CrashAfterStagedWrite)))
		if _, err := applyAddingKey(crashed, f.rel, f.absFile, "alpha"); !errors.Is(err, ErrInjectedCrash) {
			t.Fatalf("seeding: want ErrInjectedCrash, got %v", err)
		}

		st := f.store()
		st.tryLock = unsupported
		outcomes, err := st.Recover()
		if err != nil {
			t.Fatalf("Recover on a filesystem with no advisory locking: %v", err)
		}
		if len(outcomes) != 1 || outcomes[0].Action != ActionRolledBack {
			t.Fatalf("degraded recovery outcomes = %+v, want one rolled-back", outcomes)
		}
	})

	t.Run("BackupOnly", func(t *testing.T) {
		f := newFixture(t)
		f.writeAggregate("{ not json")
		st := f.store()
		st.tryLock = unsupported
		path, err := st.BackupOnly(f.rel, f.absFile, ReasonSync)
		if err != nil {
			t.Fatalf("BackupOnly on a filesystem with no advisory locking: %v", err)
		}
		got, err := readFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if got != "{ not json" {
			t.Fatalf("retained bytes = %q", got)
		}
	})

	t.Run("a timeout is still a refusal", func(t *testing.T) {
		// The degrade decision is scoped to ErrLockUnsupported ONLY. Every other
		// acquisition failure must still refuse, or "proceed unserialized" would
		// become the answer to ordinary contention.
		f := newFixture(t)
		f.writeAggregate("{}\n")
		holder := f.store()
		release, err := holder.Lock()
		if err != nil {
			t.Fatalf("holder Lock: %v", err)
		}
		defer release()

		st := f.store(WithLockTimeout(0))
		if _, err := applyAddingKey(st, f.rel, f.absFile, "alpha"); !errors.Is(err, ErrLockTimeout) {
			t.Fatalf("contended Apply: want ErrLockTimeout, got %v", err)
		}
		if fileExists(t, st.StateFile()) {
			t.Fatal("a contended Apply degraded to proceeding unserialized")
		}
	})
}

// captureLogs redirects the default logger into a buffer for one test and returns
// an accessor for what was written.
func captureLogs(t *testing.T) func() string {
	t.Helper()
	var buf strings.Builder
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return buf.String
}
