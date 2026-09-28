package pipeline

import (
	"errors"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func countIssues(issues []InspectionIssue, severity IssueSeverity, category IssueCategory) int {
	n := 0
	for _, issue := range issues {
		if issue.Severity == severity && issue.Category == category {
			n++
		}
	}
	return n
}

func issueMessages(issues []InspectionIssue) []string {
	msgs := make([]string, 0, len(issues))
	for _, issue := range issues {
		msgs = append(msgs, issue.Err.Error())
	}
	return msgs
}

func TestInspectionIssuesClassifyEverySource(t *testing.T) {
	softNotFound := &resolver.SelectedNotFoundError{Kind: ir.KindSkill, Name: "missing-skill", URL: "https://example.com/reg.git"}
	hardResolve := errors.New("resolve blew up")
	configErr := errors.New("bad config")
	catalogErr := errors.New("catalog failed")
	genericErr := errors.New("workspace validation failed")
	resolved := ResolveResult{
		Warnings:     []error{errors.New("config warning")},
		ConfigErrors: []error{configErr},
		CatalogErrors: []error{
			&catalog.OverlappingEntryWarning{Dir: "reg", FirstURL: "https://example.com/reg.git", SecondURL: "https://example.com/reg.git", SecondPath: "sub"},
			catalogErr,
		},
		ResolveErrors: []error{softNotFound, hardResolve},
		Errors:        []error{configErr, catalogErr, hardResolve, genericErr},
		CatalogLoad: catalog.LoadResult{Malformed: []skill.MalformedSkill{
			{DirName: "broken", Path: "skills/broken", Error: errors.New("bad frontmatter")},
		}},
	}
	report := &materialize.Report{Diags: []diag.Diagnostic{
		{Code: "compile.warn", Severity: diag.SeverityWarning, Resource: "alpha", Message: "lossy"},
		{Code: "compile.err", Severity: diag.SeverityError, Resource: "alpha", Message: "broken"},
		{Code: "materialize.root", Severity: diag.SeverityError, Message: "root-level failure"},
	}}
	matErr := errors.New("dry run failed")

	inspection := buildInspection(resolved, report, matErr)
	issues := inspection.Issues

	if countIssues(issues, IssueWarning, IssueConfig) != 1 {
		t.Fatalf("config warnings = %d, want 1\n%v", countIssues(issues, IssueWarning, IssueConfig), issueMessages(issues))
	}
	if countIssues(issues, IssueError, IssueConfig) != 1 {
		t.Fatalf("config errors = %d, want 1\n%v", countIssues(issues, IssueError, IssueConfig), issueMessages(issues))
	}
	if countIssues(issues, IssueWarning, IssueCatalog) != 2 {
		t.Fatalf("catalog warnings = %d, want 2\n%v", countIssues(issues, IssueWarning, IssueCatalog), issueMessages(issues))
	}
	if countIssues(issues, IssueError, IssueCatalog) != 1 {
		t.Fatalf("catalog errors = %d, want 1\n%v", countIssues(issues, IssueError, IssueCatalog), issueMessages(issues))
	}
	if countIssues(issues, IssueWarning, IssueResolution) != 1 {
		t.Fatalf("resolution warnings = %d, want 1\n%v", countIssues(issues, IssueWarning, IssueResolution), issueMessages(issues))
	}
	if countIssues(issues, IssueError, IssueResolution) != 2 {
		t.Fatalf("resolution errors = %d, want 2\n%v", countIssues(issues, IssueError, IssueResolution), issueMessages(issues))
	}
	if countIssues(issues, IssueWarning, IssueMaterialization) != 1 {
		t.Fatalf("materialization warnings = %d, want 1\n%v", countIssues(issues, IssueWarning, IssueMaterialization), issueMessages(issues))
	}
	if countIssues(issues, IssueError, IssueMaterialization) != 3 {
		t.Fatalf("materialization errors = %d, want 3\n%v", countIssues(issues, IssueError, IssueMaterialization), issueMessages(issues))
	}
	if inspection.Healthy() {
		t.Fatal("inspection with error-severity issues must not be healthy")
	}
}

func TestInspectionWarningOnlyRemainsSuccessful(t *testing.T) {
	resolved := ResolveResult{
		Warnings: []error{errors.New("config warning")},
		CatalogErrors: []error{
			&catalog.OverlappingEntryWarning{Dir: "reg", SecondURL: "https://example.com/reg.git", SecondPath: "sub"},
		},
		ResolveErrors: []error{
			&resolver.SelectedNotFoundError{Kind: ir.KindSkill, Name: "missing-skill", URL: "https://example.com/reg.git"},
		},
		CatalogLoad: catalog.LoadResult{Malformed: []skill.MalformedSkill{
			{DirName: "broken", Path: "skills/broken", Error: errors.New("bad frontmatter")},
		}},
	}
	report := &materialize.Report{Diags: []diag.Diagnostic{
		{Code: "compile.warn", Severity: diag.SeverityWarning, Resource: "alpha", Message: "lossy"},
	}}

	inspection := buildInspection(resolved, report, nil)
	if !inspection.Healthy() {
		t.Fatalf("warning-only inspection must stay healthy; issues: %v", issueMessages(inspection.Issues))
	}
	if len(inspection.Issues) == 0 {
		t.Fatal("healthy inspection must still preserve its warnings")
	}
}

func TestInspectionPreservesMalformedAndResourcelessIssues(t *testing.T) {
	resolved := ResolveResult{
		CatalogLoad: catalog.LoadResult{Malformed: []skill.MalformedSkill{
			{DirName: "broken", Path: "skills/broken", Error: errors.New("bad frontmatter")},
		}},
	}
	report := &materialize.Report{Diags: []diag.Diagnostic{
		{Code: "materialize.aggregate", Severity: diag.SeverityError, Message: "shared file is not an object"},
	}}

	inspection := buildInspection(resolved, report, nil)
	var malformedIssue, rootIssue *InspectionIssue
	for i := range inspection.Issues {
		msg := inspection.Issues[i].Err.Error()
		switch {
		case strings.Contains(msg, "malformed"):
			malformedIssue = &inspection.Issues[i]
		case strings.Contains(msg, "materialize.aggregate"):
			rootIssue = &inspection.Issues[i]
		}
	}
	if malformedIssue == nil {
		t.Fatalf("malformed issue missing: %v", issueMessages(inspection.Issues))
	}
	if malformedIssue.Severity != IssueWarning {
		t.Fatalf("malformed severity = %v, want warning", malformedIssue.Severity)
	}
	if !strings.Contains(malformedIssue.Err.Error(), "SKILL.md") {
		t.Fatalf("malformed issue lost its SKILL.md path: %q", malformedIssue.Err.Error())
	}
	if rootIssue == nil {
		t.Fatalf("resource-less materialize diagnostic dropped: %v", issueMessages(inspection.Issues))
	}
	if rootIssue.Severity != IssueError || rootIssue.Category != IssueMaterialization {
		t.Fatalf("resource-less diagnostic classified as %v/%v, want error/materialization", rootIssue.Severity, rootIssue.Category)
	}
	if inspection.Healthy() {
		t.Fatal("a resource-less error diagnostic must make the inspection unhealthy")
	}
}

func TestInspectionSemanticallyDeduplicates(t *testing.T) {
	resolved := ResolveResult{}
	report := &materialize.Report{Diags: []diag.Diagnostic{
		{Code: "compile.err", Severity: diag.SeverityError, Resource: "alpha", Source: diag.SourceRef{File: "skills/alpha/SKILL.md", Line: 3}, Message: "broken", Target: "claude"},
		{Code: "compile.err", Severity: diag.SeverityError, Resource: "alpha", Source: diag.SourceRef{File: "skills/alpha/SKILL.md", Line: 3}, Message: "broken", Target: "devin"},
		{Code: "compile.err", Severity: diag.SeverityError, Resource: "beta", Source: diag.SourceRef{File: "skills/beta/SKILL.md", Line: 3}, Message: "broken", Target: "claude"},
	}}

	inspection := buildInspection(resolved, report, nil)
	if countIssues(inspection.Issues, IssueError, IssueMaterialization) != 2 {
		t.Fatalf("materialization issues = %d, want 2 (alpha once, beta once)\n%v", countIssues(inspection.Issues, IssueError, IssueMaterialization), issueMessages(inspection.Issues))
	}
}
