// Package ir defines the intermediate representation for compilable AI-agent
// configuration resources. Each resource type (Skill, Command, Subagent, Hook,
// McpServer) implements the Resource interface so it can be uniformly processed
// by the compilation pipeline.
package ir

import (
	"fmt"
	"reflect"

	extpkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
)

// ResourceKind identifies the kind of a compilable resource.
type ResourceKind int

const (
	KindSkill      ResourceKind = iota // A skill resource
	KindCommand                        // A command resource
	KindSubagent                       // A subagent resource
	KindHook                           // A hook resource
	KindMcpServer                      // An MCP server resource
	KindCollection                     // A collection (virtual) resource
	KindRule                           // A rule / coding-convention resource
)

// String returns a human-readable name for the resource kind.
func (k ResourceKind) String() string {
	switch k {
	case KindSkill:
		return "skill"
	case KindCommand:
		return "command"
	case KindSubagent:
		return "subagent"
	case KindHook:
		return "hook"
	case KindMcpServer:
		return "mcpserver"
	case KindCollection:
		return "collection"
	case KindRule:
		return "rule"
	default:
		return "unknown"
	}
}

// Manifest describes the bundle metadata. For harnesses that have a manifest
// file (e.g. .claude-plugin/plugin.json), it is populated from that file.
// For workspace-form layouts, it is synthesized.
type Manifest struct {
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Version     string         `json:"version,omitempty"`
	Extra       map[string]any `json:"-"` // preserved unknown fields
}

// Resource is the interface implemented by all compilable resource types.
// Each resource carries a Kind for dispatch and a Name for identification.
type Resource interface {
	Kind() ResourceKind
	Name() string
}

// ValidateName validates the canonical resource-name grammar used by artifact
// paths. Names must start with an ASCII letter and contain only letters,
// digits, hyphens, and underscores.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("resource name must not be empty")
	}
	if (name[0] < 'a' || name[0] > 'z') && (name[0] < 'A' || name[0] > 'Z') {
		return fmt.Errorf("resource name %q must start with an ASCII letter", name)
	}
	for _, c := range name {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			return fmt.Errorf("resource name %q must contain only letters, digits, hyphens, and underscores", name)
		}
	}
	return nil
}

// ValidateResource checks the public compilation boundary before invoking a
// target translator. Canonical IR pointers are checked without interface
// dispatch; typed-nil resources and invalid/custom kinds cannot reach target
// translators unchecked.
func ValidateResource(resource Resource) (kind ResourceKind, name string, err error) {
	defer func() {
		if recover() != nil {
			kind, name = 0, ""
			err = fmt.Errorf("resource metadata methods panicked for %T", resource)
		}
	}()
	if resource == nil {
		return 0, "", fmt.Errorf("resource must not be nil")
	}
	value := reflect.ValueOf(resource)
	if (value.Kind() == reflect.Chan || value.Kind() == reflect.Func || value.Kind() == reflect.Interface || value.Kind() == reflect.Map || value.Kind() == reflect.Pointer || value.Kind() == reflect.Slice) && value.IsNil() {
		return 0, "", fmt.Errorf("resource must not be a typed nil (%T)", resource)
	}

	switch typed := resource.(type) {
	case *Skill:
		kind, name = typed.Kind(), typed.Name()
	case *Command:
		kind, name = typed.Kind(), typed.Name()
	case *Subagent:
		kind, name = typed.Kind(), typed.Name()
	case *Hook:
		kind, name = typed.Kind(), typed.Name()
	case *McpServer:
		kind, name = typed.Kind(), typed.Name()
	case *Rule:
		kind, name = typed.Kind(), typed.Name()
	case *Collection:
		kind, name = typed.Kind(), typed.Name()
	default:
		kind, name = resource.Kind(), resource.Name()
	}
	if kind < KindSkill || kind > KindRule {
		return 0, "", fmt.Errorf("resource type %T reports unsupported kind %q", resource, kind)
	}
	if kind == KindCollection {
		return kind, name, fmt.Errorf("resource kind %q is not compilable", kind)
	}
	if err := ValidateName(name); err != nil {
		return 0, "", err
	}
	return kind, name, nil
}

// SourceRef points to the original source location of a resource or diagnostic.
type SourceRef struct {
	File string // File path relative to the bundle root
	Line int    // 1-based line number, 0 if unknown
}

// Permissions describes the optional skill permission policy. A nil slice means
// that the corresponding policy was absent; an empty (non-nil) slice means it
// was explicitly present and empty.
type Permissions struct {
	Allow []string       `json:"allow,omitempty"`
	Ask   []string       `json:"ask,omitempty"`
	Deny  []string       `json:"deny,omitempty"`
	Extra map[string]any `json:"-"` // Unknown nested policy fields preserved for round trips.
}

// Skill describes a reusable skill, including invocation and execution policy.
type Skill struct {
	name           string         // Unique kebab-case name
	Description    string         // One-line description
	Body           string         // Markdown body content
	ArgumentHint   string         // Optional argument hint shown to callers
	Model          string         // Optional model identifier
	ModelSource    string         `json:"ModelSource,omitempty"`
	AllowedTools   []string       // Explicitly allowed tool IDs; nil means absent, empty means explicit empty
	ToolSource     string         `json:"ToolSource,omitempty"` // Harness whose native tool names AllowedTools carries; empty means canonical
	Permissions    Permissions    // Optional allow/ask/deny permission policy
	PermissionsSet bool           // Whether permissions was explicitly present in source
	Triggers       []string       // Activation triggers (e.g. "user", "model")
	Subagent       string         // Optional subagent execution mode/name
	Agent          string         // Optional named agent execution
	SupportFiles   []string       // Supporting files relative to the skill root (e.g. scripts/*)
	Ext            extpkg.Bag     // Optional target-specific sidecars
	Extra          map[string]any // Preserved unknown frontmatter fields
	Source         SourceRef      // Origin in the source bundle
}

// SkillOption customizes fields added after the original constructor API.
type SkillOption func(*Skill)

// WithSkillArgumentHint sets the optional invocation argument hint.
func WithSkillArgumentHint(v string) SkillOption { return func(s *Skill) { s.ArgumentHint = v } }

// WithSkillModel sets the optional skill model identifier.
func WithSkillModel(v string) SkillOption { return func(s *Skill) { s.Model = v } }

// WithSkillAllowedTools sets the explicit allowed-tools policy, preserving nil versus empty.
func WithSkillAllowedTools(v []string) SkillOption { return func(s *Skill) { s.AllowedTools = v } }

// WithSkillPermissions sets the explicit permission policy, preserving empty lists.
func WithSkillPermissions(v Permissions) SkillOption {
	return func(s *Skill) { s.Permissions, s.PermissionsSet = v, true }
}

// WithSkillSubagent sets the optional subagent execution mode.
func WithSkillSubagent(v string) SkillOption { return func(s *Skill) { s.Subagent = v } }

// WithSkillAgent sets the optional named agent.
func WithSkillAgent(v string) SkillOption { return func(s *Skill) { s.Agent = v } }

// WithSkillToolSource records the harness whose native tool names AllowedTools
// carries; empty means the canonical vocabulary.
func WithSkillToolSource(v string) SkillOption { return func(s *Skill) { s.ToolSource = v } }

// NewSkill creates a Skill with the stable constructor signature.
func NewSkill(name, description, body string, triggers []string, extra map[string]any, source SourceRef) *Skill {
	return &Skill{name: name, Description: description, Body: body, Triggers: triggers, Extra: extra, Source: source}
}

// NewSkillWithOptions is the additive rich constructor for typed Skill fields.
func NewSkillWithOptions(name, description, body string, triggers []string, extra map[string]any, source SourceRef, options ...SkillOption) *Skill {
	s := NewSkill(name, description, body, triggers, extra, source)
	for _, option := range options {
		if option != nil {
			option(s)
		}
	}
	return s
}

func (s *Skill) Kind() ResourceKind { return KindSkill }

// Name returns the skill name.
func (s *Skill) Name() string { return s.name }

// SetName sets the skill name.
func (s *Skill) SetName(name string) { s.name = name }

// Command represents a compilable command resource.
type Command struct {
	name        string         // Unique kebab-case name
	Description string         // One-line description
	Body        string         // Command body / script content
	Ext         extpkg.Bag     // Optional target-specific sidecars
	Extra       map[string]any // Preserved unknown frontmatter fields
	Source      SourceRef      // Origin in the source bundle
}

// NewCommand creates a Command with the given name and fields.
func NewCommand(name, description, body string, extra map[string]any, source SourceRef) *Command {
	return &Command{name: name, Description: description, Body: body, Extra: extra, Source: source}
}

// Kind returns KindCommand.
func (c *Command) Kind() ResourceKind { return KindCommand }

// Name returns the command name.
func (c *Command) Name() string { return c.name }

// SetName sets the command name.
func (c *Command) SetName(name string) { c.name = name }

// Rule represents a compilable rule resource: a coding convention or standard
// that a harness applies to guide how code is written. Rules can be authored
// either as plain Markdown (Factory-native) or as .mdc files with YAML
// frontmatter (Cursor/Windsurf-native, carrying globs/alwaysApply/description).
type Rule struct {
	name        string         // Unique kebab-case name
	Description string         // One-line description (empty for plain-markdown rules)
	Body        string         // Markdown body content
	Globs       []string       // Optional file glob patterns the rule applies to
	AlwaysApply bool           // Whether the rule always applies regardless of globs
	Ext         extpkg.Bag     // Optional target-specific sidecars
	Extra       map[string]any // Preserved unknown frontmatter fields
	Source      SourceRef      // Origin in the source bundle
}

// NewRule creates a Rule with the given name and fields.
func NewRule(name, description, body string, globs []string, alwaysApply bool, extra map[string]any, source SourceRef) *Rule {
	return &Rule{name: name, Description: description, Body: body, Globs: globs, AlwaysApply: alwaysApply, Extra: extra, Source: source}
}

// Kind returns KindRule.
func (r *Rule) Kind() ResourceKind { return KindRule }

// Name returns the rule name.
func (r *Rule) Name() string { return r.name }

// SetName sets the rule name.
func (r *Rule) SetName(name string) { r.name = name }

// Subagent represents a compilable subagent resource.
type Subagent struct {
	name            string         // Unique kebab-case name
	Description     string         // One-line description
	Body            string         // System prompt / instruction body
	Model           string         // Model identifier (e.g. "sonnet")
	ModelSource     string         `json:"ModelSource,omitempty"`
	Tools           []string       // Allowed tool IDs
	ToolSource      string         `json:"ToolSource,omitempty"` // Harness whose native tool names Tools carries; empty means canonical
	Effort          string         // Reasoning effort level (e.g. "high")
	DisallowedTools []string       // Explicitly disallowed tool IDs
	Isolation       string         // Isolation mode (e.g. "worktree")
	MaxNesting      *int           // Maximum recursive nesting; nil means absent
	Ext             extpkg.Bag     // Optional target-specific sidecars
	Extra           map[string]any // Preserved unknown frontmatter fields
	Source          SourceRef      // Origin in the source bundle
}

// SubagentOption customizes fields added after the original constructor API.
type SubagentOption func(*Subagent)

// WithSubagentMaxNesting sets an explicit maximum recursive nesting depth.
func WithSubagentMaxNesting(v int) SubagentOption { return func(s *Subagent) { s.MaxNesting = &v } }

// WithSubagentMaxNestingPtr sets the maximum recursive nesting depth pointer.
func WithSubagentMaxNestingPtr(v *int) SubagentOption { return func(s *Subagent) { s.MaxNesting = v } }

// WithSubagentToolSource records the harness whose native tool names Tools
// carries; empty means the canonical vocabulary.
func WithSubagentToolSource(v string) SubagentOption { return func(s *Subagent) { s.ToolSource = v } }

// NewSubagent creates a Subagent with the stable constructor signature.
func NewSubagent(name, description, body, model string, tools []string, effort string, disallowedTools []string, isolation string, extra map[string]any, source SourceRef) *Subagent {
	return &Subagent{name: name, Description: description, Body: body, Model: model, Tools: tools, Effort: effort, DisallowedTools: disallowedTools, Isolation: isolation, Extra: extra, Source: source}
}

// NewSubagentWithOptions is the additive rich constructor for typed Subagent fields.
func NewSubagentWithOptions(name, description, body, model string, tools []string, effort string, disallowedTools []string, isolation string, extra map[string]any, source SourceRef, options ...SubagentOption) *Subagent {
	s := NewSubagent(name, description, body, model, tools, effort, disallowedTools, isolation, extra, source)
	for _, option := range options {
		if option != nil {
			option(s)
		}
	}
	return s
}

func (s *Subagent) Kind() ResourceKind { return KindSubagent }

// Name returns the subagent name.
func (s *Subagent) Name() string { return s.name }

// SetName sets the subagent name.
func (s *Subagent) SetName(name string) { s.name = name }

// Hook represents a compilable hook resource.
type Hook struct {
	name       string         // Unique kebab-case name
	Event      string         // Triggering event (e.g. "PreToolUse", "UserPromptSubmit")
	Matcher    string         // Optional tool matcher for tool-specific events
	Command    string         // Shell command to run
	Prompt     string         // Prompt payload for prompt hooks
	Timeout    *int           // Optional timeout in seconds; nil means absent
	Type       string         // Hook type (e.g. "command" or "prompt")
	HookSource string         `json:"HookSource,omitempty"` // Harness whose native event/matcher/env values the hook carries; empty means canonical
	Ext        extpkg.Bag     // Optional target-specific sidecars
	Extra      map[string]any // Preserved unknown fields
	Source     SourceRef      // Origin in the source bundle

	// GroupExtraKeys names the Extra keys the ingester promoted from the
	// enclosing hook group rather than from the handler object itself (for
	// example Factory's group-level commandRegex). It is metadata for
	// source-revision digest projections that must replay the historical
	// handler-level Extra view; it never serializes into lock payloads.
	GroupExtraKeys map[string]bool `json:"-"`

	sourceDefinitionDigest string
}

// HookOption customizes the additive hook fields.
type HookOption func(*Hook)

// WithHookPrompt sets a prompt payload for prompt hooks.
func WithHookPrompt(v string) HookOption { return func(h *Hook) { h.Prompt = v } }

// WithHookTimeout sets an explicit hook timeout, including zero.
func WithHookTimeout(v int) HookOption { return func(h *Hook) { h.Timeout = &v } }

// WithHookTimeoutPtr sets the hook timeout pointer.
func WithHookTimeoutPtr(v *int) HookOption { return func(h *Hook) { h.Timeout = v } }

// WithHookSource records the harness whose native event, matcher, and env
// values the hook carries; empty means the canonical vocabulary.
func WithHookSource(v string) HookOption { return func(h *Hook) { h.HookSource = v } }

// WithHookWindsurfSidecar preserves Windsurf-native hook fields during ingestion.
func WithHookWindsurfSidecar(v *extpkg.Windsurf) HookOption {
	return func(h *Hook) { h.Ext.Windsurf = v }
}

// NewHook creates a Hook with the stable constructor signature.
func NewHook(name, event, command, hookType string, extra map[string]any, source SourceRef) *Hook {
	return &Hook{name: name, Event: event, Command: command, Type: hookType, Extra: extra, Source: source}
}

// NewHookWithMatcher creates a Hook with a tool matcher in addition to the stable fields.
func NewHookWithMatcher(name, event, matcher, command, hookType string, extra map[string]any, source SourceRef) *Hook {
	return &Hook{name: name, Event: event, Matcher: matcher, Command: command, Type: hookType, Extra: extra, Source: source}
}

// NewHookWithOptions is the additive rich constructor for typed Hook fields.
func NewHookWithOptions(name, event, command, hookType string, extra map[string]any, source SourceRef, options ...HookOption) *Hook {
	h := NewHook(name, event, command, hookType, extra, source)
	for _, option := range options {
		if option != nil {
			option(h)
		}
	}
	return h
}

// NewHookWithMatcherOptions is the additive rich constructor for a Hook with a matcher.
func NewHookWithMatcherOptions(name, event, matcher, command, hookType string, extra map[string]any, source SourceRef, options ...HookOption) *Hook {
	h := NewHookWithMatcher(name, event, matcher, command, hookType, extra, source)
	for _, option := range options {
		if option != nil {
			option(h)
		}
	}
	return h
}

func (h *Hook) Kind() ResourceKind { return KindHook }

// Name returns the hook name.
func (h *Hook) Name() string { return h.name }

// SetName sets the hook name.
func (h *Hook) SetName(name string) { h.name = name }

func (h *Hook) SourceDefinitionDigest() string { return h.sourceDefinitionDigest }

func (h *Hook) SetSourceDefinitionDigest(digest string) { h.sourceDefinitionDigest = digest }

// McpServer represents a compilable MCP server resource.
type McpServer struct {
	name              string            // Unique kebab-case name
	Command           string            // Executable command (stdio)
	Args              []string          // Command arguments (stdio)
	Env               map[string]string // Environment variables
	URL               string            // Remote HTTP/SSE endpoint
	Transport         string            // Explicit remote transport
	Headers           map[string]string // Remote HTTP headers
	Disabled          bool              // Whether this server is disabled
	DisabledSet       bool              // Whether disabled was explicitly present
	OAuthClientID     string            // OAuth client identifier
	OAuthClientSecret string            // OAuth client secret (sensitive)
	OAuthResource     string            // OAuth protected resource
	Ext               extpkg.Bag        // Optional target-specific sidecars
	Extra             map[string]any    // Preserved unknown fields
	Source            SourceRef         // Origin in the source bundle
}

// McpServerOption customizes the additive MCP fields.
type McpServerOption func(*McpServer)

// WithMcpURL sets the remote MCP endpoint URL.
func WithMcpURL(v string) McpServerOption { return func(m *McpServer) { m.URL = v } }

// WithMcpTransport sets the remote MCP transport identifier.
func WithMcpTransport(v string) McpServerOption { return func(m *McpServer) { m.Transport = v } }

// WithMcpHeaders sets remote MCP HTTP headers.
func WithMcpHeaders(v map[string]string) McpServerOption { return func(m *McpServer) { m.Headers = v } }

// WithMcpDisabled sets the explicit disabled state, including false.
func WithMcpDisabled(v bool) McpServerOption {
	return func(m *McpServer) { m.Disabled, m.DisabledSet = v, true }
}

// WithMcpOAuth sets MCP OAuth client and protected-resource fields.
func WithMcpOAuth(clientID, clientSecret, resource string) McpServerOption {
	return func(m *McpServer) {
		m.OAuthClientID, m.OAuthClientSecret, m.OAuthResource = clientID, clientSecret, resource
	}
}

// NewMcpServer creates a McpServer with the stable constructor signature.
func NewMcpServer(name, command string, args []string, env map[string]string, extra map[string]any, source SourceRef) *McpServer {
	return &McpServer{name: name, Command: command, Args: args, Env: env, Extra: extra, Source: source}
}

// NewMcpServerWithOptions is the additive rich constructor for typed MCP fields.
func NewMcpServerWithOptions(name, command string, args []string, env map[string]string, extra map[string]any, source SourceRef, options ...McpServerOption) *McpServer {
	m := NewMcpServer(name, command, args, env, extra, source)
	for _, option := range options {
		if option != nil {
			option(m)
		}
	}
	return m
}

func (m *McpServer) Kind() ResourceKind { return KindMcpServer }

// Name returns the MCP server name.
func (m *McpServer) Name() string { return m.name }

// SetName sets the MCP server name.
func (m *McpServer) SetName(name string) { m.name = name }

// CollectionMember describes a single resource that belongs to a collection.
type CollectionMember struct {
	Name     string       // Resource name (kebab-case)
	Kind     ResourceKind // Resource kind (defaults to KindSkill when zero)
	Registry string       // Optional registry URL override
	Ref      string       // Optional git ref override
	Path     string       // Optional skill_path override
}

// Collection is a virtual resource that groups multiple resources (skills,
// commands, subagents, hooks, MCP servers) under a single name. It does not
// materialize to disk — instead, the resolver expands it into its member
// resources, which materialize through the normal path.
type Collection struct {
	name        string
	Version     string
	Description string
	Members     []CollectionMember
	Includes    []string // Names of other collections to nest

	// Source provenance (set during discovery, not serialized).
	SourceRegistryURL string
	SourceRef         string
	SourceCatalogKey  string
	SourcePath        string

	Extra  map[string]any
	Source SourceRef
}

// NewCollection creates a Collection with the given name and members.
func NewCollection(name, version, description string, members []CollectionMember, includes []string, extra map[string]any, source SourceRef) *Collection {
	return &Collection{name: name, Version: version, Description: description, Members: members, Includes: includes, Extra: extra, Source: source}
}

// Kind returns KindCollection.
func (c *Collection) Kind() ResourceKind { return KindCollection }

// Name returns the collection name.
func (c *Collection) Name() string { return c.name }

// SetName sets the collection name.
func (c *Collection) SetName(name string) { c.name = name }

// Label returns the human-readable label for this collection (name@version).
func (c *Collection) Label() string {
	if c.Version != "" {
		return c.name + "@" + c.Version
	}
	return c.name
}
