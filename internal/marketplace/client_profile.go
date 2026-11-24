package marketplace

import (
	"context"
	"fmt"
	"mime"

	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// TargetProfileEvidence contains the exact verified transport evidence for one
// immutable Marketplace target-profile revision.
type TargetProfileEvidence struct {
	Revision store.AssessedResourceRevision
	Identity targetprofile.Identity
	Manifest []byte
	Bytes    []byte
}

func (c *Client) AcquireTargetProfile(ctx context.Context, coordinate store.ResourceCoordinate, revision store.RevisionDigest, expected *targetprofile.Identity) (TargetProfileEvidence, error) {
	fail := func(err error) (TargetProfileEvidence, error) {
		return TargetProfileEvidence{}, err
	}
	if err := nameutil.ValidateSegment(string(coordinate.Marketplace)); err != nil {
		return fail(fmt.Errorf("resource marketplace: %w", err))
	}
	if err := nameutil.ValidateSegment(string(coordinate.Registry)); err != nil {
		return fail(fmt.Errorf("resource registry: %w", err))
	}
	if coordinate.Kind != nameutil.TargetProfileKind {
		return fail(fmt.Errorf("resource kind %q is not a target profile", coordinate.Kind))
	}
	if !nameutil.IsTargetIdentifier(coordinate.Name) {
		return fail(fmt.Errorf("resource name %q is not a valid target profile identifier", coordinate.Name))
	}
	if err := validateSHA256Digest(string(revision)); err != nil {
		return fail(fmt.Errorf("revision digest %q: %w", revision, err))
	}
	if expected != nil {
		if err := expected.Validate(); err != nil {
			return fail(fmt.Errorf("expected target profile identity: %w", err))
		}
		if expected.TargetID != coordinate.Name || expected.RevisionDigest != string(revision) {
			return fail(fmt.Errorf(
				"marketplace.profile.revision-mismatch: requested coordinate %s revision %s does not match the expected target profile identity",
				coordinate, revision,
			))
		}
	}

	segments := []string{
		"api", "v1", "resources", string(coordinate.Registry), coordinate.Kind,
		coordinate.Name, "revisions", string(revision),
	}
	var response resourceRevisionResponse
	if err := c.getJSONMode(ctx, c.requestURL(segments, nil), &response, false, mpapi.TargetProfilePlaceFeature); err != nil {
		return fail(err)
	}
	rev, err := response.assessed()
	if err != nil {
		return fail(err)
	}
	if err := validateAPIResourceRevisionMatchesFor(rev, coordinate, revision, true); err != nil {
		return fail(fmt.Errorf("invalid Marketplace target profile revision: %w", err))
	}
	if rev.Revocation != nil {
		return fail(fmt.Errorf("marketplace target profile revision is revoked"))
	}
	if err := enforceMarketplaceAssessment(rev); err != nil {
		return fail(err)
	}
	if rev.EffectiveAvailability != string(mpapi.AvailabilityAvailable) &&
		rev.EffectiveAvailability != string(mpapi.AvailabilityOperatorExposed) {
		return fail(fmt.Errorf("target profile revision has unavailable or unrecognized effective availability %q", rev.EffectiveAvailability))
	}

	manifestSegments := append(append([]string(nil), segments...), "inspection")
	rawManifest, _, err := c.getRawResponseMode(
		ctx, c.requestURL(manifestSegments, nil), inspection.MaxManifestBytes, mpapi.TargetProfilePlaceFeature,
	)
	if err != nil {
		return fail(err)
	}
	manifest, err := decodeVerifiedInspectionManifest(rawManifest, rev)
	if err != nil {
		return fail(err)
	}
	if manifest.ProfileIdentity == nil {
		return fail(fmt.Errorf("target profile inspection manifest has no verified profile identity"))
	}
	identity := *manifest.ProfileIdentity
	if expected != nil && identity != *expected {
		return fail(fmt.Errorf(
			"marketplace.profile.revision-mismatch: requested coordinate %s revision %s does not match the verified target profile identity",
			coordinate, revision,
		))
	}
	if len(manifest.Files) != 1 {
		return fail(fmt.Errorf("target profile inspection manifest does not contain exactly one file"))
	}
	file := manifest.Files[0]
	if file.Size <= 0 || file.Size > targetprofile.MaxProfileBytes {
		return fail(fmt.Errorf("target profile inspection size %d is outside the accepted range", file.Size))
	}

	contentSegments := append(append([]string(nil), manifestSegments...), "content")
	body, headers, err := c.getRawResponseMode(ctx, c.requestURL(contentSegments, nil), file.Size, mpapi.TargetProfilePlaceFeature)
	if err != nil {
		return fail(err)
	}
	mediaType, _, err := mime.ParseMediaType(headers.Get("Content-Type"))
	if err != nil {
		return fail(fmt.Errorf("parse target profile content type: %w", err))
	}
	if mediaType != inspection.MediaYAML || file.MediaType != inspection.MediaYAML {
		return fail(fmt.Errorf("target profile content type %q does not match %q", mediaType, inspection.MediaYAML))
	}
	if err := inspection.VerifyFileContent(file, body); err != nil {
		return fail(fmt.Errorf("verify target profile content: %w", err))
	}
	if _, err := targetprofile.Parse(body, identity); err != nil {
		return fail(fmt.Errorf("parse verified target profile: %w", err))
	}

	return TargetProfileEvidence{
		Revision: rev, Identity: identity, Manifest: rawManifest, Bytes: body,
	}, nil
}
