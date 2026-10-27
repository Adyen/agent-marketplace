package target

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"reflect"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// NativeLayout describes the target-owned filesystem shape emitted by a
// compiler. The declaration is scope-neutral: consumers resolve partitions
// into concrete repository or machine roots, and this package never embeds
// one.
type NativeLayout struct {
	Partitions []NativePartition
}

// NativePartition describes one independently rooted part of a native layout.
type NativePartition struct {
	// ID names the partition within its target's layout.
	ID string
	// Prefix is the partition root relative to the scope base. A target on the
	// partitioned-root model names each root here (".agents", ".codex"); a
	// target on the legacy single-root model declares exactly one partition
	// with Prefix "." covering the whole root, whose concrete name the target
	// Definition already owns. Partition prefixes must be pairwise
	// element-boundary disjoint: nested roots would let one partition's
	// cleanup sweep another partition's files.
	Prefix string
	// MachineRootEnv names the environment variable that relocates this
	// partition's machine-scope root (CODEX_HOME). Resolving it is consumer
	// policy; the descriptor only declares the name. Only a partitioned-root
	// target may declare it, and each partition's value must be unique so
	// every machine root resolves independently.
	MachineRootEnv string
	// InstallationEvidence marks the partition whose presence proves the
	// target is installed on the machine. Only a partitioned-root target may
	// declare it: a legacy single-root target's evidence is its one root.
	// At most one partition may declare it — detection treats every marked
	// root as evidence, so a second claim on a shared root like .agents would
	// report the target installed on another partition's footprint alone.
	InstallationEvidence bool
	// Components are the root-relative directories the layout writes into.
	// Each is a single path element: materialization routes artifacts by
	// first path element only, so a multi-element component could never be
	// populated and "." would claim the whole partition root as prunable.
	Components []string
	// Retired lists locations, relative to the partition root, that earlier
	// output claimed but the layout no longer writes. A file entry names a
	// shared file consumers reconcile owned entries out of and delete when
	// nothing but empty members remain; a directory entry (trailing slash)
	// is left entirely alone. Consumers never write a retired location.
	Retired []string
	// ResourcePaths maps a resource kind to the root-relative path pattern
	// emitted for a named resource. Every pattern contains exactly one %s name
	// placeholder; when the placeholder is itself a directory element
	// (skills/%s/SKILL.md — see IsDirShapedPattern) the resource materializes
	// as a directory, which requires the kind to be KindSkill or declared in
	// SkillDirKinds. The placeholder must begin a path element
	// (commands/%s.md is valid; %s.md or a%sb.md are not) so the text before
	// it names a real resource directory rather than deriving "." — the
	// partition root — which consumers would register as a cleanup directory.
	// A placeholder element that is not the basename must be exactly "%s":
	// units/%s.unit/manifest.json is rejected because a suffixed non-final
	// element is neither a flat file nor one resource directory per resource
	// — its literal text would leak into derived resource directories while
	// the real per-resource directories go unmodeled.
	ResourcePaths map[ir.ResourceKind]string
	// SkillDirKinds lists the kinds besides KindSkill that materialize as a
	// resource directory rather than a flat file. Every listed kind must
	// declare a ResourcePaths pattern whose %s is a directory element, and
	// every non-skill kind with a directory-shaped pattern must be listed
	// here: IsDirShapedPattern(pattern) and membership in this set must agree
	// exactly. Listing KindSkill itself is rejected — it is implied.
	SkillDirKinds map[ir.ResourceKind]bool
	// Aggregates are the shared target files resource kinds contribute
	// entries to.
	Aggregates    []NativeAggregate
	Registrations []NativeRegistration
}

// HookShape names the entry encoding a hook aggregate file carries: how an
// event key maps to its hook entries inside the file's hook member (or its
// top level, when Member is empty).
type HookShape string

const (
	// HookShapeMatcherGroups is event → array of rule groups
	// ({matcher?, hooks: [handler]}): the Claude-family encoding shared by
	// Factory Droid, Gemini CLI, Devin CLI, and Codex.
	HookShapeMatcherGroups HookShape = "matcher-groups"
	// HookShapeEventHandlers is event → array of handler entries: the flat
	// encoding Cursor (handlers may carry a matcher field) and Windsurf use.
	HookShapeEventHandlers HookShape = "event-handlers"
)

// NativeAggregateFile locates the shared file that carries one resource
// kind's compiled entries within a single scope.
type NativeAggregateFile struct {
	// Path is the file path relative to the partition root. It names a file,
	// never the partition root itself (".") or a directory the partition
	// declares — equal to or an ancestor of a component or resource
	// directory is rejected, because a file cannot occupy a path a declared
	// directory needs. A path strictly inside a ResourcePaths-derived
	// resource directory is likewise rejected — the directory is the
	// per-resource namespace (skills/index.json under skills/%s/SKILL.md
	// would collide with a resource named "index.json") — while a file
	// inside a component directory stays legal (hooks/hooks.json).
	Path string
	// Member names the top-level member that carries the kind's entries when
	// the file is a shared envelope rather than a file dedicated to the kind.
	// Devin's user-scope hooks live under the `hooks` member of config.json.
	// An empty Member means the file's top level carries the entries directly.
	// Consumers key entries under this member, so it is load-bearing: two
	// kinds claiming the same (Path, Member) in one scope would share an
	// entry keyspace and are rejected; the same Path under different members
	// is a legal shared envelope.
	Member string
	// Envelope lists the additional top-level members the file must carry
	// beside Member when it is written — fixed scalar declarations like
	// Cursor's `version: 1`. An envelope key may not equal Member: the
	// entry member is owned by the resource entries, not by static
	// envelope data.
	Envelope map[string]any
	// Legacy lists the superseded locations in the same scope that older
	// output versions or the harness itself still read — plugin-layout
	// hook files such as hooks/hooks.json, or Windsurf's repository-scope
	// .windsurf/hooks.json. Legacy paths are relative to the same partition
	// root as Path and must be clean, non-empty, and distinct from Path.
	// Consumers reconcile owned entries out of them; the declaration never
	// makes them write targets.
	Legacy                    []string
	PreserveLegacyHandwritten bool
	LegacyMarkedFlatHooks     bool
}

// NativeAggregate identifies a shared target file produced for one resource
// kind.
//
// Project and User are declared separately because an aggregate file can
// differ by scope: a Devin hook compiles to hooks.v1.json for a repository
// root but to the `hooks` member of config.json for the user root.
// A scope-invariant aggregate declares the same file in both scopes.
type NativeAggregate struct {
	Kind    ir.ResourceKind
	Project NativeAggregateFile
	User    NativeAggregateFile
	// Shape is the hook entry encoding inside the file, required exactly
	// when Kind is ir.KindHook. It describes how entries nest under the
	// event key so serializers and ingesters need no per-target shape
	// tables.
	Shape HookShape
	// SupportDir is the directory root, relative to the partition root,
	// below which a hook's support files install
	// (<support dir>/<source>/<hook>/). It is required exactly when Kind is
	// ir.KindHook.
	SupportDir            string
	LegacyMarkedFlatHooks bool
}

type NativeRegistrationShape string

const (
	NativeRegistrationClaudeSettings NativeRegistrationShape = "claude-settings"
	NativeRegistrationCodexConfig    NativeRegistrationShape = "codex-config"
)

type NativeRegistration struct {
	Project string
	User    string
	Shape   NativeRegistrationShape
}

type NativeRegistrationFile struct {
	Path  string
	Shape NativeRegistrationShape
}

var nativeLayouts = map[Target]NativeLayout{
	TargetClaude: {Partitions: []NativePartition{
		{
			ID:         "default",
			Prefix:     ".",
			Components: []string{"skills", "commands", "agents", "hooks", "hook-support"},
			// The plugin manifest is retired: consumers strip marked entries
			// out of an existing file and delete it once only empty
			// skills/agents members remain; nothing writes it.
			Retired: []string{".claude-plugin/plugin.json"},
			ResourcePaths: map[ir.ResourceKind]string{
				ir.KindSkill:    "skills/%s/SKILL.md",
				ir.KindCommand:  "commands/%s.md",
				ir.KindSubagent: "agents/%s.md",
			},
			Aggregates: []NativeAggregate{
				{
					Kind:       ir.KindHook,
					Shape:      HookShapeMatcherGroups,
					SupportDir: "hook-support",
					Project:    NativeAggregateFile{Path: "settings.json", Member: "hooks", Legacy: []string{"hooks/hooks.json"}},
					User:       NativeAggregateFile{Path: "settings.json", Member: "hooks", Legacy: []string{"hooks/hooks.json"}},
				},
				{
					Kind:    ir.KindMcpServer,
					Project: NativeAggregateFile{Path: ".mcp.json", Member: "mcpServers"},
					User:    NativeAggregateFile{Path: ".mcp.json", Member: "mcpServers"},
				},
			},
			Registrations: []NativeRegistration{{Project: "settings.local.json", User: "settings.json", Shape: NativeRegistrationClaudeSettings}},
		},
	}},
	TargetFactory: {Partitions: []NativePartition{
		{
			ID:         "default",
			Prefix:     ".",
			Components: []string{"skills", "commands", "droids", "hooks", "hook-support"},
			// The Factory layout never claimed .factory-plugin output; an
			// existing directory under the target root is left alone.
			Retired: []string{".factory-plugin/"},
			ResourcePaths: map[ir.ResourceKind]string{
				ir.KindSkill:    "skills/%s/SKILL.md",
				ir.KindCommand:  "commands/%s.md",
				ir.KindSubagent: "droids/%s.md",
				ir.KindRule:     "rules/%s.md",
			},
			Aggregates: []NativeAggregate{
				// Factory's hooks.json carries the event map at its top
				// level; the plugin-layout hooks/hooks.json older output
				// wrote remains a loadable legacy location.
				{
					Kind:       ir.KindHook,
					Shape:      HookShapeMatcherGroups,
					SupportDir: "hook-support",
					Project:    NativeAggregateFile{Path: "hooks.json", Legacy: []string{"hooks/hooks.json"}},
					User:       NativeAggregateFile{Path: "hooks.json", Legacy: []string{"hooks/hooks.json"}},
				},
				{
					Kind:    ir.KindMcpServer,
					Project: NativeAggregateFile{Path: "mcp.json", Member: "mcpServers"},
					User:    NativeAggregateFile{Path: "mcp.json", Member: "mcpServers"},
				},
			},
		},
	}},
	TargetCursor: {Partitions: []NativePartition{
		{
			ID:         "default",
			Prefix:     ".",
			Components: []string{"skills", "commands", "agents", "hooks", "hook-support"},
			ResourcePaths: map[ir.ResourceKind]string{
				ir.KindSkill:    "skills/%s/SKILL.md",
				ir.KindCommand:  "commands/%s.md",
				ir.KindSubagent: "agents/%s.md",
				ir.KindRule:     "rules/%s.mdc",
			},
			Aggregates: []NativeAggregate{
				// Cursor's hooks.json nests events under `hooks` and pins
				// `version: 1`; handlers are flat entries that may carry a
				// matcher field rather than rule groups.
				{
					Kind:       ir.KindHook,
					Shape:      HookShapeEventHandlers,
					SupportDir: "hook-support",
					Project:    NativeAggregateFile{Path: "hooks.json", Member: "hooks", Envelope: map[string]any{"version": 1}, Legacy: []string{"hooks/hooks.json"}},
					User:       NativeAggregateFile{Path: "hooks.json", Member: "hooks", Envelope: map[string]any{"version": 1}, Legacy: []string{"hooks/hooks.json"}},
				},
				{
					Kind:    ir.KindMcpServer,
					Project: NativeAggregateFile{Path: "mcp.json", Member: "mcpServers"},
					User:    NativeAggregateFile{Path: "mcp.json", Member: "mcpServers"},
				},
			},
		},
	}},
	TargetWindsurf: {Partitions: []NativePartition{
		{
			ID:         "default",
			Prefix:     ".",
			Components: []string{"skills", "workflows", "hook-support"},
			ResourcePaths: map[ir.ResourceKind]string{
				ir.KindSkill:   ".windsurf/skills/%s/SKILL.md",
				ir.KindCommand: ".windsurf/workflows/%s.md",
				ir.KindRule:    ".windsurf/rules/%s.mdc",
			},
			Aggregates: []NativeAggregate{
				// Windsurf (Devin Desktop) reads .devin/hooks.json at
				// repository scope; the older .windsurf/hooks.json is read
				// only as a legacy location. The machine-scope file is
				// hooks.json under ~/.codeium/windsurf. Entries are flat
				// per-event handler lists: the event name encodes the tool.
				{
					Kind:       ir.KindHook,
					Shape:      HookShapeEventHandlers,
					SupportDir: "hook-support",
					Project:    NativeAggregateFile{Path: ".devin/hooks.json", Member: "hooks", Legacy: []string{".windsurf/hooks.json"}, PreserveLegacyHandwritten: true},
					User:       NativeAggregateFile{Path: "hooks.json", Member: "hooks", LegacyMarkedFlatHooks: true},
				},
				{
					Kind:    ir.KindMcpServer,
					Project: NativeAggregateFile{Path: ".windsurf/mcp_config.json", Member: "mcpServers"},
					User:    NativeAggregateFile{Path: "mcp_config.json", Member: "mcpServers"},
				},
			},
		},
	}},
	TargetGemini: {Partitions: []NativePartition{
		{
			ID:         "default",
			Prefix:     ".",
			Components: []string{"skills", "commands", "hook-support"},
			ResourcePaths: map[ir.ResourceKind]string{
				ir.KindSkill:   "skills/%s/SKILL.md",
				ir.KindCommand: "commands/%s.md",
			},
			Aggregates: []NativeAggregate{
				// Hooks merge into the scope's settings.json under `hooks`.
				{
					Kind:                  ir.KindHook,
					Shape:                 HookShapeMatcherGroups,
					SupportDir:            "hook-support",
					LegacyMarkedFlatHooks: true,
					Project:               NativeAggregateFile{Path: "settings.json", Member: "hooks"},
					User:                  NativeAggregateFile{Path: "settings.json", Member: "hooks"},
				},
				{
					Kind:    ir.KindMcpServer,
					Project: NativeAggregateFile{Path: "gemini-extension.json", Member: "mcpServers"},
					User:    NativeAggregateFile{Path: "gemini-extension.json", Member: "mcpServers"},
				},
			},
		},
	}},
	TargetDevin: {Partitions: []NativePartition{
		{
			ID:     "default",
			Prefix: ".",
			// Project hooks merge into a root-level file (hooks.v1.json),
			// never a hooks directory, so hooks is not a component.
			Components: []string{"skills", "agents", "hook-support"},
			ResourcePaths: map[ir.ResourceKind]string{
				ir.KindSkill:    "skills/%s/SKILL.md",
				ir.KindCommand:  "skills/%s/SKILL.md",
				ir.KindSubagent: "agents/%s.md",
			},
			SkillDirKinds: map[ir.ResourceKind]bool{ir.KindCommand: true},
			Aggregates: []NativeAggregate{
				// A project-scope hook object lives directly in
				// hooks.v1.json; at user scope the same entries are the
				// `hooks` member of config.json, because a user-level
				// standalone hooks.v1.json is not part of the documented
				// contract.
				{
					Kind:       ir.KindHook,
					Shape:      HookShapeMatcherGroups,
					SupportDir: "hook-support",
					Project:    NativeAggregateFile{Path: "hooks.v1.json"},
					User:       NativeAggregateFile{Path: "config.json", Member: "hooks"},
				},
				{
					Kind:    ir.KindMcpServer,
					Project: NativeAggregateFile{Path: "mcp_config.json", Member: "mcpServers"},
					User:    NativeAggregateFile{Path: "mcp_config.json", Member: "mcpServers"},
				},
			},
		},
	}},
	TargetCodex: {Partitions: []NativePartition{
		{
			ID:         "agents",
			Prefix:     ".agents",
			Components: []string{"skills"},
			ResourcePaths: map[ir.ResourceKind]string{
				ir.KindSkill:   "skills/%s/SKILL.md",
				ir.KindCommand: "skills/%s/SKILL.md",
			},
			SkillDirKinds: map[ir.ResourceKind]bool{ir.KindCommand: true},
		},
		{
			ID:                   "codex",
			Prefix:               ".codex",
			MachineRootEnv:       "CODEX_HOME",
			InstallationEvidence: true,
			Components:           []string{"agents", "hook-support"},
			ResourcePaths:        map[ir.ResourceKind]string{ir.KindSubagent: "agents/%s.toml"},
			Aggregates: []NativeAggregate{
				{
					Kind:       ir.KindHook,
					Shape:      HookShapeMatcherGroups,
					SupportDir: "hook-support",
					Project:    NativeAggregateFile{Path: "hooks.json", Member: "hooks"},
					User:       NativeAggregateFile{Path: "hooks.json", Member: "hooks"},
				},
				{
					Kind:    ir.KindMcpServer,
					Project: NativeAggregateFile{Path: "config.toml", Member: "mcp_servers"},
					User:    NativeAggregateFile{Path: "config.toml", Member: "mcp_servers"},
				},
			},
			Registrations: []NativeRegistration{{Project: "config.toml", User: "config.toml", Shape: NativeRegistrationCodexConfig}},
		},
	}},
}

// IsDirShapedPattern reports whether the pattern's %s name placeholder
// occupies a directory element ("skills/%s/SKILL.md"), meaning a named
// resource materializes as a directory rather than a flat file. Dir-shaped
// means a NON-FINAL path element is exactly "%s": a placeholder carrying a
// suffix in the basename (commands/%s.md) is flat, and one carrying a suffix
// in a non-final element (units/%s.unit/manifest.json) is neither flat nor
// dir-shaped — ValidateNativeLayout rejects it. It is the single dir-shape
// predicate: ValidateNativeLayout requires it to hold for a ResourcePaths
// entry exactly when the kind is ir.KindSkill or declared in SkillDirKinds,
// and consumers read it instead of re-deriving the shape.
func IsDirShapedPattern(pattern string) bool {
	elements := strings.Split(pattern, "/")
	return slices.Contains(elements[:len(elements)-1], "%s")
}

// LookupNativeLayout returns a defensive copy of target's native layout.
func LookupNativeLayout(target Target) (NativeLayout, bool) {
	layout, ok := nativeLayouts[target]
	if !ok {
		return NativeLayout{}, false
	}
	cloned := NativeLayout{Partitions: make([]NativePartition, len(layout.Partitions))}
	for i, partition := range layout.Partitions {
		partition.Components = slices.Clone(partition.Components)
		partition.Retired = slices.Clone(partition.Retired)
		partition.ResourcePaths = maps.Clone(partition.ResourcePaths)
		partition.SkillDirKinds = maps.Clone(partition.SkillDirKinds)
		partition.Aggregates = slices.Clone(partition.Aggregates)
		partition.Registrations = slices.Clone(partition.Registrations)
		for j := range partition.Aggregates {
			partition.Aggregates[j].Project.Envelope = maps.Clone(partition.Aggregates[j].Project.Envelope)
			partition.Aggregates[j].Project.Legacy = slices.Clone(partition.Aggregates[j].Project.Legacy)
			partition.Aggregates[j].User.Envelope = maps.Clone(partition.Aggregates[j].User.Envelope)
			partition.Aggregates[j].User.Legacy = slices.Clone(partition.Aggregates[j].User.Legacy)
		}
		cloned.Partitions[i] = partition
	}
	return cloned, true
}

func NativePluginRegistration(target Target, machineScope bool) (NativeRegistrationFile, bool) {
	layout, ok := nativeLayouts[target]
	if !ok {
		return NativeRegistrationFile{}, false
	}
	var selected NativeRegistrationFile
	found := false
	for _, partition := range layout.Partitions {
		for _, registration := range partition.Registrations {
			registeredPath := registration.Project
			if machineScope {
				registeredPath = registration.User
			}
			selected = NativeRegistrationFile{
				Path:  path.Join(partition.Prefix, registeredPath),
				Shape: registration.Shape,
			}
			found = true
			break
		}
		if found {
			break
		}
	}
	return selected, found
}

// NativeResourcePattern returns the scope-neutral path pattern the target
// emits for a named resource of the kind, containing exactly one %s name
// placeholder. The pattern is relative to the scope base, so it includes the
// partition prefix of a partitioned target.
func NativeResourcePattern(target Target, kind ir.ResourceKind) (string, bool) {
	partition, pattern, ok := nativeResource(target, kind)
	if !ok {
		return "", false
	}
	return path.Join(partition.Prefix, pattern), true
}

// NativeResourcePath returns the scope-neutral path emitted for a named
// resource.
func NativeResourcePath(target Target, kind ir.ResourceKind, name string) (string, bool) {
	pattern, ok := NativeResourcePattern(target, kind)
	if !ok {
		return "", false
	}
	return fmt.Sprintf(pattern, name), true
}

// NativeResourceRoot returns the scope-neutral directory containing a
// resource kind.
func NativeResourceRoot(target Target, kind ir.ResourceKind) (string, bool) {
	pattern, ok := NativeResourcePattern(target, kind)
	if !ok {
		return "", false
	}
	prefix, _, ok := strings.Cut(pattern, "%s")
	if !ok {
		return "", false
	}
	return path.Join(strings.TrimSuffix(prefix, "/")), true
}

// NativeAggregateFileForScope returns the shared file a resource kind merges
// into in the given scope, its path resolved against the scope base. It is
// the scope-aware accessor for aggregates whose file differs between scopes,
// like Devin's hooks (hooks.v1.json at project scope, config.json#hooks at
// user scope).
func NativeAggregateFileForScope(target Target, kind ir.ResourceKind, machineScope bool) (NativeAggregateFile, bool) {
	partition, aggregate, ok := nativeAggregate(target, kind)
	if !ok {
		return NativeAggregateFile{}, false
	}
	file := aggregate.Project
	if machineScope {
		file = aggregate.User
	}
	file.Path = path.Join(partition.Prefix, file.Path)
	return file, true
}

// NativeAggregatePath returns the scope-neutral path emitted for an aggregate
// resource kind. It refuses a scope-variant aggregate, one whose file differs
// between repository and machine scope, rather than answering for a single
// scope; scope-aware callers use NativeAggregateFileForScope.
func NativeAggregatePath(target Target, kind ir.ResourceKind) (string, bool) {
	partition, aggregate, ok := nativeAggregate(target, kind)
	if !ok || !reflect.DeepEqual(aggregate.Project, aggregate.User) {
		return "", false
	}
	return path.Join(partition.Prefix, aggregate.Project.Path), true
}

func nativeResource(target Target, kind ir.ResourceKind) (NativePartition, string, bool) {
	layout, ok := nativeLayouts[target]
	if !ok {
		return NativePartition{}, "", false
	}
	for _, partition := range layout.Partitions {
		if pattern, ok := partition.ResourcePaths[kind]; ok {
			return partition, pattern, true
		}
	}
	return NativePartition{}, "", false
}

func nativeAggregate(target Target, kind ir.ResourceKind) (NativePartition, NativeAggregate, bool) {
	layout, ok := nativeLayouts[target]
	if !ok {
		return NativePartition{}, NativeAggregate{}, false
	}
	for _, partition := range layout.Partitions {
		for _, aggregate := range partition.Aggregates {
			if aggregate.Kind == kind {
				return partition, aggregate, true
			}
		}
	}
	return NativePartition{}, NativeAggregate{}, false
}

// ValidateNativeLayouts validates every registered native layout against its
// target's declared Definition. hcc.ValidateTargetRegistrations calls it so an
// invalid declaration fails at the registration boundary rather than at the
// first consumer lookup.
func ValidateNativeLayouts() error {
	var errs []error
	for _, target := range slices.Sorted(maps.Keys(nativeLayouts)) {
		if err := ValidateNativeLayout(target, nativeLayouts[target]); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ValidateNativeLayout rejects a native-layout declaration that is malformed
// or inconsistent with the target's declared Definition. A
// partitioned target names each of its roots in a partition prefix; a
// legacy-root target declares exactly one partition covering the whole root,
// because the Definition already owns the concrete root names.
func ValidateNativeLayout(t Target, layout NativeLayout) error {
	definition, ok := Lookup(string(t))
	if !ok {
		return fmt.Errorf("native layout declared for unknown target %q", t)
	}
	var errs []error
	if len(layout.Partitions) == 0 {
		errs = append(errs, fmt.Errorf("target %q declares a native layout with no partitions", t))
	}
	if !definition.PartitionedRoots && len(layout.Partitions) > 1 {
		errs = append(errs, fmt.Errorf("target %q uses the legacy single-root model but declares %d native partitions", t, len(layout.Partitions)))
	}
	seenIDs := make(map[string]bool, len(layout.Partitions))
	seenPrefixes := make(map[string]bool, len(layout.Partitions))
	// prefixes collects the valid prefixes in declaration order so nesting is
	// rejected in either order: prefixes partition one scope base, so an
	// ancestor root's cleanup would sweep a descendant root's files.
	var prefixes []string
	seenMachineEnv := make(map[string]string, len(layout.Partitions))
	// evidencePartition records the first partition declaring installation
	// evidence; a second declaration is rejected because detection treats
	// every marked root as proof the target is installed.
	evidencePartition := ""
	evidenceDeclared := false
	resourceKinds := make(map[ir.ResourceKind]string)
	aggregateKinds := make(map[ir.ResourceKind]string)
	for _, partition := range layout.Partitions {
		if partition.ID == "" {
			errs = append(errs, fmt.Errorf("target %q declares a partition with no ID", t))
		} else if seenIDs[partition.ID] {
			errs = append(errs, fmt.Errorf("target %q declares partition ID %q more than once", t, partition.ID))
		}
		seenIDs[partition.ID] = true
		if !fs.ValidPath(partition.Prefix) {
			errs = append(errs, fmt.Errorf("target %q partition %q has invalid prefix %q", t, partition.ID, partition.Prefix))
		} else {
			if seenPrefixes[partition.Prefix] {
				errs = append(errs, fmt.Errorf("target %q declares partition prefix %q more than once", t, partition.Prefix))
			}
			seenPrefixes[partition.Prefix] = true
			for _, other := range prefixes {
				if isPathPrefix(other, partition.Prefix) || isPathPrefix(partition.Prefix, other) {
					errs = append(errs, fmt.Errorf("target %q declares partition prefixes %q and %q that nest inside each other; partition roots must be element-boundary disjoint", t, other, partition.Prefix))
					break
				}
			}
			prefixes = append(prefixes, partition.Prefix)
			switch {
			case definition.PartitionedRoots && partition.Prefix == ".":
				errs = append(errs, fmt.Errorf("target %q uses the partitioned-root model; partition %q must name a sub-root, not the scope base", t, partition.ID))
			case !definition.PartitionedRoots && partition.Prefix != ".":
				errs = append(errs, fmt.Errorf("target %q uses the legacy single-root model; partition %q must cover the whole root (prefix %q), not %q", t, partition.ID, ".", partition.Prefix))
			}
		}
		if partition.MachineRootEnv != "" {
			switch {
			case !definition.PartitionedRoots:
				errs = append(errs, fmt.Errorf("target %q uses the legacy single-root model; partition %q cannot declare machine root env %q — a single-root target has no per-partition machine root to relocate", t, partition.ID, partition.MachineRootEnv))
			case !isEnvName(partition.MachineRootEnv):
				errs = append(errs, fmt.Errorf("target %q partition %q has invalid machine root env name %q", t, partition.ID, partition.MachineRootEnv))
			}
			if other, dup := seenMachineEnv[partition.MachineRootEnv]; dup {
				errs = append(errs, fmt.Errorf("target %q partitions %q and %q share machine root env %q; each partition's machine root must resolve independently", t, other, partition.ID, partition.MachineRootEnv))
			}
			seenMachineEnv[partition.MachineRootEnv] = partition.ID
		}
		if partition.InstallationEvidence {
			switch {
			case !definition.PartitionedRoots:
				errs = append(errs, fmt.Errorf("target %q uses the legacy single-root model; partition %q cannot declare installation evidence — the single root's presence is the evidence", t, partition.ID))
			case evidenceDeclared:
				errs = append(errs, fmt.Errorf("target %q partitions %q and %q both declare installation evidence; exactly one partition's presence may prove the target is installed", t, evidencePartition, partition.ID))
			default:
				evidencePartition = partition.ID
				evidenceDeclared = true
			}
		}
		errs = append(errs, validatePartitionContent(t, partition, resourceKinds, aggregateKinds)...)
		for _, registration := range partition.Registrations {
			validShape := registration.Shape == NativeRegistrationClaudeSettings && t == TargetClaude ||
				registration.Shape == NativeRegistrationCodexConfig && t == TargetCodex
			for _, scopedPath := range []struct{ scope, path string }{
				{scope: "project", path: registration.Project},
				{scope: "user", path: registration.User},
			} {
				if !fs.ValidPath(scopedPath.path) || scopedPath.path == "." || !validShape {
					errs = append(errs, fmt.Errorf("target %q partition %q declares an invalid %s plugin registration %q", t, partition.ID, scopedPath.scope, scopedPath.path))
					continue
				}
				fullPath := path.Join(partition.Prefix, scopedPath.path)
				for _, component := range partition.Components {
					if fullPath == path.Join(partition.Prefix, component) || isPathPrefix(fullPath, path.Join(partition.Prefix, component)) {
						errs = append(errs, fmt.Errorf("target %q plugin registration path %q collides with component %q", t, fullPath, component))
					}
				}
				for kind, pattern := range partition.ResourcePaths {
					resourcePath := path.Join(partition.Prefix, strings.Replace(pattern, "%s", "name", 1))
					if fullPath == resourcePath {
						errs = append(errs, fmt.Errorf("target %q plugin registration path %q collides with %s resources", t, fullPath, kind))
					}
				}
			}
		}
	}
	return errors.Join(errs...)
}

// isPathPrefix reports whether ancestor is a strict element-boundary ancestor
// of descendant ("a" of "a/b", not "a" of "ab").
func isPathPrefix(ancestor, descendant string) bool {
	return strings.HasPrefix(descendant, ancestor+"/")
}

// validatePartitionContent checks one partition's components, resource
// patterns, skill-dir kinds, and aggregates. resourceKinds and aggregateKinds
// accumulate kind ownership across partitions so a kind claimed by two
// partitions is rejected as ambiguous.
func validatePartitionContent(t Target, partition NativePartition, resourceKinds, aggregateKinds map[ir.ResourceKind]string) []error {
	var errs []error
	if len(partition.Components) == 0 && len(partition.ResourcePaths) == 0 &&
		len(partition.Aggregates) == 0 && len(partition.Registrations) == 0 {
		errs = append(errs, fmt.Errorf("target %q partition %q is empty: a declared root must populate at least one component, resource path, or aggregate", t, partition.ID))
	}
	// dirClaims collects every directory the partition declares — its
	// components and the resource directories its patterns derive — so an
	// aggregate file path that equals or contains one is rejected.
	// resourceDirs collects only the pattern-derived directories: a resource
	// directory is the per-resource namespace, so an aggregate file strictly
	// inside one is rejected too, while a file inside a mere component
	// (hooks/hooks.json) stays legal.
	dirClaims := make(map[string]bool)
	resourceDirs := make(map[string]bool)
	seenComponents := make(map[string]bool, len(partition.Components))
	for _, component := range partition.Components {
		if !fs.ValidPath(component) {
			errs = append(errs, fmt.Errorf("target %q partition %q has invalid component %q", t, partition.ID, component))
			continue
		}
		// A component must be a single path element: materialization routes
		// artifacts by first path element only, so a multi-element component
		// is unpopulatable — it degrades to a pure prune plan — and "." would
		// register the whole partition root as a prunable directory.
		if component == "." || strings.Contains(component, "/") {
			errs = append(errs, fmt.Errorf("target %q partition %q declares component %q; a component must be a single path element", t, partition.ID, component))
			continue
		}
		if seenComponents[component] {
			errs = append(errs, fmt.Errorf("target %q partition %q declares component %q more than once", t, partition.ID, component))
		}
		seenComponents[component] = true
		dirClaims[component] = true
	}
	// retiredClaims tracks retired entries so a duplicate or an entry that
	// nests inside another retired location is rejected, and so an aggregate
	// or resource path that collides with a retired location is caught below.
	retiredClaims := make(map[string]bool, len(partition.Retired))
	for _, retired := range partition.Retired {
		cleaned := strings.TrimSuffix(retired, "/")
		switch {
		case !fs.ValidPath(cleaned) || cleaned == ".":
			errs = append(errs, fmt.Errorf("target %q partition %q has invalid retired path %q", t, partition.ID, retired))
			continue
		case retiredClaims[retired]:
			errs = append(errs, fmt.Errorf("target %q partition %q declares retired path %q more than once", t, partition.ID, retired))
		}
		retiredClaims[retired] = true
		if dirClaims[cleaned] {
			errs = append(errs, fmt.Errorf("target %q partition %q retires %q which is still a declared component", t, partition.ID, retired))
		}
		for other := range retiredClaims {
			if other == retired {
				continue
			}
			otherClean := strings.TrimSuffix(other, "/")
			if isPathPrefix(otherClean, cleaned) || isPathPrefix(cleaned, otherClean) {
				errs = append(errs, fmt.Errorf("target %q partition %q retired paths %q and %q nest inside each other", t, partition.ID, other, retired))
			}
		}
	}
	for _, kind := range slices.Sorted(maps.Keys(partition.ResourcePaths)) {
		pattern := partition.ResourcePaths[kind]
		if !compilableKind(kind) {
			errs = append(errs, fmt.Errorf("target %q partition %q declares a resource path for non-compilable kind %s", t, partition.ID, kind))
		}
		if other, dup := resourceKinds[kind]; dup {
			errs = append(errs, fmt.Errorf("target %q maps kind %s in partitions %q and %q", t, kind, other, partition.ID))
		}
		resourceKinds[kind] = partition.ID
		if strings.Count(pattern, "%s") != 1 {
			errs = append(errs, fmt.Errorf("target %q partition %q resource path for %s must contain exactly one %%s name placeholder: %q", t, partition.ID, kind, pattern))
			continue
		}
		if strings.Contains(strings.Replace(pattern, "%s", "", 1), "%") {
			errs = append(errs, fmt.Errorf("target %q partition %q resource path for %s contains a format verb other than %%s: %q", t, partition.ID, kind, pattern))
		}
		if !fs.ValidPath(strings.Replace(pattern, "%s", "name", 1)) {
			errs = append(errs, fmt.Errorf("target %q partition %q resource path for %s is not a valid relative path: %q", t, partition.ID, kind, pattern))
		}
		// The placeholder must begin a path element: a prefix not ending at
		// a "/" boundary ("%s.md", "a%sb.md") derives a resource root of "."
		// or a partial element, and consumers join that onto the install
		// root as a cleanup directory.
		prefix, suffix, _ := strings.Cut(pattern, "%s")
		if strings.HasSuffix(prefix, "/") {
			dirClaims[strings.TrimSuffix(prefix, "/")] = true
			resourceDirs[strings.TrimSuffix(prefix, "/")] = true
		} else {
			errs = append(errs, fmt.Errorf("target %q partition %q resource path for %s must start the %%s name placeholder at a path element boundary: %q", t, partition.ID, kind, pattern))
		}
		// When the placeholder's element is not the basename it must be
		// exactly "%s": a suffixed non-final element
		// (units/%s.unit/manifest.json) is neither a flat file nor one
		// resource directory per resource — its literal text would leak
		// into derived resource directories while the real per-resource
		// directories go unmodeled.
		if i := strings.Index(suffix, "/"); i > 0 {
			errs = append(errs, fmt.Errorf("target %q partition %q resource path for %s carries a %%s element with a suffix in non-final position: %q; a non-final placeholder element must be exactly %%s", t, partition.ID, kind, pattern))
		}
		// The pattern's declared shape and the kind's declared materialization
		// must agree in both directions: a directory-shaped pattern for a kind
		// that is neither KindSkill nor in SkillDirKinds derives a resource
		// directory no consumer registers, and a skill-directory kind with a
		// flat pattern would materialize through the wrong shape.
		skillDir := kind == ir.KindSkill || partition.SkillDirKinds[kind]
		switch {
		case IsDirShapedPattern(pattern) && !skillDir:
			errs = append(errs, fmt.Errorf("target %q partition %q resource path for %s places the %%s name in a directory element: %q; a directory-shaped pattern requires the kind to be a skill or be declared in SkillDirKinds", t, partition.ID, kind, pattern))
		case !IsDirShapedPattern(pattern) && skillDir:
			errs = append(errs, fmt.Errorf("target %q partition %q resource path for %s does not place the %%s name in a directory element: %q; a kind materialized as a resource directory requires a directory-shaped pattern", t, partition.ID, kind, pattern))
		}
	}
	for _, kind := range slices.Sorted(maps.Keys(partition.SkillDirKinds)) {
		if !compilableKind(kind) {
			errs = append(errs, fmt.Errorf("target %q partition %q declares non-compilable kind %s a skill directory", t, partition.ID, kind))
		}
		if kind == ir.KindSkill {
			errs = append(errs, fmt.Errorf("target %q partition %q lists kind %s in SkillDirKinds; %s is always a resource directory and the field covers kinds besides it", t, partition.ID, kind, kind))
		}
		if !partition.SkillDirKinds[kind] {
			errs = append(errs, fmt.Errorf("target %q partition %q lists kind %s in SkillDirKinds with a false value; only true entries are meaningful", t, partition.ID, kind))
		}
		if _, ok := partition.ResourcePaths[kind]; !ok {
			errs = append(errs, fmt.Errorf("target %q partition %q declares kind %s a skill directory without a resource path", t, partition.ID, kind))
		}
	}
	// fileClaims records each scope's (path, member) tuples: two kinds
	// claiming the same tuple would share one entry keyspace, while a strict
	// ancestor relation between two claimed paths would mean a file claim
	// contains another file claim. The same path under different members is
	// a legal shared envelope.
	fileClaims := map[string]map[string]ir.ResourceKind{"project": {}, "user": {}}
	claimPaths := map[string][]string{"project": {}, "user": {}}
	for _, aggregate := range partition.Aggregates {
		if !compilableKind(aggregate.Kind) {
			errs = append(errs, fmt.Errorf("target %q partition %q declares an aggregate for non-compilable kind %s", t, partition.ID, aggregate.Kind))
		}
		if other, dup := aggregateKinds[aggregate.Kind]; dup {
			errs = append(errs, fmt.Errorf("target %q declares aggregates for kind %s in partitions %q and %q", t, aggregate.Kind, other, partition.ID))
		}
		aggregateKinds[aggregate.Kind] = partition.ID
		// Shape and SupportDir describe hook entries: required exactly for
		// KindHook so a hook aggregate can never silently fall back to a
		// guessed encoding, and forbidden elsewhere so a stale declaration
		// cannot masquerade as meaningful topology.
		switch {
		case aggregate.Kind == ir.KindHook && aggregate.Shape == "":
			errs = append(errs, fmt.Errorf("target %q partition %q hook aggregate declares no shape; the hook entry encoding is load-bearing", t, partition.ID))
		case aggregate.Kind == ir.KindHook && aggregate.Shape != HookShapeMatcherGroups && aggregate.Shape != HookShapeEventHandlers:
			errs = append(errs, fmt.Errorf("target %q partition %q hook aggregate declares unknown shape %q", t, partition.ID, aggregate.Shape))
		case aggregate.Kind != ir.KindHook && aggregate.Shape != "":
			errs = append(errs, fmt.Errorf("target %q partition %q aggregate for %s declares shape %q; shape describes hook entries only", t, partition.ID, aggregate.Kind, aggregate.Shape))
		}
		switch {
		case aggregate.Kind == ir.KindHook && aggregate.SupportDir == "":
			errs = append(errs, fmt.Errorf("target %q partition %q hook aggregate declares no support directory root", t, partition.ID))
		case aggregate.Kind == ir.KindHook && (!fs.ValidPath(aggregate.SupportDir) || aggregate.SupportDir == "."):
			errs = append(errs, fmt.Errorf("target %q partition %q hook aggregate has invalid support directory %q", t, partition.ID, aggregate.SupportDir))
		case aggregate.Kind != ir.KindHook && aggregate.SupportDir != "":
			errs = append(errs, fmt.Errorf("target %q partition %q aggregate for %s declares support directory %q; support directories belong to hooks", t, partition.ID, aggregate.Kind, aggregate.SupportDir))
		}
		if aggregate.LegacyMarkedFlatHooks && (aggregate.Kind != ir.KindHook || (aggregate.Shape != HookShapeMatcherGroups && aggregate.Shape != HookShapeEventHandlers)) {
			errs = append(errs, fmt.Errorf("target %q partition %q aggregate for %s declares legacy marked flat hooks; the transition applies only to hook matcher-groups or event-handlers aggregates", t, partition.ID, aggregate.Kind))
		}
		for _, scope := range []struct {
			name string
			file NativeAggregateFile
		}{{"project", aggregate.Project}, {"user", aggregate.User}} {
			if scope.file.LegacyMarkedFlatHooks && (aggregate.Kind != ir.KindHook || (aggregate.Shape != HookShapeMatcherGroups && aggregate.Shape != HookShapeEventHandlers)) {
				errs = append(errs, fmt.Errorf("target %q partition %q %s-scope aggregate for %s declares legacy marked flat hooks on a file; the transition applies only to hook matcher-groups or event-handlers files", t, partition.ID, scope.name, aggregate.Kind))
			}
			if scope.file.PreserveLegacyHandwritten && (aggregate.Kind != ir.KindHook || aggregate.Shape != HookShapeEventHandlers || len(scope.file.Legacy) == 0) {
				errs = append(errs, fmt.Errorf("target %q partition %q %s-scope aggregate for %s preserves legacy handwritten entries; it requires a hook event-handlers aggregate with legacy paths", t, partition.ID, scope.name, aggregate.Kind))
			}
		}
		for _, scope := range []struct {
			name string
			file NativeAggregateFile
		}{{"project", aggregate.Project}, {"user", aggregate.User}} {
			if !fs.ValidPath(scope.file.Path) {
				errs = append(errs, fmt.Errorf("target %q partition %q %s-scope aggregate for %s has invalid path %q", t, partition.ID, scope.name, aggregate.Kind, scope.file.Path))
			} else if scope.file.Path == "." {
				errs = append(errs, fmt.Errorf("target %q partition %q %s-scope aggregate for %s has path %q naming the partition root, not a file", t, partition.ID, scope.name, aggregate.Kind, scope.file.Path))
			} else {
				for _, dir := range slices.Sorted(maps.Keys(dirClaims)) {
					if dir == scope.file.Path || isPathPrefix(scope.file.Path, dir) {
						errs = append(errs, fmt.Errorf("target %q partition %q %s-scope aggregate for %s has path %q that is or contains the declared directory %q", t, partition.ID, scope.name, aggregate.Kind, scope.file.Path, dir))
						break
					}
				}
				for retired := range retiredClaims {
					cleaned := strings.TrimSuffix(retired, "/")
					if scope.file.Path == cleaned || isPathPrefix(cleaned, scope.file.Path) || isPathPrefix(scope.file.Path, cleaned) {
						errs = append(errs, fmt.Errorf("target %q partition %q %s-scope aggregate for %s has path %q that overlaps retired location %q", t, partition.ID, scope.name, aggregate.Kind, scope.file.Path, retired))
						break
					}
				}
				for _, dir := range slices.Sorted(maps.Keys(resourceDirs)) {
					if isPathPrefix(dir, scope.file.Path) {
						errs = append(errs, fmt.Errorf("target %q partition %q %s-scope aggregate for %s has path %q inside resource directory %q; an aggregate file cannot occupy the per-resource namespace a ResourcePaths pattern derives", t, partition.ID, scope.name, aggregate.Kind, scope.file.Path, dir))
						break
					}
				}
				for _, claimed := range claimPaths[scope.name] {
					if isPathPrefix(claimed, scope.file.Path) || isPathPrefix(scope.file.Path, claimed) {
						errs = append(errs, fmt.Errorf("target %q partition %q %s-scope aggregate paths %q and %q are in an ancestor relation; a file claim cannot contain another file claim", t, partition.ID, scope.name, claimed, scope.file.Path))
						break
					}
				}
				tuple := scope.file.Path + "\x00" + scope.file.Member
				if other, dup := fileClaims[scope.name][tuple]; dup {
					errs = append(errs, fmt.Errorf("target %q partition %q %s-scope aggregate for %s claims the same file member (path %q, member %q) as kind %s; two kinds would share one entry keyspace", t, partition.ID, scope.name, aggregate.Kind, scope.file.Path, scope.file.Member, other))
				}
				fileClaims[scope.name][tuple] = aggregate.Kind
				claimPaths[scope.name] = append(claimPaths[scope.name], scope.file.Path)
			}
			if member := scope.file.Member; member != "" && (strings.ContainsAny(member, `/\`) || member == "." || member == "..") {
				errs = append(errs, fmt.Errorf("target %q partition %q %s-scope aggregate for %s has invalid member %q", t, partition.ID, scope.name, aggregate.Kind, member))
			}
			for _, key := range slices.Sorted(maps.Keys(scope.file.Envelope)) {
				if key == "" {
					errs = append(errs, fmt.Errorf("target %q partition %q %s-scope aggregate for %s declares an envelope member with an empty name", t, partition.ID, scope.name, aggregate.Kind))
					continue
				}
				if key == scope.file.Member {
					errs = append(errs, fmt.Errorf("target %q partition %q %s-scope aggregate for %s declares envelope member %q colliding with the entry member", t, partition.ID, scope.name, aggregate.Kind, key))
					continue
				}
				if _, err := json.Marshal(scope.file.Envelope[key]); err != nil {
					errs = append(errs, fmt.Errorf("target %q partition %q %s-scope aggregate for %s declares envelope member %q with unserializable value: %w", t, partition.ID, scope.name, aggregate.Kind, key, err))
				}
			}
			for _, legacy := range scope.file.Legacy {
				switch {
				case !fs.ValidPath(legacy) || legacy == ".":
					errs = append(errs, fmt.Errorf("target %q partition %q %s-scope aggregate for %s has invalid legacy location %q", t, partition.ID, scope.name, aggregate.Kind, legacy))
				case legacy == scope.file.Path:
					errs = append(errs, fmt.Errorf("target %q partition %q %s-scope aggregate for %s lists legacy location %q identical to its native file", t, partition.ID, scope.name, aggregate.Kind, legacy))
				}
			}
		}
	}
	return errs
}

// compilableKind reports whether kind can produce artifacts. KindCollection
// is a virtual grouping, so a layout entry for it can never be exercised.
func compilableKind(kind ir.ResourceKind) bool {
	return kind >= ir.KindSkill && kind <= ir.KindRule && kind != ir.KindCollection
}

// isEnvName reports whether name is a syntactically valid environment
// variable name: [A-Za-z_][A-Za-z0-9_]*.
func isEnvName(name string) bool {
	for i, r := range name {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r == '_':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return name != ""
}
