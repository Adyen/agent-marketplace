package marketplace

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/urlutil"
)

// MarketplaceLocator is the resolver-independent form of a Marketplace
// resource locator. Marketplace contains either the user-facing alias from a
// compact locator or the canonical endpoint URL from a browser locator. Alias
// and URL are also populated explicitly so callers do not need to infer which
// form was used.
//
// A digest-looking revision is retained in Revision and Digest. This is useful
// to callers that need to turn a locator into an exact pin, while keeping
// parsing independent from Marketplace discovery and alias resolution.
type MarketplaceLocator struct {
	Marketplace string
	Alias       string
	URL         string
	Source      string
	Kind        string
	Name        string
	Revision    string
	Digest      string
}

// ResourceLocator and Locator are compatibility-friendly names for the same
// typed representation.
type ResourceLocator = MarketplaceLocator
type Locator = MarketplaceLocator

// ParseMarketplaceLocator parses either
//
//	alias:source/kind/name[@revision-or-digest]
//	https://host/s/source/kind/name[/revisions/revision]
//
// Alias resolution is deliberately not performed here. The returned source,
// kind, and name are the route tokens, not decoded or otherwise rewritten
// identifiers.
func ParseMarketplaceLocator(raw string) (MarketplaceLocator, error) {
	input := strings.TrimSpace(raw)
	if input == "" {
		return MarketplaceLocator{}, fmt.Errorf("marketplace locator is required")
	}
	if hasControl(input) {
		return MarketplaceLocator{}, fmt.Errorf("marketplace locator contains a control character")
	}
	if strings.HasPrefix(strings.ToLower(input), "http://") || strings.HasPrefix(strings.ToLower(input), "https://") {
		return parseBrowserLocator(input)
	}
	return parseAliasLocator(input)
}

// ParseLocator is a short alias for ParseMarketplaceLocator.
func ParseLocator(raw string) (MarketplaceLocator, error) {
	return ParseMarketplaceLocator(raw)
}

// ParseResourceLocator is a short alias for ParseMarketplaceLocator.
func ParseResourceLocator(raw string) (MarketplaceLocator, error) {
	return ParseMarketplaceLocator(raw)
}

// NormalizeMarketplaceURL canonicalizes an endpoint URL for Marketplace
// identity comparisons. It accepts only HTTP(S) endpoint URLs, removes a
// trailing slash, and normalizes scheme and host casing. Browser route paths
// are not accepted as endpoint URLs; parse them with ParseMarketplaceLocator.
func NormalizeMarketplaceURL(raw string) (string, error) {
	return urlutil.NormalizeEndpointURL(raw)
}

// ValidateInstalledName validates the filesystem-facing name retained in a
// Marketplace pin. It intentionally accepts ordinary Unicode names, but
// rejects separators, controls, Windows-invalid punctuation, DOS device names,
// and trailing dots or spaces.
func ValidateInstalledName(name string) error {
	if err := nameutil.ValidateSafeFilename(name); err != nil {
		return fmt.Errorf("installed name: %w", err)
	}
	return nil
}

func parseAliasLocator(input string) (MarketplaceLocator, error) {
	colon := strings.IndexByte(input, ':')
	if colon <= 0 {
		return MarketplaceLocator{}, fmt.Errorf("invalid marketplace locator %q: expected alias:source/kind/name", input)
	}
	alias := input[:colon]
	coordinate := input[colon+1:]
	if err := validateLocatorSegment("marketplace alias", alias); err != nil {
		return MarketplaceLocator{}, err
	}
	if strings.ContainsAny(alias, "@:") {
		return MarketplaceLocator{}, fmt.Errorf("marketplace alias contains an ambiguous locator separator")
	}
	parts := strings.Split(coordinate, "/")
	if len(parts) != 3 {
		return MarketplaceLocator{}, fmt.Errorf("invalid marketplace locator %q: expected alias:source/kind/name", input)
	}
	if strings.ContainsAny(parts[0], "@:") {
		return MarketplaceLocator{}, fmt.Errorf("source contains an ambiguous locator separator")
	}
	if err := validateRouteToken("source", parts[0]); err != nil {
		return MarketplaceLocator{}, err
	}
	if err := validateKind(parts[1]); err != nil {
		return MarketplaceLocator{}, err
	}
	name, revision, digest, err := splitNameRevision(parts[2])
	if err != nil {
		return MarketplaceLocator{}, err
	}
	return MarketplaceLocator{
		Marketplace: alias,
		Alias:       alias,
		Source:      parts[0],
		Kind:        parts[1],
		Name:        name,
		Revision:    revision,
		Digest:      digest,
	}, nil
}

func parseBrowserLocator(input string) (MarketplaceLocator, error) {
	u, err := url.Parse(input)
	if err != nil {
		return MarketplaceLocator{}, fmt.Errorf("invalid browser locator: %w", err)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return MarketplaceLocator{}, fmt.Errorf("browser locator must use http or https")
	}
	if u.Host == "" || u.Hostname() == "" || u.User != nil {
		return MarketplaceLocator{}, fmt.Errorf("browser locator must contain a non-empty hostname and no user info")
	}
	if u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return MarketplaceLocator{}, fmt.Errorf("browser locator must not contain a query or fragment")
	}
	if hasUnsafeURLPath(u) {
		return MarketplaceLocator{}, fmt.Errorf("browser locator contains an unsafe path")
	}
	path := strings.TrimPrefix(u.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) != 4 && len(parts) != 6 {
		return MarketplaceLocator{}, fmt.Errorf("invalid browser locator %q: expected /s/source/kind/name with optional /revisions/revision", input)
	}
	if parts[0] != "s" || (len(parts) == 6 && parts[4] != "revisions") {
		return MarketplaceLocator{}, fmt.Errorf("invalid browser locator %q: invalid browser route", input)
	}
	for i, token := range parts {
		if token == "" {
			return MarketplaceLocator{}, fmt.Errorf("invalid browser locator %q: empty path segment", input)
		}
		if i == 0 || (len(parts) == 6 && i == 4) {
			continue
		}
		if err := validateRouteToken("browser path segment", token); err != nil {
			return MarketplaceLocator{}, err
		}
	}
	if err := validateKind(parts[2]); err != nil {
		return MarketplaceLocator{}, err
	}
	base := *u
	base.Path = ""
	base.RawPath = ""
	base.RawQuery = ""
	base.Fragment = ""
	base.ForceQuery = false
	endpoint, err := urlutil.NormalizeEndpointURL(base.String())
	if err != nil {
		return MarketplaceLocator{}, fmt.Errorf("invalid browser locator endpoint: %w", err)
	}
	return MarketplaceLocator{
		Marketplace: endpoint,
		URL:         endpoint,
		Source:      parts[1],
		Kind:        parts[2],
		Name:        parts[3],
		Revision:    browserRevision(parts),
		Digest:      digestForRevision(browserRevision(parts)),
	}, nil
}

func browserRevision(parts []string) string {
	if len(parts) == 6 {
		return parts[5]
	}
	return ""
}

func splitNameRevision(value string) (string, string, string, error) {
	if strings.Count(value, "@") > 1 {
		return "", "", "", fmt.Errorf("resource name/revision contains more than one @ separator")
	}
	name, revision := value, ""
	if at := strings.IndexByte(value, '@'); at >= 0 {
		name, revision = value[:at], value[at+1:]
		if revision == "" {
			return "", "", "", fmt.Errorf("resource revision is required after @")
		}
		if err := validateRouteToken("resource revision", revision); err != nil {
			return "", "", "", err
		}
	}
	if err := validateRouteToken("resource name", name); err != nil {
		return "", "", "", err
	}
	return name, revision, digestForRevision(revision), nil
}

func digestForRevision(value string) string {
	if strings.HasPrefix(value, "sha256:") && len(value) == len("sha256:")+64 {
		for _, r := range value[len("sha256:"):] {
			if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
				return ""
			}
		}
		return value
	}
	return ""
}

func validateKind(kind string) error {
	if !api.IsMarketplaceKind(kind) {
		return fmt.Errorf("invalid Marketplace resource kind %q", kind)
	}
	return nil
}

func validateRouteToken(label, value string) error {
	if err := validateLocatorSegment(label, value); err != nil {
		return err
	}
	if strings.Contains(value, "%") {
		return fmt.Errorf("%s contains an encoded or unsafe separator", label)
	}
	return nil
}

func validateLocatorSegment(label, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", label)
	}
	if value != strings.TrimSpace(value) {
		return fmt.Errorf("%s must not have leading or trailing whitespace", label)
	}
	if value == "." || value == ".." {
		return fmt.Errorf("%s must not be a path component %q", label, value)
	}
	if strings.ContainsAny(value, "/\\") {
		return fmt.Errorf("%s contains an unsafe path separator", label)
	}
	if hasControl(value) {
		return fmt.Errorf("%s contains a control character", label)
	}
	return nil
}

func hasControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func hasUnsafeURLPath(u *url.URL) bool {
	if u == nil {
		return true
	}
	if strings.ContainsAny(u.EscapedPath(), "\\") || strings.Contains(u.EscapedPath(), "%2f") || strings.Contains(u.EscapedPath(), "%2F") || strings.Contains(u.EscapedPath(), "%5c") || strings.Contains(u.EscapedPath(), "%5C") {
		return true
	}
	for _, r := range u.Path {
		if unicode.IsControl(r) {
			return true
		}
	}
	return strings.Contains(u.Path, "../") || strings.Contains(u.Path, "/..") || strings.Contains(u.Path, "/./")
}
