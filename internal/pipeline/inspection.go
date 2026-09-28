package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
)

type IssueSeverity uint8

const (
	IssueError IssueSeverity = iota
	IssueWarning
)

type IssueCategory string

const (
	IssueConfig          IssueCategory = "config"
	IssueCatalog         IssueCategory = "catalog"
	IssueResolution      IssueCategory = "resolution"
	IssueMaterialization IssueCategory = "materialization"
)

type InspectionIssue struct {
	Severity IssueSeverity
	Category IssueCategory
	Err      error
}

func (i InspectionIssue) Remediation() string {
	var remediable resolver.Remediable
	if !errors.As(i.Err, &remediable) {
		return ""
	}
	return remediable.Remediation()
}

type InspectionResult struct {
	ResolveResult
	Report         *materialize.Report
	MaterializeErr error
	Issues         []InspectionIssue
}

func (r InspectionResult) IssuesOf(severity IssueSeverity, category IssueCategory) []InspectionIssue {
	var out []InspectionIssue
	for _, issue := range r.Issues {
		if issue.Severity == severity && issue.Category == category {
			out = append(out, issue)
		}
	}
	return out
}

func (r InspectionResult) Healthy() bool {
	for _, issue := range r.Issues {
		if issue.Severity == IssueError {
			return false
		}
	}
	return true
}

type InspectOptions struct {
	Resolve                        ResolveOptions
	DryRun                         func(ctx context.Context, root string, artifacts []resolver.ResolvedArtifact, opts ...materialize.RunOption) (*materialize.Report, error)
	SkipMaterializeOnResolveErrors bool
}

func Inspect(ctx context.Context, opts InspectOptions) (InspectionResult, func()) {
	resolved, release := resolve(ctx, opts.Resolve)
	var report *materialize.Report
	var matErr error
	if !skipInspectionDryRun(resolved, opts.SkipMaterializeOnResolveErrors) {
		dryRun := opts.DryRun
		if dryRun == nil {
			dryRun = materialize.Run
		}
		report, matErr = dryRun(ctx, opts.Resolve.Workspace.MaterializeRoot(), resolved.Resolved,
			materialize.WithDryRun(), materialize.WithMaterializationModes(resolved.MaterializationModes))
	}
	return buildInspection(resolved, report, matErr), release
}

func skipInspectionDryRun(resolved ResolveResult, onAnyError bool) bool {
	if len(resolved.ConfigErrors) > 0 {
		return true
	}
	if len(resolved.Errors) == 0 {
		return false
	}
	if onAnyError {
		return true
	}
	return len(resolved.CatalogErrors) == 0 && len(resolved.ResolveErrors) == 0
}

func IsSoftResolveIssue(err error) bool {
	return resolver.IsSoftSelectedNotFound(err) ||
		resolver.IsSoftSelectedMalformed(err) ||
		resolver.IsNameCollision(err) ||
		resolver.IsWarning(err)
}

func buildInspection(resolved ResolveResult, report *materialize.Report, matErr error) InspectionResult {
	issues := make([]InspectionIssue, 0, len(resolved.Warnings)+len(resolved.Errors)+len(resolved.ResolveErrors))
	add := func(severity IssueSeverity, category IssueCategory, err error) {
		if err != nil {
			issues = append(issues, InspectionIssue{Severity: severity, Category: category, Err: err})
		}
	}

	hard := make(map[string]struct{}, len(resolved.Errors))
	mark := func(err error) { hard[IssueDedupKey(err)] = struct{}{} }

	for _, err := range resolved.Warnings {
		add(IssueWarning, IssueConfig, err)
	}
	for _, err := range resolved.ConfigErrors {
		mark(err)
		add(IssueError, IssueConfig, err)
	}
	for _, err := range resolved.CatalogErrors {
		if catalog.IsOverlapWarning(err) {
			add(IssueWarning, IssueCatalog, err)
			continue
		}
		mark(err)
		add(IssueError, IssueCatalog, err)
	}
	for _, malformed := range resolved.CatalogLoad.Malformed {
		add(IssueWarning, IssueCatalog, malformedResourceError(malformed))
	}
	for _, err := range resolved.ResolveErrors {
		if IsSoftResolveIssue(err) {
			add(IssueWarning, IssueResolution, err)
			continue
		}
		mark(err)
		add(IssueError, IssueResolution, err)
	}
	for _, err := range resolved.Errors {
		if _, known := hard[IssueDedupKey(err)]; known {
			continue
		}
		add(IssueError, IssueResolution, err)
	}
	if report != nil {
		for _, d := range report.Diags {
			severity := IssueWarning
			if d.IsError() {
				severity = IssueError
			}
			add(severity, IssueMaterialization, PromoteDiagnostic(d))
		}
	}
	add(IssueError, IssueMaterialization, matErr)

	return InspectionResult{
		ResolveResult:  resolved,
		Report:         report,
		MaterializeErr: matErr,
		Issues:         dedupInspectionIssues(issues),
	}
}

func malformedResourceError(m skill.MalformedSkill) error {
	path := m.Path
	if path != "" && !strings.HasSuffix(path, ".md") {
		path += "/SKILL.md"
	}
	return fmt.Errorf("malformed skill %q (%s): %w", m.DirName, path, m.Error)
}

func IssueDedupKey(err error) string {
	if err == nil {
		return "<nil>"
	}
	type keyer interface{ DedupKey() string }
	var keyed keyer
	if errors.As(err, &keyed) {
		if key := keyed.DedupKey(); key != "" {
			return key
		}
	}
	return fmt.Sprintf("%T\x00%s", err, err.Error())
}

func dedupInspectionIssues(issues []InspectionIssue) []InspectionIssue {
	deduped := make([]InspectionIssue, 0, len(issues))
	seen := make(map[string]struct{}, len(issues))
	for _, issue := range issues {
		key := fmt.Sprint(issue.Severity) + "\x00" + string(issue.Category) + "\x00" + IssueDedupKey(issue.Err)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, issue)
	}
	return deduped
}
