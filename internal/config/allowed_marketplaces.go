package config

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/urlutil"
)

// MarketplaceURLBlockedError is returned by ValidateMarketplaceURLs when one
// or more marketplace URLs do not match any pattern in the allowed_marketplaces
// policy.
type MarketplaceURLBlockedError struct {
	BlockedURLs     []string
	AllowedPatterns []string
}

func (e *MarketplaceURLBlockedError) Error() string {
	if e == nil {
		return "marketplace URL blocked by allowed_marketplaces policy"
	}
	return fmt.Sprintf("marketplace URL(s) blocked by allowed_marketplaces policy: %s (allowed patterns: %s)",
		strings.Join(e.BlockedURLs, ", "), strings.Join(e.AllowedPatterns, ", "))
}

// ValidateMarketplaceURLs checks that every marketplace entry's URL matches at
// least one pattern in patterns. A nil allowlist permits all URLs; an empty
// allowlist denies all non-empty URLs.
func ValidateMarketplaceURLs(entries []MarketplaceEntry, patterns []string) error {
	if patterns == nil {
		return nil
	}

	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return fmt.Errorf("invalid allowed_marketplaces pattern %q: %w", p, err)
		}
		compiled = append(compiled, re)
	}

	var blocked []string
	for _, entry := range entries {
		rawURL := strings.TrimSpace(entry.URL)
		if rawURL == "" {
			continue
		}
		normalized, err := urlutil.NormalizeEndpointURL(rawURL)
		if err != nil {
			return fmt.Errorf("invalid marketplace URL %q: %w", rawURL, err)
		}
		if !matchesAny(normalized, compiled) {
			blocked = append(blocked, rawURL)
		}
	}

	if len(blocked) == 0 {
		return nil
	}
	return &MarketplaceURLBlockedError{BlockedURLs: blocked, AllowedPatterns: patterns}
}
