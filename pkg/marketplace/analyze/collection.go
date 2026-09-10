package analyze

import (
	"context"
	"fmt"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type collectionAnalyzer struct{}

func (collectionAnalyzer) Name() string { return "collection" }

func (a collectionAnalyzer) Analyze(ctx context.Context, r ir.Resource, files FileSet, opts Options) (Report, error) {
	if _, ok := r.(*ir.Collection); !ok {
		return Report{}, fmt.Errorf("expected *ir.Collection, got %T", r)
	}

	return baseAnalyze(ctx, r, files, a.Name(), opts)
}
