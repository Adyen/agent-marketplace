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
// An explicit content digest is retained in Revision and Digest. This is
// useful to callers that need to turn a locator into an exact pin, while
// keeping parsing independent from Marketplace discovery and alias resolution.
type MarketplaceLocator struct {
	Marketplace string
	Alias       string
	URL         string
	Registry    string
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
//	alias:registry/kind/name[@sha256:<64 lowercase hex>]
//	https://host[/prefix]/r/registry/kind/name[/revisions/sha256:<64 lowercase hex>]
//
// Alias resolution is deliberately not performed here. The returned registry,
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

// LooksLikeMarketplaceBrowserURL reports whether an HTTP(S) URL contains the
// Marketplace browser resource route marker. Callers can reject malformed
// routes before generic Git URL parsing turns them into SSH clone URLs.
//
// Recognized Git hosting hosts (github.com, gitlab.com, and self-hosted
// hostnames carrying a github/gitlab label) keep generic Git parsing, so
// their paths are never Marketplace-looking. On every other host the check
// fails closed: any "r" path segment marks the URL as a Marketplace browser
// route, whether it starts a complete
// [/prefix]/r/registry/kind/name[/revisions/digest] route or a truncated or
// over-long malformed one. Endpoint bases containing an "r" segment are
// deliberately unsupported on unrecognized hosts; ordinary endpoint bases
// without one stay generic.
func LooksLikeMarketplaceBrowserURL(raw string) bool {
	input := strings.TrimSpace(raw)
	u, err := url.Parse(input)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return false
	}
	if isRecognizedGitHost(u.Hostname()) {
		return false
	}
	for _, part := range strings.Split(strings.Trim(u.Path, "/"), "/") {
		if part == "r" {
			return true
		}
	}
	return false
}

// isRecognizedGitHost reports whether hostname is a recognized Git hosting
// host: github.com, gitlab.com, or a self-hosted hostname carrying a
// github/gitlab label (for example gitlab.example.com).
func isRecognizedGitHost(hostname string) bool {
	for _, label := range strings.Split(strings.ToLower(hostname), ".") {
		if label == "github" || label == "gitlab" {
			return true
		}
	}
	return false
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
		return MarketplaceLocator{}, fmt.Errorf("invalid marketplace locator %q: expected alias:registry/kind/name", input)
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
		return MarketplaceLocator{}, fmt.Errorf("invalid marketplace locator %q: expected alias:registry/kind/name", input)
	}
	if strings.ContainsAny(parts[0], "@:") {
		return MarketplaceLocator{}, fmt.Errorf("registry contains an ambiguous locator separator")
	}
	if err := validateRouteToken("registry", parts[0]); err != nil {
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
		Registry:    parts[0],
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
	allParts := strings.Split(path, "/")
	routeStart := len(allParts) - 4
	if len(allParts) >= 6 && allParts[len(allParts)-6] == "r" && allParts[len(allParts)-2] == "revisions" {
		routeStart = len(allParts) - 6
	}
	if routeStart < 0 || allParts[routeStart] != "r" {
		return MarketplaceLocator{}, fmt.Errorf("invalid browser locator %q: expected [/prefix]/r/registry/kind/name with optional /revisions/revision", input)
	}
	parts := allParts[routeStart:]
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
	revision := browserRevision(parts)
	if revision != "" {
		if err := validateExplicitRevision(revision); err != nil {
			return MarketplaceLocator{}, err
		}
	}
	base := *u
	base.Path = strings.Join(allParts[:routeStart], "/")
	if base.Path != "" {
		base.Path = "/" + base.Path
	}
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
		Registry:    parts[1],
		Kind:        parts[2],
		Name:        parts[3],
		Revision:    revision,
		Digest:      digestForRevision(revision),
	}, nil
}

func browserRevision(parts []string) string {
	if len(parts) == 6 {
		return parts[5]
	}
	return ""
}

func validateExplicitRevision(revision string) error {
	if digestForRevision(revision) == "" {
		return fmt.Errorf("resource revision must be an exact content digest (sha256:<64 lowercase hex>)")
	}
	return nil
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
		if err := validateExplicitRevision(revision); err != nil {
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
