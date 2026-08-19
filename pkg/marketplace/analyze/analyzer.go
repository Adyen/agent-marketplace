package analyze

import (
	"context"
	"fmt"
	"log/slog"
	"unicode/utf8"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// FileSet is a set of supporting files keyed by relative path.
type FileSet map[string][]byte

// Report is the result of analyzing a resource and its supporting files.
type Report struct {
	Findings []Finding
	Rejected bool
}

// Analyzer analyzes a resource for security and policy findings.
type Analyzer interface {
	Name() string
	Analyze(ctx context.Context, r ir.Resource, files FileSet, opts Options) (Report, error)
}

// analyzerName returns the analyzer name to use for findings.
func analyzerName(opts Options, fallback string) string {
	if opts.Analyzer != "" {
		return opts.Analyzer
	}
	if fallback != "" {
		return fallback
	}
	return "marketplace.analyze"
}

// applyPolicy sets Severity and PolicyOutcome on each finding and returns true
// if any finding triggers a reject outcome.
func applyPolicy(findings []Finding, policy Policy) ([]Finding, bool) {
	rejected := false
	for i := range findings {
		outcome := policy.OutcomeFor(findings[i].Code)
		findings[i].PolicyOutcome = outcome
		switch outcome {
		case OutcomeReject:
			findings[i].Severity = OutcomeReject
			rejected = true
		case OutcomeWarn:
			findings[i].Severity = OutcomeWarn
		default:
			findings[i].Severity = "info"
		}
	}
	return findings, rejected
}

// truncateFindings enforces MaxFindings while never discarding a reject
// finding. It keeps all reject findings, then fills the remaining budget with
// warn and info findings in their original stable order.
func truncateFindings(findings []Finding, max int) []Finding {
	if max <= 0 || len(findings) <= max {
		return findings
	}

	var reject, warn, info []Finding
	for _, f := range findings {
		switch f.Severity {
		case OutcomeReject:
			reject = append(reject, f)
		case OutcomeWarn:
			warn = append(warn, f)
		default:
			info = append(info, f)
		}
	}

	// Reject findings are never dropped, even if that exceeds MaxFindings.
	if len(reject) >= max {
		slog.Debug("truncated findings: kept all reject findings", "kept", len(reject), "max", max)
		return reject
	}

	result := make([]Finding, 0, max)
	result = append(result, reject...)
	remaining := max - len(reject)

	n := len(warn)
	if n > remaining {
		n = remaining
	}
	if n > 0 {
		result = append(result, warn[:n]...)
		remaining -= n
	}

	n = len(info)
	if n > remaining {
		n = remaining
	}
	if n > 0 {
		result = append(result, info[:n]...)
	}

	slog.Debug("truncated findings", "kept", len(result), "dropped", len(findings)-len(result), "max", max)
	return result
}

// baseAnalyze extracts textual fields from r and the supplied files, runs the
// shared scanners and asset scanner, and applies policy outcomes. It is the
// common analysis stage used by all per-kind analyzers.
func baseAnalyze(ctx context.Context, r ir.Resource, files FileSet, name string, opts Options) (Report, error) {
	if ctx.Err() != nil {
		return Report{}, ctx.Err()
	}

	an := analyzerName(opts, name)

	if err := checkLimits(files, opts.Limits); err != nil {
		return Report{}, err
	}

	samples := extractResourceSamples(r)
	for path, data := range files {
		if len(data) == 0 {
			continue
		}
		samples = append(samples, textSample{path: path, content: string(data), isFile: true})
	}

	var findings []Finding
	for _, s := range samples {
		if s.isFile && !isValidText(s.content) {
			// Run asset scanners on non-text files but skip text scanners.
		} else {
			findings = append(findings, runSharedScanners(s.content, s.path, an)...)
		}
	}

	findings = append(findings, ScanAssets(files, an)...)

	findings, rejected := applyPolicy(findings, opts.Policy)
	if opts.Limits.MaxFindings > 0 {
		findings = truncateFindings(findings, opts.Limits.MaxFindings)
	}

	return Report{Findings: findings, Rejected: rejected}, nil
}

// textSample is a unit of content that scanners inspect.
type textSample struct {
	path    string
	content string
	isFile  bool
}

func checkLimits(files FileSet, limits Limits) error {
	if limits.MaxFiles > 0 && len(files) > limits.MaxFiles {
		return fmt.Errorf("file set exceeds MaxFiles limit %d", limits.MaxFiles)
	}
	var total int64
	for _, data := range files {
		sz := int64(len(data))
		if limits.MaxFileBytes > 0 && sz > limits.MaxFileBytes {
			return fmt.Errorf("file exceeds MaxFileBytes limit %d", limits.MaxFileBytes)
		}
		total += sz
		if limits.MaxTotalBytes > 0 && total > limits.MaxTotalBytes {
			return fmt.Errorf("file set exceeds MaxTotalBytes limit %d", limits.MaxTotalBytes)
		}
	}
	return nil
}

// isValidText reports whether s is valid UTF-8. Binary assets are rejected from
// text scanners to avoid false positives from arbitrary bytes.
func isValidText(s string) bool {
	return utf8.ValidString(s)
}
