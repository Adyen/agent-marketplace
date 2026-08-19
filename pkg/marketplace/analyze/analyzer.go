package analyze

import (
	"context"
	"fmt"
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

	if opts.Limits.MaxFindings > 0 && len(findings) > opts.Limits.MaxFindings {
		findings = findings[:opts.Limits.MaxFindings]
	}

	findings, rejected := applyPolicy(findings, opts.Policy)
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
