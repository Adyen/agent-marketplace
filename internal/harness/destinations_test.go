package harness

import (
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/ownership"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func TestResolveDestinationsExistingTargetUsesSingleRoot(t *testing.T) {
	repo := t.TempDir()
	got, err := ResolveDestinations(hcc.TargetFactory, false, repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "default" || got[0].Root != filepath.Join(repo, ".factory") || got[0].Prefix != "" || !got[0].InstallationEvidence {
		t.Fatalf("ResolveDestinations(factory) = %#v", got)
	}
}

func TestResolveDestinationsUsesRegisteredProvider(t *testing.T) {
	target := hcc.Target("partitioned-test")
	repo := t.TempDir()
	want := []Destination{{ID: "repo-test", Root: t.TempDir(), Prefix: ".test"}}
	resolvedLayoutProviders[target] = func(machineScope bool, repoRoot, homeDir string) ([]Destination, error) {
		if machineScope {
			t.Fatal("provider received machine scope")
		}
		if repoRoot != repo {
			t.Fatalf("provider repo root = %q, want %q", repoRoot, repo)
		}
		if homeDir != "" {
			t.Fatalf("provider home directory = %q, want empty", homeDir)
		}
		return want, nil
	}
	t.Cleanup(func() { delete(resolvedLayoutProviders, target) })

	got, err := ResolveDestinations(target, false, repo)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveDestinations() = %#v, want %#v", got, want)
	}
}

func TestCodexTargetInfoAdaptsNativeLayout(t *testing.T) {
	layout, ok := targetmeta.LookupNativeLayout(targetmeta.TargetCodex)
	if !ok {
		t.Fatal("Codex native layout is not registered")
	}
	for _, partition := range layout.Partitions {
		got := codexTargetInfo(partition)
		if !reflect.DeepEqual(got.Components, partition.Components) {
			t.Errorf("partition %q components = %v, want %v", partition.ID, got.Components, partition.Components)
		}
		wantResourceDirs := make(map[ir.ResourceKind]string)
		for kind, pattern := range partition.ResourcePaths {
			if kind != ir.KindSkill && kind != ir.KindCommand {
				wantResourceDirs[kind] = path.Dir(pattern)
			}
		}
		if len(wantResourceDirs) == 0 {
			wantResourceDirs = nil
		}
		if !reflect.DeepEqual(got.ResourceDirs, wantResourceDirs) {
			t.Errorf("partition %q resource dirs = %v, want %v", partition.ID, got.ResourceDirs, wantResourceDirs)
		}
		wantSkillPath := partition.ResourcePaths[ir.KindSkill]
		if got.SkillPathPattern != wantSkillPath || !reflect.DeepEqual(got.SkillDirKinds, partition.SkillDirKinds) {
			t.Errorf("partition %q skill layout = %q/%v, want %q/%v", partition.ID, got.SkillPathPattern, got.SkillDirKinds, wantSkillPath, partition.SkillDirKinds)
		}
		wantAggregates := make([]string, 0, len(partition.Aggregates))
		for _, aggregate := range partition.Aggregates {
			wantAggregates = append(wantAggregates, aggregate.Path)
		}
		slices.Sort(wantAggregates)
		if gotAggregates := got.AggregateFilesForScope(false); !reflect.DeepEqual(gotAggregates, wantAggregates) {
			t.Errorf("partition %q aggregates = %v, want %v", partition.ID, gotAggregates, wantAggregates)
		}
	}
}

func TestCodexVirtualTargetInfoAdaptsAllNativePartitions(t *testing.T) {
	layout, ok := targetmeta.LookupNativeLayout(targetmeta.TargetCodex)
	if !ok {
		t.Fatal("Codex native layout is not registered")
	}
	got, err := codexVirtualTargetInfo()
	if err != nil {
		t.Fatal(err)
	}
	var wantComponents, wantAggregates []string
	wantSkillPath := ""
	for _, partition := range layout.Partitions {
		wantComponents = append(wantComponents, partition.Prefix)
		for kind, pattern := range partition.ResourcePaths {
			switch kind {
			case ir.KindSkill:
				wantSkillPath = path.Join(partition.Prefix, pattern)
			case ir.KindCommand:
			}
		}
		for _, aggregate := range partition.Aggregates {
			wantAggregates = append(wantAggregates, path.Join(partition.Prefix, aggregate.Path))
		}
	}
	slices.Sort(wantAggregates)
	if !reflect.DeepEqual(got.Components, wantComponents) || got.SkillPathPattern != wantSkillPath || got.ResourceDirs != nil || !reflect.DeepEqual(got.AggregateFilesForScope(false), wantAggregates) {
		t.Fatalf("virtual Codex target info = %#v; want components=%v skill=%q no resource dirs aggregates=%v", got, wantComponents, wantSkillPath, wantAggregates)
	}
}

func TestResolveDestinationsExistingTargetUsesCurrentHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got, err := ResolveDestinations(hcc.TargetFactory, true, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Root != filepath.Join(home, ".factory") {
		t.Fatalf("ResolveDestinations(factory machine) = %#v", got)
	}
}

func TestResolveDestinationsCanonicalizesMissingCodexHomeBelowSymlink(t *testing.T) {
	home, realParent, linkParent := t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(realParent, linkParent); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(linkParent, "missing"))
	destinations, err := ResolveDestinations(hcc.Target(targetmeta.TargetCodex), true, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want, err := ownership.CanonicalRoot(filepath.Join(realParent, "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if got := destinations[1].Root; got != want {
		t.Fatalf("canonical CODEX_HOME = %q, want %q", got, want)
	}
}

func TestResolveDestinationsRefusesRepoRootsEscapingThroughSymlinks(t *testing.T) {
	for _, name := range []string{".agents", ".codex"} {
		t.Run(name, func(t *testing.T) {
			repo, outside := t.TempDir(), t.TempDir()
			sentinel := filepath.Join(outside, "sentinel")
			if err := os.WriteFile(sentinel, []byte("keep\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(repo, name)); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if _, err := ResolveDestinations(hcc.Target(targetmeta.TargetCodex), false, repo); err == nil {
				t.Fatal("ResolveDestinations accepted a repository destination outside the repository")
			}
			got, err := os.ReadFile(sentinel)
			if err != nil || string(got) != "keep\n" {
				t.Fatalf("outside sentinel changed: %q, %v", got, err)
			}
		})
	}
}

func TestResolveDestinationsCodexScopes(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	customCodexHome := filepath.Join(t.TempDir(), "codex-home")
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", customCodexHome)
	codex := hcc.Target(targetmeta.TargetCodex)
	repoRoot, _ := ownership.CanonicalRoot(repo)
	homeRoot, _ := ownership.CanonicalRoot(home)
	customRoot, _ := ownership.CanonicalRoot(customCodexHome)

	tests := []struct {
		name         string
		machineScope bool
		want         []Destination
	}{
		{
			name: "repository",
			want: []Destination{
				{ID: "repo-agents", Root: filepath.Join(repoRoot, ".agents"), Prefix: ".agents"},
				{ID: "repo-codex", Root: filepath.Join(repoRoot, ".codex"), Prefix: ".codex", InstallationEvidence: true},
			},
		},
		{
			name:         "machine",
			machineScope: true,
			want: []Destination{
				{ID: "machine-agents", Root: filepath.Join(homeRoot, ".agents"), Prefix: ".agents"},
				{ID: "machine-codex", Root: customRoot, Prefix: ".codex", InstallationEvidence: true},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveDestinations(codex, tt.machineScope, repo)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("destinations = %#v, want %#v", got, tt.want)
			}
			for i := range got {
				if got[i].ID != tt.want[i].ID || got[i].Root != tt.want[i].Root || got[i].Prefix != tt.want[i].Prefix || got[i].InstallationEvidence != tt.want[i].InstallationEvidence {
					t.Errorf("destination[%d] = %#v, want identity %#v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestEveryPartitionedCompilerTargetHasResolvedDestinations(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, "codex-home"))
	for _, definition := range targetmeta.Definitions() {
		if !definition.Compiler || !definition.PartitionedRoots {
			continue
		}
		target := hcc.Target(definition.Target)
		if destinations, err := ResolveDestinations(target, false, repo); err != nil || len(destinations) == 0 {
			t.Errorf("ResolveDestinations(%s, repo) = %#v, %v", target, destinations, err)
		}
		destinations, err := ResolveMachineDestinationsForHome(target, home)
		if err != nil || len(destinations) == 0 {
			t.Errorf("ResolveMachineDestinationsForHome(%s) = %#v, %v", target, destinations, err)
			continue
		}
		if !slices.ContainsFunc(destinations, func(destination Destination) bool { return destination.InstallationEvidence }) {
			t.Errorf("%s has no machine installation-evidence destination: %#v", target, destinations)
		}
	}
}

func TestResolveDestinationsRejectsOverlappingCodexMachinePartitions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	codex := hcc.Target(targetmeta.TargetCodex)
	cases := []struct {
		name string
		root string
	}{
		{name: "duplicate", root: filepath.Join(home, ".agents")},
		{name: "ancestor", root: home},
		{name: "descendant", root: filepath.Join(home, ".agents", "nested")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CODEX_HOME", tc.root)
			if _, err := ResolveDestinations(codex, true, t.TempDir()); err == nil {
				t.Fatalf("ResolveDestinations accepted overlapping CODEX_HOME %q", tc.root)
			}
		})
	}
}

func TestDestinationRouteArtifact(t *testing.T) {
	d := Destination{Prefix: ".agents"}
	if got, err := d.RouteArtifact(".agents/skills/demo/SKILL.md"); err != nil || got != "skills/demo/SKILL.md" {
		t.Fatalf("RouteArtifact() = %q, %v", got, err)
	}
	for _, path := range []string{".codex/agents/demo.toml", ".agents/../escape"} {
		if _, err := d.RouteArtifact(path); err == nil {
			t.Errorf("RouteArtifact(%q) succeeded", path)
		}
	}
}
