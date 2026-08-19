package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/api"
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
	Digest        string `yaml:"digest"`                // "sha256:<hex>"
	Acquisition   string `yaml:"acquisition,omitempty"` // "mirror" or "index"
	InstalledName string `yaml:"installed_name,omitempty"`
}

const (
	MarketplaceAcquisitionMirror = "mirror"
	MarketplaceAcquisitionIndex  = "index"
)

var contentDigestRE = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

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
	for i, e := range entries {
		errs = append(errs, validateMarketplaceEntry(i, e)...)
	}
	return errs
}

func validateMarketplaceEntry(i int, e MarketplaceEntry) []error {
	var errs []error
	if strings.TrimSpace(e.URL) == "" {
		errs = append(errs, fmt.Errorf("marketplaces[%d].url is required", i))
	} else if u, err := url.Parse(e.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "") {
		errs = append(errs, fmt.Errorf("marketplaces[%d].url must be an http or https URL", i))
	}
	if strings.TrimSpace(e.InstanceID) == "" {
		errs = append(errs, fmt.Errorf("marketplaces[%d].instance_id is required", i))
	}
	if strings.Contains(e.URL, "..") {
		errs = append(errs, fmt.Errorf("marketplaces[%d].url must not contain path traversal", i))
	}
	for j, p := range e.Resources {
		errs = append(errs, validateMarketplacePin(i, j, p)...)
	}
	return errs
}

func validateMarketplacePin(entryIdx, pinIdx int, p MarketplaceResourcePin) []error {
	var errs []error
	if strings.TrimSpace(p.SourceID) == "" {
		errs = append(errs, fmt.Errorf("marketplaces[%d].resources[%d].source_id is required", entryIdx, pinIdx))
	}
	if !api.IsMarketplaceKind(p.Kind) {
		errs = append(errs, fmt.Errorf("marketplaces[%d].resources[%d].kind %q is not a valid marketplace kind", entryIdx, pinIdx, p.Kind))
	}
	if strings.TrimSpace(p.Name) == "" {
		errs = append(errs, fmt.Errorf("marketplaces[%d].resources[%d].name is required", entryIdx, pinIdx))
	}
	if strings.TrimSpace(p.Revision) == "" {
		errs = append(errs, fmt.Errorf("marketplaces[%d].resources[%d].revision is required", entryIdx, pinIdx))
	}
	if !contentDigestRE.MatchString(p.Digest) {
		errs = append(errs, fmt.Errorf("marketplaces[%d].resources[%d].digest must be sha256:<64-hex>", entryIdx, pinIdx))
	}
	if p.Acquisition != "" && p.Acquisition != MarketplaceAcquisitionMirror && p.Acquisition != MarketplaceAcquisitionIndex {
		errs = append(errs, fmt.Errorf("marketplaces[%d].resources[%d].acquisition must be %q or %q", entryIdx, pinIdx, MarketplaceAcquisitionMirror, MarketplaceAcquisitionIndex))
	}
	return errs
}

// IsMarketplaceKind reports whether kind is a selectable Marketplace resource kind.
// Kept in config as a thin wrapper so consumers do not need to import api.
func IsMarketplaceKind(kind string) bool {
	return api.IsMarketplaceKind(kind)
}
