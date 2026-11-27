package ingest

import (
	"fmt"
	"path"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/gitidentity"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/acquire"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func resolveCheckoutAssessmentProfile(checkout *fsboundary.Dir, origin, ref string, identity targetprofile.Identity) ([]byte, error) {
	source, err := checkoutAssessmentSource(origin, ref, identity)
	if err != nil {
		return nil, err
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

func checkoutAssessmentSource(origin, ref string, identity targetprofile.Identity) (targetprofile.Source, error) {
	if err := identity.Validate(); err != nil {
		return targetprofile.Source{}, err
	}
	source, err := targetprofile.ParseSourceID(identity.SourceID)
	if err != nil {
		return targetprofile.Source{}, err
	}
	checkoutOrigin, err := gitidentity.CanonicalV1(origin)
	if source.Kind != "git" || err != nil || source.Origin != checkoutOrigin || source.Ref != ref {
		return targetprofile.Source{}, fmt.Errorf("marketplace.profile.unavailable: %s is not in the current registry checkout", identity.TargetID)
	}
	if err := acquire.ValidateExactPath(source.Root); err != nil {
		return targetprofile.Source{}, fmt.Errorf("marketplace.profile.unavailable: %w", err)
	}
	return source, nil
}

func requirePlaceMirror(registry store.Registry) error {
	if registry.Acquisition != store.AcquisitionMirror {
		return fmt.Errorf("marketplace.package.acquisition-unsupported: place packages require mirror acquisition")
	}
	return nil
}

// PlaceCheckoutPaths returns the exact profile and package paths required by configured place selections for registry.
func PlaceCheckoutPaths(cfg config.Config, registry store.Registry) ([]string, error) {
	var paths []string
	seen := make(map[string]struct{})
	for _, selection := range cfg.HostPackageAssessmentProfiles {
		if selection.RegistryID != string(registry.ID) {
			continue
		}
		if err := requirePlaceMirror(registry); err != nil {
			return nil, err
		}
		identity, err := cfg.AssessmentProfileFor(string(registry.ID), selection.Kind, selection.Name)
		if err != nil {
			return nil, err
		}
		if len(registry.Origins) == 0 {
			return nil, fmt.Errorf("marketplace.profile.unavailable: registry %q has no origin", registry.ID)
		}
		source, err := checkoutAssessmentSource(registry.Origins[len(registry.Origins)-1], registry.Ref, identity)
		if err != nil {
			return nil, err
		}
		targetID, localKind, scoped := nameutil.SplitPlaceKind(selection.Kind)
		if !scoped {
			return nil, fmt.Errorf("marketplace.profile.unconfigured: %s/%s is not a scoped place kind", selection.Kind, selection.Name)
		}
		packagePath := path.Join("packages", targetID, localKind, selection.Name)
		for _, exact := range []string{source.Root, packagePath} {
			if _, ok := seen[exact]; ok {
				continue
			}
			seen[exact] = struct{}{}
			paths = append(paths, exact)
		}
	}
	return paths, nil
}
