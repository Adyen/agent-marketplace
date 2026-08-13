//go:build unix

package ownership

import (
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// TestENOLCKRetriesAndDegradesAtDeadline treats ENOLCK as transient lock-table
// exhaustion, not a filesystem capability, and degrades only when it persists to
// the lock timeout. With a zero timeout that happens in a single attempt.
func TestENOLCKRetriesAndDegradesAtDeadline(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{}\n")

	var events []LockEvent
	st := f.store(WithLockTimeout(0))
	st.tryLock = func(*os.File) error { return classifyFlockError(syscall.ENOLCK) }
	st.lockObserver = func(ev LockEvent) { events = append(events, ev) }

	proof, _ := st.Proof(f.rel)
	priorDigest := Digest([]byte("{}\n"))
	logs := captureLogs(t)

	res, err := st.Apply(Mutation{
		Rel:                   f.rel,
		AbsFile:               f.absFile,
		NextBytes:             []byte(`{"alpha":"value-alpha"}` + "\n"),
		Entries:               []Entry{mcpEntry("alpha", "alpha", "value-alpha")},
		Reason:                ReasonSync,
		ExpectPriorGeneration: proof.Generation(),
		ExpectPriorDigest:     priorDigest,
	})
	if err != nil {
		t.Fatalf("ENOLCK should degrade and proceed, not fail: %v", err)
	}
	if res.Generation != 1 {
		t.Fatalf("degraded Apply committed generation %d, want 1", res.Generation)
	}

	unsupported := 0
	timedOut := 0
	for _, ev := range events {
		switch ev.Kind {
		case LockUnsupported:
			unsupported++
		case LockTimedOut:
			timedOut++
		}
	}
	if unsupported != 1 {
		t.Fatalf("LockUnsupported events = %d, want 1", unsupported)
	}
	if timedOut != 0 {
		t.Fatalf("saw %d LockTimedOut events for ENOLCK; want 0", timedOut)
	}

	// The diagnostic is lock-unavailable, not lock-timeout.
	if !strings.Contains(logs(), diag.CodeOwnershipLockUnavailable) || !strings.Contains(logs(), "NOT serialized") {
		t.Fatalf("ENOLCK degradation was not logged as %s with 'NOT serialized':\n%s", diag.CodeOwnershipLockUnavailable, logs())
	}
}
