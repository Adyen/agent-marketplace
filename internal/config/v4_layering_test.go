package config

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

// spec: config-layering/duplicate-and-reserved-marketplace-aliases-are-rejected
// spec: config-layering/personal-local-duplicate-aliases-are-rejected-at-load
func TestV4MarketplaceSourceNames(t *testing.T) {
	for _, tc := range []struct{ name, sources, code string }{
		{"duplicate", "  m: {marketplace: https://market.example}\n  m: {marketplace: https://market.example}\n", "v4.duplicate-key"},
		{"reserved", "  all: {marketplace: https://market.example}\n", "v4.source-name"},
		{"control character", "  \"bad\\nname\": {marketplace: https://market.example}\n", "v4.source-name"},
	} {
		for _, source := range []Source{UserPersonal, RepoCommitted, RepoPersonal} {
			t.Run(source.String()+"/"+tc.name, func(t *testing.T) {
				fixture := newV4OnlyFixture(t)
				path := fixture.paths[source]
				before := v4OnlyWriteFixture(t, path, "apiVersion: agent-marketplace/v4\nsources:\n"+tc.sources)
				_, err := LoadV4ConfigIfPresent(path, source)
				var validation *V4ValidationError
				if !errors.As(err, &validation) || validation.Code != tc.code {
					t.Fatalf("error = %v, want %s", err, tc.code)
				}
				v4OnlyAssertUnchanged(t, path, before)
				v4OnlyWriteFixture(t, path, "apiVersion: agent-marketplace/v4\nsources: {m: {marketplace: https://MARKET.example/}}\n")
				if _, err := LoadV4ConfigIfPresent(path, source); err != nil {
					t.Fatalf("valid source name control: %v", err)
				}
			})
		}
	}
}

// spec: config-layering/v4-root-rejects-unsafe-marketplace-source-urls
// spec: config-layering/marketplace-entries-are-validated
// spec: config-layering/unsafe-identity-and-endpoint-fields-are-rejected
func TestV4MarketplaceSourceAndRequestValidation(t *testing.T) {
	for _, tc := range []struct{ name, body, code string }{
		{"credentials", "sources: {m: {marketplace: 'https://user:secret@market.example'}}", "v4.marketplace-url"},
		{"query", "sources: {m: {marketplace: 'https://market.example?token=secret'}}", "v4.marketplace-url"},
		{"fragment", "sources: {m: {marketplace: 'https://market.example#route'}}", "v4.marketplace-url"},
		{"traversal", "sources: {m: {marketplace: 'https://market.example/../admin'}}", "v4.marketplace-url"},
		{"encoded path", "sources: {m: {marketplace: 'https://market.example/%2fadmin'}}", "v4.marketplace-url"},
		{"missing URL", "sources: {m: {marketplace: ''}}", "v4.source-value"},
		{"unknown source field", "sources: {m: {marketplace: https://market.example, instance_id: identity}}", "v4.unknown-field"},
		{"unknown request field", "sources: {m: {marketplace: https://market.example}}\nresources: {m: {include: [{tools/skill/review: {digest: invalid}}]}}", "v4.unknown-field"},
		{"invalid kind", "sources: {m: {marketplace: https://market.example}}\nresources: {m: [tools/not-a-kind/review]}", "v4.locator"},
		{"invalid locator", "sources: {m: {marketplace: https://market.example}}\nresources: {m: [tools/skill/../review]}", "v4.locator"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseV4Config([]byte("apiVersion: agent-marketplace/v4\n"+tc.body+"\n"), "authored.yaml", true)
			var validation *V4ValidationError
			if !errors.As(err, &validation) || validation.Code != tc.code {
				t.Fatalf("error = %v, want %s", err, tc.code)
			}
		})
	}
	cfg, err := ParseV4([]byte("apiVersion: agent-marketplace/v4\nsources: {m: {marketplace: https://MARKET.example/}}\nresources: {m: [tools/skill/review]}\n"))
	if err != nil || cfg.Sources["m"].CanonicalURL != "https://market.example" || len(cfg.Requests) != 1 {
		t.Fatalf("valid endpoint/request control: %#v %v", cfg, err)
	}
}

// spec: config-layering/layered-alias-accepted-for-one-endpoint-identity
// spec: config-layering/layered-merge-retains-per-layer-provenance-and-first-set-allowlist
func TestV4MarketplaceLayerProvenanceAndAllowlist(t *testing.T) {
	fixture := newV4OnlyFixture(t)
	for _, source := range []Source{UserPersonal, RepoCommitted, RepoPersonal} {
		path := fixture.paths[source]
		body := v4MarketplaceConfigYAML
		if source == UserPersonal {
			body += "allowed_marketplaces: ['^https://market\\.example$']\n"
		}
		if source == RepoCommitted {
			body += "allowed_marketplaces: []\n"
		}
		v4OnlyWriteFixture(t, path, body)
		doc := v4LockFixture(t, lockfile.Scope(source.LockScope()))
		encoded, err := lockfile.Encode(doc)
		if err != nil {
			t.Fatal(err)
		}
		v4OnlyWriteFixture(t, filepath.Join(filepath.Dir(path), map[bool]string{true: "lock.local.yaml", false: "lock.yaml"}[source == RepoPersonal]), string(encoded))
	}
	ec := loadEffectiveForTest(t.Context(), LoadEffectiveOptions{RepoRoot: fixture.repo})
	if len(ec.Errors) != 0 {
		t.Fatal(errors.Join(ec.Errors...))
	}
	if len(ec.Merged.Marketplaces) != 3 || !slices.Equal(ec.AllowedMarketplaces, []string{"^https://market\\.example$"}) {
		t.Fatalf("merged Marketplace policy = %#v", ec)
	}
	for i, source := range []Source{UserPersonal, RepoCommitted, RepoPersonal} {
		entry := ec.Merged.Marketplaces[i]
		if entry.Source != source || entry.URL != "https://market.example" || entry.InstanceID != "market-id" || entry.Alias != "m" {
			t.Fatalf("layer[%d] lost provenance/identity: %#v", i, entry)
		}
	}
}

// spec: config-layering/allowed-marketplace-patterns-follow-include-authority-in-a-v4-root
func TestV4MarketplaceIncludeAllowlistAuthority(t *testing.T) {
	fixture := newV4OnlyFixture(t)
	path := fixture.paths[UserPersonal]
	fragment := filepath.Join(filepath.Dir(path), "fragment.yaml")
	before := v4OnlyWriteFixture(t, fragment, "allowed_marketplaces: ['^https://market\\.example$']\nsources: {m: {marketplace: https://market.example}}\nresources: {m: [tools/skill/from-fragment]}\n")
	v4OnlyWriteFixture(t, path, "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nallowed_marketplaces: []\n")
	intent := ReadIntent(t.Context(), LoadEffectiveOptions{RepoRoot: fixture.repo})
	if len(intent.Errors) != 0 || !slices.Equal(intent.AllowedMarketplaces(), []string{"^https://market\\.example$"}) || len(intent.Layers[UserPersonal].MarketplaceSources()) != 1 || intent.Layers[UserPersonal].MarketplaceSources()[0].Requests[0].Name != "from-fragment" {
		t.Fatalf("include authority lost: %#v", intent)
	}
	if err := SetHarnessesInConfig(path, []string{"claude"}); err != nil {
		t.Fatal(err)
	}
	v4OnlyAssertUnchanged(t, fragment, before)
}
