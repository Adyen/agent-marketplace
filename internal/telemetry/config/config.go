// Package telemetryconfig defines the telemetry configuration schema and its
// validation rules.
package telemetryconfig

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/logging/logvalue"
	"github.com/Adyen/agent-marketplace/internal/redact"
)

const (
	LevelOff      = "off"
	LevelBasic    = "basic"
	LevelDetailed = "detailed"

	// LevelDiagnostic is a per-invocation overlay and must not be persisted as a
	// telemetry level in configuration.
	LevelDiagnostic = "diagnostic"

	SchemaVersion = "1"
	SchemaMajor   = 1
	SchemaMinor   = 2
)

// ResourceAttribute names a telemetry resource attribute that is allowed in the
// v1 schema. Attribute names are intentionally low-cardinality.
type ResourceAttribute string

const (
	ResourceAttrSchemaVersion  ResourceAttribute = "telemetry.schema.version"
	ResourceAttrCLIVersion     ResourceAttribute = "cli.version"
	ResourceAttrOSName         ResourceAttribute = "os.name"
	ResourceAttrArch           ResourceAttribute = "arch"
	ResourceAttrCommandName    ResourceAttribute = "command.name"
	ResourceAttrHarnesses      ResourceAttribute = "harnesses"
	ResourceAttrManaged        ResourceAttribute = "managed"
	ResourceAttrOutcomeCode    ResourceAttribute = "outcome.code"
	ResourceAttrInstallationID ResourceAttribute = "installation.id"
)

// MetricInstrument names the telemetry schema instruments.
type MetricInstrument string

const (
	MetricNamespace                         = "agent_marketplace"
	CommandCounterName     MetricInstrument = MetricNamespace + ".command.invocations"
	CommandDurationName    MetricInstrument = MetricNamespace + ".command.duration_ms"
	InventorySnapshotName  MetricInstrument = MetricNamespace + ".config.snapshot"
	InventoryRegistryName  MetricInstrument = MetricNamespace + ".config.registry"
	InventoryResourceName  MetricInstrument = MetricNamespace + ".config.resource"
	InventoryOmissionsName MetricInstrument = MetricNamespace + ".config.inventory_omissions"
)

// MetricAttribute names an allowlisted metric instrument attribute.
type MetricAttribute string

const (
	MetricAttrConfigFingerprint MetricAttribute = "config.fingerprint"
	MetricAttrHarnesses         MetricAttribute = "harnesses"
	MetricAttrHarnessScope      MetricAttribute = "harness.scope"
	MetricAttrRegistryCount     MetricAttribute = "registry.count"
	MetricAttrResourceCount     MetricAttribute = "resource.count"
	MetricAttrRegistryID        MetricAttribute = "registry.id"
	MetricAttrRegistryLocation  MetricAttribute = "registry.location"
	MetricAttrRegistryPath      MetricAttribute = "registry.path"
	MetricAttrRegistryRef       MetricAttribute = "registry.ref"
	MetricAttrConfigScope       MetricAttribute = "config.scope"
	MetricAttrRegistryRequired  MetricAttribute = "registry.required"
	MetricAttrRegistryDefault   MetricAttribute = "registry.default"
	MetricAttrResourceKind      MetricAttribute = "resource.kind"
	MetricAttrResourceName      MetricAttribute = "resource.name"
	MetricAttrResourceRequired  MetricAttribute = "resource.required"
	MetricAttrSourceKind        MetricAttribute = "source.kind"
	MetricAttrMarketplaceID     MetricAttribute = "marketplace.id"
	MetricAttrOmissionReason    MetricAttribute = "reason"
)

var InventorySnapshotAttributes = []MetricAttribute{
	MetricAttrConfigFingerprint,
	MetricAttrHarnesses,
	MetricAttrHarnessScope,
	MetricAttrRegistryCount,
	MetricAttrResourceCount,
}

var InventoryRegistryAttributes = []MetricAttribute{
	MetricAttrConfigFingerprint,
	MetricAttrRegistryID,
	MetricAttrRegistryLocation,
	MetricAttrRegistryPath,
	MetricAttrRegistryRef,
	MetricAttrConfigScope,
	MetricAttrRegistryRequired,
	MetricAttrRegistryDefault,
	MetricAttrSourceKind,
	MetricAttrMarketplaceID,
}

var InventoryOmissionAttributes = []MetricAttribute{
	MetricAttrOmissionReason,
}

var InventoryResourceAttributes = []MetricAttribute{
	MetricAttrConfigFingerprint,
	MetricAttrRegistryID,
	MetricAttrRegistryLocation,
	MetricAttrRegistryPath,
	MetricAttrRegistryRef,
	MetricAttrResourceKind,
	MetricAttrResourceName,
	MetricAttrConfigScope,
	MetricAttrResourceRequired,
	MetricAttrSourceKind,
	MetricAttrMarketplaceID,
}

// MetricSchemaAttributes is the v1 metric attribute allowlist. These are the
// only dimensions that may be attached to the command counter and duration
// histogram. Resource attributes (including schema version, CLI version, OS,
// arch, and installation.id) are intentionally omitted from instrument labels.
var MetricSchemaAttributes = []ResourceAttribute{
	ResourceAttrCommandName,
	ResourceAttrHarnesses,
	ResourceAttrManaged,
	ResourceAttrOutcomeCode,
}

// TelemetryConfig is the machine-scope telemetry policy. It is only allowed in
// organization and user-personal configuration layers.
type TelemetryConfig struct {
	Level              string                  `yaml:"level,omitempty"`
	Endpoint           string                  `yaml:"endpoint,omitempty"`
	CommunityReporting *bool                   `yaml:"community_reporting,omitempty"`
	RetentionPolicy    string                  `yaml:"retention_policy,omitempty"`
	CAFile             string                  `yaml:"ca_file,omitempty"`
	AllowInsecure      bool                    `yaml:"allow_insecure,omitempty"`
	Headers            map[string]HeaderSource `yaml:"headers,omitempty"`
}

// HeaderSource describes how to retrieve a sensitive header value without
// storing it inline in configuration.
type HeaderSource struct {
	Env  string `yaml:"env,omitempty"`
	File string `yaml:"file,omitempty"`
}

// Source returns the source kind ("env" or "file") and the reference value.
// It returns ("", "") when no source is configured.
func (h HeaderSource) Source() (string, string) {
	if h.Env != "" {
		return "env", h.Env
	}
	if h.File != "" {
		return "file", h.File
	}
	return "", ""
}

// NormalizeLevel canonicalizes a persisted telemetry level. It rejects the
// diagnostic overlay because that is a per-invocation flag.
func NormalizeLevel(level string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", LevelOff:
		return LevelOff, nil
	case LevelBasic:
		return LevelBasic, nil
	case LevelDetailed:
		return LevelDetailed, nil
	case LevelDiagnostic:
		return "", fmt.Errorf("diagnostic is a per-invocation flag, not a persisted telemetry level")
	default:
		return "", fmt.Errorf("telemetry level must be one of %s, %s, %s, got %q", LevelOff, LevelBasic, LevelDetailed, level)
	}
}

// LevelOrder returns the precedence of a level for max() comparisons.
func LevelOrder(level string) int {
	switch level {
	case LevelBasic:
		return 1
	case LevelDetailed:
		return 2
	default:
		return 0
	}
}

// LookupIP resolves a hostname to IP addresses. It defaults to net.LookupIP and
// may be overridden in tests to avoid real DNS lookups.
var LookupIP = net.LookupIP

// Validate checks a telemetry config block against the schema and layer rules.
func Validate(tc *TelemetryConfig, layer string) []error {
	if tc == nil {
		return nil
	}

	slog.Debug("validating telemetry config", "layer", layer)

	if !layerAllowsTelemetry(layer) {
		slog.Warn("telemetry is not allowed in configuration layer", "layer", layer)
		return []error{fmt.Errorf("telemetry is not allowed in %s configuration", layer)}
	}

	var errs []error
	if _, err := NormalizeLevel(tc.Level); err != nil {
		errs = append(errs, err)
	}
	// allow_insecure is only meaningful for organization-configured private endpoints.
	allowInsecure := tc.AllowInsecure && layer == "organization"
	if err := ValidateEndpoint(tc.Endpoint, allowInsecure); err != nil {
		errs = append(errs, err)
	}
	if err := validateRetentionPolicy(tc.RetentionPolicy); err != nil {
		errs = append(errs, err)
	}
	if err := validateCAFile(tc.CAFile); err != nil {
		errs = append(errs, err)
	}
	errs = append(errs, validateHeaders(tc.Headers)...)
	return errs
}

// ValidateParserSafe validates retained configuration values without applying
// layer policy or performing DNS/network lookups. It is intended for strict
// schema parsers that must reject malformed auxiliary values before dispatch.
func ValidateParserSafe(tc *TelemetryConfig) []error {
	if tc == nil {
		return nil
	}
	var errs []error
	if _, err := NormalizeLevel(tc.Level); err != nil {
		errs = append(errs, err)
	}
	if err := ValidateEndpointSyntax(tc.Endpoint); err != nil {
		errs = append(errs, err)
	}
	if err := ValidateRetentionPolicy(tc.RetentionPolicy); err != nil {
		errs = append(errs, err)
	}
	if err := ValidateCAFile(tc.CAFile); err != nil {
		errs = append(errs, err)
	}
	errs = append(errs, ValidateHeaders(tc.Headers)...)
	return errs
}

// ValidateEndpointSyntax checks endpoint URL syntax without DNS or transport
// policy checks. Layer-specific insecure HTTP policy belongs to dispatch.
func ValidateEndpointSyntax(endpoint string) error {
	if endpoint == "" {
		return nil
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("endpoint %q is not a valid URL: %w", redact.URLValue(endpoint), redact.Error(err))
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("endpoint %q must use http or https, got %q", redact.URLValue(endpoint), u.Scheme)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("endpoint %q must have a host", redact.URLValue(endpoint))
	}
	return nil
}

// ValidateEndpoint checks the endpoint syntax and static transport policy.
// DNS-dependent private-hostname validation is best effort: a DNS failure is
// logged and treated as a soft failure so offline configuration loading remains
// available.
func ValidateEndpoint(endpoint string, allowInsecure bool) error {
	return validateEndpoint(endpoint, allowInsecure, false)
}

// ValidateEndpointForDelivery performs the endpoint checks that must happen
// immediately before an HTTP request. In particular, an organization endpoint
// allowed to use insecure HTTP must resolve exclusively to private, loopback,
// or link-local addresses. DNS failures are hard failures at delivery time.
func ValidateEndpointForDelivery(endpoint string, allowInsecure bool) error {
	return validateEndpoint(endpoint, allowInsecure, true)
}

func validateEndpoint(endpoint string, allowInsecure, dnsFailureIsError bool) error {
	if endpoint == "" {
		return nil
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("endpoint %q is not a valid URL: %w", redact.URLValue(endpoint), redact.Error(err))
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("endpoint %q must use http or https, got %q", redact.URLValue(endpoint), u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("endpoint %q must have a host", redact.URLValue(endpoint))
	}
	if u.Scheme == "https" {
		return nil
	}

	if isLoopbackHost(host) {
		slog.Debug("allowing insecure HTTP endpoint", "host", logvalue.Safe(host), "reason", "loopback")
		return nil
	}

	// Literal IP address.
	if ip := net.ParseIP(host); ip != nil {
		if isLoopbackIP(ip) {
			slog.Debug("allowing insecure HTTP endpoint", "host", logvalue.Safe(host), "reason", "loopback_ip")
			return nil
		}
		if isPrivateOrLinkLocalIP(ip) {
			if allowInsecure {
				slog.Debug("allowing insecure HTTP endpoint", "host", logvalue.Safe(host), "reason", "private_ip")
				return nil
			}
			slog.Warn("rejecting insecure HTTP endpoint", "host", logvalue.Safe(host), "reason", "allow_insecure_required")
			return fmt.Errorf("insecure HTTP for private endpoint %q requires allow_insecure: true from organization policy", host)
		}
		slog.Warn("rejecting insecure HTTP endpoint", "host", logvalue.Safe(host), "reason", "public_ip")
		return fmt.Errorf("endpoint %q must use HTTPS for non-loopback, non-private hosts", redact.URLValue(endpoint))
	}

	// Hostnames are only allowed for insecure HTTP under organization policy.
	if !allowInsecure {
		slog.Warn("rejecting insecure HTTP endpoint", "host", logvalue.Safe(host), "reason", "hostname_requires_allow_insecure")
		return fmt.Errorf("insecure HTTP for non-loopback, non-private hostname %q requires allow_insecure: true from organization policy", host)
	}
	ips, err := LookupIP(host)
	if err != nil || len(ips) == 0 {
		slog.Warn("could not resolve insecure HTTP hostname", "host", logvalue.Safe(host), "reason", "hostname_does_not_resolve", "error", logvalue.Safe(err), "delivery_check", dnsFailureIsError)
		if !dnsFailureIsError {
			return nil
		}
		return fmt.Errorf("insecure HTTP hostname %q does not resolve to a private/loopback/link-local address", host)
	}
	for _, ip := range ips {
		if !isPrivateOrLinkLocalIP(ip) {
			slog.Warn("rejecting insecure HTTP endpoint", "host", logvalue.Safe(host), "resolved_ip", ip, "reason", "public_ip")
			return fmt.Errorf("insecure HTTP hostname %q resolves to public IP %v", host, ip)
		}
	}
	slog.Info("allowing insecure HTTP endpoint for private hostname", "host", logvalue.Safe(host), "ips", ips)
	return nil
}

func layerAllowsTelemetry(layer string) bool {
	switch layer {
	case "organization", "user personal":
		return true
	default:
		return false
	}
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isLoopbackIP(ip net.IP) bool {
	return ip.IsLoopback()
}

func isPrivateOrLinkLocalIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}

// ValidateRetentionPolicy checks the optional retention-policy endpoint.
func ValidateRetentionPolicy(urlStr string) error { return validateRetentionPolicy(urlStr) }

func validateRetentionPolicy(urlStr string) error {
	if urlStr == "" {
		return nil
	}
	u, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("retention_policy %q is not a valid URL: %w", redact.URLValue(urlStr), redact.Error(err))
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("retention_policy %q must use http or https, got %q", redact.URLValue(urlStr), u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("retention_policy %q must have a host", redact.URLValue(urlStr))
	}
	return nil
}

// ValidateCAFile validates an optional CA file using the telemetry file policy.
func ValidateCAFile(path string) error { return validateCAFile(path) }

func validateCAFile(path string) error {
	if path == "" {
		return nil
	}
	if _, err := validateSecretPath(path, "ca_file"); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return nil
}

// validateSecretPath checks that path is a regular file owned by the current
// user, with no group/other permissions, and located in a similarly owned parent
// directory. Symlinks are rejected.
func validateSecretPath(path, kind string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("%s %q: %w", kind, path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s %q is a symlink", kind, path)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s %q is a directory", kind, path)
	}
	if err := validateFileOwnerAndPermission(info, path, kind); err != nil {
		return nil, err
	}
	if err := validateParentDirectory(path, kind); err != nil {
		return nil, err
	}
	return info, nil
}

func validateFileOwnerAndPermission(info os.FileInfo, path, kind string) error {
	perm := info.Mode().Perm()
	if perm&0o077 != 0 {
		return fmt.Errorf("%s %q is readable or writable by group/other (%04o); restrict permissions to owner only", kind, path, perm)
	}

	uid := os.Getuid()
	if uid < 0 {
		// Ownership is not reliably available on this platform (e.g. Windows).
		return nil
	}
	owner, ok := fileOwnerUID(info)
	if !ok {
		return nil
	}
	if owner != uid {
		return fmt.Errorf("%s %q is owned by uid %d; expected current user uid %d", kind, path, owner, uid)
	}
	return nil
}

func validateParentDirectory(path, kind string) error {
	parent := filepath.Dir(path)
	if parent == "" || parent == path {
		return nil
	}
	info, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("%s parent directory %q: %w", kind, parent, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s parent directory %q is a symlink", kind, parent)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s parent %q is not a directory", kind, parent)
	}
	return validateFileOwnerAndPermission(info, parent, kind+" parent directory")
}

// ValidateHeaders validates header source declarations and any available
// values without making network requests.
func ValidateHeaders(headers map[string]HeaderSource) []error { return validateHeaders(headers) }

func validateHeaders(headers map[string]HeaderSource) []error {
	if len(headers) == 0 {
		return nil
	}
	var errs []error
	for name, src := range headers {
		if strings.TrimSpace(name) == "" {
			errs = append(errs, fmt.Errorf("telemetry headers contain an empty header name"))
			continue
		}
		if src.Env != "" && src.File != "" {
			errs = append(errs, fmt.Errorf("telemetry header %q has both env and file sources", name))
			continue
		}
		if src.Env == "" && src.File == "" {
			errs = append(errs, fmt.Errorf("telemetry header %q must specify either env or file source", name))
			continue
		}
		if src.Env != "" {
			if strings.TrimSpace(src.Env) == "" {
				errs = append(errs, fmt.Errorf("telemetry header %q has an empty env source", name))
				continue
			}
			value := os.Getenv(src.Env)
			if containsLineTerminator(value) {
				errs = append(errs, fmt.Errorf("telemetry header %q env value for %q contains line terminators", name, src.Env))
			}
			continue
		}
		if strings.TrimSpace(src.File) == "" {
			errs = append(errs, fmt.Errorf("telemetry header %q has an empty file source", name))
			continue
		}
		if _, err := validateSecretPath(src.File, "telemetry header"); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			errs = append(errs, err)
			continue
		}
		data, err := os.ReadFile(src.File)
		if err != nil {
			errs = append(errs, fmt.Errorf("telemetry header %q file source %q: %w", name, src.File, err))
			continue
		}
		value := strings.TrimSpace(string(data))
		if containsLineTerminator(value) {
			errs = append(errs, fmt.Errorf("telemetry header %q file value contains line terminators", name))
		}
	}
	return errs
}

func containsLineTerminator(s string) bool {
	for _, r := range s {
		switch r {
		case '\u000A', '\u000B', '\u000C', '\u000D', '\u0085', '\u2028', '\u2029':
			return true
		}
	}
	return false
}
