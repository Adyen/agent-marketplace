package main

import (
	"io"
	"testing"
)

func BenchmarkCLIWarningEscaping(b *testing.B) {
	issues := terminalDiagnosticFixture()
	b.ReportAllocs()
	for b.Loop() {
		if err := writeSyncIssues(io.Discard, issues, true, "warning:"); err != nil {
			b.Fatal(err)
		}
	}
}
