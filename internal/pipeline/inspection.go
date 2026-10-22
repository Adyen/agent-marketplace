package pipeline

import (
	"context"
	"errors"
	"fmt"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
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

type InspectionResult struct {
	ResolveResult
	Report         *materialize.Report
	MaterializeErr error
	Issues         []InspectionIssue
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
	Resolved                       func(InspectionResult)
	DryRun                         func(ctx context.Context, root string, artifacts []resolver.ResolvedArtifact, opts ...materialize.RunOption) (*materialize.Report, error)
	SkipMaterializeOnResolveErrors bool
}

func Inspect(ctx context.Context, opts InspectOptions) (InspectionResult, func()) {
	resolved, release := resolve(ctx, opts.Resolve)
	if opts.Resolved != nil && len(resolved.ConfigErrors) == 0 && (len(resolved.Errors) == 0 || len(resolved.CatalogErrors)+len(resolved.ResolveErrors) > 0) {
		opts.Resolved(buildInspection(resolved, nil, nil))
	}
	var report *materialize.Report
	var matErr error
	if !skipInspectionDryRun(resolved, opts.SkipMaterializeOnResolveErrors) {
		dryRun := opts.DryRun
		if dryRun == nil {
			dryRun = materialize.Run
		}
		deliverable, _ := DeliverableArtifacts(resolved.Resolved, resolved.PluginPlan)
		report, matErr = dryRun(ctx, opts.Resolve.Workspace.MaterializeRoot(), deliverable,
			materialize.WithDryRun(), materialize.WithMaterializationModes(resolved.MaterializationModes),
			materialize.WithPluginStateReconciliation())
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

func isSoftResolveIssue(err error) bool {
	return resolver.IsSoftSelectedNotFound(err) ||
		resolver.IsSoftSelectedMalformed(err) ||
		resolver.IsNameCollision(err) ||
		resolver.IsWarning(err) ||
		IsMarketplaceCacheMetadataMissingWarning(err)
}

func buildInspection(resolved ResolveResult, report *materialize.Report, matErr error) InspectionResult {
	issues := make([]InspectionIssue, 0, int64(len(resolved.Warnings))+int64(len(resolved.Errors))+int64(len(resolved.ResolveErrors)))
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
	if _, pending := DeliverableArtifacts(resolved.Resolved, resolved.PluginPlan); len(pending) > 0 {
		for _, w := range pending {
			var diagnostic interface{ DiagnosticSeverity() diag.Severity }
			if errors.As(w, &diagnostic) && diagnostic.DiagnosticSeverity() == diag.SeverityError {
				mark(w)
				add(IssueError, IssueMaterialization, w)
			} else {
				add(IssueWarning, IssueMaterialization, w)
			}
		}
	}
	for _, malformed := range resolved.CatalogLoad.Malformed {
		add(IssueWarning, IssueCatalog, malformedResourceError(malformed))
	}
	for _, err := range resolved.ResolveErrors {
		if isSoftResolveIssue(err) {
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
	return fmt.Errorf("malformed skill %q (%s): %w", m.DirName, skill.MalformedSourceFile(m), m.Error)
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
