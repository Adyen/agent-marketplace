package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestReportActionsWithMediansAndMissingBaselines(t *testing.T) {
	base := "pkg: example/internal/tui\nBenchmarkStartup-2 10 1000000 ns/op 100 B/op 10 allocs/op\nBenchmarkStartup-2 10 3000000 ns/op 300 B/op 30 allocs/op\nBenchmarkRemoved-2 10 1 ns/op 0 B/op 0 allocs/op\n"
	head := "pkg: example/internal/tui\nBenchmarkStartup-2 20 1000000 ns/op 100 B/op 10 allocs/op\nBenchmarkStartup-2 20 1000000 ns/op 100 B/op 10 allocs/op\nBenchmarkNew/enable=true-2 1 2e6 ns/op 200 B/op 20 allocs/op\npkg: example/internal/config\nBenchmarkStartup-2 1 10 ns/op 0 B/op 0 allocs/op\n"
	var out bytes.Buffer
	if err := report(strings.NewReader(base), strings.NewReader(head), &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"| Action | Base ms/op | Head ms/op | Time change | Base → head B/op | Base → head allocs/op | Samples base/head |",
		"| example/internal/tui / Startup | 2.000 | 1.000 | -50.0% | 200 → 100 | 20 → 10 | 2/2 |",
		"| example/internal/tui / New/enable=true | — | 2.000 | new | — → 200 | — → 20 | 0/1 |",
		"| example/internal/tui / Removed | 0.000 | — | removed | 0 → — | 0 → — | 1/0 |",
		"example/internal/config / Startup",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
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
