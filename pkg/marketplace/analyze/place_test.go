package analyze

import (
	"context"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/targetprofile"
)

func TestAnalyzePlaceFilesReportsExecutableMode(t *testing.T) {
	report, err := AnalyzePlaceFiles(context.Background(), []targetprofile.PackageFile{
		{SourcePath: "source.txt", DestinationPath: "mods/source.txt", Mode: 0o644, Bytes: []byte("plain text")},
		{SourcePath: "run.txt", DestinationPath: "mods/run.txt", Mode: 0o755, Bytes: []byte("plain text")},
	}, DefaultOptions())
	if err != nil {
		t.Fatalf("AnalyzePlaceFiles: %v", err)
	}
	if report.Rejected {
		t.Fatalf("executable mode should warn under public policy: %+v", report.Findings)
	}
	if len(report.Findings) != 1 || report.Findings[0].Code != FindingExecutableScript ||
		report.Findings[0].PolicyOutcome != OutcomeWarn || report.Findings[0].SourceRange == nil ||
		report.Findings[0].SourceRange.File != "run.txt" {
		t.Fatalf("findings = %+v, want one executable-mode warning at run.txt", report.Findings)
	}
}
