// Package toolpolicy holds the evidence-backed per-harness tool catalogs and
// the shared harness-neutral concept vocabulary used to translate tool names,
// hook matchers, and tool allow-lists between harnesses.
package toolpolicy

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"gopkg.in/yaml.v3"
)

//go:embed concepts.yaml catalogs/*.yaml
var catalogFiles embed.FS

// Surfaces a harness may expose for tool vocabulary. A missing surface means
// the harness has no such field and the compiler never emits one.
const (
	SurfaceHookMatcher       = "hook_matcher"
	SurfaceSubagentTools     = "subagent_tools"
	SurfaceSkillAllowedTools = "skill_allowed_tools"
)

var knownSurfaces = []string{SurfaceHookMatcher, SurfaceSubagentTools, SurfaceSkillAllowedTools}

type evidence struct {
	Checked string   `yaml:"checked"`
	Refs    []string `yaml:"refs"`
	Notes   string   `yaml:"notes"`
}

type mcpNaming struct {
	Pattern      string `yaml:"pattern"`
	All          string `yaml:"all"`
	ServerScoped bool   `yaml:"server_scoped"`
}

type tool struct {
	Concepts []string `yaml:"concepts"`
	Aliases  []string `yaml:"aliases"`
	Surfaces []string `yaml:"surfaces"`
}

type catalog struct {
	Schema   int               `yaml:"schema"`
	Harness  target.Target     `yaml:"harness"`
	Evidence evidence          `yaml:"evidence"`
	MCP      mcpNaming         `yaml:"mcp"`
	Surfaces []string          `yaml:"surfaces"`
	Tools    map[string]tool   `yaml:"-"`
	ToolDocs yaml.Node         `yaml:"tools"`
	native   map[string]string // accepted spelling (key or alias) -> key
	order    []string          // tool keys in declaration order
}

// Concepts is the shared harness-neutral vocabulary: tool concepts and
// canonical hook events.
type Concepts struct {
	Schema int               `yaml:"schema"`
	Tools  map[string]string `yaml:"tools"`
	Events map[string]string `yaml:"events"`
}

type registry struct {
	concepts Concepts
	catalogs map[target.Target]catalog
}

var shipped = sync.OnceValues(func() (*registry, error) {
	return loadCatalogs(catalogFiles, "concepts.yaml", "catalogs/*.yaml")
})

// ValidateCatalogs validates the embedded concepts vocabulary and every
// shipped harness catalog.
func ValidateCatalogs() error {
	_, err := shipped()
	return err
}

// IsToolConcept reports whether name is a declared tool concept.
func IsToolConcept(name string) bool {
	r, err := shipped()
	if err != nil {
		return false
	}
	_, ok := r.concepts.Tools[name]
	return ok
}

// IsCanonicalEvent reports whether name is a declared canonical hook event.
func IsCanonicalEvent(name string) bool {
	r, err := shipped()
	if err != nil {
		return false
	}
	_, ok := r.concepts.Events[name]
	return ok
}

func loadCatalogs(files fs.FS, conceptsName, catalogPattern string) (*registry, error) {
	r := &registry{catalogs: map[target.Target]catalog{}}
	raw, err := fs.ReadFile(files, conceptsName)
	if err != nil {
		return nil, fmt.Errorf("tool concepts %s: %w", conceptsName, err)
	}
	concepts, err := decode[Concepts](raw)
	if err != nil {
		return nil, fmt.Errorf("tool concepts %s: %w", conceptsName, err)
	}
	if err := concepts.validate(); err != nil {
		return nil, fmt.Errorf("tool concepts %s: %w", conceptsName, err)
	}
	r.concepts = concepts

	names, err := fs.Glob(files, catalogPattern)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		raw, err := fs.ReadFile(files, name)
		if err != nil {
			return nil, err
		}
		c, err := decode[catalog](raw)
		if err != nil {
			return nil, fmt.Errorf("tool catalog %s: %w", name, err)
		}
		if c.Schema != 1 || !target.IsValid(string(c.Harness)) || path.Base(name) != string(c.Harness)+".yaml" {
			return nil, fmt.Errorf("tool catalog %s: invalid schema or harness", name)
		}
		if _, exists := r.catalogs[c.Harness]; exists {
			return nil, fmt.Errorf("duplicate tool catalog %s", c.Harness)
		}
		if err := c.validate(r.concepts); err != nil {
			return nil, fmt.Errorf("tool catalog %s: %w", name, err)
		}
		r.catalogs[c.Harness] = c
	}
	if len(r.catalogs) == 0 {
		return nil, fmt.Errorf("no tool catalogs")
	}
	return r, nil
}

// DecodeStrict parses exactly one YAML document into T, rejecting unknown
// fields, YAML aliases, and duplicate mapping keys. Hook catalogs reuse it so
// every shipped policy file shares one strict decoding contract.
func DecodeStrict[T any](raw []byte) (T, error) {
	return decode[T](raw)
}

// decode parses exactly one YAML document with strict keys, rejecting YAML
// aliases and duplicate mapping keys.
func decode[T any](raw []byte) (T, error) {
	var out T
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return out, err
	}
	if err := validateYAML(&doc); err != nil {
		return out, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&out); err != nil {
		return out, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return out, fmt.Errorf("expected exactly one document")
	}
	return out, nil
}

func validateYAML(node *yaml.Node) error {
	if node.Kind == yaml.AliasNode {
		return fmt.Errorf("YAML aliases are not supported")
	}
	if node.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || seen[key.Value] {
				return fmt.Errorf("invalid or duplicate key at line %d", key.Line)
			}
			seen[key.Value] = true
		}
	}
	for _, child := range node.Content {
		if err := validateYAML(child); err != nil {
			return err
		}
	}
	return nil
}

func validName(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && utf8.ValidString(value) && !strings.ContainsFunc(value, unicode.IsControl)
}

// CheckEvidence validates one catalog's evidence block: an ISO checked date
// and at least one well-formed reference. Hook catalogs reuse it so every
// shipped policy file shares one evidence contract.
func CheckEvidence(checked string, refs []string) error {
	if _, err := time.Parse("2006-01-02", checked); err != nil || len(refs) == 0 {
		return fmt.Errorf("evidence requires a checked date and references")
	}
	for _, ref := range refs {
		if !validName(ref) {
			return fmt.Errorf("invalid evidence reference")
		}
	}
	return nil
}

func (e evidence) validate() error {
	return CheckEvidence(e.Checked, e.Refs)
}

func (c Concepts) validate() error {
	if c.Schema != 1 || len(c.Tools) == 0 || len(c.Events) == 0 {
		return fmt.Errorf("invalid schema or empty vocabulary")
	}
	for name, description := range c.Tools {
		if !validName(name) || !validName(description) {
			return fmt.Errorf("invalid tool concept %q", name)
		}
	}
	for name, description := range c.Events {
		if !validName(name) || !validName(description) {
			return fmt.Errorf("invalid canonical event %q", name)
		}
	}
	return nil
}

func (c *catalog) validate(concepts Concepts) error {
	if err := c.Evidence.validate(); err != nil {
		return err
	}
	surfaces := map[string]bool{}
	for _, surface := range c.Surfaces {
		if !slices.Contains(knownSurfaces, surface) || surfaces[surface] {
			return fmt.Errorf("invalid or duplicate surface %q", surface)
		}
		surfaces[surface] = true
	}
	if !validName(c.MCP.All) {
		return fmt.Errorf("invalid mcp all pattern")
	}
	if c.MCP.Pattern != "" {
		if !strings.Contains(c.MCP.Pattern, "{tool}") {
			return fmt.Errorf("invalid mcp pattern %q: missing {tool}", c.MCP.Pattern)
		}
		if c.MCP.ServerScoped && !strings.Contains(c.MCP.Pattern, "{server}") {
			return fmt.Errorf("invalid mcp pattern %q: server_scoped naming needs {server}", c.MCP.Pattern)
		}
	} else if c.MCP.ServerScoped {
		return fmt.Errorf("server_scoped mcp naming requires a pattern")
	}
	c.native = map[string]string{}
	c.Tools = map[string]tool{}
	c.order = nil
	if c.ToolDocs.Kind != 0 && c.ToolDocs.Kind != yaml.MappingNode {
		return fmt.Errorf("tools must be a mapping")
	}
	for i := 0; i+1 < len(c.ToolDocs.Content); i += 2 {
		keyNode, valueNode := c.ToolDocs.Content[i], c.ToolDocs.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			return fmt.Errorf("invalid tool key at line %d", keyNode.Line)
		}
		name := keyNode.Value
		var t tool
		if err := valueNode.Decode(&t); err != nil {
			return fmt.Errorf("tool %q: %w", name, err)
		}
		if _, exists := c.Tools[name]; exists {
			return fmt.Errorf("duplicate tool %q", name)
		}
		c.Tools[name] = t
		c.order = append(c.order, name)
		if !validName(name) {
			return fmt.Errorf("invalid tool name %q", name)
		}
		if len(t.Concepts) == 0 {
			return fmt.Errorf("tool %q has no concepts", name)
		}
		seen := map[string]bool{}
		for _, concept := range t.Concepts {
			if _, ok := concepts.Tools[concept]; !ok {
				return fmt.Errorf("tool %q uses unknown concept %q", name, concept)
			}
			if seen[concept] {
				return fmt.Errorf("tool %q repeats concept %q", name, concept)
			}
			seen[concept] = true
		}
		for _, surface := range t.Surfaces {
			if !surfaces[surface] {
				return fmt.Errorf("tool %q narrows to undeclared surface %q", name, surface)
			}
		}
		if err := c.addSpelling(name, name); err != nil {
			return err
		}
		for _, alias := range t.Aliases {
			if err := c.addSpelling(alias, name); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *catalog) addSpelling(spelling, key string) error {
	if !validName(spelling) {
		return fmt.Errorf("invalid spelling %q for tool %q", spelling, key)
	}
	if previous, exists := c.native[spelling]; exists && previous != key {
		return fmt.Errorf("alias %q collides with %q and %q", spelling, previous, key)
	}
	c.native[spelling] = key
	return nil
}
