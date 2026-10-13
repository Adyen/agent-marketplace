// Package target defines the authoritative harness-target registry.
//
// The package deliberately has no dependencies on compilation, ingestion, or
// marketplace packages. Those packages consume this registry, which keeps
// target identity and user-facing metadata free of import cycles.
package target

import "fmt"

// Target identifies a harness layout used as a compilation destination or
// source format.
type Target string

const (
	TargetClaude   Target = "claude"
	TargetFactory  Target = "factory"
	TargetCursor   Target = "cursor"
	TargetWindsurf Target = "windsurf"
	TargetGemini   Target = "gemini"
	TargetDevin    Target = "devin"
	TargetCodex    Target = "codex"
)

// Definition is the metadata every target must declare. Compiler and Ingester
// describe the explicit registrations supplied by the respective
// harness-compilation packages; completeness tests verify those registrations
// at the integration boundary.
type Definition struct {
	Target           Target
	Label            string
	RepoRoot         string
	UserRoot         string
	PartitionedRoots bool
	Compiler         bool
	Ingester         bool
	Conversion       bool
	UI               bool
	UIOrder          int
	// HookProjection is the optional hook-only native projection topology.
	// It is additive to the root models and the legacy NativeLayout
	// declarations: several targets do not declare a complete NativeLayout,
	// so the hook projection seam must not wait for one. Paths inside it are
	// relative to the scope root the Definition owns (RepoRoot or UserRoot);
	// a partitioned-root target names its sub-root explicitly. The hook
	// projection compiler and native inspection consume this single
	// declaration; no second path switch exists.
	HookProjection *NativeHookProjection
}

// ValidateDefinition rejects target metadata that mixes or omits the legacy
// single-root and partitioned-root models.
func ValidateDefinition(definition Definition) error {
	hasRepoRoot := definition.RepoRoot != ""
	hasUserRoot := definition.UserRoot != ""
	if hasRepoRoot != hasUserRoot {
		return fmt.Errorf("target %q must declare both legacy roots", definition.Target)
	}
	if definition.PartitionedRoots == hasRepoRoot {
		return fmt.Errorf("target %q must declare exactly one root model", definition.Target)
	}
	return nil
}

var definitions = []Definition{
	{Target: TargetClaude, Label: "Claude Code", RepoRoot: ".claude", UserRoot: ".claude", Compiler: true, Ingester: true, Conversion: true, UI: true, UIOrder: 1,
		HookProjection: &NativeHookProjection{
			Shape:      HookShapeMatcherGroups,
			SupportDir: "hook-support",
			Project:    NativeHookProjectionFile{Path: "settings.json", Member: "hooks"},
			User:       NativeHookProjectionFile{Path: "settings.json", Member: "hooks"},
		}},
	{Target: TargetFactory, Label: "Factory Droid", RepoRoot: ".factory", UserRoot: ".factory", Compiler: true, Ingester: true, Conversion: true, UI: true, UIOrder: 0,
		HookProjection: &NativeHookProjection{
			Shape:      HookShapeMatcherGroups,
			SupportDir: "hook-support",
			Project:    NativeHookProjectionFile{Path: "hooks.json"},
			User:       NativeHookProjectionFile{Path: "hooks.json"},
		}},
	{Target: TargetCursor, Label: "Cursor", RepoRoot: ".cursor", UserRoot: ".cursor", Compiler: true, Ingester: true, Conversion: true, UI: true, UIOrder: 2,
		HookProjection: &NativeHookProjection{
			Shape:      HookShapeEventHandlers,
			SupportDir: "hook-support",
			Project:    NativeHookProjectionFile{Path: "hooks.json", Member: "hooks", Envelope: map[string]any{"version": 1}},
			User:       NativeHookProjectionFile{Path: "hooks.json", Member: "hooks", Envelope: map[string]any{"version": 1}},
		}},
	{Target: TargetWindsurf, Label: "Windsurf", RepoRoot: ".", UserRoot: ".codeium/windsurf", Compiler: true, Ingester: true, Conversion: true, UI: true, UIOrder: 3,
		HookProjection: &NativeHookProjection{
			Shape:      HookShapeEventHandlers,
			SupportDir: "hook-support",
			Project:    NativeHookProjectionFile{Path: ".devin/hooks.json", Member: "hooks"},
			User:       NativeHookProjectionFile{Path: "hooks.json", Member: "hooks"},
		}},
	{Target: TargetGemini, Label: "Gemini CLI", RepoRoot: ".gemini", UserRoot: ".gemini", Compiler: true, Ingester: true, Conversion: true, UI: true, UIOrder: 4,
		HookProjection: &NativeHookProjection{
			Shape:      HookShapeMatcherGroups,
			SupportDir: "hook-support",
			Project:    NativeHookProjectionFile{Path: "settings.json", Member: "hooks"},
			User:       NativeHookProjectionFile{Path: "settings.json", Member: "hooks"},
		}},
	// D8 registered the canonical Devin ingester, so Ingester and Conversion are
	// true: `convert --from devin`, `convert --to devin`, and registry
	// `source_harness: devin` are all valid. UI became true in work package D11.
	// UIOrder 5 preserves the display order of the five targets that shipped
	// before it.
	{Target: TargetDevin, Label: "Devin CLI", RepoRoot: ".devin", UserRoot: ".config/devin", Compiler: true, Ingester: true, Conversion: true, UI: true, UIOrder: 5,
		HookProjection: &NativeHookProjection{
			Shape:      HookShapeMatcherGroups,
			SupportDir: "hook-support",
			Project:    NativeHookProjectionFile{Path: "hooks.v1.json"},
			User:       NativeHookProjectionFile{Path: "config.json", Member: "hooks"},
		}},
	{Target: TargetCodex, Label: "Codex", PartitionedRoots: true, Compiler: true, Ingester: true, Conversion: true, UI: true, UIOrder: 6,
		HookProjection: &NativeHookProjection{
			Shape:      HookShapeMatcherGroups,
			SupportDir: ".codex/hook-support",
			Project:    NativeHookProjectionFile{Path: ".codex/hooks.json", Member: "hooks"},
			User:       NativeHookProjectionFile{Path: ".codex/hooks.json", Member: "hooks"},
		}},
}

// Definitions returns all target declarations in stable display order.
func Definitions() []Definition {
	out := append([]Definition(nil), definitions...)
	for i := range out {
		out[i].HookProjection = cloneHookProjection(out[i].HookProjection)
	}
	return out
}

// Lookup returns the declaration for name, if it is a known target.
func Lookup(name string) (Definition, bool) {
	for _, definition := range definitions {
		if definition.Target == Target(name) {
			definition.HookProjection = cloneHookProjection(definition.HookProjection)
			return definition, true
		}
	}
	return Definition{}, false
}

// IsValid reports whether name identifies a declared target.
func IsValid(name string) bool {
	_, ok := Lookup(name)
	return ok
}

// Targets returns all target identifiers in stable display order.
func Targets() []Target {
	return targetsWhere(func(Definition) bool { return true })
}

// CompilerTargets returns targets with an explicitly registered compiler.
func CompilerTargets() []Target {
	return targetsWhere(func(definition Definition) bool { return definition.Compiler })
}

// IngesterTargets returns targets with an explicitly registered ingester.
func IngesterTargets() []Target {
	return targetsWhere(func(definition Definition) bool { return definition.Ingester })
}

// ConversionTargets returns targets exposed by the conversion command in
// stable display order.
func ConversionTargets() []Target {
	return targetsWhere(func(definition Definition) bool { return definition.Conversion })
}

func targetsWhere(include func(Definition) bool) []Target {
	targets := make([]Target, 0, len(definitions))
	for _, definition := range definitions {
		if include(definition) {
			targets = append(targets, definition.Target)
		}
	}
	return targets
}

// IsCompilerTarget reports whether name identifies a target with a compiler.
func IsCompilerTarget(name string) bool {
	definition, ok := Lookup(name)
	return ok && definition.Compiler
}

// IsIngesterTarget reports whether name identifies a target with an ingester.
func IsIngesterTarget(name string) bool {
	definition, ok := Lookup(name)
	return ok && definition.Ingester
}

// IsConversionTarget reports whether name is exposed by conversion UX.
func IsConversionTarget(name string) bool {
	definition, ok := Lookup(name)
	return ok && definition.Conversion
}

// Names returns all target names in stable display order.
func Names() []string { return namesFor(Targets()) }

// CompilerNames returns compiler-backed target names in stable display order.
func CompilerNames() []string { return namesFor(CompilerTargets()) }

// IngesterNames returns ingester-backed target names in stable display order.
func IngesterNames() []string { return namesFor(IngesterTargets()) }

// ConversionNames returns conversion target names in stable display order.
func ConversionNames() []string { return namesFor(ConversionTargets()) }

func namesFor(targets []Target) []string {
	names := make([]string, 0, len(targets))
	for _, target := range targets {
		names = append(names, string(target))
	}
	return names
}
