package analyze

import (
	"context"
	"fmt"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type hookAnalyzer struct{}

func (hookAnalyzer) Name() string { return "hook" }

func (a hookAnalyzer) Analyze(ctx context.Context, r ir.Resource, files FileSet, opts Options) (Report, error) {
	h, ok := r.(*ir.Hook)
	if !ok {
		return Report{}, fmt.Errorf("expected *ir.Hook, got %T", r)
	}

	report, err := baseAnalyze(ctx, r, files, a.Name(), opts)
	if err != nil {
		return Report{}, err
	}

	msg := fmt.Sprintf("resource declares hook capability: event=%s type=%s", h.Event, h.Type)
	report.Findings = append(report.Findings, NewFinding(FindingHookCapability, msg, analyzerName(opts, a.Name()), &SourceRange{File: a.Name() + "/" + h.Name()}))
	report.Findings, report.Rejected = applyPolicy(report.Findings, opts.Policy)

	return report, nil
}
