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

	// Do not pass the complete MCP struct through the generic field extractor:
	// environment/header/OAuth values are credentials and scanner messages are
	// persisted in public revision metadata. The name is the only typed field
	// that is both useful to inspect and safe to scan here; support files still
	// go through the normal bounded asset/text policy.
	samples := []textSample{{path: a.Name() + "/" + m.Name() + "/name", content: m.Name()}}
	report, err := baseAnalyzeSamples(ctx, samples, files, a.Name(), opts)
	if err != nil {
		return Report{}, err
	}

	// Do not include endpoint values in findings. MCP URLs can contain userinfo,
	// query tokens, or other credentials, and findings are persisted in public
	// revision metadata alongside the inspection manifest.
	msg := "resource declares MCP server capability"
	report.Findings = append(report.Findings, NewFinding(FindingMcpCapability, msg, analyzerName(opts, a.Name()), &SourceRange{File: a.Name() + "/" + m.Name()}))
	report.Findings, report.Rejected = applyPolicy(report.Findings, opts.Policy)

	return report, nil
}
