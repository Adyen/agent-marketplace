package target

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
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
	// Definition already owns.
	Prefix string
	// MachineRootEnv names the environment variable that relocates this
	// partition's machine-scope root (CODEX_HOME). Resolving it is consumer
	// policy; the descriptor only declares the name.
	MachineRootEnv string
	// InstallationEvidence marks the partition whose presence proves the
	// target is installed on the machine.
	InstallationEvidence bool
	// Components are the root-relative directories the layout writes into.
	Components []string
	// ResourcePaths maps a resource kind to the root-relative path pattern
	// emitted for a named resource. Every pattern contains exactly one %s name
	// placeholder; when the placeholder is itself a directory element
	// (skills/%s/SKILL.md) the resource materializes as a directory.
	ResourcePaths map[ir.ResourceKind]string
	// SkillDirKinds lists the kinds besides KindSkill that materialize as a
	// resource directory rather than a flat file. Every listed kind must
	// declare a ResourcePaths pattern whose %s is a directory element.
	SkillDirKinds map[ir.ResourceKind]bool
	// Aggregates are the shared target files resource kinds contribute
	// entries to.
	Aggregates []NativeAggregate
}

// NativeAggregateFile locates the shared file that carries one resource
// kind's compiled entries within a single scope.
type NativeAggregateFile struct {
	// Path is the file path relative to the partition root.
	Path string
	// Member names the top-level member that carries the kind's entries when
	// the file is a shared envelope rather than a file dedicated to the kind.
	// Devin's user-scope hooks live under the `hooks` member of config.json.
	// An empty Member means the file's top level carries the entries directly.
	Member string
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
}

var nativeLayouts = map[Target]NativeLayout{
	TargetClaude: {Partitions: []NativePartition{
		{
			ID:         "default",
			Prefix:     ".",
			Components: []string{"skills", "commands", "agents", "hooks", ".claude-plugin"},
			ResourcePaths: map[ir.ResourceKind]string{
				ir.KindSkill:    "skills/%s/SKILL.md",
				ir.KindCommand:  "commands/%s.md",
				ir.KindSubagent: "agents/%s.md",
			},
			Aggregates: []NativeAggregate{
				{
					Kind:    ir.KindSkill,
					Project: NativeAggregateFile{Path: ".claude-plugin/plugin.json", Member: "skills"},
					User:    NativeAggregateFile{Path: ".claude-plugin/plugin.json", Member: "skills"},
				},
				{
					Kind:    ir.KindSubagent,
					Project: NativeAggregateFile{Path: ".claude-plugin/plugin.json", Member: "agents"},
					User:    NativeAggregateFile{Path: ".claude-plugin/plugin.json", Member: "agents"},
				},
				{
					Kind:    ir.KindHook,
					Project: NativeAggregateFile{Path: "hooks/hooks.json", Member: "hooks"},
					User:    NativeAggregateFile{Path: "hooks/hooks.json", Member: "hooks"},
				},
				{
					Kind:    ir.KindMcpServer,
					Project: NativeAggregateFile{Path: ".mcp.json", Member: "mcpServers"},
					User:    NativeAggregateFile{Path: ".mcp.json", Member: "mcpServers"},
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
			Components: []string{"skills", "agents"},
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
					Kind:    ir.KindHook,
					Project: NativeAggregateFile{Path: "hooks.v1.json"},
					User:    NativeAggregateFile{Path: "config.json", Member: "hooks"},
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
			Components:           []string{"agents"},
			ResourcePaths:        map[ir.ResourceKind]string{ir.KindSubagent: "agents/%s.toml"},
			Aggregates: []NativeAggregate{
				{
					Kind:    ir.KindHook,
					Project: NativeAggregateFile{Path: "hooks.json", Member: "hooks"},
					User:    NativeAggregateFile{Path: "hooks.json", Member: "hooks"},
				},
				{
					Kind:    ir.KindMcpServer,
					Project: NativeAggregateFile{Path: "config.toml", Member: "mcp_servers"},
					User:    NativeAggregateFile{Path: "config.toml", Member: "mcp_servers"},
				},
			},
		},
	}},
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
		partition.ResourcePaths = maps.Clone(partition.ResourcePaths)
		partition.SkillDirKinds = maps.Clone(partition.SkillDirKinds)
		partition.Aggregates = slices.Clone(partition.Aggregates)
		cloned.Partitions[i] = partition
	}
	return cloned, true
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
	if !ok || aggregate.Project != aggregate.User {
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
			switch {
			case definition.PartitionedRoots && partition.Prefix == ".":
				errs = append(errs, fmt.Errorf("target %q uses the partitioned-root model; partition %q must name a sub-root, not the scope base", t, partition.ID))
			case !definition.PartitionedRoots && partition.Prefix != ".":
				errs = append(errs, fmt.Errorf("target %q uses the legacy single-root model; partition %q must cover the whole root (prefix %q), not %q", t, partition.ID, ".", partition.Prefix))
			}
		}
		if partition.MachineRootEnv != "" && !isEnvName(partition.MachineRootEnv) {
			errs = append(errs, fmt.Errorf("target %q partition %q has invalid machine root env name %q", t, partition.ID, partition.MachineRootEnv))
		}
		errs = append(errs, validatePartitionContent(t, partition, resourceKinds, aggregateKinds)...)
	}
	return errors.Join(errs...)
}

// validatePartitionContent checks one partition's components, resource
// patterns, skill-dir kinds, and aggregates. resourceKinds and aggregateKinds
// accumulate kind ownership across partitions so a kind claimed by two
// partitions is rejected as ambiguous.
func validatePartitionContent(t Target, partition NativePartition, resourceKinds, aggregateKinds map[ir.ResourceKind]string) []error {
	var errs []error
	seenComponents := make(map[string]bool, len(partition.Components))
	for _, component := range partition.Components {
		if !fs.ValidPath(component) {
			errs = append(errs, fmt.Errorf("target %q partition %q has invalid component %q", t, partition.ID, component))
		} else if seenComponents[component] {
			errs = append(errs, fmt.Errorf("target %q partition %q declares component %q more than once", t, partition.ID, component))
		}
		seenComponents[component] = true
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
	}
	for _, kind := range slices.Sorted(maps.Keys(partition.SkillDirKinds)) {
		if !compilableKind(kind) {
			errs = append(errs, fmt.Errorf("target %q partition %q declares non-compilable kind %s a skill directory", t, partition.ID, kind))
		}
		if !partition.SkillDirKinds[kind] {
			errs = append(errs, fmt.Errorf("target %q partition %q lists kind %s in SkillDirKinds with a false value; only true entries are meaningful", t, partition.ID, kind))
		}
		pattern, ok := partition.ResourcePaths[kind]
		if !ok {
			errs = append(errs, fmt.Errorf("target %q partition %q declares kind %s a skill directory without a resource path", t, partition.ID, kind))
			continue
		}
		if !strings.Contains(pattern, "%s/") {
			errs = append(errs, fmt.Errorf("target %q partition %q declares kind %s a skill directory but its pattern %q does not place the name in a directory", t, partition.ID, kind, pattern))
		}
	}
	for _, aggregate := range partition.Aggregates {
		if !compilableKind(aggregate.Kind) {
			errs = append(errs, fmt.Errorf("target %q partition %q declares an aggregate for non-compilable kind %s", t, partition.ID, aggregate.Kind))
		}
		if other, dup := aggregateKinds[aggregate.Kind]; dup {
			errs = append(errs, fmt.Errorf("target %q declares aggregates for kind %s in partitions %q and %q", t, aggregate.Kind, other, partition.ID))
		}
		aggregateKinds[aggregate.Kind] = partition.ID
		for _, scope := range []struct {
			name string
			file NativeAggregateFile
		}{{"project", aggregate.Project}, {"user", aggregate.User}} {
			if !fs.ValidPath(scope.file.Path) {
				errs = append(errs, fmt.Errorf("target %q partition %q %s-scope aggregate for %s has invalid path %q", t, partition.ID, scope.name, aggregate.Kind, scope.file.Path))
			}
			if member := scope.file.Member; member != "" && (strings.ContainsAny(member, `/\`) || member == "." || member == "..") {
				errs = append(errs, fmt.Errorf("target %q partition %q %s-scope aggregate for %s has invalid member %q", t, partition.ID, scope.name, aggregate.Kind, member))
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
