package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"path"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func buildPlaceResult(ctx context.Context, registry store.Registry, checkout *fsboundary.Dir, selection config.HostPackageAssessmentProfile, identity targetprofile.Identity, profileBytes []byte, policy analyze.Policy, limits config.Limits, license licensePolicy) (artifact.BuildResult, string, []byte, error) {
	pkg, err := targetprofile.LoadPackage(
		checkout, selection.Kind, selection.Name, profileBytes, identity,
		targetprofile.PackageLimits{FileCount: limits.FileCount, FileBytes: limits.FileBytes, TotalBytes: limits.RepositoryBytes},
	)
	if err != nil {
		return artifact.BuildResult{}, "", nil, err
	}
	if len(pkg.Metadata.Requires) != 0 {
		return artifact.BuildResult{}, "", nil, fmt.Errorf("marketplace.package.prerequisites-unsupported: %s/%s declares prerequisites", selection.Kind, selection.Name)
	}
	targetID, localKind, scoped := nameutil.SplitPlaceKind(selection.Kind)
	if !scoped {
		return artifact.BuildResult{}, "", nil, fmt.Errorf("marketplace.profile.unconfigured: %s/%s is not a scoped place kind", selection.Kind, selection.Name)
	}
	sourcePath := path.Join("packages", targetID, localKind, selection.Name)
	inspectionBody, err := json.Marshal(struct {
		Name string `json:"name"`
	}{Name: selection.Name})
	if err != nil {
		return artifact.BuildResult{}, "", nil, fmt.Errorf("encode place inspection primary: %w", err)
	}

	canonical, err := hccartifact.NewPlaceArtifact(selection.Kind, selection.Name, identity, pkg.Files)
	if err != nil {
		return artifact.BuildResult{}, "", nil, fmt.Errorf("build place artifact %q: %w", selection.Name, err)
	}
	canonical.Manifest.License = registry.License
	final, placeholder, archiveDigest, err := hccartifact.EncodeArchives(canonical)
	if err != nil {
		return artifact.BuildResult{}, "", nil, fmt.Errorf("encode place artifact %q: %w", selection.Name, err)
	}
	if limits.ArtifactBytes > 0 && (int64(len(final)) > limits.ArtifactBytes || int64(len(placeholder)) > limits.ArtifactBytes) {
		return artifact.BuildResult{}, "", nil, fmt.Errorf("marketplace.package.artifact-too-large: encoded place package %q exceeds artifact byte limit", selection.Name)
	}
	canonical.Manifest.ArchiveDigest = archiveDigest

	opts := analyze.Options{
		Policy: policy,
		Limits: analyze.Limits{
			MaxFileBytes:  limits.FileBytes,
			MaxTotalBytes: limits.RepositoryBytes,
			MaxFiles:      limits.FileCount,
			MaxFindings:   analyze.DefaultLimits().MaxFindings,
		},
	}
	report, err := analyze.AnalyzePlaceFiles(ctx, pkg.Files, opts)
	if err != nil {
		return artifact.BuildResult{}, "", nil, fmt.Errorf("analyze place package %q: %w", selection.Name, err)
	}
	result := artifact.BuildResult{
		Artifact:           canonical,
		RawArchive:         final,
		PlaceholderArchive: placeholder,
		ContentDigest:      canonical.Manifest.ContentDigest,
		ArchiveDigest:      archiveDigest,
		Findings:           report.Findings,
		Name:               selection.Name,
		Kind:               selection.Kind,
		License:            registry.License,
		Rejected:           report.Rejected,
	}
	if report.Rejected {
		result.RejectReason = "policy rejected one or more findings"
	}
	result = applyPolicyToResult(result, registry.ID, limits, license, registry.Acquisition)
	return result, sourcePath, inspectionBody, nil
}

type placeCheckoutResult struct {
	result          artifact.BuildResult
	sourcePath      string
	inspectionBody  []byte
	profileIdentity targetprofile.Identity
	profileBytes    []byte
}

func buildPlaceCheckoutResult(ctx context.Context, cfg config.Config, registry store.Registry, dir string, selection config.HostPackageAssessmentProfile, policy analyze.Policy, limits config.Limits, license licensePolicy) (placeCheckoutResult, error) {
	if err := requirePlaceMirror(registry); err != nil {
		return placeCheckoutResult{}, err
	}
	identity, err := cfg.AssessmentProfileFor(string(registry.ID), selection.Kind, selection.Name)
	if err != nil {
		return placeCheckoutResult{}, err
	}
	if len(registry.Origins) == 0 {
		return placeCheckoutResult{}, fmt.Errorf("marketplace.profile.unavailable: registry %q has no origin", registry.ID)
	}
	checkout, err := fsboundary.OpenDir(dir)
	if err != nil {
		return placeCheckoutResult{}, fmt.Errorf("open registry checkout for place package %q: %w", selection.Name, err)
	}
	defer func() { _ = checkout.Close() }()

	origin := registry.Origins[len(registry.Origins)-1]
	profileBytes, err := resolveCheckoutAssessmentProfile(checkout, origin, registry.Ref, identity)
	if err != nil {
		return placeCheckoutResult{}, err
	}
	result, sourcePath, inspectionBody, err := buildPlaceResult(ctx, registry, checkout, selection, identity, profileBytes, policy, limits, license)
	if err != nil {
		return placeCheckoutResult{}, err
	}
	return placeCheckoutResult{
		result: result, sourcePath: sourcePath, inspectionBody: inspectionBody,
		profileIdentity: identity, profileBytes: profileBytes,
	}, nil
}

func placeStoreFindings(findings []analyze.Finding) []store.Finding {
	out := make([]store.Finding, 0, len(findings))
	for _, finding := range findings {
		out = append(out, store.Finding{
			Code: finding.Code, Severity: finding.Severity,
			Policy: finding.PolicyOutcome, Message: finding.Message,
		})
	}
	return out
}
