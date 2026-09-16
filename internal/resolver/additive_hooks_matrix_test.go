package resolver

import (
	"errors"
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type additiveSourceCase struct {
	url       string
	path      string
	ref       string
	source    config.Source
	required  bool
	selector  config.KindSelector
	harnesses []string
	command   string
}

func TestResolveAdditiveHookSourceMatrix(t *testing.T) {
	tests := []struct {
		name          string
		kind          ir.ResourceKind
		sources       []additiveSourceCase
		wantURLs      []string
		wantCommands  map[string][]string
		wantMetadata  map[string][]additiveSourceMetadata
		wantWarnings  int
		wantHarnesses map[string][]hcc.Target
	}{
		{
			name: "independent URL same matcher",
			sources: []additiveSourceCase{
				{url: "git@example.com:first.git", ref: "main", source: config.RepoCommitted, selector: ks("PreToolUse-command"), command: "echo first"},
				{url: "git@example.com:second.git", ref: "main", source: config.RepoCommitted, selector: ks("PreToolUse-command"), command: "echo second"},
			},
			wantURLs:     []string{"git@example.com:first.git", "git@example.com:second.git"},
			wantCommands: map[string][]string{"git@example.com:first.git": {"echo first"}, "git@example.com:second.git": {"echo second"}},
			wantMetadata: map[string][]additiveSourceMetadata{
				"git@example.com:first.git":  {{url: "git@example.com:first.git", ref: "main", source: config.RepoCommitted, command: "echo first"}},
				"git@example.com:second.git": {{url: "git@example.com:second.git", ref: "main", source: config.RepoCommitted, command: "echo second"}},
			},
		},
		{
			name: "same URL disjoint paths",
			sources: []additiveSourceCase{
				{url: "git@example.com:shared.git", path: "teams/a", ref: "main", source: config.RepoCommitted, selector: ks("PreToolUse-command"), command: "echo a"},
				{url: "git@example.com:shared.git", path: "teams/b", ref: "main", source: config.RepoCommitted, selector: ks("PreToolUse-command"), command: "echo b"},
			},
			wantURLs:     []string{"git@example.com:shared.git", "git@example.com:shared.git"},
			wantCommands: map[string][]string{"git@example.com:shared.git": {"echo a", "echo b"}},
			wantMetadata: map[string][]additiveSourceMetadata{"git@example.com:shared.git": {
				{url: "git@example.com:shared.git", path: "teams/a", ref: "main", source: config.RepoCommitted, command: "echo a"},
				{url: "git@example.com:shared.git", path: "teams/b", ref: "main", source: config.RepoCommitted, command: "echo b"},
			}},
		},
		{
			name: "same URL ref root and child path",
			sources: []additiveSourceCase{
				{url: "git@example.com:shared.git", ref: "main", source: config.RepoCommitted, selector: ks("PreToolUse-command"), command: "echo root"},
				{url: "git@example.com:shared.git", path: "child", ref: "main", source: config.RepoCommitted, selector: ks("PreToolUse-command"), command: "echo child"},
			},
			wantURLs:     []string{"git@example.com:shared.git"},
			wantCommands: map[string][]string{"git@example.com:shared.git": {"echo child"}},
			wantMetadata: map[string][]additiveSourceMetadata{"git@example.com:shared.git": {{url: "git@example.com:shared.git", path: "child", ref: "main", source: config.RepoCommitted, command: "echo child"}}},
		},
		{
			name: "equivalent URL spelling",
			sources: []additiveSourceCase{
				{url: "git@example.com:shared", ref: "main", source: config.RepoCommitted, selector: ks("PreToolUse-command"), command: "echo one"},
				{url: "git@example.com:shared.git", ref: "main", source: config.RepoCommitted, selector: ks("PreToolUse-command"), command: "echo one"},
			},
			wantURLs:     []string{"git@example.com:shared"},
			wantCommands: map[string][]string{"git@example.com:shared": {"echo one"}},
			wantMetadata: map[string][]additiveSourceMetadata{"git@example.com:shared": {{url: "git@example.com:shared", ref: "main", source: config.RepoCommitted, command: "echo one"}}},
		},
		{
			name: "same source path different refs",
			sources: []additiveSourceCase{
				{url: "git@example.com:versions.git", ref: "v1", source: config.Organization, selector: ks("PreToolUse-command"), command: "echo v1"},
				{url: "git@example.com:versions.git", ref: "v2", source: config.UserPersonal, selector: ks("PreToolUse-command"), command: "echo v2"},
			},
			wantURLs:     []string{"git@example.com:versions.git"},
			wantCommands: map[string][]string{"git@example.com:versions.git": {"echo v2"}},
			wantMetadata: map[string][]additiveSourceMetadata{"git@example.com:versions.git": {{url: "git@example.com:versions.git", ref: "v2", source: config.UserPersonal, command: "echo v2"}}},
			wantWarnings: 1,
		},
		{
			name: "user and repo scopes",
			sources: []additiveSourceCase{
				{url: "git@example.com:scopes.git", ref: "main", source: config.UserPersonal, selector: ks("PreToolUse-command"), command: "echo same"},
				{url: "git@example.com:scopes.git", ref: "main", source: config.RepoCommitted, selector: ks("PreToolUse-command"), command: "echo same"},
			},
			wantURLs:     []string{"git@example.com:scopes.git", "git@example.com:scopes.git"},
			wantCommands: map[string][]string{"git@example.com:scopes.git": {"echo same", "echo same"}},
			wantMetadata: map[string][]additiveSourceMetadata{"git@example.com:scopes.git": {
				{url: "git@example.com:scopes.git", ref: "main", source: config.UserPersonal, command: "echo same"},
				{url: "git@example.com:scopes.git", ref: "main", source: config.RepoCommitted, command: "echo same"},
			}},
		},
		{
			name: "required and optional",
			sources: []additiveSourceCase{
				{url: "git@example.com:required.git", ref: "main", source: config.UserPersonal, required: true, command: "echo required"},
				{url: "git@example.com:optional.git", ref: "main", source: config.RepoCommitted, selector: ks("PreToolUse-command"), command: "echo optional"},
			},
			wantURLs:     []string{"git@example.com:required.git", "git@example.com:optional.git"},
			wantCommands: map[string][]string{"git@example.com:required.git": {"echo required"}, "git@example.com:optional.git": {"echo optional"}},
			wantMetadata: map[string][]additiveSourceMetadata{
				"git@example.com:required.git": {{url: "git@example.com:required.git", ref: "main", source: config.UserPersonal, required: true, command: "echo required"}},
				"git@example.com:optional.git": {{url: "git@example.com:optional.git", ref: "main", source: config.RepoCommitted, command: "echo optional"}},
			},
		},
		{
			name: "same-scope required and optional",
			sources: []additiveSourceCase{
				{url: "git@example.com:required-same-scope.git", ref: "main", source: config.RepoCommitted, required: true, command: "echo required"},
				{url: "git@example.com:optional-same-scope.git", ref: "main", source: config.RepoCommitted, selector: ks("PreToolUse-command"), command: "echo optional"},
			},
			wantURLs:     []string{"git@example.com:required-same-scope.git", "git@example.com:optional-same-scope.git"},
			wantCommands: map[string][]string{"git@example.com:required-same-scope.git": {"echo required"}, "git@example.com:optional-same-scope.git": {"echo optional"}},
			wantMetadata: map[string][]additiveSourceMetadata{
				"git@example.com:required-same-scope.git": {{url: "git@example.com:required-same-scope.git", ref: "main", source: config.RepoCommitted, required: true, command: "echo required"}},
				"git@example.com:optional-same-scope.git": {{url: "git@example.com:optional-same-scope.git", ref: "main", source: config.RepoCommitted, required: false, command: "echo optional"}},
			},
		},
		{
			name: "per entry exclude",
			sources: []additiveSourceCase{
				{url: "git@example.com:first.git", ref: "main", source: config.RepoCommitted, selector: kse([]string{"PreToolUse-command"}, []string{"PreToolUse-command"}), command: "echo excluded"},
				{url: "git@example.com:second.git", ref: "main", source: config.RepoCommitted, selector: ks("PreToolUse-command"), command: "echo kept"},
			},
			wantURLs:     []string{"git@example.com:second.git"},
			wantCommands: map[string][]string{"git@example.com:second.git": {"echo kept"}},
			wantMetadata: map[string][]additiveSourceMetadata{"git@example.com:second.git": {{url: "git@example.com:second.git", ref: "main", source: config.RepoCommitted, command: "echo kept"}}},
		},
		{
			name: "per entry harness narrowing",
			sources: []additiveSourceCase{
				{url: "git@example.com:first.git", ref: "main", source: config.RepoCommitted, selector: ks("PreToolUse-command"), harnesses: []string{"factory"}, command: "echo factory"},
				{url: "git@example.com:second.git", ref: "main", source: config.RepoCommitted, selector: ks("PreToolUse-command"), harnesses: []string{"claude"}, command: "echo claude"},
			},
			wantURLs:     []string{"git@example.com:first.git", "git@example.com:second.git"},
			wantCommands: map[string][]string{"git@example.com:first.git": {"echo factory"}, "git@example.com:second.git": {"echo claude"}},
			wantMetadata: map[string][]additiveSourceMetadata{
				"git@example.com:first.git":  {{url: "git@example.com:first.git", ref: "main", source: config.RepoCommitted, command: "echo factory"}},
				"git@example.com:second.git": {{url: "git@example.com:second.git", ref: "main", source: config.RepoCommitted, command: "echo claude"}},
			},
			wantHarnesses: map[string][]hcc.Target{
				"git@example.com:first.git":  {hcc.TargetFactory},
				"git@example.com:second.git": {hcc.TargetClaude},
			},
		},
		{
			name: "non-hook command collision control",
			kind: ir.KindCommand,
			sources: []additiveSourceCase{
				{url: "git@example.com:first.git", ref: "main", source: config.RepoCommitted, selector: ks("shared-command"), command: "echo first"},
				{url: "git@example.com:second.git", ref: "main", source: config.RepoCommitted, selector: ks("shared-command"), command: "echo second"},
			},
			wantURLs:     []string{"git@example.com:first.git"},
			wantCommands: map[string][]string{"git@example.com:first.git": {"echo first"}},
			wantMetadata: map[string][]additiveSourceMetadata{"git@example.com:first.git": {{url: "git@example.com:first.git", ref: "main", source: config.RepoCommitted, command: "echo first"}}},
			wantWarnings: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind := tt.kind
			if kind == ir.KindSkill {
				kind = ir.KindHook
			}
			cfg := &config.Config{}
			for _, source := range tt.sources {
				entry := config.RegistryEntry{
					URL: source.url, Path: source.path, Ref: source.ref, Source: source.source,
					Required: source.required, Harnesses: source.harnesses,
				}
				if kind == ir.KindCommand {
					entry.Commands = source.selector
				} else {
					entry.Hooks = source.selector
				}
				cfg.Registries = append(cfg.Registries, entry)
			}
			setCatalogKeys(cfg)
			catalogs := make(map[string][]catalog.Entry, len(cfg.Registries))
			for i, entry := range cfg.Registries {
				name := "PreToolUse-command"
				resource := ir.Resource(ir.NewHook(name, "PreToolUse", tt.sources[i].command, "command", nil, ir.SourceRef{}))
				if kind == ir.KindCommand {
					name = "shared-command"
					resource = ir.NewCommand(name, "", tt.sources[i].command, nil, ir.SourceRef{})
				}
				catalogs[entry.CatalogKey] = append(catalogs[entry.CatalogKey], catalog.Entry{Kind: kind, Name: name, Resource: resource})
			}

			resolved, errs := Resolve(cfg, catalogs, nil)
			if len(resolved) != len(tt.wantURLs) {
				t.Fatalf("Resolve returned %d resources, want %d: %#v", len(resolved), len(tt.wantURLs), resolved)
			}
			if len(errs) != tt.wantWarnings {
				t.Fatalf("Resolve errors = %v, want %d", errs, tt.wantWarnings)
			}
			if tt.wantWarnings > 0 {
				var warning *NameCollisionWarning
				if !errors.As(errs[0], &warning) {
					t.Fatalf("Resolve error = %T, want NameCollisionWarning", errs[0])
				}
			}
			for _, artifact := range resolved {
				command := ""
				if kind == ir.KindCommand {
					command = artifact.Resource.(*ir.Command).Body
				} else {
					command = artifact.Resource.(*ir.Hook).Command
				}
				want := tt.wantCommands[artifact.RegistryURL]
				if len(want) == 0 {
					t.Errorf("no expected command for %s", artifact.RegistryURL)
					continue
				}
				wantCommand := want[0]
				tt.wantCommands[artifact.RegistryURL] = want[1:]
				if command != wantCommand {
					t.Errorf("%s %s command = %q, want %q", artifact.RegistryURL, artifact.RegistryPath, command, wantCommand)
				}
				assertAdditiveSourceMetadata(t, artifact, tt.wantMetadata, command)
				if want, ok := tt.wantHarnesses[artifact.RegistryURL]; ok && !slices.Equal(artifact.Harnesses, want) {
					t.Errorf("%s harnesses = %v, want %v", artifact.RegistryURL, artifact.Harnesses, want)
				}
			}
		})
	}
}
