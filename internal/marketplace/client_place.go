package marketplace

import (
	"context"
	"fmt"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// AcquirePlacePackage fetches and verifies a scoped place package against the selected profile.
func (c *Client) AcquirePlacePackage(ctx context.Context, coordinate store.ResourceCoordinate, revision store.RevisionDigest, profileIdentity targetprofile.Identity, profileBytes []byte) (store.AssessedResourceRevision, []targetprofile.PackageFile, error) {
	fail := func(err error) (store.AssessedResourceRevision, []targetprofile.PackageFile, error) {
		return store.AssessedResourceRevision{}, nil, err
	}
	marketplaceID := string(coordinate.Marketplace)
	if marketplaceID == "" || marketplaceID != strings.TrimSpace(marketplaceID) {
		return fail(fmt.Errorf("resource marketplace is required and must be trimmed"))
	}
	if err := nameutil.ValidateSegment(string(coordinate.Registry)); err != nil {
		return fail(fmt.Errorf("resource registry: %w", err))
	}
	targetID, _, scoped := nameutil.SplitPlaceKind(coordinate.Kind)
	if !scoped || !nameutil.IsPlacePackageName(coordinate.Name) {
		return fail(fmt.Errorf("resource coordinate is not a valid scoped place package"))
	}
	if err := validateSHA256Digest(string(revision)); err != nil {
		return fail(fmt.Errorf("revision digest %q: %w", revision, err))
	}
	profile, err := targetprofile.Parse(profileBytes, profileIdentity)
	if err != nil {
		return fail(fmt.Errorf("parse selected profile: %w", err))
	}
	if targetID != profileIdentity.TargetID {
		return fail(fmt.Errorf("scoped kind target %q does not match profile target %q", targetID, profileIdentity.TargetID))
	}

	var response resourceRevisionResponse
	requestURL := c.requestURL([]string{
		"api", "v1", "resources", string(coordinate.Registry), coordinate.Kind,
		coordinate.Name, "revisions", string(revision),
	}, nil)
	if err := c.getJSONMode(ctx, requestURL, &response, false, mpapi.TargetProfilePlaceFeature); err != nil {
		return fail(err)
	}
	rev, err := response.assessed()
	if err != nil {
		return fail(err)
	}
	if err := validateAPIResourceRevisionMatchesFor(rev, coordinate, revision, true); err != nil {
		return fail(fmt.Errorf("invalid Marketplace place revision: %w", err))
	}
	if rev.Revocation != nil {
		return fail(fmt.Errorf("marketplace resource revision is revoked"))
	}
	if err := enforceMarketplaceAssessment(rev); err != nil {
		return fail(err)
	}
	if rev.EffectiveAvailability != string(mpapi.AvailabilityAvailable) &&
		rev.EffectiveAvailability != string(mpapi.AvailabilityOperatorExposed) {
		return fail(fmt.Errorf("place revision has unavailable or unrecognized effective availability %q", rev.EffectiveAvailability))
	}
	binding := rev.Assessment.HostPackageIdentity
	if binding == nil {
		return fail(fmt.Errorf("place revision has no package identity"))
	}
	if err := binding.MatchProfile(profileIdentity); err != nil {
		return fail(fmt.Errorf("marketplace.profile.revision-mismatch: package %s profile %s: %w", coordinate.Name, profileIdentity.TargetID, err))
	}

	raw, err := c.GetPlaceholderArchive(ctx, string(rev.ArchiveDigest))
	if err != nil {
		return fail(err)
	}
	archive, err := hccartifact.VerifyPlaceholderArchive(raw, string(rev.ArchiveDigest))
	if err != nil {
		return fail(fmt.Errorf("verify place package archive: %w", err))
	}
	manifest := archive.Manifest
	if manifest.Kind != coordinate.Kind || manifest.Name != coordinate.Name ||
		manifest.ContentDigest != string(rev.ContentDigest) ||
		(manifest.RegistryID != "" && manifest.RegistryID != string(coordinate.Registry)) {
		return fail(fmt.Errorf("place package archive manifest does not match requested revision"))
	}
	if manifest.HostPackageIdentity == nil || *manifest.HostPackageIdentity != *binding {
		return fail(fmt.Errorf("place package archive binding does not match assessed revision"))
	}
	files, err := archive.PlaceFiles()
	if err != nil {
		return fail(fmt.Errorf("decode place package archive: %w", err))
	}
	if err := targetprofile.ValidatePackageDestinations(profile, coordinate.Kind, files); err != nil {
		return fail(fmt.Errorf("validate place package destinations: %w", err))
	}
	return rev, files, nil
}
