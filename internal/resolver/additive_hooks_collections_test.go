package resolver

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestResolveAdditiveHooksThroughCollections(t *testing.T) {
	t.Run("direct and collection providers remain additive", func(t *testing.T) {
		a := "git@example.com:a.git"
		b := "git@example.com:b.git"
		cfg := &config.Config{Registries: []config.RegistryEntry{
			{URL: a, Ref: "main", Source: config.RepoCommitted, Hooks: ks("PreToolUse-command")},
			{URL: b, Ref: "main", Source: config.RepoCommitted, Collections: ks("guards")},
		}}
		setCatalogKeys(cfg)
		catalogs := map[string][]catalog.Entry{
			cfg.Registries[0].CatalogKey: {{Kind: ir.KindHook, Name: "PreToolUse-command", Resource: testHook("echo direct")}},
			cfg.Registries[1].CatalogKey: {
				{Kind: ir.KindCollection, Name: "guards", Resource: ir.NewCollection("guards", "", "", []ir.CollectionMember{{Name: "PreToolUse-command", Kind: ir.KindHook}}, nil, nil, ir.SourceRef{})},
				{Kind: ir.KindHook, Name: "PreToolUse-command", Resource: testHook("echo collection")},
			},
		}
		resolved, errs := Resolve(cfg, catalogs, nil)
		if len(errs) != 0 {
			t.Fatalf("Resolve errors = %v", errs)
		}
		assertHookProviders(t, resolved, map[string]hookExpectation{
			a: {command: "echo direct", via: ""},
			b: {command: "echo collection", via: "collection:guards"},
		})
	})

	t.Run("distinct collections remain additive", func(t *testing.T) {
		first := "git@example.com:first.git"
		second := "git@example.com:second.git"
		cfg := &config.Config{Registries: []config.RegistryEntry{
			{URL: first, Ref: "main", Source: config.RepoCommitted, Collections: ks("guards-first")},
			{URL: second, Ref: "main", Source: config.RepoCommitted, Collections: ks("guards-second")},
		}}
		setCatalogKeys(cfg)
		catalogs := map[string][]catalog.Entry{
			cfg.Registries[0].CatalogKey: {
				{Kind: ir.KindCollection, Name: "guards-first", Resource: collectionWithHook("guards-first")},
				{Kind: ir.KindHook, Name: "PreToolUse-command", Resource: testHook("echo first")},
			},
			cfg.Registries[1].CatalogKey: {
				{Kind: ir.KindCollection, Name: "guards-second", Resource: collectionWithHook("guards-second")},
				{Kind: ir.KindHook, Name: "PreToolUse-command", Resource: testHook("echo second")},
			},
		}
		resolved, errs := Resolve(cfg, catalogs, nil)
		if len(errs) != 0 {
			t.Fatalf("Resolve errors = %v", errs)
		}
		assertHookProviders(t, resolved, map[string]hookExpectation{
			first:  {command: "echo first", via: "collection:guards-first"},
			second: {command: "echo second", via: "collection:guards-second"},
		})
	})

	t.Run("direct provider wins same-source duplicate", func(t *testing.T) {
		url := "git@example.com:duplicate.git"
		cfg := &config.Config{Registries: []config.RegistryEntry{{
			URL: url, Ref: "main", Source: config.RepoCommitted,
			Hooks: ks("PreToolUse-command"), Collections: ks("guards"),
		}}}
		setCatalogKeys(cfg)
		catalogs := map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {
			{Kind: ir.KindCollection, Name: "guards", Resource: collectionWithHook("guards")},
			{Kind: ir.KindHook, Name: "PreToolUse-command", Resource: testHook("echo direct")},
		}}
		resolved, errs := Resolve(cfg, catalogs, nil)
		if len(errs) != 0 {
			t.Fatalf("Resolve errors = %v", errs)
		}
		assertHookProviders(t, resolved, map[string]hookExpectation{url: {command: "echo direct", via: ""}})
	})

	t.Run("same-source collections reconcile", func(t *testing.T) {
		url := "git@example.com:collections.git"
		cfg := &config.Config{Registries: []config.RegistryEntry{{
			URL: url, Ref: "main", Source: config.RepoCommitted, Collections: ks("guards-first", "guards-second"),
		}}}
		setCatalogKeys(cfg)
		catalogs := map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {
			{Kind: ir.KindCollection, Name: "guards-first", Resource: collectionWithHook("guards-first")},
			{Kind: ir.KindCollection, Name: "guards-second", Resource: collectionWithHook("guards-second")},
			{Kind: ir.KindHook, Name: "PreToolUse-command", Resource: testHook("echo first")},
		}}
		resolved, errs := Resolve(cfg, catalogs, nil)
		if len(errs) != 0 {
			t.Fatalf("Resolve errors = %v", errs)
		}
		if len(resolved) != 1 || resolved[0].Via != "collection:guards-first" {
			t.Fatalf("resolved = %#v, want first same-source collection provider", resolved)
		}
	})

	t.Run("different scopes remain additive", func(t *testing.T) {
		url := "git@example.com:scoped-collections.git"
		cfg := &config.Config{Registries: []config.RegistryEntry{
			{URL: url, Path: "machine", Ref: "main", Source: config.UserPersonal, Collections: ks("guards-machine")},
			{URL: url, Path: "repo", Ref: "main", Source: config.RepoCommitted, Collections: ks("guards-repo")},
		}}
		setCatalogKeys(cfg)
		catalogs := map[string][]catalog.Entry{
			cfg.Registries[0].CatalogKey: {
				{Kind: ir.KindCollection, Name: "guards-machine", Resource: collectionWithHook("guards-machine")},
				{Kind: ir.KindHook, Name: "PreToolUse-command", Resource: testHook("echo machine")},
			},
			cfg.Registries[1].CatalogKey: {
				{Kind: ir.KindCollection, Name: "guards-repo", Resource: collectionWithHook("guards-repo")},
				{Kind: ir.KindHook, Name: "PreToolUse-command", Resource: testHook("echo repo")},
			},
		}
		resolved, errs := Resolve(cfg, catalogs, nil)
		if len(errs) != 0 {
			t.Fatalf("Resolve errors = %v", errs)
		}
		if len(resolved) != 2 {
			t.Fatalf("resolved = %#v, want two scoped providers", resolved)
		}
		for _, artifact := range resolved {
			hook, ok := artifact.Resource.(*ir.Hook)
			if !ok {
				t.Fatalf("resolved provider resource = %T, want hook", artifact.Resource)
			}
			switch artifact.RegistryPath {
			case "machine":
				if artifact.Source != config.UserPersonal || artifact.Ref != "main" || hook.Command != "echo machine" {
					t.Fatalf("machine provider = %#v command=%q, want UserPersonal/main/echo machine", artifact, hook.Command)
				}
			case "repo":
				if artifact.Source != config.RepoCommitted || artifact.Ref != "main" || hook.Command != "echo repo" {
					t.Fatalf("repo provider = %#v command=%q, want RepoCommitted/main/echo repo", artifact, hook.Command)
				}
			default:
				t.Fatalf("resolved provider metadata = %#v, want machine or repo path", artifact)
			}
		}
	})

	t.Run("missing hook member is not suppressed", func(t *testing.T) {
		a := "git@example.com:present.git"
		b := "git@example.com:missing.git"
		cfg := &config.Config{Registries: []config.RegistryEntry{
			{URL: a, Ref: "main", Source: config.RepoCommitted, Hooks: ks("PreToolUse-command")},
			{URL: b, Ref: "main", Source: config.RepoCommitted, Collections: ks("guards")},
		}}
		setCatalogKeys(cfg)
		catalogs := map[string][]catalog.Entry{
			cfg.Registries[0].CatalogKey: {{Kind: ir.KindHook, Name: "PreToolUse-command", Resource: testHook("echo present")}},
			cfg.Registries[1].CatalogKey: {{Kind: ir.KindCollection, Name: "guards", Resource: collectionWithHook("guards")}},
		}
		resolved, errs := Resolve(cfg, catalogs, nil)
		if len(resolved) != 1 {
			t.Fatalf("resolved = %#v, want present direct hook", resolved)
		}
		var missing *SelectedNotFoundError
		if len(errs) != 1 || !errors.As(errs[0], &missing) || missing.Kind != ir.KindHook || missing.URL != b {
			t.Fatalf("errors = %v, want hook SelectedNotFoundError for %s", errs, b)
		}
	})

	t.Run("excluded collection source does not reappear", func(t *testing.T) {
		a := "git@example.com:present.git"
		b := "git@example.com:excluded.git"
		cfg := &config.Config{Registries: []config.RegistryEntry{
			{URL: a, Ref: "main", Source: config.RepoCommitted, Hooks: ks("PreToolUse-command")},
			{URL: b, Ref: "main", Source: config.RepoCommitted, Hooks: kse(nil, []string{"PreToolUse-command"}), Collections: ks("guards")},
		}}
		setCatalogKeys(cfg)
		catalogs := map[string][]catalog.Entry{
			cfg.Registries[0].CatalogKey: {{Kind: ir.KindHook, Name: "PreToolUse-command", Resource: testHook("echo present")}},
			cfg.Registries[1].CatalogKey: {
				{Kind: ir.KindCollection, Name: "guards", Resource: collectionWithHook("guards")},
				{Kind: ir.KindHook, Name: "PreToolUse-command", Resource: testHook("echo excluded")},
			},
		}
		resolved, errs := Resolve(cfg, catalogs, nil)
		if len(errs) != 0 || len(resolved) != 1 || resolved[0].RegistryURL != a {
			t.Fatalf("resolved = %#v errors = %v, want only present provider", resolved, errs)
		}
	})
}

type hookExpectation struct {
	command string
	via     string
}

func assertHookProviders(t *testing.T, resolved []ResolvedArtifact, want map[string]hookExpectation) {
	t.Helper()
	if len(resolved) != len(want) {
		t.Fatalf("resolved %d artifacts, want %d: %#v", len(resolved), len(want), resolved)
	}
	for _, artifact := range resolved {
		expectation, ok := want[artifact.RegistryURL]
		if !ok {
			t.Fatalf("unexpected provider %q: %#v", artifact.RegistryURL, artifact)
		}
		hook, ok := artifact.Resource.(*ir.Hook)
		if !ok || hook.Command != expectation.command || artifact.Via != expectation.via {
			t.Errorf("provider %q = resource %T command %q via %q, want command %q via %q", artifact.RegistryURL, artifact.Resource, hookCommand(artifact), artifact.Via, expectation.command, expectation.via)
		}
		delete(want, artifact.RegistryURL)
	}
	if len(want) != 0 {
		t.Errorf("missing providers: %v", want)
	}
}

func hookCommand(artifact ResolvedArtifact) string {
	if hook, ok := artifact.Resource.(*ir.Hook); ok {
		return hook.Command
	}
	return ""
}

func testHook(command string) *ir.Hook {
	return ir.NewHookWithMatcher("PreToolUse-command", "PreToolUse", "Bash", command, "command", nil, ir.SourceRef{})
}

func collectionWithHook(name string) *ir.Collection {
	return ir.NewCollection(name, "", "", []ir.CollectionMember{{Name: "PreToolUse-command", Kind: ir.KindHook}}, nil, nil, ir.SourceRef{})
}

func TestResolveAdditiveHooksPreservesInterleavedOrder(t *testing.T) {
	for _, test := range []struct {
		name               string
		direct             bool
		requiredCollection bool
	}{
		{name: "collection only"},
		{name: "collection before direct duplicate", direct: true},
		{name: "required collection outranks optional direct duplicate", direct: true, requiredCollection: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, b, c := "git@example.com:collection.git", "git@example.com:middle.git", "git@example.com:independent.git"
			cfg := &config.Config{Registries: []config.RegistryEntry{
				{URL: a, Ref: "main", Source: config.RepoCommitted, Required: test.requiredCollection, Collections: ks("guards")},
				{URL: b, Ref: "main", Source: config.RepoCommitted, Skills: ks("middle")},
			}}
			if test.direct {
				cfg.Registries = append(cfg.Registries, config.RegistryEntry{URL: a, Ref: "main", Source: config.RepoCommitted, Hooks: ks("PreToolUse-command")})
			}
			cfg.Registries = append(cfg.Registries, config.RegistryEntry{URL: c, Ref: "main", Source: config.RepoCommitted, Hooks: ks("PreToolUse-command")})
			setCatalogKeys(cfg)
			catalogs := map[string][]catalog.Entry{
				ck(a, "main"): {
					{Kind: ir.KindCollection, Name: "guards", Resource: collectionWithHook("guards")},
					{Kind: ir.KindHook, Name: "PreToolUse-command", Resource: testHook("echo shared")},
				},
				ck(b, "main"): {{Kind: ir.KindSkill, Name: "middle", Resource: ir.NewSkill("middle", "", "body", nil, nil, ir.SourceRef{})}},
				ck(c, "main"): {{Kind: ir.KindHook, Name: "PreToolUse-command", Resource: testHook("echo independent")}},
			}
			resolved, errs := Resolve(cfg, catalogs, nil)
			if len(errs) != 0 {
				t.Fatalf("Resolve errors: %v", errs)
			}
			var got []string
			for _, artifact := range resolved {
				got = append(got, artifact.Kind.String()+":"+artifact.Name+"|"+artifact.RegistryURL+"|"+artifact.Via)
				if artifact.Kind == ir.KindHook {
					if artifact.RegistryURL == a && artifact.Required != test.requiredCollection {
						t.Fatalf("collection hook required=%v, want %v", artifact.Required, test.requiredCollection)
					}
					wantCommand := "echo shared"
					if artifact.RegistryURL == c {
						wantCommand = "echo independent"
					}
					if h, ok := artifact.Resource.(*ir.Hook); !ok || h.Command != wantCommand {
						t.Fatalf("wrong hook payload for %s: %#v", artifact.RegistryURL, artifact.Resource)
					}
				}
			}
			want := []string{
				"hook:PreToolUse-command|git@example.com:collection.git|collection:guards",
				"skill:middle|git@example.com:middle.git|",
				"hook:PreToolUse-command|git@example.com:independent.git|",
			}
			if test.direct && !test.requiredCollection {
				want = []string{
					"skill:middle|git@example.com:middle.git|",
					"hook:PreToolUse-command|git@example.com:collection.git|",
					"hook:PreToolUse-command|git@example.com:independent.git|",
				}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("resolved order/providers = %v, want %v", got, want)
			}
		})
	}
}
