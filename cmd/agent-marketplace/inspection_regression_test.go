package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

func setupInspectionStatusRepo(t *testing.T) string {
	t.Helper()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "inspection-status.git")
	writeSkill(t, working, "valid-skill", "Valid skill", []string{"tested"})
	commitAndPushAll(t, working, "add skill")

	repoRoot := t.TempDir()
	writeConfig(t, repoRoot+"/.agents/config.yaml", `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - valid-skill
`)

	withWorkingDir(t, repoRoot)
	requireExactLocks(t)
	return repoRoot
}

func setupMalformedInspectionRepo(t *testing.T) {
	t.Helper()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "inspection-malformed.git")
	writeSkill(t, working, "valid-skill", "Valid skill", []string{"tested"})
	writeFile(t, filepath.Join(working, "skills", "broken-skill", "SKILL.md"), `---
name: broken-skill
description: [unterminated
metadata:
  tags:
    - broken
---

# Broken
`)
	commitAndPushAll(t, working, "valid and malformed skills")

	repoRoot := t.TempDir()
	writeConfig(t, repoRoot+"/.agents/config.yaml", `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - valid-skill
`)

	withWorkingDir(t, repoRoot)
	requireExactLocks(t)
}

// spec: inspection-outcomes/malformed-resource-warning
func TestSyncCheckReportsMalformedResourceAsWarning(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	setupMalformedInspectionRepo(t)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("priming sync failed: %v\n%s", err, output)
	}

	output, err := executeCommand("sync", "--check")
	if err != nil {
		t.Fatalf("warning-only malformed resource must keep sync --check successful: %v\n%s", err, output)
	}
	for _, needle := range []string{"warning:", "broken-skill", filepath.Join("skills", "broken-skill", "SKILL.md")} {
		if !strings.Contains(output, needle) {
			t.Fatalf("malformed resource warning missing %q from sync --check output:\n%s", needle, output)
		}
	}
	if strings.Contains(output, filepath.Join("skills", "broken-skill", "SKILL.md", "SKILL.md")) {
		t.Fatalf("sync --check appended a second SKILL.md to a file path:\n%s", output)
	}
}

// spec: inspection-outcomes/materialization-issue-with-or-without-a-resource
func TestStatusPreservesMaterializationIssueShapes(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	setupInspectionStatusRepo(t)

	previous := runStatusMaterialize
	runStatusMaterialize = func(ctx context.Context, root string, artifacts []resolver.ResolvedArtifact, opts ...materialize.RunOption) (*materialize.Report, error) {
		return &materialize.Report{Diags: []diag.Diagnostic{
			{
				Code:     "materialize.test.named",
				Severity: diag.SeverityWarning,
				Resource: "valid-skill",
				Message:  "named resource issue",
			},
			{
				Code:     "materialize.test.path",
				Severity: diag.SeverityWarning,
				Resource: "skills/valid-skill/SKILL.md",
				Message:  "path-shaped resource issue",
			},
			{
				Code:     "materialize.test.root",
				Severity: diag.SeverityWarning,
				Source:   diag.SourceRef{File: "settings.json"},
				Message:  "root-level issue",
			},
			{
				Code:     "materialize.test.operation",
				Severity: diag.SeverityWarning,
				Message:  "operation-only issue",
			},
		}}, nil
	}
	t.Cleanup(func() { runStatusMaterialize = previous })

	output, err := executeCommand("status")
	if err != nil {
		t.Fatalf("expected status to succeed on warning-only issue shapes: %v\n%s", err, output)
	}
	for _, needle := range []string{
		`resource "valid-skill"`,
		`resource "skills/valid-skill/SKILL.md"`,
		"settings.json",
		"materialize.test.operation",
	} {
		if !strings.Contains(output, needle) {
			t.Fatalf("materialization issue shape %q missing from status output:\n%s", needle, output)
		}
	}
}

// spec: inspection-outcomes/warning-only-inspection
func TestStatusReportsMaterializationWarningsInWarningsSection(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	setupInspectionStatusRepo(t)

	previous := runStatusMaterialize
	runStatusMaterialize = func(ctx context.Context, root string, artifacts []resolver.ResolvedArtifact, opts ...materialize.RunOption) (*materialize.Report, error) {
		return &materialize.Report{Diags: []diag.Diagnostic{{
			Code:     "materialize.test.warning",
			Severity: diag.SeverityWarning,
			Resource: "valid-skill",
			Message:  "lossy but successful",
			Hint:     "fix the field",
		}}}, nil
	}
	t.Cleanup(func() { runStatusMaterialize = previous })

	output, err := executeCommand("status")
	if err != nil {
		t.Fatalf("expected status to succeed with a warning-only diagnostic: %v\n%s", err, output)
	}
	if !strings.Contains(output, "WARNINGS") || !strings.Contains(output, "materialize.test.warning") {
		t.Fatalf("materialization warning diagnostic missing from WARNINGS:\n%s", output)
	}
	if !strings.Contains(output, "fix the field") {
		t.Fatalf("materialization warning lost its remediation hint:\n%s", output)
	}
}

func TestStatusReportsMaterializationErrorsInErrorsSection(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	setupInspectionStatusRepo(t)

	previous := runStatusMaterialize
	runStatusMaterialize = func(ctx context.Context, root string, artifacts []resolver.ResolvedArtifact, opts ...materialize.RunOption) (*materialize.Report, error) {
		return &materialize.Report{Diags: []diag.Diagnostic{{
			Code:     "materialize.test.error",
			Severity: diag.SeverityError,
			Resource: "valid-skill",
			Message:  "cannot compile",
		}}}, nil
	}
	t.Cleanup(func() { runStatusMaterialize = previous })

	output, err := executeCommand("status")
	if err == nil {
		t.Fatalf("expected status to report non-success on an error-severity materialization diagnostic:\n%s", output)
	}
	for _, needle := range []string{"ERRORS", "materialize.test.error", `resource "valid-skill"`} {
		if !strings.Contains(output, needle) {
			t.Fatalf("expected status output to contain %q:\n%s", needle, output)
		}
	}
}

func TestStatusPreservesResourcelessMaterializationIssue(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	setupInspectionStatusRepo(t)

	previous := runStatusMaterialize
	runStatusMaterialize = func(ctx context.Context, root string, artifacts []resolver.ResolvedArtifact, opts ...materialize.RunOption) (*materialize.Report, error) {
		return &materialize.Report{Diags: []diag.Diagnostic{{
			Code:     "materialize.test.aggregate",
			Severity: diag.SeverityError,
			Message:  "shared aggregate file is not an object",
		}}}, nil
	}
	t.Cleanup(func() { runStatusMaterialize = previous })

	output, err := executeCommand("status")
	if err == nil {
		t.Fatalf("expected status to report non-success on a resource-less error diagnostic:\n%s", output)
	}
	if !strings.Contains(output, "materialize.test.aggregate") {
		t.Fatalf("resource-less diagnostic missing from ERRORS:\n%s", output)
	}
}

func TestSyncCheckReportsMaterializationWarningDiagnostics(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	setupInspectionStatusRepo(t)

	previous := runSyncCheckMaterialize
	runSyncCheckMaterialize = func(ctx context.Context, root string, artifacts []resolver.ResolvedArtifact, opts ...materialize.RunOption) (*materialize.Report, error) {
		return &materialize.Report{Diags: []diag.Diagnostic{{
			Code:     "materialize.test.warning",
			Severity: diag.SeverityWarning,
			Resource: "valid-skill",
			Message:  "lossy but successful",
			Hint:     "fix the field",
		}}}, nil
	}
	t.Cleanup(func() { runSyncCheckMaterialize = previous })

	output, err := executeCommand("sync", "--check")
	if err != nil {
		t.Fatalf("warning-only diagnostics must keep sync --check successful: %v\n%s", err, output)
	}
	if !strings.Contains(output, "materialize.test.warning") {
		t.Fatalf("materialization warning missing from sync --check output:\n%s", output)
	}
	if !strings.Contains(output, "fix the field") {
		t.Fatalf("materialization warning lost its remediation hint:\n%s", output)
	}
}

// spec: inspection-outcomes/materialization-dry-run-failure
func TestSyncCheckSurfacesDryRunFailureAsInspectionError(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	setupInspectionStatusRepo(t)

	previous := runSyncCheckMaterialize
	runSyncCheckMaterialize = func(ctx context.Context, root string, artifacts []resolver.ResolvedArtifact, opts ...materialize.RunOption) (*materialize.Report, error) {
		return nil, context.DeadlineExceeded
	}
	t.Cleanup(func() { runSyncCheckMaterialize = previous })

	output, err := executeCommand("sync", "--check")
	if err == nil {
		t.Fatalf("expected sync --check to fail on a dry-run error:\n%s", output)
	}
	if !strings.Contains(output, "error:") {
		t.Fatalf("dry-run failure was never named in sync --check output:\n%s", output)
	}
}
