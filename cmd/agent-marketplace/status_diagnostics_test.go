package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

func TestStatusDisplaysMaterializationDiagnostics(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	backing := []errorRow{{Message: "existing failure"}, {Message: "untouched"}}
	view := statusView{errorRows: backing[:1], dryRunReport: &materialize.Report{Diags: []diag.Diagnostic{{Code: "materialize.devin.aggregate-write", Severity: diag.SeverityError, Target: "devin", Resource: "", Message: "fixture\x1b[31m failure", Source: diag.SourceRef{File: "mcp_config.json"}}, {Code: "fixture.warning", Severity: diag.SeverityWarning, Message: "warning"}}}}
	original := append([]diag.Diagnostic(nil), view.dryRunReport.Diags...)
	var first bytes.Buffer
	if err := writeStatusSections(&first, view); err != nil {
		t.Fatal(err)
	}
	var second bytes.Buffer
	if err := writeStatusSections(&second, view); err != nil {
		t.Fatal(err)
	}
	if first.String() != second.String() {
		t.Fatalf("status rendering changed: %q vs %q", first.String(), second.String())
	}
	for _, want := range []string{"ERRORS", "existing failure", "materialize.devin.aggregate-write", "fixture"} {
		if !strings.Contains(first.String(), want) {
			t.Fatalf("missing %q: %s", want, first.String())
		}
	}
	if strings.ContainsAny(first.String(), "\x1b") || strings.Contains(first.String(), "fixture.warning") {
		t.Fatalf("unsafe or warning diagnostic rendered: %q", first.String())
	}
	if len(backing) != 2 || backing[1].Message != "untouched" || !reflect.DeepEqual(original, view.dryRunReport.Diags) {
		t.Fatal("status rendering mutated source data")
	}
	var clean bytes.Buffer
	if err := writeStatusSections(&clean, statusView{dryRunReport: &materialize.Report{}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(clean.String(), "In sync") || strings.Contains(clean.String(), "ERRORS") {
		t.Fatalf("clean status wrong: %q", clean.String())
	}
}

func TestStatusSortsMaterializationDiagnostics(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	var previous string
	for _, codes := range [][]string{{"zzz.fixture", "aaa.fixture"}, {"aaa.fixture", "zzz.fixture"}} {
		view := statusView{errorRows: []errorRow{{Message: "middle existing failure"}}, dryRunReport: &materialize.Report{}}
		for _, code := range codes {
			view.dryRunReport.Diags = append(view.dryRunReport.Diags, diag.Diagnostic{Code: code, Severity: diag.SeverityError, Message: "fixture failure"})
		}
		var out bytes.Buffer
		if err := writeStatusSections(&out, view); err != nil {
			t.Fatal(err)
		}
		text := out.String()
		a, m, z := strings.Index(text, "aaa.fixture:"), strings.Index(text, "middle existing failure"), strings.Index(text, "zzz.fixture:")
		if a < 0 || m < a || z < m {
			t.Fatalf("errors not alphabetically sorted: %s", text)
		}
		if previous != "" && previous != text {
			t.Fatal("diagnostic order changed rendered status")
		}
		previous = text
	}
}
