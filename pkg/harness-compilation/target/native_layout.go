package target

import (
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// NativeLayout describes the target-owned filesystem shape emitted by a
// compiler. Consumers resolve its partitions into concrete roots.
type NativeLayout struct {
	Partitions []NativePartition
}

// NativePartition describes one independently rooted part of a native layout.
type NativePartition struct {
	ID                   string
	Prefix               string
	MachineRootEnv       string
	InstallationEvidence bool
	Components           []string
	ResourcePaths        map[ir.ResourceKind]string
	SkillDirKinds        map[ir.ResourceKind]bool
	Aggregates           []NativeAggregate
}

// NativeAggregate identifies a shared target file produced for one resource
// kind.
type NativeAggregate struct {
	Kind ir.ResourceKind
	Path string
}

var nativeLayouts = map[Target]NativeLayout{
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
				{Kind: ir.KindHook, Path: "hooks.json"},
				{Kind: ir.KindMcpServer, Path: "config.toml"},
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

// NativeResourcePath returns the scope-neutral path emitted for a named
// resource.
func NativeResourcePath(target Target, kind ir.ResourceKind, name string) (string, bool) {
	partition, pattern, ok := nativeResource(target, kind)
	if !ok || !strings.Contains(pattern, "%s") {
		return "", false
	}
	return path.Join(partition.Prefix, fmt.Sprintf(pattern, name)), true
}

// NativeResourceRoot returns the scope-neutral directory containing a
// resource kind.
func NativeResourceRoot(target Target, kind ir.ResourceKind) (string, bool) {
	partition, pattern, ok := nativeResource(target, kind)
	if !ok {
		return "", false
	}
	prefix, _, ok := strings.Cut(pattern, "%s")
	if !ok {
		return "", false
	}
	return path.Join(partition.Prefix, strings.TrimSuffix(prefix, "/")), true
}

// NativeAggregatePath returns the scope-neutral path emitted for an aggregate
// resource kind.
func NativeAggregatePath(target Target, kind ir.ResourceKind) (string, bool) {
	layout, ok := nativeLayouts[target]
	if !ok {
		return "", false
	}
	for _, partition := range layout.Partitions {
		for _, aggregate := range partition.Aggregates {
			if aggregate.Kind == kind {
				return path.Join(partition.Prefix, aggregate.Path), true
			}
		}
	}
	return "", false
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
