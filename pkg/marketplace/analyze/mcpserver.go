package analyze

import (
	"context"
	"fmt"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type mcpServerAnalyzer struct{}

func (mcpServerAnalyzer) Name() string { return "mcpserver" }

func (a mcpServerAnalyzer) Analyze(ctx context.Context, r ir.Resource, files FileSet, opts Options) (Report, error) {
	m, ok := r.(*ir.McpServer)
	if !ok {
		return Report{}, fmt.Errorf("expected *ir.McpServer, got %T", r)
	}

	report, err := baseAnalyze(ctx, r, files, a.Name(), opts)
	if err != nil {
		return Report{}, err
	}

	msg := "resource declares MCP server capability"
	if m.Transport != "" {
		msg = fmt.Sprintf("resource declares MCP server capability: transport=%s", m.Transport)
	} else if m.URL != "" {
		msg = fmt.Sprintf("resource declares MCP server capability: url=%s", m.URL)
	}
	report.Findings = append(report.Findings, NewFinding(FindingMcpCapability, msg, analyzerName(opts, a.Name()), &SourceRange{File: a.Name() + "/" + m.Name()}))
	report.Findings, report.Rejected = applyPolicy(report.Findings, opts.Policy)

	return report, nil
}
