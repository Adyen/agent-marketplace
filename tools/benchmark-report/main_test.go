package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func lines(name string, ns ...float64) string {
	var b strings.Builder
	for _, v := range ns {
		fmt.Fprintf(&b, "Benchmark%s-2 10 %g ns/op 100 B/op 10 allocs/op\n", name, v)
	}
	return b.String()
}

func TestReportSummarizesNotableChangesAndCollapsesTheRest(t *testing.T) {
	base := "pkg: example/internal/tui\n" + lines("Slower", 1e6, 1.01e6, 0.99e6, 1e6, 1.02e6, 0.98e6) +
		lines("Faster", 4e6, 4.1e6, 3.9e6, 4e6, 4.05e6, 3.95e6) +
		lines("Noisy", 1e6, 3e6, 1e6, 3e6, 1e6, 3e6) +
		lines("Drift", 1e6, 1.01e6, 0.99e6, 1e6, 1.02e6, 0.98e6) +
		lines("Removed", 1)
	head := "pkg: example/internal/tui\n" + lines("Slower", 2e6, 2.01e6, 1.99e6, 2e6, 2.02e6, 1.98e6) +
		lines("Faster", 1e6, 1.01e6, 0.99e6, 1e6, 1.02e6, 0.98e6) +
		lines("Noisy", 3e6, 1e6, 3e6, 1e6, 3e6, 1.1e6) +
		lines("Drift", 1.05e6, 1.06e6, 1.04e6, 1.05e6, 1.07e6, 1.03e6) +
		"BenchmarkNew/enable=true-2 1 2e6 ns/op 200 B/op 20 allocs/op\n"
	var out bytes.Buffer
	if err := report(strings.NewReader(base), strings.NewReader(head), &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	summary, details, found := strings.Cut(got, "<details>")
	if !found {
		t.Fatalf("missing collapsed full table in:\n%s", got)
	}
	for _, want := range []string{
		"**1 regression, 1 improvement** of 4 compared benchmarks",
		"| example/internal/tui / Slower | 1 ms | 2 ms | **+100.0%** | ~ | ~ |",
		"| example/internal/tui / Faster | 4 ms | 1 ms | **-75.0%** | ~ | ~ |",
		"**New:** `example/internal/tui / New/enable=true` (2 ms)",
		"**Removed:** `example/internal/tui / Removed`",
	} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary missing %q in:\n%s", want, summary)
		}
	}
	if strings.Index(summary, "Slower") > strings.Index(summary, "Faster") {
		t.Errorf("regressions should precede improvements:\n%s", summary)
	}
	for _, hidden := range []string{"Noisy", "Drift"} {
		if strings.Contains(summary, hidden) {
			t.Errorf("summary should omit %s:\n%s", hidden, summary)
		}
	}
	for _, want := range []string{
		"All 6 benchmarks",
		"| example/internal/tui / Noisy | 2 ms | 2.05 ms | ~ | ~ | ~ |",
		"| example/internal/tui / Drift | 1 ms | 1.05 ms | +5.0% | ~ | ~ |",
		"| example/internal/tui / New/enable=true | — | 2 ms | — | — | — |",
	} {
		if !strings.Contains(details, want) {
			t.Errorf("details missing %q in:\n%s", want, details)
		}
	}
}

func TestMannWhitneyP(t *testing.T) {
	separated := mannWhitneyP([]float64{1, 2, 3, 4, 5, 6}, []float64{7, 8, 9, 10, 11, 12})
	if separated >= 0.01 {
		t.Errorf("fully separated samples p = %v, want < 0.01", separated)
	}
	if p := mannWhitneyP([]float64{1, 1, 1}, []float64{1, 1, 1}); p != 1 {
		t.Errorf("identical samples p = %v, want 1", p)
	}
	if p := mannWhitneyP([]float64{1, 3, 5}, []float64{2, 4, 6}); p < 0.5 {
		t.Errorf("interleaved samples p = %v, want >= 0.5", p)
	}
}

func TestReportRejectsEmptyAndInvalidResults(t *testing.T) {
	for _, input := range []string{"PASS\n", "pkg: test\nBenchmarkBad-2 1 NaN ns/op 0 B/op 0 allocs/op\n", "pkg: test\nBenchmarkBad-2 1 -1 ns/op 0 B/op 0 allocs/op\n"} {
		var out bytes.Buffer
		if err := report(strings.NewReader(input), strings.NewReader(input), &out); err == nil {
			t.Fatalf("accepted invalid benchmark results: %q", input)
		}
	}
}
