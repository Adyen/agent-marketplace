package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
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

func (s Source) LockScope() string {
	switch s {
	case Organization:
		return "organization"
	case UserPersonal:
		return "user"
	case RepoCommitted:
		return "repo"
	case RepoPersonal:
		return "local"
	default:
		return ""
	}
}

func (s Source) LockFilename() string {
	switch s {
	case Organization, UserPersonal, RepoCommitted:
		return "lock.yaml"
	case RepoPersonal:
		return "lock.local.yaml"
	default:
		return ""
	}
}

func (s Source) LockPath(configPath string) string {
	filename := s.LockFilename()
	if filename == "" || configPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(configPath), filename)
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
// 3) Adding a case to SelectorForKind/SelectorForKindPtr
// Use this slice (AllKinds) for iteration instead of hard-coding 5-field patterns.
var AllKinds = []ir.ResourceKind{
	ir.KindSkill,
	ir.KindCommand,
	ir.KindSubagent,
	ir.KindHook,
	ir.KindMcpServer,
	ir.KindCollection,
}

// KindSelector holds the runtime per-kind selection policy.
type KindSelector struct {
	Include    []string `yaml:"include,omitempty"`
	Exclude    []string `yaml:"exclude,omitempty"`
	ExcludeAll bool     `yaml:"exclude_all,omitempty"`
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
	Mode    string            `yaml:"mode,omitempty"`
	Targets map[string]string `yaml:"targets,omitempty"`
}

// MaterializationModes is the fully resolved policy for each install scope.
// Project is allowed to differ from Machine; repo configuration must never
// influence machine-wide materialization.
type MaterializationModes struct {
	Machine        MaterializationMode
	Project        MaterializationMode
	MachineTargets map[string]MaterializationMode
	ProjectTargets map[string]MaterializationMode
}

func DefaultMaterializationModes() MaterializationModes {
	return MaterializationModes{Machine: MaterializationModeSymlink, Project: MaterializationModeSymlink}
}

func (m MaterializationModes) For(target string, machine bool) MaterializationMode {
	if machine {
		if mode := m.MachineTargets[target]; mode != "" {
			return mode
		}
		return m.Machine
	}
	if mode := m.ProjectTargets[target]; mode != "" {
		return mode
	}
	return m.Project
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
	if global != nil {
		if err := modes.apply(global.Materialization, true); err != nil {
			return modes, err
		}
	}
	if repo != nil {
		if err := modes.apply(repo.Materialization, false); err != nil {
			return modes, err
		}
	}
	if local != nil {
		if err := modes.apply(local.Materialization, false); err != nil {
			return modes, err
		}
	}
	return modes, nil
}

func (m *MaterializationModes) apply(cfg *MaterializationConfig, bothScopes bool) error {
	if cfg == nil {
		return nil
	}
	if cfg.Mode != "" {
		mode, err := normalizeMaterializationMode(cfg.Mode)
		if err != nil {
			return err
		}
		m.Project = mode
		if bothScopes {
			m.Machine = mode
		}
	}
	targets, err := normalizeTargetMaterializationModes(cfg.Targets)
	if err != nil {
		return err
	}
	m.ProjectTargets = mergeMaterializationModes(m.ProjectTargets, targets)
	if bothScopes {
		m.MachineTargets = mergeMaterializationModes(m.MachineTargets, targets)
	}
	return nil
}

func normalizeTargetMaterializationModes(targets map[string]string) (map[string]MaterializationMode, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(targets))
	for target := range targets {
		keys = append(keys, target)
	}
	sort.Strings(keys)
	modes := make(map[string]MaterializationMode, len(targets))
	for _, target := range keys {
		if target != strings.TrimSpace(target) || !IsSupportedHarness(target) {
			return nil, fmt.Errorf("materialization.targets contains unknown harness %q", target)
		}
		if strings.TrimSpace(targets[target]) == "" {
			return nil, fmt.Errorf("materialization.targets.%s must be one of symlink, file", target)
		}
		mode, err := normalizeMaterializationMode(targets[target])
		if err != nil {
			return nil, fmt.Errorf("materialization.targets.%s: %w", target, err)
		}
		modes[target] = mode
	}
	return modes, nil
}

func mergeMaterializationModes(base, override map[string]MaterializationMode) map[string]MaterializationMode {
	if len(override) == 0 {
		return base
	}
	if base == nil {
		base = make(map[string]MaterializationMode, len(override))
	}
	for target, mode := range override {
		base[target] = mode
	}
	return base
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

// UpdateCheckConfig controls the release upgrade notice.
type UpdateCheckConfig struct {
	Enabled *bool `yaml:"enabled,omitempty"`
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

	// APIVersion identifies the validated v4 input represented by this runtime config.
	APIVersion string `yaml:"apiVersion,omitempty"`

	// Harnesses lists the target harnesses to materialize into (e.g.
	// "factory", "cursor", "claude"). Target-dependent operations require them.
	Harnesses             []string               `yaml:"harnesses,omitempty"`
	Registries            []RegistryEntry        `yaml:"registries,omitempty"`
	Marketplaces          []MarketplaceEntry     `yaml:"marketplaces,omitempty"`
	MarketplaceExclusions []MarketplaceExclusion `yaml:"-"`
	Collections           KindSelector           `yaml:"collections,omitempty"`

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
	AllowedRegistries   *[]string `yaml:"allowed_registries,omitempty"`
	AllowedMarketplaces *[]string `yaml:"allowed_marketplaces,omitempty"`

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

	// APIVersion is the runtime schema sentinel copied from the bound
	// personal-local layer; it only ever carries APIVersionV4. The authored v4
	// parser owns version validation of config.local.yaml and its includes, so
	// this field never admits another consumer schema.
	APIVersion   string             `yaml:"apiVersion,omitempty"`
	Registries   []RegistryEntry    `yaml:"registries,omitempty"`
	Marketplaces []MarketplaceEntry `yaml:"marketplaces,omitempty"`
	Skills       KindSelector       `yaml:"skills,omitempty"`
	Commands     KindSelector       `yaml:"commands,omitempty"`
	Subagents    KindSelector       `yaml:"subagents,omitempty"`
	Hooks        KindSelector       `yaml:"hooks,omitempty"`
	MCP          KindSelector       `yaml:"mcp,omitempty"`
	Collections  KindSelector       `yaml:"collections,omitempty"`
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

	// V4Kind preserves an explicit v4 source discriminator for the read-only
	// legacy adapter. It is never serialized and prevents a directory named
	// ".git" from being reclassified as a Git source at runtime.
	V4Kind V4SourceKind `yaml:"-"`
	// AuthoredDirectory retains the declaration before v4 resolves its runtime
	// URL, so a repo-relative source cannot be promoted to machine scope.
	AuthoredDirectory string `yaml:"-"`
	// V4All preserves v4 all:true as an additive selection policy for the
	// runtime resolver; Default alone cannot distinguish it from legacy policy.
	V4All       bool                  `yaml:"-"`
	V4Decisions []V4SelectionDecision `yaml:"-"`

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
//   - It is a hostless file URL whose decoded path does NOT end with '.git'
//
// The .git suffix is the sole signal that a local path is a git bare repo
// (e.g. /path/to/repo.git or file:///path/to/repo.git). Without the .git
// suffix, a filesystem path is treated as a plain directory and ref is
// optional (and ignored).
func (e RegistryEntry) IsLocal() bool {
	switch e.V4Kind {
	case V4SourceDirectory:
		return true
	case V4SourceGit, V4SourceMarketplace:
		return false
	}
	u := strings.TrimSpace(e.URL)
	if u == "" {
		return false
	}
	if strings.HasPrefix(strings.ToLower(u), "file://") {
		path, err := parseLocalFileURL(u)
		return err != nil || !strings.HasSuffix(path, ".git")
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

// IsRepoRelativeLocal reports a directory declaration that depends on its
// repository scope and must not be promoted to the user-personal config.
// V4 resolves runtime URLs, so the authored spelling retains this boundary.
// Relative directories authored in machine-scoped roots retain their own base.
func (e RegistryEntry) IsRepoRelativeLocal() bool {
	if e.V4Kind == V4SourceDirectory && e.AuthoredDirectory != "" && !e.Source.IsMachineScope() {
		return !filepath.IsAbs(e.AuthoredDirectory)
	}
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
			url := entry.URL
			if entry.AuthoredDirectory != "" {
				url = entry.AuthoredDirectory
			}
			return &GlobalRelativeRegistryError{URL: url}
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

// parseLocalFileURL decodes an absolute, hostless file URL into its filesystem
// path. Invalid file URLs remain local so ResolveLocalPath can report the
// configuration error instead of treating them as git remotes.
func parseLocalFileURL(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host != "" || parsed.Path == "" || !filepath.IsAbs(parsed.Path) {
		return "", fmt.Errorf("invalid local registry file URL %q", rawURL)
	}
	path, err := url.PathUnescape(parsed.Path)
	if err != nil {
		return "", fmt.Errorf("decode local registry file URL %q: %w", rawURL, err)
	}
	return path, nil
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
	fileURL := strings.HasPrefix(strings.ToLower(u), "file://")
	if fileURL {
		var err error
		u, err = parseLocalFileURL(u)
		if err != nil {
			return "", err
		}
	}

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

	if !filepath.IsAbs(e.URL) && !fileURL && !isSubPath(u, baseDir) {
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

// APIVersionV4 is the only supported consumer source/resource schema.
const APIVersionV4 = "agent-marketplace/v4"

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

	if c.APIVersion == "" {
		if requireAPIVersion {
			errs = append(errs, v4err("v4.api-version-required", "apiVersion", 0, "root requires apiVersion: %s", APIVersionV4))
		}
	} else if c.APIVersion != APIVersionV4 {
		errs = append(errs, v4err("v4.api-version", "apiVersion", 0, "must be exactly %q", APIVersionV4))
	}

	// harnesses is required on user-personal configs and must not be empty.
	// Organization/project/local configs may omit it and inherit from the
	// user layer during merge.
	if requireHarnesses && len(c.Harnesses) == 0 {
		errs = append(errs, fmt.Errorf("harnesses is required: add a harnesses list (e.g. harnesses: [factory])"))
	}

	errs = append(errs, validateConfigHarnessTargets(c)...)

	errs = append(errs, validateRegistryEntries(c.Registries)...)
	errs = append(errs, ValidateMarketplaceEntries(c.Marketplaces)...)
	errs = append(errs, validateAllowedMarketplacePatterns(c.AllowedMarketplaces)...)
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
// registry entries) without enforcing root gating (apiVersion, harnesses).
// Authored root versions are validated by the v4 parser before binding.
func (c Config) ValidateBasic() error {
	var errs []error

	errs = append(errs, validateConfigHarnessTargets(c)...)

	errs = append(errs, validateRegistryEntries(c.Registries)...)
	errs = append(errs, ValidateMarketplaceEntries(c.Marketplaces)...)
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
	var errs []error
	if _, err := normalizeMaterializationMode(materialization.Mode); err != nil {
		errs = append(errs, err)
	}
	if _, err := normalizeTargetMaterializationModes(materialization.Targets); err != nil {
		errs = append(errs, err)
	}
	return errs
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
// enforcing root gating (harnesses). At minimum it ensures every registry
// entry has a non-empty URL and no path-traversal in Path. The runtime
// apiVersion sentinel may be empty, but when present it must be exactly
// APIVersionV4.
func (c LocalConfig) ValidateBasic() error {
	var errs []error

	errs = append(errs, validateRegistryHarnessTargets(c.Registries)...)

	if c.APIVersion != "" && c.APIVersion != APIVersionV4 {
		errs = append(errs, v4err("v4.api-version", "apiVersion", 0, "must be exactly %q", APIVersionV4))
	}

	errs = append(errs, validateRegistryEntries(c.Registries)...)
	errs = append(errs, ValidateMarketplaceEntries(c.Marketplaces)...)
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
