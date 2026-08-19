// Package telemetryconfig defines the telemetry configuration schema and its
// validation rules.
package telemetryconfig

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
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
	SchemaMinor   = 0
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
	ResourceAttrCoarseDuration ResourceAttribute = "duration.coarse"
	ResourceAttrInstallationID ResourceAttribute = "installation.id"
)

// ResourceSchemaAttributes is the v1 metric attribute allowlist. It contains
// the only attributes that may be attached to basic metrics.
var ResourceSchemaAttributes = []ResourceAttribute{
	ResourceAttrSchemaVersion,
	ResourceAttrCLIVersion,
	ResourceAttrOSName,
	ResourceAttrArch,
	ResourceAttrCommandName,
	ResourceAttrHarnesses,
	ResourceAttrManaged,
	ResourceAttrOutcomeCode,
	ResourceAttrCoarseDuration,
	ResourceAttrInstallationID,
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

// MaxLevel returns the higher of two levels.
func MaxLevel(a, b string) string {
	if LevelOrder(a) >= LevelOrder(b) {
		return a
	}
	return b
}

// Validate checks a telemetry config block against the schema and layer rules.
func Validate(tc *TelemetryConfig, layer string) []error {
	if tc == nil {
		return nil
	}
	if !layerAllowsTelemetry(layer) {
		return []error{fmt.Errorf("telemetry is not allowed in %s configuration", layer)}
	}

	var errs []error
	if _, err := NormalizeLevel(tc.Level); err != nil {
		errs = append(errs, err)
	}
	if err := ValidateEndpoint(tc.Endpoint, tc.AllowInsecure); err != nil {
		errs = append(errs, err)
	}
	if err := validateRetentionPolicy(tc.RetentionPolicy); err != nil {
		errs = append(errs, err)
	}
	if layer == "organization" && tc.Endpoint != "" && tc.RetentionPolicy == "" {
		errs = append(errs, fmt.Errorf("retention_policy is required when an organization endpoint is configured"))
	}
	if err := validateCAFile(tc.CAFile); err != nil {
		errs = append(errs, err)
	}
	errs = append(errs, validateHeaders(tc.Headers)...)
	return errs
}

// ValidateEndpoint checks that an endpoint uses a safe transport.
func ValidateEndpoint(endpoint string, allowInsecure bool) error {
	if endpoint == "" {
		return nil
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("endpoint %q is not a valid URL: %w", endpoint, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("endpoint %q must use http or https, got %q", endpoint, u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("endpoint %q must have a host", endpoint)
	}
	if u.Scheme == "https" {
		return nil
	}
	if isLoopbackHost(host) {
		return nil
	}
	if isPrivateHost(host) && allowInsecure {
		return nil
	}
	if isPrivateHost(host) {
		return fmt.Errorf("insecure HTTP for private endpoint %q requires allow_insecure: true", host)
	}
	return fmt.Errorf("endpoint %q must use HTTPS for non-loopback, non-private hosts", endpoint)
}

func layerAllowsTelemetry(layer string) bool {
	switch layer {
	case "", "organization", "user personal", "user":
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

func isPrivateHost(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsPrivate()
}

func validateRetentionPolicy(urlStr string) error {
	if urlStr == "" {
		return nil
	}
	u, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("retention_policy %q is not a valid URL: %w", urlStr, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("retention_policy %q must use http or https, got %q", urlStr, u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("retention_policy %q must have a host", urlStr)
	}
	return nil
}

func validateCAFile(path string) error {
	if path == "" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("ca_file %q: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("ca_file %q is a directory", path)
	}
	return nil
}

func validateHeaders(headers map[string]HeaderSource) []error {
	if len(headers) == 0 {
		return nil
	}
	var errs []error
	for name, src := range headers {
		if name == "" {
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
			}
			continue
		}
		if strings.TrimSpace(src.File) == "" {
			errs = append(errs, fmt.Errorf("telemetry header %q has an empty file source", name))
			continue
		}
		if _, err := os.Stat(src.File); err != nil {
			errs = append(errs, fmt.Errorf("telemetry header %q file source %q: %w", name, src.File, err))
		}
	}
	return errs
}
