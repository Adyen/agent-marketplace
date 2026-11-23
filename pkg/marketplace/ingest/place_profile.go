package ingest

import (
	"fmt"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/gitidentity"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
)

func resolveCheckoutAssessmentProfile(checkout *fsboundary.Dir, origin, ref string, identity targetprofile.Identity) ([]byte, error) {
	if err := identity.Validate(); err != nil {
		return nil, err
	}
	source, err := targetprofile.ParseSourceID(identity.SourceID)
	if err != nil {
		return nil, err
	}
	checkoutOrigin, err := gitidentity.CanonicalV1(origin)
	if source.Kind != "git" || err != nil || source.Origin != checkoutOrigin || source.Ref != ref {
		return nil, fmt.Errorf("marketplace.profile.unavailable: %s is not in the current registry checkout", identity.TargetID)
	}
	raw, _, err := checkout.ReadFile(source.Root, targetprofile.MaxProfileBytes)
	if err != nil {
		return nil, fmt.Errorf("marketplace.profile.unavailable: read profile for %s: %w", identity.TargetID, err)
	}
	if _, err := targetprofile.Parse(raw, identity); err != nil {
		return nil, fmt.Errorf("verify assessment profile %s: %w", identity.TargetID, err)
	}
	return raw, nil
}
