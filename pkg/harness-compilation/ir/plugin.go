package ir

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Plugin is a package resource: a plugin directory decoded as one unit,
// keeping package identity and member identity distinct. Members hold the
// leaf resources a plugin carries; a plugin is never lowered through the
// leaf Compile path — package compilation goes through the pluginpolicy
// projection instead.
type Plugin struct {
	// Format is the package format identifier (for example "claude-plugin"
	// or "agent-plugin").
	Format string
	// FormatVersion is the recognized format version the decoder selected
	// (for example "1.0.0"), empty when the format declares no versions.
	FormatVersion string
	// Manifest is the package manifest metadata; Extra preserves
	// unrecognized manifest fields for formats that tolerate them.
	Manifest Manifest
	// Dependencies retains the authored manifest dependency value. The CLI
	// can inspect it even though sync refuses dependency-bearing packages.
	Dependencies any `json:"-"`
	// Members is the ordered member set decoded from the package's
	// component locations.
	Members []PluginMember
	// Files is the contained file set: every regular file under the package
	// root with its clean slash-relative path, sha256 hex digest, and
	// executable bit.
	Files []PluginFile
	// Extensions carries inert client-extension manifest data (raw JSON
	// values); the library assigns it no semantics.
	Extensions map[string]any
	// Invalid records content the decoder skipped and why, each with its
	// diagnostic.
	Invalid []PluginRecord
}

// PluginMember is one leaf resource inside a plugin package.
type PluginMember struct {
	Resource Resource
}

// Key returns the member identity "kind/name" within the package.
func (m PluginMember) Key() string {
	if m.Resource == nil {
		return "unknown/"
	}
	return m.Resource.Kind().String() + "/" + m.Resource.Name()
}

// PluginFile describes one regular file contained in a plugin package.
type PluginFile struct {
	Path       string // Clean slash-relative path from the package root
	Digest     string // Lowercase sha256 hex digest of the file contents
	Executable bool   // Whether any executable bit is set
}

// PluginRecord reports one skipped component, entry, or file and the
// diagnostic that explains the skip.
type PluginRecord struct {
	Path       string // Slash-relative path or member name that was skipped
	Kind       string // What was skipped: "component", "member", "entry", "file"
	Reason     string // Short machine-stable reason (for example "invalid-frontmatter")
	Diagnostic string // Diagnostic code attached to the skip, if any
}

// Kind returns KindPlugin.
func (p *Plugin) Kind() ResourceKind { return KindPlugin }

// Name returns the plugin manifest name.
func (p *Plugin) Name() string { return p.Manifest.Name }

func (p *Plugin) DeclaredDependencies() []string {
	if p == nil || p.Dependencies == nil {
		return nil
	}
	switch dependencies := p.Dependencies.(type) {
	case []any:
		values := make([]string, 0, len(dependencies))
		for _, dependency := range dependencies {
			values = append(values, fmt.Sprint(dependency))
		}
		return values
	case map[string]any:
		values := make([]string, 0, len(dependencies))
		for name := range dependencies {
			values = append(values, name)
		}
		sort.Strings(values)
		return values
	case string:
		if strings.TrimSpace(dependencies) == "" {
			return nil
		}
		return []string{dependencies}
	default:
		return []string{fmt.Sprint(dependencies)}
	}
}

func (p *Plugin) HasDependencies() bool {
	return len(p.DeclaredDependencies()) > 0
}

// agentPluginNamePattern is the character grammar declared by the bundled
// Agent Plugins 1.0.0 plugin schema (kept in sync with
// plugindecode/schemas/plugin.schema.json). Go's regexp has no lookahead, so
// the schema's "no -- or .. runs" lookahead is checked separately in
// ValidatePluginName.
var agentPluginNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$`)

// ValidatePluginName validates a plugin manifest name under its format's
// grammar. Agent Plugins names follow the bundled 1.0.0 schema pattern
// (lowercase, up to 64 characters, no "--" or ".." runs); the native package
// formats reuse the canonical resource-name grammar from ValidateName.
func ValidatePluginName(format, name string) error {
	if format == "agent-plugin" {
		if len(name) < 1 || len(name) > 64 || !agentPluginNamePattern.MatchString(name) ||
			strings.Contains(name, "--") || strings.Contains(name, "..") {
			return fmt.Errorf("agent-plugin name %q must match the Agent Plugins 1.0.0 name grammar (1-64 lowercase letters, digits, dots, and hyphens; no %q or %q runs)", name, "--", "..")
		}
		return nil
	}
	return ValidateName(name)
}
