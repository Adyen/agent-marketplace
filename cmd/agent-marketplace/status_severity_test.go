package main

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// TestBuildStatusRowsOnlyErrorsMarkNotMaterialized pins `status` behaviour:
// diag.SeverityError is the ZERO value of diag.Severity, so an ordinal
// comparison used to flag every resource-attached diagnostic — including
// lossy-but-successful warnings — as "error: not materialized". Only an
// error-severity diagnostic may do that.
func TestBuildStatusRowsOnlyErrorsMarkNotMaterialized(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	const skillPath = "/cache/reg/skills/some-skill"

	tests := []struct {
		name       string
		severity   diag.Severity
		wantTarget string
	}{
		{name: "warning keeps the materialized path", severity: diag.SeverityWarning, wantTarget: skillPath},
		{name: "info keeps the materialized path", severity: diag.SeverityInfo, wantTarget: skillPath},
		{name: "error reports not materialized", severity: diag.SeverityError, wantTarget: "error: not materialized"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := []resolver.ResolvedArtifact{{
				Entry: catalog.Entry{
					Kind: ir.KindSkill,
					Name: "some-skill",
					Path: skillPath,
				},
				RegistryURL: "git@example.com:org/reg.git",
				Ref:         "main",
				Source:      config.RepoCommitted,
			}}
			report := &materialize.Report{
				Diags: []diag.Diagnostic{{
					Code:     diag.CodeSubagentDevinEffortDropped,
					Severity: tt.severity,
					Resource: "some-skill",
					Message:  "lossy but successful",
				}},
			}

			rows := buildStatusRows(t.TempDir(), resolved, map[string]string{}, report)
			if len(rows) != 1 {
				t.Fatalf("buildStatusRows returned %d rows, want 1", len(rows))
			}
			if rows[0].Target != tt.wantTarget {
				t.Fatalf("row Target for a %v diagnostic = %q, want %q", tt.severity, rows[0].Target, tt.wantTarget)
			}
		})
	}
}

// spec: status-reporting/status-marks-only-the-failed-resource
func TestBuildStatusRowsSameNameOtherScope(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	const name = "deploy"
	resolved := []resolver.ResolvedArtifact{
		{Entry: catalog.Entry{Kind: ir.KindSkill, Name: name, Path: "/repo/deploy"}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}},
		{Entry: catalog.Entry{Kind: ir.KindSkill, Name: name, Path: "/user/deploy"}, Source: config.UserPersonal, Harnesses: []hcc.Target{hcc.TargetFactory}},
	}
	report := &materialize.Report{
		Diags:            []diag.Diagnostic{{Code: "materialize.compile.error", Severity: diag.SeverityError, Target: "factory", Resource: name}},
		BlockedResources: []materialize.BlockedResource{{Kind: ir.KindSkill, Name: name, Target: hcc.TargetFactory, MachineScope: false}},
	}
	rows := buildStatusRows(t.TempDir(), resolved, nil, report)
	bySource := make(map[config.Source]string)
	for _, row := range rows {
		bySource[row.Source] = row.Target
	}
	if len(rows) != 2 || bySource[config.RepoCommitted] != "error: not materialized" || bySource[config.UserPersonal] != "/user/deploy" {
		t.Fatalf("scope attribution: %+v", rows)
	}
}

// spec: status-reporting/same-named-resource-of-another-kind-is-not-marked
func TestBuildStatusRowsSameNameOtherKind(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	const name = "deploy"
	resolved := []resolver.ResolvedArtifact{
		{Entry: catalog.Entry{Kind: ir.KindSkill, Name: name, Path: "/repo/skill"}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}},
		{Entry: catalog.Entry{Kind: ir.KindCommand, Name: name, Path: "/repo/command"}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}},
	}
	report := &materialize.Report{
		Diags:            []diag.Diagnostic{{Code: "materialize.compile.error", Severity: diag.SeverityError, Target: "factory", Resource: name}},
		BlockedResources: []materialize.BlockedResource{{Kind: ir.KindSkill, Name: name, Target: hcc.TargetFactory, MachineScope: false}},
	}
	rows := buildStatusRows(t.TempDir(), resolved, nil, report)
	if len(rows) != 2 || rows[0].Target != "/repo/command" || rows[1].Target != "error: not materialized" {
		t.Fatalf("kind attribution: %+v", rows)
	}
}

func TestBuildStatusRowsAmbiguousDiagnosticKeepsPaths(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	resolved := []resolver.ResolvedArtifact{
		{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "deploy", Path: "/repo/skill"}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}},
		{Entry: catalog.Entry{Kind: ir.KindCommand, Name: "deploy", Path: "/repo/command"}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}},
	}
	report := &materialize.Report{Diags: []diag.Diagnostic{{Code: "materialize.write.error", Severity: diag.SeverityError, Target: "factory", Resource: "deploy"}}}
	rows := buildStatusRows(t.TempDir(), resolved, nil, report)
	if len(rows) != 2 || rows[0].Target != "/repo/command" || rows[1].Target != "/repo/skill" {
		t.Fatalf("ambiguous diagnostic attributed to a row: %+v", rows)
	}
}

func TestBuildStatusRowsMatchesInstalledNameAndHarness(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	resolved := []resolver.ResolvedArtifact{
		{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "canonical", Path: "/repo/factory"}, IsMarketplace: true, InstalledName: "deploy", Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}},
		{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "canonical", Path: "/repo/claude"}, IsMarketplace: true, InstalledName: "deploy", Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetClaude}},
	}
	report := &materialize.Report{BlockedResources: []materialize.BlockedResource{{Kind: ir.KindSkill, Name: "deploy", Target: hcc.TargetFactory}}}
	rows := buildStatusRows(t.TempDir(), resolved, nil, report)
	byHarness := make(map[string]string)
	for _, row := range rows {
		byHarness[row.Harness] = row.Target
	}
	if len(rows) != 2 || byHarness["claude"] != "/repo/claude" || byHarness["factory"] != "error: not materialized" {
		t.Fatalf("installed name or harness attribution: %+v", rows)
	}
}

// spec: resource-selection/required-skill-keeps-a-native-path-a-same-name-command-shares
func TestBuildStatusRowsOmitTargetsLostToASharedPath(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	all := []hcc.Target{hcc.TargetDevin, hcc.TargetCodex, hcc.TargetClaude}
	resolved := []resolver.ResolvedArtifact{
		{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "audit", Path: "/org/audit"}, RegistryURL: "https://example.invalid/org", Source: config.Organization, Required: true, Harnesses: all},
		{Entry: catalog.Entry{Kind: ir.KindCommand, Name: "audit", Path: "/user/audit"}, RegistryURL: "https://example.invalid/user", Source: config.UserPersonal, Harnesses: all},
		{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "check", Path: "/org/check"}, RegistryURL: "https://example.invalid/org", Source: config.Organization, Required: true, Harnesses: []hcc.Target{hcc.TargetDevin}},
		{Entry: catalog.Entry{Kind: ir.KindCommand, Name: "check", Path: "/user/check"}, RegistryURL: "https://example.invalid/user", Source: config.UserPersonal, Harnesses: []hcc.Target{hcc.TargetDevin}},
	}
	got := make(map[string][2]string)
	for _, row := range buildStatusRows(t.TempDir(), resolved, nil, nil) {
		got[row.Kind+"/"+row.Name] = [2]string{row.Harness, row.Target}
	}
	want := map[string][2]string{
		"skill/audit":   {"Codex,claude,devin", "/org/audit"},
		"command/audit": {"claude", "/user/audit"},
		"skill/check":   {"devin", "/org/check"},
		"command/check": {"-", "not materialized: shared path"},
	}
	if len(got) != len(want) {
		t.Fatalf("rows = %v", got)
	}
	for key, row := range want {
		if got[key] != row {
			t.Errorf("%s = %v, want %v", key, got[key], row)
		}
	}
}
