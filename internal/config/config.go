package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	telemetryconfig "github.com/Adyen/agent-marketplace/internal/telemetry/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"gopkg.in/yaml.v3"
)

// Source identifies which configuration layer a registry entry originated from.
// Layers are ordered from lowest to highest precedence; a higher Source value
// wins in override scenarios.
type Source int

const (
	// SourceUnset is the zero value. Unset entries behave like RepoCommitted
	// (repo-scoped, not machine-wide). Merge always sets an explicit Source on
	// every registry entry it produces; SourceUnset only appears in config
	// structs that were not passed through Merge (e.g. in unit tests).
	SourceUnset Source = iota
	// Organization is the externally managed organization config
	// (/etc/agent-marketplace/config.yaml).
	Organization
	// UserPersonal is the user's own machine-wide config (~/.agents/config.yaml).
	UserPersonal
	// RepoCommitted is the committed per-repo config (<repo>/.agents/config.yaml).
	RepoCommitted
	// RepoPersonal is the gitignored personal override (<repo>/.agents/config.local.yaml).
	RepoPersonal
)

// Precedence returns the numeric precedence of the source.
// Higher values override lower values.
func (s Source) Precedence() int { return int(s) }

// CompareEntryPrecedence compares two entries by layer and then file order.
// It returns zero when they have equal authority. File order is ignored if
// either file value is zero, preserving behavior for configs without includes.
func CompareEntryPrecedence(aSource Source, aFile int, bSource Source, bFile int) int {
	if aSource.Precedence() != bSource.Precedence() {
		return aSource.Precedence() - bSource.Precedence()
	}
	if aFile == 0 || bFile == 0 {
		return 0
	}
	return aFile - bFile
}

// IsMachineScope reports whether skills from this source are installed at
// the user-wide scope rather than the per-repo scope.
// SourceUnset (zero value) is treated as repo-scoped.
func (s Source) IsMachineScope() bool { return s == Organization || s == UserPersonal }

// IsWritable reports whether the CLI may write (install/remove) to this
// source's config file.
func (s Source) IsWritable() bool {
	return s == UserPersonal || s == RepoCommitted || s == RepoPersonal
}

// CanConfigureTelemetry reports whether this source may contain telemetry
// policy. Only organization and user-personal layers are machine-scoped.
func (s Source) CanConfigureTelemetry() bool { return s == Organization || s == UserPersonal }

// String returns a short human-readable label used in status output.
func (s Source) String() string {
	switch s {
	case SourceUnset:
		return "-"
	case Organization:
		return "ORG"
	case UserPersonal:
		return "USER"
	case RepoCommitted:
		return "REPO"
	case RepoPersonal:
		return "REPO*"
	default:
		return "UNKNOWN"
	}
}

// Layer pairs a Source with the Config loaded from that source.
type Layer struct {
	Source Source
	Config *Config
}

// AllKinds returns the set of resource kinds that have dedicated KindSelector
// fields on LocalConfig and RegistryEntry. Adding a new kind requires:
// 1) Adding a field to LocalConfig
// 2) Adding a field to RegistryEntry
// 3) Adding a case to SelectorForKind/SelectorForKindPtr (used by entryResourcesForKind)
// Use this slice (AllKinds) for iteration instead of hard-coding 5-field patterns.
var AllKinds = []ir.ResourceKind{
	ir.KindSkill,
	ir.KindCommand,
	ir.KindSubagent,
	ir.KindHook,
	ir.KindMcpServer,
	ir.KindCollection,
}

// CollectionRef is a legacy type kept for backward compatibility with test
// code. New code should use the Collections KindSelector field instead.
// Deprecated: use Config.Collections (KindSelector) directly.
type CollectionRef struct {
	Name string `yaml:"name"`
}

// KindSelector holds per-kind include/exclude lists. It supports backward-
// compatible YAML deserialization: a bare sequence like `skills: [a, b]` is
// treated as `{include: [a, b], exclude: null}`, while the new map form
// `skills: {include: [a], exclude: [b]}` is the canonical representation.
// Marshal always emits the map form.
type KindSelector struct {
	Include    []string `yaml:"include,omitempty"`
	Exclude    []string `yaml:"exclude,omitempty"`
	ExcludeAll bool     `yaml:"exclude_all,omitempty"`
}

// UnmarshalYAML accepts both the legacy list form and the new map form:
//
//	skills: [a, b]                  → {include: [a, b]}
//	skills: {include: [a], exclude: [b]}  → as-is
func (ks *KindSelector) UnmarshalYAML(value *yaml.Node) error {
	// Try list form first: a sequence node maps directly to Include.
	if value.Kind == yaml.SequenceNode {
		var items []string
		if err := value.Decode(&items); err != nil {
			return err
		}
		ks.Include = items
		return nil
	}

	// Map form: decode normally.
	if value.Kind == yaml.MappingNode {
		// Validate that only allowed keys are present
		allowedKeys := map[string]bool{"include": true, "exclude": true, "exclude_all": true}
		for i := 0; i < len(value.Content); i += 2 {
			keyNode := value.Content[i]
			if keyNode.Value != "" && !allowedKeys[keyNode.Value] {
				return fmt.Errorf("unknown key %q in KindSelector; allowed keys are: include, exclude, exclude_all", keyNode.Value)
			}
		}

		type raw KindSelector
		var r raw
		if err := value.Decode(&r); err != nil {
			return err
		}
		ks.Include = r.Include
		ks.Exclude = r.Exclude
		ks.ExcludeAll = r.ExcludeAll
		return nil
	}

	// Null/empty node: leave as zero value.
	if value.Kind == 0 || (value.Kind == yaml.ScalarNode && value.Tag == "!!null") {
		return nil
	}

	return fmt.Errorf("expected a sequence or mapping for KindSelector, got %s", value.Tag)
}

// IsZero reports whether all fields are empty/unset.
func (ks KindSelector) IsZero() bool {
	return len(ks.Include) == 0 && len(ks.Exclude) == 0 && !ks.ExcludeAll
}

// BackupConfig controls automatic pre-sync backups of machine-scope
// harness directories.
type BackupConfig struct {
	Enabled   *bool `yaml:"enabled,omitempty"`
	MaxDays   *int  `yaml:"max_days,omitempty"`
	MaxPerDay *int  `yaml:"max_per_day,omitempty"`
}

// MaterializationMode controls how compiled resources are installed. Symlink
// keeps cache-backed installations (the historical default); File writes
// portable regular files and skill trees instead.
type MaterializationMode string

const (
	MaterializationModeSymlink MaterializationMode = "symlink"
	MaterializationModeFile    MaterializationMode = "file"
)

// MaterializationConfig is intentionally small so it can be safely committed
// in either user or project configuration.
type MaterializationConfig struct {
	Mode string `yaml:"mode,omitempty"`
}

// MaterializationModes is the fully resolved policy for each install scope.
// Project is allowed to differ from Machine; repo configuration must never
// influence machine-wide materialization.
type MaterializationModes struct {
	Machine MaterializationMode
	Project MaterializationMode
}

func DefaultMaterializationModes() MaterializationModes {
	return MaterializationModes{Machine: MaterializationModeSymlink, Project: MaterializationModeSymlink}
}

func normalizeMaterializationMode(mode string) (MaterializationMode, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", string(MaterializationModeSymlink):
		return MaterializationModeSymlink, nil
	case string(MaterializationModeFile):
		return MaterializationModeFile, nil
	default:
		return "", fmt.Errorf("materialization.mode must be one of symlink, file, got %q", mode)
	}
}

// ResolveMaterializationModes applies the intentionally asymmetric precedence:
// the user/global layer provides the default for both scopes, while committed
// and local project configuration can override only the project scope.
func ResolveMaterializationModes(global, repo *Config, local *LocalConfig) (MaterializationModes, error) {
	modes := DefaultMaterializationModes()
	if global != nil && global.Materialization != nil {
		mode, err := normalizeMaterializationMode(global.Materialization.Mode)
		if err != nil {
			return modes, err
		}
		modes.Machine, modes.Project = mode, mode
	}
	if repo != nil && repo.Materialization != nil {
		mode, err := normalizeMaterializationMode(repo.Materialization.Mode)
		if err != nil {
			return modes, err
		}
		modes.Project = mode
	}
	if local != nil && local.Materialization != nil {
		mode, err := normalizeMaterializationMode(local.Materialization.Mode)
		if err != nil {
			return modes, err
		}
		modes.Project = mode
	}
	return modes, nil
}

// Default user-level logging settings. These are the single source of truth
// for the logging runtime (internal/logging) when no explicit logging config
// is provided. The generated global skeleton deliberately omits a logging
// block; users opt in by adding one (see docs/logging.md).
const (
	DefaultLogEnabled    = true
	DefaultLogLevel      = "info"
	DefaultLogMaxSizeMB  = 10
	DefaultLogMaxBackups = 5
	DefaultLogMaxAgeDays = 30
	DefaultLogCompress   = true

	// DefaultLogDirName is the home-relative directory that holds operational
	// logs. Logs are runtime state, not regenerable cache, so they live
	// outside ~/.cache.
	DefaultLogDirName  = ".agent-marketplace"
	DefaultLogSubdir   = "logs"
	DefaultLogFileName = "agent-marketplace.log"
)

// DefaultOwnershipLockTimeout bounds how long an ownership mutation waits for
// another process to leave the per-canonical-root critical section
// (internal/ownership). It lives here, with the other defaults, because a user
// can override it through AGENT_MARKETPLACE_OWNERSHIP_LOCK_TIMEOUT and the
// project's defaults policy makes this file the single source of truth for
// anything overridable.
//
// 30s is ≫ any legitimate section (a handful of reads, a jsonc patch, and the
// fsyncs of one journaled generation — single-digit milliseconds; git fetch,
// catalog load, resolution and compilation all happen outside it) and ≪ any CI
// job timeout, so an expiry is always a real anomaly worth reporting rather than
// a tuning knob. See docs/plans/2026-08-06-ownership-locking-design.md §3.
//
// internal/ownership re-declares this value instead of importing it, because that
// package is the durable-state leaf and must not acquire a transitive dependency
// on this file's YAML loader to read one integer. (The import would compile
// everywhere, GOOS=windows included; the cost is layering, not portability.)
// The two are kept identical by
// TestDefaultLockTimeoutMatchesTheConfigDefault, which imports this package from a
// test file only.
const DefaultOwnershipLockTimeout = 30 * time.Second

// LoggingConfig controls user-level operational file logging.
type LoggingConfig struct {
	Enabled    *bool   `yaml:"enabled,omitempty"`
	Level      *string `yaml:"level,omitempty"`
	File       *string `yaml:"file,omitempty"`
	MaxSizeMB  *int    `yaml:"max_size_mb,omitempty"`
	MaxBackups *int    `yaml:"max_backups,omitempty"`
	MaxAgeDays *int    `yaml:"max_age_days,omitempty"`
	Compress   *bool   `yaml:"compress,omitempty"`
}

type IncludePaths []string

func (p *IncludePaths) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		if value.Tag == "!!null" {
			return nil
		}
		var path string
		if err := value.Decode(&path); err != nil {
			return err
		}
		*p = IncludePaths{path}
		return nil
	case yaml.SequenceNode:
		var paths []string
		if err := value.Decode(&paths); err != nil {
			return err
		}
		*p = paths
		return nil
	default:
		return fmt.Errorf("include must be a path or sequence of paths")
	}
}

type Config struct {
	Include IncludePaths `yaml:"include,omitempty"`

	// APIVersion is the schema version sentinel. The canonical version is
	// "agent-marketplace/v2"; "agent-marketplace/v3" is retained as a
	// compatibility alias. v1 configs have no apiVersion field and are
	// rejected by the loader.
	APIVersion string `yaml:"apiVersion,omitempty"`

	// Harnesses lists the target harnesses to materialize into (e.g.
	// "factory", "cursor", "claude"). Required at v2; the loader errors
	// hard if absent.
	Harnesses   []string        `yaml:"harnesses,omitempty"`
	Registries  []RegistryEntry `yaml:"registries,omitempty"`
	Collections KindSelector    `yaml:"collections,omitempty"`

	// Top-level per-kind defaults. During merge, each registry entry inherits
	// these selectors unless it provides its own per-entry selectors.
	Skills    KindSelector `yaml:"skills,omitempty"`
	Commands  KindSelector `yaml:"commands,omitempty"`
	Subagents KindSelector `yaml:"subagents,omitempty"`
	Hooks     KindSelector `yaml:"hooks,omitempty"`
	MCP       KindSelector `yaml:"mcp,omitempty"`

	Tags            []string               `yaml:"tags,omitempty"`
	Backup          *BackupConfig          `yaml:"backup,omitempty"`
	Logging         *LoggingConfig         `yaml:"logging,omitempty"`
	Materialization *MaterializationConfig `yaml:"materialization,omitempty"`

	// AllowedRegistries is a list of regex patterns that non-local registry
	// URLs must match. If nil, no allowlist is configured and all remote
	// origins are permitted. If non-nil and empty, all remote registries
	// are denied. A non-empty list permits only URLs matching at least one
	// pattern.
	//
	// Uses *[]string rather than []string to preserve the nil vs empty
	// distinction through YAML marshal/unmarshal round-trips (omitempty
	// on []string loses the distinction).
	AllowedRegistries *[]string `yaml:"allowed_registries,omitempty"`

	// Telemetry is the machine-scope telemetry policy. It is only allowed in
	// the organization and user-personal configuration layers.
	Telemetry *telemetryconfig.TelemetryConfig `yaml:"telemetry,omitempty"`
}

// SelectorForKind returns the top-level KindSelector for the given resource
// kind. Returns the zero-value KindSelector for unknown kinds.
func (c *Config) SelectorForKind(kind ir.ResourceKind) KindSelector {
	if c == nil {
		return KindSelector{}
	}
	if s, ok := c.SelectorForKindOk(kind); ok {
		return s
	}
	return KindSelector{}
}

// SelectorForKindPtr returns a pointer to the top-level KindSelector for the
// given resource kind, or nil for unknown kinds.
func (c *Config) SelectorForKindPtr(kind ir.ResourceKind) *KindSelector {
	if c == nil {
		return nil
	}
	switch kind {
	case ir.KindSkill:
		return &c.Skills
	case ir.KindCommand:
		return &c.Commands
	case ir.KindSubagent:
		return &c.Subagents
	case ir.KindHook:
		return &c.Hooks
	case ir.KindMcpServer:
		return &c.MCP
	case ir.KindCollection:
		return &c.Collections
	default:
		return nil
	}
}

// SelectorForKindOk returns the top-level KindSelector and true for known
// kinds, or zero and false for unknown kinds.
func (c *Config) SelectorForKindOk(kind ir.ResourceKind) (KindSelector, bool) {
	if p := c.SelectorForKindPtr(kind); p != nil {
		return *p, true
	}
	return KindSelector{}, false
}

type LocalConfig struct {
	Include IncludePaths `yaml:"include,omitempty"`

	// APIVersion is an optional schema-version sentinel. Local configs
	// (config.local.yaml) are generated without it, but users may add
	// "agent-marketplace/v2" or "agent-marketplace/v3" by hand (or copy it
	// from a sibling config). It is accepted but not required; when present it
	// must match one of the supported API versions. Without this field the strict
	// (KnownFields) decoder rejects any hand-added apiVersion line.
	APIVersion  string          `yaml:"apiVersion,omitempty"`
	Registries  []RegistryEntry `yaml:"registries,omitempty"`
	Skills      KindSelector    `yaml:"skills,omitempty"`
	Commands    KindSelector    `yaml:"commands,omitempty"`
	Subagents   KindSelector    `yaml:"subagents,omitempty"`
	Hooks       KindSelector    `yaml:"hooks,omitempty"`
	MCP         KindSelector    `yaml:"mcp,omitempty"`
	Collections KindSelector    `yaml:"collections,omitempty"`
	// Materialization is a project-only override. It never changes the
	// user-wide policy resolved from ~/.agents/config.yaml.
	Materialization *MaterializationConfig `yaml:"materialization,omitempty"`
}

// SelectorForKind returns the KindSelector for the given resource kind.
// Returns the zero-value KindSelector for unknown kinds.
func (c *LocalConfig) SelectorForKind(kind ir.ResourceKind) KindSelector {
	if c == nil {
		return KindSelector{}
	}
	if s, ok := c.SelectorForKindOk(kind); ok {
		return s
	}
	return KindSelector{}
}

// SelectorForKindPtr returns a pointer to the KindSelector for the given
// resource kind, or nil for unknown kinds. Used when the caller needs to
// modify the selector in-place.
func (c *LocalConfig) SelectorForKindPtr(kind ir.ResourceKind) *KindSelector {
	if c == nil {
		return nil
	}
	switch kind {
	case ir.KindSkill:
		return &c.Skills
	case ir.KindCommand:
		return &c.Commands
	case ir.KindSubagent:
		return &c.Subagents
	case ir.KindHook:
		return &c.Hooks
	case ir.KindMcpServer:
		return &c.MCP
	case ir.KindCollection:
		return &c.Collections
	default:
		return nil
	}
}

// SelectorForKindOk returns the KindSelector and true for known kinds,
// or zero and false for unknown kinds.
func (c *LocalConfig) SelectorForKindOk(kind ir.ResourceKind) (KindSelector, bool) {
	if p := c.SelectorForKindPtr(kind); p != nil {
		return *p, true
	}
	return KindSelector{}, false
}

type RegistryEntry struct {
	URL      string `yaml:"url"`
	Ref      string `yaml:"ref"`
	Path     string `yaml:"path,omitempty"`
	Required bool   `yaml:"required,omitempty"`
	// Default, when true, causes all skills in this registry to be selected
	// automatically (same as Required for selection). Unlike Required, skills
	// from a Default registry CAN be excluded at a higher-precedence layer.
	Default bool `yaml:"default,omitempty"`
	// ExcludeAll, when true, propagates ExcludeAll=true to all per-kind
	// selectors during merge resolution. This is sugar for setting
	// skills.exclude_all, commands.exclude_all, etc. individually.
	// At a higher-precedence layer, it suppresses all non-required resources
	// from the same registry URL at lower-precedence layers whose registry
	// paths are covered by this entry's path: a root-scoped (empty path) entry
	// excludes every sub-path of the URL, while a non-root path only affects
	// that path and its descendants (component-wise prefix, per isPathPrefix).
	// A sibling path of the same URL is unaffected.
	ExcludeAll  bool         `yaml:"exclude_all,omitempty"`
	Skills      KindSelector `yaml:"skills,omitempty"`
	Tags        []string     `yaml:"tags,omitempty"`
	Commands    KindSelector `yaml:"commands,omitempty"`
	Subagents   KindSelector `yaml:"subagents,omitempty"`
	Hooks       KindSelector `yaml:"hooks,omitempty"`
	MCP         KindSelector `yaml:"mcp,omitempty"`
	Collections KindSelector `yaml:"collections,omitempty"`

	// SourceHarness specifies the harness layout of this registry's content.
	// Optional; defaults to hcc.Autodetect at sync time.
	SourceHarness string `yaml:"source_harness,omitempty"`

	// Harnesses overrides the top-level harnesses list for this entry.
	// Optional; inherits from Config.Harnesses if unset.
	Harnesses []string `yaml:"harnesses,omitempty"`

	// Source identifies which configuration layer this entry originated from.
	// Set during merge; not serialized in YAML.
	Source Source `yaml:"-"`

	// FilePrecedence orders registry entries from files composed into one layer.
	// Higher values have higher precedence. Zero retains equal-source behavior
	// for configs without includes.
	FilePrecedence int `yaml:"-"`

	// CatalogKey is the key used to look up this registry's skill catalog in
	// the catalogs map. For git registries it is the cache path; for local
	// registries it is the resolved absolute path. Set by catalog loading;
	// not serialized in YAML.
	CatalogKey string `yaml:"-"`
}

// UnmarshalYAML implements custom YAML deserialization for RegistryEntry.
// It supports "path" as an alias for "url" when "url" is not specified,
// allowing users to write more natural config for local-path registries:
//
//	registries:
//	  - path: /local/skills      # alias for "url"
//	  - url: git@host:repo.git    # traditional git URL
//	    ref: main
//	    path: skills              # subdirectory within repo
//
// Rules:
//   - "url" always maps to the URL field (registry location).
//   - "path" maps to URL when "url" is absent (alias for local paths).
//   - "path" maps to Path when "url" is present (subdirectory).
func (e *RegistryEntry) UnmarshalYAML(value *yaml.Node) error {
	// Use a raw type that mirrors RegistryEntry but without the custom
	// unmarshaler, so yaml.Unmarshal handles all standard fields.
	type rawEntry struct {
		URL           string       `yaml:"url"`
		Ref           string       `yaml:"ref"`
		PathAlias     string       `yaml:"path"`
		Required      bool         `yaml:"required,omitempty"`
		Default       bool         `yaml:"default,omitempty"`
		ExcludeAll    bool         `yaml:"exclude_all,omitempty"`
		Skills        KindSelector `yaml:"skills,omitempty"`
		Tags          []string     `yaml:"tags,omitempty"`
		Commands      KindSelector `yaml:"commands,omitempty"`
		Subagents     KindSelector `yaml:"subagents,omitempty"`
		Hooks         KindSelector `yaml:"hooks,omitempty"`
		MCP           KindSelector `yaml:"mcp,omitempty"`
		Collections   KindSelector `yaml:"collections,omitempty"`
		SourceHarness string       `yaml:"source_harness,omitempty"`
		Harnesses     []string     `yaml:"harnesses,omitempty"`
	}

	var raw rawEntry
	if err := value.Decode(&raw); err != nil {
		return err
	}

	// Determine URL: "url" takes precedence; "path" is an alias when "url" is absent.
	e.URL = strings.TrimSpace(raw.URL)
	if e.URL == "" {
		e.URL = strings.TrimSpace(raw.PathAlias)
	} else {
		// "url" is present, so "path" means the subdirectory.
		e.Path = strings.TrimSpace(raw.PathAlias)
	}

	e.Ref = raw.Ref
	e.Required = raw.Required
	e.Default = raw.Default
	e.ExcludeAll = raw.ExcludeAll
	e.Skills = raw.Skills
	e.Tags = raw.Tags
	e.Commands = raw.Commands
	e.Subagents = raw.Subagents
	e.Hooks = raw.Hooks
	e.MCP = raw.MCP
	e.Collections = raw.Collections
	e.SourceHarness = raw.SourceHarness
	e.Harnesses = raw.Harnesses

	return nil
}

// SelectorForKind returns the KindSelector for the given resource kind.
// Returns the zero-value KindSelector for unknown kinds.
func (e *RegistryEntry) SelectorForKind(kind ir.ResourceKind) KindSelector {
	if s, ok := e.SelectorForKindOk(kind); ok {
		return s
	}
	return KindSelector{}
}

// SelectorForKindPtr returns a pointer to the KindSelector for the given
// resource kind, or nil for unknown kinds. Used when the caller needs to
// modify the selector in-place.
func (e *RegistryEntry) SelectorForKindPtr(kind ir.ResourceKind) *KindSelector {
	switch kind {
	case ir.KindSkill:
		return &e.Skills
	case ir.KindCommand:
		return &e.Commands
	case ir.KindSubagent:
		return &e.Subagents
	case ir.KindHook:
		return &e.Hooks
	case ir.KindMcpServer:
		return &e.MCP
	case ir.KindCollection:
		return &e.Collections
	default:
		return nil
	}
}

// SelectorForKindOk returns the KindSelector and true for known kinds,
// or zero and false for unknown kinds.
func (e *RegistryEntry) SelectorForKindOk(kind ir.ResourceKind) (KindSelector, bool) {
	if p := e.SelectorForKindPtr(kind); p != nil {
		return *p, true
	}
	return KindSelector{}, false
}

// IsLocal returns true when the URL refers to a local filesystem directory
// containing skills rather than a git remote. A URL is considered local if:
//   - It is a relative path (".", "..", "./x", "../x") — always local,
//     since relative paths cannot be git clone URLs
//   - It starts with '/' or '~/' and does NOT end with '.git'
//   - It does NOT end with '.git' (which signals a git repository)
//
// The .git suffix is the sole signal that a local path is a git bare repo
// (e.g. /path/to/repo.git). Without the .git suffix, an absolute or
// home-relative path is treated as a plain directory and ref is optional
// (and ignored). This ensures ref is optional for local paths, since ref
// does not make sense on a plain local directory.
func (e RegistryEntry) IsLocal() bool {
	u := strings.TrimSpace(e.URL)
	if u == "" {
		return false
	}
	if strings.HasSuffix(u, ".git") {
		return false
	}

	// Relative paths are always local — they cannot be git clone URLs.
	if u == "." || u == ".." || strings.HasPrefix(u, "./") || strings.HasPrefix(u, "../") {
		return true
	}

	// Absolute and home-relative paths without .git suffix are local
	// directories. Ref is optional (and ignored) for these entries.
	return u[0] == '/' || strings.HasPrefix(u, "~/")
}

// IsRepoRelativeLocal reports whether the registry URL is a local filesystem
// path that is relative to a project root (e.g. "./plugins/foo" or ".").
// Such URLs are valid in repo and local configs, where they resolve against the
// current repo root, but they are meaningless in the user-personal (global)
// config layer because they have no stable meaning outside the repo that
// contains them.
func (e RegistryEntry) IsRepoRelativeLocal() bool {
	u := strings.TrimSpace(e.URL)
	return u == "." || u == ".." || strings.HasPrefix(u, "./") || strings.HasPrefix(u, "../")
}

// GlobalRelativeRegistryError is returned when an operation attempts to write a
// repo-relative local registry URL into the user-personal (machine-wide) config
// layer. Such URLs are only meaningful within the repo that defined them.
type GlobalRelativeRegistryError struct {
	URL string
}

func (e *GlobalRelativeRegistryError) Error() string {
	if e == nil {
		return "cannot write repo-relative registry to the global config"
	}
	return fmt.Sprintf("cannot write repo-relative registry %q to the global config; use an absolute path or a git remote", e.URL)
}

func ValidateGlobalRegistryWrite(entries ...RegistryEntry) error {
	for _, entry := range entries {
		if entry.IsRepoRelativeLocal() {
			return &GlobalRelativeRegistryError{URL: entry.URL}
		}
	}
	return nil
}

func validateRegistryWrite(configPath string, entries ...RegistryEntry) error {
	if !isGlobalConfigPath(configPath) {
		return nil
	}
	return ValidateGlobalRegistryWrite(entries...)
}

func ValidateRegistryPath(path string) error {
	p := strings.TrimSpace(path)
	if p == "" {
		return nil
	}
	if filepath.IsAbs(p) {
		return fmt.Errorf("path must be relative, got absolute %q", p)
	}
	if cleaned := filepath.Clean(p); cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path must not escape the registry root, got %q", p)
	}
	return nil
}

func ValidateLocalRegistryPath(path string) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("local registry path does not exist: %s", path)
	}
	if err != nil {
		return fmt.Errorf("stat local registry path %q: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("local registry path is not a directory: %s", path)
	}
	return nil
}

// urlContainsPathTraversal reports whether a registry URL contains a ".." path
// component. This applies to local paths, home-relative paths, and absolute paths;
// it also catches relative-looking URLs that are not valid git remotes. The check
// is performed on the raw URL string before expansion so that validation can run
// without needing a base directory.
func urlContainsPathTraversal(url string) bool {
	u := strings.TrimSpace(url)
	u = strings.TrimPrefix(u, "~/")
	for _, part := range strings.Split(filepath.ToSlash(u), "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

// isSubPath reports whether path is contained within root using only lexical
// (Clean/Rel) checks. It does not follow symlinks, so it works for paths that
// do not yet exist on disk. It returns true for the root itself and for any
// descendant of root; it returns false for paths outside or equal to a parent.
func isSubPath(path, root string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// ResolveLocalPath expands the URL of a local registry entry into an absolute
// directory path. ~ is expanded to the user's home directory; relative paths
// are resolved relative to baseDir and must not escape it. Returns ("", nil) for
// non-local entries.
func (e RegistryEntry) ResolveLocalPath(baseDir string) (string, error) {
	if !e.IsLocal() {
		return "", nil
	}

	u := strings.TrimSpace(e.URL)

	if strings.HasPrefix(u, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory for local registry %q: %w", u, err)
		}
		u = filepath.Join(homeDir, u[2:])
		u = filepath.Clean(u)
		if !isSubPath(u, homeDir) {
			return "", fmt.Errorf("local registry %q resolves outside the home directory", e.URL)
		}
		return u, nil
	}

	if !filepath.IsAbs(u) {
		u = filepath.Join(baseDir, u)
	}
	u = filepath.Clean(u)

	if !filepath.IsAbs(e.URL) && !isSubPath(u, baseDir) {
		return "", fmt.Errorf("local registry %q resolves outside the base directory %q", e.URL, baseDir)
	}

	return u, nil
}

// BundlePath returns the bundle root directory within a registry.
// If entry.Path is empty, it returns basePath (registry root).
// Otherwise, it returns basePath joined with the cleaned entry.Path.
// Absolute and path-traversal values are rejected by Validate; here we
// still clean the path to produce deterministic results.
func (e RegistryEntry) BundlePath(basePath string) string {
	p := strings.TrimSpace(e.Path)
	if p == "" {
		return basePath
	}
	return filepath.Join(basePath, filepath.Clean(p))
}

// ScopeBoundaryError is returned when a repo/local operation attempts to act
// on a machine-scope (ORG/USER/TEAM) skill. The Suggestion field carries an
// alternative action the user can take (e.g. "use --scope user").
type ScopeBoundaryError struct {
	Source     Source
	Target     Source
	Suggestion string
}

func (e *ScopeBoundaryError) Error() string {
	return fmt.Sprintf("scope boundary violation: cannot act on %s resource from %s scope", e.Source, e.Target)
}

const (
	// APIVersionV2 is the canonical apiVersion sentinel.
	APIVersionV2 = "agent-marketplace/v2"
	// APIVersionV3 is an alias that is still accepted for reading but
	// should not be emitted by generators or documentation.
	APIVersionV3 = "agent-marketplace/v3"
)

// Validate performs full structural validation of the config, including
// requiring that harnesses is non-empty. This should be used for global
// (user-personal) configs where harnesses is mandatory.
func (c Config) Validate() error {
	return c.validate(true, true, "user personal")
}

// ValidateWithoutHarnesses validates the config structurally but does not
// require harnesses to be non-empty. This should be used for project/local
// level configs where harnesses is optional (inherited from the global layer
// during merge).
func (c Config) ValidateWithoutHarnesses() error {
	return c.validate(false, true, "repository committed")
}

func (c Config) validate(requireHarnesses, requireAPIVersion bool, layer string) error {
	var errs []error

	// v2 is canonical; v3 is retained as a compatibility alias.
	// apiVersion is required on Config-schema files (global/repo/org) but
	// optional on local configs, which are generated without it.
	if v := strings.TrimSpace(c.APIVersion); v == "" {
		if requireAPIVersion {
			errs = append(errs, fmt.Errorf("missing apiVersion: add %q", APIVersionV2))
		}
	} else if v != APIVersionV2 && v != APIVersionV3 {
		errs = append(errs, fmt.Errorf("unsupported apiVersion %q: expected %q or %q", v, APIVersionV2, APIVersionV3))
	}

	// harnesses is required on user-personal configs and must not be empty.
	// Organization/project/local configs may omit it and inherit from the
	// user layer during merge.
	if requireHarnesses && len(c.Harnesses) == 0 {
		errs = append(errs, fmt.Errorf("harnesses is required: add a harnesses list (e.g. harnesses: [factory])"))
	}

	errs = append(errs, validateConfigHarnessTargets(c)...)

	errs = append(errs, validateRegistryEntries(c.Registries)...)
	errs = append(errs, validateBackup(c.Backup)...)
	errs = append(errs, validateMaterialization(c.Materialization)...)
	if requireHarnesses {
		errs = append(errs, validateLogging(c.Logging)...)
	}

	errs = append(errs, validateTelemetry(c, layer)...)

	if len(errs) == 0 {
		return nil
	}

	return errors.Join(errs...)
}

// ValidateBasic validates only the structural fields of the config (e.g.
// registry entries) without enforcing v2 schema gating (apiVersion,
// harnesses). Used for org-level configs that may not declare apiVersion.
func (c Config) ValidateBasic() error {
	var errs []error

	errs = append(errs, validateConfigHarnessTargets(c)...)

	errs = append(errs, validateRegistryEntries(c.Registries)...)
	errs = append(errs, validateBackup(c.Backup)...)
	errs = append(errs, validateMaterialization(c.Materialization)...)

	if len(errs) == 0 {
		return nil
	}

	return errors.Join(errs...)
}

func validateRegistryEntries(registries []RegistryEntry) []error {
	var errs []error
	for i, registry := range registries {
		if strings.TrimSpace(registry.URL) == "" {
			errs = append(errs, fmt.Errorf("registries[%d].url is required", i))
		}

		if !registry.IsLocal() && strings.TrimSpace(registry.Ref) == "" {
			errs = append(errs, fmt.Errorf("registries[%d].ref is required for git registries", i))
		}

		if urlContainsPathTraversal(registry.URL) {
			errs = append(errs, fmt.Errorf("registries[%d].url must not contain '..' path components", i))
		}

		if err := ValidateRegistryPath(registry.Path); err != nil {
			errs = append(errs, fmt.Errorf("registries[%d].%w", i, err))
		}
	}
	return errs
}

// ToBackupConfig converts the config's BackupConfig (with pointer fields for
// nil-vs-zero detection) into a flat backup.Config with resolved defaults.
func (c *Config) ToBackupConfig() (backupConfig, error) {
	bc := backupConfig{Enabled: true, MaxDays: 14, MaxPerDay: 10}
	if c == nil || c.Backup == nil {
		return bc, nil
	}
	if c.Backup.Enabled != nil {
		bc.Enabled = *c.Backup.Enabled
	}
	if c.Backup.MaxDays != nil {
		bc.MaxDays = *c.Backup.MaxDays
	}
	if c.Backup.MaxPerDay != nil {
		bc.MaxPerDay = *c.Backup.MaxPerDay
	}
	return bc, nil
}

func validateBackup(backup *BackupConfig) []error {
	if backup == nil {
		return nil
	}
	var errs []error
	if backup.MaxDays != nil && *backup.MaxDays <= 0 {
		errs = append(errs, fmt.Errorf("backup.max_days must be positive, got %d", *backup.MaxDays))
	}
	if backup.MaxPerDay != nil && *backup.MaxPerDay <= 0 {
		errs = append(errs, fmt.Errorf("backup.max_per_day must be positive, got %d", *backup.MaxPerDay))
	}
	return errs
}

func validateMaterialization(materialization *MaterializationConfig) []error {
	if materialization == nil {
		return nil
	}
	if _, err := normalizeMaterializationMode(materialization.Mode); err != nil {
		return []error{err}
	}
	return nil
}

func validateLogging(logging *LoggingConfig) []error {
	if logging == nil {
		return nil
	}
	var errs []error
	if logging.Level != nil {
		switch strings.ToLower(strings.TrimSpace(*logging.Level)) {
		case "debug", "info", "warn", "warning", "error":
		default:
			errs = append(errs, fmt.Errorf("logging.level must be one of debug, info, warn, error, got %q", *logging.Level))
		}
	}
	if logging.MaxSizeMB != nil && *logging.MaxSizeMB <= 0 {
		errs = append(errs, fmt.Errorf("logging.max_size_mb must be positive, got %d", *logging.MaxSizeMB))
	}
	if logging.MaxBackups != nil && *logging.MaxBackups < 0 {
		errs = append(errs, fmt.Errorf("logging.max_backups must be non-negative, got %d", *logging.MaxBackups))
	}
	if logging.MaxAgeDays != nil && *logging.MaxAgeDays < 0 {
		errs = append(errs, fmt.Errorf("logging.max_age_days must be non-negative, got %d", *logging.MaxAgeDays))
	}
	return errs
}

func validateTelemetry(c Config, layer string) []error {
	if c.Telemetry == nil {
		return nil
	}
	return telemetryconfig.Validate(c.Telemetry, layer)
}

// backupConfig is the internal representation used before crossing into the
// backup package (avoids a circular import).
type backupConfig struct {
	Enabled   bool
	MaxDays   int
	MaxPerDay int
}

// AllExcludes collects all per-kind excludes from a LocalConfig into a flat
// name set.
func (c LocalConfig) AllExcludes() []string {
	var out []string
	for _, kind := range AllKinds {
		out = append(out, c.SelectorForKind(kind).Exclude...)
	}
	return out
}

// ValidateBasic validates the structural fields of the LocalConfig without
// enforcing v2 schema gating (apiVersion, harnesses). At minimum it ensures
// every registry entry has a non-empty URL and no path-traversal in Path.
// apiVersion is optional on local configs, but if present it must match
// APIVersionV2 or APIVersionV3 so typos and stale versions are caught.
func (c LocalConfig) ValidateBasic() error {
	var errs []error

	errs = append(errs, validateRegistryHarnessTargets(c.Registries)...)

	if v := strings.TrimSpace(c.APIVersion); v != "" && v != APIVersionV2 && v != APIVersionV3 {
		errs = append(errs, fmt.Errorf("unsupported apiVersion %q: expected %q or %q", v, APIVersionV2, APIVersionV3))
	}

	errs = append(errs, validateRegistryEntries(c.Registries)...)
	errs = append(errs, validateMaterialization(c.Materialization)...)

	if len(errs) == 0 {
		return nil
	}

	return errors.Join(errs...)
}

// AllExcludes collects all per-kind excludes from a RegistryEntry into a
// flat name set.
func (e RegistryEntry) AllExcludes() []string {
	var out []string
	for _, kind := range AllKinds {
		out = append(out, e.SelectorForKind(kind).Exclude...)
	}
	return out
}
