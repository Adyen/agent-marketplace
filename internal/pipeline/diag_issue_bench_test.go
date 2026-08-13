package pipeline

import (
	"fmt"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// BenchmarkNewDiagIssue guards the per-diagnostic cost of promoting a
// materialize diagnostic into an error value. Every `sync` walks
// materialize.Report.Diags once, so this runs on a user-visible latency path;
// bead agent-marketplace-oc2r made the promotion do string formatting for the
// resource and source suffix, and this is the regression guard for it.
// One b.N iteration promotes a whole report's worth of diagnostics, so the
// headline ns/op is per REPORT. ns/diagnostic is reported alongside it: the
// claim this guards is a per-diagnostic cost, and a reader of `make bench` must
// not have to know the batch size to check it.
func BenchmarkNewDiagIssue(b *testing.B) {
	const perReport = 64
	diags := benchDiagnostics(perReport)
	b.ReportAllocs()
	for b.Loop() {
		for _, d := range diags {
			issue := newDiagIssue(d)
			_ = issue.Error()
			_ = issue.DedupKey()
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*perReport), "ns/diagnostic")
}

func benchDiagnostics(n int) []diag.Diagnostic {
	diags := make([]diag.Diagnostic, 0, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("skill-%d", i)
		diags = append(diags, diag.Diagnostic{
			Code:     diag.CodeSkillDevinToolUnknown,
			Severity: diag.SeverityError,
			Target:   "devin",
			Resource: name,
			Message:  "an allowed-tools entry carries a scoped tool specifier, which has no Devin equivalent; artifact suppressed",
			Hint:     "Devin's allowed tools (read, edit, grep, glob, exec) cannot be scoped to a command; remove the specifier or drop the explicit allowed-tools restriction",
			Source:   diag.SourceRef{File: "skills/" + name + "/SKILL.md", Line: 1},
		})
	}
	return diags
}
