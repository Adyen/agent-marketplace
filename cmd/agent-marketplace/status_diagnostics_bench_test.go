package main

import (
	"io"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

func BenchmarkStatusReportDiagnostics(b *testing.B) {
	for _, sample := range []struct {
		name   string
		report *materialize.Report
	}{{"clean", &materialize.Report{}}, {"errors", &materialize.Report{Diags: []diag.Diagnostic{{Code: "materialize.devin.aggregate-write", Severity: diag.SeverityError, Target: "devin", Message: "fixture aggregate cannot be read"}}}}} {
		b.Run(sample.name, func(b *testing.B) {
			view := statusView{dryRunReport: sample.report}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if err := writeStatusSections(io.Discard, view); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
