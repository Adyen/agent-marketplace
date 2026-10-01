package target

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// TestDevinNativeLayoutIsAvailable pins the Devin descriptor, including the
// scope-variant hook aggregate: project scope emits hooks.v1.json, user scope
// emits the `hooks` member of config.json.
func TestDevinNativeLayoutIsAvailable(t *testing.T) {
	want := NativeLayout{Partitions: []NativePartition{
		{
			ID:         "default",
			Prefix:     ".",
			Components: []string{"skills", "agents"},
			ResourcePaths: map[ir.ResourceKind]string{
				ir.KindSkill:    "skills/%s/SKILL.md",
				ir.KindCommand:  "skills/%s/SKILL.md",
				ir.KindSubagent: "agents/%s.md",
			},
			SkillDirKinds: map[ir.ResourceKind]bool{ir.KindCommand: true},
			Aggregates: []NativeAggregate{
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
	}}

	got, ok := LookupNativeLayout(TargetDevin)
	if !ok {
		t.Fatal("Devin native layout is not registered")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LookupNativeLayout(devin) = %#v, want %#v", got, want)
	}

	for _, tt := range []struct {
		kind         ir.ResourceKind
		name         string
		machineScope bool
		want         string
		wantMember   string
	}{
		{kind: ir.KindSkill, name: "review", want: "skills/review/SKILL.md"},
		{kind: ir.KindCommand, name: "release", want: "skills/release/SKILL.md"},
		{kind: ir.KindSubagent, name: "planner", want: "agents/planner.md"},
	} {
		if got, ok := NativeResourcePath(TargetDevin, tt.kind, tt.name); !ok || got != tt.want {
			t.Errorf("NativeResourcePath(devin, %s, %q) = %q, %v; want %q, true", tt.kind, tt.name, got, ok, tt.want)
		}
	}
	if got, ok := NativeAggregateFileForScope(TargetDevin, ir.KindHook, false); !ok || got.Path != "hooks.v1.json" || got.Member != "" {
		t.Errorf("NativeAggregateFileForScope(devin, hook, project) = %#v, %v; want hooks.v1.json with no member", got, ok)
	}
	if got, ok := NativeAggregateFileForScope(TargetDevin, ir.KindHook, true); !ok || got.Path != "config.json" || got.Member != "hooks" {
		t.Errorf("NativeAggregateFileForScope(devin, hook, user) = %#v, %v; want config.json member hooks", got, ok)
	}
	if got, ok := NativeAggregatePath(TargetDevin, ir.KindMcpServer); !ok || got != "mcp_config.json" {
		t.Errorf("NativeAggregatePath(devin, mcpserver) = %q, %v; want mcp_config.json, true", got, ok)
	}
}

// TestNativeAggregatePathRefusesScopeVariant pins that the scope-neutral
// aggregate accessor cannot silently answer for one scope when a target's
// aggregate file differs between them.
func TestNativeAggregatePathRefusesScopeVariant(t *testing.T) {
	if got, ok := NativeAggregatePath(TargetDevin, ir.KindHook); ok || got != "" {
		t.Errorf("NativeAggregatePath(devin, hook) = %q, %v; want refusal for a scope-variant aggregate", got, ok)
	}
	// Scope-invariant aggregates still answer.
	if got, ok := NativeAggregatePath(TargetCodex, ir.KindHook); !ok || got != ".codex/hooks.json" {
		t.Errorf("NativeAggregatePath(codex, hook) = %q, %v; want .codex/hooks.json, true", got, ok)
	}
	if got, ok := NativeAggregatePath(TargetDevin, ir.KindMcpServer); !ok || got != "mcp_config.json" {
		t.Errorf("NativeAggregatePath(devin, mcpserver) = %q, %v; want mcp_config.json, true", got, ok)
	}
	if got, ok := NativeAggregatePath(TargetFactory, ir.KindHook); !ok || got != "hooks.json" {
		t.Errorf("NativeAggregatePath(factory, hook) = %q, %v; want hooks.json, true", got, ok)
	}
}

// TestClaudeNativeLayoutIsAvailable pins the Claude descriptor: one
// whole-root partition whose aggregates include the plugin manifest shared by
// skills and subagents.
func TestClaudeNativeLayoutIsAvailable(t *testing.T) {
	want := NativeLayout{Partitions: []NativePartition{
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
		},
	}}

	got, ok := LookupNativeLayout(TargetClaude)
	if !ok {
		t.Fatal("Claude native layout is not registered")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LookupNativeLayout(claude) = %#v, want %#v", got, want)
	}
	if got, ok := NativeResourcePath(TargetClaude, ir.KindCommand, "ship"); !ok || got != "commands/ship.md" {
		t.Errorf("NativeResourcePath(claude, command, ship) = %q, %v; want commands/ship.md, true", got, ok)
	}
	if got, ok := NativeAggregatePath(TargetClaude, ir.KindSubagent); !ok || got != ".claude-plugin/plugin.json" {
		t.Errorf("NativeAggregatePath(claude, subagent) = %q, %v; want .claude-plugin/plugin.json, true", got, ok)
	}
}

// TestRegisteredNativeLayoutsValidate proves the registry's own declarations
// pass validation, the same check hcc.ValidateTargetRegistrations performs,
// and that every partitioned-root definition has the layout its roots resolve
// through.
func TestRegisteredNativeLayoutsValidate(t *testing.T) {
	if err := ValidateNativeLayouts(); err != nil {
		t.Fatalf("ValidateNativeLayouts() = %v", err)
	}
	for _, definition := range Definitions() {
		layout, ok := LookupNativeLayout(definition.Target)
		if definition.PartitionedRoots && !ok {
			t.Errorf("partitioned target %q has no native layout", definition.Target)
		}
		if ok {
			if err := ValidateNativeLayout(definition.Target, layout); err != nil {
				t.Errorf("ValidateNativeLayout(%s) = %v", definition.Target, err)
			}
		}
	}
}

// TestValidateNativeLayoutRejectsInvalidDeclarations exercises the fail-closed
// checks the registration boundary relies on. The valid control cases prove
// the exercised path had something to accept.
func TestValidateNativeLayoutRejectsInvalidDeclarations(t *testing.T) {
	legacyPartition := func() NativePartition {
		return NativePartition{
			ID:     "default",
			Prefix: ".",
			ResourcePaths: map[ir.ResourceKind]string{
				ir.KindSkill: "skills/%s/SKILL.md",
			},
		}
	}

	tests := []struct {
		name    string
		target  Target
		layout  NativeLayout
		wantErr bool
		// wantErrContains, when set, must appear in the validation error so a
		// case pinned to one check cannot pass on an unrelated rejection.
		wantErrContains string
	}{
		{
			name:   "valid legacy layout",
			target: TargetClaude,
			layout: NativeLayout{Partitions: []NativePartition{legacyPartition()}},
		},
		{
			name:   "valid partitioned layout",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:     "root",
				Prefix: ".root",
				ResourcePaths: map[ir.ResourceKind]string{
					ir.KindSkill: "skills/%s/SKILL.md",
				},
			}}},
		},
		{
			name:    "unknown target",
			target:  Target("nonexistent"),
			layout:  NativeLayout{Partitions: []NativePartition{legacyPartition()}},
			wantErr: true,
		},
		{
			name:    "no partitions",
			target:  TargetClaude,
			layout:  NativeLayout{},
			wantErr: true,
		},
		{
			name:   "legacy target with two partitions",
			target: TargetClaude,
			layout: NativeLayout{Partitions: []NativePartition{
				{ID: "a", Prefix: "."},
				{ID: "b", Prefix: "."},
			}},
			wantErr: true,
		},
		{
			name:    "legacy partition with named prefix",
			target:  TargetClaude,
			layout:  NativeLayout{Partitions: []NativePartition{{ID: "a", Prefix: ".claude"}}},
			wantErr: true,
		},
		{
			name:    "partitioned partition covering scope base",
			target:  TargetCodex,
			layout:  NativeLayout{Partitions: []NativePartition{{ID: "a", Prefix: "."}}},
			wantErr: true,
		},
		{
			name:   "duplicate partition IDs",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{
				{ID: "dup", Prefix: ".a"},
				{ID: "dup", Prefix: ".b"},
			}},
			wantErr: true,
		},
		{
			name:   "duplicate partition prefixes",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{
				{ID: "a", Prefix: ".same"},
				{ID: "b", Prefix: ".same"},
			}},
			wantErr: true,
		},
		{
			name:    "empty partition ID",
			target:  TargetCodex,
			layout:  NativeLayout{Partitions: []NativePartition{{Prefix: ".a"}}},
			wantErr: true,
		},
		{
			name:    "absolute prefix",
			target:  TargetCodex,
			layout:  NativeLayout{Partitions: []NativePartition{{ID: "a", Prefix: "/abs"}}},
			wantErr: true,
		},
		{
			name:    "escaping prefix",
			target:  TargetCodex,
			layout:  NativeLayout{Partitions: []NativePartition{{ID: "a", Prefix: "../escape"}}},
			wantErr: true,
		},
		{
			name:    "empty prefix",
			target:  TargetCodex,
			layout:  NativeLayout{Partitions: []NativePartition{{ID: "a", Prefix: ""}}},
			wantErr: true,
		},
		{
			name:    "dotdot element prefix",
			target:  TargetCodex,
			layout:  NativeLayout{Partitions: []NativePartition{{ID: "a", Prefix: "x/../y"}}},
			wantErr: true,
		},
		{
			name:    "invalid machine root env name",
			target:  TargetCodex,
			layout:  NativeLayout{Partitions: []NativePartition{{ID: "a", Prefix: ".a", MachineRootEnv: "1BAD"}}},
			wantErr: true,
		},
		{
			name:    "machine root env with punctuation",
			target:  TargetCodex,
			layout:  NativeLayout{Partitions: []NativePartition{{ID: "a", Prefix: ".a", MachineRootEnv: "CODEX-HOME"}}},
			wantErr: true,
		},
		{
			name:   "valid machine root env",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID: "a", Prefix: ".a", MachineRootEnv: "CODEX_HOME",
				Components: []string{"agents"},
			}}},
		},
		{
			name:    "invalid component",
			target:  TargetCodex,
			layout:  NativeLayout{Partitions: []NativePartition{{ID: "a", Prefix: ".a", Components: []string{"../escape"}}}},
			wantErr: true,
		},
		{
			name:    "duplicate component",
			target:  TargetCodex,
			layout:  NativeLayout{Partitions: []NativePartition{{ID: "a", Prefix: ".a", Components: []string{"skills", "skills"}}}},
			wantErr: true,
		},
		{
			name:   "resource path without placeholder",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:            "a",
				Prefix:        ".a",
				ResourcePaths: map[ir.ResourceKind]string{ir.KindSkill: "skills/SKILL.md"},
			}}},
			wantErr: true,
		},
		{
			name:   "resource path with two placeholders",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:            "a",
				Prefix:        ".a",
				ResourcePaths: map[ir.ResourceKind]string{ir.KindSkill: "skills/%s/%s/SKILL.md"},
			}}},
			wantErr: true,
		},
		{
			name:   "resource path escaping root",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:            "a",
				Prefix:        ".a",
				ResourcePaths: map[ir.ResourceKind]string{ir.KindSkill: "../%s/SKILL.md"},
			}}},
			wantErr: true,
		},
		{
			name:   "resource path with foreign verb",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:            "a",
				Prefix:        ".a",
				ResourcePaths: map[ir.ResourceKind]string{ir.KindSkill: "skills/%s/%d"},
			}}},
			wantErr: true,
		},
		{
			name:   "flat resource pattern with placeholder suffix is valid",
			target: TargetClaude,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:            "default",
				Prefix:        ".",
				ResourcePaths: map[ir.ResourceKind]string{ir.KindCommand: "commands/%s.md"},
			}}},
		},
		{
			name:   "resource path placeholder in root element",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:            "a",
				Prefix:        ".a",
				ResourcePaths: map[ir.ResourceKind]string{ir.KindCommand: "%s.md"},
			}}},
			wantErr: true,
		},
		{
			name:   "resource path placeholder mid-element",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:            "a",
				Prefix:        ".a",
				ResourcePaths: map[ir.ResourceKind]string{ir.KindCommand: "a%sb.md"},
			}}},
			wantErr: true,
		},
		{
			name:   "resource path placeholder mid-element under a directory",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:            "a",
				Prefix:        ".a",
				ResourcePaths: map[ir.ResourceKind]string{ir.KindCommand: "flat/cmd-%s.md"},
			}}},
			wantErr: true,
		},
		{
			name:   "same resource kind in two partitions",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{
				{ID: "a", Prefix: ".a", ResourcePaths: map[ir.ResourceKind]string{ir.KindSkill: "skills/%s/SKILL.md"}},
				{ID: "b", Prefix: ".b", ResourcePaths: map[ir.ResourceKind]string{ir.KindSkill: "skills/%s/SKILL.md"}},
			}},
			wantErr: true,
		},
		{
			name:   "skill dir kind without resource path",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:            "a",
				Prefix:        ".a",
				SkillDirKinds: map[ir.ResourceKind]bool{ir.KindCommand: true},
			}}},
			wantErr: true,
		},
		{
			name:   "skill dir kind with flat pattern",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:            "a",
				Prefix:        ".a",
				ResourcePaths: map[ir.ResourceKind]string{ir.KindSubagent: "agents/%s.md"},
				SkillDirKinds: map[ir.ResourceKind]bool{ir.KindSubagent: true},
			}}},
			wantErr: true,
		},
		{
			name:   "aggregate with absolute path",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:     "a",
				Prefix: ".a",
				Aggregates: []NativeAggregate{{
					Kind:    ir.KindHook,
					Project: NativeAggregateFile{Path: "/abs/hooks.json"},
					User:    NativeAggregateFile{Path: "hooks.json"},
				}},
			}}},
			wantErr: true,
		},
		{
			name:   "aggregate with escaping user path",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:     "a",
				Prefix: ".a",
				Aggregates: []NativeAggregate{{
					Kind:    ir.KindHook,
					Project: NativeAggregateFile{Path: "hooks.json"},
					User:    NativeAggregateFile{Path: "../hooks.json"},
				}},
			}}},
			wantErr: true,
		},
		{
			name:   "aggregate with empty user path",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:     "a",
				Prefix: ".a",
				Aggregates: []NativeAggregate{{
					Kind:    ir.KindHook,
					Project: NativeAggregateFile{Path: "hooks.json"},
				}},
			}}},
			wantErr: true,
		},
		{
			name:   "same aggregate kind in two partitions",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{
				{ID: "a", Prefix: ".a", Aggregates: []NativeAggregate{{
					Kind:    ir.KindHook,
					Project: NativeAggregateFile{Path: "hooks.json"},
					User:    NativeAggregateFile{Path: "hooks.json"},
				}}},
				{ID: "b", Prefix: ".b", Aggregates: []NativeAggregate{{
					Kind:    ir.KindHook,
					Project: NativeAggregateFile{Path: "hooks.json"},
					User:    NativeAggregateFile{Path: "hooks.json"},
				}}},
			}},
			wantErr: true,
		},
		{
			name:   "aggregate for non-compilable kind",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:     "a",
				Prefix: ".a",
				Aggregates: []NativeAggregate{{
					Kind:    ir.KindCollection,
					Project: NativeAggregateFile{Path: "manifest.json"},
					User:    NativeAggregateFile{Path: "manifest.json"},
				}},
			}}},
			wantErr: true,
		},
		{
			name:   "aggregate member that is a path",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:     "a",
				Prefix: ".a",
				Aggregates: []NativeAggregate{{
					Kind:    ir.KindHook,
					Project: NativeAggregateFile{Path: "config.json", Member: "a/b"},
					User:    NativeAggregateFile{Path: "config.json", Member: "a/b"},
				}},
			}}},
			wantErr: true,
		},
		{
			name:   "aggregate naming the partition root",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:     "a",
				Prefix: ".a",
				Aggregates: []NativeAggregate{{
					Kind:    ir.KindHook,
					Project: NativeAggregateFile{Path: "."},
					User:    NativeAggregateFile{Path: "."},
				}},
			}}},
			wantErr: true,
		},
		{
			name:   "aggregate naming the partition root in one scope",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:     "a",
				Prefix: ".a",
				Aggregates: []NativeAggregate{{
					Kind:    ir.KindHook,
					Project: NativeAggregateFile{Path: "hooks.json"},
					User:    NativeAggregateFile{Path: "."},
				}},
			}}},
			wantErr: true,
		},
		{
			name:   "aggregate equal to a declared component",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:         "a",
				Prefix:     ".a",
				Components: []string{"skills"},
				Aggregates: []NativeAggregate{{
					Kind:    ir.KindHook,
					Project: NativeAggregateFile{Path: "skills"},
					User:    NativeAggregateFile{Path: "skills"},
				}},
			}}},
			wantErr: true,
		},
		{
			name:   "aggregate equal to a resource directory",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:            "a",
				Prefix:        ".a",
				ResourcePaths: map[ir.ResourceKind]string{ir.KindSkill: "skills/%s/SKILL.md"},
				Aggregates: []NativeAggregate{{
					Kind:    ir.KindHook,
					Project: NativeAggregateFile{Path: "skills"},
					User:    NativeAggregateFile{Path: "skills"},
				}},
			}}},
			wantErr: true,
		},
		{
			// A component cannot be multi-element, so the declared directory
			// comes from a ResourcePaths pattern deriving conf/hooks.
			name:   "aggregate ancestor of a declared resource directory",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:            "a",
				Prefix:        ".a",
				ResourcePaths: map[ir.ResourceKind]string{ir.KindSkill: "conf/hooks/%s/x.md"},
				Aggregates: []NativeAggregate{{
					Kind:    ir.KindHook,
					Project: NativeAggregateFile{Path: "conf"},
					User:    NativeAggregateFile{Path: "conf"},
				}},
			}}},
			wantErr:         true,
			wantErrContains: `that is or contains the declared directory "conf/hooks"`,
		},
		{
			name:   "aggregate inside a declared component is valid",
			target: TargetClaude,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:         "default",
				Prefix:     ".",
				Components: []string{"hooks"},
				Aggregates: []NativeAggregate{{
					Kind:       ir.KindHook,
					Shape:      HookShapeMatcherGroups,
					SupportDir: "hook-support",
					Project:    NativeAggregateFile{Path: "hooks/hooks.json", Member: "hooks"},
					User:       NativeAggregateFile{Path: "hooks/hooks.json", Member: "hooks"},
				}},
			}}},
		},
		{
			name:   "scope-variant aggregate is valid",
			target: TargetDevin,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:     "default",
				Prefix: ".",
				Aggregates: []NativeAggregate{{
					Kind:       ir.KindHook,
					Shape:      HookShapeMatcherGroups,
					SupportDir: "hook-support",
					Project:    NativeAggregateFile{Path: "hooks.v1.json"},
					User:       NativeAggregateFile{Path: "config.json", Member: "hooks"},
				}},
			}}},
		},
		{
			name:    "component naming the partition root",
			target:  TargetClaude,
			layout:  NativeLayout{Partitions: []NativePartition{{ID: "default", Prefix: ".", Components: []string{"."}}}},
			wantErr: true,
		},
		{
			name:    "multi-element component",
			target:  TargetClaude,
			layout:  NativeLayout{Partitions: []NativePartition{{ID: "default", Prefix: ".", Components: []string{"a/b"}}}},
			wantErr: true,
		},
		{
			name:   "nested partition prefixes",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{
				{ID: "a", Prefix: ".a", Components: []string{"x"}},
				{ID: "b", Prefix: ".a/b", Components: []string{"y"}},
			}},
			wantErr: true,
		},
		{
			name:   "nested partition prefixes reversed",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{
				{ID: "a", Prefix: ".a/b", Components: []string{"x"}},
				{ID: "b", Prefix: ".a", Components: []string{"y"}},
			}},
			wantErr: true,
		},
		{
			name:   "directory-shaped pattern without skill directory declaration",
			target: TargetClaude,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:            "default",
				Prefix:        ".",
				ResourcePaths: map[ir.ResourceKind]string{ir.KindCommand: "commands/%s/main.md"},
			}}},
			wantErr: true,
		},
		{
			name:   "flat skill pattern",
			target: TargetClaude,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:            "default",
				Prefix:        ".",
				ResourcePaths: map[ir.ResourceKind]string{ir.KindSkill: "skills/%s.md"},
			}}},
			wantErr: true,
		},
		{
			name:   "aggregate ancestor of another aggregate",
			target: TargetClaude,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:     "default",
				Prefix: ".",
				Aggregates: []NativeAggregate{
					{
						Kind:    ir.KindHook,
						Project: NativeAggregateFile{Path: "conf"},
						User:    NativeAggregateFile{Path: "conf"},
					},
					{
						Kind:    ir.KindMcpServer,
						Project: NativeAggregateFile{Path: "conf/hooks.json"},
						User:    NativeAggregateFile{Path: "conf/hooks.json"},
					},
				},
			}}},
			wantErr: true,
		},
		{
			name:   "aggregate ancestor of another aggregate in user scope only",
			target: TargetDevin,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:     "default",
				Prefix: ".",
				Aggregates: []NativeAggregate{
					{
						Kind:    ir.KindHook,
						Project: NativeAggregateFile{Path: "hooks.v1.json"},
						User:    NativeAggregateFile{Path: "conf"},
					},
					{
						Kind:    ir.KindMcpServer,
						Project: NativeAggregateFile{Path: "mcp_config.json", Member: "mcpServers"},
						User:    NativeAggregateFile{Path: "conf/mcp.json", Member: "mcpServers"},
					},
				},
			}}},
			wantErr: true,
		},
		{
			name:   "two kinds claiming the same path and member",
			target: TargetClaude,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:     "default",
				Prefix: ".",
				Aggregates: []NativeAggregate{
					{
						Kind:    ir.KindHook,
						Project: NativeAggregateFile{Path: "config.json", Member: "hooks"},
						User:    NativeAggregateFile{Path: "config.json", Member: "hooks"},
					},
					{
						Kind:    ir.KindMcpServer,
						Project: NativeAggregateFile{Path: "config.json", Member: "hooks"},
						User:    NativeAggregateFile{Path: "config.json", Member: "hooks"},
					},
				},
			}}},
			wantErr: true,
		},
		{
			name:   "same path different members is a legal shared envelope",
			target: TargetClaude,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:     "default",
				Prefix: ".",
				Aggregates: []NativeAggregate{
					{
						Kind:       ir.KindHook,
						Shape:      HookShapeMatcherGroups,
						SupportDir: "hook-support",
						Project:    NativeAggregateFile{Path: "plugin.json", Member: "hooks"},
						User:       NativeAggregateFile{Path: "plugin.json", Member: "hooks"},
					},
					{
						Kind:    ir.KindMcpServer,
						Project: NativeAggregateFile{Path: "plugin.json", Member: "mcp"},
						User:    NativeAggregateFile{Path: "plugin.json", Member: "mcp"},
					},
				},
			}}},
		},
		{
			name:    "machine root env on a legacy single-root target",
			target:  TargetClaude,
			layout:  NativeLayout{Partitions: []NativePartition{{ID: "default", Prefix: ".", MachineRootEnv: "SOME_HOME", Components: []string{"skills"}}}},
			wantErr: true,
		},
		{
			name:    "installation evidence on a legacy single-root target",
			target:  TargetClaude,
			layout:  NativeLayout{Partitions: []NativePartition{{ID: "default", Prefix: ".", InstallationEvidence: true, Components: []string{"skills"}}}},
			wantErr: true,
		},
		{
			name:   "duplicate machine root env across partitions",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{
				{ID: "a", Prefix: ".a", MachineRootEnv: "X_HOME", Components: []string{"x"}},
				{ID: "b", Prefix: ".b", MachineRootEnv: "X_HOME", Components: []string{"y"}},
			}},
			wantErr: true,
		},
		{
			name:   "kind skill listed in skill dir kinds",
			target: TargetClaude,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:            "default",
				Prefix:        ".",
				ResourcePaths: map[ir.ResourceKind]string{ir.KindSkill: "skills/%s/SKILL.md"},
				SkillDirKinds: map[ir.ResourceKind]bool{ir.KindSkill: true},
			}}},
			wantErr: true,
		},
		{
			name:    "wholly empty partition",
			target:  TargetCodex,
			layout:  NativeLayout{Partitions: []NativePartition{{ID: "a", Prefix: ".a"}}},
			wantErr: true,
		},
		{
			name:   "placeholder suffix in a non-final element",
			target: TargetClaude,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:            "default",
				Prefix:        ".",
				ResourcePaths: map[ir.ResourceKind]string{ir.KindCommand: "units/%s.unit/manifest.json"},
			}}},
			wantErr: true,
		},
		{
			name:   "placeholder suffix element followed by deeper file",
			target: TargetClaude,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:            "default",
				Prefix:        ".",
				ResourcePaths: map[ir.ResourceKind]string{ir.KindCommand: "a/%s.md/deep.md"},
			}}},
			wantErr: true,
		},
		{
			name:   "placeholder suffix in basename under a directory is valid",
			target: TargetClaude,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:            "default",
				Prefix:        ".",
				ResourcePaths: map[ir.ResourceKind]string{ir.KindCommand: "units/%s.unit"},
			}}},
		},
		{
			name:   "two partitions declaring installation evidence",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{
				{ID: "a", Prefix: ".a", Components: []string{"x"}, InstallationEvidence: true},
				{ID: "b", Prefix: ".b", Components: []string{"y"}, InstallationEvidence: true},
			}},
			wantErr: true,
		},
		{
			name:   "single installation evidence partition is valid",
			target: TargetCodex,
			layout: NativeLayout{Partitions: []NativePartition{
				{ID: "a", Prefix: ".a", Components: []string{"x"}, InstallationEvidence: true},
				{ID: "b", Prefix: ".b", Components: []string{"y"}},
			}},
		},
		{
			name:   "aggregate inside a derived resource directory",
			target: TargetClaude,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:            "default",
				Prefix:        ".",
				ResourcePaths: map[ir.ResourceKind]string{ir.KindSkill: "skills/%s/SKILL.md"},
				Aggregates: []NativeAggregate{{
					Kind:    ir.KindMcpServer,
					Project: NativeAggregateFile{Path: "skills/index.json", Member: "mcpServers"},
					User:    NativeAggregateFile{Path: "skills/index.json", Member: "mcpServers"},
				}},
			}}},
			wantErr: true,
		},
		{
			name:   "aggregate nested inside a derived resource directory",
			target: TargetClaude,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:            "default",
				Prefix:        ".",
				ResourcePaths: map[ir.ResourceKind]string{ir.KindSkill: "skills/%s/SKILL.md"},
				Aggregates: []NativeAggregate{{
					Kind:    ir.KindHook,
					Project: NativeAggregateFile{Path: "skills/meta/index.json", Member: "hooks"},
					User:    NativeAggregateFile{Path: "skills/meta/index.json", Member: "hooks"},
				}},
			}}},
			wantErr: true,
		},
		{
			name:   "aggregate inside a flat resource directory",
			target: TargetClaude,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:            "default",
				Prefix:        ".",
				ResourcePaths: map[ir.ResourceKind]string{ir.KindCommand: "commands/%s.md"},
				Aggregates: []NativeAggregate{{
					Kind:    ir.KindHook,
					Project: NativeAggregateFile{Path: "commands/index.json", Member: "hooks"},
					User:    NativeAggregateFile{Path: "commands/index.json", Member: "hooks"},
				}},
			}}},
			wantErr: true,
		},
		{
			name:   "aggregate beside a derived resource directory is valid",
			target: TargetClaude,
			layout: NativeLayout{Partitions: []NativePartition{{
				ID:            "default",
				Prefix:        ".",
				ResourcePaths: map[ir.ResourceKind]string{ir.KindSkill: "skills/%s/SKILL.md"},
				Aggregates: []NativeAggregate{{
					Kind:    ir.KindMcpServer,
					Project: NativeAggregateFile{Path: "skills-index.json", Member: "mcpServers"},
					User:    NativeAggregateFile{Path: "skills-index.json", Member: "mcpServers"},
				}},
			}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNativeLayout(tt.target, tt.layout)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateNativeLayout(%s) error = %v, wantErr %t", tt.target, err, tt.wantErr)
			}
			if tt.wantErrContains != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErrContains)) {
				t.Fatalf("ValidateNativeLayout(%s) error = %v, want error containing %q", tt.target, err, tt.wantErrContains)
			}
		})
	}
}

// TestIsDirShapedPattern pins the dir-shape contract: a non-final path element
// that is exactly "%s". A suffixed basename stays flat; a suffixed non-final
// element is neither, and validation rejects it rather than classify it.
func TestIsDirShapedPattern(t *testing.T) {
	for _, tt := range []struct {
		pattern string
		want    bool
	}{
		{"skills/%s/SKILL.md", true},
		{"a/b/%s/manifest.json", true},
		{"commands/%s.md", false},
		{"units/%s.unit", false},
		{"%s", false},
		{"units/%s.unit/manifest.json", false},
		{"a/%s.md/deep.md", false},
		{"a/x%s/y.md", false},
	} {
		if got := IsDirShapedPattern(tt.pattern); got != tt.want {
			t.Errorf("IsDirShapedPattern(%q) = %v, want %v", tt.pattern, got, tt.want)
		}
	}
}

// TestHookAggregateTopologyIsDeclaredForEveryTarget pins the hook aggregate
// contract for all seven targets: the native file (path and member), the
// entry shape, the support directory root, the envelope members the file
// carries beside the hook member, and the legacy locations migration reads.
func TestHookAggregateTopologyIsDeclaredForEveryTarget(t *testing.T) {
	tests := []struct {
		target      Target
		shape       HookShape
		projectFile NativeAggregateFile
		userFile    NativeAggregateFile
	}{
		{
			target:      TargetClaude,
			shape:       HookShapeMatcherGroups,
			projectFile: NativeAggregateFile{Path: "settings.json", Member: "hooks", Legacy: []string{"hooks/hooks.json"}},
			userFile:    NativeAggregateFile{Path: "settings.json", Member: "hooks", Legacy: []string{"hooks/hooks.json"}},
		},
		{
			target:      TargetFactory,
			shape:       HookShapeMatcherGroups,
			projectFile: NativeAggregateFile{Path: "hooks.json", Legacy: []string{"hooks/hooks.json"}},
			userFile:    NativeAggregateFile{Path: "hooks.json", Legacy: []string{"hooks/hooks.json"}},
		},
		{
			target:      TargetCursor,
			shape:       HookShapeEventHandlers,
			projectFile: NativeAggregateFile{Path: "hooks.json", Member: "hooks", Envelope: map[string]any{"version": 1}, Legacy: []string{"hooks/hooks.json"}},
			userFile:    NativeAggregateFile{Path: "hooks.json", Member: "hooks", Envelope: map[string]any{"version": 1}, Legacy: []string{"hooks/hooks.json"}},
		},
		{
			target:      TargetWindsurf,
			shape:       HookShapeEventHandlers,
			projectFile: NativeAggregateFile{Path: ".devin/hooks.json", Member: "hooks", Legacy: []string{".windsurf/hooks.json"}},
			userFile:    NativeAggregateFile{Path: "hooks.json", Member: "hooks"},
		},
		{
			target:      TargetGemini,
			shape:       HookShapeMatcherGroups,
			projectFile: NativeAggregateFile{Path: "settings.json", Member: "hooks"},
			userFile:    NativeAggregateFile{Path: "settings.json", Member: "hooks"},
		},
		{
			target:      TargetDevin,
			shape:       HookShapeMatcherGroups,
			projectFile: NativeAggregateFile{Path: "hooks.v1.json"},
			userFile:    NativeAggregateFile{Path: "config.json", Member: "hooks"},
		},
		{
			target:      TargetCodex,
			shape:       HookShapeMatcherGroups,
			projectFile: NativeAggregateFile{Path: "hooks.json", Member: "hooks"},
			userFile:    NativeAggregateFile{Path: "hooks.json", Member: "hooks"},
		},
	}
	for _, tt := range tests {
		layout, ok := LookupNativeLayout(tt.target)
		if !ok {
			t.Fatalf("%s has no registered native layout", tt.target)
		}
		var aggregate *NativeAggregate
		for i := range layout.Partitions {
			for j := range layout.Partitions[i].Aggregates {
				if layout.Partitions[i].Aggregates[j].Kind == ir.KindHook {
					aggregate = &layout.Partitions[i].Aggregates[j]
				}
			}
		}
		if aggregate == nil {
			t.Fatalf("%s declares no hook aggregate", tt.target)
		}
		if aggregate.Shape != tt.shape {
			t.Errorf("%s hook aggregate shape = %q, want %q", tt.target, aggregate.Shape, tt.shape)
		}
		if aggregate.SupportDir != "hook-support" {
			t.Errorf("%s hook aggregate support dir = %q, want hook-support", tt.target, aggregate.SupportDir)
		}
		for scope, want := range map[bool]NativeAggregateFile{false: tt.projectFile, true: tt.userFile} {
			if _, ok := NativeAggregateFileForScope(tt.target, ir.KindHook, scope); !ok {
				t.Fatalf("%s hook aggregate file missing for machine_scope=%v", tt.target, scope)
			}
			// NativeAggregateFileForScope joins the partition prefix; the
			// declared file is relative to it, so compare the declared value.
			declared := aggregate.Project
			if scope {
				declared = aggregate.User
			}
			if !reflect.DeepEqual(declared, want) {
				t.Errorf("%s hook aggregate file (machine_scope=%v) = %#v, want %#v", tt.target, scope, declared, want)
			}
			if !reflect.DeepEqual(declared.Legacy, want.Legacy) {
				t.Errorf("%s hook aggregate legacy (machine_scope=%v) = %v, want %v", tt.target, scope, declared.Legacy, want.Legacy)
			}
		}
	}
}

// TestValidateNativeLayoutRejectsInvalidHookDeclarations exercises the new
// hook-topology checks: shape required exactly for hook aggregates, support
// directory required exactly for hook aggregates, envelope members must not
// collide with the entry member, and legacy locations must be clean relative
// file paths distinct from the scope's own file.
func TestValidateNativeLayoutRejectsInvalidHookDeclarations(t *testing.T) {
	partition := func(aggregate NativeAggregate) NativeLayout {
		return NativeLayout{Partitions: []NativePartition{{
			ID:         "default",
			Prefix:     ".",
			Components: []string{"hooks"},
			Aggregates: []NativeAggregate{aggregate},
		}}}
	}
	hook := func() NativeAggregate {
		return NativeAggregate{
			Kind:       ir.KindHook,
			Shape:      HookShapeMatcherGroups,
			SupportDir: "hook-support",
			Project:    NativeAggregateFile{Path: "hooks.json", Member: "hooks"},
			User:       NativeAggregateFile{Path: "hooks.json", Member: "hooks"},
		}
	}
	tests := []struct {
		name            string
		mutate          func(*NativeAggregate)
		wantErrContains string
	}{
		{name: "valid hook aggregate"},
		{
			name:            "hook aggregate without shape",
			mutate:          func(a *NativeAggregate) { a.Shape = "" },
			wantErrContains: "shape",
		},
		{
			name:            "hook aggregate with unknown shape",
			mutate:          func(a *NativeAggregate) { a.Shape = HookShape("bogus") },
			wantErrContains: "shape",
		},
		{
			name:            "hook aggregate without support directory",
			mutate:          func(a *NativeAggregate) { a.SupportDir = "" },
			wantErrContains: "support",
		},
		{
			name:            "hook aggregate with invalid support directory",
			mutate:          func(a *NativeAggregate) { a.SupportDir = "../support" },
			wantErrContains: "support",
		},
		{
			name: "non-hook aggregate with shape",
			mutate: func(a *NativeAggregate) {
				a.Kind, a.Shape, a.SupportDir = ir.KindMcpServer, HookShapeMatcherGroups, ""
			},
			wantErrContains: "shape",
		},
		{
			name: "non-hook aggregate with support directory",
			mutate: func(a *NativeAggregate) {
				a.Kind, a.Shape, a.SupportDir = ir.KindMcpServer, "", "hook-support"
			},
			wantErrContains: "support",
		},
		{
			name: "envelope member collides with entry member",
			mutate: func(a *NativeAggregate) {
				a.Project.Envelope = map[string]any{"hooks": 1}
			},
			wantErrContains: "envelope",
		},
		{
			name: "envelope member empty name",
			mutate: func(a *NativeAggregate) {
				a.User.Envelope = map[string]any{"": 1}
			},
			wantErrContains: "envelope",
		},
		{
			name: "legacy location repeats the native file",
			mutate: func(a *NativeAggregate) {
				a.Project.Legacy = []string{"hooks.json"}
			},
			wantErrContains: "legacy",
		},
		{
			name: "legacy location invalid path",
			mutate: func(a *NativeAggregate) {
				a.User.Legacy = []string{"../hooks.json"}
			},
			wantErrContains: "legacy",
		},
		{
			name: "valid envelope and legacy",
			mutate: func(a *NativeAggregate) {
				a.Project.Envelope = map[string]any{"version": 1}
				a.User.Envelope = map[string]any{"version": 1}
				a.Project.Legacy = []string{"hooks/hooks.json"}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			aggregate := hook()
			if tt.mutate != nil {
				tt.mutate(&aggregate)
			}
			err := ValidateNativeLayout(TargetClaude, partition(aggregate))
			if tt.wantErrContains == "" && err != nil {
				t.Fatalf("ValidateNativeLayout = %v, want nil", err)
			}
			if tt.wantErrContains != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErrContains)) {
				t.Fatalf("ValidateNativeLayout error = %v, want error containing %q", err, tt.wantErrContains)
			}
		})
	}
}
