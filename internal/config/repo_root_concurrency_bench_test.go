package config

import (
	"runtime"
	"strconv"
	"sync"
	"testing"
)

// benchGitRootProbe substitutes a fork-free probe so these benchmarks measure
// the memo's coordination cost rather than the ~18 ms cost of `git rev-parse`.
func benchGitRootProbe(b *testing.B) {
	b.Helper()
	previous := gitRootProbe
	ResetRepoRootCache()
	gitRootProbe = func(dir string) (string, bool) { return dir, true }
	b.Cleanup(func() {
		gitRootProbe = previous
		ResetRepoRootCache()
	})
}

// BenchmarkGitRootForParallelHit measures the steady state: the answer is
// memoized and every goroutine is only paying for the shared-map access. This
// is the shape the TUI produces, where snapshot loads and background refreshes
// resolve roots from concurrent tea.Cmds.
func BenchmarkGitRootForParallelHit(b *testing.B) {
	benchGitRootProbe(b)
	_ = gitRootFor("/repo")

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = gitRootFor("/repo")
		}
	})
}

// BenchmarkGitRootForParallelHitDistinctDirs is the same steady state spread
// over many keys, so a future change that narrows the memo to a single global
// entry (or serializes distinct directories again) shows up as a slowdown here
// while BenchmarkGitRootForParallelHit stays flat.
func BenchmarkGitRootForParallelHitDistinctDirs(b *testing.B) {
	benchGitRootProbe(b)
	const dirs = 256
	keys := make([]string, dirs)
	for i := range keys {
		keys[i] = "/repo/" + strconv.Itoa(i)
		_ = gitRootFor(keys[i])
	}

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			i++
			_ = gitRootFor(keys[i%dirs])
		}
	})
}

// BenchmarkGitRootForColdSingleFlight measures one cold resolution shared by
// GOMAXPROCS callers: from an empty memo to every caller holding the answer,
// with exactly one probe. The absolute number includes goroutine start-up, so
// read it as a regression guard on the single-flight coordination, not as the
// cost of a lookup.
func BenchmarkGitRootForColdSingleFlight(b *testing.B) {
	benchGitRootProbe(b)
	waiters := runtime.GOMAXPROCS(0)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		ResetRepoRootCache()
		b.StartTimer()

		var wg sync.WaitGroup
		for w := 0; w < waiters; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = gitRootFor("/repo")
			}()
		}
		wg.Wait()
	}
}
