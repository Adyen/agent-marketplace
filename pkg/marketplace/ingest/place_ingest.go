package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"gopkg.in/yaml.v3"
)

func buildPlaceResult(ctx context.Context, registry store.Registry, checkout *fsboundary.Dir, selection config.HostPackageAssessmentProfile, identity targetprofile.Identity, profileBytes []byte, policy analyze.Policy, limits config.Limits, license licensePolicy) (placeCheckoutResult, error) {
	pkg, err := targetprofile.LoadPackage(
		checkout, selection.Kind, selection.Name, profileBytes, identity,
		targetprofile.PackageLimits{FileCount: limits.FileCount, FileBytes: limits.FileBytes, TotalBytes: limits.RepositoryBytes},
	)
	if err != nil {
		return placeCheckoutResult{}, err
	}
	targetID, localKind, scoped := nameutil.SplitPlaceKind(selection.Kind)
	if !scoped {
		return placeCheckoutResult{}, fmt.Errorf("marketplace.profile.unconfigured: %s/%s is not a scoped place kind", selection.Kind, selection.Name)
	}
	requires, err := indexedPlaceRequires(selection.Kind, selection.Name, pkg.Metadata.Requires)
	if err != nil {
		return placeCheckoutResult{}, err
	}
	sourcePath := path.Join("packages", targetID, localKind, selection.Name)
	inspectionBody, err := json.Marshal(struct {
		Name string `json:"name"`
	}{Name: selection.Name})
	if err != nil {
		return placeCheckoutResult{}, fmt.Errorf("encode place inspection primary: %w", err)
	}

	canonical, err := hccartifact.NewPlaceArtifact(selection.Kind, selection.Name, identity, pkg.Files)
	if err != nil {
		return placeCheckoutResult{}, fmt.Errorf("build place artifact %q: %w", selection.Name, err)
	}
	canonical.Manifest.License = registry.License
	final, placeholder, archiveDigest, err := hccartifact.EncodeArchives(canonical)
	if err != nil {
		return placeCheckoutResult{}, fmt.Errorf("encode place artifact %q: %w", selection.Name, err)
	}
	if limits.ArtifactBytes > 0 && (int64(len(final)) > limits.ArtifactBytes || int64(len(placeholder)) > limits.ArtifactBytes) {
		return placeCheckoutResult{}, fmt.Errorf("marketplace.package.artifact-too-large: encoded place package %q exceeds artifact byte limit", selection.Name)
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
		return placeCheckoutResult{}, fmt.Errorf("analyze place package %q: %w", selection.Name, err)
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
	return placeCheckoutResult{
		result: result, sourcePath: sourcePath, inspectionBody: inspectionBody, requires: requires,
	}, nil
}

type placeCheckoutResult struct {
	result          artifact.BuildResult
	sourcePath      string
	inspectionBody  []byte
	profileIdentity targetprofile.Identity
	profileBytes    []byte
	requires        string
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
	place, err := buildPlaceResult(ctx, registry, checkout, selection, identity, profileBytes, policy, limits, license)
	if err != nil {
		return placeCheckoutResult{}, err
	}
	place.profileIdentity = identity
	place.profileBytes = profileBytes
	return place, nil
}

func indexedPlaceRequires(packageKind, packageName string, items []yaml.Node) (string, error) {
	targetID, _, _ := nameutil.SplitPlaceKind(packageKind)
	requirements := make([]store.ResourceRequirement, 0, len(items))
	for i, item := range items {
		requirement, ok := parsePlaceRequirement(item)
		requiredTarget, _, scoped := nameutil.SplitPlaceKind(requirement.Kind)
		if !ok || !scoped || requiredTarget != targetID ||
			!nameutil.IsPlacePackageName(requirement.Name) ||
			(requirement.Kind == packageKind && requirement.Name == packageName) {
			return "", fmt.Errorf(
				"marketplace.package.prerequisites-invalid: %s/%s requirement %d must name another same-target scoped package without registry or path hints",
				packageKind, packageName, i+1,
			)
		}
		requirements = append(requirements, requirement)
	}
	raw, err := json.Marshal(requirements)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func parsePlaceRequirement(node yaml.Node) (req store.ResourceRequirement, ok bool) {
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Tag != "!!str" {
			return req, false
		}
		req.Kind, req.Name, ok = strings.Cut(node.Value, ":")
		return req, ok
	case yaml.MappingNode:
		if len(node.Content) != 4 {
			return req, false
		}
		for i := 0; i < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" ||
				value.Kind != yaml.ScalarNode || value.Tag != "!!str" || value.Value == "" {
				return req, false
			}
			switch key.Value {
			case "kind":
				if req.Kind != "" {
					return req, false
				}
				req.Kind = value.Value
			case "name":
				if req.Name != "" {
					return req, false
				}
				req.Name = value.Value
			default:
				return req, false
			}
		}
		return req, req.Kind != "" && req.Name != ""
	default:
		return req, false
	}
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
