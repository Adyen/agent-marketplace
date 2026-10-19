package resolver

import (
	"sort"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/plugindecode"
)

// plannedKey renders one planned plugin for order-insensitive comparison:
// name, sorted member keys, and the sorted set of selection edges.
func plannedKey(p catalog.PlannedPlugin) string {
	members := append([]string(nil), p.Members...)
	sort.Strings(members)
	var edges []string
	for _, edge := range p.Edges {
		rendered := string(edge.Type) + ":" + edge.Collection
		if edge.Scope != config.SourceUnset {
			rendered += "@" + edge.Scope.String()
		}
		edges = append(edges, rendered)
	}
	sort.Strings(edges)
	return p.Name + "|" + strings.Join(members, ",") + "|" + strings.Join(edges, ",")
}

func collectionEntry(name string, members []ir.CollectionMember, includes []string) catalog.Entry {
	return catalog.Entry{Kind: ir.KindCollection, Name: name, Resource: ir.NewCollection(name, "", "", members, includes, nil, ir.SourceRef{})}
}

func collectionDecisions(name string, opts config.V4ResourceOptions) config.V4SelectionDecision {
	return config.V4SelectionDecision{
		Action:  config.V4SelectionExplicit,
		Locator: "collection/" + name,
		Request: &config.V4ResourceRequest{Kind: "collection", Name: name, Options: opts},
	}
}

// The lock's plugin-v1 selection input is the resolver's own selection:
// PlanPluginSelection must report the exact plugin set, member keys, and
// selection edges each case's semantics produce. want lists each planned
// plugin as "name|member-keys|edges" so a resolver regression is caught
// here, not mirrored.
func TestPlanPluginSelectionMatchesResolve(t *testing.T) {
	kitEntry := func(decisions []config.V4SelectionDecision) config.RegistryEntry {
		return formatEntry("claude-marketplace", decisions)
	}
	reviewPlugin := pluginCatalogEntry("review-kit", "claude-plugin",
		pluginMembers([2]string{"skill", "review"}, [2]string{"command", "legacy-review"}), nil,
		plugindecode.CatalogSource{Kind: "path", Path: "plugins/review-kit"})
	lintPlugin := pluginCatalogEntry("lint-kit", "claude-plugin",
		pluginMembers([2]string{"skill", "lint"}), nil,
		plugindecode.CatalogSource{Kind: "path", Path: "plugins/lint-kit"})
	teamCollection := collectionEntry("team",
		[]ir.CollectionMember{{Name: "review-kit", Kind: ir.KindPlugin, Registry: "git@example.com:kit.git"}}, nil)

	reviewAll := "review-kit|command/legacy-review,skill/review|direct:"
	reviewViaCollection := "review-kit|command/legacy-review,skill/review|collection:team"

	cases := []struct {
		name     string
		cfg      func() *config.Config
		catalogs func(cfg *config.Config) map[string][]catalog.Entry
		localCfg *config.LocalConfig
		want     []string
		wantErr  string
	}{
		{name: "all-selects-every-plugin", cfg: func() *config.Config {
			entry := kitEntry(nil)
			entry.V4All = true
			return &config.Config{Registries: []config.RegistryEntry{entry}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			return map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {reviewPlugin, lintPlugin}}
		}, want: []string{"lint-kit|skill/lint|direct:", reviewAll}},
		{name: "tags-select-matching-plugin", cfg: func() *config.Config {
			entry := kitEntry(nil)
			entry.Tags = []string{"security"}
			return &config.Config{Registries: []config.RegistryEntry{entry}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			tagged := reviewPlugin
			tagged.Tags = []string{"security"}
			return map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {tagged, lintPlugin}}
		}, want: []string{reviewAll}},
		{name: "required-selects-plugin", cfg: func() *config.Config {
			entry := kitEntry(nil)
			entry.Required = true
			return &config.Config{Registries: []config.RegistryEntry{entry}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			return map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {reviewPlugin}}
		}, want: []string{reviewAll}},
		{name: "default-selects-plugin", cfg: func() *config.Config {
			entry := kitEntry(nil)
			entry.Default = true
			return &config.Config{Registries: []config.RegistryEntry{entry}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			return map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {reviewPlugin}}
		}, want: []string{reviewAll}},
		{name: "explicit-plugin", cfg: func() *config.Config {
			return &config.Config{Registries: []config.RegistryEntry{kitEntry(pluginDecisions([2]string{"plugin", "review-kit"}))}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			return map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {reviewPlugin, lintPlugin}}
		}, want: []string{reviewAll}},
		{name: "ordered-member-excludes", cfg: func() *config.Config {
			decision := pluginRequestDecision("review-kit", config.V4ResourceOptions{Exclude: []string{"command/legacy-review"}})
			return &config.Config{Registries: []config.RegistryEntry{kitEntry([]config.V4SelectionDecision{decision})}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			return map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {reviewPlugin}}
		}, want: []string{"review-kit|skill/review|direct:"}},
		{name: "all-with-group-wildcard-exclusion", cfg: func() *config.Config {
			entry := kitEntry([]config.V4SelectionDecision{{Action: config.V4SelectionExclude, Locator: "*", Exclusion: "*"}})
			entry.V4All = true
			return &config.Config{Registries: []config.RegistryEntry{entry}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			return map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {reviewPlugin, lintPlugin}}
		}},
		{name: "all-with-group-kind-wildcard-exclusion", cfg: func() *config.Config {
			entry := kitEntry([]config.V4SelectionDecision{{Action: config.V4SelectionExclude, Locator: "plugin/*", Exclusion: "plugin/*"}})
			entry.V4All = true
			return &config.Config{Registries: []config.RegistryEntry{entry}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			return map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {reviewPlugin, lintPlugin}}
		}},
		{name: "exclude-then-include-reselects", cfg: func() *config.Config {
			entry := kitEntry([]config.V4SelectionDecision{
				{Action: config.V4SelectionExclude, Locator: "plugin/review-kit", Exclusion: "plugin/review-kit"},
				{Action: config.V4SelectionExplicit, Locator: "plugin/review-kit", Request: &config.V4ResourceRequest{Kind: "plugin", Name: "review-kit"}},
			})
			return &config.Config{Registries: []config.RegistryEntry{entry}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			return map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {reviewPlugin, lintPlugin}}
		}, want: []string{reviewAll}},
		{name: "collection-via-explicit-decision", cfg: func() *config.Config {
			colEntry := config.RegistryEntry{URL: "git@example.com:collections.git", Ref: "main", V4Decisions: []config.V4SelectionDecision{collectionDecisions("team", config.V4ResourceOptions{})}}
			return &config.Config{Registries: []config.RegistryEntry{colEntry, kitEntry(nil)}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			return map[string][]catalog.Entry{
				cfg.Registries[0].CatalogKey: {teamCollection},
				cfg.Registries[1].CatalogKey: {reviewPlugin},
			}
		}, want: []string{reviewViaCollection}},
		{name: "collection-via-all", cfg: func() *config.Config {
			colEntry := config.RegistryEntry{URL: "git@example.com:collections.git", Ref: "main", V4All: true}
			return &config.Config{Registries: []config.RegistryEntry{colEntry, kitEntry(nil)}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			return map[string][]catalog.Entry{
				cfg.Registries[0].CatalogKey: {teamCollection},
				cfg.Registries[1].CatalogKey: {reviewPlugin},
			}
		}, want: []string{reviewViaCollection}},
		{name: "collection-member-ref-pin-binds-matching-entry", cfg: func() *config.Config {
			colEntry := config.RegistryEntry{URL: "git@example.com:collections.git", Ref: "main", Collections: ks("team")}
			return &config.Config{Registries: []config.RegistryEntry{colEntry, kitEntry(nil)}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			pinned := collectionEntry("team",
				[]ir.CollectionMember{{Name: "review-kit", Kind: ir.KindPlugin, Registry: "git@example.com:kit.git", Ref: "main"}}, nil)
			return map[string][]catalog.Entry{
				cfg.Registries[0].CatalogKey: {pinned},
				cfg.Registries[1].CatalogKey: {reviewPlugin},
			}
		}, want: []string{reviewViaCollection}},
		{name: "collection-member-ref-pin-misses-differing-ref", cfg: func() *config.Config {
			colEntry := config.RegistryEntry{URL: "git@example.com:collections.git", Ref: "main", Collections: ks("team")}
			entry := kitEntry(nil)
			entry.Ref = "next"
			return &config.Config{Registries: []config.RegistryEntry{colEntry, entry}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			pinned := collectionEntry("team",
				[]ir.CollectionMember{{Name: "review-kit", Kind: ir.KindPlugin, Registry: "git@example.com:kit.git", Ref: "main"}}, nil)
			return map[string][]catalog.Entry{
				cfg.Registries[0].CatalogKey: {pinned},
				cfg.Registries[1].CatalogKey: {reviewPlugin},
			}
		}},
		{name: "collection-member-path-pin-misses-differing-path", cfg: func() *config.Config {
			colEntry := config.RegistryEntry{URL: "git@example.com:collections.git", Ref: "main", Collections: ks("team")}
			return &config.Config{Registries: []config.RegistryEntry{colEntry, kitEntry(nil)}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			pinned := collectionEntry("team",
				[]ir.CollectionMember{{Name: "review-kit", Kind: ir.KindPlugin, Registry: "git@example.com:kit.git", Path: "other"}}, nil)
			return map[string][]catalog.Entry{
				cfg.Registries[0].CatalogKey: {pinned},
				cfg.Registries[1].CatalogKey: {reviewPlugin},
			}
		}},
		{name: "local-config-excludes-collection", cfg: func() *config.Config {
			colEntry := config.RegistryEntry{URL: "git@example.com:collections.git", Ref: "main", Collections: ks("team")}
			return &config.Config{Registries: []config.RegistryEntry{colEntry, kitEntry(nil)}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			return map[string][]catalog.Entry{
				cfg.Registries[0].CatalogKey: {teamCollection},
				cfg.Registries[1].CatalogKey: {reviewPlugin},
			}
		}, localCfg: &config.LocalConfig{Collections: config.KindSelector{ExcludeAll: true}}},
		{name: "top-level-collection-exclude", cfg: func() *config.Config {
			colEntry := config.RegistryEntry{URL: "git@example.com:collections.git", Ref: "main", Collections: ks("team")}
			return &config.Config{Registries: []config.RegistryEntry{colEntry, kitEntry(nil)}, Collections: config.KindSelector{Exclude: []string{"team"}}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			return map[string][]catalog.Entry{
				cfg.Registries[0].CatalogKey: {teamCollection},
				cfg.Registries[1].CatalogKey: {reviewPlugin},
			}
		}},
		{name: "nested-collection-includes", cfg: func() *config.Config {
			colEntry := config.RegistryEntry{URL: "git@example.com:collections.git", Ref: "main", Collections: ks("outer")}
			return &config.Config{Registries: []config.RegistryEntry{colEntry, kitEntry(nil)}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			outer := collectionEntry("outer", nil, []string{"inner"})
			inner := collectionEntry("inner",
				[]ir.CollectionMember{{Name: "review-kit", Kind: ir.KindPlugin, Registry: "git@example.com:kit.git"}}, nil)
			return map[string][]catalog.Entry{
				cfg.Registries[0].CatalogKey: {outer, inner},
				cfg.Registries[1].CatalogKey: {reviewPlugin},
			}
		}, want: []string{"review-kit|command/legacy-review,skill/review|collection:inner"}},
		{name: "nested-collection-cycle", cfg: func() *config.Config {
			colEntry := config.RegistryEntry{URL: "git@example.com:collections.git", Ref: "main", Collections: ks("a")}
			return &config.Config{Registries: []config.RegistryEntry{colEntry, kitEntry(nil)}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			a := collectionEntry("a", nil, []string{"b"})
			b := collectionEntry("b", []ir.CollectionMember{{Name: "review-kit", Kind: ir.KindPlugin, Registry: "git@example.com:kit.git"}}, []string{"a"})
			return map[string][]catalog.Entry{
				cfg.Registries[0].CatalogKey: {a, b},
				cfg.Registries[1].CatalogKey: {reviewPlugin},
			}
		}, want: []string{"review-kit|command/legacy-review,skill/review|collection:b"}},
		{name: "duplicate-collection-definition-authority", cfg: func() *config.Config {
			first := config.RegistryEntry{URL: "git@example.com:collections.git", Ref: "main", Collections: ks("team"), Source: config.Organization}
			second := config.RegistryEntry{URL: "git@example.com:other.git", Ref: "main", Collections: ks("team"), Source: config.RepoCommitted}
			kitOrg := kitEntry(nil)
			kitOrg.Source = config.Organization
			kitRepo := kitEntry(nil)
			kitRepo.Source = config.RepoCommitted
			return &config.Config{Registries: []config.RegistryEntry{first, second, kitOrg, kitRepo}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			other := collectionEntry("team",
				[]ir.CollectionMember{{Name: "lint-kit", Kind: ir.KindPlugin, Registry: "git@example.com:kit.git"}}, nil)
			return map[string][]catalog.Entry{
				cfg.Registries[0].CatalogKey: {teamCollection},
				cfg.Registries[1].CatalogKey: {other},
				cfg.Registries[2].CatalogKey: {reviewPlugin, lintPlugin},
			}
		}, want: []string{"lint-kit|skill/lint|collection:team@REPO", "review-kit|command/legacy-review,skill/review|collection:team@ORG"}},
		{name: "collection-member-undeclared-format-source-refused", cfg: func() *config.Config {
			colEntry := config.RegistryEntry{URL: "git@example.com:collections.git", Ref: "main", Source: config.UserPersonal, Collections: ks("team")}
			kit := kitEntry(nil)
			kit.Source = config.RepoCommitted
			return &config.Config{Registries: []config.RegistryEntry{colEntry, kit}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			return map[string][]catalog.Entry{
				cfg.Registries[0].CatalogKey: {teamCollection},
				cfg.Registries[1].CatalogKey: {reviewPlugin},
			}
		}, wantErr: "plugin.selection.undeclared-source"},
		{name: "required-collection-missing-member-errors", cfg: func() *config.Config {
			colEntry := config.RegistryEntry{URL: "git@example.com:collections.git", Ref: "main", Collections: ks("team"), Required: true}
			return &config.Config{Registries: []config.RegistryEntry{colEntry, kitEntry(nil)}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			broken := collectionEntry("team",
				[]ir.CollectionMember{{Name: "ghost-kit", Kind: ir.KindPlugin, Registry: "git@example.com:kit.git"}}, nil)
			return map[string][]catalog.Entry{
				cfg.Registries[0].CatalogKey: {broken},
				cfg.Registries[1].CatalogKey: {reviewPlugin},
			}
		}, wantErr: "ghost-kit"},
		{name: "cross-layer-edge-provenance", cfg: func() *config.Config {
			repo := kitEntry(pluginDecisions([2]string{"plugin", "review-kit"}))
			repo.Source = config.RepoCommitted
			user := kitEntry(pluginDecisions([2]string{"plugin", "lint-kit"}))
			user.Source = config.UserPersonal
			return &config.Config{Registries: []config.RegistryEntry{repo, user}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			return map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {reviewPlugin, lintPlugin}}
		}, want: []string{"lint-kit|skill/lint|direct:@USER", "review-kit|command/legacy-review,skill/review|direct:@REPO"}},
		{name: "collection-edge-keeps-selecting-layer", cfg: func() *config.Config {
			colEntry := config.RegistryEntry{URL: "git@example.com:collections.git", Ref: "main", Source: config.UserPersonal, Collections: ks("team")}
			kitRepo := kitEntry(nil)
			kitRepo.Source = config.RepoCommitted
			kitUser := kitEntry(nil)
			kitUser.Source = config.UserPersonal
			return &config.Config{Registries: []config.RegistryEntry{colEntry, kitRepo, kitUser}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			return map[string][]catalog.Entry{
				cfg.Registries[0].CatalogKey: {teamCollection},
				cfg.Registries[1].CatalogKey: {reviewPlugin},
			}
		}, want: []string{"review-kit|command/legacy-review,skill/review|collection:team@USER"}},
		{name: "cross-layer-same-source-both-select", cfg: func() *config.Config {
			org := kitEntry(pluginDecisions([2]string{"plugin", "review-kit"}))
			org.Source = config.Organization
			user := kitEntry(pluginDecisions([2]string{"plugin", "review-kit"}))
			user.Source = config.UserPersonal
			return &config.Config{Registries: []config.RegistryEntry{org, user}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			return map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {reviewPlugin}}
		}, want: []string{
			"review-kit|command/legacy-review,skill/review|direct:@ORG",
			"review-kit|command/legacy-review,skill/review|direct:@USER",
		}},
		{name: "per-layer-member-exclusions", cfg: func() *config.Config {
			org := kitEntry([]config.V4SelectionDecision{pluginRequestDecision("review-kit", config.V4ResourceOptions{Exclude: []string{"command/legacy-review"}})})
			org.Source = config.Organization
			user := kitEntry(pluginDecisions([2]string{"plugin", "review-kit"}))
			user.Source = config.UserPersonal
			return &config.Config{Registries: []config.RegistryEntry{org, user}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			return map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {reviewPlugin}}
		}, want: []string{
			"review-kit|command/legacy-review,skill/review|direct:@USER",
			"review-kit|skill/review|direct:@ORG",
		}},
		{name: "same-layer-duplicate-entries-differing-excludes", cfg: func() *config.Config {
			excluding := kitEntry([]config.V4SelectionDecision{pluginRequestDecision("review-kit", config.V4ResourceOptions{Exclude: []string{"command/legacy-review"}})})
			plain := kitEntry(pluginDecisions([2]string{"plugin", "review-kit"}))
			return &config.Config{Registries: []config.RegistryEntry{excluding, plain}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			return map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {reviewPlugin}}
		}, want: []string{"review-kit|skill/review|direct:"}},
		{name: "collection-edges-in-two-layers", cfg: func() *config.Config {
			orgCol := config.RegistryEntry{URL: "git@example.com:collections.git", Ref: "main", Source: config.Organization, Collections: ks("orgteam")}
			userCol := config.RegistryEntry{URL: "git@example.com:other.git", Ref: "main", Source: config.UserPersonal, Collections: ks("userteam")}
			kitOrg := kitEntry(nil)
			kitOrg.Source = config.Organization
			kitUser := kitEntry(nil)
			kitUser.Source = config.UserPersonal
			return &config.Config{Registries: []config.RegistryEntry{orgCol, userCol, kitOrg, kitUser}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			orgTeam := collectionEntry("orgteam",
				[]ir.CollectionMember{{Name: "review-kit", Kind: ir.KindPlugin, Registry: "git@example.com:kit.git"}}, nil)
			userTeam := collectionEntry("userteam",
				[]ir.CollectionMember{{Name: "review-kit", Kind: ir.KindPlugin, Registry: "git@example.com:kit.git"}}, nil)
			return map[string][]catalog.Entry{
				cfg.Registries[0].CatalogKey: {orgTeam},
				cfg.Registries[1].CatalogKey: {userTeam},
				cfg.Registries[2].CatalogKey: {reviewPlugin},
			}
		}, want: []string{
			"review-kit|command/legacy-review,skill/review|collection:orgteam@ORG",
			"review-kit|command/legacy-review,skill/review|collection:userteam@USER",
		}},
		{name: "collided-plugin-selection-still-planned", cfg: func() *config.Config {
			rival := kitEntry(pluginDecisions([2]string{"plugin", "dup-kit"}))
			rival.URL = "git@example.com:rival.git"
			kit := kitEntry(pluginDecisions([2]string{"plugin", "dup-kit"}))
			return &config.Config{Registries: []config.RegistryEntry{rival, kit}}
		}, catalogs: func(cfg *config.Config) map[string][]catalog.Entry {
			dup := pluginCatalogEntry("dup-kit", "claude-plugin",
				pluginMembers([2]string{"skill", "dup"}), nil,
				plugindecode.CatalogSource{Kind: "path", Path: "plugins/dup-kit"})
			return map[string][]catalog.Entry{
				cfg.Registries[0].CatalogKey: {dup},
				cfg.Registries[1].CatalogKey: {dup},
			}
		}, want: []string{
			"dup-kit|skill/dup|direct:",
			"dup-kit|skill/dup|direct:",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg()
			setCatalogKeys(cfg)
			catalogs := tc.catalogs(cfg)
			plan, planErrs := PlanPluginSelection(cfg, catalogs, tc.localCfg)
			if tc.wantErr != "" {
				if !strings.Contains(errStrings(planErrs), tc.wantErr) {
					t.Fatalf("plan errors %v do not name %q", planErrs, tc.wantErr)
				}
				return
			}
			var planned []string
			for _, p := range plan.Plugins {
				planned = append(planned, plannedKey(p))
			}
			sort.Strings(planned)
			if strings.Join(planned, ";") != strings.Join(tc.want, ";") {
				t.Fatalf("plan = %v, want %v (errs=%v)", planned, tc.want, planErrs)
			}
		})
	}
}

// The plan binds each planned plugin to the format source that owns its
// catalog, so per-root manifests carry exactly that root's selection.
func TestPlanPluginSelectionForEntry(t *testing.T) {
	entry := formatEntry("claude-marketplace", pluginDecisions([2]string{"plugin", "review-kit"}))
	other := formatEntry("claude-marketplace", pluginDecisions([2]string{"plugin", "lint-kit"}))
	other.URL = "git@example.com:other.git"
	cfg := &config.Config{Registries: []config.RegistryEntry{entry, other}}
	setCatalogKeys(cfg)
	reviewPlugin := pluginCatalogEntry("review-kit", "claude-plugin",
		pluginMembers([2]string{"skill", "review"}), nil, plugindecode.CatalogSource{Kind: "path", Path: "plugins/review-kit"})
	lintPlugin := pluginCatalogEntry("lint-kit", "claude-plugin",
		pluginMembers([2]string{"skill", "lint"}), nil, plugindecode.CatalogSource{Kind: "path", Path: "plugins/lint-kit"})
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {reviewPlugin},
		cfg.Registries[1].CatalogKey: {lintPlugin},
	}

	plan, errs := PlanPluginSelection(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("plan errors: %v", errs)
	}
	first := plan.ForEntry(cfg.Registries[0], config.RepoCommitted)
	if len(first) != 1 || first[0].Name != "review-kit" {
		t.Fatalf("first entry plan = %#v", first)
	}
	second := plan.ForEntry(cfg.Registries[1], config.RepoCommitted)
	if len(second) != 1 || second[0].Name != "lint-kit" {
		t.Fatalf("second entry plan = %#v", second)
	}
	externals := plan.ExternalEntries()
	if len(externals) != 0 {
		t.Fatalf("in-path plugins are not external entries: %#v", externals)
	}
}

// Two layers declaring the same format source location and selecting the
// same plugin each get their own planned record: ForEntry returns exactly
// the layer's own selection for that entry location.
func TestPlanPluginSelectionForEntryPerLayer(t *testing.T) {
	org := formatEntry("claude-marketplace", pluginDecisions([2]string{"plugin", "review-kit"}))
	org.Source = config.Organization
	user := formatEntry("claude-marketplace", pluginDecisions([2]string{"plugin", "review-kit"}))
	user.Source = config.UserPersonal
	cfg := &config.Config{Registries: []config.RegistryEntry{org, user}}
	setCatalogKeys(cfg)
	reviewPlugin := pluginCatalogEntry("review-kit", "claude-plugin",
		pluginMembers([2]string{"skill", "review"}), nil, plugindecode.CatalogSource{Kind: "path", Path: "plugins/review-kit"})
	catalogs := map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {reviewPlugin}}

	plan, errs := PlanPluginSelection(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("plan errors: %v", errs)
	}
	orgPlan := plan.ForEntry(cfg.Registries[0], config.Organization)
	if len(orgPlan) != 1 || orgPlan[0].Name != "review-kit" || len(orgPlan[0].Edges) != 1 || orgPlan[0].Edges[0].Type != PluginEdgeDirect {
		t.Fatalf("organization plan = %#v", orgPlan)
	}
	userPlan := plan.ForEntry(cfg.Registries[1], config.UserPersonal)
	if len(userPlan) != 1 || userPlan[0].Name != "review-kit" || len(userPlan[0].Edges) != 1 || userPlan[0].Edges[0].Type != PluginEdgeDirect {
		t.Fatalf("user plan = %#v", userPlan)
	}
	if repoPlan := plan.ForEntry(cfg.Registries[0], config.RepoCommitted); len(repoPlan) != 0 {
		t.Fatalf("a layer that authored no selection must get an empty plan: %#v", repoPlan)
	}
}

// External catalog entries surface in the plan as external entries carrying
// their source record, ready for the lock's per-origin acquisition.
func TestPlanPluginSelectionExternalEntries(t *testing.T) {
	entry := formatEntry("claude-marketplace", pluginDecisions([2]string{"plugin", "mirror-kit"}))
	cfg := &config.Config{Registries: []config.RegistryEntry{entry}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {
		pluginCatalogEntry("mirror-kit", "claude-plugin", nil, nil,
			plugindecode.CatalogSource{Kind: "git", URL: "https://git.example.com/mirror.git", Ref: "main"}),
	}}

	plan, errs := PlanPluginSelection(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("plan errors: %v", errs)
	}
	externals := plan.ExternalEntries()
	if len(externals) != 1 || externals[0].Name != "mirror-kit" {
		t.Fatalf("external entries = %#v", externals)
	}
	if externals[0].Entry.Plugin.Source.URL != "https://git.example.com/mirror.git" {
		t.Fatalf("external source = %#v", externals[0].Entry.Plugin.Source)
	}
}
