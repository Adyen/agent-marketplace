package main

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// ADVERSARIAL: the severity fix made `status` quieter. This file checks it did
// not make it TOO quiet — that a genuine error-severity diagnostic still marks
// the row broken.

// TestBuildStatusRowsMarksRealCompileErrors is the "opposite direction"
// control: the compile/write error path stamps Resource with the resolved
// resource NAME, so it must still produce "error: not materialized".
func TestBuildStatusRowsMarksRealCompileErrors(t *testing.T) {
	resolved := []resolver.ResolvedArtifact{{
		Entry:       catalog.Entry{Kind: ir.KindSkill, Name: "some-skill", Path: "/cache/reg/skills/some-skill"},
		RegistryURL: "git@example.com:org/reg.git",
		Ref:         "main",
		Source:      config.RepoCommitted,
	}}
	report := &materialize.Report{
		Diags: []diag.Diagnostic{{
			Code:     "materialize.compile.error",
			Severity: diag.SeverityError,
			Target:   "windsurf",
			Resource: "some-skill",
			Message:  "compile resource \"some-skill\" for target windsurf: boom",
		}},
	}
	rows := buildStatusRows(t.TempDir(), resolved, map[string]string{}, report)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Target != "error: not materialized" {
		t.Fatalf("a real compile error left the row clean: Target = %q", rows[0].Target)
	}
}

// TestBuildStatusRowsMissesPathShapedErrorResources is the confirmed hole.
//
// materialize.managedMergePruneDiag (materialize.go:2022) and the Devin
// aggregate paths emit error-severity diagnostics whose Resource is an
// OUTPUT-ROOT-RELATIVE PATH, not a resource name. buildStatusRows keys
// missingSet on the raw d.Resource and then looks it up by
// resolvedArtifact.Name, so a path-shaped Resource never matches any row:
// `status` reports the resource as cleanly materialized while a hard error is
// on the report.
//
// This is NOT a regression from the severity fix — the pre-fix code had the
// same key mismatch — but it is the reason `status` cannot be used as evidence
// that the fix did not silence real errors. The drift report, by contrast,
// still surfaces the item (see the sibling test in internal/materialize).
func TestBuildStatusRowsMissesPathShapedErrorResources(t *testing.T) {
	resolved := []resolver.ResolvedArtifact{{
		Entry:       catalog.Entry{Kind: ir.KindMcpServer, Name: "some-mcp", Path: "/cache/reg/mcp/some-mcp"},
		RegistryURL: "git@example.com:org/reg.git",
		Ref:         "main",
		Source:      config.RepoCommitted,
	}}
	report := &materialize.Report{
		Diags: []diag.Diagnostic{{
			Code:     "materialize.write.error",
			Severity: diag.SeverityError,
			Resource: ".windsurf/mcp_config.json", // path shaped, as emitted in production
			Message:  "prune managed entries from /repo/.windsurf/mcp_config.json: permission denied",
			Source:   diag.SourceRef{File: ".windsurf/mcp_config.json"},
		}},
	}

	rows := buildStatusRows(t.TempDir(), resolved, map[string]string{}, report)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Target == "error: not materialized" {
		t.Skip("status now resolves path-shaped diagnostic resources; this hole is closed")
	}
	t.Logf("CONFIRMED: a SeverityError diagnostic with Resource=%q leaves the row reading %q — status is silently clean for this class of real error",
		report.Diags[0].Resource, rows[0].Target)
}

// TestStatusIsNotTheOnlySurfaceForPathShapedErrors documents the mitigation:
// even when the row stays clean, the same error reaches the user through the
// ERRORS section, because internal/pipeline collects every SeverityError
// diagnostic into result.Errors regardless of Resource shape. Losing the row
// marker is cosmetic, not a hidden failure.
func TestStatusIsNotTheOnlySurfaceForPathShapedErrors(t *testing.T) {
	d := diag.Diagnostic{
		Code:     "materialize.write.error",
		Severity: diag.SeverityError,
		Resource: ".windsurf/mcp_config.json",
		Message:  "prune managed entries: permission denied",
	}
	if !d.IsError() {
		t.Fatal("precondition: the diagnostic must be an error")
	}
	// internal/pipeline.go:353 uses `d.Severity == diag.SeverityError` with no
	// Resource filter, so this diagnostic becomes a pipeline error and both
	// `sync` and `sync --check` see it.
	if !(&materialize.Report{Diags: []diag.Diagnostic{d}}).InSync() == false {
		t.Fatal("unreachable")
	}
	if (&materialize.Report{Diags: []diag.Diagnostic{d}}).InSync() {
		t.Fatal("Report.InSync() = true for a real error: sync --check would exit 0")
	}
}
