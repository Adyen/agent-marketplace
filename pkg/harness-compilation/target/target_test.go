package target

import (
	"reflect"
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestCodexIdentityIsPubliclyDiscoverable(t *testing.T) {
	if TargetCodex != "codex" {
		t.Fatalf("TargetCodex = %q, want codex", TargetCodex)
	}
	definition, ok := Lookup(string(TargetCodex))
	if !ok {
		t.Fatal("Codex must be publicly discoverable after target promotion")
	}
	if definition.HookProjection == nil {
		t.Fatal("Codex hook projection is not derived")
	}
	definition.HookProjection = nil
	want := Definition{Target: TargetCodex, Label: "Codex", PartitionedRoots: true, Compiler: true, Ingester: true, Conversion: true, UI: true, UIOrder: 6}
	if !reflect.DeepEqual(definition, want) {
		t.Fatalf("Lookup(codex) = %#v, want %#v", definition, want)
	}
	if !slices.Contains(Targets(), TargetCodex) {
		t.Fatal("Codex must be present in public target lists after promotion")
	}
}

func TestCodexNativeLayoutIsAvailable(t *testing.T) {
	want := NativeLayout{Partitions: []NativePartition{
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
		},
	}}

	got, ok := LookupNativeLayout(TargetCodex)
	if !ok {
		t.Fatal("Codex native layout is not registered")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LookupNativeLayout(codex) = %#v, want %#v", got, want)
	}

	got.Partitions[0].Components[0] = "changed"
	again, _ := LookupNativeLayout(TargetCodex)
	if again.Partitions[0].Components[0] != "skills" {
		t.Fatal("LookupNativeLayout returned mutable registry state")
	}

	tests := []struct {
		kind ir.ResourceKind
		name string
		want string
	}{
		{kind: ir.KindSkill, name: "review", want: ".agents/skills/review/SKILL.md"},
		{kind: ir.KindCommand, name: "release", want: ".agents/skills/release/SKILL.md"},
		{kind: ir.KindSubagent, name: "planner", want: ".codex/agents/planner.toml"},
	}
	for _, tt := range tests {
		if got, ok := NativeResourcePath(TargetCodex, tt.kind, tt.name); !ok || got != tt.want {
			t.Errorf("NativeResourcePath(codex, %s, %q) = %q, %v; want %q, true", tt.kind, tt.name, got, ok, tt.want)
		}
	}
	if got, ok := NativeResourceRoot(TargetCodex, ir.KindSkill); !ok || got != ".agents/skills" {
		t.Errorf("NativeResourceRoot(codex, skill) = %q, %v; want .agents/skills, true", got, ok)
	}
	if got, ok := NativeAggregatePath(TargetCodex, ir.KindHook); !ok || got != ".codex/hooks.json" {
		t.Errorf("NativeAggregatePath(codex, hook) = %q, %v; want .codex/hooks.json, true", got, ok)
	}
}

func TestDefinitionsAppendCodexWithoutChangingExistingTargets(t *testing.T) {
	want := []Definition{
		{Target: TargetClaude, Label: "Claude Code", RepoRoot: ".claude", UserRoot: ".claude", Compiler: true, Ingester: true, Conversion: true, UI: true, UIOrder: 1},
		{Target: TargetFactory, Label: "Factory Droid", RepoRoot: ".factory", UserRoot: ".factory", Compiler: true, Ingester: true, Conversion: true, UI: true, UIOrder: 0},
		{Target: TargetCursor, Label: "Cursor", RepoRoot: ".cursor", UserRoot: ".cursor", Compiler: true, Ingester: true, Conversion: true, UI: true, UIOrder: 2},
		{Target: TargetWindsurf, Label: "Windsurf", RepoRoot: ".", UserRoot: ".codeium/windsurf", Compiler: true, Ingester: true, Conversion: true, UI: true, UIOrder: 3},
		{Target: TargetGemini, Label: "Gemini CLI", RepoRoot: ".gemini", UserRoot: ".gemini", Compiler: true, Ingester: true, Conversion: true, UI: true, UIOrder: 4},
		{Target: TargetDevin, Label: "Devin CLI", RepoRoot: ".devin", UserRoot: ".config/devin", Compiler: true, Ingester: true, Conversion: true, UI: true, UIOrder: 5},
		{Target: TargetCodex, Label: "Codex", PartitionedRoots: true, Compiler: true, Ingester: true, Conversion: true, UI: true, UIOrder: 6},
	}
	got := Definitions()
	for i := range got {
		if got[i].HookProjection == nil {
			t.Fatalf("Definitions()[%d] has no derived hook projection", i)
		}
		got[i].HookProjection = nil
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Definitions() = %#v, want %#v", got, want)
	}
}

func TestValidateDefinitionRootModels(t *testing.T) {
	tests := []struct {
		name    string
		def     Definition
		wantErr bool
	}{
		{name: "legacy", def: Definition{Target: "legacy", RepoRoot: ".legacy", UserRoot: ".legacy"}},
		{name: "partitioned", def: Definition{Target: "split", PartitionedRoots: true}},
		{name: "neither", def: Definition{Target: "missing"}, wantErr: true},
		{name: "mixed", def: Definition{Target: "mixed", RepoRoot: ".mixed", UserRoot: ".mixed", PartitionedRoots: true}, wantErr: true},
		{name: "partial legacy", def: Definition{Target: "partial", RepoRoot: ".partial"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDefinition(tt.def)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateDefinition(%#v) error = %v, wantErr %t", tt.def, err, tt.wantErr)
			}
		})
	}
}

func TestDefinitionsHaveUniqueCompleteIdentity(t *testing.T) {
	definitions := Definitions()
	if len(definitions) == 0 {
		t.Fatal("target registry must not be empty")
	}

	seen := make(map[Target]bool, len(definitions))
	for _, definition := range definitions {
		if err := ValidateDefinition(definition); err != nil {
			t.Fatalf("target %q has invalid root metadata: %v", definition.Target, err)
		}
		if definition.Target == "" {
			t.Fatal("target definition has empty identity")
		}
		if seen[definition.Target] {
			t.Fatalf("target %q is declared more than once", definition.Target)
		}
		seen[definition.Target] = true
		if definition.Label == "" {
			t.Fatalf("target %q has incomplete user-facing or root metadata: %#v", definition.Target, definition)
		}
		if definition.Conversion && (!definition.Compiler || !definition.Ingester) {
			t.Fatalf("target %q exposes conversion without compiler and ingester capabilities", definition.Target)
		}
	}
}

func TestLookupRejectsUnknownAndNonCanonicalNames(t *testing.T) {
	if definition, ok := Lookup("devin"); !ok || definition.RepoRoot != ".devin" || definition.UserRoot != ".config/devin" {
		t.Fatalf("Lookup(\"devin\") = %#v, %v, want canonical Devin roots", definition, ok)
	}
	for _, name := range []string{"", " factory ", "Factory"} {
		if _, ok := Lookup(name); ok {
			t.Errorf("Lookup(%q) unexpectedly found a target", name)
		}
		if IsValid(name) {
			t.Errorf("IsValid(%q) unexpectedly accepted a target", name)
		}
	}
}

func TestCapabilityListsMatchDefinitions(t *testing.T) {
	var wantTargets, wantCompiler, wantIngester, wantConversion []Target
	seenOrder := make(map[int]Target)
	for _, definition := range Definitions() {
		wantTargets = append(wantTargets, definition.Target)
		if definition.Compiler {
			wantCompiler = append(wantCompiler, definition.Target)
		}
		if definition.Ingester {
			wantIngester = append(wantIngester, definition.Target)
		}
		if definition.Conversion {
			wantConversion = append(wantConversion, definition.Target)
		}
		if definition.UI {
			if other, exists := seenOrder[definition.UIOrder]; exists {
				t.Errorf("UIOrder %d is shared by %q and %q", definition.UIOrder, other, definition.Target)
			}
			seenOrder[definition.UIOrder] = definition.Target
		}
		if got := IsCompilerTarget(string(definition.Target)); got != definition.Compiler {
			t.Errorf("IsCompilerTarget(%q) = %t, want %t", definition.Target, got, definition.Compiler)
		}
		if got := IsIngesterTarget(string(definition.Target)); got != definition.Ingester {
			t.Errorf("IsIngesterTarget(%q) = %t, want %t", definition.Target, got, definition.Ingester)
		}
		if got := IsConversionTarget(string(definition.Target)); got != definition.Conversion {
			t.Errorf("IsConversionTarget(%q) = %t, want %t", definition.Target, got, definition.Conversion)
		}
	}
	if !slices.Equal(Targets(), wantTargets) {
		t.Errorf("Targets() = %v, want %v", Targets(), wantTargets)
	}
	if !slices.Equal(CompilerTargets(), wantCompiler) {
		t.Errorf("CompilerTargets() = %v, want %v", CompilerTargets(), wantCompiler)
	}
	if !slices.Equal(IngesterTargets(), wantIngester) {
		t.Errorf("IngesterTargets() = %v, want %v", IngesterTargets(), wantIngester)
	}
	if !slices.Equal(ConversionTargets(), wantConversion) {
		t.Errorf("ConversionTargets() = %v, want %v", ConversionTargets(), wantConversion)
	}
	if !slices.Equal(Names(), namesFor(wantTargets)) {
		t.Errorf("Names() = %v, want %v", Names(), namesFor(wantTargets))
	}
	if !slices.Equal(CompilerNames(), namesFor(wantCompiler)) {
		t.Errorf("CompilerNames() = %v, want %v", CompilerNames(), namesFor(wantCompiler))
	}
	if !slices.Equal(IngesterNames(), namesFor(wantIngester)) {
		t.Errorf("IngesterNames() = %v, want %v", IngesterNames(), namesFor(wantIngester))
	}
	if !slices.Equal(ConversionNames(), namesFor(wantConversion)) {
		t.Errorf("ConversionNames() = %v, want %v", ConversionNames(), namesFor(wantConversion))
	}
}
