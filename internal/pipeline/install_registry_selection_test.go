package pipeline

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestInstallRegistrySelectionMatchesOnePhysicalSource(t *testing.T) {
	const url = "https://example.com/team/tools.git"
	user := config.RegistryEntry{URL: url, Ref: "main", Source: config.UserPersonal}
	repo := config.RegistryEntry{URL: "https://example.com/team/tools", Ref: "main", Source: config.RepoCommitted}
	nested := config.RegistryEntry{URL: url, Ref: "main", Path: "skills/", Source: config.RepoCommitted}
	release := config.RegistryEntry{URL: url, Ref: "release", Source: config.UserPersonal}
	other := config.RegistryEntry{URL: "https://example.com/team/other.git", Ref: "main", Source: config.RepoCommitted}
	org := config.RegistryEntry{URL: url, Ref: "main", Source: config.Organization}
	repoRoot := config.RegistryEntry{URL: url, Ref: "main", Source: config.RepoCommitted, FilePrecedence: 2, SourceHarness: "codex"}

	for _, tc := range []struct {
		name       string
		registries []config.RegistryEntry
		filter     string
		want       []config.RegistryEntry
		wantErr    []string
	}{
		{"no filter selects every entry", []config.RegistryEntry{user, repo, other}, "", []config.RegistryEntry{user, repo, other}, nil},
		{"one source spelled differently in two layers prefers the target layer", []config.RegistryEntry{user, repo, other}, url, []config.RegistryEntry{repo}, nil},
		{"filter spelled without .git", []config.RegistryEntry{user, repo}, "https://example.com/team/tools", []config.RegistryEntry{repo}, nil},
		{"trailing slash", []config.RegistryEntry{repo}, url + "/", []config.RegistryEntry{repo}, nil},
		{"source only in another layer", []config.RegistryEntry{user, other}, url, []config.RegistryEntry{user}, nil},
		{"without a target-layer entry the highest precedence wins", []config.RegistryEntry{org, user}, url, []config.RegistryEntry{user}, nil},
		{"within a layer the higher file precedence wins", []config.RegistryEntry{repo, repoRoot}, url, []config.RegistryEntry{repoRoot}, nil},
		{"different paths", []config.RegistryEntry{repo, nested}, url, nil, []string{"2 configured entries with different refs or paths", "ref main, repository root in the repository config", "ref main, path skills in the repository config"}},
		{"different refs across layers", []config.RegistryEntry{user, repo, release}, url, nil, []string{"ref main, repository root in the user config and repository config", "ref release, repository root in the user config"}},
		{"local entries without a ref", []config.RegistryEntry{{URL: "./registry", Source: config.RepoCommitted}, {URL: "./registry", Path: "skills", Source: config.RepoCommitted}}, "./registry", nil, []string{"repository root in the repository config; path skills in the repository config"}},
		{"not declared", []config.RegistryEntry{other}, url, nil, []string{`registry "` + url + `" is not declared in any config layer`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := installRegistrySelection(tc.registries, tc.filter, config.RepoCommitted)
			if len(tc.wantErr) > 0 {
				if err == nil {
					t.Fatalf("selection = %+v, want error", got)
				}
				for _, want := range tc.wantErr {
					if !strings.Contains(err.Error(), want) {
						t.Fatalf("error %q does not contain %q", err, want)
					}
				}
				return
			}
			if err != nil || len(got) != len(tc.want) {
				t.Fatalf("selection = %+v, %v; want %+v", got, err, tc.want)
			}
			for i := range got {
				if got[i].URL != tc.want[i].URL || got[i].Source != tc.want[i].Source || got[i].Path != tc.want[i].Path || got[i].FilePrecedence != tc.want[i].FilePrecedence {
					t.Fatalf("selection[%d] = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}
