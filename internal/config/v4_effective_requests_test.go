package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"pgregory.net/rapid"
)

func TestV4EffectiveRequestsPreserveDecisions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		actions []string
		want    []string
	}{
		{"empty", nil, []string{}},
		{"include", []string{"include:skill/a"}, []string{"skill/a"}},
		{"exact", []string{"include:skill/a", "include:command/a", "exclude:skill/a"}, []string{"command/a"}},
		{"required", []string{"required:skill/a", "exclude:skill/a"}, []string{"skill/a"}},
		{"replacement", []string{"required:skill/a", "include:skill/a", "exclude:skill/a"}, []string{}},
		{"reincluded", []string{"include:skill/a", "exclude:skill/a", "include:skill/a"}, []string{"skill/a"}},
		{"future", []string{"exclude:skill/a", "include:skill/a"}, []string{"skill/a"}},
		{"missing", []string{"include:skill/a", "exclude:skill/missing"}, []string{"skill/a"}},
		{"kind-is-distinct", []string{"include:skill/a", "exclude:command/a"}, []string{"skill/a"}},
		{"registry-is-distinct", []string{"include:tools/skill/a", "exclude:other/skill/a"}, []string{"tools/skill/a"}},
		{"global", []string{"include:skill/a", "required:command/a", "exclude:*"}, []string{"command/a"}},
		{"kind", []string{"include:skill/a", "include:command/a", "exclude:skill/*"}, []string{"command/a"}},
		{"registry", []string{"include:tools/skill/a", "include:other/skill/a", "exclude:tools/*"}, []string{"other/skill/a"}},
		{"registry-kind", []string{"include:tools/skill/a", "include:tools/command/a", "exclude:tools/skill/*"}, []string{"tools/command/a"}},
		{"sorted", []string{"include:skill/z", "include:skill/a"}, []string{"skill/a", "skill/z"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := v4ExclusionDecisions(tc.actions...)
			got := effectiveV4Requests(group)
			locators := make([]string, 0, len(got))
			for _, request := range got {
				locators = append(locators, request.Locator)
			}
			if got == nil || !reflect.DeepEqual(locators, tc.want) {
				t.Fatalf("requests = %#v, want locators %#v and a non-nil slice", got, tc.want)
			}
		})
	}
}

func TestV4EffectiveRequestsIgnoreRebuildFallbacks(t *testing.T) {
	group := v4ExclusionDecisions("include:skill/kept", "exclude:skill/absent")
	group.Include = []V4ResourceRequest{{Locator: "skill/absent"}, {Locator: "skill/fallback"}}
	group.Decisions = append(group.Decisions, V4SelectionDecision{Locator: "skill/nil-request"})
	want := []V4ResourceRequest{{Locator: "skill/kept"}}
	if got := effectiveV4Requests(group); !reflect.DeepEqual(got, want) {
		t.Fatalf("effective requests = %#v, want %#v without rebuild-only fallbacks", got, want)
	}
}

func TestV4EffectiveRequestsMatchPerLocatorInterpretation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		locators := []string{"skill/a", "command/a", "tools/skill/a", "tools/command/a", "other/skill/b"}
		exclusions := append(append([]string(nil), locators...), "*", "skill/*", "tools/*", "tools/skill/*", "skill/missing")
		group := V4ResourceGroup{SourceName: "g", Include: []V4ResourceRequest{{Locator: "skill/fallback"}}}
		count := rapid.IntRange(0, 64).Draw(t, "count")
		for i := range count {
			provenance := V4Provenance{File: "fragment.yaml", Line: i + 1, Precedence: i}
			if rapid.Bool().Draw(t, "exclude") {
				group.Decisions = append(group.Decisions, V4SelectionDecision{
					SourceName: "g", Action: V4SelectionExclude, Exclusion: rapid.SampledFrom(exclusions).Draw(t, "selector"), Provenance: provenance,
				})
				continue
			}
			locator := rapid.SampledFrom(locators).Draw(t, "locator")
			parts := strings.Split(locator, "/")
			registry := ""
			if len(parts) == 3 {
				registry = parts[0]
			}
			request := V4ResourceRequest{
				SourceName: "g", Locator: locator, Required: rapid.Bool().Draw(t, "required"),
				Registry: registry, Kind: parts[len(parts)-2], Name: parts[len(parts)-1],
				Options:   V4ResourceOptions{As: fmt.Sprintf("alias-%d", i), HasAs: true},
				Harnesses: []string{"codex"}, HasHarnesses: true,
				Reason: V4SelectionExplicit, Provenance: []V4Provenance{provenance},
			}
			group.Decisions = append(group.Decisions, V4SelectionDecision{
				SourceName: "g", Action: V4SelectionExplicit, Locator: locator, Request: &request, Provenance: provenance,
			})
		}
		before, err := json.Marshal(group)
		if err != nil {
			t.Fatal(err)
		}
		// The per-locator oracle also models rebuilding; this helper has no
		// include-only fallback, so interpret just its authored decisions.
		oracleGroup := group
		oracleGroup.Include = nil
		want, _ := interpretV4RequestsByLocator(map[string]V4ResourceGroup{"g": oracleGroup})
		if want == nil {
			want = []V4ResourceRequest{}
		}
		for range 2 {
			if got := effectiveV4Requests(group); !reflect.DeepEqual(got, want) {
				t.Fatalf("requests = %#v, want %#v", got, want)
			}
		}
		after, err := json.Marshal(group)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("effective request interpretation mutated authored input: %v", err)
		}
	})
}

func TestV4EffectiveRequestsReachRuntimeAndIntent(t *testing.T) {
	cfg, err := ParseV4([]byte(`apiVersion: agent-marketplace/v4
sources:
  z: {git: https://example.test/z.git, ref: main}
  a: {git: https://example.test/a.git, ref: main}
resources:
  z:
    include: [skill/shared, command/shared]
    exclude: [skill/shared]
  a:
    required: true
    include: [skill/keep]
    exclude: [skill/keep]
`))
	if err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	out, err := cfg.ToLegacyConfig(repoLayer)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Registries) != 2 ||
		!reflect.DeepEqual(out.Registries[0].Skills.Include, []string{"keep"}) ||
		len(out.Registries[0].Skills.Exclude) != 0 ||
		!reflect.DeepEqual(out.Registries[1].Commands.Include, []string{"shared"}) ||
		len(out.Registries[1].Skills.Include) != 0 ||
		!reflect.DeepEqual(out.Registries[1].Skills.Exclude, []string{"shared"}) {
		t.Fatalf("runtime adapter changed required/exact/kind selection: %#v", out.Registries)
	}
	layer := LayerIntent{Source: RepoCommitted, v4: cfg}
	var got []string
	for _, selection := range layer.ExplicitSelections() {
		got = append(got, fmt.Sprintf("%s/%s", selection.Kind, selection.Name))
	}
	if !reflect.DeepEqual(got, []string{"skill/keep", "command/shared"}) {
		t.Fatalf("explicit intent selections = %#v", got)
	}
	after, err := json.Marshal(cfg)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("runtime/intent consumers mutated configuration: %v", err)
	}
}

func TestV4EffectiveRequestsReachMarketplaceLock(t *testing.T) {
	cfg, err := ParseV4([]byte(`apiVersion: agent-marketplace/v4
sources: {m: {marketplace: https://market.example}}
resources:
  m:
    include: [tools/skill/absent, tools/skill/keep]
    exclude: [tools/skill/absent]
`))
	if err != nil {
		t.Fatal(err)
	}
	resource := v4LockResource("tools", "tools-id", "skill", "keep", v4sha("a"), v4sha("a"))
	resource.ArchiveDigest = lockfile.Digest(v4sha("b"))
	doc := lockfile.Document{
		APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo,
		Marketplaces: []lockfile.MarketplaceLock{{
			URL: "https://market.example", InstanceID: "market-id",
			RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "tools", RegistryID: "tools-id"}},
			Resources:        []lockfile.MarketplaceResourceLock{resource},
		}},
	}
	before, err := json.Marshal([]any{cfg, doc})
	if err != nil {
		t.Fatal(err)
	}
	out, err := cfg.ToLegacyConfigWithLock(repoLayer, &doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Marketplaces) != 1 || len(out.Marketplaces[0].Resources) != 1 || out.Marketplaces[0].Resources[0].Name != "keep" ||
		len(out.MarketplaceExclusions) != 1 || out.MarketplaceExclusions[0].Locator != "tools/skill/absent" {
		t.Fatalf("Marketplace adapter changed selected evidence or exclusions: %#v", out)
	}
	after, err := json.Marshal([]any{cfg, doc})
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("Marketplace adapter mutated configuration/lock: %v", err)
	}
}
