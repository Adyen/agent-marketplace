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
	resolvedLayoutProviders[target] = func(machineScope bool, repoRoot string) ([]Destination, error) {
		if machineScope {
			t.Fatal("provider received machine scope")
		}
		if repoRoot != repo {
			t.Fatalf("provider repo root = %q, want %q", repoRoot, repo)
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
