package harness

import (
	"path/filepath"
	"reflect"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func TestResolveDestinationsExistingTargetUsesSingleRoot(t *testing.T) {
	repo := t.TempDir()
	got, err := ResolveDestinations(hcc.TargetFactory, false, repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "default" || got[0].Root != filepath.Join(repo, ".factory") || got[0].Prefix != "" {
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

func TestResolveDestinationsCodexScopes(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	customCodexHome := filepath.Join(t.TempDir(), "codex-home")
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", customCodexHome)
	codex := hcc.Target(targetmeta.TargetCodex)

	tests := []struct {
		name         string
		machineScope bool
		want         []Destination
	}{
		{
			name: "repository",
			want: []Destination{
				{ID: "repo-agents", Root: filepath.Join(repo, ".agents"), Prefix: ".agents"},
				{ID: "repo-codex", Root: filepath.Join(repo, ".codex"), Prefix: ".codex"},
			},
		},
		{
			name:         "machine",
			machineScope: true,
			want: []Destination{
				{ID: "machine-agents", Root: filepath.Join(home, ".agents"), Prefix: ".agents"},
				{ID: "machine-codex", Root: customCodexHome, Prefix: ".codex"},
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
				if got[i].ID != tt.want[i].ID || got[i].Root != tt.want[i].Root || got[i].Prefix != tt.want[i].Prefix {
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
