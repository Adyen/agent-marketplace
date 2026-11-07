// Package hookpolicy holds the evidence-backed per-harness hook catalogs
// describing native events, handler types, matchers, timeouts, environment
// variables, and repository-scope command bases used to translate hooks
// between harnesses.
package hookpolicy

import (
	"embed"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/toolpolicy"
	"gopkg.in/yaml.v3"
)

//go:embed catalogs/*.yaml
var catalogFiles embed.FS

// Timeout units a harness may use for hook deadlines; "none" means the format
// has no timeout field.
const (
	TimeoutSeconds      = "seconds"
	TimeoutMilliseconds = "milliseconds"
	TimeoutNone         = "none"
)

// Matcher syntaxes. "exact-or-regex" treats a plain name as exact and anything
// else as an unanchored regex; "regex" treats the whole matcher as a regex;
// "none" means the harness has no matcher field.
const (
	MatcherExactOrRegex = "exact-or-regex"
	MatcherRegex        = "regex"
	MatcherNone         = "none"
)

// What a hook event's matcher filters.
const (
	MatcherKindTool        = "tool"
	MatcherKindLifecycle   = "lifecycle"
	MatcherKindCommandText = "command_text"
	MatcherKindNone        = "none"
)

// Handler types the IR and catalogs know about.
var knownHandlerTypes = []string{"command", "http", "mcp_tool", "prompt", "agent"}

var shellIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type evidence struct {
	Checked string   `yaml:"checked"`
	Refs    []string `yaml:"refs"`
	Notes   string   `yaml:"notes"`
}

func (e evidence) validate() error {
	return toolpolicy.CheckEvidence(e.Checked, e.Refs)
}

// envVar is either a plain list of accepted spellings (first is emitted) or
// the object form {names, events} limiting the variable to some events.
type envVar struct {
	Names  []string `yaml:"names"`
	Events []string `yaml:"events"`
}

func (v *envVar) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.SequenceNode {
		return node.Decode(&v.Names)
	}
	type plain envVar
	return node.Decode((*plain)(v))
}

type repositoryBase struct {
	Env   string `yaml:"env"`
	Shell string `yaml:"shell"`
	Cwd   string `yaml:"cwd"`
}

type timeoutPolicy struct {
	Unit string `yaml:"unit"`
}

type timeoutBounds struct {
	Min int `yaml:"min"`
	Max int `yaml:"max"`
}

type matcherPolicy struct {
	Syntax     string `yaml:"syntax"`
	Anchor     bool   `yaml:"anchor"`
	ExactLists bool   `yaml:"exact_lists"`
}

type trustReview struct {
	Codes []string `yaml:"codes"`
}

type event struct {
	Canonical  string         `yaml:"canonical"`
	Matcher    string         `yaml:"matcher"`
	Types      []string       `yaml:"types"`
	Timeout    *timeoutBounds `yaml:"timeout"`
	Tools      []string       `yaml:"tools"`
	MCP        bool           `yaml:"mcp"`
	IngestOnly bool           `yaml:"ingest_only"`
}

type catalog struct {
	Schema         int               `yaml:"schema"`
	Harness        target.Target     `yaml:"harness"`
	Evidence       evidence          `yaml:"evidence"`
	Env            map[string]envVar `yaml:"env"`
	PluginEnv      map[string]envVar `yaml:"plugin_env"`
	RepositoryBase *repositoryBase   `yaml:"repository_base"`
	Timeout        timeoutPolicy     `yaml:"timeout"`
	Matcher        matcherPolicy     `yaml:"matcher"`
	LoadsHooksOf   []string          `yaml:"loads_hooks_of"`
	TrustReview    *trustReview      `yaml:"trust_review"`
	Events         map[string]event  `yaml:"events"`
}

type registry struct {
	catalogs    map[target.Target]catalog
	pluginNames map[string]bool
}

var shipped = sync.OnceValues(func() (*registry, error) {
	return loadCatalogs(catalogFiles, "catalogs/*.yaml")
})

// PluginRootNames returns the shipped spellings of the plugin-root concept.
func PluginRootNames() ([]string, error) {
	registry, err := shipped()
	if err != nil {
		return nil, err
	}
	var names []string
	for _, catalog := range registry.catalogs {
		names = append(names, catalog.PluginEnv["plugin_root"].Names...)
	}
	slices.Sort(names)
	return slices.Compact(names), nil
}

// ValidateCatalogs validates every shipped hook catalog against the shared
// concept vocabulary and the catalog schema rules.
func ValidateCatalogs() error {
	_, err := shipped()
	return err
}

func loadCatalogs(files fs.FS, pattern string) (*registry, error) {
	names, err := fs.Glob(files, pattern)
	if err != nil {
		return nil, err
	}
	r := &registry{catalogs: map[target.Target]catalog{}, pluginNames: map[string]bool{}}
	for _, name := range names {
		raw, err := fs.ReadFile(files, name)
		if err != nil {
			return nil, err
		}
		c, err := toolpolicy.DecodeStrict[catalog](raw)
		if err != nil {
			return nil, fmt.Errorf("hook catalog %s: %w", name, err)
		}
		if c.Schema != 1 || !target.IsValid(string(c.Harness)) || path.Base(name) != string(c.Harness)+".yaml" {
			return nil, fmt.Errorf("hook catalog %s: invalid schema or harness", name)
		}
		if _, exists := r.catalogs[c.Harness]; exists {
			return nil, fmt.Errorf("duplicate hook catalog %s", c.Harness)
		}
		if err := c.validate(); err != nil {
			return nil, fmt.Errorf("hook catalog %s: %w", name, err)
		}
		r.catalogs[c.Harness] = c
		for _, v := range c.PluginEnv {
			for _, name := range v.Names {
				r.pluginNames[name] = true
			}
		}
	}
	if len(r.catalogs) == 0 {
		return nil, fmt.Errorf("no hook catalogs")
	}
	return r, nil
}

func (c *catalog) validate() error {
	if err := c.Evidence.validate(); err != nil {
		return err
	}
	if !slices.Contains([]string{TimeoutSeconds, TimeoutMilliseconds, TimeoutNone}, c.Timeout.Unit) {
		return fmt.Errorf("invalid timeout unit %q", c.Timeout.Unit)
	}
	if !slices.Contains([]string{MatcherExactOrRegex, MatcherRegex, MatcherNone}, c.Matcher.Syntax) {
		return fmt.Errorf("invalid matcher syntax %q", c.Matcher.Syntax)
	}
	if c.Matcher.Anchor && c.Matcher.Syntax != MatcherRegex {
		return fmt.Errorf("anchor requires regex matcher syntax")
	}
	if c.Matcher.ExactLists && c.Matcher.Syntax == MatcherNone {
		return fmt.Errorf("exact_lists requires a matcher syntax")
	}
	if len(c.Events) == 0 {
		return fmt.Errorf("catalog declares no events")
	}
	envNames := map[string]string{}
	for _, group := range []struct {
		name string
		vars map[string]envVar
	}{{"env", c.Env}, {"plugin_env", c.PluginEnv}} {
		for _, concept := range slices.Sorted(maps.Keys(group.vars)) {
			v := group.vars[concept]
			if !validConceptName(concept) {
				return fmt.Errorf("invalid %s concept %q", group.name, concept)
			}
			if len(v.Names) == 0 {
				return fmt.Errorf("%s.%s declares no names", group.name, concept)
			}
			for _, name := range v.Names {
				if !shellIdentifier.MatchString(name) {
					return fmt.Errorf("invalid env name %q on %s.%s", name, group.name, concept)
				}
				if previous, exists := envNames[name]; exists {
					return fmt.Errorf("duplicate env name %q on %s.%s and %s", name, group.name, concept, previous)
				}
				envNames[name] = concept
			}
			for _, eventName := range v.Events {
				if _, ok := c.Events[eventName]; !ok {
					return fmt.Errorf("%s.%s limits to unknown event %q", group.name, concept, eventName)
				}
			}
		}
	}
	if c.RepositoryBase != nil {
		set := 0
		for _, present := range []bool{c.RepositoryBase.Env != "", c.RepositoryBase.Shell != "", c.RepositoryBase.Cwd != ""} {
			if present {
				set++
			}
		}
		if set != 1 {
			return fmt.Errorf("repository_base requires exactly one of env, shell, cwd")
		}
		if c.RepositoryBase.Env != "" {
			if _, ok := c.Env[c.RepositoryBase.Env]; !ok {
				return fmt.Errorf("repository_base references unknown env concept %q", c.RepositoryBase.Env)
			}
		}
		if c.RepositoryBase.Cwd != "" && c.RepositoryBase.Cwd != "repository" {
			return fmt.Errorf("repository_base cwd must be %q", "repository")
		}
	}
	for _, other := range c.LoadsHooksOf {
		if !target.IsValid(other) {
			return fmt.Errorf("loads_hooks_of names unknown harness %q", other)
		}
		if other == string(c.Harness) {
			return fmt.Errorf("loads_hooks_of cannot name its own harness %q", other)
		}
	}
	if c.TrustReview != nil {
		if len(c.TrustReview.Codes) == 0 {
			return fmt.Errorf("trust_review declares no codes")
		}
		for _, code := range c.TrustReview.Codes {
			if !validConceptName(code) {
				return fmt.Errorf("invalid trust_review code %q", code)
			}
		}
	}
	split := map[string][]event{}
	plain := map[string]string{}
	for _, name := range slices.Sorted(maps.Keys(c.Events)) {
		e := c.Events[name]
		if !validEventName(name) {
			return fmt.Errorf("invalid event name %q", name)
		}
		if e.Canonical != "" && !toolpolicy.IsCanonicalEvent(e.Canonical) {
			return fmt.Errorf("event %q links unknown canonical event %q", name, e.Canonical)
		}
		if e.IngestOnly && e.Canonical == "" {
			return fmt.Errorf("event %q is ingest_only without a canonical event", name)
		}
		if e.Matcher != "" && !slices.Contains([]string{MatcherKindTool, MatcherKindLifecycle, MatcherKindCommandText, MatcherKindNone}, e.Matcher) {
			return fmt.Errorf("event %q has invalid matcher kind %q", name, e.Matcher)
		}
		if c.Matcher.Syntax == MatcherNone && e.Matcher != "" && e.Matcher != MatcherKindNone {
			return fmt.Errorf("event %q declares a matcher kind on a matcher-less harness", name)
		}
		if len(e.Types) == 0 {
			return fmt.Errorf("event %q allows no handler types", name)
		}
		for _, hookType := range e.Types {
			if !slices.Contains(knownHandlerTypes, hookType) {
				return fmt.Errorf("event %q has invalid handler type %q", name, hookType)
			}
		}
		if e.Timeout != nil {
			if c.Timeout.Unit == TimeoutNone {
				return fmt.Errorf("event %q declares timeout bounds but the harness has no timeout field", name)
			}
			if e.Timeout.Min <= 0 || e.Timeout.Max < e.Timeout.Min {
				return fmt.Errorf("event %q has invalid timeout bounds", name)
			}
		}
		if e.MCP && len(e.Tools) != 0 {
			return fmt.Errorf("event %q cannot declare both tools and mcp", name)
		}
		for _, concept := range e.Tools {
			if !toolpolicy.IsToolConcept(concept) {
				return fmt.Errorf("event %q uses unknown tool concept %q", name, concept)
			}
		}
		if len(e.Tools) != 0 || e.MCP {
			if e.Canonical == "" {
				return fmt.Errorf("event %q splits tools without a canonical event", name)
			}
			split[e.Canonical] = append(split[e.Canonical], e)
		} else if e.Canonical != "" && !e.IngestOnly {
			if previous, exists := plain[e.Canonical]; exists {
				return fmt.Errorf("events %q and %q both compile canonical event %q; mark one ingest_only", previous, name, e.Canonical)
			}
			plain[e.Canonical] = name
		}
	}
	for canonical, events := range split {
		for i := 0; i < len(events); i++ {
			for j := i + 1; j < len(events); j++ {
				if splitOverlap(events[i], events[j]) {
					return fmt.Errorf("events for canonical %q overlap in their split tool sets", canonical)
				}
			}
		}
	}
	return nil
}

// splitOverlap reports whether two event-split entries cover the same
// selection: mcp entries cover every MCP tool and no declared tool concept,
// so two mcp entries collide while an mcp entry and a tool set are disjoint;
// tool sets overlap when they share a concept.
func splitOverlap(a, b event) bool {
	if a.MCP || b.MCP {
		return a.MCP && b.MCP
	}
	return slices.ContainsFunc(a.Tools, func(concept string) bool {
		return slices.Contains(b.Tools, concept)
	})
}

func validConceptName(value string) bool {
	return value != "" && !strings.ContainsAny(value, " \t") && strings.TrimSpace(value) == value
}

var eventName = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func validEventName(value string) bool {
	return eventName.MatchString(value)
}
