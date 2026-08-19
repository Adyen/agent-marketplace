package analyze

import (
	"context"
	"fmt"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type commandAnalyzer struct{}

func (commandAnalyzer) Name() string { return "command" }

func (a commandAnalyzer) Analyze(ctx context.Context, r ir.Resource, files FileSet, opts Options) (Report, error) {
	if _, ok := r.(*ir.Command); !ok {
		return Report{}, fmt.Errorf("expected *ir.Command, got %T", r)
	}

	return baseAnalyze(ctx, r, files, a.Name(), opts)
}
