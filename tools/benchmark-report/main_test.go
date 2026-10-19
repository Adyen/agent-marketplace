package main

import (
	"bytes"
	"fmt"
	"math"
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

// spec: benchmark-reporting/strong-evidence-remains-highlighted
// spec: benchmark-reporting/technical-evidence-stays-collapsed
func TestReportSummarizesNotableChangesAndCollapsesTheRest(t *testing.T) {
	base := "pkg: example/internal/tui\n" + strings.Repeat(lines("Slower", 1e6, 1.01e6, 0.99e6, 1e6, 1.02e6, 0.98e6), 2) +
		strings.Repeat(lines("Faster", 4e6, 4.1e6, 3.9e6, 4e6, 4.05e6, 3.95e6), 2) +
		lines("Noisy", 1e6, 3e6, 1e6, 3e6, 1e6, 3e6) +
		lines("Drift", 1e6, 1.01e6, 0.99e6, 1e6, 1.02e6, 0.98e6) +
		lines("Removed", 1)
	head := "pkg: example/internal/tui\n" + strings.Repeat(lines("Slower", 2e6, 2.01e6, 1.99e6, 2e6, 2.02e6, 1.98e6), 2) +
		strings.Repeat(lines("Faster", 1e6, 1.01e6, 0.99e6, 1e6, 1.02e6, 0.98e6), 2) +
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
		"**1 notable increase, 1 notable decrease** across 4 compared benchmarks",
		"| example/internal/tui / Slower | 1 ms | 2 ms | **+100.0%** | ~ | ~ |",
		"| example/internal/tui / Faster | 4 ms | 1 ms | **-75.0%** | ~ | ~ |",
		"**New:** `example/internal/tui / New/enable=true` (2 ms)",
		"**Removed:** `example/internal/tui / Removed`",
		"Observed differences; runner conditions may explain them.",
	} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary missing %q in:\n%s", want, summary)
		}
	}
	if strings.Index(summary, "Slower") > strings.Index(summary, "Faster") {
		t.Errorf("increases should precede decreases:\n%s", summary)
	}
	for _, hidden := range []string{"Noisy", "Drift", "Holm", "p<", "Samples", "Adjusted p", "Mann-Whitney"} {
		if strings.Contains(summary, hidden) {
			t.Errorf("summary should omit %s:\n%s", hidden, summary)
		}
	}
	for _, want := range []string{
		"All 6 benchmarks",
		"| example/internal/tui / Noisy | 2 ms | 2.05 ms | ~ | ~ | ~ | 6/6 |",
		"| example/internal/tui / Drift | 1 ms | 1.05 ms | +5.0% | ~ | ~ | 6/6 | 0.0492 / 1 / 1 |",
		"| example/internal/tui / New/enable=true | — | 2 ms | — | — | — | 0/1 | — / — / — |",
		"Samples (base/head)",
		"Adjusted p (time/B/allocs)",
		"Holm",
		"12 comparisons",
		"insufficient evidence",
		"do not establish that the PR caused a change",
	} {
		if !strings.Contains(details, want) {
			t.Errorf("details missing %q in:\n%s", want, details)
		}
	}
	for _, forbidden := range []string{"regression", "improvement", "no significant change"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("report should not claim %q:\n%s", forbidden, got)
		}
	}
}

// spec: benchmark-reporting/weak-evidence-is-not-promoted
func TestReportCorrectsAcrossAllComparableMetrics(t *testing.T) {
	var base, head strings.Builder
	base.WriteString("pkg: test\n")
	head.WriteString("pkg: test\n")
	for i := range 10 {
		name := fmt.Sprintf("Separated%d", i)
		base.WriteString(lines(name, 100, 101, 102, 103, 104, 105))
		head.WriteString(lines(name, 200, 201, 202, 203, 204, 205))
	}
	var out bytes.Buffer
	if err := report(strings.NewReader(base.String()), strings.NewReader(head.String()), &out); err != nil {
		t.Fatal(err)
	}
	summary, details, _ := strings.Cut(out.String(), "<details>")
	if !strings.Contains(summary, "**0 notable increases, 0 notable decreases** across 10 compared benchmarks") || strings.Contains(summary, "### Notable") {
		t.Fatalf("uncorrected differences promoted:\n%s", summary)
	}
	for _, want := range []string{"30 comparisons", "6/6 | 0.152 / 1 / 1", "insufficient evidence"} {
		if !strings.Contains(details, want) {
			t.Errorf("missing corrected evidence %q:\n%s", want, details)
		}
	}
}

// spec: benchmark-reporting/mixed-metric-directions-remain-visible
func TestReportCountsBothDirectionsForMixedMetrics(t *testing.T) {
	base := "pkg: test\n" + strings.Repeat("BenchmarkMixed-2 10 100 ns/op 200 B/op 10 allocs/op\n", 12)
	head := "pkg: test\n" + strings.Repeat("BenchmarkMixed-2 10 200 ns/op 100 B/op 10 allocs/op\n", 12)
	var out bytes.Buffer
	if err := report(strings.NewReader(base), strings.NewReader(head), &out); err != nil {
		t.Fatal(err)
	}
	summary, _, _ := strings.Cut(out.String(), "<details>")
	for _, want := range []string{"**1 notable increase, 1 notable decrease** across 1 compared benchmark", "**+100.0%**", "**-50.0%**"} {
		if !strings.Contains(summary, want) {
			t.Errorf("missing mixed direction %q:\n%s", want, summary)
		}
	}
}

func TestAdjustPValues(t *testing.T) {
	cases := []struct {
		name      string
		raw, want []float64
	}{
		{"unsorted cumulative maximum", []float64{0.04, 0.01, 0.03, 0.2}, []float64{0.09, 0.04, 0.09, 0.2}},
		{"ties and cap", []float64{0.03, 0.03, 0.8}, []float64{0.09, 0.09, 0.8}},
		{"cap at one", []float64{0.6, 0.7}, []float64{1, 1}},
		{"single", []float64{0.02}, []float64{0.02}},
		{"empty", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows := make([]comparison, len(tc.raw)+1)
			for i, p := range tc.raw {
				rows[i].time = delta{p: p, ok: true}
			}
			if got := adjustPValues(rows); got != len(tc.raw) {
				t.Fatalf("family size=%d, want %d", got, len(tc.raw))
			}
			for i, want := range tc.want {
				if got := rows[i].time.adjustedP; math.Abs(got-want) > 1e-12 {
					t.Errorf("test %d: adjusted p=%g, want %g", i, got, want)
				}
				if rows[i].time.p != tc.raw[i] {
					t.Errorf("raw p changed at %d", i)
				}
			}
		})
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
