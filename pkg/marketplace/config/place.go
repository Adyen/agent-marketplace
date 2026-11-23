package config

import (
	"errors"
	"fmt"

	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
)

const (
	profileUnconfiguredCode = "marketplace.profile.unconfigured"
	profileDuplicateCode    = "marketplace.profile.duplicate-coordinate"
)

// HostPackageAssessmentProfile pins a profile to one exact host package coordinate.
type HostPackageAssessmentProfile struct {
	RegistryID      string                 `yaml:"registry_id"`
	Kind            string                 `yaml:"kind"`
	Name            string                 `yaml:"name"`
	ProfileIdentity targetprofile.Identity `yaml:"profile_identity"`
}

// AssessmentProfileFor returns the profile explicitly configured for a package coordinate.
func (cfg Config) AssessmentProfileFor(registryID, kind, name string) (targetprofile.Identity, error) {
	var identity targetprofile.Identity
	found := false
	for i, profile := range cfg.HostPackageAssessmentProfiles {
		if profile.RegistryID != registryID || profile.Kind != kind || profile.Name != name {
			continue
		}
		if errs := validateHostPackageAssessmentProfile(i, profile); len(errs) != 0 {
			return targetprofile.Identity{}, errors.Join(errs...)
		}
		if found {
			return targetprofile.Identity{}, fmt.Errorf("%s: host_package_assessment_profiles[%d] duplicates coordinate %s/%s/%s",
				profileDuplicateCode, i, registryID, kind, name)
		}
		identity = profile.ProfileIdentity
		found = true
	}
	if !found {
		return targetprofile.Identity{}, fmt.Errorf("%s: %s/%s/%s", profileUnconfiguredCode, registryID, kind, name)
	}
	return identity, nil
}

func validateHostPackageAssessmentProfiles(profiles []HostPackageAssessmentProfile) []error {
	var errs []error
	seen := make(map[[3]string]struct{}, len(profiles))
	for i, profile := range profiles {
		errs = append(errs, validateHostPackageAssessmentProfile(i, profile)...)
		coordinate := [3]string{profile.RegistryID, profile.Kind, profile.Name}
		if _, ok := seen[coordinate]; ok {
			errs = append(errs, fmt.Errorf("host_package_assessment_profiles[%d]: duplicate exact coordinate", i))
		}
		seen[coordinate] = struct{}{}
	}
	return errs
}

func validateHostPackageAssessmentProfile(index int, profile HostPackageAssessmentProfile) []error {
	field := fmt.Sprintf("host_package_assessment_profiles[%d]", index)
	var errs []error
	if err := nameutil.ValidateSegment(profile.RegistryID); err != nil {
		errs = append(errs, fmt.Errorf("%s.registry_id %w", field, err))
	}
	targetID, _, scoped := nameutil.SplitPlaceKind(profile.Kind)
	if !scoped {
		errs = append(errs, fmt.Errorf("%s.kind must be target-scoped", field))
	} else if targetID != profile.ProfileIdentity.TargetID {
		errs = append(errs, fmt.Errorf("%s.profile_identity.target_id must match kind target", field))
	}
	if !nameutil.IsPlacePackageName(profile.Name) {
		errs = append(errs, fmt.Errorf("%s.name is not a valid place package name", field))
	}
	if err := profile.ProfileIdentity.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("%s.profile_identity: %w", field, err))
	}
	return errs
}
