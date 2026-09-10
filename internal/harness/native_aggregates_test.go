package harness

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

func TestNativeAggregateDestinationPaths(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	tests := []struct {
		target  hcc.Target
		machine bool
		want    []string
	}{
		{hcc.TargetClaude, false, []string{filepath.Join(repo, ".claude/.claude-plugin/plugin.json"), filepath.Join(repo, ".claude/hooks/hooks.json"), filepath.Join(repo, ".mcp.json")}},
		{hcc.TargetClaude, true, []string{filepath.Join(home, ".claude/.claude-plugin/plugin.json"), filepath.Join(home, ".claude/hooks/hooks.json"), filepath.Join(home, ".claude.json")}},
		{hcc.TargetGemini, false, []string{filepath.Join(repo, ".gemini/settings.json")}},
		{hcc.TargetGemini, true, []string{filepath.Join(home, ".gemini/settings.json")}},
	}
	for _, test := range tests {
		destinations, err := ResolveDestinations(test.target, test.machine, repo)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, destination := range destinations {
			if len(destination.AggregateRoutes) > 0 && (len(destination.Info.Components) > 0 || destination.InstallationEvidence) {
				t.Fatalf("aggregate root permits directory scans: %+v", destination)
			}
			if len(destination.AggregateRoutes) > 0 {
				if _, err := destination.RouteArtifact("unrelated.json"); err == nil {
					t.Fatal("aggregate destination accepts arbitrary files")
				}
				if _, err := destination.RouteArtifact("../outside.json"); err == nil {
					t.Fatal("aggregate destination accepts traversal")
				}
				source, want := ".mcp.json", ".mcp.json"
				if test.machine {
					want = ".claude.json"
				}
				if test.target == hcc.TargetGemini {
					source, want = "gemini-extension.json", "settings.json"
				}
				if got, err := destination.RouteArtifact(source); err != nil || got != want {
					t.Fatalf("native route = %q, %v; want %q", got, err, want)
				}
			}
			for _, rel := range destination.Info.AggregateFilesForScope(test.machine) {
				got = append(got, filepath.Join(destination.Root, rel))
			}
		}
		sort.Strings(got)
		sort.Strings(test.want)
		if !reflect.DeepEqual(got, test.want) {
			t.Fatalf("%s machine=%v: got %v want %v", test.target, test.machine, got, test.want)
		}
	}
}
