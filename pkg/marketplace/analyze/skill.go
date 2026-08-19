package analyze

import (
	"context"
	"fmt"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type skillAnalyzer struct{}

func (skillAnalyzer) Name() string { return "skill" }

func (a skillAnalyzer) Analyze(ctx context.Context, r ir.Resource, files FileSet, opts Options) (Report, error) {
	s, ok := r.(*ir.Skill)
	if !ok {
		return Report{}, fmt.Errorf("expected *ir.Skill, got %T", r)
	}

	report, err := baseAnalyze(ctx, r, files, a.Name(), opts)
	if err != nil {
		return Report{}, err
	}

	if len(s.AllowedTools) > 0 || s.Subagent != "" || s.Agent != "" {
		msg := "resource declares tool or execution grants"
		if len(s.AllowedTools) > 0 {
			msg = fmt.Sprintf("resource declares allowed tools: %v", s.AllowedTools)
		}
		report.Findings = append(report.Findings, NewFinding(FindingToolGrant, msg, analyzerName(opts, a.Name()), &SourceRange{File: a.Name() + "/" + s.Name()}))
		report.Findings, report.Rejected = applyPolicy(report.Findings, opts.Policy)
	}

	return report, nil
}
