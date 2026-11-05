package modelpolicy

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"maps"
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

//go:embed catalogs/*.yaml
var catalogFiles embed.FS

type evidence struct {
	Checked string   `yaml:"checked"`
	Refs    []string `yaml:"refs"`
}

type surface struct {
	ModelField       string   `yaml:"model_field"`
	EffortField      string   `yaml:"effort_field"`
	OmittedModel     string   `yaml:"omitted_model"`
	InheritModel     *string  `yaml:"inherit_model"`
	InheritedEfforts []string `yaml:"inherited_efforts"`
}

type Encoding struct {
	Model  string `yaml:"model"`
	Effort string `yaml:"effort"`
}

type record struct {
	Model     string              `yaml:"model"`
	Aliases   []string            `yaml:"aliases"`
	Reasoning map[string]Encoding `yaml:"reasoning"`
	Pin       string              `yaml:"pin"`
	Evidence  *evidence           `yaml:"evidence"`
}

type catalog struct {
	Schema   int                `yaml:"schema"`
	Harness  target.Target      `yaml:"harness"`
	Evidence evidence           `yaml:"evidence"`
	Surfaces map[string]surface `yaml:"surfaces"`
	Models   map[string]record  `yaml:"models"`
	Families map[string]record  `yaml:"families"`
	native   map[Encoding]selection
}

type selection struct {
	Identity string
	Effort   string
}

type registry struct {
	catalogs map[target.Target]catalog
	portable map[string]string
}

var shipped = sync.OnceValues(func() (*registry, error) {
	return loadCatalogs(catalogFiles, "catalogs/*.yaml")
})

func ValidateCatalogs() error {
	_, err := shipped()
	return err
}

func loadCatalogs(files fs.FS, pattern string) (*registry, error) {
	names, err := fs.Glob(files, pattern)
	if err != nil {
		return nil, err
	}
	r := &registry{catalogs: map[target.Target]catalog{}, portable: map[string]string{}}
	for _, name := range names {
		raw, err := fs.ReadFile(files, name)
		if err != nil {
			return nil, err
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("model catalog %s: %w", name, err)
		}
		if err := validateYAML(&doc); err != nil {
			return nil, fmt.Errorf("model catalog %s: %w", name, err)
		}
		decoder := yaml.NewDecoder(bytes.NewReader(raw))
		decoder.KnownFields(true)
		var c catalog
		if err := decoder.Decode(&c); err != nil {
			return nil, fmt.Errorf("model catalog %s: %w", name, err)
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return nil, fmt.Errorf("model catalog %s: expected exactly one document", name)
		}
		if c.Schema != 1 || !target.IsValid(string(c.Harness)) || path.Base(name) != string(c.Harness)+".yaml" {
			return nil, fmt.Errorf("model catalog %s: invalid schema or harness", name)
		}
		if _, exists := r.catalogs[c.Harness]; exists {
			return nil, fmt.Errorf("duplicate model catalog %s", c.Harness)
		}
		if err := c.validate(); err != nil {
			return nil, fmt.Errorf("model catalog %s: %w", name, err)
		}
		r.catalogs[c.Harness] = c
		for _, id := range slices.Sorted(maps.Keys(c.Models)) {
			_, model, _ := strings.Cut(id, "/")
			if previous, exists := r.portable[model]; exists && previous != id {
				r.portable[model] = ""
			} else if !exists {
				r.portable[model] = id
			}
		}
	}
	if len(r.catalogs) == 0 {
		return nil, fmt.Errorf("no model catalogs")
	}
	return r, nil
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

func (e evidence) validate() error {
	if _, err := time.Parse("2006-01-02", e.Checked); err != nil || len(e.Refs) == 0 {
		return fmt.Errorf("evidence requires a checked date and references")
	}
	for _, ref := range e.Refs {
		if !validName(ref) {
			return fmt.Errorf("invalid evidence reference")
		}
	}
	return nil
}

func (c *catalog) validate() error {
	if err := c.Evidence.validate(); err != nil {
		return err
	}
	for _, name := range slices.Sorted(maps.Keys(c.Surfaces)) {
		s := c.Surfaces[name]
		if name != "subagent" && name != "skill" || s.ModelField != "model" || s.OmittedModel != "inherit" && s.OmittedModel != "default" {
			return fmt.Errorf("invalid surface %q", name)
		}
		nativeEffort := map[target.Target]string{target.TargetClaude: "effort", target.TargetFactory: "reasoningEffort", target.TargetCodex: "model_reasoning_effort"}[c.Harness]
		if name == "skill" {
			nativeEffort = ""
		}
		if s.EffortField != "" && s.EffortField != nativeEffort {
			return fmt.Errorf("invalid effort field on %s", name)
		}
		if s.InheritModel != nil && ((*s.InheritModel == "" && s.OmittedModel != "inherit") || *s.InheritModel != "" && !validName(*s.InheritModel)) {
			return fmt.Errorf("invalid inheritance on %s", name)
		}
		seen := map[string]bool{}
		for _, effort := range s.InheritedEfforts {
			if !validName(effort) || seen[effort] || s.EffortField == "" {
				return fmt.Errorf("invalid inherited effort on %s", name)
			}
			seen[effort] = true
		}
	}
	c.native = map[Encoding]selection{}
	for _, records := range []map[string]record{c.Models, c.Families} {
		for _, id := range slices.Sorted(maps.Keys(records)) {
			r := records[id]
			provider, model, qualified := strings.Cut(id, "/")
			if !qualified || !validName(provider) || !validName(model) || strings.Contains(model, "/") {
				return fmt.Errorf("invalid model identity %q", id)
			}
			_, concrete := c.Models[id]
			_, family := c.Families[id]
			if concrete && family {
				return fmt.Errorf("model/family identity collision %q", id)
			}
			if r.Evidence != nil {
				if err := r.Evidence.validate(); err != nil {
					return err
				}
			}
			if r.Pin != "" {
				if _, ok := c.Models[r.Pin]; !ok || concrete || r.Model != "" || len(r.Aliases) != 0 || len(r.Reasoning) != 0 {
					return fmt.Errorf("invalid family pin %q", id)
				}
				continue
			}
			if r.Model == "" && len(r.Reasoning) == 0 {
				return fmt.Errorf("model %q has no encoding", id)
			}
			if r.Model != "" {
				if err := c.addNative(Encoding{Model: r.Model}, selection{Identity: id}); err != nil {
					return err
				}
			}
			for _, alias := range r.Aliases {
				if r.Model == "" {
					return fmt.Errorf("alias without base model %q", id)
				}
				if err := c.addNative(Encoding{Model: alias}, selection{Identity: id}); err != nil {
					return err
				}
			}
			for _, level := range slices.Sorted(maps.Keys(r.Reasoning)) {
				encoding := r.encoding(level)
				if !validName(level) || encoding.Effort != "" && (c.Surfaces["subagent"].EffortField == "" || !validName(encoding.Effort)) {
					return fmt.Errorf("invalid reasoning %q for %s", level, id)
				}
				if err := c.addNative(encoding, selection{Identity: id, Effort: level}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (r record) encoding(level string) Encoding {
	encoding := r.Reasoning[level]
	if encoding.Model == "" {
		encoding.Model = r.Model
	}
	return encoding
}

func (c *catalog) addNative(e Encoding, s selection) error {
	if !validName(e.Model) || e.Model == "inherit" {
		return fmt.Errorf("invalid model encoding for %q", s.Identity)
	}
	if previous, exists := c.native[e]; exists && previous != s {
		return fmt.Errorf("ambiguous native encoding %q", e.Model)
	}
	c.native[e] = s
	return nil
}

func (c catalog) lookup(id string) (record, bool) {
	if r, ok := c.Models[id]; ok {
		return r, true
	}
	r, ok := c.Families[id]
	return r, ok
}
