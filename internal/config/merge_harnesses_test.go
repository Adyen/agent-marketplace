package config

import (
	"reflect"
	"testing"
)

func TestMergeLayersBackfillsMachineEntriesFromMachineHarnesses(t *testing.T) {
	entry := func(url string) RegistryEntry { return RegistryEntry{URL: url, Ref: "main"} }
	for _, tc := range []struct {
		name                  string
		user, org             []string
		wantMachine, wantRepo []string
	}{
		{name: "user list", user: []string{"factory"}, wantMachine: []string{"factory"}, wantRepo: []string{"claude"}},
		{name: "organization list", org: []string{"cursor"}, wantMachine: []string{"cursor"}, wantRepo: []string{"claude"}},
		{name: "repository fallback", wantMachine: []string{"claude"}, wantRepo: []string{"claude"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The organization entry declares no harnesses and neither does its
			// own layer unless the case sets one, so it is backfilled.
			org := &Config{Harnesses: tc.org, Registries: []RegistryEntry{entry("org")}}
			user := &Config{Harnesses: tc.user}
			repo := &Config{Harnesses: []string{"claude"}, Registries: []RegistryEntry{entry("repo")}}
			merged := MergeLayers([]Layer{
				{Source: Organization, Config: org},
				{Source: UserPersonal, Config: user},
				{Source: RepoCommitted, Config: repo},
			})
			got := map[Source][]string{}
			for _, registry := range merged.Registries {
				got[registry.Source] = registry.Harnesses
			}
			if !reflect.DeepEqual(got[Organization], tc.wantMachine) || !reflect.DeepEqual(got[RepoCommitted], tc.wantRepo) {
				t.Fatalf("organization entry = %v (want %v), repository entry = %v (want %v)", got[Organization], tc.wantMachine, got[RepoCommitted], tc.wantRepo)
			}
		})
	}
}

func TestMergeLayersPlaceOnlyMachineTargetsDoNotInheritRepositoryDefaults(t *testing.T) {
	for _, machineSource := range []Source{Organization, UserPersonal} {
		t.Run(machineSource.String(), func(t *testing.T) {
			machine := &Config{
				PlaceTargets: []string{"invented-app"},
				Registries:   []RegistryEntry{{URL: "machine", Ref: "main"}},
			}
			org, user := &Config{}, &Config{}
			if machineSource == Organization {
				org = machine
			} else {
				user = machine
			}
			repo := &Config{
				Harnesses:  []string{"claude"},
				Registries: []RegistryEntry{{URL: "repo", Ref: "main"}},
			}
			merged := MergeLayers([]Layer{
				{Source: Organization, Config: org},
				{Source: UserPersonal, Config: user},
				{Source: RepoCommitted, Config: repo},
			})
			got := map[Source][]string{}
			for _, registry := range merged.Registries {
				got[registry.Source] = registry.Harnesses
			}
			if len(got[machineSource]) != 0 {
				t.Fatalf("%s registry inherited repository targets: %v", machineSource, got[machineSource])
			}
			if !reflect.DeepEqual(got[RepoCommitted], []string{"claude"}) {
				t.Fatalf("repository registry targets = %v, want [claude]", got[RepoCommitted])
			}
		})
	}
}

func TestMergeLayersEmptyCompiledMachineDeclarationKeepsRepositoryFallback(t *testing.T) {
	org := &Config{Registries: []RegistryEntry{{URL: "org", Ref: "main"}}}
	user := &Config{HasTargets: true}
	repo := &Config{
		Harnesses:  []string{"claude"},
		Registries: []RegistryEntry{{URL: "repo", Ref: "main"}},
	}
	merged := MergeLayers([]Layer{
		{Source: Organization, Config: org},
		{Source: UserPersonal, Config: user},
		{Source: RepoCommitted, Config: repo},
	})
	for _, registry := range merged.Registries {
		if registry.Source == Organization && !reflect.DeepEqual(registry.Harnesses, []string{"claude"}) {
			t.Fatalf("organization registry targets = %v, want previous repository fallback", registry.Harnesses)
		}
	}
}
