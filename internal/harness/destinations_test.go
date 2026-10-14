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
		for _, aggregate := range partition.Aggregates {
			if aggregate.Kind == ir.KindHook && aggregate.SupportDir != "" {
				wantResourceDirs[ir.KindHook] = aggregate.SupportDir
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
		var wantProjectAggregates, wantUserAggregates []string
		for _, aggregate := range partition.Aggregates {
			wantProjectAggregates = append(wantProjectAggregates, aggregate.Project.Path)
			wantUserAggregates = append(wantUserAggregates, aggregate.User.Path)
		}
		slices.Sort(wantProjectAggregates)
		slices.Sort(wantUserAggregates)
		if gotAggregates := got.AggregateFilesForScope(false); !reflect.DeepEqual(gotAggregates, wantProjectAggregates) {
			t.Errorf("partition %q project aggregates = %v, want %v", partition.ID, gotAggregates, wantProjectAggregates)
		}
		if gotAggregates := got.AggregateFilesForScope(true); !reflect.DeepEqual(gotAggregates, wantUserAggregates) {
			t.Errorf("partition %q user aggregates = %v, want %v", partition.ID, gotAggregates, wantUserAggregates)
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
	wantResourceDirs := map[ir.ResourceKind]string{}
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
			wantAggregates = append(wantAggregates, path.Join(partition.Prefix, aggregate.Project.Path))
			if aggregate.Kind == ir.KindHook && aggregate.SupportDir != "" {
				wantResourceDirs[ir.KindHook] = path.Join(partition.Prefix, aggregate.SupportDir)
			}
		}
	}
	if len(wantResourceDirs) == 0 {
		wantResourceDirs = nil
	}
	slices.Sort(wantAggregates)
	if !reflect.DeepEqual(got.Components, wantComponents) || got.SkillPathPattern != wantSkillPath || !reflect.DeepEqual(got.ResourceDirs, wantResourceDirs) || !reflect.DeepEqual(got.AggregateFilesForScope(false), wantAggregates) {
		t.Fatalf("virtual Codex target info = %#v; want components=%v skill=%q resource dirs=%v aggregates=%v", got, wantComponents, wantSkillPath, wantResourceDirs, wantAggregates)
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
