package pipeline

import (
	"context"
	"errors"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// marketplaceCacheArtifactResolver adapts inspect-mode Marketplace metadata to
// the desired set the read-only commands need: Marketplace selections resolve
// into materializable artifacts from verified local cache evidence only —
// CachedNoFetch, exactly the semantics offline sync uses — so `status` and
// `doctor` compute the same desired set as sync without downloading artifacts
// or writing config and lock files. Required-pin health failures the cache
// cannot observe (revocation, endpoint unavailability) are merged in from the
// inspection, and a pin that fails both ways is reported once.
type marketplaceCacheArtifactResolver struct {
	delegate resolver.MarketplaceResolver
	pins     []marketplace.PinStatus
}

// The resolver passes the inspect-mode Marketplace mode through its options;
// the adapter ignores it and every other mode it is handed, because its
// contract is cache-only resolution.
func (r marketplaceCacheArtifactResolver) Resolve(ctx context.Context, cfg *config.Config, targets []hcc.Target, _ resolver.MarketplaceResolveMode) ([]resolver.ResolvedArtifact, []error) {
	return r.ResolveWithPolicy(ctx, cfg, targets, resolver.MarketplaceResolveModeCachedNoFetch, nil)
}

func (r marketplaceCacheArtifactResolver) ResolveWithPolicy(ctx context.Context, cfg *config.Config, targets []hcc.Target, _ resolver.MarketplaceResolveMode, policy resolver.MarketplaceRevocationPolicy) ([]resolver.ResolvedArtifact, []error) {
	artifacts, errs := resolveMarketplaceArtifacts(ctx, r.delegate, cfg, targets, resolver.MarketplaceResolveModeCachedNoFetch, policy)
	healthErrs := marketplace.RequiredPinHealthErrors(r.pins)
	unhealthy := make(map[marketplacePinCoordinate]bool, len(healthErrs))
	for _, err := range healthErrs {
		var statusErr *marketplace.PinStatusError
		if errors.As(err, &statusErr) && statusErr != nil {
			unhealthy[marketplacePinCoordinateOfStatus(statusErr.Pin)] = true
		}
	}
	merged := append([]error(nil), healthErrs...)
	for _, err := range errs {
		var pinErr *marketplace.PinError
		switch {
		case !errors.As(err, &pinErr) || pinErr == nil:
			// Entry-level failures have no pin identity to merge on.
			merged = append(merged, err)
		case unhealthy[marketplacePinCoordinateOfPin(pinErr)]:
			// The inspection already reports this pin's health failure; a
			// second row for the same pin adds no information.
		case marketplace.IsCacheMetadataMissingError(err):
			// The pin was selected but never acquired: no local evidence
			// exists at all. The MARKETPLACE PINS table already reports the
			// pin with its cache state, so this stays soft and attributable —
			// a hard error would fail every not-yet-synced selection.
			merged = append(merged, &marketplaceCacheMetadataMissingWarning{pin: pinErr})
		case marketplace.IsCacheEvidenceError(err):
			merged = append(merged, &marketplaceCacheEvidenceError{pin: pinErr})
		default:
			merged = append(merged, err)
		}
	}
	return artifacts, merged
}

// resolveMarketplaceArtifacts mirrors the interface dispatch the resource
// resolver performs, so the adapter and the resolver cannot drift on which
// Marketplace resolver entry point is honored.
func resolveMarketplaceArtifacts(ctx context.Context, delegate resolver.MarketplaceResolver, cfg *config.Config, targets []hcc.Target, mode resolver.MarketplaceResolveMode, policy resolver.MarketplaceRevocationPolicy) ([]resolver.ResolvedArtifact, []error) {
	if withPolicy, ok := delegate.(resolver.MarketplaceResolverWithPolicy); ok {
		return withPolicy.ResolveWithPolicy(ctx, cfg, targets, mode, policy)
	}
	return delegate.Resolve(ctx, cfg, targets, mode)
}

// marketplacePinCoordinate identifies one Marketplace pin by endpoint,
// registry, kind, and name. Endpoint URLs are canonical in both the
// inspection and the resolver, so equivalent config spellings share a key.
type marketplacePinCoordinate struct {
	url      string
	registry string
	kind     string
	name     string
}

func marketplacePinCoordinateOfStatus(pin marketplace.PinStatus) marketplacePinCoordinate {
	return marketplacePinCoordinate{url: pin.URL, registry: pin.RegistryID, kind: pin.Kind, name: pin.Name}
}

func marketplacePinCoordinateOfPin(err *marketplace.PinError) marketplacePinCoordinate {
	return marketplacePinCoordinate{url: err.Entry.URL, registry: err.Pin.RegistryID, kind: err.Pin.Kind, name: err.Pin.Name}
}

// marketplaceCacheEvidenceError names a Marketplace pin whose local cache
// evidence is missing or unverified and directs the user to the acquisition
// command. Unwrapping reaches the pin error, so identity checks and
// MarketplacePinError collection keep working through the wrap.
type marketplaceCacheEvidenceError struct {
	pin *marketplace.PinError
}

func (e *marketplaceCacheEvidenceError) Error() string {
	return e.pin.Error() + "; run 'agent-marketplace sync' to acquire the pinned revision from the Marketplace"
}

func (e *marketplaceCacheEvidenceError) Unwrap() error { return e.pin }

// marketplaceCacheMetadataMissingWarning reports a Marketplace pin that was
// selected but never acquired: the pinned revision has no local cache
// evidence at all. It is a soft fact — the pins table already reports the
// pin with its cache state, and a not-yet-synced selection is not a failure —
// but it still carries pin identity, so a dependent's unmet-requirement
// warning can be suppressed rather than telling the user to select something
// that already is selected.
type marketplaceCacheMetadataMissingWarning struct {
	pin *marketplace.PinError
}

func (e *marketplaceCacheMetadataMissingWarning) Error() string {
	return e.pin.Error() + "; run 'agent-marketplace sync' to acquire the pinned revision from the Marketplace"
}

// Remediation names the single action that turns the pin into a resolvable
// artifact set.
func (e *marketplaceCacheMetadataMissingWarning) Remediation() string {
	return "run 'agent-marketplace sync' to acquire the pinned revision from the Marketplace"
}

func (e *marketplaceCacheMetadataMissingWarning) Unwrap() error { return e.pin }

// IsMarketplaceCacheMetadataMissingWarning reports whether err is the
// never-acquired-pin warning produced by cache-backed Marketplace artifact
// resolution. Such a pin is a soft fact: the run continues and the pin is
// reported with its cache state, not failed.
func IsMarketplaceCacheMetadataMissingWarning(err error) bool {
	var warning *marketplaceCacheMetadataMissingWarning
	return errors.As(err, &warning)
}

// MarketplacePendingPins collects the Marketplace pins that resolution
// attempted but could not turn into artifacts. Their hard resolution errors
// already name them, so the read-only commands that resolve Marketplace
// selections from cache (status and doctor) use this set to avoid reporting a
// selected pin as an unmet prerequisite: the remediation for "it is not
// selected" would tell the user to select something that already is. The
// pending key is the pin's kind and its selected name, including the installed
// name when one overrides it.
func MarketplacePendingPins(errGroups ...[]error) map[string]struct{} {
	pending := make(map[string]struct{})
	for _, group := range errGroups {
		for _, err := range group {
			var pinErr *marketplace.PinError
			if errors.As(err, &pinErr) {
				if pinErr != nil {
					addMarketplacePendingPin(pending, pinErr.Pin.Kind, pinErr.Pin.Name, pinErr.Pin.InstalledName)
				}
				continue
			}
			var statusErr *marketplace.PinStatusError
			if errors.As(err, &statusErr) && statusErr != nil {
				addMarketplacePendingPin(pending, statusErr.Pin.Kind, statusErr.Pin.Name, statusErr.Pin.InstalledName)
			}
		}
	}
	return pending
}

func addMarketplacePendingPin(pending map[string]struct{}, kind, name, installedName string) {
	pending[marketplacePendingKey(kind, name)] = struct{}{}
	if installedName != "" && installedName != name {
		pending[marketplacePendingKey(kind, installedName)] = struct{}{}
	}
}

// marketplacePendingKey pairs a requirement kind with a resource name. Pin
// kinds and ir.ResourceKind spellings share one vocabulary, including
// "mcpserver".
func marketplacePendingKey(kind, name string) string {
	return kind + "\x00" + name
}

// SuppressUnmetMarketplaceRequirements drops unmet-prerequisite warnings whose
// prerequisite is a Marketplace pin in the pending set collected by
// MarketplacePendingPins. Every other warning passes through unchanged, and an
// empty pending set returns the input untouched.
func SuppressUnmetMarketplaceRequirements(warnings []error, pending map[string]struct{}) []error {
	if len(pending) == 0 || len(warnings) == 0 {
		return warnings
	}
	kept := make([]error, 0, len(warnings))
	for _, warning := range warnings {
		var unmet *resolver.UnmetRequirementWarning
		if errors.As(warning, &unmet) && unmet != nil {
			if _, hit := pending[marketplacePendingKey(unmet.Requirement.Kind.String(), unmet.Requirement.Name)]; hit {
				continue
			}
		}
		kept = append(kept, warning)
	}
	return kept
}
