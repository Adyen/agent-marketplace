package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

func terminalDiagnosticFixture() []error {
	return []error{
		testRemediableResourceWarning{testResourceWarning: testResourceWarning{resource: "FIRST\x1b[31m", message: "MESSAGE\x1b[31m", dedupKey: "same"}, remediation: "REPAIR\x1b[31m"},
		testRemediableResourceWarning{testResourceWarning: testResourceWarning{resource: "SECOND\x1b[31m", message: "MESSAGE\x1b[31m", dedupKey: "same"}, remediation: "REPAIR\x1b[31m"},
	}
}

func TestCLIWarningDataIsTerminalSafe(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []struct {
		name  string
		write func(io.Writer) error
	}{
		{"grouped", func(w io.Writer) error { return writeGroupedWarnings(w, terminalDiagnosticFixture(), true) }},
		{"single sync group", func(w io.Writer) error { return writeSyncIssues(w, terminalDiagnosticFixture(), true, "warning:") }},
		{"status", func(w io.Writer) error { return writeStatusWarnings(w, terminalDiagnosticFixture()) }},
		{"multiple sync groups", func(w io.Writer) error {
			issues := terminalDiagnosticFixture()
			issues = append(issues, testResourceWarning{resource: "OTHER\x1b[31m", message: "OTHER MESSAGE\x1b[31m", dedupKey: "other"})
			return writeSyncIssues(w, issues, true, "warning:")
		}},
		{"diagnostic subgroups", func(w io.Writer) error {
			var issues []error
			for _, name := range []string{"FIRST", "SECOND"} {
				issues = append(issues, pipeline.PromoteDiagnostic(diag.Diagnostic{Code: "fixture.control", Resource: name + "\x1b[31m", Message: name + " MESSAGE\x1b[31m", Hint: "REPAIR\x1b[31m"}))
			}
			return writeSyncIssues(w, issues, true, "warning:")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := tc.write(&out); err != nil {
				t.Fatal(err)
			}
			if strings.ContainsRune(out.String(), '\x1b') {
				t.Fatalf("unsafe warning data: %q", out.String())
			}
			want := `\x1b[31m`
			if tc.name == "diagnostic subgroups" {
				want = "FIRST MESSAGE [31m"
			}
			if !strings.Contains(out.String(), want) {
				t.Fatalf("warning content lost: %q", out.String())
			}
		})
	}
}

func TestTelemetryStatusEscapesStoredFailure(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	setupTelemetryHome(t)
	state, err := telemetry.LoadAndResolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(state.StateRoot(), "last-delivery-failure")
	failure := telemetry.DeliveryFailure{Time: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Error: "SERVER\x1b[31m\r\n\u202e"}
	raw, err := json.Marshal(failure)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	output, err := executeCommand("telemetry", "status")
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(output, "\x1b\r\u202e") || !strings.Contains(output, `SERVER\x1b[31m\r\n\u202e`) {
		t.Fatalf("unsafe or missing stored failure: %q", output)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatal("display rewrote durable failure")
	}
}
