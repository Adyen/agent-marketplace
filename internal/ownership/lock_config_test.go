package ownership

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// This file covers the two carry-forward items bead agent-marketplace-wrp8 owns on
// the acquisition side: the AGENT_MARKETPLACE_OWNERSHIP_LOCK_TIMEOUT escape hatch
// that diag.CodeOwnershipLockTimeout's hint has been promising users since B1, and
// cancellation of a contended wait.
//
// Nothing here sleeps. The contention is manufactured with a real holder, so every
// assertion is about a constructed state rather than a hoped-for schedule.

// TestDefaultLockTimeoutMatchesTheConfigDefault is the guard on the one piece of
// duplication this bead deliberately introduced.
//
// AGENTS.md puts overridable defaults in internal/config, but internal/ownership
// does not IMPORT that package. Not because it cannot — the import compiles for
// every GOOS, TestNonUnixBuildCompiles included — but because internal/ownership
// is the durable-state leaf whose only non-stdlib dependency is
// pkg/harness-compilation/diag, and the import would give the ownership record,
// the journal and the mandatory safety backups a transitive dependency on the YAML
// config loader to read one integer. A test-only import costs nothing, because
// `go build ./internal/ownership/...` ignores test files — so the policy's home for
// the value and this package's independence are both kept, and drift between them
// is a failing test rather than a silent divergence.
func TestDefaultLockTimeoutMatchesTheConfigDefault(t *testing.T) {
	if DefaultLockTimeout != config.DefaultOwnershipLockTimeout {
		t.Fatalf("ownership.DefaultLockTimeout = %s but config.DefaultOwnershipLockTimeout = %s;"+
			" internal/config is the documented home for this default and the two must agree",
			DefaultLockTimeout, config.DefaultOwnershipLockTimeout)
	}
	if DefaultLockTimeout != 30*time.Second {
		t.Fatalf("DefaultLockTimeout = %s, want 30s per the design's section 3", DefaultLockTimeout)
	}
}

// TestLockTimeoutEnvVarPrecedence pins the whole resolution order the project's
// config policy requires — env var > in-process configuration > code default — and
// the failure mode of an unusable value.
//
// The env var has to beat WithLockTimeout, not the other way round: it is the only
// knob a user of a released binary has, and the hint on ownership.lock-timeout
// tells them to raise it. If the option won, that hint would be a lie on every
// call site that passes one.
func TestLockTimeoutEnvVarPrecedence(t *testing.T) {
	f := newFixture(t)

	t.Run("unset falls back to the code default", func(t *testing.T) {
		t.Setenv(LockTimeoutEnvVar, "")
		if got := f.store().lockTimeout; got != DefaultLockTimeout {
			t.Fatalf("lock timeout = %s, want the default %s", got, DefaultLockTimeout)
		}
	})

	t.Run("the option overrides the default", func(t *testing.T) {
		t.Setenv(LockTimeoutEnvVar, "")
		if got := f.store(WithLockTimeout(1234 * time.Millisecond)).lockTimeout; got != 1234*time.Millisecond {
			t.Fatalf("lock timeout = %s, want the option's 1.234s", got)
		}
	})

	t.Run("the env var overrides both", func(t *testing.T) {
		t.Setenv(LockTimeoutEnvVar, "750ms")
		if got := f.store(WithLockTimeout(1234 * time.Millisecond)).lockTimeout; got != 750*time.Millisecond {
			t.Fatalf("lock timeout = %s, want the env var's 750ms", got)
		}
	})

	t.Run("zero means fail fast and is not mistaken for unset", func(t *testing.T) {
		t.Setenv(LockTimeoutEnvVar, "0")
		st := f.store(WithLockTimeout(30 * time.Second))
		if got := st.lockTimeout; got != 0 {
			t.Fatalf("lock timeout = %s, want 0 (fail fast)", got)
		}
		// And it genuinely fails fast against a real holder rather than waiting.
		_, letGo := startHolder(t, f.targetRoot, f.stateRoot)
		defer letGo()
		started := time.Now()
		if _, err := f.store().Lock(); !errors.Is(err, ErrLockTimeout) {
			t.Fatalf("Lock with the env var at 0 = %v, want ErrLockTimeout", err)
		}
		if waited := time.Since(started); waited > 5*time.Second {
			t.Fatalf("a fail-fast acquisition waited %s", waited)
		}
	})

	t.Run("whitespace is treated as unset", func(t *testing.T) {
		t.Setenv(LockTimeoutEnvVar, "   ")
		if got := f.store().lockTimeout; got != DefaultLockTimeout {
			t.Fatalf("lock timeout = %s, want the default %s", got, DefaultLockTimeout)
		}
	})

	for _, bad := range []string{"30", "soon", "-5s"} {
		t.Run("refuses "+bad, func(t *testing.T) {
			t.Setenv(LockTimeoutEnvVar, bad)
			st, err := Open(f.targetRoot, WithStateRoot(f.stateRoot))
			if !errors.Is(err, ErrLockTimeoutConfig) {
				t.Fatalf("Open with %s=%q = (%v, %v), want ErrLockTimeoutConfig", LockTimeoutEnvVar, bad, st, err)
			}
			if st != nil {
				t.Fatal("Open returned a store together with a configuration error")
			}
		})
	}
}

// TestLockContextAbandonsAContendedWaitWhenCancelled is the Ctrl-C item.
//
// materialize.Run already carries a context and now opens the per-root section
// inside it, but the backoff loop only checked its own deadline — so a sync that
// hit contention ignored cancellation for up to 30 s. The contention here is real
// and constructed (a child process provably holds the lock), and the cancellation
// is explicit, so the test neither sleeps nor races.
func TestLockContextAbandonsAContendedWaitWhenCancelled(t *testing.T) {
	f := newFixture(t)
	_, letGo := startHolder(t, f.targetRoot, f.stateRoot)
	defer letGo()

	// A generous timeout, so a run that ignored cancellation would sit here for a
	// minute and the test would fail on its own deadline rather than flake.
	st := f.store(WithLockTimeout(60 * time.Second))
	var kinds []LockEventKind

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel from the observer, at the exact moment the acquisition reports that
	// it has begun waiting. That is the interleaving the item is about — cancelled
	// WHILE contended — constructed rather than timed.
	st.lockObserver = func(ev LockEvent) {
		kinds = append(kinds, ev.Kind)
		if ev.Kind == LockContended {
			cancel()
		}
	}
	defer cancel()

	started := time.Now()
	release, err := st.LockContext(ctx)
	waited := time.Since(started)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("LockContext on a cancelled caller = %v, want context.Canceled", err)
	}
	// A cancellation is NOT a timeout: nothing expired, no other process is at
	// fault, and reporting ownership.lock-timeout would blame one.
	if errors.Is(err, ErrLockTimeout) {
		t.Fatalf("a cancelled wait was reported as a lock timeout: %v", err)
	}
	if release != nil {
		t.Fatal("LockContext returned a releaser together with a cancellation")
	}
	if waited > 30*time.Second {
		t.Fatalf("the wait ignored cancellation for %s", waited)
	}
	if st.HoldsLock() {
		t.Fatal("a cancelled acquisition left the store believing it holds the lock")
	}
	var sawCanceled bool
	for _, k := range kinds {
		if k == LockCanceled {
			sawCanceled = true
		}
	}
	if !sawCanceled {
		t.Fatalf("observed events %v contain no %q", kinds, LockCanceled)
	}
}

// TestAnAlreadyCancelledAcquisitionCreatesNothing: a caller that is cancelled
// before it starts must not even create locks/, because that directory is the one
// thing acquisition adds to the state root.
func TestAnAlreadyCancelledAcquisitionCreatesNothing(t *testing.T) {
	f := newFixture(t)
	st := f.store()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := st.LockContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("LockContext with a cancelled context = %v, want context.Canceled", err)
	}
	if _, err := os.Lstat(f.stateRoot); !os.IsNotExist(err) {
		t.Fatalf("a cancelled acquisition created the state root: %v", err)
	}
	if err := st.WithWriteSessionContext(ctx, "reconcile", func() error {
		t.Fatal("the session body ran for a cancelled caller")
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("WithWriteSessionContext with a cancelled context = %v, want context.Canceled", err)
	}
}

// TestDegradedRootWarnsOnceNotOncePerMutation is the degrade-warning
// multiplication item.
//
// A degraded root used to log two ownership.lock-unavailable warnings per
// mutation — one from acquireLock and one from withWriteLock — which under the
// widened session's sequential mutations is 2×(1+N) identical warnings for a single
// fact about a filesystem. The fact is reported once per Store; every later
// degradation on the same Store is a debug line.
func TestDegradedRootWarnsOnceNotOncePerMutation(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{}\n")
	logs := captureLogs(t)

	st := f.store()
	st.tryLock = func(*os.File) error { return ErrLockUnsupported }

	// One session containing three sequential mutations, which is the shape
	// reconcileDevinAggregates has for a root with two aggregate files plus its
	// Recover.
	if err := st.WithWriteSession("reconcile", func() error {
		if _, err := st.Recover(); err != nil {
			return err
		}
		if _, err := applyAddingKey(st, f.rel, f.absFile, "alpha"); err != nil {
			return err
		}
		_, err := applyAddingKey(st, "hooks.v1.json", f.absFile+".hooks", "beta")
		return err
	}); err != nil {
		t.Fatalf("degraded session: %v", err)
	}

	body := logs()
	if got := countWarnings(body, "NOT serialized"); got != 1 {
		t.Fatalf("a degraded root logged %d 'NOT serialized' WARNINGS, want exactly 1:\n%s", got, body)
	}
}

// countWarnings counts log lines that are both WARN level and contain token.
func countWarnings(body, token string) int {
	count := 0
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "WARN") && strings.Contains(line, token) {
			count++
		}
	}
	return count
}
