package analyze

import (
	"context"
	"errors"
	"fmt"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// Engine runs a set of built-in analyzers against an IR resource.
type Engine struct {
	analyzers map[ir.ResourceKind][]Analyzer
}

// NewEngine returns an Engine with all built-in analyzers registered.
func NewEngine() *Engine {
	e := &Engine{analyzers: make(map[ir.ResourceKind][]Analyzer)}
	e.Register(ir.KindSkill, skillAnalyzer{})
	e.Register(ir.KindCommand, commandAnalyzer{})
	e.Register(ir.KindSubagent, subagentAnalyzer{})
	e.Register(ir.KindHook, hookAnalyzer{})
	e.Register(ir.KindMcpServer, mcpServerAnalyzer{})
	e.Register(ir.KindCollection, collectionAnalyzer{})
	return e
}

// Register adds an analyzer for a resource kind. Existing registrations for
// the kind are preserved.
func (e *Engine) Register(kind ir.ResourceKind, a Analyzer) {
	e.analyzers[kind] = append(e.analyzers[kind], a)
}

// Analyze runs all registered analyzers for r and merges their reports.
func (e *Engine) Analyze(ctx context.Context, r ir.Resource, files FileSet, opts Options) (Report, error) {
	if r == nil {
		return Report{}, errors.New("nil resource")
	}

	kind := r.Kind()
	if kind == ir.KindRule {
		return Report{}, fmt.Errorf("analyzer does not support resource kind %q", kind)
	}

	list, ok := e.analyzers[kind]
	if !ok || len(list) == 0 {
		return Report{}, fmt.Errorf("no analyzer registered for kind %q", kind)
	}

	if opts.Policy.Name == "" {
		opts.Policy = PublicPolicy()
	}
	if opts.Analyzer == "" {
		opts.Analyzer = "marketplace.analyze"
	}
	if opts.Version == "" {
		opts.Version = AnalyzerVersionV1
	}

	var merged []Finding
	rejected := false
	for _, a := range list {
		report, err := a.Analyze(ctx, r, files, opts)
		if err != nil {
			return Report{}, fmt.Errorf("analyzer %s: %w", a.Name(), err)
		}
		merged = append(merged, report.Findings...)
		if report.Rejected {
			rejected = true
		}
	}

	if opts.Limits.MaxFindings > 0 && len(merged) > opts.Limits.MaxFindings {
		merged = merged[:opts.Limits.MaxFindings]
	}

	return Report{Findings: merged, Rejected: rejected}, nil
}
