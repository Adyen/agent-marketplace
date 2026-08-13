// Package compile provides the Compiler interface and per-target registry.
package compile

import (
	"fmt"
	"log/slog"
	"regexp"
	"runtime/debug"
	"slices"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// Target represents a compilation target harness.
type Target = mapping.Target

// Target constants are re-exported from the mapping package for convenience.
const (
	TargetClaude   = mapping.TargetClaude
	TargetFactory  = mapping.TargetFactory
	TargetCursor   = mapping.TargetCursor
	TargetWindsurf = mapping.TargetWindsurf
	TargetGemini   = mapping.TargetGemini
	TargetDevin    = mapping.TargetDevin
)

// Compiler is the interface that each target compiler must implement.
type Compiler interface {
	// Target returns the target that this compiler handles.
	Target() Target
	// Translate compiles a single IR resource into target-specific artifacts
	// and diagnostics.
	Translate(r ir.Resource) ([]artifact.Artifact, []diag.Diagnostic, error)
}

// Scope is the installation scope a caller is compiling for.
//
// Almost every target output path is scope-independent: the same relative path
// is written under the repository root or under the user root, and the caller
// picks the root at materialization time. Devin hooks are the first output
// whose relative path and JSON envelope differ by scope — plan, resource
// mapping: "Hook | Project: `hooks.v1.json`; user: `config.json#hooks`" — so
// the decision has to be available during translation, not only during
// materialization.
type Scope string

const (
	// ScopeProject compiles for a repository-local target root
	// (`<repo>/.devin`). It is the zero value's meaning, so every existing
	// caller keeps its current behavior.
	ScopeProject Scope = "project"
	// ScopeUser compiles for the user target root (`~/.config/devin`).
	ScopeUser Scope = "user"
)

// Options holds the resolved compilation options for a compile invocation.
type Options struct {
	modelOverride           map[string]string
	toolOverride            map[string]string
	pluginRoot              string
	managedByField          string
	managedByValue          string
	scope                   Scope
	mcpCredentialKeyTokens  []string
	mcpInterpolationPattern *regexp.Regexp
}

// ModelOverride returns the configured model override table, if any.
func (o Options) ModelOverride() map[string]string { return o.modelOverride }

// ToolOverride returns the configured tool override table, if any.
func (o Options) ToolOverride() map[string]string { return o.toolOverride }

// PluginRoot returns the caller-provided plugin root for targets that require
// compile-time literal path expansion.
func (o Options) PluginRoot() string { return o.pluginRoot }

func (o Options) ManagedBy() (string, string) { return o.managedByField, o.managedByValue }

// Scope returns the configured installation scope. The zero value is
// ScopeProject: a caller that never thought about scope keeps compiling
// repository-local output, and an unset option can never silently redirect a
// resource into a user-wide configuration file.
func (o Options) Scope() Scope {
	if o.scope == "" {
		return ScopeProject
	}
	return o.scope
}

// McpCredentialKeyTokens returns the configured credential-key token list for
// MCP literal-credential detection. The zero value uses the default list.
func (o Options) McpCredentialKeyTokens() []string {
	tokens := o.mcpCredentialKeyTokens
	if len(tokens) == 0 {
		tokens = defaultMcpCredentialKeyTokens
	}
	return slices.Clone(tokens)
}

// McpInterpolationPattern returns the configured interpolation pattern for
// MCP literal-credential detection. The zero value uses the default pattern.
func (o Options) McpInterpolationPattern() *regexp.Regexp {
	if o.mcpInterpolationPattern == nil {
		return defaultMcpInterpolationPattern
	}
	return o.mcpInterpolationPattern
}

// OptionAwareCompiler is implemented by compilers that need access to
// invocation options in addition to the IR resource.
type OptionAwareCompiler interface {
	Compiler
	TranslateWithOptions(r ir.Resource, opts Options) ([]artifact.Artifact, []diag.Diagnostic, error)
}

// registry holds the set of registered compilers, keyed by Target.
var registry = map[Target]Compiler{}

// Register adds a Compiler to the global registry. It panics if a compiler
// is already registered for the same Target.
func Register(c Compiler) {
	t := c.Target()
	if _, exists := registry[t]; exists {
		panic(fmt.Sprintf("compile: compiler already registered for target %q", t))
	}
	registry[t] = c
}

// Lookup returns the Compiler registered for the given target, if any.
func Lookup(t Target) (Compiler, bool) {
	c, ok := registry[t]
	return c, ok
}

// RegisteredTargets returns the targets that currently have a registered
// compiler, sorted by name. It is a read-only view for startup diagnostics and
// registration-completeness tests; callers must not mutate the registry.
func RegisteredTargets() []Target {
	targets := make([]Target, 0, len(registry))
	for t := range registry {
		targets = append(targets, t)
	}
	slices.Sort(targets)
	return targets
}

// Result holds the artifacts and diagnostics produced by compilation.
type Result struct {
	Artifacts   []artifact.Artifact
	Diagnostics []diag.Diagnostic
}

// Option configures compilation behavior via functional options.
type Option func(*Options)

// WithModelOverride sets a source-to-target mapping override for model IDs.
func WithModelOverride(m map[string]string) Option {
	return func(o *Options) { o.modelOverride = m }
}

// WithToolOverride sets a source-to-target mapping override for tool IDs.
func WithToolOverride(m map[string]string) Option {
	return func(o *Options) { o.toolOverride = m }
}

// WithPluginRoot sets the literal plugin root used by targets like Windsurf
// that must materialize ${CLAUDE_PLUGIN_ROOT} during translation.
func WithPluginRoot(path string) Option {
	return func(o *Options) { o.pluginRoot = path }
}

func WithManagedBy(field, value string) Option {
	return func(o *Options) {
		o.managedByField = field
		o.managedByValue = value
	}
}

// WithScope selects the installation scope the caller is compiling for. An
// unrecognized value is ignored so an unknown scope can never quietly land
// output in an unexpected file; Options.Scope then reports ScopeProject.
func WithScope(scope Scope) Option {
	return func(o *Options) {
		if scope != ScopeProject && scope != ScopeUser {
			slog.Warn("compile: ignoring unrecognized scope", "scope", scope)
			return
		}
		o.scope = scope
	}
}

// WithMcpCredentialKeyTokens overrides the credential-key token list used by
// MCP literal-credential detection. An empty list clears the override.
func WithMcpCredentialKeyTokens(tokens ...string) Option {
	return func(o *Options) { o.mcpCredentialKeyTokens = slices.Clone(tokens) }
}

// WithMcpInterpolationPattern overrides the interpolation pattern used by
// MCP literal-credential detection. If pattern is not a valid regular
// expression, the option is ignored and the previous pattern is kept.
func WithMcpInterpolationPattern(pattern string) Option {
	return func(o *Options) {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return
		}
		o.mcpInterpolationPattern = re
	}
}

// WithMcpOptions returns an Option that copies the MCP credential-detection
// settings from opts. It lets callers pass the resolved Options value down to
// helpers that accept variadic Options.
func WithMcpOptions(opts Options) Option {
	return func(o *Options) {
		o.mcpCredentialKeyTokens = slices.Clone(opts.mcpCredentialKeyTokens)
		o.mcpInterpolationPattern = opts.mcpInterpolationPattern
	}
}

// translateSafely dispatches to the option-aware translator when the compiler
// implements it, and converts a panicking translator into an error.
func translateSafely(c Compiler, resource ir.Resource, options Options) (arts []artifact.Artifact, diags []diag.Diagnostic, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("compile: compiler rejected resource after panic",
				"target", c.Target(), "resource_type", fmt.Sprintf("%T", resource),
				"panic", recovered, "stack", string(debug.Stack()))
			err = fmt.Errorf("compile: compiler rejected resource type %T: %v", resource, recovered)
			arts, diags = nil, nil
		}
	}()
	if configurable, ok := c.(OptionAwareCompiler); ok {
		return configurable.TranslateWithOptions(resource, options)
	}
	return c.Translate(resource)
}

// Compile looks up the compiler for the given target in the global registry,
// calls Translate, and wraps the output into a Result.
// If no compiler is registered for the target, it returns an error.
func Compile(r ir.Resource, t Target, opts ...Option) (*Result, error) {
	kind, name, validationErr := ir.ValidateResource(r)
	if validationErr != nil {
		return nil, fmt.Errorf("compile: invalid resource: %w", validationErr)
	}
	slog.Debug("compile start", "target", t, "kind", kind, "resource", name)
	c, ok := registry[t]
	if !ok {
		return nil, fmt.Errorf("compile: no compiler registered for target %q", t)
	}

	cfg := &Options{}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}

	var (
		arts  []artifact.Artifact
		diags []diag.Diagnostic
		err   error
	)
	arts, diags, err = translateSafely(c, r, *cfg)
	if err != nil {
		slog.Error("compile failed", "target", t, "kind", kind, "resource", name, "error", err)
		return nil, err
	}

	slog.Debug("compile end", "target", t, "kind", kind, "resource", name, "artifacts", len(arts), "diagnostics", len(diags))
	return &Result{
		Artifacts:   arts,
		Diagnostics: diags,
	}, nil
}
