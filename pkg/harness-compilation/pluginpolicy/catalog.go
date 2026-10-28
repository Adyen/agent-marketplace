// Package pluginpolicy holds the evidence-backed plugin catalogs: one per
// recognized package format and one per registered harness target. Plugin
// delivery decisions and plugin-surface field handling are made only from
// these catalogs; native delivery may be declared verified only together
// with the identifier of a native probe present in the harness validation
// suite.
package pluginpolicy

import (
	"embed"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/hookpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/toolpolicy"
	"gopkg.in/yaml.v3"
)

//go:embed formats/*.yaml catalogs/*.yaml
var catalogFiles embed.FS

// Format roles: a package format ships one plugin; a catalog format lists
// plugins to acquire.
const (
	RolePackage = "package"
	RoleCatalog = "catalog"
)

const FactoryNativePluginProbeID = "factory-native-plugin"

// Delivery statuses for delivery.decompose and delivery.native.
const (
	DeliveryVerified    = "verified"
	DeliveryUnverified  = "unverified"
	DeliveryUnsupported = "unsupported"
)

const NativeRefreshRestartRequired = "restart-required"

// Invalid-value outcomes for manifest fields.
const (
	InvalidRefusePackage     = "refuse-package"
	InvalidIgnoreWithWarning = "ignore-with-warning"
)

// Component discovery rules.
const (
	ComponentImmediateChildren = "immediate-children"
	ComponentFile              = "file"
	ComponentInlineOrFile      = "inline-or-file"
)

// Catalog entry source outcomes.
const (
	SourceAcquire = "acquire"
	SourceRefuse  = "refuse"
)

// Plugin-surface field outcomes.
const (
	FieldKeep        = "keep"
	FieldOmitField   = "omit-field"
	FieldBlockMember = "block-member"
)

// Manifest field types.
var knownFieldTypes = []string{"string", "number", "boolean", "object", "array"}

// Catalog entry source types.
var knownSourceTypes = []string{"path", "github", "git", "npm", "url", "command"}

// Policy identifiers a catalog may reference. Each names a behavior
// implemented in Go; catalogs may not introduce new identifiers.
var knownPolicyIDs = []string{"entry-overlay.strict"}

var formatIdentifier = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

var diagnosticCode = regexp.MustCompile(`^[a-z0-9]+(\.[a-z0-9-]+)+$`)

type evidence struct {
	Checked string   `yaml:"checked"`
	Refs    []string `yaml:"refs"`
	Notes   string   `yaml:"notes"`
}

func (e evidence) validate() error {
	return toolpolicy.CheckEvidence(e.Checked, e.Refs)
}

type manifestField struct {
	Required bool   `yaml:"required"`
	Type     string `yaml:"type"`
	Invalid  string `yaml:"invalid"`
}

type manifestPolicy struct {
	Fields map[string]manifestField `yaml:"fields"`
}

type catalogPolicy struct {
	Sources      map[string]string `yaml:"sources"`
	EntryOverlay string            `yaml:"entry_overlay"`
}

type formatCatalog struct {
	Schema       int               `yaml:"schema"`
	Format       string            `yaml:"format"`
	Role         string            `yaml:"role"`
	Evidence     evidence          `yaml:"evidence"`
	Versions     []string          `yaml:"versions"`
	Manifest     manifestPolicy    `yaml:"manifest"`
	Components   map[string]string `yaml:"components"`
	Catalog      *catalogPolicy    `yaml:"catalog"`
	Dependencies string            `yaml:"dependencies"`
}

type deliveryStatus struct {
	Status string `yaml:"status"`
}

type nativeDelivery struct {
	Status  string `yaml:"status"`
	Format  string `yaml:"format"`
	Probe   string `yaml:"probe"`
	Refresh string `yaml:"refresh"`
}

type delivery struct {
	Decompose deliveryStatus `yaml:"decompose"`
	Native    nativeDelivery `yaml:"native"`
}

// fieldOutcome is either a scalar outcome (keep, omit-field) or a mapping:
// {policy: <allowlisted id>} or {block-member: true, code: <diagnostic>}.
type fieldOutcome struct {
	Outcome string
	Policy  string
	Code    string
}

func (o *fieldOutcome) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		return node.Decode(&o.Outcome)
	}
	var mapped struct {
		Policy      string `yaml:"policy"`
		BlockMember bool   `yaml:"block-member"`
		Code        string `yaml:"code"`
	}
	if err := node.Decode(&mapped); err != nil {
		return err
	}
	switch {
	case mapped.Policy != "" && !mapped.BlockMember && mapped.Code == "":
		o.Outcome, o.Policy = "policy", mapped.Policy
	case mapped.BlockMember && mapped.Policy == "":
		o.Outcome, o.Code = FieldBlockMember, mapped.Code
	default:
		return fmt.Errorf("a field outcome mapping declares exactly one of policy or block-member")
	}
	return nil
}

type pluginSurfaces struct {
	Plugin map[string]map[string]fieldOutcome `yaml:"plugin"`
}

type harnessCatalog struct {
	Schema    int            `yaml:"schema"`
	Harness   target.Target  `yaml:"harness"`
	Evidence  evidence       `yaml:"evidence"`
	Accepts   []string       `yaml:"accepts"`
	Delivery  delivery       `yaml:"delivery"`
	Surfaces  pluginSurfaces `yaml:"surfaces"`
	RootToken string         `yaml:"root_token"`
}

type registry struct {
	formats  map[string]formatCatalog
	catalogs map[target.Target]harnessCatalog
}

// ValidateCatalogs validates every shipped plugin format and harness catalog.
// knownProbes is the set of native plugin probe identifiers defined by the
// harness validation suite; a verified native delivery must name one.
func ValidateCatalogs(knownProbes []string) error {
	_, err := loadCatalogs(catalogFiles, "formats/*.yaml", "catalogs/*.yaml", knownProbes)
	return err
}

func loadCatalogs(files fs.FS, formatPattern, catalogPattern string, knownProbes []string) (*registry, error) {
	r := &registry{formats: map[string]formatCatalog{}, catalogs: map[target.Target]harnessCatalog{}}
	formatNames, err := fs.Glob(files, formatPattern)
	if err != nil {
		return nil, err
	}
	for _, name := range formatNames {
		raw, err := fs.ReadFile(files, name)
		if err != nil {
			return nil, err
		}
		c, err := toolpolicy.DecodeStrict[formatCatalog](raw)
		if err != nil {
			return nil, fmt.Errorf("plugin format catalog %s: %w", name, err)
		}
		if c.Schema != 1 || !formatIdentifier.MatchString(c.Format) || path.Base(name) != c.Format+".yaml" {
			return nil, fmt.Errorf("plugin format catalog %s: invalid schema or format %q", name, c.Format)
		}
		if _, exists := r.formats[c.Format]; exists {
			return nil, fmt.Errorf("duplicate plugin format catalog %s", c.Format)
		}
		if err := c.validate(name); err != nil {
			return nil, err
		}
		r.formats[c.Format] = c
	}
	if len(r.formats) == 0 {
		return nil, fmt.Errorf("no plugin format catalogs")
	}
	catalogNames, err := fs.Glob(files, catalogPattern)
	if err != nil {
		return nil, err
	}
	probes := map[string]bool{}
	for _, probe := range knownProbes {
		probes[probe] = true
	}
	for _, name := range catalogNames {
		raw, err := fs.ReadFile(files, name)
		if err != nil {
			return nil, err
		}
		c, err := toolpolicy.DecodeStrict[harnessCatalog](raw)
		if err != nil {
			return nil, fmt.Errorf("plugin harness catalog %s: %w", name, err)
		}
		if c.Schema != 1 || !target.IsValid(string(c.Harness)) || path.Base(name) != string(c.Harness)+".yaml" {
			return nil, fmt.Errorf("plugin harness catalog %s: invalid schema or harness", name)
		}
		if _, exists := r.catalogs[c.Harness]; exists {
			return nil, fmt.Errorf("duplicate plugin harness catalog %s", c.Harness)
		}
		if err := c.validate(name, r.formats, probes); err != nil {
			return nil, err
		}
		r.catalogs[c.Harness] = c
	}
	if len(r.catalogs) == 0 {
		return nil, fmt.Errorf("no plugin harness catalogs")
	}
	return r, nil
}

func validName(value string) bool {
	return value != "" && strings.TrimSpace(value) == value
}

func (c *formatCatalog) validate(name string) error {
	file := fmt.Sprintf("plugin format catalog %s", name)
	if err := c.Evidence.validate(); err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	if !slices.Contains([]string{RolePackage, RoleCatalog}, c.Role) {
		return fmt.Errorf("%s: invalid role %q", file, c.Role)
	}
	for _, version := range c.Versions {
		if !validName(version) {
			return fmt.Errorf("%s: invalid version %q", file, version)
		}
	}
	for _, field := range slices.Sorted(maps.Keys(c.Manifest.Fields)) {
		f := c.Manifest.Fields[field]
		if !validName(field) {
			return fmt.Errorf("%s: invalid manifest field name %q", file, field)
		}
		if !slices.Contains(knownFieldTypes, f.Type) {
			return fmt.Errorf("%s: manifest field %q has invalid type %q", file, field, f.Type)
		}
		if !slices.Contains([]string{InvalidRefusePackage, InvalidIgnoreWithWarning}, f.Invalid) {
			return fmt.Errorf("%s: manifest field %q has invalid outcome %q", file, field, f.Invalid)
		}
	}
	for _, kind := range slices.Sorted(maps.Keys(c.Components)) {
		rule := c.Components[kind]
		if !validName(kind) {
			return fmt.Errorf("%s: invalid component kind %q", file, kind)
		}
		if !slices.Contains([]string{ComponentImmediateChildren, ComponentFile, ComponentInlineOrFile}, rule) {
			return fmt.Errorf("%s: component %q has invalid discovery rule %q", file, kind, rule)
		}
	}
	switch c.Role {
	case RolePackage:
		if c.Catalog != nil {
			return fmt.Errorf("%s: a package format cannot declare catalog", file)
		}
	case RoleCatalog:
		if c.Catalog == nil || len(c.Catalog.Sources) == 0 {
			return fmt.Errorf("%s: a catalog format requires catalog.sources", file)
		}
	}
	if c.Catalog != nil {
		if c.Catalog.EntryOverlay != "" && !slices.Contains(knownPolicyIDs, c.Catalog.EntryOverlay) {
			return fmt.Errorf("%s: catalog.entry_overlay names unknown policy %q", file, c.Catalog.EntryOverlay)
		}
		for _, source := range slices.Sorted(maps.Keys(c.Catalog.Sources)) {
			outcome := c.Catalog.Sources[source]
			if !slices.Contains(knownSourceTypes, source) {
				return fmt.Errorf("%s: catalog.sources names unknown source type %q", file, source)
			}
			if !slices.Contains([]string{SourceAcquire, SourceRefuse}, outcome) {
				return fmt.Errorf("%s: catalog.sources.%s has invalid outcome %q", file, source, outcome)
			}
		}
	}
	if c.Dependencies != InvalidRefusePackage {
		return fmt.Errorf("%s: dependencies must be %q", file, InvalidRefusePackage)
	}
	return nil
}

func (c *harnessCatalog) validate(name string, formats map[string]formatCatalog, probes map[string]bool) error {
	file := fmt.Sprintf("plugin harness catalog %s", name)
	if err := c.Evidence.validate(); err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	for _, accepted := range c.Accepts {
		format, ok := formats[accepted]
		if !ok {
			return fmt.Errorf("%s: accepts names unknown format %q", file, accepted)
		}
		if format.Role != RolePackage {
			return fmt.Errorf("%s: accepts names non-package format %q", file, accepted)
		}
	}
	statuses := []string{DeliveryVerified, DeliveryUnverified, DeliveryUnsupported}
	if !slices.Contains(statuses, c.Delivery.Decompose.Status) {
		return fmt.Errorf("%s: delivery.decompose.status %q is invalid", file, c.Delivery.Decompose.Status)
	}
	native := c.Delivery.Native
	if !slices.Contains(statuses, native.Status) {
		return fmt.Errorf("%s: delivery.native.status %q is invalid", file, native.Status)
	}
	if native.Status != DeliveryVerified && (native.Format != "" || native.Probe != "") {
		return fmt.Errorf("%s: delivery.native.format/probe require verified status", file)
	}
	if native.Refresh != "" && native.Refresh != NativeRefreshRestartRequired {
		return fmt.Errorf("%s: delivery.native.refresh %q is invalid", file, native.Refresh)
	}
	if native.Status == DeliveryVerified {
		if native.Probe == "" {
			return fmt.Errorf("%s: delivery.native verified requires a probe identifier", file)
		}
		if !probes[native.Probe] {
			return fmt.Errorf("%s: delivery.native names undefined probe %q", file, native.Probe)
		}
		format, ok := formats[native.Format]
		if !ok {
			return fmt.Errorf("%s: delivery.native names unknown format %q", file, native.Format)
		}
		if format.Role != RolePackage || !slices.Contains(c.Accepts, native.Format) {
			return fmt.Errorf("%s: delivery.native verified for format %q the harness does not accept", file, native.Format)
		}
	}
	for _, kind := range slices.Sorted(maps.Keys(c.Surfaces.Plugin)) {
		if !validName(kind) {
			return fmt.Errorf("%s: invalid plugin surface kind %q", file, kind)
		}
		for _, field := range slices.Sorted(maps.Keys(c.Surfaces.Plugin[kind])) {
			outcome := c.Surfaces.Plugin[kind][field]
			if !validName(field) {
				return fmt.Errorf("%s: invalid plugin surface field %q", file, field)
			}
			switch outcome.Outcome {
			case FieldKeep, FieldOmitField:
			case "policy":
				if !slices.Contains(knownPolicyIDs, outcome.Policy) {
					return fmt.Errorf("%s: surfaces.plugin.%s.%s names unknown policy %q", file, kind, field, outcome.Policy)
				}
			case FieldBlockMember:
				if !diagnosticCode.MatchString(outcome.Code) {
					return fmt.Errorf("%s: surfaces.plugin.%s.%s block-member requires a diagnostic code", file, kind, field)
				}
			default:
				return fmt.Errorf("%s: surfaces.plugin.%s.%s has invalid outcome %q", file, kind, field, outcome.Outcome)
			}
		}
	}
	if c.RootToken != "" && !slices.Contains(hookpolicy.PluginEnvConcepts(c.Harness), c.RootToken) {
		return fmt.Errorf("%s: root_token names unknown plugin_env concept %q", file, c.RootToken)
	}
	return nil
}
