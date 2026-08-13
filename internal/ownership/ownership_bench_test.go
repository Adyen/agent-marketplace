package ownership

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Everything benchmarked here is on the `sync` latency path: every sync opens a
// store, recovers, reads a proof, classifies the aggregate's entries, and applies
// one journaled generation per aggregate file.

// discardBenchLogs keeps the cost of the default log handler — and its output,
// which otherwise interleaves with the benchmark's own lines and makes the
// result unparseable by benchstat — out of every measurement here. Apply logs
// twice per generation.
func discardBenchLogs(b *testing.B) {
	b.Helper()
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(previous) })
}

func benchStore(b *testing.B) (*Store, string, string) {
	b.Helper()
	base := b.TempDir()
	targetRoot := filepath.Join(base, "repo", ".devin")
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		b.Fatal(err)
	}
	clock := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	st, err := Open(targetRoot,
		WithStateRoot(filepath.Join(base, "state", "agent-marketplace")),
		WithClock(func() time.Time { clock = clock.Add(time.Millisecond); return clock }),
	)
	if err != nil {
		b.Fatal(err)
	}
	return st, "mcp_config.json", filepath.Join(targetRoot, "mcp_config.json")
}

func benchEntries(n int) []Entry {
	entries := make([]Entry, 0, n)
	for i := 0; i < n; i++ {
		entries = append(entries, mcpEntry(fmt.Sprintf("server-%03d", i), fmt.Sprintf("resource-%03d", i), fmt.Sprintf(`{"command":"c%d"}`, i)))
	}
	return entries
}

func benchAggregate(n int) []byte {
	var sb strings.Builder
	sb.WriteString("{\n  // handwritten header\n  \"mcpServers\": {\n")
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteString(",\n")
		}
		fmt.Fprintf(&sb, "    \"server-%03d\": {\"command\": \"c%d\"}", i, i)
	}
	sb.WriteString("\n  }\n}\n")
	return []byte(sb.String())
}

// BenchmarkApply measures one full journaled generation: mandatory safety
// backup, journal fsync, staged write, rename, state commit, journal cleanup.
// It is dominated by fsyncs by design; the number to watch for regressions is
// allocations and the count of durable writes, not raw wall time.
func BenchmarkApply(b *testing.B) {
	for _, n := range []int{1, 25, 200} {
		b.Run(fmt.Sprintf("entries=%d", n), func(b *testing.B) {
			discardBenchLogs(b)
			st, rel, abs := benchStore(b)
			entries := benchEntries(n)
			content := benchAggregate(n)
			if err := os.WriteFile(abs, content, 0o644); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				proof, _ := st.Proof(rel)
				if _, err := st.Apply(Mutation{
					Rel:                   rel,
					AbsFile:               abs,
					NextBytes:             content,
					Entries:               entries,
					Reason:                ReasonSync,
					ExpectPriorGeneration: proof.Generation(),
					ExpectPriorDigest:     Digest(content),
				}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkApplyUncontended is the before/after instrument for B2: Apply now
// takes the per-canonical-root advisory lock, and this is what every single sync
// pays for it when nobody else is on the root — one openat, one non-blocking
// flock, one metadata line, one LOCK_UN, one close, per generation.
//
// It is deliberately the SAME benchmark before and after the migration, so the
// benchstat delta attributes the cost to the acquisition rather than to a
// re-shaped fixture. The contended cost is not benchmarked: it is dominated by
// the other writer's critical section, so it would measure the fixture.
func BenchmarkApplyUncontended(b *testing.B) {
	discardBenchLogs(b)
	st, rel, abs := benchStore(b)
	entries := benchEntries(1)
	content := benchAggregate(1)
	if err := os.WriteFile(abs, content, 0o644); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		proof, _ := st.Proof(rel)
		if _, err := st.Apply(Mutation{
			Rel:                   rel,
			AbsFile:               abs,
			NextBytes:             content,
			Entries:               entries,
			Reason:                ReasonSync,
			ExpectPriorGeneration: proof.Generation(),
			ExpectPriorDigest:     Digest(content),
		}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkProof measures the per-file ownership read every sync performs before
// patching.
func BenchmarkProof(b *testing.B) {
	for _, n := range []int{1, 25, 200} {
		b.Run(fmt.Sprintf("entries=%d", n), func(b *testing.B) {
			st, rel, abs := benchStore(b)
			content := benchAggregate(n)
			if err := os.WriteFile(abs, content, 0o644); err != nil {
				b.Fatal(err)
			}
			proof, _ := st.Proof(rel)
			if _, err := st.Apply(Mutation{
				Rel:                   rel,
				AbsFile:               abs,
				NextBytes:             content,
				Entries:               benchEntries(n),
				Reason:                ReasonSync,
				ExpectPriorGeneration: proof.Generation(),
				ExpectPriorDigest:     Digest(content),
			}); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := st.Proof(rel); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkClassify measures the pure drift classification, which runs once per
// aggregate file per sync and touches no disk.
func BenchmarkClassify(b *testing.B) {
	for _, n := range []int{1, 25, 200} {
		b.Run(fmt.Sprintf("entries=%d", n), func(b *testing.B) {
			st, rel, abs := benchStore(b)
			content := benchAggregate(n)
			if err := os.WriteFile(abs, content, 0o644); err != nil {
				b.Fatal(err)
			}
			entries := benchEntries(n)
			proof, _ := st.Proof(rel)
			if _, err := st.Apply(Mutation{
				Rel:                   rel,
				AbsFile:               abs,
				NextBytes:             content,
				Entries:               entries,
				Reason:                ReasonSync,
				ExpectPriorGeneration: proof.Generation(),
				ExpectPriorDigest:     Digest(content),
			}); err != nil {
				b.Fatal(err)
			}
			proof, err := st.Proof(rel)
			if err != nil {
				b.Fatal(err)
			}
			values := make([]EntryValue, 0, len(entries))
			for _, e := range entries {
				values = append(values, EntryValue{ID: e.ID(), ValueDigest: e.ValueDigest})
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = Classify(proof, values, values)
			}
		})
	}
}

// BenchmarkValueConflictCandidates measures the third repair class's nomination
// pass, which `doctor --repair` runs once per aggregate file next to adoption and
// orphan analysis. It is a regression guard: the pass is O(existing + desired)
// with one map, and it must stay that way rather than growing a per-entry
// re-derivation.
//
// The fixture is the worst case for this class: EVERY entry is unmanaged (no
// ownership record at all, which is exactly the state the class exists for) and
// every key collides with a desired entry whose value differs, so every entry is
// nominated.
func BenchmarkValueConflictCandidates(b *testing.B) {
	for _, n := range []int{1, 25, 200} {
		b.Run(fmt.Sprintf("entries=%d", n), func(b *testing.B) {
			entries := benchEntries(n)
			existing := make([]EntryValue, 0, len(entries))
			desired := make([]EntryValue, 0, len(entries))
			for _, e := range entries {
				existing = append(existing, EntryValue{ID: e.ID(), ValueDigest: e.ValueDigest})
				desired = append(desired, EntryValue{ID: e.ID(), ValueDigest: Digest([]byte("generated:" + e.Key))})
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if got := ValueConflictCandidates(Proof{}, existing, desired); len(got) != n {
					b.Fatalf("nominated %d, want %d", len(got), n)
				}
			}
		})
	}
}

// BenchmarkRecoverNoJournal measures the startup cost of the recovery check on
// the overwhelmingly common clean path.
func BenchmarkRecoverNoJournal(b *testing.B) {
	st, _, _ := benchStore(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := st.Recover(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRootKey measures the key derivation every Open performs.
func BenchmarkRootKey(b *testing.B) {
	root := "/Users/somebody/work/some-repository/.devin"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = RootKey(root)
	}
}
