package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
)

// Validate returns an error if d is not a supported assessment decision.
func (d AssessmentDecision) Validate() error {
	switch d {
	case AssessmentDecisionAllow, AssessmentDecisionWarn, AssessmentDecisionReject:
		return nil
	default:
		return fmt.Errorf("invalid assessment decision %q", d)
	}
}

// FinalizeResourceAssessment validates an assessment's registry fields and
// returns a value with its canonical content digest populated. A supplied
// AssessmentDigest must already match the canonical identity. AssessedAt is
// recorded but deliberately excluded from that identity.
func FinalizeResourceAssessment(assessment ResourceAssessment) (ResourceAssessment, error) {
	if err := validateResourceAssessmentFields(assessment); err != nil {
		return ResourceAssessment{}, err
	}

	// Do not retain the caller's mutable slice backing array in the finalized
	// value. Findings are validated, canonicalized, and deep-cloned.
	assessment.Findings = append([]Finding(nil), assessment.Findings...)
	for i := range assessment.Findings {
		f, err := validateFinding(assessment.Findings[i], i)
		if err != nil {
			return ResourceAssessment{}, err
		}
		assessment.Findings[i] = f
	}

	digest, err := resourceAssessmentDigest(assessment)
	if err != nil {
		return ResourceAssessment{}, err
	}
	if assessment.AssessmentDigest != "" {
		if err := ValidateSHA256Digest("assessment digest", string(assessment.AssessmentDigest)); err != nil {
			return ResourceAssessment{}, err
		}
		if assessment.AssessmentDigest != digest {
			return ResourceAssessment{}, fmt.Errorf("assessment digest %q does not match canonical identity %q", assessment.AssessmentDigest, digest)
		}
	}
	assessment.AssessmentDigest = digest
	return assessment, nil
}

func (a ResourceAssessment) Equal(other ResourceAssessment) bool {
	a.AssessedAt = other.AssessedAt
	return reflect.DeepEqual(a, other)
}

// Validate checks all required assessment fields and verifies that
// AssessmentDigest matches the assessment's canonical identity.
func (a ResourceAssessment) Validate() error {
	if a.AssessmentDigest == "" {
		return fmt.Errorf("assessment digest is required")
	}
	_, err := FinalizeResourceAssessment(a)
	return err
}

type resourceAssessmentIdentity struct {
	Coordinate               ResourceCoordinate `json:"coordinate"`
	ContentDigest            RevisionDigest     `json:"content_digest"`
	AnalyzerVersion          string             `json:"analyzer_version"`
	PolicyDigest             PolicyDigest       `json:"policy_digest"`
	Decision                 AssessmentDecision `json:"decision"`
	Findings                 []Finding          `json:"findings"`
	InspectionManifestDigest RevisionDigest     `json:"inspection_manifest_digest"`
}

func resourceAssessmentDigest(assessment ResourceAssessment) (AssessmentDigest, error) {
	canonical, err := json.Marshal(resourceAssessmentIdentity{
		Coordinate:               assessment.Coordinate,
		ContentDigest:            assessment.ContentDigest,
		AnalyzerVersion:          assessment.AnalyzerVersion,
		PolicyDigest:             assessment.PolicyDigest,
		Decision:                 assessment.Decision,
		Findings:                 assessment.Findings,
		InspectionManifestDigest: assessment.InspectionManifestDigest,
	})
	if err != nil {
		return "", fmt.Errorf("marshal canonical resource assessment: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return AssessmentDigest("sha256:" + hex.EncodeToString(sum[:])), nil
}

func validateResourceAssessmentFields(assessment ResourceAssessment) error {
	coordinateParts := [...]struct {
		label string
		value string
	}{
		{label: "marketplace", value: string(assessment.Coordinate.Marketplace)},
		{label: "registry", value: string(assessment.Coordinate.Registry)},
		{label: "kind", value: assessment.Coordinate.Kind},
		{label: "name", value: assessment.Coordinate.Name},
	}
	for _, part := range coordinateParts {
		if part.value == "" {
			return fmt.Errorf("resource coordinate %s is required", part.label)
		}
	}
	if !IsMarketplaceResourceKind(assessment.Coordinate.Kind) {
		return fmt.Errorf("unsupported resource kind %q", assessment.Coordinate.Kind)
	}
	if err := ValidateSHA256Digest("content digest", string(assessment.ContentDigest)); err != nil {
		return err
	}
	if err := ValidateAnalyzerVersion(assessment.AnalyzerVersion); err != nil {
		return err
	}
	if err := ValidateSHA256Digest("policy digest", string(assessment.PolicyDigest)); err != nil {
		return err
	}
	if err := assessment.Decision.Validate(); err != nil {
		return err
	}
	if err := ValidateSHA256Digest("inspection manifest digest", string(assessment.InspectionManifestDigest)); err != nil {
		return err
	}
	if assessment.AssessedAt.IsZero() {
		return fmt.Errorf("assessed at is required")
	}
	return nil
}

func ValidateSHA256Digest(label, digest string) error {
	const prefix = "sha256:"
	if len(digest) != len(prefix)+sha256.Size*2 || digest[:len(prefix)] != prefix {
		return fmt.Errorf("%s must be sha256:<64 lowercase hex>", label)
	}
	for _, c := range digest[len(prefix):] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return fmt.Errorf("%s must be sha256:<64 lowercase hex>", label)
		}
	}
	return nil
}
