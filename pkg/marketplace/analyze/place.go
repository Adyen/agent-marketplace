package analyze

import (
	"context"

	"github.com/Adyen/agent-marketplace/internal/targetprofile"
)

// AnalyzePlaceFiles scans package bytes and delivery modes without interpreting runtime behavior.
func AnalyzePlaceFiles(ctx context.Context, files []targetprofile.PackageFile, opts Options) (Report, error) {
	if opts.Policy.Name == "" {
		opts.Policy = PublicPolicy()
	}
	input := make(FileSet, len(files))
	for _, file := range files {
		input[file.SourcePath] = file.Bytes
	}

	maxFindings := opts.Limits.MaxFindings
	scanOptions := opts
	scanOptions.Limits.MaxFindings = 0
	report, err := baseAnalyzeSamples(ctx, nil, input, "place-package", scanOptions)
	if err != nil {
		return Report{}, err
	}
	findings := append([]Finding(nil), report.Findings...)
	for _, file := range files {
		if file.Mode == 0o755 {
			findings = append(findings, NewFinding(
				FindingExecutableScript,
				"package file declares executable mode",
				analyzerName(opts, "place-package"),
				&SourceRange{File: file.SourcePath},
			))
		}
	}
	findings, rejected := applyPolicy(findings, opts.Policy)
	if maxFindings > 0 {
		findings = truncateFindings(findings, maxFindings)
	}
	return Report{Findings: findings, Rejected: rejected}, nil
}
