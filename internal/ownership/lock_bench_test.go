package ownership

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The per-root advisory lock sits on the `sync` latency path: B2 takes it inside
// Apply, Recover, and BackupOnly, and B3 takes it once around the whole Devin
// aggregate loop. The project's performance policy says a hot path earns a
// benchmark the first time it is touched, so this is the baseline anyone who
// later changes the acquisition strategy has to beat.
//
// The number that matters is the UNCONTENDED cost, because that is what every
// single sync pays: one openat plus one non-blocking flock plus one close. The
// contended cost is deliberately not benchmarked — it is dominated by the other
// process's critical section, so it would measure the fixture, not the code.

func benchLockStore(b *testing.B) *Store {
	b.Helper()
	base := b.TempDir()
	targetRoot := filepath.Join(base, "repo", ".devin")
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		b.Fatal(err)
	}
	st, err := Open(targetRoot,
		WithStateRoot(filepath.Join(base, "state", "agent-marketplace")),
		WithLockTimeout(30*time.Second),
	)
	if err != nil {
		b.Fatal(err)
	}
	return st
}

// BenchmarkLockUncontended measures a full acquire/release cycle with nobody
// else on the root: open the lock file, flock LOCK_EX|LOCK_NB, write the
// diagnostics-only metadata line, LOCK_UN, close.
func BenchmarkLockUncontended(b *testing.B) {
	st := benchLockStore(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		release, err := st.Lock()
		if err != nil {
			b.Fatal(err)
		}
		release()
	}
}

// BenchmarkLockReentrant measures a nested acquisition on a Store that already
// holds the lock. This is the cost B2's inner acquisitions inside Apply pay once
// B3 widens the section around them, and it must stay far below the uncontended
// cost: it opens no descriptor and makes no syscall.
func BenchmarkLockReentrant(b *testing.B) {
	st := benchLockStore(b)
	outer, err := st.Lock()
	if err != nil {
		b.Fatal(err)
	}
	defer outer()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		release, err := st.Lock()
		if err != nil {
			b.Fatal(err)
		}
		release()
	}
}
