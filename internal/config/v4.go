package config

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/giturl"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	telemetryconfig "github.com/Adyen/agent-marketplace/internal/telemetry/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/urlutil"

	"gopkg.in/yaml.v3"
)

// V4SourceKind identifies how a v4 source is acquired.
type V4SourceKind string

const (
	V4SourceGit         V4SourceKind = "git"
	V4SourceDirectory   V4SourceKind = "directory"
	V4SourceMarketplace V4SourceKind = "marketplace"
)

// V4Source is a named, layer-local source declaration. CanonicalURL and Path
// are normalized for later acquisition/resolution; they are never serialized
// back into a configuration file.
type V4Source struct {
	Name              string
	Kind              V4SourceKind
	URL               string
	Git               string
	Directory         string
	AuthoredDirectory string
	Marketplace       string
	Ref               string
	Path              string
	SourceHarness     string
	File              string
	Line              int
	CanonicalURL      string
	// AlsoDeclaredIn lists the other files of the include tree that declare
	// this source identically; the winning declaration alone does not own it.
	AlsoDeclaredIn []string
}

// V4ResourceOptions are options authored on one explicit resource request.
type V4ResourceOptions struct {
	As       string
	Revision string
	HasAs    bool
}

// V4ResourceRequest is the normalized form of an explicit v4 include item.
// SourceName is intentionally retained: source names are layer-local bindings,
// not acquisition identities.
type V4ResourceRequest struct {
	SourceName   string
	Locator      string
	Registry     string
	Kind         string
	Name         string
	Options      V4ResourceOptions
	Required     bool
	Harnesses    []string
	HasHarnesses bool
	Reason       V4SelectionReason
	Provenance   []V4Provenance
}

func (r V4ResourceRequest) MarketplaceKind() string {
	return api.FromIR(v4KindToIR(r.Kind))
}

type V4SelectionReason string

const (
	V4SelectionExplicit V4SelectionReason = "explicit"
	V4SelectionTags     V4SelectionReason = "tags"
	V4SelectionAll      V4SelectionReason = "all"
	V4SelectionExclude  V4SelectionReason = "exclude"
)

// V4Provenance records the source file and ordered precedence of a decision.
type V4Provenance struct {
	File       string
	Line       int
	Precedence int
}

// V4SelectionDecision preserves the ordered include/exclude stream. A future
// resolver can apply this stream without reconstructing policy from sets.
type V4SelectionDecision struct {
	SourceName string
	Locator    string
	Action     V4SelectionReason
	Request    *V4ResourceRequest
	Exclusion  string
	Provenance V4Provenance
}

// V4ResourceGroup is the normalized selector policy for one source binding.
// Pointer fields distinguish an omitted value from an explicit false/empty
// override during include composition.
type V4ResourceGroup struct {
	SourceName    string
	Include       []V4ResourceRequest
	Exclude       []string
	Tags          []string
	All           *bool
	Registries    []string
	Required      *bool
	Harnesses     []string
	HasTags       bool
	HasAll        bool
	HasRegistries bool
	HasRequired   bool
	HasHarnesses  bool
	Decisions     []V4SelectionDecision
}

// V4Config is a strict, normalized v4 document tree. Sources and groups are
// self-contained within the loaded root and its explicit includes; no other
// configuration layer is consulted.
type V4Config struct {
	APIVersion string
	Sources    map[string]V4Source
	Groups     map[string]V4ResourceGroup
	Requests   []V4ResourceRequest
	Decisions  []V4SelectionDecision
	Files      []string

	reads []ConfigRead

	// decisionStreamOffset tracks the next precedence position in the ordered
	// decision stream. It is independent from Files because one file can author
	// multiple decisions and included trees can repeat a file per branch.
	decisionStreamOffset int

	// Auxiliary policy is retained so the strict v4 parser never accepts and
	// silently discards configuration. Pointer fields preserve omitted values.
	Harnesses           []string
	HasHarnesses        bool
	AllowedRegistries   *[]string
	AllowedMarketplaces *[]string
	Backup              *BackupConfig
	Logging             *LoggingConfig
	UpdateCheck         *UpdateCheckConfig
	Telemetry           *telemetryconfig.TelemetryConfig
	Materialization     *MaterializationConfig
	GUI                 *GUIConfig

	telemetryFields v4TelemetryFields
}

type v4TelemetryFields struct {
	level, endpoint, communityReporting, retentionPolicy, caFile, allowInsecure, headers bool
}

// V4ValidationError carries a stable diagnostic code while retaining useful
// path and line context in Error().
type V4ValidationError struct {
	Code string
	Path string
	Line int
	Msg  string
}

func (e *V4ValidationError) Error() string {
	if e == nil {
		return ""
	}
	where := e.Path
	if e.Line > 0 {
		where = fmt.Sprintf("%s (line %d)", where, e.Line)
	}
	if where == "" {
		return e.Code + ": " + e.Msg
	}
	return fmt.Sprintf("%s: %s: %s", e.Code, where, e.Msg)
}

func v4err(code, p string, line int, format string, args ...any) error {
	return &V4ValidationError{Code: code, Path: p, Line: line, Msg: fmt.Sprintf(format, args...)}
}

const (
	v4MarketplaceLockRequiredCode = "v4.marketplace-lock-required"
	v4MarketplaceLockMismatchCode = "v4.marketplace-lock-mismatch"
)

func MissingMarketplaceEvidence(err error) bool {
	var validation *V4ValidationError
	return errors.As(err, &validation) &&
		(validation.Code == v4MarketplaceLockRequiredCode || validation.Code == v4MarketplaceLockMismatchCode)
}

var (
	v4SourceNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	v4NameRE       = regexp.MustCompile(`^[^/\\\x00-\x1f\x7f]+$`)
	v4SlugRE       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	v4GitScpRE     = regexp.MustCompile(`^(?:[^@\s/:]+@)?[^/\s:]+:[^\s]+$`)
	v4RevisionRE   = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

var v4Kinds = map[string]struct{}{
	"skill": {}, "command": {}, "subagent": {}, "hook": {}, "mcp": {}, "collection": {},
}

var v4LegacyKeys = map[string]struct{}{
	"registries": {}, "marketplaces": {}, "skills": {}, "commands": {}, "subagents": {},
	"hooks": {}, "mcp": {}, "collections": {}, "tags": {},
}

var v4RootKeys = map[string]struct{}{
	"apiVersion": {}, "include": {}, "sources": {}, "resources": {},
	"harnesses": {}, "allowed_registries": {}, "allowed_marketplaces": {},
	"backup": {}, "logging": {}, "update_check": {}, "telemetry": {}, "materialization": {}, "gui": {},
}

var v4SourceKeys = map[string]struct{}{
	"git": {}, "directory": {}, "marketplace": {}, "ref": {}, "path": {}, "source_harness": {},
}

var v4GroupKeys = map[string]struct{}{
	"include": {}, "exclude": {}, "tags": {}, "all": {}, "registries": {}, "required": {}, "harnesses": {},
}

// ParseV4Config parses one root or fragment. A root must carry v4 explicitly;
// fragments may omit apiVersion and inherit it from their containing tree.
func ParseV4Config(data []byte, declaringPath string, root bool) (*V4Config, error) {
	doc, err := parseV4Document(data, declaringPath, root)
	if err != nil {
		return nil, err
	}
	out := &V4Config{APIVersion: APIVersionV4, Sources: doc.sources, Groups: doc.groups, Files: []string{declaringPath}, Harnesses: cloneV4StringList(doc.harnesses), HasHarnesses: doc.hasHarnesses, AllowedRegistries: doc.allowedRegistries, AllowedMarketplaces: doc.allowedMarketplaces, Backup: doc.backup, Logging: doc.logging, UpdateCheck: doc.updateCheck, Telemetry: doc.telemetry, Materialization: doc.materialization, GUI: doc.gui, telemetryFields: doc.telemetryFields}
	if err := normalizeV4Tree(out); err != nil {
		return nil, err
	}
	return out, nil
}

// LoadV4ConfigFromPath loads a v4 root and all explicit local includes.
func LoadV4ConfigFromPath(file string) (*V4Config, error) {
	return loadV4Tree(file, true, map[string]bool{})
}

func LoadV4ConfigIfPresent(file string, source Source) (*V4Config, error) {
	data, err := readConfigFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	kind, organizationPath, err := v4LayerForSource(source, file)
	if err != nil {
		return nil, err
	}
	if source == Organization {
		if err := ValidateOrganizationConfigAuthority(file); err != nil {
			return nil, err
		}
	}
	cfg, err := loadV4TreeWithOverlays(file, true, map[string]bool{}, nil, data, organizationPath, kind)
	if err != nil {
		return nil, err
	}
	if err := cfg.validateLayerFields(kind); err != nil {
		return nil, err
	}
	return cfg, nil
}

func v4LayerForSource(source Source, path string) (layerKind, string, error) {
	switch source {
	case Organization:
		return organizationLayer, path, nil
	case UserPersonal:
		return userLayer, "", nil
	case RepoCommitted:
		return repoLayer, "", nil
	case RepoPersonal:
		return localLayer, "", nil
	default:
		return 0, "", fmt.Errorf("unsupported v4 configuration source %q", source)
	}
}

// ParseV4 is a short alias useful to in-package consumers and tests.
func ParseV4(data []byte) (*V4Config, error) { return ParseV4Config(data, "", true) }

type v4Document struct {
	path                string
	sources             map[string]V4Source
	groups              map[string]V4ResourceGroup
	harnesses           []string
	hasHarnesses        bool
	allowedRegistries   *[]string
	allowedMarketplaces *[]string
	backup              *BackupConfig
	logging             *LoggingConfig
	updateCheck         *UpdateCheckConfig
	telemetry           *telemetryconfig.TelemetryConfig
	materialization     *MaterializationConfig
	gui                 *GUIConfig
	telemetryFields     v4TelemetryFields
}

func parseV4Document(data []byte, file string, root bool, kinds ...layerKind) (*v4Document, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var node yaml.Node
	if err := dec.Decode(&node); err != nil {
		if err == io.EOF && root {
			return nil, v4err("v4.api-version-required", file, 0, "root requires apiVersion: %s", APIVersionV4)
		}
		return nil, v4err("v4.parse", file, 0, "%v", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, v4err("v4.multiple-documents", file, extra.Line, "multiple YAML documents are not allowed")
		}
		return nil, v4err("v4.parse", file, 0, "%v", err)
	}
	if err := validateV4YAMLTree(&node, file); err != nil {
		return nil, err
	}
	if node.Kind != yaml.DocumentNode || len(node.Content) == 0 {
		return nil, v4err("v4.root-type", file, 0, "expected one YAML mapping")
	}
	raw := node.Content[0]
	if root && raw.Kind == yaml.ScalarNode && raw.Tag == "!!null" {
		return nil, v4err("v4.api-version-required", file, raw.Line, "root requires apiVersion: %s", APIVersionV4)
	}
	if raw.Kind != yaml.MappingNode {
		return nil, v4err("v4.root-type", file, raw.Line, "expected YAML mapping at document root")
	}
	api := mappingValue(raw, "apiVersion")
	if api != nil {
		if api.Kind != yaml.ScalarNode || api.Tag != "!!str" || api.Value != APIVersionV4 {
			return nil, v4err("v4.api-version", file, api.Line, "apiVersion must be exactly %q", APIVersionV4)
		}
	} else if root {
		return nil, v4err("v4.api-version-required", file, raw.Line, "root requires apiVersion: %s", APIVersionV4)
	}
	for i := 0; i+1 < len(raw.Content); i += 2 {
		key := raw.Content[i]
		if _, ok := v4RootKeys[key.Value]; !ok {
			if _, legacy := v4LegacyKeys[key.Value]; legacy {
				return nil, v4DocumentError(file, raw, v4err("v4.legacy-key", key.Value, key.Line, "legacy installation key %q is not valid in v4; migrate selections to sources/resources", key.Value), !root)
			}
			return nil, v4DocumentError(file, raw, v4err("v4.unknown-field", key.Value, key.Line, "unknown top-level field"), !root)
		}
	}
	if err := validateV4AuxiliaryFields(raw); err != nil {
		return nil, v4DocumentError(file, raw, err, !root)
	}
	doc := &v4Document{path: file, sources: map[string]V4Source{}, groups: map[string]V4ResourceGroup{}}
	if err := decodeV4AuxiliaryFields(raw, doc); err != nil {
		return nil, v4DocumentError(file, raw, err, !root)
	}
	if len(kinds) > 0 {
		policy := &V4Config{HasHarnesses: doc.hasHarnesses, AllowedRegistries: doc.allowedRegistries, AllowedMarketplaces: doc.allowedMarketplaces, Backup: doc.backup, Logging: doc.logging, Telemetry: doc.telemetry, Materialization: doc.materialization}
		if err := policy.validateLayerFields(kinds[0]); err != nil {
			return nil, v4DocumentError(file, raw, err, !root)
		}
	}
	if inc := mappingValue(raw, "include"); inc != nil {
		if err := validateV4Includes(inc, file); err != nil {
			return nil, v4DocumentError(file, raw, err, !root)
		}
	}
	if sources := mappingValue(raw, "sources"); sources != nil {
		parsed, err := parseV4Sources(sources, file, v4DirectoryBase(file, root, kinds...))
		if err != nil {
			return nil, v4DocumentError(file, raw, err, !root)
		}
		doc.sources = parsed
	}
	if resources := mappingValue(raw, "resources"); resources != nil {
		groups, err := parseV4Groups(resources, file)
		if err != nil {
			return nil, v4DocumentError(file, raw, err, !root)
		}
		doc.groups = groups
	}
	return doc, nil
}

func v4DocumentError(file string, root *yaml.Node, err error, fragment bool) error {
	var validation *V4ValidationError
	if fragment && errors.As(err, &validation) && validation.Line == 0 {
		field := strings.FieldsFunc(validation.Path, func(r rune) bool { return r == '.' || r == '[' })
		var node *yaml.Node
		if len(field) > 0 {
			node = mappingValue(root, field[0])
		}
		if validation.Path == "auxiliary" {
			for _, key := range []string{"backup", "logging", "telemetry"} {
				if value := mappingValue(root, key); value != nil && value.Tag != "!!null" {
					node = value
					break
				}
			}
		} else if node == nil && validation.Path == "allowed_registries" {
			node = mappingValue(root, "allowed_marketplaces")
		}
		if node != nil {
			copy := *validation
			copy.Line = node.Line
			err = &copy
		}
	}
	return fmt.Errorf("validate config %q: %w", file, err)
}

func validateV4YAMLTree(node *yaml.Node, file string) error {
	var walk func(*yaml.Node) error
	walk = func(n *yaml.Node) error {
		if n == nil {
			return nil
		}
		if n.Kind == yaml.AliasNode {
			return v4err("v4.alias-forbidden", file, n.Line, "YAML aliases are not allowed; use explicit values")
		}
		if n.Kind == yaml.MappingNode {
			seen := map[string]bool{}
			for i := 0; i+1 < len(n.Content); i += 2 {
				key := n.Content[i]
				if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
					return v4err("v4.key-type", file, key.Line, "mapping keys must be strings")
				}
				if key.Value == "<<" {
					return v4err("v4.merge-key-forbidden", file, key.Line, "YAML merge keys are not allowed")
				}
				if seen[key.Value] {
					return v4err("v4.duplicate-key", key.Value, key.Line, "duplicate mapping key")
				}
				seen[key.Value] = true
				if err := walk(n.Content[i+1]); err != nil {
					return err
				}
			}
		} else {
			for _, child := range n.Content {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(node)
}

func mappingValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func validateV4AuxiliaryFields(root *yaml.Node) error {
	for _, key := range []string{"harnesses", "allowed_registries", "allowed_marketplaces"} {
		n := mappingValue(root, key)
		if n == nil {
			continue
		}
		if n.Kind != yaml.SequenceNode {
			return v4err("v4.type", key, n.Line, "must be a string list")
		}
		for i, item := range n.Content {
			if item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
				return v4err("v4.type", fmt.Sprintf("%s[%d]", key, i), item.Line, "must contain strings")
			}
		}
	}
	for _, key := range []string{"backup", "logging", "update_check", "telemetry", "materialization", "gui"} {
		n := mappingValue(root, key)
		if n == nil || (n.Kind == yaml.ScalarNode && n.Tag == "!!null") {
			continue
		}
		if n.Kind != yaml.MappingNode {
			return v4err("v4.type", key, n.Line, "must be a mapping")
		}
	}
	auxiliaryKeys := map[string]map[string]bool{
		"backup":          {"enabled": true, "max_days": true, "max_per_day": true},
		"logging":         {"enabled": true, "level": true, "file": true, "max_size_mb": true, "max_backups": true, "max_age_days": true, "compress": true},
		"update_check":    {"enabled": true},
		"telemetry":       {"level": true, "endpoint": true, "community_reporting": true, "retention_policy": true, "ca_file": true, "allow_insecure": true, "headers": true},
		"materialization": {"mode": true, "targets": true},
		"gui":             {"version_check": true},
	}
	for key, allowed := range auxiliaryKeys {
		n := mappingValue(root, key)
		if n == nil || n.Kind != yaml.MappingNode {
			continue
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			if !allowed[n.Content[i].Value] {
				return v4err("v4.unknown-field", key+"."+n.Content[i].Value, n.Content[i].Line, "unknown field")
			}
		}
	}
	if n := mappingValue(root, "materialization"); n != nil && n.Kind == yaml.MappingNode {
		mode := mappingValue(n, "mode")
		if mode != nil && (mode.Kind != yaml.ScalarNode || mode.Tag != "!!str" || (mode.Value != "symlink" && mode.Value != "file")) {
			return v4err("v4.type", "materialization.mode", mode.Line, "must be symlink or file")
		}
	}
	if n := mappingValue(root, "gui"); n != nil && n.Kind == yaml.MappingNode {
		versionCheck := mappingValue(n, "version_check")
		if versionCheck != nil && (versionCheck.Kind != yaml.ScalarNode || (versionCheck.Tag != "!!bool" && versionCheck.Tag != "!!null")) {
			return v4err("v4.type", "gui.version_check", versionCheck.Line, "must be a boolean")
		}
	}
	return nil
}

func decodeV4AuxiliaryFields(root *yaml.Node, doc *v4Document) error {
	decode := func(key string, dst any) error {
		n := mappingValue(root, key)
		if n == nil || (n.Kind == yaml.ScalarNode && n.Tag == "!!null") {
			return nil
		}
		data, err := yaml.Marshal(n)
		if err != nil {
			return v4err("v4.auxiliary", key, n.Line, "cannot decode: %v", err)
		}
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		if err := dec.Decode(dst); err != nil {
			return v4err("v4.auxiliary", key, n.Line, "%v", err)
		}
		return nil
	}
	if n := mappingValue(root, "harnesses"); n != nil {
		doc.hasHarnesses = true
		if err := decode("harnesses", &doc.harnesses); err != nil {
			return err
		}
		for i, harness := range doc.harnesses {
			if strings.TrimSpace(harness) == "" {
				return v4err("v4.harness", fmt.Sprintf("harnesses[%d]", i), n.Line, "harness target must not be empty")
			}
			if strings.TrimSpace(harness) != harness {
				return v4err("v4.harness", fmt.Sprintf("harnesses[%d]", i), n.Line, "harness target %q must not have surrounding whitespace", harness)
			}
		}
	}
	for _, auxiliary := range []struct {
		key string
		dst any
	}{
		{key: "allowed_registries", dst: &doc.allowedRegistries},
		{key: "allowed_marketplaces", dst: &doc.allowedMarketplaces},
	} {
		key, dst := auxiliary.key, auxiliary.dst
		if err := decode(key, dst); err != nil {
			return err
		}
		var patterns *[]string
		if key == "allowed_registries" {
			patterns = doc.allowedRegistries
		} else {
			patterns = doc.allowedMarketplaces
		}
		if patterns != nil {
			for i, pattern := range *patterns {
				if _, err := regexp.Compile(pattern); err != nil {
					return v4err("v4.auxiliary", fmt.Sprintf("%s[%d]", key, i), 0, "invalid regex: %v", err)
				}
			}
		}
	}
	if err := decode("backup", &doc.backup); err != nil {
		return err
	}
	if err := decode("logging", &doc.logging); err != nil {
		return err
	}
	if err := decode("update_check", &doc.updateCheck); err != nil {
		return err
	}
	if err := decode("telemetry", &doc.telemetry); err != nil {
		return err
	}
	if n := mappingValue(root, "telemetry"); n != nil && n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			switch n.Content[i].Value {
			case "level":
				doc.telemetryFields.level = true
			case "endpoint":
				doc.telemetryFields.endpoint = true
			case "community_reporting":
				doc.telemetryFields.communityReporting = true
			case "retention_policy":
				doc.telemetryFields.retentionPolicy = true
			case "ca_file":
				doc.telemetryFields.caFile = true
			case "allow_insecure":
				doc.telemetryFields.allowInsecure = true
			case "headers":
				doc.telemetryFields.headers = true
			}
		}
	}
	if err := decode("materialization", &doc.materialization); err != nil {
		return err
	}
	if err := decode("gui", &doc.gui); err != nil {
		return err
	}
	for _, err := range validateBackup(doc.backup) {
		return v4err("v4.auxiliary", "backup", 0, "%v", err)
	}
	for _, err := range validateLogging(doc.logging) {
		return v4err("v4.auxiliary", "logging", 0, "%v", err)
	}
	for _, err := range validateMaterialization(doc.materialization) {
		return v4err("v4.auxiliary", "materialization", 0, "%v", err)
	}
	if doc.telemetry != nil {
		for _, err := range telemetryconfig.ValidateParserSafe(doc.telemetry) {
			return v4err("v4.auxiliary", "telemetry", 0, "%v", err)
		}
	}
	return nil
}

func validateV4Includes(n *yaml.Node, file string) error {
	if n.Kind == yaml.ScalarNode && n.Tag == "!!str" {
		if strings.TrimSpace(n.Value) == "" {
			return v4err("v4.include", "include", n.Line, "include path must not be empty")
		}
		return nil
	}
	if n.Kind != yaml.SequenceNode {
		return v4err("v4.type", "include", n.Line, "must be a string or string list")
	}
	for i, child := range n.Content {
		if child.Kind != yaml.ScalarNode || child.Tag != "!!str" || strings.TrimSpace(child.Value) == "" {
			return v4err("v4.type", fmt.Sprintf("include[%d]", i), child.Line, "must contain non-empty strings")
		}
	}
	return nil
}

func parseV4Sources(n *yaml.Node, file, directoryBase string) (map[string]V4Source, error) {
	if n.Kind != yaml.MappingNode {
		return nil, v4err("v4.type", "sources", n.Line, "must be a mapping")
	}
	out := make(map[string]V4Source, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		nameNode, value := n.Content[i], n.Content[i+1]
		name := nameNode.Value
		if !v4SourceNameRE.MatchString(name) {
			return nil, v4err("v4.source-name", "sources."+name, nameNode.Line, "must match [a-zA-Z0-9][a-zA-Z0-9._-]*")
		}
		if value.Kind != yaml.MappingNode {
			return nil, v4err("v4.source-type", "sources."+name, value.Line, "must be a mapping")
		}
		src, err := parseV4Source(name, value, file, directoryBase)
		if err != nil {
			return nil, err
		}
		out[name] = src
	}
	return out, nil
}

func parseV4Source(name string, n *yaml.Node, file, directoryBase string) (V4Source, error) {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if _, ok := v4SourceKeys[n.Content[i].Value]; !ok {
			return V4Source{}, v4err("v4.unknown-field", "sources."+name+"."+n.Content[i].Value, n.Content[i].Line, "unknown source field")
		}
	}
	var discriminators []string
	for _, k := range []string{"git", "directory", "marketplace"} {
		if mappingValue(n, k) != nil {
			discriminators = append(discriminators, k)
		}
	}
	if len(discriminators) != 1 {
		return V4Source{}, v4err("v4.source-discriminator", "sources."+name, n.Line, "must declare exactly one of git, directory, marketplace")
	}
	s := V4Source{Name: name, Kind: V4SourceKind(discriminators[0]), File: file, Line: n.Line}
	if s.Kind == V4SourceMarketplace && name == "all" {
		return V4Source{}, v4err("v4.source-name", "sources."+name, n.Line, "Marketplace source name %q is reserved for federated search", name)
	}
	for _, field := range []string{"git", "directory", "marketplace", "ref", "path", "source_harness"} {
		node := mappingValue(n, field)
		if node == nil {
			continue
		}
		if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
			return V4Source{}, v4err("v4.type", "sources."+name+"."+field, node.Line, "must be a string")
		}
		value := node.Value
		switch field {
		case "git", "ref":
			// Git safety validators must see the authored value so callers cannot
			// acquire one string while caching or identifying another.
			if field == "git" {
				s.URL = value
			} else {
				s.Ref = value
			}
		case "directory", "marketplace", "path", "source_harness":
			value = strings.TrimSpace(value)
			switch field {
			case "directory", "marketplace":
				s.URL = value
			case "path":
				s.Path = value
			case "source_harness":
				s.SourceHarness = value
			}
		}
	}
	if s.URL == "" {
		return V4Source{}, v4err("v4.source-value", "sources."+name, n.Line, "%s must not be empty", discriminators[0])
	}
	switch s.Kind {
	case V4SourceGit:
		if s.Ref == "" {
			return V4Source{}, v4err("v4.git-ref-required", "sources."+name+".ref", n.Line, "git sources require an explicit ref")
		}
		if err := giturl.ValidateRef(s.Ref); err != nil {
			return V4Source{}, v4err("v4.git-ref", "sources."+name+".ref", n.Line, "%v", err)
		}
		if mappingValue(n, "directory") != nil || mappingValue(n, "marketplace") != nil {
			return V4Source{}, v4err("v4.source-field", "sources."+name, n.Line, "git declaration contains another source discriminator")
		}
		if err := validateV4GitOrigin(s.URL); err != nil {
			return V4Source{}, v4err("v4.git-origin", "sources."+name+".git", n.Line, "%v", err)
		}
		if err := validateV4Subpath(s.Path); err != nil {
			return V4Source{}, v4err("v4.git-path", "sources."+name+".path", n.Line, "%v", err)
		}
		s.CanonicalURL = NormalizeGitURL(s.URL)
		s.Git = s.URL
		s.Path = NormalizeRegistryPath(s.Path)
	case V4SourceDirectory:
		if mappingValue(n, "ref") != nil {
			return V4Source{}, v4err("v4.source-field", "sources."+name+".ref", n.Line, "ref is only valid for git sources")
		}
		if mappingValue(n, "path") != nil {
			return V4Source{}, v4err("v4.source-field", "sources."+name+".path", n.Line, "path is only valid for git sources")
		}
		s.AuthoredDirectory = s.URL
		resolved, err := resolveV4DirectorySource(directoryBase, s.URL)
		if err != nil {
			return V4Source{}, v4err("v4.directory-path", "sources."+name+".directory", n.Line, "%v", err)
		}
		s.CanonicalURL = resolved
		s.URL = resolved
		s.Directory = resolved
	case V4SourceMarketplace:
		if mappingValue(n, "ref") != nil || mappingValue(n, "path") != nil || mappingValue(n, "source_harness") != nil {
			return V4Source{}, v4err("v4.source-field", "sources."+name, n.Line, "marketplace sources accept only marketplace")
		}
		canonical, err := validateV4MarketplaceURL(s.URL)
		if err != nil {
			return V4Source{}, v4err("v4.marketplace-url", "sources."+name+".marketplace", n.Line, "%v", err)
		}
		s.CanonicalURL = canonical
		s.URL = canonical
		s.Marketplace = canonical
	}
	return s, nil
}

func v4DirectoryBase(file string, root bool, kinds ...layerKind) string {
	base := filepath.Dir(file)
	if !root || filepath.Base(base) != ".agents" || (filepath.Base(file) != "config.yaml" && filepath.Base(file) != "config.local.yaml") {
		return base
	}
	if len(kinds) > 0 {
		if kinds[0] != repoLayer && kinds[0] != localLayer {
			return base
		}
	} else if isGlobalConfigPath(file) {
		return base
	}
	return filepath.Dir(base)
}

func resolveV4DirectorySource(directoryBase, raw string) (string, error) {
	base, err := filepath.Abs(directoryBase)
	if err != nil {
		return "", fmt.Errorf("resolve declaring directory: %w", err)
	}
	resolved := raw
	relative := !filepath.IsAbs(resolved)
	if relative {
		resolved = filepath.Join(base, resolved)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	resolved = filepath.Clean(resolved)
	if relative && !v4PathWithin(base, resolved) {
		return "", errors.New("relative directory source must remain within its source base directory")
	}
	if !relative {
		return resolved, nil
	}

	// Check the deepest existing ancestor. This catches both a source that is
	// itself a symlink and a symlinked parent without requiring acquisition or
	// requiring the configured directory to exist yet.
	existing := resolved
	var missing []string
	for {
		_, statErr := os.Lstat(existing)
		if statErr == nil {
			break
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			return "", fmt.Errorf("inspect directory source: %w", statErr)
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		missing = append(missing, filepath.Base(existing))
		existing = parent
	}
	realExisting, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", fmt.Errorf("resolve directory symlinks: %w", err)
	}
	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return resolved, nil
		}
		return "", fmt.Errorf("resolve declaring directory symlinks: %w", err)
	}
	candidate := realExisting
	for i := len(missing) - 1; i >= 0; i-- {
		candidate = filepath.Join(candidate, missing[i])
	}
	if !v4PathWithin(realBase, candidate) {
		return "", errors.New("directory source symlink escapes its source base directory")
	}
	return resolved, nil
}

func v4PathWithin(root, candidate string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func validateV4GitOrigin(value string) error {
	if value == "" || strings.ContainsAny(value, "\\\r\n\x00") || strings.TrimSpace(value) != value {
		return errors.New("must be a canonical git origin")
	}
	if err := giturl.ValidateURL(value); err != nil {
		return err
	}
	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") {
		return errors.New("must be a remote git origin or hostless file:// origin; use directory for live files")
	}
	if strings.Contains(value, "://") {
		u, err := url.Parse(value)
		if err != nil || u.Scheme == "" {
			return errors.New("must be a valid remote URL")
		}
		if strings.EqualFold(u.Scheme, "file") {
			if u.Host != "" || u.Path == "" || !filepath.IsAbs(u.Path) {
				return errors.New("file git origins must be hostless absolute file:// URLs")
			}
		} else if u.Host == "" {
			return errors.New("must be a valid remote URL")
		}
	} else if !v4GitScpRE.MatchString(value) {
		return errors.New("must be a valid remote URL or scp-style git origin")
	} else {
		colon := strings.IndexByte(value, ':')
		if colon < 1 || strings.HasPrefix(value[colon+1:], "-") {
			return errors.New("scp-style path must not start with '-'")
		}
	}
	return nil
}

func validateV4MarketplaceURL(value string) (string, error) {
	return urlutil.NormalizeEndpointURL(value)
}

func validateV4Subpath(value string) error {
	if value == "" {
		return nil
	}
	if strings.ContainsRune(value, '\x00') || filepath.IsAbs(value) {
		return errors.New("must be a relative slash-separated path")
	}
	p := strings.ReplaceAll(value, "\\", "/")
	clean := path.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, "../") || clean == "." && p != "." {
		return errors.New("must not escape the source root")
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return errors.New("must contain only normal path segments")
		}
	}
	return nil
}

func parseV4Groups(n *yaml.Node, file string) (map[string]V4ResourceGroup, error) {
	if n.Kind != yaml.MappingNode {
		return nil, v4err("v4.type", "resources", n.Line, "must be a mapping")
	}
	out := map[string]V4ResourceGroup{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		name, value := n.Content[i].Value, n.Content[i+1]
		if !v4SourceNameRE.MatchString(name) {
			return nil, v4err("v4.source-name", "resources."+name, n.Content[i].Line, "must reference a valid source name")
		}
		group, err := parseV4Group(name, value, file)
		if err != nil {
			return nil, err
		}
		out[name] = group
	}
	return out, nil
}

func parseV4Group(source string, n *yaml.Node, file string) (V4ResourceGroup, error) {
	g := V4ResourceGroup{SourceName: source}
	if n.Kind == yaml.SequenceNode {
		reqs, err := parseV4Includes(source, n, file)
		g.Include = reqs
		g.Decisions = decisionsForIncludes(source, reqs, file)
		assignV4DecisionPrecedence(&g)
		return g, err
	}
	if n.Kind != yaml.MappingNode {
		return g, v4err("v4.type", "resources."+source, n.Line, "must be an include list or mapping")
	}
	var excludeNode *yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, value := n.Content[i].Value, n.Content[i+1]
		if _, ok := v4GroupKeys[key]; !ok {
			return g, v4err("v4.unknown-field", "resources."+source+"."+key, n.Content[i].Line, "unknown resource group field")
		}
		switch key {
		case "include":
			if value.Kind != yaml.SequenceNode {
				return g, v4err("v4.type", "resources."+source+".include", value.Line, "must be a list")
			}
			reqs, err := parseV4Includes(source, value, file)
			if err != nil {
				return g, err
			}
			g.Include = reqs
		case "exclude":
			excludeNode = value
			values, err := parseV4StringList(value, "resources."+source+".exclude")
			if err != nil {
				return g, err
			}
			for _, x := range values {
				if err := validateV4Exclusion(x, source); err != nil {
					return g, err
				}
			}
			g.Exclude = values
		case "tags":
			values, err := parseV4StringList(value, "resources."+source+".tags")
			if err != nil {
				return g, err
			}
			g.Tags, g.HasTags = values, true
		case "registries":
			values, err := parseV4StringList(value, "resources."+source+".registries")
			if err != nil {
				return g, err
			}
			for _, x := range values {
				if !v4SlugRE.MatchString(x) {
					return g, v4err("v4.registry-slug", "resources."+source+".registries", value.Line, "invalid registry slug %q", x)
				}
			}
			g.Registries, g.HasRegistries = values, true
		case "all", "required":
			if value.Kind != yaml.ScalarNode || value.Tag != "!!bool" {
				return g, v4err("v4.type", "resources."+source+"."+key, value.Line, "must be a boolean")
			}
			var b bool
			if err := value.Decode(&b); err != nil {
				return g, v4err("v4.type", "resources."+source+"."+key, value.Line, "must be a boolean: %v", err)
			}
			if key == "all" {
				g.All, g.HasAll = &b, true
			} else {
				g.Required, g.HasRequired = &b, true
			}
		case "harnesses":
			values, err := parseV4StringList(value, "resources."+source+".harnesses")
			if err != nil {
				return g, err
			}
			g.Harnesses, g.HasHarnesses = values, true
		}
	}
	// Exclusions are applied after inclusions within one file regardless of
	// mapping key order. Include-tree precedence is represented by the order in
	// which groups are merged, not by YAML map order.
	g.Decisions = append(decisionsForIncludes(source, g.Include, file), decisionsForExclusions(source, g.Exclude, excludeNode, file)...)
	assignV4DecisionPrecedence(&g)
	if err := validateV4Dynamic(g, source); err != nil {
		return g, err
	}
	return g, nil
}

func parseV4StringList(n *yaml.Node, p string) ([]string, error) {
	if n.Kind != yaml.SequenceNode {
		return nil, v4err("v4.type", p, n.Line, "must be a string list")
	}
	out := make([]string, 0, len(n.Content))
	seen := map[string]bool{}
	for i, x := range n.Content {
		if x.Kind != yaml.ScalarNode || x.Tag != "!!str" || strings.TrimSpace(x.Value) == "" {
			return nil, v4err("v4.type", fmt.Sprintf("%s[%d]", p, i), x.Line, "must contain non-empty strings")
		}
		if seen[x.Value] {
			return nil, v4err("v4.duplicate", p, x.Line, "duplicate value %q", x.Value)
		}
		seen[x.Value] = true
		out = append(out, x.Value)
	}
	return out, nil
}

func parseV4Includes(source string, n *yaml.Node, file string) ([]V4ResourceRequest, error) {
	out := make([]V4ResourceRequest, 0, len(n.Content))
	seen := map[string]bool{}
	for i, item := range n.Content {
		p := fmt.Sprintf("resources.%s.include[%d]", source, i)
		var locator string
		opts := V4ResourceOptions{}
		if item.Kind == yaml.ScalarNode && item.Tag == "!!str" {
			locator = item.Value
		} else if item.Kind == yaml.MappingNode {
			if len(item.Content) != 2 {
				return nil, v4err("v4.resource-item", p, item.Line, "must contain exactly one locator and options mapping")
			}
			locator = item.Content[0].Value
			optionNode := item.Content[1]
			if optionNode.Kind != yaml.MappingNode {
				return nil, v4err("v4.resource-options", p, optionNode.Line, "options must be a mapping")
			}
			for j := 0; j+1 < len(optionNode.Content); j += 2 {
				key, val := optionNode.Content[j].Value, optionNode.Content[j+1]
				if key != "as" && key != "revision" {
					return nil, v4err("v4.unknown-field", p+"."+key, optionNode.Content[j].Line, "unknown resource option")
				}
				if val.Kind != yaml.ScalarNode || val.Tag != "!!str" || strings.TrimSpace(val.Value) == "" {
					return nil, v4err("v4.resource-options", p+"."+key, val.Line, "must be a non-empty string")
				}
				if key == "as" {
					opts.As = val.Value
					opts.HasAs = true
				} else {
					opts.Revision = val.Value
				}
			}
		} else {
			return nil, v4err("v4.resource-item", p, item.Line, "must be a locator string or single-key mapping")
		}
		kind, registry, name, err := parseV4Locator(locator, source)
		if err != nil {
			return nil, v4err("v4.locator", p, item.Line, "%v", err)
		}
		if seen[locator] {
			return nil, v4err("v4.duplicate-request", p, item.Line, "duplicate locator %q in one include list", locator)
		}
		seen[locator] = true
		if opts.As == "" {
			opts.As = name
		}
		if err := nameutil.ValidateSafeFilename(opts.As); err != nil {
			return nil, v4err("v4.installed-name", p, item.Line, "as must be a safe resource name: %v", err)
		}
		if kind == "collection" && opts.HasAs {
			return nil, v4err("v4.option-combination", p, item.Line, "as is invalid for collections")
		}
		out = append(out, V4ResourceRequest{SourceName: source, Locator: locator, Registry: registry, Kind: kind, Name: name, Options: opts, Reason: V4SelectionExplicit, Provenance: []V4Provenance{{File: file, Line: item.Line}}})
	}
	return out, nil
}

func parseV4Locator(value, source string) (kind, registry, name string, err error) {
	if strings.TrimSpace(value) != value || strings.ContainsAny(value, "\\\x00\r\n") {
		return "", "", "", errors.New("must be a clean locator")
	}
	parts := strings.Split(value, "/")
	if len(parts) == 2 {
		kind, name = parts[0], parts[1]
	} else if len(parts) == 3 {
		registry, kind, name = parts[0], parts[1], parts[2]
	} else {
		return "", "", "", errors.New("must be kind/name or registry/kind/name")
	}
	if _, ok := v4Kinds[kind]; !ok || !v4NameRE.MatchString(name) || name == "." || name == ".." || strings.ContainsAny(name, "*?") {
		return "", "", "", errors.New("has invalid kind or resource name")
	}
	if err := validateMarketplaceSegment("name", name); err != nil {
		return "", "", "", err
	}
	if len(parts) == 3 && !v4SlugRE.MatchString(registry) {
		return "", "", "", errors.New("has invalid registry slug")
	}
	if source == "" {
		return "", "", "", errors.New("missing source")
	}
	// The source binding determines whether a registry slug is required; the
	// parser accepts both shapes here so it can validate syntax before binding.
	return kind, registry, name, nil
}

func validateV4Exclusion(value, source string) error {
	if value == "*" {
		return nil
	}
	if strings.ContainsAny(value, "?[") || strings.Contains(value, "**") {
		return v4err("v4.exclusion", "resources."+source+".exclude", 0, "wildcard form is not bounded")
	}
	if strings.HasSuffix(value, "/*") {
		base := strings.TrimSuffix(value, "/*")
		if base == "" {
			return nil
		}
		parts := strings.Split(base, "/")
		if len(parts) == 1 {
			if _, ok := v4Kinds[parts[0]]; !ok && !v4SlugRE.MatchString(parts[0]) {
				return v4err("v4.exclusion", value, 0, "invalid kind or registry wildcard")
			}
			return nil
		}
		if len(parts) == 2 && v4SlugRE.MatchString(parts[0]) {
			if _, ok := v4Kinds[parts[1]]; !ok {
				return v4err("v4.exclusion", value, 0, "invalid kind wildcard")
			}
			return nil
		}
		return v4err("v4.exclusion", value, 0, "invalid bounded wildcard")
	}
	if _, _, _, err := parseV4Locator(value, source); err != nil {
		return v4err("v4.exclusion", value, 0, "%v", err)
	}
	return nil
}

func validateV4ExclusionForSource(value string, kind V4SourceKind) error {
	if value == "*" {
		return nil
	}
	if strings.HasSuffix(value, "/*") {
		parts := strings.Split(strings.TrimSuffix(value, "/*"), "/")
		if kind != V4SourceMarketplace && (len(parts) != 1 || func() bool { _, ok := v4Kinds[parts[0]]; return !ok }()) {
			return v4err("v4.exclusion", value, 0, "git and directory wildcard exclusions require a resource kind")
		}
		if kind != V4SourceMarketplace && len(parts) != 1 {
			return v4err("v4.exclusion", value, 0, "git and directory wildcard exclusions are kind/* or *")
		}
		if kind == V4SourceMarketplace {
			if len(parts) == 1 {
				if _, isKind := v4Kinds[parts[0]]; isKind {
					return v4err("v4.exclusion", value, 0, "Marketplace wildcard exclusion must be registry/kind/*, registry/*, or *")
				}
				if !v4SlugRE.MatchString(parts[0]) {
					return v4err("v4.exclusion", value, 0, "invalid Marketplace registry wildcard")
				}
				return nil
			}
			if len(parts) != 2 || !v4SlugRE.MatchString(parts[0]) {
				return v4err("v4.exclusion", value, 0, "Marketplace wildcard exclusion must be registry/kind/*, registry/*, or *")
			}
			if _, isKind := v4Kinds[parts[1]]; !isKind {
				return v4err("v4.exclusion", value, 0, "invalid Marketplace kind wildcard")
			}
		}
		return nil
	}
	_, registry, _, err := parseV4Locator(value, string(kind))
	if err != nil {
		return v4err("v4.exclusion", value, 0, "%v", err)
	}
	if kind != V4SourceMarketplace && registry != "" {
		return v4err("v4.exclusion", value, 0, "git and directory exclusions cannot contain a registry slug")
	}
	if kind == V4SourceMarketplace && registry == "" {
		return v4err("v4.exclusion", value, 0, "Marketplace exclusions require a registry slug")
	}
	return nil
}

func validateV4Dynamic(g V4ResourceGroup, source string) error {
	// The acquisition kind is only known after the source binding is composed;
	// normalizeV4Tree performs the Marketplace bound check there.
	return nil
}

func decisionsForIncludes(source string, reqs []V4ResourceRequest, file string) []V4SelectionDecision {
	out := make([]V4SelectionDecision, 0, len(reqs))
	for i := range reqs {
		r := reqs[i]
		out = append(out, V4SelectionDecision{SourceName: source, Locator: r.Locator, Action: V4SelectionExplicit, Request: &r, Provenance: r.Provenance[0]})
	}
	return out
}
func decisionsForExclusions(source string, values []string, node *yaml.Node, file string) []V4SelectionDecision {
	out := make([]V4SelectionDecision, 0, len(values))
	for i, v := range values {
		line := 0
		if node != nil && node.Kind == yaml.SequenceNode && i < len(node.Content) {
			line = node.Content[i].Line
		}
		out = append(out, V4SelectionDecision{SourceName: source, Locator: v, Action: V4SelectionExclude, Exclusion: v, Provenance: V4Provenance{File: file, Line: line}})
	}
	return out
}

func assignV4DecisionPrecedence(group *V4ResourceGroup) {
	if group == nil {
		return
	}
	for i := range group.Decisions {
		group.Decisions[i].Provenance.Precedence = i
		if group.Decisions[i].Request != nil {
			group.Decisions[i].Request.Provenance = append([]V4Provenance(nil), group.Decisions[i].Request.Provenance...)
			if len(group.Decisions[i].Request.Provenance) == 0 {
				group.Decisions[i].Request.Provenance = []V4Provenance{{File: group.Decisions[i].Provenance.File, Line: group.Decisions[i].Provenance.Line}}
			}
			group.Decisions[i].Request.Provenance[0].Precedence = i
		}
	}
	includeIndex := 0
	for i := range group.Decisions {
		if group.Decisions[i].Action != V4SelectionExplicit || includeIndex >= len(group.Include) {
			continue
		}
		group.Include[includeIndex].Provenance = append([]V4Provenance(nil), group.Include[includeIndex].Provenance...)
		if len(group.Include[includeIndex].Provenance) == 0 {
			group.Include[includeIndex].Provenance = []V4Provenance{{File: group.Decisions[i].Provenance.File, Line: group.Decisions[i].Provenance.Line}}
		}
		group.Include[includeIndex].Provenance[0].Precedence = i
		includeIndex++
	}
}

func loadV4Tree(file string, root bool, visiting map[string]bool) (*V4Config, error) {
	return loadV4TreeWithOverlays(file, root, visiting, nil, nil, "")
}

func loadV4TreeWithOverlays(file string, root bool, visiting map[string]bool, overlays map[string][]byte, rootData []byte, organizationPath string, kinds ...layerKind) (*V4Config, error) {
	return loadV4TreeWithResolvedRoot(file, "", root, visiting, overlays, rootData, organizationPath, kinds...)
}

func loadV4TreeWithResolvedRoot(file, resolvedRoot string, root bool, visiting map[string]bool, overlays map[string][]byte, rootData []byte, organizationPath string, kinds ...layerKind) (*V4Config, error) {
	if organizationPath != "" {
		if root && !filepath.IsAbs(organizationPath) {
			wd, err := os.Getwd()
			if err != nil {
				return nil, fmt.Errorf("resolve organization config path %q: %w", organizationPath, err)
			}
			organizationPath = wd + string(filepath.Separator) + organizationPath
			file = organizationPath
		}
		if !root {
			if err := validateOrganizationInclude(file, organizationPath); err != nil {
				return nil, err
			}
		}
	}
	canonical := ""
	var err error
	if root && resolvedRoot != "" {
		canonical = filepath.Clean(resolvedRoot)
	} else {
		canonical, err = canonicalConfigPathTolerateMissing(file)
		if err != nil {
			return nil, err
		}
	}
	if visiting[canonical] {
		return nil, v4err("v4.include-cycle", canonical, 0, "config include cycle")
	}
	data, staged, err := v4ConfigData(canonical, file, root, rootData, overlays)
	if err != nil {
		return nil, fmt.Errorf("read v4 config %q: %w", canonical, err)
	}
	requested := file
	if abs, absErr := filepath.Abs(file); absErr == nil {
		requested = abs
	}
	read := ConfigRead{Path: canonical, RequestedPath: requested, Digest: fmt.Sprintf("%x", sha256.Sum256(data)), Overlay: staged}
	if len(kinds) == 0 && isGlobalConfigPath(file) {
		kinds = []layerKind{userLayer}
	}
	doc, err := parseV4Document(data, canonical, root, kinds...)
	if err != nil {
		var validation *V4ValidationError
		if errors.As(err, &validation) && (validation.Code == "v4.api-version" || validation.Code == "v4.api-version-required") {
			validation.Path = file
		}
		return nil, err
	}
	visiting[canonical] = true
	defer delete(visiting, canonical)
	result := &V4Config{APIVersion: APIVersionV4, Sources: map[string]V4Source{}, Groups: map[string]V4ResourceGroup{}}
	inc := mappingValue(yamlNodeRoot(data), "include")
	if inc != nil {
		paths := []string{}
		if inc.Kind == yaml.ScalarNode {
			paths = append(paths, inc.Value)
		} else {
			for _, x := range inc.Content {
				paths = append(paths, x.Value)
			}
		}
		for _, p := range paths {
			childPath := p
			if !filepath.IsAbs(childPath) {
				if organizationPath != "" {
					childPath = lexicalConfigParent(file) + string(filepath.Separator) + childPath
				} else {
					childPath = filepath.Join(filepath.Dir(canonical), childPath)
				}
			}
			child, e := loadV4TreeWithResolvedRoot(childPath, "", false, visiting, overlays, nil, organizationPath, kinds...)
			if e != nil {
				return nil, fmt.Errorf("load v4 include %q from %q: %w", childPath, canonical, e)
			}
			if err := mergeV4Config(result, child); err != nil {
				return nil, fmt.Errorf("compose v4 include %q from %q: %w", childPath, canonical, err)
			}
		}
	}
	if err := mergeV4Document(result, doc); err != nil {
		return nil, err
	}
	result.reads = append(result.reads, read)
	if root {
		if err := normalizeV4Tree(result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// v4ConfigData returns the bytes to parse for one file and whether they came
// from a staged overlay. A staged overlay takes precedence over caller-supplied
// root bytes so the overlay flag never describes bytes that were not used.
func v4ConfigData(canonical, requested string, root bool, rootData []byte, overlays map[string][]byte) ([]byte, bool, error) {
	for _, key := range []string{canonical, requested} {
		if data, ok := overlays[key]; ok {
			bounded, err := boundedConfigData(data)
			return bounded, true, err
		}
	}
	if root && rootData != nil {
		data, err := boundedConfigData(rootData)
		return data, false, err
	}
	data, err := v4ReadConfigFile(canonical)
	return data, false, err
}

var v4ReadConfigFile = readConfigFile

func yamlNodeRoot(data []byte) *yaml.Node {
	var n yaml.Node
	if yaml.Unmarshal(data, &n) != nil || n.Kind != yaml.DocumentNode || len(n.Content) == 0 {
		return nil
	}
	return n.Content[0]
}

func mergeV4Config(dst, src *V4Config) error {
	// Precedence belongs to the decision stream, not the file stream: a single
	// included file may contribute several decisions, while a shared fragment
	// may be visited once per include branch.
	offset := dst.decisionStreamOffset
	srcDecisionCount := 0
	for _, group := range src.Groups {
		srcDecisionCount += len(group.Decisions)
	}
	dst.Files = append(dst.Files, src.Files...)
	dst.reads = append(dst.reads, src.reads...)
	if src.HasHarnesses {
		dst.Harnesses, dst.HasHarnesses = cloneV4StringList(src.Harnesses), true
	}
	// Allowlist policy is first-authority: an included child claims the field
	// before its containing document can provide a weaker replacement.
	if dst.AllowedRegistries == nil && src.AllowedRegistries != nil {
		values := append([]string{}, (*src.AllowedRegistries)...)
		dst.AllowedRegistries = &values
	}
	if dst.AllowedMarketplaces == nil && src.AllowedMarketplaces != nil {
		values := append([]string{}, (*src.AllowedMarketplaces)...)
		dst.AllowedMarketplaces = &values
	}
	dst.Backup = mergeV4Backup(dst.Backup, src.Backup)
	dst.Logging = mergeV4Logging(dst.Logging, src.Logging)
	dst.UpdateCheck = mergeV4UpdateCheck(dst.UpdateCheck, src.UpdateCheck)
	dst.Telemetry = mergeV4Telemetry(dst.Telemetry, src.Telemetry, src.telemetryFields)
	dst.telemetryFields = mergeV4TelemetryFields(dst.telemetryFields, src.telemetryFields)
	dst.Materialization = mergeV4Materialization(dst.Materialization, src.Materialization)
	dst.GUI = mergeGUIConfig(dst.GUI, src.GUI)
	for name, s := range src.Sources {
		if old, ok := dst.Sources[name]; ok {
			if old.Kind != s.Kind || old.CanonicalURL != s.CanonicalURL || old.Ref != s.Ref || old.Path != s.Path || old.SourceHarness != s.SourceHarness {
				return v4err("v4.source-conflict", "sources."+name, s.Line, "source name is declared with conflicting values in %q and %q", old.File, s.File)
			}
			s.AlsoDeclaredIn = append(append(append([]string(nil), s.AlsoDeclaredIn...), old.AlsoDeclaredIn...), old.File)
		}
		dst.Sources[name] = s
	}
	groupNames := make([]string, 0, len(src.Groups))
	for name := range src.Groups {
		groupNames = append(groupNames, name)
	}
	sort.Strings(groupNames)
	for _, name := range groupNames {
		g := src.Groups[name]
		for i := range g.Include {
			for j := range g.Include[i].Provenance {
				g.Include[i].Provenance[j].Precedence += offset
			}
		}
		for i := range g.Decisions {
			g.Decisions[i].Provenance.Precedence += offset
			if g.Decisions[i].Request != nil {
				r := *g.Decisions[i].Request
				for j := range r.Provenance {
					r.Provenance[j].Precedence += offset
				}
				g.Decisions[i].Request = &r
			}
		}
		mergeV4Group(dst, name, g)
	}
	dst.Decisions = append(dst.Decisions, src.Decisions...)
	dst.decisionStreamOffset += srcDecisionCount
	return nil
}
func mergeV4Backup(dst, src *BackupConfig) *BackupConfig {
	if dst == nil && src == nil {
		return nil
	}
	out := &BackupConfig{}
	if dst != nil {
		*out = *dst
	}
	if src != nil {
		if src.Enabled != nil {
			value := *src.Enabled
			out.Enabled = &value
		}
		if src.MaxDays != nil {
			value := *src.MaxDays
			out.MaxDays = &value
		}
		if src.MaxPerDay != nil {
			value := *src.MaxPerDay
			out.MaxPerDay = &value
		}
	}
	return out
}

func mergeV4UpdateCheck(dst, src *UpdateCheckConfig) *UpdateCheckConfig {
	if dst == nil && src == nil {
		return nil
	}
	out := &UpdateCheckConfig{}
	if dst != nil {
		*out = *dst
	}
	if src != nil && src.Enabled != nil {
		value := *src.Enabled
		out.Enabled = &value
	}
	return out
}

func mergeV4Logging(dst, src *LoggingConfig) *LoggingConfig {
	if dst == nil && src == nil {
		return nil
	}
	out := &LoggingConfig{}
	if dst != nil {
		*out = *dst
	}
	if src != nil {
		if src.Enabled != nil {
			value := *src.Enabled
			out.Enabled = &value
		}
		if src.Level != nil {
			value := *src.Level
			out.Level = &value
		}
		if src.File != nil {
			value := *src.File
			out.File = &value
		}
		if src.MaxSizeMB != nil {
			value := *src.MaxSizeMB
			out.MaxSizeMB = &value
		}
		if src.MaxBackups != nil {
			value := *src.MaxBackups
			out.MaxBackups = &value
		}
		if src.MaxAgeDays != nil {
			value := *src.MaxAgeDays
			out.MaxAgeDays = &value
		}
		if src.Compress != nil {
			value := *src.Compress
			out.Compress = &value
		}
	}
	return out
}

func mergeV4Materialization(dst, src *MaterializationConfig) *MaterializationConfig {
	return mergeMaterializationConfig(dst, src)
}

func mergeV4TelemetryFields(dst, src v4TelemetryFields) v4TelemetryFields {
	return v4TelemetryFields{
		level: dst.level || src.level, endpoint: dst.endpoint || src.endpoint,
		communityReporting: dst.communityReporting || src.communityReporting,
		retentionPolicy:    dst.retentionPolicy || src.retentionPolicy, caFile: dst.caFile || src.caFile,
		allowInsecure: dst.allowInsecure || src.allowInsecure, headers: dst.headers || src.headers,
	}
}

func mergeV4Telemetry(dst, src *telemetryconfig.TelemetryConfig, srcFields v4TelemetryFields) *telemetryconfig.TelemetryConfig {
	if dst == nil && src == nil {
		return nil
	}
	out := &telemetryconfig.TelemetryConfig{}
	if dst != nil {
		*out = *dst
		out.Headers = cloneV4TelemetryHeaders(dst.Headers)
		if dst.CommunityReporting != nil {
			value := *dst.CommunityReporting
			out.CommunityReporting = &value
		}
	}
	if src == nil {
		return out
	}
	field := func(explicit bool, value string) bool { return explicit || value != "" }
	if field(srcFields.level, src.Level) {
		out.Level = src.Level
	}
	if field(srcFields.endpoint, src.Endpoint) {
		out.Endpoint = src.Endpoint
	}
	if srcFields.communityReporting || src.CommunityReporting != nil {
		if src.CommunityReporting == nil {
			out.CommunityReporting = nil
		} else {
			value := *src.CommunityReporting
			out.CommunityReporting = &value
		}
	}
	if field(srcFields.retentionPolicy, src.RetentionPolicy) {
		out.RetentionPolicy = src.RetentionPolicy
	}
	if field(srcFields.caFile, src.CAFile) {
		out.CAFile = src.CAFile
	}
	if srcFields.allowInsecure || src.AllowInsecure {
		out.AllowInsecure = src.AllowInsecure
	}
	if srcFields.headers || len(src.Headers) > 0 {
		if out.Headers == nil {
			out.Headers = map[string]telemetryconfig.HeaderSource{}
		}
		for name, header := range src.Headers {
			out.Headers[name] = header
		}
	}
	return out
}

func cloneV4TelemetryHeaders(headers map[string]telemetryconfig.HeaderSource) map[string]telemetryconfig.HeaderSource {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]telemetryconfig.HeaderSource, len(headers))
	for name, header := range headers {
		out[name] = header
	}
	return out
}

func mergeV4Document(dst *V4Config, doc *v4Document) error {
	src := &V4Config{Sources: doc.sources, Groups: doc.groups, Files: []string{doc.path}, Harnesses: doc.harnesses, HasHarnesses: doc.hasHarnesses, AllowedRegistries: doc.allowedRegistries, AllowedMarketplaces: doc.allowedMarketplaces, Backup: doc.backup, Logging: doc.logging, UpdateCheck: doc.updateCheck, Telemetry: doc.telemetry, Materialization: doc.materialization, GUI: doc.gui, telemetryFields: doc.telemetryFields}
	return mergeV4Config(dst, src)
}
func mergeV4Group(dst *V4Config, name string, src V4ResourceGroup) {
	d, ok := dst.Groups[name]
	if !ok {
		dst.Groups[name] = src
		return
	}
	d.SourceName = name
	d.Include = append(d.Include, src.Include...)
	d.Exclude = append(d.Exclude, src.Exclude...)
	d.Decisions = append(d.Decisions, src.Decisions...)
	if src.HasTags {
		d.Tags, d.HasTags = append([]string(nil), src.Tags...), true
	}
	if src.HasAll {
		b := *src.All
		d.All, d.HasAll = &b, true
	}
	if src.HasRegistries {
		d.Registries, d.HasRegistries = append([]string(nil), src.Registries...), true
	}
	if src.HasRequired {
		b := *src.Required
		d.Required, d.HasRequired = &b, true
	}
	if src.HasHarnesses {
		d.Harnesses, d.HasHarnesses = cloneV4StringList(src.Harnesses), true
	}
	dst.Groups[name] = d
}

func normalizeV4Tree(c *V4Config) error {
	for name, src := range c.Sources {
		if src.SourceHarness != "" && !target.IsValid(src.SourceHarness) {
			return v4err("v4.harness", "sources."+name+".source_harness", src.Line, "unknown target %q", src.SourceHarness)
		}
	}
	for name := range c.Groups {
		if _, ok := c.Sources[name]; !ok {
			return v4err("v4.undefined-source", "resources."+name, 0, "resource group references undefined source %q", name)
		}
	}
	for name, g := range c.Groups {
		src := c.Sources[name]
		dynamic := (g.HasAll && g.All != nil && *g.All) || (g.HasTags && len(g.Tags) > 0)
		if (src.Kind == V4SourceGit || src.Kind == V4SourceDirectory) && g.HasRegistries {
			return v4err("v4.option-combination", "resources."+name+".registries", 0, "registries is only valid for Marketplace sources")
		}
		for _, exclusion := range g.Exclude {
			if err := validateV4ExclusionForSource(exclusion, src.Kind); err != nil {
				return err
			}
		}
		if dynamic && src.Kind == V4SourceMarketplace && (!g.HasRegistries || len(g.Registries) == 0) {
			return v4err("v4.dynamic-unbounded", "resources."+name, 0, "Marketplace all/tags selection requires a non-empty registries list")
		}
		if g.HasHarnesses {
			for i, harness := range g.Harnesses {
				if !target.IsValid(harness) {
					return v4err("v4.harness", fmt.Sprintf("resources.%s.harnesses[%d]", name, i), 0, "unknown target %q", harness)
				}
			}
		}
		for i := range g.Include {
			if g.Required != nil {
				g.Include[i].Required = *g.Required
			}
			if g.HasHarnesses {
				g.Include[i].Harnesses = cloneV4StringList(g.Harnesses)
				g.Include[i].HasHarnesses = true
			}
			if g.Include[i].Options.Revision != "" && !v4RevisionRE.MatchString(g.Include[i].Options.Revision) {
				return v4err("v4.revision", "resources."+name+".include", 0, "revision must be sha256:<64 lowercase hex>")
			}
			if src.Kind != V4SourceMarketplace && g.Include[i].Registry != "" {
				return v4err("v4.locator", "resources."+name+".include", 0, "git and directory locators must be kind/name without a registry slug")
			}
			if g.Include[i].Options.Revision != "" && src.Kind != V4SourceMarketplace {
				return v4err("v4.option-combination", "resources."+name+".include", 0, "revision is only valid for Marketplace resources")
			}
			if src.Kind == V4SourceMarketplace && g.Include[i].Registry == "" {
				return v4err("v4.locator", "resources."+name+".include", 0, "Marketplace locators require a registry slug")
			}
		}
		// Associate decisions with include occurrences by stream order, not by
		// locator. A higher-precedence occurrence may intentionally repeat a
		// locator with wholly different options.
		includeIndex := 0
		for i := range g.Decisions {
			if g.Decisions[i].Action != V4SelectionExplicit {
				continue
			}
			if includeIndex >= len(g.Include) {
				return v4err("v4.internal", "resources."+name, 0, "explicit decision has no matching request")
			}
			g.Decisions[i].Request = &g.Include[includeIndex]
			includeIndex++
		}
		c.Groups[name] = g
	}
	c.rebuildRequests()
	return nil
}

func (c *V4Config) rebuildRequests() {
	if c == nil {
		return
	}
	c.Requests = nil
	c.Decisions = nil
	groupNames := make([]string, 0, len(c.Groups))
	for name := range c.Groups {
		groupNames = append(groupNames, name)
	}
	sort.Strings(groupNames)
	for _, name := range groupNames {
		g := c.Groups[name]
		byLocator := map[string]V4ResourceRequest{}
		excluded := map[string]bool{}
		for _, d := range g.Decisions {
			c.Decisions = append(c.Decisions, d)
			if d.Action == V4SelectionExclude {
				for loc, request := range byLocator {
					if v4ExclusionMatches(d.Exclusion, loc) && !request.Required {
						excluded[loc] = true
					}
				}
				continue
			}
			if d.Request != nil {
				byLocator[d.Locator] = *d.Request
				excluded[d.Locator] = false
			}
		}
		for _, r := range g.Include {
			if _, ok := byLocator[r.Locator]; !ok {
				byLocator[r.Locator] = r
			}
			if excluded[r.Locator] {
				continue
			}
		}
		locators := make([]string, 0, len(byLocator))
		for locator := range byLocator {
			locators = append(locators, locator)
		}
		sort.Strings(locators)
		for _, locator := range locators {
			r := byLocator[locator]
			if !excluded[locator] {
				c.Requests = append(c.Requests, r)
			}
		}
	}
}
func v4ExclusionMatches(exclude, locator string) bool {
	if exclude == "*" {
		return true
	}
	if strings.HasSuffix(exclude, "/*") {
		base := strings.TrimSuffix(exclude, "/*")
		parts := strings.Split(locator, "/")
		if len(parts) == 2 && base == parts[0] {
			return true
		}
		if len(parts) == 3 && (base == parts[0] || base == parts[0]+"/"+parts[1]) {
			return true
		}
	}
	return exclude == locator
}

// ToLegacyConfig adapts a validated v4 document to the internal requirement
// model used by catalog loading and resolution. It deliberately keeps the
// v4 schema version on the returned value so writers can reject legacy edits;
// the adapter is read-only compatibility, not a second v4 serializer.
func (c *V4Config) ToLegacyConfig(kind layerKind) (*Config, error) {
	return c.toLegacyConfig(kind, nil)
}

// ToLegacyConfigWithLock adapts a validated v4 document with its owning
// acquisition lock. Marketplace sources hydrate exact pins from normalized v2
// lock records; a nil or non-v2 lock fails closed for Marketplace state.
func (c *V4Config) ToLegacyConfigWithLock(kind layerKind, doc *lockfile.Document) (*Config, error) {
	if doc != nil && doc.APIVersion == lockfile.APIVersionV2 {
		normalized, err := lockfile.Normalize(*doc)
		if err != nil {
			return nil, v4err(v4MarketplaceLockMismatchCode, "", 0, "lock failed codec normalization: %v", err)
		}
		doc = &normalized
	}
	return c.toLegacyConfig(kind, doc)
}

// validateLayerFields enforces which fields each layer kind may carry. It is
// the lock-free, adaptation-free half of toLegacyConfig's document checks, so
// editors can refuse a layer-invalid document before writing any bytes.
func (c *V4Config) validateLayerFields(kind layerKind) error {
	if kind == localLayer && c.HasHarnesses {
		return v4err("v4.layer-field", "harnesses", 0, "harnesses are not valid in repository personal configuration")
	}
	if kind == localLayer && (c.AllowedRegistries != nil || c.AllowedMarketplaces != nil) {
		return v4err("v4.layer-field", "allowed_registries", 0, "allowlists are not valid in repository personal configuration")
	}
	if kind == localLayer && (c.Backup != nil || c.Logging != nil || c.Telemetry != nil) {
		return v4err("v4.layer-field", "auxiliary", 0, "backup, logging, and telemetry are not valid in repository personal configuration")
	}
	if kind == repoLayer && (c.Backup != nil || c.Logging != nil || c.Telemetry != nil) {
		return v4err("v4.layer-field", "auxiliary", 0, "backup, logging, and telemetry are not valid in repository configuration")
	}
	if (kind == repoLayer || kind == localLayer) && c.UpdateCheck != nil {
		return v4err("v4.layer-field", "update_check", 0, "update_check is only valid in organization and user configuration")
	}
	if kind == organizationLayer && c.Materialization != nil {
		return v4err("v4.layer-field", "materialization", 0, "materialization is not valid in organization configuration")
	}
	return nil
}

func (c *V4Config) marketplaceExcludeOnly(name string) bool {
	group := c.Groups[name]
	return len(group.Exclude) > 0 && len(group.Include) == 0 && !group.HasAll && !group.HasTags && !group.HasRegistries && !group.HasRequired && !group.HasHarnesses
}

// AcquiresMarketplace reports whether source name is a Marketplace source that
// acquires resources and therefore owns a lock record. An exclusion-only
// source only suppresses pins selected elsewhere and never participates in a
// lock.
func (c *V4Config) AcquiresMarketplace(name string) bool {
	return c.Sources[name].Kind == V4SourceMarketplace && !c.marketplaceExcludeOnly(name)
}

func (c *V4Config) marketplaceRegistrationOnly(name string) bool {
	group := c.Groups[name]
	return len(group.Include) == 0 && len(group.Exclude) == 0 && !group.HasAll && !group.HasTags && !group.HasRegistries && !group.HasRequired && !group.HasHarnesses
}

type v4BindingMode uint8

const (
	v4ExactBinding v4BindingMode = iota
	v4AuthoredBinding
	v4RootProjectionBinding
)

func (c *V4Config) toLegacyConfig(kind layerKind, lockDoc *lockfile.Document, modes ...v4BindingMode) (*Config, error) {
	if c == nil {
		return nil, v4err("v4.runtime", "", 0, "nil configuration")
	}
	if err := c.validateLayerFields(kind); err != nil {
		return nil, err
	}
	mode := v4ExactBinding
	if len(modes) > 0 {
		mode = modes[0]
	}
	projection := mode == v4RootProjectionBinding
	out := (LayerIntent{Source: sourceForLayer(kind), v4: c}).Settings().config()

	names := make([]string, 0, len(c.Sources))
	for name := range c.Sources {
		names = append(names, name)
	}
	sort.Strings(names)

	var lockByEndpoint map[string]lockfile.MarketplaceLock
	marketplaceEndpoints := map[string]string{}
	exclusionOnlyEndpoints := map[string]bool{}
	for _, name := range names {
		src := c.Sources[name]
		if !c.AcquiresMarketplace(name) {
			if src.Kind == V4SourceMarketplace {
				exclusionOnlyEndpoints[src.CanonicalURL] = true
			}
			continue
		}
		if _, duplicate := marketplaceEndpoints[src.CanonicalURL]; duplicate {
			return nil, v4err(v4MarketplaceLockMismatchCode, "sources."+name, src.Line, "duplicate source binding for Marketplace endpoint %q", src.CanonicalURL)
		}
		marketplaceEndpoints[src.CanonicalURL] = name
	}
	if len(marketplaceEndpoints) > 0 && lockDoc != nil && lockDoc.APIVersion == lockfile.APIVersionV2 {
		expected := lockfile.Scope(sourceForLayer(kind).LockScope())
		if lockDoc.Scope != expected {
			return nil, v4err(v4MarketplaceLockMismatchCode, "", 0, "lock scope %q does not match %q", lockDoc.Scope, expected)
		}
		lockByEndpoint = make(map[string]lockfile.MarketplaceLock, len(lockDoc.Marketplaces))
		for _, mp := range lockDoc.Marketplaces {
			if _, ok := marketplaceEndpoints[mp.URL]; !ok {
				// A record left over from before the source became exclusion-only
				// is stale, not a mismatch; the next lock refresh drops it.
				if exclusionOnlyEndpoints[mp.URL] || projection {
					continue
				}
				return nil, v4err(v4MarketplaceLockMismatchCode, "", 0, "lock endpoint %q has no matching Marketplace source", mp.URL)
			}
			lockByEndpoint[mp.URL] = mp
		}
	}
	for _, name := range names {
		src := c.Sources[name]
		if !c.AcquiresMarketplace(name) || mode != v4ExactBinding && c.marketplaceRegistrationOnly(name) {
			continue
		}
		if lockDoc == nil || lockDoc.APIVersion != lockfile.APIVersionV2 {
			return nil, v4err(v4MarketplaceLockRequiredCode, "sources."+name, src.Line, "Marketplace resources require an acquisition lock")
		}
		if _, ok := lockByEndpoint[src.CanonicalURL]; !ok {
			return nil, v4err(v4MarketplaceLockMismatchCode, "sources."+name, 0, "Marketplace source %q has no lock record for endpoint %q", name, src.CanonicalURL)
		}
	}

	for _, name := range names {
		src := c.Sources[name]
		if src.Kind == V4SourceMarketplace {
			group := c.Groups[name]
			for _, exclusion := range effectiveV4Exclusions(group) {
				if kind != userLayer && !c.AcquiresMarketplace(name) {
					return nil, v4err("v4.layer-field", "resources."+name+".exclude", src.Line, "Marketplace overrides are only supported in the user configuration")
				}
				if parts := strings.Split(exclusion, "/"); len(parts) != 3 || parts[1] == "collection" || parts[2] == "*" {
					return nil, v4err("v4.marketplace-runtime-unsupported", "resources."+name+".exclude", src.Line, "Marketplace exclusions require an exact non-collection registry/kind/name locator")
				}
				out.MarketplaceExclusions = append(out.MarketplaceExclusions, MarketplaceExclusion{URL: src.CanonicalURL, Locator: exclusion, Source: sourceForLayer(kind)})
			}
			if !c.AcquiresMarketplace(name) {
				continue
			}
			locked, present := lockByEndpoint[src.CanonicalURL]
			if !present && c.marketplaceRegistrationOnly(name) {
				out.Marketplaces = append(out.Marketplaces, MarketplaceEntry{URL: src.CanonicalURL, Alias: name, Source: sourceForLayer(kind)})
				continue
			}
			if locked.InstanceID == "" {
				return nil, v4err(v4MarketplaceLockMismatchCode, "sources."+name, src.Line, "deployed Marketplace evidence has no instance identity")
			}
			entry, err := c.marketplaceEntryForLock(name, src, group, locked, kind, projection)
			if err != nil {
				return nil, err
			}
			out.Marketplaces = append(out.Marketplaces, *entry)
			continue
		}
		entry := RegistryEntry{SourceHarness: src.SourceHarness, Ref: src.Ref, Path: src.Path, URL: src.URL, V4Kind: src.Kind}
		if src.Kind == V4SourceDirectory {
			entry.Ref = ""
			entry.Path = ""
			entry.AuthoredDirectory = src.AuthoredDirectory
			entry.Source = sourceForLayer(kind)
		}
		if group, ok := c.Groups[name]; ok {
			entry.FilePrecedence = effectiveV4GroupPrecedence(group)
			// Keep the ordered v4 stream as runtime state. Reconstructing policy
			// from Include/Exclude sets loses a later root include after a child
			// exclusion, and cannot represent re-inclusion after a wildcard.
			entry.V4Decisions = cloneV4Decisions(group.Decisions)
			if entry.V4Decisions == nil {
				entry.V4Decisions = []V4SelectionDecision{}
			}
			if group.Required != nil {
				entry.Required = *group.Required
			}
			if group.HasAll && group.All != nil && *group.All {
				entry.Default = true
				entry.V4All = true
			}
			if group.HasTags {
				entry.Tags = cloneV4StringList(group.Tags)
			}
			if group.HasHarnesses {
				entry.Harnesses = cloneV4StringList(group.Harnesses)
			}
			// A required-only group is a valid empty selection in v4. Legacy
			// Required implies auto-select-all, so only retain Required when the
			// v4 group has an actual selection policy.
			if entry.Required && len(group.Include) == 0 && (!group.HasTags || len(group.Tags) == 0) && (!group.HasAll || group.All == nil || !*group.All) {
				entry.Required = false
			}
			for _, request := range effectiveV4Requests(group) {
				if request.Options.HasAs && request.Options.As != request.Name {
					return nil, v4err("v4.runtime-option", "resources."+name+".include", 0, "installed aliases are not supported for consumer selections")
				}
				selector := entry.SelectorForKindPtr(v4KindToIR(request.Kind))
				if selector == nil {
					return nil, v4err("v4.locator", request.Locator, 0, "unsupported resource kind %q", request.Kind)
				}
				selector.Include = append(selector.Include, request.Name)
			}
			for _, exclusion := range effectiveV4Exclusions(group) {
				if exclusion == "*" {
					entry.ExcludeAll = true
					continue
				}
				kindName, resourceName := v4ExclusionKindName(exclusion)
				selector := entry.SelectorForKindPtr(v4KindToIR(kindName))
				if selector == nil {
					return nil, v4err("v4.exclusion", "resources."+name+".exclude", 0, "unsupported exclusion %q", exclusion)
				}
				if resourceName == "*" {
					selector.ExcludeAll = true
				} else {
					selector.Exclude = append(selector.Exclude, resourceName)
				}
			}
		}
		out.Registries = append(out.Registries, entry)
	}
	return out, nil
}

func effectiveV4GroupPrecedence(group V4ResourceGroup) int {
	precedence := -1
	for _, decision := range group.Decisions {
		precedence = max(precedence, decision.Provenance.Precedence)
	}
	return precedence + 1
}

// marketplaceEntryForLock hydrates one v4 Marketplace source binding from its
// normalized lock record. Only explicit include requests are supported; the
// lock's deterministic resource order is retained.
func (c *V4Config) marketplaceEntryForLock(name string, src V4Source, group V4ResourceGroup, mp lockfile.MarketplaceLock, kind layerKind, projection bool) (*MarketplaceEntry, error) {
	sourcePath := "resources." + name
	if group.HasAll || group.HasTags || group.HasRegistries || group.HasHarnesses {
		return nil, v4err("v4.marketplace-runtime-unsupported", sourcePath, src.Line, "Marketplace sources support only explicit include requests in this slice")
	}
	bindings := make(map[string]string, len(mp.RegistryBindings))
	for _, binding := range mp.RegistryBindings {
		bindings[binding.Name] = binding.RegistryID
	}
	resources := make(map[string]int, len(mp.Resources))
	for i := range mp.Resources {
		r := &mp.Resources[i]
		resources[r.RegistryID+"\x00"+r.Kind+"\x00"+r.Name] = i
	}
	direct := make(map[int]V4ResourceRequest)
	for _, request := range effectiveV4Requests(group) {
		if request.HasHarnesses {
			return nil, v4err("v4.marketplace-runtime-unsupported", request.Locator, src.Line, "Marketplace requests do not support harnesses options")
		}
		registryID, ok := bindings[request.Registry]
		if !ok {
			return nil, v4err(v4MarketplaceLockMismatchCode, request.Locator, src.Line, "registry binding %q is not in the lock", request.Registry)
		}
		lockKind := request.MarketplaceKind()
		index, ok := resources[registryID+"\x00"+lockKind+"\x00"+request.Name]
		if !ok {
			return nil, v4err(v4MarketplaceLockMismatchCode, request.Locator, src.Line, "resource %q is not in the lock", request.Locator)
		}
		if request.Options.Revision != "" && request.Options.Revision != mp.Resources[index].Revision {
			return nil, v4err(v4MarketplaceLockMismatchCode, request.Locator, src.Line, "requested revision %q does not match locked revision %q", request.Options.Revision, mp.Resources[index].Revision)
		}
		if previous, exists := direct[index]; exists {
			return nil, v4err(v4MarketplaceLockMismatchCode, request.Locator, src.Line, "requests %q and %q resolve to the same locked resource", previous.Locator, request.Locator)
		}
		direct[index] = request
	}

	collections := make(map[string]int)
	for i := range mp.Resources {
		if mp.Resources[i].Kind == api.KindCollection {
			r := &mp.Resources[i]
			collections[r.RegistryID+"\x00"+r.Name] = i
		}
	}
	reachable := make([]bool, len(mp.Resources))
	for i := range direct {
		reachable[i] = true
	}
	if len(direct) < len(mp.Resources) {
		children := make([][]int, len(mp.Resources))
		for i := range mp.Resources {
			if reachable[i] {
				continue
			}
			for _, edge := range mp.Resources[i].SelectedBy {
				parent, ok := collections[edge.RegistryID+"\x00"+edge.Name]
				if ok && mp.Resources[parent].ContentDigest == edge.Revision {
					children[parent] = append(children[parent], i)
				}
			}
		}
		pending := make([]int, 0, len(direct))
		for i := range direct {
			pending = append(pending, i)
		}
		for next := 0; next < len(pending); next++ {
			for _, child := range children[pending[next]] {
				if !reachable[child] {
					reachable[child] = true
					pending = append(pending, child)
				}
			}
		}
	}
	entry := &MarketplaceEntry{URL: mp.URL, InstanceID: mp.InstanceID, Alias: name, Source: sourceForLayer(kind), Required: group.Required != nil && *group.Required}
	for i := range mp.Resources {
		if !reachable[i] {
			if projection {
				continue
			}
			r := &mp.Resources[i]
			return nil, v4err(v4MarketplaceLockMismatchCode, sourcePath, src.Line, "locked resource %s/%s/%s is not requested or reachable from a requested collection", r.RegistryID, r.Kind, r.Name)
		}
		r := &mp.Resources[i]
		pin := MarketplaceResourcePin{
			RegistryID:                   r.RegistryID,
			RegistrySlug:                 r.RegistryName,
			Kind:                         r.Kind,
			Name:                         r.Name,
			Revision:                     r.Revision,
			Digest:                       string(r.ContentDigest),
			Acquisition:                  r.Acquisition,
			RegistryCommit:               r.RegistryCommit,
			RegistryManifestDigest:       string(r.RegistryManifestDigest),
			LockArchiveDigest:            string(r.ArchiveDigest),
			LockCollectionManifestDigest: string(r.CollectionManifestDigest),
			LockSourcePath:               r.SourcePath,
		}
		selections := make([]MarketplaceSelection, 0, len(r.SelectedBy))
		for _, edge := range r.SelectedBy {
			if projection {
				parent, exists := collections[edge.RegistryID+"\x00"+edge.Name]
				if !exists || !reachable[parent] || mp.Resources[parent].ContentDigest != edge.Revision {
					continue
				}
			}
			selections = append(selections, MarketplaceSelection{RegistryID: edge.RegistryID, Name: edge.Name, Revision: edge.Revision.String()})
		}
		pin.SelectedBy = MarketplaceSelectionsFrom(selections)
		if request, ok := direct[i]; ok {
			pin.Direct = true
			if request.Options.HasAs {
				pin.InstalledName = request.Options.As
			}
		}
		entry.Resources = append(entry.Resources, pin)
	}
	return entry, nil
}

func cloneV4StringList(values []string) []string {
	if values == nil {
		return nil
	}
	return append(make([]string, 0, len(values)), values...)
}

func cloneV4Decisions(values []V4SelectionDecision) []V4SelectionDecision {
	if values == nil {
		return nil
	}
	out := make([]V4SelectionDecision, len(values))
	copy(out, values)
	for i := range out {
		if values[i].Request != nil {
			r := *values[i].Request
			r.Harnesses = cloneV4StringList(r.Harnesses)
			r.Provenance = append([]V4Provenance(nil), r.Provenance...)
			out[i].Request = &r
		}
	}
	return out
}

// effectiveV4Requests applies the ordered decision stream to explicit
// locators, preserving a later include after an earlier exclusion.
func effectiveV4Requests(group V4ResourceGroup) []V4ResourceRequest {
	requests := map[string]V4ResourceRequest{}
	excluded := map[string]bool{}
	for _, decision := range group.Decisions {
		if decision.Action == V4SelectionExclude {
			for locator, request := range requests {
				if !request.Required && v4ExclusionMatches(decision.Exclusion, locator) {
					excluded[locator] = true
				}
			}
			continue
		}
		if decision.Request != nil {
			requests[decision.Locator] = *decision.Request
			excluded[decision.Locator] = false
		}
	}
	locators := make([]string, 0, len(requests))
	for locator, request := range requests {
		if !excluded[locator] {
			_ = request
			locators = append(locators, locator)
		}
	}
	sort.Strings(locators)
	out := make([]V4ResourceRequest, 0, len(locators))
	for _, locator := range locators {
		out = append(out, requests[locator])
	}
	return out
}

func effectiveV4Exclusions(group V4ResourceGroup) []string {
	seen := map[string]bool{}
	var active map[string]bool
	var out []string
	for _, decision := range group.Decisions {
		if decision.Action != V4SelectionExclude || seen[decision.Exclusion] {
			continue
		}
		// An exact exclusion superseded by a later include is no longer active.
		_, resourceName := v4ExclusionKindName(decision.Exclusion)
		if resourceName != "*" {
			if active == nil {
				requests := effectiveV4Requests(group)
				active = make(map[string]bool, len(requests))
				for _, request := range requests {
					active[request.Locator] = true
				}
			}
			if active[decision.Exclusion] {
				seen[decision.Exclusion] = true
				continue
			}
		}
		seen[decision.Exclusion] = true
		out = append(out, decision.Exclusion)
	}
	return out
}

func v4KindToIR(kind string) ir.ResourceKind {
	switch kind {
	case "skill":
		return ir.KindSkill
	case "command":
		return ir.KindCommand
	case "subagent":
		return ir.KindSubagent
	case "hook":
		return ir.KindHook
	case "mcp":
		return ir.KindMcpServer
	case "collection":
		return ir.KindCollection
	default:
		return ir.ResourceKind(-1)
	}
}

func v4ExclusionKindName(value string) (string, string) {
	parts := strings.Split(strings.TrimSuffix(value, "/*"), "/")
	if len(parts) == 1 {
		return parts[0], "*"
	}
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", ""
}
