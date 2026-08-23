package config

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/urlutil"
)

// MarketplaceEntry is one configured marketplace endpoint and optional exact pins.
type MarketplaceEntry struct {
	URL        string                   `yaml:"url"`
	InstanceID string                   `yaml:"instance_id"`
	Alias      string                   `yaml:"alias,omitempty"`
	Resources  []MarketplaceResourcePin `yaml:"resources,omitempty"`
	Source     Source                   `yaml:"-"`
}

// MarketplaceResourcePin is an exact Marketplace resource coordinate and digest.
type MarketplaceResourcePin struct {
	SourceID      string `yaml:"source_id"`
	SourceSlug    string `yaml:"source_slug,omitempty"`
	Kind          string `yaml:"kind"`
	Name          string `yaml:"name"`
	Revision      string `yaml:"revision"`
	Digest        string `yaml:"digest"`                // "sha256:<64 lowercase hex>"
	Acquisition   string `yaml:"acquisition,omitempty"` // "mirror" or "index-only"
	InstalledName string `yaml:"installed_name,omitempty"`
}

const (
	MarketplaceAcquisitionMirror    = "mirror"
	MarketplaceAcquisitionIndexOnly = "index-only"
)

// validateAllowedMarketplacePatterns validates that every allowlist entry is a
// valid regular expression.
func validateAllowedMarketplacePatterns(patterns *[]string) []error {
	if patterns == nil {
		return nil
	}
	var errs []error
	for i, p := range *patterns {
		if _, err := regexp.Compile(p); err != nil {
			errs = append(errs, fmt.Errorf("allowed_marketplaces[%d] is not a valid regex: %w", i, err))
		}
	}
	return errs
}

// ValidateMarketplaceEntries validates a slice of marketplace entries.
func ValidateMarketplaceEntries(entries []MarketplaceEntry) []error {
	var errs []error
	aliases := make(map[string]int)
	for i, e := range entries {
		errs = append(errs, validateMarketplaceEntry(i, e)...)
		if e.Alias == "" {
			continue
		}
		if previous, ok := aliases[e.Alias]; ok {
			errs = append(errs, fmt.Errorf("marketplaces[%d].alias %q duplicates marketplaces[%d].alias", i, e.Alias, previous))
			continue
		}
		aliases[e.Alias] = i
	}
	return errs
}

func validateMarketplaceEntry(i int, e MarketplaceEntry) []error {
	var errs []error
	if strings.TrimSpace(e.URL) == "" {
		errs = append(errs, fmt.Errorf("marketplaces[%d].url is required", i))
	} else if _, err := urlutil.NormalizeEndpointURL(e.URL); err != nil {
		errs = append(errs, fmt.Errorf("marketplaces[%d].url: %w", i, err))
	}
	if err := validateMarketplaceSegment("instance_id", e.InstanceID); err != nil {
		errs = append(errs, fmt.Errorf("marketplaces[%d].%w", i, err))
	}
	if e.Alias != "" {
		if err := validateMarketplaceSegment("alias", e.Alias); err != nil {
			errs = append(errs, fmt.Errorf("marketplaces[%d].%w", i, err))
		} else if e.Alias == "all" {
			errs = append(errs, fmt.Errorf("marketplaces[%d].alias %q is reserved for federated search", i, e.Alias))
		}
	}
	for j, p := range e.Resources {
		errs = append(errs, validateMarketplacePin(i, j, p)...)
	}
	return errs
}

func validateMarketplacePin(entryIdx, pinIdx int, p MarketplaceResourcePin) []error {
	var errs []error
	if err := validateMarketplaceSegment("source_id", p.SourceID); err != nil {
		errs = append(errs, fmt.Errorf("marketplaces[%d].resources[%d].%w", entryIdx, pinIdx, err))
	}
	if p.SourceSlug != "" {
		if err := validateMarketplaceSegment("source_slug", p.SourceSlug); err != nil {
			errs = append(errs, fmt.Errorf("marketplaces[%d].resources[%d].%w", entryIdx, pinIdx, err))
		}
	}
	if !api.IsMarketplaceKind(p.Kind) {
		errs = append(errs, fmt.Errorf("marketplaces[%d].resources[%d].kind %q is not a valid marketplace kind", entryIdx, pinIdx, p.Kind))
	}
	if err := validateMarketplaceSegment("name", p.Name); err != nil {
		errs = append(errs, fmt.Errorf("marketplaces[%d].resources[%d].%w", entryIdx, pinIdx, err))
	}
	if err := validateMarketplaceSegment("revision", p.Revision); err != nil {
		errs = append(errs, fmt.Errorf("marketplaces[%d].resources[%d].%w", entryIdx, pinIdx, err))
	}
	if !api.IsCanonicalSHA256Digest(p.Digest) {
		errs = append(errs, fmt.Errorf("marketplaces[%d].resources[%d].digest must be sha256:<64-hex>", entryIdx, pinIdx))
	}
	if p.Acquisition != "" && p.Acquisition != MarketplaceAcquisitionMirror && p.Acquisition != MarketplaceAcquisitionIndexOnly {
		errs = append(errs, fmt.Errorf("marketplaces[%d].resources[%d].acquisition must be %q or %q", entryIdx, pinIdx, MarketplaceAcquisitionMirror, MarketplaceAcquisitionIndexOnly))
	}
	if err := ValidateMarketplaceInstalledName(p.InstalledName); err != nil {
		errs = append(errs, fmt.Errorf("marketplaces[%d].resources[%d].%w", entryIdx, pinIdx, err))
	}
	return errs
}

// IsMarketplaceKind reports whether kind is a selectable Marketplace resource kind.
// Kept in config as a thin wrapper so consumers do not need to import api.
func IsMarketplaceKind(kind string) bool {
	return api.IsMarketplaceKind(kind)
}
