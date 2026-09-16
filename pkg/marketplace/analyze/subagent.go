package analyze

import (
	"context"
	"fmt"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type subagentAnalyzer struct{}

func (subagentAnalyzer) Name() string { return "subagent" }

func (a subagentAnalyzer) Analyze(ctx context.Context, r ir.Resource, files FileSet, opts Options) (Report, error) {
	s, ok := r.(*ir.Subagent)
	if !ok {
		return Report{}, fmt.Errorf("expected *ir.Subagent, got %T", r)
	}

	report, err := baseAnalyze(ctx, r, files, a.Name(), opts)
	if err != nil {
		return Report{}, err
	}

	if len(s.Tools) > 0 {
		msg := fmt.Sprintf("subagent declares tools: %v", s.Tools)
		report.Findings = append(report.Findings, NewFinding(FindingToolGrant, msg, analyzerName(opts, a.Name()), &SourceRange{File: a.Name() + "/" + s.Name()}))
		report.Findings, report.Rejected = applyPolicy(report.Findings, opts.Policy)
	}

	return report, nil
}
