package config

import (
	"fmt"

	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
)

type marketplaceProfileSelection struct {
	identity targetprofile.Identity
	source   Source
}

// SelectedMarketplaceProfiles returns the exact locked profile selected for
// each place target and validates every admitted package against that choice.
func (c *Config) SelectedMarketplaceProfiles() (map[string]targetprofile.Identity, error) {
	selected := make(map[string]targetprofile.Identity)
	if c == nil {
		return selected, nil
	}
	origins := make(map[string]marketplaceProfileSelection)
	for _, entry := range c.Marketplaces {
		for _, pin := range entry.Resources {
			if c.MarketplacePinExcluded(entry, pin) || pin.Kind != nameutil.TargetProfileKind || !pin.Direct {
				continue
			}
			if entry.Source != UserPersonal && entry.Source != Organization {
				return nil, fmt.Errorf("marketplace.profile.invalid-scope: target profile %s cannot be selected from %s layer", pin.Name, entry.Source)
			}
			identity := pin.LockProfileIdentity
			if err := identity.Validate(); err != nil {
				return nil, fmt.Errorf("marketplace.profile.selection-invalid: target profile %s: %w", pin.Name, err)
			}
			if identity.TargetID != pin.Name || identity.RevisionDigest != pin.Digest || pin.Revision != pin.Digest {
				return nil, fmt.Errorf("marketplace.profile.selection-invalid: target profile %s does not match its locked name and digest", pin.Name)
			}
			if previous, ok := origins[identity.TargetID]; ok {
				if previous.identity != identity {
					return nil, fmt.Errorf(
						"marketplace.profile.selection-ambiguous: target %s has profile %s revision %s from %s and profile %s revision %s from %s",
						identity.TargetID, previous.identity.SourceID, previous.identity.RevisionDigest, previous.source,
						identity.SourceID, identity.RevisionDigest, entry.Source,
					)
				}
				continue
			}
			selected[identity.TargetID] = identity
			origins[identity.TargetID] = marketplaceProfileSelection{identity: identity, source: entry.Source}
		}
	}
	for _, entry := range c.Marketplaces {
		for _, pin := range entry.Resources {
			if c.MarketplacePinExcluded(entry, pin) {
				continue
			}
			targetID, _, scoped := nameutil.SplitPlaceKind(pin.Kind)
			if !scoped {
				continue
			}
			if entry.Source != UserPersonal && entry.Source != Organization {
				return nil, fmt.Errorf("marketplace.profile.invalid-scope: scoped package %s/%s cannot be used from %s layer", pin.Kind, pin.Name, entry.Source)
			}
			if !nameutil.IsPlacePackageName(pin.Name) {
				return nil, fmt.Errorf("marketplace.package.binding-invalid: invalid scoped package name %q", pin.Name)
			}
			if !api.IsCanonicalSHA256Digest(pin.LockAssessmentDigest) {
				return nil, fmt.Errorf("marketplace.cache.profile-binding-missing: package %s/%s has no valid assessment digest", pin.Kind, pin.Name)
			}
			if err := pin.LockHostPackageIdentity.ValidateFor(pin.Kind, pin.Digest); err != nil {
				return nil, fmt.Errorf("marketplace.cache.profile-binding-missing: package %s/%s: %w", pin.Kind, pin.Name, err)
			}
			identity, ok := selected[targetID]
			if !ok {
				return nil, fmt.Errorf("marketplace.profile.deselected-package: package %s/%s requires selected target profile %s", pin.Kind, pin.Name, targetID)
			}
			if err := pin.LockHostPackageIdentity.MatchProfile(identity); err != nil {
				return nil, fmt.Errorf("marketplace.profile.revision-mismatch: package %s/%s selected profile %s: %w", pin.Kind, pin.Name, targetID, err)
			}
		}
	}
	return selected, nil
}
