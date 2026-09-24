package config

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestLoadEffectiveCanceledContextDoesNotBind(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ec := LoadEffective(ctx, LoadEffectiveOptions{})
	if len(ec.Errors) != 1 || !errors.Is(ec.Errors[0], context.Canceled) {
		t.Fatalf("errors = %v, want one context.Canceled", ec.Errors)
	}
	if ec.Merged != nil {
		t.Fatalf("merged = %#v, want nil", ec.Merged)
	}
	for _, lr := range ec.LayerResults() {
		if lr.Config != nil || lr.Local != nil || len(lr.Errors) > 0 || len(lr.Reads) > 0 {
			t.Fatalf("layer result = %#v, want zero", lr)
		}
	}
}

func TestLayerIntentViews(t *testing.T) {
	dir := t.TempDir()
	v4, err := ParseV4Config([]byte(`apiVersion: agent-marketplace/v4
harnesses: [claude, devin]
allowed_registries: []
allowed_marketplaces: [https://mp-allowed.example]
sources:
  zdir: {directory: ./regs}
  agit: {git: https://example.com/repo.git, ref: main}
  mmp: {marketplace: https://market.example}
resources:
  agit: {all: true}
  mmp: {include: [main/collection/onboarding]}
  zdir: {all: true}
`), dir+"/config.yaml", true)
	if err != nil {
		t.Fatal(err)
	}
	li := LayerIntent{Source: UserPersonal, v4: v4}

	wantSources := []SourceDecl{
		{Kind: V4SourceGit, CanonicalURL: "https://example.com/repo"},
		{Kind: V4SourceMarketplace, CanonicalURL: "https://market.example"},
		{Kind: V4SourceDirectory},
	}
	if got := li.Sources(); !reflect.DeepEqual(got, wantSources) {
		t.Fatalf("sources = %#v, want %#v", got, wantSources)
	}
	if !li.NeedsLock() {
		t.Fatal("v4 layer with git and acquiring marketplace sources must need a lock")
	}
	if !li.NeedsMarketplaceLock() {
		t.Fatal("v4 layer with an acquiring marketplace source must need a marketplace lock")
	}

	settings := li.Settings()
	if settings.AllowedRegistries == nil || len(*settings.AllowedRegistries) != 0 {
		t.Fatalf("allowed registries = %#v, want explicit empty", settings.AllowedRegistries)
	}
	if settings.AllowedMarketplaces == nil || !reflect.DeepEqual(*settings.AllowedMarketplaces, []string{"https://mp-allowed.example"}) {
		t.Fatalf("allowed marketplaces = %#v", settings.AllowedMarketplaces)
	}
	(*settings.AllowedMarketplaces)[0] = "mutated"
	if (*v4.AllowedMarketplaces)[0] != "https://mp-allowed.example" {
		t.Fatal("Settings must clone allowlists")
	}
	settings.Harnesses[0] = "mutated"
	if v4.Harnesses[0] != "claude" {
		t.Fatal("Settings must clone harnesses")
	}

	legacy := &Config{
		Registries: []RegistryEntry{
			{URL: "https://example.com/legacy.git"},
			{URL: dir + "/local-regs"},
		},
		Marketplaces: []MarketplaceEntry{{URL: "https://mp.example"}},
	}
	ll := LayerIntent{Source: UserPersonal, legacy: legacy}
	wantLegacySources := []SourceDecl{
		{Kind: V4SourceGit, CanonicalURL: "https://example.com/legacy"},
		{Kind: V4SourceDirectory},
		{Kind: V4SourceMarketplace, CanonicalURL: "https://mp.example"},
	}
	if got := ll.Sources(); !reflect.DeepEqual(got, wantLegacySources) {
		t.Fatalf("legacy sources = %#v, want %#v", got, wantLegacySources)
	}
	if !ll.NeedsLock() {
		t.Fatal("legacy layer with a remote git registry must need a lock")
	}
	if ll.NeedsMarketplaceLock() {
		t.Fatal("legacy layer must not need a marketplace lock")
	}
}

func TestBindPreservesLayerErrorOrder(t *testing.T) {
	orgEvidenceErr := errors.New("organization evidence error")
	userReadErr := errors.New("user config read error")

	intent := Intent{
		Layers: [5]LayerIntent{
			Organization:  {Source: Organization, legacy: &Config{}},
			UserPersonal:  {Source: UserPersonal, errors: []error{userReadErr}},
			RepoCommitted: {Source: RepoCommitted},
			RepoPersonal:  {Source: RepoPersonal},
		},
	}
	for _, source := range intentLayerOrder {
		intent.Errors = append(intent.Errors, intent.Layers[source].errors...)
	}

	ev := Evidence{}
	ev.errors[Organization] = []error{orgEvidenceErr}

	ec := Bind(intent, ev)

	want := append([]error{}, ec.Organization.Errors...)
	want = append(want, ec.User.Errors...)
	want = append(want, ec.Repo.Errors...)
	want = append(want, ec.LocalLayer.Errors...)
	if len(ec.Errors) != len(want) {
		t.Fatalf("errors = %v, want %v", ec.Errors, want)
	}
	for i := range want {
		if ec.Errors[i] != want[i] {
			t.Fatalf("errors[%d] = %v, want %v (full errors %v)", i, ec.Errors[i], want[i], ec.Errors)
		}
	}
	if len(ec.Errors) != 2 || ec.Errors[0] != orgEvidenceErr || ec.Errors[1] != userReadErr {
		t.Fatalf("errors = %v, want [organization evidence error, user read error]", ec.Errors)
	}
	counts := map[error]int{}
	for _, err := range ec.Errors {
		counts[err]++
	}
	if counts[orgEvidenceErr] != 1 || counts[userReadErr] != 1 {
		t.Fatalf("errors = %v, want each error exactly once", ec.Errors)
	}
}

func TestIntentViews(t *testing.T) {
	org := LayerIntent{Source: Organization, legacy: &Config{Harnesses: []string{"cursor"}}}
	user := LayerIntent{Source: UserPersonal, legacy: &Config{Harnesses: []string{"devin"}}}
	repo := LayerIntent{Source: RepoCommitted, legacy: &Config{Harnesses: []string{"claude"}}}

	intent := Intent{Layers: [5]LayerIntent{Organization: org, UserPersonal: user, RepoCommitted: repo}}
	harnesses, source := intent.TargetHarnesses()
	if source != RepoCommitted || !reflect.DeepEqual(harnesses, []string{"claude"}) {
		t.Fatalf("target harnesses = %v from %v, want [claude] from repo", harnesses, source)
	}
	harnesses[0] = "mutated"
	if repo.legacy.Harnesses[0] != "claude" {
		t.Fatal("TargetHarnesses must return a clone")
	}

	intent.Layers[RepoCommitted] = LayerIntent{Source: RepoCommitted}
	harnesses, source = intent.TargetHarnesses()
	if source != UserPersonal || !reflect.DeepEqual(harnesses, []string{"devin"}) {
		t.Fatalf("target harnesses = %v from %v, want [devin] from user", harnesses, source)
	}

	intent.Layers[UserPersonal] = LayerIntent{Source: UserPersonal}
	harnesses, source = intent.TargetHarnesses()
	if source != Organization || !reflect.DeepEqual(harnesses, []string{"cursor"}) {
		t.Fatalf("target harnesses = %v from %v, want [cursor] from organization", harnesses, source)
	}

	intent.Layers[Organization] = LayerIntent{Source: Organization}
	if harnesses, source := intent.TargetHarnesses(); harnesses != nil || source != SourceUnset {
		t.Fatalf("target harnesses = %v from %v, want unset", harnesses, source)
	}
}
