package main

import (
	"fmt"
	"io"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

func BenchmarkWriteGroupedWarnings(b *testing.B) {
	warnings := make([]error, 100)
	for i := range warnings {
		warnings[i] = testResourceWarning{
			resource: fmt.Sprintf("resource-%03d", i),
			message:  "resource was dropped",
		}
	}

	b.ReportAllocs()
	for b.Loop() {
		if err := writeGroupedWarnings(io.Discard, warnings, false); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWriteSyncWarnings(b *testing.B) {
	warnings := make([]error, 100)
	for i := range warnings {
		warnings[i] = pipeline.PromoteDiagnostic(diag.Diagnostic{
			Code:     diag.CodeSubagentWindsurfNoPrimitive,
			Severity: diag.SeverityWarning,
			Resource: fmt.Sprintf("resource-%03d", i),
			Message:  "Windsurf has no subagent primitive; resource was dropped",
			Hint:     "Consider expressing this behavior as a Windsurf workflow instead",
			Source:   diag.SourceRef{File: fmt.Sprintf("agents/resource-%03d.md", i), Line: 1},
		})
	}

	b.ReportAllocs()
	for b.Loop() {
		if err := writeSyncWarnings(io.Discard, warnings, false, "⚠"); err != nil {
			b.Fatal(err)
		}
	}
}
