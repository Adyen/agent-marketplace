package marketplace

import (
	"fmt"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// RevocationOverride is an exact, invocation-scoped authorization for one
// revoked Marketplace revision.
type RevocationOverride struct {
	Coordinate    store.ResourceCoordinate
	ContentDigest store.RevisionDigest
}

// RevocationOverridePolicy holds only the overrides supplied to one command
// invocation. It has no config or cache persistence.
type RevocationOverridePolicy struct {
	overrides []RevocationOverride
}

// ParseRevocationOverride parses the canonical ResourceCoordinate followed by
// an exact content digest: marketplace/source/kind/name@sha256:<64 lowercase hex>.
func ParseRevocationOverride(raw string) (RevocationOverride, error) {
	input := strings.TrimSpace(raw)
	if input == "" {
		return RevocationOverride{}, fmt.Errorf("revoked override is required")
	}
	if strings.Count(input, "@") != 1 {
		return RevocationOverride{}, fmt.Errorf("invalid revoked override %q: expected marketplace/source/kind/name@sha256:<64 lowercase hex>", input)
	}
	at := strings.LastIndexByte(input, '@')
	coordinate, digest := input[:at], input[at+1:]
	parsed, err := store.ParseResourceCoordinate(coordinate)
	if err != nil {
		return RevocationOverride{}, err
	}
	for _, field := range []struct {
		label string
		value string
	}{
		{label: "marketplace instance", value: string(parsed.Marketplace)},
		{label: "registry", value: string(parsed.Registry)},
		{label: "resource name", value: parsed.Name},
	} {
		if err := validateRouteToken(field.label, field.value); err != nil {
			return RevocationOverride{}, err
		}
	}
	if err := validateKind(parsed.Kind); err != nil {
		return RevocationOverride{}, err
	}
	if err := validateSHA256Digest(digest); err != nil {
		return RevocationOverride{}, fmt.Errorf("content digest %q: %w", digest, err)
	}
	return RevocationOverride{Coordinate: parsed, ContentDigest: store.RevisionDigest(digest)}, nil
}

// NewRevocationOverridePolicy validates and retains one or more exact
// invocation-scoped overrides.
func NewRevocationOverridePolicy(raw []string) (*RevocationOverridePolicy, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	policy := &RevocationOverridePolicy{overrides: make([]RevocationOverride, 0, len(raw))}
	for _, value := range raw {
		override, err := ParseRevocationOverride(value)
		if err != nil {
			return nil, err
		}
		policy.overrides = append(policy.overrides, override)
	}
	return policy, nil
}

// AllowRevoked implements resolver.MarketplaceRevocationPolicy.
func (p *RevocationOverridePolicy) AllowRevoked(identity resolver.MarketplaceRevocationIdentity) bool {
	if p == nil {
		return false
	}
	for _, override := range p.overrides {
		if string(override.Coordinate.Marketplace) == identity.Marketplace &&
			string(override.Coordinate.Registry) == identity.Registry &&
			override.Coordinate.Kind == identity.Kind &&
			override.Coordinate.Name == identity.Name &&
			string(override.ContentDigest) == identity.ContentDigest {
			return true
		}
	}
	return false
}
