package main

import (
	"io"
	"strconv"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// staleSelectionFixture builds n stale selections, each reported twice (the
// shape a multi-registry config produces), so the benchmarks below measure the
// dedup work on realistic input.
func staleSelectionFixture(n int) []*resolver.SelectedNotFoundError {
	stale := make([]*resolver.SelectedNotFoundError, 0, n*2)
	for i := 0; i < n; i++ {
		name := "stale-skill-" + strconv.Itoa(i)
		for range 2 {
			stale = append(stale, &resolver.SelectedNotFoundError{
				Kind: ir.KindSkill,
				Name: name,
				URL:  "git@example.com:infra.git",
				Ref:  "main",
			})
		}
	}
	return stale
}

// BenchmarkDedupStaleSelections guards the helper both the prune path and the
// skipped-prune message share. It runs once per `sync --prune-stale`.
func BenchmarkDedupStaleSelections(b *testing.B) {
	stale := staleSelectionFixture(50)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if got := len(dedupStaleSelections(stale)); got != 50 {
			b.Fatalf("dedup returned %d selections, want 50", got)
		}
	}
}

// BenchmarkPrintPruneSkipped measures rendering the skipped-prune explanation
// added by bead agent-marketplace-tbjf: it is on the sync output path whenever a
// run has stale selections and an untrustworthy catalog.
func BenchmarkPrintPruneSkipped(b *testing.B) {
	stale := staleSelectionFixture(50)
	trustErrs := []error{io.ErrUnexpectedEOF}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := printPruneSkipped(io.Discard, stale, trustErrs); err != nil {
			b.Fatal(err)
		}
	}
}
